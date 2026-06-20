# Overmind Plan B — Worker Runtime (Resident Standing Behaviors) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the Plan A stub worker into a real **resident** worker by extracting play_as automation primitives into a shared `pkg/worker` library, giving `cmd/worker` a lean command dispatch, and running a per-role standing behavior (scheduled market/facility KB-tracking + idle local mining) read from `data/overmind/roles.yaml`.

**Architecture:** A new `pkg/worker` library holds the already-decoupled automation primitives (parser, loop engine, token resolver, scripts, scheduler), shared KB-capture helpers, a `CommandRunner` interface, a lean `WorkerDispatch` (≈15 commands → `game.GameClient` + KB-capture), a roles loader, and a `RunStanding` driver. `cmd/tools/play_as` deletes its copies and imports the library (behavior unchanged via thin wrappers); `cmd/worker` drives `RunStanding`. "Identical behavior" holds at the script layer because per-command semantics come from the shared client + capture helpers.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3` (already used for fleet config), `modernc.org/sqlite` (via `pkg/knowledge`), `github.com/rsned/spacemolt/pkg/game`, `pkg/knowledge`, `pkg/overmind/{control,checkpoint}`. Module path `github.com/rsned/spacemolt`.

## Global Constraints

- Target Go 1.24+; use modern features (range-over-int, `b.Loop()` in benchmarks). — verbatim from CLAUDE.md
- All new code must pass `golangci-lint` with no new findings; run it after each series of changes. — verbatim from CLAUDE.md
- Any sleep/pause MUST use a predefined constant in `pkg/game/constants.go`; if none fits, stop and ask the user to add one. The real values (verified 2026-06-20) are `SleepTick=10s`, `SleepQuick=2s`, `SleepShort≈3.3s (Tick/3)`, `SleepMedium=5s (Tick/2)`, `SleepLong=20s (2·Tick)`, `SleepDock=15s`, `SleepReconnect=30s` — read the constant, do not trust the CLAUDE.md table.
- Run `go build ./...` and `go test ./...` before committing. — verbatim from CLAUDE.md
- Compiled binaries go in `bin/`, never the repo root. — verbatim from CLAUDE.md
- Always check actual API/server response struct field names / method signatures before coding against them — do not assume. — verbatim from CLAUDE.md
- New JSON Schemas use Draft 2020-12. — verbatim from CLAUDE.md (no schemas expected in this plan)

## Refactor-Move Convention (applies to Tasks 1–6)

Tasks 1–6 **move existing, already-tested code** from `cmd/tools/play_as` (`package main`) into `pkg/worker` (`package worker`). For each move:

1. The function/type bodies are **copied verbatim** — do NOT rewrite logic. The only allowed edits are: (a) capitalizing the identifier to export it where the table says so, (b) for capture functions only, replacing the `globalKB` reference with a `kb knowledge.Base` parameter, (c) adjusting internal call sites to the new (exported) names.
2. The corresponding `_test.go` file moves too, with identifiers updated to the exported names. Tests are the safety net proving the move preserved behavior — run them in the new location.
3. After moving, update every play_as call site to the `worker.`-qualified name (or a thin wrapper, where the table specifies), then **delete** the old play_as source file.
4. `go build ./...` and `go test ./...` must be green at each task's commit — `package main` must never be left referencing a deleted symbol.

Because these are verbatim relocations of code already in the repo, move-task steps cite the **source location and exact symbol list** rather than re-printing hundreds of unchanged lines; printing code from memory would risk corrupting working logic. New logic (Tasks 7–11) shows complete code.

---

## File Structure

| Path | Responsibility |
|------|----------------|
| `pkg/worker/parse.go` | Command tokenizer + block/statement/loop-header parsing. |
| `pkg/worker/parse_test.go` | Parser tests (moved). |
| `pkg/worker/loop.go` | `ExecuteLoop` recursive loop engine. |
| `pkg/worker/loop_test.go` | Loop-engine tests (moved). |
| `pkg/worker/tokens.go` | `ResolveTokens` `$TOKEN$` resolver + `tokenError`. |
| `pkg/worker/tokens_test.go` | Token tests (moved). |
| `pkg/worker/scripts.go` | Script-file resolution / save / list / split. |
| `pkg/worker/scripts_test.go` | Script tests (moved). |
| `pkg/worker/schedule.go` | `Scheduler` recurring-task engine. |
| `pkg/worker/schedule_test.go` | Scheduler tests (moved). |
| `pkg/worker/capture.go` | KB-capture helpers `(ctx, client, kb)`. |
| `pkg/worker/capture_test.go` | Capture tests (moved/ported). |
| `pkg/worker/runner.go` | `CommandRunner` interface. |
| `pkg/worker/dispatch.go` | `WorkerDispatch` lean command→client+capture. |
| `pkg/worker/dispatch_test.go` | Dispatch tests (fake client). |
| `pkg/worker/roles.go` | `Role`, `ScheduleEntry`, `LoadRoles`. |
| `pkg/worker/roles_test.go` | Roles parse + dispatchability guard. |
| `pkg/worker/standing.go` | `RunStanding` + `StandingDeps`. |
| `pkg/worker/standing_test.go` | Standing-behavior tests (fake clock/runner). |
| `cmd/worker/main.go` | Extend Plan A wiring → `RunStanding`. |
| `cmd/tools/play_as/repl_runner.go` | `replRunner` adapting `executeCommand` to `CommandRunner` (REPL side). |
| `cmd/tools/play_as/readline.go` | `readLogicalCommand` (kept in play_as; REPL/liner only). |
| `data/overmind/roles.yaml` | Resident standing-behavior config. |
| `data/scripts/idle_mine.smolt` | Idle local-mining script. |
| `data/scripts/track_station.smolt` | Tracking script (catalog example). |

Deleted by end of plan: `cmd/tools/play_as/{loop_block.go, scripts.go, schedule.go, tokens.go}` and their `_test.go` files (content relocated).

---

## Task 1: Move parser primitives → `pkg/worker/parse.go`

**Files:**
- Create: `pkg/worker/parse.go`, `pkg/worker/parse_test.go`
- Create: `cmd/tools/play_as/readline.go` (extract `readLogicalCommand`)
- Modify: `cmd/tools/play_as/main.go` (5 `splitArgs` call sites → `worker.SplitArgs`; `hasTopLevelOpenBrace`/`parseStatements`/`parseLoopHeader` call sites → `worker.*`)
- Source to relocate: `cmd/tools/play_as/loop_block.go` (parser parts only) + `splitArgs` from `cmd/tools/play_as/main.go:8237`
- Delete at end of task: nothing yet (loop_block.go still holds `executeLoop`/`readLogicalCommand` until Tasks 2–3)

**Interfaces:**
- Produces (package `worker`):
  - `type Statement struct { Raw string; Tokens []string }`
  - `func SplitArgs(s string) []string`
  - `func ScanBraceDepth(s string) (depth int, inQuote bool)`
  - `func HasTopLevelOpenBrace(s string) bool`
  - `func ParseStatements(body string) ([]Statement, error)`
  - `func ParseLoopHeader(stmt Statement) (count int, force bool, body string, isBlock bool, err error)`
  - unexported helpers: `afterTokens`, `extractBracedBody`, `blockPreview`

- [ ] **Step 1: Create the package directory and move the parser test**

Create `pkg/worker/parse_test.go`. From `cmd/tools/play_as/loop_block_test.go`, copy the test functions that exercise ONLY the parser/tokenizer (`scanBraceDepth`, `hasTopLevelOpenBrace`, `parseStatements`, `parseLoopHeader`, `splitArgs`, `Statement`, `blockPreview`, `afterTokens`, `extractBracedBody`). Change `package main` → `package worker`, and update identifiers to the exported names (`SplitArgs`, `ScanBraceDepth`, `HasTopLevelOpenBrace`, `ParseStatements`, `ParseLoopHeader`). Leave the `executeLoop`/`readLogicalCommand` tests in `loop_block_test.go` for now.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run TestParse -v` (adjust `-run` to match the copied test names)
Expected: FAIL — `package worker` / identifiers undefined (no `parse.go` yet).

