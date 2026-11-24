package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/navigation"
)

// treasure_cache -> ironhearth runs only through the_rampart (Crimson's one
// customs-patrolled entry) or a leviathan system. pirate-7 and pirate-9
// accepted hot_cell couriers there on 2026-11-24 and then sat an hour at the
// giver, unable to depart, while the deadline ran down.
func contrabandTestGraph() navigation.JumpGraph {
	g := navigation.JumpGraph{}
	link := func(a, b string) {
		g[a] = append(g[a], b)
		g[b] = append(g[b], a)
	}
	link("treasure_cache", "the_rampart")
	link("the_rampart", "ironhearth")
	link("treasure_cache", "taygeta")
	link("taygeta", "ironhearth")
	// frontier is reachable two ways: through gold_run (customs) or the long
	// way round through haven and market_prime.
	link("treasure_cache", "gold_run")
	link("gold_run", "frontier")
	link("treasure_cache", "haven")
	link("haven", "market_prime")
	link("market_prime", "frontier")
	return g
}

func TestContrabandDistRefusesDestinationOnlyReachableThroughBlocked(t *testing.T) {
	blocked := map[string]string{"the_rampart": "customs", "taygeta": "leviathan"}
	d := contrabandDist(contrabandTestGraph(), "treasure_cache", []string{"ironhearth"}, blocked)
	if d["ironhearth"] < navigation.RouteInf {
		t.Errorf("ironhearth = %d jumps, want unreachable (every road crosses a blocked system)", d["ironhearth"])
	}
}

func TestContrabandDistTakesTheDetour(t *testing.T) {
	blocked := map[string]string{"gold_run": "customs"}
	d := contrabandDist(contrabandTestGraph(), "treasure_cache", []string{"frontier"}, blocked)
	if d["frontier"] != 3 {
		t.Errorf("frontier = %d jumps, want 3 (round gold_run via haven, market_prime)", d["frontier"])
	}
}

// Autopilot exempts the route's endpoints, so a blocked system may still be
// the destination; it just cannot be flown through.
func TestContrabandDistAllowsBlockedDestination(t *testing.T) {
	blocked := map[string]string{"the_rampart": "customs"}
	d := contrabandDist(contrabandTestGraph(), "treasure_cache", []string{"the_rampart"}, blocked)
	if d["the_rampart"] != 1 {
		t.Errorf("the_rampart = %d jumps, want 1 (endpoint is exempt)", d["the_rampart"])
	}
}

func TestContrabandDistNothingBlockedMatchesPlainBFS(t *testing.T) {
	g := contrabandTestGraph()
	want := navigation.BFSJumps(g, "treasure_cache", []string{"ironhearth", "frontier"})
	got := contrabandDist(g, "treasure_cache", []string{"ironhearth", "frontier"}, nil)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
}
