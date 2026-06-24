# Arbitrage Scanner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a cross-station arbitrage scanner that detects buy-low/sell-high spreads from the latest market captures, persists them to `arbitrage_opportunities`, and exposes them via a CLI (`cmd/arbitrage-scanner`) and a dashboard tab — gross-spread MVP, logistics deferred to Phase 4b.

**Architecture:** All logic in `pkg/market` as methods on `*Collector` (mirrors `GetMatrix`/`FindBestPrices`); `cmd/arbitrage-scanner` is a thin subcommand CLI over them; the market dashboard gains an additive Opportunities endpoint + tab. A new `GetItemStationPrices` read gives per-station best ask/bid + depth from the latest capture; `ScanArbitrage` pairs them, `ClaimOpportunity`/`CompleteOpportunity`/`GetOpportunities` round out claiming and display.

**Tech Stack:** Go 1.24+, `modernc.org/sqlite`, stdlib `net/http`/`flag`/`embed`, vanilla JS.

## Global Constraints

- Go 1.24+; use `range`-over-int and `b.Loop()` where applicable (`min` builtin is available).
- All new code must pass `golangci-lint` with no new findings.
- After each series of changes run `go build ./...` and `go test ./...` (interface/struct changes break things the build alone misses).
- SQLite timestamps are RFC3339 UTC strings (matches existing `pkg/market` convention).
- Sleeps/pauses use constants from `pkg/game/constants.go` (none expected in this work).
- Spec: `docs/superpowers/specs/2026-06-24-arbitrage-scanner-design.md`.
- `arbitrage_opportunities` schema already exists (migration). Columns referenced: `id` (autoincrement PK), `from_station_id`, `to_station_id`, `item_id`, `action_type` (CHECK buy_then_sell|sell_then_buy), `buy_price`, `sell_price`, `quantity`, `gross_profit`, `fuel_cost`, `travel_ticks`, `cargo_required`, `risk_score` (default 0), `claimed_by` (nullable), `claimed_at` (nullable), `status` (default 'available', CHECK available|claimed|completed|expired), `expires_at`, `discovered_at`, `discovered_by`, `notes` (nullable).
- No coordination gate: all `pkg/market` changes are additive (new file `arbitrage.go` + new types appended to `types.go`); nothing conflicts with in-flight work.

---

## File Structure

- **Modify:** `pkg/market/types.go` — append `ItemStationPrice`, `ScanOptions`, `ScanResult`; extend the existing `ArbitrageOpportunity` with display-name fields.
- **Create:** `pkg/market/arbitrage.go` — `GetItemStationPrices`, `ScanArbitrage` (+ unexported `arbCandidate`, `scanItemSet`), `ClaimOpportunity`, `CompleteOpportunity`, `GetOpportunities`.
- **Create:** `pkg/market/arbitrage_test.go` — white-box tests (`package market`) for all of the above.
- **Create:** `cmd/arbitrage-scanner/main.go` — subcommand CLI (`scan`/`list`/`claim`/`complete`).
- **Create:** `cmd/arbitrage-scanner/main_test.go` — flag-parsing tests.
- **Modify:** `cmd/market-dashboard/main.go` (route), `handlers.go` (handler), `web/index.html` (tab), `web/app.js` (render + view branch), `handlers_test.go` (test).

---

### Task 1: `GetItemStationPrices` + `ItemStationPrice` type

The arbitrage primitive: per-station best ask/bid + depth from the latest capture.

**Files:**
- Modify: `pkg/market/types.go` (append type after `BestPrice`, ~line 100)
- Create: `pkg/market/arbitrage.go`
- Test: `pkg/market/arbitrage_test.go`

**Interfaces:**
- Consumes: `*Collector`, `c.db`, `market_orders`, `stations`.
- Produces: `ItemStationPrice` type; `func (c *Collector) GetItemStationPrices(ctx context.Context, itemID string) ([]ItemStationPrice, error)`.

- [ ] **Step 1: Append the `ItemStationPrice` type to `pkg/market/types.go`** (after the `BestPrice` struct):

```go
// ItemStationPrice is one item's best ask/bid per station, from the latest capture.
// BestAsk is the cheapest sell order (where you BUY); BestBid is the highest buy
// order (where you SELL). AskQty/BidQty total the quantity of orders tying at that
// best price. The arbitrage scanner pairs these across stations.
type ItemStationPrice struct {
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	BestAsk     float64   `json:"best_ask"`
	AskQty      float64   `json:"ask_qty"`
	BestBid     float64   `json:"best_bid"`
	BidQty      float64   `json:"bid_qty"`
	HasSell     bool      `json:"has_sell"`
	HasBuy      bool      `json:"has_buy"`
	CapturedAt  time.Time `json:"captured_at"`
}
```

- [ ] **Step 2: Create `pkg/market/arbitrage.go`** with the method. (Later tasks append to this same file; it ultimately imports `context`, `database/sql`, `fmt`, `sort`, `time`.)

