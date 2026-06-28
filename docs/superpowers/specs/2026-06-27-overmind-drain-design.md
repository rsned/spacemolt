# Overmind Graceful Drain — Design

**Date:** 2026-06-27
**Status:** Approved (design); ready for implementation plan.

## Problem

Stopping the overmind today sends `control.TypeAbort` to every worker immediately
(`cmd/overmind/main.go` SIGTERM/SIGINT path) and the supervisor exits within ~2s,
interrupting any in-flight haul mid-transit. Hauls resume later via claim-recovery,
but the turndown is not clean: a worker can be aborted undocked, mid-route, or
between buy and sell legs. We want a way to bring the fleet to a **safe, quiescent
state** — every worker docked with its goods settled and idle — *before* stopping,
so a turndown (or a maintenance pause) leaves no work half-done.

## Goal

Add an **independent drain state** to the overmind control plane: a signal quiesces
every worker to a docked/idle hold (each finishing its current unit of work first),
the overmind reports progress and a fleet-wide "drained" condition, and **stop** and
**resume** are separate operator actions taken from that held state.

Drain is *not* coupled to shutdown. The fleet can drain, hold, be inspected, and then
either resume work or be stopped cleanly.

## Key Insight — why this is small

Two existing facts make drain a thin addition rather than new per-role machinery:

1. **The standing loop already gates new work at the top of each pass**
   (`pkg/worker/standing.go:97`, the `deps.Paused()` check). An in-flight pass
   completes before the next is skipped. So "finish current work, take no new work"
   is already the loop's behavior under Pause — drain reuses that gate.

2. **One idle-script pass = one complete, docked-and-settled work unit, per role.**
   The idle scripts are authored as whole units that end in a safe state:
   - Hauler `haul.smolt`: `haul` runs one full cycle (resume/claim → buy → sell →
     complete) then returns.
   - Miner `mine_local.smolt` / `mining_run.smolt`:
     `travel belt → loop -f N mine → travel station → dock → deposit_all` — ends
     docked with cargo deposited.
   - Explorer `explore.smolt`: one survey pass.

   So the natural drain boundary (the pass boundary) **already lands each role at
   "docked, goods settled, idle."** No per-role Go logic is required; the
   role-general drain semantics fall out of how the scripts are written. A hauler
   draining mid-haul finishes *that haul*; a miner draining mid-run finishes filling
   and depositing. Exactly the desired per-role behavior.

## Operator Model

| Action            | Signal to overmind | Effect                                                        |
|-------------------|--------------------|---------------------------------------------------------------|
| **Drain**         | `SIGUSR1`          | Broadcast `TypeDrain`; poll + log progress; on all-idle log "fleet drained — safe to stop"; HOLD. |
| **Resume**        | `SIGUSR2`          | Broadcast `TypeResume`; workers resume taking passes.         |
| **Stop**          | `SIGTERM`/`SIGINT` | Existing abort/stop path (now clean — workers already docked-idle). |

Drain is fleet-wide in v1 (no per-worker/per-group drain — YAGNI; defers to the
future group-command work). Resume is likewise fleet-wide.

## Architecture

### 1. Control messages — `pkg/overmind/control`

- Add `TypeDrain Type = "drain"`. Payload-less envelope, exactly like the existing
  `TypePause`/`TypeResume`.
- Reuse the existing `TypeResume` for resume.
- Add a `Drained bool` field to `control.Status` (the worker→overmind heartbeat).
  This is the **level-triggered** idle report.

### 2. Worker — `cmd/worker/main.go`

- Add `draining atomic.Bool` and `drained atomic.Bool` alongside the existing
  `paused atomic.Bool`.
- Control reader: `case control.TypeDrain → draining.Store(true)`;
  `case control.TypeResume → draining.Store(false); paused.Store(false)`.
- Status builder (`cmd/worker/main.go:376`) sets `Drained: drained.Load()`.

### 3. Standing loop — `pkg/worker/standing.go`

`StandingDeps` gains two injected collaborators, mirroring the existing `Paused`:

- `Draining func() bool` — a second gate, OR-ed with `Paused`: a new pass starts
  only when neither is set.
- `SetDrained func(bool)` — the loop publishes its idle/holding state through this
  (no-op default when nil, keeping tests hermetic).

Loop behavior (the gate at the top of the loop):

```
for {
    if ctx.Done(): return
    if (paused || draining):
        SetDrained(draining)   // drained = true only when held *because* of drain
        sleep(SleepMedium); continue
    SetDrained(false)          // actively running a pass → not drained
    ExecMu.Lock()
    run task-or-idle pass      // in-flight; a drain that arrives now is honored next top
    ExecMu.Unlock()
    sleep(IdleInterval)
}
```

