# Overmind Plan B — Worker Runtime (Resident Standing Behaviors) — Design

**Date:** 2026-06-20
**Status:** Approved design; ready for implementation planning
**Builds on:** Plan A (`docs/superpowers/plans/2026-06-19-overmind-plan-a-control-plane.md`, merged to `main` 2026-06-20)
**Parent design:** `docs/superpowers/specs/2026-06-19-overmind-fleet-manager-design.md`

## Problem

Plan A delivered the overmind↔worker control plane: a supervisor that spawns,
health-checks, restarts, and checkpoints workers over an NDJSON Unix socket. But
the Plan A worker (`cmd/worker`) is a **thin stub** — it connects to the game,
emits heartbeats, obeys `abort`/`pause`/`resume`, and checkpoints, but runs **no
real automation**.

Plan B turns the stub into a genuinely useful **resident** worker: one parked at
a station that keeps that station's market and facility data fresh in the shared
SQLite knowledge base, and mines locally on idle cycles. To do this without
reimplementing behavior, we extract the `play_as` automation primitives (loop
engine, scripts, scheduler, token substitution) into a shared `pkg/worker`
library imported by **both** the `play_as` REPL and the headless `cmd/worker`.

## Goals

- Extract the reusable automation primitives from `cmd/tools/play_as` into a new
  shared `pkg/worker` library; `play_as` deletes its copies and imports them
  (its behavior unchanged).
- Give `cmd/worker` a **lean command dispatch** (≈15 commands → `game.GameClient`
  + KB-capture) instead of play_as's 3000-line `executeCommand`.
- Run a per-role **standing behavior** for the resident role, read from
  `data/overmind/roles.yaml`: scheduled market/facility tracking + an idle
  local-mining loop.
- Preserve **KB-tracking fidelity** — the worker writes the same data, to the
  same tables, via the same capture helpers play_as uses.
- Wire the standing behavior to the existing control channel: `pause`/`resume`
  park/resume idle work, `abort` checkpoints and exits cleanly.
- Seed the shared script catalog (`data/scripts/`) with the resident's scripts.

## Non-Goals (this plan)

- `assign_task` / `task_progress` / `task_done` control path and the tactical
  planner — **Phase 2**.
- Mobile worker roles (hauler, salvager, explorer, mobile miner) — **Phase 2**.
- Extraction of `autopilot.go` (multi-hop jump routing) — residents mine
  *locally* (intra-system `travel` only), so autopilot is not needed; it stays
  in `play_as` and is extracted alongside mobile workers in **Phase 2**.
- Guardrail rules (combat/hull/jettison auto-act) — **Plan C**.
- Web mission-control UI — **Plan D**.
- LLM-on-demand escalation — **Phase 3**.

## Why This Architecture

The coupling analysis (recorded below) showed the play_as automation primitives
are *already* decoupled from the REPL: `loop_block.go`, `scripts.go`,
`schedule.go`, and `tokens.go` reference **zero** REPL globals. They take
callbacks (`runStatement func(tokens []string) error`, `run func(ScheduledTask)`)
and an `io.Writer`. The only thing trapping them in `package main` is that the
callbacks ultimately call `executeCommand()` — the ~3000-line dispatch switch
(`cmd/tools/play_as/main.go:5585`) that prints to `os.Stdout` and lives in
`package main`.

Two further facts shape the design:

1. **A worker is a separate process.** `executeCommand`-style stdout output going
   to the worker's own stdout is fine — the supervisor captures it as worker
   logs. The structured `status` the worker reports up the control channel comes
   from `client.GetState()` (already wired in Plan A), **not** from parsing
   command output. So the worker never needs play_as's terminal formatters.

2. **`resolveTokens(args, *game.State)`** (`tokens.go:36`) is already fully
   state-driven with no globals. It resolves `$SYSTEM$`, `$SHIP$`, `$CREDITS$`,
   and POI-type tokens (`$ASTEROID_BELT$`, `$STATION$`) to ids from the live
   system. This is exactly the parameter mechanism the worker scripts need, so a
   resident's idle script needs almost no per-station configuration — location is
   resolved from state at run time.

