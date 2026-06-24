# Overmind Phase 2b — Assigned Tasks + Directed Mining Run Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the overmind an assign-task path — it loads tasks from a seed file, assigns each to an eligible idle worker over the control channel, and the worker runs the task's script once (pausing its standing idle behavior) and reports completion — proven with a directed mining-run job.

**Architecture:** A new `control.Assign` message carries `{task_id, script, params}` from overmind to worker. The overmind side adds an in-memory task store (`pkg/overmind/tasks`) loaded from `data/overmind/tasks.yaml`, with an assignment pass driven from `cmd/overmind`'s existing status ticker. The worker side adds `$KEY$` param substitution and makes `RunStanding` task-aware: when a task is pending it runs the task script (params substituted, then live tokens resolved) on the same `ExecMu` as idle work, then reports a `task_done`/`task_failed` event and resumes idle. The concrete job is `data/scripts/mining_run.smolt` run by a new `miner` role.

**Tech Stack:** Go 1.24, existing `pkg/overmind/{control,supervisor}`, `pkg/worker` (loop engine, dispatch, scripts), YAML config.

## Global Constraints

- Target Go 1.24+; use `game.Sleep*` constants for any durations.
- All new code must pass `golangci-lint run <pkg>` with no new findings; match the package's existing `//nolint:errcheck` convention on writes to an `io.Writer`.
- Run `go build ./...` and the relevant package tests before each commit.
- `pkg/worker` must NOT import `pkg/overmind/control` — the worker learns about tasks through a lightweight local `AssignedTask` struct and injected callbacks, exactly as it already takes `Paused func() bool`.
- Reuse existing pieces unchanged: the loop engine (`runLine`, `SplitScriptCommands`, `ResolveTokens`, `ResolveScriptArg`), `WorkerDispatch`, the control codec (`NewEnvelope`/`Into`/`Encoder`/`Decoder`), and `Server.Send`/`SetEventHook`/`Fleet.Snapshot`.
- A task runs once (not looped); task work stays on the single `ExecMu` so it never interleaves with scheduled commands on the one game connection.

Spec: `docs/superpowers/specs/2026-06-24-overmind-assigned-tasks-design.md`.

Key existing signatures this plan builds on:
- `control.NewEnvelope(t control.Type, agentID string, payload any) (control.Envelope, error)`; `env.Into(v any) error`; `control.Event{Kind, Detail, Timestamp string}`; `control.Status{… ActiveTaskID string}`.
- `supervisor.Server.Send(agentID string, env control.Envelope) error`; `Server.SetEventHook(func(agentID string, ev control.Event))`; `Fleet.Snapshot() []supervisor.WorkerInfo`; `WorkerInfo{AgentID, Role string; Healthy bool; LastStatus control.Status}`.
- `worker.ResolveScriptArg(arg, agentID string) (string, bool)`; `worker.SplitScriptCommands(content string) ([]string, error)`; `worker.StandingDeps`; `(StandingDeps).runLine(ctx, line string)`.

---

### Task 1: `control.Assign` message

**Files:**
- Modify: `pkg/overmind/control/messages.go`
- Test: `pkg/overmind/control/messages_test.go`

