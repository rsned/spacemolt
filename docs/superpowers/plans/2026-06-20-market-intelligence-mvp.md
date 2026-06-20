# Market Intelligence System - MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a separate SQLite database system that collects hourly market snapshots from ~40 station agents, storing individual orders for deep analysis and supporting real-time arbitrage detection.

**Architecture:** 
- New `pkg/market/` package with normalized schema (items, stations, market_orders, OHLCV)
- Separate SQLite database at `/data/market.db` with write contention handling
- Integration with existing `play_as` scheduler for hourly collection
- 40 station agents run `update_market` action hourly

**Tech Stack:** Go 1.24, SQLite (modernc.org/sqlite), existing game client patterns

---

## Task 1: Create pkg/market package structure and types

**Files:**
- Create: `pkg/market/types.go`
- Create: `pkg/market/doc.go`

**Purpose:** Define the core types that mirror the schema design.

- [ ] **Step 1: Create package documentation**

Create `pkg/market/doc.go`:

```go
// Package market provides a separate SQLite database for collecting and analyzing
// game market data across all stations. Unlike the main knowledge base's
// market_demand_history (which stores aggregated hourly samples), this system
// preserves individual orders for deep analysis, cross-station arbitrage
// detection, and time-series pattern discovery.
//
// Database location: /home/robert/spacemolt/spacemolt/data/market.db
//
// The schema is normalized with dimension tables (items, stations) and fact
// tables (market_orders, market_ohlcv, arbitrage_opportunities).
package market
```

Run: `touch /home/robert/spacemolt/spacemolt/pkg/market/doc.go`

- [ ] **Step 2: Create types file with core data structures**

Create `pkg/market/types.go`:

```go
package market

import "time"

// Item represents a tradeable item in the market catalog.
type Item struct {
	ItemID         string    `json:"item_id"`
	ItemName       string    `json:"item_name"`
	Category       string    `json:"category"`
	FirstSeenUTC   string    `json:"first_seen_utc"`
	LastUpdatedUTC string    `json:"last_updated_utc"`
}

// Station represents a station/POI with a market.
type Station struct {
	StationID      string    `json:"station_id"`
	StationName    string    `json:"station_name"`
	SystemID       string    `json:"system_id"`
	SystemName     string    `json:"system_name"`
	FirstSeenUTC   string    `json:"first_seen_utc"`
	LastUpdatedUTC string    `json:"last_updated_utc"`
}

// Order represents a single buy or sell order from the market.
type Order struct {
	StationID  string    `json:"station_id"`
	ItemID     string    `json:"item_id"`
	Side       string    `json:"side"`        // "buy" or "sell"
	PriceEach  float64   `json:"price_each"`
	Quantity   float64   `json:"quantity"`
	MyQuantity float64   `json:"my_quantity"`
	Source     string    `json:"source"`
	CapturedAt time.Time `json:"captured_at"`
	BucketUTC  string    `json:"bucket_utc"` // Truncated to hour
}

// OHLCV represents Open, High, Low, Close, Volume for a time bucket.
type OHLCV struct {
	StationID  string    `json:"station_id"`
	ItemID     string    `json:"item_id"`
	Side       string    `json:"side"`
	BucketUTC  string    `json:"bucket_utc"`
	OpenPrice  float64   `json:"open_price"`
	HighPrice  float64   `json:"high_price"`
	LowPrice   float64   `json:"low_price"`
	ClosePrice float64   `json:"close_price"`
	Volume     float64   `json:"volume"`
	TradeCount int       `json:"trade_count"`
	VWAP       float64   `json:"vwap"` // Volume-weighted average price
}

// ArbitrageOpportunity represents a profitable trading opportunity.
type ArbitrageOpportunity struct {
	ID             int       `json:"id"`
	FromStationID  string    `json:"from_station_id"`
	ToStationID    string    `json:"to_station_id"`
	ItemID         string    `json:"item_id"`
	ActionType     string    `json:"action_type"`     // "buy_then_sell" or "sell_then_buy"
	BuyPrice       float64   `json:"buy_price"`
	SellPrice      float64   `json:"sell_price"`
	Quantity       float64   `json:"quantity"`
	GrossProfit    float64   `json:"gross_profit"`
	FuelCost       float64   `json:"fuel_cost"`
	TravelTicks    int       `json:"travel_ticks"`
	CargoRequired  float64   `json:"cargo_required"`
	RiskScore      float64   `json:"risk_score"`
	ClaimedBy      string    `json:"claimed_by"`
	ClaimedAt      string    `json:"claimed_at"`
	Status         string    `json:"status"`        // "available", "claimed", "completed", "expired"
	ExpiresAt      string    `json:"expires_at"`
	DiscoveredAt   string    `json:"discovered_at"`
	DiscoveredBy   string    `json:"discovered_by"`
	Notes          string    `json:"notes"`
}

// MarketSnapshot represents a complete market state at one station.
type MarketSnapshot struct {
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	Orders      []Order   `json:"orders"`
	CapturedAt  time.Time `json:"captured_at"`
}
```

