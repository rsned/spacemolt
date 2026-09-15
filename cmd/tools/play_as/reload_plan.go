package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// `reload` takes two ids the pilot has no way to produce mid-fight: the
// weapon's MODULE INSTANCE id from get_ship, and the item id of an ammo type
// that fits it. explorer-8 hit an empty magazine inside a creature battle and
// the only answer the REPL gave was the usage string.
//
// Both halves are already knowable. get_ship's modules carry id, ammo_type,
// magazine_size and current_ammo; the catalog pairs a weapon's ammo_type with
// an ammo item's effect.subtype (missile_launcher_i/ii -> missile ->
// standard_guided_missiles, railgun -> ferrous_slug_case, autocannon ->
// standard_rounds_box, and so on). This resolves the two against each other.

// reloadAmmoCandidate is one ammo stack in cargo that fits a given weapon.
type reloadAmmoCandidate struct {
	ItemID   string
	Name     string
	Quantity float64
}

// reloadPlanEntry is one fitted, ammo-using weapon and what can feed it.
type reloadPlanEntry struct {
	WeaponID     string // module INSTANCE id — what reload wants, not the type id
	WeaponName   string
	WeaponTypeID string
	AmmoType     string
	CurrentAmmo  int
	MagazineSize int
	LoadedAmmoID string
	// Candidates are the fitting ammo stacks in cargo, best first.
	Candidates []reloadAmmoCandidate
}

// Empty reports a dry magazine — the weapon cannot fire at all.
func (e reloadPlanEntry) Empty() bool { return e.CurrentAmmo <= 0 }

// Full reports a magazine with no room, which reload would refuse.
func (e reloadPlanEntry) Full() bool {
	return e.MagazineSize > 0 && e.CurrentAmmo >= e.MagazineSize
}

// ammoLookup resolves an item id to its display name and ammo type. An empty
// ammoType means the item is not ammunition. Backed by the knowledge base's
// item catalog; injected so the resolver stays pure.
type ammoLookup func(itemID string) (name, ammoType string)