**Interfaces:**
- Produces: `control.TypeAssign control.Type = "assign"`; `control.Assign{TaskID string; Script string; Params map[string]string}`.
- Consumes: existing `NewEnvelope` / `Into` / `Encoder` / `Decoder`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/overmind/control/messages_test.go` (match the existing round-trip test style in that file):

```go
func TestAssignRoundTrip(t *testing.T) {
	in := Assign{
		TaskID: "mine-bunda-iron",
		Script: "mining_run",
		Params: map[string]string{"TARGET_SYSTEM": "bunda", "COUNT": "20"},
	}
	env, err := NewEnvelope(TypeAssign, "miner-1", in)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if env.Type != TypeAssign {
		t.Fatalf("type = %q, want %q", env.Type, TypeAssign)
	}
	var out Assign
	if err := env.Into(&out); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if out.TaskID != in.TaskID || out.Script != in.Script || out.Params["TARGET_SYSTEM"] != "bunda" || out.Params["COUNT"] != "20" {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/overmind/control/ -run TestAssignRoundTrip 2>&1 | head`
Expected: build failure — `TypeAssign` / `Assign` undefined.

- [ ] **Step 3: Add the type and struct**

In `pkg/overmind/control/messages.go`, add to the `const (...)` Type block (after `TypeResume`):

```go
	TypeAssign Type = "assign"
```

And add the struct alongside the other payload structs (e.g. after `Abort`):

```go
// Assign tells a worker to run a one-shot task: resolve Script via the worker's
// script search path, substitute Params ($KEY$) into it, then run it once in
// place of the idle behavior. Completion is reported back via an Event
// (Kind "task_done" / "task_failed").
type Assign struct {
	TaskID string            `json:"task_id"`
	Script string            `json:"script"`
	Params map[string]string `json:"params,omitempty"`
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/overmind/control/ -run TestAssignRoundTrip -v`
Expected: PASS.

- [ ] **Step 5: Build + lint**

Run: `go build ./... && golangci-lint run ./pkg/overmind/control/`
Expected: build OK; 0 issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/overmind/control/messages.go pkg/overmind/control/messages_test.go
git commit -m "feat(overmind): control Assign message for task dispatch"
```

---

### Task 2: `SubstituteParams` — `$KEY$` substitution

**Files:**
- Create: `pkg/worker/params.go`
- Create: `pkg/worker/params_test.go`

**Interfaces:**
- Produces: `func SubstituteParams(lines []string, params map[string]string) []string`.

- [ ] **Step 1: Write the failing test**

Create `pkg/worker/params_test.go`:

```go
package worker

import (
	"slices"
	"testing"
)

func TestSubstituteParams(t *testing.T) {
	lines := []string{
		"autopilot $TARGET_SYSTEM$",
		"loop -f $COUNT$ mine",
		"travel $ASTEROID_BELT$", // live token — must be left untouched
	}
	got := SubstituteParams(lines, map[string]string{"TARGET_SYSTEM": "bunda", "COUNT": "20"})
	want := []string{
		"autopilot bunda",
		"loop -f 20 mine",
		"travel $ASTEROID_BELT$",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSubstituteParamsTokenInQuotedArg(t *testing.T) {
	got := SubstituteParams([]string{`chat "heading to $TARGET_SYSTEM$ now"`},
		map[string]string{"TARGET_SYSTEM": "sol"})
	want := []string{`chat "heading to sol now"`}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSubstituteParamsEmptyIsNoOp(t *testing.T) {
	in := []string{"autopilot $TARGET_SYSTEM$"}
	got := SubstituteParams(in, nil)
	if !slices.Equal(got, in) {
		t.Fatalf("got %v, want unchanged %v", got, in)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run TestSubstituteParams 2>&1 | head`
Expected: build failure — `SubstituteParams` undefined.

- [ ] **Step 3: Implement**

Create `pkg/worker/params.go`:

```go
package worker

import "strings"

// SubstituteParams replaces every $KEY$ occurrence in each line with
// params[KEY], for keys present in params. Keys absent from params (e.g. live
// state tokens like $ASTEROID_BELT$) are left untouched so they pass through to
// ResolveTokens. Returns a new slice; the input is not mutated. A nil/empty
// params map is a no-op.
func SubstituteParams(lines []string, params map[string]string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	if len(params) == 0 {
		return out
	}
	for k, v := range params {
		token := "$" + k + "$"
		for i := range out {
			if strings.Contains(out[i], token) {
				out[i] = strings.ReplaceAll(out[i], token, v)
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/worker/ -run TestSubstituteParams -v`
Expected: PASS (all three).

- [ ] **Step 5: Build + lint**

Run: `go build ./... && golangci-lint run ./pkg/worker/`
Expected: build OK; 0 issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/params.go pkg/worker/params_test.go
git commit -m "feat(worker): SubstituteParams for task script \$KEY\$ substitution"
```

---

### Task 3: Task model + seed loader

**Files:**
- Create: `pkg/overmind/tasks/task.go`
- Create: `pkg/overmind/tasks/task_test.go`
- Create: `pkg/overmind/tasks/testdata/tasks_valid.yaml`
- Create: `pkg/overmind/tasks/testdata/tasks_dup.yaml`

**Interfaces:**
- Produces: `tasks.TaskStatus` (`StatusPending/Assigned/Running/Done/Failed`); `tasks.Task{ID, Script string; Params map[string]string; RoleRequired, AgentID string; Status TaskStatus; AssignedTo string}`; `tasks.LoadTasks(path string) ([]Task, error)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/overmind/tasks/testdata/tasks_valid.yaml`:

```yaml
tasks:
  - id: mine-bunda-iron
    script: mining_run
    role_required: miner
    params: { TARGET_SYSTEM: bunda, COUNT: "20" }
  - id: mine-dustfall
    script: mining_run
    agent_id: miner-3
    role_required: miner
    params: { TARGET_SYSTEM: dustfall, COUNT: "15" }
```

Create `pkg/overmind/tasks/testdata/tasks_dup.yaml`:

```yaml
tasks:
  - id: dupe
    script: mining_run
    role_required: miner
  - id: dupe
    script: mining_run
    role_required: miner
```

Create `pkg/overmind/tasks/task_test.go`:

```go
package tasks

import (
	"path/filepath"
	"testing"
)

func TestLoadTasksValid(t *testing.T) {
	got, err := LoadTasks(filepath.Join("testdata", "tasks_valid.yaml"))
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want 2", len(got))
	}
	if got[0].ID != "mine-bunda-iron" || got[0].Script != "mining_run" ||
		got[0].RoleRequired != "miner" || got[0].Params["TARGET_SYSTEM"] != "bunda" {
		t.Fatalf("task[0] mismatch: %+v", got[0])
	}
	if got[0].Status != StatusPending {
		t.Fatalf("task[0] status = %q, want %q", got[0].Status, StatusPending)
	}
	if got[1].AgentID != "miner-3" {
		t.Fatalf("task[1] agent_id = %q, want miner-3", got[1].AgentID)
	}
}

func TestLoadTasksRejectsDuplicateID(t *testing.T) {
	if _, err := LoadTasks(filepath.Join("testdata", "tasks_dup.yaml")); err == nil {
		t.Fatal("expected error on duplicate id, got nil")
	}
}

func TestLoadTasksRejectsMissingFields(t *testing.T) {
	// written inline via a temp file: a task missing script
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := writeFile(p, "tasks:\n  - id: x\n    role_required: miner\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTasks(p); err == nil {
		t.Fatal("expected error on missing script, got nil")
	}
}
```

Add this helper at the bottom of `task_test.go`:

```go
func writeFile(path, content string) error {
	return osWriteFile(path, content)
}
```

and create the tiny indirection in the test file's imports — actually use `os` directly: replace the helper with a direct call. Use this final form for the missing-fields test instead of `writeFile`:

```go
func TestLoadTasksRejectsMissingFields(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte("tasks:\n  - id: x\n    role_required: miner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTasks(p); err == nil {
		t.Fatal("expected error on missing script, got nil")
	}
}
```

and add `"os"` to the test imports; drop the `writeFile`/`osWriteFile` helper entirely.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/overmind/tasks/ 2>&1 | head`
Expected: build failure — package/`LoadTasks`/`StatusPending` undefined.

- [ ] **Step 3: Implement**

Create `pkg/overmind/tasks/task.go`:

```go
// Package tasks holds the overmind's assigned-task model, seed-file loader, and
// the in-memory store that matches pending tasks to idle workers.
package tasks

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TaskStatus is the lifecycle state of a task.
type TaskStatus string

const (
	StatusPending  TaskStatus = "pending"
	StatusAssigned TaskStatus = "assigned"
	StatusRunning  TaskStatus = "running"
	StatusDone     TaskStatus = "done"
	StatusFailed   TaskStatus = "failed"
)

// Task is one unit of assignable work.
type Task struct {
	ID           string            `yaml:"id"`
	Script       string            `yaml:"script"`
	Params       map[string]string `yaml:"params"`
	RoleRequired string            `yaml:"role_required"`
	AgentID      string            `yaml:"agent_id"` // optional pin

	Status     TaskStatus `yaml:"-"`
	AssignedTo string     `yaml:"-"`
}

type tasksFile struct {
	Tasks []Task `yaml:"tasks"`
}

// LoadTasks parses the seed file at path, validating each task and defaulting
// Status to pending. Duplicate or empty ids, and missing script/role_required,
// are errors.
func LoadTasks(path string) ([]Task, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("tasks: read %s: %w", path, err)
	}
	var tf tasksFile
	if err := yaml.Unmarshal(raw, &tf); err != nil {
		return nil, fmt.Errorf("tasks: parse %s: %w", path, err)
	}
	seen := make(map[string]bool, len(tf.Tasks))
	for i := range tf.Tasks {
		t := &tf.Tasks[i]
		switch {
		case t.ID == "":
			return nil, fmt.Errorf("tasks: task #%d has empty id", i)
		case seen[t.ID]:
			return nil, fmt.Errorf("tasks: duplicate id %q", t.ID)
		case t.Script == "":
			return nil, fmt.Errorf("tasks: task %q has empty script", t.ID)
		case t.RoleRequired == "":
			return nil, fmt.Errorf("tasks: task %q has empty role_required", t.ID)
		}
		seen[t.ID] = true
		t.Status = StatusPending
	}
	return tf.Tasks, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/overmind/tasks/ -v`
Expected: PASS.

- [ ] **Step 5: Build + lint**

Run: `go build ./... && golangci-lint run ./pkg/overmind/tasks/`
Expected: build OK; 0 issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/overmind/tasks/
git commit -m "feat(overmind): task model and seed-file loader"
```

---

### Task 4: Task store + assignment coordinator

**Files:**
- Create: `pkg/overmind/tasks/store.go`
- Create: `pkg/overmind/tasks/store_test.go`

**Interfaces:**
- Consumes: `Task`/`TaskStatus` (Task 3); `control.NewEnvelope`/`Assign`/`Envelope`/`Event` (Task 1); `supervisor.WorkerInfo`.
- Produces: `tasks.Sender` interface (`Send(agentID string, env control.Envelope) error`); `tasks.NewStore(ts []Task, logger *log.Logger) *Store`; `(*Store).AssignPending(workers []supervisor.WorkerInfo, sender Sender)`; `(*Store).HandleEvent(agentID string, ev control.Event)`; `(*Store).Snapshot() []Task`.

- [ ] **Step 1: Write the failing test**

Create `pkg/overmind/tasks/store_test.go`:

```go
package tasks

import (
	"log"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

type fakeSender struct{ sent map[string]control.Assign }

func (f *fakeSender) Send(agentID string, env control.Envelope) error {
	if f.sent == nil {
		f.sent = map[string]control.Assign{}
	}
	var a control.Assign
	_ = env.Into(&a)
	f.sent[agentID] = a
	return nil
}

func idleWorker(id, role string) supervisor.WorkerInfo {
	return supervisor.WorkerInfo{AgentID: id, Role: role, Healthy: true}
}

func newStore(ts ...Task) *Store {
	return NewStore(ts, log.New(io.Discard, "", 0))
}

func TestAssignByRoleToIdleWorker(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusPending})
	fs := &fakeSender{}
	s.AssignPending([]supervisor.WorkerInfo{idleWorker("explorer-1", "explorer"), idleWorker("miner-2", "miner")}, fs)
	if _, ok := fs.sent["miner-2"]; !ok {
		t.Fatalf("expected assign sent to miner-2, sent=%v", fs.sent)
	}
	if s.Snapshot()[0].Status != StatusAssigned || s.Snapshot()[0].AssignedTo != "miner-2" {
		t.Fatalf("task not marked assigned: %+v", s.Snapshot()[0])
	}
}

func TestAssignHonorsPin(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", AgentID: "miner-5", Status: StatusPending})
	fs := &fakeSender{}
	s.AssignPending([]supervisor.WorkerInfo{idleWorker("miner-2", "miner"), idleWorker("miner-5", "miner")}, fs)
	if _, ok := fs.sent["miner-5"]; !ok {
		t.Fatalf("expected assign to pinned miner-5, sent=%v", fs.sent)
	}
}

func TestAssignSkipsBusyWorker(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusPending})
	busy := idleWorker("miner-2", "miner")
	busy.LastStatus = control.Status{ActiveTaskID: "other"}
	fs := &fakeSender{}
	s.AssignPending([]supervisor.WorkerInfo{busy}, fs)
	if len(fs.sent) != 0 {
		t.Fatalf("expected no assignment to busy worker, sent=%v", fs.sent)
	}
	if s.Snapshot()[0].Status != StatusPending {
		t.Fatalf("task should stay pending, got %q", s.Snapshot()[0].Status)
	}
}

