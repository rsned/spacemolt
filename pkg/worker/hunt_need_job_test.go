package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// A Station Need delivered into a Lawless system is skipped: the delivery
// leg would leave empire-policed space.
func TestHuntNeedJobSkipsUnpolicedDelivery(t *testing.T) {
	kb := huntNeedKBFixture(t)
	if err := kb.RememberSystem(context.Background(), knowledge.System{
		ID: "levy", Name: "Levy", PoliceLevel: 0, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "pollux"}},
	}); err != nil {
		t.Fatal(err)
	}
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{creatures: huntGrazers("c1")})
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "accept:") {
		t.Fatalf("accepted a need delivering into Lawless space; calls %v\n%s", f.calls, log.String())
	}
	if !strings.Contains(log.String(), "delivery system levy is not empire-policed") {
		t.Errorf("the skip must be logged, got:\n%s", log.String())
	}
}

// A need the hold cannot fit (10 caustic_sac, 6 free) is skipped.
func TestHuntNeedJobSkipsWhatTheHoldCannotFit(t *testing.T) {
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{cargoCapacity: 10, cargoUsed: 4, creatures: huntGrazers("c1")})
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "accept:") {
		t.Fatalf("accepted a need the hold cannot fit; calls %v\n%s", f.calls, log.String())
	}
	if !strings.Contains(log.String(), "needs 10 free hold, have 6") {
		t.Errorf("the skip must be logged, got:\n%s", log.String())
	}
}

// Whole pass: the board offers a Station Need whose "Dropped by:" names a
// predator. Nothing is accepted and nothing is hunted.
func TestHuntNeedDroppedByAPredatorIsNeverAccepted(t *testing.T) {
	kb := huntNeedKBFixture(t)
	kb.byName[knowledge.NormalizeCreatureName("Tempest Eel")] = "tempest_eel"
	kb.species = append(kb.species, knowledge.WildlifeSpecies{Species: "tempest_eel", Role: "predator"})
	// Sighted at a policed ground in reach, so only the predator rule stands
	// between the board and an accept.
	kb.sightings = append(kb.sightings, knowledge.WildlifeSighting{
		Species: "tempest_eel", SystemID: "pollux", POIID: "pollux_outer_belt",
		Source: knowledge.WildlifeSourceNearby, ObservedCount: 1, ObservedUTC: huntAgo(time.Hour),
	})
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{creatures: huntGrazers("c1")})
	e := huntCausticNeed(2)
	e.Description = "The Levy is short of caustic sacs.\nDropped by: Tempest-Eel."
	f.raw["missions"] = boardJSON(t, e)
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "accept:") || f.huntCalls != 0 {
		t.Fatalf("a predator's part was taken on; calls %v\n%s", f.calls, log.String())
	}
	if !strings.Contains(log.String(), "species tempest_eel has role predator") {
		t.Errorf("the refusal must be logged, got:\n%s", log.String())
	}
}
