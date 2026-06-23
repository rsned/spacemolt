# Overmind Supervisor Liveness Hardening — Design

**Date:** 2026-06-23
**Status:** Approved (brainstorming)
**Scope:** Plan A hardening (#1) — fix the supervisor double-spawn race and stagger fleet startup. Prerequisite reliability work before Phase 2 mobile roles.

## Problem

The overmind supervisor (`pkg/overmind/supervisor`) spawns ~40 worker processes and keeps them alive. Two related defects make the fleet unstable at startup:

1. **Double-spawn race.** `Supervisor.launch` (`supervisor.go:85`) starts a worker but **discards the `*exec.Cmd`** — it only runs `go cmd.Wait()` to reap. `reapAndRestart` (`supervisor.go:98`) then re-launches a spec whenever it is `!seen` in the fleet snapshot **or** silent past `SilenceTimeout`. A worker still completing a (throttled) `/login` has not yet sent its `Hello`, so it is `!seen` → a **second process spawns** → another throttled `/login` → a vicious cycle. The current `SilenceTimeout` of ~5 min (`NewSupervisor`, `supervisor.go:55`) is a band-aid that masks rather than fixes this.

2. **Thundering-herd login.** All workers launch in a tight loop, hammering the per-IP/minute `/login` rate limit (see `reference_login_rate_limits`), which stretches each cold-start into minutes and feeds defect #1.

## Key fact: heartbeat is independent of game commands

The worker's liveness signal (`LastSeen`, derived from `control.Status`) is produced by a **dedicated heartbeat goroutine** (`cmd/worker/main.go:277-308`) ticking every `SleepTick` (10s). It reads a non-blocking state snapshot (`client.GetState()` → `state.Clone()`) and sends `TypeStatus`. Standing behavior — autopilot and command sequences — runs on a **separate goroutine** (`cmd/worker/main.go:255-269`) serialized on its own `execMu`. The heartbeat does **not** acquire `execMu` and does not wait on command execution.

**Consequence:** a worker grinding through a long mutation sequence (e.g. autopilot `jump → jump → jump`) keeps emitting heartbeats and looks healthy. `LastSeen` is independent of sent game commands. The `SilenceTimeout` therefore only trips when the heartbeat goroutine itself stalls (process wedged, deadlock, blocked socket write) — the genuine "hung" condition. This decoupling is the reason the timeout values below are safe; future tuning must not assume heartbeat and command execution share a thread.

## Design

### 1. Per-agent process registry

`Supervisor` gains a mutex-guarded registry of live processes (the `cmd.Wait()` goroutines write it, the reap tick reads it):

```go
type workerProc struct {
    cmd        *exec.Cmd
    cancel     context.CancelFunc // per-worker ctx; cancel == SIGKILL fallback
    launchedAt time.Time
    exited     chan struct{}      // closed when cmd.Wait() returns
}

func (p *workerProc) alive() bool {
    select {
    case <-p.exited:
        return false
    default:
        return true
    }
}
```

`launch` derives a per-worker context (`context.WithCancel(parentCtx)`), stores the `cancel`, records `launchedAt`, and registers the proc under the agent id. The existing reaping goroutine closes `exited` when `cmd.Wait()` returns. `procs map[string]*workerProc` is guarded by a `sync.Mutex`.

### 2. Restart decision matrix

`reapAndRestart` replaces the old `!seen || NeedsRestart` test with a process-state-aware decision, evaluated per spec:

| Process state | Fleet view | Action |
|---|---|---|
| no proc registered | — | **launch** (never started, or fully reaped) |
| alive | seen, silent past `SilenceTimeout` | **hung → kill, then respawn** |
| alive | seen, healthy | leave running; clear restart counter |
| alive | not seen, within `BootTimeout` of `launchedAt` | **booting → leave alone** (the fix) |
| alive | not seen, past `BootTimeout` | **wedged in boot → kill, then respawn** |
| exited | — | **respawn** |

This splits the single overloaded `SilenceTimeout` into two timeouts with distinct jobs:

- **`BootTimeout`** — generous; covers worst-case throttled cold-start. Takes over the band-aid role the inflated `SilenceTimeout` was playing. Applies only before the first `Hello`.
- **`SilenceTimeout`** — lowered to ~1–2 min; applies only to established workers (already seen) whose heartbeat has gone quiet. Faster recovery from real hangs without risking premature kills, because a healthy worker beats every 10s (≥6 beats inside the window).

The crash-loop guard (`MaxRestarts`, `restarts` counter) is preserved unchanged: incremented on each kill/respawn, cleared when a worker is observed healthy.

### 3. Graceful kill, then SIGKILL

```go
func (s *Supervisor) kill(p *workerProc) {
    if p.cmd.Process != nil {
        _ = p.cmd.Process.Signal(syscall.SIGTERM) // worker checkpoints + exits (main.go:88, :206)
    }
    select {
    case <-p.exited:               // clean exit within grace
    case <-time.After(s.KillGrace):
        p.cancel()                 // ctx cancel → SIGKILL via exec.CommandContext
        <-p.exited
    }
}
```

The worker already installs a `SIGINT/SIGTERM` handler (`cmd/worker/main.go:88`) and saves a checkpoint before exit, so SIGTERM yields clean state. Kill is **synchronous before respawn** so two processes never run for one agent. Hung workers are rare, so briefly blocking the reap tick during a kill is acceptable. The SIGTERM path is also safe mid-autopilot: the signal cancels the worker ctx, `RunStanding` observes `ctx.Done()` and stops, and the checkpoint is written before exit.

### 4. Staggered startup

`Run`'s initial launch loop sleeps `StaggerInterval` between spawns:

```go
for i, spec := range s.specs {
    if i > 0 {
        select {
        case <-time.After(s.StaggerInterval):
        case <-ctx.Done():
            return nil
        }
    }
    s.launch(ctx, spec)
}
```

- Default `StaggerInterval = game.SleepMedium` (5s) → ~3.3 min to boot 40 workers.
- Exposed as a `--stagger` flag on `cmd/overmind` for **empirical tuning**: launch 10/20/30+ and observe where `/login` throttling begins, then adjust.
- **Restart relaunches stay immediate** — single-worker recovery should not wait out a stagger. Mass-simultaneous-restart login bursts (e.g. after a server blip) are noted as a follow-up; the liveness fix already removes the self-amplification, and empirical stagger tuning informs whether restart pacing is needed later.

### 5. Tunables

New `Supervisor` fields with defaults (all overridable in tests via direct field assignment, as `SilenceTimeout`/`MaxRestarts` already are):

| Field | Default | Role |
|---|---|---|
| `StaggerInterval` | `game.SleepMedium` (5s) | delay between initial spawns; `--stagger` flag |
| `BootTimeout` | `30 * game.SleepTick` (5 min) | max time alive-but-no-`Hello` before treating a boot as wedged |
| `SilenceTimeout` | `9 * game.SleepTick` (90s) | lowered; heartbeat-gap tolerance for established workers |
| `KillGrace` | `game.SleepMedium` (5s) | SIGTERM→SIGKILL escalation window |

`cmd/overmind` exposes `--stagger` (duration). The other three remain code defaults for now; promote to flags only if tuning demands it (YAGNI).

## Testing

Tests spawn **real** child processes through the injected `SpawnFunc` (e.g. a long-lived `sleep` for "alive", a process that exits immediately for "dead") so `cmd.Wait()`, `Process.Signal`, and ctx-cancel behave honestly — no mocking the unit under test. Timeouts are set small via the `Supervisor` fields for determinism.

Cases:

1. **Booting-not-respawned (regression guard for the double-spawn bug):** proc alive, no `Hello`, within `BootTimeout` → `reapAndRestart` does **not** spawn a second process. Assert exactly one proc for the agent.
2. **Wedged boot:** proc alive, no `Hello`, past `BootTimeout` → killed and respawned.
3. **Hung established worker:** seen (had `Hello`/Status), then silent past `SilenceTimeout` → killed and respawned.
4. **Dead worker:** child process exits → respawned on next tick.
5. **Healthy worker untouched:** recent Status → not killed, restart counter cleared.
6. **Graceful kill escalation:** a child that ignores SIGTERM is SIGKILLed after `KillGrace`; a child that exits on SIGTERM is not force-killed (assert via exit timing/signal).
7. **Stagger spacing:** `Run` spaces initial launches by `StaggerInterval` (assert launch timestamps / count over a short window with a small interval).
8. **MaxRestarts still bounds restarts-per-incident** and is cleared on health (existing behavior preserved).

## Non-goals

- `assign_task`, mobile roles, autopilot extraction — those are #2 and Phase 2.
- gRPC / multi-host transport — deferred.
- Restart-burst pacing beyond initial stagger — follow-up, pending empirical data.
- One-WS-multiplexes-many-sessions research spike — separate item.

## Files touched

- `pkg/overmind/supervisor/supervisor.go` — `workerProc`, registry, rewritten `launch`/`reapAndRestart`, `kill`, staggered `Run`, new tunable fields.
- `pkg/overmind/supervisor/supervisor_test.go` — new cases above.
- `cmd/overmind/main.go` — `--stagger` flag wired to `Supervisor.StaggerInterval`.
