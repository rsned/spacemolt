# Item Production Cost Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cost every item's production (labor, rental fees, time; raw resources at 0), publish a per-item cost section on the KB, and generate a private local page that ranks empire Station Manager bids by profit per hour.

**Architecture:** The code lives in the KB repo (`~/spacemolt/kb`, module `github.com/rsned/spacemolt-kb`). A new package, `pkg/prodcost`, does three jobs:
- builds the routes for each recipe from the catalogs and the knowledge DB
- costs every item's tree
- ranks opportunities from `market.db`

One generator, `cmd/generate-production-costs`, renders:
- the public section into `kb/production-costs/`
- the private page into `private/production-costs/`, which is git-ignored

The private page re-totals in the browser through `kb/production-costs/prodcost.js`. A node test checks that script against goldens written by the Go engine.

**Tech Stack:** Go 1.25 (`modernc.org/sqlite`, `html/template`), plain JS (node:test for tests), SQLite read-only.

**Spec:** `~/spacemolt/spacemolt/docs/superpowers/specs/2026-11-02-item-production-cost-model-design.md`

## Global Constraints

**Scope and safety**
- Read-only against every DB: open with `?mode=ro`, as `cmd/generate-build-costs/main.go:openRO` does. Nothing calls the game.
- Public pages never contain market prices, opportunities, CRFT facilities, or toggled assumptions. The private output lives only in `private/production-costs/`, which is added to the KB `.gitignore`.

**Costing defaults**
- Raw resources = catalog items with a non-empty `extracted_by` (all 63 have category `ore`). They cost 0 credits and 0 time.
- Hand route: 0 credits per run, with ticks per run equal to the recipe's `crafting_time`.
- Facility route at level L: labor = that facility's catalog `labor_cost`.
  - Ticks per run = `crafting_time / (5 × 3^(L−1))`. Verified 11-02: assemble_reinforced_frame at empire levels 1–4 runs 3.96 / 1.32 / 0.44 / 0.147 ticks.
  - Output per run = the recipe's output quantity at **every** level.
- Empire route: labor + the empire `rental_fee_per_run`, which is uniform per recipe across empire stations (`owner_faction=''`).
- Faction line: labor + that line's own fee, with ticks per run taken from the line's captured `production.ticks_per_run`.
- Labor applies to every facility route. There is no facility route without labor.
- Usable routes, used to cost trees and opportunities: hand, empire, public faction lines, and CRFT's own facilities (`faction_facilities` rows whose faction tag is `CRFT`). Unbuilt own routes are information only.
- A catalog facility with a non-empty `empire` field gets no own route. A `pirate_base_only` facility's own route is tagged `pirate base only`.
- Items with no recipe that aren't raw are **unpriced**. A dependent item is marked "has an unpriced input" and shows a partial cost.
- By-products: a run's cost is assigned to `outputs[0]` only.
- The private page skips any recipe whose inputs include `energy_crystal`. The public page doesn't.

**Labels and display**
- Route labels, exactly: `hand`, `own L1 (labor 10)`, `empire L1 (labor 10 + fee 9)`, `<faction tag> L2 @ <station name> (labor 16 + fee 7)`. Own routes for `pirate_base_only` facilities get the suffix ` · pirate base only`.
- Highlight boxes, only on items with more than one route:
  - cheapest Credits/u: green, `outline:2px solid var(--up)`
  - fastest Time/u: blue, `outline:2px solid var(--link)`
  - both: `outline-offset:-3px; padding:4px 8px`

**Time and haul**
- 1 tick = 10 s.
- Haul origin: Conjunction Junction, base `c976fa7df53c87f9616165d7d411b41a`, system `cloverfield`.
- Jump ticks = `max(1, 7 − speed)`. Defaults: `-ship-speed 3`, `-hold 2000`.

**Build hygiene**
- `golangci-lint run ./...` clean, `go build ./...` and `go test ./...` pass, and `node --test tests/js/` passes. Binaries go in `bin/`.

## Review Focus

