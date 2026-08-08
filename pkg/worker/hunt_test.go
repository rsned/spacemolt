package worker

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// boardMission is the shorthand newHuntFake uses to build a combat board
// entry with a single kill_creature objective.
type boardMission struct {
	id         string
	difficulty int
	quantity   int
}

// huntFakeOpts configures newHuntFake.
type huntFakeOpts struct {
	board     []boardMission
	creatures []string
	// hullAfter1 is the hull fraction (0-1) the fake reports once the FIRST
	// resolved fight completes. 0 means no scripted drop (hull stays full).
	hullAfter1 float64
}

// huntFake wraps the shared fakeClient (dispatch_test.go) with wildlife-hunt
// recording: engagement count, whether a flee stance was issued, and (for
// TestHuntFleesBelowHullThreshold) a scripted hull drop once the first fight
// resolves. Everything else — GetState, GetRawJSON, IsConnected,
// AcceptMission, CompleteMission, GetActiveMissions, GetStatus, GetMissions —
// comes straight from the embedded *fakeClient; this type only adds what the
// shared fake doesn't already have (Hunt, Battle, GetNearby, GetBattleStatus).
type huntFake struct {
	*fakeClient
	huntCalls   int
	fled        bool
	hullAfter1  float64
	hullApplied bool
}

// newHuntFake builds a huntFake docked at a known POI (so Hunt's step-1
// repositioning never triggers) with the board and creature list serialized
// into the raw JSON store exactly as get_missions/get_nearby would deliver
// them, plus a canned "fight resolved" battle_status reply.
func newHuntFake(t *testing.T, opts huntFakeOpts) *huntFake {
	t.Helper()

	entries := make([]serverapi.MissionBoardEntry, 0, len(opts.board))
	for _, m := range opts.board {
		entries = append(entries, serverapi.MissionBoardEntry{
			MissionID:  m.id,
			Type:       "combat",
			Title:      m.id,
			Difficulty: m.difficulty,
			Objectives: []serverapi.MissionObjective{{Type: "kill_creature", Quantity: m.quantity}},
		})
	}

	creatures := make([]serverapi.NearbyCreature, 0, len(opts.creatures))
	for _, id := range opts.creatures {
		creatures = append(creatures, serverapi.NearbyCreature{
			CreatureID: id, Species: "ash_scarab", Role: "scavenger", Hull: 45, MaxHull: 45,
		})
	}
	nearbyRaw, err := json.Marshal(serverapi.GetNearbyResponse{
		Creatures: creatures, CreatureCount: len(creatures), POIID: "commerce_fields",
	})
	if err != nil {
		t.Fatalf("marshal nearby fixture: %v", err)
	}
	battleRaw, err := json.Marshal(serverapi.GetBattleStatusResponse{IsParticipant: false})
	if err != nil {
		t.Fatalf("marshal battle_status fixture: %v", err)
	}

	f := &fakeClient{
		state: &game.State{
			System:     game.SystemData{ID: "test_system"},
			CurrentPOI: "commerce_fields",
			Ship:       game.Ship{Hull: 100, MaxHull: 100},
		},
		raw: map[string][]byte{
			"missions":      boardJSON(t, entries...),
			"nearby":        nearbyRaw,
			"battle_status": battleRaw,
		},
	}
	return &huntFake{fakeClient: f, hullAfter1: opts.hullAfter1}
}

func (f *huntFake) GetNearby(ctx context.Context) error {
	f.calls = append(f.calls, "get_nearby")
	return nil
}

func (f *huntFake) Hunt(ctx context.Context, creatureID string) error {
	f.huntCalls++
	f.calls = append(f.calls, "hunt:"+creatureID)
	return nil
}

// GetBattleStatus resolves the fight immediately (battle_status was seeded
// with IsParticipant: false), applying the scripted hull drop the first time
// it is polled after a Hunt call, so callers see fresh combat damage before
// deciding whether to flee.
func (f *huntFake) GetBattleStatus(ctx context.Context) error {
	f.calls = append(f.calls, "get_battle_status")
	if f.huntCalls == 1 && f.hullAfter1 != 0 && !f.hullApplied {
		f.hullApplied = true
		f.state.Ship.Hull = f.hullAfter1 * f.state.Ship.MaxHull
	}
	return nil
}

func (f *huntFake) Battle(ctx context.Context, action string, payload map[string]any) error {
	f.calls = append(f.calls, "battle:"+action)
	if action == "stance" && payload["stance"] == "flee" {
		f.fled = true
	}
	return nil
}

// A gated pass must not issue a hunt at all.
func TestHuntRefusesOverCapMission(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "leviathan_bounty", difficulty: 6, quantity: 1}},
		creatures: []string{"c1"},
	})
	var log strings.Builder
	if err := Hunt(context.Background(), HuntDeps{
		Client: c, Out: &log, AgentID: "pirate-6", MaxDifficulty: 1, WildlifeOnly: true,
	}); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 0 {
		t.Errorf("hunt issued %d times against an over-cap mission, want 0", c.huntCalls)
	}
	if !strings.Contains(log.String(), "difficulty 6 over cap 1") {
		t.Errorf("refusal must be logged with its reason, got:\n%s", log.String())
	}
}

// The counted objective drives the loop: 3 grazers means 3 hunts.
func TestHuntKillsToObjectiveCount(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures: []string{"c1", "c2", "c3", "c4"},
	})
	if err := Hunt(context.Background(), HuntDeps{
		Client: c, Out: io.Discard, AgentID: "pirate-6", MaxDifficulty: 1, WildlifeOnly: true,
	}); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 3 {
		t.Errorf("hunt calls = %d, want 3 (the objective quantity)", c.huntCalls)
	}
}

// No creatures at this POI is a normal outcome, not an error: the herd moved or
// the belt is mined thin. It must not fail the pass.
//
// huntCalls==0 alone does not isolate the step-4 empty-belt check: the
// engagement loop's own huntPickQuarry also returns "nothing to hunt" for a
// nil quarry slice, so a version of Hunt with the step-4 check deleted
// entirely still reports zero hunts and a nil error — proven by deleting it,
// which left only the two assertions below red. The log-message assertion
// distinguishes the two: step 4 logs "no creatures at this POI" BEFORE ever
// entering the loop, while the loop's own fallback logs a different message
// ("no huntable creatures remain").
func TestHuntNoCreaturesIsNotAnError(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures: nil,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), HuntDeps{
		Client: c, Out: &log, AgentID: "pirate-6", MaxDifficulty: 1, WildlifeOnly: true,
	}); err != nil {
		t.Fatalf("an empty belt must not fail the pass: %v", err)
	}
	if c.huntCalls != 0 {
		t.Error("no creatures means no hunts")
	}
	if !strings.Contains(log.String(), "no creatures at this POI") {
		t.Errorf("must log the step-4 empty-belt outcome specifically, got:\n%s", log.String())
	}
}

// The flee threshold aborts the hunt rather than trading the hull for a kill.
func TestHuntFleesBelowHullThreshold(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:      []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures:  []string{"c1", "c2", "c3"},
		hullAfter1: 0.1, // 10% hull after the first fight
	})
	var log strings.Builder
	if err := Hunt(context.Background(), HuntDeps{
		Client: c, Out: &log, AgentID: "pirate-6",
		MaxDifficulty: 1, WildlifeOnly: true, FleeAtHull: 0.3,
	}); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 1 {
		t.Errorf("hunt calls = %d, want 1 — the pass must stop at the flee threshold", c.huntCalls)
	}
	if !c.fled {
		t.Error("dropping below the flee threshold must issue a flee stance")
	}
}