func TestHandleEventMarksDone(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusAssigned, AssignedTo: "miner-2"})
	s.HandleEvent("miner-2", control.Event{Kind: "task_done", Detail: "t1"})
	if s.Snapshot()[0].Status != StatusDone {
		t.Fatalf("task not marked done: %+v", s.Snapshot()[0])
	}
}

func TestHandleEventMarksFailed(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusAssigned, AssignedTo: "miner-2"})
	s.HandleEvent("miner-2", control.Event{Kind: "task_failed", Detail: "t1: jump failed"})
	if s.Snapshot()[0].Status != StatusFailed {
		t.Fatalf("task not marked failed: %+v", s.Snapshot()[0])
	}
}

func TestReassignsOnWorkerDeath(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusAssigned, AssignedTo: "miner-2"})
	fs := &fakeSender{}
	// miner-2 is gone from the snapshot (died); a fresh idle miner-9 is present.
	s.AssignPending([]supervisor.WorkerInfo{idleWorker("miner-9", "miner")}, fs)
	if _, ok := fs.sent["miner-9"]; !ok {
		t.Fatalf("expected reassignment to miner-9 after death, sent=%v", fs.sent)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/overmind/tasks/ -run 'TestAssign|TestHandleEvent|TestReassigns' 2>&1 | head`
Expected: build failure — `NewStore`/`Store`/`Sender` undefined.

- [ ] **Step 3: Implement**

Create `pkg/overmind/tasks/store.go`:

```go
package tasks

import (
	"log"
	"strings"
	"sync"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// Sender routes a control envelope to a named worker (satisfied by
// *supervisor.Server).
type Sender interface {
	Send(agentID string, env control.Envelope) error
}

// Store holds tasks in memory and matches pending ones to idle workers.
type Store struct {
	mu     sync.Mutex
	tasks  []Task
	logger *log.Logger
}

// NewStore wraps a loaded task slice.
func NewStore(ts []Task, logger *log.Logger) *Store {
	return &Store{tasks: ts, logger: logger}
}

// AssignPending reconciles assignments against the current fleet snapshot, then
// sends an Assign for each pending task to an eligible idle worker. Called each
// status tick. Tasks assigned to a worker absent from the snapshot (died) are
// returned to pending for reassignment.
func (s *Store) AssignPending(workers []supervisor.WorkerInfo, sender Sender) {
	s.mu.Lock()
	defer s.mu.Unlock()

	present := make(map[string]supervisor.WorkerInfo, len(workers))
	for _, w := range workers {
		present[w.AgentID] = w
	}

	// Reconcile: revert tasks whose assigned worker is gone (died/unregistered).
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.Status == StatusAssigned || t.Status == StatusRunning {
			if _, ok := present[t.AssignedTo]; !ok {
				s.logger.Printf("task %q: worker %q gone, returning to pending", t.ID, t.AssignedTo)
				t.Status = StatusPending
				t.AssignedTo = ""
			}
		}
	}

	// Track which workers are already busy this pass so we don't double-assign.
	busy := make(map[string]bool)
	for _, w := range workers {
		if w.LastStatus.ActiveTaskID != "" {
			busy[w.AgentID] = true
		}
	}
	for i := range s.tasks {
		if s.tasks[i].Status == StatusAssigned || s.tasks[i].Status == StatusRunning {
			busy[s.tasks[i].AssignedTo] = true
		}
	}

	for i := range s.tasks {
		t := &s.tasks[i]
		if t.Status != StatusPending {
			continue
		}
		worker, ok := s.pickWorker(t, workers, busy)
		if !ok {
			continue // none eligible this pass; retried next tick
		}
		env, err := control.NewEnvelope(control.TypeAssign, worker, control.Assign{
			TaskID: t.ID, Script: t.Script, Params: t.Params,
		})
		if err != nil {
			s.logger.Printf("task %q: build assign envelope: %v", t.ID, err)
			continue
		}
		if err := sender.Send(worker, env); err != nil {
			s.logger.Printf("task %q: send assign to %q: %v", t.ID, worker, err)
			continue
		}
		t.Status = StatusAssigned
		t.AssignedTo = worker
		busy[worker] = true
		s.logger.Printf("task %q assigned to %q", t.ID, worker)
	}
}

// pickWorker returns an eligible idle worker for t: the pinned AgentID if set
// and free, otherwise the first healthy, non-busy worker of the required role.
func (s *Store) pickWorker(t *Task, workers []supervisor.WorkerInfo, busy map[string]bool) (string, bool) {
	if t.AgentID != "" {
		for _, w := range workers {
			if w.AgentID == t.AgentID && w.Healthy && !busy[w.AgentID] {
				return w.AgentID, true
			}
		}
		return "", false
	}
	for _, w := range workers {
		if w.Role == t.RoleRequired && w.Healthy && !busy[w.AgentID] {
			return w.AgentID, true
		}
	}
	return "", false
}

// HandleEvent updates task status from a worker's task_done / task_failed event.
// The event Detail begins with the task id (see the worker's OnTaskResult).
func (s *Store) HandleEvent(agentID string, ev control.Event) {
	if ev.Kind != "task_done" && ev.Kind != "task_failed" {
		return
	}
	taskID := ev.Detail
	if i := strings.IndexByte(taskID, ':'); i >= 0 {
		taskID = taskID[:i]
	}
	taskID = strings.TrimSpace(taskID)

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.ID != taskID {
			continue
		}
		if ev.Kind == "task_done" {
			t.Status = StatusDone
		} else {
			t.Status = StatusFailed
		}
		s.logger.Printf("task %q -> %s (worker %q)", t.ID, t.Status, agentID)
		return
	}
}