**Decision (lean dispatch + shared automation):** `pkg/worker` holds the shared
automation engine behind a `CommandRunner` interface. `play_as` keeps its rich
`executeCommand` and adapts it to the interface (behavior unchanged).
`cmd/worker` gets its own lean dispatch covering only the curated worker-script
command vocabulary. This is the lowest-risk option: "identical behavior" holds at
the *script/automation* layer, and per-command semantics come from the shared
`game.GameClient` methods and the shared KB-capture helpers. The divergence risk
(a script using a command the lean dispatch doesn't know) is bounded by the
curated catalog and killed outright by a test that asserts every command named in
`roles.yaml` / `data/scripts/` is dispatchable.

The rejected alternative — a "big-bang" extraction of `executeCommand` and its
entire transitive closure of formatters/helpers/globals into a library
(`pkg/playas`) — is the parent design's eventual ideal (one dispatch everywhere)
but moves ~330KB across ~60 files and turns the package-level globals
(`globalClient`, `globalKB`, `globalAgentID`, …) into a session struct. Too large
and too risky for this slice; revisit if/when worker command needs outgrow the
curated catalog.

## Package & File Layout

```
pkg/worker/                        # NEW shared library
  parse.go      # splitArgs, scanBraceDepth, hasTopLevelOpenBrace,
                #   parseStatements, parseLoopHeader, afterTokens,
                #   extractBracedBody, blockPreview, Statement   (from loop_block.go)
  loop.go       # ExecuteLoop, readLogicalCommand                (from loop_block.go)
  tokens.go     # ResolveTokens, resolveOneToken, tokenError,
                #   knownPOITypes                                 (from tokens.go)
  scripts.go    # ResolveScriptArg, SplitScriptCommands,
                #   ListScripts, SaveScript, validateScriptName,
                #   scriptSearchPaths                             (from scripts.go)
  schedule.go   # Scheduler, ScheduledTask, currentBoundary,
                #   nextBoundary                                  (from schedule.go)
  runner.go     # CommandRunner interface
  dispatch.go   # WorkerDispatch — lean command→client+capture; CommandRunner
  capture.go    # KBUpdateSystem/POI/Station/Facilities,
                #   CaptureMarket, convertMarketListings, helpers
                #   (moved from kb_update.go/demand_capture.go;
                #    signatures take (ctx, client, kb knowledge.Base))
  roles.go      # Role, LoadRoles(path) (data/overmind/roles.yaml)
  standing.go   # RunStanding(ctx, role, deps) — scheduler + idle loop

cmd/worker/main.go                 # extend Plan A wiring: LoadRoles →
                                   #   build WorkerDispatch → RunStanding
                                   #   (heartbeat / control reader / pause /
                                   #    abort / checkpoint retained from Plan A)

cmd/tools/play_as/                 # DELETE loop_block.go, scripts.go,
                                   #   schedule.go, tokens.go
                                   # executeCommand path imports pkg/worker
                                   #   {ExecuteLoop, ResolveTokens, ...}
                                   # handleScheduleAdd/Remove/ViewScheduled
                                   #   (REPL-arg parsing + printing) STAY,
                                   #   now calling pkg/worker.Scheduler
                                   # kb_update.go / demand_capture.go keep the
                                   #   existing globalKB-reading function names
                                   #   as thin wrappers → pkg/worker capture
                                   #   (the ~14 call sites are untouched)

data/overmind/roles.yaml           # NEW: resident standing behavior
data/scripts/                      # ADD track_station.smolt, idle_mine.smolt
                                   #   (catalog already exists: travel_core.smolt)
```

### What moves vs. stays in play_as

| play_as symbol | Disposition |
|----------------|-------------|
| `loop_block.go` (parsers, `executeLoop`, `readLogicalCommand`) | **Move** to `pkg/worker` (`parse.go`, `loop.go`), exported. |
| `scripts.go` (script resolution/save/list) | **Move** to `pkg/worker/scripts.go`, exported. |
| `schedule.go` `Scheduler`/`ScheduledTask`/boundary fns | **Move** to `pkg/worker/schedule.go`, exported. |
| `schedule.go` `handleScheduleAdd/Remove/ViewScheduled` | **Stay** in play_as (REPL-arg parsing + `fmt.Println` table). Rewritten to call the moved `Scheduler`. |
| `tokens.go` (`resolveTokens`, `tokenError`) | **Move** to `pkg/worker/tokens.go`, exported. |
| `splitArgs` (`main.go:8237`) | **Move** to `pkg/worker/parse.go` (exported `SplitArgs`); update 5 play_as call sites. |
| `kbUpdateSystem/POI/Station/Facilities`, `captureMarket`, `convertMarketListings` | **Move** the implementations to `pkg/worker/capture.go` taking `(ctx, client, kb)`. Leave same-named one-line wrappers in play_as reading `globalKB` → the ~14 existing call sites are unchanged. |
| `executeCommand`, all `*_format.go`, the rest of `kb_update.go`, `autopilot.go`, `craftable.go`, `sellable.go`, `demand_*`, `explore.go`, etc. | **Stay** in play_as. |

## Components

### `CommandRunner` (runner.go)

```go
// CommandRunner executes one already-tokenized command line.
// tokens[0] is the command name; the rest are its arguments.
type CommandRunner interface {
    Run(ctx context.Context, tokens []string) error
}
```

`ExecuteLoop` and the script/schedule runners are driven through a
`CommandRunner` (the existing `runStatement func(tokens []string) error`
callback shape is preserved internally; the interface is the public seam). Token
resolution happens *before* dispatch: the worker's runner calls
`ResolveTokens(tokens, client.GetState())` then `WorkerDispatch.Run(...)`.

- **play_as** implements `CommandRunner` with a `replRunner` that wraps
  `executeCommand` (so REPL behavior is byte-for-byte unchanged).
- **cmd/worker** uses `WorkerDispatch`.

### `WorkerDispatch` (dispatch.go)

A lean, table-driven dispatch implementing `CommandRunner`. It maps the curated
worker-script command vocabulary to `game.GameClient` methods plus the shared
KB-capture helpers. Constructed with the game client and the `knowledge.Base`.

Phase-1 resident vocabulary (exact set finalized in the implementation plan from
the seeded scripts; this is the working list):

| Command | Action |
|---------|--------|
| `undock` | `client.Undock(ctx)` |
| `dock [poi]` | `client.Dock(ctx, poi)` |
| `travel <poi>` | `client.Travel(ctx, poi)` (intra-system) |
| `mine [poi]` | `client.Mine(ctx, …)` |
| `refuel` | `client.Refuel(ctx)` |
| `repair` | `client.Repair(ctx, …)` |
| `deposit_all` / `deposit_credits` | storage deposit calls |
| `sell_all` | `client.SellAll(ctx, …)` |
| `view_market` | `client.ViewMarket(...)` + `CaptureMarket(ctx, client, kb)` |
| `get_facilities` / facility list | client call + `KBUpdateFacilities(ctx, client, kb)` |
| `kb_update` | `KBUpdateSystem/POI/Station/Facilities(ctx, client, kb)` |
| `get_status` / `get_system` / `get_cargo` | client query (no tick cost) |

An unknown command returns an error (which the loop/standing layer logs); it is
**not** silently ignored. The `roles_test` guard ensures the seeded
roles/scripts never name a command outside this set.

### KB-capture (capture.go)

The capture implementations move here, re-typed to take an explicit
`knowledge.Base`:

```go
func KBUpdateSystem(ctx context.Context, client game.GameClient, kb knowledge.Base) error
func KBUpdatePOI(ctx context.Context, client game.GameClient, kb knowledge.Base) error
func KBUpdateStation(ctx context.Context, client game.GameClient, kb knowledge.Base) error
func KBUpdateFacilities(ctx context.Context, client game.GameClient, kb knowledge.Base) error
func CaptureMarket(ctx context.Context, client game.GameClient, kb knowledge.Base)
```

Internally unchanged from today (same tables, same dedup/merge, same
`knowledge.SQLiteKB` type assertions where needed) — only the `globalKB`
reference becomes the `kb` parameter. play_as keeps `kbUpdateSystem(client, ctx)`
etc. as thin wrappers (`return worker.KBUpdateSystem(ctx, client, globalKB)`), so
its existing call sites and behavior are untouched.

### Roles (roles.go)

```go
type ScheduleEntry struct {
    Every   string // "30m", "1h", "daily", ... (see Scheduler frequencies)
    Command string // a command line, may contain $TOKEN$s
}
type Role struct {
    Schedule   []ScheduleEntry
    Idle       string            // bare script name resolved via data/scripts
    IdleParams map[string]string // substituted into the idle script
}
func LoadRoles(path string) (map[string]Role, error)
```

`data/overmind/roles.yaml`:

```yaml
roles:
  resident:
    schedule:
      # Seed ships `hourly` to reuse the existing Scheduler frequency set
      # unchanged (see the `every:` note below). `30m` is a deferred refinement.
      - { every: hourly, command: "kb_update; view_market" }
    idle: idle_mine            # data/scripts/idle_mine.smolt
    idle_params: { N: "20" }   # substituted into the idle script
```

**Note on `every:`** — `schedule.go`'s `Scheduler` currently supports the closed
set `hourly|daily|weekly` keyed to wall-clock boundaries. `30m` is not in that
set. The implementation plan resolves this by **mapping `roles.yaml`
frequencies onto the existing supported set** (e.g. the seed uses `hourly` for
tracking) rather than widening the `Scheduler` frequency model in this plan; if a
finer cadence is required, that is a one-line addition to `validFrequencies` +
`currentBoundary`/`nextBoundary` flagged for the user per the Sleep-constants
rule. The seed roles.yaml ships with `hourly` to avoid changing the scheduler.

### Standing behavior (standing.go)

```go
type StandingDeps struct {
    Runner    CommandRunner    // WorkerDispatch
    Scheduler *Scheduler       // per-agent, loaded from data/agents/<id>/schedule.json
    Client    game.GameClient  // for state (token resolution, status)
    ExecMu    *sync.Mutex      // single mutex serializing scheduled + idle work
    Paused    func() bool      // gate from the control reader's paused atomic.Bool
    Out       io.Writer        // worker stdout (logs)
    NowFn     func() time.Time
}
func RunStanding(ctx context.Context, role Role, deps StandingDeps) error
```

`RunStanding`:
1. Registers the role's schedule entries into the `Scheduler` (idempotent;
   keyed so re-runs after restart don't duplicate) and calls
   `Scheduler.startLoop(ctx, interval, deps.ExecMu, run, deps.NowFn)`, where
   `run` executes the scheduled command through `deps.Runner` under `ExecMu`.
2. Runs the idle loop in the calling goroutine: resolve the idle script once to
   commands (`ResolveScriptArg` + `SplitScriptCommands`), then repeatedly — while
   `ctx` is live — if `!deps.Paused()`, acquire `ExecMu`, run one pass of the
   idle script (`ExecuteLoop` / per-statement `Runner.Run` with `ResolveTokens`
   against fresh state), release `ExecMu`; if paused, sleep a `pkg/game` Sleep
   constant and re-check.
3. Returns when `ctx` is cancelled (abort/signal), after the in-flight pass
   completes.

The **single `ExecMu`** (the same pattern play_as uses today at `main.go:396`)
guarantees scheduled tracking and idle mining never interleave on the one game
connection.

### cmd/worker wiring (main.go)

Extends the Plan A `main.go` after the `hello` send and before the heartbeat
loop:

1. `roles, _ := worker.LoadRoles("data/overmind/roles.yaml")`; select
   `roles[*role]`. If the role is unknown or has no standing behavior, fall back
   to the Plan A idle heartbeat (logged), so an unconfigured worker still runs.
2. Open the per-agent `Scheduler` (`data/agents/<id>/schedule.json`, the path
   play_as already uses).
3. Build `WorkerDispatch{client, kb}`. The worker opens the shared KB the same
   way other agents do (resolve the shared SQLite path; if unavailable, capture
   commands no-op with a logged warning — mining still works).
4. Launch `RunStanding(ctx, role, deps)` in a goroutine, sharing the existing
   `paused atomic.Bool` (via `deps.Paused`) and a new `execMu`.
5. Keep the existing heartbeat + control-reader goroutines unchanged. `abort`
   still cancels `ctx` → `RunStanding` returns → checkpoint saved → exit.

## Data Flow (resident worker, happy path)

```
boot (Plan A) ─ open checkpoint ─ connect game ─ reconcile ─ dial socket ─ hello
      │
      ├─ LoadRoles → resident{ schedule:[hourly "kb_update; view_market"],
      │                        idle: idle_mine, params:{N:20} }
      │
      ├─ Scheduler.startLoop ──(every boundary, under ExecMu)──▶ Runner.Run:
      │        kb_update  → KBUpdate{System,POI,Station,Facilities}(ctx,client,kb)
      │        view_market→ client.ViewMarket + CaptureMarket(ctx,client,kb)
      │                                              └────────────▶ shared SQLite KB
      │
      ├─ RunStanding idle loop ──(when !paused, under ExecMu)──▶ ResolveTokens +
      │        idle_mine.smolt: undock; travel $ASTEROID_BELT$; loop -f 20 mine;
      │                         travel $STATION$; dock; deposit_all
      │
      ├─ heartbeat goroutine (Plan A) ── status from client.GetState() ──▶ overmind
      │
      └─ control reader (Plan A): pause→Paused()=true (idle parks),
                                  resume→Paused()=false,
                                  abort→cancel ctx → checkpoint → exit
```

## Error Handling

Loop semantics are preserved verbatim from `loop_block.go`:

- `-f` (force): continue past ordinary errors, return nil at end.
- `*game.GoalReachedError`: positive exit from the innermost loop (🎯), even
  under `-f`.
- `*tokenError`: fatal — aborts the whole loop immediately, even under `-f`.
- `context.Canceled`: clean abort of all enclosing loops (⛔), no error line.

Worker-level:

- **Idle-cycle failure** (non-fatal): logged to worker stdout; the next idle pass
  retries (effectively force at the cycle granularity). A `tokenError` (e.g. no
  asteroid in the resident's system) is logged once and the idle loop backs off a
  `pkg/game` Sleep constant before retrying, to avoid a hot spin.
- **Scheduled-task failure**: logged; the task waits for its next boundary
  (existing `Scheduler` semantics — `checkDue` already stamped `LastRun`).
- **KB unavailable**: capture commands log a warning and no-op; mining/movement
  still function (a resident with no KB still mines, it just doesn't track).
- **abort / SIGTERM**: `ctx` cancelled → in-flight pass finishes → `RunStanding`
  returns → Plan A checkpoint save → exit 0.

## Testing

**Relocated (already global-free, move with their code):**
- `loop_block_test.go` → `pkg/worker` (parser + `ExecuteLoop` behavior).
- `scripts_test.go`, `schedule_test.go`, `tokens_test.go` → `pkg/worker`.

**New in `pkg/worker`:**
- `dispatch_test.go` — a fake `game.GameClient` asserts each command token maps
  to the correct client method (and that tracking commands invoke the capture
  helpers); unknown command returns an error.
- `roles_test.go` — `LoadRoles` parses the YAML; **guard test**: every command
  named in `data/overmind/roles.yaml` and in every `data/scripts/*.smolt` the
  roles reference is dispatchable by `WorkerDispatch` (kills lean-dispatch
  divergence).
- `standing_test.go` — fake clock + fake `CommandRunner`: scheduled entries fire
  at boundaries, the idle loop runs repeatedly, `Paused()==true` halts idle work
  without exiting, `ctx` cancel returns cleanly. No real game connection.
- `capture_test.go` — port the existing `demand_capture_test.go` assertions to
  the parameterized `(ctx, client, kb)` signature against an in-memory KB.

**Regression guard:**
- play_as's existing test suite must remain green — proving the REPL behavior is
  unchanged after the extraction and the wrapper shims.
- `go build ./...` and `go test ./...` green; `golangci-lint` adds no new
  findings; the worker binary builds to `bin/`.

## Coupling Reference (from analysis, 2026-06-20)

- Central dispatch: `executeCommand(client, ctx, parts, format) error`
  (`cmd/tools/play_as/main.go:5585`); wrapper
  `executeLogicalCommand(client, ctx, cmd, format, cfg, agentID) error`
  (`main.go:8559`).
- `executeLoop(ctx, out io.Writer, count, force, body, depth, runStatement)`
  (`loop_block.go:259`); invoked with
  `runStatement := func(tokens) error { return executeCommand(client, ctx, tokens, format) }`
  and `os.Stdout` (`main.go:8614`).
- `Scheduler.startLoop(ctx, interval, execMu, run, nowFn)` (`schedule.go:192`);
  REPL passes a `run` closure over `executeLogicalCommand` under `execMu`
  (`main.go:404`).
- `resolveTokens(args, state)` — single call site `main.go:5591`.
- `splitArgs` — `main.go:8237`, 5 call sites.
- Capture helpers read package global `globalKB`; call sites in `autopilot.go`,
  `explore.go`, `kb_update.go`, `sellable.go`, `main.go` (~14 total).
- play_as has **no** single session struct — state is package globals
  (`globalClient`, `globalKB`, `globalAgentID`, `globalClock`, …) plus
  function-locals in `runREPL`. This is *why* the lean-dispatch option is
  preferred over a big-bang extraction that would have to refactor all of them.

## Open Questions / Future Specs

- Finer scheduler cadence (`30m`, `15m`): deferred; seed uses `hourly`. Widening
  `validFrequencies` is a small, separately-flagged change.
- Exact final worker command vocabulary: pinned by the seeded scripts in the
  implementation plan; the `roles_test` guard keeps it honest thereafter.
- The big-bang `pkg/playas` extraction (one dispatch everywhere) remains the
  parent design's long-term direction if worker command needs outgrow the
  curated catalog.
