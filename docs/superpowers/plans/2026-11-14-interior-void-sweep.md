# Interior Void Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pathfinder explorers fly a 100 LY square wave through every void zone inside 7,150 LY, one zone each, largest first, with a shared claim/coverage ledger.

**Architecture:** `pkg/sweep` gains pure zone geometry (grid → zones → lanes → waypoints) and a flock-guarded run ledger (zones, claims, coverage). The existing `pathfinder_sweep` worker verb gets a `void` mode that reuses its drift loop, arrival/explore handling, safety rules, fuel-home and dock-resume paths, swapping ring targets for the zone's waypoint list. A `cmd/tools/void-zones` tool generates the ledger from the KB and renders the coverage map.

**Tech Stack:** Go 1.24, SQLite KB via `knowledge.Base`, YAML fleet config.

**Spec:** docs/superpowers/specs/2026-11-14-interior-void-sweep-design.md

## Global Constraints

- Void point: nearest known system > **350 LY** (all position sources; skip non-Sol rows at 0,0).
- Area: radius ≤ **7,150 LY**. Grid **50 LY**, 4-connected cells. Zone min area **40,000 LY²** (200 × 200).
- Lanes **100 LY** apart, first lane **50 LY** in from the side, parallel to the bounding box's long side, alternating direction, one span per lane (first to last void cell on that line).
- Order: largest zone first. One active zone per explorer.
- Claim lease expires after **2 h** with no progress saved.
- Hull rule: an explorer whose `maxTank / launchCost < 30` launches is "small"; it takes the largest unclaimed zone needing ≤ `2 × launchesPerTank` launches (need = 2 × lanes), else the largest unclaimed zone. Others take the largest unclaimed zone.
- Never `time.Sleep` in pkg/worker; use `settle(ctx, d)` / Sleep constants.
- Never route through strongholds without the pirate unlock (existing `smugglingUnlocked` gates).
- golangci-lint clean; `go build ./...` and `go test ./...` pass before each commit; binaries to `bin/`.
- Commit only the task's files (never `git add -A`: data/*.json is dirty).

## Review Focus

- Two worker processes claiming at the same instant must never get the same zone (flock around read-modify-write) — tested in Task 2.
- A worker restarted mid-zone must resume at its saved waypoint, not re-claim or skip — tested in Task 5.
- A U-shaped zone's lane spans the gap (one span per lane) and no lane is empty — tested in Task 1.
- A storm deflection mid-lane must not advance the waypoint — tested in Task 5.
- The ledger tool must refuse to overwrite a run with claims or coverage — tested in Task 3.

---

### Task 1: Void zone geometry

**Files:**
- Create: `pkg/sweep/void.go`
- Test: `pkg/sweep/void_test.go`

**Interfaces:**
- Produces:
  - `type Point struct{ X, Y float64 }`
  - `type ZoneOpts struct{ Grid, Threshold, MaxRadius, MinArea float64 }` and `func DefaultZoneOpts() ZoneOpts` (50, 350, 7150, 40000)
  - `type Zone struct { ID string; Cells [][2]int; Grid, Area, CX, CY, W, H float64 }` (JSON tags snake_case; `ID` is `"V01"`, `"V02"`… in area order)
  - `func ComputeZones(systems []Point, o ZoneOpts) []Zone` — sorted by Area descending
  - `type Lane struct{ AX, AY, BX, BY float64 }`
  - `func Lanes(z Zone, spacing float64) []Lane`
  - `func Waypoints(lanes []Lane) []Point` — A0, B0, A1, B1, …

- [ ] **Step 1: Write failing tests**

