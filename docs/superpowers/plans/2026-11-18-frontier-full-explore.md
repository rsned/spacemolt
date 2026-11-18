# Frontier Full Explore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eight explorers, each with a pathfinder drive and two Survey Scanner IIs, fully explore every frontier system (survey at 90 power, every POI visited, relics interacted with), coordinated by a shared ledger.

**Architecture:** The per-system explore logic moves out of `cmd/tools/play_as` into a shared package `pkg/explore`, so `play_as` and the worker run one implementation; relic, hazard-entry, hull-floor and repair-kit behaviour are added there. A ledger in `pkg/sweep` (same file-lock pattern as the void run) holds per-system status and cluster claims. A new worker role `frontier_explore` chains the existing pathfinder staging/launch/arrival code with the shared explore and the ledger. Two small tools generate the ledger and plan/apply the fits.

**Tech Stack:** Go 1.24+, SQLite KB (`pkg/knowledge`), existing `pkg/sweep`, `pkg/worker`, `pkg/game`.

**Spec:** `docs/superpowers/specs/2026-11-18-frontier-full-explore-design.md`

## Global Constraints

- Any sleep or pause uses a `Sleep*` constant from `pkg/game/constants.go`; in `pkg/worker` use the `settle(ctx, d)` seam, never `time.Sleep`.
- Binaries build into `bin/`.
- Check real server struct field names in `pkg/game/serverapi` before coding against them.
- `go build ./...`, `go test ./...` and `golangci-lint run` on touched packages before every commit; no new lint findings.
- Go 1.24 idioms: range over integers, `b.Loop()`.
- Commit by explicit path. Never `git add -A` (dirty `data/*.json`).
- Never edit an applied KB migration; add a new one.
- Never route through a stronghold system without the pirate unlock.
- Do not log in as a live fleet agent; release it via the fleet overrides first.
- Spec values, verbatim: survey power **≥ 90**; hull floor **50%**; **10** repair kits per ship; kit = **50** hull (`repairKitHull`); wedges **45°** centred on E 0°, NE 45°, N 90°, NW 135°, W 180°, SW 225°, S 270°, SE 315° (CCW from +x); roster explorers **1, 2, 5, 6, 8, 9, 11, 12**; ledger at `data/sweep/frontier-run.json`; state at `data/agents/<id>/frontier_state.json`.

## Review Focus

1. **A ship restarts mid-cluster.** Expected: it resumes the same cluster from its state file and does not re-claim or re-tour finished systems. (Task 7 test.)
2. **`survey_system` reports power below 90 mid-run** (a scanner went offline). Expected: the system is not marked done; the ship stops and says why. (Task 7 test.)
3. **A relic's `get_poi` carries no `interaction` block at all.** Expected: recorded as `no_prompt`, `interact_poi` still tried once for type `relic`, error text kept. (Task 3 test.)
4. **Two ships read the ledger at the same moment and want the same cluster.** Expected: exactly one claim wins under the file lock; the other gets the next target. (Task 5 test.)
5. **Hull at or below 50% with zero kits and no station in the cluster.** Expected: the ship leaves the cluster for the nearest usable station instead of skipping every remaining POI in a loop. (Task 7 test.)

---

