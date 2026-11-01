# Line Dashboard v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the one-line, hand-written dashboard on :8093 into one that works out every CRFT production, upkeep and sales line from the fleet's own configuration. The board is grouped by station and category, each line says which rule broke, and shared tanks are drawn as shared.

**Architecture:** The worker gains two small records that the dashboard reads from `data/agents/<id>/`:
- `task-outcomes.json`: the latest outcome of each scheduled task, idle line and fleet behaviour
- `craft-queue.json`: the agent's queued craft jobs

`pkg/linedash` gains a derivation stage. It builds the existing `Line`/`Node`/`Edge` structures from these sources, instead of reading `lines.yaml`:
- schedules
- fleet yamls
- the knowledge DB: stations, recipes, item categories, our faction facilities
- the facility catalog: upkeep inputs
- market.db: our asks

The existing `Evaluate`, staleness rule and `Layout` are reused. The board and line templates are rewritten for the station-grouped table and shared tanks.

**Tech Stack:** Go 1.24+, `database/sql` + sqlite (read-only), `html/template`, `gopkg.in/yaml.v3`; existing packages `pkg/worker`, `pkg/linedash`, `cmd/worker`, `cmd/tools/line-dashboard`.

**Spec:** `docs/superpowers/specs/2026-11-01-line-dashboard-v2-design.md`

## Global Constraints

- Read-only dashboard: it never calls the game and never writes game state. It opens every database read-only.
- `energy_crystal` and `starshine` never appear on a line. Keep `forbiddenItems` and apply it in derivation: skip those items, don't fail.
- Schedule seeding is additive only. A command added to `data/overmind/roles.yaml` fires forever once seeded.
- Workers use the Sleep constants in `pkg/game/constants.go` and the `settle` seam. No `time.Sleep` in pkg/worker.
- Check real API and DB field names before coding against them. The names in this plan were read from the live DBs and catalog on 2026-11-01.
- Station ids are dual-named (POI id ≠ base id). Never compare raw ids across sources; always resolve through the station index (Task 4).
- `golangci-lint run` reports no new findings. Run `go build ./...` and `go test ./...` before each commit. Binaries go in `bin/`.
- Never `git add -A`: `data/*.json` is dirty with runtime state. Stage by path.
- The base font is at least 28px and the diagram draws at least 2 px per unit. Both shipped 2026-11-01; don't regress them.

## Review Focus

1. **The same station under two ids**: `voss_redoubt` (POI) in a fleet yaml, `voss_redoubt_station` (base) in faction_facilities. It must be one station section, not two. Pinned in Task 4 (`TestStationIndexResolvesPOIAndBaseToOneStation`) and Task 6 (`TestDeriveMergesAliasedStation`).
2. **A schedule.json that is corrupt, missing, or in the old `{"tasks":[...]}` shape.** Skip that agent with a warning; never blank the board. Task 5 (`TestLoadSchedulesSkipsCorruptKeepsOthers`, `TestLoadSchedulesAcceptsWrappedShape`).
3. **An outcome recorded under an old task id after the schedule renumbered.** Ignore keys that no longer exist, and never show a why from a task that's gone. Task 7 (`TestWhyIgnoresOutcomeForRemovedTask`).
4. **A worker writing task-outcomes.json while the dashboard reads it.** The write must be atomic (temp file + rename), so a reader never sees half a file. Task 1 (`TestOutcomeLogWriteIsAtomic`).
5. **A failure followed by a success.** The why must clear. A stale refusal under a green line is the anti-goal. Task 7 (`TestWhyClearsAfterLaterSuccess`).

---

### Task 1: Outcome records (`pkg/outcomes`)

A leaf package both sides import, so the dashboard never imports `pkg/worker`.

**Files:**
- Create: `pkg/outcomes/outcomes.go`, `pkg/outcomes/outcomes_test.go`

**Interfaces:**
- Produces:
  - `type Outcome struct { Key, Command, Status, Reason string; At time.Time }`. Status is `"ok"` or `"failed"`. JSON tags: `key`, `command`, `status`, `reason`, `at`.
  - `const FileName = "task-outcomes.json"`
  - `func New(agentDir string) *Log`
  - `func (l *Log) Record(key, command string, err error, at time.Time)`: logs a write failure to nothing, because a lost record costs one stale why and must never stop a worker.
  - `func Load(agentDir string) (map[string]Outcome, error)`: a missing file gives an empty map and a nil error.
  - `func Reason(err error) string`: the error on one line, newlines replaced with `"; "`, cut to 300 runes.
  - Key formats, used by Tasks 2 and 7:
    - `"task:<id>"` for scheduled tasks
    - `"idle:<command line>"` for idle script lines
    - `"behaviour:consign"`, `"behaviour:consign_offer"`, `"behaviour:faction_pull"`, `"behaviour:shopkeep"`

- [ ] **Step 1: Write the failing tests**

