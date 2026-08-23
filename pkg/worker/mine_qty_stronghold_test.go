package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// findMinePOI ranked resource POIs by pure BFS distance with no regard for who
// controls the system. Ten mineable POIs sit inside pirate strongholds --
// including zaniah, where six agents have been destroyed, and algol, where
// assist-sol lost a 97k tanker on 2026-08-15. An agent at the -30 pirate
// default is attacked on sight there, so "nearest" was quietly "deadliest".

// newStrongholdMineKB puts the ONLY belt carrying resourceID inside a pirate
// stronghold one jump out, and a farther belt carrying it in safe space.
func newStrongholdMineKB(t *testing.T, resourceID string) *knowledge.SQLiteKB {
	t.Helper()
	kb := newDeliverTestKB(t)
	ctx := context.Background()

	systems := []knowledge.System{
		{ID: "mine_sys", Name: "Mine Sys", LastUpdatedTick: 1},
		{ID: "zaniah", Name: "Zaniah", IsStronghold: true, LastUpdatedTick: 1},
		{ID: "safe_far", Name: "Safe Far", LastUpdatedTick: 1},
	}
	for _, s := range systems {
		if err := kb.RememberSystem(ctx, s); err != nil {
			t.Fatalf("RememberSystem(%s): %v", s.ID, err)
		}
	}
	// mine_sys -- zaniah (1 hop) and mine_sys -- safe_far (2 hops via zaniah's
	// neighbour) so the stronghold is strictly nearer.
	conns := [][2]string{{"mine_sys", "zaniah"}, {"zaniah", "safe_far"}}
	for _, c := range conns {
		if err := kb.RememberConnection(ctx, c[0], c[1]); err != nil {
			t.Fatalf("RememberConnection(%s,%s): %v", c[0], c[1], err)
		}
	}
	for _, p := range []knowledge.POI{
		{ID: "zaniah_belt", SystemID: "zaniah", Name: "Zaniah Belt", Type: "asteroid_belt",
			Resources: []game.POIResource{{ResourceID: resourceID, Richness: 90, Remaining: 9000}}, LastUpdatedTick: 1},
		{ID: "safe_belt", SystemID: "safe_far", Name: "Safe Belt", Type: "asteroid_belt",
			Resources: []game.POIResource{{ResourceID: resourceID, Richness: 50, Remaining: 5000}}, LastUpdatedTick: 1},
	} {
		if err := kb.RememberPOI(ctx, p); err != nil {
			t.Fatalf("RememberPOI(%s): %v", p.ID, err)
		}
	}
	return kb
}

func lockedMineState() *game.State {
	st := &game.State{}
	st.System.ID = "mine_sys"
	st.Ship.Fuel = 130
	st.Ship.MaxFuel = 130
	st.Player.Standings = map[string]game.EmpireStanding{
		"pirate_zaniah": {Reputation: -30, Baseline: -30},
	}
	return st
}

func TestFindMinePOISkipsStrongholdsWhenLocked(t *testing.T) {
	kb := newStrongholdMineKB(t, "raw_ore")
	d := noWaitMineDispatch(nil, kb, t.TempDir())

	sys, poi, err := d.findMinePOI(context.Background(), "mine_sys", "raw_ore",
		strongholdRefsFor(lockedMineState(), mustSystems(t, kb)))
	if err != nil {
		t.Fatalf("findMinePOI: %v", err)
	}
	if sys == "zaniah" || poi == "zaniah_belt" {
		t.Fatalf("locked miner routed into pirate stronghold: sys=%s poi=%s", sys, poi)
	}
	if sys != "safe_far" || poi != "safe_belt" {
		t.Errorf("want safe_far/safe_belt, got %s/%s", sys, poi)
	}
}

func TestFindMinePOIUsesStrongholdsWhenUnlocked(t *testing.T) {
	kb := newStrongholdMineKB(t, "raw_ore")
	d := noWaitMineDispatch(nil, kb, t.TempDir())

	st := lockedMineState()
	st.Player.Standings = map[string]game.EmpireStanding{
		"pirate_zaniah": {Reputation: 10, Baseline: 10},
	}
	sys, _, err := d.findMinePOI(context.Background(), "mine_sys", "raw_ore",
		strongholdRefsFor(st, mustSystems(t, kb)))
	if err != nil {
		t.Fatalf("findMinePOI: %v", err)
	}
	if sys != "zaniah" {
		t.Errorf("unlocked miner should use the nearer stronghold belt, got %s", sys)
	}
}

func TestFindMinePOIUnreadableStandingsFailsClosed(t *testing.T) {
	kb := newStrongholdMineKB(t, "raw_ore")
	d := noWaitMineDispatch(nil, kb, t.TempDir())

	// nil state == a capability we cannot read. Guessing unlocked costs the ship.
	sys, _, err := d.findMinePOI(context.Background(), "mine_sys", "raw_ore",
		strongholdRefsFor(nil, mustSystems(t, kb)))
	if err != nil {
		t.Fatalf("findMinePOI: %v", err)
	}
	if sys == "zaniah" {
		t.Error("unreadable standings must be treated as LOCKED, but routed to the stronghold")
	}
}

func TestFindMinePOIStrongholdOnlyReturnsAnError(t *testing.T) {
	// The only belt carrying the ore is in a stronghold: refuse, don't fly.
	kb := newDeliverTestKB(t)
	ctx := context.Background()
	for _, s := range []knowledge.System{
		{ID: "mine_sys", Name: "Mine Sys", LastUpdatedTick: 1},
		{ID: "zaniah", Name: "Zaniah", IsStronghold: true, LastUpdatedTick: 1},
	} {
		if err := kb.RememberSystem(ctx, s); err != nil {
			t.Fatalf("RememberSystem: %v", err)
		}
	}
	if err := kb.RememberConnection(ctx, "mine_sys", "zaniah"); err != nil {
		t.Fatalf("RememberConnection: %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{
		ID: "zaniah_belt", SystemID: "zaniah", Name: "Zaniah Belt", Type: "asteroid_belt",
		Resources: []game.POIResource{{ResourceID: "raw_ore", Richness: 90, Remaining: 9000}}, LastUpdatedTick: 1,
	}); err != nil {
		t.Fatalf("RememberPOI: %v", err)
	}
	d := noWaitMineDispatch(nil, kb, t.TempDir())
	_, _, err := d.findMinePOI(ctx, "mine_sys", "raw_ore",
		strongholdRefsFor(lockedMineState(), mustSystems(t, kb)))
	if err == nil {
		t.Fatal("expected an error when every candidate is a stronghold")
	}
	if !strings.Contains(err.Error(), "stronghold") {
		t.Errorf("error should name the reason, got %q", err)
	}
}

func mustSystems(t *testing.T, kb knowledge.Base) []knowledge.System {
	t.Helper()
	sys, err := kb.GetSystems(context.Background())
	if err != nil {
		t.Fatalf("GetSystems: %v", err)
	}
	return sys
}
