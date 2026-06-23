# Overmind Supervisor Liveness Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the overmind supervisor double-spawn race by tracking each worker's live process, gracefully kill hung workers, and stagger fleet startup to respect the per-IP `/login` rate limit.

**Architecture:** The supervisor gains a per-agent registry of `*workerProc` (the `*exec.Cmd`, a per-worker cancel func, a launch timestamp, and an `exited` channel closed when the process dies). `reapAndRestart` uses process liveness plus the fleet's `LastSeen` to distinguish booting / healthy / hung / dead workers, splitting the old single `SilenceTimeout` into a generous `BootTimeout` (pre-`Hello`) and a lowered `SilenceTimeout` (established workers). Hung workers get SIGTERM then SIGKILL. Initial launches are spaced by `StaggerInterval`.

**Tech Stack:** Go 1.24, standard library `os/exec`, `context`, `syscall`, `sync`. Tests use real short-lived/long-lived child processes (`true`, `sleep`, `sh -c 'trap "" TERM; sleep'`) through the injected `SpawnFunc`.

## Global Constraints

- Target Go 1.24+; use `range` over integers where natural.
- Use the predefined `game.Sleep*` constants for all durations (`pkg/game/constants.go`); do not introduce bare `time.Second` literals for tunable waits. Actual values: `SleepTick=10s`, `SleepMedium=SleepTick/2=5s`.
- All new code must pass `golangci-lint` with no new findings.
- Run `go build ./...` and `go test ./...` before every commit.
- Tests must exercise real behavior — spawn real child processes through `SpawnFunc`; do not mock `*exec.Cmd` (the unit under test is the supervisor's decision logic, and `*exec.Cmd` is its real collaborator).
- Compiled binaries go in `bin/`, never the repo root.

Design spec: `docs/superpowers/specs/2026-06-23-overmind-supervisor-liveness-hardening-design.md`.

---

### Task 1: Per-agent process registry

Introduce `workerProc` and make `launch` track each spawned process. `reapAndRestart` is unchanged in this task (still uses the old fleet-only logic) so the build and existing tests stay green; only the bookkeeping is added.

**Files:**
- Modify: `pkg/overmind/supervisor/supervisor.go` (imports, `Supervisor` struct, `NewSupervisor`, `launch`)
- Test: `pkg/overmind/supervisor/supervisor_test.go`

**Interfaces:**
- Produces:
  - `type workerProc struct { cmd *exec.Cmd; cancel context.CancelFunc; launchedAt time.Time; exited chan struct{} }`
  - `func (p *workerProc) alive() bool`
  - `Supervisor.procs map[string]*workerProc` guarded by `Supervisor.procMu sync.Mutex`
  - `Supervisor` fields `BootTimeout`, `StaggerInterval`, `KillGrace time.Duration` (set in `NewSupervisor`; consumed in later tasks)

- [ ] **Step 1: Write the failing test**

Add to `pkg/overmind/supervisor/supervisor_test.go`:

```go
func TestLaunchTracksLiveProcess(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "w1"}}
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, "sleep", "60")
		return cmd, cmd.Start()
	}
	sup := NewSupervisor(nil, NewFleet(), specs, spawn, log.New(io.Discard, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])

	sup.procMu.Lock()
	p := sup.procs["w1"]
	sup.procMu.Unlock()
	if p == nil {
		t.Fatal("launch did not register a workerProc")
	}
	if !p.alive() {
		t.Fatal("freshly launched process should be alive")
	}

	// Killing the process must close exited and flip alive().
	_ = p.cmd.Process.Kill()
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("exited channel not closed after process death")
	}
	if p.alive() {
		t.Fatal("alive() should be false after process exit")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/supervisor/ -run TestLaunchTracksLiveProcess -v`
Expected: FAIL to compile — `sup.procs`, `workerProc`, `alive` undefined.

- [ ] **Step 3: Add the type, fields, defaults, and rewritten launch**

In `pkg/overmind/supervisor/supervisor.go`, update the import block to add `sync` and `syscall`:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)
```

Add the `workerProc` type (place it just above the `Supervisor` struct):

```go
// workerProc tracks one live worker process so the supervisor can tell a
// still-booting worker apart from a dead one, and can kill a hung one before
// respawning. exited is closed by the reaping goroutine when cmd.Wait returns.
type workerProc struct {
	cmd        *exec.Cmd
	cancel     context.CancelFunc // cancels the worker's ctx -> SIGKILL
	launchedAt time.Time
	exited     chan struct{}
}