Run: `go build ./pkg/market/...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add pkg/market/
git commit -m "feat(market): add package structure and core types

- Add pkg/market package with normalized types
- Define Item, Station, Order, OHLCV, ArbitrageOpportunity
- MarketSnapshot for station-level captures
"
```

---

## Task 2: Create database schema and migrations

**Files:**
- Create: `pkg/market/schema.sql`
- Create: `pkg/market/migrations.go`

- [ ] **Step 1: Create schema SQL file**

Create `pkg/market/schema.sql`:

```sql
-- Item catalog
CREATE TABLE IF NOT EXISTS items (
    item_id         TEXT PRIMARY KEY,
    item_name       TEXT NOT NULL,
    category        TEXT,
    first_seen_utc  TEXT NOT NULL,
    last_updated_utc TEXT NOT NULL
);

-- Stations (points of interest with markets)
CREATE TABLE IF NOT EXISTS stations (
    station_id      TEXT PRIMARY KEY,
    station_name    TEXT NOT NULL,
    system_id       TEXT NOT NULL,
    system_name     TEXT NOT NULL,
    first_seen_utc  TEXT NOT NULL,
    last_updated_utc TEXT NOT NULL
);

-- Individual orders (main fact table)
CREATE TABLE IF NOT EXISTS market_orders (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    station_id      TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    side            TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    price_each      REAL NOT NULL,
    quantity        REAL NOT NULL,
    my_quantity     REAL DEFAULT 0,
    source          TEXT,
    captured_at     TEXT NOT NULL,
    bucket_utc      TEXT NOT NULL,
    FOREIGN KEY (station_id) REFERENCES stations(station_id),
    FOREIGN KEY (item_id) REFERENCES items(item_id)
);

CREATE INDEX IF NOT EXISTS idx_orders_station_item ON market_orders(station_id, item_id, bucket_utc);
CREATE INDEX IF NOT EXISTS idx_orders_item_time ON market_orders(item_id, captured_at);
CREATE INDEX IF NOT EXISTS idx_orders_bucket ON market_orders(bucket_utc);

-- Hourly OHLCV aggregates
CREATE TABLE IF NOT EXISTS market_ohlcv (
    station_id      TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    side            TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    bucket_utc      TEXT NOT NULL,
    open_price      REAL NOT NULL,
    high_price      REAL NOT NULL,
    low_price       REAL NOT NULL,
    close_price     REAL NOT NULL,
    volume          REAL NOT NULL,
    trade_count     INTEGER NOT NULL,
    vwap            REAL NOT NULL,
    FOREIGN KEY (station_id) REFERENCES stations(station_id),
    FOREIGN KEY (item_id) REFERENCES items(item_id),
    PRIMARY KEY (station_id, item_id, side, bucket_utc)
);

-- Arbitrage opportunities
CREATE TABLE IF NOT EXISTS arbitrage_opportunities (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    from_station_id     TEXT NOT NULL,
    to_station_id       TEXT NOT NULL,
    item_id             TEXT NOT NULL,
    action_type         TEXT NOT NULL CHECK (action_type IN ('buy_then_sell', 'sell_then_buy')),
    buy_price           REAL NOT NULL,
    sell_price          REAL NOT NULL,
    quantity            REAL NOT NULL,
    gross_profit        REAL NOT NULL,
    fuel_cost           REAL NOT NULL,
    travel_ticks        INTEGER NOT NULL,
    cargo_required      REAL NOT NULL,
    risk_score          REAL DEFAULT 0,
    claimed_by          TEXT,
    claimed_at          TEXT,
    status              TEXT DEFAULT 'available' CHECK (status IN ('available', 'claimed', 'completed', 'expired')),
    expires_at          TEXT NOT NULL,
    discovered_at       TEXT NOT NULL,
    discovered_by       TEXT NOT NULL,
    notes               TEXT,
    FOREIGN KEY (from_station_id) REFERENCES stations(station_id),
    FOREIGN KEY (to_station_id) REFERENCES stations(station_id),
    FOREIGN KEY (item_id) REFERENCES items(item_id)
);

CREATE INDEX IF NOT EXISTS idx_arbitrage_status ON arbitrage_opportunities(status, expires_at);
CREATE INDEX IF NOT EXISTS idx_arbitrage_item ON arbitrage_opportunities(item_id, status);
```

- [ ] **Step 2: Create migrations Go file**

Create `pkg/market/migrations.go`:

```go
package market

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var schemaSQL string

// runMigrations creates all tables and indexes. Idempotent.
func runMigrations(db *sql.DB) error {
	_, err := db.Exec(schemaSQL)
	if err != nil {
		return fmt.Errorf("failed to run schema: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Commit**

```bash
git add pkg/market/schema.sql pkg/market/migrations.go
git commit -m "feat(market): add database schema and migrations

