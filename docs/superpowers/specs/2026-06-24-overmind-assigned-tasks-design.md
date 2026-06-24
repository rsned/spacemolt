# Overmind Phase 2b — Assigned Tasks + Directed Mining Run

**Status:** Approved design; ready for implementation planning.

## Problem

The overmind supervises a fleet of workers that today run only **standing
behaviors** (a role's idle script + scheduled commands, via `RunStanding`).
The control channel can `Pause`/`Resume`/`Abort` a worker but cannot hand it a
**discrete, goal-directed job**. This is the spine of Phase 2: until the
overmind can assign a task to a worker and observe it complete, there is no
tactical planner, no mobile-role dispatch, and no objective execution.

This slice (Phase 2b) builds the **assign-task path** end-to-end and proves it
with one concrete mobile-role job — a **directed mining run** — issued from a
startup seed file. It deliberately excludes the tactical planner (auto
matching at scale), runtime/UI assignment, and other mobile roles; those are
2c+ and ride on this foundation.

Parent design: `docs/superpowers/specs/2026-06-19-overmind-fleet-manager-design.md`
(Task/Goal/Brain model, Phase 2 row of the roadmap). Phase 2a (mobile explorer
role) shipped: `docs/superpowers/specs/2026-06-23-overmind-explorer-role-design.md`.

## Goals

- A worker can receive a task over the control channel, run the task's script
  once (pausing its standing idle behavior), report progress and
  completion, then resume standing — all on the single game connection.
- The overmind loads tasks from a seed file at startup, assigns each to an
  eligible idle worker (pinned agent or by required role), and tracks each
  task's status to `done`/`failed` from worker events.
- Task scripts support `$KEY$` parameter substitution (e.g. target system,
  mine count) layered under the existing live-state `$TOKEN$` resolution.
- One real job works: a `miner` worker autopilots to a target system, mines a
  fixed number of cycles, docks, and deposits.

## Non-Goals (this slice)

- Tactical planner / auto-rebalancing / coverage-aware mass assignment. v1
  assigns seed tasks to the first eligible idle worker; that is all.
- Runtime/interactive assignment (file-watch, socket/CLI push, UI). Startup
  seed file only; new tasks require editing the file and restarting.
- Auto-retry policy for failed tasks (a failed task stays `failed`).
- Other mobile roles (hauler, salvager) and ship module management. Separate
  slices.
- Fine-grained task `cursor`/resumability. A mining run re-runs from scratch
  if reassigned.
- Retrofitting `IdleParams` substitution (the shared helper makes it trivial
  later, but wiring it is out of scope here).

## Roles & Worker Model (recap)

Workers are either station residents (parked, always-on tracking grid) or
specialized mobile workers dispatched for travel jobs. This slice adds the
first **task-driven mobile role**: `miner`. A miner's standing behavior is
light (it mostly waits for assigned tasks); when assigned a mining-run task it
executes it, then returns to idle standing.

## Architecture & Data Flow

```
data/overmind/tasks.yaml ──load (startup)──▶ Task store (overmind, in-memory)
                                                  │  assignment pass
                                                  │  (inside the status/reap loop):
                                                  │  pending task → eligible idle worker
                                                  ▼
   overmind ──control: Assign{task_id, script, params}──▶ worker control reader
                                                  │
   RunStanding is task-aware: on the next idle pass, if an assigned task is
   present, it runs the task script ONCE — params substituted, then live
   tokens resolved — under the SAME ExecMu used for idle/scheduled work,
   instead of the idle script. Status.ActiveTaskID is set for the duration.
                                                  │
   worker ──Event{kind: task_done | task_failed, detail: taskID + info}──▶ overmind
                                                  │
   overmind marks the task done/failed; worker clears the active task and
   resumes normal idle standing.
```

Running the task on the existing single `ExecMu` means it cannot collide with
scheduled commands or the one game WebSocket connection — the same
serialization guarantee standing behaviors already rely on.

## Components

Each unit has one responsibility and is independently testable.

### 1. Task model + seed loader — `pkg/overmind`

```go
type Task struct {
    ID           string            // unique; from tasks.yaml
    Script       string            // script name resolved via ResolveScriptArg, e.g. "mining_run"
    Params       map[string]string // $KEY$ substitutions, e.g. {"TARGET_SYSTEM":"bunda","COUNT":"20"}
    RoleRequired string            // e.g. "miner"
    AgentID      string            // optional: pin to a specific worker
    Status       TaskStatus        // pending | assigned | running | done | failed
    AssignedTo   string            // worker agent id once assigned
}
```

`TaskStatus` is a string enum: `pending`, `assigned`, `running`, `done`,
`failed`. `LoadTasks(path string) ([]Task, error)` parses
`data/overmind/tasks.yaml`:

```yaml
tasks:
  - id: mine-bunda-iron
    script: mining_run
    role_required: miner
    params: { TARGET_SYSTEM: bunda, COUNT: "20" }
  - id: mine-dustfall
    script: mining_run
    agent_id: miner-3          # optional pin
    role_required: miner
    params: { TARGET_SYSTEM: dustfall, COUNT: "15" }
```

Loader validates: non-empty `id` (unique), non-empty `script`, non-empty
`role_required`; initial `Status = pending`. Unknown/duplicate ids are an
error.

### 2. Control message — `pkg/overmind/control`

Add `TypeAssign Type = "assign"` and:

```go
type Assign struct {
    TaskID string            `json:"task_id"`
    Script string            `json:"script"`
    Params map[string]string `json:"params,omitempty"`
}
```

Completion reuses the existing `Event` envelope (`Kind` = `"task_done"` or
`"task_failed"`, `Detail` carries the task id and any error) plus the existing
`Status.ActiveTaskID`. No other new message types.

### 3. Overmind task store + assignment

In-memory store keyed by task id, loaded from the seed file at startup. An
assignment pass runs inside the existing status/reap loop:

- For each task with `Status == pending`: choose a target worker.
  - If `AgentID` is set, that worker (only if healthy and idle).
  - Else the first worker whose role == `RoleRequired`, healthy, and **idle**
    (alive, sent Hello, `Status.ActiveTaskID == ""`).
- If a worker is found: send `Assign` on its control connection; set
  `Status = assigned`, `AssignedTo = worker`.
- On an inbound `Event{task_done}` for a task → `Status = done`. On
  `task_failed` → `Status = failed`.
- If a worker with an `assigned`/`running` task dies (supervisor detects via
  the existing process registry), return its task to `pending` so it can be
  reassigned. Logged.

"Idle" reuses the `Status.ActiveTaskID` field already reported by workers.
No coverage logic is needed here because mining-run tasks target the `miner`
role, not residents; coverage-aware assignment is a planner concern (2c+).

### 4. Worker task execution

- The control reader handles `TypeAssign`: it stores the assigned task in an
  `atomic.Pointer[control.Assign]` (`pendingTask`), mirroring the existing
  `paused atomic.Bool` pattern.
- `RunStanding` becomes task-aware. Its dependencies gain a way to read and
  clear the pending task (a small `NextTask func() *AssignedTask` +
  `OnTaskResult func(taskID string, err error)` injected from `cmd/worker`,
  keeping `pkg/worker` free of `control` imports). Each idle pass, under
  `ExecMu`:
  - If a task is pending: resolve its script via `ResolveScriptArg`, split into
    command lines, `SubstituteParams` with the task params, then run the lines
    through the existing loop engine (each line `ResolveTokens` + dispatch),
    exactly as the idle script runs. Set `Status.ActiveTaskID` for the
    duration (via the status builder in `cmd/worker`).
  - On completion call `OnTaskResult(taskID, err)`, which emits the
    `task_done`/`task_failed` event and clears `ActiveTaskID`; clear
    `pendingTask`.
  - If no task is pending: run the idle script as today.
- A task runs **once** (not looped). Scheduled commands still tick between task
  lines under `ExecMu`, unchanged.

### 5. Param substitution — `pkg/worker`

```go
// SubstituteParams replaces every $KEY$ in each command line with params[KEY],
// before live-state $TOKEN$ resolution. Unknown $KEY$ left untouched so live
// tokens (e.g. $ASTEROID_BELT$) pass through to ResolveTokens.
func SubstituteParams(cmds [][]string, params map[string]string) [][]string
```

Applied to task scripts before `ResolveTokens`. Same helper can later make
`IdleParams` real (out of scope here).

### 6. Mining-run script + miner role + miner workers

`data/scripts/mining_run.smolt` (git-allowlisted like the other catalog
scripts):

```
autopilot $TARGET_SYSTEM$
travel $ASTEROID_BELT$
loop -f $COUNT$ mine
travel $STATION$
dock
deposit_all
```

`$TARGET_SYSTEM$` and `$COUNT$` are task params (substituted first);
`$ASTEROID_BELT$` and `$STATION$` are live-state tokens resolved in the target
system after arrival. All six commands already exist in `WorkerDispatch`.

`data/overmind/roles.yaml` gains a `miner` role with a light idle (`get_status`,
so it does nothing costly while waiting) and no schedule. Miner workers
(`miner-1..N`, accounts exist) are added to the fleet roster used for launch.

**Decisions (confirmed in brainstorming):**
- Completion = a fixed `COUNT` mine cycles from params, not "until cargo full."
- Deposit at the target system's station; no return-to-home in v1 (a future
  param).
