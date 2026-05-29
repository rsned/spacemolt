# Demand Ledger & Report Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist market buy-order demand (especially NPC Station Manager orders) into the knowledge SQLite DB as the player visits stations, and add a `demand` report command to `play_as` that shows the best-paying demand galaxy-wide, what the player can fulfill now, and what they can craft to fulfill.

**Architecture:** Two new SQLite tables in the existing knowledge DB — `market_buy_demand` (cheap compact best-buy per station/item, upserted on every market read) and `market_buy_orders` (full `Source`-classified order depth from explicit deep scans). Store/load methods live on `*SQLiteKB` (matching the existing `faction_orders` pattern — **not** the `Base` interface). A `demand` command in `play_as` loads the ledger offline, matches it against live cargo/storage and `craftplan` craftability, and renders a styled/JSON report. Capture is wired into the `view_market`, `sellable`, and `dock` paths.

**Tech Stack:** Go 1.24+, modernc.org/sqlite, existing `pkg/knowledge` KB, `pkg/craftplan` engine, `cmd/tools/play_as` REPL.

**Reference spec:** `docs/superpowers/specs/2026-05-29-demand-ledger-design.md`

**Conventions to follow:**
- KB row structs mirror `pkg/knowledge/faction.go` `FactionOrderRow`. Store/load mirror `faction_store.go` / `faction_load.go` using the `kb.inTx`, `utc()`, `parseUTC()` helpers in `faction_tx.go`.
- `play_as` pure cores (builders/parsers) are unit-tested; network glue is thin and verified manually. Mirror `sellable.go` (pure `buildSellablePlan`/`fillItem` + thin `runSellable`).
- Run `golangci-lint` after each task's implementation; fix any new findings before committing.
- Sleep/pause values come from `pkg/game/constants.go` (`SleepQuick`).

---

## File Structure

| File | Responsibility | New/Modify |
|------|----------------|------------|
| `pkg/knowledge/sqlite_migrations.go` | Add migration #36 (two tables) | Modify |
| `pkg/knowledge/demand.go` | `MarketDemandRow`, `MarketBuyOrderRow` structs | Create |
| `pkg/knowledge/demand_store.go` | `UpsertMarketDemand`, `ReplaceMarketBuyOrders` | Create |
| `pkg/knowledge/demand_load.go` | `LoadMarketDemand` + unexported loaders | Create |
| `pkg/knowledge/demand_test.go` | Migration + round-trip tests | Create |
| `cmd/tools/play_as/demand_report.go` | Pure `buildDemandReport`, classification, types | Create |
| `cmd/tools/play_as/demand_report_test.go` | Table-driven builder/classification tests | Create |
| `cmd/tools/play_as/demand_render.go` | Styled + JSON renderers | Create |
| `cmd/tools/play_as/demand_capture.go` | Compact capture parse + `captureDemand` helper | Create |
| `cmd/tools/play_as/demand_capture_test.go` | Compact + deep parse tests | Create |
| `cmd/tools/play_as/demand_scan.go` | Deep-scan handler + per-item parse | Create |
| `cmd/tools/play_as/demand_cmd.go` | `runDemand` (report orchestration) + flag parsing | Create |
| `cmd/tools/play_as/main.go` | Dispatch `case "demand"`; capture hooks in `view_market`/`dock` | Modify |
| `cmd/tools/play_as/sellable.go` | Capture hook after market fetch | Modify |

---

## Task 1: Migration #36 — ledger tables

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` (append to the slice returned by `migrations()`, after the `version: 35` entry near line 305)
- Test: `pkg/knowledge/demand_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/knowledge/demand_test.go`:

```go
package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestKB(t *testing.T) *SQLiteKB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "demand_test.db")
	kb, err := NewSQLiteKB(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	return kb
}

func TestMigration36CreatesDemandTables(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	for _, table := range []string{"market_buy_demand", "market_buy_orders"} {
		var name string
		err := kb.db.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %q not found: %v", table, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestMigration36CreatesDemandTables -v`
Expected: FAIL — `table "market_buy_demand" not found: sql: no rows in result set`

- [ ] **Step 3: Add the migration**

In `pkg/knowledge/sqlite_migrations.go`, inside `migrations()`, add this entry immediately after the `version: 35` struct (before the closing `}` of the returned slice):

```go
		{
			version: 36,
			name:    "market_buy_demand",
			sql: `
				CREATE TABLE market_buy_demand (
					station_id      TEXT NOT NULL,
					system_id       TEXT,
					item_id         TEXT NOT NULL,
					item_name       TEXT,
					best_buy_price  REAL NOT NULL DEFAULT 0,
					buy_quantity    REAL NOT NULL DEFAULT 0,
					captured_utc    TEXT NOT NULL,
					PRIMARY KEY (station_id, item_id)
				);
				CREATE INDEX market_buy_demand_item ON market_buy_demand(item_id);

				CREATE TABLE market_buy_orders (
					station_id    TEXT NOT NULL,
					system_id     TEXT,
					item_id       TEXT NOT NULL,
					item_name     TEXT,
					price_each    REAL NOT NULL DEFAULT 0,
					quantity      REAL NOT NULL DEFAULT 0,
					source        TEXT,
					captured_utc  TEXT NOT NULL
				);
				CREATE INDEX market_buy_orders_station_item ON market_buy_orders(station_id, item_id);
				CREATE INDEX market_buy_orders_item ON market_buy_orders(item_id);
			`,
		},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestMigration36CreatesDemandTables -v`
Expected: PASS

- [ ] **Step 5: Lint**

Run: `golangci-lint run pkg/knowledge/...`
Expected: no new findings

- [ ] **Step 6: Commit**

```bash
git add pkg/knowledge/sqlite_migrations.go pkg/knowledge/demand_test.go
git commit -m "feat(knowledge): migration 36 — market buy-order demand ledger tables"
```

---

## Task 2: KB row structs

**Files:**
- Create: `pkg/knowledge/demand.go`

- [ ] **Step 1: Write the structs**

Create `pkg/knowledge/demand.go`:

```go
package knowledge

import "time"

// MarketDemandRow is the compact best-buy demand for one item at one station,
// captured from a view_market summary (no item_id). One row per (station, item).
type MarketDemandRow struct {
	StationID    string
	SystemID     string
	ItemID       string
	ItemName     string
	BestBuyPrice float64
	BuyQuantity  float64
	CapturedAt   time.Time
}

// MarketBuyOrderRow is a single buy order from a deep scan (view_market with an
// item_id), carrying Source so the report can distinguish Station Manager
// ("station") orders from player orders.
type MarketBuyOrderRow struct {
	StationID  string
	SystemID   string
	ItemID     string
	ItemName   string
	PriceEach  float64
	Quantity   float64
	Source     string
	CapturedAt time.Time
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/knowledge/`
Expected: builds with no output

- [ ] **Step 3: Commit**

```bash
git add pkg/knowledge/demand.go
git commit -m "feat(knowledge): MarketDemandRow and MarketBuyOrderRow structs"
```

---

## Task 3: KB store methods

**Files:**
- Create: `pkg/knowledge/demand_store.go`
- Test: `pkg/knowledge/demand_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `pkg/knowledge/demand_test.go`:

```go
func TestUpsertMarketDemandReplacesByKey(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	if err := kb.UpsertMarketDemand(ctx, []MarketDemandRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", BestBuyPrice: 10, BuyQuantity: 100, CapturedAt: t0},
	}); err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	// Re-capture same (station,item) with a higher price -> row is replaced, not duplicated.
	if err := kb.UpsertMarketDemand(ctx, []MarketDemandRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", BestBuyPrice: 12, BuyQuantity: 80, CapturedAt: t0.Add(time.Hour)},
	}); err != nil {
		t.Fatalf("upsert2: %v", err)
	}

	summary, _, err := kb.LoadMarketDemand(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("want 1 demand row, got %d", len(summary))
	}
	if summary[0].BestBuyPrice != 12 || summary[0].BuyQuantity != 80 {
		t.Fatalf("want price 12 qty 80, got %v / %v", summary[0].BestBuyPrice, summary[0].BuyQuantity)
	}
}

