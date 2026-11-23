// Package reload resolves which fitted weapons need ammunition and which cargo
// stack feeds each one. It is shared by play_as (the `reload` command) and the
// worker fleet (hunters reloading between engagements).
package reload

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// unwrapActionResult returns the inner "result" object of an action_result
// frame, or raw unchanged when there is none.
func unwrapActionResult(raw []byte) []byte {
	var probe struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || len(probe.Result) == 0 {
		return raw
	}
	return probe.Result
}

// Candidate is one ammo stack in cargo that fits a given weapon.
type Candidate struct {
	ItemID   string
	Name     string
	Quantity float64
}

// Entry is one fitted, ammo-using weapon and what can feed it.
type Entry struct {
	WeaponID     string // module INSTANCE id — what reload wants, not the type id
	WeaponName   string
	WeaponTypeID string
	AmmoType     string
	CurrentAmmo  int
	MagazineSize int
	LoadedAmmoID string
	// Candidates are the fitting ammo stacks in cargo, best first.
	Candidates []Candidate
}

// Empty reports a dry magazine — the weapon cannot fire at all.
func (e Entry) Empty() bool { return e.CurrentAmmo <= 0 }

// Full reports a magazine with no room, which reload would refuse.
func (e Entry) Full() bool {
	return e.MagazineSize > 0 && e.CurrentAmmo >= e.MagazineSize
}

// Lookup resolves an item id to its display name and ammo type. An empty
// ammoType means the item is not ammunition. Backed by the knowledge base's
// item catalog; injected so the resolver stays pure.
type Lookup func(itemID string) (name, ammoType string)

// BuildPlan pairs each ammo-using fitted weapon with the ammo in cargo
// that fits it. Energy weapons (no ammo type) and non-weapon modules are
// skipped: they never need reloading.
func BuildPlan(modules []serverapi.ShipModule, cargo []serverapi.CargoItem, lookup Lookup) []Entry {
	return BuildLoadout(modules, cargo, lookup).Plan
}

// FreeWeapon is a fitted weapon module that takes no ammunition.
type FreeWeapon struct{ ModuleID, TypeID string }

// Loadout is every fitted weapon, split by whether it needs ammunition.
type Loadout struct {
	Plan []Entry      // ammo-fed weapons, as BuildPlan returns
	Free []FreeWeapon // weapon modules with no ammo type
}

// BuildLoadout is BuildPlan plus the weapons BuildPlan skips: it returns the
// ammo-fed plan and, separately, the weapon modules with no ammo type.
func BuildLoadout(modules []serverapi.ShipModule, cargo []serverapi.CargoItem, lookup Lookup) Loadout {
	return BuildLoadoutWith(modules, cargo, lookup, lookup)
}

// BuildLoadoutWith is BuildLoadout with the two questions asked of separate
// lookups: ammo says which cargo items are ammunition, weapon says which ammo
// type a fitted module takes when the wire omits it. They must not be one
// catalog lookup that answers both: a spare weapon module in the hold then
// reads as ammunition for its own kind (pirate-10 looped on reloading its
// Railgun I with a railgun_i from cargo, 2026-11-23).
func BuildLoadoutWith(modules []serverapi.ShipModule, cargo []serverapi.CargoItem, ammo, weapon Lookup) Loadout {
	lookup := ammo
	var lo Loadout

	for _, m := range modules {
		if m.Type != "" && m.Type != "weapon" {
			continue
		}
		ammoType := m.AmmoType
		if ammoType == "" && weapon != nil && m.TypeID != "" {
			// The live get_ship shape carries ammo_type, but we have no
			// capture proving the server always populates it; the catalog
			// knows it from the module's type id either way.
			_, ammoType = weapon(m.TypeID)
		}
		if ammoType == "" {
			lo.Free = append(lo.Free, FreeWeapon{ModuleID: m.ID, TypeID: m.TypeID})
			continue
		}

		entry := Entry{
			WeaponID:     m.ID,
			WeaponName:   m.Name,
			WeaponTypeID: m.TypeID,
			AmmoType:     ammoType,
			CurrentAmmo:  m.CurrentAmmo,
			MagazineSize: m.MagazineSize,
			LoadedAmmoID: m.LoadedAmmoID,
		}

		for _, c := range cargo {
			if c.Quantity <= 0 || lookup == nil {
				continue
			}
			name, at := lookup(c.ItemID)
			if at != ammoType {
				continue
			}
			if name == "" {
				name = c.Name
			}
			entry.Candidates = append(entry.Candidates, Candidate{
				ItemID: c.ItemID, Name: name, Quantity: c.Quantity,
			})
		}

		// Rank the ammo already in the magazine first: reloading a partial
		// magazine with a different type throws the remaining rounds away
		// (ReloadResponse.rounds_discarded). Then most-stocked, then id, so
		// the command we print does not change between calls.
		loaded := entry.LoadedAmmoID
		sort.SliceStable(entry.Candidates, func(i, j int) bool {
			a, b := entry.Candidates[i], entry.Candidates[j]
			if (a.ItemID == loaded) != (b.ItemID == loaded) {
				return a.ItemID == loaded
			}
			if a.Quantity != b.Quantity {
				return a.Quantity > b.Quantity
			}
			return a.ItemID < b.ItemID
		})

		lo.Plan = append(lo.Plan, entry)
	}

	return lo
}