- Executor = the new `miner` mobile role.

## Error Handling

- **No eligible worker** for a pending task → it stays `pending` and is retried
  on the next assignment pass; logged once per pass at low volume.
- **Task script failure** (unreachable target, missing asteroid belt or station
  in the target system, jump/dock error) → the loop engine returns an error;
  the worker emits `task_failed` with the detail and resumes idle standing. The
  overmind marks the task `failed` (no auto-retry in v1).
- **Worker death mid-task** → the supervisor's existing restart path relaunches
  the worker; the overmind returns the task to `pending` for reassignment.
- **Malformed `tasks.yaml`** → `LoadTasks` returns an error and the overmind
  logs it and starts with no tasks (the fleet still runs standing behaviors);
  it does not crash.

## Testing

Unit tests, one focus each:

- **Task loader** — parses a valid `tasks.yaml` (with and without `agent_id`
  pin); rejects missing/duplicate ids and missing `script`/`role_required`;
  defaults `Status` to `pending`.
- **`SubstituteParams`** — replaces `$KEY$` from params, leaves unknown
  `$TOKEN$` untouched (so live tokens survive), handles a token inside a quoted
  arg, no-op when params empty.
- **Assignment matching** — pins to `AgentID` when set; otherwise first
  idle worker of `RoleRequired`; skips busy (`ActiveTaskID != ""`) and
  unhealthy workers; leaves task `pending` when none eligible.