// alive reports whether the process has not yet exited.
func (p *workerProc) alive() bool {
	select {
	case <-p.exited:
		return false
	default:
		return true
	}
}
```

Add fields to the `Supervisor` struct (after `restarts`):

```go
	// procs tracks the live process per agent id; procMu guards it.
	procMu sync.Mutex
	procs  map[string]*workerProc

	// StaggerInterval spaces initial worker launches to stay under the per-IP
	// /login rate limit. BootTimeout bounds how long a worker may be alive but
	// not yet have sent Hello before it is treated as wedged. KillGrace is the
	// SIGTERM->SIGKILL escalation window.
	StaggerInterval time.Duration
	BootTimeout     time.Duration
	KillGrace       time.Duration
```

Update `NewSupervisor`'s returned struct literal so it reads:

```go
	return &Supervisor{
		server: server, fleet: fleet, specs: specs, spawn: spawn, logger: logger,
		SilenceTimeout:  9 * game.SleepTick,  // 90s: heartbeat-gap tolerance for established workers
		BootTimeout:     30 * game.SleepTick, // 5min: max alive-but-no-Hello before a boot is "wedged"
		StaggerInterval: game.SleepMedium,    // 5s between initial spawns (per-IP /login pacing)
		KillGrace:       game.SleepMedium,    // 5s SIGTERM->SIGKILL window
		MaxRestarts:     100,
		restarts:        make(map[string]int),
		procs:           make(map[string]*workerProc),
	}
```

Also delete the now-stale multi-line comment above the old `SilenceTimeout` field in the struct (the one beginning "SilenceTimeout MUST exceed worst-case worker cold-start"); replace it with a one-line comment:

```go
	// SilenceTimeout is the heartbeat-gap tolerance for an established worker
	// (one that has already sent Hello). Cold-start is covered by BootTimeout.
	SilenceTimeout time.Duration
	MaxRestarts    int
	restarts       map[string]int
