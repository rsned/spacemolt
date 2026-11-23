package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// A held need whose only ground lies beyond the starting radius must not
// livelock at radius 6 until it expires: each failed ground search for THAT
// need widens its own radius (persisted across passes), on the same schedule
// as huntNeedRadius, until the ground is in reach.
func TestHuntHeldNeedWidensItsRadiusOnFailedSearches(t *testing.T) {
	ctx := context.Background()
	kb := huntNeedKBFixture(t)
	// A policed chain test_system - x1 - ... - x8, belt_grazers only at x8.
	prev := "test_system"
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("x%d", i)
		conns := []knowledge.SystemConnection{{SystemID: prev}}
		if i < 8 {
			conns = append(conns, knowledge.SystemConnection{SystemID: fmt.Sprintf("x%d", i+1)})
		}
		if err := kb.RememberSystem(ctx, knowledge.System{ID: id, PoliceLevel: 55, LastVisitedTick: 1, Connections: conns}); err != nil {
			t.Fatal(err)
		}
		prev = id
	}
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID: "test_system", Name: "Test", PoliceLevel: 55, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "pollux"}, {SystemID: "x1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "far_belt", SystemID: "x8", Type: "asteroid_belt"}); err != nil {
		t.Fatal(err)
	}
	kb.sightings = []knowledge.WildlifeSighting{{
		Species: "belt_grazer", SystemID: "x8", POIID: "far_belt",
		Source: knowledge.WildlifeSourceNearby, ObservedCount: 4, ObservedUTC: huntAgo(time.Hour),
	}}

	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{huntCausticActive(2)},
		creatures:      huntGrazers("c1"),
	})
	var log strings.Builder
	deps := huntNeedDeps(f, kb, &log)
	reached := false
	for pass := range 4 {
		f.state.CurrentPOI, f.state.Doc = "haven_station", true
		if err := Hunt(ctx, deps); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if called(f.huntFake, "travel:far_belt") {
			reached = true
			break
		}
	}
	if !reached {
		t.Fatalf("a held need never widened its search to an 8-jump ground; log:\n%s", log.String())
	}
	if called(f.huntFake, "abandon") {
		t.Errorf("a need that found its ground within the patience must not be abandoned; calls %v", f.calls)
	}
}

// A held need with no ground anywhere is abandoned after huntNeedAbandonAfter
// dry searches, and remembered so the board entry is not taken straight back.
// Observed live 2026-11-22: pirate-5 and pirate-10 each sat on such a need,
// which blocks every other job until it expires.
func TestHuntAbandonsAHeldNeedAfterDrySearches(t *testing.T) {
	ctx := context.Background()
	kb := huntNeedKBFixture(t)
	kb.sightings = nil
	held := huntCausticActive(2)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{held},
	})
	var log strings.Builder
	deps := huntNeedDeps(f, kb, &log)
	for pass := range huntNeedAbandonAfter {
		if called(f.huntFake, "abandon") {
			t.Fatalf("abandoned after only %d dry search(es); log:\n%s", pass, log.String())
		}
		f.state.CurrentPOI, f.state.Doc = "haven_station", true
		deps.unarmedGate = nil
		if err := Hunt(ctx, deps); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	if !called(f.huntFake, "abandon:"+held.MissionID) {
		t.Fatalf("no abandon after %d dry searches; calls %v\n%s", huntNeedAbandonAfter, f.calls, log.String())
	}
	board := held.TemplateID
	if board == "" {
		board = held.MissionID
	}
	r := huntLoadRoam(deps, &log)
	if !huntNeedGaveUp(r, board, huntNow(deps)) {
		t.Errorf("the abandoned need %q must be remembered; roam %+v", board, r)
	}
	if huntNeedGaveUp(r, board, huntNow(deps).Add(huntNeedGaveUpFor+time.Minute)) {
		t.Errorf("the memory must lapse after %s", huntNeedGaveUpFor)
	}
}
