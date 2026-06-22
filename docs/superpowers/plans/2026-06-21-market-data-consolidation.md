# Market Data Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `pkg/market` (`data/market.db`) the single source of volatile market data by moving market snapshots, LLM analysis, and best-price queries out of the knowledge DB, retiring dead price-trend code, and fixing the hardcoded market DB path.

**Architecture:** Direct injection. Add the needed read/write methods to `pkg/market.Collector` (additive, no breakage), rewire the ~8 writers/readers to take a `*market.Collector`, then remove the market surface from `knowledge.Base`/`SQLiteKB`/`MemoryKB` and drop the knowledge market tables via a migration. Fresh cutover — no data migration.

**Tech Stack:** Go 1.24+, `modernc.org/sqlite`, standard `database/sql`.

## Global Constraints

- Go 1.24+; use `range`-over-int and `b.Loop()` where applicable.
- Sleeps/pauses must use constants from `pkg/game/constants.go` (none expected in this work).
- All new code must pass `golangci-lint` with no new findings.
- After each series of changes run `go build ./...` and `go test ./...` (not just build — interface changes break mocks the build alone misses).
- SQLite timestamps are RFC3339 UTC strings (matches existing `pkg/market` convention).
- Spec: `docs/superpowers/specs/2026-06-21-market-data-consolidation-design.md`.

**Port set (have live callers):** `GetLatestSnapshot`, `HasSnapshotToday`, `FindBestPrices`, `StoreAnalysis`, `GetLatestAnalysis` (+ existing `WriteSnapshot`).
**Delete as dead (0 callers):** `GetMarketSnapshots`, `GetMarketItems`, `GetMarketAnalysisHistory`, `AnalyzePriceTrends`, and helpers `ShouldRefreshMarket`, `GetMarketAge`, `ShouldRefreshMarketAnalysis`, `GetMarketAnalysisAge`.
**Leave untouched:** `base_market`/`bases`, demand ledger (`demand_*.go` + `market_buy/sell_*` tables), `import-base-data`.

---

### Task 1: `pkg/market` snapshot read API — `GetLatestSnapshot` + `HasSnapshotToday`

**Files:**
- Modify: `pkg/market/query.go`
- Test: `pkg/market/query_test.go`

**Interfaces:**
- Consumes: existing `Collector`, `MarketSnapshot{StationID,StationName,SystemID,SystemName,Orders,CapturedAt}`, `Order`, `WriteSnapshot`.
- Produces:
  - `func (c *Collector) GetLatestSnapshot(ctx context.Context, stationID string) (*MarketSnapshot, error)` — most recent capture for a station, or `(nil, nil)` if none.
  - `func (c *Collector) HasSnapshotToday(ctx context.Context, stationID string) (bool, error)` — true if any order for the station was captured today (UTC).

- [ ] **Step 1: Write the failing tests**

Add to `pkg/market/query_test.go`:

```go
func TestGetLatestSnapshot_Empty(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.GetLatestSnapshot(context.Background(), "stn1")
	if err != nil {
		t.Fatalf("GetLatestSnapshot failed: %v", err)
	}
	if snap != nil {
		t.Errorf("expected nil snapshot, got %+v", snap)
	}
}

func TestGetLatestSnapshot_ReturnsNewestCapture(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	older := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)

	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
		CapturedAt: older,
		Orders:     []Order{{StationID: "stn1", ItemID: "iron", ItemName: "Iron", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: older}},
	}); err != nil {
		t.Fatalf("WriteSnapshot older: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
		CapturedAt: newer,
		Orders:     []Order{{StationID: "stn1", ItemID: "iron", ItemName: "Iron", Side: "sell", PriceEach: 7, Quantity: 12, CapturedAt: newer}},
	}); err != nil {
		t.Fatalf("WriteSnapshot newer: %v", err)
	}

	snap, err := c.GetLatestSnapshot(ctx, "stn1")
	if err != nil {
		t.Fatalf("GetLatestSnapshot failed: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snap.StationName != "One" || snap.SystemID != "sys1" {
		t.Errorf("station/system not populated: %+v", snap)
	}
	if len(snap.Orders) != 1 || snap.Orders[0].PriceEach != 7 {
		t.Errorf("expected newest order price 7, got %+v", snap.Orders)
	}
}

func TestHasSnapshotToday(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
		CapturedAt: now,
		Orders:     []Order{{StationID: "stn1", ItemID: "iron", ItemName: "Iron", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	has, err := c.HasSnapshotToday(ctx, "stn1")
	if err != nil {
		t.Fatalf("HasSnapshotToday failed: %v", err)
	}
	if !has {
		t.Error("expected HasSnapshotToday=true")
	}
	hasOther, err := c.HasSnapshotToday(ctx, "stn-absent")
	if err != nil {
		t.Fatalf("HasSnapshotToday(absent) failed: %v", err)
	}
	if hasOther {
		t.Error("expected false for station with no orders")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/market/ -run 'GetLatestSnapshot|HasSnapshotToday' -v`
Expected: FAIL — `c.GetLatestSnapshot undefined`, `c.HasSnapshotToday undefined`.

- [ ] **Step 3: Implement the methods**

Append to `pkg/market/query.go`:

