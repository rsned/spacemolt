package plans

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/tasks"
)

func controlEvent(kind, detail string) control.Event {
	return control.Event{Kind: kind, Detail: detail}
}

func newRunner(t *testing.T) *Runner {
	t.Helper()
	qd, sd := t.TempDir(), t.TempDir()
	return &Runner{
		QueueDir: qd, StateDir: sd,
		Store:   tasks.NewStore(nil, log.New(io.Discard, "", 0)),
		Handoff: handoff.NewQueue(filepath.Join(t.TempDir(), "handoff.json")),
		Roster:  []RosterAgent{{AgentID: "craftsman-2", Station: "hub_a"}},
		Managed: map[string]string{"marketbot_sol": "sol_central"},
		Logger:  log.New(io.Discard, "", 0),
	}
}

func dropPlan(t *testing.T, r *Runner, qf QueueFile) {
	t.Helper()
	raw, _ := json.Marshal(qf)
	if err := os.WriteFile(filepath.Join(r.QueueDir, qf.Manifest.PlanID+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTickIntakesAndDispatchesReady(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "p1", BudgetCap: 100},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "mine-1", Kind: craftbrain.KindMine, ItemID: "ore", Qty: 5},
		}}})
	r.Tick()
	// Queue file consumed, state file exists, task in store.
	if _, err := os.Stat(filepath.Join(r.QueueDir, "p1.json")); !os.IsNotExist(err) {
		t.Error("queue file not consumed")
	}
	task, ok := r.Store.Get("p1/mine-1/r0")
	if !ok || task.Script != "mine_node" || task.RoleRequired != "craftsman" {
		t.Fatalf("task = %+v, %v", task, ok)
	}
}

func TestTickCollectsDoneAndReleasesDependent(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "p2", BudgetCap: 1000},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "w", Qty: 2, RecipeID: "make_w",
				StationID: "hub_a", FeeTotal: 40, DependsOn: []string{"mine-2"}},
			{ID: "mine-2", Kind: craftbrain.KindMine, ItemID: "ore", Qty: 5},
		}}})
	r.Tick() // intake + dispatch mine-2
	// Simulate worker completion via the store's own event path.
	r.Store.HandleEvent("craftsman-2", controlEvent("task_done", "p2/mine-2/r0"))
	r.Tick() // collect + release craft-1
	if task, ok := r.Store.Get("p2/craft-1/r0"); !ok || task.AgentID != "craftsman-2" {
		t.Fatalf("craft task = %+v, %v (want pinned to craftsman-2)", task, ok)
	}
	runs, _ := LoadAllRuns(r.StateDir)
	if runs[0].Spent != 0 { // mine has no fee
		t.Errorf("spent = %d after mine", runs[0].Spent)
	}
	if n := runs[0].NodeByID("mine-2"); n.State != NodeDone || n.DoneQty != 5 {
		t.Errorf("mine-2 = %+v", n)
	}
}

func TestTickBudgetParksAndPauses(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "p3", BudgetCap: 10},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "w", Qty: 1, RecipeID: "make_w",
				StationID: "hub_a", FeeTotal: 40},
		}}})
	r.Tick()
	runs, _ := LoadAllRuns(r.StateDir)
	n := runs[0].NodeByID("craft-1")
	if n.State != NodeParked || n.Park != ParkOverBudget {
		t.Fatalf("craft-1 = %+v, want parked/over_budget", n)
	}
	if runs[0].Status != "paused" {
		t.Errorf("status = %q, want paused", runs[0].Status)
	}
}

// TestTickBudgetGateAccountsForInFlightSpend pins the fix for a budget-gate
// bug: two spending nodes ready in the same tick, each individually under
// cap but over cap combined. The gate must count the first node's FeeTotal
// as committed (in-flight) the moment it dispatches — even though pr.Spent
// only increments on completion — so the second node parks instead of
// overshooting BudgetCap.
func TestTickBudgetGateAccountsForInFlightSpend(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "p6", BudgetCap: 50},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-a", Kind: craftbrain.KindCraft, ItemID: "a", Qty: 1, RecipeID: "make_a",
				StationID: "hub_a", FeeTotal: 30},
			{ID: "craft-b", Kind: craftbrain.KindCraft, ItemID: "b", Qty: 1, RecipeID: "make_b",
				StationID: "hub_a", FeeTotal: 30},
		}}})
	r.Tick()

	runs, _ := LoadAllRuns(r.StateDir)
	a := runs[0].NodeByID("craft-a")
	b := runs[0].NodeByID("craft-b")

	if a.State != NodeDispatched {
		t.Errorf("craft-a = %+v, want dispatched", a)
	}
	if b.State != NodeParked || b.Park != ParkOverBudget {
		t.Errorf("craft-b = %+v, want parked/over_budget", b)
	}
	if !runs[0].Control.Pause {
		t.Error("Control.Pause = false, want true")
	}
	if _, ok := r.Store.Get("p6/craft-a/r0"); !ok {
		t.Error("craft-a task not in store")
	}
	if _, ok := r.Store.Get("p6/craft-b/r0"); ok {
		t.Error("craft-b task should not be in store: combined with craft-a's in-flight fee it overshoots BudgetCap")
	}
}

