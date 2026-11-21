# Recurring Station-Manager Demand Report Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An offline CLI, `bin/sm-demand`, that ranks recurring Station-Manager buy demand by restock rate × price and says what we could craft from CRFT faction storage to sell into it.

**Architecture:** Pure analysis in a new `pkg/smdemand` (series metrics, craft expansion, ranking) fed by thin read-only loaders over the knowledge DB and assets.db; `cmd/tools/sm-demand` wires flags, loaders and a text/JSON renderer.

**Tech Stack:** Go 1.24+, `database/sql` + `modernc.org/sqlite` (driver name `"sqlite"`), `pkg/navigation` (`JumpGraph`, `BFSJumps`, `JumpGraphFromConnections`).

**Spec:** `docs/superpowers/specs/2026-11-20-recurring-sm-demand-report-design.md`

## Global Constraints

- Read-only on every DB: open with `file:<path>?mode=ro`.
- No game login, no network, no writes; no schema migration.
- SM demand only: series built from `sm_qty` / `sm_best_price` columns.
- Primary measure `restock_per_day`; ranking by `est_cr_per_day = restock_per_day × typical_price`, desc.
- Sharp-drop threshold default `0.25` (`--fill-drop`); defaults `--min-presence 0.5`, `--min-fills 2`, `--days 14`, min `days_span ≥ 2` days.
- Craft expansion depth cap 4; single best recipe per item, never mixed; each item evaluated independently against the full pool.
- Facility-only recipes count iff some pool base has a `faction_facilities` row with `category='production'`; such rows are flagged `F`.
- CRFT faction id: resolve from KB `factions` where `tag='CRFT'` (live: `e727c0e918d994c72db2978fe5b18edc`); `--faction-id` overrides.
- Home system for jumps: `cloverfield`.
- Binary in `bin/`; `go build ./...`, `go test ./...`, `golangci-lint run` clean; TDD.
- Commit by explicit path only (never `git add -A`; data/*.json is dirty).

## Review Focus

1. A series with capture gaps (hours missing): a rise across the gap counts as restock, nothing counts as a drop across a gap of zero samples, and `max_gap` reports the longest gap — tested in Task 1.
2. A station_id that matches only `bases.poi_id`, not `bases.id` (18 of 46 live): must still resolve name and system — tested in Task 3.
3. An item whose recipe forms a cycle (A needs B, B's recipe needs A) or exceeds depth 4: expansion must terminate and report the item as not craftable via that path — tested in Task 2.
4. An item with SM demand but no recipe: shows `—` in CRAFT, no panic — tested in Task 2.
5. `sm_qty` falling to 0 then recovering (order fully filled, then restocked): the drop counts as one fill of the whole quantity, the recovery as restock, and presence excludes the zero hours — tested in Task 1.

---

### Task 1: Series metrics

**Files:**
- Create: `pkg/smdemand/series.go`
- Test: `pkg/smdemand/series_test.go`

**Interfaces:**
- Produces:
  ```go
  type Sample struct { Bucket time.Time; Qty, Price float64 }
  type Metrics struct {
      Hours int; DaysSpan float64; Presence float64; MaxGap time.Duration
      RestockUnits, RestockPerDay float64
      Fills int; FillsPerDay, TakenUnits float64
      TypicalQty, TypicalPrice float64
      EstCrPerDay float64
      CurrentQty, CurrentPrice float64; LastSeen time.Time
  }
  func Analyze(samples []Sample, fillDrop float64) Metrics // samples in any order; sorts a copy by Bucket
  ```

- [ ] **Step 1: Write failing tests**

`TestAnalyzeCreepAndSharpDrop`: hourly samples qty `2,6,7,9,17,20,4,8,14,16` price 100 each, `fillDrop=0.25`. Assert `RestockUnits == 4+1+2+8+3+4+6+2 == 30`, `Fills == 1`, `TakenUnits == 16`, `Hours == 10`, `Presence == 1`, `TypicalPrice == 100`, `CurrentQty == 16`, `DaysSpan == 9.0/24`, `EstCrPerDay == RestockPerDay*100`.

`TestAnalyzeSmallDropIsNoise`: qty `100,90,95` → `Fills == 0`, `TakenUnits == 0`, `RestockUnits == 5`.

`TestAnalyzeGapCountsRiseAsRestock`: buckets at h0 qty 10, h5 qty 30 → `RestockUnits == 20`, `MaxGap == 5h`, `Hours == 2`.

`TestAnalyzeFullFillThenRestock`: qty `50,0,0,40` → `Fills == 1`, `TakenUnits == 50`, `RestockUnits == 40`, `Presence == 0.5`, `TypicalQty == 45` (median of 50,40).

`TestAnalyzeEmpty`: nil samples → zero `Metrics`, no panic.

- [ ] **Step 2: Run** `go test ./pkg/smdemand/ -run Analyze` — expect FAIL (undefined: Analyze).

- [ ] **Step 3: Implement `Analyze`.** Consecutive-captured-sample comparison: rise → `RestockUnits += cur-prev`; drop with `prev > 0 && (prev-cur)/prev >= fillDrop` → `Fills++`, `TakenUnits += prev-cur`. `DaysSpan` = (last−first bucket) in days; per-day rates are 0 when `DaysSpan == 0`. Typical values are medians over samples with `Qty > 0`.

- [ ] **Step 4: Run** `go test ./pkg/smdemand/` — expect PASS.

- [ ] **Step 5: Commit** `git add pkg/smdemand/series.go pkg/smdemand/series_test.go && git commit -m "smdemand: series metrics — restock creep, sharp-drop fills"`

### Task 2: Craft expansion against a pool

**Files:**
- Create: `pkg/smdemand/craft.go`
- Test: `pkg/smdemand/craft_test.go`

**Interfaces:**
- Produces:
  ```go
  type Recipe struct { ID string; FacilityOnly bool; Inputs, Outputs map[string]int }
  type CraftResult struct {
      HasRecipe bool; RecipeID string; Units int; Depth int; Facility bool
      Missing map[string]int // base inputs short for TargetQty; nil when Units >= target
  }
  // byOutput: item_id -> recipes producing it. allowFacility gates FacilityOnly recipes.
  func Craftable(item string, target int, pool map[string]int, byOutput map[string][]Recipe, allowFacility bool) CraftResult
  ```

- [ ] **Step 1: Write failing tests**

`TestCraftableDirect`: recipe `r1` makes `steel_plate×1` from `iron_ore×5`; pool `iron_ore:52` → `Units == 10`, `Depth == 1`, `HasRecipe`.

`TestCraftableIntermediate`: `circuit_board×1` ← `copper_wiring×2 + silicon×1`; `copper_wiring×1` ← `copper_ore×3`; pool `copper_ore:30, silicon:20` → `Units == 5`, `Depth == 2`.

`TestCraftableUsesStockOfIntermediateFirst`: as above plus pool `copper_wiring:4` → `Units == 7` (4 wiring in stock + 30/3=10 crafted = 14 wiring → 7 boards).

`TestCraftablePicksBestSingleRecipe`: two recipes for X: `a×10 → X×1` and `b×1 → X×1`, pool `a:10, b:5` → `Units == 5`, `RecipeID` = the `b` recipe.

`TestCraftableFacilityGate`: only recipe is `FacilityOnly`; `allowFacility=false` → `Units == 0`, `HasRecipe == true`; `allowFacility=true` → `Units > 0`, `Facility == true`.

`TestCraftableNoRecipe`: unknown item → `HasRecipe == false`, `Units == 0`.

`TestCraftableCycleTerminates`: `A ← B×1`, `B ← A×1`, empty pool → returns `Units == 0` without hanging (run under `t.Deadline`-bounded context or just rely on test timeout).

`TestCraftableMissingForTarget`: direct `steel_plate ← iron_ore×5`, pool `iron_ore:20`, target 10 → `Units == 4`, `Missing == {"iron_ore": 30}`.

- [ ] **Step 2: Run** `go test ./pkg/smdemand/ -run Craftable` — expect FAIL.

- [ ] **Step 3: Implement `Craftable`.** Approach: for each candidate recipe of `item`, binary-search the largest `n` (0..upper bound from pool size) such that `feasible(item, n)` succeeds on a *copy* of the pool, where `feasible` consumes stock first and recursively crafts the shortfall through the first-preferred recipe of each intermediate (same best-recipe rule, decided by a one-level-greedy pick: the recipe whose inputs are most in stock), with depth ≤ 4 and a visiting-set to break cycles. Keep the recipe with the highest `n`; `Depth` = deepest level actually used. `Missing` = base-input shortfall from a dry run at `target` with shortfalls recorded instead of failing. Output quantity per run is `Outputs[item]`; runs = ceil(need/out).

- [ ] **Step 4: Run** `go test ./pkg/smdemand/` — expect PASS.

- [ ] **Step 5: Commit** `git add pkg/smdemand/craft.go pkg/smdemand/craft_test.go && git commit -m "smdemand: craft expansion from a storage pool"`

### Task 3: Loaders (KB + assets.db)

**Files:**
- Create: `pkg/smdemand/load.go`
- Test: `pkg/smdemand/load_test.go` (builds temp SQLite files with only the columns used)

**Interfaces:**
- Consumes: `Sample`, `Recipe` (Tasks 1–2).
- Produces:
  ```go
  type Key struct{ Station, Item string }
  type StationInfo struct{ Name, System string }
  type KBData struct {
      Series     map[Key][]Sample
      ItemNames  map[string]string
      Stations   map[string]StationInfo // keyed by BOTH bases.id and bases.poi_id
      ByOutput   map[string][]Recipe
      Graph      navigation.JumpGraph
      FactionID  string                 // CRFT, unless overridden
      Production map[string]bool        // base_id -> has a production facility
  }
  func LoadKB(ctx context.Context, kbPath string, since time.Time, factionID string) (*KBData, error)
  type Pool struct{ Items map[string]int; Bases []string; CapturedAt time.Time }
  func LoadPool(ctx context.Context, assetsPath, factionID string, bases []string) (Pool, error) // bases empty = all of the faction's bases
  ```

- [ ] **Step 1: Write failing tests**

`TestLoadKBResolvesStationByPOIID`: `bases(id='b1', poi_id='p1', name='Junction')`, `pois(id='p1', system_id='cloverfield')`; demand row `station_id='p1'` → `Stations["p1"] == {Junction, cloverfield}` and `Stations["b1"]` likewise.

`TestLoadKBSeriesSMOnly`: two demand rows for one (station,item), `sm_qty` 5 and 0 → series has 2 samples with `Qty` 5 and 0 (from `sm_qty`, not `total_qty`); rows with `bucket_utc` before `since` excluded.

`TestLoadKBFactionAndProduction`: `factions(faction_id='f1', tag='CRFT')`, `faction_facilities(faction_id='f1', base_id='b1', category='production')` → `FactionID=='f1'`, `Production["b1"]`.

`TestLoadKBRecipes`: one recipe with `facility_only=1`, 2 inputs, 1 output → `ByOutput[out][0]` matches.

`TestLoadPoolLatestCapturePerBase`: two captures for base b1 (older qty 1, newer qty 9) and one for b2 → `Items[x] == 9 + b2's`; `bases=["b1"]` limits to b1.

- [ ] **Step 2: Run** `go test ./pkg/smdemand/ -run Load` — expect FAIL.

- [ ] **Step 3: Implement.** Queries: `market_demand_history` (`station_id, item_id, item_name, bucket_utc, sm_qty, sm_best_price` where `bucket_utc >= ?`); `bases b LEFT JOIN pois p ON p.id=b.poi_id` for names/systems; `recipes`/`recipe_inputs`/`recipe_outputs` excluding `hidden=1`; `connections` → `navigation.JumpGraphFromConnections` (check its exact signature first); `factions WHERE tag='CRFT'` unless `factionID != ""`; `faction_facilities WHERE category='production'`. Pool: latest `captured_at` per `base_id` for the faction, summed. `bucket_utc` parse with `time.RFC3339`.

- [ ] **Step 4: Run** `go test ./pkg/smdemand/` — expect PASS.

- [ ] **Step 5: Commit** `git add pkg/smdemand/load.go pkg/smdemand/load_test.go && git commit -m "smdemand: read-only loaders for demand history, recipes, storage pool"`

### Task 4: Report assembly (rows, filters, ranking, item rollup)

**Files:**
- Create: `pkg/smdemand/report.go`
- Test: `pkg/smdemand/report_test.go`

**Interfaces:**
- Consumes: `Analyze`, `Craftable`, `KBData`, `Pool`.
- Produces:
  ```go
  type Options struct {
      FillDrop, MinPresence float64; MinFills int; MinDays float64
      Item, Station string; CraftableOnly bool; Limit int; Home string
  }
  type Row struct {
      Item, ItemName, Station, StationName, System string
      Jumps int // -1 unknown/unreachable
      Metrics
      Stock int; Craft CraftResult
  }
  type ItemRow struct {
      Item, ItemName string; Stations int; RestockPerDay, EstCrPerDay float64
      BestStation string; Stock int; Craft CraftResult
  }
  func BuildRows(kb *KBData, pool Pool, opt Options) []Row        // filtered, ranked by EstCrPerDay desc, Limit applied
  func RollupItems(rows []Row) []ItemRow                         // ranked by EstCrPerDay desc
  ```

- [ ] **Step 1: Write failing tests** (hand-built `KBData`/`Pool`, no DB)

`TestBuildRowsRanksByEstCrPerDay`: two series, higher restock×price first.

`TestBuildRowsFilters`: a series with presence 0.3 dropped at `MinPresence 0.5`; one with 1 fill dropped at `MinFills 2`; one spanning 1 day dropped at `MinDays 2`.

`TestBuildRowsFacilityAllowedOnlyWithProductionBase`: pool bases `[b1]` with `Production["b1"]` → facility recipe counts; pool `[b2]` without → `Craft.Units == 0`.

`TestBuildRowsJumps`: graph `cloverfield-a-b`, station in `b` → `Jumps == 2`; unknown system → `-1`.

`TestRollupItems`: same item at two stations sums `RestockPerDay`/`EstCrPerDay`, `Stations == 2`, `BestStation` = higher `EstCrPerDay`.

- [ ] **Step 2: Run** `go test ./pkg/smdemand/ -run 'BuildRows|Rollup'` — expect FAIL.

- [ ] **Step 3: Implement.** `Craftable` called once per distinct item (memoise) with `target = ceil(TypicalQty)`; `allowFacility` = any `pool.Bases` in `kb.Production`. Jumps via `navigation.BFSJumps(kb.Graph, opt.Home, systems)`, mapping `navigation.RouteInf` → -1. `Stock = pool.Items[item]`.

- [ ] **Step 4: Run** `go test ./pkg/smdemand/` — expect PASS.

- [ ] **Step 5: Commit** `git add pkg/smdemand/report.go pkg/smdemand/report_test.go && git commit -m "smdemand: rank recurring SM demand and roll up per item"`

### Task 5: CLI `cmd/tools/sm-demand`

**Files:**
- Create: `cmd/tools/sm-demand/main.go`, `cmd/tools/sm-demand/render.go`
- Test: `cmd/tools/sm-demand/render_test.go`

**Interfaces:**
- Consumes: `LoadKB`, `LoadPool`, `BuildRows`, `RollupItems`, `Row`, `ItemRow`.
- Produces: `func renderStations(w io.Writer, rows []smdemand.Row)`, `func renderItems(w io.Writer, rows []smdemand.ItemRow)`, `func renderFooter(w io.Writer, f footer)`.

Flags (spec, exact): `--kb-path data/spacemolt-knowledge.db`, `--assets-db-path data/assets.db`, `--days 14`, `--min-presence 0.5`, `--min-fills 2`, `--fill-drop 0.25`, `--base` (repeatable or comma list), `--item`, `--station`, `--by station|item` (default station), `--craftable-only`, `--limit 40`, `--json`, `--faction-id`.

- [ ] **Step 1: Write failing tests**

`TestRenderStationsColumns`: one row → output header contains `ITEM`, `STATION (SYSTEM)`, `JMP`, `PRES`, `RESTOCK/D`, `FILLS/D`, `TYP QTY`, `TYP PRICE`, `EST CR/D`, `NOW QTY@PRICE`, `STOCK`, `CRAFT`, `MISSING`; a facility craft renders as `840F`; no recipe renders `—`; missing renders `water_ice×1460`.

`TestRenderFooterCaveat`: footer contains the data window, station count, pool bases + capture time, and the sentence `a sharp drop may be a cancel or reprice, not a fill`.

- [ ] **Step 2: Run** `go test ./cmd/tools/sm-demand/` — expect FAIL.

- [ ] **Step 3: Implement** renderers with `text/tabwriter`; `main` parses flags, `since = now - days`, calls loaders and `BuildRows` (Home `"cloverfield"`), then text or `json.NewEncoder(os.Stdout)` of `[]Row` / `[]ItemRow`.

- [ ] **Step 4: Run** `go test ./cmd/tools/sm-demand/ ./pkg/smdemand/` — expect PASS.

- [ ] **Step 5: Live smoke.** `go build -o bin/sm-demand ./cmd/tools/sm-demand && ./bin/sm-demand --limit 20 && ./bin/sm-demand --by item --limit 20 && ./bin/sm-demand --json --limit 3 | head -40`. Expected: non-empty tables; footer shows 46-ish stations and CRFT bases incl. `c976fa7df53c87f9616165d7d411b41a`; sanity-check one top row against `sqlite3 -readonly` on its raw series.

- [ ] **Step 6: Full gate.** `go build ./... && go test ./... && golangci-lint run ./pkg/smdemand/... ./cmd/tools/sm-demand/...` — all clean.

- [ ] **Step 7: Commit** `git add cmd/tools/sm-demand/ && git commit -m "sm-demand: CLI report of recurring Station-Manager demand"`
