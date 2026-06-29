# Haul Auto Ship-Replacement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the haul standing behavior the ability to auto-replace a destroyed/sub-spec hauler — buy a pilotable, high-utility-slot hull, strip non-cargo utility modules, fit cargo expanders, and re-insure — funded by the agent's own credits (insurance payout), self-healing after any future loss.

**Architecture:** A new `pkg/worker/rebuy.go` holds pure selection/decision helpers plus a `MaybeReplaceShip` orchestrator, wired into `Haul()` as an early-pass check before opportunity claiming. Procurement is a query against data the resident marketbots already collect: hull *availability* from the KB `ship_listings` table, cargo-expander availability from `market.db` sell orders. Full hull *specs* (tier, utility slots, cargo, piloting requirement) come from `kb.GetShipClasses(ctx)`. When the local shipyard can stock a qualifying hull the agent buys+refits in place; otherwise it autopilots one hop toward a station that does.

**Catalog data source (verified 2026-06-28):** `kb.GetShipClasses(ctx)` reads `FROM ships` (the catalog table is named `ships`, NOT `ship_classes`). On the live worker KB it returns **320 populated ship classes** with the fields the plan needs (`tier`, `utility_slots`, `cargo_capacity`, `piloting_required`, `starter_ship`, `price`). It is **stale**, though: the latest `data/game-api/latest/catalog_ships.json` has **331** items. Task 0 refreshes it. The `cmd/data/import-catalog-ships` tool reads `catalog_ships.json` (wrapper `ShipsResponse{Items []ShipClassJSON}`) → `kb.StoreShipClasses` → `ships` table (DELETE-then-insert; default DB `data/spacemolt-knowledge.db`).

**Tech Stack:** Go 1.24; `pkg/worker` (haul behavior, autopilot, dispatch); `pkg/knowledge` (SQLite KB: ship_listings, catalog ship_classes); `pkg/market` (market.db sell-order queries); `pkg/game` (GameClient).

## Global Constraints

- Target Go 1.24+; use `b.Loop()` in any benchmark, range-over-int where natural.
- All new code must pass `golangci-lint` with zero new findings; run it after each task.
- Run `go build ./...` and `go test ./...` before each commit.
- Any sleep/pause MUST use a predefined constant from `pkg/game/constants.go` (`SleepQuick`, `SleepShort`, `SleepDock`, …). Do not introduce raw `time.Sleep(literal)`.
- Do NOT assume server/KB struct field names — read the struct before coding against it (project rule).
- Compiled binaries go in `bin/`, never the repo root.
- Commit after every task with a `feat(worker/haul):` / `feat(knowledge):` scoped message.
- The feature is OFF unless `HaulDeps.Rebuy.Enable` is true (safe rollout; haul behavior unchanged when disabled).

## Verified Interfaces (read before starting)

