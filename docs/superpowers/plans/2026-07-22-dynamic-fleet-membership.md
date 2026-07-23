# Dynamic Fleet Membership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add/remove/update a single worker in a live overmind fleet — via a dashboard button or yaml-edit + SIGHUP — without a full drain+restart.

**Architecture:** One membership engine inside `Supervisor`: a mutex-guarded pending-request queue drained at the top of each reap tick (same pattern as the existing quarantine `releases`), so specs/restarts/proc state stay owned by the reap goroutine. Removal drains one worker, then stops it; adds pace through the existing `RestartBatch` login budget; spec changes rolling-restart one worker. Dashboard-removes persist via an overrides sidecar (`effective = yaml − overrides.removed`); the yaml is never machine-rewritten. Spec: `docs/superpowers/specs/2026-07-22-dynamic-fleet-membership-design.md`.

**Tech Stack:** Go 1.24 (backend), React 19 + TypeScript + Vite (frontend), NDJSON envelopes over unix sockets.

## Global Constraints

- The membership queue is drained ONLY at the start of `reapAndRestart`; `specs`, `restarts`, and `leaving` state are mutated ONLY from the reap goroutine (Roster() reads via `specsMu`).
- Removal is drain-then-stop: per-worker `TypeDrain` envelope, wait for `LastStatus.Drained`, force-stop after `RemoveDrainTimeout` (default `4 * time.Minute`). A worker with no live connection/process is stopped immediately.
- Adds and rolling-restart relaunches consume the existing `RestartBatch` budget (1/tick) — never burst logins.
- Effective roster = yaml specs − overrides.removed, applied identically at boot and on SIGHUP.
- Yaml parse failure on SIGHUP keeps the current roster (loud log, no changes). Corrupt/missing overrides file reads as empty.
- The fleet yaml is NEVER machine-rewritten. Only the dashboard writes the overrides file (atomic temp+rename).
- `admin_readd` resurrects only yaml-listed agents (`unknown_agent` otherwise). Duplicate membership requests collapse last-op-wins per agent.
- Admin socket connections never register in the server's `conns` map and close after one ack.
- All sleeps use `pkg/game/constants.go` constants; timeouts are plain `time.Duration` config fields like the existing ones.
- Tests must discriminate — prove by neutering the target behavior and observing red (repo vacuous-test lesson).
- New code passes `golangci-lint run` with no new findings. Commit with `--no-verify`; stage files explicitly (never `git add -A`).

---

## File map

| File | Change |
|---|---|
| `pkg/overmind/control/messages.go` | + `TypeAdminRemove/TypeAdminReadd/TypeAdminAck`, `AdminRequest`, `AdminAck` |
| `pkg/overmind/supervisor/fleet.go` | + `Leaving` flag, `MarkLeaving`, `Remove` |
| `pkg/overmind/supervisor/membership.go` (new) | `MembershipOp/MembershipRequest`, queue, remove/add/update lifecycle |
| `pkg/overmind/supervisor/supervisor.go` | queue drain in `reapAndRestart`, `Sender`, `specsMu`, `Roster()`, `RemoveDrainTimeout` |
| `pkg/overmind/supervisor/server.go` | admin envelope routing + `AdminHook` |
| `pkg/overmind/supervisor/overrides.go` (new) | `Overrides` load/save/subtract |
| `pkg/overmind/balances/balances.go` | `LiveRecord.Leaving`, `StatusFile.Removed`, `WriteStatus(live, removed, now)` |
| `cmd/overmind/main.go` + `membership.go` (new) | SIGHUP, `diffSpecs`, roster state, boot subtraction, AdminHook wiring |
| `pkg/ovdash/snapshot.go` | `FleetDef.Socket`, `AgentState.Leaving`, `Snapshot.Removed` |
| `pkg/ovdash/admin.go` (new) | `AdminRemove/AdminReadd` (overrides edit + socket round-trip) |
| `cmd/overmind-dashboard/main.go` | POST remove/readd endpoints |
| `frontend/src/components/overmind/` | Remove button, `draining` chip, Removed section + Re-add |

---

### Task 1: Admin control envelopes

**Files:**
- Modify: `pkg/overmind/control/messages.go`
- Test: `pkg/overmind/control/messages_test.go`

**Interfaces:**
- Produces: `control.TypeAdminRemove`, `control.TypeAdminReadd`, `control.TypeAdminAck` (Type constants `"admin_remove"`, `"admin_readd"`, `"admin_ack"`); `control.AdminRequest{AgentID string}`; `control.AdminAck{AgentID, Status, Detail string}`; status constants `control.AckAccepted = "accepted"`, `control.AckUnknownAgent = "unknown_agent"`, `control.AckAlreadyPending = "already_pending"`.

- [ ] **Step 1: Write the failing test** (append to `messages_test.go`, matching its existing table style):

```go
func TestAdminEnvelopeRoundTrip(t *testing.T) {
	env, err := NewEnvelope(TypeAdminRemove, "craftsman-1", AdminRequest{AgentID: "craftsman-1"})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	var req AdminRequest
	if err := env.Into(&req); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if req.AgentID != "craftsman-1" {
		t.Fatalf("AgentID = %q, want craftsman-1", req.AgentID)
	}
	ack, err := NewEnvelope(TypeAdminAck, "craftsman-1", AdminAck{AgentID: "craftsman-1", Status: AckAccepted})
	if err != nil {
		t.Fatalf("NewEnvelope ack: %v", err)
	}
	var a AdminAck
	if err := ack.Into(&a); err != nil {
		t.Fatalf("Into ack: %v", err)
	}
	if a.Status != AckAccepted {
		t.Fatalf("Status = %q, want %q", a.Status, AckAccepted)
	}
}
```

- [ ] **Step 2: Run** `go test ./pkg/overmind/control/ -run TestAdminEnvelope -v` — FAIL (undefined: TypeAdminRemove).

- [ ] **Step 3: Implement** — in `messages.go`, extend the const block and add below `Assign`:

```go
	TypeAdminRemove Type = "admin_remove"
	TypeAdminReadd  Type = "admin_readd"
	TypeAdminAck    Type = "admin_ack"
```

```go
// AdminRequest asks the overmind to change fleet membership for one agent.
// Sent by an admin client (dashboard backend) instead of a Hello; the
// connection receives one AdminAck and closes.
type AdminRequest struct {
	AgentID string `json:"agent_id"`
}

// AdminAck statuses.
const (
	AckAccepted       = "accepted"
	AckUnknownAgent   = "unknown_agent"
	AckAlreadyPending = "already_pending"
)

// AdminAck is the overmind's reply to an AdminRequest.
type AdminAck struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
}
```

- [ ] **Step 4: Run** the test — PASS. Run `go build ./...`.
- [ ] **Step 5: Commit** `git add pkg/overmind/control/messages.go pkg/overmind/control/messages_test.go && git commit --no-verify -m 'feat(control): admin_remove/admin_readd/admin_ack envelopes'`

---

### Task 2: Fleet leaving/remove state

**Files:**
- Modify: `pkg/overmind/supervisor/fleet.go`
- Test: `pkg/overmind/supervisor/fleet_test.go`

**Interfaces:**
- Produces: `WorkerInfo.Leaving bool`; `(*Fleet).MarkLeaving(agentID string)`; `(*Fleet).ClearLeaving(agentID string)`; `(*Fleet).Remove(agentID string)` (drops the entry from the map entirely so it leaves `Snapshot()`).

- [ ] **Step 1: Write the failing test** (append to `fleet_test.go`):

```go
func TestFleetLeavingAndRemove(t *testing.T) {
	f := NewFleet()
	f.ApplyHello(control.Hello{AgentID: "a1", Role: "missionrunner"}, 42, time.Now())
	f.MarkLeaving("a1")
	snap := f.Snapshot()
	if len(snap) != 1 || !snap[0].Leaving {
		t.Fatalf("after MarkLeaving: snapshot = %+v, want one entry with Leaving=true", snap)
	}
	f.ClearLeaving("a1")
	if snap = f.Snapshot(); snap[0].Leaving {
		t.Fatal("ClearLeaving did not clear the flag")
	}
	f.MarkLeaving("a1")
	f.Remove("a1")
	if got := f.Snapshot(); len(got) != 0 {
		t.Fatalf("after Remove: snapshot has %d entries, want 0", len(got))
	}
	// Remove of an unknown agent must not create an entry.
	f.Remove("ghost")
	if got := f.Snapshot(); len(got) != 0 {
		t.Fatalf("Remove(ghost) created an entry: %+v", got)
	}
}
```

- [ ] **Step 2: Run** `go test ./pkg/overmind/supervisor/ -run TestFleetLeaving -v` — FAIL (unknown field Leaving).

- [ ] **Step 3: Implement** — in `fleet.go`: add to `WorkerInfo` (after `QuarantineReason`):

