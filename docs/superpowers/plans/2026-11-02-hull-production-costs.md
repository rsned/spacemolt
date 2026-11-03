# Hull Production Costs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A private page listing all 370 hulls. Each row shows what the hull's build materials cost us to make, plus labor, royalty and license, next to the materials bought at the cheapest market asks and the cheapest finished-hull listing.

**Architecture:**
- **Two repos.**
  - The game client (`~/spacemolt/spacemolt`) starts recording every `commission_quote`, `commission_ship` and `buy_ship_license` reply into two new knowledge-DB tables.
  - The KB repo (`~/spacemolt/kb`) adds a third `prodcost.Compute` run with hull rules. A new `prodcost.CostHulls` turns its results into per-hull rows. `cmd/generate-production-costs` renders those rows as `private/production-costs/hulls.html`.
- **The DB is shared.** The KB's `spacemolt-knowledge.db` is a symlink to `~/spacemolt/spacemolt/data/spacemolt-knowledge.db`, so the generator reads the new tables directly.

**Tech Stack:** Go 1.24, `modernc.org/sqlite`, `html/template`, the existing `prodcost.js` page script.

**Spec:** `docs/superpowers/specs/2026-11-02-hull-production-costs-design.md` (spacemolt repo)

## Global Constraints

- Go 1.24+ idioms (range over int; `b.Loop()` in benchmarks).
- `golangci-lint run` adds no new findings in either repo. `go build ./...` and `go test ./...` pass in both.
- Built binaries go in `bin/`.
- Never edit an applied migration. The new schema is migration **66** (the latest is 65, `bases_type_and_wrecked`).
- The page is read-only. Nothing calls the game. The capture is passive and must never break the response path: log write errors and swallow them.
- Commit only the files the task touches. **Never `git add -A`**: the spacemolt repo has dirty `data/*.json`.
- Spec values used verbatim:
  - royalty = **10%** of computed material make cost
  - tier-1 labor **1,000**
  - license cost by `tier`: **T1 1M, T2 5M, T3 10M, T4 20M, T5 50M**
  - listing window **3 days**
  - unbuilt facilities costed at **L1** labor
  - seed license: congregation, faction `e727c0e918d994c72db2978fe5b18edc` (CRFT), tick **2301382**, cost **1,000,000**

## Review Focus

1. **A knowledge DB without the new tables:** for example, a run before migration 66 has applied. The page must still render, with labor unknown and no licenses owned, not fail. *(Task 7 test.)*
2. **A quote from a yard that isn't ours** has no "N royalty" text. Royalty must be recorded as NULL, not 0. *(Task 1 test, Task 2 test.)*
3. **Some quote replies arrive wrapped as an action_result** (`{"command":…,"result":{…}}`) and others flat. Both must be recorded. *(Task 2 test.)*
4. **A build material with no recipe that isn't salvage** must be flagged "unpriced input", never silently costed at 0. *(Task 6 test.)*
5. **Different quoted labor within one tier** must show as a range for unquoted classes in that tier, and the quoted value for quoted classes. *(Task 6 test.)*

---

