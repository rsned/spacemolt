package plans

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/craftbrain"
)

func craftbrainNode(id string) craftbrain.Node {
	return craftbrain.Node{ID: id, Kind: craftbrain.KindMine, ItemID: "x", Qty: 1}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pr := &PlanRun{Manifest: Manifest{PlanID: "p1", BudgetCap: 500}, Status: "running",
		Nodes: []*NodeRun{{Node: craftbrainNode("mine-1"), State: NodeWaiting}}}
	if err := SaveRun(dir, pr); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRun(filepath.Join(dir, "p1.state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.BudgetCap != 500 || len(got.Nodes) != 1 || got.Nodes[0].Node.ID != "mine-1" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestLoadAllRunsSkipsCorrupt(t *testing.T) {
	dir := t.TempDir()
	_ = SaveRun(dir, &PlanRun{Manifest: Manifest{PlanID: "good"}, Status: "running"})
	if err := os.WriteFile(filepath.Join(dir, "bad.state.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	runs, errs := LoadAllRuns(dir)
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want 1 error for corrupt file", errs)
	}
	if len(runs) != 1 || runs[0].Manifest.PlanID != "good" {
		t.Fatalf("runs = %+v", runs)
	}
}