### Task 1: KB table `relic_interactions`

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` (append migration **77**)
- Create: `pkg/knowledge/relic_interactions.go`, `pkg/knowledge/relic_interactions_test.go`
- Modify: `pkg/knowledge/base.go`, `pkg/knowledge/memory.go` (interface + in-memory impl)

**Interfaces:**
- Produces:
  ```go
  type RelicInteraction struct {
      POIID, SystemID, AgentID string
      Tick        int64
      Outcome     string // "data_copied" | "items" | "nothing" | "refused" | "mission_offered_skipped" | "no_prompt"
      Prompt      string // get_poi interaction.message
      Message     string // interact_poi message
      DataCopied  bool
      Items       map[string]int
      MissionJSON string
      Error       string
      ObservedUTC time.Time
  }
  // on knowledge.Base:
  RecordRelicInteraction(ctx context.Context, r RelicInteraction) error
  RelicInteractions(ctx context.Context) ([]RelicInteraction, error)
  ```
  `Responded()` on `RelicInteraction` is true for `data_copied` and `items`.

- [ ] **Step 1:** Test `TestRelicInteractionsRoundTrip` (SQLite via the package's existing temp-DB helper, and MemoryKB): record two rows for one POI from different agents, read back both with `Items` and `MissionJSON` intact; `Responded()` true only for `data_copied`/`items`.
- [ ] **Step 2:** Run `go test ./pkg/knowledge -run RelicInteractions`; expect compile failure.
- [ ] **Step 3:** Add migration 77: table `relic_interactions(poi_id, system_id, agent_id, tick, outcome, prompt, message, data_copied, items_json, mission_json, error, observed_utc)`, primary key `(poi_id, agent_id, tick)`, index on `outcome`. Implement both stores.
- [ ] **Step 4:** Run the test and `go test ./pkg/knowledge`; expect PASS.
- [ ] **Step 5:** Commit `pkg/knowledge/...` — "kb: relic_interactions table (migration 77)".

### Task 2: Shared per-system explore in `pkg/explore`

Move, do not rewrite. `play_as` output and behaviour stay byte-for-byte where its tests pin them.

**Files:**
- Create: `pkg/explore/explore.go`, `survey.go`, `wildlife.go`, `route.go`, `reporter.go`, plus tests moved alongside
- Modify: `cmd/tools/play_as/explore.go`, `kb_update.go` (thin wrappers), `cmd/tools/play_as/auto_explore.go` (call site only)

**Interfaces:**
- Produces:
  ```go
  type Reporter interface {
      Printf(format string, args ...any) // human progress lines
      Raw(label string, payload []byte)  // raw replies for json/raw formats
  }
  type Deps struct {
      Client  game.GameClient
      KB      knowledge.Base
      Market  *market.Collector // nil skips market.db
      AgentID string
      Report  Reporter          // nil -> discard
  }
  type Options struct {
      RefuelAtStations bool
      StopOnUnscanned  bool    // play_as only
      EnterHazards     bool    // Task 4
      HullFloor        float64 // Task 4; 0 disables
      Relics           bool    // Task 3
  }
  type POIResult struct {
      ID, Name, Type string
      Visited        bool
      Skipped        string // reason, "" when visited
  }
  type Result struct {
      SystemID    string
      SurveyPower int
      Revealed    int
      POIs        []POIResult
      Relics      int
      Stopped     string // "" | "fuel" | "battle" | "unscanned" | "hull"
  }
  func System(ctx context.Context, d Deps, o Options) (Result, error)
  func PlanRoute(pois []game.POI, startPOI string) []game.POI
  ```
- Consumes: `worker.KBUpdatePOI`, `worker.SurveyPOIsToKB` (already exported).

- [ ] **Step 1:** Move `exploreSystem`, `surveySystem`, `saveSurveyPOIs`, `saveFaintSignatures`, `saveSurveyAnomaly`, the wildlife capture/second-look functions, `captureSightings`, `planExploreRoute`, `poiDistance`, `currentPOIPosition` and the survey-scanner check into `pkg/explore`, replacing `fmt.Printf`/`format` branches with `Reporter` calls and `globalKB` with `Deps.KB`. Move their tests with them.
- [ ] **Step 2:** In `play_as`, keep `explore`, `exploreSystem`, `surveySystem` as wrappers that build `Deps` with a `Reporter` reproducing today's styled/raw/json output, and map `Result.Stopped` back to today's return values.
- [ ] **Step 3:** Add test `TestSystemSurveysBeforeTouring` in `pkg/explore`: with a fake client whose survey reveals a hidden POI, the call order is `survey_system`, `get_system`, then travel to every POI including the revealed one; `Result.SurveyPower` equals the reply's `survey_power`.
- [ ] **Step 4:** Run `go test ./pkg/explore ./cmd/tools/play_as`; expect PASS with no play_as test edited except import/rename.
- [ ] **Step 5:** Commit — "explore: move per-system explore from play_as into pkg/explore".

### Task 3: Relic step

**Files:**
- Create: `pkg/explore/relic.go`, `pkg/explore/relic_test.go`
- Modify: `pkg/explore/explore.go` (call the step after the POI update when `Options.Relics`)
- Modify: `cmd/tools/play_as/explore.go` (wrappers pass `Relics: true`)

**Interfaces:**
- Consumes: `knowledge.RelicInteraction`, `Base.RecordRelicInteraction` (Task 1); `serverapi.POIInteractionInfo`, `serverapi.InteractPOIResponse`.
- Produces: `func relicStep(ctx context.Context, d Deps, p game.POI) (tried bool)`; a POI is a relic when `p.Type == "relic"` or its `get_poi` reply has a non-empty `interaction.command`.

- [ ] **Step 1:** Tests, one per outcome, each asserting the recorded `Outcome` and whether `interact_poi` was sent:
  - prompt with `mission` → `mission_offered_skipped`, **no** `interact_poi`;
  - reply `data_copied:true` → `data_copied`;
  - reply with `items_recovered` → `items`, quantities recorded;
  - reply with message only → `nothing`;
  - server error → `refused`, error text recorded;
  - type `relic` with no `interaction` block → `interact_poi` sent once, prompt empty, outcome from the reply (or `no_prompt` + error when refused);
  - a relic already recorded by this agent in this process is not tried twice.
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Implement with `client.RawCommand(ctx, "interact_poi", nil)` and the cached `get_poi` reply.
- [ ] **Step 4:** Run `go test ./pkg/explore`; expect PASS.
- [ ] **Step 5:** Commit — "explore: interact with relics once and record every reply".

### Task 4: Hazard entry, hull floor, repair kits

**Files:**
- Create: `pkg/explore/hull.go`, `pkg/explore/hull_test.go`
- Modify: `pkg/explore/explore.go`

**Interfaces:**
- Produces:
  ```go
  const KitItem = "repair_kit"
  const KitHull = 50
  // repairToFloor uses kits (client.Repair from cargo) until hull > floor or kits run out.
  func repairToFloor(ctx context.Context, d Deps, floor float64) (hullFrac float64, kitsLeft int)
  ```
  Tour rules in `System`: before travelling to each POI, read hull (`GetStatus`); if `hull/max <= HullFloor`, `repairToFloor`; still at or below → `POIResult.Skipped = "hull N%"`, log `skipped <poi>: hull N%`, continue. After the route, retry floor-skipped POIs once. After any hazard dose, use a kit while missing hull ≥ `KitHull`. With `EnterHazards`, a POI with a known hazard is travelled to, worked, and left after the first dose (`State.LastHazardDose` at that POI); without it, today's skip rule stands. If every remaining POI is floor-skipped and no kits remain, `Result.Stopped = "hull"`.

- [ ] **Step 1:** Tests:
  - `TestHullFloorSkipsAndLogs`: hull 50%, no kits → POI skipped with reason, later POIs also skipped, `Stopped == "hull"`.
  - `TestHullFloorRepairsFromKits`: hull 45% of 480, 3 kits → repairs until above 50%, POI visited, kits decremented.
  - `TestFloorSkippedPOIRetriedAtEnd`: skipped at 50%, hull restored mid-tour (kit withdrawn at a station stop) → POI visited on the retry.
  - `TestEnterHazardLeavesAfterOneDose`: hazardous POI, `EnterHazards` → travel, update, relic step run, then leaves; marked visited.
  - `TestHazardSkippedWithoutEnterHazards`: unchanged skip.
  - `TestKitUsedAfterDoseWhenMissingAKit`: hull 430/480 after dose → one kit used; 460/480 → none.
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Implement. Hull exactly 50% counts as at the floor.
- [ ] **Step 4:** Run `go test ./pkg/explore`; expect PASS.
- [ ] **Step 5:** Commit — "explore: enter hazards for one dose, 50% hull floor, repair from kits".

### Task 5: Frontier ledger

**Files:**
- Create: `pkg/sweep/frontier.go`, `pkg/sweep/frontier_test.go`

**Interfaces:**
- Produces:
  ```go
  type FrontierStatus string // "todo" | "claimed" | "done" | "incomplete"
  type FrontierSystem struct {
      ID, Name    string
      X, Y        float64
      Wedge       string   // "E","NE","N","NW","W","SW","S","SE"
      Cluster     string   // id of the lane-connected group at generation
      Status      FrontierStatus
      Claimant    string
      SurveyPower int
      POIsSeen, POIsVisited, Relics int
      Skipped     []string // POI ids
      Reason      string   // when incomplete
      UpdatedUTC  time.Time
  }
  type FrontierRun struct {
      GeneratedUTC time.Time
      Systems      map[string]*FrontierSystem
  }
  type FrontierInput struct { ID, Name string; X, Y float64; Lethal string }
  func Wedge(x, y float64) string
  func NewFrontierRun(systems []FrontierInput, lanes [][2]string, now time.Time) *FrontierRun
  func LoadFrontierRun(path string) (*FrontierRun, error)
  func SaveFrontierRun(path string, r *FrontierRun) error
  // ClaimCluster claims every todo system lane-connected to systemID (per the lanes
  // recorded in the ledger) for agent; false when any is claimed by someone else.
  func ClaimCluster(path, agent, systemID string, now time.Time) (ids []string, ok bool, err error)
  func AddSystem(path string, in FrontierInput, laneTo, agent string, now time.Time) error
  func FinishSystem(path, agent string, s FrontierSystem, now time.Time) error
  // NextTarget: nearest todo system to (x,y); own wedge first, then any.
  func NextTarget(path, wedge string, x, y float64) (*FrontierSystem, error)
  // StartTarget: todo system in wedge with the smallest distance from centre.
  func StartTarget(path, wedge string) (*FrontierSystem, error)
  func ReleaseAgent(path, agent string, now time.Time) error
  ```
  A `FrontierInput.Lethal` reason generates the system as `incomplete` with `Reason: "hostile: <text>"`. All mutators take the same `.lock` flock as `withVoidRun`.

- [ ] **Step 1:** Tests: `TestWedge` (0°→E, 22.4°→E, 22.6°→NE, 135°→NW, 337.6°→E boundary cases); `TestNewFrontierRunClusters` (lanes group systems; singletons are their own cluster); `TestClaimClusterIsExclusive` (two goroutines, one wins); `TestAddSystemJoinsClaimedCluster`; `TestNextTargetPrefersOwnWedgeThenAny`; `TestStartTargetIsNearestCentre`; `TestLethalGeneratedIncomplete`; `TestFinishSystemRejectsOtherClaimant`; `TestReleaseAgentReturnsClaimsToTodo`.
- [ ] **Step 2:** Run `go test ./pkg/sweep -run Frontier`; expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./pkg/sweep`; expect PASS.
- [ ] **Step 5:** Commit — "sweep: frontier run ledger".

