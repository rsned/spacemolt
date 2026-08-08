package worker

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func combatEntry(id string, difficulty int) serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: id, Type: "combat", Title: id, Difficulty: difficulty,
		Objectives: []serverapi.MissionObjective{{Type: "kill_creature", Quantity: 3}},
	}
}

// The whole point of the gate: difficulty decides, never reward.
func TestHuntAdmissibleDifficultyCap(t *testing.T) {
	for _, tc := range []struct {
		id    string
		diff  int
		cap   int
		admit bool
	}{
		{"first_hunt_belt_grazers", 1, 1, true},
		{"grazer_cull", 2, 1, false},
		{"grazer_cull", 2, 2, true},
		{"ice_field_thinning", 2, 2, true},
		{"nebula_drift_hunt", 2, 2, true},
		{"starfall_prospector_defense", 4, 2, false},
		{"leviathan_bounty", 6, 2, false},
		{"smugglers_route", 7, 2, false},
	} {
		ok, reason := huntAdmissible(combatEntry(tc.id, tc.diff), tc.cap, true)
		if ok != tc.admit {
			t.Errorf("%s (diff %d, cap %d): admitted = %v, want %v (reason %q)",
				tc.id, tc.diff, tc.cap, ok, tc.admit, reason)
		}
		if !ok && reason == "" {
			t.Errorf("%s: a refusal must carry a reason", tc.id)
		}
	}
}

// leviathan_bounty is the mission a reward-maximising selector picks first: the
// best XP in the table, 8,000cr, AND repeatable, so it would be chosen forever.
// The Molt Leviathan hunts ships and fights to the death. It must stay refused
// however good its numbers look.
func TestHuntAdmissibleRefusesLeviathanOnAnyReward(t *testing.T) {
	e := combatEntry("leviathan_bounty", 6)
	e.Rewards = &serverapi.MissionRewards{Credits: 1_000_000}
	if ok, _ := huntAdmissible(e, 2, true); ok {
		t.Fatal("a difficulty-6 mission must be refused no matter how large the reward")
	}

	// wildlifeOnly=false is what makes this test mean anything. leviathan_bounty
	// is ALSO outside the wildlife allowlist, so with gate 2 on it is refused
	// twice over and the assertion above passes even if the difficulty check is
	// deleted entirely — proven by removing that branch, which left this test
	// green. Lifting gate 2 leaves the difficulty cap as the only thing standing
	// between the fleet and a boss that fights to the death.
	for capLevel := range 3 {
		ok, reason := huntAdmissible(e, capLevel, false)
		if ok {
			t.Errorf("cap %d admitted the leviathan with gate 2 lifted — "+
				"the difficulty cap is not doing its job", capLevel)
		}
		if !strings.Contains(reason, "difficulty") {
			t.Errorf("cap %d refused for %q; the reason must name difficulty, "+
				"or some other gate is silently doing the work", capLevel, reason)
		}
	}
}

// Gate 2 is what lets gate 1 rise to 2 without admitting the two combat
// missions that shoot back.
func TestHuntAdmissibleWildlifeOnly(t *testing.T) {
	for _, id := range []string{"pirate_bounty", "convoy_defense"} {
		if ok, reason := huntAdmissible(combatEntry(id, 2), 2, true); ok {
			t.Errorf("%s must be refused while wildlifeOnly is set", id)
		} else if reason == "" {
			t.Errorf("%s: refusal needs a reason", id)
		}
		if ok, _ := huntAdmissible(combatEntry(id, 2), 2, false); !ok {
			t.Errorf("%s must be admitted once wildlifeOnly is lifted", id)
		}
	}
}

// Non-combat types are none of this gate's business.
func TestHuntAdmissibleRejectsNonCombat(t *testing.T) {
	e := combatEntry("some_delivery", 1)
	e.Type = "delivery"
	if ok, _ := huntAdmissible(e, 1, true); ok {
		t.Error("a delivery mission must not be admitted by the hunt gate")
	}
}

// A board entry with no kill objective is not huntable even if it is combat.
//
// The mission id must be one the wildlife allowlist ACCEPTS, and the difficulty
// must be under the cap, or an earlier gate refuses the entry first and this
// test proves nothing. An earlier version used the id "odd", which is not on the
// allowlist: gate 2 rejected it before the kill-objective check ran, so deleting
// that check left this test green.
func TestHuntAdmissibleNeedsAKillObjective(t *testing.T) {
	e := combatEntry("grazer_cull", 1) // on the allowlist, under the cap
	e.Objectives = []serverapi.MissionObjective{{Type: "dock_at_base"}}

	// Sanity: with a kill objective this entry IS admissible, so anything the
	// assertion below catches is the missing objective and nothing else.
	if ok, reason := huntAdmissible(combatEntry("grazer_cull", 1), 1, true); !ok {
		t.Fatalf("fixture is wrong: grazer_cull should be admissible, got %q", reason)
	}

	ok, reason := huntAdmissible(e, 1, true)
	if ok {
		t.Error("combat mission with no kill objective must be refused")
	}
	if !strings.Contains(reason, "kill_creature") {
		t.Errorf("refused for %q; the reason must name the missing kill objective, "+
			"or another gate is doing the work", reason)
	}
}

// Defaults are the safe end: cap 1, wildlife only.
func TestHuntGateDefaults(t *testing.T) {
	if huntDefaultMaxDifficulty != 1 {
		t.Errorf("default cap = %d, want 1", huntDefaultMaxDifficulty)
	}
	if !huntWildlifeOnlyDefault {
		t.Error("wildlife-only must default on")
	}
}
