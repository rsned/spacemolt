# Server Restart Pause Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Workers pause new jumps/travel across an announced server restart, and an autopilot route broken by one logs it and resumes once.

**Architecture:** Mirror the customs-hold pattern. pkg/game records the restart on `State` from the `server_restart_warning` push and clears it on `Reconnect`; pkg/worker gets `waitOutRestart` beside `waitOutHold`, and Autopilot resumes itself once on a restart-shaped failure.

**Tech Stack:** Go 1.24, existing pkg/game client + pkg/worker autopilot.

**Spec:** docs/superpowers/specs/2026-11-22-server-restart-pause-design.md

## Global Constraints

- Sleeps only through `pkg/game/constants.go` constants; in pkg/worker only via `settle(ctx, d)` (never `time.Sleep`).
- New constant: `SleepServerRestartMaxWait = 5 * time.Minute`.
- `go build ./...`, `go test ./...`, `golangci-lint run` clean.
- Binaries to bin/.

## Review Focus

- A warning with no restart afterwards (no reconnect): waits end at the cap, not forever.
- A warning with no `seconds_until_restart`: `At` = warning time + 60 s.
- Resume must fire at most once per Autopilot call (no loop on a persistent failure).
- A non-restart jump failure (no warning seen) is returned exactly as today, with no resume.
- `State.Clone` must not share the `ServerRestart` pointer.

---

### Task 1: Record the restart on State (pkg/game)

**Files:**
- Create: `pkg/game/server_restart.go`, `pkg/game/server_restart_test.go`
- Modify: `pkg/game/types.go` (State fields + Clone), `pkg/game/client.go:3021` (handler), `pkg/game/client.go` Reconnect (clear), `pkg/game/constants.go`

**Interfaces:**
- Produces: `type ServerRestart struct { At time.Time; Target string }`; `State.ServerRestart *ServerRestart`; `State.LastRestartAt time.Time`; `func (s *State) RestartPending(now time.Time) bool`; `func (s *State) RestartRecent(now time.Time) bool` (pending, or `LastRestartAt` within `SleepServerRestartMaxWait` of now); `const SleepServerRestartMaxWait`.

- [ ] **Step 1: Failing tests** in `server_restart_test.go`:
  - `TestServerRestartWarningRecordsPending`: handleResponse with the 11-22 payload (`seconds_until_restart` 60, target `0.733.0`) → `ServerRestart.Target == "0.733.0"`, `At` ≈ now+60s, `RestartPending(now)` true, event log contains `SERVER RESTART` and the message; `State.Clone` copy mutation does not change the client's.
  - `TestServerRestartWarningWithoutSecondsDefaults60s`.
  - `TestRestartPendingExpiresAfterCap`: `RestartPending(At+SleepServerRestartMaxWait+1s)` false; `RestartRecent` true just after a clear and false after the cap.
  - `TestClearServerRestartKeepsLastRestartAt`: after `clearServerRestart()` → `ServerRestart` nil, `LastRestartAt == At`.
- [ ] **Step 2: Run** `go test ./pkg/game -run 'ServerRestart|RestartPending' -v` → FAIL (undefined).
- [ ] **Step 3: Implement** types/methods in `server_restart.go`, fields + clone in `types.go`, constant, handler sets state + `c.logEvent("🔄 SERVER RESTART: "+msg)`, `func (c *Client) clearServerRestart()` called at the end of a successful `Reconnect`.
- [ ] **Step 4: Run** the same tests → PASS; `go test ./pkg/game`.
- [ ] **Step 5: Commit** (only these files).

### Task 2: Pause and resume in the worker (pkg/worker)

**Files:**
- Modify: `pkg/worker/hold_gate.go` (add `waitOutRestart`), `pkg/worker/autopilot.go` (call it beside `waitOutHold`; resume)
- Test: `pkg/worker/restart_gate_test.go`

**Interfaces:**
- Consumes: Task 1 `State.RestartPending`, `State.RestartRecent`, `ServerRestart.Target`, `SleepServerRestartMaxWait`.
- Produces: `func waitOutRestart(ctx context.Context, out io.Writer, client stateReader)`; Autopilot resumes once.

- [ ] **Step 1: Failing tests** in `restart_gate_test.go` (reuse `routeFake`, `burnFake`, `sleepFunc` swap like `liftHoldAfterTicks`):
  - `TestWaitOutRestartWaitsUntilCleared`: pending restart cleared after 3 tick-waits → returns after clear, output has `server restart` and `resum`.
  - `TestWaitOutRestartNoopWhenNone`: no output.
  - `TestWaitOutRestartIsBounded`: never-cleared restart → at most `SleepServerRestartMaxWait/SleepTick + 2` reads.
  - `TestAutopilotResumesRouteAfterRestartInterruption`: fake whose jump into `s2` fails once with `timeout waiting for state change` while setting a restart (then cleared by the sleep swap) → Autopilot returns nil, route completes (`jump:s5`), output has `interrupted by server restart`.
  - `TestAutopilotJumpFailureWithoutRestartIsReturned`: same failure with no restart → error returned, no second attempt.
  - `TestAutopilotResumesAtMostOnce`: jump into `s2` always fails with a restart recorded → error returned after exactly one resume.
- [ ] **Step 2: Run** `go test ./pkg/worker -run 'Restart' -v` → FAIL.
- [ ] **Step 3: Implement** `waitOutRestart` (no-op unless `RestartPending(time.Now())`; log pause with target; loop `settle(ctx, game.SleepTick)` up to the cap in ticks; log resume). Autopilot: `waitOutRestart` before each `waitOutHold`; rename body to `autopilotOnce` and make `Autopilot` call it, and on error with `client.GetState().RestartRecent(time.Now())` and not yet resumed, log the interruption line, `waitOutRestart`, call `autopilotOnce` again.
- [ ] **Step 4: Run** tests → PASS; `go test ./pkg/worker`; `go build ./...`; `golangci-lint run ./pkg/game/... ./pkg/worker/...`.
- [ ] **Step 5: Commit** (only these files); rebuild `bin/worker`.
