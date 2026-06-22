# Market Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone, read-only web dashboard (`cmd/market-dashboard`) over `data/market.db` for validating market collection — a matrix of best buy/sell prices, a cell-detail order book, a per-item price-over-time view, and a capture-health view — plus the additive `pkg/market` read methods that back them and a small category-capture fix.

**Architecture:** Direct reads from `*market.Collector`. Add four read methods to `pkg/market` (additive) and fix category capture (small in-place edit), then a new `cmd/market-dashboard` binary: stdlib `http.ServeMux` JSON API + a vanilla-JS UI embedded via `//go:embed` (no build step).

**Tech Stack:** Go 1.24+, `modernc.org/sqlite`, stdlib `net/http` (Go 1.22+ enhanced `ServeMux` with `r.PathValue`), `embed`, vanilla JS/HTML/CSS.

## Global Constraints

- Go 1.24+; use `range`-over-int and `b.Loop()` where applicable.
- All new code must pass `golangci-lint` with no new findings.
- After each series of changes run `go build ./...` and `go test ./...` (interface/struct changes break things the build alone misses).
- SQLite timestamps are RFC3339 UTC strings (matches existing `pkg/market` convention).
- Sleeps/pauses use constants from `pkg/game/constants.go` (none expected in this work).
- Spec: `docs/superpowers/specs/2026-06-22-market-dashboard-design.md`.

**Port set (new read methods on `*Collector`):** `GetMatrix`, `GetStationOrders`, `GetItemPriceHistory`, `GetCaptureHealth` (existing `GetStats`, `GetLatestOrders` already present).
**Cell semantics (locked):** for a cell's latest capture — `BestSell` = MIN price over sell orders (cheapest ask); `BestBuy` = MAX price over buy orders (highest bid); `VWAP` = Σ(price·qty)/Σ(qty) over **sell** orders; `Volume` = Σ quantity over **sell** orders; `OrderCount` = COUNT(*) both sides; `HasSell`/`HasBuy` = whether that side has rows. Sparse cell (no orders for item×station) → absent from the per-station map (rendered `—`).

---

## Pre-flight: Sync to head (COORDINATION GATE — do before Task 1)

This branch (`feat/market-dashboard`) was cut from `main` at the Phase 1 merge (`944b899`). A parallel effort (`feat/market-data-consolidation`) is also editing `pkg/market`. **Before implementing**, rebase onto `main` once consolidation has merged so the additive changes here land cleanly on top.

- [ ] **Step 1: Confirm consolidation has merged to `main`**

Run: `git fetch origin && git log --oneline origin/main | head -20`
Expected: a merge commit for `feat/market-data-consolidation` appears on `origin/main`. If not yet present, STOP here and resume once it lands (continue only the spec/plan work in the meantime).

- [ ] **Step 2: Rebase this branch onto `main`**

Run:
```bash
git fetch origin
git rebase origin/main
```
Expected: clean auto-merge. The `pkg/market` changes in this plan are additive (new methods appended to `query.go`; the category fix is a small in-place edit in `capture.go`/`collector.go`/`types.go`) so they should not conflict with consolidation's additive methods. If a conflict arises in `query.go`, keep both sets of functions.

- [ ] **Step 3: Verify the rebased tree is green**

Run: `go build ./... && go test ./pkg/market/...`
Expected: PASS. Then proceed to Task 1.

---

### Task 1: Category-capture fix — persist `item.Category`

The server sends `category` on every `view_market` item row (`serverapi.ViewMarketItem.Category`, `types.go:307`), but `parseViewMarket` never reads it and `WriteSnapshot` hardcodes `Category: ""`. Thread it through.

**Files:**
- Modify: `pkg/market/types.go` (add `Category` to `Order`)
- Modify: `pkg/market/capture.go:34-44,52-62` (populate `Category`)
- Modify: `pkg/market/collector.go:347-365` (use `o.Category` instead of `""`)
- Test: `pkg/market/collector_test.go`

**Interfaces:**
- Consumes: existing `Order`, `WriteSnapshot`, `serverapi.ViewMarketItem.Category`.
- Produces: `Order.Category string` (new field); `items.category` now populated on capture.

- [ ] **Step 1: Write the failing test**

Append to `pkg/market/collector_test.go`:

```go
func TestWriteSnapshotPersistsItemCategory(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
		CapturedAt: now,
		Orders: []Order{{
			StationID: "stn1", ItemID: "iron_ore", ItemName: "Iron Ore", Category: "raw",
			Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now,
		}},
	}); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	var category string
	if err := c.db.QueryRowContext(ctx,
		`SELECT category FROM items WHERE item_id = ?`, "iron_ore").Scan(&category); err != nil {
		t.Fatalf("query item: %v", err)
	}
	if category != "raw" {
		t.Errorf("items.category = %q, want %q", category, "raw")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestWriteSnapshotPersistsItemCategory -v`
Expected: FAIL — `items.category = "", want "raw"`.

- [ ] **Step 3: Add `Category` to `Order`**

In `pkg/market/types.go`, add the field to the `Order` struct (after `ItemName`):

```go
// Order represents a single buy or sell order from the market.
type Order struct {
	StationID  string    `json:"station_id"`
	ItemID     string    `json:"item_id"`
	ItemName   string    `json:"item_name"`
	Category   string    `json:"category"`
	Side       string    `json:"side"` // "buy" or "sell"
	PriceEach  float64   `json:"price_each"`
	Quantity   float64   `json:"quantity"`
	MyQuantity float64   `json:"my_quantity"`
	Source     string    `json:"source"`
	CapturedAt time.Time `json:"captured_at"`
	BucketUTC  string    `json:"bucket_utc"` // Truncated to hour
}
```

- [ ] **Step 4: Populate `Category` in `parseViewMarket`**

In `pkg/market/capture.go`, add `Category: item.Category,` to **both** `Order{...}` literals (buy loop at ~line 34 and sell loop at ~line 52). Buy example:

```go
			orders = append(orders, Order{
				StationID:  stationID,
				ItemID:     item.ItemID,
				ItemName:   item.ItemName,
				Category:   item.Category,
				Side:       "buy",
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
```

Apply the identical `Category: item.Category,` line in the sell-order literal.

- [ ] **Step 5: Use `o.Category` in `WriteSnapshot`'s item map**

In `pkg/market/collector.go`, replace the `Category: ""` line in `WriteSnapshot`'s item-map block and add first-non-empty-wins handling (mirroring `ItemName`). The block becomes:

