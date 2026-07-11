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

// TestNewRunInsertsXferForCrossStationBuy (C2): a buy node at seller_x feeding
// a craft at hub_a strands its goods at seller_x — BuyDirected gifts them to
// the consumer craft's pinned agent AT the seller station. A synthetic xfer
// must move them to the craft station, couriered by that same agent (the buy's
// recipient) and self-deposited.
func TestNewRunInsertsXferForCrossStationBuy(t *testing.T) {
	qf := QueueFile{
		Manifest: Manifest{PlanID: "pbuy"},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 2, StationID: "hub_a",
				DependsOn: []string{"buy-2"}},
			{ID: "buy-2", Kind: craftbrain.KindBuy, ItemID: "ore", Qty: 10, StationID: "seller_x"},
		}},
	}
	pr := NewRun(qf, roster2())
	c1 := pr.NodeByID("craft-1")

	var xfer *NodeRun
	for _, n := range pr.Nodes {
		if n.Synthetic {
			xfer = n
		}
	}
	if xfer == nil {
		t.Fatal("no synthetic xfer inserted for a cross-station buy->craft edge")
	}
	if xfer.Node.Kind != craftbrain.KindHaul || xfer.Node.FromBase != "seller_x" || xfer.Node.ToBase != "hub_a" {
		t.Errorf("xfer = %+v, want haul seller_x->hub_a", xfer.Node)
	}
	if xfer.Node.ItemID != "ore" || xfer.Node.Qty != 10 {
		t.Errorf("xfer item/qty = %s/%d, want ore/10", xfer.Node.ItemID, xfer.Node.Qty)
	}
	if xfer.Agent != c1.Agent {
		t.Errorf("xfer courier = %q, want the consumer craft's agent %q (the buy's recipient holds the goods)", xfer.Agent, c1.Agent)
	}
	if xfer.Recipient != "" {
		t.Errorf("xfer recipient = %q, want self (empty) — courier self-deposits at the craft station", xfer.Recipient)
	}
	if slices.Contains(c1.Node.DependsOn, "buy-2") {
		t.Error("craft-1 still depends directly on buy-2")
	}
	if !slices.Contains(c1.Node.DependsOn, xfer.Node.ID) {
		t.Error("craft-1 does not depend on the xfer node")
	}
}

// TestNewRunNoXferForSameStationBuy (C2 negative): a buy at the SAME station as
// its consumer craft needs no xfer — the buy already gifts to the consumer
// agent there.
func TestNewRunNoXferForSameStationBuy(t *testing.T) {
	qf := QueueFile{
		Manifest: Manifest{PlanID: "pbuy2"},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 2, StationID: "hub_a",
				DependsOn: []string{"buy-2"}},
			{ID: "buy-2", Kind: craftbrain.KindBuy, ItemID: "ore", Qty: 10, StationID: "hub_a"},
		}},
	}
	pr := NewRun(qf, roster2())
	for _, n := range pr.Nodes {
		if n.Synthetic {
			t.Fatalf("unexpected synthetic xfer for a same-station buy->craft edge: %+v", n.Node)
		}
	}
}

// TestNewRunInsertsXferForSameStationDifferentAgentCrafts (C3): two crafts at
// the same station pinned to DIFFERENT agents (round-robin) still strand the
// intermediate — the producer deposits into its own storage; the consumer
// (different agent) can't withdraw it. A same-station xfer (gift) must bridge
// them: courier = producer's agent, recipient = consumer's agent.
func TestNewRunInsertsXferForSameStationDifferentAgentCrafts(t *testing.T) {
	qf := QueueFile{
		Manifest: Manifest{PlanID: "pc3"},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 2, StationID: "hub_a",
				DependsOn: []string{"craft-2"}},
			{ID: "craft-2", Kind: craftbrain.KindCraft, ItemID: "part", Qty: 4, StationID: "hub_a"},
		}},
	}
	pr := NewRun(qf, roster2())
	c1, c2 := pr.NodeByID("craft-1"), pr.NodeByID("craft-2")
	if c1.Agent == c2.Agent {
		t.Fatalf("precondition: round-robin must pin the two crafts to different agents, got both %q", c1.Agent)
	}

	var xfer *NodeRun
	for _, n := range pr.Nodes {
		if n.Synthetic {
			xfer = n
		}
	}
	if xfer == nil {
		t.Fatal("no synthetic xfer for same-station different-agent craft->craft edge")
	}
	if xfer.Node.FromBase != "hub_a" || xfer.Node.ToBase != "hub_a" {
		t.Errorf("xfer = %+v, want a same-station (hub_a->hub_a) gift", xfer.Node)
	}
	if xfer.Agent != c2.Agent {
		t.Errorf("xfer courier = %q, want producer craft-2's agent %q", xfer.Agent, c2.Agent)
	}
	if xfer.Recipient != c1.Agent {
		t.Errorf("xfer recipient = %q, want consumer craft-1's agent %q", xfer.Recipient, c1.Agent)
	}
	if !slices.Contains(c1.Node.DependsOn, xfer.Node.ID) {
		t.Error("craft-1 does not depend on the xfer node")
	}
}

// TestNewRunNoXferForSameStationSameAgentCrafts (C3 negative): two crafts at
// the same station pinned to the SAME agent (roster of 1) need no xfer — the
// producer's outputs and the consumer's inputs share one storage.
func TestNewRunNoXferForSameStationSameAgentCrafts(t *testing.T) {
	qf := QueueFile{
		Manifest: Manifest{PlanID: "pc3b"},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 2, StationID: "hub_a",
				DependsOn: []string{"craft-2"}},
			{ID: "craft-2", Kind: craftbrain.KindCraft, ItemID: "part", Qty: 4, StationID: "hub_a"},
		}},
	}
	pr := NewRun(qf, []RosterAgent{{AgentID: "craftsman-2", Station: "hub_a"}})
	c1, c2 := pr.NodeByID("craft-1"), pr.NodeByID("craft-2")
	if c1.Agent != c2.Agent {
		t.Fatalf("precondition: roster of 1 must pin both crafts to the same agent, got %q and %q", c1.Agent, c2.Agent)
	}
	for _, n := range pr.Nodes {
		if n.Synthetic {
			t.Fatalf("unexpected synthetic xfer for a same-station same-agent craft->craft edge: %+v", n.Node)
		}
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
