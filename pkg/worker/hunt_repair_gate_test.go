package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntRepairDockKB adds a repair-service station at pollux (beside the belt)
// to a KB built by base, and marks haven_station as having repair or not.
func huntRepairDockKB(t *testing.T, base knowledge.Base, havenRepairs bool) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(base.RememberBase(ctx, knowledge.SpaceBase{
		ID: "haven_station", POIID: "haven_station", PublicAccess: true,
		Services: map[string]bool{"repair": havenRepairs},
	}))
	must(base.RememberPOI(ctx, knowledge.POI{ID: "pollux_dock", SystemID: "pollux", Type: "station"}))
	must(base.RememberBase(ctx, knowledge.SpaceBase{
		ID: "pollux_dock", POIID: "pollux_dock", PublicAccess: true,
		Services: map[string]bool{"repair": true},
	}))
}

// A Station Need never flies out to a ground on a hull below the repair bar.
// The dock it read the board at has no repair service, so it goes to the
// nearest station that has one -- and does not hunt this pass.
func TestHuntNeedGoesToARepairStationBeforeTheGround(t *testing.T) {
	kb := huntNeedKBFixture(t)
	huntRepairDockKB(t, kb.Base, false)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{shipHullFrac: 0.5, creatures: huntGrazers("c1", "c2", "c3")})
	f.repairErr = errors.New("no repair service here")
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "travel:pollux_outer_belt") || f.huntCalls != 0 {
		t.Fatalf("flew to the ground on a damaged hull; calls %v\n%s", f.calls, log.String())
	}
	if !called(f.huntFake, "travel:pollux_dock") || lastCall(f.huntFake, "dock") < firstCall(f.huntFake, "travel:pollux_dock") {
		t.Fatalf("want a leg to the repair station pollux_dock and a dock there; calls %v\n%s", f.calls, log.String())
	}
}

// When the dock DOES offer repair and the repair still failed (credits), the
// hunter holds where it is rather than flying anywhere.
func TestHuntKillMissionHoldsForRepair(t *testing.T) {
	kb := huntNeedKBFixture(t)
	huntRepairDockKB(t, kb.Base, true)
	c := newHuntFake(t, huntFakeOpts{
		board:        []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures:    huntGrazers("c1"),
		shipHullFrac: 0.5,
	})
	c.repairErr = errors.New("insufficient credits")
	var log strings.Builder
	deps := huntDeps(c, &log)
	deps.KB = kb
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(c, "undock") || called(c, "travel:") || c.huntCalls != 0 {
		t.Fatalf("a damaged hull at a repair dock must hold; calls %v\n%s", c.calls, log.String())
	}
	if !strings.Contains(log.String(), "held for repair") {
		t.Errorf("the hold must be logged, got:\n%s", log.String())
	}
}

// With nowhere known to repair, hold too: never out to a ground.
func TestHuntHoldsWhenNoRepairStationIsKnown(t *testing.T) {
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{shipHullFrac: 0.5, creatures: huntGrazers("c1")})
	f.repairErr = errors.New("no repair service here")
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "undock") || called(f.huntFake, "travel:") || f.huntCalls != 0 {
		t.Fatalf("no repair anywhere: must hold docked; calls %v\n%s", f.calls, log.String())
	}
}
