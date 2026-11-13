# Pathfinder Frontier Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ten explorer ships, each with a Pathfinder Drive, sail one concentric ring each (7,500–7,950 LY, 50 LY apart) clockwise around the galaxy. They explore every system a drift lands in, record measured coordinates, and end at the nearest empire station.

**Architecture:** There are three layers:
- **`pkg/sweep`:** pure, I/O-free logic (geometry, ring progress, fuel budget, persisted state), unit-tested in isolation.
- **The worker verb `pathfinder_sweep`** (`pkg/worker/pathfinder_sweep*.go`): runs one bounded pass per call, the way `explore` does. The role's idle loop calls it again and again. State is saved to `data/agents/<id>/sweep_state.json` after every change, so a restart, explore run or GSA tow resumes at the saved ring angle.
- **Small additions** to serverapi (decoding the drift position), knowledge (the source of each position, measured positions), galaxy (nearest station by straight-line distance) and the overmind config (a new role and fleet).

**Tech Stack:** Go 1.24+ (range over ints, `b.Loop()`), SQLite KB migrations, overmind fleet YAML, `play_as` for the hull purchases and fitting.

**Spec:** `docs/superpowers/specs/2026-11-12-pathfinder-frontier-sweep-design.md`

## Global Constraints

- Ring radii 7,500, 7,550, … 7,950 LY from (0,0); one ship per ring; direction clockwise, so the angle around the centre **decreases**.
- Re-aim every **5°** of arc. Bearing = degrees counter-clockwise from +x, in KB map coordinates. Drift speed is 10 LY/tick.
- fuel_cell restores **20**; premium_fuel_cell restores **50**. Both can be used mid-drift.
- Before every launch or redirect: tank + cells ≥ this launch + one more + the final station jump. The end-of-lap form: go home when the total is ≤ **75**.
- Never route to or end at a **stronghold** (standing rule: strongholds are dead ends without the pirate unlock).
- Every sleep uses `settle(ctx, …)` with a `game.Sleep*` constant (`game.SleepTick` multiples). Never `time.Sleep` in `pkg/worker`.
- New KB migration = **75**. After it, run `./scripts/sql/regenerate_initialize_database.sh`, or `TestInitializeDatabaseSQLInSync` fails.
- Binaries go to `bin/` (`go build -o bin/...`). `play_as` runs via `go run ./cmd/tools/play_as`.
- After every task: `go build ./...`, `go test ./...` and `golangci-lint run ./...` all clean, with no new findings.
- Do NOT add predator or hostile avoidance beyond "leave on hazard or repeaters" (operator declined predator avoidance).
- Commit only the files the task names. Never `git add -A`: the tree has dirty `data/*.json`.

## Review Focus

1. **The poll loop overshoots its target.** A pass returns mid-leg, or the gap between idle-loop calls is several ticks. The next pass must treat a passed target as reached (projection test), never turn back. Test: `TestReachedWhenPassed` (Task 2) and `TestSweepPassOvershootAdvances` (Task 6).
2. **The ship is far off its ring** (start, or after an explore run or a tow). It must aim at the ring at the *saved* angle, with no 5° advance, so no arc is skipped. Test: `TestNextTargetRejoinsAtSavedAngle` (Task 2).
3. **Fuel gets close to the reserve while drifting.** No redirect may be issued that leaves less than the home threshold. Cells are burned before the tank is too low. Test: `TestSweepPassGoesHomeAtThreshold` (Task 6).
4. **The angle wraps past ±180°.** Progress must be an unwrapped, monotonically decreasing number, so the lap ends after exactly 360°. Test: `TestAdvanceAcrossWrap` (Task 2).
5. **An arrival lands in a well-known old-galaxy system** (the band reaches Mirzam and Alcor at about -85°). The explore run must not wander the whole old map re-capturing stale systems. Test: `TestExploreRunSkipsStale` (Task 7).

---

### Task 1: Decode the drift position from get_location

**Files:**
- Modify: `pkg/game/serverapi/responses.go` (next to `PathfinderJumpResponse`, ~line 1898)
- Test: `pkg/game/serverapi/location_transit_test.go`

**Interfaces:**
- Produces:
  - `type LocationResponse struct { Location LocationInfo `json:"location"`; Message string `json:"message"` }`
  - `type LocationInfo struct { SystemID string `json:"system_id"`; POIID string `json:"poi_id"`; Connections []string `json:"connections"`; DockedAt *string `json:"docked_at"`; InTransit bool `json:"in_transit"`; TransitType string `json:"transit_type"`; TransitX, TransitY, TransitBearing float64 (`transit_x`, `transit_y`, `transit_bearing`); TransitTicksElapsed int `json:"transit_ticks_elapsed"`; NearbyRepeaters []RepeaterInfo `json:"nearby_repeaters"` }`
  - `func (l LocationInfo) Drifting() bool`: true when InTransit and TransitType == "pathfinder".
