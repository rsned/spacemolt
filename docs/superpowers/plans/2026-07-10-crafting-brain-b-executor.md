# Crafting Brain B — Executor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Execute a reviewed `pkg/craftbrain` plan across a new craftsman fleet: dependency-ordered dispatch through the overmind, budget enforcement, marketbot stock handoffs, and a live mermaid dashboard.

**Architecture:** A `pkg/overmind/plans` runner inside the (existing) overmind binary polls a queue dir for dispatched plans, pins craft nodes to agents, releases dependency-ready nodes into the existing `tasks.Store` as pinned tasks, and collects results from task events. Four new worker verbs (`deliver`, `buy_directed`, `craft_node` behavior, `mine_qty`) execute nodes; a flock-guarded handoff queue moves holder-owned stock via `send_gift`; `cmd/tools/craft-dashboard` renders plan state files.

**Tech Stack:** Go 1.24+, SQLite (`pkg/knowledge`), existing overmind control plane (`pkg/overmind/{control,supervisor,tasks}`), mermaid.js (vendored) for the dashboard.

**Spec:** `docs/superpowers/specs/2026-07-10-crafting-brain-b-executor-design.md` — read it first, especially "Recipient pinning and synthetic transport" and "Verify live before building".

## Global Constraints

- Go 1.24+: use range-over-int, `slices`/`maps`/`cmp` packages; benchmarks use `b.Loop()`.
- All new code passes `golangci-lint run <pkg>` with zero new findings; run it after every task.
- Sleeps ONLY via constants in `pkg/game/constants.go` (`game.SleepQuick`, `game.SleepShort`, …). Never a literal duration. If none fits, stop and ask the operator.
- Compiled binaries go in `bin/`, never the repo root.
- New GameClient interface methods break mocks in `pkg/agent` and `pkg/skills` — after any interface change run full `go test ./...`, not just your package.
- Commit format: `type(scope): message` ending with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Check `.gitignore` before adding files under `data/` (broad ignore patterns; add `!` negations as needed).
- The pre-commit hook builds, tests (race), and lints staged packages — a failing hook means fix, not bypass.
- craftbrain quantity convention: **qty is always output units, never runs.** Verbs compute runs internally.

---

### Task 0: Live mechanics verification (OPERATOR + LEAD SESSION — not a subagent task)

**Files:**
- Create: `docs/superpowers/specs/2026-07-10-executor-b-live-mechanics.md`

**Interfaces:**
- Produces: a findings doc that Tasks 7, 8, and 10 MUST read before implementing. Task 8's `CraftDryRunResult` fields and Task 10's gift call shape are finalized from it.

