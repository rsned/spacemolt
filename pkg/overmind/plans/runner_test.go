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

// TestTickDispatchesCraftNodeParams pins the craft_node task's Params shape
// (nodeTask, pkg/overmind/plans/params.go): RECIPE/NUM_OUTPUTS/STATION/
// FACILITY plus EST_FEE — the node's FeeTotal, formatted as a plain decimal
// string — which the craft_node worker verb's 2x-replan budget gate reads
// (its own FeeTotal is not otherwise visible to the worker-side script).
// FacilityID empty means hand-craft, so FACILITY must come through as the
// literal "hand" sentinel.
func TestTickDispatchesCraftNodeParams(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "p1", BudgetCap: 1000},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 5,
				RecipeID: "make_widget", StationID: "hub_a", FeeTotal: 40},
		}}})
	r.Tick()
	task, ok := r.Store.Get("p1/craft-1/r0")
	if !ok {
		t.Fatalf("task not dispatched")
	}
	if task.Script != "craft_node" || task.RoleRequired != "craftsman" {
		t.Fatalf("task = %+v", task)
	}
	want := map[string]string{
		"RECIPE":      "make_widget",
		"NUM_OUTPUTS": "5",
		"STATION":     "hub_a",
		"FACILITY":    "hand",
		"EST_FEE":     "40",
	}
	for k, v := range want {
		if got := task.Params[k]; got != v {
			t.Errorf("Params[%q] = %q, want %q", k, got, v)
		}
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

	// Operator retries the parked node. Since C1, the Runner adopts Control
	// from disk each tick, so the retry must be written through the disk seam
	// (as the CLI does) — a direct r.runs mutation would be clobbered. This
	// case is now largely subsumed by TestTickAdoptsDiskRetryBetweenTicks; it
	// stays for its retry-count-reset assertion.
	cliMutate(t, r, "p7", func(pr *PlanRun) { pr.Control.RetryNodes = []string{"mine-1"} })
	r.Tick()

	runs, _ = LoadAllRuns(r.StateDir)
	n = runs[0].NodeByID("mine-1")
	if n.State != NodeDispatched || n.Park != "" || n.Retries != 0 {
		t.Fatalf("mine-1 after retry = %+v, want dispatched/no-park/retries=0", n)
	}
}

// cliMutate mirrors the play_as plan_* mutators' LoadRun -> mutate Control ->
// SaveRun contract: it loads the on-disk state file into a FRESH PlanRun (a
// separate handle from the Runner's own r.runs[planID]), mutates only its
// Control, and saves it back under the flock — exactly what the operator CLI
// does between Runner ticks. This is the real seam C1 must survive: the Runner
// must adopt this disk-written Control on its next tick instead of clobbering
// it with its own stale in-memory copy.
func cliMutate(t *testing.T, r *Runner, planID string, f func(*PlanRun)) {
	t.Helper()
	pr, err := LoadRun(statePath(r.StateDir, planID))
	if err != nil {
		t.Fatalf("cliMutate load %s: %v", planID, err)
	}
	f(pr)
	if err := SaveRun(r.StateDir, pr); err != nil {
		t.Fatalf("cliMutate save %s: %v", planID, err)
	}
}

// TestTickAdoptsDiskCancelBetweenTicks (C1 case a): an operator plan_cancel
// written to disk between two Runner ticks must stick — the Runner's end-of-
// tick SaveRun must not overwrite it with its own stale in-memory Control.
func TestTickAdoptsDiskCancelBetweenTicks(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "pc", BudgetCap: 100},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "mine-1", Kind: craftbrain.KindMine, ItemID: "ore", Qty: 5},
		}}})
	r.Tick()
	cliMutate(t, r, "pc", func(pr *PlanRun) { pr.Control.Cancel = true })
	r.Tick()

	runs, _ := LoadAllRuns(r.StateDir)
	if !runs[0].Control.Cancel {
		t.Fatal("Control.Cancel was clobbered by the Runner's stale in-memory Control")
	}
	if runs[0].Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", runs[0].Status)
	}
}