- `client.GetRawJSON("location")` holds the **whole** payload (`{"location":{…},"message":…}`). Decode it into `LocationResponse`.

- [ ] **Step 1: Write the failing test.** `TestLocationResponseDriftSample` decodes this live sample, recorded from craftsman-1 on 2026-11-13:
  ```json
  {"location":{"connections":[],"docked_at":null,"in_transit":true,"nearby_repeaters":[],"poi_id":"","system_id":"","transit_bearing":226.5,"transit_ticks_elapsed":297,"transit_type":"pathfinder","transit_x":-4790.231354544311,"transit_y":-6130.078720788663},"message":"Current location"}
  ```
  Asserts:
  - `Drifting()` is true.
  - `TransitX == -4790.231354544311`.
  - `TransitBearing == 226.5`.
  - `TransitTicksElapsed == 297`.
  - `DockedAt == nil`.

  `TestLocationResponseInSystem` decodes `{"location":{"system_id":"frontier","poi_id":"icecap_drift","connections":["horizon"],"docked_at":null,"nearby_repeaters":[{"id":"r1"}]}}`. Asserts:
  - `Drifting()` is false.
  - `SystemID == "frontier"`.
  - `len(NearbyRepeaters) == 1`.
- [ ] **Step 2: Run the test and confirm it fails to compile.** Run: `go test ./pkg/game/serverapi -run TestLocationResponse`. Expected: undefined `LocationResponse`.
- [ ] **Step 3: Add the structs and `Drifting()`.** Reuse the existing `RepeaterInfo`.
- [ ] **Step 4: Run the test and confirm it passes.** Same command. Expected: PASS.
- [ ] **Step 5: Lint and commit.**
  ```bash
  golangci-lint run ./pkg/game/serverapi/...
  git add pkg/game/serverapi/responses.go pkg/game/serverapi/location_transit_test.go
  git commit -m "serverapi: LocationResponse decodes pathfinder transit position"
  ```

### Task 2: Sweep geometry and ring progress (`pkg/sweep`)

**Files:**
- Create: `pkg/sweep/geom.go`
- Create: `pkg/sweep/ring.go`
- Test: `pkg/sweep/geom_test.go`
- Test: `pkg/sweep/ring_test.go`

**Interfaces:**
- Produces constants:
  - `StepDeg = 5.0`
  - `RejoinTolerance = 25.0` (LY: half the ring spacing)
  - `DriftPerTick = 10.0`
- Produces geometry functions:
  - `func Polar(x, y float64) (r, deg float64)`: deg in (-180, 180].
  - `func RingPoint(radius, deg float64) (x, y float64)`
  - `func BearingTo(x0, y0, x1, y1 float64) float64`: result in [0, 360).
  - `func Reached(x, y, tx, ty, bearing float64) bool`: true when the distance is ≤ `DriftPerTick`, or when the target lies behind the ship along `bearing` (dot product ≤ 0).
- Produces ring functions:
  - `func NextTarget(s *State, x, y float64) (tx, ty, deg float64)`. If |r(x,y) − s.Ring| > RejoinTolerance, the target is the ring point at `s.ProgressDeg` (rejoin, no advance). Otherwise it is the ring point at `s.ProgressDeg − StepDeg`.
  - `func Advance(s *State, deg float64)`: `s.SweptDeg += s.ProgressDeg − deg`; `s.ProgressDeg = deg`.
  - `func LapDone(s *State) bool`: `s.SweptDeg >= 360`.
- `ProgressDeg` is **unwrapped**: it keeps decreasing past -180 (e.g. -190). Only `RingPoint`, through cos/sin, cares. The `State` type is defined in Task 4. For this task, declare the minimal `State{Ring, ProgressDeg, SweptDeg float64}` in `ring.go`; Task 4 extends it in `state.go` (move the type there).

