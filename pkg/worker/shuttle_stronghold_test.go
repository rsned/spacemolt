package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// johnny_cab was given the pirate unlock (11-15) to take the high-paying
// stronghold fares, but the shuttle dropped every stronghold destination
// regardless. Unlocked shuttles block none; locked ones block them all.
func TestShuttleBlockedStrongholds(t *testing.T) {
	systems := []knowledge.System{{ID: "voss", IsStronghold: true}, {ID: "sol"}}
	locked := &game.State{}
	unlocked := &game.State{}
	unlocked.Player.Standings = map[string]game.EmpireStanding{"pirate_voss": {Baseline: 10}}

	if got := shuttleBlockedStrongholds(locked, systems); !got["voss"] || got["sol"] {
		t.Errorf("locked: %v, want voss blocked only", got)
	}
	if got := shuttleBlockedStrongholds(unlocked, systems); len(got) != 0 {
		t.Errorf("unlocked: %v, want nothing blocked", got)
	}
	if got := shuttleBlockedStrongholds(nil, systems); !got["voss"] {
		t.Errorf("unknown standings: %v, want voss blocked", got)
	}
}
