package worker

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// MineQtyMaxDryPasses bounds how many consecutive Mine() calls that grow the
// target item's cargo count by zero MineQty tolerates before it gives up on
// the belt and delivers whatever it has aboard. A pass that DOES grow cargo
// resets this counter back to zero.
const MineQtyMaxDryPasses = 5

// MineQtyMinFuelReserve is the fuel a worker must hold before MineQty will
// leave a station. Below it the verb refuses the trip rather than starting one
// it cannot finish.
//
// This exists because MineQty had no fuel guard at all. On 2026-07-12
// craftsman-10 took a mine_qty node, flew out on a tank it could not return
// on, mined down to roughly five fuel, froze undocked with no way back,
// absorbed three stall-restarts and was quarantined; craftsman-2 repeated it
// on the retry. A refused trip is recoverable by refuelling. A stranded miner
// needs the rescue fleet.
//
// Deliberately a flat floor and not a computed round-trip cost: jump fuel is
// ceil(scale^1.5 * speed) per jump and depends on the hull, so a wrong
// estimate would fail toward departing. A flat floor fails toward staying.
const MineQtyMinFuelReserve = 25

// mineResourcePOITypes are the resource-bearing POI types findMinePOI falls
// back to searching by type when the KB's poi_resources table has no entry
// naming the target item at any known POI (e.g. the deposit has never been
// surveyed). Mirrors the resource-POI vocabulary tokens.go's knownPOITypes
// enumerates (minus station/base/planet/etc., which never yield mine
// resources).
var mineResourcePOITypes = []string{
	"asteroid_belt", "asteroid", "asteroid_field", "gas_cloud", "ice_field", "nebula",
}

// MineQty mines ITEM at its nearest known resource POI until QTY units are in
// cargo (recomputed against whatever is already aboard, so a resumed run
// doesn't over-mine) or the belt stops yielding, then hands the haul off to
// RECIPIENT at TO via Deliver's empty-FROM mode (the goods are already in
// cargo — Deliver skips the withdraw leg and travels straight to TO).
//
// Bounded: stops mining after MineQtyMaxDryPasses consecutive Mine() calls
// that grow the item's cargo count by zero, or as soon as the cargo hold has
// no free space left — either way, whatever got mined is delivered and this
// returns nil (partial success is success; the plan runner's DoneQty
// semantics handle shortfalls). Only a hard failure (no known resource POI at
// all, a Mine() error, or a Deliver error such as a bad recipient) returns an
// error.
func (d *WorkerDispatch) MineQty(ctx context.Context, itemID string, qty int, to, recipient string) error {
	if qty < 1 {
		return fmt.Errorf("mine_qty: qty must be >= 1, got %d", qty)
	}
	state := d.Client.GetState()
	if state == nil || state.System.ID == "" {
		return fmt.Errorf("mine_qty: current system unknown")
	}
	current := state.System.ID

	// Checked before findMinePOI so a grounded miner spends no lookups and,
	// more importantly, never reaches autopilotAndUndock.
	if state.Ship.Fuel < MineQtyMinFuelReserve {
		return fmt.Errorf("mine_qty: fuel %d below reserve %d — refusing to depart (refuel first)",
			int(state.Ship.Fuel), MineQtyMinFuelReserve)
	}

	if cargoCount(state, itemID) < qty {
		sys, poi, err := d.findMinePOI(ctx, current, itemID, d.mineStrongholdRefs(ctx, state))
		if err != nil {
			return fmt.Errorf("mine_qty: locate resource %q: %w", itemID, err)
		}
		if err := d.autopilotAndUndock(ctx, sys, poi); err != nil {
			return fmt.Errorf("mine_qty: travel to %s: %w", poi, err)
		}
		if err := d.mineLoop(ctx, itemID, qty, poi); err != nil {
			return err
		}
	}

	return d.deliverMined(ctx, itemID, qty, to, recipient)
}

// deliverMined hands whatever target-item cargo is aboard (capped at qty) to
// RECIPIENT at TO. Tail of MineQty; MineQtyAt uses handOffHold instead.
func (d *WorkerDispatch) deliverMined(ctx context.Context, itemID string, qty int, to, recipient string) error {
	minedQty := cargoCount(d.Client.GetState(), itemID)
	if minedQty <= 0 {
		return fmt.Errorf("mine_qty: %s not found at any known resource POI and none in cargo", itemID)
	}
	return d.Deliver(ctx, itemID, min(minedQty, qty), "", to, recipient)
}