// Snapshot returns a copy of all tasks.
func (s *Store) Snapshot() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, len(s.tasks))
	copy(out, s.tasks)
	return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/overmind/tasks/ -v`
Expected: PASS (all store + loader tests).

- [ ] **Step 5: Build + lint**

Run: `go build ./... && golangci-lint run ./pkg/overmind/tasks/`
Expected: build OK; 0 issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/overmind/tasks/store.go pkg/overmind/tasks/store_test.go
git commit -m "feat(overmind): task store with idle-worker assignment + event handling"
```

---

### Task 5: Worker task-aware standing loop

**Files:**
- Modify: `pkg/worker/standing.go`
- Test: `pkg/worker/standing_test.go`

**Interfaces:**
- Consumes: `SubstituteParams` (Task 2); `ResolveScriptArg`, `SplitScriptCommands`, existing `runLine` machinery.
- Produces: `worker.AssignedTask{ID, Script string; Params map[string]string}`; two new `StandingDeps` fields `NextTask func() *AssignedTask` and `OnTaskResult func(taskID string, err error)`; an internal `(StandingDeps).runTask`. `runLine` is refactored to return `error` (callers that ignore it keep current behavior).

- [ ] **Step 1: Write the failing test**

Add to `pkg/worker/standing_test.go` (this file already constructs `StandingDeps` with a fake runner; mirror its existing helpers — `Runner`, `ExecMu`, `Client`). Use the existing fake command runner in that file; if its name differs, adapt the variable names but keep the assertions.