```

Replace `launch` with the registry-aware version:

```go
func (s *Supervisor) launch(ctx context.Context, spec WorkerSpec) {
	wctx, wcancel := context.WithCancel(ctx)
	cmd, err := s.spawn(wctx, spec, s.socket())
	if err != nil {
		wcancel()
		s.logger.Printf("spawn %q failed: %v", spec.AgentID, err)
		return
	}
	if cmd == nil {
		wcancel()
		return
	}
	proc := &workerProc{
		cmd:        cmd,
		cancel:     wcancel,
		launchedAt: time.Now(),
		exited:     make(chan struct{}),
	}
	s.procMu.Lock()
	s.procs[spec.AgentID] = proc
	s.procMu.Unlock()
	go func() {
		// Reap the child when it exits (or is killed on ctx cancel) so it does
		// not linger as a zombie. wcancel here releases the per-worker context
		// when the process ends on its own; it is idempotent with kill().
		_ = cmd.Wait()
		proc.cancel()
		close(proc.exited)
	}()
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/overmind/supervisor/ -run TestLaunchTracksLiveProcess -v`
Expected: PASS.

- [ ] **Step 5: Verify nothing else broke + lint**

Run: `go build ./... && go test ./pkg/overmind/... && golangci-lint run ./pkg/overmind/supervisor/`
Expected: build OK; existing supervisor tests still PASS (old `reapAndRestart` unchanged); 0 lint issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/supervisor_test.go
git commit -m "feat(overmind): track live worker processes in supervisor"
```

---

### Task 2: Graceful kill (SIGTERM then SIGKILL)

**Files:**
- Modify: `pkg/overmind/supervisor/supervisor.go` (add `kill`)
- Test: `pkg/overmind/supervisor/supervisor_test.go`

**Interfaces:**
- Consumes: `workerProc`, `Supervisor.KillGrace` (Task 1).
- Produces: `func (s *Supervisor) kill(p *workerProc)` — sends SIGTERM, waits up to `KillGrace`, then cancels (SIGKILL); returns only after the process has exited.

- [ ] **Step 1: Write the failing tests**

Add to `pkg/overmind/supervisor/supervisor_test.go`:

```go
func TestKillGracefulOnSigterm(t *testing.T) {
	// A plain `sleep` dies on SIGTERM, so kill should return well before grace.
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, "sleep", "60")
		return cmd, cmd.Start()
	}
	sup := NewSupervisor(nil, NewFleet(), []WorkerSpec{{AgentID: "g"}}, spawn, log.New(io.Discard, "", 0))
	sup.KillGrace = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup.launch(ctx, WorkerSpec{AgentID: "g"})

	sup.procMu.Lock()
	p := sup.procs["g"]
	sup.procMu.Unlock()

	start := time.Now()
	sup.kill(p)
	if p.alive() {
		t.Fatal("process should be dead after kill")
	}
	if elapsed := time.Since(start); elapsed >= sup.KillGrace {
		t.Fatalf("SIGTERM-respecting process should die before grace, took %v", elapsed)
	}
}

func TestKillEscalatesToSigkill(t *testing.T) {
	// This child ignores SIGTERM, so kill must escalate to SIGKILL after grace.
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, "sh", "-c", `trap "" TERM; sleep 60`)
		return cmd, cmd.Start()
	}
	sup := NewSupervisor(nil, NewFleet(), []WorkerSpec{{AgentID: "k"}}, spawn, log.New(io.Discard, "", 0))
	sup.KillGrace = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup.launch(ctx, WorkerSpec{AgentID: "k"})

	sup.procMu.Lock()
	p := sup.procs["k"]
	sup.procMu.Unlock()

	start := time.Now()
	sup.kill(p)
	if p.alive() {
		t.Fatal("process should be dead after SIGKILL escalation")
	}
	if elapsed := time.Since(start); elapsed < sup.KillGrace {
		t.Fatalf("escalation should wait at least KillGrace, took %v", elapsed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/overmind/supervisor/ -run TestKill -v`
Expected: FAIL to compile — `sup.kill` undefined.

- [ ] **Step 3: Implement kill**

Add to `pkg/overmind/supervisor/supervisor.go`:

```go
// kill terminates a live worker: SIGTERM first (the worker checkpoints and
// exits on it), escalating to SIGKILL via ctx cancel if it does not exit
// within KillGrace. Returns only once the process has actually exited, so the
// caller can safely respawn without two processes for one agent.
func (s *Supervisor) kill(p *workerProc) {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.exited:
		// Exited cleanly within the grace window.
	case <-time.After(s.KillGrace):
		p.cancel() // ctx cancel -> SIGKILL via exec.CommandContext
		<-p.exited
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/overmind/supervisor/ -run TestKill -v`
Expected: PASS (both).

- [ ] **Step 5: Lint**

Run: `golangci-lint run ./pkg/overmind/supervisor/`
Expected: 0 issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/supervisor_test.go
git commit -m "feat(overmind): graceful SIGTERM then SIGKILL of hung workers"
```

---

### Task 3: Process-aware restart decision matrix

Rewrite `reapAndRestart` to use process liveness + fleet `LastSeen`. Replace the two old reap tests (whose premise — restart any `!seen` spec — no longer holds) with a matrix of focused tests.

**Files:**
- Modify: `pkg/overmind/supervisor/supervisor.go` (`reapAndRestart`, add `tryRestart`)
- Test: `pkg/overmind/supervisor/supervisor_test.go` (replace `TestReapAndRestartCapsUnseenWorkers` and `TestReapAndRestartCounterResetsOnHealthy`; add matrix tests)

**Interfaces:**
- Consumes: `workerProc.alive`, `Supervisor.procs/procMu`, `kill` (Tasks 1–2), `NeedsRestart`, `Supervisor.SilenceTimeout/BootTimeout/MaxRestarts/restarts`, `Fleet.Snapshot/MarkRestart`.
- Produces: `func (s *Supervisor) tryRestart(ctx context.Context, spec WorkerSpec, killed bool)` — relaunches `spec` unless the crash-loop cap (`MaxRestarts`) is hit; increments the counter and marks the fleet entry restarting.

- [ ] **Step 1: Write the failing tests**

In `pkg/overmind/supervisor/supervisor_test.go`, **delete** `TestReapAndRestartCapsUnseenWorkers` and `TestReapAndRestartCounterResetsOnHealthy` entirely, and add these. (Helpers `aliveSpawn` and `waitExited` are defined once here.)

```go
// aliveSpawn returns a SpawnFunc that launches a long-lived `sleep`, counting
// invocations into n.
func aliveSpawn(n *atomic.Int32) SpawnFunc {
	return func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		n.Add(1)
		cmd := exec.CommandContext(ctx, "sleep", "60")
		return cmd, cmd.Start()
	}
}

