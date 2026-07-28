package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
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