```go
package market

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// GetItemStationPrices returns one item's best ask and best bid per station, each
// computed from that station's latest capture. BestAsk is the cheapest sell order
// (where you would buy); BestBid is the highest buy order (where you would sell).
// AskQty/BidQty total the quantity of orders tying at that best price. Returns an
// empty slice when the item has no orders.
func (c *Collector) GetItemStationPrices(ctx context.Context, itemID string) ([]ItemStationPrice, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT o.station_id, COALESCE(s.station_name, o.station_id),
		       COALESCE(s.system_id, ''), COALESCE(s.system_name, ''),
		       o.side, o.price_each, o.quantity, o.captured_at
		FROM market_orders o
		JOIN stations s ON s.station_id = o.station_id
		JOIN (
			SELECT station_id, MAX(captured_at) AS mx
			FROM market_orders
			WHERE item_id = ?
			GROUP BY station_id
		) latest ON latest.station_id = o.station_id AND latest.mx = o.captured_at
		WHERE o.item_id = ?
		ORDER BY o.station_id`, itemID, itemID)
	if err != nil {
		return nil, fmt.Errorf("query item station prices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	order := []string{}
	byStation := map[string]*ItemStationPrice{}
	for rows.Next() {
		var stID, stName, sysID, sysName, side, capStr string
		var price, qty float64
		if err := rows.Scan(&stID, &stName, &sysID, &sysName, &side, &price, &qty, &capStr); err != nil {
			return nil, fmt.Errorf("scan item station price: %w", err)
		}
		p, ok := byStation[stID]
		if !ok {
			p = &ItemStationPrice{StationID: stID, StationName: stName, SystemID: sysID, SystemName: sysName}
			byStation[stID] = p
			order = append(order, stID)
		}
		p.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		switch side {
		case "sell":
			switch {
			case !p.HasSell:
				p.BestAsk, p.AskQty, p.HasSell = price, qty, true
			case price < p.BestAsk:
				p.BestAsk, p.AskQty = price, qty
			case price == p.BestAsk:
				p.AskQty += qty
			}
		case "buy":
			switch {
			case !p.HasBuy:
				p.BestBid, p.BidQty, p.HasBuy = price, qty, true
			case price > p.BestBid:
				p.BestBid, p.BidQty = price, qty
			case price == p.BestBid:
				p.BidQty += qty
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate item station prices: %w", err)
	}
	out := make([]ItemStationPrice, 0, len(order))
	for _, stID := range order {
		out = append(out, *byStation[stID])
	}
	return out, nil
}
```

- [ ] **Step 3: Create `pkg/market/arbitrage_test.go`** with the tests (the `openArbDB` helper is reused by later tasks):

```go
package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openArbDB(t *testing.T) *Collector {
	t.Helper()
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestGetItemStationPrices(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	write := func(stn, sys string, orders []Order) {
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: stn, StationName: stn, SystemID: sys, SystemName: sys,
			CapturedAt: now, Orders: orders,
		}); err != nil {
			t.Fatalf("WriteSnapshot %s: %v", stn, err)
		}
	}
	write("stnA", "sysA", []Order{
		{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now},
		{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 7, Quantity: 20, CapturedAt: now},
		{StationID: "stnA", ItemID: "iron_ore", Side: "buy", PriceEach: 3, Quantity: 5, CapturedAt: now},
	})
	write("stnB", "sysB", []Order{
		{StationID: "stnB", ItemID: "iron_ore", Side: "sell", PriceEach: 9, Quantity: 4, CapturedAt: now},
		{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 8, Quantity: 1, CapturedAt: now},
		{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 10, Quantity: 2, CapturedAt: now},
	})

	prices, err := c.GetItemStationPrices(ctx, "iron_ore")
	if err != nil {
		t.Fatalf("GetItemStationPrices: %v", err)
	}
	if len(prices) != 2 {
		t.Fatalf("prices = %d stations, want 2", len(prices))
	}
	by := map[string]ItemStationPrice{}
	for _, p := range prices {
		by[p.StationID] = p
	}
	a := by["stnA"]
	if !a.HasSell || a.BestAsk != 5 || a.AskQty != 10 {
		t.Errorf("stnA ask = %v qty %v (has %v), want 5/10", a.BestAsk, a.AskQty, a.HasSell)
	}
	if !a.HasBuy || a.BestBid != 3 || a.BidQty != 5 {
		t.Errorf("stnA bid = %v qty %v, want 3/5", a.BestBid, a.BidQty)
	}
	b := by["stnB"]
	if !b.HasSell || b.BestAsk != 9 || b.AskQty != 4 {
		t.Errorf("stnB ask = %v qty %v, want 9/4", b.BestAsk, b.AskQty)
	}
	if !b.HasBuy || b.BestBid != 10 || b.BidQty != 2 {
		t.Errorf("stnB bid = %v qty %v, want 10/2 (ties at 10 summed)", b.BestBid, b.BidQty)
	}
	none, err := c.GetItemStationPrices(ctx, "nope")
	if err != nil {
		t.Fatalf("absent: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("absent = %d, want 0", len(none))
	}
}

func TestGetItemStationPricesLatestCaptureWins(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	t1 := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 24, 11, 0, 0, 0, time.UTC)
	snap := func(at time.Time, price float64) {
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
			CapturedAt: at,
			Orders:     []Order{{StationID: "stn1", ItemID: "iron_ore", Side: "sell", PriceEach: price, Quantity: 1, CapturedAt: at}},
		}); err != nil {
			t.Fatalf("WriteSnapshot %v: %v", at, err)
		}
	}
	snap(t1, 9)
	snap(t2, 4) // newer capture supersedes

	prices, err := c.GetItemStationPrices(ctx, "iron_ore")
	if err != nil {
		t.Fatalf("GetItemStationPrices: %v", err)
	}
	if len(prices) != 1 || prices[0].BestAsk != 4 {
		t.Errorf("expected latest-capture best ask 4, got %+v", prices)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/market/ -run TestGetItemStationPrices -v`
Expected: PASS (both subtests).

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/types.go pkg/market/arbitrage.go pkg/market/arbitrage_test.go
git commit -m "feat(market): add GetItemStationPrices arbitrage primitive"
```

---

### Task 2: `ScanArbitrage` + `ScanOptions`/`ScanResult` types

Detect + persist spreads; expire prior `available` rows.

**Files:**
- Modify: `pkg/market/types.go` (append `ScanOptions`, `ScanResult`)
- Modify: `pkg/market/arbitrage.go` (append method + helpers)
- Test: `pkg/market/arbitrage_test.go`

**Interfaces:**
- Consumes: `GetItemStationPrices` (Task 1), `writeRetry`, `market_orders`, `arbitrage_opportunities`.
- Produces: `ScanOptions`, `ScanResult` types; `func (c *Collector) ScanArbitrage(ctx context.Context, opts ScanOptions) (ScanResult, error)`.

- [ ] **Step 1: Append `ScanOptions` and `ScanResult` to `pkg/market/types.go`** (after `ItemStationPrice`):

```go
// ScanOptions parameterizes an arbitrage scan. Zero-valued fields take the
// documented defaults when ScanArbitrage runs.
type ScanOptions struct {
	MinProfit   float64       `json:"min_profit"`   // gross_profit floor (default 1000)
	MinPrice    float64       `json:"min_price"`    // per-order price floor, filters basement orders (default 10)
	MinQuantity float64       `json:"min_quantity"` // per-order depth floor (default 1)
	ExpiresIn   time.Duration `json:"expires_in"`   // opportunity TTL (default 6h)
	Items       []string      `json:"items"`        // allowlist; empty = all traded items
	Limit       int           `json:"limit"`        // cap rows inserted (default 500)
}

// ScanResult reports what a ScanArbitrage run did.
type ScanResult struct {
	Expired     int       `json:"expired"`
	Inserted    int       `json:"inserted"`
	GeneratedAt time.Time `json:"generated_at"`
}
```

- [ ] **Step 2: Append `ScanArbitrage` + helpers to `pkg/market/arbitrage.go`**

```go
// arbCandidate is an in-memory opportunity before persistence.
type arbCandidate struct {
	fromStation, toStation, itemID        string
	buyPrice, sellPrice, qty, gross       float64
}

// ScanArbitrage detects cross-station buy-low/sell-high spreads from the latest
// market captures and persists them to arbitrage_opportunities, expiring any
// previously-available rows first (claimed/completed rows persist). Logistics
// (fuel/distance/ticks) are deferred to Phase 4b: fuel_cost=0, travel_ticks=0,
// cargo_required=quantity, notes='logistics:deferred'. Reads happen outside the
// write transaction so the write lock is held only briefly (important with ~40
// capturing agents); a capture landing mid-scan is harmless since opportunities
// are advisory.
func (c *Collector) ScanArbitrage(ctx context.Context, opts ScanOptions) (ScanResult, error) {
	if opts.MinProfit == 0 {
		opts.MinProfit = 1000
	}
	if opts.MinPrice == 0 {
		opts.MinPrice = 10
	}
	if opts.MinQuantity == 0 {
		opts.MinQuantity = 1
	}
	if opts.ExpiresIn == 0 {
		opts.ExpiresIn = 6 * time.Hour
	}
	if opts.Limit == 0 {
		opts.Limit = 500
	}

	itemIDs, err := c.scanItemSet(ctx, opts.Items)
	if err != nil {
		return ScanResult{}, err
	}

	now := time.Now().UTC()
	var candidates []arbCandidate
	for _, itemID := range itemIDs {
		prices, err := c.GetItemStationPrices(ctx, itemID)
		if err != nil {
			return ScanResult{}, fmt.Errorf("scan item %s: %w", itemID, err)
		}
		for _, src := range prices { // src = where you BUY (a sell/ask)
			if !src.HasSell || src.BestAsk < opts.MinPrice || src.AskQty < opts.MinQuantity {
				continue
			}
			for _, dst := range prices { // dst = where you SELL (a buy/bid)
				if dst.StationID == src.StationID {
					continue
				}
				if !dst.HasBuy || dst.BestBid < opts.MinPrice || dst.BidQty < opts.MinQuantity {
					continue
				}
				if dst.BestBid <= src.BestAsk {
					continue
				}
				qty := min(src.AskQty, dst.BidQty)
				gross := (dst.BestBid - src.BestAsk) * qty
				if gross < opts.MinProfit {
					continue
				}
				candidates = append(candidates, arbCandidate{
					fromStation: src.StationID,
					toStation:   dst.StationID,
					itemID:      itemID,
					buyPrice:    src.BestAsk,
					sellPrice:   dst.BestBid,
					qty:         qty,
					gross:       gross,
				})
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].gross > candidates[j].gross })
	if len(candidates) > opts.Limit {
		candidates = candidates[:opts.Limit]
	}

	expiresAt := now.Add(opts.ExpiresIn).Format(time.RFC3339)
	discoveredAt := now.Format(time.RFC3339)

	var res ScanResult
	err = c.writeRetry(ctx, func(tx *sql.Tx) error {
		exp, err := tx.ExecContext(ctx, `UPDATE arbitrage_opportunities SET status='expired' WHERE status='available'`)
		if err != nil {
			return fmt.Errorf("expire opportunities: %w", err)
		}
		expired := 0
		if n, err := exp.RowsAffected(); err == nil {
			expired = int(n)
		}
		inserted := 0
		for _, cand := range candidates {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO arbitrage_opportunities
				  (from_station_id, to_station_id, item_id, action_type, buy_price, sell_price,
				   quantity, gross_profit, fuel_cost, travel_ticks, cargo_required, status,
				   expires_at, discovered_at, discovered_by, notes)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				cand.fromStation, cand.toStation, cand.itemID, "buy_then_sell",
				cand.buyPrice, cand.sellPrice, cand.qty, cand.gross,
				0.0, 0, cand.qty, "available", expiresAt, discoveredAt, "arbitrage_scanner", "logistics:deferred")
			if err != nil {
				return fmt.Errorf("insert opportunity: %w", err)
			}
			inserted++
		}
		res.Expired = expired
		res.Inserted = inserted
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}
	res.GeneratedAt = now
	return res, nil
}

// scanItemSet returns the items to scan: the allowlist when non-empty, otherwise
// every distinct item_id present in market_orders (only traded items can yield a
// spread).
func (c *Collector) scanItemSet(ctx context.Context, allow []string) ([]string, error) {
	if len(allow) > 0 {
		return allow, nil
	}
	rows, err := c.db.QueryContext(ctx, `SELECT DISTINCT item_id FROM market_orders`)
	if err != nil {
		return nil, fmt.Errorf("query traded items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan traded item: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
```

- [ ] **Step 3: Append tests to `pkg/market/arbitrage_test.go`**

```go
// insertRawOpp inserts a minimal opportunity row directly (white-box) for
// lifecycle tests. status may be any allowed value.
func insertRawOpp(t *testing.T, c *Collector, status string) {
	t.Helper()
	_, err := c.db.Exec(`INSERT INTO arbitrage_opportunities
		(from_station_id, to_station_id, item_id, action_type, buy_price, sell_price,
		 quantity, gross_profit, fuel_cost, travel_ticks, cargo_required, status,
		 expires_at, discovered_at, discovered_by)
		VALUES ('stnA','stnB','iron_ore','buy_then_sell',1,2,1,1,0,0,1,?,
		 '2030-01-01T00:00:00Z','2026-06-24T00:00:00Z','test')`, status)
	if err != nil {
		t.Fatalf("insertRawOpp: %v", err)
	}
}

func countStatus(t *testing.T, c *Collector, status string) int {
	t.Helper()
	var n int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM arbitrage_opportunities WHERE status = ?`, status).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", status, err)
	}
	return n
}