1. **A recipe whose facilities are all catalog-only (nobody captured a public line, no CRFT line).** Its only usable route may be hand, or none. If it has none, the item must show "no usable route", not cost 0. Test: `TestComputeNoUsableRoute` (Task 3).
2. **Cycles.** `electrolyze_water` makes hydrogen and oxygen, and some recipes consume their own family. Costing must end and mark the cycle route as such, not as 0 or infinite. Test: `TestComputeCycleTerminates` (Task 3).
3. **Unpriced inputs deep in the tree.** The partial flag and the unpriced item ids must reach the top item. Test: `TestComputeUnpricedPropagates` (Task 3).
4. **A bidding station unreachable from Cloverfield** (no path in `connections`). The opportunity is kept with Hours shown as `unreachable` and sorted last. It is not dropped, and it is never assigned 0 hours. Test: `TestOpportunitiesUnreachableStation` (Task 6).
5. **Buy-now quantity deeper than the book.** The price covers what's listed, and the rest is marked short, not priced at the last level forever. Test: `TestBuyNowShortBook` (Task 6).

---

### Task 1: Catalog loaders for recipes and the facility and item fields we need

**Files:**
- Modify: `pkg/catalog/catalog.go`
- Test: `pkg/catalog/catalog_test.go`

**Interfaces:**
- Produces:
  - `type Recipe struct { ID, Name, Category string; Inputs, Outputs []Material; CraftingTime float64; HandCraftable bool; ProducedBy []string }`, with ProducedBy taken from `produced_by_facility_ids`
  - `func LoadRecipes(root string) ([]Recipe, error)`
  - `Facility` gains `LaborCost int` (`labor_cost`), `Empire string` (`empire`) and `PirateBaseOnly bool` (`pirate_base_only`)
  - `Item` gains `ExtractedBy []string` (`extracted_by`) and `Size float64` (`size`)

- [ ] **Step 1: Write the failing test.** Write `TestLoadRecipes` against a temp root holding a snapshot dir with a two-recipe `catalog_recipes.json` in the `{"items":[...]}` shape. Assert that `refine_steel` has `CraftingTime == 0.54`, `Outputs[0] == {steel_plate, 2}`, `HandCraftable == false`, and `ProducedBy` contains `iron_refinery`. Extend the existing facility test fixture with `"labor_cost":10,"empire":"haven"` and assert both fields.
- [ ] **Step 2: Run it to see it fail.** Run `go test ./pkg/catalog/ -run 'TestLoadRecipes|TestLoadFacilities'`. Expected: FAIL, with `LoadRecipes` undefined.
- [ ] **Step 3: Implement.** Use the existing `loadCatalog(root, "catalog_recipes.json", &dst)`.
- [ ] **Step 4: Run it to see it pass.** Same command. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/catalog && git commit -m "catalog: recipes loader; facility labor/empire/pirate fields; item extracted_by/size"`

### Task 2: Routes for each recipe

**Files:**
- Create: `pkg/prodcost/types.go`, `pkg/prodcost/routes.go`
- Test: `pkg/prodcost/routes_test.go`

**Interfaces:**
- Consumes: `catalog.Recipe`, `catalog.Facility` (Task 1).
- Produces:

```go
type RouteKind string // "hand", "own", "empire", "faction", "crft"

type Line struct { // one captured public_facilities or CRFT faction_facilities row
    StationID, StationName, FactionID, FactionTag, RecipeID string
    Level int
    Fee float64         // rental_fee_per_run; 0 for crft
    TicksPerRun float64 // captured; 0 = unknown -> computed from level
    Empire bool         // owner_faction == ""
}

type Route struct {
    RecipeID string
    Kind RouteKind
    Level int
    Labor, Fee float64
    TicksPerRun, OutPerRun float64
    StationID, StationName, FactionTag string // empty for hand/own/empire
    Usable bool
    Tag string // "pirate base only" or ""
}