- [ ] **Step 3: Create `pkg/worker/parse.go`**

`package worker`. Copy verbatim from `loop_block.go`: `scanBraceDepth`, `hasTopLevelOpenBrace`, `Statement`, `blockPreview`, `parseStatements`, `parseLoopHeader`, `afterTokens`, `extractBracedBody`. Copy `splitArgs` verbatim from `main.go:8237`. Export by capitalizing: `SplitArgs`, `ScanBraceDepth`, `HasTopLevelOpenBrace`, `ParseStatements`, `ParseLoopHeader`. Keep `afterTokens`, `extractBracedBody`, `blockPreview` unexported. Update internal references (e.g. `parseStatements` calls `splitArgs` → `SplitArgs`; `parseLoopHeader` calls `afterTokens`/`extractBracedBody`; keep imports `context`? no — parser needs only `fmt`, `strconv`, `strings`). Remove now-unused imports.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/worker/ -v`
Expected: PASS.

- [ ] **Step 5: Rewire play_as and remove the moved parser symbols from `package main`**

In `loop_block.go`, delete the moved functions (`scanBraceDepth`, `hasTopLevelOpenBrace`, `Statement`, `blockPreview`, `parseStatements`, `parseLoopHeader`, `afterTokens`, `extractBracedBody`) — but KEEP `executeLoop` and `readLogicalCommand` (Tasks 2–3 handle them). `executeLoop` references `Statement`/`parseLoopHeader`/`parseStatements`/`splitArgs` → point them at `worker.Statement` / `worker.ParseLoopHeader` / `worker.ParseStatements` / `worker.SplitArgs`. Delete `splitArgs` from `main.go:8237`; update its 5 call sites to `worker.SplitArgs`. Update `executeLogicalCommand` (main.go ~8586–8617) to `worker.HasTopLevelOpenBrace`, `worker.ParseLoopHeader`, `worker.ParseStatements`. Move `readLogicalCommand` into the new `cmd/tools/play_as/readline.go` (it uses `liner` + `worker.ScanBraceDepth`); delete it from `loop_block.go`. Add `import "github.com/rsned/spacemolt/pkg/worker"` where needed.

- [ ] **Step 6: Build, test, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/... ./cmd/tools/play_as/...`
Expected: all green, no new findings.

```bash
git add pkg/worker/parse.go pkg/worker/parse_test.go cmd/tools/play_as/
git commit -m "refactor(worker): extract command parser into pkg/worker"
```

---

## Task 2: Move token resolver → `pkg/worker/tokens.go`

**Files:**
- Create: `pkg/worker/tokens.go`, `pkg/worker/tokens_test.go`
- Modify: `cmd/tools/play_as/main.go:5591` (`resolveTokens` → `worker.ResolveTokens`)
- Delete: `cmd/tools/play_as/tokens.go`, `cmd/tools/play_as/tokens_test.go`

**Interfaces:**
- Consumes: `pkg/game` (`*game.State`).
- Produces:
  - `func ResolveTokens(args []string, state *game.State) ([]string, error)`
  - unexported: `tokenError` (with `Error()`), `resolveOneToken`, `knownPOITypes`, `tokenRe`

- [ ] **Step 1: Move the test**

Copy `cmd/tools/play_as/tokens_test.go` → `pkg/worker/tokens_test.go`; `package main` → `package worker`; `resolveTokens` → `ResolveTokens`. Keep references to `tokenError`/`resolveOneToken` as-is (same package).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/worker/ -run Token -v`
Expected: FAIL — `ResolveTokens` undefined.

- [ ] **Step 3: Create `pkg/worker/tokens.go`**

Copy `cmd/tools/play_as/tokens.go` verbatim into `package worker`; rename `resolveTokens` → `ResolveTokens`. Keep `tokenError`, `resolveOneToken`, `knownPOITypes`, `tokenRe` unexported. Imports unchanged (`fmt`, `regexp`, `slices`, `strconv`, `strings`, `pkg/game`).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/worker/ -v`
Expected: PASS.

- [ ] **Step 5: Rewire and delete**

Update `main.go:5591` to `worker.ResolveTokens(parts, client.GetState())`. Delete `cmd/tools/play_as/tokens.go` and `tokens_test.go`. NOTE: `executeLoop` (still in `loop_block.go`) references `*tokenError` via `errors.As` — that reference moves with `executeLoop` in Task 3; until then, temporarily keep a type alias is NOT needed because `executeLoop` will be moved next and play_as's non-loop path does not type-assert `tokenError`. If the build complains that `loop_block.go`'s `executeLoop` references an undefined `tokenError`, proceed directly to Task 3 (these two tasks may be committed together if the intermediate state does not build). Prefer: do Task 3 immediately after.

- [ ] **Step 6: Build, test, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/... ./cmd/tools/play_as/...`
Expected: green (if `executeLoop`'s `tokenError` reference breaks the build, fold Task 3 into this commit).

```bash
git add pkg/worker/tokens.go pkg/worker/tokens_test.go cmd/tools/play_as/
git commit -m "refactor(worker): extract token resolver into pkg/worker"
```

---

## Task 3: Move loop engine → `pkg/worker/loop.go`

**Files:**
- Create: `pkg/worker/loop.go`, `pkg/worker/loop_test.go`
- Modify: `cmd/tools/play_as/main.go` (~8617 `executeLoop` → `worker.ExecuteLoop`)
- Delete: `cmd/tools/play_as/loop_block.go`, `cmd/tools/play_as/loop_block_test.go`

**Interfaces:**
- Consumes: `Statement`, `ParseLoopHeader`, `ParseStatements`, `SplitArgs` (Task 1); `tokenError` (Task 2); `*game.GoalReachedError` (`pkg/game`).
- Produces:
  - `func ExecuteLoop(ctx context.Context, out io.Writer, count int, force bool, body []Statement, depth int, runStatement func(tokens []string) error) error`

- [ ] **Step 1: Move the loop-engine test**

Move the remaining test functions from `cmd/tools/play_as/loop_block_test.go` (those exercising `executeLoop`) into `pkg/worker/loop_test.go`; `package main` → `package worker`; `executeLoop` → `ExecuteLoop`; update any `parseStatements`/`splitArgs`/`Statement` references to exported names.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/worker/ -run Loop -v`
Expected: FAIL — `ExecuteLoop` undefined.

- [ ] **Step 3: Create `pkg/worker/loop.go`**

Copy `executeLoop` verbatim from `loop_block.go` into `package worker`; rename `executeLoop` → `ExecuteLoop`. Its recursive self-call and references become `ExecuteLoop`, `ParseLoopHeader`, `ParseStatements`, `SplitArgs`, `Statement`; the `errors.As(err, &tokErr)` keeps `*tokenError` (same package); `*game.GoalReachedError` stays. Imports: `context`, `errors`, `fmt`, `io`, `strings`, `pkg/game`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/worker/ -v`
Expected: PASS.

- [ ] **Step 5: Rewire and delete**

Update play_as's loop invocation (`main.go` ~8614–8617):
```go
runStatement := func(tokens []string) error {
    return executeCommand(client, ctx, tokens, format)
}
resultErr = worker.ExecuteLoop(ctx, os.Stdout, count, force, stmts, 0, runStatement)
```
(`stmts` is now `[]worker.Statement`.) Delete `cmd/tools/play_as/loop_block.go` and `loop_block_test.go` (their remaining content — parser moved in Task 1, `readLogicalCommand` moved to `readline.go` in Task 1, `executeLoop` moved here).

- [ ] **Step 6: Build, test, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/... ./cmd/tools/play_as/...`
Expected: all green.