```go
func TestOutcomeRecordAndLoad(t *testing.T)           // Record("task:7","craft x 1",nil,t0) then Record("task:8","buy y",errors.New("price 70 exceeds ceiling 50 (replan)"),t1); Load -> 2 entries; [task:8].Status=="failed", Reason=="price 70 exceeds ceiling 50 (replan)"; [task:7].Status=="ok", Reason==""
func TestOutcomeRecordOverwritesSameKey(t *testing.T)  // failed then ok for "task:7" -> one entry, Status "ok", At == second time
func TestOutcomeLoadMissingFileIsEmpty(t *testing.T)   // Load(t.TempDir()) -> len 0, err nil
func TestOutcomeLogWriteIsAtomic(t *testing.T)         // after Record, no "*.tmp" file remains in the dir, and the file parses as JSON
func TestReasonOneLineAndCapped(t *testing.T)          // Reason(errors.New("a\nb")) == "a; b"; a 400-rune message -> 300 runes
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/outcomes/`. Expected: FAIL, package has no non-test files / undefined: New.
- [ ] **Step 3: Implement `pkg/outcomes/outcomes.go`.** `Log` holds the path and a `sync.Mutex`. `Record` loads, sets and saves the map, writing `FileName+".tmp"`, then `os.Rename`, with permissions 0o644.
- [ ] **Step 4: Run them.** Run `go test ./pkg/outcomes/ && golangci-lint run ./pkg/outcomes/...`. Expected: PASS, 0 issues.
- [ ] **Step 5: Commit.** `git add pkg/outcomes && git commit -m "outcomes: per-agent latest task outcome records"`

### Task 2: Workers record outcomes

**Files:**
- Modify: `pkg/worker/standing.go`: add a `StandingDeps` field; record in the scheduled `run` closure (around line 239) and in the idle loop's `runLine` calls (around line 318)
- Modify: `cmd/worker/main.go`: construct the log; record in the consign, consign_offer, faction_pull and shopkeep closures (around lines 565–625)
- Test: `pkg/worker/standing_outcomes_test.go`

**Interfaces:**
- Consumes: `outcomes.New`, `(*outcomes.Log).Record`, the key formats from Task 1.
- Produces: `StandingDeps.Outcomes *outcomes.Log`. Nil means don't record, so play_as and tests are unchanged.

- [ ] **Step 1: Write the failing tests.** Drive `RunStanding` with the existing fake runner pattern from `standing_test.go`.

```go
func TestStandingRecordsScheduledOutcome(t *testing.T) // a scheduled task whose dispatch returns errors.New("cannot_craft: Not enough materials") -> Load(dir)["task:<id>"].Status=="failed", Reason contains "cannot_craft"
func TestStandingRecordsIdleLineOutcome(t *testing.T)  // idle line "ensure_home" succeeding -> Load(dir)["idle:ensure_home"].Status=="ok"
func TestStandingNilOutcomesIsNoop(t *testing.T)       // Outcomes nil -> no file written, no panic
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/worker/ -run TestStanding.*Outcome`. Expected: FAIL, unknown field Outcomes.
- [ ] **Step 3: Implement.** In `run`, change `_ = deps.runLine(ctx, t.Command)` to capture the error and record `fmt.Sprintf("task:%d", t.ID)`. Idle lines record `"idle:"+line`. In `cmd/worker/main.go`:
  - create `outLog := outcomes.New(filepath.Join("data","agents",*agentID))` and set `deps.Outcomes`
  - in each behaviour closure, call `outLog.Record("behaviour:<name>", "<name>", err, time.Now())` with that closure's error

  For consign, record `cerr`. A pass that sent gifts and had none refused is "ok".
- [ ] **Step 4: Run tests.** Run `go build ./... && go test ./pkg/worker/ ./cmd/worker/ && golangci-lint run ./pkg/worker/... ./cmd/worker/...`. Expected: PASS, 0 issues.
- [ ] **Step 5: Commit.** `git add pkg/worker/standing.go pkg/worker/standing_outcomes_test.go cmd/worker/main.go && git commit -m "worker: record each task, idle line and behaviour outcome"`

### Task 3: `capture_craft_queue` verb