```go
func TestComputeZonesFindsTheHoleAndSortsBySize(t *testing.T)
// systems on a 300 LY lattice over ±3000 except two holes: a 1200x1200 square
// centred (1500,1500) and an 800x800 square centred (-1500,-1500).
// want len==2, zones[0].ID=="V01", zones[0].Area > zones[1].Area,
// zones[0] centre within 100 LY of (1500,1500); no cell centre farther than 7150 from origin.
func TestComputeZonesSkipsSmallZones(t *testing.T) // a 150x150 hole -> 0 zones
func TestLanesRunAlongLongSideAndAlternate(t *testing.T)
// a 1000(W) x 400(H) rectangle of cells: lanes horizontal, 4 lanes at y = minY+50, +150, +250, +350;
// lanes[0] runs west->east (AX<BX), lanes[1] east->west; every span inside the rectangle.
func TestLanesSpanAGapInAUShapedZone(t *testing.T)
// U shape (two vertical arms joined at the bottom): a lane through both arms is ONE span arm-to-arm.
func TestWaypointsInterleaveLaneEnds(t *testing.T) // 2 lanes -> [A0,B0,A1,B1]
```

- [ ] **Step 2: Run** `go test ./pkg/sweep -run 'Zones|Lanes|Waypoints'` — Expected: FAIL (undefined).
- [ ] **Step 3: Implement** in `pkg/sweep/void.go`. Nearest-system test uses a 500 LY bucket grid (search ±2 buckets is enough for 350 LY). Cells are `[i,j]` with centre `((i+.5)G, (j+.5)G)`. Lane axis: horizontal when W ≥ H. A lane at offset v keeps cells whose centre lies within G/2 of the line.
- [ ] **Step 4: Run** the same command — Expected: PASS.
- [ ] **Step 5: Commit** `git add pkg/sweep/void.go pkg/sweep/void_test.go && git commit -m "sweep: void zone geometry"`

### Task 2: Void run ledger (zones, claims, coverage)

**Files:**
- Create: `pkg/sweep/voidrun.go`
- Test: `pkg/sweep/voidrun_test.go`

**Interfaces:**
- Consumes: `Zone`, `Lanes`, `Waypoints` (Task 1).
- Produces:
  - `type VoidZone struct { Zone; Waypoints []Point; Claimant string; Next int; Done bool; LeaseUTC string; Found []string }`
  - `type VoidRun struct { Created string; Zones []*VoidZone }`
  - `const VoidLease = 2 * time.Hour`
  - `func NewVoidRun(zones []Zone, spacing float64, now time.Time) *VoidRun`
  - `func LoadVoidRun(path string) (*VoidRun, error)`; `func (r *VoidRun) Active() bool` (any claim or any Next>0/Done)
  - `func ClaimZone(path, agent string, launchesPerTank float64, ok func(*VoidZone) bool, now time.Time) (*VoidZone, error)` — returns the agent's own unfinished claim if it has one; else expires leases older than VoidLease; else picks per the hull rule among unclaimed, not-done zones passing `ok`; nil,nil when none.
  - `func SaveProgress(path, agent, zoneID string, next int, found []string, now time.Time) error` — sets Next, appends found (dedup), refreshes lease; Done and Claimant="" when next ≥ len(Waypoints). Errors if agent is not the claimant.
  - All ledger writes: flock on `path+".lock"`, temp file + rename (pattern: pkg/rescue/queue.go:189-200).

- [ ] **Step 1: Write failing tests**

```go
func TestClaimZoneLargestFirstAndOncePerZone(t *testing.T)   // agents a,b big tanks -> V01, V02
func TestClaimZoneReturnsOwnClaimOnRestart(t *testing.T)     // a claims V01, claims again -> V01
func TestClaimZoneSmallTankTakesAFittingZone(t *testing.T)   // launchesPerTank 15: V01 needs 60, V02 needs 20 -> V02
func TestClaimZoneTakesOverAnExpiredLease(t *testing.T)      // a's lease 3h old -> b gets V01 resuming at a's Next
func TestClaimZoneSkipsZonesOkRejects(t *testing.T)
func TestSaveProgressMarksDoneAndRejectsNonClaimant(t *testing.T)
func TestClaimZoneConcurrentClaimsNeverShare(t *testing.T)   // 10 goroutines claim at once -> 10 distinct zones
```

- [ ] **Step 2: Run** `go test ./pkg/sweep -run 'ClaimZone|SaveProgress'` — Expected: FAIL.
- [ ] **Step 3: Implement** `pkg/sweep/voidrun.go`.
- [ ] **Step 4: Run** with `-race` — Expected: PASS.
- [ ] **Step 5: Commit** `git add pkg/sweep/voidrun.go pkg/sweep/voidrun_test.go && git commit -m "sweep: void run ledger with flock-guarded claims"`