```bash
git add pkg/worker/loop.go pkg/worker/loop_test.go cmd/tools/play_as/
git commit -m "refactor(worker): extract loop engine into pkg/worker"
```

---

## Task 4: Move script resolution → `pkg/worker/scripts.go`

**Files:**
- Create: `pkg/worker/scripts.go`, `pkg/worker/scripts_test.go`
- Modify: `cmd/tools/play_as/main.go` (`runScript` ~9662/9673 → `worker.ResolveScriptArg`/`worker.SplitScriptCommands`; `listScripts` ~604 → `worker.ListScripts`; `saveScript` ~636 → `worker.SaveScript`)
- Delete: `cmd/tools/play_as/scripts.go`, `cmd/tools/play_as/scripts_test.go`

**Interfaces:**
- Consumes: `ScanBraceDepth` (Task 1).
- Produces:
  - `func ResolveScriptArg(arg, agentID string) (string, bool)`
  - `func SplitScriptCommands(content string) ([]string, error)`
  - `func ListScripts(agentID string) (perAgent, shared []string)`
  - `func SaveScript(name, content string) error`
  - unexported: `validateScriptName`, `scriptSearchPaths`, `isExplicitScriptPath`, const `scriptExt`

- [ ] **Step 1: Move the test**

Copy `cmd/tools/play_as/scripts_test.go` → `pkg/worker/scripts_test.go`; `package main` → `package worker`; rename to exported names.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/worker/ -run Script -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Create `pkg/worker/scripts.go`**

Copy `cmd/tools/play_as/scripts.go` verbatim; export `ResolveScriptArg`, `SplitScriptCommands`, `ListScripts`, `SaveScript`. `splitScriptCommands` calls `scanBraceDepth` → `ScanBraceDepth`. Keep `validateScriptName`, `scriptSearchPaths`, `isExplicitScriptPath`, `scriptExt` unexported.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/worker/ -v`
Expected: PASS.

- [ ] **Step 5: Rewire and delete**

Update play_as call sites: `resolveScriptArg`→`worker.ResolveScriptArg`, `splitScriptCommands`→`worker.SplitScriptCommands`, `listScripts`→`worker.ListScripts`, `saveScript`→`worker.SaveScript`. Delete `cmd/tools/play_as/scripts.go` and `scripts_test.go`.

- [ ] **Step 6: Build, test, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/... ./cmd/tools/play_as/...`
Expected: all green.

```bash
git add pkg/worker/scripts.go pkg/worker/scripts_test.go cmd/tools/play_as/
git commit -m "refactor(worker): extract script resolution into pkg/worker"
```

---

## Task 5: Move scheduler → `pkg/worker/schedule.go`

**Files:**
- Create: `pkg/worker/schedule.go`, `pkg/worker/schedule_test.go`
- Modify: `cmd/tools/play_as/schedule.go` (keep ONLY `handleScheduleAdd`/`handleScheduleRemove`/`handleViewScheduled`, rewired to `worker.*`)
- Modify: `cmd/tools/play_as/main.go` (~399 `LoadScheduler`→`worker.LoadScheduler`; ~404 `scheduler.startLoop`→`scheduler.StartLoop`)
- Delete: `cmd/tools/play_as/schedule_test.go` (engine tests move; `schedule_cmd_test.go` for the handlers stays)

**Interfaces:**
- Produces:
  - `type ScheduledTask struct { ID int; Frequency string; Command string; CreatedAt time.Time; LastRun time.Time }` (JSON tags identical to current)
  - `type Scheduler struct { ... }` (unexported fields)
  - `func LoadScheduler(path string) (*Scheduler, error)`
  - `func (s *Scheduler) Add(freq, command string, now time.Time) (ScheduledTask, error)`
  - `func (s *Scheduler) Remove(id int) bool`
  - `func (s *Scheduler) List() []ScheduledTask`
  - `func (s *Scheduler) Due(now time.Time) []ScheduledTask`
  - `func (s *Scheduler) StartLoop(ctx context.Context, interval time.Duration, execMu *sync.Mutex, run func(ScheduledTask), nowFn func() time.Time)`
  - `func NextBoundary(freq string, now time.Time) time.Time`
  - `func CurrentBoundary(freq string, now time.Time) time.Time`
  - `var ValidFrequencies map[string]bool` (exported so dispatch/roles can validate)

- [ ] **Step 1: Move the engine test**

Copy `cmd/tools/play_as/schedule_test.go` → `pkg/worker/schedule_test.go`; `package main`→`package worker`; rename `startLoop`→`StartLoop`, `nextBoundary`→`NextBoundary`, `currentBoundary`→`CurrentBoundary`, `validFrequencies`→`ValidFrequencies`. Leave `schedule_cmd_test.go` (handler tests) in play_as for now.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/worker/ -run Schedul -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Create `pkg/worker/schedule.go`**

Copy from `cmd/tools/play_as/schedule.go` everything EXCEPT `handleScheduleAdd`/`handleScheduleRemove`/`handleViewScheduled` (those stay in play_as). Export: `StartLoop`, `NextBoundary`, `CurrentBoundary`, `ValidFrequencies`. Keep `checkDue`, `dueLocked`, `fire`, `nextIDLocked`, `saveLocked` unexported. `fire` currently prints `"⏰ backfilling…"` via `fmt.Printf` — leave as-is (writes to stdout; fine for both REPL and worker logs). Imports: `context, encoding/json, fmt, os, path/filepath, sort, strconv, strings, sync, time` (drop any unused after removing handlers).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/worker/ -v`
Expected: PASS.

- [ ] **Step 5: Rewire play_as handlers and keep them**

In `cmd/tools/play_as/schedule.go`, delete everything except the three `handle*` functions. Rewire them to the `worker` types: signatures become
```go
func handleScheduleAdd(s *worker.Scheduler, runner func(string), now time.Time, args []string)
func handleScheduleRemove(s *worker.Scheduler, args []string)
func handleViewScheduled(s *worker.Scheduler, now time.Time)
```
Inside, `s.Add`/`s.Remove`/`s.List` are now `worker.Scheduler` methods; `nextBoundary` → `worker.NextBoundary`. Add `import "github.com/rsned/spacemolt/pkg/worker"`. In `main.go`: `worker.LoadScheduler(...)`, and the `startLoop` call → `scheduler.StartLoop(ctx, game.SleepLong, &execMu, func(t worker.ScheduledTask){...}, func() time.Time { return time.Now().UTC() })`. Confirm `schedule_cmd_test.go` still compiles (handlers keep the same names, new param types) — update its `*Scheduler` references to `*worker.Scheduler`.

- [ ] **Step 6: Build, test, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/... ./cmd/tools/play_as/...`
Expected: all green.

```bash
git add pkg/worker/schedule.go pkg/worker/schedule_test.go cmd/tools/play_as/
git commit -m "refactor(worker): extract scheduler engine into pkg/worker"
```

---

## Task 6: Move KB-capture helpers → `pkg/worker/capture.go`

**Files:**
- Create: `pkg/worker/capture.go`, `pkg/worker/capture_test.go`
- Modify: `cmd/tools/play_as/kb_update.go` (keep thin `globalKB` wrappers; move bodies)
- Modify: `cmd/tools/play_as/demand_capture.go` (keep thin `captureMarket` wrapper; move body + helpers)
- Modify: imports in play_as files that call the helpers (no call-site signature changes — wrappers preserve old signatures)