- [ ] **Step 1: Write the failing tests.**
  - `TestPolarFacingAway`: `Polar(-6439, 4003)` gives r ≈ 7581.9 (±1), deg ≈ 148.1 (±0.1).
  - `TestBearingToAxes`: `BearingTo(0,0,1,0)==0`; `BearingTo(0,0,0,1)==90`; `BearingTo(0,0,-1,0)==180`; `BearingTo(0,0,0,-1)==270`.
  - `TestReachedWithinOneTick`: ship at (0,0), target (9,0), bearing 0 gives true. Target (50,0) gives false.
  - `TestReachedWhenPassed`: ship at (60,0), target (50,0), bearing 0 gives true.
  - `TestNextTargetOnRingSteps5`: `s{Ring:7500, ProgressDeg:148}`, ship at `RingPoint(7500,148)` gives deg == 143.
  - `TestNextTargetRejoinsAtSavedAngle`: `s{Ring:7950, ProgressDeg:148}`, ship at `RingPoint(7582,148)` gives deg == 148 and the target is `RingPoint(7950,148)` (±0.01).
  - `TestAdvanceAcrossWrap`: start `ProgressDeg:-178`, then `Advance(s,-183)`. SweptDeg == 5, ProgressDeg == -183. After 72 steps of 5° from 148, `LapDone` is true; after 71 it is false.
- [ ] **Step 2: Run the tests and confirm they fail.** Run: `go test ./pkg/sweep/`. Expected: build failure (package empty).
- [ ] **Step 3: Implement** `geom.go` and `ring.go` with the signatures above.
- [ ] **Step 4: Run the tests and confirm they pass.** Same command. Expected: PASS.
- [ ] **Step 5: Lint and commit.**
  ```bash
  golangci-lint run ./pkg/sweep/...
  git add pkg/sweep/
  git commit -m "sweep: ring geometry and unwrapped clockwise progress"
  ```

### Task 3: Fuel budget (`pkg/sweep/fuel.go`)

**Files:**
- Create: `pkg/sweep/fuel.go`
- Test: `pkg/sweep/fuel_test.go`

**Interfaces:**
- Produces:
  - `const FuelCellRestore = 20`, `PremiumCellRestore = 50`, `MinHomeFuel = 75.0`
  - `func Total(tank float64, cells, premium int) float64`: tank + 20·cells + 50·premium.
  - `func HomeThreshold(launchCost float64) float64`: max(75, 3·launchCost). Three launches: this one, one more, and the final jump home.
  - `func CanContinue(tank float64, cells, premium int, launchCost float64) bool`: `Total(...) >= HomeThreshold(launchCost)`.
  - `type Refill struct { Item string; Qty int }`
  - `func Refills(tank, maxTank float64, cells, premium int, launchCost float64) []Refill`:
    - Returns nil when tank ≥ 2·launchCost.
    - Otherwise fills without overflow: premium first, `floor((max−tank)/50)` capped at `premium`; then fuel_cell with the rest, `floor(room/20)` capped at `cells`.
    - Skips zero quantities.
  - `func MeasuredLaunchCost(before, after float64) (float64, bool)`: returns `before−after, true` when it is in (0, before]; otherwise `0, false`.

- [ ] **Step 1: Write the failing tests.**
  - `TestHomeThreshold`: `HomeThreshold(23)==75`, `HomeThreshold(45)==135`.
  - `TestCanContinue`: tank 40, 5 cells, launch cost 45: total 140, true. Tank 40, 4 cells: total 120, false.
  - `TestRefillsNoneWhenTankHealthy`: tank 100, cost 45 gives nil.
  - `TestRefillsPremiumThenCells`: tank 30, max 350, cells 100, premium 3, cost 23. Expected: `[{premium_fuel_cell 3} {fuel_cell 8}]` (room 320 − 150 = 170, so 8 cells).
  - `TestRefillsNoOverflow`: tank 10, max 45, cells 10, premium 0, cost 23. Expected: `[{fuel_cell 1}]`.
  - `TestMeasuredLaunchCost`: (1120, 1075) gives 45, true. (100, 100) gives false. (100, 120) gives false.
- [ ] **Step 2: Run the tests and confirm they fail.** Run: `go test ./pkg/sweep/ -run 'Fuel|Refill|Home|CanContinue|Launch'`. Expected: undefined symbols.
- [ ] **Step 3: Implement `fuel.go`.**
- [ ] **Step 4: Run the tests and confirm they pass.** Expected: PASS.
- [ ] **Step 5: Lint and commit.**
  ```bash
  golangci-lint run ./pkg/sweep/...
  git add pkg/sweep/fuel.go pkg/sweep/fuel_test.go
  git commit -m "sweep: fuel budget, home threshold, mid-drift cell refills"
  ```

### Task 4: Persisted sweep state

**Files:**
- Create: `pkg/sweep/state.go` (move `State` here from `ring.go`)
- Test: `pkg/sweep/state_test.go`

