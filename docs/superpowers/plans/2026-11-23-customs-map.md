# Customs Map Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Learn from captured action-log events which systems customs inspects, and route contraband-carrying ships around them.

**Architecture:** `pkg/customs` derives a per-system map from `assets.db` `action_log_events` and writes `data/overmind/customs-monitored.json`. A tool and an hourly worker verb refresh it. Autopilot reads the JSON per call. When the hold carries KB-category `contraband`, autopilot merges the monitored systems into its existing avoid/detour path. When no detour exists, it fails with `ErrNoCustomsFreeRoute` and records an alert.

**Tech Stack:** Go 1.24+, database/sql + modernc/mattn sqlite (whichever `pkg/assets` already uses), existing `navigation.ShortestPath` detour.

**Spec:** `docs/superpowers/specs/2026-11-23-customs-map-design.md`

## Global Constraints

- No `time.Sleep` in pkg/worker; no new sleeps at all. Use the `settle` seam if a wait is needed.
- No KB migration; `assets.db` is read only.
- `data_json` values are all strings; keys are verified: inspection `{system_id, poi_id, empire_id, outcome}`, seizure `{system_id, empire_id, seized_items}`, jump `{from_system, to_system}`.
- Defaults: window 14 days, `ClearMinArrivals` 10, `ClearAfterArrivals` 20.
- File paths: output `data/overmind/customs-monitored.json`, overrides `data/overmind/customs-overrides.json`, alerts `data/overmind/customs-alerts.json`.
- Clean ships (no contraband aboard) see zero behavior change. A missing or unreadable map means zero behavior change.
- Every change passes `go build ./...`, `go test ./...` and `golangci-lint run` with no new findings.

## Review Focus

1. **Contraband aboard, but origin or destination is monitored.** Never refuse for that reason. Exclude both from the customs avoid set and log a warning. Test in Task 4.
2. **Autopilot on a ship whose KB is a stub (`fakeKB` with a nil embedded `Base`) and which has cargo.** It must not panic. The map is loaded first, and GetItem is called only when the map is non-empty. Test in Task 4.
3. **Hourly retry of a blocked contraband task.** The first failure logs the full line and writes an alert. Repeats for the same agent and destination log one short line. Test in Task 4.
4. **A system with exactly one old inspection followed by 20 clean arrivals.** It must classify as observed clear, not monitored. Test in Task 1.
5. **Overrides naming a system with no evidence at all.** It still appears in the map with the forced class. Test in Task 2.

---

### Task 1: `pkg/customs` Build and classification

**Files:**
- Create: `pkg/customs/build.go`
- Test: `pkg/customs/build_test.go`

**Interfaces:**
- Produces:
  ```go
  type Class string
  const (Monitored Class = "monitored"; Clear Class = "clear"; Unknown Class = "unknown")
  type SystemStat struct {
      Empire         string    `json:"empire,omitempty"`
      Class          Class     `json:"class"`
      Arrivals       int       `json:"arrivals"`
      Inspections    int       `json:"inspections"`
      Seizures       int       `json:"seizures"`
      ArrivalsSince  int       `json:"arrivals_since_last_inspection"`
      LastInspection time.Time `json:"last_inspection,omitzero"`
      SeizedItems    []string  `json:"seized_items,omitempty"`
      Reason         string    `json:"reason,omitempty"` // override reason
  }
  type Options struct { Window time.Duration; ClearMinArrivals, ClearAfterArrivals int; EmpireOf func(systemID string) string }
  func DefaultOptions() Options
  func Build(ctx context.Context, db *sql.DB, now time.Time, o Options) (map[string]SystemStat, error)
  ```
- Rules:
  - Count only events with `created_at` ≥ now-Window.
  - A system is Monitored when its window inspections > 0 and `ArrivalsSince` < ClearAfterArrivals.
  - Otherwise it is Clear when Arrivals ≥ ClearMinArrivals; anything else is Unknown.
  - Empire comes from the inspection `empire_id` when present, else from `EmpireOf` (nil-safe).
  - Only systems with at least one arrival or inspection appear.