### Task 6: Tools `frontier-run` and `frontier-fit`

**Files:**
- Create: `cmd/tools/frontier-run/main.go`, `cmd/tools/frontier-run/main_test.go`
- Create: `pkg/sweep/frontierfit.go`, `pkg/sweep/frontierfit_test.go`
- Create: `cmd/tools/frontier-fit/main.go`
- Modify: `scripts/build.sh` (build both into `bin/`)

**Interfaces:**
- Consumes: Task 5 ledger; `Base.RelicInteractions` (Task 1); `serverapi.GetShipResponse`.
- Produces:
  ```go
  type FitModule struct { ID, TypeID, Name, Slot string; CPU, Power int }
  type FitPlan struct {
      Remove []FitModule // in removal order
      Add    []string    // type ids to fit, e.g. two "survey_scanner_ii"
      CPUAfter, CPUCap, PowerAfter, PowerCap int
      OK     bool
      Why    string      // when !OK
  }
  // PlanFit keeps pathfinder_drive, targets two survey_scanner_ii, and removes
  // other utility modules first, then weapons, then defence, until slots and CPU fit.
  func PlanFit(fitted []FitModule, utilitySlots, cpuCap, powerCap, scannerCPU, scannerPower int) FitPlan
  ```
- `frontier-run generate` (frontier = KB systems with `position_source != 'api'`; lethal list from the KB hostile tables for Repeater Monolith and leviathan sightings), `status`, `assign` (prints a proposed ship→wedge table: Reserves to the wedges whose mean distance to the nearest refuelling station is largest), `relics` (lists `Responded()` rows, then `mission_offered_skipped`).
- `frontier-fit --agent <id>` is a dry run printing before/after fit and removals; `--apply` performs it docked at CJ: withdraw scanners and 10 kits from faction storage, `UninstallMod`, `InstallMod`, deposit removed modules, then `survey_system` once and print the power. It logs in as the agent, so it refuses to run while that agent's worker process exists.