```go
		// Group orders by item for upsert (first non-empty ItemName/Category wins)
		itemMap := make(map[string]Item)
		for _, o := range snapshot.Orders {
			existing, ok := itemMap[o.ItemID]
			if !ok {
				itemMap[o.ItemID] = Item{
					ItemID:         o.ItemID,
					ItemName:       o.ItemName,
					Category:       o.Category,
					FirstSeenUTC:   now,
					LastUpdatedUTC: now,
				}
				continue
			}
			if existing.ItemName == "" && o.ItemName != "" {
				existing.ItemName = o.ItemName
			}
			if existing.Category == "" && o.Category != "" {
				existing.Category = o.Category
			}
			itemMap[o.ItemID] = existing
		}
```

(`upsertItem` already writes `category`; no change needed there.)

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestWriteSnapshotPersistsItemCategory -v`
Expected: PASS.

- [ ] **Step 7: Full package test + lint + commit**

```bash
go test ./pkg/market/...
golangci-lint run ./pkg/market/...
git add pkg/market/types.go pkg/market/capture.go pkg/market/collector.go pkg/market/collector_test.go
git commit -m "fix(market): persist item.Category through capture path"
```

---

### Task 2: `GetMatrix` + Matrix types

**Files:**
- Modify: `pkg/market/types.go` (append Matrix types)
- Modify: `pkg/market/query.go` (append method; add `strings` import)
- Test: `pkg/market/query_test.go`

**Interfaces:**
- Consumes: `*Collector`, `Station`, `Item` (for category/name).
- Produces:
  - `type MatrixQuery struct { Category, Search string; Page, Limit int }`
  - `type MatrixCell struct { StationID, StationName, SystemID, SystemName string; BestSell, BestBuy, VWAP, Volume float64; OrderCount int; CapturedAt time.Time; HasSell, HasBuy bool }`
  - `type MatrixItem struct { ItemID, ItemName, Category string; Cells []MatrixCell }`
  - `type Matrix struct { Stations []Station; Items []MatrixItem; TotalItems, Page, Limit int; GeneratedAt time.Time }`
  - `func (c *Collector) GetMatrix(ctx context.Context, q MatrixQuery) (*Matrix, error)` — paginated items × all stations; per cell, the latest capture's aggregates per the locked cell semantics. Empty filter matches all.

- [ ] **Step 1: Write the failing test**

Append to `pkg/market/query_test.go`:

```go
func TestGetMatrix(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = c.Close() }()

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
		{StationID: "stnA", ItemID: "iron_ore", ItemName: "Iron Ore", Category: "raw", Side: "sell", PriceEach: 9, Quantity: 10, CapturedAt: now},
		{StationID: "stnA", ItemID: "iron_ore", ItemName: "Iron Ore", Category: "raw", Side: "sell", PriceEach: 11, Quantity: 20, CapturedAt: now},
		{StationID: "stnA", ItemID: "iron_ore", ItemName: "Iron Ore", Category: "raw", Side: "buy", PriceEach: 3, Quantity: 5, CapturedAt: now},
	})
	write("stnB", "sysB", []Order{
		{StationID: "stnB", ItemID: "iron_ore", ItemName: "Iron Ore", Category: "raw", Side: "sell", PriceEach: 7, Quantity: 4, CapturedAt: now},
	})
	write("stnB", "sysB", []Order{
		{StationID: "stnB", ItemID: "copper_ore", ItemName: "Copper Ore", Category: "raw", Side: "sell", PriceEach: 2, Quantity: 1, CapturedAt: now},
	})

	m, err := c.GetMatrix(ctx, MatrixQuery{Page: 1, Limit: 50})
	if err != nil {
		t.Fatalf("GetMatrix: %v", err)
	}
	if len(m.Stations) != 2 {
		t.Fatalf("stations = %d, want 2", len(m.Stations))
	}
	if m.TotalItems != 2 {
		t.Fatalf("total items = %d, want 2", m.TotalItems)
	}
	byItem := map[string]MatrixItem{}
	for _, it := range m.Items {
		byItem[it.ItemID] = it
	}
	iron := byItem["iron_ore"]
	if len(iron.Cells) != 2 {
		t.Fatalf("iron cells = %d, want 2", len(iron.Cells))
	}
	cellOf := map[string]MatrixCell{}
	for _, cc := range iron.Cells {
		cellOf[cc.StationID] = cc
	}
	a := cellOf["stnA"]
	if !a.HasSell || a.BestSell != 9 {
		t.Errorf("stnA best sell = %v (has %v), want 9", a.BestSell, a.HasSell)
	}
	if !a.HasBuy || a.BestBuy != 3 {
		t.Errorf("stnA best buy = %v, want 3", a.BestBuy)
	}
	// VWAP over sell = (9*10 + 11*20)/(10+20) = 310/30 ≈ 10.333
	if math.Abs(a.VWAP-(9*10+11*20)/30.0) > 1e-6 {
		t.Errorf("stnA vwap = %v, want ~10.333", a.VWAP)
	}
	if a.Volume != 30 {
		t.Errorf("stnA volume = %v, want 30", a.Volume)
	}
	if a.OrderCount != 3 {
		t.Errorf("stnA order count = %v, want 3", a.OrderCount)
	}
	b := cellOf["stnB"]
	if b.BestSell != 7 || b.HasBuy {
		t.Errorf("stnB cell wrong: %+v", b)
	}

	// Category filter
	mf, err := c.GetMatrix(ctx, MatrixQuery{Category: "raw", Page: 1, Limit: 50})
	if err != nil {
		t.Fatalf("GetMatrix filtered: %v", err)
	}
	if mf.TotalItems != 2 {
		t.Errorf("filtered total = %d, want 2", mf.TotalItems)
	}
	mnone, err := c.GetMatrix(ctx, MatrixQuery{Category: "module", Page: 1, Limit: 50})
	if err != nil {
		t.Fatalf("GetMatrix none: %v", err)
	}
	if mnone.TotalItems != 0 || len(mnone.Items) != 0 {
		t.Errorf("expected empty matrix for unknown category, got %+v", mnone)
	}
}
```

Add `"math"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestGetMatrix -v`
Expected: FAIL — `c.GetMatrix undefined`, `MatrixQuery`/`MatrixCell`/`MatrixItem`/`Matrix` undefined.

- [ ] **Step 3: Append the Matrix types to `pkg/market/types.go`**

```go
// MatrixQuery parameterizes an items×stations matrix request.
type MatrixQuery struct {
	Category string `json:"category"` // "" = all
	Search   string `json:"search"`   // case-insensitive substring on item_id / item_name
	Page     int    `json:"page"`     // 1-based
	Limit    int    `json:"limit"`    // default 50
}