**Interfaces:**
- Produces:
  - `type Phase string`, with constants `PhaseLap = "lap"`, `PhaseExplore = "explore"`, `PhaseHome = "home"`, `PhaseDone = "done"`.
  - `type Gap struct { FromDeg, ToDeg float64; Reason string }`
  - The `State` struct, with these JSON fields (snake_case tags):
    - `Ring`, `StartDeg`, `ProgressDeg`, `SweptDeg float64`
    - `Phase Phase`
    - `TargetX`, `TargetY`, `TargetDeg`, `PlannedBearing float64`
    - `LaunchCost float64`; `LaunchCostMeasured bool`
    - `Launches int`
    - `LastX`, `LastY float64`; `Drifted bool` (set while a drift is in progress; it is the cue that an arrival needs recording)
    - `ArrivalToured bool`; `ExploreJumps int`
    - `Gaps []Gap`; `Unfinished []string`
    - `HomeSystem string`
    - `UpdatedUTC string`
  - `func NewState(ring, startDeg, launchCostEstimate float64) *State`: Phase lap, ProgressDeg = startDeg.
  - `func StatePath(agentsDir, agentID string) string`: `<agentsDir>/<agentID>/sweep_state.json`.
  - `func Load(path string) (*State, bool, error)`: a missing file gives `nil, false, nil`.
  - `func Save(path string, s *State) error`: atomic (temp file in the same directory, then rename); stamps `UpdatedUTC`.
  - `func (s *State) AddUnfinished(systemID string)`: de-duplicated.

- [ ] **Step 1: Write the failing tests.**
  - `TestStateRoundTrip`: Save, then Load, gives a deep-equal state (ignoring UpdatedUTC).
  - `TestLoadMissing`: ok is false, err is nil.
  - `TestSaveAtomicLeavesNoTemp`: after Save, the directory holds only `sweep_state.json`.
  - `TestAddUnfinishedDedup`: adding "a" twice gives `["a"]`.