- [ ] **Step 1:** Tests for `PlanFit`: Reserve (3 utility, CPU 28) with drive + fuel module + weapon + shield → removes all three, `CPUAfter == 28`, OK; Compendium (CPU 32) keeps a ≤ 4 CPU defence module; already-fitted ship → empty plan; drive missing → `!OK` with reason; one scanner already fitted → adds one.
- [ ] **Step 2:** Test `frontier-run generate` against a MemoryKB fixture: api systems excluded, lanes into api space not clustered, output file loads.
- [ ] **Step 3:** Run; expect FAIL. Implement both tools.
- [ ] **Step 4:** Run `go test ./pkg/sweep ./cmd/tools/frontier-run` and `./scripts/build.sh`; expect PASS and `bin/frontier-run`, `bin/frontier-fit` present.
- [ ] **Step 5:** Commit — "tools: frontier-run ledger tool and frontier-fit".

### Task 7: Worker role `frontier_explore`

**Files:**
- Create: `pkg/worker/frontier_explore.go`, `pkg/worker/frontier_state.go`, `pkg/worker/frontier_explore_test.go`
- Modify: `pkg/worker/dispatch.go` (verb `frontier_explore`, fields `FrontierWedge`, `FrontierRun`), `cmd/worker/main.go` (flags `--frontier-wedge`, `--frontier-run`, default `data/sweep/frontier-run.json`), `pkg/overmind/supervisor/config.go` + `supervisor.go` (yaml `frontier_wedge`), `data/overmind/roles.yaml` (role `frontier_explore`, idle `frontier_explore`)

