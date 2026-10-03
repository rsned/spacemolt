package worker

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func unloadState(poi, home string, used, capacity float64, cargo []game.CargoItem) *game.State {
	return &game.State{
		Player:     game.Player{ID: "p1", HomeBase: home},
		CurrentPOI: poi,
		Ship:       game.Ship{ID: "s1", CargoUsed: used, CargoCapacity: capacity, Cargo: cargo},
	}
}

// TestUnloadRefreshesCargoBeforeDecidingTheHoldIsEmpty is the same stale-read
// bug as missionResume, in a second function. missionUnloadAtHomeBase judged the
// hold off the cached clone and returned early on len(items)==0, so cargo the
// client had never been told about could not be unloaded -- the ~1.9M credits of
// supplied contraband sitting across the unlock fleet was structurally
// unreachable, even with the worker parked on its own home base.
func TestUnloadRefreshesCargoBeforeDecidingTheHoldIsEmpty(t *testing.T) {
	fc := &fakeClient{state: unloadState("home_station", "home_station", 300, 315, nil)}
	fc.onGetCargo = func() {
		fc.state.Ship.Cargo = []game.CargoItem{{ItemID: "starshine", Quantity: 200}}
	}
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())

	missionUnloadAtHomeBase(context.Background(), deps, io.Discard)

	if !slices.ContainsFunc(fc.calls, func(c string) bool { return c == "deposit_all" || c == "sell:starshine" }) {
		t.Fatalf("a hold the server says is full must be unloaded, not read as empty: %v", fc.calls)
	}
}

// TestUnloadFreesAJammedHoldAwayFromHome covers the second half of the deadlock.
// The five stuck unlock agents are parked at the mission GIVER, not their home
// base, with holds at 96-97%: explorer-4 at 451/465 cannot accept an
// an_introduction grant of 15 units, and re-accepting is the only way to get the
// stronghold passage back after an abandon released it. Unload stays passive --
// it never diverts the worker -- but when the worker is already docked somewhere
// with a hold too jammed to take mission cargo, waiting for a home visit that may
// never come just keeps it stuck.
func TestUnloadFreesAJammedHoldAwayFromHome(t *testing.T) {
	fc := &fakeClient{state: unloadState("treasure_cache_trading_post", "central_nexus", 451, 465,
		[]game.CargoItem{{ItemID: "starshine", Quantity: 300}, {ItemID: "nerve_burn", Quantity: 151}})}
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())

	missionUnloadAtHomeBase(context.Background(), deps, io.Discard)

	if !slices.Contains(fc.calls, "deposit_all") {
		t.Fatalf("a jammed hold at a dockable station must be freed: %v", fc.calls)
	}
}

// The passivity that must not regress: a worker with plenty of room, docked away
// from home, keeps its cargo. Unloading everywhere would scatter goods across
// the galaxy and strip holds that are doing useful work.
func TestUnloadLeavesARoomyHoldAloneAwayFromHome(t *testing.T) {
	fc := &fakeClient{state: unloadState("some_station", "central_nexus", 100, 465,
		[]game.CargoItem{{ItemID: "iron_ore", Quantity: 100}})}
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())

	missionUnloadAtHomeBase(context.Background(), deps, io.Discard)

	if slices.Contains(fc.calls, "deposit_all") {
		t.Fatalf("a roomy hold away from home must be left alone: %v", fc.calls)
	}
}
