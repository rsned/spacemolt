# Overmind Graceful Drain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an independent "drain" fleet state to the overmind — quiesce every worker to a docked/idle hold (each finishing its current work unit first), report a fleet-wide drained condition, with stop and resume as separate operator actions.

**Architecture:** Reuse the standing loop's existing top-of-pass gate (a second `Draining` gate beside `Paused`) and the level-triggered Status heartbeat (a new `Drained` bool). `SIGUSR1` broadcasts a new `control.TypeDrain`; workers finish their current idle-script pass, then hold and report `Drained=true`; the overmind polls `Fleet.DrainProgress` and logs. `SIGUSR2` broadcasts `control.TypeResume`; `SIGTERM` is the unchanged stop path (now clean).

**Tech Stack:** Go 1.24, standard library (`os/signal`, `sync/atomic`), the existing `pkg/overmind/control` NDJSON control channel.

## Global Constraints

- Go 1.24; `go build ./...` && `go test ./...` must pass before every commit.
- golangci-lint clean — no new findings (`golangci-lint run <pkg>`).
- Sleep/pause durations come from `pkg/game/constants.go` (e.g. `game.SleepShort`, `game.SleepMedium`) — never literal `time.Sleep` durations.
- Commit only drain-scoped files with explicit `git add <path>` — never `git add -A` (the tree has unrelated WIP).
- Commit messages end with: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Do NOT change the `pkg/game.GameClient` interface.
- Build any binaries to `bin/`, never the repo root.

## File Structure

- `pkg/overmind/control/messages.go` — add `TypeDrain` constant + `Status.Drained` field.
- `pkg/overmind/control/drain_test.go` (new) — wire round-trip tests.
- `pkg/worker/standing.go` — `StandingDeps.Draining` + `StandingDeps.SetDrained`; second gate in the loop.
- `pkg/worker/standing_test.go` — drain-gate tests.
- `cmd/worker/main.go` — `draining`/`drained` atomics; `TypeDrain` handling; `TypeResume` also clears draining; `buildStatus` gains a `drained` param.
- `cmd/worker/main_test.go` — update `buildStatus` call sites; add a drained-heartbeat test.
- `pkg/overmind/supervisor/fleet.go` — `Fleet.DrainProgress()`.
- `pkg/overmind/supervisor/fleet_test.go` — `DrainProgress` test (append; create if absent).
- `cmd/overmind/drain.go` (new) — `broadcast` fan-out helper + `drainComplete` predicate (testable, no signals/IO).
- `cmd/overmind/drain_test.go` (new) — helper tests.
- `cmd/overmind/main.go` — register `SIGUSR1`/`SIGUSR2`; signal loop dispatches drain/resume/stop.

---

### Task 1: Control wire — `TypeDrain` + `Status.Drained`

**Files:**
- Modify: `pkg/overmind/control/messages.go`
- Test: `pkg/overmind/control/drain_test.go` (new)

**Interfaces:**
- Produces: `control.TypeDrain Type = "drain"`; `control.Status.Drained bool` (json `drained,omitempty`). Consumed by Tasks 3, 4, 5.

- [ ] **Step 1: Write the failing tests**

Create `pkg/overmind/control/drain_test.go`:

```go
package control

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDrainEnvelopeRoundTrip(t *testing.T) {
	env, err := NewEnvelope(TypeDrain, "trader-1", nil)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(env); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := NewDecoder(&buf).Decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Type != TypeDrain {
		t.Fatalf("Type = %q, want %q", got.Type, TypeDrain)
	}
}

func TestStatusDrainedJSONRoundTrip(t *testing.T) {
	raw, err := json.Marshal(Status{Drained: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var st Status
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !st.Drained {
		t.Fatalf("Drained round-trip lost: %s", raw)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/overmind/control/ -run 'Drain|Drained' -v`
Expected: compile failure — `undefined: TypeDrain` and `unknown field Drained`.

