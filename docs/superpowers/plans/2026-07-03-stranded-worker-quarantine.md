# Stranded-Worker Quarantine + Assist Rescue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect workers stranded in a way a restart cannot fix (fuel-dead undocked, or repeated futile stall-restarts), pull them from the fleet into a visible quarantine with the game session freed, and rescue them automatically via the five `assist-<capital>` agents flying fuel out to them.

**Architecture:** Detection and quarantine live in `pkg/overmind/supervisor` (extending the existing stall watchdog). Cross-overmind communication is a flock-guarded JSON queue file in a new neutral package `pkg/rescue` (neutral so both the overmind and workers can import it). Rescue execution is a new `assist` standing behavior in `pkg/worker` run by a 4th overmind. Rejoin is automatic: the quarantining overmind polls the queue and relaunches workers whose record is `done`.

**Tech Stack:** Go, `syscall.Flock`, existing `pkg/galaxy` / `pkg/navigation` / `pkg/knowledge` routing, existing supervisor/worker/overmind patterns.

**Spec:** `docs/superpowers/specs/2026-07-03-stranded-worker-quarantine-design.md`

## Global Constraints

- Go 1.24+: use modern idioms (range over int, `b.Loop()` in benchmarks).
- Every task: `go build ./...` and `go test ./...` green, `golangci-lint run` introduces no new findings, before each commit.
- Sleeps only via `pkg/game/constants.go` constants; this plan adds **no new sleeps** (all polling piggybacks existing tickers/standing loops).
- Compiled binaries go to `bin/`, never the repo root.
- Constants (values from spec, verbatim): supervisor `FuelStrandFraction = 0.10`, `FuelStrandFloor = 10`, `StallRestartLimit = 3`; package `rescue` (prefix dropped, read as `rescue.FuelPerJump`): `FuelPerJump = 5`, `FuelBuffer = 5`, `FuelMin = 10`, `FuelFallback = 25`.
- Work on a feature branch off the current branch (`fix/shuttle-targeting`), e.g. `feat/stranded-quarantine`.
- Adding a method to `game.GameClient` breaks mock clients in `pkg/agent` and `pkg/skills` (and fakes in `pkg/worker` tests) — `go build ./...` does NOT catch mocks in test files; always run `go test ./...`.

---

## Phase 1 — Quarantine

### Task 1: Double-fire fix + stall-restart counter (Fleet)

**Files:**
- Modify: `pkg/overmind/supervisor/fleet.go`
- Test: `pkg/overmind/supervisor/fleet_test.go`

**Interfaces:**
- Consumes: existing `WorkerInfo`, `Fleet`, `Stalled()`, `statusProgressed()`.
- Produces: `WorkerInfo.StallRestarts int`; `func (f *Fleet) MarkStallRestart(agentID string)`; `MarkRestart` zeroing `LastProgress`; `ApplyStatus` resetting `StallRestarts` on progress. Task 2 and Task 5 rely on these exact names.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/overmind/supervisor/fleet_test.go`:

```go
func TestMarkRestartResetsProgressClock(t *testing.T) {
	f := NewFleet()
	now := time.Now()
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, now)
	f.ApplyStatus("a", control.Status{System: "x"}, now)

	f.MarkRestart("a")

	w := f.Snapshot()[0]
	if !w.LastProgress.IsZero() {
		t.Fatalf("MarkRestart must zero LastProgress, got %v", w.LastProgress)
	}
	// A zero LastProgress disables Stalled until the new process reports in —
	// this is the double-fire regression guard.
	if Stalled(w, now.Add(time.Hour), time.Minute) {
		t.Fatal("freshly restarted worker must not be Stalled")
	}
}

func TestStallRestartCounter(t *testing.T) {
	f := NewFleet()
	now := time.Now()
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, now)

	f.MarkStallRestart("a")
	f.MarkStallRestart("a")
	if got := f.Snapshot()[0].StallRestarts; got != 2 {
		t.Fatalf("StallRestarts = %d, want 2", got)
	}

	// Forward progress resets the counter.
	f.ApplyStatus("a", control.Status{System: "x"}, now)
	if got := f.Snapshot()[0].StallRestarts; got != 0 {
		t.Fatalf("progress must reset StallRestarts, got %d", got)
	}
}