**Interfaces:**
- Consumes: `explore.System` with `Options{RefuelAtStations: true, EnterHazards: true, HullFloor: 0.5, Relics: true}`; Task 5 ledger functions; existing `sweepHeadFor`/`sweepLaunch`/`sweepStageInSystem`/`sweepLocation`/`sweepFuel`/`sweepFuelledFilter`/`sweepLaneHome`, `sweep.CanContinue`, `Autopilot`.
- Produces:
  ```go
  type frontierState struct {
      Phase     string   // "verify"|"stage"|"launch"|"work"|"refuel"|"next"|"home"|"done"
      Target    string   // system being flown to
      Cluster   []string // claimed, in BFS order
      Done      []string
      Launches  int      // toward Target; 2 failed -> incomplete "unreachable"
      ResumeAt  string   // system to return to after a refuel/repair stop
      UpdatedUTC time.Time
  }
  func (d *WorkerDispatch) FrontierExplore(ctx context.Context) error // one step per call
  ```

Behaviour per phase:
- `verify`: `get_ship` shows `pathfinder_drive` and two `survey_scanner_ii`, and cargo holds ≥ 1 `repair_kit`; else stay docked and log why. First target = `sweep.StartTarget(wedge)`.
- `stage`/`launch`: lanes to the charted system nearest the target, then launch at the target's ledger position. Captured by a different frontier system → `AddSystem` if unknown and work it; charted system → relaunch.
- `work`: on arrival `ClaimCluster`; not ok → `next`. Per system: `explore.System`; record via `FinishSystem` — `done` when `SurveyPower >= 90` and no POI skipped, else `incomplete` with the reason and skipped POI ids. New frontier lanes from `get_system` → `AddSystem` and append to `Cluster`; lanes into api space are not followed. `Stopped == "battle"` → flee stance, mark `incomplete: hostile`, move on. `SurveyPower < 90` → stop, log, do not mark done.
- Before every lane jump or launch: fuel must cover the nearest usable refuelling station plus one launch (`sweep.CanContinue` with the measured launch cost); otherwise `refuel` with `ResumeAt` set. `Stopped == "hull"` → same detour, repairing and restocking kits to 10 where faction storage allows.
- `next`: `sweep.NextTarget`; none → `home`, lane/pathfinder to CJ, dock, `done`.
- A stranded ship (fuel below one launch, no reachable station) calls `DistressSignal(ctx, "tow")` once and waits.