func (r Route) Label() string // exact formats in Global Constraints
func Routes(rec catalog.Recipe, facs map[string]catalog.Facility, lines []Line) []Route
```

Rules:
- **Hand:** one route when `HandCraftable`.
- **Own:** one route per producing facility level, `Usable=false`. Skip facilities with `Empire != ""`.
- **Empire:** one route per distinct level among empire lines, with labor from the catalog facility at that level and `Usable=true`.
- **Faction lines:** one route per non-empire line, `Usable=true`.
- **CRFT lines:** one per line, `Kind="crft"`, `Fee=0`, `Usable=true`.

- [ ] **Step 1: Write the failing tests.** Build fixtures from real values: refine_steel `crafting_time 0.54`, output 2, `iron_refinery` L1 labor 10, `industrial_forge` L2 labor 20, an empire L1 line with fee 9, and a faction line with fee 4 and captured ticks 0.083.
  - `TestRoutesHandAndEmpire`: basic_iron_smelting yields one `hand` route, 0 labor, `TicksPerRun 0.27`, `OutPerRun 1`.
  - `TestRoutesEmpireLabel`: the empire route's label is `empire L1 (labor 10 + fee 9)` and its `TicksPerRun` is `0.108`.
  - `TestRoutesOwnLevelSpeed`: own L2 has `TicksPerRun == 0.54/15` and `Usable == false`, with label `own L2 (labor 20)`.
  - `TestRoutesEmpireFacilityHasNoOwn`: a facility with `Empire:"haven"` produces no own route.
  - `TestRoutesPirateTag`: a `PirateBaseOnly` facility's own label ends with ` · pirate base only`.
  - `TestRoutesFactionUsesCapturedTicks`: the faction line's `TicksPerRun == 0.083`.
- [ ] **Step 2: Run them to see them fail.** Run `go test ./pkg/prodcost/ -run TestRoutes`. Expected: FAIL, because the package doesn't exist yet.
- [ ] **Step 3: Implement** `Routes` and `Label` in `routes.go`, and the types in `types.go`.
- [ ] **Step 4: Run them to see them pass.** Same command. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/prodcost && git commit -m "prodcost: routes per recipe (hand/own/empire/faction/crft) with labor+fee labels"`

### Task 3: The costing engine

**Files:**
- Create: `pkg/prodcost/engine.go`
- Test: `pkg/prodcost/engine_test.go`

**Interfaces:**
- Consumes: `Route` and `Routes` (Task 2), and the catalog types.
- Produces:

```go
type World struct {
    Items map[string]catalog.Item
    Recipes []catalog.Recipe
    Facilities map[string]catalog.Facility
    Lines []Line
}

type Options struct {
    SkipInputs map[string]bool // private: {"energy_crystal": true}
}

type InputLine struct {
    ItemID string
    QtyPerUnit float64 // input units per 1 output unit
    Via string         // chosen route label for that input; "raw" or "unpriced"
}

type RouteCost struct {
    Route
    CreditsPerUnit, TicksPerUnit float64 // whole tree; usable inputs on their cheapest route
    RawPerUnit float64                   // raw resource units per output unit (for mine-fuel)
    Partial bool
    Unpriced []string
    Cycle bool
    Inputs []InputLine
}

type ItemCost struct {
    ItemID string
    Routes []RouteCost  // every route of every recipe whose outputs[0] is the item, sorted credits asc then ticks asc
    Cheapest, Fastest int // indexes into Routes among Usable routes; -1 when none
    NoUsableRoute bool
}

func Compute(w World, opt Options) map[string]*ItemCost
```

Rules:
- Per unit, a route's own cost is `(Labor+Fee)/OutPerRun` credits and `TicksPerRun/OutPerRun` ticks.
- Each input adds `QtyPerUnit ×` that input's **cheapest usable** route values, credits first with ticks breaking ties.
- Raw inputs add 0 and add `QtyPerUnit` to `RawPerUnit`.
- Unpriced inputs set `Partial` and append to `Unpriced`.
- Memoised depth-first search with an on-stack set. An input already on the stack marks the route `Cycle` and contributes nothing, as `pkg/craftbrain`'s cycle-break does.
- Recipes containing a `SkipInputs` item are left out entirely.