// procOf fetches the tracked proc for an agent (white-box helper).
func procOf(sup *Supervisor, id string) *workerProc {
	sup.procMu.Lock()
	defer sup.procMu.Unlock()
	return sup.procs[id]
}

func TestReapBootingWorkerNotRespawned(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "boot"}}
	sup := NewSupervisor(nil, NewFleet(), specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0]) // alive, no Hello yet, within BootTimeout
	// Several reap passes must NOT spawn a duplicate (the double-spawn bug).
	for range 5 {
		sup.reapAndRestart(ctx)
	}
	if spawned.Load() != 1 {
		t.Fatalf("booting worker should not be respawned, got %d spawns", spawned.Load())
	}
}

func TestReapWedgedBootKilledAndRespawned(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "wedged"}}
	sup := NewSupervisor(nil, NewFleet(), specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.BootTimeout = time.Nanosecond // any alive-but-unseen worker is "wedged"
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	sup.reapAndRestart(ctx)
	if spawned.Load() != 2 {
		t.Fatalf("wedged boot should be killed and respawned, got %d spawns", spawned.Load())
	}
}

func TestReapHungEstablishedWorker(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "hung"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Nanosecond // seen worker is immediately "silent"
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "hung", Role: "idle"}, 1, time.Now())
	fleet.ApplyStatus("hung", control.Status{}, time.Now())

	sup.reapAndRestart(ctx)
	if spawned.Load() != 2 {
		t.Fatalf("hung established worker should be killed and respawned, got %d spawns", spawned.Load())
	}
}

func TestReapHealthyWorkerUntouched(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "ok"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Hour
	sup.restarts["ok"] = 7 // pretend it had a rough start
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "ok", Role: "idle"}, 1, time.Now())
	fleet.ApplyStatus("ok", control.Status{}, time.Now())

	sup.reapAndRestart(ctx)
	if spawned.Load() != 1 {
		t.Fatalf("healthy worker must not be respawned, got %d spawns", spawned.Load())
	}
	if sup.restarts["ok"] != 0 {
		t.Fatalf("healthy worker should clear its restart counter, got %d", sup.restarts["ok"])
	}
}