- [ ] **Step 3: Add the constant and field**

In `pkg/overmind/control/messages.go`, add `TypeDrain` to the const block (after `TypeAssign`):

```go
	TypeAssign Type = "assign"
	TypeDrain  Type = "drain"
```

In the `Status` struct, add `Drained` after `ActiveTaskID`:

```go
	StandingBehavior string  `json:"standing_behavior"`
	ActiveTaskID     string  `json:"active_task_id"`
	Drained          bool    `json:"drained,omitempty"`
	Timestamp        string  `json:"timestamp"`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/overmind/control/ -run 'Drain|Drained' -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/overmind/control/messages.go pkg/overmind/control/drain_test.go
git commit -m "feat(overmind/control): add TypeDrain + Status.Drained wire fields

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Standing loop — `Draining` gate + `SetDrained`

**Files:**
- Modify: `pkg/worker/standing.go`
- Test: `pkg/worker/standing_test.go`

**Interfaces:**
- Produces: `StandingDeps.Draining func() bool` and `StandingDeps.SetDrained func(bool)`. The loop starts a new pass only when neither `Paused()` nor `Draining()` is true; while held it calls `SetDrained(draining)`, and before running a pass it calls `SetDrained(false)`. Consumed by Task 3 (`cmd/worker` injects `draining.Load` / `drained.Store`).

- [ ] **Step 1: Write the failing test**

Append to `pkg/worker/standing_test.go`:

```go
func TestRunStandingDrainingHoldsAndReportsDrained(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var draining atomic.Bool
	draining.Store(true)
	var drained atomic.Bool
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Draining: draining.Load, SetDrained: drained.Store, Out: io.Discard,
		NowFn: func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(20 * time.Millisecond)
	if n := len(r.snapshot()); n != 0 {
		t.Fatalf("draining worker ran %d commands, want 0", n)
	}
	if !drained.Load() {
		t.Fatal("expected SetDrained(true) while held by drain")
	}
}