func TestStallRestartCounterSurvivesIdenticalHeartbeat(t *testing.T) {
	f := NewFleet()
	now := time.Now()
	st := control.Status{System: "x", POI: "p", Fuel: 0, MaxFuel: 100}
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, now)
	f.ApplyStatus("a", st, now)
	f.MarkStallRestart("a")
	f.MarkRestart("a")
	// worker relaunches: Hello then an identical (still-stranded) heartbeat
	f.ApplyHello(control.Hello{AgentID: "a"}, 2, now.Add(time.Second))
	f.ApplyStatus("a", st, now.Add(2*time.Second))
	if got := f.Snapshot()[0].StallRestarts; got != 1 {
		t.Fatalf("identical post-restart heartbeat must not reset counter, got %d", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/overmind/supervisor/ -run 'TestMarkRestartResetsProgressClock|TestStallRestartCounter' -v`
Expected: FAIL — `f.MarkStallRestart undefined`, `w.StallRestarts undefined`.

- [ ] **Step 3: Implement**

In `pkg/overmind/supervisor/fleet.go`:

Add to `WorkerInfo` (after `Restarts int`):

```go
	// StallRestarts counts consecutive stall-watchdog restarts with no forward
	// progress in between. Progress (ApplyStatus with a changed status) resets
	// it. Reaching StallRestartLimit is the escalation signal for quarantine.
	StallRestarts int
```

Rewrite `ApplyStatus` body:

```go
func (f *Fleet) ApplyStatus(agentID string, st control.Status, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	progressed := statusProgressed(w.LastStatus, st)
	if w.LastProgress.IsZero() || progressed {
		w.LastProgress = now
	}
	if progressed {
		w.StallRestarts = 0
	}
	w.LastStatus, w.LastSeen, w.Healthy = st, now, true
}
```

Amend `MarkRestart` (the double-fire fix) and add `MarkStallRestart`:

```go
// MarkRestart increments the restart counter and marks the worker unhealthy.
// It also zeroes LastProgress so the stall watchdog cannot re-fire on the
// fresh process before it reports in (Stalled is disabled on a zero
// LastProgress; ApplyHello restarts the clock).
func (f *Fleet) MarkRestart(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.Restarts++
	w.Healthy = false
	w.LastProgress = time.Time{}
}

// MarkStallRestart records that the stall watchdog is restarting this worker.
func (f *Fleet) MarkStallRestart(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.get(agentID).StallRestarts++
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/overmind/supervisor/ -v`
Expected: all PASS (including the pre-existing suite).

- [ ] **Step 5: Build, lint, commit**

```bash
go build ./... && go test ./pkg/overmind/... && golangci-lint run pkg/overmind/...
git add pkg/overmind/supervisor/fleet.go pkg/overmind/supervisor/fleet_test.go
git commit -m "fix(overmind): stall watchdog double-fire; add consecutive stall-restart counter"
```

---

### Task 2: Quarantine state + `Stranded()` predicate (Fleet)

**Files:**
- Modify: `pkg/overmind/supervisor/fleet.go`
- Test: `pkg/overmind/supervisor/fleet_test.go`

**Interfaces:**
- Consumes: Task 1's `StallRestarts`.
- Produces (Task 5/6/7 rely on these exact names):
  - `WorkerInfo.Quarantined bool`, `WorkerInfo.QuarantineReason string`
  - `func (f *Fleet) Quarantine(agentID, reason string)` (also sets `Healthy = false`)
  - `func (f *Fleet) ClearQuarantine(agentID string)` (clears flag+reason, zeroes `LastProgress` and `StallRestarts` so the relaunched worker is not insta-requarantined)
  - `func (f *Fleet) IsQuarantined(agentID string) bool`
  - `func Stranded(info WorkerInfo, now time.Time, stallTimeout time.Duration, fuelFraction, fuelFloor float64, stallRestartLimit int) (bool, string)`

- [ ] **Step 1: Write the failing tests**

Append to `fleet_test.go`:

```go
func TestStranded(t *testing.T) {
	now := time.Now()
	base := func(mut func(*WorkerInfo)) WorkerInfo {
		w := WorkerInfo{
			AgentID:      "a",
			Role:         "hauler",
			LastProgress: now.Add(-time.Hour),
			LastStatus:   control.Status{Docked: false, Fuel: 2, MaxFuel: 420},
		}
		if mut != nil {
			mut(&w)
		}
		return w
	}
	timeout := 15 * time.Minute
	cases := []struct {
		name string
		w    WorkerInfo
		want bool
	}{
		{"fuel dead big tank", base(nil), true},                    // 2 < max(42, 10)
		{"fuel below floor small tank", base(func(w *WorkerInfo) { // 8 < max(5, 10)
			w.LastStatus.MaxFuel, w.LastStatus.Fuel = 50, 8
		}), true},
		{"fuel healthy", base(func(w *WorkerInfo) { w.LastStatus.Fuel = 60 }), false},
		{"not stalled yet", base(func(w *WorkerInfo) { w.LastProgress = now }), false},
		{"docked exempt", base(func(w *WorkerInfo) { w.LastStatus.Docked = true }), false},
		{"drained exempt", base(func(w *WorkerInfo) { w.LastStatus.Drained = true }), false},
		{"never seen exempt", base(func(w *WorkerInfo) { w.LastProgress = time.Time{} }), false},
		{"assist role exempt from fuel check", base(func(w *WorkerInfo) { w.Role = "assist" }), false},
		{"escalation trips regardless of fuel", base(func(w *WorkerInfo) {
			w.LastStatus.Fuel, w.StallRestarts = 400, 3
		}), true},
		{"escalation below limit", base(func(w *WorkerInfo) {
			w.LastStatus.Fuel, w.StallRestarts = 400, 2
		}), false},
		{"assist escalation still trips", base(func(w *WorkerInfo) {
			w.Role, w.StallRestarts = "assist", 3
		}), true},
	}
	for _, tc := range cases {
		got, reason := Stranded(tc.w, now, timeout, 0.10, 10, 3)
		if got != tc.want {
			t.Errorf("%s: Stranded = %v (reason %q), want %v", tc.name, got, reason, tc.want)
		}
		if got && reason == "" {
			t.Errorf("%s: stranded must carry a reason", tc.name)
		}
	}
}

func TestQuarantineLifecycle(t *testing.T) {
	f := NewFleet()
	now := time.Now()
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, now)
	f.ApplyStatus("a", control.Status{System: "x"}, now)
	f.MarkStallRestart("a")

	f.Quarantine("a", "fuel-dead")
	w := f.Snapshot()[0]
	if !w.Quarantined || w.QuarantineReason != "fuel-dead" || w.Healthy {
		t.Fatalf("quarantine not recorded: %+v", w)
	}
	if !f.IsQuarantined("a") {
		t.Fatal("IsQuarantined false after Quarantine")
	}

	f.ClearQuarantine("a")
	w = f.Snapshot()[0]
	if w.Quarantined || w.QuarantineReason != "" {
		t.Fatalf("quarantine not cleared: %+v", w)
	}
	if w.StallRestarts != 0 || !w.LastProgress.IsZero() {
		t.Fatalf("ClearQuarantine must reset stall state, got restarts=%d progress=%v",
			w.StallRestarts, w.LastProgress)
	}
	// Quarantine on an unknown agent creates the entry (boot-restore path).
	f.Quarantine("newcomer", "restored from queue")
	if !f.IsQuarantined("newcomer") {
		t.Fatal("Quarantine must create missing entries for boot restore")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/overmind/supervisor/ -run 'TestStranded|TestQuarantineLifecycle' -v`
Expected: FAIL — `Stranded` signature mismatch / fields undefined.

- [ ] **Step 3: Implement**

In `fleet.go`, add to `WorkerInfo`:

```go
	// Quarantined means the supervisor has pulled this worker from the fleet
	// (stranded — a restart cannot fix it) pending rescue. A quarantined
	// worker's process is stopped and it is never relaunched until
	// ClearQuarantine.
	Quarantined      bool
	QuarantineReason string
```

Add methods:

```go
// Quarantine flags a worker as pulled-from-fleet. Creates the entry if absent
// (the boot-time restore path runs before any Hello).
func (f *Fleet) Quarantine(agentID, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.Quarantined, w.QuarantineReason, w.Healthy = true, reason, false
}

// ClearQuarantine releases a worker for relaunch. Stall state is reset so the
// watchdog gives the rescued worker a fresh window before re-evaluating.
func (f *Fleet) ClearQuarantine(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.Quarantined, w.QuarantineReason = false, ""
	w.StallRestarts = 0
	w.LastProgress = time.Time{}
}

// IsQuarantined reports whether the worker is currently pulled from the fleet.
func (f *Fleet) IsQuarantined(agentID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	w, ok := f.workers[agentID]
	return ok && w.Quarantined
}

// Stranded reports whether a stalled worker is beyond what a restart can fix,
// and why. Two signals (spec 2026-07-03-stranded-worker-quarantine):
//   - fuel-dead: undocked + stalled + fuel below max(fuelFraction×MaxFuel,
//     fuelFloor) — it cannot move, so respawning is futile. Assist-role
//     workers are exempt (they legitimately run their tank down mid-rescue).
//   - escalation: stallRestartLimit consecutive stall-restarts produced no
//     progress — whatever is wrong, restarting is not fixing it.
func Stranded(info WorkerInfo, now time.Time, stallTimeout time.Duration, fuelFraction, fuelFloor float64, stallRestartLimit int) (bool, string) {
	if !Stalled(info, now, stallTimeout) {
		return false, ""
	}
	if stallRestartLimit > 0 && info.StallRestarts >= stallRestartLimit {
		return true, fmt.Sprintf("stalled: %d futile stall-restarts without progress", info.StallRestarts)
	}
	if info.Role == "assist" {
		return false, ""
	}
	st := info.LastStatus
	threshold := fuelFloor
	if frac := fuelFraction * st.MaxFuel; frac > threshold {
		threshold = frac
	}
	if st.Fuel < threshold {
		return true, fmt.Sprintf("fuel-dead: stalled >%s undocked, fuel %.0f/%.0f", stallTimeout, st.Fuel, st.MaxFuel)
	}
	return false, ""
}
```

(Add `"fmt"` to fleet.go imports.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/overmind/supervisor/ -v`
Expected: all PASS.

- [ ] **Step 5: Build, lint, commit**

```bash
go build ./... && go test ./pkg/overmind/... && golangci-lint run pkg/overmind/...
git add pkg/overmind/supervisor/fleet.go pkg/overmind/supervisor/fleet_test.go
git commit -m "feat(overmind): quarantine state + Stranded predicate (fuel-dead + escalation)"
```

---

### Task 3: Rescue queue package (`pkg/rescue`)

**Files:**
- Create: `pkg/rescue/queue.go`
- Test: `pkg/rescue/queue_test.go`

**Interfaces:**
- Consumes: nothing project-internal (stdlib + `syscall`).
- Produces (Tasks 4/7/9 rely on these exact names):

```go
type Status string
const (
	StatusPending Status = "pending"
	StatusClaimed Status = "claimed"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)
type Record struct { /* fields below */ }
func NewQueue(path string) *Queue
func (q *Queue) List() ([]Record, error)
func (q *Queue) Enqueue(rec Record) (bool, error)                 // false = agent already has a record
func (q *Queue) Transition(agentID string, from, to Status, mutate func(*Record)) (bool, error)
func (q *Queue) Remove(agentID string) (*Record, error)           // nil if absent
```

- [ ] **Step 1: Write the failing tests**

Create `pkg/rescue/queue_test.go`:

```go
package rescue

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestQueueLifecycle(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	ok, err := q.Enqueue(Record{AgentID: "trader-8", Fleet: "haul", RescueFuel: 15})
	if err != nil || !ok {
		t.Fatalf("enqueue: ok=%v err=%v", ok, err)
	}
	if ok, _ := q.Enqueue(Record{AgentID: "trader-8"}); ok {
		t.Fatal("duplicate enqueue must be rejected")
	}

	recs, err := q.List()
	if err != nil || len(recs) != 1 {
		t.Fatalf("list: %v %v", recs, err)
	}
	if recs[0].Status != StatusPending || recs[0].RequestedAt == "" || recs[0].UpdatedAt == "" {
		t.Fatalf("enqueue must default status/timestamps: %+v", recs[0])
	}

	ok, err = q.Transition("trader-8", StatusPending, StatusClaimed, func(r *Record) { r.ClaimedBy = "assist-sol" })
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if ok, _ := q.Transition("trader-8", StatusPending, StatusClaimed, nil); ok {
		t.Fatal("wrong-from transition must fail (CAS)")
	}
	if ok, _ := q.Transition("nobody", StatusPending, StatusClaimed, nil); ok {
		t.Fatal("unknown agent transition must fail")
	}
	if ok, _ := q.Transition("trader-8", StatusClaimed, StatusDone, nil); !ok {
		t.Fatal("done transition failed")
	}

	rec, err := q.Remove("trader-8")
	if err != nil || rec == nil || rec.ClaimedBy != "assist-sol" {
		t.Fatalf("remove: %+v %v", rec, err)
	}
	if rec, _ := q.Remove("trader-8"); rec != nil {
		t.Fatal("second remove must return nil")
	}
	if recs, _ := q.List(); len(recs) != 0 {
		t.Fatalf("queue should be empty, got %v", recs)
	}
}

func TestQueueMissingFileIsEmpty(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "absent.json"))
	recs, err := q.List()
	if err != nil || len(recs) != 0 {
		t.Fatalf("missing file: %v %v", recs, err)
	}
}

func TestQueueCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQueue(path).List(); err == nil {
		t.Fatal("corrupt file must return an error (caller logs and skips the tick)")
	}
}

func TestQueueConcurrentWriters(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "queue.json"))
	var wg sync.WaitGroup
	agents := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for _, id := range agents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := q.Enqueue(Record{AgentID: id}); err != nil {
				t.Errorf("enqueue %s: %v", id, err)
			}
		}()
	}
	wg.Wait()
	recs, err := q.List()
	if err != nil || len(recs) != len(agents) {
		t.Fatalf("want %d records, got %d (%v)", len(agents), len(recs), err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/rescue/ -v`
Expected: FAIL — package does not exist / does not compile.

- [ ] **Step 3: Implement**

Create `pkg/rescue/queue.go`:

```go
// Package rescue is the cross-overmind stranded-worker rescue channel: a
// flock-guarded JSON queue file. Fleet overminds enqueue quarantined workers;
// the assist overmind's workers claim and complete rescues; fleet overminds
// relaunch workers whose record is done. Operators edit the same file for
// manual rescues. Spec: docs/superpowers/specs/2026-07-03-stranded-worker-quarantine-design.md
package rescue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Status is a rescue record's lifecycle state.
type Status string

// Lifecycle: pending → claimed → done | failed. done triggers rejoin; failed
// waits for the operator.
const (
	StatusPending Status = "pending"
	StatusClaimed Status = "claimed"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Record is one stranded worker awaiting (or through) rescue.
type Record struct {
	AgentID        string  `json:"agent_id"`
	TargetUsername string  `json:"target_username"`
	Fleet          string  `json:"fleet"`
	System         string  `json:"system"`
	SystemID       string  `json:"system_id"`
	POI            string  `json:"poi"`
	Fuel           float64 `json:"fuel"`
	MaxFuel        float64 `json:"max_fuel"`
	RescueFuel     int     `json:"rescue_fuel"`
	Reason         string  `json:"reason"`
	Status         Status  `json:"status"`
	ClaimedBy      string  `json:"claimed_by"`
	Error          string  `json:"error,omitempty"`
	RequestedAt    string  `json:"requested_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// Queue is a handle on the shared queue file. Safe for concurrent use across
// processes: every operation takes an exclusive flock on a sidecar lock file
// (never renamed, so the lock identity is stable), then atomically rewrites
// the queue via temp+rename.
type Queue struct {
	path string
	now  func() time.Time
}

// NewQueue returns a queue handle on path. The file need not exist yet.
func NewQueue(path string) *Queue {
	return &Queue{path: path, now: time.Now}
}

// List returns all records. A missing file is an empty queue; a corrupt file
// is an error (callers log and skip the tick rather than clobbering it).
func (q *Queue) List() ([]Record, error) {
	var out []Record
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		out = recs
		return nil, false, nil
	})
	return out, err
}

// Enqueue appends rec (status pending, timestamps stamped) unless the agent
// already has a record of any status — one record per agent, so re-detection
// while a rescue is in flight is a no-op.
func (q *Queue) Enqueue(rec Record) (bool, error) {
	inserted := false
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for _, r := range recs {
			if r.AgentID == rec.AgentID {
				return nil, false, nil
			}
		}
		ts := q.now().UTC().Format(time.RFC3339)
		rec.Status = StatusPending
		rec.RequestedAt, rec.UpdatedAt = ts, ts
		inserted = true
		return append(recs, rec), true, nil
	})
	return inserted, err
}