- **`control.Assign` codec** — round-trips through the envelope encode/decode
  (mirrors existing `messages_test.go`).
- **Worker task-aware loop** — with a pending task, `RunStanding` runs the task
  script via a fake client/dispatch (asserting the dispatched commands), calls
  `OnTaskResult` with success, then resumes the idle script on the next pass;
  a failing task line yields `task_failed` and still resumes idle.
- **Status transitions** — overmind store: `pending→assigned` on assign,
  `→done`/`→failed` on the matching event, `→pending` on assigned-worker death.
- **Seeded-command drift guard** — `mining_run.smolt`'s commands
  (`autopilot`, `travel`, `mine`, `dock`, `deposit_all`) are already dispatchable;
  `TestSeededCommandsAreDispatchable` continues to pass with the new script.

## Component Boundaries (summary)

| Unit | Responsibility | Depends on |
|------|----------------|------------|
| `Task` + `LoadTasks` | task data model + seed parsing | yaml |
| `control.Assign` | wire format for assignment | control codec |
| overmind task store + assignment | match pending tasks to idle workers, track status | Task, control, supervisor process/health registry |
| worker task execution | run an assigned task once, report result, resume idle | `RunStanding`, loop engine, `SubstituteParams` |
| `SubstituteParams` | `$KEY$` → value before token resolution | — |
| `mining_run.smolt` + `miner` role/roster | the concrete first job | WorkerDispatch (existing commands) |