**Interfaces:**
- Consumes: `pkg/game`, `pkg/knowledge`.
- Produces:
  - `func KBUpdateSystem(ctx context.Context, client game.GameClient, kb knowledge.Base) error`
  - `func KBUpdatePOI(ctx context.Context, client game.GameClient, kb knowledge.Base) error`
  - `func KBUpdateStation(ctx context.Context, client game.GameClient, kb knowledge.Base) error`
  - `func KBUpdateFacilities(ctx context.Context, client game.GameClient, kb knowledge.Base) error`
  - `func KBUpdateAll(ctx context.Context, client game.GameClient, kb knowledge.Base) error`
  - `func CaptureMarket(ctx context.Context, client game.GameClient, kb knowledge.Base)`
  - unexported helpers moved alongside: `currentTick`, `extractConnections`, `convertMarketListings`, `extractShipListingsFromRaw`, `parseStationBuyOrders`, `parseStationSellOrders`, `aggregateDemandHistory`, `aggregateSupplyHistory`, `isFresh`

**Detail:** These functions currently read package global `globalKB` (and some type-assert `globalKB.(*knowledge.SQLiteKB)`). The move replaces `globalKB` with the `kb` parameter; the `*knowledge.SQLiteKB` assertions become `kb.(*knowledge.SQLiteKB)`. Helpers that DON'T touch the KB (`convertMarketListings`, `extractConnections`, `extractShipListingsFromRaw`, `currentTick`, the `parse*`/`aggregate*`/`isFresh` pure functions) move verbatim. Do NOT move `kbUpdateMissions`/`kbUpdateFaction`/`cmdSeenFactions`/`seedFactionsFromList`/`formatMissionDiffValue`/`formatObjectivesDiff`/`formatObjectivesJSON` — they stay in play_as (not needed by residents; out of scope).

- [ ] **Step 1: Port the capture test**

Copy the body/order-parsing/aggregation tests from `cmd/tools/play_as/demand_capture_test.go` into `pkg/worker/capture_test.go` (`package worker`); these test the pure helpers (`parseStationBuyOrders`, `aggregateDemandHistory`, `isFresh`, etc.) and need no signature change. For any test that previously relied on `globalKB`, construct an in-memory KB: `kb, _ := knowledge.NewMemoryKB()` (or the existing in-memory constructor — confirm name in `pkg/knowledge/memory.go`) and pass it explicitly. Leave the original `demand_capture_test.go` tests that cover play_as-only paths in place.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/worker/ -run Capture -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Create `pkg/worker/capture.go`**

Move the function bodies listed in **Interfaces** above from `kb_update.go` / `demand_capture.go` into `package worker`. Apply the `globalKB → kb` substitution and export the five `KBUpdate*` + `KBUpdateAll` + `CaptureMarket`. `KBUpdateAll` is the moved `kbUpdateAll` (it sequences System→POI→Station→Facilities; rewire its internal calls to the exported names with the `kb` arg). Imports: `context, encoding/json, fmt, time, pkg/game, pkg/knowledge` (prune to actual use).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/worker/ -v`
Expected: PASS.

- [ ] **Step 5: Replace play_as bodies with thin wrappers**

In `cmd/tools/play_as/kb_update.go`, replace the moved functions with one-line wrappers preserving the OLD signatures so the ~14 call sites are untouched:
```go
func kbUpdateSystem(client game.GameClient, ctx context.Context) error {
    return worker.KBUpdateSystem(ctx, client, globalKB)
}
func kbUpdatePOI(client game.GameClient, ctx context.Context) error {
    return worker.KBUpdatePOI(ctx, client, globalKB)
}
func kbUpdateStation(client game.GameClient, ctx context.Context) error {
    return worker.KBUpdateStation(ctx, client, globalKB)
}
func kbUpdateFacilities(client game.GameClient, ctx context.Context) error {
    return worker.KBUpdateFacilities(ctx, client, globalKB)
}
func kbUpdateAll(client game.GameClient, ctx context.Context) error {
    return worker.KBUpdateAll(ctx, client, globalKB)
}
```
In `cmd/tools/play_as/demand_capture.go`, replace `captureMarket` with:
```go
func captureMarket(client game.GameClient, ctx context.Context) {
    worker.CaptureMarket(ctx, client, globalKB)
}
```
Remove the now-moved pure helpers from `demand_capture.go`/`kb_update.go` (they live in `worker` now; if a play_as test still references one directly, point it at `worker.`). Add the `worker` import. Keep the `globalKB == nil` guard behavior: `worker.KBUpdateSystem` etc. must early-return when `kb == nil` (the moved code already does this — verify the nil check moved with it).

- [ ] **Step 6: Build, test, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/... ./cmd/tools/play_as/...`
Expected: all green.

```bash
git add pkg/worker/capture.go pkg/worker/capture_test.go cmd/tools/play_as/
git commit -m "refactor(worker): extract KB-capture helpers into pkg/worker"
```

---

## Task 7: `CommandRunner` interface + lean `WorkerDispatch`

**Files:**
- Create: `pkg/worker/runner.go`, `pkg/worker/dispatch.go`, `pkg/worker/dispatch_test.go`

**Interfaces:**
- Consumes: `ResolveTokens` (Task 2), capture helpers (Task 6), `pkg/game`, `pkg/knowledge`.
- Produces:
  - `type CommandRunner interface { Run(ctx context.Context, tokens []string) error }`
  - `type WorkerDispatch struct { Client game.GameClient; KB knowledge.Base; Out io.Writer }`
  - `func NewWorkerDispatch(client game.GameClient, kb knowledge.Base, out io.Writer) *WorkerDispatch`
  - `func (d *WorkerDispatch) Run(ctx context.Context, tokens []string) error`
  - `func (d *WorkerDispatch) Supports(cmd string) bool`

- [ ] **Step 1: Write the failing test**

Create `pkg/worker/dispatch_test.go`. Use a fake `game.GameClient`. The interface is large (150+ methods), so embed it and override only what's needed:
```go
package worker

import (
	"context"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// fakeClient records which command methods were invoked.
type fakeClient struct {
	game.GameClient // embedded; unimplemented methods panic if called
	calls           []string
	state           *game.State
}

func (f *fakeClient) Undock(ctx context.Context) error { f.calls = append(f.calls, "undock"); return nil }
func (f *fakeClient) Dock(ctx context.Context) error   { f.calls = append(f.calls, "dock"); return nil }
func (f *fakeClient) Mine(ctx context.Context) error   { f.calls = append(f.calls, "mine"); return nil }
func (f *fakeClient) Refuel(ctx context.Context) error { f.calls = append(f.calls, "refuel"); return nil }
func (f *fakeClient) Repair(ctx context.Context) error { f.calls = append(f.calls, "repair"); return nil }
func (f *fakeClient) DepositAllItems(ctx context.Context) error {
	f.calls = append(f.calls, "deposit_all")
	return nil
}
func (f *fakeClient) SellAllBulk(ctx context.Context, reserved []string) error {
	f.calls = append(f.calls, "sell_all")
	return nil
}
func (f *fakeClient) Travel(ctx context.Context, poi string) (*game.TravelResult, error) {
	f.calls = append(f.calls, "travel:"+poi)
	return &game.TravelResult{}, nil
}
func (f *fakeClient) GetState() *game.State { return f.state }

func TestDispatchRunsKnownCommands(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, io.Discard)
	for _, tc := range [][]string{{"undock"}, {"mine"}, {"dock"}, {"refuel"}, {"deposit_all"}} {
		if err := d.Run(context.Background(), tc); err != nil {
			t.Fatalf("Run(%v): %v", tc, err)
		}
	}
	want := []string{"undock", "mine", "dock", "refuel", "deposit_all"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls=%v want=%v", f.calls, want)
	}
}

func TestDispatchTravelArg(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, io.Discard)
	if err := d.Run(context.Background(), []string{"travel", "POI-1"}); err != nil {
		t.Fatalf("travel: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "travel:POI-1" {
		t.Fatalf("calls=%v", f.calls)
	}
}

func TestDispatchUnknownCommandErrors(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, io.Discard)
	if err := d.Run(context.Background(), []string{"frobnicate"}); err == nil {
		t.Fatal("expected error for unknown command")
	}
	if d.Supports("frobnicate") {
		t.Fatal("Supports should be false for unknown command")
	}
	if !d.Supports("mine") {
		t.Fatal("Supports should be true for mine")
	}
}
```
> NOTE: confirm `game.TravelResult` is the exact return type (`pkg/game/interface.go:25`). If `GetState()` is not on the interface or has a different name, adjust the fake (check `pkg/game/interface.go`).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/worker/ -run Dispatch -v`
Expected: FAIL — `NewWorkerDispatch`/`CommandRunner` undefined.

- [ ] **Step 3: Write `pkg/worker/runner.go`**

```go
package worker

