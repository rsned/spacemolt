package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// strongholdGateKB is a KB whose only stronghold is alhena, matching the live
// topology where every stronghold is a degree-1 dead end.
func strongholdGateKB(t *testing.T) *fakeKB {
	t.Helper()
	return &fakeKB{systems: strongholdSystems()}
}

// strongholdDestFake routes current -> alhena, a degree-1 stronghold, which is
// the shape of every stronghold route: all nine are dead ends, so a stronghold
// is ALWAYS the destination and never a transit hop.
func strongholdDestFake() *fakeClient {
	return &fakeClient{
		state: &game.State{},
		route: []game.RouteStep{
			{SystemID: "treasure_cache", Name: "Treasure Cache"},
			{SystemID: "glenhaven", Name: "Glenhaven"},
			{SystemID: "alhena", Name: "Alhena"},
		},
	}
}

// TestAutopilotRefusesAStrongholdWithoutPassage is the safety property that must
// not regress: a locked agent with no mission passage is still refused, left
// docked and alive. 8 of 24 recorded ship losses were this exact route.
func TestAutopilotRefusesAStrongholdWithoutPassage(t *testing.T) {
	f := strongholdDestFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: strongholdGateKB(t),
	}, "alhena", "")

	if !errors.Is(err, ErrRouteThroughStronghold) {
		t.Fatalf("a locked agent with no passage must be refused, got %v", err)
	}
	if slices.ContainsFunc(f.calls, func(c string) bool { return len(c) > 5 && c[:5] == "jump:" }) {
		t.Fatalf("refusal must leave the agent docked, got %v", f.calls)
	}
}

// TestAutopilotHonoursMissionPassageToTheDestination pins the deadlock found on
// explorer-5 (2026-10-02). an_introduction -- the mission that GRANTS the pirate
// unlock -- delivers to Voss Redoubt in Alhena, a stronghold, and the server
// grants passage for its duration: "While this mission is active, pirate NPCs
// will leave you alone and their stations will let you dock." The movement gate
// knew nothing of that passage, so it refused the route with "complete the
// pirate unlock to fly it" -- the unlock being precisely what the refused flight
// would earn. The mission layer had already approved the endpoint; the movement
// layer vetoed it anyway. 124 agents sat locked behind this.
//
// Exempting only the named destination is safe BECAUSE strongholds are degree-1:
// one can never be a transit hop, so this grants nothing beyond the endpoint the
// mission already authorises.
func TestAutopilotHonoursMissionPassageToTheDestination(t *testing.T) {
	f := strongholdDestFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: strongholdGateKB(t),
		PassageTo: map[string]bool{"alhena": true},
	}, "alhena", "")

	if err != nil {
		t.Fatalf("an active mission granting passage must be allowed to fly: %v", err)
	}
	if !slices.Contains(f.calls, "jump:alhena") {
		t.Fatalf("expected the jump into alhena, got %v", f.calls)
	}
}

// Passage is per-destination, not a blanket unlock: a pass naming one stronghold
// must not open a route to a different one.
func TestAutopilotPassageDoesNotCoverADifferentStronghold(t *testing.T) {
	f := strongholdDestFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: strongholdGateKB(t),
		PassageTo: map[string]bool{"zaniah": true},
	}, "alhena", "")

	if !errors.Is(err, ErrRouteThroughStronghold) {
		t.Fatalf("passage to zaniah must not open alhena, got %v", err)
	}
}