// Transition moves the agent's record from → to (compare-and-set; false when
// the record is absent or not in from), applying mutate to the record first.
func (q *Queue) Transition(agentID string, from, to Status, mutate func(*Record)) (bool, error) {
	moved := false
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for i := range recs {
			if recs[i].AgentID != agentID || recs[i].Status != from {
				continue
			}
			if mutate != nil {
				mutate(&recs[i])
			}
			recs[i].Status = to
			recs[i].UpdatedAt = q.now().UTC().Format(time.RFC3339)
			moved = true
			return recs, true, nil
		}
		return nil, false, nil
	})
	return moved, err
}

// Remove deletes the agent's record and returns it (nil if absent).
func (q *Queue) Remove(agentID string) (*Record, error) {
	var removed *Record
	err := q.withLock(func(recs []Record) ([]Record, bool, error) {
		for i := range recs {
			if recs[i].AgentID == agentID {
				r := recs[i]
				removed = &r
				return append(recs[:i], recs[i+1:]...), true, nil
			}
		}
		return nil, false, nil
	})
	return removed, err
}

// withLock runs fn over the current records under an exclusive flock. fn
// returns (newRecords, write, err); when write is true the queue file is
// atomically replaced. The lock lives on a sidecar .lock file so the rename
// never invalidates a held lock.
func (q *Queue) withLock(fn func([]Record) ([]Record, bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return fmt.Errorf("rescue queue: mkdir: %w", err)
	}
	lock, err := os.OpenFile(q.path+".lock", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("rescue queue: open lock: %w", err)
	}
	defer lock.Close() //nolint:errcheck
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("rescue queue: flock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	var recs []Record
	raw, err := os.ReadFile(q.path)
	switch {
	case os.IsNotExist(err):
		// empty queue
	case err != nil:
		return fmt.Errorf("rescue queue: read: %w", err)
	case len(raw) > 0:
		if err := json.Unmarshal(raw, &recs); err != nil {
			return fmt.Errorf("rescue queue: parse %s: %w", q.path, err)
		}
	}

	next, write, err := fn(recs)
	if err != nil || !write {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("rescue queue: marshal: %w", err)
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("rescue queue: write tmp: %w", err)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("rescue queue: rename: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/rescue/ -race -v`
Expected: all PASS (race detector on, for the concurrent test).

- [ ] **Step 5: Build, lint, commit**

```bash
go build ./... && go test ./pkg/rescue/ -race && golangci-lint run pkg/rescue/...
git add pkg/rescue/
git commit -m "feat(rescue): flock-guarded cross-overmind rescue queue file"
```

---

### Task 4: Enqueue enrichment — username, system id, rescue fuel

**Files:**
- Create: `pkg/rescue/enrich.go`
- Test: `pkg/rescue/enrich_test.go`

**Interfaces:**
- Consumes: `game.LoadCredentials(agentDir)` (`pkg/game/agent.go:69`, struct `game.Credentials{Username, Password, Empire}`); `knowledge.System{ID, Name}`; `galaxy.GalaxyGraph.BuildFromDB(ctx, kb)` + `galaxy.FindNearestByPOIType(ctx, kb, graph, fromSystem, "station", 1)` returning `[]NearestResult{SystemID, SystemName, Hops}`.
- Produces (Task 7 relies on these exact names):

```go
const (
	FuelPerJump  = 5
	FuelBuffer   = 5
	FuelMin      = 10
	FuelFallback = 25
)
func ResolveUsername(agentsDir, agentID string) (string, error)
func ResolveSystemID(systems []knowledge.System, nameOrID string) (string, bool)
func FuelForSystem(ctx context.Context, kb knowledge.Base, systemID string) (int, error) // error ⇒ caller uses FuelFallback
func fuelForHops(hops int) int
```

- [ ] **Step 1: Write the failing tests**

Create `pkg/rescue/enrich_test.go`:

```go
package rescue

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestFuelForHops(t *testing.T) {
	cases := []struct{ hops, want int }{
		{0, 10}, // 5*0+5=5 → floor 10
		{1, 10}, // 5+5=10
		{2, 15},
		{5, 30},
	}
	for _, tc := range cases {
		if got := fuelForHops(tc.hops); got != tc.want {
			t.Errorf("fuelForHops(%d) = %d, want %d", tc.hops, got, tc.want)
		}
	}
}

func TestResolveUsername(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "trader-8")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	creds := `{"username": "Jaxon 'JunkKing' Jarvis", "password": "x", "empire": "nebula"}`
	if err := os.WriteFile(filepath.Join(agentDir, "credentials.json"), []byte(creds), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveUsername(dir, "trader-8")
	if err != nil || got != "Jaxon 'JunkKing' Jarvis" {
		t.Fatalf("ResolveUsername = %q, %v", got, err)
	}
	if _, err := ResolveUsername(dir, "missing-agent"); err == nil {
		t.Fatal("missing agent must error")
	}
}

func TestResolveSystemID(t *testing.T) {
	systems := []knowledge.System{
		{ID: "first_step", Name: "First Step"},
		{ID: "bd20_2457", Name: "BD+20 2457"},
	}
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"first_step", "first_step", true}, // already an id
		{"First Step", "first_step", true}, // display name
		{"bd+20 2457", "bd20_2457", true},  // name, case-insensitive
		{"Atlantis", "", false},
	}
	for _, tc := range cases {
		got, ok := ResolveSystemID(systems, tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ResolveSystemID(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/rescue/ -run 'TestFuelForHops|TestResolveUsername|TestResolveSystemID' -v`
Expected: FAIL — undefined functions.

- [ ] **Step 3: Implement**

Create `pkg/rescue/enrich.go`:

```go
package rescue

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Rescue-fuel sizing (spec: 5 per jump to the nearest station + one jump's
// slack for the intra-system leg; floor 10; fallback when routing fails).
const (
	FuelPerJump  = 5
	FuelBuffer   = 5
	FuelMin      = 10
	FuelFallback = 25
)

func fuelForHops(hops int) int {
	f := FuelPerJump*hops + FuelBuffer
	if f < FuelMin {
		return FuelMin
	}
	return f
}

// ResolveUsername reads the agent's in-game username from
// <agentsDir>/<agentID>/credentials.json. In-game usernames differ from the
// on-disk agent aliases; refuel --target needs the in-game one.
func ResolveUsername(agentsDir, agentID string) (string, error) {
	creds, err := game.LoadCredentials(filepath.Join(agentsDir, agentID))
	if err != nil {
		return "", fmt.Errorf("rescue: credentials for %s: %w", agentID, err)
	}
	return creds.Username, nil
}

// ResolveSystemID maps a system display name (what worker heartbeats carry,
// e.g. "First Step") or an id to the KB system id. Exact id match wins;
// otherwise a case-insensitive name match.
func ResolveSystemID(systems []knowledge.System, nameOrID string) (string, bool) {
	for _, s := range systems {
		if s.ID == nameOrID {
			return s.ID, true
		}
	}
	for _, s := range systems {
		if strings.EqualFold(s.Name, nameOrID) {
			return s.ID, true
		}
	}
	return "", false
}

// FuelForSystem sizes the rescue transfer for a strandee in systemID: BFS to
// the nearest station-bearing system, then fuelForHops. On any routing
// failure the caller should fall back to FuelFallback.
func FuelForSystem(ctx context.Context, kb knowledge.Base, systemID string) (int, error) {
	graph := &galaxy.GalaxyGraph{}
	if err := graph.BuildFromDB(ctx, kb); err != nil {
		return FuelFallback, fmt.Errorf("rescue: build graph: %w", err)
	}
	near, err := galaxy.FindNearestByPOIType(ctx, kb, graph, systemID, "station", 1)
	if err != nil {
		return FuelFallback, fmt.Errorf("rescue: nearest station from %s: %w", systemID, err)
	}
	if len(near) == 0 {
		return FuelFallback, fmt.Errorf("rescue: no station reachable from %s", systemID)
	}
	return fuelForHops(near[0].Hops), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/rescue/ -v`
Expected: all PASS. (`FuelForSystem`'s KB glue is exercised by compile + the galaxy package's own tests; its arithmetic is covered via `fuelForHops`.)

- [ ] **Step 5: Build, lint, commit**

```bash
go build ./... && go test ./pkg/rescue/ && golangci-lint run pkg/rescue/...
git add pkg/rescue/enrich.go pkg/rescue/enrich_test.go
git commit -m "feat(rescue): enqueue enrichment — username, system id, distance-sized rescue fuel"
```

---

### Task 5: Supervisor quarantine wiring

**Files:**
- Modify: `pkg/overmind/supervisor/supervisor.go`
- Test: `pkg/overmind/supervisor/supervisor_test.go`

**Interfaces:**
- Consumes: Task 1/2 Fleet methods, `Stranded()`.
- Produces (Task 7 relies on these): Supervisor fields `FuelStrandFraction float64`, `FuelStrandFloor float64`, `StallRestartLimit int`, `OnQuarantine func(w WorkerInfo, reason string)`; method `func (s *Supervisor) ReleaseQuarantine(agentID string)` (thread-safe, effective on the next reap tick).

- [ ] **Step 1: Write the failing test**

Append to `pkg/overmind/supervisor/supervisor_test.go`:

```go
func TestStrandedWorkerQuarantinedAndReleased(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "dead"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.StallTimeout = time.Nanosecond
	sup.KillGrace = time.Second
	var quarantines atomic.Int32
	sup.OnQuarantine = func(w WorkerInfo, reason string) {
		if w.AgentID != "dead" || reason == "" {
			t.Errorf("bad quarantine callback: %q %q", w.AgentID, reason)
		}
		quarantines.Add(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "dead", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("dead", control.Status{Fuel: 0, MaxFuel: 420}, now)

	sup.reapAndRestart(ctx) // fuel-dead + stalled(1ns) → quarantine, not restart
	if quarantines.Load() != 1 {
		t.Fatalf("want 1 quarantine callback, got %d", quarantines.Load())
	}
	if !fleet.IsQuarantined("dead") {
		t.Fatal("worker not marked quarantined")
	}
	if spawned.Load() != 1 {
		t.Fatalf("quarantined worker must not respawn, got %d spawns", spawned.Load())
	}

	sup.reapAndRestart(ctx) // still held
	if spawned.Load() != 1 || quarantines.Load() != 1 {
		t.Fatalf("quarantine must hold: spawns=%d quarantines=%d", spawned.Load(), quarantines.Load())
	}

	sup.ReleaseQuarantine("dead")
	sup.reapAndRestart(ctx) // released → relaunch
	if spawned.Load() != 2 {
		t.Fatalf("released worker should relaunch, got %d spawns", spawned.Load())
	}
	if fleet.IsQuarantined("dead") {
		t.Fatal("release must clear the quarantine flag")
	}
}

func TestStallRestartStillFiresWhenFueled(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "stuck"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.StallTimeout = time.Nanosecond
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "stuck", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("stuck", control.Status{Fuel: 300, MaxFuel: 420}, now)

	sup.reapAndRestart(ctx) // fueled → plain stall restart, counter increments
	if spawned.Load() != 2 {
		t.Fatalf("fueled stalled worker should restart, got %d spawns", spawned.Load())
	}
	if got := fleet.Snapshot()[0].StallRestarts; got != 1 {
		t.Fatalf("stall restart must increment counter, got %d", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/overmind/supervisor/ -run 'TestStrandedWorkerQuarantinedAndReleased|TestStallRestartStillFiresWhenFueled' -v`
Expected: FAIL — fields/method undefined.

- [ ] **Step 3: Implement**

In `supervisor.go`:

Add fields to `Supervisor` (after `MaxRestarts`):

```go
	// Stranded-quarantine tuning (see Stranded in fleet.go). OnQuarantine is
	// invoked (from the reap goroutine) after a worker is quarantined so the
	// host can enqueue a rescue; nil-safe.
	FuelStrandFraction float64
	FuelStrandFloor    float64
	StallRestartLimit  int
	OnQuarantine       func(w WorkerInfo, reason string)

	// releases holds ReleaseQuarantine requests until the next reap tick, so
	// the restarts map stays single-goroutine.
	releaseMu sync.Mutex
	releases  []string
```

In `NewSupervisor`, add defaults after `MaxRestarts: 100,`:

```go
		FuelStrandFraction: 0.10, // fuel-dead when fuel < max(10% of tank, floor)
		FuelStrandFloor:    10,
		StallRestartLimit:  3, // quarantine after 3 futile stall-restarts
```

Add methods:

```go
// ReleaseQuarantine schedules a quarantined worker for relaunch on the next
// reap tick (after its rescue record is done). Safe from any goroutine.
func (s *Supervisor) ReleaseQuarantine(agentID string) {
	s.releaseMu.Lock()
	defer s.releaseMu.Unlock()
	s.releases = append(s.releases, agentID)
}

func (s *Supervisor) drainReleases() []string {
	s.releaseMu.Lock()
	defer s.releaseMu.Unlock()
	out := s.releases
	s.releases = nil
	return out
}
```

In `reapAndRestart`, at the very top (before the snapshot):

```go
	for _, id := range s.drainReleases() {
		s.fleet.ClearQuarantine(id)
		delete(s.restarts, id)
		s.logger.Printf("quarantine released for %q; relaunching", id)
	}
```

In the spec loop, immediately after `for _, spec := range s.specs {`:

```go
		if s.fleet.IsQuarantined(spec.AgentID) {
			continue // pulled from fleet; waiting on rescue
		}
```

Replace the `case seen && Stalled(...)` branch body with:

```go
		case seen && Stalled(w, now, s.StallTimeout):
			if stranded, reason := Stranded(w, now, s.StallTimeout, s.FuelStrandFraction, s.FuelStrandFloor, s.StallRestartLimit); stranded {
				// Beyond what a restart can fix: pull it from the fleet. The
				// kill frees the game session for the rescuer/operator.
				if s.logger != nil {
					s.logger.Printf("QUARANTINED %s: %s; rescue queued — no further restarts", spec.AgentID, reason)
				}
				s.kill(proc)
				s.fleet.Quarantine(spec.AgentID, reason)
				if s.OnQuarantine != nil {
					s.OnQuarantine(w, reason)
				}
				continue
			}
			// Heartbeating but frozen: undocked with no progress for a long
			// window (the station-less-pocket trap). A plain restart forces a
			// fresh login + reconcile, which re-runs the role's stranded-recovery
			// (e.g. the shuttle escape hatch), so the respawn actually recovers.
			if s.logger != nil {
				s.logger.Printf("stall watchdog: %s frozen undocked in %q for >%s (last progress %s); restarting",
					spec.AgentID, w.LastStatus.System, s.StallTimeout, w.LastProgress.Format(time.RFC3339))
			}
			s.fleet.MarkStallRestart(spec.AgentID)
			s.kill(proc)
			s.tryRestart(ctx, spec, true, &budget)
```

In `Run`, guard the initial launch loop the same way — inside `for i, spec := range s.specs {`, before the stagger wait:

```go
		if s.fleet.IsQuarantined(spec.AgentID) {
			continue // restored-from-queue quarantine: do not launch stranded
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/overmind/supervisor/ -race -v`
Expected: all PASS (the whole suite — the existing stall test `TestReapHungEstablishedWorker` etc. must stay green).

- [ ] **Step 5: Build, lint, commit**

```bash
go build ./... && go test ./pkg/overmind/... -race && golangci-lint run pkg/overmind/...
git add pkg/overmind/supervisor/
git commit -m "feat(overmind): quarantine stranded workers instead of thrashing restarts"
```

---

### Task 6: Surface quarantine in the status file

**Files:**
- Modify: `pkg/overmind/balances/balances.go` (LiveRecord)
- Modify: `cmd/overmind/main.go:202-253` (`recordBalances` mapping)
- Test: `pkg/overmind/balances/balances_test.go`

**Interfaces:**
- Consumes: `WorkerInfo.Quarantined/QuarantineReason` (Task 2).
- Produces: `LiveRecord.Quarantined bool` (`json:"quarantined,omitempty"`), `LiveRecord.QuarantineReason string` (`json:"quarantine_reason,omitempty"`).

- [ ] **Step 1: Write the failing test**

Append to `pkg/overmind/balances/balances_test.go`:

```go
func TestLiveRecordQuarantineFields(t *testing.T) {
	rec := LiveRecord{AgentID: "trader-8", Quarantined: true, QuarantineReason: "fuel-dead"}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"quarantined":true`, `"quarantine_reason":"fuel-dead"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("marshal missing %s: %s", want, data)
		}
	}
	// omitted when healthy — keeps the existing status files byte-compatible
	data, _ = json.Marshal(LiveRecord{AgentID: "ok"})
	if strings.Contains(string(data), "quarantine") {
		t.Errorf("quarantine fields must be omitempty: %s", data)
	}
}
```

(Add `"encoding/json"` and `"strings"` imports to the test file if absent.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/balances/ -run TestLiveRecordQuarantineFields -v`
Expected: FAIL — fields undefined.

- [ ] **Step 3: Implement**

In `balances.go`, add to `LiveRecord` after `Restarts int`:

```go
	// Quarantined mirrors the supervisor's pulled-from-fleet state; the reason
	// says why (e.g. "fuel-dead: ..."). Omitted for healthy workers.
	Quarantined      bool   `json:"quarantined,omitempty"`
	QuarantineReason string `json:"quarantine_reason,omitempty"`
```

In `cmd/overmind/main.go` `recordBalances`, add to the `balances.LiveRecord{...}` literal alongside `Healthy: w.Healthy, Restarts: w.Restarts,`:

```go
			Quarantined: w.Quarantined, QuarantineReason: w.QuarantineReason,
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/overmind/... -v`
Expected: PASS.

- [ ] **Step 5: Build, lint, commit**

```bash
go build ./... && go test ./pkg/overmind/... && golangci-lint run pkg/overmind/... cmd/overmind/...
git add pkg/overmind/balances/ cmd/overmind/main.go
git commit -m "feat(overmind): surface quarantine state in fleet status files"
```

---

### Task 7: Overmind wiring — enqueue on quarantine, boot restore, rejoin poll

**Files:**
- Create: `cmd/overmind/rescueops.go`
- Modify: `cmd/overmind/main.go`

**Interfaces:**
- Consumes: `rescue.Queue` (Task 3), enrichment (Task 4), `Supervisor.OnQuarantine`/`ReleaseQuarantine` (Task 5), `Fleet.Quarantine` (Task 2), `knowledge.NewSQLiteKB(knowledge.Config{DBPath, WAL})`.
- Produces: new flags `--rescue-queue`, `--rescue-history`, `--fleet-name`, `--kb-path`; functions `makeOnQuarantine`, `restoreQuarantine`, `pollRescues`, `appendRescueHistory` (all in `cmd/overmind`).

No unit tests in this task — it is glue in package main, matching the repo's existing `recordBalances`/`drainFleet` pattern; the logic it composes is tested in Tasks 1–5. Verification is compile + a live smoke test at deploy.

- [ ] **Step 1: Add flags and KB handle**

In `cmd/overmind/main.go`, alongside the existing flags (`main.go:28-37`):

```go
	rescueQueuePath := flag.String("rescue-queue", "data/overmind/rescue-queue.json", "Shared stranded-worker rescue queue file")
	rescueHistPath := flag.String("rescue-history", "data/overmind/rescue-history.jsonl", "Archive of completed rescue records")
	fleetName := flag.String("fleet-name", "", "Fleet name stamped on rescue records (default: socket basename)")
	kbPath := flag.String("kb-path", "data/spacemolt-knowledge.db", "Knowledge base for rescue-fuel routing")
```

After flag parsing, derive the default fleet name and open the KB (warn-and-continue, mirroring `cmd/worker/main.go:244-246`):

```go
	if *fleetName == "" {
		*fleetName = strings.TrimSuffix(filepath.Base(*socketPath), ".sock")
	}
	var kb knowledge.Base
	if sqliteKB, kbErr := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *kbPath, WAL: true}); kbErr != nil {
		logger.Printf("warning: open KB %s: %v (rescue fuel falls back to %d)", *kbPath, kbErr, rescue.FuelFallback)
	} else {
		kb = sqliteKB
	}
	queue := rescue.NewQueue(*rescueQueuePath)
```

(Add imports: `"path/filepath"`, `"strings"`, `"github.com/rsned/spacemolt/pkg/knowledge"`, `"github.com/rsned/spacemolt/pkg/rescue"`.)

- [ ] **Step 2: Create `cmd/overmind/rescueops.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// makeOnQuarantine builds the supervisor callback that files a rescue request
// for a quarantined worker: resolve the in-game username, resolve the system
// id, size the fuel transfer, enqueue. Every enrichment failure degrades
// gracefully (empty username / fallback fuel) — the record must always land.
func makeOnQuarantine(ctx context.Context, logger *log.Logger, queue *rescue.Queue, kb knowledge.Base, fleetName string) func(supervisor.WorkerInfo, string) {
	return func(w supervisor.WorkerInfo, reason string) {
		st := w.LastStatus
		rec := rescue.Record{
			AgentID: w.AgentID, Fleet: fleetName, Reason: reason,
			System: st.System, POI: st.POI, Fuel: st.Fuel, MaxFuel: st.MaxFuel,
			RescueFuel: rescue.FuelFallback,
		}
		if u, err := rescue.ResolveUsername("data/agents", w.AgentID); err != nil {
			logger.Printf("rescue: username for %s: %v (operator must fill target_username)", w.AgentID, err)
		} else {
			rec.TargetUsername = u
		}
		if kb != nil {
			if systems, err := kb.GetSystems(ctx); err == nil {
				if id, ok := rescue.ResolveSystemID(systems, st.System); ok {
					rec.SystemID = id
					if fuel, err := rescue.FuelForSystem(ctx, kb, id); err == nil {
						rec.RescueFuel = fuel
					} else {
						logger.Printf("rescue: fuel sizing for %s: %v (using fallback %d)", w.AgentID, err, rescue.FuelFallback)
					}
				} else {
					logger.Printf("rescue: cannot resolve system %q for %s (using fallback fuel)", st.System, w.AgentID)
				}
			} else {
				logger.Printf("rescue: GetSystems: %v", err)
			}
		}
		if ok, err := queue.Enqueue(rec); err != nil {
			logger.Printf("rescue: enqueue %s: %v", w.AgentID, err)
		} else if ok {
			logger.Printf("rescue: queued %s (%s @ %s/%s, %d fuel)", w.AgentID, reason, rec.System, rec.POI, rec.RescueFuel)
		}
	}
}

// restoreQuarantine runs once at boot, before the supervisor launches anyone:
// agents of this fleet with an open rescue record stay quarantined instead of
// launching stranded; done records archive immediately and launch normally.
func restoreQuarantine(logger *log.Logger, fleet *supervisor.Fleet, queue *rescue.Queue, histPath, fleetName string) {
	recs, err := queue.List()
	if err != nil {
		logger.Printf("rescue: boot queue read: %v (launching full roster)", err)
		return
	}
	for _, rec := range recs {
		if rec.Fleet != fleetName {
			continue
		}
		if rec.Status == rescue.StatusDone {
			archiveRescue(logger, queue, histPath, rec.AgentID)
			continue
		}
		fleet.Quarantine(rec.AgentID, rec.Reason)
		logger.Printf("rescue: %s restored to quarantine at boot (%s, status %s)", rec.AgentID, rec.Reason, rec.Status)
	}
}

// pollRescues runs each status tick: any of our quarantined workers whose
// record went done is archived and released for relaunch.
func pollRescues(logger *log.Logger, sup *supervisor.Supervisor, queue *rescue.Queue, histPath, fleetName string, snap []supervisor.WorkerInfo) {
	quarantined := false
	for _, w := range snap {
		if w.Quarantined {
			quarantined = true
			break
		}
	}
	if !quarantined {
		return
	}
	recs, err := queue.List()
	if err != nil {
		logger.Printf("rescue: queue read: %v", err)
		return
	}
	byAgent := make(map[string]rescue.Record, len(recs))
	for _, rec := range recs {
		byAgent[rec.AgentID] = rec
	}
	for _, w := range snap {
		if !w.Quarantined {
			continue
		}
		rec, ok := byAgent[w.AgentID]
		if !ok {
			// Operator deleted the record: treat as manually resolved.
			logger.Printf("rescue: no record for quarantined %s; releasing", w.AgentID)
			sup.ReleaseQuarantine(w.AgentID)
			continue
		}
		if rec.Fleet == fleetName && rec.Status == rescue.StatusDone {
			archiveRescue(logger, queue, histPath, w.AgentID)
			sup.ReleaseQuarantine(w.AgentID)
			logger.Printf("rescue: %s rescued (+%d fuel by %s); rejoining fleet", w.AgentID, rec.RescueFuel, rec.ClaimedBy)
		}
	}
}

// archiveRescue moves a record out of the queue into the history jsonl.
func archiveRescue(logger *log.Logger, queue *rescue.Queue, histPath, agentID string) {
	rec, err := queue.Remove(agentID)
	if err != nil || rec == nil {
		logger.Printf("rescue: archive %s: rec=%v err=%v", agentID, rec, err)
		return
	}
	line, err := json.Marshal(rec)
	if err != nil {
		logger.Printf("rescue: marshal history %s: %v", agentID, err)
		return
	}
	f, err := os.OpenFile(histPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Printf("rescue: open history: %v", err)
		return
	}
	defer f.Close() //nolint:errcheck
	if _, err := f.Write(append(line, '\n')); err != nil {
		logger.Printf("rescue: append history: %v", err)
	}
}
```

- [ ] **Step 3: Wire into main**

In `main.go`, after the supervisor is constructed (`main.go:49-77` area):

```go
	sup.OnQuarantine = makeOnQuarantine(ctx, logger, queue, kb, *fleetName)
	restoreQuarantine(logger, fleet, queue, *rescueHistPath, *fleetName)
```

(`restoreQuarantine` must run before `sup.Run(ctx)` starts launching.)

In the status ticker (`main.go:129-159`), after `recordBalances(...)`:

```go
			pollRescues(logger, sup, queue, *rescueHistPath, *fleetName, snap)
```

- [ ] **Step 4: Build, test, lint**

Run: `go build ./... && go test ./... && golangci-lint run cmd/overmind/...`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add cmd/overmind/
git commit -m "feat(overmind): rescue-queue wiring — enqueue on quarantine, boot restore, rejoin poll"
```

**Phase 1 is deployable here:** rebuild `bin/overmind` + `bin/worker`, restart fleets (SIGUSR1 drain first), and the 7 current strands quarantine within one stall window; manual rescue = refuel by hand, then set the record's `status` to `"done"`:
`jq '(.[] | select(.agent_id=="trader-8") | .status) = "done"' data/overmind/rescue-queue.json > /tmp/rq.json && mv /tmp/rq.json data/overmind/rescue-queue.json`

---

## Phase 2 — Assist rescue

### Task 8: Client `RefuelShip` (ship-to-ship fuel transfer)

**Files:**
- Modify: `pkg/game/client.go` (next to `Refuel`, `client.go:1980`)
- Modify: `pkg/game/interface.go:63` (Ship Maintenance group)
- Modify: `pkg/agent/runner.go` (dispatch case near the `"refuel"` case at `runner.go:567`; `isActionCommand` at `runner.go:872`)
- Modify: `pkg/agent/runner_test.go:79` (`mockGameClient`), `pkg/skills/client_dispatcher_test.go:438` (`mockGameClient`), plus any `pkg/worker` test fakes that implement `game.GameClient`

**Interfaces:**
- Produces: `RefuelShip(ctx context.Context, target string, quantity int) error` on `Client` and `GameClient`. Server API: `refuel(item_id?, quantity?, target?)` — target is the **in-game username**; quantity 0 omitted (server default).

- [ ] **Step 1: Add to the interface first (compile-driven TDD)**

In `pkg/game/interface.go`, under `// Ship Maintenance` next to `Refuel(ctx context.Context) error`:

```go
	// RefuelShip transfers fuel to another player's ship (requires a fitted
	// refuel_rig; target is the in-game username, not the on-disk alias).
	RefuelShip(ctx context.Context, target string, quantity int) error
```

- [ ] **Step 2: Run tests to see every implementer break**

Run: `go build ./... ; go test ./... 2>&1 | grep -l 'RefuelShip' ` — or just read the failures.
Expected: FAIL — `*game.Client`, both `mockGameClient`s, and any worker test fakes no longer satisfy `game.GameClient`.

- [ ] **Step 3: Implement the client method**

In `pkg/game/client.go`, directly after `Refuel` (line ~1990):

```go
// RefuelShip transfers fuel from this ship to target's ship (ship-to-ship,
// needs a refuel_rig fitted). quantity <= 0 lets the server pick its default.
func (c *Client) RefuelShip(ctx context.Context, target string, quantity int) error {
	payload := map[string]any{"target": target}
	if quantity > 0 {
		payload["quantity"] = quantity
	}
	msg := protocol.Message{
		Type:      "refuel",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return maybeGoalReached("refuel", err)
}
```

- [ ] **Step 4: Dispatch + classification + mocks**

In `pkg/agent/runner.go`, next to the `case "refuel":` at `runner.go:567`:

```go
	case "refuel_ship":
		r.logger.Printf("[%s] -> RefuelShip(%s)", r.agent.ID(), decision.Target)
		return r.gameClient.RefuelShip(actionCtx, decision.Target, 0)
```

In `isActionCommand` (`runner.go:872`), add `"refuel_ship"` to the action (tick-consuming) set, alongside `"refuel"`.

Add no-op stubs to both mocks (matching each file's existing stub style):

```go
func (m *mockGameClient) RefuelShip(ctx context.Context, target string, quantity int) error {
	return nil
}
```

Fix any `pkg/worker` test fakes the same way (compiler will list them).

- [ ] **Step 5: Run tests to verify everything passes**

Run: `go build ./... && go test ./...`
Expected: all green.

- [ ] **Step 6: Lint, commit**

```bash
golangci-lint run
git add pkg/game/ pkg/agent/ pkg/skills/ pkg/worker/
git commit -m "feat(game): RefuelShip ship-to-ship fuel transfer (refuel target/quantity)"
```

---

### Task 9: Assist standing behavior

**Files:**
- Create: `pkg/worker/assist.go`
- Test: `pkg/worker/assist_test.go`
- Modify: `pkg/worker/dispatch.go` (WorkerDispatch fields + `supported` list at `dispatch.go:47-55` + case in `Run`)
- Modify: `cmd/worker/main.go` (`--rescue-queue` flag; thread `--station` + queue into the dispatch)
- Modify: `data/overmind/roles.yaml` (add `assist` role)

**Interfaces:**
- Consumes: `rescue.Queue`/`Record`/`Status` (Task 3), `RefuelShip` (Task 8), `Autopilot(ctx, AutopilotDeps{Client, Out, OnWaypoint}, system, poi)` (`pkg/worker/autopilot.go`), `navigation.JumpGraphFromConnections` + `navigation.BFSJumps` + `navigation.RouteInf` (`pkg/navigation`).
- Produces:

```go
type RescueQueue interface {
	List() ([]rescue.Record, error)
	Transition(agentID string, from, to rescue.Status, mutate func(*rescue.Record)) (bool, error)
}
type AssistDeps struct {
	Client      game.GameClient
	KB          knowledge.Base
	Queue       RescueQueue
	Out         io.Writer
	AgentID     string
	HomeStation string
	// Navigate overrides Autopilot in tests; nil means real Autopilot.
	Navigate func(ctx context.Context, system, poi string) error
}
func Assist(ctx context.Context, deps AssistDeps) error
func assistElect(agentID string, homes map[string]string, strandSystemID string, graph navigation.JumpGraph) bool
var assistHomes = map[string]string{ /* agent id → home system id */ }
```

- [ ] **Step 1: Verify the five home-system ids against the KB**

Run:

```bash
sqlite3 data/spacemolt-knowledge.db "SELECT id, name FROM systems WHERE name IN ('Haven','Sol','Krynn','Frontier','Nexus Prime');"
```

Use the returned ids in `assistHomes` (expected shape: `haven`, `sol`, `krynn`, `frontier`, `nexus_prime` — adjust to actual output).

- [ ] **Step 2: Write the failing tests**

Create `pkg/worker/assist_test.go`:

```go
package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/navigation"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// line graph: h1 - m1 - strand - m2 - h2
func assistTestGraph() navigation.JumpGraph {
	return navigation.JumpGraph{
		"h1":     {"m1"},
		"m1":     {"h1", "strand"},
		"strand": {"m1", "m2"},
		"m2":     {"strand", "h2"},
		"h2":     {"m2"},
	}
}

func TestAssistElect(t *testing.T) {
	graph := assistTestGraph()
	homes := map[string]string{"assist-a": "h1", "assist-b": "h2"}
	cases := []struct {
		name    string
		agent   string
		strand  string
		want    bool
	}{
		{"equidistant tie goes to lexicographic smaller", "assist-a", "strand", true},
		{"equidistant tie loser", "assist-b", "strand", false},
		{"strictly closer wins", "assist-b", "m2", true},
		{"strictly farther loses", "assist-a", "m2", false},
		{"unknown agent never claims", "assist-x", "strand", false},
		{"unreachable system never claims", "assist-a", "nowhere", false},
	}
	for _, tc := range cases {
		if got := assistElect(tc.agent, homes, tc.strand, graph); got != tc.want {
			t.Errorf("%s: assistElect = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type fakeRescueQueue struct {
	recs []rescue.Record
}

func (f *fakeRescueQueue) List() ([]rescue.Record, error) { return f.recs, nil }
func (f *fakeRescueQueue) Transition(agentID string, from, to rescue.Status, mutate func(*rescue.Record)) (bool, error) {
	for i := range f.recs {
		if f.recs[i].AgentID == agentID && f.recs[i].Status == from {
			if mutate != nil {
				mutate(&f.recs[i])
			}
			f.recs[i].Status = to
			return true, nil
		}
	}
	return false, nil
}
```

Then the engine test. It drives `Assist` with the test override `Navigate` (records destinations, no real autopilot) and the package's existing fake `game.GameClient` (the one the shuttle/autopilot tests use — extend it to record `RefuelShip` calls):

```go
func TestAssistRunsClaimedRescue(t *testing.T) {
	// Worker already owns a claimed record (e.g. resumed after restart):
	// travel → RefuelShip(username, fuel) → done → home.
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-a",
	}}}
	var visited []string
	client := newFakeGameClient(t) // the package's existing fake; adapt name to actual helper
	deps := AssistDeps{
		Client: client, Queue: q, Out: io.Discard,
		AgentID: "assist-a", HomeStation: "h1_station",
		Navigate: func(ctx context.Context, system, poi string) error {
			visited = append(visited, system+"/"+poi)
			return nil
		},
	}
	if err := Assist(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if q.recs[0].Status != rescue.StatusDone {
		t.Fatalf("record status = %s, want done", q.recs[0].Status)
	}
	if got := client.refuelShipCalls; len(got) != 1 || got[0].target != "Big Jim" || got[0].quantity != 15 {
		t.Fatalf("RefuelShip calls = %+v", got)
	}
	if len(visited) == 0 || visited[0] != "strand/strand_star" {
		t.Fatalf("first hop must be the strandee, got %v", visited)
	}
}

func TestAssistFailureMarksFailed(t *testing.T) {
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-a",
	}}}
	client := newFakeGameClient(t)
	client.refuelShipErr = errors.New("target not found")
	deps := AssistDeps{
		Client: client, Queue: q, Out: io.Discard,
		AgentID: "assist-a", HomeStation: "h1_station",
		Navigate: func(ctx context.Context, system, poi string) error { return nil },
	}
	if err := Assist(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if q.recs[0].Status != rescue.StatusFailed || q.recs[0].Error == "" {
		t.Fatalf("failed rescue must mark record failed with error, got %+v", q.recs[0])
	}
}
```

Adapt `newFakeGameClient` / `refuelShipCalls` / `refuelShipErr` to the package's actual fake-client helper (the shuttle/autopilot test fake); add the recording fields to it. Note in a comment when the local fake also stubs `GetState` (the ensure-home no-op path needs a state or nil handling).

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestAssist' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 4: Implement `pkg/worker/assist.go`**

```go
package worker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// assistHomes maps each assist agent to its home capital's system id. The set
// is fixed (one agent per empire capital); the claim election in assistElect
// relies on every agent being able to compute all five distances locally.
var assistHomes = map[string]string{
	"assist-haven":    "haven",
	"assist-sol":      "sol",
	"assist-krynn":    "krynn",
	"assist-frontier": "frontier",
	"assist-nexus":    "nexus_prime",
}

// RescueQueue is the slice of rescue.Queue the assist behavior consumes.
type RescueQueue interface {
	List() ([]rescue.Record, error)
	Transition(agentID string, from, to rescue.Status, mutate func(*rescue.Record)) (bool, error)
}

// AssistDeps wires one assist worker: fly rescue fuel to quarantined workers
// from the shared queue, then return to the home capital and re-tank.
type AssistDeps struct {
	Client      game.GameClient
	KB          knowledge.Base
	Queue       RescueQueue
	Out         io.Writer
	AgentID     string
	HomeStation string // station POI id at the home capital (fleet yaml `station`)
	// Navigate overrides Autopilot in tests; nil uses the real thing.
	Navigate func(ctx context.Context, system, poi string) error
}

func (d AssistDeps) navigate(ctx context.Context, system, poi string) error {
	if d.Navigate != nil {
		return d.Navigate(ctx, system, poi)
	}
	return Autopilot(ctx, AutopilotDeps{Client: d.Client, Out: d.Out}, system, poi)
}

// Assist runs one pass of the assist standing behavior (the standing loop
// re-invokes it): resume an owned claim, else claim the nearest pending
// rescue, else make sure we are docked at home with a full tank.
func Assist(ctx context.Context, deps AssistDeps) error {
	recs, err := deps.Queue.List()
	if err != nil {
		fmt.Fprintf(deps.Out, "assist: queue read: %v\n", err) //nolint:errcheck
		return nil
	}
	for _, r := range recs {
		if r.Status == rescue.StatusClaimed && r.ClaimedBy == deps.AgentID {
			return runRescue(ctx, deps, r)
		}
	}
	if rec, ok := claimNearestPending(ctx, deps, recs); ok {
		return runRescue(ctx, deps, rec)
	}
	return assistEnsureHome(ctx, deps)
}

func claimNearestPending(ctx context.Context, deps AssistDeps, recs []rescue.Record) (rescue.Record, bool) {
	var graph navigation.JumpGraph
	for _, r := range recs {
		if r.Status != rescue.StatusPending || r.SystemID == "" {
			continue
		}
		if graph == nil {
			if deps.KB == nil {
				return rescue.Record{}, false
			}
			conns, err := deps.KB.GetConnections(ctx)
			if err != nil {
				fmt.Fprintf(deps.Out, "assist: connections: %v\n", err) //nolint:errcheck
				return rescue.Record{}, false
			}
			graph = navigation.JumpGraphFromConnections(conns)
		}
		if !assistElect(deps.AgentID, assistHomes, r.SystemID, graph) {
			continue
		}
		ok, err := deps.Queue.Transition(r.AgentID, rescue.StatusPending, rescue.StatusClaimed,
			func(rec *rescue.Record) { rec.ClaimedBy = deps.AgentID })
		if err != nil || !ok {
			continue // raced another rescuer; move on
		}
		r.Status, r.ClaimedBy = rescue.StatusClaimed, deps.AgentID
		return r, true
	}
	return rescue.Record{}, false
}

// assistElect reports whether agentID should claim a rescue in strandSystemID:
// its home is (one of) the nearest homes, ties broken by lexicographically
// smaller agent id. Deterministic per record, so all five agents agree without
// talking; the queue's CAS claim covers any leftover race.
func assistElect(agentID string, homes map[string]string, strandSystemID string, graph navigation.JumpGraph) bool {
	mySys, ok := homes[agentID]
	if !ok {
		return false
	}
	targets := make([]string, 0, len(homes))
	for _, sys := range homes {
		targets = append(targets, sys)
	}
	dist := navigation.BFSJumps(graph, strandSystemID, targets)
	my, ok := dist[mySys]
	if !ok || my >= navigation.RouteInf {
		return false
	}
	for id, sys := range homes {
		if id == agentID {
			continue
		}
		d, ok := dist[sys]
		if !ok || d >= navigation.RouteInf {
			continue
		}
		if d < my || (d == my && id < agentID) {
			return false
		}
	}
	return true
}

func runRescue(ctx context.Context, deps AssistDeps, rec rescue.Record) error {
	fail := func(stage string, err error) error {
		fmt.Fprintf(deps.Out, "assist: rescue %s failed at %s: %v\n", rec.AgentID, stage, err) //nolint:errcheck
		if _, terr := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, rescue.StatusFailed,
			func(r *rescue.Record) { r.Error = stage + ": " + err.Error() }); terr != nil {
			fmt.Fprintf(deps.Out, "assist: mark failed %s: %v\n", rec.AgentID, terr) //nolint:errcheck
		}
		return assistEnsureHome(ctx, deps)
	}
	fmt.Fprintf(deps.Out, "assist: rescuing %s at %s/%s (%d fuel)\n", rec.AgentID, rec.SystemID, rec.POI, rec.RescueFuel) //nolint:errcheck
	if err := deps.navigate(ctx, rec.SystemID, rec.POI); err != nil {
		return fail("travel", err)
	}
	if err := deps.Client.RefuelShip(ctx, rec.TargetUsername, rec.RescueFuel); err != nil {
		return fail("refuel", err)
	}
	if ok, err := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, rescue.StatusDone, nil); err != nil || !ok {
		fmt.Fprintf(deps.Out, "assist: mark done %s: ok=%v err=%v\n", rec.AgentID, ok, err) //nolint:errcheck
	}
	fmt.Fprintf(deps.Out, "assist: rescued %s (+%d fuel to %s)\n", rec.AgentID, rec.RescueFuel, rec.TargetUsername) //nolint:errcheck
	return assistEnsureHome(ctx, deps)
}

// assistEnsureHome parks the rescuer docked at its home capital with a full
// tank so the next rescue starts fresh. Best-effort: failures log and return
// nil so the standing loop retries next pass.
func assistEnsureHome(ctx context.Context, deps AssistDeps) error {
	home, ok := assistHomes[deps.AgentID]
	if !ok || deps.HomeStation == "" {
		fmt.Fprintf(deps.Out, "assist: no home configured for %s\n", deps.AgentID) //nolint:errcheck
		return nil
	}
	if st := deps.Client.GetState(); st != nil && st.System.ID == home && st.Doc {
		return nil
	}
	if err := deps.navigate(ctx, home, deps.HomeStation); err != nil {
		fmt.Fprintf(deps.Out, "assist: return home: %v\n", err) //nolint:errcheck
		return nil
	}
	if err := deps.Client.Dock(ctx); err != nil && !strings.Contains(err.Error(), "Already docked") {
		fmt.Fprintf(deps.Out, "assist: dock home: %v\n", err) //nolint:errcheck
		return nil
	}
	if err := deps.Client.Refuel(ctx); err != nil {
		fmt.Fprintf(deps.Out, "assist: home refuel: %v\n", err) //nolint:errcheck
	}
	return nil
}
```

(Adjust `st.System.ID` / `st.Doc` to the actual `game.State` field names — the shuttle engine reads the same two; copy its accessors.)

- [ ] **Step 5: Wire dispatch, worker main, roles**

`pkg/worker/dispatch.go`:
- Add fields to `WorkerDispatch`: `Station string` and `Rescue RescueQueue`.
- Add `"assist"` to the `supported` command list (`dispatch.go:47-55`).
- Add the case in `Run` next to `case "shuttle":`:

```go
	case "assist":
		if d.Rescue == nil {
			return fmt.Errorf("assist: no rescue queue configured (--rescue-queue)")
		}
		return Assist(ctx, AssistDeps{
			Client: d.Client, KB: d.KB, Queue: d.Rescue, Out: d.Out,
			AgentID: d.AgentID, HomeStation: d.Station,
		})
```

`cmd/worker/main.go`:
- Add flag: `rescueQueuePath := flag.String("rescue-queue", "data/overmind/rescue-queue.json", "Shared stranded-worker rescue queue file")`
- Where the dispatch is built (`main.go:261-315`, `dispatch.AgentID = *agentID`), add:

```go
	dispatch.Station = *station
	dispatch.Rescue = rescue.NewQueue(*rescueQueuePath)
```

(`*rescue.Queue` satisfies `worker.RescueQueue`. Import `"github.com/rsned/spacemolt/pkg/rescue"`.)

`data/overmind/roles.yaml` — add:

```yaml
  assist:
    idle: assist
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -v && go test ./...`
Expected: all PASS — including `roles_test.go`'s supported-command drift guard picking up `assist`.

- [ ] **Step 7: Lint, commit**

```bash
go build ./... && golangci-lint run pkg/worker/... cmd/worker/...
git add pkg/worker/ cmd/worker/main.go data/overmind/roles.yaml
git commit -m "feat(worker): assist standing behavior — claim, fly, refuel, return home"
```

---

### Task 10: Assist fleet config + deployment runbook

**Files:**
- Create: `data/overmind/assist-fleet.yaml`
- Modify: `docs/superpowers/specs/2026-07-03-stranded-worker-quarantine-design.md` (append the runbook as a Deployment section)

- [ ] **Step 1: Find each capital's station POI id**

```bash
for sys in haven sol krynn frontier nexus_prime; do
  sqlite3 data/spacemolt-knowledge.db "SELECT system_id, id, name FROM pois WHERE system_id='$sys' AND type='station';"
done
```

Pick the public station per capital (Haven's is `grand_exchange`, Sol's `sol_central`, Nexus Prime's `the_core` per fleet-status data; confirm Krynn and Frontier — note Frontier's stations may lack public-access base rows, the known KB gap; if so, pick the one the assist agent can actually dock at, verified live).

- [ ] **Step 2: Write `data/overmind/assist-fleet.yaml`**

```yaml
workers:
  - { agent_id: assist-haven, role: assist, station: grand_exchange }
  - { agent_id: assist-sol, role: assist, station: sol_central }
  - { agent_id: assist-krynn, role: assist, station: war_citadel }
  - { agent_id: assist-frontier, role: assist, station: expedition_launch }
  - { agent_id: assist-nexus, role: assist, station: the_core }
```

(Station values from Step 1 — the above are best-known guesses; correct them against the query output.)

- [ ] **Step 3: Verify credentials + refuel_rig fit**

```bash
for a in assist-haven assist-sol assist-krynn assist-frontier assist-nexus; do
  test -f data/agents/$a/credentials.json && echo "$a creds OK" || echo "$a MISSING CREDS"
done
```

Fitted `refuel_rig` per agent is a live-game check (the user said they fly starter ships with refuel rigs); note any gaps for the user rather than guessing.

- [ ] **Step 4: Append the deployment runbook to the spec doc**

```markdown
## Deployment

Rebuild: `go build -o bin/overmind ./cmd/overmind && go build -o bin/worker ./cmd/worker`

Assist overmind (4th fleet):
setsid nohup ./bin/overmind --socket data/overmind/assist.sock \
  --fleet data/overmind/assist-fleet.yaml --worker-bin bin/worker \
  --status-file data/overmind/assist-status.json \
  --history-file data/overmind/assist-history.jsonl \
  --stagger 10s >> data/overmind/assist-overmind.log 2>&1 &

Fleet overminds pick up quarantine on their next binary restart (drain first:
kill -USR1 <pid>). Manual rescue: refuel by hand, then mark the record done —
jq '(.[] | select(.agent_id=="X") | .status) = "done"' data/overmind/rescue-queue.json > /tmp/rq && mv /tmp/rq data/overmind/rescue-queue.json
```

- [ ] **Step 5: Check .gitignore, commit**

```bash
git check-ignore -v data/overmind/assist-fleet.yaml || echo "not ignored, good"
git add data/overmind/assist-fleet.yaml docs/superpowers/specs/2026-07-03-stranded-worker-quarantine-design.md
git commit -m "feat(overmind): assist fleet roster + deployment runbook"
```

---

## Live validation (after both phases)

1. Deploy phase 1 to the haul overmind first (it owns all 7 current strands): drain (`kill -USR1`), stop, relaunch on the new binary. Within one stall window all 7 should log `QUARANTINED`, their processes stop, `rescue-queue.json` gains 7 pending records with usernames and sized fuel, and `fleet-status.json` shows `"quarantined": true`.
2. Launch the assist overmind. Watch one rescue end-to-end: claim → travel → `refuel --target` → `done` → hauler relaunch → hauler autopilots to a station and refuels.
3. Confirm the remaining 6 drain through the queue; confirm assist agents return home and re-tank between rescues.
4. Regression: verify no marketbot/shuttle quarantines (they are docked/mobile), and that the stall watchdog still plain-restarts a fueled stalled worker.
