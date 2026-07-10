package plans

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/craftbrain"
)

func twoStep() *PlanRun {
	return &PlanRun{Manifest: Manifest{PlanID: "p"}, Status: "running", Nodes: []*NodeRun{
		{Node: craftbrain.Node{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "w", Qty: 2,
			DependsOn: []string{"mine-2"}}, State: NodeWaiting},
		{Node: craftbrain.Node{ID: "mine-2", Kind: craftbrain.KindMine, ItemID: "ore", Qty: 10}, State: NodeWaiting},
	}}
}

func TestReadyNodesRespectsDeps(t *testing.T) {
	pr := twoStep()
	ready := pr.ReadyNodes()
	if len(ready) != 1 || ready[0].Node.ID != "mine-2" {
		t.Fatalf("ready = %+v, want just mine-2", ready)
	}
	pr.NodeByID("mine-2").State = NodeDone
	ready = pr.ReadyNodes()
	if len(ready) != 1 || ready[0].Node.ID != "craft-1" {
		t.Fatalf("after mine done, ready = %+v, want craft-1", ready)
	}
}

func TestReadyNodesEmptyWhenPaused(t *testing.T) {
	pr := twoStep()
	pr.Control.Pause = true
	pr.Recompute()
	if got := pr.ReadyNodes(); len(got) != 0 {
		t.Fatalf("paused plan returned ready nodes: %+v", got)
	}
	if pr.Status != "paused" {
		t.Fatalf("status = %q, want paused", pr.Status)
	}
}

func TestRecomputePartialWithParked(t *testing.T) {
	pr := twoStep()
	pr.NodeByID("mine-2").State = NodeDone
	c := pr.NodeByID("craft-1")
	c.State = NodeParked
	c.Park = ParkFailed
	pr.Recompute()
	if pr.Status != "partial" {
		t.Fatalf("status = %q, want partial", pr.Status)
	}
}

func TestItemProgress(t *testing.T) {
	pr := twoStep()
	pr.NodeByID("mine-2").DoneQty = 4
	prog := pr.ItemProgress()
	if p := prog["ore"]; p.Done != 4 || p.Total != 10 {
		t.Fatalf("ore = %+v, want 4/10", p)
	}
}