func TestReapDeadWorkerRespawnedUpToCap(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "crash"}}
	// Each spawn exits immediately, modelling a crash-loop.
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		spawned.Add(1)
		cmd := exec.CommandContext(ctx, "true")
		return cmd, cmd.Start()
	}
	sup := NewSupervisor(nil, NewFleet(), specs, spawn, log.New(io.Discard, "", 0))
	sup.MaxRestarts = 3
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for range 10 {
		// Make the reap deterministic: ensure the current proc is fully reaped
		// (exited closed) before the next decision pass.
		if p := procOf(sup, "crash"); p != nil {
			<-p.exited
		}
		sup.reapAndRestart(ctx)
	}
	if spawned.Load() != 3 {
		t.Fatalf("crash-loop should respawn up to MaxRestarts (3), got %d", spawned.Load())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/overmind/supervisor/ -run TestReap -v`
Expected: the new behavioral tests FAIL (old `reapAndRestart` respawns booting workers / doesn't clear the way the new ones expect), confirming they exercise the new logic.

- [ ] **Step 3: Rewrite reapAndRestart and add tryRestart**

In `pkg/overmind/supervisor/supervisor.go`, replace the whole `reapAndRestart` method with:

```go
func (s *Supervisor) reapAndRestart(ctx context.Context) {
	now := time.Now()
	healthy := make(map[string]WorkerInfo)
	for _, w := range s.fleet.Snapshot() {
		healthy[w.AgentID] = w
	}
	for _, spec := range s.specs {
		proc := procSnapshot(s, spec.AgentID)

		// No process tracked: never launched, or fully reaped earlier.
		if proc == nil {
			s.tryRestart(ctx, spec, false)
			continue
		}

		if proc.alive() {
			w, seen := healthy[spec.AgentID]
			switch {
			case seen && NeedsRestart(w, now, s.SilenceTimeout):
				// Established worker whose heartbeat went silent: hung.
				s.kill(proc)
				s.tryRestart(ctx, spec, true)
			case !seen && now.Sub(proc.launchedAt) > s.BootTimeout:
				// Alive but never sent Hello within the boot window: wedged.
				s.kill(proc)
				s.tryRestart(ctx, spec, true)
			case seen && w.Healthy:
				// Healthy: clear the crash-loop counter so MaxRestarts bounds
				// restarts-per-incident, not lifetime restarts.
				delete(s.restarts, spec.AgentID)
			default:
				// Still booting (alive, no Hello yet, within BootTimeout): leave it.
			}
			continue
		}

		// Process has exited: respawn (subject to the crash-loop cap).
		s.tryRestart(ctx, spec, false)
	}
}

// procSnapshot returns the tracked proc for an agent under the registry lock.
func procSnapshot(s *Supervisor, agentID string) *workerProc {
	s.procMu.Lock()
	defer s.procMu.Unlock()
	return s.procs[agentID]
}

// tryRestart relaunches spec unless the crash-loop cap is reached. killed marks
// whether a live process was just terminated (for the log line).
func (s *Supervisor) tryRestart(ctx context.Context, spec WorkerSpec, killed bool) {
	if s.restarts[spec.AgentID] >= s.MaxRestarts {
		return
	}
	s.restarts[spec.AgentID]++
	s.fleet.MarkRestart(spec.AgentID)
	s.logger.Printf("restarting worker %q (killed=%v, restart #%d)", spec.AgentID, killed, s.restarts[spec.AgentID])
	s.launch(ctx, spec)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/overmind/supervisor/ -run TestReap -v`
Expected: PASS (all five).

- [ ] **Step 5: Full package test + lint**

Run: `go build ./... && go test ./pkg/overmind/... && golangci-lint run ./pkg/overmind/supervisor/`
Expected: build OK; all tests PASS; 0 lint issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/supervisor_test.go
git commit -m "feat(overmind): process-aware restart matrix (booting/hung/dead)"
```

---

### Task 4: Staggered startup

**Files:**
- Modify: `pkg/overmind/supervisor/supervisor.go` (`Run`)
- Test: `pkg/overmind/supervisor/supervisor_test.go` (update `TestSupervisorSpawnsEachSpecOnce`; add stagger spacing test)

**Interfaces:**
- Consumes: `Supervisor.StaggerInterval` (Task 1), `launch`.
- Produces: `Run` spaces initial launches by `StaggerInterval` (ctx-cancellable).

- [ ] **Step 1: Write/adjust the tests**

In `pkg/overmind/supervisor/supervisor_test.go`, update `TestSupervisorSpawnsEachSpecOnce` to disable stagger (otherwise only one worker launches inside the 300ms window). Change the line after `sup.SilenceTimeout = time.Hour` to also set:

```go
	sup.StaggerInterval = 0 // launch back-to-back for this test
```

Then add a new test:

```go
func TestRunStaggersInitialLaunches(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "a"}, {AgentID: "b"}, {AgentID: "c"}}
	var spawned atomic.Int32
	sup := NewSupervisor(nil, NewFleet(), specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Hour
	sup.StaggerInterval = 100 * time.Millisecond

	// Cancel after only enough time for the first launch (plus margin), well
	// before the second stagger interval elapses.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = sup.Run(ctx)

	if got := spawned.Load(); got != 1 {
		t.Fatalf("with a 100ms stagger and 50ms budget, expected 1 launch, got %d", got)
	}
}
```

- [ ] **Step 2: Run tests to verify the new one fails**

Run: `go test ./pkg/overmind/supervisor/ -run 'TestRunStaggers|TestSupervisorSpawnsEachSpecOnce' -v`
Expected: `TestRunStaggersInitialLaunches` FAILS (current `Run` launches all three immediately → `got 3`); `TestSupervisorSpawnsEachSpecOnce` PASSES.

- [ ] **Step 3: Add the stagger to Run**

In `pkg/overmind/supervisor/supervisor.go`, replace the initial launch loop at the top of `Run` with a staggered, ctx-cancellable loop:

```go
func (s *Supervisor) Run(ctx context.Context) error {
	for i, spec := range s.specs {
		if i > 0 && s.StaggerInterval > 0 {
			select {
			case <-time.After(s.StaggerInterval):
			case <-ctx.Done():
				return nil
			}
		}
		s.launch(ctx, spec)
	}
	ticker := time.NewTicker(game.SleepMedium)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.reapAndRestart(ctx)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/overmind/supervisor/ -run 'TestRunStaggers|TestSupervisorSpawnsEachSpecOnce' -v`
Expected: PASS (both).

- [ ] **Step 5: Full package test + lint**

Run: `go test ./pkg/overmind/... && golangci-lint run ./pkg/overmind/supervisor/`
Expected: all PASS; 0 issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/supervisor_test.go
git commit -m "feat(overmind): stagger initial worker launches"
```

---

### Task 5: Wire the `--stagger` flag

**Files:**
- Modify: `cmd/overmind/main.go`

**Interfaces:**
- Consumes: `Supervisor.StaggerInterval` (Task 1).

- [ ] **Step 1: Add the flag and assign it**

In `cmd/overmind/main.go`, add the flag in the flag block (after `fleetPath`):

```go
	stagger := flag.Duration("stagger", game.SleepMedium, "Delay between initial worker launches (per-IP /login pacing)")
```

Ensure `"github.com/rsned/spacemolt/pkg/game"` is in the import block (add it if missing). After `sup := supervisor.NewSupervisor(...)` (around line 47), assign:

```go
	sup.StaggerInterval = *stagger
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./cmd/overmind/`
Expected: builds clean.

- [ ] **Step 3: Verify the flag is registered**

Run: `go run ./cmd/overmind/ --help 2>&1 | grep -- -stagger`
Expected: a line documenting `-stagger` with the `5s` default.

- [ ] **Step 4: Lint**

Run: `golangci-lint run ./cmd/overmind/`
Expected: 0 issues.

- [ ] **Step 5: Commit**

```bash
git add cmd/overmind/main.go
git commit -m "feat(overmind): --stagger flag for empirical login-rate tuning"
```

---

### Task 6: Full-suite verification

**Files:** none (verification only).

- [ ] **Step 1: Build, race-test the package, lint the touched packages**

Run:
```bash
go build ./... && \
go test -race ./pkg/overmind/... && \
go test ./... && \
golangci-lint run ./pkg/overmind/... ./cmd/overmind/
```
Expected: build OK; `-race` clean on the supervisor (validates the proc registry + reaping goroutine + `kill` have no data races); full suite PASS; 0 lint issues.

- [ ] **Step 2: Sanity-check the binary builds to bin/**

Run: `go build -o bin/overmind ./cmd/overmind/ && go build -o bin/worker ./cmd/worker/`
Expected: both build into `bin/`.

---

## Self-Review

**Spec coverage:**
- Per-agent process registry (spec §1) → Task 1.
- Restart decision matrix incl. booting/hung/dead + split BootTimeout/SilenceTimeout (spec §2) → Task 3 (timeouts/fields seeded in Task 1).
- Graceful SIGTERM→SIGKILL (spec §3) → Task 2.
- Staggered startup + restart-relaunch-immediate (spec §4) → Task 4 (restart path in Task 3 calls `launch` directly, not the staggered loop — relaunches stay immediate, matching the spec).
- Tunables + `--stagger` flag (spec §5) → Task 1 (fields/defaults) + Task 5 (flag).
- Testing cases (spec "Testing") → booting-not-respawned (Task 3), wedged (Task 3), hung (Task 3), dead (Task 3), healthy-untouched + counter clear (Task 3), SIGTERM→SIGKILL escalation (Task 2), stagger spacing (Task 4), MaxRestarts cap preserved (Task 3 `TestReapDeadWorkerRespawnedUpToCap`).

**Placeholder scan:** none — every code step shows full code and exact commands.

**Type consistency:** `workerProc{cmd,cancel,launchedAt,exited}` and `alive()` defined in Task 1 and used unchanged in Tasks 2–3. `tryRestart(ctx, spec, killed bool)` defined and called consistently in Task 3. `procSnapshot`/`procOf` are distinct names (production helper vs test helper) by design. `StaggerInterval`/`BootTimeout`/`KillGrace` defined in Task 1, consumed in Tasks 2–5. Defaults use real constants (`game.SleepTick`=10s, `game.SleepMedium`=5s).