- [ ] **Step 1: Write the failing tests.**
  - `TestComputeHandIsFree`: steel_plate via basic_iron_smelting gives `CreditsPerUnit 0` and `TicksPerUnit 0.27`.
  - `TestComputeEmpirePerUnit`: refine_steel at an empire L1 gives `(10+9)/2 = 9.5` credits and `0.054` ticks.
  - `TestComputeHighlights`: steel_plate has `Cheapest` on the hand route and `Fastest` on the faction line route.
  - `TestComputeInputsRollUp`: an item needing 3 copper_wiring (hand, 0.32 ticks) plus its own empire route of labor 12 + fee 325 gives `CreditsPerUnit 337` and `TicksPerUnit == own + 3×0.32`.
  - `TestComputeNoUsableRoute`: a facility-only recipe with no lines gives `NoUsableRoute` and `Cheapest == -1`, while its own routes are still listed.
  - `TestComputeCycleTerminates`: A needs B and B needs A. It returns, and one route has `Cycle`.
  - `TestComputeUnpricedPropagates`: X ← Y ← whale_oil (no recipe, not raw). X has `Partial` and `Unpriced == ["whale_oil"]`.
  - `TestComputeSkipInputs`: with `SkipInputs{energy_crystal}`, circuit_board shows no `fabricate_circuit_boards` route.
  - `TestComputeByproductPrimaryOnly`: electrolyze_water appears under hydrogen_gas only.
- [ ] **Step 2: Run them to see them fail.** Run `go test ./pkg/prodcost/ -run TestCompute`. Expected: FAIL, because `Compute` is undefined.
- [ ] **Step 3: Implement** `Compute` in `engine.go`.
- [ ] **Step 4: Run them to see them pass.** Run `go test ./pkg/prodcost/`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/prodcost && git commit -m "prodcost: tree costing engine with highlights, cycles, unpriced inputs"`

### Task 4: Load the world from the catalogs and the knowledge DB

**Files:**
- Create: `pkg/prodcost/load.go`
- Test: `pkg/prodcost/load_test.go`

**Interfaces:**
- Consumes: the catalog loaders (Task 1) and `World` (Task 3).
- Produces: `func LoadWorld(catalogRoot string, kdb *sql.DB, ourTag string) (World, error)`.
  - Lines come from `public_facilities`: `recipe_id`, `level`, `rental_fee_per_run`, `owner_faction`, `json_extract(details_json,'$.production.ticks_per_run')`, and the station name from `pois.name` keyed by `station_id`, falling back to the id.
  - CRFT lines come from `faction_facilities` joined to `factions` on `faction_id` where `factions.tag = ourTag`. Pass `""` for the public build so no CRFT lines are loaded.
  - Faction tags come from `factions.tag`, falling back to the first 6 characters of the id.

- [ ] **Step 1: Write the failing test.** `TestLoadWorldFromSQLite` creates a temp SQLite DB holding the four tables with the exact schemas from `~/spacemolt/spacemolt/data/spacemolt-knowledge.db` (copy them with `.schema`). It inserts one empire line, one faction line and one CRFT row. Assert:
  - `ourTag="CRFT"` yields 3 lines, the CRFT line with Kind `crft` once routed.
  - `ourTag=""` yields 2 lines.
  - The faction line's `TicksPerRun` is read from `details_json`.
- [ ] **Step 2: Run it to see it fail.** Run `go test ./pkg/prodcost/ -run TestLoadWorld`. Expected: FAIL.
- [ ] **Step 3: Implement** `LoadWorld`.
- [ ] **Step 4: Run it to see it pass.** Same command. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/prodcost && git commit -m "prodcost: load world from catalogs, public_facilities and CRFT faction_facilities"`

### Task 5: Public KB section

**Files:**
- Create: `cmd/generate-production-costs/main.go`, `render.go`, `templates/index.html.tmpl`, `templates/item.html.tmpl`
- Modify: `internal/kbnav/kbnav.go` (add `{"production-costs", "Production Costs"}` after Facilities), `README.md` (a generator table row), `.gitignore` (add `/private/`)
- Test: `cmd/generate-production-costs/render_test.go`