// MineQtyAt is MineQty pinned to an explicit (system, POI) instead of the
// nearest known deposit. It applies the same fuel floor and stronghold gate,
// plus the Lawless gate: a bot without the pirate unlock refuses a Lawless
// system, while unknown security fails open with a warning on d.Out.
func (d *WorkerDispatch) MineQtyAt(ctx context.Context, itemID string, qty int, systemID, poiID, to, recipient string) error {
	if qty < 1 {
		return fmt.Errorf("mine_qty_at: qty must be >= 1, got %d", qty)
	}
	state := d.Client.GetState()
	if state == nil || state.System.ID == "" {
		return fmt.Errorf("mine_qty_at: current system unknown")
	}
	if state.Ship.Fuel < MineQtyMinFuelReserve {
		return fmt.Errorf("mine_qty_at: fuel %d below reserve %d — refusing to depart (refuel first)",
			int(state.Ship.Fuel), MineQtyMinFuelReserve)
	}
	if d.KB == nil {
		return fmt.Errorf("mine_qty_at: no knowledge base configured")
	}
	sys, err := d.KB.GetSystem(ctx, systemID)
	if err != nil {
		return fmt.Errorf("mine_qty_at: look up system %q: %w", systemID, err)
	}
	if sys != nil && strongholdBlocked(d.mineStrongholdRefs(ctx, state), sys.ID, sys.Name) {
		return fmt.Errorf("mine_qty_at: %s is a pirate stronghold and this agent lacks the pirate unlock", systemID)
	}
	blocked, reason := lawlessBlocked(sys, d.pirateUnlocked(ctx))
	if blocked {
		return fmt.Errorf("mine_qty_at: lawless gate: %s", reason)
	}
	if reason != "" {
		fmt.Fprintf(d.Out, "mine_qty_at: %s\n", reason) //nolint:errcheck
	}

	if cargoCount(state, itemID) < qty {
		if err = d.autopilotAndUndock(ctx, systemID, poiID); err != nil {
			return fmt.Errorf("mine_qty_at: travel to %s: %w", poiID, err)
		}
		if err = d.mineLoop(ctx, itemID, qty, poiID); err != nil {
			return err
		}
	}
	delivered, err := d.handOffHold(ctx, to, recipient, nil)
	if err != nil {
		return err
	}
	if delivered == 0 {
		fmt.Fprintf(d.Out, "mine_qty_at: hold empty after mining %s at %s — nothing to hand off\n", itemID, poiID) //nolint:errcheck
	}
	return nil
}

// handOffHold delivers every cargo line not in keep to RECIPIENT at TO, largest
// quantity first, so a byproduct mined alongside the target does not sit in the
// hold. fuel_cell is always kept: it is the refuel fallback. It returns the
// number of cargo lines delivered. The first failed delivery aborts the rest,
// since a bad recipient or station fails every line.
func (d *WorkerDispatch) handOffHold(ctx context.Context, to, recipient string, keep map[string]bool) (delivered int, err error) {
	state := d.Client.GetState()
	if state == nil {
		return 0, nil
	}
	lines := slices.Clone(state.Ship.Cargo)
	slices.SortStableFunc(lines, func(a, b game.CargoItem) int { return cmp.Compare(b.Quantity, a.Quantity) })
	for _, line := range lines {
		if line.Quantity <= 0 || line.ItemID == "fuel_cell" || keep[line.ItemID] {
			continue
		}
		if err := d.Deliver(ctx, line.ItemID, int(line.Quantity), "", to, recipient); err != nil {
			return delivered, fmt.Errorf("hand off %s: %w", line.ItemID, err)
		}
		delivered++
	}
	return delivered, nil
}

