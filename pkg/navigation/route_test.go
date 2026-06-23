package navigation

import (
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// buildTestGraph builds an undirected graph from edge pairs.
func buildTestGraph(edges [][2]string) JumpGraph {
	g := make(JumpGraph)
	for _, e := range edges {
		g[e[0]] = append(g[e[0]], e[1])
		g[e[1]] = append(g[e[1]], e[0])
	}
	return g
}

// distMatrix computes all-pairs jump distances for use in OptimalOrder tests.
func distMatrix(g JumpGraph, nodes []string) map[string]map[string]int {
	d := make(map[string]map[string]int)
	for _, n := range nodes {
		d[n] = BFSJumps(g, n, nodes)
	}
	return d
}

func TestBFSJumps(t *testing.T) {
	// a - b - c - d   and   a - e - d
	g := buildTestGraph([][2]string{
		{"a", "b"}, {"b", "c"}, {"c", "d"}, {"a", "e"}, {"e", "d"},
	})
	targets := []string{"a", "b", "c", "d", "e", "x"}
	got := BFSJumps(g, "a", targets)

	want := map[string]int{"a": 0, "b": 1, "c": 2, "d": 2, "e": 1, "x": RouteInf}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("BFSJumps a->%s = %d, want %d", k, got[k], v)
		}
	}
}

func TestOptimalOrder(t *testing.T) {
	g := buildTestGraph([][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}})
	nodes := []string{"a", "b", "c", "d"}
	dist := distMatrix(g, nodes)

	order, total, ok := OptimalOrder("a", []string{"d", "b", "c"}, dist, false)
	if !ok {
		t.Fatal("OptimalOrder returned not ok")
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if !slices.Equal(order, []string{"b", "c", "d"}) {
		t.Errorf("order = %v, want [b c d]", order)
	}
}

func TestOptimalOrderReturn(t *testing.T) {
	g := buildTestGraph([][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}, {"d", "a"}})
	nodes := []string{"a", "b", "c", "d"}
	dist := distMatrix(g, nodes)

	_, total, ok := OptimalOrder("a", []string{"b", "c", "d"}, dist, true)
	if !ok {
		t.Fatal("OptimalOrder returned not ok")
	}
	if total != 4 {
		t.Errorf("total = %d, want 4 (full loop)", total)
	}
}

func TestOptimalOrderUnreachable(t *testing.T) {
	g := buildTestGraph([][2]string{{"a", "b"}})
	nodes := []string{"a", "b", "z"}
	dist := distMatrix(g, nodes)

	if _, _, ok := OptimalOrder("a", []string{"b", "z"}, dist, false); ok {
		t.Error("expected not ok for unreachable waypoint, got ok")
	}
}

func TestJumpGraphFromConnections(t *testing.T) {
	conns := []knowledge.Connection{
		{FromSystem: "a", ToSystem: "b"},
		{FromSystem: "b", ToSystem: "c"},
		{FromSystem: "", ToSystem: "x"}, // skipped: empty endpoint
	}
	g := JumpGraphFromConnections(conns)
	if !slices.Contains(g["a"], "b") || !slices.Contains(g["b"], "a") {
		t.Errorf("expected undirected a<->b, got %v", g)
	}
	if !slices.Contains(g["b"], "c") || !slices.Contains(g["c"], "b") {
		t.Errorf("expected undirected b<->c, got %v", g)
	}
	if len(g["x"]) != 0 {
		t.Errorf("empty-endpoint connection should be skipped, got %v", g["x"])
	}
}
