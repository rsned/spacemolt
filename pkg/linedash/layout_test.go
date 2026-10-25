package linedash

import "testing"

func TestLayoutColumnsFollowLongestPath(t *testing.T) {
	line := fixtureLine(t)
	r := Evaluate(line, baseInputs(line))
	Layout(&r)

	wantCol := map[string]int{
		"ice_mine": 0, "culture_buy": 0,
		"voss_inputs": 1,
		"still":       2,
		"voss_output": 3,
		"alhena_sell": 4, "tc_sell": 4,
	}
	seen := map[[2]int]string{}
	for _, n := range r.Nodes {
		col, ok := wantCol[n.ID]
		if !ok {
			t.Fatalf("unexpected node %q", n.ID)
		}
		if wantX := layoutMargin + col*(nodeWidth+colGutter); n.X != wantX {
			t.Errorf("%s: X = %d, want %d (column %d)", n.ID, n.X, wantX, col)
		}
		if n.Y < layoutMargin {
			t.Errorf("%s: Y = %d, want >= margin %d", n.ID, n.Y, layoutMargin)
		}
		key := [2]int{n.X, n.Y}
		if other, dup := seen[key]; dup {
			t.Errorf("%s and %s share position %v", n.ID, other, key)
		}
		seen[key] = n.ID
	}
	// Rows follow declaration order within a column.
	if a, b := nodeOf(t, r, "ice_mine"), nodeOf(t, r, "culture_buy"); a.Y >= b.Y {
		t.Errorf("ice_mine Y %d should be above culture_buy Y %d", a.Y, b.Y)
	}
	if a, b := nodeOf(t, r, "alhena_sell"), nodeOf(t, r, "tc_sell"); a.Y >= b.Y {
		t.Errorf("alhena_sell Y %d should be above tc_sell Y %d", a.Y, b.Y)
	}
	if got := nodeOf(t, r, "ice_mine").Y; got != layoutMargin {
		t.Errorf("first row Y = %d, want %d", got, layoutMargin)
	}

	// Deterministic: a second layout of a copy gives the same positions.
	again := r
	again.Nodes = append([]NodeReport(nil), r.Nodes...)
	Layout(&again)
	for i := range r.Nodes {
		if r.Nodes[i].X != again.Nodes[i].X || r.Nodes[i].Y != again.Nodes[i].Y {
			t.Errorf("%s: layout not deterministic", r.Nodes[i].ID)
		}
	}
}

func TestLayoutRowPitchGrowsWithTallNodes(t *testing.T) {
	r := LineReport{Nodes: []NodeReport{
		{ID: "a", Detail: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}},
		{ID: "b"},
	}}
	Layout(&r)
	hA := nodeHeight(r.Nodes[0])
	if hA <= rowMinPitch {
		t.Fatalf("test needs a tall node, got height %d", hA)
	}
	if want := layoutMargin + hA + layoutGutter; r.Nodes[1].Y != want {
		t.Errorf("second row Y = %d, want %d", r.Nodes[1].Y, want)
	}
	if got := nodeHeight(NodeReport{}); got != nodeMinHeight {
		t.Errorf("empty node height = %d, want min %d", got, nodeMinHeight)
	}
}

func TestLayoutSurvivesCycle(t *testing.T) {
	r := LineReport{
		Nodes: []NodeReport{{ID: "a"}, {ID: "b"}},
		Edges: []EdgeReport{{From: "a", To: "b"}, {From: "b", To: "a"}},
	}
	Layout(&r) // must terminate
	for _, n := range r.Nodes {
		if n.X < layoutMargin || n.Y < layoutMargin {
			t.Errorf("%s placed at %d,%d", n.ID, n.X, n.Y)
		}
	}
}