```go
// GetLatestSnapshot returns the most recent captured market state for a
// station, reconstructed from the orders sharing the newest captured_at.
// Returns (nil, nil) when the station has no orders.
func (c *Collector) GetLatestSnapshot(ctx context.Context, stationID string) (*MarketSnapshot, error) {
	var latest string
	err := c.db.QueryRowContext(ctx,
		`SELECT MAX(captured_at) FROM market_orders WHERE station_id = ?`, stationID).Scan(&latest)
	if err == sql.ErrNoRows || latest == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest captured_at: %w", err)
	}

	snap := &MarketSnapshot{StationID: stationID}
	_ = c.db.QueryRowContext(ctx,
		`SELECT station_name, system_id, system_name FROM stations WHERE station_id = ?`, stationID).
		Scan(&snap.StationName, &snap.SystemID, &snap.SystemName)
	snap.CapturedAt, _ = time.Parse(time.RFC3339, latest)

	rows, err := c.db.QueryContext(ctx, `
		SELECT station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at
		FROM market_orders
		WHERE station_id = ? AND captured_at = ?
	`, stationID, latest)
	if err != nil {
		return nil, fmt.Errorf("query latest orders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var o Order
		var capStr string
		if err := rows.Scan(&o.StationID, &o.ItemID, &o.Side, &o.PriceEach, &o.Quantity, &o.MyQuantity, &o.Source, &capStr); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		o.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		snap.Orders = append(snap.Orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orders: %w", err)
	}
	return snap, nil
}

// HasSnapshotToday reports whether any order for the station was captured
// today (UTC).
func (c *Collector) HasSnapshotToday(ctx context.Context, stationID string) (bool, error) {
	startOfDay := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	var n int
	err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM market_orders WHERE station_id = ? AND captured_at >= ?`,
		stationID, startOfDay).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count today's orders: %w", err)
	}
	return n > 0, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/market/ -run 'GetLatestSnapshot|HasSnapshotToday' -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/query.go pkg/market/query_test.go