func TestRunStandingResumeAfterDrainRunsIdleAndClearsDrained(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var draining atomic.Bool
	draining.Store(true)
	var drained atomic.Bool
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Draining: draining.Load, SetDrained: drained.Store, Out: io.Discard,
		NowFn: func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(20 * time.Millisecond)
	draining.Store(false) // resume
	time.Sleep(20 * time.Millisecond)
	if len(r.snapshot()) == 0 {
		t.Fatal("resumed worker never ran an idle command")
	}
	if drained.Load() {
		t.Fatal("expected drained cleared once passes resumed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'RunStandingDraining|RunStandingResumeAfterDrain' -v`
Expected: compile failure — `unknown field Draining` / `SetDrained` in `StandingDeps`.

- [ ] **Step 3: Add the fields**

In `pkg/worker/standing.go`, add to the `StandingDeps` struct (after the `Paused` field):

```go
	Paused    func() bool                         // gate from the control reader's paused flag
	Draining  func() bool                         // second gate (drain): finish current pass, take no new work
	SetDrained func(bool)                         // publishes whether the worker is held idle due to drain
```

- [ ] **Step 4: Default `SetDrained` and rewrite the loop gate**

In `RunStanding`, add a default near the other `deps.X == nil` defaults (after the `IdleInterval`/`ScheduleInterval` defaults):

```go
	if deps.SetDrained == nil {
		deps.SetDrained = func(bool) {}
	}
```

Replace the idle-loop body (the `for { ... }` block starting `select { case <-ctx.Done(): return nil }`) with:

```go
	// Idle loop.
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		paused := deps.Paused != nil && deps.Paused()
		draining := deps.Draining != nil && deps.Draining()
		if paused || draining {
			deps.SetDrained(draining) // drained only when held *because* of drain
			if sleepCtx(ctx, game.SleepMedium) {
				return nil
			}
			continue
		}
		deps.SetDrained(false)
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
		if sleepCtx(ctx, deps.IdleInterval) {
			return nil
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'RunStanding' -v`
Expected: PASS (all existing `RunStanding*` tests plus the two new ones — the existing `TestRunStandingPausedDoesNotRunIdle` still passes because `Draining`/`SetDrained` are nil-defaulted).

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/standing.go pkg/worker/standing_test.go
git commit -m "feat(worker): drain gate + SetDrained in the standing loop

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Worker wiring — atomics, control handling, heartbeat

**Files:**
- Modify: `cmd/worker/main.go`
- Test: `cmd/worker/main_test.go`

**Interfaces:**
- Consumes: `control.TypeDrain` (Task 1), `StandingDeps.Draining`/`SetDrained` (Task 2), `control.Status.Drained` (Task 1).
- Produces: `buildStatus(st *game.State, standing, taskID string, drained bool, now time.Time) control.Status` — signature gains a `drained` param before `now`.

- [ ] **Step 1: Write the failing test**

In `cmd/worker/main_test.go`, add:

```go
func TestBuildStatusCarriesDrained(t *testing.T) {
	st := &game.State{CurrentSystem: "sol"}
	if got := buildStatus(st, "hauler", "", true, time.Unix(1000, 0)); !got.Drained {
		t.Fatalf("Drained = false, want true")
	}
	if got := buildStatus(st, "hauler", "", false, time.Unix(1000, 0)); got.Drained {
		t.Fatalf("Drained = true, want false")
	}
}
```

Also update the three existing `buildStatus(...)` call sites in this file to pass the new `drained` arg (`false`):
- `TestBuildStatusAndKnownState`: `buildStatus(st, "track_station", "t-1", false, now)`
- `TestBuildStatusPrefersSystemDisplayName`: `buildStatus(st, "hauler", "", false, time.Unix(1000, 0))`
- `TestBuildStatusFallsBackWhenSystemDataStale`: `buildStatus(st, "hauler", "", false, time.Unix(1000, 0))`

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/worker/ -run 'TestBuildStatus' -v`
Expected: compile failure — too many arguments / signature mismatch (the production `buildStatus` still takes 4 params).

- [ ] **Step 3: Update `buildStatus` signature and body**

In `cmd/worker/main.go`, change the `buildStatus` signature and set `Drained`:

```go
func buildStatus(st *game.State, standing, taskID string, drained bool, now time.Time) control.Status {
	return control.Status{
		System:           displaySystem(st),
		POI:              st.CurrentPOI,
		Docked:           st.CurrentPOI != "" && !st.Traveling,
		Hull:             st.Hull,
		MaxHull:          st.MaxHull,
		Fuel:             st.Fuel,
		MaxFuel:          st.MaxFuel,
		Credits:          st.Credits,
		CargoUsed:        st.Ship.CargoUsed,
		CargoCapacity:    st.Ship.CargoCapacity,
		StandingBehavior: standing,
		ActiveTaskID:     taskID,
		Drained:          drained,
		Timestamp:        now.Format(time.RFC3339Nano),
	}
}
```

- [ ] **Step 4: Declare the atomics**

In `cmd/worker/main.go`, next to `var paused atomic.Bool` (line ~185), add:

```go
		var paused atomic.Bool
		var draining atomic.Bool
		var drained atomic.Bool
```

- [ ] **Step 5: Handle `TypeDrain` and extend `TypeResume`**

In the control-reader switch (cases `TypePause`/`TypeResume`), change to:

```go
				case control.TypePause:
					paused.Store(true)
					logger.Printf("paused")
				case control.TypeResume:
					paused.Store(false)
					draining.Store(false)
					logger.Printf("resumed")
				case control.TypeDrain:
					draining.Store(true)
					logger.Printf("draining: will finish current pass then idle")
```

- [ ] **Step 6: Inject the gate + publisher into `StandingDeps`, and feed the heartbeat**

In the `StandingDeps{...}` literal (line ~273), add after `Paused: paused.Load,`:

```go
					Paused:     paused.Load,
					Draining:   draining.Load,
					SetDrained: drained.Store,
```

At the heartbeat build site (line ~331) pass the flag:

```go
				status := buildStatus(nowState, standing, tid, drained.Load(), time.Now())
```

- [ ] **Step 7: Run tests + build to verify**

Run: `go test ./cmd/worker/ -run 'TestBuildStatus' -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 8: Commit**

```bash
git add cmd/worker/main.go cmd/worker/main_test.go
git commit -m "feat(worker): handle TypeDrain, report Drained in heartbeat

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Supervisor — `Fleet.DrainProgress`

**Files:**
- Modify: `pkg/overmind/supervisor/fleet.go`
- Test: `pkg/overmind/supervisor/fleet_test.go` (append; create if absent)

**Interfaces:**
- Consumes: `WorkerInfo.LastStatus.Drained` (set by the existing `ApplyStatus`, which stores the whole `control.Status`), `WorkerInfo.Healthy`.
- Produces: `func (f *Fleet) DrainProgress() (idle, total int, busy []string)` — `total` = healthy workers; `idle` = healthy workers whose last heartbeat had `Drained=true`; `busy` = sorted agent IDs of healthy, not-yet-drained workers.

- [ ] **Step 1: Write the failing test**

Append to `pkg/overmind/supervisor/fleet_test.go` (create the file with this package header if it does not exist):

```go
package supervisor

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

func TestDrainProgressCountsHealthyDrained(t *testing.T) {
	f := NewFleet()
	now := time.Unix(1000, 0)
	for _, id := range []string{"a", "b", "c"} {
		f.ApplyHello(control.Hello{AgentID: id, Role: "hauler"}, 1, now)
	}
	f.ApplyStatus("a", control.Status{Drained: true}, now)
	f.ApplyStatus("b", control.Status{Drained: false}, now)
	// c never reports a status but is healthy from hello.
	idle, total, busy := f.DrainProgress()
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if idle != 1 {
		t.Fatalf("idle = %d, want 1", idle)
	}
	if len(busy) != 2 || busy[0] != "b" || busy[1] != "c" {
		t.Fatalf("busy = %v, want [b c]", busy)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/supervisor/ -run 'DrainProgress' -v`
Expected: compile failure — `f.DrainProgress undefined`.

- [ ] **Step 3: Implement `DrainProgress`**

In `pkg/overmind/supervisor/fleet.go`, add (the file already imports `slices`, `strings`, `sync`, `time`):

```go
// DrainProgress reports drain quiescence across currently-healthy workers:
// total healthy, how many last reported Drained, and the sorted ids of those
// still busy. A worker that is healthy but has not yet reported a heartbeat
// counts as busy (not drained).
func (f *Fleet) DrainProgress() (idle, total int, busy []string) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, w := range f.workers {
		if !w.Healthy {
			continue
		}
		total++
		if w.LastStatus.Drained {
			idle++
		} else {
			busy = append(busy, w.AgentID)
		}
	}
	slices.Sort(busy)
	return idle, total, busy
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/overmind/supervisor/ -run 'DrainProgress' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/overmind/supervisor/fleet.go pkg/overmind/supervisor/fleet_test.go
git commit -m "feat(overmind/supervisor): Fleet.DrainProgress drain-quiescence count

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Overmind — drain/resume signals

**Files:**
- Create: `cmd/overmind/drain.go`
- Test: `cmd/overmind/drain_test.go` (new)
- Modify: `cmd/overmind/main.go`

**Interfaces:**
- Consumes: `control.TypeDrain`/`control.TypeResume`/`control.NewEnvelope` (Task 1), `supervisor.WorkerInfo`/`fleet.Snapshot()`/`fleet.DrainProgress()` (Task 4), `srv.Send(agentID, env)`.
- Produces: `broadcast(s controlSender, workers []supervisor.WorkerInfo, t control.Type, payload any, log func(string)) int` and `drainComplete(idle, total int) bool` in package `main`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/overmind/drain_test.go`:

```go
package main

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

type fakeSender struct {
	sent []string // agent ids
	fail map[string]bool
}

func (f *fakeSender) Send(agentID string, _ control.Envelope) error {
	if f.fail[agentID] {
		return errSendFail
	}
	f.sent = append(f.sent, agentID)
	return nil
}

func TestBroadcastFansOutToAllWorkers(t *testing.T) {
	s := &fakeSender{}
	workers := []supervisor.WorkerInfo{{AgentID: "a"}, {AgentID: "b"}, {AgentID: "c"}}
	n := broadcast(s, workers, control.TypeDrain, nil, func(string) {})
	if n != 3 {
		t.Fatalf("sent count = %d, want 3", n)
	}
	if len(s.sent) != 3 {
		t.Fatalf("delivered = %v, want 3", s.sent)
	}
}

func TestBroadcastCountsOnlySuccesses(t *testing.T) {
	s := &fakeSender{fail: map[string]bool{"b": true}}
	workers := []supervisor.WorkerInfo{{AgentID: "a"}, {AgentID: "b"}}
	n := broadcast(s, workers, control.TypeResume, nil, func(string) {})
	if n != 1 {
		t.Fatalf("sent count = %d, want 1 (b failed)", n)
	}
}

func TestDrainComplete(t *testing.T) {
	cases := []struct {
		idle, total int
		want        bool
	}{
		{0, 0, true},  // no workers -> trivially drained
		{3, 3, true},  // all idle
		{2, 3, false}, // one busy
		{0, 1, false},
	}
	for _, c := range cases {
		if got := drainComplete(c.idle, c.total); got != c.want {
			t.Fatalf("drainComplete(%d,%d) = %v, want %v", c.idle, c.total, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/overmind/ -run 'Broadcast|DrainComplete' -v`
Expected: compile failure — `broadcast` / `drainComplete` / `errSendFail` / `controlSender` undefined.

- [ ] **Step 3: Implement the helpers**

Create `cmd/overmind/drain.go`:

```go
package main

import (
	"errors"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// errSendFail is used by tests to simulate a failed control send.
var errSendFail = errors.New("send failed")

// controlSender is the subset of *supervisor.Server the drain helpers need.
type controlSender interface {
	Send(agentID string, env control.Envelope) error
}

// broadcast sends a control message of type t (with payload) to every worker,
// returning the number successfully delivered. A failed send is logged and
// skipped (a worker may have exited) — never fatal to the fan-out.
func broadcast(s controlSender, workers []supervisor.WorkerInfo, t control.Type, payload any, log func(string)) int {
	sent := 0
	for _, w := range workers {
		env, err := control.NewEnvelope(t, w.AgentID, payload)
		if err != nil {
			log("build " + string(t) + " envelope for " + w.AgentID + ": " + err.Error())
			continue
		}
		if err := s.Send(w.AgentID, env); err != nil {
			log("send " + string(t) + " to " + w.AgentID + ": " + err.Error())
			continue
		}
		sent++
	}
	return sent
}

// drainComplete reports whether the fleet has reached drain quiescence: no
// healthy workers, or all healthy workers idle.
func drainComplete(idle, total int) bool {
	return total == 0 || idle >= total
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/overmind/ -run 'Broadcast|DrainComplete' -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Wire the signal handlers in `main.go`**

In `cmd/overmind/main.go`, register the new signals:

```go
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)
```

Replace the single-shot signal goroutine:

```go
	go func() {
		sig := <-sigCh
		logger.Printf("received signal %v, shutting down", sig)
		cancel()
	}()
```

with a dispatching loop (drain runs in its own goroutine so a later SIGTERM stays responsive):

```go
	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				logger.Printf("received signal %v, shutting down", sig)
				cancel()
				return
			case syscall.SIGUSR1:
				logger.Printf("received SIGUSR1: draining fleet")
				go drainFleet(srv, fleet, logger)
			case syscall.SIGUSR2:
				logger.Printf("received SIGUSR2: resuming fleet")
				n := broadcast(srv, fleet.Snapshot(), control.TypeResume, nil, func(m string) { logger.Print(m) })
				logger.Printf("resume sent to %d workers", n)
			}
		}
	}()
```

Add `drainFleet` near the bottom of `main.go` (after `main`):

```go
// drainFleet broadcasts a drain to all workers, then polls drain quiescence,
// logging progress until the fleet is drained or a bounded number of polls
// elapses (stragglers that cannot reach idle are surfaced, never force-aborted).
func drainFleet(srv *supervisor.Server, fleet *supervisor.Fleet, logger *log.Logger) {
	logf := func(m string) { logger.Print(m) }
	n := broadcast(srv, fleet.Snapshot(), control.TypeDrain, nil, logf)
	logger.Printf("drain sent to %d workers", n)
	const maxPolls = 60 // ~ maxPolls * game.SleepShort upper bound
	for i := 0; i < maxPolls; i++ {
		idle, total, busy := fleet.DrainProgress()
		if drainComplete(idle, total) {
			logger.Printf("fleet drained — safe to stop (%d/%d idle)", idle, total)
			return
		}
		logger.Printf("drain: %d/%d idle — still busy: %v", idle, total, busy)
		time.Sleep(game.SleepShort)
	}
	idle, total, busy := fleet.DrainProgress()
	logger.Printf("drain poll ended: %d/%d idle — still busy: %v (force-stop with SIGTERM if needed)", idle, total, busy)
}
```

Confirm the imports block in `main.go` includes `log`, `time`, `syscall`, `github.com/rsned/spacemolt/pkg/game`, `github.com/rsned/spacemolt/pkg/overmind/control`, and `github.com/rsned/spacemolt/pkg/overmind/supervisor` (add any missing). `srv` is `*supervisor.Server`, `fleet` is `*supervisor.Fleet`, `logger` is `*log.Logger` — all already in scope in `main`.

- [ ] **Step 6: Build, vet, and run the package tests**

Run: `go build ./... && go test ./cmd/overmind/ ./pkg/worker/ ./pkg/overmind/... -count=1`
Expected: build clean; all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/overmind/drain.go cmd/overmind/drain_test.go cmd/overmind/main.go
git commit -m "feat(overmind): SIGUSR1 drain / SIGUSR2 resume with quiescence polling

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Full-suite gate + lint

**Files:** none (verification only).

- [ ] **Step 1: Full build + test + lint**

Run:
```bash
go build ./... && go test ./... && golangci-lint run ./cmd/worker/ ./cmd/overmind/ ./pkg/worker/ ./pkg/overmind/...
```
Expected: build clean; all tests PASS; `0 issues`.

- [ ] **Step 2: Rebuild the worker + overmind binaries to bin/**

Run:
```bash
go build -o bin/worker ./cmd/worker && go build -o bin/overmind ./cmd/overmind
```
Expected: both binaries built under `bin/`. (Deploying them to the live fleet is a separate, operator-gated step — do not restart the fleet here.)

---

## Manual verification (after deploy, operator-gated)

Drain is exercised end-to-end only against a running overmind + workers:

1. `kill -USR1 <overmind-pid>` → overmind logs `drain sent to N workers`, then `drain: k/N idle …` lines, then `fleet drained — safe to stop`.
2. Each worker log shows `draining: will finish current pass then idle`, completes its in-flight haul/mining run, then goes quiet (no new passes).
3. `kill -USR2 <overmind-pid>` → `resume sent to N workers`; workers log `resumed` and resume passes.
4. From a drained hold, `kill -TERM <overmind-pid>` stops cleanly with workers already docked-idle.