import "context"

// CommandRunner executes one already-tokenized command line. tokens[0] is the
// command name; the remaining elements are its arguments. It is the seam shared
// by the play_as REPL (rich dispatch) and the headless worker (lean dispatch).
type CommandRunner interface {
	Run(ctx context.Context, tokens []string) error
}
```

- [ ] **Step 4: Write `pkg/worker/dispatch.go`**

```go
package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// WorkerDispatch is the lean, headless command dispatch used by cmd/worker. It
// covers the curated worker-script vocabulary only; each command maps directly
// to a game.GameClient method, plus shared KB-capture for tracking commands.
// Unknown commands return an error (never silently ignored). KB may be nil, in
// which case tracking commands degrade to a no-op capture (movement/mining still
// work).
type WorkerDispatch struct {
	Client game.GameClient
	KB     knowledge.Base
	Out    io.Writer
}

// NewWorkerDispatch builds a dispatch over the given client and KB. out receives
// human-readable progress lines (worker stdout / logs).
func NewWorkerDispatch(client game.GameClient, kb knowledge.Base, out io.Writer) *WorkerDispatch {
	if out == nil {
		out = io.Discard
	}
	return &WorkerDispatch{Client: client, KB: kb, Out: out}
}

// supported is the curated command set. Keep in sync with data/scripts and
// data/overmind/roles.yaml; roles_test.go enforces that every command named
// there is present here.
var supported = map[string]bool{
	"undock": true, "dock": true, "travel": true, "mine": true,
	"refuel": true, "repair": true, "deposit_all": true, "sell_all": true,
	"view_market": true, "facilities": true, "kb_update": true,
	"get_status": true, "get_system": true, "get_cargo": true,
}

// Supports reports whether cmd is in the curated worker vocabulary.
func (d *WorkerDispatch) Supports(cmd string) bool { return supported[cmd] }

// Run dispatches one tokenized command. Token resolution ($SYSTEM$, $STATION$,
// POI-type tokens) is the caller's responsibility (RunStanding resolves before
// calling) — Run treats tokens as literal.
func (d *WorkerDispatch) Run(ctx context.Context, tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	cmd := tokens[0]
	args := tokens[1:]
	switch cmd {
	case "undock":
		return d.Client.Undock(ctx)
	case "dock":
		return d.Client.Dock(ctx)
	case "mine":
		return d.Client.Mine(ctx)
	case "refuel":
		return d.Client.Refuel(ctx)
	case "repair":
		return d.Client.Repair(ctx)
	case "deposit_all":
		return d.Client.DepositAllItems(ctx)
	case "sell_all":
		return d.Client.SellAllBulk(ctx, nil)
	case "travel":
		if len(args) < 1 {
			return fmt.Errorf("travel: missing target POI")
		}
		_, err := d.Client.Travel(ctx, args[0])
		return err
	case "get_status":
		return d.Client.GetStatus(ctx)
	case "get_system":
		return d.Client.GetSystem(ctx)
	case "get_cargo":
		return d.Client.GetCargo(ctx)
	case "view_market":
		if err := d.Client.ViewMarket(ctx, map[string]any{}); err != nil {
			return err
		}
		CaptureMarket(ctx, d.Client, d.KB)
		return nil
	case "facilities":
		if err := d.Client.Facility(ctx, map[string]any{}); err != nil {
			return err
		}
		return KBUpdateFacilities(ctx, d.Client, d.KB)
	case "kb_update":
		return KBUpdateAll(ctx, d.Client, d.KB)
	default:
		return fmt.Errorf("worker dispatch: unsupported command %q", cmd)
	}
}
```
> NOTE: confirm the `view_market` / `facilities` payload shape against play_as's `executeCommand` cases (`main.go` `view_market`, `facility`). If those commands require an explicit `station_id`, build the payload from `d.Client.GetState()` (current docked POI) instead of `map[string]any{}`. Adjust before finalizing; the test does not cover the live payload.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/worker/ -run Dispatch -v`
Expected: PASS.

- [ ] **Step 6: Build, test, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/...`
Expected: all green.

```bash
git add pkg/worker/runner.go pkg/worker/dispatch.go pkg/worker/dispatch_test.go
git commit -m "feat(worker): CommandRunner interface and lean WorkerDispatch"
```

---

## Task 8: Roles loader + `roles.yaml` + seed scripts + dispatchability guard

**Files:**
- Create: `pkg/worker/roles.go`, `pkg/worker/roles_test.go`
- Create: `data/overmind/roles.yaml`
- Create: `data/scripts/idle_mine.smolt`, `data/scripts/track_station.smolt`

**Interfaces:**
- Consumes: `WorkerDispatch.Supports` (Task 7), `SplitArgs`/`ParseStatements` (Task 1), `gopkg.in/yaml.v3`.
- Produces:
  - `type ScheduleEntry struct { Every string; Command string }`
  - `type Role struct { Schedule []ScheduleEntry; Idle string; IdleParams map[string]string }`
  - `func LoadRoles(path string) (map[string]Role, error)`

- [ ] **Step 1: Create the seed config + scripts**

`data/overmind/roles.yaml`:
```yaml
# Per-role standing behaviors for overmind workers (Plan B).
# `every` uses the Scheduler frequency set: hourly | daily | weekly.
# Commands and idle scripts may use $TOKEN$s resolved from live game state
# ($SYSTEM$, $SHIP$, $CREDITS$, and POI-type tokens like $ASTEROID_BELT$, $STATION$).
roles:
  resident:
    schedule:
      - { every: hourly, command: "kb_update" }
      - { every: hourly, command: "view_market" }
    idle: idle_mine
    idle_params:
      N: "20"
