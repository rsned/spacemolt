package worker

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/reload"
)

// huntAmmoNeed is one ammo type's provisioning need: which item to bring and
// how many of it are missing for the chosen reloads per weapon. ItemID is
// empty when neither a loaded weapon nor the depot names one; the market then
// decides.
type huntAmmoNeed struct {
	AmmoType string
	ItemID   string
	Missing  int
}

// huntAmmoNeeds works out, per ammo type fitted, the item to bring and how
// many are missing for `reloads` per weapon (one ammo item is one reload).
// Everything aboard that fits the type counts. The item is the one loaded in
// a weapon of that type, else the fitting item with most in storage, else
// empty. storage and lookup may be nil.
func huntAmmoNeeds(lo reload.Loadout, reloads int, storage map[string]int, lookup reload.Lookup) []huntAmmoNeed {
	var order []string
	weapons := map[string]int{}
	loaded := map[string]string{}
	aboard := map[string]map[string]int{}
	for _, e := range lo.Plan {
		if _, ok := weapons[e.AmmoType]; !ok {
			order = append(order, e.AmmoType)
			aboard[e.AmmoType] = map[string]int{}
		}
		weapons[e.AmmoType]++
		if loaded[e.AmmoType] == "" {
			loaded[e.AmmoType] = e.LoadedAmmoID
		}
		for _, c := range e.Candidates {
			aboard[e.AmmoType][c.ItemID] = int(c.Quantity)
		}
	}

	needs := make([]huntAmmoNeed, 0, len(order))
	for _, at := range order {
		have := 0
		for _, n := range aboard[at] {
			have += n
		}
		item := loaded[at]
		if item == "" {
			item = huntBestStoredAmmo(storage, at, lookup)
		}
		needs = append(needs, huntAmmoNeed{AmmoType: at, ItemID: item, Missing: max(0, weapons[at]*reloads-have)})
	}
	return needs
}

// huntBestStoredAmmo is the item of ammoType with most in storage (ties by
// id); empty when storage holds none or lookup is nil. A loaded item is only
// preferred over it while something here can supply it; see huntProvisionStock.
func huntBestStoredAmmo(storage map[string]int, ammoType string, lookup reload.Lookup) string {
	if lookup == nil {
		return ""
	}
	item, best := "", 0
	for id, n := range storage {
		if _, t := lookup(id); t != ammoType || n <= 0 {
			continue
		}
		if n > best || (n == best && id < item) {
			item, best = id, n
		}
	}
	return item
}

// huntProvisionOptions offers each reloads-per-weapon bucket whose extra cargo
// fits in the hold left after the job's reserve and the loot reserve.
func huntProvisionOptions(lo reload.Loadout, freeHold, reserve int) []huntdecide.Option {
	budget := max(0, freeHold-reserve-huntLootReserve)
	var opts []huntdecide.Option
	for _, r := range huntProvisionBuckets {
		cargo := 0
		for _, n := range huntAmmoNeeds(lo, r, nil, nil) {
			cargo += n.Missing
		}
		if cargo > budget {
			continue
		}
		opts = append(opts, huntdecide.Option{
			ID:       huntProvisionOptionID(r),
			Features: map[string]any{"reloads_per_weapon": r, "cargo_units": cargo},
		})
	}
	return opts
}

// huntLoadoutArmed reports whether the ship can fire, by huntCanFire's rule:
// an ammo-free weapon is fitted, or an ammo-fed one holds rounds, or fitting
// ammo for one is aboard (its Candidates are the fitting stacks in cargo). A
// dry magazine with ammo aboard is loaded by the reload gate at the ground.
func huntLoadoutArmed(lo reload.Loadout) bool {
	if len(lo.Free) > 0 {
		return true
	}
	return slices.ContainsFunc(lo.Plan, func(e reload.Entry) bool {
		return e.CurrentAmmo > 0 || slices.ContainsFunc(e.Candidates, func(c reload.Candidate) bool { return c.Quantity > 0 })
	})
}