### Task 3: `void-zones` tool (generate, status, map)

**Files:**
- Create: `cmd/tools/void-zones/main.go`, `cmd/tools/void-zones/map.go`
- Test: `cmd/tools/void-zones/main_test.go`

**Interfaces:**
- Consumes: `ComputeZones`, `NewVoidRun`, `LoadVoidRun`, `(*VoidRun).Active` (Tasks 1–2); KB systems via `knowledge.Base` (all systems with positions).
- Produces: CLI `void-zones generate|status|map [--kb data/spacemolt-knowledge.db] [--run data/sweep/void-run.json] [--out file.html]`.
  - `generate`: refuses (exit 1, message names the file and says to archive it) when the run file exists and `Active()`; otherwise writes a fresh run.
  - `status`: one line per zone: id, area, lanes, claimant, next/total, done, found count.
  - `map`: self-contained HTML (inline SVG, theme tokens as in the published void map) showing systems, zones, flown lane segments (waypoints < Next) and found systems; written to `--out`.
  - Core functions are testable without a DB: `func generate(systems []sweep.Point, runPath string, now time.Time) error`, `func renderMap(run *sweep.VoidRun, systems []sweep.Point) string`.

- [ ] **Step 1: Write failing tests** `TestGenerateRefusesAnActiveRun` (claimed run on disk → error, file unchanged), `TestGenerateWritesAFreshRun`, `TestRenderMapShowsFlownLanes` (output contains a flown-lane element for a zone with Next=2).
- [ ] **Step 2: Run** `go test ./cmd/tools/void-zones` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS. Also `go build -o bin/void-zones ./cmd/tools/void-zones`.
- [ ] **Step 5: Commit** the three files.

### Task 4: Config plumbing for void mode

**Files:**
- Modify: `pkg/overmind/supervisor/config.go` (beside `SweepStage`), `pkg/overmind/supervisor/supervisor.go:89-96`, `cmd/worker/main.go` (sweep flags ~line 92 and ~516), `pkg/worker/dispatch.go:61-72`
- Test: existing supervisor arg tests file (add a case)

**Interfaces:**
- Produces: yaml `sweep_mode: void` and `sweep_void_run: <path>` → worker flags `--sweep-mode`, `--sweep-void-run` (default `data/sweep/void-run.json`) → `WorkerDispatch.SweepMode string`, `WorkerDispatch.SweepVoidRun string`.

- [ ] **Step 1: Failing test** `TestSweepModeFlagsPassedThrough`: spec with `SweepMode:"void"`, `SweepVoidRun:"x.json"` → args contain `--sweep-mode void --sweep-void-run x.json`; empty values add nothing.
- [ ] **Step 2: Run** `go test ./pkg/overmind/supervisor -run SweepMode` — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — PASS; `go build ./...`.
- [ ] **Step 5: Commit** the four files + test.

### Task 5: Worker void mode

**Files:**
- Modify: `pkg/sweep/state.go` (fields), `pkg/worker/pathfinder_sweep.go` (entry check, drift branch), `pkg/worker/pathfinder_sweep_arrival.go` (`sweepRejoin`), `pkg/worker/pathfinder_sweep_resume.go` (`sweepAfterLap`)
- Create: `pkg/worker/pathfinder_void.go`
- Test: `pkg/worker/pathfinder_void_test.go` (reuse `newArrivalDispatch`, `sweepFakeClient`, `placeSystem`, `addSweepStation`, `inSystem`)

**Interfaces:**
- Consumes: Tasks 1, 2, 4.
- Produces:
  - `State` fields: `Mode string` (`""`=ring, `"void"`), `ZoneID string`, `Waypoint int`, `Waypoints []sweep.Point` (copied from the claim), `ZoneFound []string`.
  - `func NewVoidState(launchCost float64) *State` (Mode "void", Phase lap, no zone).
  - `func (d *WorkerDispatch) voidClaim(ctx, s *sweep.State, path string) (bool, error)` — claim via `sweep.ClaimZone` with launchesPerTank = maxTank / s.LaunchCost and ok = "a station system exists within 2,500 LY of Waypoints[0]" (`galaxy.NearestStationByDistance`); copies zone into state; Waypoint = the claim's Next.
  - `func (d *WorkerDispatch) voidTarget(s *sweep.State) (x, y float64, done bool)`.