- Create normalized schema: items, stations, market_orders, OHLCV
- Add arbitrage_opportunities table
- Include proper indexes for common query patterns
"
```

---

## Task 3: Create Collector with database connection handling

**Files:**
- Create: `pkg/market/collector.go`
- Create: `pkg/market/collector_test.go`

- [ ] **Step 1: Create Collector struct and Config**

Create `pkg/market/collector.go`:

```go
package market

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Config holds configuration for the market database.
type Config struct {
	DBPath       string
	WAL          bool
	MaxOpenConns int
	MaxIdleConns int
	BusyTimeout  time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		DBPath:       filepath.Join(os.Getenv("HOME"), "spacemolt", "spacemolt", "data", "market.db"),
		WAL:          true,
		MaxOpenConns: 25,
		MaxIdleConns: 5,
		BusyTimeout:  5 * time.Second,
	}
}

// Collector handles market data collection.
type Collector struct {
	db *sql.DB
}

// Open creates a new collector with the market database.
func Open(cfg Config) (*Collector, error) {
	if cfg.DBPath == "" {
		cfg = DefaultConfig()
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = DefaultConfig().MaxOpenConns
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = DefaultConfig().MaxIdleConns
	}
	if cfg.BusyTimeout == 0 {
		cfg.BusyTimeout = DefaultConfig().BusyTimeout
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}

	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", int(cfg.BusyTimeout.Milliseconds()))); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	if cfg.WAL {
		if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("enable WAL: %w", err)
		}
	}

	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &Collector{db: db}, nil
}

// Close closes the database connection.
func (c *Collector) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}
```

- [ ] **Step 2: Add writeSnapshot with retry logic**

Add to `pkg/market/collector.go`:

```go
// maxRetryAttempts is the number of retries for SQLITE_BUSY.
const maxRetryAttempts = 5

// baseRetryDelay is the initial retry delay.
const baseRetryDelay = 50 * time.Millisecond

// writeRetry executes a write operation with exponential backoff retry.
func (c *Collector) writeRetry(ctx context.Context, fn func(tx *sql.Tx) error) error {
	var lastErr error
	for attempt := 0; attempt < maxRetryAttempts; attempt++ {
		if attempt > 0 {
			delay := baseRetryDelay * time.Duration(1<<(attempt-1))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		tx, err := c.db.Begin()
		if err != nil {
			lastErr = err
			continue
		}

		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			if isBusyError(err) {
				lastErr = err
				continue
			}
			return err
		}

		if err := tx.Commit(); err != nil {
			if isBusyError(err) {
				lastErr = err
				continue
			}
			return err
		}

		return nil
	}
	return fmt.Errorf("failed after %d attempts: %w", maxRetryAttempts, lastErr)
}

// isBusyError checks if err is SQLITE_BUSY.
func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	// Check for "database is locked" or "database is busy"
	errStr := err.Error()
	return len(errStr) >= 14 && // "database is busy" length
		(len(errStr) >= 15 && errStr[len(errStr)-15:] == "database is busy") ||
		(len(errStr) >= 17 && errStr[len(errStr)-17:] == "database is locked")
}
```

- [ ] **Step 3: Create basic test**

Create `pkg/market/collector_test.go`:

```go
package market

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpen(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		DBPath:       dbPath,
		WAL:          true,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
		BusyTimeout:  time.Second,
	}

	collector, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer collector.Close()

	if collector.db == nil {
		t.Fatal("db is nil")
	}

	// Verify tables exist
	var tableName string
	err = collector.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='items'").Scan(&tableName)
	if err != nil {
		t.Fatalf("items table not found: %v", err)
	}
}
```

- [ ] **Step 4: Run test**

Run: `go test -v ./pkg/market/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/market/collector.go pkg/market/collector_test.go
git commit -m "feat(market): add Collector with connection handling

- Add Config and Collector for market DB
- Implement WAL mode and connection pooling
- Add write retry with exponential backoff for SQLITE_BUSY
- Add basic test for database initialization
"
```

---

## Task 4: Implement order persistence

**Files:**
- Modify: `pkg/market/collector.go`
- Modify: `pkg/market/collector_test.go`

- [ ] **Step 1: Add upsertStation method**

Add to `pkg/market/collector.go`:

```go
// upsertStation adds or updates a station record.
func (c *Collector) upsertStation(tx *sql.Tx, s Station) error {
	_, err := tx.Exec(`
		INSERT INTO stations (station_id, station_name, system_id, system_name, first_seen_utc, last_updated_utc)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(station_id) DO UPDATE SET
			station_name = excluded.station_name,
			system_id = excluded.system_id,
			system_name = excluded.system_name,
			last_updated_utc = excluded.last_updated_utc
	`, s.StationID, s.StationName, s.SystemID, s.SystemName, s.FirstSeenUTC, s.LastUpdatedUTC)
	return err
}