// buildReloadPlan pairs each ammo-using fitted weapon with the ammo in cargo
// that fits it. Energy weapons (no ammo type) and non-weapon modules are
// skipped: they never need reloading.
func buildReloadPlan(modules []serverapi.ShipModule, cargo []serverapi.CargoItem, lookup ammoLookup) []reloadPlanEntry {
	var plan []reloadPlanEntry

	for _, m := range modules {
		if m.Type != "" && m.Type != "weapon" {
			continue
		}
		ammoType := m.AmmoType
		if ammoType == "" && lookup != nil && m.TypeID != "" {
			// The live get_ship shape carries ammo_type, but we have no
			// capture proving the server always populates it; the catalog
			// knows it from the module's type id either way.
			_, ammoType = lookup(m.TypeID)
		}
		if ammoType == "" {
			continue
		}

		entry := reloadPlanEntry{
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
			entry.Candidates = append(entry.Candidates, reloadAmmoCandidate{
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

		plan = append(plan, entry)
	}

	return plan
}

// reloadTarget is one resolved reload: the pair of ids the command needs.
type reloadTarget struct {
	WeaponID   string
	WeaponName string
	AmmoItemID string
	AmmoName   string
}

// reloadAllTargets picks the weapons `reload all` should act on: dry magazines
// with ammo aboard to fill them. A partial magazine is left alone — topping it
// up costs a tick mid-fight and risks discarding the loaded rounds — and a dry
// weapon with no matching cargo cannot be helped.
func reloadAllTargets(plan []reloadPlanEntry) []reloadTarget {
	var targets []reloadTarget
	for _, e := range plan {
		if !e.Empty() || len(e.Candidates) == 0 {
			continue
		}
		targets = append(targets, reloadTarget{
			WeaponID:   e.WeaponID,
			WeaponName: e.WeaponName,
			AmmoItemID: e.Candidates[0].ItemID,
			AmmoName:   e.Candidates[0].Name,
		})
	}
	return targets
}

// formatReloadPlan renders the plan as the answer to a bare `reload`: every
// ammo-using weapon, its magazine state, and the exact command to fill it.
func formatReloadPlan(plan []reloadPlanEntry) []string {
	if len(plan) == 0 {
		return []string{"No fitted weapon takes ammo (energy weapons need no reload)."}
	}

	lines := []string{"Fitted weapons that take ammo (one reload per module, one tick each):"}
	for _, e := range plan {
		name := e.WeaponName
		if name == "" {
			name = e.WeaponTypeID
		}
		head := fmt.Sprintf("  %s (%s) — %d/%d %s", name, e.WeaponTypeID, e.CurrentAmmo, e.MagazineSize, e.AmmoType)
		switch {
		case e.Empty():
			head += "  EMPTY"
		case e.Full():
			head += "  full"
		}
		lines = append(lines, head)

		if len(e.Candidates) == 0 {
			lines = append(lines, fmt.Sprintf("    no %s ammo in cargo", e.AmmoType))
			continue
		}
		best := e.Candidates[0]
		label := best.Name
		if label == "" {
			label = best.ItemID
		}
		lines = append(lines, fmt.Sprintf("    reload %s %s   (%s %s in cargo)",
			e.WeaponID, best.ItemID, num(best.Quantity), label))
		for _, alt := range e.Candidates[1:] {
			altLabel := alt.Name
			if altLabel == "" {
				altLabel = alt.ItemID
			}
			lines = append(lines, fmt.Sprintf("    or: reload %s %s   (%s %s in cargo)",
				e.WeaponID, alt.ItemID, num(alt.Quantity), altLabel))
		}
	}
	lines = append(lines, "Run `reload all` to reload every empty weapon in sequence.")
	return lines
}

// kbAmmoLookup resolves item ids against the knowledge base's item catalog,
// which holds both halves of the pairing: an ammo item's own ammo type
// (CatalogItem.Ammo) and a weapon module's required one
// (CatalogItem.Module.Weapon). Results are memoised — a plan looks up every
// cargo stack against every weapon.
func kbAmmoLookup(ctx context.Context, kb knowledge.Base) ammoLookup {
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
			switch {
			case item.Ammo != nil:
				e.ammoType = item.Ammo.AmmoType
			case item.Module != nil && item.Module.Weapon != nil:
				e.ammoType = item.Module.Weapon.AmmoType
			}
		}
		cache[itemID] = e
		return e.name, e.ammoType
	}
}

// fetchReloadPlan refreshes get_ship and resolves its weapons against cargo.
func fetchReloadPlan(ctx context.Context, client game.GameClient) ([]reloadPlanEntry, error) {
	if err := client.GetShip(ctx); err != nil {
		return nil, fmt.Errorf("get_ship: %w", err)
	}
	raw := client.GetRawJSON("ship")
	if len(raw) == 0 {
		return nil, fmt.Errorf("get_ship returned nothing")
	}
	var resp serverapi.GetShipResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse get_ship: %w", err)
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

	return buildReloadPlan(resp.Modules, cargo, kbAmmoLookup(ctx, globalKB)), nil
}

// showReloadPlan answers a bare `reload`.
func showReloadPlan(ctx context.Context, client game.GameClient) error {
	plan, err := fetchReloadPlan(ctx, client)
	if err != nil {
		return err
	}
	for _, line := range formatReloadPlan(plan) {
		fmt.Println(line)
	}
	if globalKB == nil {
		fmt.Println("(no knowledge base: run with --db-path to resolve ammo types from the catalog)")
	}
	return nil
}

// reloadAll reloads every dry weapon that has ammo aboard. The server charges
// a tick per module, so these are issued in sequence, not fired off together.
func reloadAll(ctx context.Context, client game.GameClient, format outputFormat) error {
	plan, err := fetchReloadPlan(ctx, client)
	if err != nil {
		return err
	}
	targets := reloadAllTargets(plan)
	if len(targets) == 0 {
		fmt.Println("Nothing to reload: no weapon is both empty and has matching ammo in cargo.")
		for _, line := range formatReloadPlan(plan) {
			fmt.Println(line)
		}
		return nil
	}

	var failed int
	for i, t := range targets {
		fmt.Printf("[%d/%d] reload %s (%s) ← %s\n", i+1, len(targets), t.WeaponName, t.WeaponID, t.AmmoItemID)
		cmd := fmt.Sprintf("reload %s %s", t.WeaponID, t.AmmoItemID)
		err := simpleCommand(client, func(ctx context.Context) error {
			return client.Reload(ctx, t.WeaponID, t.AmmoItemID)
		}, ctx, 3*time.Second, cmd, format)
		if err != nil {
			failed++
			fmt.Printf("   failed: %v\n", err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d reloads failed", failed, len(targets))
	}
	return nil
}