`drained` therefore flips **true exactly when the worker transitions to held-idle
after completing its current pass**, and **false** the moment it runs a pass again
(after resume). A worker already idle between passes when drain arrives reports
`drained` on its next loop iteration.

### 4. Supervisor — `pkg/overmind/supervisor/`

- `WorkerInfo` (`fleet.go:14`) gains `Drained bool`, set from `st.Drained` in
  `ApplyStatus`.
- Add `Server.Broadcast(env control.Envelope) error` — iterate the registered
  `s.conns` and `Encode` to each (collect/aggregate send errors; a disconnected
  worker is not fatal to the broadcast).
- Add a fleet helper to count drained vs. connected workers (e.g.
  `Fleet.DrainProgress() (idle, total int, busy []string)`), used by the
  overmind's drain poller for progress logging.

### 5. Overmind — `cmd/overmind/main.go`

- Register `SIGUSR1` and `SIGUSR2` (in addition to the existing `SIGINT`/`SIGTERM`).
- `SIGUSR1`: `Broadcast(TypeDrain)`, then poll `DrainProgress()` on a ticker,
  logging `"drain: N/M idle…"` as it advances. On all connected workers drained,
  log `"fleet drained — safe to stop"`. The poll is **bounded** (a max wait); if it
  expires with stragglers, log `"K/M idle — still busy: <agent ids>"` and stop
  polling. **No force-abort** — the fleet simply holds; the operator decides.
- `SIGUSR2`: `Broadcast(TypeResume)`.
- `SIGTERM`/`SIGINT`: unchanged abort/stop path.

## Data Flow

```
operator ──SIGUSR1──> overmind
   overmind ──Broadcast(TypeDrain)──> every worker control conn
      worker: draining=true; finishes in-flight pass; gate skips → drained=true
      worker ──Status{Drained:true} (heartbeat)──> supervisor.ApplyStatus
   overmind polls Fleet.DrainProgress() → logs "N/M idle" → "fleet drained"
operator ──SIGTERM──> overmind (clean stop)   |   ──SIGUSR2──> resume (Broadcast TypeResume)
```

## Error / Edge Handling

- **Stuck worker** (cannot finish its pass — e.g. a non-filling sell loop): never
  reports `Drained`. The bounded poll surfaces it by name and the fleet holds; no
  auto-abort. Operator force-stops if desired.
- **Worker disconnect during drain**: drops from `conns`; `DrainProgress` counts
  only connected workers, so a dead worker does not block "fleet drained". A
  reconnecting worker re-sends Status; level-triggered `Drained` re-establishes the
  correct state with no edge-event replay needed.
- **Resume mid-drain** (some idle, some still finishing): `TypeResume` clears
  `draining` everywhere; idle workers run their next pass and clear `drained`;
  in-flight workers were never blocked. Consistent.
- **Drain while a worker has an assigned task** (`ActiveTaskID` set): the task is a
  pass like any other — it runs to completion, then the worker holds. No special case.

## Why level-triggered (the one real fork)

A `Drained bool` on the existing Status heartbeat was chosen over a one-shot
`worker→overmind` "drained!" event because level-triggered state survives reconnects
and resume/re-drain cleanly and rides the channel the supervisor already ingests — no
new upstream message type. (Rejected: edge-event = added surface + replay edge cases;
reusing `TypePause` wholesale = conflates per-worker pause with fleet drain and still
needs the idle-report field anyway.)

## Testing

- **`pkg/overmind/control`**: codec round-trip for a `TypeDrain` envelope; `Status`
  JSON round-trip including `Drained`.
- **`pkg/worker/standing.go`**: drain-gate tests mirroring
  `TestRunStandingPausedDoesNotRunIdle` —
  (a) drain set before a pass → no pass runs, `SetDrained(true)` observed;
  (b) drain set mid-pass → the in-flight pass completes, then no further passes and
  `drained` becomes true;
  (c) resume after drain → passes resume and `drained` clears.
- **`pkg/overmind/supervisor`**: `ApplyStatus` stores `Drained`; `Broadcast` reaches
  all registered conns and tolerates a closed one; `DrainProgress` counts
  idle/total/busy correctly.
- **No new game-server calls**; all new logic is exercised with fakes.

## Out of Scope (v1)

- Per-worker or per-group drain (fleet-wide only).
- Auto-stop-after-drain (the operator stops explicitly; the chosen model holds).
- A frontend/fleet-status.json surface for drain state (the overmind logs it;
  wiring it into `fleet-status.json` can follow if wanted).

## Constraints

Go 1.24; golangci-lint clean; `go build ./...` && `go test ./...` before each commit;
Sleep values from `pkg/game/constants.go`; binaries to `bin/`; commit only
drain-scoped files (no `git add -A`).