```go
	// Leaving means a membership-remove is in progress: the worker has been
	// sent a drain and will be stopped and dropped from the fleet once idle
	// (or after the remove-drain timeout). Surfaced to the dashboard as the
	// "draining" chip.
	Leaving bool
```

and the two methods (near Quarantine):

```go
// MarkLeaving flags a worker as being removed from the fleet (drain sent,
// stop pending). Creates the entry if absent so a booting worker can still
// be marked.
func (f *Fleet) MarkLeaving(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.get(agentID).Leaving = true
}

// ClearLeaving clears the removal-in-progress flag (rolling update: the same
// agent relaunches, so its entry stays and must not keep the draining chip).
func (f *Fleet) ClearLeaving(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w, ok := f.workers[agentID]; ok {
		w.Leaving = false
	}
}

// Remove drops a worker from the registry entirely (membership removal
// complete). Unknown ids are a no-op — Remove must not create entries.
func (f *Fleet) Remove(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.workers, agentID)
}
```

- [ ] **Step 4: Run** the test — PASS. `go test ./pkg/overmind/supervisor/ -count=1` — all PASS.
- [ ] **Step 5: Commit** `git add pkg/overmind/supervisor/fleet.go pkg/overmind/supervisor/fleet_test.go && git commit --no-verify -m 'feat(supervisor): fleet Leaving flag and Remove'`

---

### Task 3: Supervisor membership engine

**Files:**
- Create: `pkg/overmind/supervisor/membership.go`
- Modify: `pkg/overmind/supervisor/supervisor.go`
- Test: `pkg/overmind/supervisor/membership_test.go` (new)

**Interfaces:**
- Consumes: Task 2's `MarkLeaving`/`Remove`; existing `kill`, `launch`, `tryRestart`, `Server.Send`, `control.TypeDrain`.
- Produces:
  - `type MembershipOp string` with `MembershipAdd/MembershipRemove/MembershipUpdate` (`"add"`, `"remove"`, `"update"`).
  - `type MembershipRequest struct { Op MembershipOp; Spec WorkerSpec }` (for remove, only `Spec.AgentID` is meaningful).
  - `(*Supervisor).EnqueueMembership(req MembershipRequest)` — safe from any goroutine.
  - `(*Supervisor).Roster() []WorkerSpec` — copy of current specs, safe from any goroutine.
  - `Supervisor.Sender ControlSender` field (`type ControlSender interface { Send(agentID string, env control.Envelope) error }`) — `*Server` satisfies it; tests inject a fake.
  - `Supervisor.RemoveDrainTimeout time.Duration` (default `4 * time.Minute`).

**Design (all reap-goroutine-owned):** `Supervisor` gains `memberMu sync.Mutex` + `pending []MembershipRequest`, `specsMu sync.Mutex` guarding `specs` mutations/reads, and `leaving map[string]*leavingState` where

```go
type leavingState struct {
	deadline time.Time       // force-stop after this
	relaunch *WorkerSpec     // non-nil => rolling update: relaunch after stop
}
```

`reapAndRestart` gains two phases at the top: (1) `applyMembership(ctx)` — drain the queue (collapse last-op-wins by AgentID, preserving first-seen order), dispatch each request; (2) `progressLeaving(ctx, now, &budget)` — check each leaving agent for drained/timeout, complete stops, fire relaunches through the budget. The main spec loop must `continue` past agents present in `leaving` (they are no longer restart candidates) — add `if s.leaving[spec.AgentID] != nil { continue }` right after the quarantine check.