func TestReplaceMarketBuyOrdersRoundTrip(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	orders := []MarketBuyOrderRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: t0},
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: "player", CapturedAt: t0},
	}
	if err := kb.ReplaceMarketBuyOrders(ctx, "stn1", "iron_ore", orders); err != nil {
		t.Fatalf("replace1: %v", err)
	}
	// Replacing again with one order leaves exactly one row for that key.
	if err := kb.ReplaceMarketBuyOrders(ctx, "stn1", "iron_ore", orders[:1]); err != nil {
		t.Fatalf("replace2: %v", err)
	}
	_, deep, err := kb.LoadMarketDemand(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(deep) != 1 {
		t.Fatalf("want 1 deep order after replace, got %d", len(deep))
	}
	if deep[0].Source != "station" || deep[0].PriceEach != 10 {
		t.Fatalf("unexpected deep order: %+v", deep[0])
	}
}
```

Add `"time"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run 'TestUpsertMarketDemand|TestReplaceMarketBuyOrders' -v`
Expected: FAIL — `kb.UpsertMarketDemand undefined` (compile error)

- [ ] **Step 3: Write the store methods**

Create `pkg/knowledge/demand_store.go`:

```go
package knowledge

import "context"

// UpsertMarketDemand inserts or updates the compact best-buy demand for each
// (station_id, item_id). Empty SystemID/ItemName are stored as "" (never NULL)
// so loaders can scan into plain strings.
func (kb *SQLiteKB) UpsertMarketDemand(ctx context.Context, rows []MarketDemandRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_buy_demand
					(station_id, system_id, item_id, item_name, best_buy_price, buy_quantity, captured_utc)
				VALUES (?,?,?,?,?,?,?)
				ON CONFLICT(station_id, item_id) DO UPDATE SET
					system_id      = excluded.system_id,
					item_name      = excluded.item_name,
					best_buy_price = excluded.best_buy_price,
					buy_quantity   = excluded.buy_quantity,
					captured_utc   = excluded.captured_utc`,
				r.StationID, r.SystemID, r.ItemID, r.ItemName, r.BestBuyPrice, r.BuyQuantity, utc(r.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceMarketBuyOrders replaces all deep-scan buy orders for one
// (station_id, item_id) with the supplied set.
func (kb *SQLiteKB) ReplaceMarketBuyOrders(ctx context.Context, stationID, itemID string, orders []MarketBuyOrderRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM market_buy_orders WHERE station_id=? AND item_id=?`, stationID, itemID); err != nil {
			return err
		}
		for _, o := range orders {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_buy_orders
					(station_id, system_id, item_id, item_name, price_each, quantity, source, captured_utc)
				VALUES (?,?,?,?,?,?,?,?)`,
				o.StationID, o.SystemID, o.ItemID, o.ItemName, o.PriceEach, o.Quantity, o.Source, utc(o.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}
```

- [ ] **Step 4: Implement the loader (needed for the test to compile/pass)**

This is implemented fully in Task 4. For now the test references `LoadMarketDemand`; complete Task 4's `demand_load.go` before re-running. Proceed to Task 4, then return to Step 5.

- [ ] **Step 5: Run test to verify it passes** (after Task 4)

Run: `go test ./pkg/knowledge/ -run 'TestUpsertMarketDemand|TestReplaceMarketBuyOrders' -v`
Expected: PASS

- [ ] **Step 6: Lint + commit** (after Task 4)

```bash
golangci-lint run pkg/knowledge/...
git add pkg/knowledge/demand_store.go pkg/knowledge/demand_test.go
git commit -m "feat(knowledge): UpsertMarketDemand and ReplaceMarketBuyOrders"
```

---

## Task 4: KB load method

**Files:**
- Create: `pkg/knowledge/demand_load.go`

> Note: Task 3's tests depend on this. Implement here, then finish Task 3 Steps 5–6.

- [ ] **Step 1: Write the loader**

Create `pkg/knowledge/demand_load.go`:

```go
package knowledge

import "context"

// LoadMarketDemand returns the full ledger: the compact best-buy summary rows
// and the deep per-order rows. The report layer decides how to merge them.
func (kb *SQLiteKB) LoadMarketDemand(ctx context.Context) ([]MarketDemandRow, []MarketBuyOrderRow, error) {
	summary, err := kb.loadMarketDemandSummary(ctx)
	if err != nil {
		return nil, nil, err
	}
	deep, err := kb.loadMarketBuyOrders(ctx)
	if err != nil {
		return nil, nil, err
	}
	return summary, deep, nil
}

func (kb *SQLiteKB) loadMarketDemandSummary(ctx context.Context) ([]MarketDemandRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT station_id, system_id, item_id, item_name, best_buy_price, buy_quantity, captured_utc
		FROM market_buy_demand
		ORDER BY item_id, best_buy_price DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MarketDemandRow
	for rows.Next() {
		var r MarketDemandRow
		var capStr string
		if err := rows.Scan(&r.StationID, &r.SystemID, &r.ItemID, &r.ItemName,
			&r.BestBuyPrice, &r.BuyQuantity, &capStr); err != nil {
			return nil, err
		}
		r.CapturedAt = parseUTC(capStr)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadMarketBuyOrders(ctx context.Context) ([]MarketBuyOrderRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT station_id, system_id, item_id, item_name, price_each, quantity, source, captured_utc
		FROM market_buy_orders
		ORDER BY station_id, item_id, price_each DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MarketBuyOrderRow
	for rows.Next() {
		var r MarketBuyOrderRow
		var capStr string
		if err := rows.Scan(&r.StationID, &r.SystemID, &r.ItemID, &r.ItemName,
			&r.PriceEach, &r.Quantity, &r.Source, &capStr); err != nil {
			return nil, err
		}
		r.CapturedAt = parseUTC(capStr)
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 2: Run Task 3's tests (they exercise store + load together)**

Run: `go test ./pkg/knowledge/ -run 'TestUpsertMarketDemand|TestReplaceMarketBuyOrders' -v`
Expected: PASS

- [ ] **Step 3: Lint**

Run: `golangci-lint run pkg/knowledge/...`
Expected: no new findings

- [ ] **Step 4: Commit (load layer), then complete Task 3 Steps 5–6**

```bash
git add pkg/knowledge/demand_load.go
git commit -m "feat(knowledge): LoadMarketDemand reader"
```

---

## Task 5: Demand report builder (pure core)

**Files:**
- Create: `cmd/tools/play_as/demand_report.go`
- Test: `cmd/tools/play_as/demand_report_test.go`

This is the heart of the feature. It is pure (no network), mirroring `sellable.go`'s `buildSellablePlan`.

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/play_as/demand_report_test.go`:

```go
package main

import (
	"testing"
	"time"

	"spacemolt/pkg/knowledge"
)

func TestBuildDemandReportClassifiesAndFulfills(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-1 * time.Hour)

	summary := []knowledge.MarketDemandRow{
		// Compact-only item -> class "?".
		{StationID: "stnA", SystemID: "sysA", ItemID: "titanium", ItemName: "Titanium", BestBuyPrice: 30, BuyQuantity: 40, CapturedAt: fresh},
	}
	deep := []knowledge.MarketBuyOrderRow{
		// Station order at 10, player order above it at 12 -> top is PLR>SM.
		{StationID: "stnA", SystemID: "sysA", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: fresh},
		{StationID: "stnA", SystemID: "sysA", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: "player", CapturedAt: fresh},
		// Pure station demand at another station -> STN.
		{StationID: "stnB", SystemID: "sysB", ItemID: "copper", ItemName: "Copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: fresh},
	}
	onHand := map[string]float64{
		"iron_ore": 30, // can fulfill 30 of the 70 total iron demand
		"copper":   0,
	}
	canCraft := map[string]int{
		"titanium": 5, // craftable to fulfill
	}

	rep := buildDemandReport(summary, deep, onHand, canCraft, now, demandOptions{sort: sortByPrice})

	byItem := map[string]demandReportRow{}
	for _, r := range rep.Rows {
		byItem[r.ItemID] = r
	}

	iron := byItem["iron_ore"]
	if iron.Class != classAboveSM {
		t.Errorf("iron class: want %s got %s", classAboveSM, iron.Class)
	}
	if iron.Price != 12 || iron.Quantity != 70 {
		t.Errorf("iron price/qty: want 12/70 got %v/%v", iron.Price, iron.Quantity)
	}
	if iron.FulfillQty != 30 || iron.FulfillValue != 360 {
		t.Errorf("iron fulfill: want 30/360 got %v/%v", iron.FulfillQty, iron.FulfillValue)
	}
	if byItem["copper"].Class != classStation {
		t.Errorf("copper class: want %s got %s", classStation, byItem["copper"].Class)
	}
	if byItem["titanium"].Class != classUnknown {
		t.Errorf("titanium class: want %s got %s", classUnknown, byItem["titanium"].Class)
	}
	if byItem["titanium"].CanCraft != 5 {
		t.Errorf("titanium craft: want 5 got %d", byItem["titanium"].CanCraft)
	}
}

func TestBuildDemandReportFiltersAndStaleness(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	summary := []knowledge.MarketDemandRow{
		{StationID: "s1", ItemID: "a", ItemName: "A", BestBuyPrice: 5, BuyQuantity: 10, CapturedAt: stale},
		{StationID: "s1", ItemID: "b", ItemName: "B", BestBuyPrice: 50, BuyQuantity: 10, CapturedAt: fresh},
	}

	// minPrice filters out item a (price 5).
	rep := buildDemandReport(summary, nil, nil, nil, now, demandOptions{minPrice: 10})
	if len(rep.Rows) != 1 || rep.Rows[0].ItemID != "b" {
		t.Fatalf("minPrice filter: want only b, got %+v", rep.Rows)
	}
	// Staleness flag set for the >24h-old row when not filtered out.
	rep2 := buildDemandReport(summary, nil, nil, nil, now, demandOptions{})
	for _, r := range rep2.Rows {
		wantStale := r.ItemID == "a"
		if r.AgeStale != wantStale {
			t.Errorf("item %s stale: want %v got %v", r.ItemID, wantStale, r.AgeStale)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestBuildDemandReport -v`
Expected: FAIL — `undefined: buildDemandReport` (compile error)

- [ ] **Step 3: Write the builder + types**

Create `cmd/tools/play_as/demand_report.go`:

```go
package main

import (
	"sort"
	"time"

	"spacemolt/pkg/knowledge"
)

// demandClass labels who is creating the demand for a row.
type demandClass string

const (
	classStation demandClass = "STN"    // Source == "station" (Station Manager)
	classAboveSM demandClass = "PLR>SM" // player order priced above the best station order
	classPlayer  demandClass = "PLR"    // player order, no higher station competitor
	classUnknown demandClass = "?"      // compact-only, source not known
)

type demandSort int

const (
	sortByProceeds demandSort = iota // default: FulfillValue desc
	sortByPrice                      // Price desc
	sortByAge                        // most-recently captured first
)

type onlyFilter int

const (
	onlyAll onlyFilter = iota
	onlyFulfillable
	onlyCraftable
)

// demandOptions are the report filters/sort parsed from `demand` flags.
type demandOptions struct {
	item        string
	station     string
	minPrice    float64
	maxAge      time.Duration // 0 = no max-age filter
	stationOnly bool
	only        onlyFilter
	sort        demandSort
	limit       int // 0 = no limit
}

// demandReportRow is one (station, item) demand line in the report.
type demandReportRow struct {
	StationID    string      `json:"station_id"`
	SystemID     string      `json:"system_id"`
	ItemID       string      `json:"item_id"`
	ItemName     string      `json:"item_name"`
	Price        float64     `json:"price"`
	Quantity     float64     `json:"quantity"`
	Class        demandClass `json:"class"`
	OnHand       float64     `json:"on_hand"`
	FulfillQty   float64     `json:"fulfill_qty"`
	FulfillValue float64     `json:"fulfill_value"`
	CanCraft     int         `json:"can_craft"`
	CapturedAt   time.Time   `json:"captured_at"`
	AgeStale     bool        `json:"age_stale"`
}

type demandReport struct {
	Rows         []demandReportRow `json:"rows"`
	TotalFulfill float64           `json:"total_fulfill"`
	Generated    time.Time         `json:"generated"`
}

const demandStaleAfter = 24 * time.Hour // station freshness threshold

// classifyDemand inspects the deep buy orders for one (station, item) and
// returns the headline class, best price, and total demand quantity.
func classifyDemand(orders []knowledge.MarketBuyOrderRow) (demandClass, float64, float64) {
	var bestStation, topPrice, totalQty float64
	var topSource string
	for _, o := range orders {
		totalQty += o.Quantity
		if o.Source == "station" && o.PriceEach > bestStation {
			bestStation = o.PriceEach
		}
		if o.PriceEach > topPrice {
			topPrice = o.PriceEach
			topSource = o.Source
		}
	}
	switch {
	case topSource == "station":
		return classStation, topPrice, totalQty
	case bestStation > 0 && topPrice > bestStation:
		return classAboveSM, topPrice, totalQty
	default:
		return classPlayer, topPrice, totalQty
	}
}

type demandAgg struct {
	stationID, systemID, itemID, itemName string
	price, qty                            float64
	class                                 demandClass
	captured                              time.Time
}

// buildDemandReport merges the compact summary with deep order depth, scores
// each (station, item) against on-hand inventory and craftability, applies
// filters, and sorts. Pure: callers pass `now` explicitly for testability.
func buildDemandReport(
	summary []knowledge.MarketDemandRow,
	deep []knowledge.MarketBuyOrderRow,
	onHand map[string]float64,
	canCraft map[string]int,
	now time.Time,
	opts demandOptions,
) demandReport {
	key := func(s, i string) string { return s + "\x00" + i }
	agg := map[string]*demandAgg{}

	for _, s := range summary {
		agg[key(s.StationID, s.ItemID)] = &demandAgg{
			stationID: s.StationID, systemID: s.SystemID, itemID: s.ItemID, itemName: s.ItemName,
			price: s.BestBuyPrice, qty: s.BuyQuantity, class: classUnknown, captured: s.CapturedAt,
		}
	}

	deepByKey := map[string][]knowledge.MarketBuyOrderRow{}
	for _, o := range deep {
		k := key(o.StationID, o.ItemID)
		deepByKey[k] = append(deepByKey[k], o)
	}
	for k, ords := range deepByKey {
		cls, price, qty := classifyDemand(ords)
		a := agg[k]
		if a == nil {
			a = &demandAgg{stationID: ords[0].StationID, systemID: ords[0].SystemID, itemID: ords[0].ItemID}
			agg[k] = a
		}
		a.class, a.price, a.qty = cls, price, qty
		for _, o := range ords {
			if o.CapturedAt.After(a.captured) {
				a.captured = o.CapturedAt
			}
			if a.itemName == "" {
				a.itemName = o.ItemName
			}
		}
	}

	var rows []demandReportRow
	var total float64
	for _, a := range agg {
		if opts.item != "" && a.itemID != opts.item {
			continue
		}
		if opts.station != "" && a.stationID != opts.station {
			continue
		}
		if a.price < opts.minPrice {
			continue
		}
		if opts.stationOnly && a.class != classStation {
			continue
		}
		if opts.maxAge > 0 && now.Sub(a.captured) > opts.maxAge {
			continue
		}

		onhand := onHand[a.itemID]
		fulfill := onhand
		if fulfill > a.qty {
			fulfill = a.qty
		}
		craft := canCraft[a.itemID]

		switch opts.only {
		case onlyFulfillable:
			if fulfill <= 0 {
				continue
			}
		case onlyCraftable:
			if craft <= 0 {
				continue
			}
		}

		row := demandReportRow{
			StationID: a.stationID, SystemID: a.systemID, ItemID: a.itemID, ItemName: a.itemName,
			Price: a.price, Quantity: a.qty, Class: a.class,
			OnHand: onhand, FulfillQty: fulfill, FulfillValue: fulfill * a.price,
			CanCraft: craft, CapturedAt: a.captured,
			AgeStale: now.Sub(a.captured) > demandStaleAfter,
		}
		rows = append(rows, row)
		total += row.FulfillValue
	}

	sortDemandRows(rows, opts.sort)
	if opts.limit > 0 && len(rows) > opts.limit {
		rows = rows[:opts.limit]
	}
	return demandReport{Rows: rows, TotalFulfill: total, Generated: now}
}

// sortDemandRows orders rows by the chosen key with deterministic tie-breakers
// (item_id, station_id) so output and tests are stable despite map iteration.
func sortDemandRows(rows []demandReportRow, mode demandSort) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch mode {
		case sortByPrice:
			if a.Price != b.Price {
				return a.Price > b.Price
			}
		case sortByAge:
			if !a.CapturedAt.Equal(b.CapturedAt) {
				return a.CapturedAt.After(b.CapturedAt)
			}
		default: // sortByProceeds
			if a.FulfillValue != b.FulfillValue {
				return a.FulfillValue > b.FulfillValue
			}
		}
		if a.ItemID != b.ItemID {
			return a.ItemID < b.ItemID
		}
		return a.StationID < b.StationID
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestBuildDemandReport -v`
Expected: PASS

- [ ] **Step 5: Lint**

Run: `golangci-lint run cmd/tools/play_as/...`
Expected: no new findings

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/demand_report.go cmd/tools/play_as/demand_report_test.go
git commit -m "feat(play_as): demand report builder with SM/above-SM classification"
```

---

## Task 6: Renderers (styled + JSON)

**Files:**
- Create: `cmd/tools/play_as/demand_render.go`

- [ ] **Step 1: Write the renderers**

Create `cmd/tools/play_as/demand_render.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func renderDemandJSON(rep demandReport) string {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\":%q}\n", err.Error())
	}
	return string(b) + "\n"
}