// upsertItem adds or updates an item record.
func (c *Collector) upsertItem(tx *sql.Tx, item Item) error {
	_, err := tx.Exec(`
		INSERT INTO items (item_id, item_name, category, first_seen_utc, last_updated_utc)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			item_name = excluded.item_name,
			category = excluded.category,
			last_updated_utc = excluded.last_updated_utc
	`, item.ItemID, item.ItemName, item.Category, item.FirstSeenUTC, item.LastUpdatedUTC)
	return err
}
```

- [ ] **Step 2: Add insertOrders method**

Add to `pkg/market/collector.go`:

```go
// insertOrders adds order rows within a transaction.
func (c *Collector) insertOrders(tx *sql.Tx, orders []Order) error {
	if len(orders) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`
		INSERT INTO market_orders (station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at, bucket_utc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, o := range orders {
		if _, err := stmt.Exec(o.StationID, o.ItemID, o.Side, o.PriceEach, o.Quantity, o.MyQuantity, o.Source, o.CapturedAt.UTC().Format(time.RFC3339), o.BucketUTC); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 3: Add writeSnapshot method**

Add to `pkg/market/collector.go`:

```go
// WriteSnapshot persists a market snapshot atomically.
func (c *Collector) WriteSnapshot(ctx context.Context, snapshot MarketSnapshot) error {
	now := time.Now().UTC().Format(time.RFC3339)
	bucketUTC := snapshot.CapturedAt.UTC().Truncate(time.Hour).Format(time.RFC3339)

	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		// Upsert station
		if err := c.upsertStation(tx, Station{
			StationID:      snapshot.StationID,
			StationName:    snapshot.StationName,
			SystemID:       snapshot.SystemID,
			SystemName:     snapshot.SystemName,
			FirstSeenUTC:   now,
			LastUpdatedUTC: now,
		}); err != nil {
			return fmt.Errorf("upsert station: %w", err)
		}

		// Group orders by item for upsert
		itemMap := make(map[string]Item)
		for _, o := range snapshot.Orders {
			if _, ok := itemMap[o.ItemID]; !ok {
				itemMap[o.ItemID] = Item{
					ItemID:         o.ItemID,
					ItemName:       "", // Will be set below
					Category:       "",
					FirstSeenUTC:   now,
					LastUpdatedUTC: now,
				}
			}
			// Use the first non-empty ItemName we find
			if o.ItemName != "" && itemMap[o.ItemID].ItemName == "" {
				itemMap[o.ItemID].ItemName = o.ItemName
			}
		}

		// Upsert items
		for _, item := range itemMap {
			if err := c.upsertItem(tx, item); err != nil {
				return fmt.Errorf("upsert item %s: %w", item.ItemID, err)
			}
		}

		// Set bucket UTC on all orders
		ordersWithBucket := make([]Order, len(snapshot.Orders))
		for i, o := range snapshot.Orders {
			ordersWithBucket[i] = o
			ordersWithBucket[i].BucketUTC = bucketUTC
		}

		// Insert orders
		if err := c.insertOrders(tx, ordersWithBucket); err != nil {
			return fmt.Errorf("insert orders: %w", err)
		}

		return nil
	})
}
```

- [ ] **Step 4: Add tests for order persistence**

Add to `pkg/market/collector_test.go`:

```go
func TestWriteSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	collector, err := Open(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer collector.Close()

	ctx := context.Background()
	snapshot := MarketSnapshot{
		StationID:   "station_test",
		StationName: "Test Station",
		SystemID:    "system_test",
		SystemName:  "Test System",
		CapturedAt:  time.Now().UTC(),
		Orders: []Order{
			{
				StationID:  "station_test",
				ItemID:     "iron",
				ItemName:   "Iron Ore",
				Side:       "buy",
				PriceEach:  100.0,
				Quantity:   500.0,
				MyQuantity: 0,
				Source:     "player",
				CapturedAt: time.Now().UTC(),
			},
			{
				StationID:  "station_test",
				ItemID:     "copper",
				ItemName:   "Copper Ore",
				Side:       "sell",
				PriceEach:  200.0,
				Quantity:   300.0,
				MyQuantity: 0,
				Source:     "station",
				CapturedAt: time.Now().UTC(),
			},
		},
	}

	if err := collector.WriteSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("WriteSnapshot failed: %v", err)
	}

	// Verify station was written
	var stationName string
	err = collector.db.QueryRow("SELECT station_name FROM stations WHERE station_id = ?", "station_test").Scan(&stationName)
	if err != nil {
		t.Fatalf("Query station failed: %v", err)
	}
	if stationName != "Test Station" {
		t.Errorf("station_name = %q, want %q", stationName, "Test Station")
	}

	// Verify orders were written
	var orderCount int
	err = collector.db.QueryRow("SELECT COUNT(*) FROM market_orders WHERE station_id = ?", "station_test").Scan(&orderCount)
	if err != nil {
		t.Fatalf("Count orders failed: %v", err)
	}
	if orderCount != 2 {
		t.Errorf("order_count = %d, want 2", orderCount)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test -v ./pkg/market/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/market/
git commit -m "feat(market): add order persistence

- Add upsertStation, upsertItem, insertOrders methods
- Implement WriteSnapshot with atomic transaction
- Add tests for snapshot persistence
- Handle item/station metadata updates
"
```

---

## Task 5: Implement OHLCV aggregation

**Files:**
- Modify: `pkg/market/collector.go`
- Modify: `pkg/market/collector_test.go`

- [ ] **Step 1: Add OHLCV computation helper**

Add to `pkg/market/collector.go`:

```go
// computeOHLCV calculates OHLCV from orders grouped by (station, item, side).
type ohlcvAccumulator struct {
	stationID, itemID, side        string
	open, high, low, close        float64
	volume                        float64
	sumPriceTimesQty              float64 // For VWAP calculation
	tradeCount                    int
	firstPriceSet                 bool
}

func computeOHLCV(orders []Order, bucketUTC string) []OHLCV {
	// Group by (station_id, item_id, side)
	key := func(stationID, itemID, side string) string {
		return stationID + "\x00" + itemID + "\x00" + side
	}

	accs := make(map[string]*ohlcvAccumulator)
	order := []string{} // preserve order for deterministic output

	for _, o := range orders {
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		k := key(o.StationID, o.ItemID, o.Side)
		acc, ok := accs[k]
		if !ok {
			acc = &ohlcvAccumulator{
				stationID:     o.StationID,
				itemID:        o.ItemID,
				side:          o.Side,
				firstPriceSet: false,
			}
			accs[k] = acc
			order = append(order, k)
		}

		acc.volume += o.Quantity
		acc.sumPriceTimesQty += o.PriceEach * o.Quantity
		acc.tradeCount++

		if !acc.firstPriceSet {
			acc.open = o.PriceEach
			acc.high = o.PriceEach
			acc.low = o.PriceEach
			acc.close = o.PriceEach
			acc.firstPriceSet = true
		} else {
			acc.close = o.PriceEach
			if o.PriceEach > acc.high {
				acc.high = o.PriceEach
			}
			if o.PriceEach < acc.low {
				acc.low = o.PriceEach
			}
		}
	}

	// Convert to OHLCV with VWAP
	result := make([]OHLCV, 0, len(order))
	for _, k := range order {
		acc := accs[k]
		vwap := 0.0
		if acc.volume > 0 {
			// VWAP = sum(price * quantity) / sum(quantity)
			vwap = acc.sumPriceTimesQty / acc.volume
		}
		result = append(result, OHLCV{
			StationID:  acc.stationID,
			ItemID:     acc.itemID,
			Side:       acc.side,
			BucketUTC:  bucketUTC,
			OpenPrice:  acc.open,
			HighPrice:  acc.high,
			LowPrice:   acc.low,
			ClosePrice: acc.close,
			Volume:     acc.volume,
			TradeCount: acc.tradeCount,
			VWAP:       vwap,
		})
	}

	return result
}
```

- [ ] **Step 2: Add upsertOHLCV method**

Add to `pkg/market/collector.go`:

```go
// upsertOHLCV inserts or updates OHLCV records.
func (c *Collector) upsertOHLCV(tx *sql.Tx, ohlcv OHLCV) error {
	_, err := tx.Exec(`
		INSERT INTO market_ohlcv (station_id, item_id, side, bucket_utc, open_price, high_price, low_price, close_price, volume, trade_count, vwap)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(station_id, item_id, side, bucket_utc) DO UPDATE SET
			open_price = excluded.open_price,
			high_price = excluded.high_price,
			low_price = excluded.low_price,
			close_price = excluded.close_price,
			volume = excluded.volume,
			trade_count = excluded.trade_count,
			vwap = excluded.vwap
	`, ohlcv.StationID, ohlcv.ItemID, ohlcv.Side, ohlcv.BucketUTC,
		ohlcv.OpenPrice, ohlcv.HighPrice, ohlcv.LowPrice, ohlcv.ClosePrice,
		ohlcv.Volume, ohlcv.TradeCount, ohlcv.VWAP)
	return err
}
```

- [ ] **Step 3: Update WriteSnapshot to include OHLCV**

Modify the WriteSnapshot method in `pkg/market/collector.go`, add before the return statement in writeRetry:

```go
		// Compute and upsert OHLCV
		ohlcvList := computeOHLCV(ordersWithBucket, bucketUTC)
		for _, ohlcv := range ohlcvList {
			if err := c.upsertOHLCV(tx, ohlcv); err != nil {
				return fmt.Errorf("upsert OHLCV: %w", err)
			}
		}
```

- [ ] **Step 4: Add OHLCV test**

Add to `pkg/market/collector_test.go`:

```go
func TestOHLCV(t *testing.T) {
	orders := []Order{
		{StationID: "stn1", ItemID: "iron", Side: "buy", PriceEach: 100, Quantity: 10},
		{StationID: "stn1", ItemID: "iron", Side: "buy", PriceEach: 110, Quantity: 5},
		{StationID: "stn1", ItemID: "iron", Side: "buy", PriceEach: 90, Quantity: 8},
	}

	ohlcv := computeOHLCV(orders, "2026-06-20T12:00:00Z")

	if len(ohlcv) != 1 {
		t.Fatalf("len(ohlcv) = %d, want 1", len(ohlcv))
	}

	o := ohlcv[0]
	if o.OpenPrice != 100 {
		t.Errorf("open = %f, want 100", o.OpenPrice)
	}
	if o.HighPrice != 110 {
		t.Errorf("high = %f, want 110", o.HighPrice)
	}
	if o.LowPrice != 90 {
		t.Errorf("low = %f, want 90", o.LowPrice)
	}
	if o.ClosePrice != 90 {
		t.Errorf("close = %f, want 90", o.ClosePrice)
	}
	if o.Volume != 23 {
		t.Errorf("volume = %f, want 23", o.Volume)
	}
	// VWAP = (100*10 + 110*5 + 90*8) / 23 = (1000 + 550 + 720) / 23 = 2270/23 ≈ 98.7
	expectedVWAP := (100*10 + 110*5 + 90*8) / 23
	if o.VWAP != expectedVWAP {
		t.Errorf("vwap = %f, want %f", o.VWAP, expectedVWAP)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test -v ./pkg/market/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/market/
git commit -m "feat(market): add OHLCV aggregation

- Implement computeOHLCV for time-series aggregates
- Add upsertOHLCV for persistence
- Update WriteSnapshot to compute and store OHLCV
- Add tests for OHLCV computation
"
```

---

## Task 6: Create market capture integration for game client

**Files:**
- Create: `pkg/market/capture.go`
- Create: `pkg/market/capture_test.go`

- [ ] **Step 1: Create capture integration**

Create `pkg/market/capture.go`:

```go
package market

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// parseViewMarket parses a raw view_market JSON response into Orders.
func parseViewMarket(raw []byte, stationID, systemID string, capturedAt time.Time) ([]Order, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var resp serverapi.ViewMarketResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}

	var orders []Order
	now := capturedAt.UTC()

	for _, item := range resp.Items {
		// Buy orders
		for _, o := range item.BuyOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			orders = append(orders, Order{
				StationID:  stationID,
				ItemID:     item.ItemID,
				ItemName:   item.ItemName,
				Side:       "buy",
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}

		// Sell orders
		for _, o := range item.SellOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			orders = append(orders, Order{
				StationID:  stationID,
				ItemID:     item.ItemID,
				ItemName:   item.ItemName,
				Side:       "sell",
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}
	}

	return orders, nil
}

// CaptureFromClient captures market data from a game client's last view_market response.
func CaptureFromClient(ctx context.Context, client game.GameClient, collector *Collector) error {
	state := client.GetState()
	if state == nil {
		return nil
	}

	// Must be at a station
	if state.CurrentPOI == "" {
		return nil
	}

	raw := client.GetRawJSON("market")
	if len(raw) == 0 {
		return nil
	}

	orders, err := parseViewMarket(raw, state.CurrentPOI, state.CurrentSystem, time.Now())
	if err != nil {
		return err
	}

	snapshot := MarketSnapshot{
		StationID:   state.CurrentPOI,
		StationName: state.CurrentPOI, // Could be enhanced with base name
		SystemID:    state.CurrentSystem,
		SystemName:  state.CurrentSystem,
		Orders:      orders,
		CapturedAt:  time.Now(),
	}

	return collector.WriteSnapshot(ctx, snapshot)
}
```

- [ ] **Step 2: Add capture test**

Create `pkg/market/capture_test.go`:

```go
package market

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseViewMarket(t *testing.T) {
	raw := []byte(`{
		"action": "view_market",
		"base_id": "station_test",
		"items": [
			{
				"item_id": "iron",
				"item_name": "Iron Ore",
				"best_buy": 100,
				"best_sell": 110,
				"buy_orders": [
					{"price_each": 100, "quantity": 500, "my_quantity": 0, "source": "player"}
				],
				"sell_orders": [
					{"price_each": 110, "quantity": 300, "my_quantity": 0, "source": "station"}
				]
			}
		]
	}`)

	orders, err := parseViewMarket(raw, "station_test", "system_test", time.Now().UTC())
	if err != nil {
		t.Fatalf("parseViewMarket failed: %v", err)
	}

	if len(orders) != 2 {
		t.Fatalf("len(orders) = %d, want 2", len(orders))
	}

	// Check buy order
	buy := orders[0]
	if buy.Side != "buy" {
		t.Errorf("first order side = %s, want buy", buy.Side)
	}
	if buy.ItemID != "iron" {
		t.Errorf("first order item_id = %s, want iron", buy.ItemID)
	}

	// Check sell order
	sell := orders[1]
	if sell.Side != "sell" {
		t.Errorf("second order side = %s, want sell", sell.Side)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test -v ./pkg/market/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/market/capture.go pkg/market/capture_test.go
git commit -m "feat(market): add view_market capture integration

- Add parseViewMarket to extract orders from JSON
- Implement CaptureFromClient for game client integration
- Add tests for parsing logic
"
```

---

## Task 7: Add update_market command to play_as

**Files:**
- Modify: `cmd/tools/play_as/main.go`
- Modify: `cmd/tools/play_as/completer.go`

- [ ] **Step 1: Find the command registration pattern**

Search for existing command registrations:

Run: `grep -n "handle.*Command\|\"view_market\"" /home/robert/spacemolt/spacemolt/cmd/tools/play_as/main.go | head -20`

Expected output: Show how commands are registered

- [ ] **Step 2: Add global collector reference**

At the top of `cmd/tools/play_as/main.go`, add after other imports:

```go
// Market collector for the update_market command
var globalMarketCollector *market.Collector
```

Add import:
```go
	"github.com/rsned/spacemolt/pkg/market"
```

- [ ] **Step 3: Add update_market command handler**

Add to `cmd/tools/play_as/main.go`:

```go
// handleUpdateMarket captures the current station's market data to the market DB.
func handleUpdateMarket(client game.GameClient) {
	if globalMarketCollector == nil {
		cfg := market.DefaultConfig()
		c, err := market.Open(cfg)
		if err != nil {
			fmt.Printf("error opening market DB: %v\n", err)
			return
		}
		globalMarketCollector = c
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.ViewMarket(ctx); err != nil {
		fmt.Printf("error viewing market: %v\n", err)
		return
	}

	if err := market.CaptureFromClient(ctx, client, globalMarketCollector); err != nil {
		fmt.Printf("error capturing market: %v\n", err)
		return
	}

	fmt.Printf("✓ Captured market data for %s\n", client.GetState().CurrentPOI)
}
```

- [ ] **Step 4: Register the command**

Find the command registration section (search for `case "view_market":`) and add:

```go
	case "update_market":
		handleUpdateMarket(client)
```

- [ ] **Step 5: Add to command completer**

Modify `cmd/tools/play_as/completer.go`, find the commands list and add `"update_market"`.

- [ ] **Step 6: Test the command**

Run: `go build ./cmd/tools/play_as && ./play_as --help | grep update_market`
Expected: Command should be available

- [ ] **Step 7: Commit**

```bash
git add cmd/tools/play_as/
git commit -m "feat(play_as): add update_market command

- Add globalMarketCollector for market DB connection
- Implement handleUpdateMarket for capturing market data
- Register update_market command
- Add to command completer
"
```

---

## Task 8: Add scheduler support for update_market

**Files:**
- Modify: `cmd/tools/play_as/main.go` (if needed for scheduler integration)

- [ ] **Step 1: Verify scheduler works with update_market**

The existing scheduler should already work with any command. Test it:

The scheduler in `schedule.go` uses `executeLogicalCommand` which should automatically pick up the new `update_market` command since we registered it in Task 7.

- [ ] **Step 2: Add documentation comment**

Add to the schedule help text if needed:

Run: `grep -n "schedule_add usage" /home/robert/spacemolt/spacemolt/cmd/tools/play_as/schedule.go`

The existing implementation should already support `update_market` via the generic command runner.

- [ ] **Step 3: No commit needed**

This is verification only. The existing scheduler infrastructure already supports the new command.

---

## Task 9: Add query helpers for verification

**Files:**
- Create: `pkg/market/query.go`
- Create: `pkg/market/query_test.go`

- [ ] **Step 1: Create query helpers**

Create `pkg/market/query.go`:

```go
package market

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Stats represents database statistics.
type Stats struct {
	StationCount int     `json:"station_count"`
	ItemCount    int     `json:"item_count"`
	OrderCount   int64   `json:"order_count"`
	OHLCVCount   int64   `json:"ohlcv_count"`
	LatestCapture string `json:"latest_capture"`
}

// GetStats returns database statistics.
func (c *Collector) GetStats(ctx context.Context) (*Stats, error) {
	var stats Stats

	// Count stations
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM stations").Scan(&stats.StationCount)
	if err != nil {
		return nil, fmt.Errorf("count stations: %w", err)
	}

	// Count items
	err = c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&stats.ItemCount)
	if err != nil {
		return nil, fmt.Errorf("count items: %w", err)
	}

	// Count orders
	err = c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM market_orders").Scan(&stats.OrderCount)
	if err != nil {
		return nil, fmt.Errorf("count orders: %w", err)
	}

	// Count OHLCV
	err = c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM market_ohlcv").Scan(&stats.OHLCVCount)
	if err != nil {
		return nil, fmt.Errorf("count ohlcv: %w", err)
	}

	// Latest capture
	err = c.db.QueryRowContext(ctx, "SELECT MAX(captured_at) FROM market_orders").Scan(&stats.LatestCapture)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("latest capture: %w", err)
	}

	return &stats, nil
}

// GetLatestOrders returns recent orders for a station.
func (c *Collector) GetLatestOrders(ctx context.Context, stationID string, limit int) ([]Order, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at
		FROM market_orders
		WHERE station_id = ?
		ORDER BY captured_at DESC
		LIMIT ?
	`, stationID, limit)
	if err != nil {
		return nil, fmt.Errorf("query orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var o Order
		var capturedAtStr string
		err := rows.Scan(&o.StationID, &o.ItemID, &o.Side, &o.PriceEach, &o.Quantity, &o.MyQuantity, &o.Source, &capturedAtStr)
		if err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		o.CapturedAt, _ = time.Parse(time.RFC3339, capturedAtStr)
		orders = append(orders, o)
	}

	return orders, nil
}
```

- [ ] **Step 2: Add test**

Create `pkg/market/query_test.go`:

```go
package market

import (
	"context"
	"testing"
)

func TestGetStats(t *testing.T) {
	tmpDir := t.TempDir()
	collector, err := Open(Config{DBPath: tmpDir + "/test.db"})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer collector.Close()

	ctx := context.Background()
	stats, err := collector.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.StationCount != 0 {
		t.Errorf("station_count = %d, want 0", stats.StationCount)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test -v ./pkg/market/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/market/query.go pkg/market/query_test.go
git commit -m "feat(market): add query helpers for verification

- Add GetStats for database statistics
- Add GetLatestOrders for retrieving recent orders
- Add tests for query helpers
"
```

---

## Task 10: Create CLI tool for manual verification

**Files:**
- Create: `cmd/tools/market-stats/main.go`

- [ ] **Step 1: Create market-stats tool**

Create `cmd/tools/market-stats/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/rsned/spacemolt/pkg/market"
)

func main() {
	cfg := market.DefaultConfig()
	c, err := market.Open(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening market DB: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	ctx := context.Background()
	stats, err := c.GetStats(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting stats: %v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Market Database Statistics")
	fmt.Fprintln(w, "========================")
	fmt.Fprintf(w, "Stations:\t%d\n", stats.StationCount)
	fmt.Fprintf(w, "Items:\t%d\n", stats.ItemCount)
	fmt.Fprintf(w, "Orders:\t%d\n", stats.OrderCount)
	fmt.Fprintf(w, "OHLCV records:\t%d\n", stats.OHLCVCount)
	fmt.Fprintf(w, "Latest capture:\t%s\n", stats.LatestCapture)
	w.Flush()
}
```

- [ ] **Step 2: Build and test**

Run: `go build ./cmd/tools/market-stats && ./market-stats`
Expected: Shows database statistics

- [ ] **Step 3: Commit**

```bash
git add cmd/tools/market-stats/
git commit -m "feat(market): add market-stats CLI tool

- Create simple tool to display database statistics
- Useful for verifying data collection
"
```

---

## Task 11: Integration test with real game connection

**Files:**
- Create: `pkg/market/integration_test.go`

- [ ] **Step 1: Create integration test documentation**

Create `pkg/market/integration_test.go`:

```go
// +build integration

package market

import (
	"context"
	"testing"
	"time"
)

// TestCaptureIntegration requires a real game connection.
// Run with: go test -tags=integration -v ./pkg/market/...
func TestCaptureIntegration(t *testing.T) {
	t.Skip("requires real game connection - manual verification only")

	// Manual test procedure:
	// 1. Start play_as with your agent
	// 2. Run: update_market
	// 3. Run: market-stats
	// 4. Verify station count and order count increase
	// 5. Query database: sqlite3 data/market.db "SELECT COUNT(*) FROM market_orders"
}
```

- [ ] **Step 2: Add verification instructions to README**

Create or update `README.md` in `pkg/market/`:

```markdown
# Market Intelligence Package

## Verification

After deploying to agents, verify data collection:

1. Check database stats:
   ```bash
   go run ./cmd/tools/market-stats
   ```

2. Query directly:
   ```bash
   sqlite3 data/market.db "SELECT COUNT(*) FROM market_orders"
   sqlite3 data/market.db "SELECT station_id, COUNT(*) FROM market_orders GROUP BY station_id"
   ```

3. Check hourly captures:
   ```bash
   sqlite3 data/market.db "SELECT bucket_utc, COUNT(*) FROM market_orders GROUP BY bucket_utc ORDER BY bucket_utc DESC LIMIT 10"
   ```
```

- [ ] **Step 3: Commit**

```bash
git add pkg/market/
git commit -m "docs(market): add integration test notes and verification docs

- Add integration test placeholder
- Document verification procedures
"
```

---

## Final Steps

- [ ] **Step 1: Build all packages**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 2: Run all tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 3: Run golangci-lint**

Run: `golangci-lint run ./...`
Expected: No new findings

- [ ] **Step 4: Create data directory if needed**

Run: `mkdir -p /home/robert/spacemolt/spacemolt/data`

- [ ] **Step 5: Verify deployment readiness**

The system is ready for deployment to the 40 station agents:
- Agents can run `schedule_add hourly update_market` to begin hourly collection
- Use `market-stats` tool to verify data accumulation
- Database will be created at `/data/market.db`

---

## Summary

This plan implements **Phase 1 (MVP - Data Collection)** of the Market Intelligence System:

✅ Normalized schema with items, stations, market_orders, OHLCV tables
✅ Collector with write contention handling (retry + WAL mode)
✅ Integration with play_as scheduler via `update_market` command
✅ Query helpers and CLI tool for verification
✅ Ready for deployment to 40 station agents

**Next phases** (separate plans):
- Phase 2: Webapp for market matrix visualization
- Phase 3: Arbitrage scanner implementation
- Phase 4: Agent integration with opportunities
