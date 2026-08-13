package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Central Nexus is one of the thirteen stations whose base id and POI id differ, and
// one of the three where they are not even textually related: POI "the_core" resolves
// to base "central_nexus". No string rule can bridge that pair — only the bases table
// can. (Verified against data/spacemolt-knowledge.db: bases.poi_id='the_core' ->
// bases.id='central_nexus', Central Nexus in Nexus Prime.)
const (
	nexusBaseID = "central_nexus"
	nexusPOIID  = "the_core"
)

func pinnedDeps(t *testing.T, pin string, fc *fakeClient) (MissionDeps, *strings.Builder) {
	t.Helper()
	kb := &fakeKB{bases: map[string]*knowledge.SpaceBase{
		nexusPOIID: {ID: nexusBaseID, POIID: nexusPOIID, Name: "Central Nexus"},
	}}
	var out strings.Builder
	return MissionDeps{
		Client:      fc,
		KB:          kb,
		AgentID:     "t",
		HomeStation: pin,
		State:       &missionRunState{dry: missionDryPassLimit - 1},
	}, &out
}

func dockedAt(base string) *fakeClient {
	return &fakeClient{state: &game.State{
		Doc:    true,
		Player: game.Player{DockedAtBase: base},
	}}
}

// TestDryPassRecognisesAPinWrittenAsAPOIID is the regression. A pin written as a POI
// id never equalled docked_at_base (a BASE id), so a worker standing AT its pinned
// station concluded it was elsewhere and flew "back" to where it already was, every
// dry pass. That drained alhena's and sheratan's 130-unit tanks to zero — 164 loops
// for alhena — and stranded both in strongholds with dead fuel desks.
func TestDryPassRecognisesAPinWrittenAsAPOIID(t *testing.T) {
	fc := dockedAt(nexusBaseID)
	deps, out := pinnedDeps(t, nexusPOIID, fc)
	if err := missionDryPass(context.Background(), deps, out); err != nil {
		t.Fatalf("dry pass: %v", err)
	}
	if deps.State.parkedUntil.IsZero() {
		t.Fatalf("a worker docked at its pin must park, not travel; log said: %s", out.String())
	}
	if strings.Contains(out.String(), "returning to pinned station") {
		t.Fatalf("must not route to a station it is already at: %s", out.String())
	}
}

// TestDryPassStillRecognisesAPinWrittenAsABaseID keeps the common case working: most
// pins and base ids match outright and must not need a KB round trip to be believed.
func TestDryPassStillRecognisesAPinWrittenAsABaseID(t *testing.T) {
	fc := dockedAt(nexusBaseID)
	deps, out := pinnedDeps(t, nexusBaseID, fc)
	if err := missionDryPass(context.Background(), deps, out); err != nil {
		t.Fatalf("dry pass: %v", err)
	}
	if deps.State.parkedUntil.IsZero() {
		t.Fatalf("a matching pin must park; log said: %s", out.String())
	}
}

// TestDryPassStillTravelsWhenGenuinelyElsewhere proves the alias tolerance did not
// turn into "always parked": a worker that really has wandered off must still go back.
func TestDryPassStillTravelsWhenGenuinelyElsewhere(t *testing.T) {
	fc := dockedAt("some_other_station")
	deps, out := pinnedDeps(t, nexusPOIID, fc)
	_ = missionDryPass(context.Background(), deps, out)
	if !deps.State.parkedUntil.IsZero() {
		t.Fatal("a worker away from its pin must not park")
	}
	if !strings.Contains(out.String(), "returning to pinned station") {
		t.Fatalf("expected a return-to-pin log line, got: %s", out.String())
	}
}

// TestDryPassDoesNotParkWhileUndocked guards the other half of the docked_at_base
// gotcha: the field is stale while undocked, so it must never alone imply arrival.
func TestDryPassDoesNotParkWhileUndocked(t *testing.T) {
	fc := dockedAt(nexusBaseID)
	fc.state.Doc = false
	deps, out := pinnedDeps(t, nexusPOIID, fc)
	_ = missionDryPass(context.Background(), deps, out)
	if !deps.State.parkedUntil.IsZero() {
		t.Fatal("an undocked worker must not be treated as parked at its pin")
	}
}

// fueled sets the tank and wallet on a docked fake so the top-up path can be driven.
func fueled(fc *fakeClient, fuel, maxFuel, credits float64) *fakeClient {
	fc.state.Fuel, fc.state.MaxFuel, fc.state.Credits = fuel, maxFuel, credits
	return fc
}

func clientCalled(fc *fakeClient, name string) bool {
	for _, c := range fc.calls {
		if c == name {
			return true
		}
	}
	return false
}

