package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// freshEnough is a survey age the eligibility rule must treat as current.
var freshEnough = time.Duration(game.FreshnessSystem)*time.Second - time.Hour

// longEnoughAgo is a survey age the rule must treat as due again.
var longEnoughAgo = time.Duration(game.FreshnessSystem)*time.Second + time.Hour

func TestSystemEligible(t *testing.T) {
	now := time.Date(2026, 8, 29, 3, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		id      string
		age     time.Duration
		known   bool
		visited bool
		want    bool
	}{
		{name: "never surveyed is always eligible", id: "a", want: true},
		{name: "surveyed long ago is due again", id: "a", known: true, age: longEnoughAgo, want: true},
		{name: "surveyed recently is not", id: "a", known: true, age: freshEnough},
		{name: "visited this run is not, however stale", id: "a", known: true, age: longEnoughAgo, visited: true},
		{name: "an empty id is never eligible", id: "", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := systemEligible{
				surveyed:       map[string]time.Time{},
				visitedThisRun: map[string]bool{},
				now:            now,
			}
			if tc.known {
				e.surveyed[tc.id] = now.Add(-tc.age)
			}
			if tc.visited {
				e.visitedThisRun[tc.id] = true
			}
			if got := e.eligible(tc.id); got != tc.want {
				t.Errorf("eligible(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

// chain is a---b---c---d, a line, which is the shape that broke the old picker.
func chain() map[string][]string {
	return buildAdjacency([]knowledge.Connection{
		{FromSystem: "a", ToSystem: "b"},
		{FromSystem: "b", ToSystem: "c"},
		{FromSystem: "c", ToSystem: "d"},
	})
}

func TestNextHopToward(t *testing.T) {
	now := time.Date(2026, 8, 29, 3, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Minute)

	t.Run("an eligible neighbour is the hop itself", func(t *testing.T) {
		e := systemEligible{visitedThisRun: map[string]bool{}, now: now}
		hop, target, jumps, ok := nextHopToward(chain(), "a", e)
		if !ok || hop != "b" || target != "b" || jumps != 1 {
			t.Fatalf("got (%q,%q,%d,%v), want (b,b,1,true)", hop, target, jumps, ok)
		}
	})

	t.Run("routes THROUGH a fresh system to reach a stale one", func(t *testing.T) {
		// b and c are freshly surveyed; d has never been. The old picker gave
		// up here, because every immediate neighbour was "visited". The walk
		// must cross b and c to reach d.
		e := systemEligible{
			surveyed:       map[string]time.Time{"b": fresh, "c": fresh},
			visitedThisRun: map[string]bool{},
			now:            now,
		}
		hop, target, jumps, ok := nextHopToward(chain(), "a", e)
		if !ok || hop != "b" || target != "d" || jumps != 3 {
			t.Fatalf("got (%q,%q,%d,%v), want (b,d,3,true)", hop, target, jumps, ok)
		}
	})

	t.Run("a corridor visited this run is still traversable", func(t *testing.T) {
		// This is the narrow-map case: b was crossed this run, so it can never
		// be a destination, but it is the ONLY way to d.
		e := systemEligible{
			surveyed:       map[string]time.Time{"c": fresh},
			visitedThisRun: map[string]bool{"b": true},
			now:            now,
		}
		hop, target, _, ok := nextHopToward(chain(), "a", e)
		if !ok || hop != "b" || target != "d" {
			t.Fatalf("got (%q,%q,%v), want hop b toward d", hop, target, ok)
		}
	})

	t.Run("nearest wins over stalest", func(t *testing.T) {
		// d is staler than b, but b is nearer. Ranking by staleness alone would
		// send the walk across the map; breadth-first ordering must not.
		e := systemEligible{
			surveyed: map[string]time.Time{
				"b": now.Add(-longEnoughAgo),
				"d": now.Add(-longEnoughAgo * 10),
			},
			visitedThisRun: map[string]bool{},
			now:            now,
		}
		_, target, jumps, ok := nextHopToward(chain(), "a", e)
		if !ok || target != "b" || jumps != 1 {
			t.Fatalf("got (%q,%d,%v), want target b at 1 jump", target, jumps, ok)
		}
	})

	t.Run("nothing eligible reports so rather than looping", func(t *testing.T) {
		e := systemEligible{
			surveyed: map[string]time.Time{
				"a": fresh, "b": fresh, "c": fresh, "d": fresh,
			},
			visitedThisRun: map[string]bool{},
			now:            now,
		}
		if _, _, _, ok := nextHopToward(chain(), "a", e); ok {
			t.Error("reported a target when every system was fresh")
		}
	})

	t.Run("an unknown origin yields nothing", func(t *testing.T) {
		e := systemEligible{visitedThisRun: map[string]bool{}, now: now}
		if _, _, _, ok := nextHopToward(chain(), "", e); ok {
			t.Error("reported a target from an empty origin")
		}
	})
}

func TestBuildAdjacencyIsUndirectedAndDeduped(t *testing.T) {
	adj := buildAdjacency([]knowledge.Connection{
		{FromSystem: "a", ToSystem: "b"},
		{FromSystem: "b", ToSystem: "a"}, // the reverse row, already implied
		{FromSystem: "a", ToSystem: "b"}, // an exact duplicate
	})
	if got := len(adj["a"]); got != 1 {
		t.Errorf("a has %d neighbours, want 1 (deduped): %v", got, adj["a"])
	}
	if got := len(adj["b"]); got != 1 {
		t.Errorf("b has %d neighbours, want 1 (undirected): %v", got, adj["b"])
	}
}