// TestTickAdoptsDiskResumeOfBudgetPausedPlan (C1 case b): a plan that
// budget-parked and paused (Pause=true saved to disk by the Runner) is
// resumed by the operator via a disk write (Pause=false, cap raised, node
// retried). The Runner must adopt that disk Control on its next tick so Pause
// stays false — not revert to the paused state its in-memory copy still holds.
func TestTickAdoptsDiskResumeOfBudgetPausedPlan(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "pr", BudgetCap: 10},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "w", Qty: 1, RecipeID: "make_w",
				StationID: "hub_a", FeeTotal: 40},
		}}})
	r.Tick() // parks craft-1 over_budget, pauses the plan
	runs, _ := LoadAllRuns(r.StateDir)
	if runs[0].Status != "paused" {
		t.Fatalf("precondition: status = %q, want paused", runs[0].Status)
	}

	// Operator resumes with a raised cap and retries the parked node, via the
	// disk handle (as plan_resume + plan_retry do).
	cliMutate(t, r, "pr", func(pr *PlanRun) {
		pr.Control.Pause = false
		pr.Control.RaiseCap = 1000
		pr.Control.RetryNodes = []string{"craft-1"}
	})
	r.Tick()

	runs, _ = LoadAllRuns(r.StateDir)
	if runs[0].Control.Pause {
		t.Fatal("resume reverted: Control.Pause re-set true by the Runner's stale in-memory copy")
	}
	if runs[0].Status == "paused" {
		t.Errorf("status = %q, want the plan un-paused", runs[0].Status)
	}
	if n := runs[0].NodeByID("craft-1"); n.State != NodeDispatched {
		t.Errorf("craft-1 = %+v, want dispatched after resume+raise-cap+retry", n)
	}
}

// TestTickAdoptsDiskRetryBetweenTicks (C1 case c): a plan_retry written to
// disk between ticks must be adopted and consumed — the parked node resets and
// the RetryNodes entry is cleared.
func TestTickAdoptsDiskRetryBetweenTicks(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "prt", BudgetCap: 100},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "mine-1", Kind: craftbrain.KindMine, ItemID: "ore", Qty: 5},
		}}})
	r.Tick()
	for i := range MaxNodeRetries + 1 {
		id := taskIDFor("prt", "mine-1", i)
		r.Store.HandleEvent("craftsman-2", controlEvent("task_failed", id+": belt empty"))
		r.Tick()
	}
	runs, _ := LoadAllRuns(r.StateDir)
	if n := runs[0].NodeByID("mine-1"); n.State != NodeParked {
		t.Fatalf("precondition: mine-1 = %+v, want parked", n)
	}

	// Operator retries the parked node via the disk handle (as plan_retry does).
	cliMutate(t, r, "prt", func(pr *PlanRun) {
		pr.Control.RetryNodes = []string{"mine-1"}
	})
	r.Tick()

	runs, _ = LoadAllRuns(r.StateDir)
	n := runs[0].NodeByID("mine-1")
	if n.State != NodeDispatched || n.Park != "" || n.Retries != 0 {
		t.Fatalf("mine-1 after disk retry = %+v, want dispatched/no-park/retries=0", n)
	}
	if len(runs[0].Control.RetryNodes) != 0 {
		t.Errorf("RetryNodes = %v, want consumed", runs[0].Control.RetryNodes)
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

// TestTickBuyNodeLostTaskParksNeedsOperator (I1): a dispatched buy node whose
// task vanishes from the store (e.g. lost across an overmind restart) must NOT
// auto-retry — BuyDirected's recompute only counts cargo, so a
// completed-but-unrecorded buy would re-purchase everything. The defensive
// missing-task path parks buy nodes needs-operator instead. Genuine
// task_failed events still retry (covered by TestTickRetriesThenParksFailed).
func TestTickBuyNodeLostTaskParksNeedsOperator(t *testing.T) {
	r := newRunner(t)
	dropPlan(t, r, QueueFile{Manifest: Manifest{PlanID: "pb", BudgetCap: 1000},
		Plan: craftbrain.Plan{Nodes: []craftbrain.Node{
			{ID: "buy-1", Kind: craftbrain.KindBuy, ItemID: "ore", Qty: 10,
				StationID: "seller_x", FeeTotal: 30},
		}}})
	r.Tick() // dispatch buy-1
	if _, ok := r.Store.Get("pb/buy-1/r0"); !ok {
		t.Fatal("precondition: buy-1 not dispatched")
	}

	// The task disappears from the store (overmind restart lost it) without a
	// task_failed event ever arriving.
	r.Store.Remove("pb/buy-1/r0")
	r.Tick()

	runs, _ := LoadAllRuns(r.StateDir)
	n := runs[0].NodeByID("buy-1")
	if n.State != NodeParked || n.Park != ParkNeedsOperator {
		t.Fatalf("buy-1 = %+v, want parked/needs_operator (a lost buy must not auto-retry)", n)
	}
	if _, ok := r.Store.Get("pb/buy-1/r1"); ok {
		t.Error("buy-1 was re-dispatched — a lost buy must not re-purchase on retry")
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
