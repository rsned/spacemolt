package worker

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
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

	minedQty := cargoCount(d.Client.GetState(), itemID)
	if minedQty <= 0 {
		return fmt.Errorf("mine_qty: %s not found at any known resource POI and none in cargo", itemID)
	}
	deliverQty := min(minedQty, qty)
	return d.Deliver(ctx, itemID, deliverQty, "", to, recipient)
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
	if err := Autopilot(ctx, AutopilotDeps{Client: d.Client, Out: d.Out}, system, poi); err != nil {
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
	if err := graph.BuildFromDB(ctx, d.KB); err != nil {
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
