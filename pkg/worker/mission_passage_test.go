package worker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// strongholdActive builds an active smuggling mission delivering into alhena,
// a stronghold, under the given template id.
func strongholdActive(templateID string) serverapi.ActiveMission {
	return serverapi.ActiveMission{
		MissionID: "held", TemplateID: templateID, Type: "smuggling", Title: "An Introduction",
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "starshine", Required: 10, SystemID: "alhena", TargetBase: "voss_redoubt_station"},
		},
	}
}

func passageDeps(t *testing.T, templateID string) (*fakeClient, *fakeMissionStore, MissionDeps) {
	t.Helper()
	fc := &fakeClient{
		state: missionState(true, 50000, 10),
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t, strongholdActive(templateID)),
		},
	}
	fc.state.Ship.Cargo = []game.CargoItem{{ItemID: "starshine", Quantity: 200}}
	store := &fakeMissionStore{}
	deps := missionDeps(fc, store, &fakeKB{
		systems: []knowledge.System{{ID: "alhena", Name: "Alhena", IsStronghold: true}, {ID: "haven", Name: "Haven"}},
		conns:   []knowledge.Connection{{FromSystem: "haven", ToSystem: "alhena"}},
	})
	deps.Categories = []string{missionTypeSmuggling, missionTypeDelivery}
	deps.Out = io.Discard

	return fc, store, deps
}

// TestResumeFliesAnIntroductionIntoItsStronghold pins the deadlock that kept 124
// agents locked. an_introduction delivers INTO Voss Redoubt at Alhena and the
// server grants docking for its duration ("While this mission is active, pirate
// NPCs will leave you alone and their stations will let you dock"). The passage
// marker the resume path looked for -- chain_next -- is present on the BOARD but
// is NOT echoed by get_active_missions (verified live, 2026-10-02), so the check
// was always false and every such mission parked forever. an_introduction is the
// only mission known to carry passage, so it is recognised by template id.
func TestResumeFliesAnIntroductionIntoItsStronghold(t *testing.T) {
	_, _, deps := passageDeps(t, "an_introduction")
	var navTo string
	deps.nav = func(ctx context.Context, system, poi string, passage map[string]bool) error {
		navTo = system
		return nil
	}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if navTo != "alhena" {
		t.Fatalf("an_introduction must fly to its stronghold destination on its guest pass; navigated to %q", navTo)
	}
}

// The safety half: passage is granted by template, not by "it is a smuggling
// mission pointing at a stronghold". An ordinary courier to the same place has
// no pass and must still be held, never flown.
func TestResumeStillHoldsAnOrdinarySmugglingRunToAStronghold(t *testing.T) {
	_, _, deps := passageDeps(t, "smuggling_courier_claim_whatever")
	var navTo string
	deps.nav = func(ctx context.Context, system, poi string, passage map[string]bool) error {
		navTo = system
		return nil
	}
	var log strings.Builder
	deps.Out = &log

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if navTo != "" {
		t.Fatalf("a passage-less smuggling run must not fly into a stronghold; navigated to %q", navTo)
	}
	if !strings.Contains(log.String(), "pirate stronghold") {
		t.Fatalf("holding a stronghold-bound mission must say why: %s", log.String())
	}
}

// TestResumeCompletesWhenAlreadyDockedAtTheDestination pins the livelock this
// family keeps producing. A held mission whose destination is where the worker
// already stands routes to "already at target", then docks -- and the server
// answers "Already docked", which missionResume treated as a failed pass. It
// returned before missionComplete and re-resumed the same mission every tick:
// trader-2 looped on A Word in Private once a pass, observed live 2026-10-02.
//
// dockIdempotent exists for exactly this and was already used by
// mission_explore; the resume path called raw Dock. The loop was previously
// masked by the stale-cargo read, which abandoned these missions before they
// could reach the dock.
func TestResumeCompletesWhenAlreadyDockedAtTheDestination(t *testing.T) {
	active := serverapi.ActiveMission{
		MissionID: "held", TemplateID: "a_word_in_private", Type: "smuggling", Title: "A Word in Private",
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "starshine", Required: 10, SystemID: "haven", TargetBase: "haven_station"},
		},
	}
	fc := &fakeClient{
		state:          missionState(true, 50000, 10),
		completeReward: 2000,
		dockErr:        errors.New("Already docked"),
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t, active),
		},
	}
	fc.state.Ship.Cargo = []game.CargoItem{{ItemID: "starshine", Quantity: 200}}
	store := &fakeMissionStore{}
	deps := missionDeps(fc, store, missionKB())
	deps.Categories = []string{missionTypeSmuggling, missionTypeDelivery}
	deps.Out = io.Discard
	deps.nav = func(ctx context.Context, system, poi string, passage map[string]bool) error { return nil }

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if !strings.Contains(strings.Join(fc.calls, " "), "complete:held") {
		t.Fatalf("a mission already at its destination must complete, not loop: %v", fc.calls)
	}
}
