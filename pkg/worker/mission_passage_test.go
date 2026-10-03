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

// TestAcceptPathCarriesPassageToTheMovementGate pins the gap left by the first
// passage fix: only the RESUME call site handed PassageTo down to Autopilot, so
// a freshly accepted an_introduction was refused by the movement gate with
// "complete the pirate unlock to fly it" the moment it tried to depart.
//
// Observed live on explorer-4 and engineer-6 (2026-10-02): the run path failed,
// the mission was left held, and missionResume flew it correctly on the next
// pass. Self-healing, but it burned a pass per mission and logged a refusal that
// reads like the fix is broken.
func TestAcceptPathCarriesPassageToTheMovementGate(t *testing.T) {
	entry := boardEntry("an_introduction", "starshine", 10, "voss_redoubt_station", "alhena", 3000, 20000)
	entry.TemplateID = "an_introduction"
	entry.Type = missionTypeSmuggling
	entry.ChainNext = "supply_run"
	entry.ProvidedItems = map[string]int{"starshine": 10}
	active := serverapi.ActiveMission{
		MissionID: "hex_intro", TemplateID: "an_introduction", Type: missionTypeSmuggling, Title: entry.Title,
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "starshine", Required: 10, SystemID: "alhena", TargetBase: "voss_redoubt_station"},
		},
	}
	// The FIRST get_active_missions must come back empty, or missionResume owns
	// the mission and the board/accept path -- the one under test -- never runs.
	fc := &fakeClient{
		state:             missionState(true, 500000, 10),
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active), activeJSON(t, active), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions": boardJSON(t, entry),
		},
	}
	fc.state.Ship.Cargo = []game.CargoItem{{ItemID: "starshine", Quantity: 200}}

	deps := missionDeps(fc, &fakeMissionStore{asks: map[string]float64{"starshine": 20}}, &fakeKB{
		systems: []knowledge.System{{ID: "alhena", Name: "Alhena", IsStronghold: true}, {ID: "haven", Name: "Haven"}},
		conns:   []knowledge.Connection{{FromSystem: "haven", ToSystem: "alhena"}},
	})
	deps.Categories = []string{missionTypeSmuggling, missionTypeDelivery}
	deps.Out = io.Discard

	var sawPassage map[string]bool
	var navCalls int
	deps.nav = func(ctx context.Context, system, poi string, passage map[string]bool) error {
		if system == "alhena" {
			navCalls++
			sawPassage = passage
		}

		return nil
	}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if navCalls == 0 {
		t.Fatal("no navigation to the stronghold destination happened at all")
	}
	if !sawPassage["alhena"] {
		t.Fatalf("flying an_introduction into a stronghold must carry passage for it, got %v", sawPassage)
	}
}
