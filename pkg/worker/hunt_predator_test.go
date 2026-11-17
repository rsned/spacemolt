package worker

import (
	"context"
	"strings"
	"testing"
)

// Spec: "If a predator is present when the hunter arrives at a ground, it
// leaves that ground." Every creature-work branch (Station Need, home, and a
// wildlife kill mission) arrives at the ground, sees a predator beside the
// grazers, hunts nothing there, and, with no other ground to try, leaves for
// a station rather than sit beside it.
func TestHuntLeavesAGroundWithAPredator(t *testing.T) {
	predatorBelt := []huntCreature{
		{id: "eel", species: "tempest_eel", role: "predator", hull: 280},
		{id: "g1"}, {id: "g2"}, {id: "g3"},
	}
	unknownBelt := []huntCreature{
		{id: "thing", species: "unknown_thing", role: huntNoRole, hull: 60},
		{id: "g1"}, {id: "g2"}, {id: "g3"},
	}
	for _, tc := range []struct {
		name      string
		creatures []huntCreature
		run       func(t *testing.T, creatures []huntCreature) (*huntFake, string)
	}{
		{"need/predator", predatorBelt, huntPredatorNeedPass},
		{"need/no role", unknownBelt, huntPredatorNeedPass},
		{"kill mission/predator", predatorBelt, huntPredatorKillPass},
		{"kill mission/no role", unknownBelt, huntPredatorKillPass},
		{"home/predator", predatorBelt, huntPredatorHomePass},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, log := tc.run(t, tc.creatures)
			if f.huntCalls != 0 {
				t.Fatalf("hunted %d time(s) at a ground with a predator; calls %v\n%s", f.huntCalls, f.calls, log)
			}
			if !strings.Contains(log, "PREDATOR PRESENT") {
				t.Errorf("the predator must be logged, got:\n%s", log)
			}
			nearby := firstCall(f, "get_nearby")
			leave := lastCall(f, "dock")
			if nearby < 0 || leave < nearby {
				t.Errorf("must leave the predator ground for a station (dock after get_nearby), calls %v\n%s", f.calls, log)
			}
		})
	}
}

func huntPredatorNeedPass(t *testing.T, creatures []huntCreature) (*huntFake, string) {
	t.Helper()
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{creatures: creatures})
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	return f.huntFake, log.String()
}

func huntPredatorKillPass(t *testing.T, creatures []huntCreature) (*huntFake, string) {
	t.Helper()
	f := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: creatures,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(f, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	return f, log.String()
}

func huntPredatorHomePass(t *testing.T, creatures []huntCreature) (*huntFake, string) {
	t.Helper()
	f := huntRoamFake(t, huntFakeOpts{creatures: creatures, startPOI: huntHomePOI})
	f.state.System.ID = huntHomeSystem
	var log strings.Builder
	deps := huntDeps(f, &log)
	deps.KB = huntRoamKB(t, false)
	if err := saveHuntRoam(huntRoamPath(deps.AgentsDir, deps.AgentID), huntRoam{Visited: []string{"test_system"}}); err != nil {
		t.Fatal(err)
	}
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	return f, log.String()
}