// mineLoop repeatedly calls Mine(ctx) — sleeping game.SleepTick (via
// minePollSleep, tests inject a zero-delay stand-in) between calls — until
// cargo holds qty of itemID, the hold has no free space left, or
// MineQtyMaxDryPasses consecutive passes grow cargo by zero. poiLabel is used
// only for log/error messages.
func (d *WorkerDispatch) mineLoop(ctx context.Context, itemID string, qty int, poiLabel string) error {
	dryPasses := 0
	for cargoCount(d.Client.GetState(), itemID) < qty {
		if cargoFreeSpace(d.Client.GetState()) <= 0 {
			fmt.Fprintf(d.Out, "mine_qty: cargo full at %d/%d %s — stopping to deliver\n", //nolint:errcheck
				cargoCount(d.Client.GetState(), itemID), qty, itemID)
			return nil
		}
		before := cargoCount(d.Client.GetState(), itemID)
		if err := d.Client.Mine(ctx); err != nil {
			return fmt.Errorf("mine_qty: mine at %s: %w", poiLabel, err)
		}
		sleep := d.minePollSleep
		if sleep == nil {
			sleep = craftPollSleepFunc
		}
		if err := sleep(ctx, game.SleepTick); err != nil {
			return err
		}
		after := cargoCount(d.Client.GetState(), itemID)
		if after > before {
			dryPasses = 0
			continue
		}
		dryPasses++
		if dryPasses >= MineQtyMaxDryPasses {
			fmt.Fprintf(d.Out, "mine_qty: %d consecutive dry passes mining %s at %s — stopping with %d/%d aboard\n", //nolint:errcheck
				dryPasses, itemID, poiLabel, after, qty)
			return nil
		}
	}
	return nil
}

// autopilotAndUndock routes to (system, poi) via Autopilot and ensures the
// ship ends up undocked — mining requires being at the resource POI AND
// undocked. Autopilot's final in-system hop (autopilotTravelToPOI) already
// leaves the ship undocked in the common case; this only issues an explicit
// Undock when the ship is still (or again) docked on arrival, e.g. a prior
// task left it docked somewhere in the destination system.
func (d *WorkerDispatch) autopilotAndUndock(ctx context.Context, system, poi string) error {
	if err := Autopilot(ctx, AutopilotDeps{Client: d.Client, Out: d.Out, KB: d.KB}, system, poi); err != nil {
		return err
	}
	if state := d.Client.GetState(); state != nil && state.IsDocked() {
		return d.Client.Undock(ctx)
	}
	return nil
}

// mineCandidateSlate is how many nearest systems findMinePOI considers before
// giving up. One was enough when every candidate was acceptable; with the
// stronghold gate the nearest belt may be unusable, and 10 covers the ten
// mineable POIs that sit inside pirate strongholds without walking the galaxy.
const mineCandidateSlate = 10

// strongholdBlocked reports whether a destination is off-limits. Both the id
// and the name are checked because buildStrongholdRefs registers both: seven
// strongholds are dual-named between base id and poi id, so an id-only match
// silently misses them.
func strongholdBlocked(strongholds map[string]bool, systemID, systemName string) bool {
	if len(strongholds) == 0 {
		return false
	}
	return strongholds[systemID] || (systemName != "" && strongholds[systemName])
}

// lawlessBlocked reports whether a bot may not mine in sys. A Lawless system
// has no police protection, and a bot whose pirate baseline is below the unlock
// threshold is a pirate target there, so a locked bot is refused.
//
// Unknown security (nil system or empty SecurityStatus) fails OPEN with a
// warning reason: the KB simply has not captured the system yet, which is not
// evidence that it is dangerous, and blocking would strand the miner.
func lawlessBlocked(sys *knowledge.System, unlocked bool) (blocked bool, reason string) {
	if sys == nil || sys.SecurityStatus == "" {
		id := "system"
		if sys != nil {
			id = sys.ID
		}
		return false, fmt.Sprintf("security of %s unknown; proceeding", id)
	}
	if unlocked || !strings.Contains(strings.ToLower(sys.SecurityStatus), "lawless") {
		return false, ""
	}
	return true, fmt.Sprintf("%s is Lawless (%s) and this agent lacks the pirate unlock", sys.ID, sys.SecurityStatus)
}