// huntProvision runs docked, with a job in hand, before the hunter leaves. It
// brings ammunition aboard from station storage, buys any shortfall on the
// station market under a credit cap, loads the weapons and reports whether
// the ship can shoot. armed=false means no weapon can fire and the hunter must
// not leave. reserve is hold space the job needs kept free (a Station Need's
// quantity; 0 otherwise). Only the reloads-per-weapon choice goes through the
// decision seam; a failure to read the ship fails open.
func huntProvision(ctx context.Context, deps HuntDeps, out io.Writer, who string, job huntJob, s *huntdecide.Sortie, reserve int) (armed bool) {
	lo, err := reload.FetchLoadout(ctx, deps.Client, deps.KB)
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: provision skipped: %v\n", who, err) //nolint:errcheck
		return true
	}
	if len(lo.Plan) == 0 {
		return true // energy weapons only: nothing to provision
	}

	st := deps.Client.GetState()
	freeHold := 0
	if st != nil {
		freeHold = int(st.Ship.CargoCapacity - st.Ship.CargoUsed)
	}
	station := ""
	if st != nil {
		station = st.CurrentPOI
	}

	var rec *huntProvisionRecord
	opts := huntProvisionOptions(lo, freeHold, reserve)
	if len(opts) > 0 {
		seq := s.NextSeq()
		choice := huntDecider(deps).Decide(ctx, huntdecide.Decision{
			Kind: huntdecide.KindProvision, SortieID: s.ID, Seq: seq,
			State: huntDecisionState(ctx, deps, job, s, lo), Options: opts,
		})
		reloads := huntProvisionChosen(opts, choice.OptionID)
		rec = huntProvisionStock(ctx, deps, out, who, lo, reloads)
		rec.seq = seq
		rec.reloads = reloads
	}

	// Load what is aboard, then read the ship again to see whether it can fire:
	// loaded rounds, or fitting ammo aboard should the dock-side reload have
	// been refused (huntLoadoutArmed).
	armed = huntReload(ctx, deps, out, who)
	if after, ferr := reload.FetchLoadout(ctx, deps.Client, deps.KB); ferr == nil {
		armed = huntLoadoutArmed(after)
	}

	if rec != nil {
		_ = deps.Decisions.Append(huntNow(deps), huntdecide.OutcomeRecord{
			Type: "outcome", At: huntNow(deps), SortieID: s.ID, Seq: rec.seq, Phase: "provision",
			Data: map[string]any{
				"wanted": rec.wanted, "from_storage": rec.fromStorage, "from_market": rec.fromMarket,
				"credits_paid": rec.paid, "depot_left": rec.depotLeft, "armed": armed,
			},
		})
		if rec.storageRead && deps.HomeStation != "" && huntAtStation(ctx, deps.KB, st, deps.HomeStation) {
			for _, low := range rec.depotLow {
				fmt.Fprintf(out, "hunt[%s]: DEPOT LOW: %s %d left at %s (under %d sorties)\n", //nolint:errcheck
					who, low.item, low.left, deps.HomeStation, huntDepotLowSorties)
			}
		}
	}

	if !armed && len(opts) == 0 {
		fmt.Fprintf(out, "hunt[%s]: BLOCKED UNARMED at %s: no weapon can fire and the hold has no room for ammo (free %d, reserved %d)\n", //nolint:errcheck
			who, station, freeHold, reserve)
	} else if !armed {
		types := make([]string, 0, len(lo.Plan))
		for _, e := range lo.Plan {
			if !slices.Contains(types, e.AmmoType) {
				types = append(types, e.AmmoType)
			}
		}
		fmt.Fprintf(out, "hunt[%s]: BLOCKED UNARMED at %s: no weapon can fire and no %s ammo in storage or on the market within the cap\n", //nolint:errcheck
			who, station, strings.Join(types, "/"))
	}
	return armed
}

// huntProvisionChosen is the reloads-per-weapon of the option the decider
// picked, or of the first option when the pick is not among them.
func huntProvisionChosen(opts []huntdecide.Option, id string) int {
	pick := opts[0]
	for _, o := range opts {
		if o.ID == id {
			pick = o
		}
	}
	if n, ok := pick.Features["reloads_per_weapon"].(int); ok {
		return n
	}
	return huntProvisionDefault
}

// huntDepotLowItem is a provisioned item whose depot stock is under the
// low-water mark.
type huntDepotLowItem struct {
	item string
	left int
}

// huntProvisionRecord is what one provisioning did, for the outcome record.
type huntProvisionRecord struct {
	seq, reloads int
	wanted       map[string]int
	fromStorage  map[string]int
	fromMarket   map[string]int
	depotLeft    map[string]int
	paid         float64
	storageRead  bool
	depotLow     []huntDepotLowItem
}