func renderDemandStyled(rep demandReport) string {
	var sb strings.Builder
	if len(rep.Rows) == 0 {
		sb.WriteString("No captured demand matches. Visit stations and run view_market/sellable to fill the ledger, or `demand scan` for full depth.\n")
		return sb.String()
	}
	fmt.Fprintf(&sb, "Demand ledger — %d rows, total fulfillable value %.0f\n", len(rep.Rows), rep.TotalFulfill)
	fmt.Fprintf(&sb, "%-7s %-16s %8s %8s %8s %8s %6s  %-10s\n",
		"CLASS", "ITEM", "PRICE", "DEMAND", "ONHAND", "FILLVAL", "CRAFT", "STATION")
	for _, r := range rep.Rows {
		station := r.StationID
		if r.AgeStale {
			station += " (STALE)"
		}
		craft := ""
		if r.CanCraft > 0 {
			craft = fmt.Sprintf("%d", r.CanCraft)
		}
		name := r.ItemName
		if name == "" {
			name = r.ItemID
		}
		fmt.Fprintf(&sb, "%-7s %-16s %8.0f %8.0f %8.0f %8.0f %6s  %-10s\n",
			r.Class, truncate(name, 16), r.Price, r.Quantity, r.OnHand, r.FulfillValue, craft, station)
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
```

> If a `truncate` helper already exists in `cmd/tools/play_as`, delete this copy and use the existing one. Check with: `grep -rn "func truncate" cmd/tools/play_as/`. If it exists, remove the `truncate` function above and the duplicate will cause a build error pointing you to it.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/tools/play_as/`
Expected: builds (or a "truncate redeclared" error — if so, follow the note above)

- [ ] **Step 3: Lint + commit**

```bash
golangci-lint run cmd/tools/play_as/...
git add cmd/tools/play_as/demand_render.go
git commit -m "feat(play_as): styled and JSON renderers for demand report"
```

---

## Task 7: Compact capture helper

**Files:**
- Create: `cmd/tools/play_as/demand_capture.go`
- Test: `cmd/tools/play_as/demand_capture_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/play_as/demand_capture_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestParseDemandRowsFromCompact(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	raw := []byte(`{"items":[
		{"item_id":"iron_ore","item_name":"Iron Ore","best_buy":10.5,"buy_quantity":100},
		{"item_id":"copper","item_name":"Copper","buy_price":8,"buy_quantity":40},
		{"item_id":"junk","item_name":"Junk","best_buy":0,"buy_quantity":0}
	]}`)

	rows := parseDemandRows(raw, "stn1", "sys1", now)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (junk skipped), got %d: %+v", len(rows), rows)
	}
	byItem := map[string]float64{}
	for _, r := range rows {
		byItem[r.ItemID] = r.BestBuyPrice
		if r.StationID != "stn1" || r.SystemID != "sys1" || !r.CapturedAt.Equal(now) {
			t.Errorf("row metadata wrong: %+v", r)
		}
	}
	if byItem["iron_ore"] != 10.5 {
		t.Errorf("iron price: want 10.5 got %v", byItem["iron_ore"])
	}
	if byItem["copper"] != 8 { // falls back to buy_price when best_buy is 0
		t.Errorf("copper price: want 8 got %v", byItem["copper"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestParseDemandRowsFromCompact -v`
Expected: FAIL — `undefined: parseDemandRows`

- [ ] **Step 3: Write the capture helper**

Create `cmd/tools/play_as/demand_capture.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"time"

	"spacemolt/pkg/game"
	"spacemolt/pkg/game/serverapi"
	"spacemolt/pkg/knowledge"
)

// parseDemandRows turns a compact view_market response (no item_id) into
// MarketDemandRow values, keeping only items with actual buy demand. Uses
// best_buy, falling back to buy_price when best_buy is zero.
func parseDemandRows(raw []byte, stationID, systemID string, now time.Time) []knowledge.MarketDemandRow {
	if len(raw) == 0 || stationID == "" {
		return nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	var out []knowledge.MarketDemandRow
	for _, it := range resp.Items {
		price := it.BestBuy
		if price <= 0 {
			price = float64(it.BuyPrice)
		}
		qty := float64(it.BuyQuantity)
		if price <= 0 || qty <= 0 {
			continue
		}
		out = append(out, knowledge.MarketDemandRow{
			StationID:    stationID,
			SystemID:     systemID,
			ItemID:       it.ItemID,
			ItemName:     it.ItemName,
			BestBuyPrice: price,
			BuyQuantity:  qty,
			CapturedAt:   now,
		})
	}
	return out
}

// captureDemand persists the compact buy-order demand from the client's most
// recent view_market response. Best-effort: silently no-ops when the KB is
// absent, there is no market data, or the player is not at a station.
func captureDemand(client game.GameClient, ctx context.Context) {
	if globalKB == nil {
		return
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return
	}
	state := client.GetState()
	if state == nil {
		return
	}
	rows := parseDemandRows(client.GetRawJSON("market"), state.CurrentPOI, state.CurrentSystem, time.Now())
	if len(rows) == 0 {
		return
	}
	_ = sqlite.UpsertMarketDemand(ctx, rows)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestParseDemandRowsFromCompact -v`
Expected: PASS

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run cmd/tools/play_as/...
git add cmd/tools/play_as/demand_capture.go cmd/tools/play_as/demand_capture_test.go
git commit -m "feat(play_as): compact demand capture helper"
```

---

## Task 8: Deep-scan handler

**Files:**
- Create: `cmd/tools/play_as/demand_scan.go`
- Test: `cmd/tools/play_as/demand_capture_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `cmd/tools/play_as/demand_capture_test.go`:

```go
func TestParseDeepOrders(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	// Single-item view_market response: items[0].buy_orders carries source.
	raw := []byte(`{"items":[{"item_id":"iron_ore","item_name":"Iron Ore","buy_orders":[
		{"price_each":10,"quantity":50,"source":"station"},
		{"price_each":12,"quantity":20,"source":"player"},
		{"price_each":0,"quantity":5,"source":"player"}
	]}]}`)

	rows := parseDeepOrders(raw, "stn1", "sys1", "iron_ore", now)
	if len(rows) != 2 { // zero-price order skipped
		t.Fatalf("want 2 deep orders, got %d: %+v", len(rows), rows)
	}
	if rows[0].Source != "station" || rows[0].PriceEach != 10 || rows[0].Quantity != 50 {
		t.Errorf("row0 wrong: %+v", rows[0])
	}
	if rows[1].ItemName != "Iron Ore" || rows[1].StationID != "stn1" {
		t.Errorf("row1 metadata wrong: %+v", rows[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestParseDeepOrders -v`
Expected: FAIL — `undefined: parseDeepOrders`

- [ ] **Step 3: Write the deep-scan handler**

Create `cmd/tools/play_as/demand_scan.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"spacemolt/pkg/game"
	"spacemolt/pkg/game/serverapi"
	"spacemolt/pkg/knowledge"
)

// parseDeepOrders turns a single-item view_market response (items[0].buy_orders)
// into MarketBuyOrderRow values, skipping zero-price/zero-qty entries.
func parseDeepOrders(raw []byte, stationID, systemID, itemID string, now time.Time) []knowledge.MarketBuyOrderRow {
	if len(raw) == 0 {
		return nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Items) == 0 {
		return nil
	}
	it := resp.Items[0]
	name := it.ItemName
	var out []knowledge.MarketBuyOrderRow
	for _, o := range it.BuyOrders {
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		out = append(out, knowledge.MarketBuyOrderRow{
			StationID: stationID, SystemID: systemID, ItemID: itemID, ItemName: name,
			PriceEach: o.PriceEach, Quantity: o.Quantity, Source: o.Source, CapturedAt: now,
		})
	}
	return out
}

// runDemandScan does an explicit deep pass at the current station: for every
// item with buy demand in the compact summary, it fetches the full order book
// (view_market with item_id) and stores Source-classified rows. This is the
// only chatty path — one server call per item, paced by SleepQuick.
func runDemandScan(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
		return fmt.Errorf("demand scan: no knowledge DB configured (start play_as with --db)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("demand scan: knowledge DB is not SQLite-backed")
	}
	state := client.GetState()
	if state == nil || state.CurrentPOI == "" {
		return fmt.Errorf("demand scan: must be docked at a station")
	}
	stationID, systemID := state.CurrentPOI, state.CurrentSystem

	// 1. Compact summary to discover which items have buy demand.
	if err := client.GetListings(ctx); err != nil {
		return fmt.Errorf("demand scan: view_market: %w", err)
	}
	captureDemand(client, ctx) // also refresh the compact ledger
	items := parseDemandRows(client.GetRawJSON("market"), stationID, systemID, time.Now())
	if len(items) == 0 {
		fmt.Println("demand scan: no buy demand at this station.")
		return nil
	}

	fmt.Printf("demand scan: deep-scanning %d items at %s…\n", len(items), stationID)
	scanned := 0
	for _, it := range items {
		if err := client.ViewMarket(ctx, map[string]any{"item_id": it.ItemID}); err != nil {
			fmt.Printf("  %s: %v (skipped)\n", it.ItemID, err)
			continue
		}
		orders := parseDeepOrders(client.GetRawJSON("market"), stationID, systemID, it.ItemID, time.Now())
		if err := sqlite.ReplaceMarketBuyOrders(ctx, stationID, it.ItemID, orders); err != nil {
			fmt.Printf("  %s: store failed: %v\n", it.ItemID, err)
			continue
		}
		scanned++
		time.Sleep(game.SleepQuick)
	}
	fmt.Printf("demand scan: captured full order depth for %d/%d items.\n", scanned, len(items))
	return nil
}
```

> Note: `SleepQuick` is exported from package `game` (`pkg/game/constants.go`, value `SleepTick/5`), so the call is `game.SleepQuick` with the existing `spacemolt/pkg/game` import — no separate constants import. Do not invent a new sleep value.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestParseDeepOrders -v`
Expected: PASS

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run cmd/tools/play_as/...
git add cmd/tools/play_as/demand_scan.go cmd/tools/play_as/demand_capture_test.go
git commit -m "feat(play_as): demand scan for full Source-classified order depth"
```

---

## Task 9: Report orchestration (`runDemand`) + flag parsing

**Files:**
- Create: `cmd/tools/play_as/demand_cmd.go`

This wires the ledger + live inventory + craftability into `buildDemandReport`. It is network-bound, so it has no unit test; it is exercised manually in Task 11.

- [ ] **Step 1: Write the orchestration + flag parser**

Create `cmd/tools/play_as/demand_cmd.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"spacemolt/pkg/craftplan"
	"spacemolt/pkg/game"
	"spacemolt/pkg/knowledge"
)

// parseDemandOptions converts `demand` flags into demandOptions.
func parseDemandOptions(args []string) (demandOptions, error) {
	opts := demandOptions{sort: sortByProceeds, only: onlyAll}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, val, hasEq := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		next := func() (string, error) {
			if hasEq {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("demand: %s requires a value", arg)
			}
			i++
			return args[i], nil
		}
		switch key {
		case "item":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.item = strings.ToLower(v)
		case "station":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.station = v
		case "min-price":
			v, err := next()
			if err != nil {
				return opts, err
			}
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return opts, fmt.Errorf("demand: --min-price: %w", err)
			}
			opts.minPrice = n
		case "max-age":
			v, err := next()
			if err != nil {
				return opts, err
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				return opts, fmt.Errorf("demand: --max-age: %w", err)
			}
			opts.maxAge = d
		case "limit":
			v, err := next()
			if err != nil {
				return opts, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return opts, fmt.Errorf("demand: --limit: %w", err)
			}
			opts.limit = n
		case "station-only":
			opts.stationOnly = true
		case "only":
			v, err := next()
			if err != nil {
				return opts, err
			}
			switch strings.ToLower(v) {
			case "fulfillable":
				opts.only = onlyFulfillable
			case "craftable":
				opts.only = onlyCraftable
			case "all":
				opts.only = onlyAll
			default:
				return opts, fmt.Errorf("demand: --only must be fulfillable|craftable|all")
			}
		case "sort":
			v, err := next()
			if err != nil {
				return opts, err
			}
			switch strings.ToLower(v) {
			case "price":
				opts.sort = sortByPrice
			case "age":
				opts.sort = sortByAge
			case "proceeds":
				opts.sort = sortByProceeds
			default:
				return opts, fmt.Errorf("demand: --sort must be price|proceeds|age")
			}
		default:
			return opts, fmt.Errorf("demand: unknown flag %q", arg)
		}
	}
	return opts, nil
}

// liveOnHand returns item_id -> (ship cargo + current-station storage) quantity.
func liveOnHand(client game.GameClient, ctx context.Context) map[string]float64 {
	out := map[string]float64{}
	if err := client.GetCargo(ctx); err == nil {
		var resp struct {
			Cargo []storageItem `json:"cargo"`
		}
		if raw := client.GetRawJSON("cargo"); len(raw) > 0 {
			if json.Unmarshal(raw, &resp) == nil {
				for _, c := range resp.Cargo {
					out[c.ItemID] += c.Quantity
				}
			}
		}
	}
	if err := client.ViewStorage(ctx); err == nil {
		var resp struct {
			Items []storageItem `json:"items"`
		}
		if raw := client.GetRawJSON("storage"); len(raw) > 0 {
			if json.Unmarshal(raw, &resp) == nil {
				for _, s := range resp.Items {
					out[s.ItemID] += s.Quantity
				}
			}
		}
	}
	return out
}

// liveCanCraft returns output item_id -> craftable batch count from the
// current inventory/skills (direct recipes only; no BOM/crafting DB needed).
func liveCanCraft(client game.GameClient, ctx context.Context) map[string]int {
	out := map[string]int{}
	src := newPlayAsSource(client, ensureCraftingDB())
	eng := craftplan.New(src)
	rows, _, err := eng.Craftable(ctx, craftplan.CraftableOpts{})
	if err != nil {
		return out
	}
	for _, r := range rows {
		if r.CanMake > out[r.OutputItemID] {
			out[r.OutputItemID] = r.CanMake
		}
	}
	return out
}

// runDemand loads the ledger, scores it against live inventory/craftability,
// and renders the report. Works offline from the ledger; inventory/craft
// signals degrade gracefully to empty when not connected/docked.
func runDemand(client game.GameClient, ctx context.Context, opts demandOptions, format outputFormat) error {
	if globalKB == nil {
		return fmt.Errorf("demand: no knowledge DB configured (start play_as with --db)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("demand: knowledge DB is not SQLite-backed")
	}
	summary, deep, err := sqlite.LoadMarketDemand(ctx)
	if err != nil {
		return fmt.Errorf("demand: load ledger: %w", err)
	}

	onHand := liveOnHand(client, ctx)
	canCraft := liveCanCraft(client, ctx)

	rep := buildDemandReport(summary, deep, onHand, canCraft, time.Now(), opts)

	switch format {
	case formatStyled:
		fmt.Print(renderDemandStyled(rep))
	default:
		fmt.Print(renderDemandJSON(rep))
	}
	return nil
}
```

> **Verify before writing:** confirm `craftplan.CraftableRow` exposes `OutputItemID` and `CanMake` (it does per `pkg/craftplan/types.go`), and that `newPlayAsSource` and `ensureCraftingDB` are in package `main` (they are, in `craftable.go`). If `CraftableOpts{}` with no fields triggers a BOM path, it does not — `Reachable` defaults to false, so `Craftable` returns direct recipes without the crafting DB.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/tools/play_as/`
Expected: builds with no output

- [ ] **Step 3: Lint + commit**

```bash
golangci-lint run cmd/tools/play_as/...
git add cmd/tools/play_as/demand_cmd.go
git commit -m "feat(play_as): runDemand report orchestration and flag parsing"
```

---

## Task 10: Dispatch + capture hooks

**Files:**
- Modify: `cmd/tools/play_as/main.go` (add `case "demand"` in `executeCommand`; add capture hooks to the `view_market` and `dock` cases)
- Modify: `cmd/tools/play_as/sellable.go` (capture hook after market fetch)

- [ ] **Step 1: Add the `demand` dispatch case**

In `cmd/tools/play_as/main.go`, in the `executeCommand` switch, add a new case immediately after the `case "sellable":` block (ends near line 6328):

```go
	case "demand":
		if len(parts) > 1 && strings.ToLower(parts[1]) == "scan" {
			return runDemandScan(client, ctx)
		}
		opts, err := parseDemandOptions(parts[1:])
		if err != nil {
			return err
		}
		return runDemand(client, ctx, opts, format)
```

- [ ] **Step 2: Hook compact capture into the `view_market` case**

In `executeCommand`'s `case "view_market":` (near line 4672), the two return sites call `simpleCommand(...)`. Replace each `return simpleCommand(...)` with a capture-then-return. The no-arg branch becomes:

```go
		if len(parts) < 2 {
			err := simpleCommand(client, func(ctx context.Context) error {
				return client.ViewMarket(ctx, nil)
			}, ctx, 2*time.Second, cmd, format)
			captureDemand(client, ctx)
			return err
		}
```

And the trailing branch (after the payload-building loop) becomes:

```go
		err := simpleCommand(client, func(ctx context.Context) error {
			return client.ViewMarket(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)
		// Only the compact (no item_id) summary feeds the demand ledger; a
		// per-item view_market here is left to `demand scan`.
		if payload["item_id"] == nil {
			captureDemand(client, ctx)
		}
		return err
```

- [ ] **Step 3: Hook capture into the `dock` case**

Replace the `case "dock":` block (near line 4465) with one that, after a successful dock, best-effort fetches the market and captures demand:

```go
	case "dock":
		err := simpleCommand(client, func(ctx context.Context) error {
			err := client.Dock(ctx)
			return reconcileDockState(ctx, client, "dock", err)
		}, ctx, 3*time.Second, cmd, format)
		if err == nil {
			// Best-effort: pull the local market so the demand ledger fills as
			// you travel. Stations without a market simply error and are ignored.
			if mErr := client.GetListings(ctx); mErr == nil {
				captureDemand(client, ctx)
			}
		}
		return err
```

- [ ] **Step 4: Hook capture into `sellable`**

In `cmd/tools/play_as/sellable.go`, inside `runSellable`, immediately after the market is fetched and `marketRaw` is unmarshalled (after the `view_market` block near line 40, before the `get_cargo` step), add:

```go
	// The demand ledger is fed opportunistically whenever we read a market.
	captureDemand(client, ctx)
```

- [ ] **Step 5: Verify it compiles**

Run: `go build ./cmd/tools/play_as/`
Expected: builds with no output

- [ ] **Step 6: Run the full package tests**

Run: `go test ./cmd/tools/play_as/`
Expected: PASS (all existing + new tests)

- [ ] **Step 7: Lint + commit**

```bash
golangci-lint run cmd/tools/play_as/...
git add cmd/tools/play_as/main.go cmd/tools/play_as/sellable.go
git commit -m "feat(play_as): wire demand command + auto-capture on view_market/dock/sellable"
```

---

## Task 11: Full-build gate + manual verification

**Files:** none (verification only)

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: builds with no output

- [ ] **Step 2: Full test suite**

Run: `go test ./...`
Expected: PASS (no failures introduced; note: this also catches any `knowledge.Base` mock that needs no change since we added methods only to `*SQLiteKB`)

- [ ] **Step 3: Full lint**

Run: `golangci-lint run ./...`
Expected: no new findings versus baseline

- [ ] **Step 4: Build the play_as binary to bin/**

Run: `go build -o bin/play_as ./cmd/tools/play_as`
Expected: binary at `bin/play_as` (never the repo root, per CLAUDE.md)

- [ ] **Step 5: Manual smoke (requires credentials + a DB path)**

Document, do not script. With a live agent session started via `play_as ... --db <path>`:
1. Dock at a station with a market → confirm no errors; run `demand` → the station's items appear with class `?` (compact).
2. Run `demand scan` → confirm per-item progress and a final "captured full order depth for N/N items" line.
3. Run `demand` again → station-sourced items now show `STN`; any player order above the station price shows `PLR>SM`.
4. Run `demand --only fulfillable` → only rows where you hold cargo/storage; `demand --station-only` → only `STN` rows; `demand --sort price` → ordered by price desc.
5. Run `format json` then `demand` → valid JSON with `rows`, `total_fulfill`, `generated`.

- [ ] **Step 6: Final commit (if any verification fixups were needed)**

```bash
git add -A
git commit -m "chore(demand): verification fixups"
```

---

## Self-Review Notes (for the implementer)

- **No interface changes:** New KB methods are on `*SQLiteKB` only (matching `faction_orders`). `play_as` type-asserts `globalKB.(*knowledge.SQLiteKB)` (precedent: `kb_update.go:505`). This is a deliberate deviation from the spec's "add to the `Base` interface" note — it avoids touching `MemoryKB` and any `knowledge.Base` mocks. `go test ./...` in Task 11 confirms nothing else broke.
- **Type consistency:** `demandReportRow`, `demandOptions`, `demandClass` constants (`classStation`/`classAboveSM`/`classPlayer`/`classUnknown`), `demandSort`, `onlyFilter` are defined once in Task 5 and referenced unchanged in Tasks 6/9/10. `MarketDemandRow`/`MarketBuyOrderRow` defined in Task 2, used in 3/4/5/7/8/9.
- **Capture depth:** compact path (`parseDemandRows`) writes `market_buy_demand`; deep path (`parseDeepOrders`) writes `market_buy_orders`. Report merges, preferring deep classification.
- **Phase 2 (faction storage) is intentionally not in this plan** — it is a separate spec section and a future plan.
```