```go
func TestRunStandingRunsAssignedTaskThenResumesIdle(t *testing.T) {
	// A miner-style task whose script lives in data/scripts is resolved by
	// ResolveScriptArg; here we point at an agent-local script via t.TempDir
	// is overkill — instead assert via a recording runner that the task's
	// commands run, then idle resumes. We inject the task through NextTask.
	rec := &recordingRunner{} // existing fake in this file: records dispatched commands
	var execMu sync.Mutex
	delivered := false
	var gotTaskID string
	var gotErr error

	task := &AssignedTask{ID: "t1", Script: "mining_run", Params: map[string]string{"TARGET_SYSTEM": "bunda", "COUNT": "2"}}

	deps := StandingDeps{
		Runner: rec,
		Client: stubStateClient{}, // existing fake returning a *game.State
		ExecMu: &execMu,
		Out:    io.Discard,
		AgentID: "miner-1",
		IdleInterval:     time.Millisecond,
		ScheduleInterval: time.Hour,
		NextTask: func() *AssignedTask {
			if delivered {
				return nil
			}
			delivered = true
			return task
		},
		OnTaskResult: func(id string, err error) { gotTaskID, gotErr = id, err },
	}

	// Role with a distinct idle command so we can tell idle from task work.
	role := Role{Idle: "get_status"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// let a couple of idle passes happen, then stop
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_ = RunStanding(ctx, role, deps)

	if gotTaskID != "t1" {
		t.Fatalf("OnTaskResult task id = %q, want t1", gotTaskID)
	}
	if gotErr != nil {
		t.Fatalf("OnTaskResult err = %v, want nil", gotErr)
	}
	// The mining_run script issues an autopilot to the substituted target.
	if !rec.ranCommandContaining("autopilot", "bunda") {
		t.Fatalf("expected task autopilot to bunda; recorded=%v", rec.commands())
	}
	// After the task, idle (get_status) must have run on a later pass.
	if !rec.ranCommandContaining("get_status") {
		t.Fatalf("expected idle get_status to resume; recorded=%v", rec.commands())
	}
}
```