- [ ] **Step 1:** Tests with the package's fake client and a temp ledger:
  - `TestFrontierVerifyRefusesWithoutTwoScanners`
  - `TestFrontierWorksWholeClusterAndMarksDone` (3 systems, lanes discovered on the way)
  - `TestFrontierResumesClusterAfterRestart` (state file reloaded; finished systems not re-toured)
  - `TestFrontierLowSurveyPowerStops` (power 60 → not done, phase stays `work`, log names the power)
  - `TestFrontierSecondShipLeavesClaimedCluster`
  - `TestFrontierHullStopDetoursToStation` (zero kits, hull 50%, no station in cluster → `refuel` phase with `ResumeAt`)
  - `TestFrontierFuelGateBeforeLaunch`
  - `TestFrontierUnreachableAfterTwoLaunches` → `incomplete: unreachable`
  - `TestFrontierApiLaneNotFollowed`
- [ ] **Step 2:** Run `go test ./pkg/worker -run Frontier`; expect FAIL.
- [ ] **Step 3:** Implement, then wire the verb, flags and yaml field.
- [ ] **Step 4:** Run `go test ./pkg/worker ./pkg/overmind/... ./cmd/worker`; expect PASS.
- [ ] **Step 5:** Commit — "worker: frontier_explore role".

### Task 8: Worker tour uses the shared explore

**Files:**
- Modify: `pkg/worker/explore_tour.go` (`systemTour` calls `explore.System`), `pkg/worker/explore_tour_test.go`, `pkg/worker/pathfinder_sweep_arrival.go` (`sweepTourFn`)

**Interfaces:**
- Consumes: `explore.System` with `Options{RefuelAtStations: true, Relics: true}` (`EnterHazards` false, `HullFloor` 0: the `explore` and `pathfinder_sweep` roles keep today's hazard skip).

- [ ] **Step 1:** Update `explore_tour_test.go` expectations to the shared order (survey first) and add `TestSystemTourInteractsWithRelic`.
- [ ] **Step 2:** Run; expect FAIL. Replace the body of `systemTour`; keep `reportWormholes` and `exploreStationStop` behaviour by passing them through `Deps` hooks or calling them around `explore.System`.
- [ ] **Step 3:** Run `go build ./... && go test ./... && golangci-lint run ./pkg/... ./cmd/...`; expect PASS, 0 issues.
- [ ] **Step 4:** Commit — "worker: explore and sweep tours run the shared explore".

### Task 9: Rollout (operator-gated; no code)

- [ ] **Step 1:** `bin/kb-migrate` to apply migration 77 to the live KB.
- [ ] **Step 2:** `bin/frontier-run generate`, then `status` and `assign`; show the operator the wedge table and the systems generated `incomplete: hostile`. **Wait for the operator's confirmation or edits.**
- [ ] **Step 3:** For each of explorers 1, 2, 8, 9, 11, 12 (docked at CJ): release it from the sweep fleet (`sweep-overrides.json` `removed` + SIGHUP), run `bin/frontier-fit --agent <id>` (dry run). Show all six plans to the operator. **Wait for approval before any `--apply`.**
- [ ] **Step 4:** `--apply` on one ship first. Confirm: power reads 90, 10 kits aboard, and a kit can be used undocked (undock, check `repair` from cargo is accepted when hull is below max, or note it untested if hull is full). Then the rest. Compendiums also take fuel cells from CJ faction storage as a
second reserve (`frontier-fit --apply --fuel-cells N`; propose N from stock
on hand and ask the operator).
- [ ] **Step 5:** Edit `data/overmind/sweep-fleet.yaml`: each ready ship `role: frontier_explore, station: "", frontier_wedge: <dir>`, keeping the old line as a `# was:` comment. Remove the ships from the overrides `removed` list.
- [ ] **Step 6:** `./scripts/build.sh`, back up `bin/worker` and `bin/overmind`, restart the sweep overmind (TERM, confirm 0 processes on `sweep.sock`, remove the socket, relaunch with the captured command line).
- [ ] **Step 7:** Watch the first ship through verify → launch → first system `done` in `bin/frontier-run status` before leaving the run unattended.
- [ ] **Step 8:** When explorer-5 and explorer-6 dock at CJ, repeat steps 3–5 for them and SIGHUP the sweep overmind.