```

`data/scripts/idle_mine.smolt`:
```
# Resident idle local-mining cycle. Location POIs resolve from live state.
undock
travel $ASTEROID_BELT$
loop -f 20 mine
travel $STATION$
dock
deposit_all
```

`data/scripts/track_station.smolt`:
```
# Resident market/facility tracking pass (catalog example).
kb_update
view_market
facilities
```

- [ ] **Step 2: Write the failing test**

`pkg/worker/roles_test.go`:
```go
package worker

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRolesParsesResident(t *testing.T) {
	roles, err := LoadRoles(filepath.Join("..", "..", "data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	r, ok := roles["resident"]
	if !ok {
		t.Fatal("resident role missing")
	}
	if r.Idle != "idle_mine" {
		t.Fatalf("idle=%q", r.Idle)
	}
	if len(r.Schedule) == 0 {
		t.Fatal("resident has no schedule entries")
	}
	if r.IdleParams["N"] != "20" {
		t.Fatalf("idle_params N=%q", r.IdleParams["N"])
	}
}

func TestLoadRolesRejectsMissing(t *testing.T) {
	if _, err := LoadRoles(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestSeededCommandsAreDispatchable kills lean-dispatch divergence: every
// command named in roles.yaml schedule entries AND in every data/scripts script
// referenced by a role must be in the WorkerDispatch curated vocabulary.
// Tokens, loop headers, blank lines, and comments are skipped.
func TestSeededCommandsAreDispatchable(t *testing.T) {
	d := NewWorkerDispatch(nil, nil, io.Discard)
	roles, err := LoadRoles(filepath.Join("..", "..", "data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	check := func(cmdLine string) {
		for _, st := range mustStatements(t, cmdLine) {
			head := strings.ToLower(firstWord(st))
			if head == "loop" || head == "" {
				continue // loop bodies are checked statement-by-statement below
			}
			if !d.Supports(head) {
				t.Errorf("command %q (from %q) not supported by WorkerDispatch", head, cmdLine)
			}
		}
	}
	for name, r := range roles {
		for _, se := range r.Schedule {
			check(se.Command)
		}
		if r.Idle != "" {
			path, ok := ResolveScriptArg(r.Idle, name)
			if !ok {
				t.Fatalf("role %q idle script %q not found", name, r.Idle)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			cmds, err := SplitScriptCommands(string(content))
			if err != nil {
				t.Fatalf("split %s: %v", path, err)
			}
			for _, c := range cmds {
				check(c)
			}
		}
	}
}

func firstWord(s string) string {
	toks := SplitArgs(s)
	if len(toks) == 0 {
		return ""
	}
	return toks[0]
}

func mustStatements(t *testing.T, body string) []string {
	t.Helper()
	stmts, err := ParseStatements(body)
	if err != nil {
		t.Fatalf("ParseStatements(%q): %v", body, err)
	}
	var out []string
	for _, s := range stmts {
		// For a loop block, also pull the inner statements so their commands
		// (e.g. "mine") are validated.
		if len(s.Tokens) > 0 && strings.EqualFold(s.Tokens[0], "loop") {
			_, _, inner, isBlock, perr := ParseLoopHeader(s)
			if perr == nil {
				if isBlock {
					innerStmts, _ := ParseStatements(inner)
					for _, is := range innerStmts {
						out = append(out, is.Raw)
					}
				} else {
					out = append(out, inner)
				}
				continue
			}
		}
		out = append(out, s.Raw)
	}
	return out
}
```
> The `check` helper validates the head token; for `loop -f 20 mine` the head is `loop` (skipped) and `mustStatements` already pulled out `mine` as a separate entry, so `mine` is validated. `NewWorkerDispatch(nil, nil, …)` is safe because `Supports` never touches the client.

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./pkg/worker/ -run Roles -v` and `-run Seeded`
Expected: FAIL — `LoadRoles` undefined.

- [ ] **Step 4: Write `pkg/worker/roles.go`**

```go
package worker

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ScheduleEntry is one recurring command in a role's standing behavior.
type ScheduleEntry struct {
	Every   string `yaml:"every"`   // hourly | daily | weekly
	Command string `yaml:"command"` // a command line; may contain $TOKEN$s
}

// Role is a worker's default standing behavior: recurring scheduled commands
// plus an idle script run on idle cycles.
type Role struct {
	Schedule   []ScheduleEntry   `yaml:"schedule"`
	Idle       string            `yaml:"idle"`        // bare script name (data/scripts)
	IdleParams map[string]string `yaml:"idle_params"` // substituted into the idle script
}

type rolesFile struct {
	Roles map[string]Role `yaml:"roles"`
}

// LoadRoles parses the roles config at path. Every schedule entry must name a
// valid frequency.
func LoadRoles(path string) (map[string]Role, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("worker: read roles: %w", err)
	}
	var rf rolesFile
	if err := yaml.Unmarshal(raw, &rf); err != nil {
		return nil, fmt.Errorf("worker: parse roles: %w", err)
	}
	for name, r := range rf.Roles {
		for i, se := range r.Schedule {
			if !ValidFrequencies[se.Every] {
				return nil, fmt.Errorf("worker: role %q schedule[%d]: invalid frequency %q", name, i, se.Every)
			}
			if se.Command == "" {
				return nil, fmt.Errorf("worker: role %q schedule[%d]: empty command", name, i)
			}
		}
	}
	return rf.Roles, nil
}
```
> Confirm `ValidFrequencies` was exported in Task 5. If `gopkg.in/yaml.v3` is not the module's YAML lib, run `grep -rhoE '"(gopkg.in/yaml.v[23]|sigs.k8s.io/yaml)"' --include=*.go . | sort -u` and use whichever prints (Plan A's `supervisor/config.go` already loads YAML — match it).

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/worker/ -v`
Expected: PASS (roles parse + dispatchability guard).

- [ ] **Step 6: Build, test, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/...`
Expected: all green.

```bash
git add pkg/worker/roles.go pkg/worker/roles_test.go data/overmind/roles.yaml data/scripts/idle_mine.smolt data/scripts/track_station.smolt
git commit -m "feat(worker): roles loader, seed roles.yaml + scripts, dispatchability guard"
```

---

## Task 9: Standing-behavior driver `RunStanding`

**Files:**
- Create: `pkg/worker/standing.go`, `pkg/worker/standing_test.go`

**Interfaces:**
- Consumes: `Role` (Task 8), `Scheduler`/`ScheduledTask` (Task 5), `CommandRunner` (Task 7), `ResolveTokens` (Task 2), `ResolveScriptArg`/`SplitScriptCommands` (Task 4), `ParseStatements`/`Statement` (Task 1), `ExecuteLoop` (Task 3), `pkg/game`.
- Produces:
  - `type StandingDeps struct { Runner CommandRunner; Scheduler *Scheduler; Client interface{ GetState() *game.State }; ExecMu *sync.Mutex; Paused func() bool; Out io.Writer; NowFn func() time.Time; IdleInterval time.Duration; ScheduleInterval time.Duration; AgentID string }`
  - `func RunStanding(ctx context.Context, role Role, deps StandingDeps) error`

- [ ] **Step 1: Write the failing test**

`pkg/worker/standing_test.go`:
```go
package worker

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// recordRunner records every command line it runs.
type recordRunner struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordRunner) Run(_ context.Context, tokens []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, joinTokens(tokens))
	return nil
}
func (r *recordRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}
func joinTokens(t []string) string {
	s := ""
	for i, x := range t {
		if i > 0 {
			s += " "
		}
		s += x
	}
	return s
}

type stateClient struct{ st *game.State }

func (s stateClient) GetState() *game.State { return s.st }

func TestRunStandingRunsIdleLoopThenStopsOnCancel(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")

	ctx, cancel := context.WithCancel(context.Background())
	// idle script with no tokens so resolution is a no-op.
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner:       r,
		Scheduler:    sched,
		Client:       stateClient{st: &game.State{}},
		ExecMu:       &mu,
		Paused:       func() bool { return false },
		Out:          io.Discard,
		NowFn:        func() time.Time { return time.Unix(0, 0).UTC() },
		IdleInterval: time.Millisecond,
		AgentID:      "test",
	}
	// Override script resolution by writing the script under a temp scripts dir
	// the resolver can find; simplest: inject commands directly (see Step 4 note).
	done := make(chan struct{})
	go func() { _ = RunStanding(ctx, role, deps); close(done) }()
	// Let a few idle passes run.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunStanding did not return after cancel")
	}
	if len(r.snapshot()) == 0 {
		t.Fatal("idle loop never ran a command")
	}
}

func TestRunStandingPausedDoesNotRunIdle(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var paused atomic.Bool
	paused.Store(true)
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: paused.Load, Out: io.Discard,
		NowFn: func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(20 * time.Millisecond)
	if n := len(r.snapshot()); n != 0 {
		t.Fatalf("paused worker ran %d commands", n)
	}
}
```
> The idle-script body must be injectable for tests. Implement `RunStanding` so that when `ResolveScriptArg` cannot find the role's idle script it falls back to a built-in no-op idle script consisting of a single `get_status` command (which `recordRunner` happily records). Name the missing script `noop_idle` in the test so the fallback path is exercised. Document this fallback in the code. (Alternatively, write a real `noop_idle.smolt` into a temp scripts dir — the fallback keeps the test hermetic.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/worker/ -run RunStanding -v`
Expected: FAIL — `RunStanding`/`StandingDeps` undefined.

- [ ] **Step 3: Choose the idle back-off Sleep constant**

The idle loop must pause between passes / when paused using a `pkg/game` Sleep constant (Global Constraint). Use `game.SleepShort` (~3.3s) as the default `IdleInterval` when the caller leaves it zero, and `game.SleepMedium` (5s) as the paused re-check interval. Read `pkg/game/constants.go` to confirm these exist and their values before using them.

- [ ] **Step 4: Write `pkg/worker/standing.go`**

```go
package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// StandingDeps are the collaborators RunStanding needs. All are injectable so
// the driver is testable without a game connection.
type StandingDeps struct {
	Runner    CommandRunner               // executes a tokenized command (WorkerDispatch)
	Scheduler *Scheduler                  // per-agent recurring tasks
	Client    interface{ GetState() *game.State } // for token resolution
	ExecMu    *sync.Mutex                 // serializes scheduled + idle work on the one game conn
	Paused    func() bool                 // gate from the control reader's paused flag
	Out       io.Writer                   // worker stdout / logs
	NowFn     func() time.Time            // injectable clock

	IdleInterval     time.Duration // between idle passes (0 → game.SleepShort)
	ScheduleInterval time.Duration // scheduler tick (0 → game.SleepLong)
	AgentID          string        // for script resolution search paths
}

// RunStanding drives a worker's default standing behavior until ctx is
// cancelled: it registers the role's scheduled commands with the Scheduler and
// runs the role's idle script in a loop, both serialized on deps.ExecMu so they
// never interleave on the single game connection. It returns when ctx is
// cancelled, after any in-flight idle pass completes.
func RunStanding(ctx context.Context, role Role, deps StandingDeps) error {
	if deps.Out == nil {
		deps.Out = io.Discard
	}
	if deps.NowFn == nil {
		deps.NowFn = func() time.Time { return time.Now().UTC() }
	}
	if deps.IdleInterval == 0 {
		deps.IdleInterval = game.SleepShort
	}
	if deps.ScheduleInterval == 0 {
		deps.ScheduleInterval = game.SleepLong
	}

	// Register schedule entries (idempotent: skip a command already registered,
	// so a restart does not duplicate it).
	if deps.Scheduler != nil {
		existing := make(map[string]bool)
		for _, t := range deps.Scheduler.List() {
			existing[t.Frequency+"|"+t.Command] = true
		}
		for _, se := range role.Schedule {
			if existing[se.Every+"|"+se.Command] {
				continue
			}
			if _, err := deps.Scheduler.Add(se.Every, se.Command, deps.NowFn()); err != nil {
				fmt.Fprintf(deps.Out, "standing: schedule add %q failed: %v\n", se.Command, err)
			}
		}
		// run executes one scheduled command line under ExecMu.
		run := func(t ScheduledTask) {
			fmt.Fprintf(deps.Out, "⏰ [scheduled %s] %s\n", t.Frequency, t.Command)
			deps.runLine(ctx, t.Command)
		}
		deps.Scheduler.StartLoop(ctx, deps.ScheduleInterval, deps.ExecMu, run, deps.NowFn)
	}

	// Resolve the idle script once into command lines.
	idleCmds := deps.resolveIdle(role)

	// Idle loop.
	for {
		if ctx.Err() != nil {
			return nil
		}
		if deps.Paused != nil && deps.Paused() {
			if sleepCtx(ctx, game.SleepMedium) {
				return nil
			}
			continue
		}
		deps.ExecMu.Lock()
		for _, line := range idleCmds {
			if ctx.Err() != nil {
				deps.ExecMu.Unlock()
				return nil
			}
			deps.runLine(ctx, line)
		}
		deps.ExecMu.Unlock()
		if sleepCtx(ctx, deps.IdleInterval) {
			return nil
		}
	}
}

// resolveIdle loads the role's idle script into command lines, falling back to a
// single get_status when the script is absent (keeps an unconfigured worker
// alive and tests hermetic).
func (deps StandingDeps) resolveIdle(role Role) []string {
	if role.Idle == "" {
		return []string{"get_status"}
	}
	path, ok := ResolveScriptArg(role.Idle, deps.AgentID)
	if !ok {
		fmt.Fprintf(deps.Out, "standing: idle script %q not found; using get_status\n", role.Idle)
		return []string{"get_status"}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(deps.Out, "standing: read idle script %q: %v; using get_status\n", role.Idle, err)
		return []string{"get_status"}
	}
	cmds, err := SplitScriptCommands(string(content))
	if err != nil {
		fmt.Fprintf(deps.Out, "standing: parse idle script %q: %v; using get_status\n", role.Idle, err)
		return []string{"get_status"}
	}
	return cmds
}

// runLine resolves tokens against live state and executes a single command line.
// A loop header is expanded via ExecuteLoop; a plain line goes straight to the
// runner. Errors are logged (idle work is best-effort, force-like).
func (deps StandingDeps) runLine(ctx context.Context, line string) {
	stmts, err := ParseStatements(line)
	if err != nil {
		fmt.Fprintf(deps.Out, "standing: parse %q: %v\n", line, err)
		return
	}
	for _, st := range stmts {
		if ctx.Err() != nil {
			return
		}
		if len(st.Tokens) > 0 && strings.EqualFold(st.Tokens[0], "loop") {
			count, force, body, isBlock, perr := ParseLoopHeader(st)
			if perr != nil {
				fmt.Fprintf(deps.Out, "standing: %v\n", perr)
				continue
			}
			var inner []Statement
			if isBlock {
				inner, err = ParseStatements(body)
			} else {
				inner = []Statement{{Raw: body, Tokens: SplitArgs(body)}}
			}
			if err != nil {
				fmt.Fprintf(deps.Out, "standing: %v\n", err)
				continue
			}
			rs := func(tokens []string) error { return deps.dispatch(ctx, tokens) }
			if lerr := ExecuteLoop(ctx, deps.Out, count, force, inner, 0, rs); lerr != nil {
				fmt.Fprintf(deps.Out, "standing: loop: %v\n", lerr)
			}
			continue
		}
		if derr := deps.dispatch(ctx, st.Tokens); derr != nil {
			fmt.Fprintf(deps.Out, "standing: %q: %v\n", st.Raw, derr)
		}
	}
}

// dispatch resolves tokens against live state, then runs them.
func (deps StandingDeps) dispatch(ctx context.Context, tokens []string) error {
	var st *game.State
	if deps.Client != nil {
		st = deps.Client.GetState()
	}
	resolved, err := ResolveTokens(tokens, st)
	if err != nil {
		return err
	}
	return deps.Runner.Run(ctx, resolved)
}

// sleepCtx sleeps d or returns true if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) (cancelled bool) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}
```
`resolveIdle` reads the script with `os.ReadFile` (the `os` import is in the block above) and passes `string(content)` to `SplitScriptCommands`. No extra helper is needed.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/worker/ -run RunStanding -v`
Expected: PASS (idle loop runs & stops on cancel; paused worker runs nothing).

- [ ] **Step 6: Full suite, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/...`
Expected: all green.

```bash
git add pkg/worker/standing.go pkg/worker/standing_test.go
git commit -m "feat(worker): RunStanding standing-behavior driver"
```

---

## Task 10: Wire `cmd/worker` to `RunStanding`

**Files:**
- Modify: `cmd/worker/main.go`
- Modify: `cmd/worker/main_test.go` (extend)

**Interfaces:**
- Consumes: `worker.{LoadRoles, NewWorkerDispatch, LoadScheduler, RunStanding, StandingDeps}`, `knowledge.NewSQLiteKB`.

- [ ] **Step 1: Add CLI flags and KB open**

In `cmd/worker/main.go` add flags after the existing ones:
```go
rolesPath := flag.String("roles", filepath.Join("data", "overmind", "roles.yaml"), "Path to roles config")
kbPath := flag.String("kb-path", filepath.Join("data", "spacemolt-knowledge.db"), "Path to shared knowledge base")
```
After the game client is connected and `hello` is sent (inside the `conn != nil` block, before the heartbeat loop), open the shared KB (best-effort — a resident with no KB still mines):
```go
var kb knowledge.Base
if sqliteKB, kbErr := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *kbPath, WAL: true}); kbErr != nil {
    logger.Printf("warning: open KB %s: %v (tracking disabled)", *kbPath, kbErr)
} else {
    kb = sqliteKB
    defer func() { _ = sqliteKB.Close() }()
}
```
> Confirm `knowledge.Base`, `knowledge.NewSQLiteKB`, `knowledge.Config{DBPath, WAL}` against `pkg/knowledge` (verified pattern from play_as/databot). Add the `pkg/knowledge` import.

- [ ] **Step 2: Build the dispatch + scheduler and launch RunStanding**

Still inside the `conn != nil` block, before `// ── Step 7: Heartbeat loop`:
```go
// ── Standing behavior ────────────────────────────────────────────────────
roles, rolesErr := worker.LoadRoles(*rolesPath)
if rolesErr != nil {
    logger.Printf("warning: load roles %s: %v (no standing behavior)", *rolesPath, rolesErr)
}
roleCfg, haveRole := roles[*role]
if haveRole {
    dispatch := worker.NewWorkerDispatch(client, kb, os.Stdout)
    sched, schedErr := worker.LoadScheduler(filepath.Join("data", "agents", *agentID, "schedule.json"))
    if schedErr != nil {
        logger.Printf("warning: load scheduler: %v", schedErr)
    }
    var execMu sync.Mutex
    go func() {
        deps := worker.StandingDeps{
            Runner:    dispatch,
            Scheduler: sched,
            Client:    client,
            ExecMu:    &execMu,
            Paused:    paused.Load,
            Out:       os.Stdout,
            NowFn:     func() time.Time { return time.Now().UTC() },
            AgentID:   *agentID,
        }
        if rerr := worker.RunStanding(ctx, roleCfg, deps); rerr != nil {
            logger.Printf("standing behavior ended: %v", rerr)
        }
    }()
    standing = *role // report the role label as the standing behavior in heartbeats
    logger.Printf("standing behavior started for role %q", *role)
} else {
    logger.Printf("no standing behavior for role %q; idle heartbeat only", *role)
}
```
> `paused` is the existing `atomic.Bool` declared in the reader goroutine scope. Move its declaration up so it is visible here (declare `var paused atomic.Bool` before the reader goroutine and the standing block). `client` already satisfies `interface{ GetState() *game.State }`. The existing `sync` import covers `sync.Mutex`; add it if missing.

- [ ] **Step 3: Extend the worker test**

In `cmd/worker/main_test.go`, add a build-level smoke test that `worker.LoadRoles` reads the seeded config and the binary's flags parse. Keep it lightweight (no game connection):
```go
func TestRolesConfigLoads(t *testing.T) {
	roles, err := worker.LoadRoles(filepath.Join("..", "..", "data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	if _, ok := roles["resident"]; !ok {
		t.Fatal("resident role missing from seeded roles.yaml")
	}
}
```
Add imports `path/filepath` and `github.com/rsned/spacemolt/pkg/worker`.

- [ ] **Step 4: Run worker tests**

Run: `go test ./cmd/worker/ -v`
Expected: PASS.

- [ ] **Step 5: Build the worker binary to bin/**

Run: `go build -o bin/worker ./cmd/worker && echo OK`
Expected: `OK` (binary in `bin/`, never repo root).

- [ ] **Step 6: Full suite, lint, commit**

Run: `go build ./... && go test ./... && golangci-lint run ./cmd/worker/... ./pkg/worker/...`
Expected: all green.

```bash
git add cmd/worker/main.go cmd/worker/main_test.go
git commit -m "feat(worker): drive resident standing behavior in cmd/worker"
```

---

## Task 11: End-to-end verification & docs

**Files:**
- Modify: `cmd/tools/play_as/README.md` (note automation primitives now live in `pkg/worker`)
- Verify only: no code changes expected beyond doc.

- [ ] **Step 1: Whole-repo green**

Run: `go build ./... && go test ./... && golangci-lint run`
Expected: all green, zero new lint findings. If `golangci-lint run` (whole repo) surfaces pre-existing findings unrelated to this work, scope to the touched packages and note it.

- [ ] **Step 2: Manual smoke (supervisor + worker), optional but recommended**

Build both binaries and run the overmind against the seeded fleet to confirm a resident worker boots, sends `hello`, and logs standing-behavior lines (scheduled `kb_update`/`view_market` and idle mining attempts). This needs live game credentials; if unavailable, skip and note it.
```bash
go build -o bin/overmind ./cmd/overmind && go build -o bin/worker ./cmd/worker
# Run per cmd/overmind usage; observe worker stdout for "⏰ [scheduled ...]" and idle "── [n/N]" loop lines.
```

- [ ] **Step 3: Update the play_as README**

Add a short note under the scripting/automation section of `cmd/tools/play_as/README.md`: the loop engine, token resolver, script resolution, and scheduler now live in `pkg/worker` and are shared with the headless overmind worker (`cmd/worker`); play_as imports them and is behavior-compatible.

- [ ] **Step 4: Commit**

```bash
git add cmd/tools/play_as/README.md
git commit -m "docs(play_as): note automation primitives moved to pkg/worker"
```

---

## Self-Review Notes (coverage vs. spec)

- **Extract automation primitives → pkg/worker:** Tasks 1–5 (parse, loop, tokens, scripts, schedule).
- **KB-tracking fidelity via shared capture:** Task 6 (capture helpers, `(ctx, client, kb)`, play_as wrappers).
- **Lean dispatch + CommandRunner:** Task 7.
- **roles.yaml + shared script catalog + divergence guard:** Task 8.
- **Standing behavior (scheduler + idle loop under one ExecMu, pause/abort):** Task 9.
- **cmd/worker wiring (real worker, KB open, RunStanding, role label in heartbeat):** Task 10.
- **play_as behavior unchanged:** enforced by relocated tests staying green + thin wrappers (Tasks 1–6) and the regression run in Task 11.
- **Deferred (asserted out of scope):** autopilot extraction, assign_task path, mobile roles, guardrails, web UI, finer scheduler cadence (seed uses `hourly`).
```
