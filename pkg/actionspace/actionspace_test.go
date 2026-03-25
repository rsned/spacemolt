package actionspace

import "testing"

func TestActionResultBranchingCount(t *testing.T) {
	r := ActionResult{Valid: true, BranchingCount: 1}
	if r.BranchingCount != 1 {
		t.Errorf("got %d, want 1", r.BranchingCount)
	}

	r2 := ActionResult{Valid: true, Targets: []string{"a", "b", "c"}, BranchingCount: 3}
	if r2.BranchingCount != 3 {
		t.Errorf("got %d, want 3", r2.BranchingCount)
	}

	r3 := ActionResult{Valid: false, BranchingCount: 0}
	if r3.BranchingCount != 0 {
		t.Errorf("got %d, want 0", r3.BranchingCount)
	}
}

func TestActionSpaceValidActions(t *testing.T) {
	as := ActionSpace{
		Actions: []ActionResult{
			{Action: Action{Name: "mine"}, Valid: true, BranchingCount: 1},
			{Action: Action{Name: "buy"}, Valid: false},
			{Action: Action{Name: "travel"}, Valid: true, Targets: []string{"poi_1"}, BranchingCount: 1},
		},
	}

	valid := as.ValidActions()
	if len(valid) != 2 {
		t.Fatalf("got %d valid, want 2", len(valid))
	}
	if valid[0].Action.Name != "mine" {
		t.Errorf("got %s, want mine", valid[0].Action.Name)
	}
}

func TestActionSpaceValidByCategory(t *testing.T) {
	as := ActionSpace{
		Actions: []ActionResult{
			{Action: Action{Name: "mine", Category: "mining"}, Valid: true, BranchingCount: 1},
			{Action: Action{Name: "buy", Category: "trading"}, Valid: true, BranchingCount: 1},
			{Action: Action{Name: "sell", Category: "trading"}, Valid: false},
		},
	}

	trading := as.ValidByCategory("trading")
	if len(trading) != 1 {
		t.Fatalf("got %d, want 1", len(trading))
	}
	if trading[0].Action.Name != "buy" {
		t.Errorf("got %s, want buy", trading[0].Action.Name)
	}
}