// pirateUnlocked reports whether this agent holds the pirate unlock, read from
// the latest standings capture. No asset store, or a read error, means false:
// the safe reading for a gate is "locked". A read error is logged to d.Out.
func (d *WorkerDispatch) pirateUnlocked(ctx context.Context) bool {
	if d.Assets == nil {
		return false
	}
	ok, err := d.Assets.HoldsPirateUnlock(ctx, d.AgentID)
	if err != nil {
		fmt.Fprintf(d.Out, "mine_qty: pirate unlock check: %v (treating as locked)\n", err) //nolint:errcheck
		return false
	}
	return ok
}

// mineStrongholdRefs is the mining path's copy of haul's guard: the stronghold
// systems THIS agent must avoid, read from live standings so an agent that
// banks the pirate unlock mid-run picks it up on the next pass.
//
// A KB we cannot read yields the empty set, which would open every stronghold.
// That is the one direction this must not fail, so an error falls back to
// blocking every stronghold we last knew about rather than none.
func (d *WorkerDispatch) mineStrongholdRefs(ctx context.Context, state *game.State) map[string]bool {
	if d.KB == nil {
		return nil
	}
	systems, err := d.KB.GetSystems(ctx)
	if err != nil {
		fmt.Fprintf(d.Out, "mine_qty: stronghold guard: read systems: %v (treating all strongholds as blocked)\n", err) //nolint:errcheck
		return nil
	}
	return strongholdRefsFor(state, systems)
}

// findMinePOI locates the nearest known resource POI yielding itemID,
// starting from currentSystem. Primary lookup: the KB's poi_resources table
// for itemID specifically (galaxy.FindNearestByResource), which naturally
// checks currentSystem first — FindNearest's BFS starts there, so a match in
// the current system comes back at zero hops before any farther system is
// considered. Fallback (used when no POI is known anywhere to carry itemID —
// e.g. the deposit has never been surveyed): the nearest POI of any
// resource-bearing type (galaxy.FindNearestByPOIType over
// mineResourcePOITypes), mirroring shuttle.go's shuttleRecoverIfStranded
// resource-POI lookup pattern — build a galaxy.GalaxyGraph, call the galaxy
// finder, resolve the destination POI id via KB.GetPOIs.
func (d *WorkerDispatch) findMinePOI(ctx context.Context, currentSystem, itemID string, strongholds map[string]bool) (system, poi string, err error) {
	if d.KB == nil {
		return "", "", fmt.Errorf("find resource poi: no knowledge base configured")
	}
	graph := &galaxy.GalaxyGraph{}
	if err = graph.BuildFromDB(ctx, d.KB); err != nil {
		return "", "", fmt.Errorf("build galaxy graph: %w", err)
	}

	// Ask for a slate rather than the single nearest: the nearest belt is often
	// the one nobody safely mines, so the gate needs runners-up to fall back to.
	results, err := galaxy.FindNearestByResource(ctx, d.KB, graph, currentSystem, itemID, mineCandidateSlate)
	if err != nil {
		return "", "", fmt.Errorf("find resource %q: %w", itemID, err)
	}
	blocked := false
	for _, r := range results {
		if len(r.POIs) == 0 {
			continue
		}
		if strongholdBlocked(strongholds, r.SystemID, r.SystemName) {
			blocked = true
			continue
		}
		return r.SystemID, r.POIs[0].ID, nil
	}

	for _, poiType := range mineResourcePOITypes {
		typeResults, terr := galaxy.FindNearestByPOIType(ctx, d.KB, graph, currentSystem, poiType, mineCandidateSlate)
		if terr != nil || len(typeResults) == 0 {
			continue
		}
		for _, tr := range typeResults {
			if strongholdBlocked(strongholds, tr.SystemID, tr.SystemName) {
				blocked = true
				continue
			}
			pois, perr := d.KB.GetPOIs(ctx, tr.SystemID)
			if perr != nil {
				continue
			}
			for _, p := range pois {
				if p.Type == poiType {
					return tr.SystemID, p.ID, nil
				}
			}
		}
	}
	// Distinguish "nowhere known" from "known, but every candidate would get us
	// killed" -- the operator response differs, and a silent 'not found' on a
	// stronghold-only resource reads as a survey gap that no survey can close.
	if blocked {
		return "", "", fmt.Errorf("no reachable resource POI for %q outside a pirate stronghold (complete the pirate unlock to mine there)", itemID)
	}
	return "", "", fmt.Errorf("no known resource POI for %q", itemID)
}