- `HaulDeps` (`pkg/worker/haul.go`): has `Client game.GameClient`, `KB knowledge.Base`, `Market OpportunityStore`, `Out io.Writer`, `AgentID string`, `Now func() time.Time`. This plan ADDS a `Rebuy RebuyConfig` field.
- `knowledge.ShipClassDef` (`pkg/knowledge/catalog.go`): `ID, Name string; Tier int; Price int; CargoCapacity int; UtilitySlots int; StarterShip bool; PilotingRequired int; RequiredSkills map[string]int`. Query: `kb.GetShipClasses(ctx) ([]ShipClassDef, error)`.
- `knowledge.ShipListing` (`pkg/knowledge/base.go:138`): `ShipClass, ShipName string; BasePrice float64; CargoSpace, ModuleSlots, UtilitySlots, WeaponSlots int`.
- `knowledge.ShipListings`: `{ SystemID, SystemName, StationID, StationName string; GameTick int64; Listings []ShipListing }`. Query: `kb.GetLatestShipListings(ctx, systemID, stationID) (*ShipListings, error)`. **No cross-station query exists — Task 4 adds `GetAllLatestShipListings`.**
- `serverapi.ShipListingDetail` (live `browse_ships` item): `ListingID, ClassID string; Tier, Price int`. Used at the destination to get the buyable `ListingID`.
- `game.Skill` (`pkg/game/types.go`): `{ Level int; XP float64 }`; player skills at `state.Player.Skills map[string]Skill`. (Verify the piloting key — likely `"piloting"` — via Task 1's read step.)
- GameClient (`pkg/game/interface.go`): `BrowseShips(ctx, payload map[string]any) error`, `BuyListedShip(ctx, listingID string) error`, `SwitchShip(ctx, shipID string) error`, `UninstallMod(ctx, moduleID string) error`, `InstallMod(ctx, moduleID string) error`, `BuyInsurance(ctx, ticks int) error`. Raw responses fetched via `client.GetRawJSON(key)` (see `capture.go` for the pattern).
- `market.Collector.GetItemStationPrices(ctx, itemID) ([]ItemStationPrice, error)`; `ItemStationPrice` has `StationID, SystemID string; BestAsk float64; AskQty float64; HasSell bool`.
- `worker.Autopilot(ctx, AutopilotDeps, targetSystem, targetPOI string) error` (`pkg/worker/autopilot.go`) for routing.

## File Structure

- **Create** `pkg/worker/rebuy.go` — `RebuyConfig`, `HullTarget`, `HullLocation`; pure helpers (`pilotingLevel`, `currentUtilitySlots`, `SelectHaulerHull`, `NeedsReplacement`, `FindHullStations`, `FindExpanderStations`); orchestrators (`ReplaceAndRefit`, `MaybeReplaceShip`).
- **Create** `pkg/worker/rebuy_test.go` — unit tests for every pure helper + a fake-client orchestration test.
- **Modify** `pkg/knowledge/base.go` — add `GetAllLatestShipListings(ctx) ([]ShipListings, error)` to the `Base` interface.
- **Modify** `pkg/knowledge/sqlite.go` — implement it for `SQLiteKB`.
- **Modify** `pkg/knowledge/memory.go` — implement it for `MemoryKB`.
- **Modify** `pkg/worker/haul.go` — add `Rebuy RebuyConfig` to `HaulDeps`; call `MaybeReplaceShip` early in `Haul()`.
- **Modify** roster/role wiring (`cmd/worker/main.go` or roles config) — populate `HaulDeps.Rebuy` from a default config (Task 9).

---

### Task 0 (Prerequisite): Refresh + modernize the catalog import

Run this FIRST — the rest of the plan reads `kb.GetShipClasses`, which is stale (320 vs 331). This refreshes the `ships` table from the latest catalog and makes future refreshes one command. Not behavior code, so no unit test; verified by a row count.

**Files:**
- Modify: `cmd/data/import-catalog-ships/main.go`

**Interfaces:**
- Produces: a populated/refreshed `ships` table (331 rows) read by `kb.GetShipClasses(ctx)` in later tasks. No Go API changes.

- [ ] **Step 1: Read the tool.** Open `cmd/data/import-catalog-ships/main.go`. Confirm: it takes the catalog JSON path as a positional arg (`os.Args[1]`), parses `ShipsResponse{Items []ShipClassJSON}`, and writes via `kb.StoreShipClasses` to DB `data/spacemolt-knowledge.db` (override env `SPACEMOLT_DB`). Confirm `ShipClassJSON`'s tags cover the fields the plan needs (`tier`, `utility_slots`, `cargo_capacity`, `piloting_required`, `starter_ship`) — they do as of 2026-06-28.

- [ ] **Step 2: Modernize the input default.** Make the latest data-file the default when no arg is given, so refreshes are one command, while keeping the positional override:

```go
jsonFile := "data/game-api/latest/catalog_ships.json"
if len(os.Args) >= 2 {
	jsonFile = os.Args[1]
}
```

(Replace the existing `if len(os.Args) < 2 { log.Fatalf(...) }` + `jsonFile := os.Args[1]` block.)

- [ ] **Step 3: Build to bin/.**

Run: `go build -o bin/import-catalog-ships ./cmd/data/import-catalog-ships`
Expected: no output (success).

- [ ] **Step 4: Import the real, current data.**

Run: `./bin/import-catalog-ships`
Expected: log line reporting ~331 ship classes imported, no error.

- [ ] **Step 5: Verify the refresh landed.**

Run: `sqlite3 data/spacemolt-knowledge.db "SELECT count(*) FROM ships;"`
Expected: `331` (up from 320).

- [ ] **Step 6: Commit.**

```bash
git add cmd/data/import-catalog-ships/main.go
git commit -m "feat(data): default import-catalog-ships to latest catalog file; refresh ships catalog (320->331)"
```

> Optional follow-up (not required by this plan): the sibling `import-catalog-items` / `import-catalog-skills` / `import-catalog-recipes` tools can be modernized the same way to refresh their catalogs from `data/game-api/latest/`. The crafting DB (`data/crafting/crafting.db`) also mirrors item data. Out of scope here — ships is all the rebuy feature reads.

---

### Task 1: Config + skill/slot accessors (pure)

**Files:**
- Create: `pkg/worker/rebuy.go`
- Test: `pkg/worker/rebuy_test.go`

**Interfaces:**
- Produces: `type RebuyConfig struct { Enable bool; MinUtilitySlots int; ReserveFloor float64; PriceMarkup float64 }`; `func pilotingLevel(state *game.State) int`; `func currentUtilitySlots(state *game.State) int`.

- [ ] **Step 1: Read the structs you depend on.** Open `pkg/game/types.go` and confirm: the `Player.Skills` map key for piloting (search `piloting`/`small_ships`), and the `Ship` struct's installed-module + utility-slot fields (e.g. `Ship.Modules`, `Ship.UtilitySlots`). Note exact names — the next steps assume `state.Player.Skills["piloting"].Level` and `state.Ship.UtilitySlots`; adjust if the read shows otherwise.

- [ ] **Step 2: Write the failing test**

```go
package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestPilotingLevelAndSlots(t *testing.T) {
	st := &game.State{}
	st.Player.Skills = map[string]game.Skill{"piloting": {Level: 7}}
	st.Ship.UtilitySlots = 2
	if got := pilotingLevel(st); got != 7 {
		t.Errorf("pilotingLevel = %d, want 7", got)
	}
	if got := currentUtilitySlots(st); got != 2 {
		t.Errorf("currentUtilitySlots = %d, want 2", got)
	}
	// Missing skill defaults to 0.
	if got := pilotingLevel(&game.State{}); got != 0 {
		t.Errorf("missing piloting = %d, want 0", got)
	}
}
```

- [ ] **Step 3: Run it, expect FAIL** — `go test ./pkg/worker/ -run TestPilotingLevelAndSlots` → undefined `pilotingLevel`.

- [ ] **Step 4: Implement in `pkg/worker/rebuy.go`**

```go
package worker

import "github.com/rsned/spacemolt/pkg/game"

// RebuyConfig controls the haul auto-replacement behavior. Zero value is
// disabled (Enable=false), so haul behavior is unchanged unless opted in.
type RebuyConfig struct {
	Enable          bool    // master switch
	MinUtilitySlots int     // a hull must offer at least this many utility slots
	ReserveFloor    float64 // keep at least this many credits after rebuy+refit+insure
	PriceMarkup     float64 // catalog list_price is multiplied by this for budgeting (~3.0)
}

// pilotingLevel returns the agent's piloting skill level, 0 if unknown.
func pilotingLevel(state *game.State) int {
	if state == nil {
		return 0
	}
	return state.Player.Skills["piloting"].Level
}

// currentUtilitySlots returns the agent's current ship utility-slot count.
func currentUtilitySlots(state *game.State) int {
	if state == nil {
		return 0
	}
	return state.Ship.UtilitySlots
}
```

- [ ] **Step 5: Run it, expect PASS** — `go test ./pkg/worker/ -run TestPilotingLevelAndSlots`.

- [ ] **Step 6: Lint + commit**

```bash
golangci-lint run ./pkg/worker/...
git add pkg/worker/rebuy.go pkg/worker/rebuy_test.go
git commit -m "feat(worker/haul): rebuy config + piloting/slot accessors"
```

---

### Task 2: SelectHaulerHull — catalog hull picker (pure)

**Files:**
- Modify: `pkg/worker/rebuy.go`
- Test: `pkg/worker/rebuy_test.go`

**Interfaces:**
- Consumes: `knowledge.ShipClassDef` (`PilotingRequired`, `Tier`, `UtilitySlots`, `CargoCapacity`, `Price`, `StarterShip`, `Hidden`).
- Produces: `type HullTarget struct { ClassID, Name string; Tier, UtilitySlots, CargoCapacity int }`; `func SelectHaulerHull(classes []knowledge.ShipClassDef, pilotingLevel int, budget float64, minUtilitySlots int) (HullTarget, bool)`.

- [ ] **Step 1: Write the failing test**

```go
func TestSelectHaulerHull(t *testing.T) {
	classes := []knowledge.ShipClassDef{
		{ID: "pod", Name: "Pod", Tier: 1, UtilitySlots: 1, CargoCapacity: 50, Price: 1000, StarterShip: true, PilotingRequired: 0},
		{ID: "mule", Name: "Mule", Tier: 1, UtilitySlots: 3, CargoCapacity: 120, Price: 8000, PilotingRequired: 0},
		{ID: "ox", Name: "Ox", Tier: 1, UtilitySlots: 4, CargoCapacity: 100, Price: 9000, PilotingRequired: 0},
		{ID: "freighter", Name: "Freighter", Tier: 2, UtilitySlots: 6, CargoCapacity: 400, Price: 40000, PilotingRequired: 10},
	}
	// Piloting 7, budget 30000: freighter excluded (piloting), pod excluded (slots<3).
	// Ox (4 slots) beats Mule (3 slots) on the primary criterion.
	got, ok := SelectHaulerHull(classes, 7, 30000, 3)
	if !ok || got.ClassID != "ox" {
		t.Fatalf("got %+v ok=%v, want ox", got, ok)
	}
	// Piloting 12, budget 100000: freighter now pilotable and has most slots.
	got, ok = SelectHaulerHull(classes, 12, 100000, 3)
	if !ok || got.ClassID != "freighter" {
		t.Fatalf("got %+v ok=%v, want freighter", got, ok)
	}
	// Budget too low for anything but the starter -> no qualifying hull.
	if _, ok := SelectHaulerHull(classes, 7, 2000, 3); ok {
		t.Errorf("expected no hull within tiny budget")
	}
}
```

- [ ] **Step 2: Run it, expect FAIL** — undefined `SelectHaulerHull`.

- [ ] **Step 3: Implement (append to `pkg/worker/rebuy.go`)**

```go
import "github.com/rsned/spacemolt/pkg/knowledge"

// HullTarget is a chosen replacement ship class.
type HullTarget struct {
	ClassID       string
	Name          string
	Tier          int
	UtilitySlots  int
	CargoCapacity int
}

// SelectHaulerHull picks the best catalog hull the agent can pilot and afford:
// candidates must be non-starter, satisfy the piloting requirement, clear
// minUtilitySlots, and cost at most budget. Ranking: most utility slots, then
// most base cargo, then cheaper, then id for stability. ok=false if none qualify.
func SelectHaulerHull(classes []knowledge.ShipClassDef, pilotingLevel int, budget float64, minUtilitySlots int) (HullTarget, bool) {
	best := HullTarget{}
	found := false
	bestPrice := 0
	for _, c := range classes {
		if c.StarterShip || c.UtilitySlots < minUtilitySlots {
			continue
		}
		if c.PilotingRequired > pilotingLevel {
			continue
		}
		if float64(c.Price) > budget {
			continue
		}
		cand := HullTarget{ClassID: c.ID, Name: c.Name, Tier: c.Tier, UtilitySlots: c.UtilitySlots, CargoCapacity: c.CargoCapacity}
		if !found || betterHull(cand, c.Price, best, bestPrice) {
			best, bestPrice, found = cand, c.Price, true
		}
	}
	return best, found
}

// betterHull reports whether candidate a (price ap) outranks current best b (price bp):
// more utility slots, then more cargo, then cheaper, then lower class id.
func betterHull(a HullTarget, ap int, b HullTarget, bp int) bool {
	if a.UtilitySlots != b.UtilitySlots {
		return a.UtilitySlots > b.UtilitySlots
	}
	if a.CargoCapacity != b.CargoCapacity {
		return a.CargoCapacity > b.CargoCapacity
	}
	if ap != bp {
		return ap < bp
	}
	return a.ClassID < b.ClassID
}
```

- [ ] **Step 4: Run it, expect PASS.**
- [ ] **Step 5: Lint + commit** — `git commit -m "feat(worker/haul): catalog hauler-hull selection"`.

---

### Task 3: NeedsReplacement — the trigger predicate (pure)

**Files:**
- Modify: `pkg/worker/rebuy.go`
- Test: `pkg/worker/rebuy_test.go`

**Interfaces:**
- Produces: `func NeedsReplacement(state *game.State, byClass map[string]knowledge.ShipClassDef, cfg RebuyConfig) bool`.

- [ ] **Step 1: Write the failing test**

```go
func TestNeedsReplacement(t *testing.T) {
	byClass := map[string]knowledge.ShipClassDef{
		"pod":  {ID: "pod", StarterShip: true, UtilitySlots: 1},
		"mule": {ID: "mule", UtilitySlots: 3},
	}
	cfg := RebuyConfig{Enable: true, MinUtilitySlots: 3, ReserveFloor: 10000}
	starter := &game.State{}
	starter.Ship.ClassID = "pod"
	starter.Ship.UtilitySlots = 1
	starter.Player.Credits = 80000
	if !NeedsReplacement(starter, byClass, cfg) {
		t.Error("starter pod with credits should need replacement")
	}
	// Already a good hull -> no replacement.
	good := &game.State{}
	good.Ship.ClassID = "mule"
	good.Ship.UtilitySlots = 3
	good.Player.Credits = 80000
	if NeedsReplacement(good, byClass, cfg) {
		t.Error("3-slot mule should not need replacement")
	}
	// Below reserve floor -> can't afford, skip.
	broke := &game.State{}
	broke.Ship.ClassID = "pod"
	broke.Ship.UtilitySlots = 1
	broke.Player.Credits = 5000
	if NeedsReplacement(broke, byClass, cfg) {
		t.Error("below reserve floor should skip replacement")
	}
	// Disabled config -> never.
	if NeedsReplacement(starter, byClass, RebuyConfig{Enable: false, MinUtilitySlots: 3}) {
		t.Error("disabled config should never replace")
	}
}
```

- [ ] **Step 2: Run it, expect FAIL.**

- [ ] **Step 3: Implement (verify `state.Ship.ClassID` field name in the Task 1 read; adjust if different)**

```go
// NeedsReplacement reports whether the current ship is a sub-spec hauler worth
// replacing: the agent is opted in, its hull is a starter or has fewer than
// cfg.MinUtilitySlots utility slots, and it holds more than cfg.ReserveFloor
// credits (so a rebuy is affordable at all). byClass maps class id -> catalog def.
func NeedsReplacement(state *game.State, byClass map[string]knowledge.ShipClassDef, cfg RebuyConfig) bool {
	if !cfg.Enable || state == nil {
		return false
	}
	if state.Player.Credits <= cfg.ReserveFloor {
		return false
	}
	if currentUtilitySlots(state) >= cfg.MinUtilitySlots {
		return false
	}
	// Belt-and-suspenders: a known starter hull always qualifies even if the
	// live slot count is momentarily stale.
	if def, ok := byClass[state.Ship.ClassID]; ok && def.StarterShip {
		return true
	}
	return true
}
```

- [ ] **Step 4: Run it, expect PASS.**
- [ ] **Step 5: Lint + commit** — `git commit -m "feat(worker/haul): replacement-trigger predicate"`.

---

### Task 4: KB cross-station ship-listing query

**Files:**
- Modify: `pkg/knowledge/base.go` (interface), `pkg/knowledge/sqlite.go`, `pkg/knowledge/memory.go`
- Test: `pkg/knowledge/sqlite_test.go` (or the existing ship-listings test file)

**Interfaces:**
- Produces: `GetAllLatestShipListings(ctx context.Context) ([]ShipListings, error)` on `knowledge.Base` — the most-recent `ShipListings` row per station.

- [ ] **Step 1: Read** `pkg/knowledge/sqlite.go` around the existing `GetLatestShipListings` (search `ship_listings`) to copy its row-scan and the table's timestamp/tick column used for "latest".

- [ ] **Step 2: Write the failing test** (mirror an existing ship-listings test; store listings for two stations, assert both come back)

```go
func TestGetAllLatestShipListings(t *testing.T) {
	kb := newTestSQLiteKB(t) // use the existing test constructor in this package
	ctx := context.Background()
	mk := func(sys, st string, slots int) knowledge.ShipListings {
		return knowledge.ShipListings{SystemID: sys, StationID: st, GameTick: 1,
			Listings: []knowledge.ShipListing{{ShipClass: "mule", UtilitySlots: slots, CargoSpace: 120}}}
	}
	if err := kb.StoreShipListings(ctx, mk("sol", "sol_central", 3), "test"); err != nil {
		t.Fatal(err)
	}
	if err := kb.StoreShipListings(ctx, mk("haven", "haven_station", 4), "test"); err != nil {
		t.Fatal(err)
	}
	all, err := kb.GetAllLatestShipListings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d station listings, want 2", len(all))
	}
}
```

- [ ] **Step 3: Run it, expect FAIL** — undefined method.

- [ ] **Step 4: Add to the `Base` interface** in `pkg/knowledge/base.go` next to the other ship-listing methods:

```go
	GetAllLatestShipListings(ctx context.Context) ([]ShipListings, error)
```

- [ ] **Step 5: Implement on `SQLiteKB`** in `pkg/knowledge/sqlite.go` — select the latest row per `(system_id, station_id)`, reusing the same scan as `GetLatestShipListings`. Sketch (adapt column names to the table actually read in Step 1):

```go
// GetAllLatestShipListings returns the most-recent ship-listings snapshot for
// every station the KB has ever captured (one row group per station).
func (k *SQLiteKB) GetAllLatestShipListings(ctx context.Context) ([]ShipListings, error) {
	rows, err := k.db.QueryContext(ctx, `
		SELECT system_id, station_id, MAX(game_tick) AS gt
		FROM ship_listings GROUP BY system_id, station_id`)
	if err != nil {
		return nil, fmt.Errorf("knowledge: all ship listings: %w", err)
	}
	type key struct{ sys, st string }
	var keys []key
	for rows.Next() {
		var sys, st string
		var gt int64
		if err := rows.Scan(&sys, &st, &gt); err != nil {
			rows.Close() //nolint:errcheck,sqlclosecheck
			return nil, err
		}
		keys = append(keys, key{sys, st})
	}
	rows.Close() //nolint:errcheck,sqlclosecheck
	out := make([]ShipListings, 0, len(keys))
	for _, kk := range keys {
		sl, err := k.GetLatestShipListings(ctx, kk.sys, kk.st)
		if err != nil {
			return nil, err
		}
		if sl != nil {
			out = append(out, *sl)
		}
	}
	return out, nil
}
```

- [ ] **Step 6: Implement on `MemoryKB`** in `pkg/knowledge/memory.go` (return the latest per station from its in-memory map; if MemoryKB doesn't track ship listings, return `nil, nil` with a clarifying comment so the interface is satisfied).

- [ ] **Step 7: Run it, expect PASS** — `go test ./pkg/knowledge/ -run TestGetAllLatestShipListings`.

- [ ] **Step 8: Verify no other `Base` implementers broke** — `go build ./...` then `go test ./...` (per the memory note, mock implementers in `pkg/agent`/`pkg/skills` may need the new method stubbed).

- [ ] **Step 9: Lint + commit** — `git commit -m "feat(knowledge): GetAllLatestShipListings cross-station query"`.

---

### Task 5: FindHullStations + FindExpanderStations (pure)

**Files:**
- Modify: `pkg/worker/rebuy.go`
- Test: `pkg/worker/rebuy_test.go`

**Interfaces:**
- Consumes: `knowledge.ShipListings`, `knowledge.ShipClassDef`, `market.ItemStationPrice`.
- Produces: `type HullLocation struct { SystemID, StationID string; Target HullTarget }`; `func FindHullStations(listings []knowledge.ShipListings, byClass map[string]knowledge.ShipClassDef, pilotingLevel, minUtilitySlots int) []HullLocation`; `func FindExpanderStations(prices []market.ItemStationPrice) []string` (station ids with live sell stock).

- [ ] **Step 1: Write the failing test**

```go
func TestFindHullStationsAndExpanders(t *testing.T) {
	byClass := map[string]knowledge.ShipClassDef{
		"mule":      {ID: "mule", Name: "Mule", Tier: 1, UtilitySlots: 3, CargoCapacity: 120, PilotingRequired: 0},
		"freighter": {ID: "freighter", Name: "Freighter", Tier: 2, UtilitySlots: 6, PilotingRequired: 10},
	}
	listings := []knowledge.ShipListings{
		{SystemID: "sol", StationID: "sol_central", Listings: []knowledge.ShipListing{{ShipClass: "mule", UtilitySlots: 3}}},
		{SystemID: "vega", StationID: "vega_yard", Listings: []knowledge.ShipListing{{ShipClass: "freighter", UtilitySlots: 6}}},
	}
	// Piloting 7: only the mule station qualifies (freighter needs piloting 10).
	hs := FindHullStations(listings, byClass, 7, 3)
	if len(hs) != 1 || hs[0].StationID != "sol_central" || hs[0].Target.ClassID != "mule" {
		t.Fatalf("got %+v, want only sol_central/mule", hs)
	}
	exp := FindExpanderStations([]market.ItemStationPrice{
		{StationID: "a", HasSell: true, AskQty: 5},
		{StationID: "b", HasSell: false},
		{StationID: "c", HasSell: true, AskQty: 0}, // no real stock
	})
	if len(exp) != 1 || exp[0] != "a" {
		t.Fatalf("got %v, want [a]", exp)
	}
}
```

- [ ] **Step 2: Run it, expect FAIL.**

- [ ] **Step 3: Implement (append to `pkg/worker/rebuy.go`)**

```go
import "github.com/rsned/spacemolt/pkg/market"

// HullLocation is a station known (from KB ship_listings) to stock a qualifying hull.
type HullLocation struct {
	SystemID  string
	StationID string
	Target    HullTarget
}

// FindHullStations scans captured ship listings for stations stocking a hull the
// agent can pilot (catalog PilotingRequired <= pilotingLevel) that meets
// minUtilitySlots. The catalog (byClass) supplies tier/piloting/cargo, since the
// persisted listing row does not carry tier. One entry per station (its best hull).
func FindHullStations(listings []knowledge.ShipListings, byClass map[string]knowledge.ShipClassDef, pilotingLevel, minUtilitySlots int) []HullLocation {
	var out []HullLocation
	for _, sl := range listings {
		best, found := HullTarget{}, false
		bestPrice := 0
		for _, item := range sl.Listings {
			def, ok := byClass[item.ShipClass]
			if !ok || def.StarterShip || def.PilotingRequired > pilotingLevel || def.UtilitySlots < minUtilitySlots {
				continue
			}
			cand := HullTarget{ClassID: def.ID, Name: def.Name, Tier: def.Tier, UtilitySlots: def.UtilitySlots, CargoCapacity: def.CargoCapacity}
			if !found || betterHull(cand, def.Price, best, bestPrice) {
				best, bestPrice, found = cand, def.Price, true
			}
		}
		if found {
			out = append(out, HullLocation{SystemID: sl.SystemID, StationID: sl.StationID, Target: best})
		}
	}
	return out
}

// FindExpanderStations returns the station ids that currently list a real sell
// quantity for the queried expander item.
func FindExpanderStations(prices []market.ItemStationPrice) []string {
	var out []string
	for _, p := range prices {
		if p.HasSell && p.AskQty > 0 {
			out = append(out, p.StationID)
		}
	}
	return out
}
```

- [ ] **Step 4: Run it, expect PASS.**
- [ ] **Step 5: Lint + commit** — `git commit -m "feat(worker/haul): hull + expander station discovery"`.

---

### Task 6: ReplaceAndRefit — buy → strip → fit → insure (orchestration)

**Files:**
- Modify: `pkg/worker/rebuy.go`
- Test: `pkg/worker/rebuy_test.go`

**Interfaces:**
- Consumes: `HaulDeps` (Client, Out), `HullTarget`, `RebuyConfig`.
- Produces: `func ReplaceAndRefit(ctx context.Context, deps HaulDeps, target HullTarget) error`; helper `func utilityModulesToStrip(state *game.State) []string` (utility-slot module ids that are NOT `cargo_expander_*` or `afterburner_i`).

- [ ] **Step 1: Read** the `game.Ship` modules struct (`pkg/game/types.go`, search `Module`) to confirm: how installed modules are listed, their slot-type field (utility vs weapon/defense), and the module id field. The strip helper below assumes `state.Ship.Modules []Module` with `Module.ItemID` and `Module.Slot`/`Module.SlotType == "utility"`. Adjust names to the real struct.

- [ ] **Step 2: Write the failing test for the pure strip helper**

```go
func TestUtilityModulesToStrip(t *testing.T) {
	st := &game.State{}
	st.Ship.Modules = []game.Module{
		{ItemID: "cargo_expander_ii", SlotType: "utility"},
		{ItemID: "afterburner_i", SlotType: "utility"},
		{ItemID: "mining_laser_i", SlotType: "utility"},   // strip
		{ItemID: "scanner_iii", SlotType: "utility"},      // strip
		{ItemID: "pulse_cannon", SlotType: "weapon"},      // not utility, keep
	}
	got := utilityModulesToStrip(st)
	if len(got) != 2 || got[0] != "mining_laser_i" || got[1] != "scanner_iii" {
		t.Fatalf("got %v, want [mining_laser_i scanner_iii]", got)
	}
}
```

- [ ] **Step 3: Run it, expect FAIL.**

- [ ] **Step 4: Implement the strip helper + orchestrator (append to `pkg/worker/rebuy.go`)**

```go
import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// utilityModulesToStrip lists installed utility-slot module ids that are not
// cargo expanders or afterburner_i — the junk to uninstall to free slots for
// cargo expanders. Order follows the ship's module list (deterministic).
func utilityModulesToStrip(state *game.State) []string {
	var out []string
	for _, m := range state.Ship.Modules {
		if m.SlotType != "utility" {
			continue
		}
		if strings.HasPrefix(m.ItemID, "cargo_expander_") || m.ItemID == "afterburner_i" {
			continue
		}
		out = append(out, m.ItemID)
	}
	return out
}

// ReplaceAndRefit performs the in-station replacement sequence at the agent's
// CURRENT docked station: browse_ships -> buy the listing matching target ->
// switch to it -> uninstall non-cargo utility modules -> fit cargo expanders
// into freed slots (best tier in local stock) -> buy_insurance. Each game call
// is followed by SleepQuick to let state settle. Returns the first hard error.
func ReplaceAndRefit(ctx context.Context, deps HaulDeps, target HullTarget) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	client := deps.Client

	// 1. Find the live listing id for target at this station.
	listingID, ok, err := liveListingID(ctx, client, target.ClassID)
	if err != nil {
		return fmt.Errorf("rebuy: browse_ships: %w", err)
	}
	if !ok {
		fmt.Fprintf(out, "rebuy: %s not actually listed here; skipping\n", target.ClassID) //nolint:errcheck
		return nil
	}

	// 2. Buy + switch.
	if err := client.BuyListedShip(ctx, listingID); err != nil {
		return fmt.Errorf("rebuy: buy_listed_ship %s: %w", listingID, err)
	}
	game.SleepFor(game.SleepDock) // see Step 5 note on sleep constant
	// (switch_ship if the server does not auto-switch — verify in Step 5.)

	// 3. Strip junk utility modules.
	st := client.GetState()
	for _, modID := range utilityModulesToStrip(st) {
		if err := client.UninstallMod(ctx, modID); err != nil {
			fmt.Fprintf(out, "rebuy: uninstall %s: %v\n", modID, err) //nolint:errcheck
			continue
		}
		game.SleepFor(game.SleepQuick)
	}

	// 4. Fit cargo expanders into freed utility slots (best tier available in
	//    local market). buyAndInstallExpander returns false when no stock/slots
	//    remain, ending the loop.
	for buyAndInstallExpander(ctx, deps, out) {
	}

	// 5. Insure the new hull for the next loss.
	if err := client.BuyInsurance(ctx, defaultInsuranceTicks); err != nil {
		fmt.Fprintf(out, "rebuy: buy_insurance: %v\n", err) //nolint:errcheck
	}
	fmt.Fprintf(out, "rebuy: replaced into %s (%d utility slots)\n", target.ClassID, target.UtilitySlots) //nolint:errcheck
	return nil
}
```

- [ ] **Step 5: Fill the helper stubs and constants used above.** In this step, implement: `liveListingID(ctx, client, classID) (string, bool, error)` (calls `client.BrowseShips`, reads `client.GetRawJSON("ships")`, unmarshals into `[]serverapi.ShipListingDetail`, returns the first listing whose `ClassID == classID`); `buyAndInstallExpander(ctx, deps, out) bool` (query local market for `cargo_expander_iii` then `_ii` then `_i`, `client.Buy` one if affordable above reserve, `client.InstallMod` it, return true; false when none/full); `const defaultInsuranceTicks = ...` (read `BuyInsurance` semantics + a sane default, e.g. one rent cycle). Replace the `game.SleepFor(...)` placeholder with the real sleep idiom used elsewhere in `pkg/worker` (grep `SleepQuick` in `pkg/worker` for the exact call form — likely `time.Sleep(game.SleepQuick)`). Confirm whether `BuyListedShip` auto-switches or a `SwitchShip` is required, and add it if so. Each sub-helper gets a focused unit test against a fake `game.GameClient`.

- [ ] **Step 6: Run tests, expect PASS** — `go test ./pkg/worker/ -run 'Strip|ReplaceAndRefit|Expander|Listing'`.
- [ ] **Step 7: Lint + commit** — `git commit -m "feat(worker/haul): buy/strip/fit/insure refit sequence"`.

---

### Task 7: MaybeReplaceShip — per-pass orchestrator with routing

**Files:**
- Modify: `pkg/worker/rebuy.go`
- Test: `pkg/worker/rebuy_test.go`

**Interfaces:**
- Consumes: `HaulDeps` (Client, KB, Market, Out, Rebuy), all helpers above, `worker.Autopilot`.
- Produces: `func MaybeReplaceShip(ctx context.Context, deps HaulDeps) (acted bool, err error)`.

- [ ] **Step 1: Write the failing test** (fake client docked at a station that stocks a qualifying hull → expect `acted=true` and that buy was attempted; fake client in starter hull where only a remote station has stock → expect autopilot invoked toward it). Use the package's existing fake `game.GameClient` (search `pkg/worker` tests for the fake) and a `MemoryKB` seeded via `StoreShipClasses` + `StoreShipListings`.

- [ ] **Step 2: Run it, expect FAIL.**

- [ ] **Step 3: Implement (append to `pkg/worker/rebuy.go`)**

```go
// MaybeReplaceShip runs one pass of the auto-replacement flow. If the agent does
// not need a new ship (or rebuy is disabled), it returns acted=false immediately
// so Haul proceeds normally. Otherwise: load the catalog + cross-station ship
// listings; if a qualifying hull is stocked at the CURRENT station and the agent
// is docked, refit here; else autopilot one hop toward the nearest station that
// stocks one. Returns acted=true when it bought or moved, so the caller returns
// and re-evaluates next pass.
func MaybeReplaceShip(ctx context.Context, deps HaulDeps) (bool, error) {
	if !deps.Rebuy.Enable {
		return false, nil
	}
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	state := deps.Client.GetState()
	classes, err := deps.KB.GetShipClasses(ctx)
	if err != nil {
		return false, fmt.Errorf("rebuy: catalog: %w", err)
	}
	byClass := indexShipClasses(classes) // map[string]ShipClassDef
	if !NeedsReplacement(state, byClass, deps.Rebuy) {
		return false, nil
	}

	budget := state.Player.Credits - deps.Rebuy.ReserveFloor
	listings, err := deps.KB.GetAllLatestShipListings(ctx)
	if err != nil {
		return false, fmt.Errorf("rebuy: listings: %w", err)
	}
	hullStations := FindHullStations(listings, byClass, pilotingLevel(state), deps.Rebuy.MinUtilitySlots)
	if len(hullStations) == 0 {
		fmt.Fprintln(out, "rebuy: no known station stocks a qualifying hull; idling") //nolint:errcheck
		return false, nil
	}

	// If docked here and this station stocks one within budget, refit in place.
	if state.Docked {
		for _, hs := range hullStations {
			if hs.StationID == state.POIID && float64(byClassPrice(byClass, hs.Target.ClassID)) <= budget {
				if err := ReplaceAndRefit(ctx, deps, hs.Target); err != nil {
					return true, err
				}
				return true, nil
			}
		}
	}

	// Otherwise route toward the nearest qualifying hull station within budget.
	dest, ok := nearestAffordable(state, hullStations, byClass, budget, deps)
	if !ok {
		fmt.Fprintln(out, "rebuy: qualifying hulls known but none affordable/reachable; idling") //nolint:errcheck
		return false, nil
	}
	fmt.Fprintf(out, "rebuy: routing to %s for a %s hull\n", dest.StationID, dest.Target.ClassID) //nolint:errcheck
	if err := Autopilot(ctx, autopilotDepsFor(deps), dest.SystemID, dest.StationID); err != nil {
		return true, fmt.Errorf("rebuy: autopilot to %s: %w", dest.SystemID, err)
	}
	return true, nil
}
```

- [ ] **Step 4: Implement the small helpers used above** in the same step: `indexShipClasses([]ShipClassDef) map[string]ShipClassDef`; `byClassPrice(map, id) int`; `nearestAffordable(state, []HullLocation, byClass, budget, deps) (HullLocation, bool)` (filter by price<=budget, pick fewest jumps via `navigation.BFSJumps` over a graph built from `deps.KB.GetConnections`; reuse the pattern in `haul.go`); `autopilotDepsFor(deps) AutopilotDeps` (mirror how `dispatch.go` builds `AutopilotDeps`). Verify the `state.Docked` / `state.POIID` field names against `pkg/game/types.go` and fix if different.

- [ ] **Step 5: Run tests, expect PASS.**
- [ ] **Step 6: Lint + commit** — `git commit -m "feat(worker/haul): MaybeReplaceShip orchestrator + routing"`.

---

### Task 8: Wire into Haul()

**Files:**
- Modify: `pkg/worker/haul.go`
- Test: `pkg/worker/haul_test.go`

**Interfaces:**
- Consumes: `MaybeReplaceShip`. Adds `Rebuy RebuyConfig` to `HaulDeps`.

- [ ] **Step 1: Add the field** to `HaulDeps` in `pkg/worker/haul.go`:

```go
	// Rebuy enables + tunes auto ship-replacement after a loss. Zero value
	// (Enable=false) leaves haul behavior unchanged.
	Rebuy RebuyConfig
```

- [ ] **Step 2: Write the failing test** — a `HaulDeps` with `Rebuy.Enable=true` and a fake client in a starter hull with credits should attempt replacement (assert a `browse_ships`/autopilot call happened) and NOT claim a haul that pass. With `Rebuy.Enable=false`, behavior is unchanged (existing tests still pass).

- [ ] **Step 3: Run it, expect FAIL.**

- [ ] **Step 4: Insert the early-pass check** in `Haul()`, right after `current := state.System.ID` and the systems/graph load, BEFORE the resume/claim logic:

```go
	// Auto-replace a destroyed/sub-spec hull before doing anything else this pass.
	if acted, err := MaybeReplaceShip(ctx, deps); err != nil {
		fmt.Fprintf(out, "haul: rebuy: %v\n", err) //nolint:errcheck
		return nil
	} else if acted {
		return nil // bought or moved; re-evaluate next pass
	}
```

- [ ] **Step 5: Run tests, expect PASS** — `go test ./pkg/worker/ -run Haul`.
- [ ] **Step 6: Lint + commit** — `git commit -m "feat(worker/haul): wire auto-replacement into Haul pass"`.

---

### Task 9: Roster/role wiring + default config

**Files:**
- Modify: `cmd/worker/main.go` (where `HaulDeps` is constructed for the `hauler` standing behavior)
- Test: manual / build

**Interfaces:**
- Consumes: `RebuyConfig`.

- [ ] **Step 1: Read** `cmd/worker/main.go` (and/or `pkg/worker/standing.go`) to find where the `hauler` role builds `HaulDeps`, and how config flags reach it.

- [ ] **Step 2: Populate `Rebuy`** with safe defaults when constructing `HaulDeps`:

```go
Rebuy: worker.RebuyConfig{
	Enable:          true,
	MinUtilitySlots: 3,
	ReserveFloor:    50000,
	PriceMarkup:     3.0,
},
```

- [ ] **Step 3: Build** — `go build -o bin/worker ./cmd/worker` (binary to `bin/`, never repo root).
- [ ] **Step 4: Full verification** — `go build ./...` && `go test ./...` && `golangci-lint run ./...`.
- [ ] **Step 5: Commit** — `git commit -m "feat(worker/haul): enable auto-replacement for hauler role (3 slots, 50k reserve)"`.

---

## Self-Review

**Spec coverage:**
- Insurance pays credits, no equipped replacement → Tasks 3/6/7 spend credits to rebuy+refit. ✓
- Hull pick: Tier ≤ piloting (≥10 for Tier 2+), max utility slots, decent cargo → Task 2 `SelectHaulerHull` + Task 5 catalog `PilotingRequired` gate. ✓
- `browse_ships` station-local, `buy_listed_ship`, strip non-`cargo_expander_*`/`afterburner_i` utility mods, fit expanders, `buy_insurance` → Task 6. ✓
- `cargo_expander_iii` scarce + station-local; expander market intel → Task 5 `FindExpanderStations` + Task 6 `buyAndInstallExpander` (iii→ii→i fallback). ✓
- Marketbot-sourced distributed intel (hulls in `ship_listings`, expanders in `market.db`) → Task 4 cross-station query + Task 5/7. ✓
- ~3× catalog price budgeting → `RebuyConfig.PriceMarkup` (Task 1) used in budget checks (Tasks 2/7). ✓
- Procurement routing when local can't supply → Task 7 `nearestAffordable` + `Autopilot`. ✓
- Auto, in haul behavior → Task 8 wires into `Haul()`. ✓
- Credit reserve floor → `RebuyConfig.ReserveFloor` throughout. ✓

**Open items the implementer MUST verify (flagged inline, not placeholders):** exact `game.State`/`game.Ship`/`game.Module` field names (`ClassID`, `Docked`, `POIID`, `Skills["piloting"]`, `Modules[].SlotType`/`ItemID`) — Tasks 1/3/6/7 each begin with a read step; `BuyListedShip` auto-switch behavior (Task 6 Step 5); `BuyInsurance` tick semantics (Task 6 Step 5); the `ship_listings` "latest" column (Task 4 Step 1); the package's existing fake `GameClient` for orchestration tests (Tasks 6/7).

**Type consistency:** `HullTarget`, `HullLocation`, `RebuyConfig` field names are used identically across Tasks 1–8; `betterHull` is defined once (Task 2) and reused (Task 5); `byClass map[string]knowledge.ShipClassDef` is the shared catalog index from Task 7 `indexShipClasses`.

**Risks / notes:** Tier-2+ stays unreachable until pilots train piloting ≥10 — expected (most stay Tier 1). The scanner-regenerated stronghold opportunities are already handled by the separate stronghold guard (commit `d269b79`); this plan is independent of it. Feature ships OFF by default (`Enable` gate) so it can be merged before the roster flips it on.