- [ ] **Step 2: Run the tests and confirm they fail.** Run: `go test ./pkg/sweep/ -run State`. Expected: undefined symbols.
- [ ] **Step 3: Implement.** Use the pattern of `pkg/worker/freight_persist.go` (`saveHeldFreight`).
- [ ] **Step 4: Run the tests and confirm they pass.** Run: `go test ./pkg/sweep/`. Expected: PASS (Task 2's tests still pass after the move).
- [ ] **Step 5: Lint and commit.**
  ```bash
  golangci-lint run ./pkg/sweep/...
  git add pkg/sweep/
  git commit -m "sweep: persisted per-agent sweep state"
  ```

### Task 5: KB measured positions, and nearest station by distance

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` (migration 75)
- Regenerate: `scripts/sql/initialize_database.sql`, via `./scripts/sql/regenerate_initialize_database.sh`
- Create: `pkg/knowledge/system_position.go`
- Modify: `pkg/knowledge/memory.go` (MemoryKB implementation)
- Create: `pkg/galaxy/nearest_by_distance.go`
- Test: `pkg/knowledge/system_position_test.go`
- Test: `pkg/galaxy/nearest_by_distance_test.go`

**Interfaces:**
- Migration 75:
  - Adds `systems.position_source TEXT NOT NULL DEFAULT 'api'`, using `ensureColumn`.
  - Sets `'estimate'` for the 19 operator-estimated frontier systems. These are exactly the systems with no lane path to Sol on 2026-11-12: `ash_pearl, bristle, brushfield, cold_lens, facing_away, far_wick, gloaming, grey_sill, long_night, narrows, quiet_floor, quiet_low, sallow, short_lane, slow_clock, split_lane, still_point, the_sanctum, whitecap`.
- Produces:
  - `type SystemPositionRecorder interface { RecordMeasuredSystemPosition(ctx context.Context, systemID string, x, y float64) (bool, error) }`, implemented on `*SQLiteKB` and `*MemoryKB`. It writes the position and sets `position_source='measured'` only when the stored source is not `'api'`, or the stored position is (0,0). It returns whether it wrote. An unknown system id gives `false, nil`.
  - `galaxy.NearestStationByDistance(ctx context.Context, kb knowledge.Base, x, y float64) (systemID string, ok bool, err error)`:
    - Candidates come from `queryAccessibleStations(ctx, kb, false)`, so strongholds are excluded.
    - Systems whose stored position is (0,0) are skipped.
    - The closest by straight-line distance wins; ties are broken by the smaller id.

- [ ] **Step 1: Write the failing tests.**
  - `TestRecordMeasuredOverwritesEstimate` (SQLite, temp dir): seed `whitecap` at (-4788,-6131) and run the migrations. Recording (-4790.2,-6130.1) returns true; the stored source is `measured`.
  - `TestRecordMeasuredKeepsAPI`: `sol` at (0,100) with source api. Recording returns false and the position is unchanged.
  - `TestRecordMeasuredFillsZero`: a new system at (0,0) with source api. Recording returns true.
  - `TestNearestStationByDistanceSkipsStronghold`: a MemoryKB with:
    - a public station system A at (100,0);
    - a stronghold station system B at (10,0);
    - a public station system C at (0,0), which must be skipped.

    From (0,0) the answer is A.
- [ ] **Step 2: Run the tests and confirm they fail.** Run: `go test ./pkg/knowledge/ ./pkg/galaxy/ -run 'RecordMeasured|NearestStationByDistance'`. Expected: undefined symbols.
- [ ] **Step 3: Implement** the migration, the recorder (SQLite and memory) and `NearestStationByDistance`. Then run `./scripts/sql/regenerate_initialize_database.sh`.
- [ ] **Step 4: Run the tests and confirm they pass.** Run: `go test ./pkg/knowledge/ ./pkg/galaxy/`. Expected: PASS, including `TestInitializeDatabaseSQLInSync`.
- [ ] **Step 5: Lint and commit.**
  ```bash
  golangci-lint run ./pkg/knowledge/... ./pkg/galaxy/...
  git add pkg/knowledge/sqlite_migrations.go pkg/knowledge/system_position.go pkg/knowledge/system_position_test.go pkg/knowledge/memory.go scripts/sql/initialize_database.sql pkg/galaxy/nearest_by_distance.go pkg/galaxy/nearest_by_distance_test.go
  git commit -m "knowledge: systems.position_source (migration 75) + measured positions; galaxy: nearest station by distance"
  ```

### Task 6: Worker verb, the drift pass (lap legs, storms, refills, going home)

**Files:**
- Create: `pkg/worker/pathfinder_sweep.go`
- Test: `pkg/worker/pathfinder_sweep_test.go`. Define `sweepFakeClient` here:
  - It embeds `game.GameClient`, like `fakeClient` in `dispatch_test.go`.
  - It returns scripted `location` payloads, one per `RawCommand("get_location")`, via `GetRawJSON("location")`.
  - It records `Jump` targets and `UseItem` calls, and holds a mutable `*game.State` (fuel, cargo).
- Tests replace `sleepFunc` with a no-op.

**Interfaces:**
- Consumes: Task 1 `LocationResponse`; Tasks 2–4 `sweep.*`; Task 5 `SystemPositionRecorder`, `galaxy.NearestStationByDistance`.
- Produces:
  - `WorkerDispatch` fields `SweepRing`, `SweepStartDeg`, `SweepLaunchCost float64` (set from flags in Task 8).
  - `func (d *WorkerDispatch) PathfinderSweep(ctx context.Context) error`: one bounded pass.
  - `const sweepPassBudget = 150` (ticks; 25 min, under `SleepCommandMaxWait`).
  - `func (d *WorkerDispatch) sweepLaunch(ctx context.Context, s *sweep.State, bearing float64) error`:
    - Applies the refills (`UseItem`).
    - Calls `Jump(ctx, strconv.FormatFloat(bearing,'f',1,64))`.
    - Refreshes fuel via `GetStatus` and, on the first launch, sets `LaunchCost` from `MeasuredLaunchCost`.
    - Sets `PlannedBearing`, `Drifted=true`, `Launches++`.
    - Saves the state.
- Behaviour of one pass in this task (the transit branch, `loc.Location.Drifting()`):
  1. Load or create the state:
     - With no file, use `NewState(SweepRing, SweepStartDeg, SweepLaunchCost)`.
     - With `SweepRing == 0`, return an error saying "sweep_ring not configured".
     - In `PhaseDone`, log `pathfinder_sweep: lap complete` and return nil.
  2. Record `LastX`/`LastY` from each reading.
  3. Storm check: |transit_bearing − PlannedBearing| > 0.5°.
     - Append `Gap{FromDeg: ProgressDeg, ToDeg: TargetDeg, Reason: "storm"}`.
     - Advance to TargetDeg, so the deflected arc is logged and not re-swept.
     - Re-aim with the next target.
  4. Poll every `game.SleepTick` until `Reached(target)` or the pass budget is spent. Re-read the position each tick.
  5. In `PhaseLap`, when the target is reached:
     - `Advance`.
     - If `LapDone`, or `!CanContinue(...)`, switch to `PhaseHome`. Set `HomeSystem` from `NearestStationByDistance` and redirect toward its position.
     - Otherwise redirect to `NextTarget`.
  6. In `PhaseHome`, while drifting, only wait.

- [ ] **Step 1: Write the failing tests.**
  - `TestSweepPassRedirectsAtWaypoint`:
    - State: ring 7500, ProgressDeg 148, target at 143. Fuel 1000, launch cost 45, measured.
    - Scripted readings: one 300 LY short of the target, then one 5 LY short.
    - Expect: one `Jump` whose bearing equals `BearingTo(target, ring point at 138)` to 1 decimal; ProgressDeg 143; SweptDeg 5.
  - `TestSweepPassOvershootAdvances`: the first reading is already 30 LY past the target along the bearing. Expect an immediate redirect and no turn back: the `Jump` bearing differs from the reverse of `PlannedBearing` by more than 90°.
  - `TestSweepPassGoesHomeAtThreshold`:
    - Tank 100, no cells, launch cost 45 (threshold 135); reached a waypoint.
    - KB has a public station system at (-6000, 4000).
    - Expect Phase home, `HomeSystem` set, and the `Jump` bearing toward that station.
  - `TestSweepPassUsesCellsBeforeRedirect`: tank 40, max 1020, 30 fuel_cells, launch cost 45. Expect `UseItem("fuel_cell", 30)` before the `Jump`.
  - `TestSweepPassStormLogsGap`: reading bearing 190 while planned 226.5. Expect one Gap with Reason "storm", and a redirect.
  - `TestSweepPassStatePersisted`: after the pass, `sweep.Load(StatePath(...))` gives the new ProgressDeg and a Launches count of +1.
- [ ] **Step 2: Run the tests and confirm they fail.** Run: `go test ./pkg/worker/ -run TestSweepPass`. Expected: undefined `PathfinderSweep`.
- [ ] **Step 3: Implement the transit branch** and `sweepLaunch`. The in-system branch returns nil with the log line `pathfinder_sweep: in system (arrival handling: task 7)` until Task 7.
- [ ] **Step 4: Run the tests and confirm they pass.** Run: `go test ./pkg/worker/ -run TestSweepPass -race`. Expected: PASS.
- [ ] **Step 5: Lint and commit.**
  ```bash
  golangci-lint run ./pkg/worker/...
  git add pkg/worker/pathfinder_sweep.go pkg/worker/pathfinder_sweep_test.go
  git commit -m "worker: pathfinder_sweep drift pass (5-degree legs, storms, refills, home trigger)"
  ```

### Task 7: Worker verb, arrival, explore run, rejoin and end at a station

**Files:**
- Create: `pkg/worker/pathfinder_sweep_arrival.go`
- Test: `pkg/worker/pathfinder_sweep_arrival_test.go`

**Interfaces:**
- Consumes:
  - Task 6's pass, `sweepLaunch` and `sweepFakeClient`.
  - `NextExploreTarget`, `Explore`, `systemTour`, `hazardShouldLeave`, `exploreRefuelDocked`.
  - `knowledge.CaptureHostilesNearby`.
- Produces: `func (d *WorkerDispatch) sweepInSystem(ctx context.Context, s *sweep.State, loc serverapi.LocationResponse) error`, called by `PathfinderSweep` when not drifting.
- `const sweepExploreStale = math.MaxInt64 / 2`. Passed as `StaleTicks`, it means stale systems never qualify: only undiscovered and never-visited systems do.
- `const sweepExploreMaxJumps = 30`.
- Behaviour:
  1. **Arrival** (when `s.Drifted`):
     - Call `RecordMeasuredSystemPosition(system, LastX, LastY)` (type-assert the KB) and log whether it wrote.
     - Call `CaptureHostilesNearby` with `loc.Location.NearbyRepeaters`.
     - Set `Drifted=false`, `ArrivalToured=false`, `ExploreJumps=0`.
     - In `PhaseLap`, set the Phase to explore.
  2. **PhaseExplore:**
     - **Leave** when there are repeaters at the POI or `hazardShouldLeave(state, now)`: `AddUnfinished(system)`, then go straight to rejoin (step 3).
     - Else, if `!ArrivalToured`: run `systemTour` once and set `ArrivalToured=true`.
     - Else, if `ExploreJumps < 30` and `NextExploreTarget(..., sweepExploreStale, ...)` is ok: run one `Explore` step (StaleTicks `sweepExploreStale`), then `ExploreJumps++`.
     - Else: if `ExploreJumps >= 30`, `AddUnfinished(current)`. If docked at a station, refuel (`exploreRefuelDocked`). Then rejoin.
     - One action per pass. Save after each.
  3. **Rejoin:**
     - If `!CanContinue`, go to PhaseHome.
     - Otherwise set Phase lap; `NextTarget` from the current system's KB position. When that position is (0,0) (a system the explore run reached by lane, with no coordinates yet), fall back to the last drift reading `LastX`/`LastY`. Then `sweepLaunch` toward the target.
  4. **PhaseHome in a system:**
     - Current system == `HomeSystem`, or it holds an accessible non-stronghold station: dock, refuel, Phase done, log `pathfinder_sweep: lap complete at <system>; ring <R>, swept <deg>, gaps <n>, unfinished <list>`.
     - Else, a station reachable by lanes (`galaxy.FindNearestStation`, includeStrongholds false): `Autopilot` to it.
     - Else, if `Total ≥ LaunchCost`: `sweepLaunch` toward `NearestStationByDistance`.
     - Else: log `waiting for GSA tow` and return nil.

- [ ] **Step 1: Write the failing tests.**
  - `TestSweepArrivalRecordsMeasuredPosition`: Drifted with LastX/LastY set. After the pass, the KB position is the reading and the source is measured.
  - `TestSweepArrivalLeavesOnRepeaters`: arrival location with 1 repeater. Expect no tour or Explore calls, one `Jump` (rejoin), and the system in Unfinished.
  - `TestExploreRunSkipsStale`: MemoryKB in which every neighbour is visited but stale. Expect no Explore step; the rejoin launch happens on the pass after the arrival tour.
  - `TestExploreRunCapsJumps`: ExploreJumps 30 with targets remaining. Expect the system in Unfinished and a rejoin launch.
  - `TestSweepRejoinTargetsSavedAngle`: ProgressDeg 120, system at r 7,600, ring 7,900. The `Jump` bearing aims at `RingPoint(7900,120)`.
  - `TestSweepRejoinFromUnplacedSystem`: current system stored at (0,0), LastX/LastY = RingPoint(7600,121), ring 7900, ProgressDeg 120 → `Jump` bearing = `BearingTo(LastX,LastY, RingPoint(7900,120))`, not one computed from the origin.
  - `TestSweepHomeDocksAndFinishes`: PhaseHome in a system with a public station. Expect Dock and Refuel, Phase done, then a second pass does nothing.
  - `TestSweepHomeWaitsForTowWhenDry`: PhaseHome, no reachable station, fuel 10 and no cells, launch cost 45. Expect no Jump and nil error.
- [ ] **Step 2: Run the tests and confirm they fail.** Run: `go test ./pkg/worker/ -run 'TestSweep|TestExploreRun'`. Expected: undefined `sweepInSystem`.
- [ ] **Step 3: Implement** `sweepInSystem` and wire it into `PathfinderSweep` in place of the Task 6 placeholder.
- [ ] **Step 4: Run the tests and confirm they pass.** Run: `go test ./pkg/worker/ -race`. Expected: PASS (whole package).
- [ ] **Step 5: Lint and commit.**
  ```bash
  golangci-lint run ./pkg/worker/...
  git add pkg/worker/pathfinder_sweep_arrival.go pkg/worker/pathfinder_sweep_arrival_test.go pkg/worker/pathfinder_sweep.go
  git commit -m "worker: pathfinder_sweep arrival, explore run, rejoin, station finish"
  ```

### Task 8: Wire the verb into worker, overmind and fleet config

**Files:**
- Modify: `pkg/worker/dispatch.go`. Add `"pathfinder_sweep": true` to `supported` and a `case "pathfinder_sweep": return d.PathfinderSweep(ctx)`.
- Modify: `pkg/overmind/supervisor/config.go`. Add yaml fields `sweep_ring`, `sweep_start_deg`, `sweep_launch_cost` (`float64`, omitempty).
- Modify: `pkg/overmind/supervisor/supervisor.go`. Pass the flags `--sweep-ring`, `--sweep-start-deg`, `--sweep-launch-cost` when they are non-zero. Follow the pattern of `--freight-max-packages` (around line 83).
- Modify: `cmd/worker/main.go`. Add the matching flags and set `dispatch.SweepRing/SweepStartDeg/SweepLaunchCost` (pattern around line 506).
- Modify: `data/overmind/roles.yaml`. New role `pathfinder_sweep`:
  - The same `schedule:` block as `explorer`.
  - `idle: pathfinder_sweep`.
  - A comment pointing at the spec.
- Create: `data/overmind/sweep-fleet.yaml`.
  - A header comment: the spec link and "launch LAST, after mission-learn has released the explorers".
  - One line per ship: `{ agent_id: explorer-N, role: pathfinder_sweep, station: "", sweep_ring: <R>, sweep_start_deg: 148, sweep_launch_cost: <est> }`.
  - Rings 7500…7950 are assigned in the order of Task 9's fitting table.
  - Launch-cost estimates: Reserve 41, Compendium 23, Survey Vessel 45.
- Tests: `pkg/worker/roles_test.go` (already enforces that roles.yaml commands are supported), `pkg/overmind/supervisor/supervisor_test.go`.

**Interfaces:**
- Consumes: Task 6 `WorkerDispatch.SweepRing/SweepStartDeg/SweepLaunchCost`, `PathfinderSweep`.

- [ ] **Step 1: Write the failing test.** `TestSweepFlagsForwarded` in `supervisor_test.go`: a spec with `SweepRing: 7550, SweepStartDeg: 148, SweepLaunchCost: 23` yields args containing `--sweep-ring 7550 --sweep-start-deg 148 --sweep-launch-cost 23`. A zero spec yields none of them.
- [ ] **Step 2: Run the test and confirm it fails.** Run: `go test ./pkg/overmind/supervisor/ -run TestSweepFlagsForwarded`. Expected: unknown field.
- [ ] **Step 3: Implement** the config, supervisor, worker flags and dispatch case, plus the roles.yaml role and `sweep-fleet.yaml`. Leave the ten agent lines commented out until Task 9 fills in each ring and hull.
- [ ] **Step 4: Run the build and the full test suite.**
  ```bash
  go build ./... && go test ./... 2>&1 | tail -20
  ```
  Expected: every package ok, including `roles_test.go`.
- [ ] **Step 5: Lint and commit.**
  ```bash
  golangci-lint run ./...
  git add pkg/worker/dispatch.go pkg/overmind/supervisor/config.go pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/supervisor_test.go cmd/worker/main.go data/overmind/roles.yaml data/overmind/sweep-fleet.yaml
  git commit -m "overmind: pathfinder_sweep role, sweep-fleet.yaml, sweep_* worker flags"
  ```

### Task 9 (operations): Buy, fit and stock the ten ships; first-launch check; sail

Not code. Each step is a `play_as` session. To take an explorer out of `mission-learn-fleet`:
1. Add it to `data/overmind/mission-learn-overrides.json` `removed`.
2. SIGHUP the overmind: `pgrep -f '[b]in/overmind .*mission-learn'`.
3. Wait until the worker process is gone. Never run play_as while it is up.

**Supplies:**
- **Drives:** the operator is bringing **7 pathfinder_drives to CJ** (Conjunction Junction). Buy only **3** more at 276,220 (Sirius Observatory, Nova Terra, Alpha Centauri or Mobile Capital).
- **Hulls:** 3 Reserve, 6 Compendium, 1 Survey Vessel. explorer-3 already flies a Compendium.
- **Fuel cells per ship:** Compendium ≥ 70 fuel_cell, which covers about 65 needed plus the extra launches at each system reached. Survey Vessel ≥ 120. Reserve: none needed, but carry 20.

- [ ] **Step 1: Find the hulls.** For each hull class, use `browse_ships` / `list_ships` at a shipyard. Record the selling station and price in a fitting table at the bottom of this file: agent, hull, station, ring, drive source.
- [ ] **Step 2: Buy and fit each ship.** For each of the 10 agents:
  - `buy_listed_ship` (or `buy_ship`), then `switch_ship`, then travel to CJ.
  - Take a drive from where the operator left it. If it is in faction storage, `faction_withdraw` lands it in cargo (Officer rank needed).
  - `install_mod pathfinder_drive`.
  - Buy the fuel cells.

  Check: `get_ship` shows `pathfinder_drive` among the modules, and the cargo holds the cell count.
- [ ] **Step 3: First-launch check, on one Compendium.**
  - From an old-galaxy system, `jump 148` and then `get_location`. Read `transit_x/y`.
  - Record the fuel before and after (`get_ship`).
  - Fix Task 8's `sweep_launch_cost` for that hull to the measured value, and update the spec's hull table.
  - Check: the drift speed is 10 LY/tick. If it is not, stop and tell the operator: `DriftPerTick` would need to become per-hull.
- [ ] **Step 4: Fill `sweep-fleet.yaml`.** Uncomment the ten lines with their rings, then commit:
  ```bash
  git add data/overmind/sweep-fleet.yaml docs/superpowers/plans/2026-11-12-pathfinder-frontier-sweep.md
  git commit -m "sweep: round 1 fleet assignments"
  ```
- [ ] **Step 5: Launch.** Build `bin/worker` and `bin/overmind` (`go build -o bin/...`). Start the sweep-fleet overmind as the cold-start runbook does for a fleet. Check:
  - Each worker logs `pathfinder_sweep` passes.
  - `data/agents/explorer-N/sweep_state.json` appears, with `launches ≥ 1`.
  - `get_location` shows `transit_type pathfinder`.
- [ ] **Step 6: Monitor.** Add `pathfinder_sweep|PATHFINDER STORM|lap complete|waiting for GSA tow` to the fleet log monitor.
- [ ] **Step 7: At each finish.** When an agent logs `lap complete`:
  - Remove it from `sweep-fleet.yaml` (or via its override).
  - Put it back in mission-learn by removing it from `removed`, then SIGHUP.
  - Report its gaps and unfinished systems to the operator.