> Implementer note: this test depends on `data/scripts/mining_run.smolt` existing and on the existing fake-runner/stub-client helpers in `standing_test.go`. If `mining_run.smolt` is not yet present (it lands in Task 7), make this test self-contained by pointing the task at a temp agent-local script: write `data/agents/miner-1/scripts/mining_run.smolt` under `t.TempDir()`-based chdir, OR change `task.Script` to an inline temp script via `ResolveScriptArg`'s agent-local search path. The required assertion is unchanged: the task's substituted commands run, `OnTaskResult("t1", nil)` fires, and idle resumes. Match the helper names actually present in `standing_test.go`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run TestRunStandingRunsAssignedTask 2>&1 | head`
Expected: build failure — `AssignedTask` / `NextTask` / `OnTaskResult` undefined.

- [ ] **Step 3: Add the task fields, refactor `runLine` to return error, add `runTask`**

In `pkg/worker/standing.go`:

Add the task struct near the top (after imports):

```go
// AssignedTask is a one-shot task handed to the worker by the overmind. It is a
// local mirror of control.Assign so pkg/worker does not import pkg/overmind/control.
type AssignedTask struct {
	ID     string
	Script string
	Params map[string]string
}
```

Add two fields to `StandingDeps`:

```go
	// Task hooks (nil when the worker has no control channel). NextTask returns
	// and consumes the pending assigned task (or nil); OnTaskResult reports a
	// finished task's id and error (nil = success).
	NextTask     func() *AssignedTask
	OnTaskResult func(taskID string, err error)
```

Change the idle loop body (currently the `deps.ExecMu.Lock(); for _, line := range idleCmds {…}; deps.ExecMu.Unlock()` block) to prefer a task when present:

```go
		deps.ExecMu.Lock()
		if task := deps.nextTask(); task != nil {
			deps.runTask(ctx, task)
		} else {
			for _, line := range idleCmds {
				select {
				case <-ctx.Done():
					deps.ExecMu.Unlock()
					return nil
				default:
				}
				_ = deps.runLine(ctx, line)
			}
		}
		deps.ExecMu.Unlock()
```

Add the helpers at the end of the file:

```go
// nextTask returns the pending assigned task, or nil when there is no task hook
// or nothing pending.
func (deps StandingDeps) nextTask() *AssignedTask {
	if deps.NextTask == nil {
		return nil
	}
	return deps.NextTask()
}

// runTask resolves the task's script, substitutes its params, runs the lines
// once (stopping at the first error), and reports the result. Must be called
// with deps.ExecMu held.
func (deps StandingDeps) runTask(ctx context.Context, task *AssignedTask) {
	report := func(err error) {
		if deps.OnTaskResult != nil {
			deps.OnTaskResult(task.ID, err)
		}
	}
	path, ok := ResolveScriptArg(task.Script, deps.AgentID)
	if !ok {
		report(fmt.Errorf("task %q: script %q not found", task.ID, task.Script))
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		report(fmt.Errorf("task %q: read script: %w", task.ID, err))
		return
	}
	lines, err := SplitScriptCommands(string(content))
	if err != nil {
		report(fmt.Errorf("task %q: parse script: %w", task.ID, err))
		return
	}
	lines = SubstituteParams(lines, task.Params)
	fmt.Fprintf(deps.Out, "▶ task %s: running %s (%d lines)\n", task.ID, task.Script, len(lines)) //nolint:errcheck
	for _, line := range lines {
		if ctx.Err() != nil {
			report(ctx.Err())
			return
		}
		if e := deps.runLine(ctx, line); e != nil {
			report(fmt.Errorf("task %q: %q: %w", task.ID, line, e))
			return
		}
	}
	report(nil)
}
```

Refactor `runLine` to return `error`. Change its signature to `func (deps StandingDeps) runLine(ctx context.Context, line string) error`, return `nil` on success and the dispatch error on failure (keep the existing `fmt.Fprintf(deps.Out, "standing: %q: %v\n", …)` log, then `return derr`). Update its two existing call sites to ignore the result: the scheduler `run` closure (`deps.runLine(ctx, t.Command)` → `_ = deps.runLine(ctx, t.Command)`) and any other existing caller.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/worker/ -run TestRunStanding -v`
Expected: PASS. Then the full package: `go test ./pkg/worker/`.

- [ ] **Step 5: Build + lint**

