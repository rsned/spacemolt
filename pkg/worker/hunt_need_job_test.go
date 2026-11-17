package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
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

// A held need whose delivery system is (now) Lawless is held, not resumed:
// neither hunted for nor delivered.
func TestHuntResumedNeedHeldWhenDeliveryUnpoliced(t *testing.T) {
	kb := huntNeedKBFixture(t)
	if err := kb.RememberSystem(context.Background(), knowledge.System{
		ID: "levy", Name: "Levy", PoliceLevel: 0, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "pollux"}},
	}); err != nil {
		t.Fatal(err)
	}
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{huntCausticActive(2)},
		creatures:      huntGrazers("c1"),
	})
	f.raw["missions"] = boardJSON(t)
	f.state.Ship.Cargo = []game.CargoItem{{ItemID: "caustic_sac", Quantity: 10}}
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "travel:") || called(f.huntFake, "complete:") || f.huntCalls != 0 || called(f.huntFake, "abandon") {
		t.Fatalf("a held need delivering into Lawless space must be held untouched; calls %v\n%s", f.calls, log.String())
	}
	if !strings.Contains(log.String(), "holding need hex-caustic") {
		t.Errorf("the hold must be logged, got:\n%s", log.String())
	}
}

// Station ids are dual-named: the mission names the BASE id, the ship's
// position is the station POI id. Docked at the target under its POI id, the
// hunter completes in place -- no undock, no travel, no re-dock.
func TestHuntDeliveryResolvesTheBaseToItsPOI(t *testing.T) {
	kb := huntNeedKBFixture(t)
	ctx := context.Background()
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "levy_poi", SystemID: "levy", Type: "station"}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: "levy_base", POIID: "levy_poi", PublicAccess: true}); err != nil {
		t.Fatal(err)
	}
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{huntCausticActive(2)},
		startPOI:       "levy_poi",
	})
	f.state.System.ID = "levy"
	f.state.Ship.Cargo = []game.CargoItem{{ItemID: "caustic_sac", Quantity: 10}}
	var log strings.Builder
	if err := Hunt(ctx, huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "undock") || called(f.huntFake, "travel:") {
		t.Fatalf("left the target station it was docked at; calls %v\n%s", f.calls, log.String())
	}
	if !called(f.huntFake, "complete:hex-caustic") {
		t.Fatalf("want the need completed in place; calls %v\n%s", f.calls, log.String())
	}

	// Elsewhere, the leg flies to the station POI, not the base id.
	g := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{huntCausticActive(2)},
	})
	g.state.Ship.Cargo = []game.CargoItem{{ItemID: "caustic_sac", Quantity: 10}}
	if err := Hunt(ctx, huntNeedDeps(g, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(g.huntFake, "travel:levy_poi") || !called(g.huntFake, "complete:hex-caustic") {
		t.Fatalf("want travel to levy_poi then complete; calls %v\n%s", g.calls, log.String())
	}
}