// TestTickDoneNodeMovesInFlightFeeToSpentOnce extends the budget in-flight
// accounting: once a dispatched spending node completes, its fee must land
// in pr.Spent exactly once — not double-counted between the in-flight sum
// and Spent. A third node sized to fit only if craft-a's fee is counted
// exactly once proves it.
func TestTickDoneNodeMovesInFlightFeeToSpentOnce(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "p6b", BudgetCap: 50},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-a", Kind: craftbrain.KindCraft, ItemID: "a", Qty: 1, RecipeID: "make_a",
				StationID: "hub_a", FeeTotal: 30},
		}}})
	r.Tick() // dispatch craft-a; in-flight = 30, Spent = 0
	r.Store.HandleEvent("craftsman-2", controlEvent("task_done", "p6b/craft-a/r0"))
	r.Tick() // collect craft-a done; Spent must become 30, in-flight must drop to 0

	runs, _ := LoadAllRuns(r.StateDir)
	pr := runs[0]
	if pr.Spent != 30 {
		t.Fatalf("Spent = %d, want 30 (counted exactly once)", pr.Spent)
	}
	a := pr.NodeByID("craft-a")
	if a.State != NodeDone {
		t.Fatalf("craft-a = %+v, want done", a)
	}

	// Now a second node with FeeTotal 20 must be admitted: Spent(30) +
	// in-flight(0, craft-a is done not dispatched) + 20 = 50, exactly at cap.
	// If craft-a's fee were still being double-counted as in-flight, this
	// would wrongly park.
	pr.Nodes = append(pr.Nodes, &NodeRun{
		Node:  craftbrain.Node{ID: "craft-c", Kind: craftbrain.KindCraft, ItemID: "c", Qty: 1, RecipeID: "make_c", StationID: "hub_a", FeeTotal: 20},
		State: NodeWaiting,
		Agent: "craftsman-2",
	})
	if err := SaveRun(r.StateDir, pr); err != nil {
		t.Fatal(err)
	}
	r.runs["p6b"] = pr
	r.Tick()

	runs, _ = LoadAllRuns(r.StateDir)
	c := runs[0].NodeByID("craft-c")
	if c.State != NodeDispatched {
		t.Fatalf("craft-c = %+v, want dispatched (30 spent + 20 fits exactly at cap 50)", c)
	}
}

// TestControlRetryResetsRetryCount pins the fix for applyControl's
// RetryNodes handling: an operator retry of a parked(failed) node must grant
// a fresh MaxNodeRetries budget, not resume the exhausted one.
func TestControlRetryResetsRetryCount(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "p7", BudgetCap: 100},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "mine-1", Kind: craftbrain.KindMine, ItemID: "ore", Qty: 5},
		}}})
	r.Tick()
	for i := range MaxNodeRetries + 1 {
		id := taskIDFor("p7", "mine-1", i)
		r.Store.HandleEvent("craftsman-2", controlEvent("task_failed", id+": belt empty"))
		r.Tick()
	}
	runs, _ := LoadAllRuns(r.StateDir)
	n := runs[0].NodeByID("mine-1")
	if n.State != NodeParked || n.Park != ParkFailed || n.Retries != MaxNodeRetries+1 {
		t.Fatalf("mine-1 = %+v, want parked/failed with retries = %d", n, MaxNodeRetries+1)
	}

	// Operator retries the parked node directly on the runner's live state.
	r.runs["p7"].Control.RetryNodes = []string{"mine-1"}
	r.Tick()

	runs, _ = LoadAllRuns(r.StateDir)
	n = runs[0].NodeByID("mine-1")
	if n.State != NodeDispatched || n.Park != "" || n.Retries != 0 {
		t.Fatalf("mine-1 after retry = %+v, want dispatched/no-park/retries=0", n)
	}
}

func TestTickManagedHolderGetsHandoffBeforeDispatch(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "p4", BudgetCap: 100},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "haul-1", Kind: craftbrain.KindHaul, ItemID: "gas", Qty: 8,
				Holder: "marketbot_sol", FromBase: "sol_central", ToBase: "hub_a"},
		}}})
	r.Tick()
	if _, ok := r.Store.Get("p4/haul-1/r0"); ok {
		t.Fatal("haul dispatched before handoff completed")
	}
	recs, _ := r.Handoff.List()
	if len(recs) != 1 || recs[0].Holder != "marketbot_sol" || recs[0].Status != handoff.StatusPending {
		t.Fatalf("handoff = %+v", recs)
	}
	// Marketbot completes the gift; next tick dispatches the courier leg.
	_, _ = r.Handoff.Transition(recs[0].ID, handoff.StatusPending, handoff.StatusDone,
		func(rec *handoff.Record) { rec.MovedQty = 8 })
	r.Tick()
	if _, ok := r.Store.Get("p4/haul-1/r0"); !ok {
		t.Fatal("haul not dispatched after handoff done")
	}
}

func TestTickRetriesThenParksFailed(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "p5", BudgetCap: 100},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "mine-1", Kind: craftbrain.KindMine, ItemID: "ore", Qty: 5},
		}}})
	r.Tick()
	for i := range MaxNodeRetries + 1 {
		id := taskIDFor("p5", "mine-1", i)
		r.Store.HandleEvent("craftsman-2", controlEvent("task_failed", id+": belt empty"))
		r.Tick()
	}
	runs, _ := LoadAllRuns(r.StateDir)
	n := runs[0].NodeByID("mine-1")
	if n.State != NodeParked || n.Park != ParkFailed || n.Retries != MaxNodeRetries+1 {
		t.Fatalf("mine-1 = %+v", n)
	}
}