Run: `go build ./... && golangci-lint run ./pkg/worker/`
Expected: build OK; 0 issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/standing.go pkg/worker/standing_test.go
git commit -m "feat(worker): task-aware standing loop runs assigned task then resumes idle"
```

---

### Task 6: Worker control wiring — receive Assign, report result, set ActiveTaskID

**Files:**
- Modify: `cmd/worker/main.go`

**Interfaces:**
- Consumes: `control.TypeAssign`/`Assign` (Task 1); `worker.AssignedTask` + `StandingDeps.NextTask`/`OnTaskResult` (Task 5); existing `sendEnvelope(enc, type, agentID, payload)`, `buildStatus(st, standing, taskID, now)`.

- [ ] **Step 1: Add task plumbing state (above the reader goroutine, near `var paused atomic.Bool`)**

```go
	var pendingTask atomic.Pointer[worker.AssignedTask]
	var activeTaskID atomic.Pointer[string]
```

- [ ] **Step 2: Handle `TypeAssign` in the control reader switch**

Add a case alongside `TypePause`/`TypeResume` in the reader goroutine's `switch env.Type`:

```go
				case control.TypeAssign:
					var as control.Assign
					if intoErr := env.Into(&as); intoErr != nil {
						logger.Printf("warning: decode assign payload: %v", intoErr)
						break
					}
					logger.Printf("received task %q (script=%s)", as.TaskID, as.Script)
					pendingTask.Store(&worker.AssignedTask{ID: as.TaskID, Script: as.Script, Params: as.Params})
```

- [ ] **Step 3: Wire `NextTask`/`OnTaskResult` into the `StandingDeps` construction**

Where `worker.StandingDeps{…}` is built (the block with `ExecMu`, `Paused`, etc.), add:

```go
			NextTask: func() *worker.AssignedTask {
				t := pendingTask.Swap(nil)
				if t != nil {
					id := t.ID
					activeTaskID.Store(&id)
				}
				return t
			},
			OnTaskResult: func(taskID string, err error) {
				kind := "task_done"
				detail := taskID
				if err != nil {
					kind = "task_failed"
					detail = taskID + ": " + err.Error()
				}
				if sendErr := sendEnvelope(enc, control.TypeEvent, *agentID, control.Event{
					Kind: kind, Detail: detail, Timestamp: time.Now().Format(time.RFC3339Nano),
				}); sendErr != nil {
					logger.Printf("warning: send task event: %v", sendErr)
				}
				activeTaskID.Store(nil)
				logger.Printf("task %s finished: %s", taskID, kind)
			},
```

> Note: `enc` (the `*control.Encoder`) is in scope where the standing deps are built in the socket branch. If the deps are constructed before `enc`, move the `enc :=` / encoder creation above the deps block. Confirm by reading the surrounding code.

- [ ] **Step 4: Feed the live task id into the status heartbeat**

In the heartbeat `case <-ticker.C:` block, replace the fixed `activeTaskID` argument to `buildStatus` with the live value:

```go
				tid := ""
				if p := activeTaskID.Load(); p != nil {
					tid = *p
				}
				status := buildStatus(nowState, standing, tid, time.Now())
```

(Remove or shadow the old local `activeTaskID` string from `savedIntent` so it does not conflict with the new `atomic.Pointer[string]`; the assigned-task id now drives `ActiveTaskID`. If checkpoint resume needs the saved id, seed `activeTaskID.Store(&savedIntent.ActiveTaskID)` once at startup when non-empty.)

- [ ] **Step 5: Build + lint + worker tests**

Run: `go build ./... && golangci-lint run ./cmd/worker/ && go test ./cmd/worker/ ./pkg/worker/`
Expected: build OK; 0 issues; tests PASS. (cmd/worker has no unit tests for the loop; the build + pkg/worker tests are the gate. A wrong wiring fails the build.)

- [ ] **Step 6: Commit**

```bash
git add cmd/worker/main.go
git commit -m "feat(worker): receive Assign, run task, report result, surface ActiveTaskID"
```

---

### Task 7: Overmind wiring + mining-run script + miner role

**Files:**
- Modify: `cmd/overmind/main.go`
- Create: `data/scripts/mining_run.smolt`
- Modify: `.gitignore`
- Modify: `data/overmind/roles.yaml`
- Create: `data/overmind/tasks.yaml` (example seed; safe default = empty list)

**Interfaces:**
- Consumes: `tasks.LoadTasks`/`NewStore`/`AssignPending`/`HandleEvent` (Tasks 3–4); `srv.SetEventHook`, `fleet.Snapshot`, the existing status ticker; dispatch commands `autopilot/travel/mine/dock/deposit_all` (existing).

- [ ] **Step 1: Create the mining-run script**

Create `data/scripts/mining_run.smolt`:

```
autopilot $TARGET_SYSTEM$
travel $ASTEROID_BELT$
loop -f $COUNT$ mine
travel $STATION$
dock
deposit_all
```

- [ ] **Step 2: Allowlist it in `.gitignore`**

Add after the existing `!data/scripts/` script negations (e.g. after `!data/scripts/explore.smolt`):

```
!data/scripts/mining_run.smolt
```

Verify: `git check-ignore data/scripts/mining_run.smolt && echo IGNORED || echo TRACKED` → expect `TRACKED`.

- [ ] **Step 3: Add the `miner` role to `data/overmind/roles.yaml`**

Under `roles:` (sibling of `resident`/`explorer`):

```yaml
  miner:
    idle: get_status