// huntProvisionStock brings the ammo the chosen reloads need aboard: storage
// first, then the station market under the credit cap and floor. A withdraw or
// buy error is logged and that ammo type is skipped.
func huntProvisionStock(ctx context.Context, deps HuntDeps, out io.Writer, who string, lo reload.Loadout, reloads int) *huntProvisionRecord {
	rec := &huntProvisionRecord{
		wanted: map[string]int{}, fromStorage: map[string]int{}, fromMarket: map[string]int{}, depotLeft: map[string]int{},
	}
	lookup := reload.KBLookup(ctx, deps.KB)

	var held map[string]int
	if serr := deps.Client.ViewStorage(ctx); serr != nil {
		fmt.Fprintf(out, "hunt[%s]: view_storage failed: %v\n", who, serr) //nolint:errcheck
	} else if h, ok := consignHeld(deps.Client.GetRawJSON("storage")); ok {
		held, rec.storageRead = h, true
	}

	needs := huntAmmoNeeds(lo, reloads, held, lookup)
	var listings []game.MarketListing
	marketRead, marketOK := false, false
	ensureMarket := func() bool {
		if !marketRead {
			marketRead = true
			if merr := deps.Client.ViewMarket(ctx, map[string]any{}); merr != nil {
				fmt.Fprintf(out, "hunt[%s]: view_market failed: %v\n", who, merr) //nolint:errcheck
			} else {
				listings, marketOK = deps.Client.GetMarketListings(), true
			}
		}
		return marketOK
	}
	spent := 0.0
	spendCap := deps.AmmoCreditCap
	if spendCap <= 0 {
		spendCap = huntDefaultAmmoCreditCap
	}

	for _, need := range needs {
		if need.Missing == 0 {
			continue
		}
		item, missing := need.ItemID, need.Missing
		// The loaded item is preferred only while something here can supply
		// it; otherwise another fitting item from storage, else the market's
		// cheapest, keeps the hunter armed.
		if item != "" && held[item] <= 0 && (!ensureMarket() || len(huntCheapestListings(listings, need, lookup)) == 0) {
			item = huntBestStoredAmmo(held, need.AmmoType, lookup)
			need.ItemID = item
		}
		if item != "" {
			rec.wanted[item] = missing
		}

		if n := min(missing, held[item]); item != "" && n > 0 {
			if werr := deps.Client.WithdrawItems(ctx, item, float64(n)); werr != nil {
				fmt.Fprintf(out, "hunt[%s]: withdraw %d %s failed: %v\n", who, n, item, werr) //nolint:errcheck
				continue
			}
			held[item] -= n
			rec.fromStorage[item] += n
			missing -= n
		}
		if item != "" && rec.storageRead {
			rec.depotLeft[item] = held[item]
			if left := held[item]; left < huntDepotLowSorties*weaponsOfType(lo, need.AmmoType)*reloads {
				rec.depotLow = append(rec.depotLow, huntDepotLowItem{item: item, left: left})
			}
		}
		if missing <= 0 {
			continue
		}

		if !ensureMarket() {
			continue
		}
		cheap := huntCheapestListings(listings, need, lookup)
		if len(cheap) == 0 {
			continue
		}
		if item == "" {
			item = cheap[0].ItemID
			rec.wanted[item] = missing
			cheap = slices.DeleteFunc(cheap, func(l game.MarketListing) bool { return l.ItemID != item })
		}
		for _, l := range cheap {
			if missing <= 0 {
				break
			}
			n := min(missing, int(l.Quantity))
			if l.PricePerUnit > 0 {
				n = min(n, int((spendCap-spent)/l.PricePerUnit))
				// Credits are read fresh: a buy changes them, and the floor is
				// for fuel and repair.
				if serr := deps.Client.GetStatus(ctx); serr != nil {
					fmt.Fprintf(out, "hunt[%s]: get_status failed, not buying %s: %v\n", who, item, serr) //nolint:errcheck
					break
				}
				if cur := deps.Client.GetState(); cur != nil {
					n = min(n, int((cur.Credits-huntCreditFloor)/l.PricePerUnit))
				}
			}
			if n <= 0 {
				break
			}
			if berr := deps.Client.Buy(ctx, item, float64(n)); berr != nil {
				fmt.Fprintf(out, "hunt[%s]: buy %d %s failed: %v\n", who, n, item, berr) //nolint:errcheck
				break
			}
			spent += l.PricePerUnit * float64(n)
			rec.fromMarket[item] += n
			missing -= n
		}
	}
	rec.paid = spent
	return rec
}

// weaponsOfType counts the fitted weapons that take ammoType.
func weaponsOfType(lo reload.Loadout, ammoType string) int {
	n := 0
	for _, e := range lo.Plan {
		if e.AmmoType == ammoType {
			n++
		}
	}
	return n
}

// huntCheapestListings returns the station's sell listings for the need,
// cheapest first: the named item's, or any item of the ammo type when none is
// named.
func huntCheapestListings(listings []game.MarketListing, need huntAmmoNeed, lookup reload.Lookup) []game.MarketListing {
	var out []game.MarketListing
	for _, l := range listings {
		if l.Type != "sell" || l.Quantity < 1 {
			continue
		}
		if need.ItemID != "" {
			if l.ItemID != need.ItemID {
				continue
			}
		} else if _, t := lookup(l.ItemID); t != need.AmmoType {
			continue
		}
		out = append(out, l)
	}
	slices.SortStableFunc(out, func(a, b game.MarketListing) int {
		switch {
		case a.PricePerUnit < b.PricePerUnit:
			return -1
		case a.PricePerUnit > b.PricePerUnit:
			return 1
		}
		return strings.Compare(a.ItemID, b.ItemID)
	})
	return out
}
