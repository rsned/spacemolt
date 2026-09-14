package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// The horizon/first_step oscillation, 2026-09-14. explorer-8 bounced between
// the two for 30+ hops of a 200-hop run.
//
// The mechanism: the STORED graph carries edges the server no longer honors
// (phantom rows from copied neighbour lists). liveAdjacency corrects the row
// for the system we are standing in — and then throws that correction away on
// the next hop. So each end of the pair believes the eligible target lies
// behind the other:
//
//   at horizon:    stored first_step->gsc_0010 is a phantom, so gsc_0010 looks
//                  2 jumps away via first_step  -> hop to first_step
//   at first_step: live says no gsc_0010; but horizon's row is stored again,
//                  so stale deep_range looks 2 jumps away via horizon
//                                                  -> hop back to horizon
//
// visitedThisRun does not help: both are ROUTE systems, not destinations, and
// routing through ineligible systems is deliberate.
func TestNextHopToward_DoesNotOscillateOnPhantomEdges(t *testing.T) {
	stale := time.Now().UTC().Add(-14 * 24 * time.Hour)
	fresh := time.Now().UTC()

	// Stored graph, phantom edges included.
	stored := map[string][]string{
		"horizon":    {"first_step", "the_telescope", "deep_range", "iron_reach"},
		"first_step": {"horizon", "the_telescope", "gsc_0010", "alpheratz"},
	}
	elig := systemEligible{
		visitedThisRun: map[string]bool{},
		now:            time.Now().UTC(),
		surveyed: map[string]time.Time{
			"horizon": fresh, "first_step": fresh, "the_telescope": fresh,
			// The phantom-only systems are stale, so they look worth visiting.
			"deep_range": stale, "iron_reach": stale,
			"gsc_0010": stale, "alpheratz": stale,
		},
	}

	// What the server actually reports when you stand in each system: neither
	// of the phantom pairs exists.
	liveHorizon := []game.ConnectionInfo{
		{SystemID: "first_step"}, {SystemID: "the_telescope"},
	}
	liveFirstStep := []game.ConnectionInfo{
		{SystemID: "horizon"}, {SystemID: "the_telescope"},
	}

	// Walk the run the way autoExplore does: at each hop, reload the stored
	// graph, learn the current system's live row, and pick. A frontier memory
	// that keeps every row it has learned must break the cycle; forgetting
	// them (the old behaviour) bounces forever.
	live := map[string][]game.ConnectionInfo{
		"horizon": liveHorizon, "first_step": liveFirstStep,
	}
	mem := newFrontierMemory()

	at := "horizon"
	seq := []string{at}
	for range 6 {
		mem.learn(at, live[at])
		hop, _, _, ok := nextHopToward(mem.adjacency(stored, at), at, elig)
		if !ok {
			break
		}
		at = hop
		seq = append(seq, at)
	}

	// horizon -> first_step once is fine; coming back is the bug.
	for i := 2; i < len(seq); i++ {
		if seq[i] == seq[i-2] {
			t.Fatalf("oscillated: %v", seq)
		}
	}
}

// liveAdjacency must not mutate rows it was not given. Keeping corrections
// across hops is only safe if each call replaces exactly one row.
func TestLiveAdjacency_ReplacesOnlyTheNamedRow(t *testing.T) {
	adj := map[string][]string{
		"a": {"b", "phantom"},
		"b": {"a", "other_phantom"},
	}
	adj = liveAdjacency(adj, "a", []game.ConnectionInfo{{SystemID: "b"}})
	if len(adj["a"]) != 1 || adj["a"][0] != "b" {
		t.Errorf("row a = %v, want [b]", adj["a"])
	}
	if len(adj["b"]) != 2 {
		t.Errorf("row b was disturbed: %v", adj["b"])
	}
}