Behaviour (each its own test):
- [ ] **Step 1: Failing tests**

```go
func TestVoidDockedWithNoZoneClaimsAndHeadsForTheFirstWaypoint(t *testing.T)
// docked, fresh void state, ledger with one zone -> state.ZoneID=="V01"; a launch whose bearing aims at Waypoints[0]
// (or, when a charted system is nearer Waypoints[0] than this one and lane-reachable, an ensureHomeNav to it first).
func TestVoidDriftReachingAWaypointAdvancesAndSavesProgress(t *testing.T)
// drifting at Waypoints[1] -> Waypoint 2, ledger Next==2, redirect toward Waypoints[2].
func TestVoidStormDoesNotAdvance(t *testing.T)
// bearing off by >0.5° -> gap logged, Waypoint unchanged, re-aim at the same waypoint.
func TestVoidLandingRejoinsAtTheCurrentWaypoint(t *testing.T)
// landed (explore finished) -> launch toward Waypoints[s.Waypoint], not a ring point; found system appended to ZoneFound and ledger Found.
func TestVoidLastWaypointMarksZoneDoneAndHeadsHome(t *testing.T)
// reaching the last waypoint -> ledger Done, state Phase home.
func TestVoidDockedAfterZoneClaimsTheNext(t *testing.T)
func TestVoidNoZonesLeftStaysDocked(t *testing.T)    // logs "void sweep: no zones left"
func TestVoidRestartResumesSavedWaypoint(t *testing.T) // state Waypoint 5 + own claim -> aims at Waypoints[5]
func TestVoidLowFuelGoesHomeThenResumes(t *testing.T)  // CanContinue false -> home; docked + refuelled -> rejoin at saved waypoint
```

- [ ] **Step 2: Run** `go test ./pkg/worker -run Void` — FAIL.
- [ ] **Step 3: Implement.** In `PathfinderSweep`: when `d.SweepMode == "void"`, skip the `SweepRing` check, start from `NewVoidState`, and route `PhaseDone` / docked-home to "save ledger, claim next" instead of `sweepAfterLap`'s lap message. In `sweepDrift`'s `PhaseLap` branch and `sweepRejoin`, use `voidTarget` instead of `NextTarget`/`Advance`/`LapDone` when `s.Mode == "void"`; after each reached waypoint call `sweep.SaveProgress`. Arrivals in hidden systems add to `ZoneFound` (the system id from `sweepArrival` when the system is not `sweepAPISystem`). Everything else (explore, safety, fuel, cells, refuel-resume) stays shared.
- [ ] **Step 4: Run** `go test ./pkg/worker -count=1` (all, not just Void) and `golangci-lint run ./...` — PASS / 0 issues.
- [ ] **Step 5: Commit** the listed files.

### Task 6: Rollout (operator-gated; after round 2)

- [ ] **Step 1:** Wait until every `data/agents/explorer-*/sweep_state.json` in sweep-fleet.yaml has `phase: done`. Report to the operator; do not proceed without their go.
- [ ] **Step 2:** `bin/void-zones generate` → `bin/void-zones status` (expect ~58 zones). `bin/void-zones map --out <scratchpad>/void_map.html` and republish the void map artifact (same file path / URL https://claude.ai/artifact/M3DFdEbXkHWTPEze3hWtcp).
- [ ] **Step 3:** Archive round-2 states to `data/sweep-archive/round2-<date>/` with the overmind stopped; set each worker in `data/overmind/sweep-fleet.yaml` to `sweep_mode: void` (keep `sweep_launch_cost`; ring/start/stage fields become unused). Commit the yaml only.
- [ ] **Step 4:** Rebuild `bin/worker`, relaunch the sweep overmind with identical argv, re-arm the fleet monitor with `void sweep:` events (zone claimed, zone done, no zones left).