// MatrixCell is one item×station cell: aggregates over the station's latest capture.
type MatrixCell struct {
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	BestSell    float64   `json:"best_sell"`
	BestBuy     float64   `json:"best_buy"`
	VWAP        float64   `json:"vwap"`       // volume-weighted avg over sell orders
	Volume      float64   `json:"volume"`     // sum of sell quantities
	OrderCount  int       `json:"order_count"`
	CapturedAt  time.Time `json:"captured_at"`
	HasSell     bool      `json:"has_sell"`
	HasBuy      bool      `json:"has_buy"`
}

// MatrixItem is one matrix row: an item across all stations.
type MatrixItem struct {
	ItemID   string       `json:"item_id"`
	ItemName string       `json:"item_name"`
	Category string       `json:"category"`
	Cells    []MatrixCell `json:"cells"` // aligned to Matrix.Stations order
}

// Matrix is a paginated items×stations snapshot.
type Matrix struct {
	Stations    []Station    `json:"stations"`
	Items       []MatrixItem `json:"items"`
	TotalItems  int          `json:"total_items"`
	Page        int          `json:"page"`
	Limit       int          `json:"limit"`
	GeneratedAt time.Time    `json:"generated_at"`
}
```

- [ ] **Step 4: Append `GetMatrix` to `pkg/market/query.go`**

Ensure `"strings"` is imported. Append:

```go
// GetMatrix returns a paginated items×stations matrix. For each matching item and
// each station, the cell aggregates the station's latest capture of that item:
// BestSell = cheapest ask, BestBuy = highest bid, VWAP/Volume over sell orders,
// OrderCount over both sides. Cells for item×station pairs with no orders are
// omitted from MatrixItem.Cells (sparse → rendered as "—" by callers).
func (c *Collector) GetMatrix(ctx context.Context, q MatrixQuery) (*Matrix, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 {
		q.Limit = 50
	}
	offset := (q.Page - 1) * q.Limit
	search := "%" + strings.ToLower(q.Search) + "%"

	// 1. Stations (columns).
	stations, err := c.stations(ctx)
	if err != nil {
		return nil, err
	}

	// 2. Total matching items (before pagination).
	var total int
	countErr := c.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM items
		WHERE (? = '' OR category = ?)
		  AND (? = '%%' OR LOWER(item_id) LIKE ? OR LOWER(item_name) LIKE ?)`,
		q.Category, q.Category, search, search, search).Scan(&total)
	if countErr != nil {
		return nil, fmt.Errorf("count matrix items: %w", countErr)
	}

	m := &Matrix{
		Stations:    stations,
		TotalItems:  total,
		Page:        q.Page,
		Limit:       q.Limit,
		GeneratedAt: time.Now().UTC(),
		Items:       []MatrixItem{},
	}
	if total == 0 || offset >= total {
		return m, nil
	}

	// 3. Paginated item IDs for this page.
	itemRows, err := c.db.QueryContext(ctx, `
		SELECT item_id, item_name, COALESCE(category,'') FROM items
		WHERE (? = '' OR category = ?)
		  AND (? = '%%' OR LOWER(item_id) LIKE ? OR LOWER(item_name) LIKE ?)
		ORDER BY item_id LIMIT ? OFFSET ?`,
		q.Category, q.Category, search, search, search, q.Limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query matrix items: %w", err)
	}
	type itemMeta struct{ id, name, category string }
	var pageItems []itemMeta
	for itemRows.Next() {
		var im itemMeta
		if err := itemRows.Scan(&im.id, &im.name, &im.category); err != nil {
			return nil, fmt.Errorf("scan matrix item: %w", err)
		}
		pageItems = append(pageItems, im)
	}
	if err := itemRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matrix items: %w", err)
	}
	_ = itemRows.Close()

	if len(pageItems) == 0 {
		return m, nil
	}

	// 4. Latest-capture aggregates for these items across all stations.
	ids := make([]any, 0, len(pageItems))
	for _, im := range pageItems {
		ids = append(ids, im.id)
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := append([]any{}, ids...)

	query := `
		SELECT o.station_id, COALESCE(s.station_name, o.station_id),
		       COALESCE(s.system_id, ''), COALESCE(s.system_name, ''),
		       o.item_id,
		       MIN(CASE WHEN o.side = 'sell' THEN o.price_each END) AS best_sell,
		       MAX(CASE WHEN o.side = 'buy'  THEN o.price_each END) AS best_buy,
		       COALESCE(SUM(CASE WHEN o.side = 'sell' THEN o.price_each * o.quantity END), 0) * 1.0 /
		       NULLIF(SUM(CASE WHEN o.side = 'sell' THEN o.quantity END), 0) AS vwap,
		       COALESCE(SUM(CASE WHEN o.side = 'sell' THEN o.quantity END), 0) AS volume,
		       COUNT(*) AS order_count, MAX(o.captured_at) AS captured_at
		FROM market_orders o
		JOIN stations s ON s.station_id = o.station_id
		JOIN (
			SELECT station_id, item_id, MAX(captured_at) AS mx
			FROM market_orders
			WHERE item_id IN (` + placeholders + `)
			GROUP BY station_id, item_id
		) latest ON latest.station_id = o.station_id AND latest.item_id = o.item_id AND latest.mx = o.captured_at
		WHERE o.item_id IN (` + placeholders + `)
		GROUP BY o.station_id, o.item_id`
	args = append(args, ids...)
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query matrix cells: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type cellKey struct{ station, item string }
	cells := make(map[cellKey]MatrixCell)
	for rows.Next() {
		var cc MatrixCell
		var itemID, capStr string
		var bestSell, bestBuy sql.NullFloat64
		var vwap sql.NullFloat64
		if err := rows.Scan(&cc.StationID, &cc.StationName, &cc.SystemID, &cc.SystemName,
			&itemID, &bestSell, &bestBuy, &vwap, &cc.Volume, &cc.OrderCount, &capStr); err != nil {
			return nil, fmt.Errorf("scan matrix cell: %w", err)
		}
		if bestSell.Valid {
			cc.BestSell = bestSell.Float64
			cc.HasSell = true
		}
		if bestBuy.Valid {
			cc.BestBuy = bestBuy.Float64
			cc.HasBuy = true
		}
		if vwap.Valid {
			cc.VWAP = vwap.Float64
		}
		cc.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		cells[cellKey{cc.StationID, itemID}] = cc
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matrix cells: %w", err)
	}

	// 5. Assemble rows aligned to stations order.
	for _, im := range pageItems {
		row := MatrixItem{ItemID: im.id, ItemName: im.name, Category: im.category}
		for _, st := range stations {
			if cc, ok := cells[cellKey{st.StationID, im.id}]; ok {
				row.Cells = append(row.Cells, cc)
			} else {
				row.Cells = append(row.Cells, MatrixCell{StationID: st.StationID, StationName: st.StationName, SystemID: st.SystemID, SystemName: st.SystemName})
			}
		}
		m.Items = append(m.Items, row)
	}
	return m, nil
}

// stations returns all stations ordered by name.
func (c *Collector) stations(ctx context.Context) ([]Station, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT station_id, station_name, system_id, system_name, first_seen_utc, last_updated_utc FROM stations ORDER BY station_name`)
	if err != nil {
		return nil, fmt.Errorf("query stations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Station
	for rows.Next() {
		var s Station
		if err := rows.Scan(&s.StationID, &s.StationName, &s.SystemID, &s.SystemName, &s.FirstSeenUTC, &s.LastUpdatedUTC); err != nil {
			return nil, fmt.Errorf("scan station: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestGetMatrix -v`
Expected: PASS.

- [ ] **Step 6: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/types.go pkg/market/query.go pkg/market/query_test.go
git commit -m "feat(market): add GetMatrix + Matrix types"
```

---

### Task 3: `GetStationOrders` — latest-capture order book for a station×item

**Files:**
- Modify: `pkg/market/query.go` (append method)
- Test: `pkg/market/query_test.go`

**Interfaces:**
- Consumes: `*Collector`, `Order`.
- Produces: `func (c *Collector) GetStationOrders(ctx context.Context, stationID, itemID string) ([]Order, error)` — all orders in the station's latest capture, filtered to `itemID` when non-empty, ordered by side then price. Empty slice when none.

- [ ] **Step 1: Write the failing test**

Append to `pkg/market/query_test.go`:

```go
func TestGetStationOrders(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
		CapturedAt: now,
		Orders: []Order{
			{StationID: "stn1", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 1, CapturedAt: now},
			{StationID: "stn1", ItemID: "iron_ore", Side: "buy", PriceEach: 2, Quantity: 1, CapturedAt: now},
			{StationID: "stn1", ItemID: "copper_ore", Side: "sell", PriceEach: 9, Quantity: 1, CapturedAt: now},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	all, err := c.GetStationOrders(ctx, "stn1", "")
	if err != nil {
		t.Fatalf("GetStationOrders all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all orders = %d, want 3", len(all))
	}
	iron, err := c.GetStationOrders(ctx, "stn1", "iron_ore")
	if err != nil {
		t.Fatalf("GetStationOrders iron: %v", err)
	}
	if len(iron) != 2 {
		t.Fatalf("iron orders = %d, want 2", len(iron))
	}
	none, err := c.GetStationOrders(ctx, "stn-absent", "")
	if err != nil {
		t.Fatalf("GetStationOrders absent: %v", err)
	}
	if none != nil && len(none) != 0 {
		t.Errorf("absent = %v, want empty", none)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestGetStationOrders -v`
Expected: FAIL — `c.GetStationOrders undefined`.

- [ ] **Step 3: Append `GetStationOrders` to `pkg/market/query.go`**

```go
// GetStationOrders returns the orders from a station's latest capture, optionally
// filtered to itemID, ordered by side then price. Returns an empty slice when the
// station has no orders.
func (c *Collector) GetStationOrders(ctx context.Context, stationID, itemID string) ([]Order, error) {
	query := `
		SELECT station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at
		FROM market_orders
		WHERE station_id = ? AND captured_at = (
				SELECT MAX(captured_at) FROM market_orders WHERE station_id = ?
			)` + itemFilter(itemID) + `
		ORDER BY side, price_each`
	rows, err := c.db.QueryContext(ctx, query, stationID, stationID, itemID)
	if err != nil {
		return nil, fmt.Errorf("query station orders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Order
	for rows.Next() {
		var o Order
		var capStr string
		if err := rows.Scan(&o.StationID, &o.ItemID, &o.Side, &o.PriceEach, &o.Quantity, &o.MyQuantity, &o.Source, &capStr); err != nil {
			return nil, fmt.Errorf("scan station order: %w", err)
		}
		o.CapturedAt, _ = time.Parse(time.RFC3339, capStr)
		out = append(out, o)
	}
	return out, rows.Err()
}

// itemFilter returns the SQL fragment narrowing to itemID when non-empty.
// The caller binds itemID as the next parameter in both branches.
func itemFilter(itemID string) string {
	if itemID == "" {
		return ""
	}
	return ` AND item_id = ?`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestGetStationOrders -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/query.go pkg/market/query_test.go
git commit -m "feat(market): add GetStationOrders for cell-detail order book"
```

---

### Task 4: `GetItemPriceHistory` — per-item VWAP series across buckets

**Files:**
- Modify: `pkg/market/types.go` (append `ItemPricePoint`), `pkg/market/query.go` (append method)
- Test: `pkg/market/query_test.go`

**Interfaces:**
- Consumes: `*Collector`, `market_ohlcv` table.
- Produces:
  - `type ItemPricePoint struct { StationID, StationName, Side, BucketUTC string; VWAP, High, Low, Volume float64; TradeCount int }`
  - `func (c *Collector) GetItemPriceHistory(ctx context.Context, itemID string, limit int) ([]ItemPricePoint, error)` — `market_ohlcv` rows for the item joined to station names, newest buckets first, capped at `limit`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/market/query_test.go`:

```go
func TestGetItemPriceHistory(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	t1 := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 21, 11, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{t1, t2} {
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
			CapturedAt: at,
			Orders:     []Order{{StationID: "stn1", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: at}},
		}); err != nil {
			t.Fatalf("WriteSnapshot %v: %v", at, err)
		}
	}

	pts, err := c.GetItemPriceHistory(ctx, "iron_ore", 50)
	if err != nil {
		t.Fatalf("GetItemPriceHistory: %v", err)
	}
	if len(pts) != 2 {
		t.Fatalf("points = %d, want 2", len(pts))
	}
	if pts[0].StationName != "One" || pts[0].Side != "sell" {
		t.Errorf("first point wrong: %+v", pts[0])
	}
	if pts[0].BucketUTC < pts[1].BucketUTC {
		t.Errorf("expected newest-first, got %s before %s", pts[0].BucketUTC, pts[1].BucketUTC)
	}
	absent, err := c.GetItemPriceHistory(ctx, "nope", 50)
	if err != nil {
		t.Fatalf("GetItemPriceHistory absent: %v", err)
	}
	if len(absent) != 0 {
		t.Errorf("absent = %d, want 0", len(absent))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestGetItemPriceHistory -v`
Expected: FAIL — `c.GetItemPriceHistory undefined`, `ItemPricePoint` undefined.

- [ ] **Step 3: Append type + method**

Append to `pkg/market/types.go`:

```go
// ItemPricePoint is one OHLCV bucket for an item at a station.
type ItemPricePoint struct {
	StationID   string  `json:"station_id"`
	StationName string  `json:"station_name"`
	Side        string  `json:"side"`
	BucketUTC   string  `json:"bucket_utc"`
	VWAP        float64 `json:"vwap"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Volume      float64 `json:"volume"`
	TradeCount  int     `json:"trade_count"`
}
```

Append to `pkg/market/query.go`:

```go
// GetItemPriceHistory returns an item's OHLCV buckets across stations, newest
// first. VWAP is the robust series (close is last-order-in-snapshot and can be
// noisy for thin items). Empty slice when the item has no history.
func (c *Collector) GetItemPriceHistory(ctx context.Context, itemID string, limit int) ([]ItemPricePoint, error) {
	if limit < 1 {
		limit = 50
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT o.station_id, COALESCE(s.station_name, o.station_id), o.side, o.bucket_utc,
		       o.vwap, o.high_price, o.low_price, o.volume, o.trade_count
		FROM market_ohlcv o
		JOIN stations s ON s.station_id = o.station_id
		WHERE o.item_id = ?
		ORDER BY o.bucket_utc DESC
		LIMIT ?`, itemID, limit)
	if err != nil {
		return nil, fmt.Errorf("query item price history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ItemPricePoint
	for rows.Next() {
		var p ItemPricePoint
		if err := rows.Scan(&p.StationID, &p.StationName, &p.Side, &p.BucketUTC,
			&p.VWAP, &p.High, &p.Low, &p.Volume, &p.TradeCount); err != nil {
			return nil, fmt.Errorf("scan price point: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestGetItemPriceHistory -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/...
git add pkg/market/types.go pkg/market/query.go pkg/market/query_test.go
git commit -m "feat(market): add GetItemPriceHistory for price-over-time view"
```

---

### Task 5: `GetCaptureHealth` — per-station capture cadence

**Files:**
- Modify: `pkg/market/types.go` (append `StationCaptures`), `pkg/market/query.go` (append method)
- Test: `pkg/market/query_test.go`

**Interfaces:**
- Consumes: `*Collector`, `market_orders`.
- Produces:
  - `type StationCaptures struct { StationID, StationName, SystemID, SystemName string; CaptureTimes []string; Count int; Latest, Earliest string }`
  - `func (c *Collector) GetCaptureHealth(ctx context.Context) ([]StationCaptures, error)` — per station, distinct `captured_at` values (desc) + count + earliest/latest.

- [ ] **Step 1: Write the failing test**

Append to `pkg/market/query_test.go`:

```go
func TestGetCaptureHealth(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	t1 := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 21, 11, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{t1, t2} {
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
			CapturedAt: at,
			Orders:     []Order{{StationID: "stn1", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 1, CapturedAt: at}},
		}); err != nil {
			t.Fatalf("WriteSnapshot %v: %v", at, err)
		}
	}

	health, err := c.GetCaptureHealth(ctx)
	if err != nil {
		t.Fatalf("GetCaptureHealth: %v", err)
	}
	if len(health) != 1 {
		t.Fatalf("stations = %d, want 1", len(health))
	}
	h := health[0]
	if h.StationID != "stn1" || h.Count != 2 {
		t.Errorf("health = %+v, want stn1 count 2", h)
	}
	if h.Latest < h.Earliest {
		t.Errorf("latest %s < earliest %s", h.Latest, h.Earliest)
	}
	if len(h.CaptureTimes) != 2 || h.CaptureTimes[0] < h.CaptureTimes[1] {
		t.Errorf("capture times not newest-first: %v", h.CaptureTimes)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestGetCaptureHealth -v`
Expected: FAIL — `c.GetCaptureHealth undefined`, `StationCaptures` undefined.

- [ ] **Step 3: Append type + method**

Append to `pkg/market/types.go`:

```go
// StationCaptures summarizes one station's capture history for health checks.
type StationCaptures struct {
	StationID    string   `json:"station_id"`
	StationName  string   `json:"station_name"`
	SystemID     string   `json:"system_id"`
	SystemName   string   `json:"system_name"`
	CaptureTimes []string `json:"capture_times"` // distinct captured_at, newest first
	Count        int      `json:"count"`
	Latest       string   `json:"latest"`
	Earliest     string   `json:"earliest"`
}
```

Append to `pkg/market/query.go`:

```go
// GetCaptureHealth returns per-station capture history: distinct captured_at
// timestamps (newest first), count, and earliest/latest. Used to spot cadence
// gaps in collection.
func (c *Collector) GetCaptureHealth(ctx context.Context) ([]StationCaptures, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT s.station_id, s.station_name, s.system_id, s.system_name, o.captured_at
		FROM stations s
		JOIN market_orders o ON o.station_id = s.station_id
		GROUP BY s.station_id, o.captured_at
		ORDER BY s.station_name, o.captured_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query capture health: %w", err)
	}
	defer func() { _ = rows.Close() }()
	order := []string{}
	byStation := map[string]*StationCaptures{}
	times := map[string][]string{}
	for rows.Next() {
		var stID, stName, sysID, sysName, cap string
		if err := rows.Scan(&stID, &stName, &sysID, &sysName, &cap); err != nil {
			return nil, fmt.Errorf("scan capture health: %w", err)
		}
		sc, ok := byStation[stID]
		if !ok {
			sc = &StationCaptures{StationID: stID, StationName: stName, SystemID: sysID, SystemName: sysName, Latest: cap}
			byStation[stID] = sc
			order = append(order, stID)
		}
		sc.Earliest = cap // rows are DESC per station, so last seen is earliest
		sc.Count++
		times[stID] = append(times[stID], cap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate capture health: %w", err)
	}
	out := make([]StationCaptures, 0, len(order))
	for _, stID := range order {
		sc := *byStation[stID]
		sc.CaptureTimes = times[stID]
		out = append(out, sc)
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestGetCaptureHealth -v`
Expected: PASS.

- [ ] **Step 5: Full package test + lint + commit**

```bash
go test ./pkg/market/...
golangci-lint run ./pkg/market/...
git add pkg/market/types.go pkg/market/query.go pkg/market/query_test.go
git commit -m "feat(market): add GetCaptureHealth for capture-cadence view"
```

---

### Task 6: Dashboard skeleton — server, `/api/stats`, `/api/matrix`, embedded placeholder UI

**Files:**
- Create: `cmd/market-dashboard/main.go`
- Create: `cmd/market-dashboard/handlers.go`
- Create: `cmd/market-dashboard/web/index.html` (placeholder)
- Test: `cmd/market-dashboard/handlers_test.go`

**Interfaces:**
- Consumes: `market.Open`, `market.Config`, `market.GetStats`, `market.GetMatrix`, `market.MatrixQuery`.
- Produces: a runnable `cmd/market-dashboard` binary serving `/api/stats`, `/api/matrix`, and `/` (embedded UI), with flags `--addr` (default `:8090`) and `--market-db-path` (default `data/market.db`).

- [ ] **Step 1: Create the placeholder UI**

Create `cmd/market-dashboard/web/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Market Dashboard</title></head>
<body><h1>Market Dashboard</h1><p>Loading…</p></body>
</html>
```

- [ ] **Step 2: Create `cmd/market-dashboard/main.go`**

```go
// Command market-dashboard serves a read-only web dashboard over data/market.db
// for validating market collection. See docs/superpowers/specs/2026-06-22-market-dashboard-design.md.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/rsned/spacemolt/pkg/market"
)

//go:embed all:web
var webFS embed.FS

func main() {
	addr := flag.String("addr", ":8090", "HTTP listen address")
	dbPath := flag.String("market-db-path", "data/market.db", "Path to the market SQLite database")
	flag.Parse()

	collector, err := market.Open(market.Config{DBPath: *dbPath})
	if err != nil {
		log.Fatalf("market-dashboard: open %s: %v", *dbPath, err)
	}
	defer func() { _ = collector.Close() }()

	srv := &server{col: collector}

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("market-dashboard: embed sub: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stats", srv.statsHandler)
	mux.HandleFunc("GET /api/matrix", srv.matrixHandler)
	mux.Handle("GET /", http.FileServer(http.FS(sub)))

	log.Printf("market-dashboard: serving %s on %s", *dbPath, *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("market-dashboard: %v", err)
	}
}

// server holds shared dependencies for HTTP handlers.
type server struct {
	col *market.Collector
}

// writeJSON encodes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ensure the imports used by handlers.go are referenced here too.
var _ = fmt.Sprint
```

- [ ] **Step 3: Create `cmd/market-dashboard/handlers.go`**

```go
package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/rsned/spacemolt/pkg/market"
)

func (s *server) statsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := s.col.GetStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *server) matrixHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	mq := market.MatrixQuery{
		Category: q.Get("category"),
		Search:   q.Get("q"),
		Page:     page,
		Limit:    limit,
	}
	m, err := s.col.GetMatrix(r.Context(), mq)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, m)
}
```

- [ ] **Step 4: Write the handler test**

Create `cmd/market-dashboard/handlers_test.go`:

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
)

func newTestServer(t *testing.T) (*server, *market.Collector) {
	t.Helper()
	c, err := market.Open(market.Config{DBPath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &server{col: c}, c
}

func TestStatsHandler(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	srv.statsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var stats market.Stats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stats.StationCount != 0 {
		t.Errorf("station_count = %d, want 0", stats.StationCount)
	}
}

func TestMatrixHandlerEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/matrix", nil)
	rec := httptest.NewRecorder()
	srv.matrixHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var m market.Matrix
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.TotalItems != 0 || len(m.Items) != 0 {
		t.Errorf("expected empty matrix, got %+v", m)
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/market-dashboard/... -v`
Expected: PASS (skeleton builds; stats/matrix handlers return JSON).

- [ ] **Step 6: Build, lint, commit**

```bash
go build ./...
golangci-lint run ./cmd/market-dashboard/...
git add cmd/market-dashboard/
git commit -m "feat(market-dashboard): server skeleton with /api/stats and /api/matrix"
```

---

### Task 7: Remaining handlers — `/api/station/{id}/orders`, `/api/item/{id}/history`, `/api/captures`

**Files:**
- Modify: `cmd/market-dashboard/main.go` (register routes)
- Modify: `cmd/market-dashboard/handlers.go` (three handlers)
- Test: `cmd/market-dashboard/handlers_test.go`

**Interfaces:**
- Consumes: `market.GetStationOrders`, `market.GetItemPriceHistory`, `market.GetCaptureHealth`.
- Produces: the three remaining JSON endpoints.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/market-dashboard/handlers_test.go`:

```go
func TestStationOrdersHandler_Absent(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/station/nope/orders", nil)
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	srv.stationOrdersHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var orders []market.Order
	if err := json.Unmarshal(rec.Body.Bytes(), &orders); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(orders) != 0 {
		t.Errorf("orders = %d, want 0", len(orders))
	}
}

func TestItemHistoryHandler_Absent(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/item/nope/history", nil)
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	srv.itemHistoryHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var pts []market.ItemPricePoint
	if err := json.Unmarshal(rec.Body.Bytes(), &pts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pts) != 0 {
		t.Errorf("points = %d, want 0", len(pts))
	}
}

func TestCapturesHandlerEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/captures", nil)
	rec := httptest.NewRecorder()
	srv.capturesHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var health []market.StationCaptures
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(health) != 0 {
		t.Errorf("health = %d, want 0", len(health))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/market-dashboard/... -run 'StationOrders|ItemHistory|Captures' -v`
Expected: FAIL — `srv.stationOrdersHandler` / `itemHistoryHandler` / `capturesHandler` undefined.

- [ ] **Step 3: Add the three handlers to `handlers.go`**

```go
func (s *server) stationOrdersHandler(w http.ResponseWriter, r *http.Request) {
	stationID := r.PathValue("id")
	if stationID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing station id"})
		return
	}
	orders, err := s.col.GetStationOrders(r.Context(), stationID, r.URL.Query().Get("item"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if orders == nil {
		orders = []market.Order{}
	}
	writeJSON(w, http.StatusOK, orders)
}

func (s *server) itemHistoryHandler(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	if itemID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing item id"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	pts, err := s.col.GetItemPriceHistory(r.Context(), itemID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if pts == nil {
		pts = []market.ItemPricePoint{}
	}
	writeJSON(w, http.StatusOK, pts)
}

func (s *server) capturesHandler(w http.ResponseWriter, r *http.Request) {
	health, err := s.col.GetCaptureHealth(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if health == nil {
		health = []market.StationCaptures{}
	}
	writeJSON(w, http.StatusOK, health)
}
```

- [ ] **Step 4: Register the routes in `main.go`**

Add these three lines to the mux setup in `main.go` (after the matrix route):

```go
	mux.HandleFunc("GET /api/station/{id}/orders", srv.stationOrdersHandler)
	mux.HandleFunc("GET /api/item/{id}/history", srv.itemHistoryHandler)
	mux.HandleFunc("GET /api/captures", srv.capturesHandler)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./cmd/market-dashboard/... -v`
Expected: PASS (all handler tests).

- [ ] **Step 6: Build, lint, commit**

```bash
go build ./...
golangci-lint run ./cmd/market-dashboard/...
git add cmd/market-dashboard/
git commit -m "feat(market-dashboard): add station-orders, item-history, captures handlers"
```

---

### Task 8: Embedded UI — 4 views (matrix, cell-detail, price-over-time, capture-health)

**Files:**
- Modify: `cmd/market-dashboard/web/index.html` (replace placeholder)
- Create: `cmd/market-dashboard/web/app.js`
- Create: `cmd/market-dashboard/web/style.css`

**Interfaces:**
- Consumes: the five JSON endpoints (`/api/stats`, `/api/matrix`, `/api/station/{id}/orders`, `/api/item/{id}/history`, `/api/captures`).

- [ ] **Step 1: Replace `web/index.html`**

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Market Dashboard</title>
<link rel="stylesheet" href="/style.css">
</head>
<body>
<header>
  <h1>Market Dashboard</h1>
  <div id="stats" class="stats"></div>
  <button id="refresh" type="button">Refresh</button>
</header>
<nav class="tabs">
  <button data-view="matrix" class="tab active" type="button">Matrix</button>
  <button data-view="price" class="tab" type="button">Price over time</button>
  <button data-view="health" class="tab" type="button">Capture health</button>
</nav>
<div id="matrix-controls" class="controls">
  <select id="category"><option value="">All categories</option></select>
  <input id="search" type="search" placeholder="search item id / name">
  <span id="pager"></span>
</div>
<div id="price-controls" class="controls hidden">
  <input id="price-item" type="search" placeholder="item id, e.g. iron_ore">
  <button id="price-go" type="button">Show</button>
</div>
<main id="app"></main>
<dialog id="cell-dialog"><button id="cell-close" type="button">close</button><div id="cell-body"></div></dialog>
<script src="/app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Create `web/style.css`**

```css
* { box-sizing: border-box; }
body { margin: 0; font: 14px/1.4 system-ui, sans-serif; color: #222; background: #fafafa; }
header { display: flex; align-items: center; gap: 1rem; padding: .6rem 1rem; background: #1f2937; color: #fff; }
header h1 { font-size: 1.1rem; margin: 0; }
.stats { color: #cbd5e1; font-size: .85rem; }
button { cursor: pointer; }
.tabs { display: flex; gap: .25rem; padding: .25rem 1rem; background: #e5e7eb; }
.tab { border: 0; background: transparent; padding: .35rem .7rem; border-radius: 4px 4px 0 0; }
.tab.active { background: #fff; font-weight: 600; }
.controls { padding: .5rem 1rem; display: flex; gap: .5rem; align-items: center; }
.hidden { display: none; }
table { border-collapse: collapse; width: 100%; font-size: .82rem; }
th, td { border: 1px solid #ddd; padding: .25rem .4rem; text-align: right; white-space: nowrap; }
th { background: #f3f4f6; position: sticky; top: 0; }
td.item { text-align: left; font-family: ui-monospace, monospace; }
td.sparse { color: #bbb; }
.sell { color: #047857; }
.buy { color: #b45309; }
main { padding: 0 1rem 2rem; }
dialog { width: min(640px, 90vw); }
#error { color: #b91c1c; padding: 1rem; }
```

- [ ] **Step 3: Create `web/app.js`**

```js
const app = document.getElementById('app');
const statsEl = document.getElementById('stats');
let view = 'matrix';

const fmt = (n) => (n == null || Number.isNaN(n)) ? '—' : Number(n).toLocaleString(undefined, { maximumFractionDigits: 2 });
function relTime(ts) {
  if (!ts) return '—';
  const d = (Date.now() - new Date(ts).getTime()) / 1000;
  if (d < 60) return 'just now';
  if (d < 3600) return Math.floor(d / 60) + ' min ago';
  if (d < 86400) return Math.floor(d / 3600) + ' hr ago';
  if (d < 604800) return Math.floor(d / 86400) + ' days ago';
  return ts.slice(0, 10);
}
async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(res.status + ' ' + (await res.text()));
  return res.json();
}
function showError(e) { app.innerHTML = '<div id="error">Error: ' + (e.message || e) + '</div>'; }

async function loadStats() {
  try {
    const s = await getJSON('/api/stats');
    statsEl.textContent =
      `stations ${s.station_count} · items ${s.item_count} · orders ${s.order_count} · ohlcv ${s.ohlcv_count} · latest ${s.latest_capture || '—'}`;
  } catch (e) { /* stats are best-effort */ }
}

async function loadCategories() {
  const sel = document.getElementById('category');
  // Derive from the first matrix page's items (categories self-populate as captures land).
  // Lightweight: just fetch a wide matrix and collect distinct categories.
  try {
    const m = await getJSON('/api/matrix?page=1&limit=500');
    const cats = [...new Set(m.items.map(i => i.category).filter(Boolean))].sort();
    cats.forEach(c => { const o = document.createElement('option'); o.value = c; o.textContent = c; sel.appendChild(o); });
  } catch (e) { /* ignore */ }
}

async function renderMatrix() {
  const cat = document.getElementById('category').value;
  const q = document.getElementById('search').value;
  const params = new URLSearchParams({ category: cat, q, page: '1', limit: '50' });
  try {
    const m = await getJSON('/api/matrix?' + params);
    if (!m.items.length) { app.innerHTML = '<p>No items match.</p>'; renderPager(0, 0, 50); return; }
    const cols = ['<th>item</th>'].concat(m.stations.map(s => `<th>${s.station_name}</th>`)).join('');
    const rows = m.items.map(it =>
      `<tr><td class="item">${it.item_id}<br><small>${it.item_name || ''} · ${it.category || ''}</small></td>` +
      it.cells.map(c => `<td class="cell" data-item="${it.item_id}" data-station="${c.station_id}">${
        (c.has_sell || c.has_buy)
          ? `${c.has_sell ? '<span class="sell">'+fmt(c.best_sell)+'</span>' : ''} ${c.has_buy ? '<span class="buy">'+fmt(c.best_buy)+'</span>' : ''}<br>${c.has_sell?'vol '+fmt(c.volume):''} · ${relTime(c.captured_at)}`
          : '<span class="sparse">—</span>'
      }</td>`).join('')).join('');
    app.innerHTML = `<table><thead><tr>${cols}</tr></thead><tbody>${rows}</tbody></table>`;
    renderPager(m.page, m.total_items, m.limit);
    app.querySelectorAll('td.cell').forEach(td => {
      td.addEventListener('click', () => openCell(td.dataset.item, td.dataset.station));
    });
  } catch (e) { showError(e); }
}

function renderPager(page, total, limit) {
  const pages = Math.ceil(total / limit);
  document.getElementById('pager').textContent = pages > 1 ? `page ${page}/${pages} · ${total} items` : `${total} items`;
}

async function openCell(item, station) {
  const body = document.getElementById('cell-body');
  body.textContent = 'Loading…';
  document.getElementById('cell-dialog').showModal();
  try {
    const orders = await getJSON(`/api/station/${encodeURIComponent(station)}/orders?item=${encodeURIComponent(item)}`);
    if (!orders.length) { body.innerHTML = '<p>No orders.</p>'; return; }
    const rows = orders.map(o =>
      `<tr><td>${o.side}</td><td>${fmt(o.price_each)}</td><td>${fmt(o.quantity)}</td><td>${o.source || ''}</td></tr>`).join('');
    body.innerHTML = `<h3>${item} @ ${station}</h3><table><thead><tr><th>side</th><th>price</th><th>qty</th><th>source</th></tr></thead><tbody>${rows}</tbody></table>`;
  } catch (e) { body.innerHTML = '<p id="error">' + (e.message || e) + '</p>'; }
}

async function renderPrice() {
  const item = document.getElementById('price-item').value.trim();
  if (!item) { app.innerHTML = '<p>Enter an item id above (e.g. iron_ore).</p>'; return; }
  try {
    const pts = await getJSON(`/api/item/${encodeURIComponent(item)}/history?limit=100`);
    if (!pts.length) { app.innerHTML = `<p>No history for ${item}.</p>`; return; }
    const rows = pts.map(p =>
      `<tr><td>${p.bucket_utc}</td><td>${p.station_name}</td><td>${p.side}</td><td class="sell">${fmt(p.vwap)}</td><td>${fmt(p.high)}</td><td>${fmt(p.low)}</td><td>${fmt(p.volume)}</td><td>${p.trade_count}</td></tr>`).join('');
    app.innerHTML = `<h3>${item} — VWAP over time</h3>
      <table><thead><tr><th>bucket</th><th>station</th><th>side</th><th>vwap</th><th>high</th><th>low</th><th>vol</th><th>trades</th></tr></thead><tbody>${rows}</tbody></table>
      <p><small>VWAP is the robust series; high/low reveal thin-item noise.</small></p>`;
  } catch (e) { showError(e); }
}

async function renderHealth() {
  try {
    const health = await getJSON('/api/captures');
    if (!health.length) { app.innerHTML = '<p>No captures yet.</p>'; return; }
    const blocks = health.map(h =>
      `<h3>${h.station_name} <small>(${h.count} captures, ${h.earliest} → ${h.latest})</small></h3>
       <ul>${h.capture_times.map(t => `<li>${t}</li>`).join('')}</ul>`).join('');
    app.innerHTML = blocks;
  } catch (e) { showError(e); }
}

function showView(v) {
  view = v;
  document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.view === v));
  document.getElementById('matrix-controls').classList.toggle('hidden', v !== 'matrix');
  document.getElementById('price-controls').classList.toggle('hidden', v !== 'price');
  if (v === 'matrix') renderMatrix();
  else if (v === 'price') renderPrice();
  else if (v === 'health') renderHealth();
}

document.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => showView(t.dataset.view)));
document.getElementById('refresh').addEventListener('click', () => { loadStats(); showView(view); });
document.getElementById('category').addEventListener('change', renderMatrix);
document.getElementById('search').addEventListener('input', debounce(renderMatrix, 300));
document.getElementById('price-go').addEventListener('click', renderPrice);
document.getElementById('price-item').addEventListener('keydown', e => { if (e.key === 'Enter') renderPrice(); });
document.getElementById('cell-close').addEventListener('click', () => document.getElementById('cell-dialog').close());

function debounce(fn, ms) { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; }

showView('matrix');
loadStats();
loadCategories();
```

- [ ] **Step 4: Build and smoke-compile the binary**

Run: `go build -o bin/market-dashboard ./cmd/market-dashboard`
Expected: builds with the embedded assets. (`bin/` per project rule for built binaries.)

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./cmd/market-dashboard/...
git add cmd/market-dashboard/web/
git commit -m "feat(market-dashboard): embedded 4-view vanilla-JS UI"
```

---

### Task 9: Final verification

**Files:** none (verification only).

- [ ] **Step 1: Whole-tree build + test + lint**

Run:
```bash
go build ./...
go test ./...
golangci-lint run ./...
```
Expected: all PASS, no new lint findings.

- [ ] **Step 2: Manual smoke against the real DB**

Run:
```bash
go build -o bin/market-dashboard ./cmd/market-dashboard
./bin/market-dashboard --market-db-path /home/robert/spacemolt/spacemolt/data/market.db --addr :8090
```
Then open `http://localhost:8090` and confirm:
- Stats header shows ~4 stations / ~557 items / ~20k orders / latest capture.
- Matrix renders items × 4 stations; ore rows (iron_ore, copper_ore) show sell/buy/volume/freshness; sparse cells show `—`.
- Clicking a populated cell opens the order-book dialog.
- "Price over time" for `iron_ore` shows per-bucket VWAP rows.
- "Capture health" shows the irregular per-station capture timestamps (2–6 each).
- Category `<select>` populates as categories self-heal (may be sparse until re-captures land after Task 1).

- [ ] **Step 3: Commit any smoke-driven fixes, then final commit**

```bash
git add -A
git commit -m "test(market-dashboard): end-to-end smoke verification"
```

---

## Self-Review

**Spec coverage:**
- Category-capture fix → Task 1. ✓
- `GetMatrix` / `GetStationOrders` / `GetItemPriceHistory` / `GetCaptureHealth` → Tasks 2–5. ✓
- `cmd/market-dashboard` server + 5 endpoints → Tasks 6–7. ✓
- Embedded vanilla-JS UI, 4 views → Task 8. ✓
- Cell semantics (best sell/buy, VWAP/volume over sell, order count, freshness, sparse `—`) → Task 2 SQL + Task 8 rendering. ✓
- Real category filter (post-fix) → Task 1 fix + Task 8 dropdown. ✓
- Capture-health view → Task 5 + Task 8. ✓
- Price-over-time (VWAP robust series, thin-item caveat visible) → Task 4 + Task 8. ✓
- Error handling (empty arrays / nil-safe; `items: []` for no-match) → Tasks 6–7. ✓
- Testing (temp-DB unit tests + httptest) → every task. ✓
- Coordination: sync-to-head gate → Pre-flight. ✓

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to" — every code step carries complete code.

**Type consistency:** Method names match across producers (Tasks 2–5) and consumers (Tasks 6–8): `GetMatrix`/`MatrixQuery`, `GetStationOrders`, `GetItemPriceHistory`/`ItemPricePoint`, `GetCaptureHealth`/`StationCaptures`. Field names (`best_sell`, `has_buy`, `bucket_utc`, `capture_times`) consistent between Go JSON tags and JS. `Order.Category` added in Task 1 is used by the Task 1 test and exists for later capture paths.

**Note on intermediate state:** Each task compiles and its tests pass independently. The dashboard tasks (6–8) depend on the read methods (2–5) being present; execute in order.