This task is performed interactively against the live game (play_as as craftsman-2; NEVER craftsman-1 — that is the operator's own session). Record every raw JSON response in the findings doc.

- [ ] **Step 1: send_gift items.** While docked, gift a cheap item to another owned agent (e.g. craftsman-3): run play_as `send_gift` with an item payload; record exact payload keys accepted (target naming: username vs agent id), whether goods must be in cargo, where they land for the recipient (expected: recipient storage at the sender's station), any qty caps, and whether recipient acceptance is needed.
- [ ] **Step 2: `send_gift --source=storage`.** Try the storage-sourced variant; record whether the server patch has landed. If yes, note payload shape.
- [ ] **Step 3: craft dry_run.** Run a `craft` with `dry_run: true` for (a) a hand recipe, (b) a facility recipe with explicit `facility_id` on a foreign-faction public facility. Record the full response JSON: cost fields, time fields, station/facility echo, error shapes when the facility is invalid.
- [ ] **Step 4: craft `deliver_to`.** Determine what `deliver_to` accepts (station? player?) and where output lands. Record whether it can replace synthetic transport nodes (spec amendment note).
- [ ] **Step 5: Write findings doc + commit.** Structure: one section per mechanic, raw JSON in fenced blocks, a "Decisions for implementation" list at top (gift payload shape, dry-run response struct fields, whether --source=storage is live, whether deliver_to changes Task 5's synthetic-transport logic).

```bash
git add docs/superpowers/specs/2026-07-10-executor-b-live-mechanics.md
git commit -m "docs(craftbrain): executor B live mechanics findings"
```

---

### Task 1: pkg/handoff — flock-guarded handoff queue

**Files:**
- Create: `pkg/handoff/queue.go`
- Test: `pkg/handoff/queue_test.go`

**Interfaces:**
- Consumes: nothing (mirrors `pkg/rescue/queue.go` — read it first, `pkg/rescue/queue.go:58-178`, and copy its withLock mechanics exactly: LOCK_EX flock on a stable `.lock` sidecar, read-mutate-atomic-rename).
- Produces:

```go
package handoff

type Status string

const (
    StatusPending Status = "pending"
    StatusDone    Status = "done"
    StatusFailed  Status = "failed"
)

// Record is one "holder gifts stock to recipient" instruction.
type Record struct {
    ID          string `json:"id"` // "<plan-id>/<node-id>" — unique per node
    Holder      string `json:"holder"`       // agent_id that owns the stock
    Station     string `json:"station"`      // base_id where the stock sits
    ItemID      string `json:"item_id"`
    Qty         int    `json:"qty"`
    Recipient   string `json:"recipient"`    // agent_id to send_gift to
    Status      Status `json:"status"`
    MovedQty    int    `json:"moved_qty"`    // actually transferred (may be < Qty)
    Error       string `json:"error,omitempty"`
    RequestedAt string `json:"requested_at"`
    UpdatedAt   string `json:"updated_at"`
}

func NewQueue(path string) *Queue
func (q *Queue) List() ([]Record, error)
func (q *Queue) Enqueue(rec Record) (bool, error) // false if ID already present (idempotent)
func (q *Queue) Transition(id string, from, to Status, mutate func(*Record)) (bool, error)
func (q *Queue) Remove(id string) (*Record, error)
```

- [ ] **Step 1: Write failing tests** — `pkg/handoff/queue_test.go`:

```go
package handoff

import (
    "path/filepath"
    "testing"
)

func TestEnqueueListRoundTrip(t *testing.T) {
    q := NewQueue(filepath.Join(t.TempDir(), "handoff-queue.json"))
    ok, err := q.Enqueue(Record{ID: "p1/haul-2", Holder: "marketbot_sol", Station: "sol_central",
        ItemID: "steel_plate", Qty: 40, Recipient: "craftsman-2", Status: StatusPending})
    if err != nil || !ok {
        t.Fatalf("enqueue = %v, %v", ok, err)
    }
    // Same ID again is a no-op, not a duplicate.
    ok, err = q.Enqueue(Record{ID: "p1/haul-2", Status: StatusPending})
    if err != nil || ok {
        t.Fatalf("re-enqueue = %v, %v; want false, nil", ok, err)
    }
    recs, err := q.List()
    if err != nil || len(recs) != 1 || recs[0].ItemID != "steel_plate" {
        t.Fatalf("list = %+v, %v", recs, err)
    }
}

func TestTransitionCompareAndSet(t *testing.T) {
    q := NewQueue(filepath.Join(t.TempDir(), "handoff-queue.json"))
    if _, err := q.Enqueue(Record{ID: "p1/n1", Status: StatusPending}); err != nil {
        t.Fatal(err)
    }
    ok, err := q.Transition("p1/n1", StatusPending, StatusDone, func(r *Record) { r.MovedQty = 40 })
    if err != nil || !ok {
        t.Fatalf("transition = %v, %v", ok, err)
    }
    // From-state no longer matches: CAS must refuse.
    ok, err = q.Transition("p1/n1", StatusPending, StatusFailed, nil)
    if err != nil || ok {
        t.Fatalf("stale transition = %v, %v; want false, nil", ok, err)
    }
    recs, _ := q.List()
    if recs[0].Status != StatusDone || recs[0].MovedQty != 40 {
        t.Fatalf("record = %+v", recs[0])
    }
}

func TestMissingFileIsEmptyQueue(t *testing.T) {
    q := NewQueue(filepath.Join(t.TempDir(), "nope.json"))
    recs, err := q.List()
    if err != nil || len(recs) != 0 {
        t.Fatalf("list = %v, %v; want empty, nil", recs, err)
    }
}
```

- [ ] **Step 2: Run tests, verify FAIL** — `go test ./pkg/handoff/ -v` → compile error (package empty). Expected.
- [ ] **Step 3: Implement `pkg/handoff/queue.go`.** Copy `pkg/rescue/queue.go`'s `withLock` (flock LOCK_EX on `path+".lock"`, read file — missing = empty slice, corrupt = error — mutate, write via temp file + `os.Rename`) with `[]Record` and key on `ID` instead of agent_id. `Enqueue` appends only if no record has the same ID; `Transition` finds by ID, checks `from`, applies mutate, sets `Status=to` and `UpdatedAt` (RFC3339 UTC via injectable-free `time.Now().UTC()` — this is a data file, not test-sensitive logic); `Remove` deletes by ID returning the removed record or nil.
- [ ] **Step 4: Run tests, verify PASS** — `go test ./pkg/handoff/ -v` → all PASS.
- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/handoff/
git add pkg/handoff/
git commit -m "feat(handoff): flock-guarded stock-handoff queue"
```

---

### Task 2: tasks.Store runtime Add / Remove / Get

**Files:**
- Modify: `pkg/overmind/tasks/store.go`
- Test: `pkg/overmind/tasks/store_runtime_test.go`

**Interfaces:**
- Consumes: existing `tasks.Task`, `tasks.Store` (`pkg/overmind/tasks/task.go:25`, `store.go:19`).
- Produces:

```go
func (s *Store) Add(t Task) error          // validates like LoadTasks (non-empty ID/Script/RoleRequired, no ':'), rejects duplicate ID; Status forced to StatusPending
func (s *Store) Remove(id string) bool     // deletes by ID; true if found
func (s *Store) Get(id string) (Task, bool)
```

Note: task IDs from the plan runner use `/` as separator (`<plan-id>/<node-id>`), which is legal — only `:` is reserved (the worker's `OnTaskResult` failure Detail is `id: err`, parsed at the first `:` in `HandleEvent`, `store.go:120`).

- [ ] **Step 1: Write failing tests** — `pkg/overmind/tasks/store_runtime_test.go`:

```go
package tasks

import (
    "io"
    "log"
    "testing"
)

func newTestStore() *Store {
    return NewStore(nil, log.New(io.Discard, "", 0))
}

func TestAddValidatesAndRejectsDuplicates(t *testing.T) {
    s := newTestStore()
    good := Task{ID: "p1/craft-1", Script: "craft_node", RoleRequired: "craftsman"}
    if err := s.Add(good); err != nil {
        t.Fatalf("add: %v", err)
    }
    if err := s.Add(good); err == nil {
        t.Fatal("duplicate id accepted")
    }
    for _, bad := range []Task{
        {ID: "", Script: "x", RoleRequired: "r"},
        {ID: "a:b", Script: "x", RoleRequired: "r"},
        {ID: "b", Script: "", RoleRequired: "r"},
        {ID: "c", Script: "x", RoleRequired: ""},
    } {
        if err := s.Add(bad); err == nil {
            t.Errorf("invalid task accepted: %+v", bad)
        }
    }
    got, ok := s.Get("p1/craft-1")
    if !ok || got.Status != StatusPending {
        t.Fatalf("get = %+v, %v; want pending task", got, ok)
    }
}

func TestRemove(t *testing.T) {
    s := newTestStore()
    _ = s.Add(Task{ID: "p1/n1", Script: "deliver_node", RoleRequired: "craftsman"})
    if !s.Remove("p1/n1") {
        t.Fatal("remove: not found")
    }
    if s.Remove("p1/n1") {
        t.Fatal("second remove reported found")
    }
    if _, ok := s.Get("p1/n1"); ok {
        t.Fatal("removed task still present")
    }
}
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./pkg/overmind/tasks/ -run 'TestAdd|TestRemove' -v` → undefined methods.
- [ ] **Step 3: Implement** in `store.go` (all under `s.mu`):

```go
// Add inserts a runtime task (plan-runner injection path). Same validation as
// LoadTasks; Status is forced to pending regardless of input.
func (s *Store) Add(t Task) error {
    switch {
    case t.ID == "":
        return fmt.Errorf("tasks: empty id")
    case strings.Contains(t.ID, ":"):
        return fmt.Errorf("tasks: id %q must not contain ':'", t.ID)
    case t.Script == "":
        return fmt.Errorf("tasks: task %q has empty script", t.ID)
    case t.RoleRequired == "":
        return fmt.Errorf("tasks: task %q has empty role_required", t.ID)
    }
    s.mu.Lock()
    defer s.mu.Unlock()
    for i := range s.tasks {
        if s.tasks[i].ID == t.ID {
            return fmt.Errorf("tasks: duplicate id %q", t.ID)
        }
    }
    t.Status = StatusPending
    t.AssignedTo = ""
    s.tasks = append(s.tasks, t)
    return nil
}
```

`Remove` filters the slice by ID; `Get` scans and returns a copy. Add `"fmt"` to imports.

- [ ] **Step 4: Run, verify PASS** — `go test ./pkg/overmind/tasks/ -v` → all PASS (including pre-existing tests).
- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/overmind/tasks/
git add pkg/overmind/tasks/
git commit -m "feat(overmind): runtime Add/Remove/Get on the task store"
```

---

### Task 3: pkg/overmind/plans — plan-run model, accept transform, persistence

**Files:**
- Create: `pkg/overmind/plans/run.go` (types + NewRun accept transform)
- Create: `pkg/overmind/plans/persist.go` (flock load/save)
- Test: `pkg/overmind/plans/run_test.go`, `pkg/overmind/plans/persist_test.go`

**Interfaces:**
- Consumes: `craftbrain.Plan`, `craftbrain.Node`, `craftbrain.Kind*`, `craftbrain.StatusBlocked` (`pkg/craftbrain/plan.go`).
- Produces:

```go
package plans

type NodeState string

const (
    NodeWaiting    NodeState = "waiting"    // deps not yet done
    NodeDispatched NodeState = "dispatched" // task in the store (assigned or running)
    NodeDone       NodeState = "done"
    NodeParked     NodeState = "parked"
)

type ParkReason string

const (
    ParkCycle         ParkReason = "cycle"
    ParkBlocked       ParkReason = "blocked"
    ParkNeedsOperator ParkReason = "needs_operator"
    ParkOverBudget    ParkReason = "over_budget"
    ParkFailed        ParkReason = "failed"
    ParkReplan        ParkReason = "replan"
)

type RosterAgent struct {
    AgentID string `yaml:"agent_id" json:"agent_id"`
    Station string `yaml:"station" json:"station"`
}

// Manifest is the dispatch-time envelope around a craftbrain plan.
type Manifest struct {
    PlanID       string   `json:"plan_id"`
    BudgetCap    int      `json:"budget_cap"`
    MineItems    []string `json:"mine_items,omitempty"` // leaves tagged mine at dispatch
    Assembly     string   `json:"assembly"`             // any_docked_station resolved to this base
    DispatchedAt string   `json:"dispatched_at"`
}

// QueueFile is the JSON dropped into the craft-queue dir by play_as dispatch.
type QueueFile struct {
    Manifest Manifest        `json:"manifest"`
    Plan     craftbrain.Plan `json:"plan"`
}

type NodeRun struct {
    Node       craftbrain.Node `json:"node"`
    State      NodeState       `json:"state"`
    Park       ParkReason      `json:"park,omitempty"`
    ParkDetail string          `json:"park_detail,omitempty"`
    Agent      string          `json:"agent,omitempty"`     // pin (crafts + synthetic xfers) or assignee
    Recipient  string          `json:"recipient,omitempty"` // consumer's pinned agent for feeder nodes
    DoneQty    int             `json:"done_qty"`
    Retries    int             `json:"retries"`
    Synthetic  bool            `json:"synthetic,omitempty"`
    SpentActual int            `json:"spent_actual"`        // fees/buys actually paid
}

type Control struct {
    Pause      bool     `json:"pause,omitempty"`
    Cancel     bool     `json:"cancel,omitempty"`
    RaiseCap   int      `json:"raise_cap,omitempty"`   // new absolute cap; 0 = unchanged
    RetryNodes []string `json:"retry_nodes,omitempty"` // node ids to reset from parked(failed)
}

type PlanRun struct {
    Manifest Manifest   `json:"manifest"`
    Nodes    []*NodeRun `json:"nodes"` // plan order + synthetic xfers appended
    Spent    int        `json:"spent"`
    Status   string     `json:"status"` // running | paused | done | partial | cancelled
    Control  Control    `json:"control"`
    Diagnostics []string `json:"diagnostics,omitempty"`
}

func (pr *PlanRun) NodeByID(id string) *NodeRun

// NewRun applies the accept transform: park cycle/blocked nodes, pin craft
// nodes round-robin over roster, set Recipient on feeder nodes, insert
// synthetic xfer nodes for cross-station craft->craft edges.
func NewRun(qf QueueFile, roster []RosterAgent) *PlanRun

func SaveRun(dir string, pr *PlanRun) error     // <dir>/<plan-id>.state.json, flock + tmp-rename
func LoadRun(path string) (*PlanRun, error)
func LoadAllRuns(dir string) ([]*PlanRun, error) // glob *.state.json, skip corrupt with error log
```

**NewRun rules (the heart of this task):**
1. Copy nodes in plan order into NodeRuns, `State=NodeWaiting`, `DoneQty=0`.
2. **Cycle park:** run Kahn's algorithm over `DependsOn` edges; any node not in the topological output is on (or downstream of) a cycle → `NodeParked/ParkCycle`, detail listing the cycle members. Never trust A2's diagnostics alone — recompute.
3. **Blocked park:** `Node.Kind == craftbrain.KindBlocked` OR `Node.Status == craftbrain.StatusBlocked` → `NodeParked/ParkBlocked`, detail = `Node.Reason`.
4. **Pin crafts:** iterate craft nodes in plan order, assign `Agent` round-robin over `roster` (stable: `roster[i % len]`). Empty roster → every craft parks `ParkNeedsOperator` with detail "no craft roster".
5. **Recipients:** for every edge consumer→producer (consumer's `DependsOn` contains producer id): if consumer is a pinned craft node, producer's `Recipient = consumer.Agent`; if producer is a haul/buy/mine node its `Node.ToBase` (haul) is left as-is (already the consumer's station from A2/dispatch resolution). A producer feeding multiple consumers keeps the FIRST consumer's recipient and appends a diagnostic (`"node X feeds multiple crafts; recipient pinned to first"`) — v1 accepts the simplification.
6. **Synthetic xfers:** for each craft→craft dependency edge where producer.StationID != consumer.StationID, insert `NodeRun{Synthetic: true}` with `Node = craftbrain.Node{ID: "xfer-<n>", Kind: KindHaul, ItemID: producer.ItemID, Qty: producer's contribution (v1: producer.Qty), FromBase: producer.StationID, ToBase: consumer.StationID, DependsOn: []string{producer.ID}}`, `Agent = producer.Agent` (goods are in the producer agent's storage), `Recipient = consumer.Agent`; rewrite the consumer's `DependsOn` to replace producer.ID with the xfer id.
7. `Status = "running"`; carry `qf.Plan.Diagnostics` into `PlanRun.Diagnostics`.

- [ ] **Step 1: Write failing tests** — `pkg/overmind/plans/run_test.go`:

```go
package plans

import (
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
```

`pkg/overmind/plans/persist_test.go`:

```go
package plans

import (
    "os"
    "path/filepath"
    "testing"
)

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
    runs, err := LoadAllRuns(dir)
    if err != nil {
        t.Fatal(err)
    }
    if len(runs) != 1 || runs[0].Manifest.PlanID != "good" {
        t.Fatalf("runs = %+v", runs)
    }
}
```

(with a tiny helper in the test file: `func craftbrainNode(id string) craftbrain.Node { return craftbrain.Node{ID: id, Kind: craftbrain.KindMine, ItemID: "x", Qty: 1} }`)

- [ ] **Step 2: Run, verify FAIL** — `go test ./pkg/overmind/plans/ -v` → package does not exist. Expected.
- [ ] **Step 3: Implement `run.go` + `persist.go`.** NewRun per the numbered rules above (Kahn over DependsOn: build in-degree from edges consumer→producer reversed; standard queue loop; unvisited = cycle-parked). Persistence mirrors `pkg/rescue` withLock on `<dir>/<plan-id>.state.json.lock`, JSON-indent the PlanRun, temp+rename. `LoadAllRuns` uses `filepath.Glob(dir + "/*.state.json")`, logs and skips unparseable files (return them in an `errs` diagnostic? No — log to stderr via a `logger *log.Logger` param is overkill; skip silently is FORBIDDEN — collect a `[]error` second return and let the caller log). Signature adjustment: `func LoadAllRuns(dir string) ([]*PlanRun, []error)`.
- [ ] **Step 4: Run, verify PASS** — `go test ./pkg/overmind/plans/ -v`.
- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/overmind/plans/
git add pkg/overmind/plans/
git commit -m "feat(plans): plan-run model, accept transform (pin/park/xfer), flock persistence"
```

---

### Task 4: plans scheduler — readiness, progress rollups, completion

**Files:**
- Create: `pkg/overmind/plans/schedule.go`
- Test: `pkg/overmind/plans/schedule_test.go`

**Interfaces:**
- Consumes: Task 3's types.
- Produces:

```go
// ReadyNodes returns nodes whose deps are all done, state waiting, plan not
// paused/cancelled. Parked and synthetic-parked nodes never appear.
func (pr *PlanRun) ReadyNodes() []*NodeRun

// ItemProgress sums done vs total qty per item over mine/buy/haul/craft nodes.
type Progress struct{ Done, Total int }
func (pr *PlanRun) ItemProgress() map[string]Progress

// Recompute derives PlanRun.Status: "cancelled" if Control.Cancel;
// "paused" if Control.Pause; "done" when every node is done;
// "partial" when nothing is waiting/dispatched but parked nodes remain;
// otherwise "running".
func (pr *PlanRun) Recompute()
```

- [ ] **Step 1: Write failing tests** — `schedule_test.go`:

```go
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
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./pkg/overmind/plans/ -run 'TestReady|TestRecompute|TestItemProgress' -v`.
- [ ] **Step 3: Implement `schedule.go`.** ReadyNodes: build `done` set; a node is ready when `State == NodeWaiting`, not parked, every dep in done set, and `pr.Status == "running"` (call Recompute first inside ReadyNodes to honor fresh Control flags). ItemProgress: skip `KindBlocked` and synthetic nodes are INCLUDED (they are real work). Recompute per the doc comment; "done" requires zero waiting/dispatched AND zero parked.
- [ ] **Step 4: Run, verify PASS.** Full package: `go test ./pkg/overmind/plans/ -v`.
- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/overmind/plans/
git add pkg/overmind/plans/
git commit -m "feat(plans): readiness, item progress rollups, plan status recompute"
```

---

### Task 5: plans Runner — intake, dispatch bridge, retries, budget, handoff gating

**Files:**
- Create: `pkg/overmind/plans/runner.go`, `pkg/overmind/plans/params.go`
- Test: `pkg/overmind/plans/runner_test.go`

**Interfaces:**
- Consumes: Tasks 1-4 (`handoff.Queue`, `tasks.Store.Add/Remove/Get/Snapshot`, `PlanRun`), `supervisor.WorkerInfo` (only `.AgentID`; keep the dependency thin by accepting `[]string` of healthy agent ids instead).
- Produces:

```go
// Runner drives all plan runs one Tick at a time. Construct once in cmd/overmind.
type Runner struct {
    QueueDir string
    StateDir string
    Store    *tasks.Store
    Handoff  *handoff.Queue
    Roster   []RosterAgent     // craft fleet (pin targets)
    Managed  map[string]string // holder agent_id -> its home station (marketbot roster)
    Logger   *log.Logger

    runs map[string]*PlanRun // loaded lazily; keyed by plan id
}

const MaxNodeRetries = 2

// Tick: 1) intake queue files -> NewRun -> save + delete queue file;
// 2) per run: apply Control (retry/raise-cap), sync node states from the task
// store, accumulate DoneQty/SpentActual from finished tasks, retry failed
// nodes (<= MaxNodeRetries), enqueue handoffs, budget-gate and dispatch ready
// nodes, Recompute, save.
func (r *Runner) Tick()

// nodeTask maps a NodeRun to a tasks.Task (script name + params). Exported for
// the runner test; params.go owns the mapping table.
func nodeTask(planID string, n *NodeRun) tasks.Task
```

**params.go mapping (single source of truth):**

| Node kind | Script (`data/scripts/<name>.smolt`) | Params |
|---|---|---|
| craft | `craft_node` | `RECIPE`=Node.RecipeID, `NUM_OUTPUTS`=Node.Qty, `STATION`=Node.StationID, `FACILITY`=Node.FacilityID or `hand` |
| haul (incl. synthetic) | `deliver_node` | `ITEM`, `QTY`, `FROM`=FromBase, `TO`=ToBase, `RECIPIENT`=n.Recipient or `self` |
| buy | `buy_node` | `ITEM`, `QTY`, `STATION`=Node.StationID, `MAX_UNIT_PRICE` (from Node context — see dispatch Task 13, which stamps it into Node.Reason? NO — add it properly: dispatch writes it into `Node.FeeTotal/Qty` when converting mine→buy; for A2 buy nodes use plan-estimate `FeeTotal/Qty`, else `0` meaning no ceiling), `RECIPIENT` |
| mine | `mine_node` | `ITEM`, `QTY`, `TO`=consumer station (from Recipient's craft node StationID; assembly fallback), `RECIPIENT` |

Task fields: `ID = planID + "/" + n.Node.ID` (retries: `+ "/r" + strconv.Itoa(n.Retries)` so re-adds never collide), `RoleRequired = "craftsman"`, `AgentID = n.Agent` (empty = any craftsman).

**Tick specifics:**
- **Intake:** `filepath.Glob(QueueDir + "/*.json")`; decode QueueFile; `NewRun(qf, r.Roster)`; `SaveRun`; `os.Remove` the queue file; log. Decode failure → rename to `<file>.rejected` + log (never delete silently, never retry-loop).
- **Handoff gating:** a haul node with `n.Node.Holder != ""` and `n.Node.Holder != n.Agent` whose holder is in `Managed` requires a handoff: on first readiness, `Enqueue(handoff.Record{ID: taskIDBase, Holder, Station: n.Node.FromBase, ItemID, Qty, Recipient: courier-or-recipient})` — **recipient of the gift is the node's executing courier when FromBase != ToBase (courier carries), else the node's Recipient directly (same-station collapse: no courier flight needed)**. The node is NOT dispatched to the store until its handoff record reads `done`; same-station-collapse nodes are marked NodeDone directly when the handoff completes, with `DoneQty = record.MovedQty`. Holder not in `Managed` and not the executing agent → `NodeParked/ParkNeedsOperator` with detail `"holder <h> at <base>: move <qty> <item> to <recipient>"`.
- **Task sync:** for every non-parked node with `State == NodeDispatched`, `Store.Get(taskID)`: missing = treat as failed (defensive); `StatusDone` → `NodeDone`, `DoneQty = n.Node.Qty` (progress events refine this later — v1 sets full qty on done), `Spent += n.Node.FeeTotal`, `n.SpentActual = n.Node.FeeTotal`, `Store.Remove(taskID)`; `StatusFailed` → retry or park (`Retries++`; `> MaxNodeRetries` → `NodeParked/ParkFailed`), `Store.Remove(taskID)`.
- **Budget gate:** before dispatching a ready node with spend (`FeeTotal > 0` or kind buy): projected = `Spent + n.Node.FeeTotal`; `> BudgetCap` (when cap > 0) → `NodeParked/ParkOverBudget` + `Control.Pause = true` (plan pauses; resume via CLI).
- **Control apply:** `RaiseCap > 0` → `Manifest.BudgetCap = RaiseCap`, clear; `RetryNodes` → for each parked(failed|over_budget|needs_operator) node: back to `NodeWaiting`, clear Park fields, clear list.
- **Cancel:** `Control.Cancel` → for each dispatched node leave it (verb finishes; sync still collects), dispatch nothing new, `Recompute` yields "cancelled".

- [ ] **Step 1: Write failing tests** — `runner_test.go` (fake-free: real Store with io.Discard logger, temp dirs, real handoff queue):

```go
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
    "github.com/rsned/spacemolt/pkg/overmind/tasks"
)

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
```

Test helpers in the same file: `func controlEvent(kind, detail string) control.Event { return control.Event{Kind: kind, Detail: detail} }` (import `pkg/overmind/control`) and export a tiny `func taskIDFor(planID, nodeID string, retry int) string` from `params.go`.

Note for the implementer: `HandleEvent` needs the task to exist in the store with `Status assigned/running` to transition — check its actual behavior (`store.go:120`): it matches by ID regardless of status, so simulating without a real Assign is fine.

- [ ] **Step 2: Run, verify FAIL** — `go test ./pkg/overmind/plans/ -run TestTick -v`.
- [ ] **Step 3: Implement `runner.go` + `params.go`** per the specifics above. Keep Tick single-threaded (called from the overmind main select loop; no internal goroutines). Load runs lazily on first Tick (`LoadAllRuns`, log errors).
- [ ] **Step 4: Run, verify PASS** — full `go test ./pkg/overmind/plans/ -v`.
- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/overmind/plans/ ./pkg/handoff/ ./pkg/overmind/tasks/
git add pkg/overmind/plans/
git commit -m "feat(plans): runner tick — intake, dispatch bridge, retries, budget, handoff gating"
```

---

### Task 6: deliver verb

**Files:**
- Create: `pkg/worker/deliver.go`
- Modify: `pkg/worker/dispatch.go` (supported map + case)
- Test: `pkg/worker/deliver_test.go`

**Interfaces:**
- Consumes: `game.GameClient` (`WithdrawItems`, `DepositItems`, `SendGift`, `Dock`, `GetState`), `Autopilot(ctx, AutopilotDeps{...}, targetSystem, targetPOI)` (`pkg/worker/autopilot.go:100`), `*knowledge.SQLiteKB` via type-assert for base→system/poi resolution.
- Produces:

```go
// Deliver moves ITEM xQTY from the worker's own storage at FROM to RECIPIENT
// at TO. recipient "self" (or the worker's own agent id) means deposit into own
// storage at TO instead of gifting. Returns nil with a short-delivery note in
// the progress output when the source held less than qty (moved what existed).
func (d *WorkerDispatch) Deliver(ctx context.Context, itemID string, qty int, from, to, recipient string) error

// resolveBase returns (systemID, poiID) for a base id, mirroring the two-step
// lookup in cmd/tools/play_as/source_sql.go SystemOf (bases JOIN pois, then
// pois fallback). Requires *knowledge.SQLiteKB; other KB impls return an error.
func resolveBase(ctx context.Context, kb knowledge.Base, baseID string) (string, string, error)
```

Dispatch wiring: add `"deliver": true` to `supported` (`dispatch.go:49`) and a case:

```go
case "deliver":
    if len(args) < 5 {
        return fmt.Errorf("deliver: want ITEM QTY FROM TO RECIPIENT, got %v", args)
    }
    qty, err := strconv.Atoi(args[1])
    if err != nil || qty < 1 {
        return fmt.Errorf("deliver: bad qty %q", args[1])
    }
    return d.Deliver(ctx, args[0], qty, args[2], args[3], args[4])
```

**Deliver algorithm (the recompute-remaining invariant):**
1. Resolve FROM and TO to (system, poi) via `resolveBase`.
2. Count the item already in cargo (`d.Client.GetState().Ship.Cargo` — check the actual field name in `pkg/game/types.go` `Ship` struct before coding; do NOT guess). `carrying := cargoCount(state, itemID)`.
3. If `carrying < qty`: autopilot to FROM, dock, `WithdrawItems(ctx, itemID, float64(min(qty-carrying, cargoFree)))`; withdraw errors that read as "not enough"/"no such item" are NOT fatal — take what exists (re-read cargo to learn the actual amount), note the shortfall.
4. Autopilot to TO, dock.
5. If `recipient == "self" || recipient == d.AgentID`: `DepositItems(ctx, itemID, float64(carrying))`. Else `SendGift(ctx, payload)` — payload shape per Task 0 findings doc (`docs/superpowers/specs/2026-07-10-executor-b-live-mechanics.md`); read it before implementing.
6. Loop 2-5 while short of qty AND the last withdraw actually produced items (progress guard: a pass that moves 0 breaks the loop and returns nil with the shortfall logged to `d.Out`).
7. Between game actions use `game.SleepQuick`; after travel legs Autopilot already paces itself.

- [ ] **Step 1: Write failing test** — `pkg/worker/deliver_test.go`. Model on existing worker verb tests (`pkg/worker/assist.go` tests use a fake GameClient — find the existing fake: `grep -rn "fakeClient\|mockClient" pkg/worker/*_test.go` and REUSE it; do not build a new mock). Test cases: (a) full delivery gifts at destination — assert SendGift called with item+qty+recipient after autopilot to TO; (b) recipient self deposits instead — assert DepositItems, no SendGift; (c) source short — withdraw yields 3 of 5, verb returns nil, gift qty 3.
- [ ] **Step 2: Run, verify FAIL** — `go test ./pkg/worker/ -run TestDeliver -v`.
- [ ] **Step 3: Implement** `deliver.go` + dispatch case per the algorithm.
- [ ] **Step 4: Run, verify PASS** — `go test ./pkg/worker/ -v` (the full package; roles_test must still pass).
- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/worker/
git add pkg/worker/
git commit -m "feat(worker): deliver verb — directed source->recipient transport"
```

---

### Task 7: buy_directed verb

**Files:**
- Create: `pkg/worker/buy_directed.go`
- Modify: `pkg/worker/dispatch.go`
- Test: `pkg/worker/buy_directed_test.go`

**Interfaces:**
- Consumes: `game.GameClient.Buy(ctx, itemID string, quantity float64)`, market view for price check (`d.Client.ViewMarket`? — check the exact GameClient method the `view_market` dispatch case uses at `dispatch.go:156` and reuse), Deliver's `resolveBase` + gift-or-deposit helper (extract `giftOrDeposit(ctx, d, itemID string, qty int, recipient string) error` from Task 6 into `deliver.go` if not already shaped that way).
- Produces:

```go
// BuyDirected buys ITEM xQTY at STATION with a per-unit price ceiling
// (0 = no ceiling), then hands to RECIPIENT (gift, or deposit when self).
// Recompute-remaining: counts recipient-bound goods already in cargo first.
func (d *WorkerDispatch) BuyDirected(ctx context.Context, itemID string, qty int, station string, maxUnit float64, recipient string) error
```

Dispatch: `"buy_directed": true` + case parsing `ITEM QTY STATION MAX_UNIT_PRICE RECIPIENT` (`strconv.ParseFloat` for the ceiling; `0` disables).

Algorithm: resolve station → autopilot+dock → check live ask via the same market-view path `haulGate` uses (`pkg/worker/haul.go:98` reads `[]market.ItemStationPrice` — mirror how `runClaimedHaul` fetches live prices, `haul.go:708`; reuse that helper if exported, else extract one) → if ask > maxUnit && maxUnit > 0: fail with `"price %v exceeds ceiling %v (replan)"` (the runner's park detail will carry it) → `Buy` in cargo-sized chunks → `giftOrDeposit`.

- [ ] **Step 1: Write failing test** — cases: (a) buys and gifts; (b) ceiling exceeded → error containing "replan", no Buy call; (c) qty larger than cargo → two Buy calls (loop).
- [ ] **Step 2: Run, verify FAIL** — `go test ./pkg/worker/ -run TestBuyDirected -v`.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run full package, verify PASS.**
- [ ] **Step 5: Lint + commit**

```bash
git add pkg/worker/
git commit -m "feat(worker): buy_directed verb with per-unit price ceiling"
```

---

### Task 8: craft_node verb + CraftDryRun client method

**Files:**
- Modify: `pkg/game/interface.go`, `pkg/game/crafting.go`, `pkg/game/serverapi/responses.go`
- Create: `pkg/worker/craft_node.go`
- Modify: `pkg/worker/dispatch.go`
- Test: `pkg/game/crafting_test.go` (extend), `pkg/worker/craft_node_test.go`

**PRECONDITION: read `docs/superpowers/specs/2026-07-10-executor-b-live-mechanics.md` (Task 0). The dry-run response struct fields below are the EXPECTED shape — correct them against the captured JSON before implementing.**

**Interfaces:**
- Produces (pkg/game):

```go
// serverapi (adjust fields to Task 0 findings):
type CraftDryRunResult struct {
    Action     string  `json:"action"`
    RecipeID   string  `json:"recipe_id"`
    Quantity   int     `json:"quantity"`
    Cost       float64 `json:"cost"`        // total fee for the queued job
    TimeTicks  float64 `json:"time_ticks"`  // duration estimate
    FacilityID string  `json:"facility_id,omitempty"`
    StationID  string  `json:"station_id,omitempty"`
}

// GameClient addition:
CraftDryRun(ctx context.Context, recipeID string, quantity int, facilityID string) (*serverapi.CraftDryRunResult, error)
```

`CraftDryRun` submits `protocol.Message{Type: "craft", Payload: {recipe_id, quantity, dry_run: true, facility_id?}}` via `Submit(... WithTerminator(terminateOnActionOrOK) ...)` (mirror `CraftBulk`, `pkg/game/crafting.go:144`) and decodes the result body into the struct.

**⚠ Interface ripple:** adding to `GameClient` breaks `MCPGameClient` and every mock in `pkg/agent`/`pkg/skills`. Add the method to all of them (mocks: return a canned result). Run FULL `go test ./...`.

- Produces (pkg/worker):

```go
// CraftOutputs crafts NUM_OUTPUTS units of RECIPE's output at STATION.
// facility "hand" = hand-craft where docked; otherwise the facility instance id.
// Runs = ceil(numOutputs / output_per_run) — the recipe's output quantity is
// read from the KB (recipes/recipe_outputs), never assumed 1.
// Dry-run first: fee and duration logged; the runner's budget gate already
// approved the ESTIMATE, so a dry-run cost more than 2x the node estimate
// fails with "replan" (stale catalog).
func (d *WorkerDispatch) CraftOutputs(ctx context.Context, recipeID string, numOutputs int, station, facility string) error
```

Dispatch: `"craft_node": true` + case parsing `RECIPE NUM_OUTPUTS STATION FACILITY`.

Algorithm: resolve station (skip travel when facility == "hand" && station == current station) → autopilot+dock → read recipe output-per-run from KB (type-assert `*knowledge.SQLiteKB`, query `SELECT quantity FROM recipe_outputs WHERE recipe_id = ? LIMIT 1`; missing → error) → **recompute remaining**: outputs already in own storage+cargo at station count against numOutputs (query storage via `ViewStorage` + `GetRawJSON("storage")` — check the raw key name used by `pkg/agent/storage_capture.go` and reuse its decode shape) → dry-run (skip when facility == "hand" and Task 0 found hand dry-run unsupported) → craft in `MaxCraftBatchSize(state)` chunks via `CraftWithQuantity` (hand) or the facility-targeted payload (per Task 0: likely `CraftBulk`-shaped single job with `facility_id`) → wait out craft time between batches using `game.SleepTick` polling of the job queue (`CraftQueued` — see `pkg/game/client_helpers.go:340`) → deposit outputs to storage.

- [ ] **Step 1: Write failing test for CraftDryRun** (pkg/game): use the package's existing fake-server/websocket test harness — find how `TestCraft*` tests in `pkg/game` simulate responses and mirror one, returning a canned dry-run body; assert fields decode.
- [ ] **Step 2: Run, verify FAIL.**
- [ ] **Step 3: Implement CraftDryRun + serverapi struct + MCP/mock ripple.** Run `go build ./...` then FULL `go test ./...` — fix every mock the interface change breaks.
- [ ] **Step 4: Write failing test for CraftOutputs** (pkg/worker fake client): (a) computes runs from output_per_run 2 → NUM_OUTPUTS 5 → CraftWithQuantity called with quantity 5 (server takes units) in ≤ MaxCraftBatchSize chunks; (b) dry-run cost 2.5x estimate → error containing "replan", no craft call; (c) 3 of 5 outputs already in storage → crafts only remaining 2.
- [ ] **Step 5: Run, verify FAIL, implement, verify PASS** — `go test ./pkg/worker/ ./pkg/game/ -v` then full `go test ./...`.
- [ ] **Step 6: Lint + commit**

```bash
golangci-lint run ./pkg/game/ ./pkg/worker/
git add pkg/game/ pkg/worker/ pkg/agent/ pkg/skills/
git commit -m "feat(worker): craft_node verb + CraftDryRun client method"
```

---

### Task 9: mine_qty verb

**Files:**
- Create: `pkg/worker/mine_qty.go`
- Modify: `pkg/worker/dispatch.go`
- Test: `pkg/worker/mine_qty_test.go`

**Interfaces:**
- Consumes: `d.Client.Mine(ctx)`, `galaxy.FindNearestByPOIType` (the shuttle recovery path uses it — `pkg/worker/shuttle.go`; mirror its resource-POI lookup), Task 6's `Deliver`.
- Produces:

```go
// MineQty mines until QTY of ITEM is in cargo (or the belt stops yielding),
// then delivers the haul to RECIPIENT at TO via Deliver's gift-or-deposit.
// Bounded: stops after MineQtyMaxPasses mine calls that yield no new units.
func (d *WorkerDispatch) MineQty(ctx context.Context, itemID string, qty int, to, recipient string) error

const MineQtyMaxDryPasses = 5
```

Dispatch: `"mine_qty": true`, case parses `ITEM QTY TO RECIPIENT`.

Algorithm: recompute remaining (cargo count) → find nearest POI yielding the resource: query KB `poi_resources` for the item in the current system first, else `FindNearestByPOIType` fallback by resource POI type → autopilot (undocks as needed) → loop `Mine(ctx)` with `game.SleepTick` between calls, tracking cargo growth; `MineQtyMaxDryPasses` consecutive no-growth passes → break → `Deliver`-style transport to TO/RECIPIENT (call `d.Deliver(ctx, itemID, minedQty, "", to, recipient)` with empty FROM meaning "already in cargo" — extend Deliver: empty `from` skips the withdraw leg).

- [ ] **Step 1: Write failing test** — (a) mines until qty reached then gifts at TO; (b) dry belt (fake yields nothing) → stops after MineQtyMaxDryPasses, delivers what it has, returns nil.
- [ ] **Step 2-4: FAIL → implement (incl. the Deliver empty-FROM extension + its test) → PASS** — `go test ./pkg/worker/ -v`.
- [ ] **Step 5: Lint + commit**

```bash
git add pkg/worker/
git commit -m "feat(worker): mine_qty verb — mine to quantity then deliver"
```

---

### Task 10: marketbot handoff standing hook

**Files:**
- Create: `pkg/worker/handoff_pass.go`
- Modify: `pkg/worker/standing.go` (StandingDeps + idle-pass hook), `cmd/worker/main.go` (wiring), `cmd/worker` flags
- Test: `pkg/worker/handoff_pass_test.go`

**PRECONDITION: Task 0 findings for the gift payload; Task 1 for the queue.**

**Interfaces:**
- Consumes: `handoff.Queue`, `d.Client.WithdrawItems/SendGift/GetState`.
- Produces:

```go
// HandoffPass fulfills pending handoff records owned by this agent at its
// current docked station: withdraw from storage (batched by cargo hold, or a
// single --source=storage gift when Task 0 confirmed the server patch), gift
// to the recipient, mark done with MovedQty. Short stock → done with
// MovedQty < Qty (the plan runner surfaces the shortfall).
func (d *WorkerDispatch) HandoffPass(ctx context.Context, q *handoff.Queue) error
```

StandingDeps addition (mirror the PayDebts hook exactly, `pkg/worker/standing.go:46-48,117-119`):

```go
// Handoffs, when set, runs once per non-drained idle pass under ExecMu to
// fulfill pending stock-handoff records for this agent. nil when the worker
// has no handoff queue configured.
Handoffs func(context.Context)
```

cmd/worker: new flag `--handoff-queue` (default "", disabled); when set, wire `deps.Handoffs = func(ctx context.Context) { _ = dispatch.HandoffPass(ctx, handoffQueue) }` alongside the PayDebts wiring.

- [ ] **Step 1: Write failing test** — fake client + real queue in tempdir: (a) pending record for this agent at current station → withdraw + gift + record done with MovedQty; (b) record for another holder untouched; (c) record at a different station untouched; (d) storage short (withdraw yields 3 of 8) → done, MovedQty 3.
- [ ] **Step 2-4: FAIL → implement → PASS** — `go test ./pkg/worker/ -v` and `go build ./cmd/worker/`.
- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/worker/ ./cmd/worker/
git add pkg/worker/ cmd/worker/
git commit -m "feat(worker): handoff standing pass — resident holders gift staged stock"
```

---

### Task 11: scripts, craftsman role, craft fleet config

**Files:**
- Create: `data/scripts/craft_node.smolt`, `data/scripts/deliver_node.smolt`, `data/scripts/buy_node.smolt`, `data/scripts/mine_node.smolt`
- Create: `data/overmind/craft-fleet.yaml`
- Modify: `data/overmind/roles.yaml`, `.gitignore` (negations for the new .smolt files if `data/scripts` is ignore-patterned — CHECK first)
- Test: `pkg/worker/roles_test.go` already enforces script/verb sync — run it.

Script contents (each one line; params substituted by `SubstituteParams`):

```
# craft_node.smolt — craft NUM_OUTPUTS units of RECIPE at STATION (facility or hand)
craft_node $RECIPE$ $NUM_OUTPUTS$ $STATION$ $FACILITY$
```

```
# deliver_node.smolt — move ITEM xQTY from FROM to RECIPIENT at TO
deliver $ITEM$ $QTY$ $FROM$ $TO$ $RECIPIENT$
```

```
# buy_node.smolt — buy ITEM xQTY at STATION under MAX_UNIT_PRICE, hand to RECIPIENT
buy_directed $ITEM$ $QTY$ $STATION$ $MAX_UNIT_PRICE$ $RECIPIENT$
```

```
# mine_node.smolt — mine QTY of ITEM then deliver to RECIPIENT at TO
mine_qty $ITEM$ $QTY$ $TO$ $RECIPIENT$
```

roles.yaml addition (mirror the resident role's capture schedule so idle craftsmen keep contributing data):

```yaml
  craftsman:
    schedule:
      - {every: hourly, command: update_market}
      - {every: hourly, command: facilities}
      - {every: daily, command: kb_update}
    idle: idle_market
```

(`idle_market` already exists: dock + refuel — exactly right for a craftsman parked at home.)

craft-fleet.yaml (stations REQUIRE OPERATOR CONFIRMATION — query where each craftsman is actually docked: `sqlite3 data/spacemolt-knowledge.db "SELECT agent_id, base_id FROM storage_snapshots WHERE agent_id LIKE 'craftsman-%' GROUP BY agent_id"` gives last-seen bases; the operator confirms/corrects before first launch):

```yaml
workers:
  - {agent_id: craftsman-2, role: craftsman, station: <operator-confirm>}
  - {agent_id: craftsman-3, role: craftsman, station: <operator-confirm>}
  # ... craftsman-4..10 likewise
```

- [ ] **Step 1:** Check `.gitignore` handling of `data/scripts/*.smolt` (`git check-ignore data/scripts/craft_node.smolt`); add negations if ignored.
- [ ] **Step 2:** Write the four scripts + roles.yaml entry + craft-fleet.yaml.
- [ ] **Step 3:** Run `go test ./pkg/worker/ -run TestRoles -v` and `go test ./pkg/worker/ -run TestSeeded -v` — the roles/dispatch sync tests must accept the new role and scripts. (Note: `TestSeededCommandsAreDispatchable` has a PRE-EXISTING failure for shuttle/assist idle scripts — do not fix it here, but do not add NEW failures.)
- [ ] **Step 4: Commit**

```bash
git add data/scripts/ data/overmind/roles.yaml data/overmind/craft-fleet.yaml .gitignore
git commit -m "feat(overmind): craftsman role, craft-fleet roster, node scripts"
```

---

### Task 12: cmd/overmind wiring

**Files:**
- Modify: `cmd/overmind/main.go`
- Test: build + a focused flag test if `cmd/overmind` has one (check; if not, wiring is exercised by Task 15's smoke).

**Interfaces:**
- Consumes: `plans.Runner` (Task 5), existing main select loop (`cmd/overmind/main.go:180-186`).

Changes:
1. Flags: `--plan-queue` (default "", disabled), `--plan-state-dir` (default "data/overmind/craft-plans"), `--handoff-queue` (default "data/overmind/handoff-queue.json", only used when plan-queue set), `--holders-roster` (default "data/overmind/mb-fleet.yaml" — parsed with `supervisor.LoadFleet`? check the loader name in `pkg/overmind/supervisor/config.go` — into the Managed map).
2. When `--plan-queue` is set: construct `plans.Runner{QueueDir, StateDir, Store: taskStore, Handoff: handoff.NewQueue(...), Roster: from the fleet config (agent_id+station of every craftsman-role worker), Managed: from holders-roster, Logger: logger}` and call `runner.Tick()` in the ticker case right after `taskStore.AssignPending(snap, srv)`.
3. `DefaultSpawn` must forward `--handoff-queue` to spawned workers when set (mirror how `--rescue-queue` is forwarded — and note the known gap that operator overrides split paths; keep parity, don't fix here).

- [ ] **Step 1:** Implement flags + wiring.
- [ ] **Step 2:** `go build ./cmd/overmind/ ./cmd/worker/` and `go test ./cmd/... ./pkg/overmind/...` — green (modulo pre-existing reds).
- [ ] **Step 3: Lint + commit**

```bash
golangci-lint run ./cmd/overmind/
git add cmd/overmind/
git commit -m "feat(overmind): plan-queue runner wiring + handoff flags"
```

---

### Task 13: play_as dispatch + plan control commands

**Files:**
- Create: `cmd/tools/play_as/dispatch.go`
- Modify: `cmd/tools/play_as/main.go` (command cases + help text)
- Test: `cmd/tools/play_as/dispatch_test.go`

**Interfaces:**
- Consumes: `plans.QueueFile/Manifest/PlanRun/LoadRun/SaveRun/Control`, `craftbrain.Plan` JSON (from `build --json` output saved to a file), `finditem.Find` (already used by `find_item`) for buy-conversion pricing, `CraftDryRun` (Task 8) for facility verification.
- Produces play_as commands:

```
dispatch <plan.json> [--budget=N] [--mine=item1,item2] [--assembly=BASE] [--skip-verify]
plan_status [plan-id]
plan_pause <plan-id> | plan_resume <plan-id> [--raise-cap=N]
plan_cancel <plan-id> | plan_retry <plan-id> <node-id>
```

**dispatch behavior** (`runDispatch(client game.GameClient, ctx context.Context, args []string) error`):
1. Read + decode the plan JSON (a `craftbrain.Plan`).
2. Resolve every `any_docked_station` StationID/ToBase to `--assembly` (default: first entry of `data/overmind/craft-fleet.yaml`).
3. Leaf tagging: for each mine node whose item is NOT in `--mine`: look up sellers via `finditem.Find`; found → convert the node to `Kind: KindBuy` with `StationID` = best seller station and stamp `FeeTotal = ceil(ask*qty)` (the buy ceiling source); none → leave as mine.
4. Unless `--skip-verify`: for up to 10 facility craft nodes run `CraftDryRun`; a dry-run cost > 2x the node's `FeeTotal` aborts dispatch with a re-plan message.
5. Budget: `--budget` or `ceil(1.25 * (sum FeeTotal + sum buy costs))`.
6. Plan id: `<target>-<YYYYMMDD-HHMMSS>` UTC. Write `data/overmind/craft-queue/<plan-id>.json`.

**plan_* behavior:** `plan_status` lists `craft-plans/*.state.json` (id, status, spent/cap, per-state node counts, parked details); the mutators `LoadRun` → set Control field → `SaveRun` (flock makes this safe against the overmind's writes).

- [ ] **Step 1: Write failing tests** for the pure parts: leaf tagging (fake finditem results — check `finditem.Result` fields in `pkg/finditem/`), assembly resolution, budget default math, queue-file shape. Follow the existing play_as test style (table-driven, no live client — see `plan_route_test.go`).
- [ ] **Step 2-4: FAIL → implement → PASS** — `go test ./cmd/tools/play_as/ -v`.
- [ ] **Step 5:** Add help lines to the play_as help output (`main.go` — grep `craftable [--reachable]` for the help block).
- [ ] **Step 6: Lint + commit**

```bash
golangci-lint run ./cmd/tools/play_as/
git add cmd/tools/play_as/
git commit -m "feat(play_as): dispatch + plan_* control commands"
```

---

### Task 14: craft-dashboard

**Files:**
- Create: `pkg/craftdash/render.go`, `pkg/craftdash/render_test.go`
- Create: `cmd/tools/craft-dashboard/main.go`, `cmd/tools/craft-dashboard/assets/mermaid.min.js` (vendored)

**Interfaces:**
- Consumes: `plans.PlanRun/LoadAllRuns/ItemProgress`.
- Produces:

```go
package craftdash

// MermaidForPlan renders the plan DAG as mermaid flowchart source.
// State → class: done -> "done" (faded grey), dispatched -> "active" (bold),
// parked -> "parked" (red), waiting -> default. Node label:
// "<kind> <item> xN" + agent on active nodes + park reason on parked ones.
// Edges: consumer --> producer dependency arrows; edges into active nodes
// use a thick link (==>).
func MermaidForPlan(pr *plans.PlanRun) string

// Page renders the full HTML for all runs: per-plan header (id, status,
// spent/cap), the mermaid <pre class="mermaid">, and the progress table.
func Page(runs []*plans.PlanRun, refreshSec int) []byte
```

Mermaid source shape (golden-tested):

```
flowchart TD
  classDef done fill:#eee,color:#999,stroke:#ccc
  classDef active fill:#fffbe6,stroke:#e6a700,stroke-width:3px
  classDef parked fill:#ffe6e6,stroke:#cc0000,stroke-width:2px
  craft-1["craft widget x2<br/>@craftsman-2"]:::active
  mine-2["mine ore x10"]:::done
  craft-1 ==> mine-2
```

(Node ids sanitized: mermaid ids must not contain `/` — replace with `_`.)

main.go: flags `--addr :8091`, `--plans-dir data/overmind/craft-plans`, `--refresh 30`; handler re-reads `LoadAllRuns` per request, serves `Page`; `/assets/mermaid.min.js` served via `go:embed`; the page `<script>` initializes `mermaid.initialize({startOnLoad: true})` and meta-refreshes every `--refresh` seconds. Follow `cmd/tools/overmind-status/main.go` structure.

- [ ] **Step 1:** Vendor mermaid: `curl -L -o cmd/tools/craft-dashboard/assets/mermaid.min.js https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js` (check ~2-3MB size sanity; commit it — check `.gitignore` for `assets/` patterns first).
- [ ] **Step 2: Write failing golden tests** — build a small PlanRun fixture (one done, one active-with-agent, one parked node) and assert the exact mermaid string; assert Page contains the progress row `4 / 10 ore` style cells and the spent/cap header.
- [ ] **Step 3-4: FAIL → implement → PASS** — `go test ./pkg/craftdash/ -v`; `go build -o bin/craft-dashboard ./cmd/tools/craft-dashboard/`.
- [ ] **Step 5: Manual render check:** create a fixture state file in a temp dir, run `bin/craft-dashboard --plans-dir <tmp> --addr :8091`, `curl -s localhost:8091 | head -50` shows the mermaid block. Kill it.
- [ ] **Step 6: Lint + commit**

```bash
golangci-lint run ./pkg/craftdash/ ./cmd/tools/craft-dashboard/
git add pkg/craftdash/ cmd/tools/craft-dashboard/
git commit -m "feat(craftdash): mermaid plan dashboard on :8091"
```

---

### Task 15: integration sweep + rollout runbook

**Files:**
- Modify: `docs/superpowers/specs/2026-07-10-crafting-brain-b-executor-design.md` (append "Rollout" section)

- [ ] **Step 1:** `go build ./...` and FULL `go test ./...` — green except the two documented pre-existing reds (`pkg/game` espionage command coverage, `pkg/worker` shuttle/assist idle-script resolution). Any OTHER red is yours: fix it.
- [ ] **Step 2:** `golangci-lint run ./...` over changed packages — zero new findings.
- [ ] **Step 3:** Append the Rollout runbook to the spec:
  - Rebuild `bin/overmind`, `bin/worker`, `bin/craft-dashboard`.
  - **mb fleet redeploy required** for the handoff hook (drain USR1 → TERM → relaunch with `--handoff-queue`; exact launch lines in the ops memory / `data/overmind/*-overmind.log`).
  - Craft fleet first launch: operator confirms craft-fleet.yaml stations, funds wallets, then launch with `--stagger 10s` (login rate limits; FRESH logins are unmetered but stay conservative).
  - Live smoke: `play_as build air_recycler 2 --json > /tmp/plan.json` → `dispatch /tmp/plan.json --budget 5000` → watch `:8091` → verify gift handoffs + final goods at the assembly base.
- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-07-10-crafting-brain-b-executor-design.md
git commit -m "docs(craftbrain): executor B rollout runbook"
```

---

## Self-review notes (already applied)

- Spec coverage: every spec section maps to a task — lifecycle/CLI (5, 13), DAG contract (3, 4), verbs (6-9), handoff (1, 10), budget (5, 8, 13), fleet (11, 12), dashboard (14), live verifications (0), rollout (15).
- The `MAX_UNIT_PRICE` source is dispatch-stamped `FeeTotal` on converted buy nodes and plan-estimate `FeeTotal` on A2 buy nodes (Task 5 table + Task 13 step 3 agree).
- Type consistency: `plans.RosterAgent`, `handoff.Record`, `nodeTask`/`taskIDFor`, `CraftDryRunResult` are each defined once and referenced by those names in consuming tasks.
- Known softness (deliberate): Task 8's dry-run struct fields and Task 10's gift payload finalize against the Task 0 findings doc — that is the point of Task 0, not a placeholder.