```

(No schedule; idle is a cheap no-op while the miner waits for assigned tasks.)

- [ ] **Step 4: Create an example seed `data/overmind/tasks.yaml`**

```yaml
# Seed tasks assigned at overmind startup. Empty list = no tasks (fleet still
# runs standing behaviors). Add mining runs here, e.g.:
# tasks:
#   - id: mine-bunda-iron
#     script: mining_run
#     role_required: miner
#     params: { TARGET_SYSTEM: bunda, COUNT: "20" }
tasks: []
```

(Track this file; it is under `data/overmind/` which is not blanket-ignored — confirm with `git check-ignore`.)

- [ ] **Step 5: Wire the task store into `cmd/overmind/main.go`**

Add a `--tasks` flag near the other flags:

```go
	tasksPath := flag.String("tasks", "data/overmind/tasks.yaml", "Path to the assigned-task seed file")
```

After the supervisor is built (around the `sup := …` block), load tasks and build the store:

```go
	var taskStore *tasks.Store
	if loaded, terr := tasks.LoadTasks(*tasksPath); terr != nil {
		logger.Printf("tasks: %v (continuing with no tasks)", terr)
		taskStore = tasks.NewStore(nil, logger)
	} else {
		logger.Printf("loaded %d task(s) from %s", len(loaded), *tasksPath)
		taskStore = tasks.NewStore(loaded, logger)
	}
	srv.SetEventHook(func(agentID string, ev control.Event) {
		taskStore.HandleEvent(agentID, ev)
	})
```

In the status ticker (`case <-ticker.C:`), add the assignment pass next to the snapshot log:

```go
		case <-ticker.C:
			snap := fleet.Snapshot()
			taskStore.AssignPending(snap, srv)
			logFleetSnapshot(logger, snap)
```

Add the import `"github.com/rsned/spacemolt/pkg/overmind/tasks"`.

- [ ] **Step 6: Build, lint, and run the drift guard**

Run:
```
go build ./... && golangci-lint run ./cmd/overmind/ ./pkg/overmind/...
go test ./pkg/worker/ -run TestSeededCommandsAreDispatchable -v
go test ./pkg/overmind/...
```
Expected: build OK; 0 lint issues; `TestSeededCommandsAreDispatchable` PASS (mining_run's commands — autopilot/travel/mine/dock/deposit_all — are already dispatchable); overmind suite PASS.

- [ ] **Step 7: Rebuild binaries**

```bash
go build -o bin/overmind ./cmd/overmind && go build -o bin/worker ./cmd/worker
```

- [ ] **Step 8: Commit**

```bash
git add cmd/overmind/main.go data/scripts/mining_run.smolt .gitignore data/overmind/roles.yaml data/overmind/tasks.yaml
git commit -m "feat(overmind): wire task store/assignment + mining_run script + miner role"
```

---

## Self-Review

**Spec coverage:**
- `control.Assign` message → Task 1.
- `$KEY$` param substitution (`SubstituteParams`) → Task 2.
- Task model + `tasks.yaml` seed loader (validation, pending default) → Task 3.
- Task store + assignment (pin / role / idle-only / none-eligible) + status transitions + worker-death reassignment → Task 4.
- Worker task-aware standing loop (run task once, report, resume idle; failure path) on one ExecMu → Task 5.
- Worker control wiring (receive Assign, emit task_done/failed, surface ActiveTaskID) → Task 6.
- Overmind wiring (load seed, event hook, AssignPending in ticker) + `mining_run.smolt` + `miner` role + `.gitignore` + seed file → Task 7.
- Error handling: no-eligible (stays pending, Task 4), script failure (task_failed, Tasks 5/6), worker death (reassign, Task 4), malformed tasks.yaml (continue with no tasks, Task 7).
- Testing: loader, SubstituteParams, assignment matching + transitions, Assign codec, task-aware loop, drift guard — Tasks 1–7.

**Placeholder scan:** Task 5's test carries an explicit implementer note because it depends on helper names already in `standing_test.go` and on `mining_run.smolt` (Task 7) — the note gives a concrete self-contained fallback (agent-local temp script) and a fixed assertion, so it is not an open-ended placeholder. All other steps carry complete code.

**Type consistency:** `AssignedTask{ID,Script,Params}` defined in Task 5, produced by Task 6's reader, consumed by Task 5's loop. `control.Assign{TaskID,Script,Params}` (Task 1) ↔ `worker.AssignedTask` mapping in Task 6. `tasks.Task`/`TaskStatus` (Task 3) consumed by Task 4 + Task 7. `Sender.Send(agentID, control.Envelope) error` (Task 4) satisfied by `*supervisor.Server.Send` (existing). `Store.AssignPending([]supervisor.WorkerInfo, Sender)` / `HandleEvent(string, control.Event)` (Task 4) called from Task 7. `OnTaskResult(taskID, err)` event `Detail` = `taskID` (success) or `taskID + ": " + err` (failure); `Store.HandleEvent` parses the id by splitting on `':'` — consistent.

**Ordering note:** Task 5's test is most robust once Task 7's `mining_run.smolt` exists; if executed strictly in order, use the implementer note's agent-local temp-script fallback so Task 5 is self-contained. Tasks 1–4 are fully independent; 5→6 are worker-side; 7 is the overmind/config join.