- [ ] **Step 1: Write the failing tests** — create `membership_test.go`. Use a fake spawn that records launches without real processes and a fake sender that records drain envelopes. Model a live proc by launching via the fake spawn (`exec.Command("sleep", "60")` is NOT used — instead reuse the package's existing test helpers if `supervisor_test.go` has a fake-spawn pattern; **read `supervisor_test.go` first and reuse its helpers**. If none fit, use this pattern):

```go
type fakeSender struct {
	mu    sync.Mutex
	sent  []control.Envelope
}

func (f *fakeSender) Send(agentID string, env control.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	env.AgentID = agentID
	f.sent = append(f.sent, env)
	return nil
}

func (f *fakeSender) drains() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for _, e := range f.sent {
		if e.Type == control.TypeDrain {
			ids = append(ids, e.AgentID)
		}
	}
	return ids
}

// newTestSup builds a supervisor with a spawn that starts real do-nothing
// processes (so kill/exited plumbing works) and a fake sender.
func newTestSup(t *testing.T, specs []WorkerSpec) (*Supervisor, *fakeSender, *Fleet) {
	t.Helper()
	fleet := NewFleet()
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, "sleep", "60")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd, nil
	}
	sup := NewSupervisor(nil, fleet, specs, spawn, log.New(io.Discard, "", 0))
	sender := &fakeSender{}
	sup.Sender = sender
	sup.StaggerInterval = 0
	return sup, sender, fleet
}

func TestRemoveDrainsThenStops(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "a1", Role: "missionrunner"}}
	sup, sender, fleet := newTestSup(t, specs)
	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "a1", Role: "missionrunner"}, 1, time.Now())
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t", System: "sol"}, time.Now())

	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "a1"}})
	sup.Tick(ctx)

	// Phase 1: drain sent, worker marked leaving, process still alive.
	if got := sender.drains(); len(got) != 1 || got[0] != "a1" {
		t.Fatalf("drains = %v, want [a1]", got)
	}
	snap := fleet.Snapshot()
	if len(snap) != 1 || !snap[0].Leaving {
		t.Fatalf("after remove tick: %+v, want a1 Leaving", snap)
	}
	if p := procSnapshot(sup, "a1"); p == nil || !p.alive() {
		t.Fatal("worker was stopped before it drained")
	}

	// Worker reports drained -> next tick stops and removes it.
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t", System: "sol", Drained: true}, time.Now())
	sup.Tick(ctx)
	if p := procSnapshot(sup, "a1"); p != nil && p.alive() {
		t.Fatal("worker still alive after drained tick")
	}
	if got := fleet.Snapshot(); len(got) != 0 {
		t.Fatalf("fleet still has %+v after removal", got)
	}
	if got := sup.Roster(); len(got) != 0 {
		t.Fatalf("roster still has %+v after removal", got)
	}

	// The removed agent must NOT be respawned by later ticks.
	sup.Tick(ctx)
	if p := procSnapshot(sup, "a1"); p != nil && p.alive() {
		t.Fatal("removed agent was respawned")
	}
}

func TestRemoveForceStopsAfterTimeout(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "a1", Role: "missionrunner"}}
	sup, _, fleet := newTestSup(t, specs)
	sup.RemoveDrainTimeout = -time.Second // already expired: force path
	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "a1", Role: "missionrunner"}, 1, time.Now())
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t"}, time.Now()) // never Drained

	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "a1"}})
	sup.Tick(ctx) // sends drain, deadline already past
	sup.Tick(ctx) // force-stop fires
	if p := procSnapshot(sup, "a1"); p != nil && p.alive() {
		t.Fatal("worker not force-stopped after timeout")
	}
	if got := len(sup.Roster()); got != 0 {
		t.Fatalf("roster size = %d, want 0", got)
	}
}

func TestAddLaunchesThroughBudgetAndResetsRestarts(t *testing.T) {
	ctx := t.Context()
	sup, _, _ := newTestSup(t, nil)
	sup.restarts["b1"] = 7 // stale counter from a previous life
	sup.EnqueueMembership(MembershipRequest{Op: MembershipAdd, Spec: WorkerSpec{AgentID: "b1", Role: "missionrunner"}})
	sup.EnqueueMembership(MembershipRequest{Op: MembershipAdd, Spec: WorkerSpec{AgentID: "b2", Role: "missionrunner"}})
	sup.Tick(ctx)
	alive := 0
	for _, id := range []string{"b1", "b2"} {
		if p := procSnapshot(sup, id); p != nil && p.alive() {
			alive++
		}
	}
	if alive != 1 {
		t.Fatalf("launched %d workers in one tick, want 1 (RestartBatch budget)", alive)
	}
	if got := len(sup.Roster()); got != 2 {
		t.Fatalf("roster size = %d, want 2 (both specs recorded)", got)
	}
	// memberAdd cleared the stale counter of 7; the budgeted launch itself
	// then counts as restart #1 via tryRestart. Stale-clear is what matters.
	if got := sup.restarts["b1"]; got > 1 {
		t.Fatalf("restarts[b1] = %d, want <=1 (stale counter must be cleared on add)", got)
	}
	sup.Tick(ctx)
	for _, id := range []string{"b1", "b2"} {
		if p := procSnapshot(sup, id); p == nil || !p.alive() {
			t.Fatalf("%s not launched by second tick", id)
		}
	}
}

func TestUpdateRollsOneWorker(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "a1", Role: "missionrunner", FreightMaxPackages: 3}}
	sup, sender, fleet := newTestSup(t, specs)
	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "a1", Role: "missionrunner"}, 1, time.Now())
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t"}, time.Now())

	updated := WorkerSpec{AgentID: "a1", Role: "missionrunner", FreightMaxPackages: 7}
	sup.EnqueueMembership(MembershipRequest{Op: MembershipUpdate, Spec: updated})
	sup.Tick(ctx)
	if got := sender.drains(); len(got) != 1 {
		t.Fatalf("drains = %v, want one for a1", got)
	}
	old := procSnapshot(sup, "a1")
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t", Drained: true}, time.Now())
	sup.Tick(ctx) // stop
	sup.Tick(ctx) // relaunch through budget
	fresh := procSnapshot(sup, "a1")
	if fresh == nil || !fresh.alive() || fresh == old {
		t.Fatal("update did not relaunch a fresh process")
	}
	roster := sup.Roster()
	if len(roster) != 1 || roster[0].FreightMaxPackages != 7 {
		t.Fatalf("roster = %+v, want a1 with FreightMaxPackages 7", roster)
	}
}

func TestDuplicateRequestsCollapseLastOpWins(t *testing.T) {
	ctx := t.Context()
	sup, sender, _ := newTestSup(t, nil)
	sup.EnqueueMembership(MembershipRequest{Op: MembershipAdd, Spec: WorkerSpec{AgentID: "c1"}})
	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "c1"}})
	sup.Tick(ctx)
	// Last op wins: remove of a never-launched agent completes immediately, no drain needed.
	if got := len(sup.Roster()); got != 0 {
		t.Fatalf("roster = %d entries, want 0", got)
	}
	if got := sender.drains(); len(got) != 0 {
		t.Fatalf("drains = %v, want none (no live worker)", got)
	}
}

func TestRemoveQuarantinedIsBookkeepingOnly(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "q1", Role: "missionrunner"}}
	sup, sender, fleet := newTestSup(t, specs)
	fleet.Quarantine("q1", "stranded")
	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "q1"}})
	sup.Tick(ctx)
	if got := sender.drains(); len(got) != 0 {
		t.Fatalf("drain sent to quarantined worker: %v", got)
	}
	if got := len(sup.Roster()); got != 0 {
		t.Fatalf("roster = %d, want 0", got)
	}
	if got := fleet.Snapshot(); len(got) != 0 {
		t.Fatalf("fleet still has %+v", got)
	}
}
```

- [ ] **Step 2: Run** `go test ./pkg/overmind/supervisor/ -run 'TestRemove|TestAdd|TestUpdate|TestDuplicate' -v` — FAIL (undefined: MembershipRequest).

- [ ] **Step 3: Implement** — create `membership.go`:

```go
package supervisor

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// MembershipOp identifies a live-roster change.
type MembershipOp string

// Membership operations.
const (
	MembershipAdd    MembershipOp = "add"
	MembershipRemove MembershipOp = "remove"
	MembershipUpdate MembershipOp = "update"
)

// MembershipRequest asks the supervisor to change the roster for one agent.
// For MembershipRemove only Spec.AgentID is meaningful.
type MembershipRequest struct {
	Op   MembershipOp
	Spec WorkerSpec
}

// ControlSender delivers a control envelope to one connected worker.
// *Server satisfies it; tests inject fakes.
type ControlSender interface {
	Send(agentID string, env control.Envelope) error
}

// leavingState tracks one in-progress removal, owned by the reap goroutine.
type leavingState struct {
	deadline time.Time   // force-stop once past this
	relaunch *WorkerSpec // non-nil => rolling update: relaunch after the stop
}

// EnqueueMembership queues a roster change for the next reap tick. Safe from
// any goroutine.
func (s *Supervisor) EnqueueMembership(req MembershipRequest) {
	s.memberMu.Lock()
	defer s.memberMu.Unlock()
	s.pending = append(s.pending, req)
}

// Roster returns a copy of the current specs. Safe from any goroutine.
func (s *Supervisor) Roster() []WorkerSpec {
	s.specsMu.Lock()
	defer s.specsMu.Unlock()
	out := make([]WorkerSpec, len(s.specs))
	copy(out, s.specs)
	return out
}

func (s *Supervisor) drainMembership() []MembershipRequest {
	s.memberMu.Lock()
	defer s.memberMu.Unlock()
	out := s.pending
	s.pending = nil
	return out
}

// collapseRequests keeps the LAST request per agent, preserving first-seen
// agent order.
func collapseRequests(reqs []MembershipRequest) []MembershipRequest {
	last := make(map[string]MembershipRequest, len(reqs))
	var order []string
	for _, r := range reqs {
		if _, seen := last[r.Spec.AgentID]; !seen {
			order = append(order, r.Spec.AgentID)
		}
		last[r.Spec.AgentID] = r
	}
	out := make([]MembershipRequest, 0, len(order))
	for _, id := range order {
		out = append(out, last[id])
	}
	return out
}

// applyMembership dispatches queued roster changes. Reap-goroutine only.
func (s *Supervisor) applyMembership(now time.Time) {
	for _, req := range collapseRequests(s.drainMembership()) {
		switch req.Op {
		case MembershipAdd:
			s.memberAdd(req.Spec)
		case MembershipRemove:
			s.memberRemove(req.Spec.AgentID, nil, now)
		case MembershipUpdate:
			spec := req.Spec
			s.memberRemove(spec.AgentID, &spec, now)
		}
	}
}

// memberAdd records the spec; the launch itself happens in the main reap loop
// (no tracked proc -> tryRestart), which enforces the RestartBatch budget.
func (s *Supervisor) memberAdd(spec WorkerSpec) {
	if s.hasSpec(spec.AgentID) {
		s.setSpec(spec) // add of an existing agent refreshes its spec
		return
	}
	s.specsMu.Lock()
	s.specs = append(s.specs, spec)
	s.specsMu.Unlock()
	delete(s.restarts, spec.AgentID) // fresh life: reset the crash-loop counter
	s.logger.Printf("membership: added %q to roster", spec.AgentID)
}

// memberRemove starts (or immediately completes) a removal. relaunch non-nil
// makes this a rolling update. Reap-goroutine only.
func (s *Supervisor) memberRemove(agentID string, relaunch *WorkerSpec, now time.Time) {
	if !s.hasSpec(agentID) && relaunch == nil {
		s.logger.Printf("membership: remove %q ignored (not in roster)", agentID)
		return
	}
	if s.fleet.IsQuarantined(agentID) {
		// Already stopped; bookkeeping only.
		s.completeRemoval(agentID, relaunch)
		return
	}
	proc := procSnapshot(s, agentID)
	if proc == nil || !proc.alive() {
		s.completeRemoval(agentID, relaunch)
		return
	}
	if s.leaving[agentID] != nil {
		return // removal already in progress
	}
	s.fleet.MarkLeaving(agentID)
	s.leaving[agentID] = &leavingState{deadline: now.Add(s.RemoveDrainTimeout), relaunch: relaunch}
	if s.Sender != nil {
		if err := s.Sender.Send(agentID, control.Envelope{Type: control.TypeDrain, AgentID: agentID}); err != nil {
			s.logger.Printf("membership: drain to %q failed (%v); will force-stop at deadline", agentID, err)
		}
	}
	s.logger.Printf("membership: removing %q — drain sent, force-stop after %s", agentID, s.RemoveDrainTimeout)
}

// progressLeaving advances in-flight removals: stop when drained or past the
// deadline, then complete (and relaunch updates through the budget).
// Reap-goroutine only.
func (s *Supervisor) progressLeaving(ctx context.Context, now time.Time, budget *int) {
	if len(s.leaving) == 0 {
		return
	}
	status := make(map[string]WorkerInfo)
	for _, w := range s.fleet.Snapshot() {
		status[w.AgentID] = w
	}
	for agentID, st := range s.leaving {
		w, seen := status[agentID]
		drained := seen && w.LastStatus.Drained
		proc := procSnapshot(s, agentID)
		gone := proc == nil || !proc.alive()
		if !gone && !drained && now.Before(st.deadline) {
			continue // still draining
		}
		if !gone {
			s.kill(proc)
		}
		relaunch := st.relaunch
		delete(s.leaving, agentID)
		s.completeRemoval(agentID, relaunch)
		if relaunch != nil {
			// Relaunch through the budget so rolling updates never burst logins.
			s.tryRestart(ctx, *relaunch, true, budget)
		}
	}
}

// completeRemoval clears every trace of the agent; if relaunch is non-nil the
// spec is re-recorded so the relaunch (or a later reap tick) can spawn it.
func (s *Supervisor) completeRemoval(agentID string, relaunch *WorkerSpec) {
	s.specsMu.Lock()
	kept := s.specs[:0]
	for _, sp := range s.specs {
		if sp.AgentID != agentID {
			kept = append(kept, sp)
		}
	}
	s.specs = kept
	if relaunch != nil {
		s.specs = append(s.specs, *relaunch)
	}
	s.specsMu.Unlock()

	s.procMu.Lock()
	delete(s.procs, agentID)
	s.procMu.Unlock()

	delete(s.restarts, agentID)
	if relaunch == nil {
		s.fleet.Remove(agentID)
		s.logger.Printf("membership: %q removed from fleet", agentID)
	} else {
		s.fleet.ClearQuarantine(agentID) // also resets stall counters for the fresh life
		s.fleet.ClearLeaving(agentID)    // entry stays: drop the draining chip
		s.logger.Printf("membership: %q spec updated; relaunching", agentID)
	}
}

func (s *Supervisor) hasSpec(agentID string) bool {
	s.specsMu.Lock()
	defer s.specsMu.Unlock()
	for _, sp := range s.specs {
		if sp.AgentID == agentID {
			return true
		}
	}
	return false
}

func (s *Supervisor) setSpec(spec WorkerSpec) {
	s.specsMu.Lock()
	defer s.specsMu.Unlock()
	for i, sp := range s.specs {
		if sp.AgentID == spec.AgentID {
			s.specs[i] = spec
			return
		}
	}
}
```

In `supervisor.go`:
- Add fields to `Supervisor` (near `releases`):

```go
	// Membership: queued roster changes applied at reap-tick start (same
	// single-goroutine ownership pattern as releases). leaving tracks
	// in-progress removals; RemoveDrainTimeout bounds the per-worker drain
	// wait before force-stop. Sender delivers per-worker control envelopes
	// (the *Server in production; a fake in tests).
	memberMu sync.Mutex
	pending  []MembershipRequest
	specsMu  sync.Mutex
	leaving  map[string]*leavingState
	Sender   ControlSender
	RemoveDrainTimeout time.Duration
```

- In `NewSupervisor`: `leaving: make(map[string]*leavingState)`, `RemoveDrainTimeout: 4 * time.Minute`, and after construction `if server != nil { s.Sender = server }` (build the struct into a variable first; the typed-nil guard matters).
- In `reapAndRestart`, insert at the very top (before the releases drain):

```go
	now0 := time.Now()
	s.applyMembership(now0)
```

(`membership.go` then needs no `context` import unless `progressLeaving` keeps it — it does, for `tryRestart`.)

and after the `budget` is computed (immediately after the `if budget <= 0` block):

```go
	s.progressLeaving(ctx, now0, &budget)
```

- In the main spec loop, right after the quarantine `continue`:

```go
		if s.leaving[spec.AgentID] != nil {
			continue // removal in progress; not a restart candidate
		}
```

- Change every direct iteration/mutation of `s.specs` in `supervisor.go` (`Run`'s initial loop reads once at start — snapshot it via `s.Roster()`; `reapAndRestart`'s `for _, spec := range s.specs` and the `budget = len(s.specs)` default → use a local `roster := s.Roster()`).

- [ ] **Step 4: Run** `go test ./pkg/overmind/supervisor/ -count=1 -race -v -run 'TestRemove|TestAdd|TestUpdate|TestDuplicate|TestFleet'` — PASS. Then the whole package `-count=1 -race` — all PASS (existing supervisor tests must not regress).
- [ ] **Step 5: Discrimination check** — temporarily comment out the `s.progressLeaving(...)` call; `TestRemoveDrainsThenStops` must fail (worker never stopped). Restore.
- [ ] **Step 6: Commit** `git add pkg/overmind/supervisor/membership.go pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/membership_test.go && git commit --no-verify -m 'feat(supervisor): membership engine — queued add/remove/update with drain-then-stop'`

---

### Task 4: Server admin routing

**Files:**
- Modify: `pkg/overmind/supervisor/server.go`
- Test: `pkg/overmind/supervisor/server_test.go`

**Interfaces:**
- Produces: `Server.SetAdminHook(h func(op control.Type, agentID string) control.AdminAck)`. `handleConn` routes `TypeAdminRemove`/`TypeAdminReadd` to the hook, writes one `TypeAdminAck` envelope, and returns (connection closes; never registered in `conns`).

- [ ] **Step 1: Write the failing test** (append to `server_test.go`; follow its existing socket-dial pattern — read the file first and reuse its helpers for starting a Server on a temp socket):

```go
func TestAdminConnRoutedAndAcked(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")
	fleet := NewFleet()
	srv, err := NewServer(sock, fleet, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	var gotOp control.Type
	var gotID string
	srv.SetAdminHook(func(op control.Type, agentID string) control.AdminAck {
		gotOp, gotID = op, agentID
		return control.AdminAck{AgentID: agentID, Status: control.AckAccepted}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go srv.Serve(ctx) //nolint:errcheck

	conn, err := net.DialTimeout("unix", sock, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	enc := control.NewEncoder(conn)
	env, _ := control.NewEnvelope(control.TypeAdminRemove, "a1", control.AdminRequest{AgentID: "a1"})
	if err := enc.Encode(env); err != nil {
		t.Fatalf("encode: %v", err)
	}
	dec := control.NewDecoder(conn)
	reply, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if reply.Type != control.TypeAdminAck {
		t.Fatalf("reply type = %q, want admin_ack", reply.Type)
	}
	var ack control.AdminAck
	if err := reply.Into(&ack); err != nil {
		t.Fatalf("into: %v", err)
	}
	if ack.Status != control.AckAccepted || gotOp != control.TypeAdminRemove || gotID != "a1" {
		t.Fatalf("ack=%+v gotOp=%q gotID=%q", ack, gotOp, gotID)
	}
	// The admin conn must not have registered as a worker connection.
	if err := srv.Send("a1", control.Envelope{Type: control.TypeDrain}); err == nil {
		t.Fatal("admin conn registered in conns: Send should fail for a1")
	}
}
```

- [ ] **Step 2: Run** `go test ./pkg/overmind/supervisor/ -run TestAdminConn -v` — FAIL (SetAdminHook undefined).

- [ ] **Step 3: Implement** — in `server.go`: add field `adminHook func(op control.Type, agentID string) control.AdminAck` beside `eventHook`, plus:

```go
// SetAdminHook installs the callback for admin membership envelopes.
func (s *Server) SetAdminHook(h func(op control.Type, agentID string) control.AdminAck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adminHook = h
}
```

In `handleConn`'s switch, before `default:`:

```go
		case control.TypeAdminRemove, control.TypeAdminReadd:
			var req control.AdminRequest
			if err := env.Into(&req); err != nil {
				s.logger.Printf("bad admin request: %v", err)
				return
			}
			s.mu.RLock()
			hook := s.adminHook
			s.mu.RUnlock()
			ack := control.AdminAck{AgentID: req.AgentID, Status: control.AckUnknownAgent, Detail: "no admin hook installed"}
			if hook != nil {
				ack = hook(env.Type, req.AgentID)
			}
			reply, err := control.NewEnvelope(control.TypeAdminAck, req.AgentID, ack)
			if err == nil {
				err = enc.Encode(reply)
			}
			if err != nil {
				s.logger.Printf("admin ack write failed: %v", err)
			}
			return // one request per admin connection; close without registering
```

- [ ] **Step 4: Run** the test — PASS; whole package `-count=1` — PASS.
- [ ] **Step 5: Commit** `git add pkg/overmind/supervisor/server.go pkg/overmind/supervisor/server_test.go && git commit --no-verify -m 'feat(supervisor): route admin membership envelopes to hook with ack'`

---

### Task 5: Overrides sidecar

**Files:**
- Create: `pkg/overmind/supervisor/overrides.go`
- Test: `pkg/overmind/supervisor/overrides_test.go` (new)

**Interfaces:**
- Produces:

```go
type Overrides struct {
	Removed   []string `json:"removed"`
	UpdatedAt string   `json:"updated_at,omitempty"`
	By        string   `json:"by,omitempty"`
}
func LoadOverrides(path string) Overrides           // missing/corrupt -> empty (corrupt logs via returned bool? see below)
func SaveOverrides(path string, o Overrides) error  // atomic temp+rename
func SubtractOverrides(specs []WorkerSpec, o Overrides) []WorkerSpec
func (o Overrides) IsRemoved(agentID string) bool
func (o *Overrides) Add(agentID string)    // idempotent
func (o *Overrides) Delete(agentID string) // idempotent
```

`LoadOverrides` signature: `func LoadOverrides(path string) (Overrides, error)` — missing file returns `(Overrides{}, nil)`; corrupt file returns `(Overrides{}, err)` so callers can log loudly and continue with empty (spec: corrupt reads as empty, loud log).

- [ ] **Step 1: Write the failing test:**

```go
func TestOverridesRoundTripAndSubtract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-overrides.json")

	// Missing file -> empty, no error.
	o, err := LoadOverrides(path)
	if err != nil || len(o.Removed) != 0 {
		t.Fatalf("missing file: o=%+v err=%v, want empty/nil", o, err)
	}

	o.Add("craftsman-1")
	o.Add("craftsman-1") // idempotent
	if err := SaveOverrides(path, o); err != nil {
		t.Fatalf("save: %v", err)
	}
	back, err := LoadOverrides(path)
	if err != nil || !back.IsRemoved("craftsman-1") || len(back.Removed) != 1 {
		t.Fatalf("reload: %+v err=%v", back, err)
	}

	specs := []WorkerSpec{{AgentID: "craftsman-1"}, {AgentID: "fighter-4"}}
	eff := SubtractOverrides(specs, back)
	if len(eff) != 1 || eff[0].AgentID != "fighter-4" {
		t.Fatalf("subtract: %+v, want only fighter-4", eff)
	}

	back.Delete("craftsman-1")
	if back.IsRemoved("craftsman-1") {
		t.Fatal("delete did not remove")
	}

	// Corrupt file -> empty + error (caller logs and continues).
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	o2, err := LoadOverrides(path)
	if err == nil || len(o2.Removed) != 0 {
		t.Fatalf("corrupt: o=%+v err=%v, want empty + error", o2, err)
	}
}
```

- [ ] **Step 2: Run** — FAIL (undefined: LoadOverrides).

- [ ] **Step 3: Implement** `overrides.go`:

```go
package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Overrides is the dashboard-written membership sidecar: agents in Removed are
// subtracted from the yaml roster at boot and on SIGHUP. The fleet yaml itself
// is never machine-rewritten; this file is the only machine-written state.
type Overrides struct {
	Removed   []string `json:"removed"`
	UpdatedAt string   `json:"updated_at,omitempty"`
	By        string   `json:"by,omitempty"`
}

// IsRemoved reports whether agentID is override-removed.
func (o Overrides) IsRemoved(agentID string) bool {
	return slices.Contains(o.Removed, agentID)
}

// Add records agentID as removed (idempotent).
func (o *Overrides) Add(agentID string) {
	if !o.IsRemoved(agentID) {
		o.Removed = append(o.Removed, agentID)
	}
}

// Delete clears agentID from the removed set (idempotent).
func (o *Overrides) Delete(agentID string) {
	o.Removed = slices.DeleteFunc(o.Removed, func(id string) bool { return id == agentID })
}

// LoadOverrides reads the sidecar. A missing file is an empty override set; a
// corrupt file returns empty plus the error so the caller can log loudly and
// proceed (never block a fleet on a bad sidecar).
func LoadOverrides(path string) (Overrides, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Overrides{}, nil
	}
	if err != nil {
		return Overrides{}, fmt.Errorf("supervisor: read overrides: %w", err)
	}
	var o Overrides
	if err := json.Unmarshal(raw, &o); err != nil {
		return Overrides{}, fmt.Errorf("supervisor: parse overrides %s: %w", path, err)
	}
	return o, nil
}

// SaveOverrides writes the sidecar atomically (temp file + rename).
func SaveOverrides(path string, o Overrides) error {
	raw, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return fmt.Errorf("supervisor: marshal overrides: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".overrides-*.tmp")
	if err != nil {
		return fmt.Errorf("supervisor: temp overrides: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("supervisor: write overrides: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("supervisor: close overrides: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("supervisor: rename overrides: %w", err)
	}
	return nil
}

// SubtractOverrides returns specs minus the override-removed agents.
func SubtractOverrides(specs []WorkerSpec, o Overrides) []WorkerSpec {
	out := make([]WorkerSpec, 0, len(specs))
	for _, sp := range specs {
		if !o.IsRemoved(sp.AgentID) {
			out = append(out, sp)
		}
	}
	return out
}
```

- [ ] **Step 4: Run** the test — PASS.
- [ ] **Step 5: Commit** `git add pkg/overmind/supervisor/overrides.go pkg/overmind/supervisor/overrides_test.go && git commit --no-verify -m 'feat(supervisor): overrides sidecar (load/save/subtract)'`

---

### Task 6: cmd/overmind — SIGHUP diff, boot subtraction, status writer, AdminHook wiring

**Files:**
- Create: `cmd/overmind/membership.go`
- Modify: `cmd/overmind/main.go` (signal switch at :127-140 area; boot roster load ~where `LoadFleet` is called; `recordBalances` at :239)
- Modify: `pkg/overmind/balances/balances.go` (`LiveRecord` + `StatusFile` + `WriteStatus`)
- Test: `cmd/overmind/membership_test.go` (new), extend `pkg/overmind/balances/balances_test.go`

**Interfaces:**
- Consumes: `supervisor.LoadFleet`, `LoadOverrides`, `SubtractOverrides`, `(*Supervisor).Roster()/EnqueueMembership`, `(*Server).SetAdminHook`, control ack constants.
- Produces:

```go
// cmd/overmind/membership.go
type rosterState struct {           // guards the latest yaml+overrides view
	mu        sync.Mutex
	yamlSpecs []supervisor.WorkerSpec
	overrides supervisor.Overrides
}
func diffSpecs(current, desired []supervisor.WorkerSpec) []supervisor.MembershipRequest
func (rs *rosterState) reload(fleetPath, overridesPath string, logger *log.Logger) ([]supervisor.WorkerSpec, bool)
func makeAdminHook(rs *rosterState, sup *supervisor.Supervisor, overridesPath string, logger *log.Logger) func(control.Type, string) control.AdminAck
```

- `balances.LiveRecord` gains `Leaving bool \`json:"leaving,omitempty"\``; `balances.StatusFile` gains `Removed []string \`json:"removed,omitempty"\``; signature becomes `WriteStatus(live []LiveRecord, removed []string, now time.Time) error`.
- Overrides path derivation: `overridesPath := strings.TrimSuffix(*socketPath, ".sock") + "-overrides.json"` (so `data/overmind/mission-learn.sock` → `data/overmind/mission-learn-overrides.json`), overridable via new flag `--overrides-file`.

- [ ] **Step 1: Write the failing tests** (`cmd/overmind/membership_test.go`):

```go
func TestDiffSpecs(t *testing.T) {
	cur := []supervisor.WorkerSpec{
		{AgentID: "keep", Role: "missionrunner"},
		{AgentID: "drop", Role: "missionrunner"},
		{AgentID: "change", Role: "missionrunner", FreightMaxPackages: 3},
	}
	des := []supervisor.WorkerSpec{
		{AgentID: "keep", Role: "missionrunner"},
		{AgentID: "change", Role: "missionrunner", FreightMaxPackages: 7},
		{AgentID: "new", Role: "missionrunner"},
	}
	got := diffSpecs(cur, des)
	want := map[string]supervisor.MembershipOp{
		"drop": supervisor.MembershipRemove,
		"change": supervisor.MembershipUpdate,
		"new": supervisor.MembershipAdd,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d requests (%+v), want %d", len(got), got, len(want))
	}
	for _, r := range got {
		if want[r.Spec.AgentID] != r.Op {
			t.Fatalf("agent %q: op %q, want %q", r.Spec.AgentID, r.Op, want[r.Spec.AgentID])
		}
		if r.Spec.AgentID == "change" && r.Spec.FreightMaxPackages != 7 {
			t.Fatalf("update carried stale spec: %+v", r.Spec)
		}
	}
}

func TestReloadKeepsRosterOnParseError(t *testing.T) {
	dir := t.TempDir()
	fleetPath := filepath.Join(dir, "fleet.yaml")
	overridesPath := filepath.Join(dir, "fleet-overrides.json")
	good := "workers:\n  - { agent_id: a1, role: missionrunner, station: \"\" }\n"
	if err := os.WriteFile(fleetPath, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := &rosterState{}
	logger := log.New(io.Discard, "", 0)
	eff, ok := rs.reload(fleetPath, overridesPath, logger)
	if !ok || len(eff) != 1 {
		t.Fatalf("good reload: eff=%+v ok=%v", eff, ok)
	}
	if err := os.WriteFile(fleetPath, []byte("workers: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := rs.reload(fleetPath, overridesPath, logger); ok {
		t.Fatal("parse error must return ok=false (keep current roster)")
	}
}

func TestAdminHook(t *testing.T) {
	dir := t.TempDir()
	overridesPath := filepath.Join(dir, "o.json")
	rs := &rosterState{yamlSpecs: []supervisor.WorkerSpec{{AgentID: "a1", Role: "missionrunner"}}}
	fleet := supervisor.NewFleet()
	sup := supervisor.NewSupervisor(nil, fleet, nil, func(ctx context.Context, spec supervisor.WorkerSpec, socket string) (*exec.Cmd, error) {
		return nil, nil
	}, log.New(io.Discard, "", 0))
	hook := makeAdminHook(rs, sup, overridesPath, log.New(io.Discard, "", 0))

	if ack := hook(control.TypeAdminRemove, "ghost"); ack.Status != control.AckUnknownAgent {
		t.Fatalf("remove ghost: %+v, want unknown_agent", ack)
	}
	if ack := hook(control.TypeAdminRemove, "a1"); ack.Status != control.AckAccepted {
		t.Fatalf("remove a1: %+v, want accepted", ack)
	}
	if ack := hook(control.TypeAdminReadd, "a1"); ack.Status != control.AckAccepted {
		t.Fatalf("readd a1: %+v, want accepted", ack)
	}
	if ack := hook(control.TypeAdminReadd, "ghost"); ack.Status != control.AckUnknownAgent {
		t.Fatalf("readd ghost: %+v, want unknown_agent (not in yaml)", ack)
	}
}
```

- [ ] **Step 2: Run** `go test ./cmd/overmind/ -run 'TestDiffSpecs|TestReload|TestAdminHook' -v` — FAIL (undefined).

- [ ] **Step 3: Implement** `cmd/overmind/membership.go`:

```go
package main

import (
	"log"
	"reflect"
	"sync"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// rosterState holds the latest yaml + overrides view shared between the
// SIGHUP handler, the admin hook, and the status writer.
type rosterState struct {
	mu        sync.Mutex
	yamlSpecs []supervisor.WorkerSpec
	overrides supervisor.Overrides
}

// reload re-reads the fleet yaml and overrides sidecar and returns the
// effective roster. ok=false means the yaml failed to parse — the caller MUST
// keep the current roster (loud log already emitted). A corrupt overrides
// file degrades to empty with a loud log (spec: never block a fleet on a bad
// sidecar).
func (rs *rosterState) reload(fleetPath, overridesPath string, logger *log.Logger) ([]supervisor.WorkerSpec, bool) {
	specs, err := supervisor.LoadFleet(fleetPath)
	if err != nil {
		logger.Printf("SIGHUP: FLEET YAML UNREADABLE (%v) — keeping current roster", err)
		return nil, false
	}
	ov, err := supervisor.LoadOverrides(overridesPath)
	if err != nil {
		logger.Printf("SIGHUP: overrides unreadable (%v) — treating as empty", err)
		ov = supervisor.Overrides{}
	}
	rs.mu.Lock()
	rs.yamlSpecs = specs
	rs.overrides = ov
	rs.mu.Unlock()
	return supervisor.SubtractOverrides(specs, ov), true
}

// removedList returns a copy of the current override-removed ids.
func (rs *rosterState) removedList() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]string, len(rs.overrides.Removed))
	copy(out, rs.overrides.Removed)
	return out
}

// yamlSpec looks up an agent's spec in the latest yaml copy.
func (rs *rosterState) yamlSpec(agentID string) (supervisor.WorkerSpec, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, sp := range rs.yamlSpecs {
		if sp.AgentID == agentID {
			return sp, true
		}
	}
	return supervisor.WorkerSpec{}, false
}

// diffSpecs computes the membership requests that turn current into desired.
func diffSpecs(current, desired []supervisor.WorkerSpec) []supervisor.MembershipRequest {
	curBy := make(map[string]supervisor.WorkerSpec, len(current))
	for _, sp := range current {
		curBy[sp.AgentID] = sp
	}
	var reqs []supervisor.MembershipRequest
	seen := make(map[string]bool, len(desired))
	for _, want := range desired {
		seen[want.AgentID] = true
		have, ok := curBy[want.AgentID]
		switch {
		case !ok:
			reqs = append(reqs, supervisor.MembershipRequest{Op: supervisor.MembershipAdd, Spec: want})
		case !reflect.DeepEqual(have, want):
			reqs = append(reqs, supervisor.MembershipRequest{Op: supervisor.MembershipUpdate, Spec: want})
		}
	}
	for _, have := range current {
		if !seen[have.AgentID] {
			reqs = append(reqs, supervisor.MembershipRequest{Op: supervisor.MembershipRemove, Spec: supervisor.WorkerSpec{AgentID: have.AgentID}})
		}
	}
	return reqs
}

// makeAdminHook builds the server admin callback: remove records the override
// is NOT done here (the dashboard owns the sidecar); this hook only enqueues
// the live change and acks. readd resolves the spec from the latest yaml copy.
func makeAdminHook(rs *rosterState, sup *supervisor.Supervisor, overridesPath string, logger *log.Logger) func(control.Type, string) control.AdminAck {
	return func(op control.Type, agentID string) control.AdminAck {
		switch op {
		case control.TypeAdminRemove:
			if _, ok := rs.yamlSpec(agentID); !ok {
				// Not in yaml — still allow removing a live roster member
				// (e.g. added earlier via a now-edited yaml) if present.
				found := false
				for _, sp := range sup.Roster() {
					if sp.AgentID == agentID {
						found = true
						break
					}
				}
				if !found {
					return control.AdminAck{AgentID: agentID, Status: control.AckUnknownAgent}
				}
			}
			// Track locally so the status writer's removed list is fresh even
			// before the dashboard's sidecar write is re-read.
			rs.mu.Lock()
			rs.overrides.Add(agentID)
			rs.mu.Unlock()
			sup.EnqueueMembership(supervisor.MembershipRequest{Op: supervisor.MembershipRemove, Spec: supervisor.WorkerSpec{AgentID: agentID}})
			logger.Printf("admin: remove %q enqueued", agentID)
			return control.AdminAck{AgentID: agentID, Status: control.AckAccepted}
		case control.TypeAdminReadd:
			spec, ok := rs.yamlSpec(agentID)
			if !ok {
				return control.AdminAck{AgentID: agentID, Status: control.AckUnknownAgent, Detail: "agent not in fleet yaml"}
			}
			rs.mu.Lock()
			rs.overrides.Delete(agentID)
			rs.mu.Unlock()
			sup.EnqueueMembership(supervisor.MembershipRequest{Op: supervisor.MembershipAdd, Spec: spec})
			logger.Printf("admin: readd %q enqueued", agentID)
			return control.AdminAck{AgentID: agentID, Status: control.AckAccepted}
		default:
			return control.AdminAck{AgentID: agentID, Status: control.AckUnknownAgent, Detail: "unsupported op"}
		}
	}
}
```

In `main.go`:
- New flag beside the others: `overridesPath := flag.String("overrides-file", "", "Membership overrides sidecar (default: <socket dir>/<fleet>-overrides.json)")`; after `*fleetName` is derived: `if *overridesPath == "" { *overridesPath = strings.TrimSuffix(*socketPath, ".sock") + "-overrides.json" }`.
- Boot: replace the direct `LoadFleet` result with the subtraction — build `rs := &rosterState{}`, call `eff, ok := rs.reload(*fleetPath, *overridesPath, logger)`; if `!ok` exit fatal (boot, unlike SIGHUP, must not run with an unknown roster); pass `eff` to `NewSupervisor`.
- Wire the hook after supervisor construction: `srv.SetAdminHook(makeAdminHook(rs, sup, *overridesPath, logger))`.
- Signal switch: add `syscall.SIGHUP` to `signal.Notify(...)` and:

```go
			case syscall.SIGHUP:
				logger.Printf("received SIGHUP: reloading fleet roster")
				if eff, ok := rs.reload(*fleetPath, *overridesPath, logger); ok {
					reqs := diffSpecs(sup.Roster(), eff)
					for _, r := range reqs {
						sup.EnqueueMembership(r)
					}
					logger.Printf("SIGHUP: %d membership change(s) enqueued", len(reqs))
				}
```

- Status writer: in `pkg/overmind/balances/balances.go` add `Leaving bool \`json:"leaving,omitempty"\`` to `LiveRecord`, `Removed []string \`json:"removed,omitempty"\`` to `StatusFile`, change `WriteStatus(live []LiveRecord, removed []string, now time.Time)` to stamp it; in `recordBalances` add `Leaving: w.Leaving,` to the LiveRecord literal and change the call to `recorder.WriteStatus(live, removedIDs, now)` — thread `removedIDs := rs.removedList()` through `recordBalances`'s signature (`recordBalances(ctx, logger, recorder, marketCol, snap, rs.removedList())`). Fix `WriteStatus`'s other callers/tests (grep `WriteStatus(` — balances tests).

- [ ] **Step 4: Run** `go test ./cmd/overmind/ ./pkg/overmind/... -count=1` — PASS; `go build ./...` — clean.
- [ ] **Step 5: Commit** `git add cmd/overmind/membership.go cmd/overmind/membership_test.go cmd/overmind/main.go pkg/overmind/balances/balances.go pkg/overmind/balances/balances_test.go && git commit --no-verify -m 'feat(overmind): SIGHUP roster diff, boot subtraction, admin hook, leaving/removed in status file'`

---

### Task 7: Dashboard backend — admin endpoints + snapshot fields

**Files:**
- Create: `pkg/ovdash/admin.go`, `pkg/ovdash/admin_test.go`
- Modify: `pkg/ovdash/snapshot.go` (FleetDef.Socket, AgentState.Leaving, Snapshot.Removed)
- Modify: `cmd/overmind-dashboard/main.go` (two POST routes)
- Test: `pkg/ovdash/snapshot_test.go` additions; `cmd/overmind-dashboard/main_test.go` additions

**Interfaces:**
- Consumes: `supervisor.LoadOverrides/SaveOverrides/Overrides`, `control` envelopes.
- Produces:

```go
// pkg/ovdash/admin.go
type AdminResult struct {
	Status  string `json:"status"`  // accepted | unknown_agent | already_pending | recorded_offline
	Detail  string `json:"detail,omitempty"`
}
func AdminRemove(dir, fleetLabel, agentID string) (AdminResult, error)
func AdminReadd(dir, fleetLabel, agentID string) (AdminResult, error)
func fleetByLabel(label string) (FleetDef, bool)
```

- `FleetDef` gains `Socket string` (basename of the fleet's unix socket in the same dir as the status files). Registry values: haul→`"haul.sock"`, mb→`"mb.sock"`, shuttle→`"shuttle.sock"`, assist→`"assist.sock"`, craft→`"craft.sock"`, Missions→`"mission-learn.sock"` (**read the actual `Fleets` var first and set Socket per entry; labels/order must not change**). Overrides basename = `strings.TrimSuffix(Socket, ".sock") + "-overrides.json"`.
- `AgentState` gains `Leaving bool \`json:"leaving,omitempty"\`` (mapped in `ReadSnapshot` from the status file's per-worker `leaving`). `Snapshot` gains `Removed map[string][]string \`json:"removed,omitempty"\`` (fleet label → removed ids, read from each fleet's overrides sidecar in `ReadSnapshot`; missing sidecar → absent key).

**AdminRemove flow (AdminReadd is symmetric with Delete + `admin_readd`):** load overrides from `filepath.Join(dir, overridesBase)` → `Add(agentID)` → stamp `UpdatedAt` (RFC3339 now) + `By: "dashboard"` → `SaveOverrides` → dial `filepath.Join(dir, def.Socket)` with `net.DialTimeout("unix", ..., 3*time.Second)` → send `TypeAdminRemove` envelope → read one reply with a 5s read deadline → map ack to AdminResult. Dial/read failure is NOT an error: return `AdminResult{Status: "recorded_offline", Detail: "overrides recorded; applies at next overmind start"}` (spec's degraded mode). Only overrides-save failures return a non-nil error.

- [ ] **Step 1: Write the failing test** (`pkg/ovdash/admin_test.go`) — spin a real unix-socket listener in the test that speaks one ack, plus the socket-down path:

```go
func TestAdminRemoveRoundTripAndOffline(t *testing.T) {
	dir := t.TempDir()
	def, ok := fleetByLabel("haul")
	if !ok {
		t.Fatal("haul fleet missing from registry")
	}
	sock := filepath.Join(dir, def.Socket)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		dec := control.NewDecoder(conn)
		env, err := dec.Decode()
		if err != nil || env.Type != control.TypeAdminRemove {
			return
		}
		var req control.AdminRequest
		_ = env.Into(&req)
		reply, _ := control.NewEnvelope(control.TypeAdminAck, req.AgentID, control.AdminAck{AgentID: req.AgentID, Status: control.AckAccepted})
		_ = control.NewEncoder(conn).Encode(reply)
	}()

	res, err := AdminRemove(dir, "haul", "trader-9")
	if err != nil {
		t.Fatalf("AdminRemove: %v", err)
	}
	if res.Status != control.AckAccepted {
		t.Fatalf("status = %q, want accepted", res.Status)
	}
	ov, err := supervisor.LoadOverrides(filepath.Join(dir, "haul-overrides.json"))
	if err != nil || !ov.IsRemoved("trader-9") {
		t.Fatalf("overrides not recorded: %+v err=%v", ov, err)
	}

	// Socket down: override still recorded, degraded status.
	res2, err := AdminRemove(dir, "mb", "marketbot_001")
	if err != nil {
		t.Fatalf("offline AdminRemove: %v", err)
	}
	if res2.Status != "recorded_offline" {
		t.Fatalf("offline status = %q, want recorded_offline", res2.Status)
	}
	ov2, _ := supervisor.LoadOverrides(filepath.Join(dir, "mb-overrides.json"))
	if !ov2.IsRemoved("marketbot_001") {
		t.Fatal("offline path did not record override")
	}

	// Readd clears the override.
	if _, err := AdminReadd(dir, "mb", "marketbot_001"); err != nil {
		t.Fatalf("AdminReadd: %v", err)
	}
	ov3, _ := supervisor.LoadOverrides(filepath.Join(dir, "mb-overrides.json"))
	if ov3.IsRemoved("marketbot_001") {
		t.Fatal("readd did not clear override")
	}
}
```

- [ ] **Step 2: Run** `go test ./pkg/ovdash/ -run TestAdminRemove -v` — FAIL.

- [ ] **Step 3: Implement** `pkg/ovdash/admin.go` per the flow above (complete file):

```go
package ovdash

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// AdminResult is the dashboard-facing outcome of a membership action.
type AdminResult struct {
	Status string `json:"status"` // accepted | unknown_agent | already_pending | recorded_offline
	Detail string `json:"detail,omitempty"`
}

// fleetByLabel finds a fleet registry entry by its UI label.
func fleetByLabel(label string) (FleetDef, bool) {
	for _, f := range Fleets {
		if f.Label == label {
			return f, true
		}
	}
	return FleetDef{}, false
}

// AdminRemove records agentID in the fleet's overrides sidecar, then asks the
// live overmind to remove it. A dead socket is the documented degraded mode:
// the override alone guarantees the removal applies at the next overmind boot.
func AdminRemove(dir, fleetLabel, agentID string) (AdminResult, error) {
	return adminOp(dir, fleetLabel, agentID, control.TypeAdminRemove)
}

// AdminReadd clears agentID from the overrides sidecar, then asks the live
// overmind to relaunch it from its yaml spec.
func AdminReadd(dir, fleetLabel, agentID string) (AdminResult, error) {
	return adminOp(dir, fleetLabel, agentID, control.TypeAdminReadd)
}

func adminOp(dir, fleetLabel, agentID string, op control.Type) (AdminResult, error) {
	def, ok := fleetByLabel(fleetLabel)
	if !ok {
		return AdminResult{}, fmt.Errorf("ovdash: unknown fleet %q", fleetLabel)
	}
	ovPath := filepath.Join(dir, strings.TrimSuffix(def.Socket, ".sock")+"-overrides.json")
	ov, err := supervisor.LoadOverrides(ovPath)
	if err != nil {
		// Corrupt sidecar: degrade to empty (matches overmind read semantics)
		// and let the save below rewrite it cleanly.
		ov = supervisor.Overrides{}
	}
	if op == control.TypeAdminRemove {
		ov.Add(agentID)
	} else {
		ov.Delete(agentID)
	}
	ov.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	ov.By = "dashboard"
	if err := supervisor.SaveOverrides(ovPath, ov); err != nil {
		return AdminResult{}, err //nolint:wrapcheck
	}

	conn, err := net.DialTimeout("unix", filepath.Join(dir, def.Socket), 3*time.Second)
	if err != nil {
		return AdminResult{Status: "recorded_offline", Detail: "overrides recorded; applies at next overmind start"}, nil
	}
	defer conn.Close() //nolint:errcheck
	env, err := control.NewEnvelope(op, agentID, control.AdminRequest{AgentID: agentID})
	if err != nil {
		return AdminResult{}, err //nolint:wrapcheck
	}
	if err := control.NewEncoder(conn).Encode(env); err != nil {
		return AdminResult{Status: "recorded_offline", Detail: "overrides recorded; send failed: " + err.Error()}, nil
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply, err := control.NewDecoder(conn).Decode()
	if err != nil {
		return AdminResult{Status: "recorded_offline", Detail: "overrides recorded; no ack: " + err.Error()}, nil
	}
	var ack control.AdminAck
	if err := reply.Into(&ack); err != nil {
		return AdminResult{Status: "recorded_offline", Detail: "overrides recorded; bad ack: " + err.Error()}, nil
	}
	return AdminResult{Status: ack.Status, Detail: ack.Detail}, nil
}
```

Snapshot additions (`snapshot.go`): add `Socket string` to `FleetDef` and set it on every registry entry (read the current `Fleets` var; haul entry `{File: "fleet", Label: "haul", ...}` gets `Socket: "haul.sock"`, the Missions entry gets `Socket: "mission-learn.sock"`, others `<label>.sock`). Add `Leaving bool \`json:"leaving,omitempty"\`` to `AgentState` and map it where `ReadSnapshot` builds AgentState from the status-file worker (the `leaving` JSON field lands via the balances.LiveRecord shape — mirror however `Docked`/`Healthy` are read). Add `Removed map[string][]string \`json:"removed,omitempty"\`` to `Snapshot`, populated in `ReadSnapshot` via `supervisor.LoadOverrides(filepath.Join(dir, ...))` per fleet (errors → skip fleet, no entry). Extend `snapshot_test.go` with a fixture status file containing `"leaving": true` + an overrides sidecar, asserting both surface.

Dashboard routes (`cmd/overmind-dashboard/main.go`, in `mux()`):

```go
	m.HandleFunc("POST /api/overmind/fleets/{fleet}/agents/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
		res, err := ovdash.AdminRemove(s.cfg.StatusDir, r.PathValue("fleet"), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONResp(w, res)
	})
	m.HandleFunc("POST /api/overmind/fleets/{fleet}/agents/{id}/readd", func(w http.ResponseWriter, r *http.Request) {
		res, err := ovdash.AdminReadd(s.cfg.StatusDir, r.PathValue("fleet"), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONResp(w, res)
	})
```

Add a handler test in `main_test.go` following its existing httptest pattern (POST to remove with a temp StatusDir; expect 200 + `recorded_offline` since no socket exists, and the overrides file created; POST to an unknown fleet expects 400).

- [ ] **Step 4: Run** `go test ./pkg/ovdash/ ./cmd/overmind-dashboard/ -count=1` — PASS; `go build ./...`.
- [ ] **Step 5: Commit** `git add pkg/ovdash/admin.go pkg/ovdash/admin_test.go pkg/ovdash/snapshot.go pkg/ovdash/snapshot_test.go cmd/overmind-dashboard/main.go cmd/overmind-dashboard/main_test.go && git commit --no-verify -m 'feat(ovdash): fleet membership admin endpoints (remove/readd) + leaving/removed in snapshot'`

---

### Task 8: Frontend — Remove/Re-add buttons, draining chip, Removed section

**Files:**
- Modify: `frontend/src/components/overmind/AgentCard.tsx` (remove button + draining chip)
- Modify: `frontend/src/components/overmind/FleetRail.tsx` (wire onRemove, Removed section + Re-add)
- Modify: `frontend/src/components/overmind/OvermindPage.tsx` (pass removed map + action callbacks; refresh after action)
- Modify: `frontend/src/types/` overmind types file (AgentState.leaving, Snapshot.removed — **find the existing type defs by grepping `agent_id` under `frontend/src/types/` and mirror the backend JSON**)

**Interfaces:**
- Consumes: `POST /api/overmind/fleets/{fleet}/agents/{id}/remove` and `/readd` returning `{status, detail?}`; snapshot fields `leaving` (per agent) and `removed` (fleet label → ids).

- [ ] **Step 1: Types + API helpers.** Add to the overmind types: `leaving?: boolean` on the agent state type; `removed?: Record<string, string[]>` on the snapshot type. Create `frontend/src/lib/fleetAdmin.ts`:

```ts
export async function removeAgent(fleet: string, agentId: string): Promise<{ status: string; detail?: string }> {
  const res = await fetch(`/api/overmind/fleets/${encodeURIComponent(fleet)}/agents/${encodeURIComponent(agentId)}/remove`, { method: 'POST' });
  if (!res.ok) throw new Error(`remove failed: ${res.status} ${await res.text()}`);
  return res.json();
}

export async function readdAgent(fleet: string, agentId: string): Promise<{ status: string; detail?: string }> {
  const res = await fetch(`/api/overmind/fleets/${encodeURIComponent(fleet)}/agents/${encodeURIComponent(agentId)}/readd`, { method: 'POST' });
  if (!res.ok) throw new Error(`readd failed: ${res.status} ${await res.text()}`);
  return res.json();
}
```

- [ ] **Step 2: AgentCard.** Read the component first; add (matching its existing class/style idiom): a small `✕` button in the card header, visible on hover, `title="Remove from fleet"`, calling `onRemove?.(agent)` with `e.stopPropagation()` (the card body remains the select click); when `agent.leaving` render a `draining` chip styled like the existing status chips (amber). New optional prop: `onRemove?: (agent: AgentState) => void`.

- [ ] **Step 3: FleetRail.** New props: `removed?: Record<string, string[]>`, `onRemove?: (agent: AgentState) => void`, `onReadd?: (fleet: string, agentId: string) => void`. Pass `onRemove` through to `AgentCard`. After each fleet's agent cards, when `removed?.[fleet]?.length`, render a `Removed` subsection: one muted row per id with a `Re-add` button calling `onReadd(fleet, id)`.

- [ ] **Step 4: OvermindPage.** Wire the callbacks with a `window.confirm` guard:

```tsx
const handleRemove = useCallback(async (agent: AgentState) => {
  if (!window.confirm(`Remove ${agent.agent_id} from the ${agent.fleet} fleet?\nIt will drain, stop, and stay out until re-added.`)) return;
  try {
    const res = await removeAgent(agent.fleet, agent.agent_id);
    if (res.status !== 'accepted') alert(`${agent.agent_id}: ${res.status}${res.detail ? ` — ${res.detail}` : ''}`);
  } catch (err) {
    alert(String(err));
  }
}, []);

const handleReadd = useCallback(async (fleet: string, agentId: string) => {
  try {
    const res = await readdAgent(fleet, agentId);
    if (res.status !== 'accepted') alert(`${agentId}: ${res.status}${res.detail ? ` — ${res.detail}` : ''}`);
  } catch (err) {
    alert(String(err));
  }
}, []);
```

Pass `removed={snapshot.removed}` (or however the snapshot state is named in the page — read it first) plus the two handlers into `FleetRail`. Snapshot polling/SSE already refreshes the rail, so the removed section and chips update on their own — no manual refetch.

- [ ] **Step 5: Build.** Run the frontend build (check `frontend/package.json` scripts; expect `npm run build --prefix frontend` or equivalent) — no type errors.

- [ ] **Step 6: Commit** `git add frontend/src/components/overmind/AgentCard.tsx frontend/src/components/overmind/FleetRail.tsx frontend/src/components/overmind/OvermindPage.tsx frontend/src/lib/fleetAdmin.ts frontend/src/types/ && git commit --no-verify -m 'feat(frontend): fleet remove/re-add buttons, draining chip, removed section'`

---

## Rollout (operator steps, not plan tasks)

1. `go build -o bin/overmind ./cmd/overmind && go build -o bin/overmind-dashboard ./cmd/overmind-dashboard` (+ frontend build; dist served from disk).
2. Restart each fleet overmind once on the new binary (the last full-restart-for-membership ever), per `reference_overmind_launch_commands`. Restart the dashboard.
3. First real use: re-add craftsman-1 after the 2026-07-22 manual triage — un-comment its yaml line, `kill -HUP <mission-learn overmind pid>`, watch "membership: added" in the log. Or exercise the button path end-to-end with a scratch removal first.
4. Batch ops (engineer-style pulls): edit yaml, one SIGHUP.

## Self-Review Notes

- Spec coverage: queue-at-tick (T3), per-worker drain-then-stop + timeout (T3), add-through-budget + restart reset (T3), rolling update (T3), admin envelopes + ack + no-registration (T1/T4), overrides sidecar + atomic write (T5), SIGHUP diff + parse-error-keeps-roster + boot subtraction (T6), status-file leaving/removed (T6), dashboard endpoints + degraded offline mode (T7), UI button/chip/removed-section (T8). Edge cases: re-add-while-draining lands as a queued add that `memberAdd` records and the reap loop launches after `completeRemoval` (T3 queue ordering); remove-unknown ack (T6 hook); drain-timeout (T3); corrupt overrides (T5/T6/T7).
- Type consistency: `MembershipRequest{Op, Spec}` used identically in T3/T6; `control.AdminAck{AgentID, Status, Detail}` in T1/T4/T6/T7; `WriteStatus(live, removed, now)` changed in T6 only; `FleetDef.Socket` defined and consumed in T7.
- Known judgment calls for implementers: reuse existing test helpers in `supervisor_test.go`/`server_test.go`/`main_test.go` where they exist (the plan's fakes are the fallback shape); `Fleets` registry Socket values must be set by reading the actual var, not assumed.
