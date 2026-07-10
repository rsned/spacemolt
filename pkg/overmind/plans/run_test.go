package plans

import (
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/craftbrain"
)

func roster2() []RosterAgent {
	return []RosterAgent{{AgentID: "craftsman-2", Station: "hub_a"}, {AgentID: "craftsman-3", Station: "hub_b"}}
}

func TestNewRunParksCyclesAndBlocked(t *testing.T) {
	qf := QueueFile{
		Manifest: Manifest{PlanID: "p1", BudgetCap: 1000},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "a", Qty: 1, StationID: "hub_a", DependsOn: []string{"craft-2"}},
			{ID: "craft-2", Kind: craftbrain.KindCraft, ItemID: "b", Qty: 1, StationID: "hub_a", DependsOn: []string{"craft-1"}}, // cycle
			{ID: "blocked-3", Kind: craftbrain.KindBlocked, ItemID: "c", Qty: 2, Reason: "no facility"},
			{ID: "mine-4", Kind: craftbrain.KindMine, ItemID: "d", Qty: 5},
		}},
	}
	pr := NewRun(qf, roster2())
	if n := pr.NodeByID("craft-1"); n.State != NodeParked || n.Park != ParkCycle {
		t.Errorf("craft-1 = %s/%s, want parked/cycle", n.State, n.Park)
	}
	if n := pr.NodeByID("blocked-3"); n.State != NodeParked || n.Park != ParkBlocked || n.ParkDetail != "no facility" {
		t.Errorf("blocked-3 = %+v", n)
	}
	if n := pr.NodeByID("mine-4"); n.State != NodeWaiting {
		t.Errorf("mine-4 = %s, want waiting", n.State)
	}
}

func TestNewRunPinsCraftsAndRecipients(t *testing.T) {
	qf := QueueFile{
		Manifest: Manifest{PlanID: "p2"},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 2, StationID: "hub_a",
				DependsOn: []string{"haul-2", "craft-3"}},
			{ID: "haul-2", Kind: craftbrain.KindHaul, ItemID: "ore", Qty: 10, FromBase: "far", ToBase: "hub_a"},
			{ID: "craft-3", Kind: craftbrain.KindCraft, ItemID: "part", Qty: 4, StationID: "hub_b"},
		}},
	}
	pr := NewRun(qf, roster2())
	c1, c3 := pr.NodeByID("craft-1"), pr.NodeByID("craft-3")
	if c1.Agent != "craftsman-2" || c3.Agent != "craftsman-3" {
		t.Fatalf("pins = %q, %q; want round-robin craftsman-2, craftsman-3", c1.Agent, c3.Agent)
	}
	if h := pr.NodeByID("haul-2"); h.Recipient != "craftsman-2" {
		t.Errorf("haul-2 recipient = %q, want craft-1's agent", h.Recipient)
	}
	// craft-3 (hub_b) feeds craft-1 (hub_a): a synthetic xfer must exist and
	// craft-1 must now depend on it instead of craft-3 directly.
	var xfer *NodeRun
	for _, n := range pr.Nodes {
		if n.Synthetic {
			xfer = n
		}
	}
	if xfer == nil {
		t.Fatal("no synthetic xfer inserted")
	}
	if xfer.Node.FromBase != "hub_b" || xfer.Node.ToBase != "hub_a" ||
		xfer.Agent != "craftsman-3" || xfer.Recipient != "craftsman-2" {
		t.Errorf("xfer = %+v", xfer)
	}
	found := false
	for _, d := range c1.Node.DependsOn {
		if d == xfer.Node.ID {
			found = true
		}
		if d == "craft-3" {
			t.Error("craft-1 still depends directly on craft-3")
		}
	}
	if !found {
		t.Error("craft-1 does not depend on the xfer node")
	}
}

// TestNewRunDoesNotMutateInput pins the "never mutate the input QueueFile"
// invariant. It mirrors TestNewRunPinsCraftsAndRecipients's fixture (a
// cross-station craft->craft edge) so rule 6's in-place DependsOn rewrite is
// actually exercised, then deep-snapshots every input node's DependsOn (and
// the Manifest's MineItems) before calling NewRun and asserts none of it
// changed afterward.
func TestNewRunDoesNotMutateInput(t *testing.T) {
	qf := QueueFile{
		Manifest: Manifest{PlanID: "p3", MineItems: []string{"ore", "gas"}},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 2, StationID: "hub_a",
				DependsOn: []string{"haul-2", "craft-3"}},
			{ID: "haul-2", Kind: craftbrain.KindHaul, ItemID: "ore", Qty: 10, FromBase: "far", ToBase: "hub_a"},
			{ID: "craft-3", Kind: craftbrain.KindCraft, ItemID: "part", Qty: 4, StationID: "hub_b"},
		}},
	}

	// Deep-snapshot before NewRun runs; append([]string(nil), ...) copies the
	// string headers into a fresh backing array so later in-place edits to
	// qf.Plan.Nodes[i].DependsOn (were the clone-fix reverted) can't leak
	// into these snapshots too.
	wantDependsOn := make([][]string, len(qf.Plan.Nodes))
	for i, n := range qf.Plan.Nodes {
		wantDependsOn[i] = append([]string(nil), n.DependsOn...)
	}
	wantMineItems := append([]string(nil), qf.Manifest.MineItems...)

	pr := NewRun(qf, roster2())

	// Sanity: confirm rule 6 actually fired on this fixture, otherwise this
	// test can't distinguish "no mutation" from "no rewrite happened at all".
	syntheticFound := false
	for _, n := range pr.Nodes {
		if n.Synthetic {
			syntheticFound = true
		}
	}
	if !syntheticFound {
		t.Fatal("expected a rule-6 synthetic xfer on this fixture (see TestNewRunPinsCraftsAndRecipients); fixture drifted")
	}

	for i, n := range qf.Plan.Nodes {
		if !slices.Equal(n.DependsOn, wantDependsOn[i]) {
			t.Errorf("input node %s DependsOn mutated by NewRun: got %v, want %v", n.ID, n.DependsOn, wantDependsOn[i])
		}
	}
	if !slices.Equal(qf.Manifest.MineItems, wantMineItems) {
		t.Errorf("input Manifest.MineItems mutated by NewRun: got %v, want %v", qf.Manifest.MineItems, wantMineItems)
	}

	// pr.Manifest.MineItems must not share a backing array with the input's.
	if len(pr.Manifest.MineItems) == 0 {
		t.Fatal("pr.Manifest.MineItems is empty; fixture drifted")
	}
	pr.Manifest.MineItems[0] = "MUTATED"
	if qf.Manifest.MineItems[0] == "MUTATED" {
		t.Error("pr.Manifest.MineItems shares a backing array with input qf.Manifest.MineItems")
	}
}
