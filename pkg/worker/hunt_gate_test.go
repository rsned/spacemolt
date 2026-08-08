package worker

import (
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
	// And at every cap this iteration will ever use.
	for capLevel := range 3 {
		if ok, _ := huntAdmissible(e, capLevel, true); ok {
			t.Errorf("cap %d admitted the leviathan", capLevel)
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
func TestHuntAdmissibleNeedsAKillObjective(t *testing.T) {
	e := combatEntry("odd", 1)
	e.Objectives = []serverapi.MissionObjective{{Type: "dock_at_base"}}
	if ok, _ := huntAdmissible(e, 1, true); ok {
		t.Error("combat mission with no kill objective must be refused")
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