// TestParkingAtAPinTopsUpTheTank is the other half of the alhena/sheratan strand: a
// pinned MISSION worker runs no idle script, so unlike a resident it never refuels
// once it stops travelling. Both sat at 0/130 holding a quarter of a million credits
// without ever asking. (Asking turned out to fail at those two strongholds — the
// desks really are depleted — but a refusal you can read beats silence you cannot.)
func TestParkingAtAPinTopsUpTheTank(t *testing.T) {
	fc := fueled(dockedAt(nexusBaseID), 0, 130, 242722)
	deps, out := pinnedDeps(t, nexusPOIID, fc)
	if err := missionDryPass(context.Background(), deps, out); err != nil {
		t.Fatalf("dry pass: %v", err)
	}
	if !clientCalled(fc, "refuel") {
		t.Fatalf("a pinned worker parking on an empty tank must refuel; log: %s", out.String())
	}
	if deps.State.parkedUntil.IsZero() {
		t.Fatal("it must still park after topping up")
	}
}

// TestParkingWithAFullTankSkipsTheRefuel keeps the top-up from becoming a per-park
// tax on a worker that needs nothing.
func TestParkingWithAFullTankSkipsTheRefuel(t *testing.T) {
	fc := fueled(dockedAt(nexusBaseID), 130, 130, 242722)
	deps, out := pinnedDeps(t, nexusPOIID, fc)
	if err := missionDryPass(context.Background(), deps, out); err != nil {
		t.Fatalf("dry pass: %v", err)
	}
	if clientCalled(fc, "refuel") {
		t.Fatal("a full tank must not trigger a refuel")
	}
}

// TestParkingBrokeSaysSoAndStillParks: with no credits there is nothing to buy, and
// refusing to park would only spend fuel the worker does not have.
func TestParkingBrokeSaysSoAndStillParks(t *testing.T) {
	fc := fueled(dockedAt(nexusBaseID), 0, 130, 0)
	deps, out := pinnedDeps(t, nexusPOIID, fc)
	if err := missionDryPass(context.Background(), deps, out); err != nil {
		t.Fatalf("dry pass: %v", err)
	}
	if clientCalled(fc, "refuel") {
		t.Fatal("a broke worker must not attempt a refuel")
	}
	if !strings.Contains(out.String(), "no credits to refuel") {
		t.Fatalf("the reason must be visible in the log, got: %s", out.String())
	}
	if deps.State.parkedUntil.IsZero() {
		t.Fatal("it must still park")
	}
}

// TestRefuelFailureStillParks: a stronghold with a dead fuel desk must not turn into
// a worker that refuses to settle.
func TestRefuelFailureStillParks(t *testing.T) {
	fc := fueled(dockedAt(nexusBaseID), 0, 130, 242722)
	fc.refuelErr = errors.New("station_fuel_empty")
	deps, out := pinnedDeps(t, nexusPOIID, fc)
	if err := missionDryPass(context.Background(), deps, out); err != nil {
		t.Fatalf("dry pass: %v", err)
	}
	if deps.State.parkedUntil.IsZero() {
		t.Fatal("a failed refuel must not prevent parking")
	}
	if !strings.Contains(out.String(), "refuel at pinned station") {
		t.Fatalf("the failure must be visible, got: %s", out.String())
	}
}

// TestPinMatchesTheCurrentPOIWithNoBasesRow covers the case the KB cannot answer:
// four of the nine pirate strongholds have never been scanned, so `bases` has no row
// mapping their POI to a base id. A worker pinned there by POI id is standing exactly
// where it belongs, and must not be sent "back" to itself until its tank runs dry.
func TestPinMatchesTheCurrentPOIWithNoBasesRow(t *testing.T) {
	fc := &fakeClient{state: &game.State{
		Doc: true,
		Player: game.Player{
			DockedAtBase: "some_unscanned_base_id",
			CurrentPOI:   "sable_port",
		},
		Fuel: 100, MaxFuel: 100,
	}}
	// An EMPTY bases map: the station is unknown to the KB, as the real ones are.
	deps := MissionDeps{
		Client:      fc,
		KB:          &fakeKB{bases: map[string]*knowledge.SpaceBase{}},
		AgentID:     "t",
		HomeStation: "sable_port",
		State:       &missionRunState{dry: missionDryPassLimit - 1},
	}
	var out strings.Builder
	if err := missionDryPass(context.Background(), deps, &out); err != nil {
		t.Fatalf("dry pass: %v", err)
	}
	if deps.State.parkedUntil.IsZero() {
		t.Fatalf("a POI-id pin at an unscanned station must park; log: %s", out.String())
	}
	if strings.Contains(out.String(), "returning to pinned station") {
		t.Fatalf("must not route to the POI it is docked at: %s", out.String())
	}
}