### Task 1: Knowledge tables for shipyard quotes and licenses (spacemolt repo)

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go`: append migration 66 `shipyard_quotes_and_ship_licenses`.
- Create: `pkg/knowledge/shipyard.go`
- Test: `pkg/knowledge/shipyard_test.go`

**Interfaces:**
- Produces:
  ```go
  type ShipyardQuote struct {
      ShipClass, StationID, FactionID, AgentID, Mode string // Mode "quote" | "commission"
      Labor, MaterialCost, ProvideMaterialsTotal, YardMargin, ShipyardTierHere int
      Royalty *int // nil when the reply carried no royalty text
      Tick int64
      SeenAt time.Time
      Raw []byte
  }
  type ShipLicense struct {
      FactionID, ShipClass, Empire, AgentID string
      CostPaid int
      Tick int64
      SeenAt time.Time
  }
  type ShipyardRecorder interface {
      RecordShipyardQuote(ctx context.Context, q ShipyardQuote) error
      RecordShipLicense(ctx context.Context, l ShipLicense) error
  }
  ```
  `*SQLiteKB` implements `ShipyardRecorder`. `knowledge.Base` is not changed; callers type-assert, as with `BoardingRecorder`.
- **Schema:**
  - `shipyard_quotes`: `id` INTEGER PK, then the fields above, with `royalty` nullable, `raw` TEXT and `seen_at` TEXT (RFC3339). Index on `(ship_class)`.
  - `ship_licenses`: PK `(faction_id, ship_class)`. `INSERT OR REPLACE` keeps the latest purchase.
  - Migration 66's `fn` seeds the congregation license from Global Constraints with `INSERT OR IGNORE`: empire `outerrim`, agent `craftsman-1`, seen_at `2026-11-02T19:54:20Z`.

- [ ] **Step 1: Write failing tests**
  - `TestMigration66SeedsCongregationLicense`: fresh DB, then one row in `ship_licenses` with ship_class `congregation`, cost_paid 1000000, tick 2301382.
  - `TestRecordShipyardQuoteNullRoyalty`: record a quote with `Royalty: nil`. Reading it back gives a NULL `royalty` (`sql.NullInt64.Valid == false`).
  - `TestRecordShipyardQuoteRoundTrip`: Labor 1000, Royalty 734, Mode "quote", and every field survives.
  - `TestRecordShipLicenseReplaces`: recording the same faction and class twice leaves one row with the second tick.
- [ ] **Step 2: Run** `go test ./pkg/knowledge -run 'Migration66|ShipyardQuote|ShipLicense'`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** the migration, the types and the two methods in `shipyard.go`.
- [ ] **Step 4: Run** the same command, then `go test ./pkg/knowledge`. Expected: PASS.
- [ ] **Step 5: Commit**
  ```bash
  git add pkg/knowledge/sqlite_migrations.go pkg/knowledge/shipyard.go pkg/knowledge/shipyard_test.go
  git commit -m "knowledge: shipyard_quotes + ship_licenses tables (migration 66), seed congregation license"
  ```

### Task 2: Game client shipyard observer (spacemolt repo)

**Files:**
- Create: `pkg/game/observed_shipyard.go`
- Modify: `pkg/game/client.go`: a `shipyardObserver` field plus mutex next to `passengerObserver`, and a call to `c.notifyShipyardFromPayload(resp.Payload)` next to **both** existing `c.notifyPassengersFromPayload(resp.Payload)` calls (~lines 5350 and 5797).
- Test: `pkg/game/observed_shipyard_test.go`

**Interfaces:**
- Produces:
  ```go
  type ObservedShipyard struct {
      Kind string // "quote" | "commission" | "license"
      ShipClass, Empire string
      Labor, MaterialCost, ProvideMaterialsTotal, YardMargin, ShipyardTierHere, CostPaid int
      Royalty *int
      BaseID, FactionID string // from state: Player.DockedAtBase, Player.FactionID
      Tick int64               // c.currentTick()
      SeenAt time.Time
      Raw []byte               // the JSON of the matched object
  }
  type ShipyardObserver func(ObservedShipyard)
  func (c *Client) SetShipyardObserver(fn ShipyardObserver)
  func (c *Client) ShipyardObserver() ShipyardObserver
  func parseRoyalty(texts ...string) *int // first match of `(\d+) royalty` across texts
  ```
- **Detection** runs on the payload, and on `payload["result"]` when that is an object:
  - **quote:** has `can_commission` and `ship_class`.
  - **commission:** has `commission_id` and `ship_class`.
  - **license:** has `cost_paid`, `ship_class` and `royalty_percent`.
- **Royalty** is parsed from `blockers[]` and `message`. For example, "treasury pays 1000 labor + 734 royalty" gives 734.
- **`yard_margin` is not in the serverapi struct.** Read it from the map (the 11-02 quote carries `"yard_margin":11975`).
- The observer short-circuits when it's nil, and is called without holding `c.mu`.

- [ ] **Step 1: Write failing tests**, using the three 11-02 payloads verbatim from `data/stations/cloverfield/events.jsonl` (quote line 1653, commission and license line 1646) as fixtures:
  - `TestShipyardObserverQuote`: Kind "quote", Labor 1000, Royalty 734, YardMargin 11975, ProvideMaterialsTotal 12975, ShipyardTierHere 2.
  - `TestShipyardObserverCommissionWrappedInResult`: payload `{"command":"commission_ship","result":{…}}` gives Kind "commission", Labor 1000, Royalty 723.
  - `TestShipyardObserverLicense`: Kind "license", CostPaid 1000000, Empire "outerrim".
  - `TestShipyardObserverNoRoyaltyText`: a quote whose message has no "royalty" gives `Royalty == nil`.
  - `TestShipyardObserverIgnoresOtherPayloads`: a `browse_ships` payload doesn't fire the observer.
- [ ] **Step 2: Run** `go test ./pkg/game -run ShipyardObserver`. Expected: FAIL.
- [ ] **Step 3: Implement** `observed_shipyard.go` and the two call sites.
- [ ] **Step 4: Run** the same command, then `go test ./pkg/game`. Expected: PASS.
- [ ] **Step 5: Commit**
  ```bash
  git add pkg/game/observed_shipyard.go pkg/game/observed_shipyard_test.go pkg/game/client.go
  git commit -m "game: shipyard observer for commission_quote/commission_ship/buy_ship_license replies"
  ```

### Task 3: Wire the observer to the KB in every client (spacemolt repo)

**Files:**
- Create: `pkg/agent/shipyard_capture.go`, test `pkg/agent/shipyard_capture_test.go`
- Modify:
  - `cmd/tools/play_as/main.go`: next to `agent.WireBoardingObservers(c, sqliteKB)` (~line 260)
  - `cmd/worker/main.go`: ~line 424
  - `cmd/auto-explorer/main.go`: ~line 1581

**Interfaces:**
- Consumes: `game.ObservedShipyard`, `(*game.Client).SetShipyardObserver` (Task 2), `knowledge.ShipyardRecorder` and its row types (Task 1).
- Produces: `func WireShipyardObserver(c *game.Client, kb knowledge.Base, agentID string)`.
  - **Mapping:** Kind "quote" or "commission" goes to `RecordShipyardQuote` with Mode = Kind and StationID = BaseID. Kind "license" goes to `RecordShipLicense`.
  - **Guards:** no-op when `c`/`kb` is nil or `kb` isn't a `ShipyardRecorder`.

- [ ] **Step 1: Write failing tests**
  - `TestWireShipyardObserverRecordsQuote`: a fake recorder receives one quote with Labor 1000 when the observer is invoked.
  - `TestWireShipyardObserverSkipsNonRecorder`: a KB without the interface leaves `c.ShipyardObserver() == nil`.
- [ ] **Step 2: Run** `go test ./pkg/agent -run WireShipyard`. Expected: FAIL.
- [ ] **Step 3: Implement** it and add the three wiring calls.
- [ ] **Step 4: Run** `go build ./... && go test ./pkg/agent ./cmd/...`. Expected: PASS.
- [ ] **Step 5: Lint and commit**
  ```bash
  golangci-lint run ./pkg/knowledge/... ./pkg/game/... ./pkg/agent/... ./cmd/...
  git add pkg/agent/shipyard_capture.go pkg/agent/shipyard_capture_test.go cmd/tools/play_as/main.go cmd/worker/main.go cmd/auto-explorer/main.go
  git commit -m "agent: record shipyard quotes and licenses from play_as, workers, auto-explorer"
  ```

### Task 4: Catalog ship fields (KB repo)

**Files:**
- Modify: `pkg/catalog/catalog.go`, the `Ship` struct
- Test: `pkg/catalog/catalog_test.go`

**Interfaces:**
- Produces: these new `Ship` fields with their JSON tags:
  ```go
  Tier          int    `json:"tier"`
  Faction       string `json:"faction"`
  CargoCapacity int    `json:"cargo_capacity"`
  StarterShip   bool   `json:"starter_ship"`
  PrestigeLock  string `json:"prestige_lock"`
  NPCRole       string `json:"npc_role"`
  CapturePolicy string `json:"capture_policy"`
  ```

- [ ] **Step 1: Write the failing test** `TestLoadShipsHullFields`. Extend the test fixture with a ship carrying all seven fields, and assert each one round-trips.
- [ ] **Step 2: Run** `go test ./pkg/catalog -run HullFields`. Expected: FAIL.
- [ ] **Step 3: Add** the fields.
- [ ] **Step 4: Run** `go test ./pkg/catalog ./...`. Expected: PASS (existing generators are unaffected).
- [ ] **Step 5: Commit** `git add pkg/catalog && git commit -m "catalog: ship tier, faction, cargo and licensing fields"`

### Task 5: Compute option for unbuilt facilities at L1 (KB repo)

**Files:**
- Modify: `pkg/prodcost/engine.go` (`Options`, and `item()` where `Routes(...)` results are iterated)
- Test: `pkg/prodcost/engine_test.go`

**Interfaces:**
- Produces: `Options.UnbuiltL1 bool`. When true, an own route is treated as `Usable` if its `Level` is the lowest own level among that recipe's own routes. This happens in the engine, not in `Routes()`, so the item pages are unchanged. Higher own levels stay unusable.
- **Consumers tell such a step** by `RouteCost.Kind == KindOwn && RouteCost.Usable`. No other route kind can be an unusable own route.

- [ ] **Step 1: Write failing tests**
  - `TestUnbuiltL1MakesFacilityOnlyItemCostable`: a fixture recipe that is facility-only, with own L1 labor 40 and L2 labor 80, and no lines. With `Options{}` the item has `NoUsableRoute`. With `Options{UnbuiltL1: true}` its cheapest route has Kind own, Level 1, and CreditsPerUnit 40 ÷ out.
  - `TestUnbuiltL1DoesNotChangeDefault`: the existing golden output (`golden_test.go`) is identical with `Options{}`. Run the golden test unchanged.
- [ ] **Step 2: Run** `go test ./pkg/prodcost -run UnbuiltL1`. Expected: FAIL.
- [ ] **Step 3: Implement** it.
- [ ] **Step 4: Run** `go test ./pkg/prodcost`. Expected: PASS, golden included.
- [ ] **Step 5: Commit** `git add pkg/prodcost/engine.go pkg/prodcost/engine_test.go && git commit -m "prodcost: Options.UnbuiltL1 costs facility-only steps at lowest own level"`

### Task 6: Per-hull costing (KB repo)

**Files:**
- Create: `pkg/prodcost/hulls.go`
- Test: `pkg/prodcost/hulls_test.go`

**Interfaces:**
- Consumes:
  - `Compute` results run with `Options{UnbuiltL1: true}` and **no SkipInputs** (Task 5)
  - `catalog.Ship` (Task 4)
  - `Market` and `BuyNow` (existing)
  - the loader types from Task 7, declared here
- Produces:
  ```go
  type HullFlag string
  const (
      FlagCrystal  HullFlag = "uses energy crystal"
      FlagUnbuilt  HullFlag = "facility not built"
      FlagSalvage  HullFlag = "needs salvage"
      FlagUnpriced HullFlag = "unpriced input"
      FlagCycle    HullFlag = "cycle"
      FlagLaborUnknown HullFlag = "labor unknown"
  )
  type Quote struct{ ShipClass string; Tier, Labor int; Royalty *int; StationID, Mode string; Tick int64 }
  type HullData struct {
      Quotes   []Quote
      Licensed map[string]bool          // ship class -> CRFT owns its license
      Listings map[string]ListingMin    // ship class -> cheapest in window
  }
  type ListingMin struct{ Price, Count int; StationID string }
  type HullMaterial struct {
      ItemID string; Qty float64
      Route string                 // chosen route label, "raw" or "unpriced"
      CreditsPerUnit, Credits float64
      Flags []HullFlag             // flags found anywhere in this material's tree
      AskPrice, AskShort float64; AskStation string // BuyNow for Qty
  }
  type LicenseState string // "owned" | "not owned" | "not licensable" | "not buildable"
  type HullCost struct {
      Ship catalog.Ship
      Materials []HullMaterial
      MaterialCredits float64   // sum of Credits; a floor when Partial
      Partial bool              // a salvage/unpriced/cycle step contributed nothing
      Labor *float64            // nil = unknown
      LaborNote string          // "quoted" | "tier quote" | "tier range 1,000–1,400" | ""
      Royalty float64           // 0.10 * MaterialCredits
      QuotedRoyalty *int        // latest recorded royalty for this class, for the gap note
      MakeTotal float64         // MaterialCredits + Royalty + (*Labor or 0)
      License LicenseState; LicenseCost int
      MarketCredits float64; MarketShort bool // sum of AskPrice*Qty; any AskShort>0 or no ask
      Listing *ListingMin
      Flags []HullFlag          // union over materials, plus FlagLaborUnknown
      Best bool                 // MakeTotal < MarketCredits && (Listing==nil || MakeTotal < Listing.Price); false if Labor nil
  }
  func CostHulls(ships []catalog.Ship, w World, costs map[string]*ItemCost, m Market, d HullData) []HullCost
  func LicenseCost(tier int) int // 1→1_000_000, 2→5_000_000, 3→10_000_000, 4→20_000_000, 5→50_000_000, else 0
  ```
- **Tree walk** (per material, recursive over `costs[item].Routes[Cheapest]` inputs, with a visited set):
  - a route whose recipe inputs include `energy_crystal` adds FlagCrystal
  - a usable own route adds FlagUnbuilt
  - an item with no recipe and no `ExtractedBy` adds FlagSalvage if its id is `salvage_components`, `salvage_metal` or `rare_salvage`, else FlagUnpriced
  - `Cycle` adds FlagCycle
- **Labor:**
  - Use the latest quote for this class if one exists.
  - Otherwise use the quotes at the same `Ship.Tier`: if they're all equal, that value with "tier quote"; if they differ, the minimum with LaborNote "tier range a–b".
  - Otherwise nil, plus FlagLaborUnknown.
  - Tier-0 `reclaim_only` hulls get Labor nil and no FlagLaborUnknown (not buildable).
- **License state:**
  - `CapturePolicy == "reclaim_only"` → not buildable.
  - `PrestigeLock != ""`, `StarterShip` or `NPCRole == "boss_flagship"` → not licensable.
  - `Faction != ""` → owned or not owned, with LicenseCost(Tier).
  - Otherwise → not licensable.

- [ ] **Step 1: Write failing tests** with a fixture world that rebuilds the Congregation's worked example:
  - aluminum_sheet: facility route at 8/u (empire labor + fee = 16 per run, out 2)
  - flex_polymer: hand from raw
  - cargo_container: hand, 3 sheet + 5 steel
  - fuel_tank: hand, with titanium_alloy as a faction line at 37/u
  - hull_plating: hand, 4 steel + 1 titanium_alloy
  - steel_plate: hand from raw

  Tests:
  - `TestCostHullsCongregationWorkedExample`:
    - MaterialCredits 984
    - per-material Credits 400, 0, 288, 185, 111
    - Royalty 98.4
    - with quote {congregation, tier 1, Labor 1000, Royalty 723}: Labor 1000, LaborNote "quoted", MakeTotal 2082.4, QuotedRoyalty 723
    - Licensed congregation: License "owned", LicenseCost 1_000_000
  - `TestCostHullsFlagsCrystalAndUnbuilt`: a material whose only recipe uses energy_crystal and runs only in an unbuilt facility gets both flags, and its Credits include the L1 labor.
  - `TestCostHullsSalvageIsFlaggedFloor`: build_materials with salvage_metal ×200 gives FlagSalvage, Partial true, and salvage contributes 0.
  - `TestCostHullsUnpricedNonSalvage`: a material with no recipe called `mystery_part` gives FlagUnpriced, not FlagSalvage.
  - `TestCostHullsLaborTierRange`: quotes {a, T2, 2000} and {b, T2, 2400}; class c at T2 gets Labor 2000 with LaborNote "tier range 2,000–2,400"; class a gets 2000 "quoted".
  - `TestCostHullsLaborUnknown`: no quotes at T3 gives Labor nil, FlagLaborUnknown, and Best false.
  - `TestCostHullsLicenseStates`: reclaim_only → not buildable; PrestigeLock → not licensable; boss_flagship → not licensable; pirate faction → not owned with LicenseCost(tier).
  - `TestLicenseCost`: tiers 1–5 give the spec's values, and 0 and 6 give 0.
- [ ] **Step 2: Run** `go test ./pkg/prodcost -run 'CostHulls|LicenseCost'`. Expected: FAIL.
- [ ] **Step 3: Implement** `hulls.go`.
- [ ] **Step 4: Run** `go test ./pkg/prodcost`. Expected: PASS.
- [ ] **Step 5: Commit** `git add pkg/prodcost/hulls.go pkg/prodcost/hulls_test.go && git commit -m "prodcost: CostHulls — per-hull make cost, flags, labor, royalty, license, market"`

### Task 7: Load hull data from the knowledge DB (KB repo)

**Files:**
- Create: `pkg/prodcost/hullload.go`
- Test: `pkg/prodcost/hullload_test.go`

**Interfaces:**
- Consumes: the `HullData`, `Quote` and `ListingMin` types (Task 6). The spacemolt tables `shipyard_quotes` and `ship_licenses` (Task 1), with their column names exactly as created there. `ship_listings` (`class_id`, `price`, `station_id`, `captured_at`).
- Produces: `func LoadHullData(kdb *sql.DB, ourTag string, tierOf map[string]int, now time.Time) (HullData, error)`.
  - Our faction id is resolved the same way as `LoadWorld` (`factions.tag`).
  - `Quote.Tier` comes from `tierOf[ship_class]`.
  - Listings use `captured_at > now - 72h`.
  - **A missing table** (`no such table`) gives empty data for that part with no error. Any other error is returned.

- [ ] **Step 1: Write failing tests**
  - `TestLoadHullDataMissingTables`: a DB with only `factions` and `ship_listings` gives empty Quotes and Licensed, and nil error.
  - `TestLoadHullDataReadsAll`: one quote with NULL royalty and one with 723, the CRFT license for congregation, plus listings 12202 and 13000 inside the window and 9000 outside it. Expect: Quotes length 2 with Royalty nil and 723, `Licensed["congregation"]`, and `Listings["congregation"] == {12202, 2, …}`.
- [ ] **Step 2: Run** `go test ./pkg/prodcost -run LoadHullData`. Expected: FAIL.
- [ ] **Step 3: Implement** it.
- [ ] **Step 4: Run** `go test ./pkg/prodcost`. Expected: PASS.
- [ ] **Step 5: Commit** `git add pkg/prodcost/hullload.go pkg/prodcost/hullload_test.go && git commit -m "prodcost: LoadHullData (quotes, licenses, 3-day listing minimums)"`

### Task 8: Render the hull page (KB repo)

**Files:**
- Create: `cmd/generate-production-costs/hulls.go`, `cmd/generate-production-costs/templates/hulls.html.tmpl`
- Modify:
  - `cmd/generate-production-costs/main.go`: a third world/compute run plus `renderHulls`
  - `templates/private.html.tmpl`: a link to `hulls.html`
  - `render.go`: add `Ships` to `DataAges` only if needed for the age line
- Test: `cmd/generate-production-costs/hulls_test.go`

**Interfaces:**
- Consumes: `CostHulls`, `LoadHullData` and `HullCost` (Tasks 6–7). `catalog.LoadShips`. `PageItem` names from `prodcost.PageData` for material display names.
- Produces: `func renderHulls(outDir string, hulls []prodcost.HullCost, items map[string]catalog.Item, ages DataAges) error`, which writes `outDir/hulls.html` beside the private `index.html`.
- **main.go:**
  - `hw` = the same private world as `pw`.
  - `hcosts := prodcost.Compute(hw, prodcost.Options{UnbuiltL1: true})`. This run has no crystal skip. The existing private run keeps its crystal skip unchanged.
  - Load the ships, call `LoadHullData(kdb, *ourTag, tierOf, time.Now())`, then `CostHulls`, then `renderHulls(*privateOut, …)`.
  - Log the hull count, the count of partial hulls and the count with labor unknown.
- **Page:**
  - **One row per hull** with the spec's columns: Hull · Tier · Empire · Cargo · Materials · Labor · Royalty · Make total · License · Materials at market · Cheapest listing · Flags.
  - **Materials** gets a "≥" prefix when Partial.
  - **Labor** shows "?" when nil, or the value with LaborNote as a tooltip.
  - **Make total** gets a box when `Best`.
  - **Royalty** gets a note when QuotedRoyalty is present and differs ("server charged 723").
  - **Each row expands** (`<details>`) to a materials table: qty, route, cost/u, total, flags, and ask price @ station.
  - **Material links:** each material links to the private item page's anchor for it. Where the material carries FlagCrystal it links to the public item page instead (the private page skips crystals). Use the same base path the private page already uses for public links.
  - **Sorting:** reuse `prodcost.js`'s column-sort handler if it's generic. Otherwise add a small inline sort over `data-sort` attributes.
  - **Ages:** show catalog, facilities, market and listings ages.

- [ ] **Step 1: Write failing tests** with a fixture of three hulls (the Congregation example from Task 6, one partial salvage hull, one labor-unknown hull):
  - `TestRenderHullsRowsAndFlags`: the output contains "congregation", "984", "2,082", "owned", "≥" on the salvage row, "?" on the unknown-labor row, and "needs salvage".
  - `TestRenderHullsBestBox`: only the Congregation row has the best-box class.
  - `TestRenderHullsCrystalLinksPublic`: a material with FlagCrystal links to the public item page path, and others link to the private anchor.
  - `TestRenderHullsNotPublished`: `renderPublic` output (existing helper `renderTo`) contains no "hulls.html" and no hull rows.
- [ ] **Step 2: Run** `go test ./cmd/generate-production-costs -run RenderHulls`. Expected: FAIL.
- [ ] **Step 3: Implement** `hulls.go`, the template, the main wiring and the link from `private.html.tmpl`.
- [ ] **Step 4: Run** `go test ./... && golangci-lint run`. Expected: PASS, no new findings.
- [ ] **Step 5: Commit**
  ```bash
  git add cmd/generate-production-costs/hulls.go cmd/generate-production-costs/hulls_test.go cmd/generate-production-costs/templates/hulls.html.tmpl cmd/generate-production-costs/templates/private.html.tmpl cmd/generate-production-costs/main.go cmd/generate-production-costs/render.go
  git commit -m "production-costs: private hull costs page"
  ```

### Task 9: End-to-end run and check against the worked example

**Files:** none; this is verification only.

- [ ] **Step 1:** In the spacemolt repo, run `go build -o bin/play_as ./cmd/tools/play_as && go build ./... && go test ./...`. Expected: PASS. Migration 66 applies on the next KB open, so check with `sqlite3 data/spacemolt-knowledge.db "select ship_class,cost_paid from ship_licenses"` → `congregation|1000000`. If nothing has opened the DB yet, open it once with the built binary or a test helper.
- [ ] **Step 2:** In the KB repo, run `go run ./cmd/generate-production-costs`. Expected log: hull count 370.
- [ ] **Step 3:** Check `private/production-costs/hulls.html`:
  - The Congregation row shows materials 984, royalty 98, labor 1,000 (quoted), make total about 2,082, and License owned.
  - Materials at market is within ±15% of 6,053, since prices move.
  - The cheapest listing is present.
  - The 42 salvage hulls are marked "≥" with "needs salvage".
  - Every other hull's materials show no "≥".

  Report the actual counts. Any non-salvage partial is a defect to fix before finishing.
- [ ] **Step 4:** Push nothing without the operator. Report the commits in both repos.