**Files:**
- Create: `pkg/worker/capture_craft_queue.go`, `pkg/worker/capture_craft_queue_test.go`
- Modify: `pkg/worker/dispatch.go` (the supported set and dispatch case), `data/overmind/roles.yaml` (the `craftsman` and `resident` roles' schedules)

**Interfaces:**
- Consumes: `d.Client.RawCommand(ctx, "craft", map[string]any{"action": "queue"})` and `d.Client.GetRawJSON(...)`, the same call `craft_node.go` makes; `serverapi.CraftQueueListing` and `CraftJobEntry`.
- Produces:
  - file `data/agents/<id>/craft-queue.json`: `{"captured_at": RFC3339, "jobs": [{"recipe","runs_remaining","eta_ticks","position","facility_id","base_id","status"}]}`
  - `const CraftQueueFile = "craft-queue.json"`
  - `func CaptureCraftQueue(ctx context.Context, client game.GameClient, agentDir string, now time.Time) (int, error)`: returns the number of jobs written
  - verb `capture_craft_queue`

- [ ] **Step 1: Write the failing tests.**

```go
func TestCaptureCraftQueueWritesJobs(t *testing.T)      // fake returns {"action":"queue","jobs":[{"job_id":"j1","recipe":"grow_sunspindle","runs_remaining":1,"eta_ticks":72,"position":2,"facility_id":"","base_id":"c976fa7df53c87f9616165d7d411b41a","status":"queued"}]} -> file has 1 job with eta_ticks 72; return 1
func TestCaptureCraftQueueEmptyListingWritesEmpty(t *testing.T) // {"action":"queue","jobs":[]} -> file with jobs [] and captured_at set (an empty queue is a real observation)
func TestCaptureCraftQueueForeignReplyLeavesFile(t *testing.T)  // reply without "action" -> error, previous file untouched
func TestCaptureCraftQueueIsSupported(t *testing.T)     // supported["capture_craft_queue"]
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/worker/ -run CraftQueue`. Expected: FAIL, undefined.
- [ ] **Step 3: Implement.** Write atomically, temp file then rename. Add the dispatch case, using the agent dir as `announce` does. In `roles.yaml`, add `{ every: hourly, command: "capture_craft_queue" }` to the `craftsman` and `resident` roles, with a dated comment that it feeds the line dashboard's queue backlog.
- [ ] **Step 4: Run tests.** Run `go build ./... && go test ./pkg/worker/ && golangci-lint run ./pkg/worker/...`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/worker/capture_craft_queue.go pkg/worker/capture_craft_queue_test.go pkg/worker/dispatch.go data/overmind/roles.yaml && git commit -m "worker: capture_craft_queue for the line dashboard backlog"`

### Task 4: Dashboard reference data (stations, recipes, items, facilities, upkeep)

**Files:**
- Create: `pkg/linedash/refdata.go`, `pkg/linedash/refdata_test.go`; fixtures in `pkg/linedash/testdata/` (a tiny sqlite built by the test, plus `catalog_facilities.json`)

**Interfaces:**
- Produces:
  - `type Station struct { BaseID, POIID, Name, SystemID, SystemName, Condition, Owner string; SatisfactionPct int }`. `Owner` is the faction tag when `faction_bases` lists the base for our faction (columns `faction_id, base_id`); otherwise it's empty.
  - `type Recipe struct { ID, Name string; CraftingTime int; Inputs, Outputs map[string]int }`
  - `type Facility struct { BaseID, Type string; Level int; RecipeID, Status string; CapturedAt time.Time }`
  - `type RefData struct { Stations map[string]Station; Recipes map[string]Recipe; ItemCategory map[string]string; ItemName map[string]string; Facilities map[string][]Facility; Upkeep map[string]map[string]int }`
    - `Stations` is keyed by EVERY alias, base id and POI id, both pointing at the same `Station`
    - `Facilities` is keyed by base id
    - `Upkeep` maps facility type to item to quantity per cycle
  - `func LoadRefData(ctx context.Context, kbPath, facilityCatalogPath, factionID string) (RefData, error)`
  - `func (r RefData) Station(id string) (Station, bool)`
- Live column names, read 2026-11-01:
  - `bases(id, poi_id, name, condition, satisfaction_pct)`
  - `pois(id, system_id, base_id)`
  - `systems(id, name)`
  - `recipes(id, name, crafting_time)`
  - `recipe_inputs(recipe_id, item_id, quantity)`
  - `recipe_outputs(recipe_id, item_id, quantity)`
  - `items(id, name, category)`
  - `item_modules(item_id, type)`: every item with an empty `items.category` (248 on 2026-11-01) is a ship module listed here
  - `faction_facilities(faction_id, base_id, facility_type, level, status, recipe_id, captured_utc)`
  - catalog: `catalog_facilities.json` entries with `id` and `maintenance_inputs: [{item_id, quantity}]`

- [ ] **Step 1: Write the failing tests.**

```go
func TestStationIndexResolvesPOIAndBaseToOneStation(t *testing.T) // bases row (voss_redoubt_station, poi voss_redoubt) -> Station("voss_redoubt").BaseID == Station("voss_redoubt_station").BaseID == "voss_redoubt_station"
func TestLoadRefDataRecipesAndCategories(t *testing.T)            // recipe grow_sunspindle inputs {purified_water:300, liquid_nitrogen:30} outputs {sunspindle:200}; ItemCategory["steel_plate"] read from items.category
func TestLoadRefDataModulesCategorisedAsModule(t *testing.T)       // item with empty items.category and an item_modules row -> ItemCategory == "module"
func TestLoadRefDataFacilitiesForOurFactionOnly(t *testing.T)     // two factions' rows -> only factionID's kept, keyed by base_id
func TestLoadRefDataUpkeep(t *testing.T)                          // atmospheric_recycler -> {sunspindle:1, water_ice:3, purified_water:1, oxygen_gas:2, catalytic_scrubber_cartridge:1}
func TestLoadRefDataMissingKBErrors(t *testing.T)
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/linedash/ -run RefData\|StationIndex`. Expected: FAIL, undefined.
- [ ] **Step 3: Implement.** Use the existing `openReadOnly`. Find CRFT's faction id from `faction_profile` in assets.db, or pass it as a flag (Task 10); the main resolves it.
- [ ] **Step 4: Run tests.** Run `go test ./pkg/linedash/ && golangci-lint run ./pkg/linedash/...`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/linedash/refdata.go pkg/linedash/refdata_test.go pkg/linedash/testdata && git commit -m "linedash: station index, recipes, categories, facilities, upkeep"`

### Task 5: Schedule reader and task parser

**Files:**
- Create: `pkg/linedash/schedules.go`, `pkg/linedash/schedules_test.go`

**Interfaces:**
- Produces:
  - `type Task struct { ID int; Frequency, Command string }`
  - `func LoadSchedules(agentsDir string) (map[string][]Task, error)`: partial on error, like `LoadRoster`
  - `type TaskSpec struct { Verb, Recipe, Item, From, To, Recipient string; Qty int; MaxPrice float64 }`
  - `func ParseTask(cmd string) (TaskSpec, bool)`: `ok` is false for any verb other than these four:
    - `craft <recipe> <qty> [preset]`
    - `deliver <item> <qty> <from> <to> <recipient>`
    - `faction_deposit <item>`
    - `buy_directed <item> <qty> <station> <max_price> <recipient>`

  These argument orders were read from live schedules: trader-10 ids 10 and 11, trader-2 id 22, miningbot_0000 ids 33–37.

- [ ] **Step 1: Write the failing tests.**

```go
func TestParseTaskCraft(t *testing.T)          // "craft carbon_arc_circuit_etching 1000 prefer_own" -> Verb craft, Recipe carbon_arc_circuit_etching, Qty 1000
func TestParseTaskDeliver(t *testing.T)        // "deliver oxygen_gas 1900 gold_run_extraction_hub 862e07fa76571d0a76cab3cbaf4046e7 miningbot_0000" -> Item, Qty, From, To, Recipient
func TestParseTaskBuyDirected(t *testing.T)    // "buy_directed pharmaceutical_culture 850 sirius_observatory_station 90 self" -> MaxPrice 90, From sirius_observatory_station
func TestParseTaskFactionDeposit(t *testing.T) // "faction_deposit oxygen_gas" -> Item oxygen_gas
func TestParseTaskOtherVerbs(t *testing.T)     // "capture_storage", "craft" (no recipe) -> ok false
func TestLoadSchedulesSkipsCorruptKeepsOthers(t *testing.T) // one corrupt file -> other agents present, err non-nil
func TestLoadSchedulesAcceptsWrappedShape(t *testing.T)     // {"tasks":[...]} also parses
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/linedash/ -run ParseTask\|LoadSchedules`. Expected: FAIL.
- [ ] **Step 3: Implement.** Use `strings.Fields`. The trailing preset on `craft` is ignored.
- [ ] **Step 4: Run tests.** Run `go test ./pkg/linedash/`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/linedash/schedules.go pkg/linedash/schedules_test.go && git commit -m "linedash: read schedules and parse line-relevant tasks"`

### Task 6: Derive lines

**Files:**
- Create: `pkg/linedash/derive.go`, `pkg/linedash/derive_test.go`
- Modify: `pkg/linedash/config.go`: add fields to `Line` and `Node` below; the yaml tags stay

**Interfaces:**
- Consumes: `RefData`, `Station()` (Task 4); `Task`, `ParseTask` (Task 5); `RosterEntry` (`roster.go`); `forbiddenItems`.
- Produces:
  - new `Line` fields:
    - `StationID, StationName, SystemName, Category string`
    - `Kind string` (`"hand"`, `"facility"` or `"sales"`)
    - `Purpose string` (`"upkeep"`, `"for_sale"` or `"feeds"`)
    - `DependedOn Deps`
    - `TaskKeys map[string][]string` (agent → outcome keys of the tasks that make this line; Task 7)
  - `type Deps struct { Production, Upkeep, Sales int }`
  - new `Node` field: `SharedBy int`, the number of lines that draw on this tank (2+ = shared)
  - `type Listing struct { StationID, Item, Agent string }`
  - `type DeriveInputs struct { Roster map[string]RosterEntry; Schedules map[string][]Task; Ref RefData; Listings []Listing; FactionTag string; DeferFleets []string }`
  - `func Derive(in DeriveInputs) []Line`
  - Line ids:
    - production: `"<baseID>:<recipe>"`
    - sales: `"<baseID>:sell:<item>"`

**Rules, from the spec:**
1. Only roster agents with a non-empty `Station` that isn't `Removed` and whose fleet isn't in `DeferFleets` make lines. Resolve each station through `Ref.Station`; skip unresolved ones with a warning returned by the caller (Task 10).
2. Each distinct (base, craft recipe) is one production line. The **produce** node:
   - holds the recipe, `UnitsPerRun`/`InputsPerRun` from `Ref.Recipes`, the crafting agents, and a faction holder at that base
   - Kind is `facility` if any `Ref.Facilities[base]` has `RecipeID == recipe`, else `hand`
3. Each recipe input becomes a **store** node at that base, held by the faction (holder `{Faction: FactionTag, Station: base}`), linked by a craft edge to the produce node. The output becomes a store node with a craft edge from produce.
4. A `deliver` task whose `To` resolves to the base and whose `Item` is a line's input adds a **purchase** or **store** source node at `From`. It's linked by a haul edge with `Agents: [agent]`. `buy_directed` makes that source node a purchase node carrying `MaxPrice`; add `MaxPrice float64` to `Node`.
5. A `faction_deposit <item>` task is not a node. It adds the agent to the matching input store's agents.
6. For each `Listing` whose item isn't produced at that base, there's one sales line: a store node plus a sell node with the listing's agent.
7. **Purpose:**
   - `upkeep` if the output item is in `Ref.Upkeep` of any facility type present at that base
   - else `for_sale` if any sales or sell node at the base sells it
   - else `feeds`

   **Category:**
   - `"Station upkeep"` for upkeep lines
   - `"Market/Sales"` for sales lines
   - otherwise `Ref.ItemCategory[output]`, title-cased. Ship modules come through as `"Module"` (Task 4); an item still without a category becomes `"Other"`
8. **SharedBy:** for every (base, item) input store, the number of lines at that base that consume it. **DependedOn** on a producing line counts the consumer lines of its output item at the same base, split by the consumer's Purpose: `feeds` counts as Production, `upkeep` as Upkeep, `for_sale` as Sales.
9. Lines and nodes never contain `forbiddenItems`. Drop the task and keep going.

- [ ] **Step 1: Write the failing tests.** Build `DeriveInputs` in Go with a 2-station fixture: Conjunction Junction with a refinery and a recycler, and Voss with a still.

```go
func TestDeriveFacilityVsHand(t *testing.T)       // refine_steel at CJ (Iron Refinery recipe_id refine_steel) -> Kind "facility"; grow_sunspindle (no facility) -> Kind "hand"
func TestDeriveUpkeepPurposeAndCategory(t *testing.T) // grow_sunspindle at CJ (recycler upkeep includes sunspindle) -> Purpose "upkeep", Category "Station upkeep"
func TestDeriveSharedInputCounted(t *testing.T)   // two recipes at CJ both consuming steel_plate -> the steel_plate store node SharedBy==2 in both lines
func TestDeriveDependedOn(t *testing.T)           // refine_steel line DependedOn == Deps{Production:2}
func TestDeriveDeliverAddsHaulFromPurchase(t *testing.T) // buy_directed + deliver culture to voss -> purchase node MaxPrice 90, haul edge agents [trader-10]
func TestDeriveSalesLineForUnproducedListing(t *testing.T) // Listing{CJ, void_dust, miningbot_0000} with no void_dust recipe at CJ -> line id "c976...:sell:void_dust", Category "Market/Sales", Kind "sales"
func TestDeriveSkipsUnpinnedRemovedAndDeferred(t *testing.T) // station "" agent, removed agent, mb-fleet agent -> no lines from them
func TestDeriveMergesAliasedStation(t *testing.T) // one agent pinned "voss_redoubt", another "voss_redoubt_station" -> both lines StationID "voss_redoubt_station"
func TestDeriveDropsForbiddenItems(t *testing.T)  // a craft whose output is energy_crystal -> no line, no panic
func TestDeriveStableOrder(t *testing.T)          // same inputs twice -> identical slice (sorted by StationName, Category, Name)
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/linedash/ -run Derive`. Expected: FAIL, undefined.
- [ ] **Step 3: Implement `Derive`.** Every derived `Line` must pass the existing `Line.validate()`. Add a test helper asserting that, then call `validate()` in each test.
- [ ] **Step 4: Run tests.** Run `go test ./pkg/linedash/ && golangci-lint run ./pkg/linedash/...`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/linedash/derive.go pkg/linedash/derive_test.go pkg/linedash/config.go && git commit -m "linedash: derive lines from schedules, fleets and the knowledge DB"`

### Task 7: The why, conformance and queue backlog

**Files:**
- Modify: `pkg/linedash/eval.go`: `Inputs`, `NodeReport`, `LineReport`, new alerts, and the why computation
- Create: `pkg/linedash/why.go`, `pkg/linedash/why_test.go`

**Interfaces:**
- Consumes: `outcomes.Outcome` (Task 1); the `craft-queue.json` shape (Task 3); `Line.TaskKeys`, `Node.SharedBy`, `Node.MaxPrice` (Task 6).
- Produces:
  - `Inputs.Outcomes map[string]map[string]outcomes.Outcome` (agent → key → outcome)
  - `Inputs.Queues map[string]CraftQueue`
  - `type CraftQueue struct { CapturedAt time.Time; Jobs []QueuedJob }`
  - `type QueuedJob struct { Recipe string; RunsRemaining, ETATicks, Position int; BaseID string }`
  - `func LoadCraftQueue(agentDir string) (CraftQueue, error)`
  - `NodeReport.Why string`; `LineReport.Why string`
  - the derived Line fields copied into `LineReport`: `StationID`, `StationName`, `SystemName`, `Category`, `Kind`, `Purpose`, `DependedOn`, and `Agents int` (= `len(AgentsOf(line))`). The station's `Owner`, `Condition` and `SatisfactionPct` are copied too, for the board heading.
  - `NodeReport.SharedBy int`, copied from `Node.SharedBy`; Task 9 draws it.
  - alert `AlertNotConforming Alert = "agent not doing its assignment"`

**Rules:**
- **A node's Why:**
  - Take the newest **failed** outcome among its agents' `TaskKeys` that is newer than every **ok** outcome for the same key.
  - Render it as `"<agent>: <reason>"`.
  - Keys not in the current `TaskKeys` are ignored.
  - A node that is starved or choked with no such outcome gets `"cause unknown: <first alert>"`.
  - A produce node whose input store is empty and has `SharedBy >= 2` gets `"<item> 0 (shared with <N> lines)"`.
- **The line's Why** is the Why of the first node, in node order, with the line's worst state.
- **Conformance:** an agent on a line is non-conforming when either:
  - every outcome among its line `TaskKeys` within `CraftWindow` is failed
  - its live record is away from its pinned station and it isn't in transit

  The node gets `AlertNotConforming`.
- **Queue:** a produce node adds a Detail line `"queue: <runs> runs, ETA <ticks> ticks"` from its agents' queued jobs for its recipe. The queue's `CapturedAt` is a freshness source under the existing staleness rule, so an old queue greys the node.

- [ ] **Step 1: Write the failing tests.**

```go
func TestWhyNamesLatestFailure(t *testing.T)            // trader-10 "task:10" failed "buy_directed: price 70 exceeds ceiling 50 (replan)" -> purchase node Why "trader-10: buy_directed: price 70 exceeds ceiling 50 (replan)"
func TestWhyClearsAfterLaterSuccess(t *testing.T)       // failed at t0, ok at t1>t0 -> Why ""
func TestWhyIgnoresOutcomeForRemovedTask(t *testing.T)  // failed "task:99" not in TaskKeys -> Why ""
func TestWhyCauseUnknownWhenStarvedWithoutRecord(t *testing.T) // starved node, no outcomes -> Why "cause unknown: output stalled waiting on inputs"
func TestWhySharedTankEmpty(t *testing.T)               // steel_plate store 0, SharedBy 3 -> consumer Why "steel_plate 0 (shared with 3 lines)"
func TestNotConformingAllFailed(t *testing.T)           // craftsman-8 every moonshine outcome failed in window -> AlertNotConforming on its node
func TestQueueDetailAndStaleness(t *testing.T)          // queue 1 job grow_sunspindle 72 ticks -> Detail contains "queue: 1 runs, ETA 72 ticks"; CapturedAt 3h old -> node Stale
func TestLineReportCarriesDerivedFields(t *testing.T)   // Evaluate copies StationName, Category, Kind, Purpose, DependedOn; Agents == len(AgentsOf)
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/linedash/ -run Why\|Conforming\|Queue\|Carries`. Expected: FAIL.
- [ ] **Step 3: Implement** in `why.go`. Call it from `Evaluate` after node states are set. Existing eval tests must stay green.
- [ ] **Step 4: Run tests.** Run `go test ./pkg/linedash/ && golangci-lint run ./pkg/linedash/...`. Expected: PASS, including all earlier eval tests.
- [ ] **Step 5: Commit.** `git add pkg/linedash/why.go pkg/linedash/why_test.go pkg/linedash/eval.go && git commit -m "linedash: name the broken rule, flag non-conforming agents, show queue backlog"`

### Task 8: Station-grouped board

**Files:**
- Modify: `pkg/linedash/render.go` (`RenderBoard`, `boardRow`, `boardData`), `pkg/linedash/page.gohtml` (the board template), `pkg/linedash/render_test.go`

**Interfaces:**
- Consumes: the `LineReport` fields from Task 7.
- Produces:
  - `RenderBoard(reports []LineReport, warnings []string, refreshSec int) ([]byte, error)`. The signature gains `warnings`, which appear under the header: override and cadence warnings from Task 10.
  - A "Lines | Fleet" switch where Fleet is a disabled placeholder.
  - Groups:
    - stations in name order
    - within a station, categories in this order: `Station upkeep`, `Refined`, `Component`, `Contraband`, then other categories alphabetically, then `Market/Sales` last. The live `items.category` values are `refined`, `component`, `consumable`, `ore`, `ammo`, `misc`, `contraband`, `drone`, `material` and `fuel`. Ship modules, which have no `items.category`, show as `Module`.
    - rows sorted by state, worst first, then name
  - Station heading: `<name>`, `<system>`, `<owner>` when set, `<condition> <pct>%`
  - Columns:
    - state dot + name, linking to `/line/<id>`
    - kind tags: `hand`/`facility`/`sales` and `upkeep`/`for sale`/`feeds lines`, each a distinct class
    - depended on, as `"N production · N upkeep · N sales"` with zero parts omitted
    - why, with the rule text in the choked colour
    - agents
    - /day

- [ ] **Step 1: Write the failing tests.**

```go
func TestBoardGroupsByStationThenCategory(t *testing.T) // two stations, three categories -> station headings in name order; "Station upkeep" sub-row before "Refined"; "Market/Sales" last
func TestBoardRowShowsKindTagsDepsAgentsWhy(t *testing.T) // row contains "facility", "feeds lines", "2 production", ">10<" (agents), the Why text
func TestBoardHandAndFacilityTagsDiffer(t *testing.T)   // class "tag-hand" and "tag-facility" both present for mixed reports
func TestBoardShowsWarnings(t *testing.T)               // warnings ["override names unknown line x"] rendered
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/linedash/ -run Board`. Expected: FAIL.
- [ ] **Step 3: Implement.** Template sizes follow the 2026-11-01 doubling: base 28px, tags 22px. Update `cmd/tools/line-dashboard/main.go` call sites to pass `nil` warnings until Task 10.
- [ ] **Step 4: Run tests.** Run `go build ./... && go test ./pkg/linedash/ ./cmd/tools/line-dashboard/`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/linedash/render.go pkg/linedash/page.gohtml pkg/linedash/render_test.go cmd/tools/line-dashboard/main.go && git commit -m "linedash: station-grouped board with kind, deps, why and agents"`

### Task 9: Shared tanks in the line diagram

**Files:**
- Modify: `pkg/linedash/render.go` (`nodeView`), `pkg/linedash/page.gohtml` (the line SVG), `pkg/linedash/layout.go` (`nodeBody`), `pkg/linedash/render_test.go`

**Interfaces:**
- Consumes: `NodeReport.SharedBy` (Task 7).
- Produces:
  - A node with `SharedBy >= 2` renders with class `shared`, which gives a dashed outline (`stroke-dasharray: 7 5`) **and** a 45° two-tone stripe fill (an SVG `<pattern>` with `patternTransform="rotate(45)"`, defined once per page).
  - Its body gains the line `"shared ×N"`.
  - A node with `SharedBy < 2` is drawn as before.

- [ ] **Step 1: Write the failing tests.**

```go
func TestLineSharedTankDashedAndStriped(t *testing.T) // SharedBy 4 -> page contains class="node shared", "stroke-dasharray", `fill="url(#shared-stripes)"`, "shared ×4"
func TestLineDedicatedTankPlain(t *testing.T)         // SharedBy 1 -> no "shared ×", node has no shared class
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/linedash/ -run SharedTank\|DedicatedTank`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests.** Run `go test ./pkg/linedash/`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/linedash/render.go pkg/linedash/page.gohtml pkg/linedash/layout.go pkg/linedash/render_test.go && git commit -m "linedash: mark shared tanks (dashed + striped, shared ×N)"`

### Task 10: Wire derivation into the server; overrides file; retire lines.yaml

**Files:**
- Modify: `cmd/tools/line-dashboard/main.go`, `cmd/tools/line-dashboard/main_test.go`
- Modify: `pkg/linedash/config.go`. Replace `LoadLines`/`Config.Lines` with `LoadOverrides`; `Thresholds` and `withDefaults` stay. Keep `Line.validate()` for derived lines.
- Create: `data/overmind/line-overrides.yaml` (thresholds copied from today's `lines.yaml` block; empty `lines:`)
- Delete: `data/overmind/lines.yaml`. Move the moonshine fixture under `pkg/linedash/testdata/` if tests still need it.
- Modify: `pkg/linedash/market.go`: add `func (m *Market) OurListings(ctx context.Context) ([]Listing, error)`, the latest-captured `side='sell' AND my_quantity > 0` rows per (station_id, item_id). Its agent is the roster agent pinned to that station, or the faction tag when none is pinned.

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `type Overrides struct { Thresholds Thresholds; Lines map[string]LineOverride }`
  - `type LineOverride struct { Name, Category string; Hidden bool }`, decoded with `KnownFields(true)`
  - `func LoadOverrides(path string) (Overrides, error)`
  - `func ApplyOverrides(lines []Line, o Overrides) ([]Line, []string)`: returns the warnings, for example `"override names unknown line <id>"`
  - `func CadenceWarnings(lines []Line, sched map[string][]Task) []string`: one warning per line agent whose schedule lacks hourly-or-finer `capture_action_log` or `capture_storage`, as `"<agent>: needs hourly capture_action_log"`
- New flags:
  - `--overrides data/overmind/line-overrides.yaml`, replacing `--lines`
  - `--kb-db data/spacemolt-knowledge.db`
  - `--facility-catalog data/game-api/latest/catalog_facilities.json`
  - `--faction CRFT`
  - `--defer-fleets mb`
- The build loop:
  1. `LoadRefData`
  2. `LoadSchedules`
  3. `OurListings`
  4. `Derive`
  5. `ApplyOverrides`
  6. load outcomes and queues for each line's agents via `outcomes.Load` and `LoadCraftQueue`
  7. `Evaluate`
  8. `Layout`

  Warnings go to `RenderBoard`. If ref data fails to load, keep the last good derived set, as the old `lastCfg` did, and say so in warnings.

- [ ] **Step 1: Write the failing tests.**

```go
func TestOverridesRenameAndHide(t *testing.T)          // override {name:"Steel", hidden:false} and {hidden:true} on another -> renamed, hidden dropped
func TestOverridesUnknownLineWarns(t *testing.T)       // override for "nope" -> warning "override names unknown line nope"
func TestOverridesUnknownKeyErrors(t *testing.T)       // "nmae:" -> load error
func TestCadenceWarningsFlagsDailyCapture(t *testing.T) // agent with daily capture_storage -> warning
func TestOurListingsLatestCaptureOnly(t *testing.T)    // two captures; only the newest my_quantity>0 rows returned
func TestServerBuildsFromDerivation(t *testing.T)      // main_test: temp KB + schedules + fleet yaml -> /api/lines.json lists the derived line id "<base>:<recipe>"
```

- [ ] **Step 2: Run them.** Run `go test ./pkg/linedash/ ./cmd/tools/line-dashboard/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Update the line-dashboard memory and runbook text in `docs/COLD_START.md`: new flags, `lines.yaml` gone, `line-overrides.yaml` present.
- [ ] **Step 4: Run tests.** Run `go build ./... && go test ./... && golangci-lint run ./...`. Expected: PASS, 0 new issues.
- [ ] **Step 5: Commit.**

  ```
  git add cmd/tools/line-dashboard pkg/linedash data/overmind/line-overrides.yaml docs/COLD_START.md
  git rm data/overmind/lines.yaml
  git commit -m "line-dashboard: derive every line; overrides replace lines.yaml"
  ```

### Task 11: Capture cadence and rollout. STOP for the operator before the roll.

**Files:**
- Modify: `data/overmind/roles.yaml`. In the `craftsman` and `resident` roles, raise `capture_action_log` and `capture_storage` to hourly where they are daily or twice_daily, with a dated comment saying the line dashboard greys a line whose sources are older than 2 hours.
- Modify: individual `data/agents/<id>/schedule.json` files that `CadenceWarnings` still names once the dashboard is running. Seeding only adds, so the finer task is added and `RetireCovered` drops the coarser one on the next start.

- [ ] **Step 1: Run the cadence report.** Run `go build -o bin/line-dashboard ./cmd/tools/line-dashboard && go build -o bin/worker ./cmd/worker`. Start the dashboard on a spare port, `--addr :8094`, and read its warnings. Expected: a list of agents needing hourly captures.
- [ ] **Step 2: Edit roles.yaml and the named schedules.** The scheduler reloads schedule.json on mtime, so running workers pick these up without a restart.
- [ ] **Step 3: STOP. Ask the operator for the go-ahead to roll the workers onto the new `bin/worker`,** which records outcomes and captures the craft queue. Say which fleets will roll, and in what order.
- [ ] **Step 4: After a yes, do the staged roll one fleet at a time.**
  1. TERM the overmind.
  2. Wait for its socket to show zero workers.
  3. Relaunch it with the argv captured beforehand.

  Never restart a disconnected worker. Then restart the dashboard on :8093 with the new flags, using `setsid nohup … >> data/overmind/line-dashboard.log 2>&1 < /dev/null &`.
- [ ] **Step 5: Verify.** After an hour, `/api/lines.json` should list CJ lines carrying a `Why` and fresh sources, and the landing page should show both stations. Commit `roles.yaml` and the touched schedule.json files **by path**.
