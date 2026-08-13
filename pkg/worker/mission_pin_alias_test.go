package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Voss Redoubt is one of the thirteen stations whose base id and POI id differ, and
// one of the three where they are not even textually related — no string rule can
// bridge them, only the bases table can.
const (
	vossBaseID = "voss_redoubt_station"
	vossPOIID  = "the_core"
)

func pinnedDeps(t *testing.T, pin string, fc *fakeClient) (MissionDeps, *strings.Builder) {
	t.Helper()
	kb := &fakeKB{bases: map[string]*knowledge.SpaceBase{
		vossPOIID: {ID: vossBaseID, POIID: vossPOIID, Name: "Voss Redoubt Station"},
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
	fc := dockedAt(vossBaseID)
	deps, out := pinnedDeps(t, vossPOIID, fc)
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
	fc := dockedAt(vossBaseID)
	deps, out := pinnedDeps(t, vossBaseID, fc)
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
	deps, out := pinnedDeps(t, vossPOIID, fc)
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
	fc := dockedAt(vossBaseID)
	fc.state.Doc = false
	deps, out := pinnedDeps(t, vossPOIID, fc)
	_ = missionDryPass(context.Background(), deps, out)
	if !deps.State.parkedUntil.IsZero() {
		t.Fatal("an undocked worker must not be treated as parked at its pin")
	}
}