**Interfaces:**
- Consumes: `LoadWorld`, `Compute`, `ItemCost` and `Route.Label` (Tasks 2–4).
- Produces:
  - `func renderPublic(outDir string, w prodcost.World, costs map[string]*prodcost.ItemCost, ages DataAges) error`
  - `type DataAges struct{ Catalog, Facilities, Market string }`, reused by Task 7
  - flags: `-catalog` (default `../spacemolt/data/game-api`), `-knowledge` (default `spacemolt-knowledge.db`), `-market` (default `../spacemolt/data/market.db`), `-out` (default `kb/production-costs`), `-private-out` (default `private/production-costs`), `-our-tag` (default `CRFT`), `-ship-speed 3`, `-hold 2000`

Page content:
- **Index:** one table, columns Item · Recipe · Route · Out/run · Credits/u · Time/u.
  - Each item is a `<tbody>` whose first row is its Cheapest route; the other routes sit in a `<details>` row.
  - Boxes as in Global Constraints. Partial costs show `≥ N` plus a `has an unpriced input` note; `NoUsableRoute` shows `no usable route`.
  - Time/u is shown in ticks to 3 significant figures.
- **Item page** (`<item_id>.html`): one section per recipe, with its route table (Route · Labor · Fee · Run total · Ticks/run · Out/run · Credits/u · Time/u, with the item's Cheapest and Fastest cells boxed the same way as the index). Below it, the input tree with each input's `Via` label, then links to `../items/<category>/<id>.html` and `../build-costs/<id>.html`.
- Uses the KB header from `kbnav.Header` and the same theme variables as `cmd/generate-build-costs/templates/detail.html.tmpl`.

- [ ] **Step 1: Write the failing tests.**
  - `TestRenderPublicIndexHighlights`: render a two-route steel_plate world. The index HTML contains `empire L1 (labor 10 + fee 9)`, exactly one `class="hl-cheap"` and one `class="hl-fast"`, and no `Ask` or `Bid` column header.
  - `TestRenderPublicSingleRouteNoBox`: a one-route item has neither class.
  - `TestRenderPublicItemPage`: `plasma_injector.html` contains both recipe ids and `has an unpriced input` when partial.
  - `TestRenderPublicNoPrivateData`: rendered with a world containing a `crft` line, no output file contains `CRFT` or `crft`.
- [ ] **Step 2: Run them to see them fail.** Run `go test ./cmd/generate-production-costs/`. Expected: FAIL.
- [ ] **Step 3: Implement.** `main.go` builds the public world with `ourTag=""` and calls `renderPublic`, keeping the private world for Task 7. Run `go run ./cmd/generate-nav`.
- [ ] **Step 4: Run tests and check a live render.** Run `go test ./cmd/generate-production-costs/ && go run ./cmd/generate-production-costs`. Expected: PASS. `kb/production-costs/index.html` exists. `grep -c 'labor 10 + fee 9' kb/production-costs/steel_plate.html` is at least 1.
- [ ] **Step 5: Commit.** `git add cmd/generate-production-costs internal/kbnav kb/nav.js README.md .gitignore kb/production-costs && git commit -m "kb: production cost section — every route per item, cheapest/fastest boxed"`

### Task 6: Market inputs and opportunities

**Files:**
- Create: `pkg/prodcost/market.go`, `pkg/prodcost/opportunities.go`; Move: `cmd/generate-build-costs/hops.go` helpers into `pkg/galaxyhops/hops.go`, exporting `LoadSystemResolver`, `LoadConnections`, `BFSHops`, and updating generate-build-costs' callers.
- Test: `pkg/prodcost/market_test.go`, `pkg/prodcost/opportunities_test.go`, plus the moved `hops_test.go`.

**Interfaces:**
- Consumes: `ItemCost` and `RouteCost` (Task 3).
- Produces:

```go
type Level struct{ StationID string; Price, Qty float64 }

type Market struct {
    Asks map[string][]Level // item -> sell levels, price asc, latest capture per station
    Bids map[string][]Level // item -> source='station' buy levels, latest capture per station
    StationSystem map[string]string
}

func LoadMarket(mdb, kdb *sql.DB) (Market, error)

// BuyNow walks asks cheapest-first. avgPrice covers the filled part;
// short = qty - filled; haulJumps = jumps from the costliest filled level's station to home.
func BuyNow(m Market, item string, qty float64) (avgPrice, short float64, station string)

type Policy string // "cheapest" or "fastest"

type Opportunity struct {
    ItemID, StationID string
    Bid, Depth, CostPerUnit, MarginPerUnit, Total float64
    MakeHours, HaulHours, Hours, ProfitPerHour float64
    Unreachable bool
    RouteLabel string
    Policy Policy
    Partial bool
}

type HaulParams struct{ Home string; JumpTicks, Hold float64; Hops map[string]map[string]int }

func Opportunities(costs map[string]*ItemCost, w World, m Market, hp HaulParams) []Opportunity
```

Rules:
- One row per bid level per station.
- For each **usable** top-level route × policy ∈ {cheapest, fastest}, where policy chooses each input's route across the whole tree, compute profit per hour and keep the best. Add `func CostUnder(costs, w, itemID string, top Route, p Policy) (credits, ticks float64)` to engine.go for this.
- `MakeHours = Depth × ticks × 10 / 3600`.
- Trips = `ceil(Depth × Items[item].Size / Hold)`.
- Haul jumps per trip:
  - for hand: `2 × hops(home, bidder)`
  - for empire, faction or crft lines: `hops(home, F) + hops(F, bidder) + hops(bidder, home)`, where F is the line's station. For an empire route, F is the nearest empire station with that recipe line.
- `HaulHours = trips × jumps × JumpTicks × 10 / 3600`.
- Unreachable: any hop is missing. Sort last; never 0 hours.
- Sort by ProfitPerHour descending.

- [ ] **Step 1: Write the failing tests.**
  - `TestBuyNowWalksBook`: asks 10@5 and 10@8, qty 15, gives avg `(50+40)/15` and short 0.
  - `TestBuyNowShortBook`: qty 30 gives short 10.
  - `TestOpportunityMarginAndHours`: bid 2,230 × depth 12 for a hand-only item at 0 cost and 0.5 ticks/u, with the bidder 3 hops from home and JumpTicks 4. Expect `Total 26760`, `MakeHours == 12×0.5×10/3600`, and `HaulHours == 1×6×4×10/3600`.
  - `TestOpportunitiesPicksBestProfitPerHour`: an item with a slow free hand route and a fast empire route chooses the one with the higher ProfitPerHour, and `RouteLabel` reflects it.
  - `TestOpportunitiesUnreachableStation`: no path gives `Unreachable` and a sort position after every reachable row.
  - Moved hops tests pass under `pkg/galaxyhops`.
- [ ] **Step 2: Run them to see them fail.** Run `go test ./pkg/prodcost/ ./pkg/galaxyhops/`. Expected: FAIL.
- [ ] **Step 3: Implement** the move first, then run `go test ./cmd/generate-build-costs/` (expected: PASS), then `market.go` and `opportunities.go`.
- [ ] **Step 4: Run all of them to see them pass.** Run `go test ./pkg/... ./cmd/generate-build-costs/`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add pkg/galaxyhops pkg/prodcost cmd/generate-build-costs && git commit -m "prodcost: market books, buy-now, opportunities ranked by profit/hr; hops moved to pkg/galaxyhops"`

### Task 7: Private page with in-page recalculation

**Files:**
- Create: `cmd/generate-production-costs/private.go`, `templates/private.html.tmpl`, `kb/production-costs/prodcost.js`
- Create: `tests/js/prodcost.test.js`, `pkg/prodcost/testdata/js-golden.json`
- Test: `cmd/generate-production-costs/private_test.go`, `pkg/prodcost/golden_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `func renderPrivate(outDir string, w prodcost.World, costs map[string]*prodcost.ItemCost, m prodcost.Market, opps []prodcost.Opportunity, ages DataAges) error`, which writes `index.html` and `data.json`
  - `prodcost.js`, which exports for node with `module.exports = {total, rank}` and attaches to `window.ProdCost` in the browser, matching how `bom-explorer.js` exports:
    - `total(data, itemId, choices, settings) -> {credits, ticks, partial}`
    - `rank(data, choices, settings) -> opportunities[]`
  - `choices`: `{itemId: "mine"|"buy"|"hand"|"<route label>"}`
  - `settings`: `{rawMode: "mine"|"buy", haulFuelPerJump, mineFuelPerUnit, priceUnpricedAtAsk: bool}`

`data.json` holds, per item, every route's own per-run values (labor, fee, ticks, out, inputs), each item's buy-now average price, the bid levels, hops from home, and the haul parameters. The JS computes totals and haul trips the same way Go does.

Page sections:
1. **Opportunities:** sortable, with Route/Policy columns, an `unreachable` text, and a partial mark.
2. **Baseline:** the public columns plus Ask, Best bid, a `buy is cheaper` flag (ask < Credits/u of the cheapest usable route) and a `market has no price` flag.
3. **Item detail:** each step has a `<select>` with mine/buy/hand/route labels.
- A settings bar with every setting, and the age of each data source.

- [ ] **Step 1: Write the failing tests.**
  - `TestWriteJSGolden`: Go computes the default-setting totals for a fixed fixture world (the steel_plate, copper_wiring and plasma_injector fixtures from Task 3) and compares them to `testdata/js-golden.json`. Regenerate with `-update`.
  - `tests/js/prodcost.test.js` loads the same fixture data and golden. `total()` must match every golden item to 1e-9.
    - A test with `rawMode:"buy"` raises copper_wiring by 8 × the ore's buy-now price per unit.
    - A test with `haulFuelPerJump: 100` raises an opportunity's cost by `trips×jumps×100/depth`.
  - `TestRenderPrivateSections`: the output contains `Profit/hr`, `buy is cheaper`, `market has no price`, and a `<select` per tree step. No recipe containing `energy_crystal` appears in data.json.
- [ ] **Step 2: Run them to see them fail.** Run `go test ./pkg/prodcost/ ./cmd/generate-production-costs/ && node --test tests/js/prodcost.test.js`. Expected: FAIL.
- [ ] **Step 3: Implement** `renderPrivate`, the template, and `prodcost.js`. `main.go` builds the private world (`-our-tag`), with `Options{SkipInputs: energy_crystal}`, `LoadMarket` and `Opportunities`.
- [ ] **Step 4: Run them to see them pass.** Same command. Expected: PASS.
- [ ] **Step 5: Commit.** `git add cmd/generate-production-costs kb/production-costs/prodcost.js tests/js/prodcost.test.js pkg/prodcost && git commit -m "prodcost: private opportunities page with in-page recalculation"`

### Task 8: Live run and spot checks

**Files:**
- Modify: `README.md` (usage example), only if a check uncovers a fix.

- [ ] **Step 1: Run the generator live.** Run `go build -o bin/generate-production-costs ./cmd/generate-production-costs && bin/generate-production-costs`. Expected: exits 0 and prints item, route and opportunity counts plus the data ages.
- [ ] **Step 2: Check known values on the generated pages.**
  - steel_plate: green box on `hand` 0.
  - refine_steel empire route reads `empire L1 (labor 10 + fee 9)` at 9.5/u.
  - build_plasma_injector empire `labor 12 + fee 325`.
  - The private Opportunities table lists plasma_injector bids at ~2,230 with a route and hours.
  - `git status` shows nothing under `private/`.
- [ ] **Step 3: Run the full gate.** Run `golangci-lint run ./... && go test ./... && node --test tests/js/`. Expected: all clean.
- [ ] **Step 4: Commit any fixes found**, with tests first per TDD, then stop for the operator's look before any site publish.