git commit -m "feat(market): add GetLatestSnapshot + HasSnapshotToday read API"
```

---

### Task 2: `pkg/market` `FindBestPrices` + `BestPrice` type

**Files:**
- Modify: `pkg/market/types.go`, `pkg/market/query.go`
- Test: `pkg/market/query_test.go`

**Interfaces:**
- Produces:
  - `type BestPrice struct { ItemID, StationID, StationName, SystemID, SystemName string; Price, Quantity float64; ListingType string; CapturedAt time.Time }`
  - `func (c *Collector) FindBestPrices(ctx context.Context, itemID, side string, limit int) ([]BestPrice, error)` — across stations, the best prices for an item on the given side (`"sell"` → lowest price ascending; `"buy"` → highest price descending), using each station's latest capture.

- [ ] **Step 1: Write the failing test**

Add to `pkg/market/query_test.go`:

```go
func TestFindBestPrices(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	now := time.Now().UTC()
	write := func(stn string, price float64) {
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: stn, StationName: stn, SystemID: "sys", SystemName: "S",
			CapturedAt: now,
			Orders:     []Order{{StationID: stn, ItemID: "iron", ItemName: "Iron", Side: "sell", PriceEach: price, Quantity: 10, CapturedAt: now}},
		}); err != nil {
			t.Fatalf("WriteSnapshot %s: %v", stn, err)
		}
	}
	write("stnA", 9)
	write("stnB", 4)
	write("stnC", 7)

	best, err := c.FindBestPrices(ctx, "iron", "sell", 2)
	if err != nil {
		t.Fatalf("FindBestPrices failed: %v", err)
	}
	if len(best) != 2 {
		t.Fatalf("expected 2 results, got %d", len(best))
	}
	if best[0].StationID != "stnB" || best[0].Price != 4 {
		t.Errorf("cheapest sell should be stnB@4, got %+v", best[0])
	}
	if best[0].ListingType != "sell" || best[0].ItemID != "iron" {
		t.Errorf("metadata not populated: %+v", best[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run FindBestPrices -v`
Expected: FAIL — `c.FindBestPrices undefined`, `BestPrice` undefined.

- [ ] **Step 3: Implement type + method**

Append to `pkg/market/types.go`:

```go
// BestPrice is the best available price for an item at a station, used for
// cross-station comparison.
type BestPrice struct {
	ItemID      string    `json:"item_id"`
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	Price       float64   `json:"price"`
	Quantity    float64   `json:"quantity"`
	ListingType string    `json:"listing_type"` // "buy" or "sell"
	CapturedAt  time.Time `json:"captured_at"`
}
```

Append to `pkg/market/query.go`:

```go
// FindBestPrices returns the best prices for an item on the given side across
// all stations, using each station's most recent order for that item.
// side "sell" ranks ascending (cheapest first); "buy" ranks descending.
func (c *Collector) FindBestPrices(ctx context.Context, itemID, side string, limit int) ([]BestPrice, error) {
	order := "ASC"
	if side == "buy" {
		order = "DESC"
	}
	// Latest order per station for this item+side, then rank by price.
	query := `
		SELECT mo.station_id, COALESCE(s.station_name, mo.station_id),
		       COALESCE(s.system_id, ''), COALESCE(s.system_name, ''),
		       mo.price_each, mo.quantity, mo.side, mo.captured_at
		FROM market_orders mo
		JOIN (
			SELECT station_id, MAX(captured_at) AS mx
			FROM market_orders
			WHERE item_id = ? AND side = ?
			GROUP BY station_id
		) latest ON latest.station_id = mo.station_id AND latest.mx = mo.captured_at
		LEFT JOIN stations s ON s.station_id = mo.station_id
		WHERE mo.item_id = ? AND mo.side = ?
		ORDER BY mo.price_each ` + order + `
		LIMIT ?`
	rows, err := c.db.QueryContext(ctx, query, itemID, side, itemID, side, limit)
	if err != nil {
		return nil, fmt.Errorf("query best prices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []BestPrice
	for rows.Next() {
		bp := BestPrice{ItemID: itemID}
		var capStr string
		if err := rows.Scan(&bp.StationID, &bp.StationName, &bp.SystemID, &bp.SystemName,
			&bp.Price, &bp.Quantity, &bp.ListingType, &capStr); err != nil {
			return nil, fmt.Errorf("scan best price: %w", err)
		}
		bp.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		out = append(out, bp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate best prices: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run FindBestPrices -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/types.go pkg/market/query.go pkg/market/query_test.go
git commit -m "feat(market): add FindBestPrices + BestPrice type"
```

---

### Task 3: `pkg/market` analysis storage — `analyses` table + `MarketAnalysis`

**Files:**
- Modify: `pkg/market/schema.sql`, `pkg/market/types.go`, `pkg/market/query.go`
- Test: `pkg/market/analysis_test.go` (create)

**Interfaces:**
- Produces:
  - `type MarketAnalysis struct { SystemID, SystemName, StationID, StationName string; GameTick int64; CapturedAt time.Time; AgentID string; Mode string; SkillLevel int; ScanningRange string; StationsInRange, ItemsScanned int; TopInsights []map[string]any; TotalItems, TotalPages, Page int; Hint string; XPGained, AnalysisData map[string]any }`
  - `func (c *Collector) StoreAnalysis(ctx context.Context, a MarketAnalysis) error`
  - `func (c *Collector) GetLatestAnalysis(ctx context.Context, stationID string) (*MarketAnalysis, error)` — `(nil, nil)` if none.

- [ ] **Step 1: Add the schema**

Append to `pkg/market/schema.sql`:

```sql
-- LLM market analysis (analyze_market output)
CREATE TABLE IF NOT EXISTS analyses (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    station_id        TEXT NOT NULL,
    station_name      TEXT,
    system_id         TEXT,
    system_name       TEXT,
    game_tick         INTEGER NOT NULL,
    captured_at       TEXT NOT NULL,
    agent_id          TEXT,
    mode              TEXT,
    skill_level       INTEGER,
    scanning_range    TEXT,
    stations_in_range INTEGER,
    items_scanned     INTEGER,
    top_insights      TEXT,
    total_items       INTEGER,
    total_pages       INTEGER,
    page              INTEGER,
    hint              TEXT,
    xp_gained         TEXT,
    analysis_data     TEXT
);

CREATE INDEX IF NOT EXISTS idx_analyses_station_time ON analyses(station_id, captured_at);
```

- [ ] **Step 2: Write the failing test**

Create `pkg/market/analysis_test.go`:

```go
package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAndGetLatestAnalysis(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()

	if got, err := c.GetLatestAnalysis(ctx, "stn1"); err != nil || got != nil {
		t.Fatalf("empty GetLatestAnalysis = (%v, %v), want (nil, nil)", got, err)
	}

	older := MarketAnalysis{
		StationID: "stn1", SystemID: "sys1", GameTick: 100,
		CapturedAt: time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC),
		Mode:       "basic", SkillLevel: 2, ItemsScanned: 10, Hint: "old",
		TopInsights:  []map[string]any{{"item": "iron", "score": 1.0}},
		XPGained:     map[string]any{"trading": 5},
		AnalysisData: map[string]any{"k": "v"},
	}
	newer := older
	newer.CapturedAt = time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	newer.Hint = "new"

	if err := c.StoreAnalysis(ctx, older); err != nil {
		t.Fatalf("StoreAnalysis older: %v", err)
	}
	if err := c.StoreAnalysis(ctx, newer); err != nil {
		t.Fatalf("StoreAnalysis newer: %v", err)
	}

	got, err := c.GetLatestAnalysis(ctx, "stn1")
	if err != nil {
		t.Fatalf("GetLatestAnalysis: %v", err)
	}
	if got == nil || got.Hint != "new" {
		t.Fatalf("expected newest analysis (hint=new), got %+v", got)
	}
	if got.SkillLevel != 2 || len(got.TopInsights) != 1 || got.AnalysisData["k"] != "v" {
		t.Errorf("fields not round-tripped: %+v", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/market/ -run Analysis -v`
Expected: FAIL — `MarketAnalysis` / `StoreAnalysis` / `GetLatestAnalysis` undefined.

- [ ] **Step 4: Add type + methods**

Append to `pkg/market/types.go`:

```go
// MarketAnalysis is LLM-generated market analysis (analyze_market output).
type MarketAnalysis struct {
	SystemID        string
	SystemName      string
	StationID       string
	StationName     string
	GameTick        int64
	CapturedAt      time.Time
	AgentID         string
	Mode            string
	SkillLevel      int
	ScanningRange   string
	StationsInRange int
	ItemsScanned    int
	TopInsights     []map[string]any
	TotalItems      int
	TotalPages      int
	Page            int
	Hint            string
	XPGained        map[string]any
	AnalysisData    map[string]any
}
```

Append to `pkg/market/query.go` (ensure `encoding/json` is imported):

```go
// StoreAnalysis inserts an LLM market-analysis record.
func (c *Collector) StoreAnalysis(ctx context.Context, a MarketAnalysis) error {
	insights, _ := json.Marshal(a.TopInsights)
	xp, _ := json.Marshal(a.XPGained)
	data, _ := json.Marshal(a.AnalysisData)
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO analyses (station_id, station_name, system_id, system_name,
				game_tick, captured_at, agent_id, mode, skill_level, scanning_range,
				stations_in_range, items_scanned, top_insights, total_items, total_pages,
				page, hint, xp_gained, analysis_data)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.StationID, a.StationName, a.SystemID, a.SystemName,
			a.GameTick, a.CapturedAt.UTC().Format(time.RFC3339), a.AgentID, a.Mode,
			a.SkillLevel, a.ScanningRange, a.StationsInRange, a.ItemsScanned,
			string(insights), a.TotalItems, a.TotalPages, a.Page, a.Hint,
			string(xp), string(data))
		return err
	})
}

// GetLatestAnalysis returns the most recent analysis for a station, or (nil, nil).
func (c *Collector) GetLatestAnalysis(ctx context.Context, stationID string) (*MarketAnalysis, error) {
	var a MarketAnalysis
	var capStr, insights, xp, data string
	err := c.db.QueryRowContext(ctx, `
		SELECT station_id, station_name, system_id, system_name, game_tick, captured_at,
		       agent_id, mode, skill_level, scanning_range, stations_in_range, items_scanned,
		       top_insights, total_items, total_pages, page, hint, xp_gained, analysis_data
		FROM analyses WHERE station_id = ?
		ORDER BY captured_at DESC LIMIT 1`, stationID).
		Scan(&a.StationID, &a.StationName, &a.SystemID, &a.SystemName, &a.GameTick, &capStr,
			&a.AgentID, &a.Mode, &a.SkillLevel, &a.ScanningRange, &a.StationsInRange, &a.ItemsScanned,
			&insights, &a.TotalItems, &a.TotalPages, &a.Page, &a.Hint, &xp, &data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest analysis: %w", err)
	}
	a.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
	_ = json.Unmarshal([]byte(insights), &a.TopInsights)
	_ = json.Unmarshal([]byte(xp), &a.XPGained)
	_ = json.Unmarshal([]byte(data), &a.AnalysisData)
	return &a, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/market/ -run Analysis -v`
Expected: PASS.

- [ ] **Step 6: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/schema.sql pkg/market/types.go pkg/market/query.go pkg/market/analysis_test.go
git commit -m "feat(market): add analyses table + StoreAnalysis/GetLatestAnalysis"
```

---

### Task 4: DB-path cleanup (`#4`) — relative default + `--market-db-path` flag

**Files:**
- Modify: `pkg/market/collector.go:26-31` (`DefaultConfig`)
- Modify: `cmd/tools/play_as/main.go` (flag + injected collector)
- Test: `pkg/market/collector_test.go`

**Interfaces:**
- Produces: `DefaultConfig().DBPath == "data/market.db"` (relative, like `knowledge` default). A package-level `globalMarketCollector` in play_as constructed once from a `--market-db-path` flag.

- [ ] **Step 1: Write the failing test**

Add to `pkg/market/collector_test.go`:

```go
func TestDefaultConfigPathIsRelative(t *testing.T) {
	got := DefaultConfig().DBPath
	if got != "data/market.db" {
		t.Errorf("DefaultConfig().DBPath = %q, want \"data/market.db\"", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestDefaultConfigPathIsRelative -v`
Expected: FAIL — path is the hardcoded `$HOME/...` value.

- [ ] **Step 3: Fix `DefaultConfig`**

In `pkg/market/collector.go`, change the `DBPath` line in `DefaultConfig()`:

```go
		DBPath:       filepath.Join("data", "market.db"),
```

Remove the now-unused `os` import if `os` is no longer referenced in the file (run `goimports`/build to confirm).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestDefaultConfigPathIsRelative -v`
Expected: PASS.

- [ ] **Step 5: Add the play_as flag + injected collector**

In `cmd/tools/play_as/main.go`, near the existing `dbPath` flag (~line 92):

```go
	marketDBPath := flag.String("market-db-path", "data/market.db", "Path to the separate market database")
```

After the knowledge base is opened (~line 198), construct the collector once:

```go
	if *marketDBPath != "" {
		mc, err := market.Open(market.Config{DBPath: *marketDBPath})
		if err != nil {
			logger.Printf("Warning: failed to open market db at %s: %v", *marketDBPath, err)
		} else {
			globalMarketCollector = mc
			logger.Printf("Market database loaded: %s", *marketDBPath)
		}
	}
```

In the `update_market` case (~line 5912), delete the lazy-open block so it uses the pre-constructed `globalMarketCollector`, returning a clear error if nil:

```go
	case "update_market":
		if globalMarketCollector == nil {
			return fmt.Errorf("update_market: market db not configured (set --market-db-path)")
		}
		if err := simpleCommand(client, func(ctx context.Context) error {
			return client.ViewMarket(ctx, nil)
		}, ctx, 2*time.Second, cmd, format); err != nil {
			return err
		}
		if err := market.CaptureFromClient(ctx, client, globalMarketCollector); err != nil {
			return fmt.Errorf("update_market: capture: %w", err)
		}
		station := ""
		if state := client.GetState(); state != nil {
			station = state.CurrentPOI
		}
		fmt.Printf("✓ Captured market data for %s\n", station)
		return nil
```

- [ ] **Step 6: Build + lint + commit**

Run: `go build ./... && go test ./pkg/market/... ./cmd/tools/play_as/...`
Expected: PASS.

```bash
golangci-lint run ./pkg/market/... ./cmd/tools/play_as/...
git add pkg/market/collector.go pkg/market/collector_test.go cmd/tools/play_as/main.go
git commit -m "fix(market): relative default DB path + injected play_as collector (#4)"
```

---

### Task 5: Rewire market writers to `*market.Collector`

**Re-slice note (from execution discovery):** the only snapshot writer in `pkg/worker` is `KBUpdateStation` (`capture.go:499`), reached via `KBUpdateAll` ← `pkg/worker/dispatch.go` + play_as wrappers. `CaptureMarket` (`capture.go:665`) is the **demand ledger** (deferred, leave alone). The agent writer `CaptureMarketData` is called by `RefreshMarketData` (a Task-6 reader), so the whole agent chain moves in **Task 6**, not here. **Task 5 scope = worker path + auto-explorer only.**

**Files:**
- Modify: `pkg/worker/capture.go` (delete `convertMarketListings`; rewrite the snapshot-write block in `KBUpdateStation` ~490-503; add `mc *market.Collector` param to `KBUpdateStation` and `KBUpdateAll`)
- Modify: `pkg/worker/dispatch.go` (add `Market *market.Collector` field to `WorkerDispatch`; pass `d.Market` to `KBUpdateAll`)
- Modify: `cmd/worker/main.go` (construct + set `WorkerDispatch.Market` from `--market-db-path`, default `data/market.db`)
- Modify: play_as `kbUpdateAll`/`kbUpdateStation` wrapper(s) (pass `globalMarketCollector`)
- Modify: `cmd/auto-explorer/main.go:380,444-470` (inline writer → `mc.WriteSnapshot`; `HasMarketSnapshotToday` → `mc.HasSnapshotToday`; construct `mc` from a new `--market-db-path` flag)
- **NOT** `pkg/agent/market_capture.go` — moved to Task 6.

**Interfaces:**
- Consumes: `market.Collector.WriteSnapshot`, `market.Order`, `market.MarketSnapshot`, `market.HasSnapshotToday`.
- Produces (changed signatures):
  - `func KBUpdateStation(ctx context.Context, client game.GameClient, kb knowledge.Base, mc *market.Collector, source string) error`
  - `func KBUpdateAll(ctx context.Context, client game.GameClient, kb knowledge.Base, mc *market.Collector, detectedBy string) error`
  - `WorkerDispatch` gains field `Market *market.Collector`.
  - **shared** converter `func market.OrdersFromListings(stationID string, gameListings []game.MarketListing, source string, capturedAt time.Time) []market.Order` (lives in `pkg/market`, used by `pkg/worker` here and `pkg/agent` in Task 6 — `pkg/market` already imports `pkg/game`).

Success bar for this task: `go build ./...` and `go test ./...` both green. The knowledge `StoreMarketSnapshot`/`HasMarketSnapshotToday` methods still EXIST (removed in Task 7), so the untouched agent path keeps compiling.

- [ ] **Step 1: Add a shared game→market order converter in `pkg/market` with a test**

Create `pkg/market/convert.go`:

```go
package market

import (
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// OrdersFromListings maps game market listings to market.Order rows.
// source tags provenance (e.g. "play_as", "agent").
func OrdersFromListings(stationID string, gameListings []game.MarketListing, source string, capturedAt time.Time) []Order {
	orders := make([]Order, 0, len(gameListings))
	for _, l := range gameListings {
		orders = append(orders, Order{
			StationID:  stationID,
			ItemID:     l.ItemID,
			ItemName:   l.ItemID,
			Side:       l.Type, // "buy" or "sell"
			PriceEach:  l.PricePerUnit,
			Quantity:   l.Quantity,
			Source:     source,
			CapturedAt: capturedAt,
		})
	}
	return orders
}
```

Create `pkg/market/convert_test.go`:

```go
package market

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestOrdersFromListings(t *testing.T) {
	now := time.Now().UTC()
	in := []game.MarketListing{{ItemID: "iron", Type: "sell", PricePerUnit: 5, Quantity: 10}}
	out := OrdersFromListings("stn1", in, "play_as", now)
	if len(out) != 1 {
		t.Fatalf("expected 1 order, got %d", len(out))
	}
	o := out[0]
	if o.StationID != "stn1" || o.ItemID != "iron" || o.Side != "sell" || o.PriceEach != 5 || o.Quantity != 10 || o.Source != "play_as" {
		t.Errorf("bad conversion: %+v", o)
	}
}
```

Note: confirm `pkg/market` importing `pkg/game` does not create an import cycle (it should not — `pkg/game` does not import `pkg/market`; `capture.go` already imports `game`).

- [ ] **Step 2: Run test to verify it fails then passes**

Run: `go test ./pkg/market/ -run TestOrdersFromListings -v`
Expected: FAIL (file not yet present) → after creating both files, PASS.

- [ ] **Step 3: Rewire the worker capture write path**

In `pkg/worker/capture.go`, replace the market-listings block (~490-503) so it writes via the collector. The capture entry point must receive a `*market.Collector` — locate the function holding this block (the KB capture routine) and add an `mc *market.Collector` parameter, threading it from its caller. Replace:

```go
		listings := client.GetMarketListings()
		snapshot := convertMarketListings(systemID, systemName, poiID, poiName, currentTick(state), listings)
		if err := kb.StoreMarketSnapshot(ctx, snapshot, source); err != nil {
			fmt.Printf("Warning: failed to save market snapshot: %v\n", err)
		} else {
			fmt.Printf("Saved market snapshot: %d listings\n", len(listings))
		}
```

with:

```go
		listings := client.GetMarketListings()
		if mc != nil {
			now := time.Now().UTC()
			snap := market.MarketSnapshot{
				StationID: poiID, StationName: poiName,
				SystemID: systemID, SystemName: systemName,
				CapturedAt: now,
				Orders:     market.OrdersFromListings(poiID, listings, "play_as", now),
			}
			if err := mc.WriteSnapshot(ctx, snap); err != nil {
				fmt.Printf("Warning: failed to save market snapshot: %v\n", err)
			} else {
				fmt.Printf("Saved market snapshot: %d listings\n", len(listings))
			}
		}
```

Delete the now-unused `convertMarketListings` function (`capture.go:50-71`) and add `"github.com/rsned/spacemolt/pkg/market"` to imports.

- [ ] **Step 4: Thread `mc` through the worker dispatch + callers**

Add a `Market *market.Collector` field to `WorkerDispatch` (`pkg/worker/dispatch.go`). In its `update_all`/`update_station` cases pass `d.Market` to `KBUpdateAll`/`KBUpdateStation`. In `cmd/worker/main.go`, open a `*market.Collector` from a `--market-db-path` flag (default `data/market.db`) and set it on the `WorkerDispatch`. In play_as, update the `kbUpdateAll`/`kbUpdateStation` wrapper(s) to pass `globalMarketCollector`.

(The agent writer `CaptureMarketData` is intentionally NOT changed here — it moves to Task 6 with the rest of the agent read+write chain, since its caller `RefreshMarketData` is a Task-6 reader. It keeps using `kb.StoreMarketSnapshot`, which still exists until Task 7, so the build stays green.)

- [ ] **Step 5: Rewire auto-explorer writer + `HasSnapshotToday`**

In `cmd/auto-explorer/main.go`: open a `*market.Collector` in `main` from a new `--market-db-path` flag (default `data/market.db`); thread it to the function holding lines 380/444. Replace `kb.HasMarketSnapshotToday(ctx, state.System.ID, poiID)` with `mc.HasSnapshotToday(ctx, poiID)`. Replace the inline `convertMarketListingsToKnowledge` + `StoreMarketSnapshot` write with a `mc.WriteSnapshot` using a `market.MarketSnapshot` (reuse the same converter shape as Step 4). Delete `convertMarketListingsToKnowledge` (444-470).

- [ ] **Step 6: Build, test, commit**

Run: `go build ./... && go test ./pkg/worker/... ./pkg/agent/... ./cmd/auto-explorer/...`
Expected: PASS (knowledge market methods still exist; only writers moved).

```bash
golangci-lint run ./pkg/worker/... ./pkg/agent/... ./cmd/auto-explorer/...
git add -A
git commit -m "refactor(market): rewire snapshot writers to market.Collector"
```

---

### Task 6: Rewire market readers to `*market.Collector`

**Files:**
- Modify: `pkg/agentstate/agentstate.go` (struct + `New`/`NewWithAgent`), `pkg/agentstate/refresh.go:165-183`, `pkg/agentstate/accessors.go:186-192`
- Modify: `pkg/agent/market_refresh.go:33-70`, `pkg/agent/market_analysis.go:39-189`
- Modify: `pkg/agent/market_capture.go` (`CaptureMarketData` — moved here from Task 5; take `mc *market.Collector`, write the snapshot via `mc.WriteSnapshot` using `market.OrdersFromListings`, drop `kb.StoreMarketSnapshot`)
- Modify: `cmd/auto-craftsman/profit_selector.go:33`, `cmd/auto-craftsman/main.go` (collector construction)
- Modify: `pkg/unified/server.go:84` (pass collector to `agentstate.New`)

**Whole agent chain moves together:** `RefreshMarketData` (reader) calls `CaptureMarketData` (writer) and is called by `profit_selector`. Rewire all three plus `market_analysis` and `agentstate` to `*market.Collector` in this task so the build stays green. The `CaptureMarketData` write uses the shared `market.OrdersFromListings` converter from Task 5.

**Interfaces:**
- Consumes: `market.GetLatestSnapshot`, `market.GetLatestAnalysis`, `market.FindBestPrices`, `market.StoreAnalysis`.
- Produces (changed signatures):
  - `agentstate.New(state *game.State, kb knowledge.Base, mc *market.Collector) *AgentState`
  - `agentstate.NewWithAgent(state *game.State, kb knowledge.Base, mc *market.Collector, agentCtx *AgentContext) *AgentState`
  - `AgentState.enriched` market fields become `*market.MarketSnapshot`, `*market.MarketAnalysis`, `[]market.BestPrice`.
  - `func (s *AgentState) MarketSnapshot() *market.MarketSnapshot`, `func (s *AgentState) BestSellsForCargo() []market.BestPrice`.
  - `func RefreshMarketData(ctx, client *game.Client, mc *market.Collector, agentID string) (*market.MarketSnapshot, error)`
  - `func RefreshMarketAnalysis(ctx, client *game.Client, mc *market.Collector, agentID string) (*market.MarketAnalysis, error)`
  - `func CaptureMarketAnalysis(ctx, client *game.Client, mc *market.Collector, agentID string) error`

- [ ] **Step 1: Migrate `agentstate` struct + accessors + refresh**

In `pkg/agentstate/agentstate.go`: change the enriched fields:

```go
	MarketSnapshot  *market.MarketSnapshot
	MarketAnalysis  *market.MarketAnalysis
	NearbyBestBuys  []market.BestPrice
	NearbyBestSells []market.BestPrice
```

Add an `mc *market.Collector` field; update `New`/`NewWithAgent` to accept and store it. Import `pkg/market`.

In `pkg/agentstate/refresh.go:165-183`, read from the collector:

```go
	s.enriched.MarketSnapshot, _ = s.mc.GetLatestSnapshot(ctx, stationID)
	s.enriched.MarketAnalysis, _ = s.mc.GetLatestAnalysis(ctx, stationID)

	s.enriched.NearbyBestSells = nil
	for _, item := range state.Ship.Cargo {
		if prices, err := s.mc.FindBestPrices(ctx, item.ItemID, "buy", 3); err == nil {
			s.enriched.NearbyBestSells = append(s.enriched.NearbyBestSells, prices...)
		}
	}
```

(Apply the same `s.mc.FindBestPrices(... "sell" ...)` swap at refresh.go:181.) Guard against a nil `s.mc` (return early in `refreshMarket` if nil) so callers that don't supply a collector don't panic.

In `pkg/agentstate/accessors.go:186-192`, change the two return types to `*market.MarketSnapshot` and `[]market.BestPrice`.

- [ ] **Step 2: Update the `agentstate.New` caller**

In `pkg/unified/server.go:84`, thread a collector. If the unified server has no collector yet, open one from config (`market.Open(market.Config{DBPath: cfg.MarketDBPath})`, default `data/market.db`) at server init and pass it:

```go
		return agentstate.New(state, kb, srv.marketCollector)
```

(Add a `marketCollector *market.Collector` field to the server struct, opened once at startup.)

- [ ] **Step 3: Migrate `RefreshMarketData`**

In `pkg/agent/market_refresh.go`, change `RefreshMarketData` to take `mc *market.Collector`, return `*market.MarketSnapshot`, and replace the two `kb.GetLatestMarketSnapshot(ctx, state.System.ID, stationID)` reads (lines 43, 60) with `mc.GetLatestSnapshot(ctx, stationID)`. Delete the dead `GetMarketAge`/`ShouldRefreshMarket` helpers (lines 78-99) in this file.

- [ ] **Step 4: Migrate `market_analysis.go`**

In `pkg/agent/market_analysis.go`: change `RefreshMarketAnalysis`/`CaptureMarketAnalysis` to take `mc *market.Collector`. Replace `kb.GetLatestMarketAnalysis(...)` with `mc.GetLatestAnalysis(ctx, stationID)`, build a `market.MarketAnalysis` (line ~161) and store via `mc.StoreAnalysis(ctx, analysis)`. Delete dead helpers `ShouldRefreshMarketAnalysis`/`GetMarketAnalysisAge` (lines 196-220).

- [ ] **Step 5: Migrate the auto-craftsman caller**

In `cmd/auto-craftsman/main.go`: open a `*market.Collector` from a new `--market-db-path` flag and pass it to the profit selector path. In `cmd/auto-craftsman/profit_selector.go:33`, change the call to `agent.RefreshMarketData(ctx, wsClient, mc, ...)` and the cached fallback to `mc.GetLatestSnapshot(ctx, state.CurrentPOI)`.

- [ ] **Step 6: Build, test, commit**

Run: `go build ./... && go test ./pkg/agentstate/... ./pkg/agent/... ./pkg/unified/... ./cmd/auto-craftsman/...`
Expected: PASS.

```bash
golangci-lint run ./pkg/agentstate/... ./pkg/agent/... ./pkg/unified/... ./cmd/auto-craftsman/...
git add -A
git commit -m "refactor(market): rewire snapshot/analysis/best-price readers to market.Collector"
```

---

### Task 6.5: Repoint cmd/tools/view-market at market.db (added during execution)

Discovered during execution: `cmd/tools/view-market/main.go` (830 lines, 5 subcommands: latest/history/items/prices/arbitrage) reads the knowledge `market_snapshots`/`market_listings` tables via raw SQL and uses `knowledge.MarketListing`. After Tasks 5–6 nothing writes those tables, and Task 7 drops them + the type. User chose (2026-06-21) to **repoint** it at `market.db` rather than retire it. Must land BEFORE Task 7 so the build stays green. Full brief: `.superpowers/sdd/task-6.5-brief.md`. Self-contained (only this file + a new smoke test).

---

### Task 7: Knowledge teardown — remove market surface + drop tables

**Files:**
- Modify: `pkg/knowledge/base.go` (interface + types), `pkg/knowledge/sqlite.go`, `pkg/knowledge/analytics.go`, `pkg/knowledge/memory.go`
- Modify: `pkg/knowledge/sqlite_migrations.go` (add version 46 drop migration)
- Modify: test mocks `pkg/galaxy/graph_test.go`, `pkg/knowledge/memory_catalog_test.go` (and any others the build flags)

**Interfaces:**
- Produces: `knowledge.Base` with **no** market methods; `market_snapshots`/`market_listings`/`market_analyses`/`price_trends` dropped from the knowledge DB.

- [ ] **Step 1: Write the failing migration test**

Add to `pkg/knowledge/` a test (e.g. `market_drop_test.go`):

```go
package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigration46DropsMarketTables(t *testing.T) {
	kb, err := NewSQLiteKB(Config{DBPath: filepath.Join(t.TempDir(), "k.db")})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })

	for _, tbl := range []string{"market_snapshots", "market_listings", "market_analyses", "price_trends"} {
		var name string
		err := kb.db.QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err == nil {
			t.Errorf("table %s still exists after migration 46", tbl)
		}
	}
	// Tables that must remain.
	for _, tbl := range []string{"base_market", "market_buy_orders", "market_sell_orders"} {
		var name string
		if err := kb.db.QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name); err != nil {
			t.Errorf("table %s should still exist: %v", tbl, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestMigration46 -v`
Expected: FAIL — market tables still present.

- [ ] **Step 3: Add migration version 46**

In `pkg/knowledge/sqlite_migrations.go`, append to the `migrations()` slice (after version 45):

```go
		{
			version: 46,
			name:    "drop_market_snapshot_tables",
			sql: `
				DROP TABLE IF EXISTS market_listings;
				DROP TABLE IF EXISTS market_snapshots;
				DROP TABLE IF EXISTS market_analyses;
				DROP TABLE IF EXISTS price_trends;
			`,
		},
```

- [ ] **Step 4: Run migration test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestMigration46 -v`
Expected: PASS.

- [ ] **Step 5: Remove methods + types from the interface and impls**

In `pkg/knowledge/base.go`: delete interface methods `StoreMarketSnapshot`, `GetMarketSnapshots`, `GetLatestMarketSnapshot`, `GetMarketItems`, `HasMarketSnapshotToday`, `StoreMarketAnalysis`, `GetLatestMarketAnalysis`, `GetMarketAnalysisHistory`, `AnalyzePriceTrends`, `FindBestPrices`; delete types `MarketSnapshot`, `MarketListing`, `MarketAnalysis`, `PriceTrend`, `BestPrice`.

Delete their implementations from `pkg/knowledge/sqlite.go`, `pkg/knowledge/analytics.go`, and `pkg/knowledge/memory.go` (and the `MemoryKB` in-memory market fields/structs they use). Keep all `base_market`/`bases`/demand-ledger code.

- [ ] **Step 6: Fix test mocks**

In `pkg/galaxy/graph_test.go`, remove the `mockKB` methods that implemented the deleted interface methods (e.g. `AnalyzePriceTrends`, `FindBestPrices`, market snapshot/analysis methods). Do the same in `pkg/knowledge/memory_catalog_test.go` and any other file the build flags.

- [ ] **Step 7: Build whole tree + full test**

Run: `go build ./... && go test ./...`
Expected: PASS. Fix any remaining references the compiler surfaces (the build is the gate — every removed symbol must be gone).

- [ ] **Step 8: Lint + commit**

```bash
golangci-lint run ./...
git add -A
git commit -m "refactor(knowledge): remove market surface + drop market tables (migration 46)"
```

---

### Task 8: End-to-end verification

**Files:**
- Test: `pkg/market/integration_test.go` (extend) or a new focused integration test.

- [ ] **Step 1: Add a round-trip integration test**

Extend `pkg/market/integration_test.go` with a test that writes a snapshot via `WriteSnapshot`, reads it back via `GetLatestSnapshot`, runs `FindBestPrices`, and asserts the cheapest-station result — proving the single-source read/write path end to end.

```go
func TestMarketRoundTrip(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "rt.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", StationName: "One", SystemID: "sys", SystemName: "S",
		CapturedAt: now,
		Orders:     []Order{{StationID: "stn1", ItemID: "iron", ItemName: "Iron", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	snap, err := c.GetLatestSnapshot(ctx, "stn1")
	if err != nil || snap == nil || len(snap.Orders) != 1 {
		t.Fatalf("GetLatestSnapshot = (%+v, %v)", snap, err)
	}
	best, err := c.FindBestPrices(ctx, "iron", "sell", 1)
	if err != nil || len(best) != 1 || best[0].StationID != "stn1" {
		t.Fatalf("FindBestPrices = (%+v, %v)", best, err)
	}
}
```

- [ ] **Step 2: Full build + test + lint**

Run:
```bash
go build ./...
go test ./...
golangci-lint run ./...
```
Expected: all PASS, no new lint findings.

- [ ] **Step 3: Manual smoke (optional, documented)**

Build play_as and confirm `update_market` writes to the configured DB:
```bash
go build -o bin/play_as ./cmd/tools/play_as
# run play_as against an agent, dock at a station, issue: update_market
# confirm data/market.db grows and `bin/market-stats` shows the capture
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "test(market): end-to-end round-trip verification"
```

---

## Self-Review

**Spec coverage:**
- Snapshots/listings move → Tasks 1, 5, 6, 7. ✓
- Analysis move → Tasks 3, 6, 7. ✓
- `FindBestPrices`/`BestPrice` move → Tasks 2, 6, 7. ✓
- `price_trends` retire → Task 7 (interface/impl delete + drop migration). ✓
- Dead helpers/methods delete → Tasks 6 (helpers in agent files) + 7 (interface). ✓
- `#4` path cleanup → Task 4. ✓
- `base_market`/demand ledger untouched → asserted by Task 7 Step 1 test. ✓
- Fresh cutover (no data migration) → Task 7 drop migration only. ✓
- `go test ./...` after interface change → Task 7 Step 7, Task 8. ✓

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to" — all steps carry concrete code or concrete edit instructions with the exact lines/symbols.

**Type consistency:** `MarketSnapshot`/`Order`/`BestPrice`/`MarketAnalysis` fields are used identically across tasks (e.g. `Side`, `PriceEach`, `PricePerUnit` mapping is consistent: `game.MarketListing.PricePerUnit` → `market.Order.PriceEach` in both converters). Method names match between producer (Tasks 1-3) and consumer (Tasks 5-6): `GetLatestSnapshot`, `HasSnapshotToday`, `FindBestPrices`, `StoreAnalysis`, `GetLatestAnalysis`, `WriteSnapshot`.

**Note on intermediate state:** Between Task 5 (writers flipped to `market.db`) and Task 6 (readers flipped), the rewired readers still read the knowledge DB and will see no fresh data — transient, within-plan only, resolved by Task 6. Each task still compiles and its tests pass.