- [ ] **Step 1: Write the failing tests.** Use a helper `testDB(t)` that opens `:memory:` and creates `action_log_events(player_id, event_id, event_type, category, created_at, data_json)` (copy the column list from `pkg/assets/schema.sql`), plus `addEvent(t, db, typ, at, map[string]string)`.
  - `TestBuildMonitoredFromRecentInspection`: 1 inspection in grumium 2 days ago plus 3 arrivals → Monitored, Inspections 1, Empire "solarian".
  - `TestBuildAgesOutAfterCleanArrivals`: 1 inspection 3 days ago, then 20 arrivals after it → Clear, ArrivalsSince 20.
  - `TestBuildIgnoresEventsOutsideWindow`: an inspection 15 days ago plus 12 recent arrivals → Clear.
  - `TestBuildUnknownBelowMinArrivals`: 4 arrivals, no inspections → Unknown.
  - `TestBuildCountsSeizuresAndItems`: a seizure with `seized_items` `["3x Void Dust"]` → Seizures 1, SeizedItems contains "3x Void Dust".
- [ ] **Step 2:** Run `go test ./pkg/customs/ -run TestBuild -v`. Expected: FAIL (package/functions undefined).
- [ ] **Step 3:** Implement `Build`. Use one query per event type, filtered by `event_type` and `created_at`, and `json_extract` for the fields. `seized_items` is a JSON array string inside `data_json`; decode it with `encoding/json`.
- [ ] **Step 4:** Run `go test ./pkg/customs/ -v`. Expected: PASS.
- [ ] **Step 5:** Commit: `customs: derive per-system customs map from captured action-log events`.

### Task 2: Overrides, JSON write and load

**Files:**
- Create: `pkg/customs/file.go`
- Test: `pkg/customs/file_test.go`
- Create: `data/overmind/customs-overrides.json`

**Interfaces:**
- Consumes: `SystemStat`, `Class` (Task 1).
- Produces:
  ```go
  const DefaultMapPath = "data/overmind/customs-monitored.json"
  const DefaultOverridesPath = "data/overmind/customs-overrides.json"
  type Overrides struct { Monitored map[string]string `json:"monitored"`; Clear map[string]string `json:"clear"` }
  type Map struct { GeneratedAt time.Time `json:"generated_at"`; WindowDays int `json:"window_days"`; Systems map[string]SystemStat `json:"systems"` }
  func LoadOverrides(path string) (Overrides, error)   // missing file → zero value, nil
  func Apply(stats map[string]SystemStat, o Overrides)  // forced class + Reason; adds absent systems
  func Write(path string, m Map) error                 // temp file + rename
  func Load(path string) (Map, error)                  // missing → zero Map, error wraps fs.ErrNotExist
  func (m Map) Monitored() map[string]string           // lowercased id → "customs: N inspections, last <date>"
  ```
- Seed the overrides file with:
  `{"monitored":{},"clear":{"windmere":"v0.734.1: Crimson customs Rampart only","blood_forge":"v0.734.1: Crimson customs Rampart only","frostfeld":"v0.734.1: Crimson customs Rampart only"}}`

- [ ] **Step 1: Write the failing tests.**
  - `TestApplyForcesClassAndAddsUnknownSystem`: an override with `clear:{windmere}` on a Monitored windmere → Clear with that Reason. `monitored:{timberline}` with no stat → the entry exists and is Monitored.
  - `TestWriteLoadRoundTrip`.
  - `TestLoadMissingFile`: `errors.Is(err, fs.ErrNotExist)`.
  - `TestMonitoredListsOnlyMonitored`.
