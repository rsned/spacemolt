package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// fakeKB is a minimal knowledge.Base for selection tests: it embeds the
// interface (unimplemented methods panic) and serves seeded systems/connections.
type fakeKB struct {
	knowledge.Base
	systems []knowledge.System
	conns   []knowledge.Connection
}

func (f *fakeKB) GetSystems(context.Context) ([]knowledge.System, error) {
	return f.systems, nil
}
func (f *fakeKB) GetConnections(context.Context) ([]knowledge.Connection, error) {
	return f.conns, nil
}

// No-op write stubs so the autopilot OnWaypoint capture path (KBUpdateSystem /
// KBUpdatePOI) is harmless in tests instead of panicking on the embedded nil Base.
func (f *fakeKB) RememberSystem(context.Context, knowledge.System) error { return nil }
func (f *fakeKB) RememberPOI(context.Context, knowledge.POI) error       { return nil }

// GetConnectionMetrics lets the galaxy graph's BuildFromDB (used by the haul
// route-safety + stranded-recovery checks) run on the fake KB; topology comes from
// GetConnections, so an empty metrics set is fine.
func (f *fakeKB) GetConnectionMetrics(context.Context) ([]knowledge.ConnectionMetric, error) {
	return nil, nil
}

func undirected(pairs ...[2]string) []knowledge.Connection {
	out := make([]knowledge.Connection, 0, len(pairs)*2)
	for _, p := range pairs {
		out = append(out,
			knowledge.Connection{FromSystem: p[0], ToSystem: p[1]},
			knowledge.Connection{FromSystem: p[1], ToSystem: p[0]},
		)
	}
	return out
}

func TestNextExploreTargetFrontierBeatsUnvisited(t *testing.T) {
	// a-b and a-c, both one jump away. c is a known unvisited system; b is a
	// frontier (a connection endpoint absent from GetSystems). Frontier wins.
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 10, LastUpdatedTick: 100},
			{ID: "c", LastVisitedTick: 0},
		},
		conns: undirected([2]string{"a", "b"}, [2]string{"a", "c"}),
	}
	target, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok || target != "b" {
		t.Fatalf("want frontier b, got target=%q ok=%v", target, ok)
	}
}

func TestNextExploreTargetNearestUnvisited(t *testing.T) {
	// No frontier (all endpoints known). b is 1 jump, c is 2. Both unvisited.
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 10, LastUpdatedTick: 100},
			{ID: "b", LastVisitedTick: 0},
			{ID: "c", LastVisitedTick: 0},
		},
		conns: undirected([2]string{"a", "b"}, [2]string{"b", "c"}),
	}
	target, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok || target != "b" {
		t.Fatalf("want nearest unvisited b, got target=%q ok=%v", target, ok)
	}
}

func TestNextExploreTargetStaleWhenNoUnvisited(t *testing.T) {
	// All known and visited; b is stale (last updated long ago), c is fresh.
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 5, LastUpdatedTick: 9_999},
			{ID: "b", LastVisitedTick: 5, LastUpdatedTick: 100},   // 9999-100 > 8640 -> stale
			{ID: "c", LastVisitedTick: 5, LastUpdatedTick: 9_900}, // fresh
		},
		conns: undirected([2]string{"a", "b"}, [2]string{"a", "c"}),
	}
	target, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 9_999)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok || target != "b" {
		t.Fatalf("want stale b, got target=%q ok=%v", target, ok)
	}
}

func TestNextExploreTargetNoneWhenAllFresh(t *testing.T) {
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 5, LastUpdatedTick: 9_990},
			{ID: "b", LastVisitedTick: 5, LastUpdatedTick: 9_990},
		},
		conns: undirected([2]string{"a", "b"}),
	}
	_, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 9_999)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Fatal("expected no target when everything reachable is fresh")
	}
}

func TestNextExploreTargetUnreachableExcluded(t *testing.T) {
	// d is a frontier but on a disconnected island (c-d), unreachable from a.
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 5, LastUpdatedTick: 100},
			{ID: "b", LastVisitedTick: 5, LastUpdatedTick: 100}, // fresh, reachable
			{ID: "c", LastVisitedTick: 5, LastUpdatedTick: 100},
		},
		conns: undirected([2]string{"a", "b"}, [2]string{"c", "d"}),
	}
	_, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 200)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Fatal("unreachable frontier d must not be selected")
	}
}

func TestNextExploreTargetExpandsOverCalls(t *testing.T) {
	// Chain a-b-c. From "a", the nearest frontier is "b". After the explorer
	// visits "b" (KB now records it as visited+fresh), a second call from "b"
	// must advance to the next frontier "c" — not oscillate back to "a".
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 50, LastUpdatedTick: 100},
		},
		conns: undirected([2]string{"a", "b"}, [2]string{"b", "c"}),
	}

	target, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 100)
	if err != nil {
		t.Fatalf("call 1 err: %v", err)
	}
	if !ok || target != "b" {
		t.Fatalf("call 1: want frontier b, got target=%q ok=%v", target, ok)
	}

	// Simulate visiting b: it becomes a known, freshly-updated system.
	kb.systems = append(kb.systems, knowledge.System{ID: "b", LastVisitedTick: 100, LastUpdatedTick: 100})

	target, ok, err = NextExploreTarget(context.Background(), kb, "b", DefaultExploreStaleTicks, 100)
	if err != nil {
		t.Fatalf("call 2 err: %v", err)
	}
	if !ok || target != "c" {
		t.Fatalf("call 2: want next frontier c (no oscillation back to a), got target=%q ok=%v", target, ok)
	}
}
