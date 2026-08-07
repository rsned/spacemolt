# Mission + Freight Scan-Rate Backoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop workers re-reading the mission and freight boards every ~6 seconds. Gate only the *search for new work* behind a per-agent interval — 60s after activity, 4 minutes at rest — while leaving in-flight freight, delivery and transit running at today's cadence.

**Architecture:** A small `pollGate` value type in `pkg/worker` holding one timestamp, carried on the existing per-agent `missionRunState` (already the home for cross-pass memory like `parkedUntil`). `Missions()` consults it before the two new-work searches and reports the outcome back to it. Nothing else in the pass changes.

**Tech Stack:** Go 1.24, existing `pkg/worker` mission path, `MissionDeps.Now` for injected time.

## Scope

Implements `docs/superpowers/specs/2026-08-07-mission-freight-scan-backoff-design.md` (phase 1).

**In scope:** the mission board read and the freight contract search inside `Missions()`.

**Explicitly out of scope:** the generic idle-loop backoff for all roles (phase 2, deferred — haul's arbitrage pool refreshes every 10 minutes and polling it slowly costs real credits, so it must not change on the same pass).

## Global Constraints

- Go 1.24. Modern idioms (`for i := range n`).
- `golangci-lint run` must report zero new findings. This config wants a blank line before a final `return` in a multi-statement block — match the surrounding style in `pkg/worker`.
- `go build ./...` and `go test ./pkg/worker/ -count=1` must pass. **Always pass `-count=1`** — a cached PASS has previously hidden a real compile break in this repo.
- `pkg/worker` tests are slow (~150s). Budget for it; do not skip them.
- Any sleep or pause must use a predefined constant from `pkg/game/constants.go`. **This plan introduces two new duration constants** (below) — they are poll *intervals*, not sleeps, and live in `pkg/worker/mission.go` beside `missionParkWindow`, which is the established precedent for a mission-path duration.
- Commit messages end with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`
- **Stage files explicitly by path. NEVER `git add -A`** — a live 110-worker fleet continuously rewrites `data/agents/*/schedule.json` and `data/overmind/*`.
- Do not touch `data/` at all in this plan.

## Verified facts this plan depends on

Confirmed against the code on 2026-08-07. Do not re-derive.

| Thing | Location | Note |
|---|---|---|
| `missionRunState` | `pkg/worker/mission.go:69` | cross-pass memory; already holds `dry`, `cursor`, `hopsDry`, `parkedUntil` |
| owner of that state | `pkg/worker/dispatch.go:158` | `mission: &missionRunState{}` on `WorkerDispatch` — one per worker process, in-memory |
| clock injection | `MissionDeps.Now func() time.Time` (`mission.go:300`), helper `missionNow(deps)` (`mission.go:369`) | already exists; tests inject it |
| mission board read | `missionReadBoard(ctx, deps, out)` defined `mission.go:1240`, **called once at `mission.go:638`** | issues `GetMissions` |
| freight search | `if deps.EnableFreight { ... freightCandidate(...) }` block beginning `mission.go:612`, call at `:630` | the freight *search* |
| freight reconcile | separate `if deps.EnableFreight` block at `mission.go:484` | **MUST NOT be gated** — see below |
| dry counter | `missionDryPass(...)` `mission.go:1122`, increments `deps.State.dry` at `:1128` | drives repositioning |
| reposition threshold | `missionDryPassLimit = 3` (`mission.go:25`) | 3 consecutive dry passes ⇒ hop |

**The safety property this whole design rests on:** the freight *reconcile* block at `mission.go:484` runs before the search and handles already-held contracts. Its own comment states reconcile "must precede EVERY early return". Gating it would strand in-flight freight. **Gate the search at `:612` and the board read at `:638`. Never gate `:484`.**

---

### Task 1: The `pollGate` type

**Files:**
- Create: `pkg/worker/pollgate.go`
- Create: `pkg/worker/pollgate_test.go`

**Interfaces:**
- Produces:
  ```go
  type pollGate struct{ nextAt time.Time }
  func (g *pollGate) shouldPoll(now time.Time) bool
  func (g *pollGate) noteResult(foundWork bool, now time.Time)
  ```
  Zero value polls immediately (a fresh worker looks straight away). `noteResult(true, …)` schedules the next poll `boardPollFast` out; `noteResult(false, …)` schedules it `boardPollRest` out.

- [ ] **Step 1: Write the failing test**

Create `pkg/worker/pollgate_test.go`:

```go
package worker

import (
	"testing"
	"time"
)

// TestPollGateZeroValuePollsImmediately pins that a fresh worker looks for work
// on its very first pass rather than waiting out an interval it never earned.
func TestPollGateZeroValuePollsImmediately(t *testing.T) {
	var g pollGate
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	if !g.shouldPoll(now) {
		t.Error("zero-value gate must allow the first poll")
	}
}

// TestPollGateIntervals pins the two-state cadence: a productive read comes back
// quickly, a dry read backs off. These are the only two states by design -- there
// is no ladder to expire, because the next dry read relaxes the interval itself.
func TestPollGateIntervals(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		found     bool
		wait      time.Duration
		wantPoll  bool
	}{
		{"found work, just before the fast interval", true, boardPollFast - time.Second, false},
		{"found work, at the fast interval", true, boardPollFast, true},
		{"dry, one minute later", false, time.Minute, false},
		{"dry, just before the rest interval", false, boardPollRest - time.Second, false},
		{"dry, at the rest interval", false, boardPollRest, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g pollGate
			g.noteResult(tt.found, base)
			if got := g.shouldPoll(base.Add(tt.wait)); got != tt.wantPoll {
				t.Errorf("shouldPoll after %s = %v, want %v", tt.wait, got, tt.wantPoll)
			}
		})
	}
}

// TestPollGateProductiveReadShortensAnExistingBackoff pins the snap-back: an
// agent that has been idling on the 4-minute interval and then finds work must
// return to the fast interval, not serve out the old backoff.
func TestPollGateProductiveReadShortensAnExistingBackoff(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	var g pollGate

	g.noteResult(false, base) // dry: next poll at +4min
	at := base.Add(boardPollRest)
	if !g.shouldPoll(at) {
		t.Fatalf("gate should have opened at the rest interval")
	}
	g.noteResult(true, at) // this read found work

	if g.shouldPoll(at.Add(boardPollFast - time.Second)) {
		t.Error("must still wait out the fast interval")
	}
	if !g.shouldPoll(at.Add(boardPollFast)) {
		t.Error("must poll again one fast interval after finding work")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run TestPollGate -count=1 -v`
Expected: FAIL — `undefined: pollGate`, `undefined: boardPollFast`, `undefined: boardPollRest`.

- [ ] **Step 3: Write the implementation**

Create `pkg/worker/pollgate.go`:

```go
package worker

import "time"

const (
	// boardPollFast is the floor between new-work searches: even a worker that
	// just accepted something waits this long before looking again.
	//
	// An action costs one tick to execute and another to hear back, so nothing
	// can have changed inside 2*game.SleepTick (20s); boards specifically turn
	// over far slower than that, and the operator's floor for them is a minute.
	boardPollFast = time.Minute
	// boardPollRest is the interval once a search comes back dry. Boards change
	// on the order of minutes, and the measured cadence before this change was
	// ~6s -- roughly 40x more often than there was anything new to find.
	boardPollRest = 4 * time.Minute
)

// pollGate throttles the search for new work to an interval that reflects how
// fast the thing being polled actually changes.
//
// Two states, one timestamp, no ladder to expire: a productive search schedules
// the next one at boardPollFast, a dry one at boardPollRest. The relaxation is
// self-managing because the next dry result pushes the interval back out.
//
// The zero value polls immediately, so a fresh or restarted worker looks for
// work on its first pass rather than serving out an interval it never earned.
type pollGate struct {
	nextAt time.Time
}

// shouldPoll reports whether the caller may search for new work now.
func (g *pollGate) shouldPoll(now time.Time) bool {
	return !now.Before(g.nextAt)
}

// noteResult records the outcome of a search and schedules the next one.
func (g *pollGate) noteResult(foundWork bool, now time.Time) {
	if foundWork {
		g.nextAt = now.Add(boardPollFast)

		return
	}
	g.nextAt = now.Add(boardPollRest)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestPollGate -count=1 -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Verify and lint**

Run: `go build ./... && go test ./pkg/worker/ -count=1 && golangci-lint run pkg/worker/...`
Expected: build clean, package green, zero lint findings.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/pollgate.go pkg/worker/pollgate_test.go
git commit -m "feat(worker): pollGate for throttling new-work searches

Two states and one timestamp: a productive search schedules the next at 60s,
a dry one at 4 minutes. The zero value polls immediately so a restarted worker
does not serve out an interval it never earned.

The 60s floor comes from the tick model -- an action costs a tick to execute
and a tick to hear back, so nothing can change inside 20s, and boards turn over
far slower than that.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Gate the new-work searches

**Files:**
- Modify: `pkg/worker/mission.go` (add the field to `missionRunState`; gate the freight search and the board read; protect the dry counter)
- Modify: `pkg/worker/mission_test.go` (append)

**Interfaces:**
- Consumes: `pollGate`, `boardPollFast`, `boardPollRest` from Task 1.
- Produces: `missionRunState` gains a `boards pollGate` field. No exported surface changes.

**What to gate — read this before editing.** `Missions()` has two `if deps.EnableFreight` blocks and they are not interchangeable:

| Location | What it is | Gate it? |
|---|---|---|
| `mission.go:484` | freight **reconcile** of already-held contracts | **NO — never.** Its own comment says reconcile "must precede EVERY early return". Gating it strands in-flight freight. |
| `mission.go:612` | freight **search** (`freightCandidate` at `:630`) | yes |
| `mission.go:638` | `missionReadBoard` — the mission board read | yes |

- [ ] **Step 1: Write the failing tests**

Append to `pkg/worker/mission_test.go`. These use the harness already in that file:
`missionDeps(fc, store, kb)` (`:209`), `fakeClient` with its `calls []string` recorder,
`missionState(docked, credits, …)`, `boardJSON(t, entries…)`, `missionKB()`,
`fakeMissionStore`. Do **not** introduce a second fake.

```go
// TestMissionsGateSuppressesTheBoardRead pins the whole point of the change: a
// second pass inside the backoff window must not issue get_missions at all. The
// measured cadence before this was ~6s against boards that change on the order
// of minutes -- ~547k reads/day across the mission fleet.
func TestMissionsGateSuppressesTheBoardRead(t *testing.T) {
	clock := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t), activeJSON(t)},
		raw:               map[string][]byte{"missions": boardJSON(t)}, // empty board: dry
	}
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())
	deps.State = &missionRunState{}
	deps.Now = func() time.Time { return clock }

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if got := strings.Count(strings.Join(fc.calls, " "), "get_missions"); got != 1 {
		t.Fatalf("pass 1 get_missions = %d, want 1", got)
	}

	clock = clock.Add(10 * time.Second) // well inside the 4-minute backoff
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	if got := strings.Count(strings.Join(fc.calls, " "), "get_missions"); got != 1 {
		t.Errorf("get_missions after a gated pass = %d, want 1 (the second read must be suppressed)", got)
	}
}

// TestMissionsGatedPassDoesNotCountAsDry pins the guard that keeps this change
// from costing money. missionDryPassLimit is 3, and reaching it repositions the
// worker. A skipped read is NOT evidence the station is empty -- without this,
// the dry counter would keep ticking at ~6s while real reads happened every 4
// minutes, firing the reposition threshold ~40x sooner in wall-clock terms and
// burning fuel hopping between boards the worker never actually looked at.
func TestMissionsGatedPassDoesNotCountAsDry(t *testing.T) {
	clock := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t), activeJSON(t), activeJSON(t)},
		raw:               map[string][]byte{"missions": boardJSON(t)},
	}
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())
	deps.State = &missionRunState{}
	deps.Now = func() time.Time { return clock }

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if deps.State.dry != 1 {
		t.Fatalf("after one real dry read, dry = %d, want 1", deps.State.dry)
	}

	for i := range 2 {
		clock = clock.Add(10 * time.Second)
		if err := Missions(context.Background(), deps); err != nil {
			t.Fatalf("gated pass %d: %v", i, err)
		}
	}
	if deps.State.dry != 1 {
		t.Errorf("dry = %d after two gated passes, want 1 -- a skipped read is not a dry pass", deps.State.dry)
	}
}
```

For the third test, **reuse the existing held-freight setup verbatim** rather than
building a new fixture. Find the test in this file that already exercises
`freightReconcileSet` with `EnableFreight: true` and a held contract, copy its
`fakeClient`/`fakeFreightStore` construction, then:

```go
// TestMissionsGatedPassStillReconcilesHeldFreight pins the safety property the
// whole gating design rests on: in-flight contracts are handled on EVERY pass,
// gated or not. mission.go's own comment says reconcile "must precede EVERY
// early return" -- throttling it would strand freight mid-route.
func TestMissionsGatedPassStillReconcilesHeldFreight(t *testing.T) {
	// <copy the held-freight fixture from the existing reconcile test>
	deps.State = &missionRunState{}
	deps.Now = func() time.Time { return clock }

	// Pass 1 opens the gate and reconciles.
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	before := len(fc.calls)

	// Pass 2 is gated for the SEARCH, but reconcile must still run.
	clock = clock.Add(10 * time.Second)
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("gated pass: %v", err)
	}
	joined := strings.Join(fc.calls[before:], " ")
	if strings.Contains(joined, "get_missions") {
		t.Error("gated pass must not read the board")
	}
	// Assert the reconcile path's own observable call still appears in the
	// gated pass — use whichever call the existing reconcile test asserts on.
	if !strings.Contains(joined, "<reconcile call from the existing test>") {
		t.Errorf("gated pass must still reconcile held freight; calls = %v", fc.calls[before:])
	}
}
```

The two placeholders in angle brackets are the only things to fill from the
existing test; everything else is complete. If the harness needs a longer
`activeMissionsSeq` than the counts above, extend it — that is fixture
plumbing, not a design change.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestMissionsGate -count=1 -v`
Expected: FAIL, and **for the right reason** — a second `GetMissions` observed, and `dry` observed as 3 rather than 1. A compile error does not prove the behaviour; fix compilation and re-run until you see the real assertion failures, then paste them into the report.

- [ ] **Step 3: Add the field to `missionRunState`**

In `pkg/worker/mission.go`, alongside `parkedUntil`:

```go
	// boards throttles the search for new work (mission board + freight
	// contracts). It does NOT gate freight reconcile, delivery or transit —
	// only the question "is there anything new here". See boardPollFast /
	// boardPollRest.
	boards pollGate
```

- [ ] **Step 4: Gate the two searches**

Both searches sit in one contiguous region of `Missions()` (the freight search at `:612`, the mission board read at `:638`). Evaluate the gate once per pass, before that region:

```go
	// One decision per pass, so the freight search and the mission board read
	// stay in lockstep and cannot half-poll.
	searchForWork := deps.State == nil || deps.State.boards.shouldPoll(missionNow(deps))
```

Guard the freight search block at `:612` with `if deps.EnableFreight && searchForWork {`, and the board read at `:638` so that when `searchForWork` is false the pass skips `missionReadBoard` **and** the candidate evaluation it feeds, without logging a "no candidate" line.

`deps.State == nil` must mean "always search": tests that omit `State` already opt out of repositioning, and they must not silently acquire throttling too.

- [ ] **Step 5: Record the outcome and protect the dry counter**

After the search region, when `searchForWork` was true, report what it found:

```go
	if searchForWork && deps.State != nil {
		deps.State.boards.noteResult(foundWork, missionNow(deps))
	}
```

where `foundWork` is true when this pass accepted a mission or took a freight contract. Derive it from the existing signals the pass already computes for those two outcomes — do not add a new tracking variable if one already exists.

Then guard the dry-pass call so a **gated pass never reaches it**:

```go
	if !searchForWork {
		return nil // gated: not a dry pass — we did not look
	}
```

placed so it precedes `missionDryPass`. This is the single most important line in the task: without it the reposition threshold fires ~40× sooner in wall-clock terms.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestMissions -count=1 -v`
Expected: PASS — the three new tests **and** every pre-existing `TestMissions*` test. The existing suite is the regression net for the pass's other behaviour; if one of them now fails, the gate is placed wrongly.

- [ ] **Step 7: Verify the whole package and lint**

Run: `go build ./... && go test ./pkg/worker/ -count=1 && golangci-lint run pkg/worker/...`
Expected: all green. `pkg/worker` takes ~150s.

- [ ] **Step 8: Commit**

```bash
git add pkg/worker/mission.go pkg/worker/mission_test.go
git commit -m "feat(worker): throttle the mission and freight new-work search

Workers re-read both boards every ~6s measured, against boards that change on
the order of minutes: ~547k reads/day across the mission fleet and ~300k wasted
log lines a day. The search now runs at most every 60s after activity and every
4 minutes at rest.

Only the SEARCH is gated. Freight reconcile, delivery and transit still run on
every pass — reconcile must precede every early return, so throttling it would
strand in-flight freight.

A gated pass deliberately does not count as a dry pass: a skipped read is not
evidence the station is empty, and counting it would fire the reposition
threshold ~40x sooner in wall-clock terms and burn fuel hopping between boards
the worker never looked at.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Rollout

The change is inert until a worker's binary is replaced, and the behaviour change is confined to how often new-work searches fire.

1. **Rebuild `bin/worker`** by building to a temp path and renaming — the running fleet has the current binary open, so `go build -o bin/worker` fails with "text file busy".
2. **One worker first.** Restart a single mission-fleet worker (SIGTERM by explicit PID; its supervisor respawns it on the new binary). Do not scan `/proc` for a pattern that also appears in your own command line — that trap has previously stopped the scanning script itself.
3. **Confirm from its log** that board reads land ~4 minutes apart at rest and ~60s apart after an accept, and that `missions: no acceptable missions on this board` becomes rare rather than dominant.
4. **Then the rest**, paced. Fresh logins are not rate-limited the way reconnects are, but stagger anyway.

**Expected signal:** mission-fleet log growth drops by roughly an order of magnitude. Today's baseline for comparison: 132,280 `no acceptable missions` lines, 123,941 freight `no candidate` lines, 43,387 `no board entries here`.

**What would say this went wrong:** repositioning frequency going *up*, or workers hopping between stations more than before. That means the dry-counter guard is not working, and it costs fuel — roll back rather than debug in place.

## Follow-on

Phase 2: lift `pollGate` into `RunStanding` to drive `IdleInterval` adaptively for every role, which is where the haul (3.4 GB) and marketbot (4.5 GB) log volume lives. Its floor is the principled `2 * game.SleepTick` = 20s rather than the 60s used for boards — different loops, different useful minimums.