- [ ] **Step 2:** Run `go test ./pkg/customs/ -v`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./pkg/customs/ -v`. Expected: PASS.
- [ ] **Step 5:** Commit: `customs: overrides file, atomic map write/load`.

### Task 3: `customs-map` tool and hourly worker verb (Phase A complete)

**Files:**
- Create: `cmd/tools/customs-map/main.go`
- Modify: `pkg/worker/dispatch.go`, adding a `case "customs_map":` beside `capture_action_log` (around line 950).
- Test: `pkg/worker/customs_map_test.go`
- Modify: one always-on CJ agent's `data/agents/<agent>/schedule.json`. Use craftsman-3, which runs `capture_intel`; check that it already has `capture_action_log`, otherwise use miningbot_0000. Add `hourly customs_map` with id = max+1.

**Interfaces:**
- Consumes: `customs.Build`, `Apply`, `LoadOverrides`, `Write`, `DefaultOptions` (Tasks 1–2).
- Produces:
  - `func RefreshCustomsMap(ctx context.Context, db *sql.DB, kb knowledge.Base, mapPath, overridesPath string, now time.Time) (customs.Map, error)` in `pkg/worker/customs_map.go`. It is shared by the verb and the tool. `EmpireOf` uses `kb.GetSystem(...).Empire` and is nil-safe on a nil kb.
  - The verb is `customs_map`. With `d.Assets == nil` it is a no-op (same as other captures). Otherwise it calls `RefreshCustomsMap` with the default paths and prints `customs_map: N monitored, M clear, K unknown`.
  - The tool is `customs-map build|report [-assets data/assets.db] [-kb data/spacemolt-knowledge.db]`. `report` prints, per empire, a "monitored" list (system, inspections, last date, seizures) and a "clear" list (system, arrivals). An "alerts" section lists `customs-alerts.json` entries (Task 4) when that file exists.

- [ ] **Step 1: Write the failing test.** `TestRefreshCustomsMapWritesFile`: a temp DB with 1 inspection plus a temp overrides file with `clear` for a second system → the file exists, `Load` returns both, and the classes match.
- [ ] **Step 2:** Run `go test ./pkg/worker/ -run TestRefreshCustomsMap -v`. Expected: FAIL.
- [ ] **Step 3:** Implement `RefreshCustomsMap`, the verb and the tool.
- [ ] **Step 4:** Run `go test ./pkg/worker/ -run TestRefreshCustomsMap -v`. Expected: PASS. Then run `go build -o bin/customs-map ./cmd/tools/customs-map && bin/customs-map build && bin/customs-map report`. Expected: grumium, the_levy and gold_run are listed as monitored; windmere is clear (override).
- [ ] **Step 5:** Add the schedule task and commit only the code, the overrides file and that one schedule.json (`git add` the explicit paths, never `-A`). Message: `customs: customs-map tool + hourly customs_map verb`.

### Task 4: Contraband routing in autopilot (Phase B)

**Files:**
- Create: `pkg/worker/customs_route.go`
- Modify: `pkg/worker/autopilot.go`. Add the `AutopilotDeps` fields, and merge into the avoid block at about lines 306–350.
- Test: `pkg/worker/customs_route_test.go`

**Interfaces:**
- Consumes: `customs.Load`, `customs.Map.Monitored`, `customs.DefaultMapPath`.
- Produces:
  - `var ErrNoCustomsFreeRoute = fmt.Errorf("%w: no customs-free route", ErrRouteThroughAvoided)`, so `errors.Is` matches both.
  - `AutopilotDeps.CustomsMapPath string` (empty → `customs.DefaultMapPath`) and `AutopilotDeps.CustomsAlertsPath string` (empty → `data/overmind/customs-alerts.json`).
  - `func contrabandAboard(ctx context.Context, kb knowledge.Base, cargo []game.CargoItem) []string`. It returns the contraband item ids (with quantities, for the log) whose `kb.GetItem(id).Category == "contraband"`. A nil kb → nil.
  - `func customsAvoid(ctx, deps, client, origin, dest string, out io.Writer) map[string]string`. Load the map; on error or an empty map, return nil, and do this before touching the KB. If the map has entries, check the contraband aboard; with none aboard, return nil. Otherwise return Monitored() minus origin and dest (both lowercased). If dest or origin was monitored, log `customs: <sys> is monitored but is the route endpoint; flying anyway`. Always log `customs: contraband aboard (<items>); avoiding N monitored systems: a,b,c`.
  - Alerts file `{"alerts":{"<agent>|<dest>":{"first_seen":..., "last_seen":..., "count":N, "monitored":[...]}}}`. Use `recordCustomsAlert(path, agent, dest string, monitored []string, now time.Time) (first bool)`. The agent id is `client.GetState().Player.Username`, or empty if unavailable. Get the time from `deps` or `time.Now` (no sleeps).
- Merge rule in autopilot:
  - Merge `customsAvoid(...)` into `avoid` exactly as `ExtraAvoid` is merged.
  - When the detour fails and any hit is in the customs set:
    - Return `fmt.Errorf("%w: to %s (monitored on every path: %s)", ErrNoCustomsFreeRoute, ...)`.
    - When `recordCustomsAlert` returns first=true, log the full line `customs: no customs-free route to <dest> (monitored on every path: ...)`. Otherwise log `customs: still no customs-free route to <dest> (alert x<count>)`.

- [ ] **Step 1: Write the failing tests.** Reuse `avoidKB` and `izarRouteFake` from `autopilot_avoid_test.go`. Wrap the KB in `itemKB{*fakeKB; items map[string]string}`, overriding `GetItem` to return `{Category: items[id]}`. Write the map with `customs.Write` to `t.TempDir()`. Set `AvoidSystemsPath` to a nonexistent temp path.
  - `TestContrabandDetoursAroundMonitoredSystem`: izar monitored, cargo `void_dust` x3 (contraband), detour exists → jumps `[tidewater vega rigel]`.
  - `TestCleanCargoIgnoresCustomsMap`: same setup but cargo `iron_ore` → jumps `[tidewater izar rigel]`.
  - `TestContrabandNoDetourFailsWithAlert`: no detour → `errors.Is(err, ErrNoCustomsFreeRoute)` and `errors.Is(err, ErrRouteThroughAvoided)`; no jumps; the alerts file has count 1. A second call → count 2, and its output does NOT contain "no customs-free route to" (the short line instead).
  - `TestContrabandMonitoredDestinationStillFlies`: rigel monitored → jumps `[tidewater izar rigel]`, and the output contains "route endpoint".
  - `TestMissingCustomsMapNoKBCalls`: plain `fakeKB` (nil Base, GetItem would panic), cargo non-empty, map path missing → flies `[tidewater izar rigel]` without panicking.
- [ ] **Step 2:** Run `go test ./pkg/worker/ -run 'Contraband|CustomsMap' -v`. Expected: FAIL.
- [ ] **Step 3:** Implement `customs_route.go` and the autopilot merge.
- [ ] **Step 4:** Run `go test ./pkg/worker/ -v -run 'Contraband|CustomsMap|Autopilot'`, then `go test ./...` and `golangci-lint run ./pkg/customs/... ./pkg/worker/... ./cmd/tools/customs-map/...`. Expected: all PASS, no new lint.
- [ ] **Step 5:** Commit: `autopilot: route contraband around monitored customs systems; ErrNoCustomsFreeRoute + alerts`.

### Task 5: Rollout

- [ ] **Step 1:** `go build -o bin/worker ./cmd/worker && go build -o bin/customs-map ./cmd/tools/customs-map`. Run `bin/customs-map build` once so the map exists before any worker reads it.
- [ ] **Step 2:** Do NOT mass-restart workers. Restart only the agent that got the `customs_map` schedule task, with a TERM (it is connected). Other workers pick up contraband routing on their next natural restart. Note this in memory.
- [ ] **Step 3:** Watch for the first `customs_map:` log line (hourly) and any `customs:` lines from contraband carriers (the moonshine line and the nerve_burn / void_dust restock haulers).
- [ ] **Step 4:** Memory:
  - `reference_customs_mechanics.md`: live truth = `bin/customs-map report`; overrides file; alerts file.
  - `MEMORY.md` index line.
  - Then `make memory-sync`.
