package main

import (
	"slices"
	"testing"
)

// buildTestGraph builds an undirected graph from edge pairs.
func buildTestGraph(edges [][2]string) map[string][]string {
	g := make(map[string][]string)
	for _, e := range edges {
		g[e[0]] = append(g[e[0]], e[1])
		g[e[1]] = append(g[e[1]], e[0])
	}
	return g
}

func TestBFSJumps(t *testing.T) {
	// a - b - c - d   and   a - e - d
	g := buildTestGraph([][2]string{
		{"a", "b"}, {"b", "c"}, {"c", "d"}, {"a", "e"}, {"e", "d"},
	})
	targets := []string{"a", "b", "c", "d", "e", "x"}
	got := bfsJumps(g, "a", targets)

	want := map[string]int{"a": 0, "b": 1, "c": 2, "d": 2, "e": 1, "x": routeInf}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("bfsJumps a->%s = %d, want %d", k, got[k], v)
		}
	}
}

// distMatrix computes all-pairs jump distances for use in optimalOrder tests.
func distMatrix(g map[string][]string, nodes []string) map[string]map[string]int {
	d := make(map[string]map[string]int)
	for _, n := range nodes {
		d[n] = bfsJumps(g, n, nodes)
	}
	return d
}

func TestOptimalOrder(t *testing.T) {
	// Linear chain: start=a, waypoints b,c,d at distances 1,2,3.
	// Visiting in reverse (d,c,b) would cost 3+1+1=5; in order 1+1+1=3.
	g := buildTestGraph([][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}})
	nodes := []string{"a", "b", "c", "d"}
	dist := distMatrix(g, nodes)

	order, total, ok := optimalOrder("a", []string{"d", "b", "c"}, dist, false)
	if !ok {
		t.Fatal("optimalOrder returned not ok")
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if !slices.Equal(order, []string{"b", "c", "d"}) {
		t.Errorf("order = %v, want [b c d]", order)
	}
}

func TestOptimalOrderReturn(t *testing.T) {
	// Square loop: a-b-c-d-a. Visiting b,c,d and returning to a costs 4.
	g := buildTestGraph([][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}, {"d", "a"}})
	nodes := []string{"a", "b", "c", "d"}
	dist := distMatrix(g, nodes)

	order, total, ok := optimalOrder("a", []string{"b", "c", "d"}, dist, true)
	if !ok {
		t.Fatal("optimalOrder returned not ok")
	}
	if total != 4 {
		t.Errorf("total = %d, want 4 (full loop), got order %v", total, order)
	}
}

func TestOptimalOrderUnreachable(t *testing.T) {
	g := buildTestGraph([][2]string{{"a", "b"}})
	nodes := []string{"a", "b", "z"}
	dist := distMatrix(g, nodes)

	if _, _, ok := optimalOrder("a", []string{"b", "z"}, dist, false); ok {
		t.Error("expected not ok for unreachable waypoint, got ok")
	}
}

func TestResolveSystemToken(t *testing.T) {
	byID := map[string]string{"market_prime": "market_prime", "sol": "sol"}
	byName := map[string]string{"market prime": "market_prime", "sol": "sol"}

	cases := []struct {
		tok  string
		want string
		ok   bool
	}{
		{"market_prime", "market_prime", true},
		{"Market Prime", "market_prime", true}, // name with space + case
		{"SOL", "sol", true},
		{"nowhere", "", false},
	}
	for _, c := range cases {
		got, ok := resolveSystemToken(c.tok, byID, byName)
		if ok != c.ok || got != c.want {
			t.Errorf("resolveSystemToken(%q) = (%q, %v), want (%q, %v)", c.tok, got, ok, c.want, c.ok)
		}
	}
}