func TestScanArbitrageHappyPath(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{{StationID: "stnA", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnA: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
		Orders: []Order{{StationID: "stnB", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnB: %v", err)
	}

	// buy at stnA ask 5, sell at stnB bid 8, qty min(10,5)=5, gross (8-5)*5=15.
	res, err := c.ScanArbitrage(ctx, ScanOptions{MinProfit: 1, MinPrice: 1, MinQuantity: 1, ExpiresIn: time.Hour})
	if err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if res.Inserted != 1 {
		t.Fatalf("inserted = %d, want 1", res.Inserted)
	}
	var buyPrice, sellPrice, qty, gross float64
	var actionType, notes, status string
	if err := c.db.QueryRow(`SELECT buy_price, sell_price, quantity, gross_profit, action_type, notes, status
		FROM arbitrage_opportunities WHERE item_id = ?`, "iron_ore").
		Scan(&buyPrice, &sellPrice, &qty, &gross, &actionType, &notes, &status); err != nil {
		t.Fatalf("query opp: %v", err)
	}
	if buyPrice != 5 || sellPrice != 8 || qty != 5 || gross != 15 {
		t.Errorf("row = buy %v sell %v qty %v gross %v, want 5/8/5/15", buyPrice, sellPrice, qty, gross)
	}
	if actionType != "buy_then_sell" || notes != "logistics:deferred" || status != "available" {
		t.Errorf("meta = action %q notes %q status %q", actionType, notes, status)
	}
}

func TestScanArbitrageSameStationExcluded(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// One station with both a cheap ask and a high bid — no cross-station pair possible.
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{
			{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now},
			{StationID: "stnA", ItemID: "iron_ore", Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	res, err := c.ScanArbitrage(ctx, ScanOptions{MinProfit: 1, MinPrice: 1})
	if err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if res.Inserted != 0 {
		t.Errorf("same-station pairs must be excluded; inserted %d", res.Inserted)
	}
}

func TestScanArbitrageFiltersBasementByMinPrice(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 1, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnA: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
		Orders: []Order{{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 200, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnB: %v", err)
	}
	// Default MinPrice 10 filters the 1cr ask (gross (200-1)*10=1990 would otherwise clear).
	def, err := c.ScanArbitrage(ctx, ScanOptions{})
	if err != nil {
		t.Fatalf("ScanArbitrage default: %v", err)
	}
	if def.Inserted != 0 {
		t.Errorf("default MinPrice=10 should filter 1cr ask; inserted %d", def.Inserted)
	}
	// Lowering the floor to 1 lets it through.
	loose, err := c.ScanArbitrage(ctx, ScanOptions{MinPrice: 1})
	if err != nil {
		t.Fatalf("ScanArbitrage loose: %v", err)
	}
	if loose.Inserted != 1 {
		t.Errorf("MinPrice=1 should let the spread through; inserted %d", loose.Inserted)
	}
}

func TestScanArbitrageExpiresAvailablePreservesClaimed(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertRawOpp(t, c, "available") // prior available → should be expired by the scan
	insertRawOpp(t, c, "claimed")   // claimed → preserved

	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnA: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
		Orders: []Order{{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnB: %v", err)
	}

	res, err := c.ScanArbitrage(ctx, ScanOptions{MinProfit: 1, MinPrice: 1})
	if err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if res.Expired != 1 {
		t.Errorf("expired = %d, want 1 (prior available)", res.Expired)
	}
	if got := countStatus(t, c, "available"); got != 1 {
		t.Errorf("available = %d, want 1 (the fresh insert)", got)
	}
	if got := countStatus(t, c, "claimed"); got != 1 {
		t.Errorf("claimed = %d, want 1 (preserved)", got)
	}
	if got := countStatus(t, c, "expired"); got != 1 {
		t.Errorf("expired rows = %d, want 1", got)
	}
}

func TestScanArbitrageLimitCap(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// Two items each yielding a spread across stnA/stnB → 2 candidates; cap at 1.
	write := func(item string) {
		_ = c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
			Orders: []Order{{StationID: "stnA", ItemID: item, Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
		})
		_ = c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
			Orders: []Order{{StationID: "stnB", ItemID: item, Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now}},
		})
	}
	write("iron_ore")
	write("copper_ore")
	res, err := c.ScanArbitrage(ctx, ScanOptions{MinProfit: 1, MinPrice: 1, Limit: 1})
	if err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if res.Inserted != 1 {
		t.Errorf("inserted = %d, want 1 (Limit cap)", res.Inserted)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/market/ -run TestScanArbitrage -v`
Expected: PASS (all five subtests).

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/types.go pkg/market/arbitrage.go pkg/market/arbitrage_test.go
git commit -m "feat(market): add ScanArbitrage spread detection + persist"
```

---

### Task 3: `ClaimOpportunity` + `CompleteOpportunity`

Atomic claiming atoms.

**Files:**
- Modify: `pkg/market/arbitrage.go` (append)
- Test: `pkg/market/arbitrage_test.go`

**Interfaces:**
- Consumes: `writeRetry`, `arbitrage_opportunities`.
- Produces: `func (c *Collector) ClaimOpportunity(ctx context.Context, id int, agentID string) (bool, error)`; `func (c *Collector) CompleteOpportunity(ctx context.Context, id int, agentID string) (bool, error)`.

- [ ] **Step 1: Append the methods to `pkg/market/arbitrage.go`**

```go
// ClaimOpportunity atomically claims an available opportunity for agentID.
// Returns true if claimed, false if it was already claimed/expired/gone.
func (c *Collector) ClaimOpportunity(ctx context.Context, id int, agentID string) (bool, error) {
	claimed := false
	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE arbitrage_opportunities SET status='claimed', claimed_by=?, claimed_at=?
			 WHERE id=? AND status='available'`,
			agentID, time.Now().UTC().Format(time.RFC3339), id)
		if err != nil {
			return fmt.Errorf("claim opportunity: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			claimed = n > 0
		}
		return nil
	})
	return claimed, err
}

// CompleteOpportunity atomically marks a claimed opportunity completed, but only
// if agentID owns the claim. Returns false (no error) if not owned/claimed.
func (c *Collector) CompleteOpportunity(ctx context.Context, id int, agentID string) (bool, error) {
	completed := false
	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE arbitrage_opportunities SET status='completed'
			 WHERE id=? AND claimed_by=? AND status='claimed'`, id, agentID)
		if err != nil {
			return fmt.Errorf("complete opportunity: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			completed = n > 0
		}
		return nil
	})
	return completed, err
}
```

- [ ] **Step 2: Append tests to `pkg/market/arbitrage_test.go`**

```go
func TestClaimOpportunity(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	insertRawOpp(t, c, "available") // id=1

	ok, err := c.ClaimOpportunity(ctx, 1, "agent1")
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	// Double-claim by another agent fails.
	ok2, _ := c.ClaimOpportunity(ctx, 1, "agent2")
	if ok2 {
		t.Errorf("double-claim should return false")
	}
	// Claiming an already-expired row fails.
	insertRawOpp(t, c, "expired") // id=2
	ok3, _ := c.ClaimOpportunity(ctx, 2, "agent1")
	if ok3 {
		t.Errorf("claim on expired should return false")
	}
}

func TestCompleteOpportunity(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	insertRawOpp(t, c, "available") // id=1
	if ok, _ := c.ClaimOpportunity(ctx, 1, "agent1"); !ok {
		t.Fatalf("setup claim failed")
	}

	ok, err := c.CompleteOpportunity(ctx, 1, "agent1")
	if err != nil || !ok {
		t.Errorf("complete by owner: ok=%v err=%v", ok, err)
	}

	// Wrong agent cannot complete.
	insertRawOpp(t, c, "available") // id=2
	c.ClaimOpportunity(ctx, 2, "agent1")
	ok2, _ := c.CompleteOpportunity(ctx, 2, "agent2")
	if ok2 {
		t.Errorf("complete by non-owner should return false")
	}
}
```

- [ ] **Step 3: Run the tests to verify they pass**

Run: `go test ./pkg/market/ -run 'TestClaimOpportunity|TestCompleteOpportunity' -v`
Expected: PASS.

- [ ] **Step 4: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/arbitrage.go pkg/market/arbitrage_test.go
git commit -m "feat(market): add Claim/CompleteOpportunity atoms"
```

---

### Task 4: `GetOpportunities` + extend `ArbitrageOpportunity`

Read opportunities for the dashboard, with joined station/system/item names.

**Files:**
- Modify: `pkg/market/types.go` (extend `ArbitrageOpportunity` with display fields)
- Modify: `pkg/market/arbitrage.go` (append method)
- Test: `pkg/market/arbitrage_test.go`

**Interfaces:**
- Consumes: `arbitrage_opportunities`, `stations`, `items`.
- Produces: `ArbitrageOpportunity` extended with `FromStationName`/`FromSystemName`/`ToStationName`/`ToSystemName`/`ItemName`; `func (c *Collector) GetOpportunities(ctx context.Context, status string, limit int) ([]ArbitrageOpportunity, error)`.

- [ ] **Step 1: Extend `ArbitrageOpportunity` in `pkg/market/types.go`** — append these five additive fields after `Notes` (line ~75):

```go
	RiskScore     float64 `json:"risk_score"`
	ClaimedBy     string  `json:"claimed_by"`
	ClaimedAt     string  `json:"claimed_at"`
	Status        string  `json:"status"` // "available", "claimed", "completed", "expired"
	ExpiresAt     string  `json:"expires_at"`
	DiscoveredAt  string  `json:"discovered_at"`
	DiscoveredBy  string  `json:"discovered_by"`
	Notes         string  `json:"notes"`
	FromStationName string `json:"from_station_name"` // joined on read
	FromSystemName  string `json:"from_system_name"`  // joined on read
	ToStationName   string `json:"to_station_name"`   // joined on read
	ToSystemName    string `json:"to_system_name"`    // joined on read
	ItemName        string `json:"item_name"`         // joined on read
}
```

(Only the last five lines are new; the preceding fields already exist — match from `RiskScore` through `Notes` to anchor the edit and append the five new fields before the closing brace.)

- [ ] **Step 2: Append `GetOpportunities` to `pkg/market/arbitrage.go`**

```go
// GetOpportunities returns opportunities ordered by gross_profit DESC, optionally
// filtered to a status ("" = all). Station/system/item names are joined for
// display. Returns an empty slice when none match.
func (c *Collector) GetOpportunities(ctx context.Context, status string, limit int) ([]ArbitrageOpportunity, error) {
	if limit < 1 {
		limit = 50
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT ao.id, ao.from_station_id, COALESCE(fs.station_name, ''), COALESCE(fs.system_name, ''),
		       ao.to_station_id, COALESCE(ts.station_name, ''), COALESCE(ts.system_name, ''),
		       ao.item_id, COALESCE(i.item_name, ''), ao.action_type, ao.status,
		       ao.buy_price, ao.sell_price, ao.quantity, ao.gross_profit,
		       ao.fuel_cost, ao.travel_ticks, ao.cargo_required,
		       ao.claimed_by, ao.claimed_at, ao.expires_at, ao.discovered_at, ao.notes
		FROM arbitrage_opportunities ao
		LEFT JOIN stations fs ON fs.station_id = ao.from_station_id
		LEFT JOIN stations ts ON ts.station_id = ao.to_station_id
		LEFT JOIN items i ON i.item_id = ao.item_id
		WHERE (? = '' OR ao.status = ?)
		ORDER BY ao.gross_profit DESC
		LIMIT ?`, status, status, limit)
	if err != nil {
		return nil, fmt.Errorf("query opportunities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ArbitrageOpportunity
	for rows.Next() {
		var o ArbitrageOpportunity
		var claimedBy, claimedAt, notes sql.NullString
		if err := rows.Scan(&o.ID, &o.FromStationID, &o.FromStationName, &o.FromSystemName,
			&o.ToStationID, &o.ToStationName, &o.ToSystemName,
			&o.ItemID, &o.ItemName, &o.ActionType, &o.Status,
			&o.BuyPrice, &o.SellPrice, &o.Quantity, &o.GrossProfit,
			&o.FuelCost, &o.TravelTicks, &o.CargoRequired,
			&claimedBy, &claimedAt, &o.ExpiresAt, &o.DiscoveredAt, &notes); err != nil {
			return nil, fmt.Errorf("scan opportunity: %w", err)
		}
		o.ClaimedBy = claimedBy.String
		o.ClaimedAt = claimedAt.String
		o.Notes = notes.String
		out = append(out, o)
	}
	return out, rows.Err()
}
```

- [ ] **Step 3: Append the test to `pkg/market/arbitrage_test.go`**

```go
func TestGetOpportunities(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_ = c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{{StationID: "stnA", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
	})
	_ = c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
		Orders: []Order{{StationID: "stnB", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now}},
	})
	if _, err := c.ScanArbitrage(ctx, ScanOptions{MinProfit: 1, MinPrice: 1}); err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}

	opps, err := c.GetOpportunities(ctx, "available", 50)
	if err != nil {
		t.Fatalf("GetOpportunities: %v", err)
	}
	if len(opps) != 1 {
		t.Fatalf("opps = %d, want 1", len(opps))
	}
	o := opps[0]
	if o.FromStationName != "Alpha" || o.ToStationName != "Beta" {
		t.Errorf("names = %q → %q, want Alpha → Beta", o.FromStationName, o.ToStationName)
	}
	if o.FromSystemName != "Sol" || o.ToSystemName != "Sirius" {
		t.Errorf("systems = %q → %q, want Sol → Sirius", o.FromSystemName, o.ToSystemName)
	}
	if o.ItemName != "Iron Ore" {
		t.Errorf("item_name = %q, want Iron Ore", o.ItemName)
	}
	if o.GrossProfit != 15 {
		t.Errorf("gross = %v, want 15", o.GrossProfit)
	}

	// status filter excludes when mismatched.
	none, err := c.GetOpportunities(ctx, "claimed", 50)
	if err != nil {
		t.Fatalf("GetOpportunities claimed: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("claimed filter = %d, want 0", len(none))
	}
	// "" returns all.
	all, _ := c.GetOpportunities(ctx, "", 50)
	if len(all) != 1 {
		t.Errorf("all = %d, want 1", len(all))
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/market/ -run TestGetOpportunities -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/types.go pkg/market/arbitrage.go pkg/market/arbitrage_test.go
git commit -m "feat(market): add GetOpportunities + display-name fields"
```

---

### Task 5: `cmd/arbitrage-scanner` CLI

Thin subcommand CLI over the package methods.

**Files:**
- Create: `cmd/arbitrage-scanner/main.go`
- Create: `cmd/arbitrage-scanner/main_test.go`

**Interfaces:**
- Consumes: `market.Open`, `market.Config`, `market.ScanOptions`, `ScanArbitrage`, `GetOpportunities`, `ClaimOpportunity`, `CompleteOpportunity`.
- Produces: a runnable `cmd/arbitrage-scanner` binary with subcommands `scan` (default), `list`, `claim`, `complete`.

- [ ] **Step 1: Create `cmd/arbitrage-scanner/main.go`**

```go
// Command arbitrage-scanner detects cross-station buy-low/sell-high spreads in
// data/market.db, persists them to arbitrage_opportunities, and lists/claims/
// completes them. See docs/superpowers/specs/2026-06-24-arbitrage-scanner-design.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
)

const usage = `arbitrage-scanner — detect and manage cross-station arbitrage opportunities.

Usage:
  arbitrage-scanner scan     [flags]   detect spreads and write opportunities (default)
  arbitrage-scanner list     [flags]   list opportunities
  arbitrage-scanner claim    --id N --agent X
  arbitrage-scanner complete --id N --agent X

Scan flags:
  --market-db-path PATH   path to market SQLite DB (default data/market.db)
  --min-profit FLOAT       gross-profit floor (default 1000)
  --min-price FLOAT        per-order price floor, filters basement orders (default 10)
  --min-quantity FLOAT     per-order depth floor (default 1)
  --expires DURATION       opportunity TTL (default 6h)
  --items a,b,c            item allowlist (default: all traded items)
  --limit N                cap on inserted rows (default 500)
  --json                   machine-readable output
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "arbitrage-scanner:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return runScan(defaultScanArgs())
	}
	switch args[0] {
	case "scan":
		cfg, err := parseScanArgs(args[1:])
		if err != nil {
			return err
		}
		return runScan(cfg)
	case "list":
		return runList(args[1:])
	case "claim":
		return runClaim(args[1:])
	case "complete":
		return runComplete(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		if strings.HasPrefix(args[0], "-") { // flags with no subcommand → default scan
			cfg, err := parseScanArgs(args)
			if err != nil {
				return err
			}
			return runScan(cfg)
		}
		return fmt.Errorf("unknown subcommand %q (want scan|list|claim|complete)", args[0])
	}
}

// scanConfig holds parsed scan flags.
type scanConfig struct {
	dbPath string
	opts   market.ScanOptions
	asJSON bool
}

func defaultScanArgs() scanConfig {
	return scanConfig{
		dbPath: "data/market.db",
		opts: market.ScanOptions{
			MinProfit: 1000, MinPrice: 10, MinQuantity: 1,
			ExpiresIn: 6 * time.Hour, Limit: 500,
		},
	}
}

func parseScanArgs(args []string) (scanConfig, error) {
	cfg := defaultScanArgs()
	var items string
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.StringVar(&cfg.dbPath, "market-db-path", cfg.dbPath, "path to market SQLite database")
	fs.Float64Var(&cfg.opts.MinProfit, "min-profit", cfg.opts.MinProfit, "gross-profit floor")
	fs.Float64Var(&cfg.opts.MinPrice, "min-price", cfg.opts.MinPrice, "per-order price floor (filters basement orders)")
	fs.Float64Var(&cfg.opts.MinQuantity, "min-quantity", cfg.opts.MinQuantity, "per-order depth floor")
	fs.DurationVar(&cfg.opts.ExpiresIn, "expires", cfg.opts.ExpiresIn, "opportunity TTL")
	fs.StringVar(&items, "items", "", "comma-separated item allowlist (default: all traded items)")
	fs.IntVar(&cfg.opts.Limit, "limit", cfg.opts.Limit, "cap on inserted rows")
	fs.BoolVar(&cfg.asJSON, "json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	for _, s := range strings.Split(items, ",") {
		if t := strings.TrimSpace(s); t != "" {
			cfg.opts.Items = append(cfg.opts.Items, t)
		}
	}
	return cfg, nil
}

func runScan(cfg scanConfig) error {
	c, err := market.Open(market.Config{DBPath: cfg.dbPath})
	if err != nil {
		return fmt.Errorf("open %s: %w", cfg.dbPath, err)
	}
	defer func() { _ = c.Close() }()

	res, err := c.ScanArbitrage(context.Background(), cfg.opts)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	if cfg.asJSON {
		out := map[string]any{
			"expired":      res.Expired,
			"inserted":     res.Inserted,
			"generated_at": res.GeneratedAt,
		}
		if top, err := c.GetOpportunities(context.Background(), "available", 20); err == nil {
			out["top"] = top
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	fmt.Printf("scan complete: expired %d available, inserted %d opportunities (at %s)\n",
		res.Expired, res.Inserted, res.GeneratedAt.Format(time.RFC3339))
	return nil
}

func runList(args []string) error {
	dbPath := "data/market.db"
	status := "available"
	limit := 50
	asJSON := false
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.StringVar(&dbPath, "market-db-path", dbPath, "path to market SQLite database")
	fs.StringVar(&status, "status", status, "status filter (available|claimed|completed|expired|\"\")")
	fs.IntVar(&limit, "limit", limit, "row cap")
	fs.BoolVar(&asJSON, "json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := market.Open(market.Config{DBPath: dbPath})
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { _ = c.Close() }()

	opps, err := c.GetOpportunities(context.Background(), status, limit)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(opps)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tITEM\tFROM→TO\tBUY\tSELL\tQTY\tGROSS\tSTATUS\tEXPIRES")
	for _, o := range opps {
		fmt.Fprintf(w, "%d\t%s\t%s→%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			o.ID, o.ItemID, o.FromStationID, o.ToStationID,
			fmtPrice(o.BuyPrice), fmtPrice(o.SellPrice), fmtPrice(o.Quantity), fmtPrice(o.GrossProfit),
			o.Status, o.ExpiresAt)
	}
	return w.Flush()
}

func runClaim(args []string) error {
	dbPath, id, agent, err := parseIDAgent("claim", args)
	if err != nil {
		return err
	}
	c, err := market.Open(market.Config{DBPath: dbPath})
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { _ = c.Close() }()
	ok, err := c.ClaimOpportunity(context.Background(), id, agent)
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	if ok {
		fmt.Printf("claimed opportunity %d by %s\n", id, agent)
	} else {
		fmt.Printf("unavailable: opportunity %d is not claimable\n", id)
	}
	return nil
}

func runComplete(args []string) error {
	dbPath, id, agent, err := parseIDAgent("complete", args)
	if err != nil {
		return err
	}
	c, err := market.Open(market.Config{DBPath: dbPath})
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { _ = c.Close() }()
	ok, err := c.CompleteOpportunity(context.Background(), id, agent)
	if err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	if ok {
		fmt.Printf("completed opportunity %d\n", id)
	} else {
		fmt.Printf("not-owned: opportunity %d is not completable by %s\n", id, agent)
	}
	return nil
}

func parseIDAgent(name string, args []string) (dbPath string, id int, agent string, err error) {
	dbPath = "data/market.db"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&dbPath, "market-db-path", dbPath, "path to market SQLite database")
	fs.IntVar(&id, "id", 0, "opportunity id")
	fs.StringVar(&agent, "agent", "", "agent id")
	if err = fs.Parse(args); err != nil {
		return
	}
	if id == 0 {
		err = fmt.Errorf("--id is required")
		return
	}
	if agent == "" {
		err = fmt.Errorf("--agent is required")
		return
	}
	return
}

func fmtPrice(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
```

- [ ] **Step 2: Create `cmd/arbitrage-scanner/main_test.go`** (flag parsing; heavy logic is pkg-tested)

```go
package main

import (
	"testing"
	"time"
)

func TestParseScanArgsDefaults(t *testing.T) {
	cfg, err := parseScanArgs(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.dbPath != "data/market.db" {
		t.Errorf("dbPath = %q", cfg.dbPath)
	}
	if cfg.opts.MinProfit != 1000 || cfg.opts.MinPrice != 10 || cfg.opts.MinQuantity != 1 {
		t.Errorf("defaults = profit %v price %v qty %v", cfg.opts.MinProfit, cfg.opts.MinPrice, cfg.opts.MinQuantity)
	}
	if cfg.opts.ExpiresIn != 6*time.Hour {
		t.Errorf("expires = %v", cfg.opts.ExpiresIn)
	}
	if cfg.opts.Limit != 500 {
		t.Errorf("limit = %v", cfg.opts.Limit)
	}
	if cfg.asJSON || len(cfg.opts.Items) != 0 {
		t.Errorf("asJSON=%v items=%v", cfg.asJSON, cfg.opts.Items)
	}
}

func TestParseScanArgsOverrides(t *testing.T) {
	args := []string{
		"-min-profit", "500", "-min-price", "1", "-items", "iron_ore, copper_ore",
		"-expires", "2h", "-limit", "10", "-json", "-market-db-path", "/tmp/x.db",
	}
	cfg, err := parseScanArgs(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.opts.MinProfit != 500 || cfg.opts.MinPrice != 1 || cfg.opts.ExpiresIn != 2*time.Hour || cfg.opts.Limit != 10 {
		t.Errorf("overrides wrong: %+v", cfg.opts)
	}
	if cfg.dbPath != "/tmp/x.db" {
		t.Errorf("dbPath = %q", cfg.dbPath)
	}
	if !cfg.asJSON {
		t.Errorf("asJSON should be true")
	}
	if len(cfg.opts.Items) != 2 || cfg.opts.Items[0] != "iron_ore" || cfg.opts.Items[1] != "copper_ore" {
		t.Errorf("items = %v", cfg.opts.Items)
	}
}

func TestParseIDAgentRequiresFlags(t *testing.T) {
	if _, _, _, err := parseIDAgent("claim", nil); err == nil {
		t.Error("expected error when --id/--agent missing")
	}
	_, id, agent, err := parseIDAgent("claim", []string{"-id", "7", "-agent", "a1"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != 7 || agent != "a1" {
		t.Errorf("id=%d agent=%q", id, agent)
	}
}
```

- [ ] **Step 3: Run the tests + build to verify they pass**

Run: `go test ./cmd/arbitrage-scanner/... -v` and `go build ./...`
Expected: PASS; binary builds.

- [ ] **Step 4: Build binary into `bin/`, lint, commit**

```bash
go build -o bin/arbitrage-scanner ./cmd/arbitrage-scanner
golangci-lint run ./cmd/arbitrage-scanner/...
git add cmd/arbitrage-scanner/
git commit -m "feat(arbitrage-scanner): CLI scan/list/claim/complete subcommands"
```

---

### Task 6: Dashboard Opportunities view

Additive endpoint + tab in `cmd/market-dashboard`.

**Files:**
- Modify: `cmd/market-dashboard/main.go` (register route)
- Modify: `cmd/market-dashboard/handlers.go` (add handler)
- Modify: `cmd/market-dashboard/web/index.html` (add tab)
- Modify: `cmd/market-dashboard/web/app.js` (render + view branch)
- Test: `cmd/market-dashboard/handlers_test.go`

**Interfaces:**
- Consumes: `market.GetOpportunities`.
- Produces: `GET /api/opportunities` + an "Opportunities" tab.

- [ ] **Step 1: Register the route in `cmd/market-dashboard/main.go`** — add after the captures route (line ~42):

```go
	mux.HandleFunc("GET /api/captures", srv.capturesHandler)
	mux.HandleFunc("GET /api/opportunities", srv.opportunitiesHandler)
```

- [ ] **Step 2: Add the handler to `cmd/market-dashboard/handlers.go`** (append):

```go
func (s *server) opportunitiesHandler(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	opps, err := s.col.GetOpportunities(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if opps == nil {
		opps = []market.ArbitrageOpportunity{}
	}
	writeJSON(w, http.StatusOK, opps)
}
```

- [ ] **Step 3: Add the tab to `cmd/market-dashboard/web/index.html`** — add after the Capture health tab (line ~18):

```html
  <button data-view="health" class="tab" type="button">Capture health</button>
  <button data-view="opps" class="tab" type="button">Opportunities</button>
```

- [ ] **Step 4: Add render + view branch to `cmd/market-dashboard/web/app.js`**

Append `renderOpps` after `renderHealth`:

```js
async function renderOpps() {
  try {
    const opps = await getJSON('/api/opportunities?limit=100');
    if (!opps.length) { app.innerHTML = '<p>No opportunities. Run <code>arbitrage-scanner scan</code>.</p>'; return; }
    const rows = opps.map(o =>
      `<tr><td class="item">${o.item_id}<br><small>${o.item_name || ''}</small></td>
       <td>${o.from_station_name || o.from_station_id} → ${o.to_station_name || o.to_station_id}<br><small>${o.from_system_name || ''} → ${o.to_system_name || ''}</small></td>
       <td>${fmt(o.buy_price)}</td><td>${fmt(o.sell_price)}</td><td>${fmt(o.quantity)}</td>
       <td class="sell">${fmt(o.gross_profit)}</td><td>${o.status}</td><td>${relTime(o.expires_at)}</td></tr>`).join('');
    app.innerHTML = `<h3>Arbitrage opportunities</h3>
      <table><thead><tr><th>item</th><th>from → to</th><th>buy</th><th>sell</th><th>qty</th><th>gross</th><th>status</th><th>expires</th></tr></thead><tbody>${rows}</tbody></table>`;
  } catch (e) { showError(e); }
}
```

Add the `opps` branch to `showView` (after the `health` branch):

```js
  if (v === 'matrix') renderMatrix();
  else if (v === 'price') renderPrice();
  else if (v === 'health') renderHealth();
  else if (v === 'opps') renderOpps();
```

- [ ] **Step 5: Add the handler test to `cmd/market-dashboard/handlers_test.go`** (append):

```go
func TestOpportunitiesHandlerEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/opportunities", nil)
	rec := httptest.NewRecorder()
	srv.opportunitiesHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var opps []market.ArbitrageOpportunity
	if err := json.Unmarshal(rec.Body.Bytes(), &opps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(opps) != 0 {
		t.Errorf("opps = %d, want 0", len(opps))
	}
}
```

- [ ] **Step 6: Run the tests + build to verify they pass**

Run: `go test ./cmd/market-dashboard/... -v` and `go build ./...`
Expected: PASS (all dashboard tests incl. the new one); embed compiles.

- [ ] **Step 7: Lint + commit**

```bash
golangci-lint run ./cmd/market-dashboard/...
git add cmd/market-dashboard/
git commit -m "feat(market-dashboard): opportunities endpoint + tab"
```

---

### Task 7: Final verification + smoke

**Files:** none (verification only).

- [ ] **Step 1: Whole-tree build + test + lint**

```bash
go build ./...
go test ./...
golangci-lint run ./...
```
Expected: all PASS, no new lint findings.

- [ ] **Step 2: Smoke against the real DB**

```bash
go build -o bin/arbitrage-scanner ./cmd/arbitrage-scanner
./bin/arbitrage-scanner scan --market-db-path /home/robert/spacemolt/spacemolt/data/market.db
./bin/arbitrage-scanner list --market-db-path /home/robert/spacemolt/spacemolt/data/market.db --limit 20
./bin/arbitrage-scanner scan --market-db-path /home/robert/spacemolt/spacemolt/data/market.db --json | head
```
Then start the dashboard and open the Opportunities tab:
```bash
go build -o bin/market-dashboard ./cmd/market-dashboard
./bin/market-dashboard --market-db-path /home/robert/spacemolt/spacemolt/data/market.db --addr :8090
```
Confirm:
- `scan` reports `expired N available, inserted M opportunities`; `list` shows rows with item/from→to/buy/sell/qty/gross/status/expires; `--json` dump's `top` rows look credible (no 1cr basement asks as buy sources).
- Dashboard Opportunities tab renders the same rows, sorted by gross desc; empty state renders if no opportunities.

- [ ] **Step 3: claim → complete round-trip (manual)**

Pick an id from `list`, then:
```bash
./bin/arbitrage-scanner claim --market-db-path /home/robert/spacemolt/spacemolt/data/market.db --id <ID> --agent smoke
./bin/arbitrage-scanner complete --market-db-path /home/robert/spacemolt/spacemolt/data/market.db --id <ID> --agent smoke
```
Confirm: `claim` prints `claimed`; the dashboard row shows status `claimed`; `complete` prints `completed`; re-claiming prints `unavailable`.

- [ ] **Step 4: Commit any smoke-driven notes (none expected)**

If smoke surfaced fixes, commit them; otherwise no commit (verification only).

---

## Self-Review

**Spec coverage:**
- Gross-spread MVP (logistics deferred, `fuel_cost=0`/`travel_ticks=0`/`cargo_required=qty`/`notes='logistics:deferred'`) → Task 2 INSERT + `logistics:deferred` asserted in Task 2 test. ✓
- Raw best-price + floor (MinPrice/MinQuantity) → Task 2 (`GetItemStationPrices` best ask/bid + Task 2 filter + `TestScanArbitrageFiltersBasementByMinPrice`). ✓
- `GetItemStationPrices` primitive (both-side depth, latest capture) → Task 1. ✓
- `ScanArbitrage` detect+persist, expire-then-insert → Task 2. ✓
- `ClaimOpportunity` / `CompleteOpportunity` → Task 3. ✓
- `GetOpportunities` + display-name join → Task 4. ✓
- `cmd/arbitrage-scanner` subcommands (scan/list/claim/complete, `--json`) → Task 5. ✓
- Dashboard `/api/opportunities` + Opportunities tab → Task 6. ✓
- Edge cases (basement, same-station, expire lifecycle, no-data, claim race, wrong agent, limit) → Tasks 2–4 tests. ✓
- TDD per slice + build/test/lint after each series + smoke → every task + Task 7. ✓
- Out of scope correctly omitted: no logistics, no scheduling, no `sell_then_buy`, no `failed` status, no multi-hop. ✓

**Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to" — every code step carries complete code; every test is fully written.

**Type consistency:** `ItemStationPrice` (Task 1) consumed by `ScanArbitrage` (Task 2) with matching `BestAsk`/`AskQty`/`BestBid`/`BidQty`/`HasSell`/`HasBuy`. `ScanOptions`/`ScanResult` (Task 2) match the CLI's `market.ScanOptions` usage (Task 5). `ArbitrageOpportunity` display fields (Task 4) match the JS field reads in Task 6 (`from_station_name`, `to_station_name`, `from_system_name`, `to_system_name`, `item_name`, `gross_profit`, `buy_price`, `sell_price`, `quantity`, `status`, `expires_at`). Method names match across producers (Tasks 1–4) and consumers (Tasks 5–6): `GetItemStationPrices`, `ScanArbitrage`, `ClaimOpportunity`, `CompleteOpportunity`, `GetOpportunities`.

**Note on intermediate state:** Each task compiles and its tests pass independently. Tasks 5–6 depend on the package methods (Tasks 1–4); execute in order.