// Target is one resolved reload: the pair of ids the command needs.
type Target struct {
	WeaponID   string
	WeaponName string
	AmmoItemID string
	AmmoName   string
}

// Targets picks the weapons `reload all` should act on: dry magazines
// with ammo aboard to fill them. A partial magazine is left alone — topping it
// up costs a tick mid-fight and risks discarding the loaded rounds — and a dry
// weapon with no matching cargo cannot be helped.
func Targets(plan []Entry) []Target {
	var targets []Target
	for _, e := range plan {
		if !e.Empty() || len(e.Candidates) == 0 {
			continue
		}
		targets = append(targets, Target{
			WeaponID:   e.WeaponID,
			WeaponName: e.WeaponName,
			AmmoItemID: e.Candidates[0].ItemID,
			AmmoName:   e.Candidates[0].Name,
		})
	}
	return targets
}

// formatReloadPlan renders the plan as the answer to a bare `reload`: every

// KBLookup resolves AMMUNITION item ids against the knowledge base's item
// catalog: the name and the ammo type of an ammo item, and an empty ammo type
// for everything else, a weapon module included. Results are memoised -- a
// plan looks up every cargo stack against every weapon.
func KBLookup(ctx context.Context, kb knowledge.Base) Lookup {
	return kbLookup(ctx, kb, func(item *knowledge.CatalogItem) string {
		if item.Ammo != nil {
			return item.Ammo.AmmoType
		}
		return ""
	})
}

// KBWeaponLookup resolves a weapon module's type id to the ammo type it
// takes (CatalogItem.Module.Weapon), empty for anything that is not an
// ammo-fed weapon.
func KBWeaponLookup(ctx context.Context, kb knowledge.Base) Lookup {
	return kbLookup(ctx, kb, func(item *knowledge.CatalogItem) string {
		if item.Module != nil && item.Module.Weapon != nil {
			return item.Module.Weapon.AmmoType
		}
		return ""
	})
}

func kbLookup(ctx context.Context, kb knowledge.Base, ammoType func(*knowledge.CatalogItem) string) Lookup {
	type entry struct{ name, ammoType string }
	cache := map[string]entry{}

	return func(itemID string) (string, string) {
		if kb == nil || itemID == "" {
			return "", ""
		}
		if e, ok := cache[itemID]; ok {
			return e.name, e.ammoType
		}
		e := entry{}
		if item, err := kb.GetItem(ctx, itemID); err == nil && item != nil {
			e.name = item.Name
			e.ammoType = ammoType(item)
		}
		cache[itemID] = e
		return e.name, e.ammoType
	}
}

// Fetch refreshes get_ship and resolves its weapons against cargo.
func Fetch(ctx context.Context, client game.GameClient, kb knowledge.Base) ([]Entry, error) {
	lo, err := FetchLoadout(ctx, client, kb)
	if err != nil {
		return nil, err
	}
	return lo.Plan, nil
}

// FetchLoadout refreshes get_ship and resolves its weapons against cargo,
// returning both the ammo-fed plan and the ammo-free weapons.
func FetchLoadout(ctx context.Context, client game.GameClient, kb knowledge.Base) (Loadout, error) {
	if err := client.GetShip(ctx); err != nil {
		return Loadout{}, fmt.Errorf("get_ship: %w", err)
	}
	raw := client.GetRawJSON("ship")
	if len(raw) == 0 {
		return Loadout{}, fmt.Errorf("get_ship returned nothing")
	}
	var resp serverapi.GetShipResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return Loadout{}, fmt.Errorf("parse get_ship: %w", err)
	}

	// Cargo comes from the same reply's ship block, so the magazine state and
	// the ammo count are from one moment rather than two.
	cargo := resp.Ship.Cargo
	if len(cargo) == 0 {
		// get_ship omitted the cargo block; fall back to the client's own
		// copy, which carries only ids and quantities.
		if st := client.GetState(); st != nil {
			for _, c := range st.Ship.Cargo {
				cargo = append(cargo, serverapi.CargoItem{ItemID: c.ItemID, Quantity: c.Quantity})
			}
		}
	}

	return BuildLoadoutWith(resp.Modules, cargo, KBLookup(ctx, kb), KBWeaponLookup(ctx, kb)), nil
}
