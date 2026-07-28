package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// The pirate BASELINE, not reputation, is the durable record that chain-2
// mission 1 was completed: reputation floats with activity and decays back
// toward baseline, so a high reputation on a default baseline is a temporary
// state, not an unlock. Everything starts at -30/-30; an_introduction raises
// the baseline to 10.
func TestSmugglingUnlockedReadsBaselineNotReputation(t *testing.T) {
	standing := func(rep, base int) *game.State {
		return &game.State{Player: game.Player{
			Standings: map[string]game.EmpireStanding{
				"pirates":  {Reputation: rep, Baseline: base},
				"solarian": {Reputation: 20, Baseline: 20},
			},
		}}
	}
	// Prove the baseline is what is read: if the predicate keyed on reputation
	// it would pass the "high rep on a default baseline" case below.
	for _, tc := range []struct {
		name  string
		state *game.State
		want  bool
	}{
		{"default hostile agent", standing(-30, -30), false},
		{"unlocked, rep sitting at baseline", standing(10, 10), true},
		{"unlocked and earning above baseline", standing(29, 10), true},
		{"high rep on a default baseline is NOT the unlock", standing(45, -30), false},
		{"nil state", nil, false},
		{"no standings yet (fresh worker)", &game.State{}, false},
		{"standings present, no pirate block", &game.State{Player: game.Player{
			Standings: map[string]game.EmpireStanding{"solarian": {Reputation: 20, Baseline: 20}},
		}}, false},
	} {
		if got := smugglingUnlocked(tc.state); got != tc.want {
			t.Errorf("%s: smugglingUnlocked = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The XP total is what a climbing worker's trip line leads with, so it must
// survive the shapes a board actually produces: several missions batched to one
// destination, entries with no rewards block at all, and multi-skill awards.
func TestTripSkillXPTotalsAcrossTheBatch(t *testing.T) {
	withXP := func(m map[string]int) missionCandidate {
		return missionCandidate{Entry: serverapi.MissionBoardEntry{
			Rewards: &serverapi.MissionRewards{Credits: 2000, SkillXP: m},
		}}
	}
	trip := []missionCandidate{
		withXP(map[string]int{"smuggling": 250}),
		withXP(map[string]int{"smuggling": 250}),
		{Entry: serverapi.MissionBoardEntry{}},                                     // no rewards block
		{Entry: serverapi.MissionBoardEntry{Rewards: &serverapi.MissionRewards{}}}, // rewards, no XP
		withXP(map[string]int{"smuggling": 75, "nebula_attunement": 35}),
	}
	if got := tripSkillXP(trip); got != 610 {
		t.Errorf("tripSkillXP = %d, want 610 (250+250+75+35)", got)
	}
	if got := tripSkillXP(nil); got != 0 {
		t.Errorf("tripSkillXP(nil) = %d, want 0", got)
	}
	// A credits-only trip must fall through to the plain line, not the XP one.
	if got := tripSkillXP([]missionCandidate{{Entry: serverapi.MissionBoardEntry{
		Rewards: &serverapi.MissionRewards{Credits: 5000},
	}}}); got != 0 {
		t.Errorf("credits-only trip reported %d XP, want 0", got)
	}
}
