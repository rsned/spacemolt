# CJ Station Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An hourly-fed, background-tab station console for Conjunction Junction (rotating hologram with coloured, labelled zones, popups with 48 h trends, model switcher) plus sectioned play_as formatters for `get_base` and `list_station_passengers`.

**Architecture:** miningbot_0000's schedule captures `get_base`, `facility list` and `list_station_passengers` hourly into `data/assets.db` (pkg/assets owns parsing + storage, like `capture_base.go`). A new `pkg/stationstate` turns the latest reading into per-zone severities: the **only** place thresholds live; both the play_as formatter and the console server call it, and the browser just renders the JSON it is given. `cmd/tools/station-console` serves an embedded three.js page plus JSON and the KB mesh files.

**Tech Stack:** Go 1.24 (`embed`, `net/http`, `database/sql` on the existing assets store), three.js 0.160.0 from jsDelivr (same as the spike), no frontend build step.

**Spec:** `docs/superpowers/specs/2026-11-08-cj-station-console-design.md`

## Global Constraints

- CJ only: base `c976fa7df53c87f9616165d7d411b41a`, poi `862e07fa76571d0a76cab3cbaf4046e7`, system cloverfield.
- Captures never fail the capture pass (same rule as `CaptureBase`: a failed read returns nil and writes nothing).
- Console listens on **:8094**; models read from `--models` (default `~/spacemolt/kb/data/mesh_bakeoff/out-stations`), not copied into the repo.
- Default model `player__wheel__capital_s5521`; switcher offers the nine in the spec.
- Stale banner when the latest reading is older than **2 h**. Trends cover **48 h**.
- Any sleep in Go uses `pkg/game/constants.go` Sleep constants; no `time.Sleep` in pkg/worker.
- All new Go passes `golangci-lint` with no new findings; `go build ./... && go test ./...` before each commit.
- Commits: `git commit --no-verify` (the pre-commit hook trips on old findings), named files only, never `git add -A` (dirty data/*.json). End messages with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Field names come from recorded replies (spec "Data sources"), never guessed.

## Review Focus

1. **No data yet** (empty tables, first deploy): `Evaluate` on a zero `Reading` and the console's `/api/state` must return a "NO DATA" status, not panic or show THRIVING. Test in Task 2 and Task 4.
2. **No battery built** (`battery_capacity` 0): battery 0 must not turn the core red. Test in Task 2.
3. **`max_shield` / `max_hull` / `fuel_capacity` / `life_support_demand` = 0**: no division by zero, no NaN in JSON. Test in Task 2.
4. **Tourism counter with one snapshot, or a decrease** (server reset): rate is "unknown", never negative. Test in Task 1.
5. **A facility type no table knows** (future builds): lands in COMMERCE · ADMIN and its unmet maintenance still colours that zone. Test in Task 2.

---

### Task 1: Facility + passenger capture and history reads (pkg/assets)

**Files:**
- Create: `pkg/assets/capture_station.go`, `pkg/assets/capture_station_test.go`, `pkg/assets/testdata/cj_facility_list.json`, `pkg/assets/testdata/cj_station_passengers.json`
- Modify: `pkg/assets/schema.sql` (two new append-only tables), `pkg/worker/dispatch.go` (two cases + vocabulary), `pkg/worker/schedule.go` (`widePhaseCommands`)

**Interfaces:**
- Produces:
  - `type FacilityRow struct { FacilityID, Type, Name, Category, Recipe string; Level int64; MaintenanceSatisfied *bool; MaintenanceLevel int64; QueuedRuns, QueuedItems, BacklogTicks int64; ItemsPerHour float64 }`
  - `func FacilityRowsFrom(raw []byte) (baseID string, rows []FacilityRow, ok bool)` — reads `faction_facilities[]` (with nested `production{}`); ok false when unreadable or no `base_id`.
  - `type PassengerSnapshot struct { Waiting, LoungeOccupancy, LoungeCapacity int64; DemandLevel string; FareSurge float64; MarketConditions string }`
  - `func PassengerSnapshotFrom(raw []byte) (PassengerSnapshot, bool)` — fields `count`/`waiting[]`, `transit_lounge.{occupancy,capacity}`, `demand_level`, `fare_surge`, `market_conditions`; ok false when `station` is empty.
  - `func CaptureFacilities(ctx, client game.GameClient, st *Store, agentID string, now time.Time) error` — docked only; `client.Facility(ctx, map[string]any{"action":"list"})`, then `GetRawJSON("facility")`.
  - `func CapturePassengers(ctx, client game.GameClient, st *Store, agentID string, baseID string, now time.Time) error` — `client.RawCommand(ctx, "list_station_passengers", nil)`, then `GetRawJSON("list_station_passengers")` (verify that key in `storeRawJSON`; the live log says "Stored raw JSON for list_station_passengers"). `baseID` is the docked base from state, since the reply only names the station.
  - `type StationReading struct { Base BaseSnapshot; Facilities []FacilityRow; Passengers *PassengerSnapshot; CapturedAt time.Time; TourismPerHour *float64 }`
  - `func (s *Store) LatestStation(ctx, baseID string) (StationReading, bool, error)` — newest base snapshot, the facility rows of the newest facility capture, newest passenger row; `TourismPerHour` from the newest two base rows ≥ 30 min apart, nil if fewer than two or the delta is negative.
  - `type HistoryPoint struct { At time.Time; ShieldPct, HullPct, FuelPct, PowerDrawPct, LifeMarginPct float64; TourismPerHour *float64; BacklogTicks int64 }`
  - `func (s *Store) StationHistory(ctx, baseID string, since time.Time) ([]HistoryPoint, error)` — one point per base snapshot; `BacklogTicks` = sum over that hour's facility capture (nearest at or before).
- Tables: `facility_snapshots(base_id, agent_id, captured_at, facility_id, type, name, category, recipe, level, maintenance_satisfied INTEGER NULL, maintenance_level, queued_runs, queued_items, backlog_ticks, items_per_hour REAL)` indexed `(base_id, captured_at)`; `station_passenger_snapshots(base_id, agent_id, captured_at, waiting, lounge_occupancy, lounge_capacity, demand_level, fare_surge REAL, market_conditions)` indexed the same. New tables go in `schema.sql` (CREATE IF NOT EXISTS); never edit an applied migration.

- [ ] **Step 1: Fixtures.** Copy the `raw` of the 2026-11-09T06:38:41Z miningbot_0000 `facility list` event in `data/stations/cloverfield/events.jsonl` to `testdata/cj_facility_list.json`. Write `testdata/cj_station_passengers.json` from the operator's paste: `{"count":0,"demand_level":"discounted","fare_surge":0.6,"market_conditions":"Conjunction Junction is well served by passenger carriers, so fares are discounted to 0.6x of normal. The station's economy is struggling: few citizens can afford to travel, so expect little new demand until it recovers.","station":"Conjunction Junction","transit_lounge":{"capacity":20,"lounge":"Transit Lounge","occupancy":0,"passengers":[]},"waiting":[]}`.
- [ ] **Step 2: Failing tests** in `capture_station_test.go`:
  - `TestFacilityRowsFrom_CJ`: baseID `c976fa7df53c87f9616165d7d411b41a`, 28 rows; the `copper_wire_mill` row has Category `production`, Recipe `Process Copper Wiring`, `ItemsPerHour` 7428, `QueuedRuns` 0; the `point_defense_battery` row has Category `defense` and `*MaintenanceSatisfied == true`; `faction_office` has `MaintenanceSatisfied == nil`.
  - `TestFacilityRowsFrom_Rejects`: `""`, `{}`, `not json` → ok false.
  - `TestPassengerSnapshotFrom_CJ`: DemandLevel `discounted`, FareSurge 0.6, LoungeCapacity 20, LoungeOccupancy 0, Waiting 0, MarketConditions starts with `Conjunction Junction is well served`.
  - `TestLatestStation_TourismRate`: base rows at T0 revenue 3956 and T0+90m revenue 7638 → `*TourismPerHour` ≈ 2454.67 (±0.5); with only the first row → nil; with T0+90m revenue 100 → nil.
  - `TestLatestStation_Empty`: empty store → ok false, err nil.
  - `TestStationHistory_48h`: three base rows (T0, T0+1h, T0+50h) and `since = T0+50h-48h` → 2 points; `ShieldPct` 100 for max_shield 12000/shield 12000 and 0 (not NaN) when max_shield is 0.
- [ ] **Step 3: Run** `go test ./pkg/assets -run 'FacilityRows|PassengerSnapshot|LatestStation|StationHistory'`. Expected: FAIL, undefined names.
- [ ] **Step 4: Implement** the Produces list and the two tables.
- [ ] **Step 5: Run** the same command. Expected: PASS.
- [ ] **Step 6: Worker verbs.** Add `case "capture_facilities"` → `assets.CaptureFacilities(...)` and `case "capture_station_passengers"` → `assets.CapturePassengers(..., state docked base id, ...)` in `pkg/worker/dispatch.go` beside `capture_base`; add both to the `supported` vocabulary and `widePhaseCommands`. Add a dispatch test mirroring the existing `capture_base` one if there is one (`grep -n capture_base pkg/worker/*_test.go`).
- [ ] **Step 7:** `go build ./... && go test ./pkg/assets ./pkg/worker && golangci-lint run ./pkg/assets/... ./pkg/worker/...`. Expected: PASS, no new findings.
- [ ] **Step 8: Commit** `pkg/assets/capture_station*.go pkg/assets/testdata/cj_*.json pkg/assets/schema.sql pkg/worker/dispatch.go pkg/worker/schedule.go` (+ test file if added): "assets: hourly facility list + station passenger capture for the CJ console".

### Task 2: Zone severities (pkg/stationstate)

**Files:**
- Create: `pkg/stationstate/stationstate.go`, `pkg/stationstate/zones.go`, `pkg/stationstate/stationstate_test.go`

**Interfaces:**
- Consumes: `assets.StationReading`, `assets.FacilityRow` (Task 1).
- Produces:
  - `type Severity int` with `OK`, `Strained`, `Critical` and `func (Severity) String() string` → `"ok"|"strained"|"critical"`; `NoData` as a 4th value for "no reading".
  - `type Zone string` constants: `Core="core"`, `Life="life"`, `Fuel="fuel"`, `Production="production"`, `Tourism="tourism"`, `Shipyard="shipyard"`, `Defense="defense"`, `Commerce="commerce"`; `var Zones = []Zone{...}` in that order; `func (Zone) Label() string` → `CORE · POWER`, `LIFE SUPPORT`, `FUEL`, `PRODUCTION`, `TOURISM`, `SHIPYARD`, `DEFENSE`, `COMMERCE · ADMIN`.
  - `func ZoneOf(facilityType, category string) Zone` (zones.go). Table: core ← `stellar_collector_grid`, `bunker_fed_reactor`, any type containing `reactor` or `battery` except `point_defense_battery`; life ← `atmospheric_recycler`, `hydroponics*`/plants; fuel ← `fuel_tank_expansion`, `refueling_dock`, `h2_fuel_combustor`; shipyard ← `fabrication_yard`, `faction_ship_garage`; tourism ← `transit_lounge`, `rec_lounge`, any `*_lounge`/dining venue; defense ← category `defense` (`point_defense_battery`, `fire_control_center`, `reinforced_plating`, `shield_projector`); production ← remaining category `production`; everything else → commerce.
  - `type ZoneState struct { Zone Zone; Label string; Severity Severity; Brief string; Reasons []string; Facilities []assets.FacilityRow }`
  - `type Status struct { Overall Severity; Word string; Zones []ZoneState; ShieldShells int; Bars map[string]float64; CapturedAt time.Time; Stale bool }`. `Word` is `THRIVING`/`STRAINED`/`CRITICAL`/`NO DATA`. `Bars` keys: `hull`, `shield`, `fuel`, `power`, `life`, `satisfaction` (percent, 0 when the max is 0).
  - `func Evaluate(r assets.StationReading, ok bool, now time.Time) Status` — `Stale` when `now - CapturedAt > 2h`.
- Thresholds (pinned here, nowhere else): power draw/supply ≥70% strained, ≥90% critical; battery stored 0 critical **only when battery_capacity > 0**; life supply/demand <110% strained, <100% critical; fuel <40% strained, <15% critical; production strained when any production-zone line has `BacklogTicks > 0`, critical when all do; shield <60% strained, <25% critical (defense zone); hull <75% strained, <40% critical (defense zone); any facility with `MaintenanceSatisfied` false → its zone at least strained; tourism strained when `DemandLevel` is set and not `normal`, or `MarketConditions` contains `struggling`; `h2_fuel_combustor` backlog counts toward fuel, not production. `ShieldShells` = 1 + count of `shield_projector` facilities. Overall = worst zone; any `Reasons` line names the number (e.g. `fuel 39% < 40%`).

- [ ] **Step 1: Failing tests** in `stationstate_test.go` (table-driven over a `cj()` reading built from the Task 1 fixtures plus base: hull 85000/85000, shield 12000/12000, armor 60, dps 30, fuel 21293/55000, power 40 supply/0 draw, battery 0/0, life 12/10, satisfaction 100):
  - `TestEvaluate_CJTonight`: fuel zone `Strained` (38.7% < 40), tourism `Strained` (discounted + struggling), core `OK` (no battery built), overall `STRAINED`, `ShieldShells` 1, `Bars["fuel"]` ≈ 38.7.
  - `TestEvaluate_NoBatteryIsNotCritical`: battery 0/0 → core OK; battery 0/100 → core Critical.
  - `TestEvaluate_ZeroMaxes`: all max/capacity/demand 0 → no NaN or Inf in `Bars` (check with `math.IsNaN`/`IsInf`), no panic.
  - `TestEvaluate_NoData`: `Evaluate(assets.StationReading{}, false, now)` → `Overall NoData`, `Word "NO DATA"`.
  - `TestEvaluate_Stale`: CapturedAt now-3h → `Stale` true; now-1h → false.
  - `TestEvaluate_UnknownTypeMaintenance`: add `{Type:"quantum_spa", Category:"service", MaintenanceSatisfied:&false}` → `ZoneOf` commerce and commerce zone `Strained`.
  - `TestEvaluate_PowerCritical`: draw 38/40 → core Critical; 30/40 → Strained.
  - `TestEvaluate_ProductionBacklog`: one production line backlog 50 → Strained; all → Critical; only `h2_fuel_combustor` backlogged → production OK.
  - `TestEvaluate_ShieldShells`: two `shield_projector` rows → 3.
- [ ] **Step 2: Run** `go test ./pkg/stationstate`. Expected: FAIL, package missing.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./pkg/stationstate && golangci-lint run ./pkg/stationstate/...`. Expected: PASS, clean.
- [ ] **Step 5: Commit** `pkg/stationstate/`: "stationstate: CJ zone severities shared by play_as and the console".

### Task 3: play_as formatters

**Files:**
- Modify: `cmd/tools/play_as/base_format.go`, `cmd/tools/play_as/base_format_test.go`, `cmd/tools/play_as/passenger_format.go` (`formatStationPassengers`), its test file

**Interfaces:**
- Consumes: `assets.BaseSnapshotFrom`, `assets.FacilityRowsFrom`, `assets.PassengerSnapshotFrom`, `stationstate.Evaluate`.
- Produces: `func formatBase(raw []byte, facilityRaw []byte) string` (call site `main.go:894` passes `client.GetRawJSON("facility")`, which may be stale or nil; use it only when its `base_id` equals the get_base `base.id`).

Output shape for `get_base` (no JSON dump any more; colour via the file's existing ANSI helpers, green/yellow/red by severity):
```
Conjunction Junction — THRIVING (satisfaction 100%)        STRAINED
  Hull      [██████████] 85,000 / 85,000   armor 60
  Shield    [██████████] 12,000 / 12,000   1 shell
  Defense   30 dps · reach 1 · point_defense_battery L1, fire_control_center L1, reinforced_plating L1
  Power     [░░░░░░░░░░] draw 0 / 40 · battery none
  Life      12 / 10 (120%)
  Fuel      [████░░░░░░] 21,293 / 55,000 (39%)  ← fuel 39% < 40%
  Tourism   7,638 cr lifetime · leisure 2
  Production  (only with a matching facility list) one line per backlogged line: name, queued runs, backlog ticks
  Maintenance (only with a matching facility list) one line per facility with maintenance unmet
```
Rules: the condition word is the server's; the right-hand word is `stationstate` overall. Tourism rate is not shown here (it needs history).

`list_station_passengers` prepends, before the existing waiting list / "No passengers waiting" line:
```
Transit Lounge  0 / 20 seated
Demand          discounted · fares 0.6×
  "Conjunction Junction is well served by passenger carriers, … until it recovers."
```

- [ ] **Step 1: Failing tests:** update `base_format_test.go` (`TestFormatBase_CJSections`: contains `Hull`, `85,000 / 85,000`, `Shield`, `30 dps`, `21,293 / 55,000 (39%)`, `7,638 cr lifetime`, `STRAINED`; with the Task 1 facility fixture also contains `point_defense_battery L1`; with a facility raw for another base id it does not). Add `TestFormatStationPassengers_Lounge`: contains `0 / 20 seated`, `discounted · fares 0.6×`, `economy is struggling`, and still `No passengers waiting at Conjunction Junction`.
- [ ] **Step 2: Run** `go test ./cmd/tools/play_as -run 'FormatBase|FormatStationPassengers'`. Expected: FAIL.
- [ ] **Step 3: Implement**; update the call site.
- [ ] **Step 4: Run** the same plus `go build ./... && golangci-lint run ./cmd/tools/play_as/...`. Expected: PASS, clean.
- [ ] **Step 5: Commit** the four files: "play_as: sectioned get_base and lounge/demand header for list_station_passengers".

### Task 4: station-console server

**Files:**
- Create: `cmd/tools/station-console/main.go`, `cmd/tools/station-console/server.go`, `cmd/tools/station-console/server_test.go`, `cmd/tools/station-console/models.go` (model list), `cmd/tools/station-console/web/` (Task 5 fills it; this task ships a placeholder `index.html`)

**Interfaces:**
- Consumes: `assets.Open`-style store constructor used by other dashboards (check `cmd/tools/line-dashboard/main.go`), `Store.LatestStation`, `Store.StationHistory`, `stationstate.Evaluate`.
- Produces HTTP:
  - `GET /` → embedded `web/index.html`; `GET /static/*` → embedded `web/*`.
  - `GET /api/state` → `stationstate.Status` as JSON plus `"station":"Conjunction Junction"`, `"passengers"` (the snapshot), `"tourism_per_hour"` (number or null), `"base"` (the raw snapshot numbers the popups show).
  - `GET /api/history?hours=48` → `[]assets.HistoryPoint` (hours clamped 1–168).
  - `GET /api/models` → `[{"id":"player__wheel__capital_s5521","label":"Player wheel (capital)","default":true}, …]` for the nine spec models.
  - `GET /models/{id}/mesh.glb` and `/models/{id}/keyed.png` → files under `--models`; 404 for ids not in the list (no path traversal: look up by id, never join user input).
  - `GET /zones/{id}.json` → embedded `web/zones/{id}.json` (Task 5/6).
- Flags: `--addr :8094`, `--db data/assets.db`, `--models ~/spacemolt/kb/data/mesh_bakeoff/out-stations` (expand `~`), `--base c976fa7df53c87f9616165d7d411b41a`.

- [ ] **Step 1: Failing tests** (`httptest`, temp store seeded through `InsertBaseSnapshot` and Task 1 inserts):
  - `TestAPIState_NoData`: empty DB → 200, JSON `"Word":"NO DATA"`.
  - `TestAPIState_CJ`: seeded → `"Word":"STRAINED"`, 8 zones, `"tourism_per_hour"` null with one base row.
  - `TestAPIHistory_Clamp`: `hours=9999` behaves as 168; `hours=abc` → 400.
  - `TestModels_DefaultWheel`: 9 entries, exactly one `default`, it is `player__wheel__capital_s5521`.
  - `TestModelFile_RejectsUnknown`: `/models/..%2F..%2Fetc/mesh.glb` and `/models/nope/mesh.glb` → 404.
- [ ] **Step 2: Run** `go test ./cmd/tools/station-console`. Expected: FAIL.
- [ ] **Step 3: Implement.** Build to `bin/station-console` only.
- [ ] **Step 4: Run** tests + `go build -o bin/station-console ./cmd/tools/station-console && golangci-lint run ./cmd/tools/station-console/...`. Expected: PASS, clean.
- [ ] **Step 5: Commit** the directory: "station-console: JSON + model server for the CJ console on :8094".

### Task 5: Console page (hologram, labels, popups, trends, switcher, badge)

**Files:**
- Create: `cmd/tools/station-console/web/index.html`, `web/console.js`, `web/console.css`, `web/zones/player__wheel__capital_s5521.json`
- Reference: the spike `.superpowers/brainstorm/149828-1794193505/content/hologram-zones-v2.html` (rendering, label projection, hover focus, popup look)

**Interfaces:**
- Consumes: Task 4 endpoints.
- Produces: zone-map JSON format, used by Task 6:
```json
{ "model": "player__wheel__capital_s5521",
  "orient": {"upAxis": "thinnest", "tiltX": -0.35},
  "regions": [
    {"zone": "core",  "shape": "radial", "r": [0, 0.20]},
    {"zone": "life",  "shape": "radial", "r": [0.20, 0.40]},
    {"zone": "fuel",  "shape": "radial", "r": [0.40, 0.72]},
    {"zone": "production", "shape": "sector", "r": [0.72, 9], "a": [0, 0.2]},
    {"zone": "tourism",    "shape": "sector", "r": [0.72, 9], "a": [0.2, 0.4]},
    {"zone": "shipyard",   "shape": "sector", "r": [0.72, 9], "a": [0.4, 0.6]},
    {"zone": "defense",    "shape": "sector", "r": [0.72, 9], "a": [0.6, 0.8]},
    {"zone": "commerce",   "shape": "sector", "r": [0.72, 9], "a": [0.8, 1.0]}
  ],
  "labels": {"core": {"offset": [0, -150]}, "...": {}} }
```
  Shapes: `radial` (normalised distance from the axle, `r` range), `sector` (`r` range + angle fraction `a` around the axle), `band` (height range `h` along the up axis, for spindles/drums/terraces), `box` (`min`/`max` in normalised model space). First matching region wins; unmatched vertices are hull-only (grey-cyan, damage tint only). Anchor = centroid of the zone's vertices unless `labels[zone].anchor` is given.

Behaviour (from the spec; the spike is the reference implementation):
- Header: status word coloured by overall severity, station name/system, "last reading HH:MM (N min ago)", stale banner when `Stale`. Bottom bars from `Bars`. Corner badge: `/models/{id}/keyed.png` in the hologram tint.
- Hologram: EdgesGeometry 20°, additive per-vertex colours by zone severity (cyan/yellow/red), critical zones pulse; hull damage = scorched flicker on `1 - hull%` of vertices; `ShieldShells` icosahedron shells coloured by shield % (cyan→yellow→red), opacity scales with shield %, outer shells hidden first as shield drops, flicker below 25%; OrbitControls auto-rotate, drag, zoom.
- Labels: 8 zone tags with leader lines, `Label` + `Brief`; dim+dashed when OK, solid+coloured otherwise; hover → focus zone (others ×0.12); click → popup.
- Popup: zone label, `Reasons`, facility table (name, level, maintenance ok/unmet, and for production lines recipe, queued runs, backlog ticks, items/hr), tourism popup quotes `market_conditions` verbatim and shows lounge seats, demand, fare ×, tourism/hr; defense popup lists the defense facilities with levels, dps, reach, armor, shells. Trend sparklines (inline SVG, 48 h from `/api/history`) per zone: core power draw %, life margin %, fuel %, production backlog, tourism/hr, defense shield % and hull %.
- Switcher: dropdown from `/api/models`, default from the `default` flag, choice remembered in `localStorage` (wrapped in try/catch); loads `/zones/{id}.json`, falling back to a generic radial map when a model has none.
- Polls `/api/state` every 60 s and `/api/history` every 10 min; no full reload.

- [ ] **Step 1:** Write the page + wheel zone map. Keep JS in `console.js` with pure helpers (`zoneForVertex(map, x, y, z)`, `severityColor(sev)`) at the top.
- [ ] **Step 2: Verify** with `bin/station-console` against the live `data/assets.db`, using the run skill or Claude-in-Chrome: screenshot shows the wheel rotating, 8 labels, FUEL and TOURISM yellow (matches Task 2's CJ-tonight result), clicking TOURISM shows the market-conditions sentence, switching models loads each of the nine meshes without console errors. Expected: all true.
- [ ] **Step 3:** `go test ./cmd/tools/station-console` (embed still compiles). Expected: PASS.
- [ ] **Step 4: Commit** `cmd/tools/station-console/web/`: "station-console: hologram page with zones, labels, popups, trends, switcher".

### Task 6: Zone maps for the other eight models

**Files:**
- Create: `cmd/tools/station-console/web/zones/<id>.json` for `nebula__drum__moderate_s5230`, `solarian__spindle__moderate_s5060`, `crimson__ring__moderate_s5310`, `crimson__wheel__moderate_s5320`, `voidborn__pylon_toroid__moderate_s5440`, `voidborn__spindle__capital_s5461`, `player__drum__capital_s5531`, `player__terraces__capital_s5551`

- [ ] **Step 1:** For each, look at `view_side.png`/`view_td.png`/`view_tq.png` in the model folder and write a rough map with the Task 5 format: rings/wheels/toroids use `radial`+`sector` like the wheel; spindles and drums use `band` along the long axis (core in the middle band, life and fuel adjacent, the five outer zones as end bands/sectors); terraces use `box` tiers. All eight zones must appear in every map.
- [ ] **Step 2: Verify** in the browser, one model at a time: every zone colours some visible part of the model and every label's leader line lands on its zone. Expected: true for all eight (rough is fine; the wheel is the hand-tuned one).
- [ ] **Step 3:** Add `TestZoneMaps_AllZonesPresent` to `server_test.go`: every embedded `zones/*.json` parses, names its file's model, and has a region for each of the 8 zones. Run `go test ./cmd/tools/station-console`. Expected: PASS.
- [ ] **Step 4: Commit** the eight files + test: "station-console: rough zone maps for the other eight station models".

### Task 7: Go live (schedule, worker restart, runbook)

**Files:**
- Modify: `data/agents/miningbot_0000/schedule.json`, `docs/COLD_START.md`

- [ ] **Step 1:** `go build -o bin/worker ./cmd/worker` and `go build -o bin/station-console ./cmd/tools/station-console`.
- [ ] **Step 2:** In miningbot_0000's schedule: change id 51 `capture_base` from `daily` to `hourly`; add `hourly capture_facilities` and `hourly capture_station_passengers` (next free ids). Restart only miningbot_0000's worker so it loads the new binary (`kill -TERM` its `bin/worker --agent miningbot_0000` pid; the overmind respawns it; check it is connected first, never restart a disconnected worker).
- [ ] **Step 3: Verify** within ~70 min: `sqlite3 data/assets.db "select count(*) from facility_snapshots where base_id='c976fa7df53c87f9616165d7d411b41a'"` > 0, `station_passenger_snapshots` > 0, and two `base_snapshots` rows an hour apart. Expected: all true; `/api/state` shows a non-null `tourism_per_hour` after the second base row.
- [ ] **Step 4:** Launch `nohup bin/station-console > data/overmind/station-console.log 2>&1 &`; add the same line to `docs/COLD_START.md` in the dashboards section next to the :8093 line dashboard.
- [ ] **Step 5: Commit** `docs/COLD_START.md` and `data/agents/miningbot_0000/schedule.json` (named paths only): "station-console: hourly CJ captures live; console in cold start".
