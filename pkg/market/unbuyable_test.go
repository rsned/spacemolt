package market

import (
	"context"
	"testing"
	"time"
)

// seedArbBook lays down the minimum a scan needs to mint a row for item: an ask at
// one station and a fatter bid at another. Written through WriteSnapshot so the
// stations rows GetItemStationPrices joins against exist, exactly as a live capture
// would create them.
func seedArbBook(t *testing.T, c *Collector, item string, ask, bid float64) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	write := func(stn string, o Order) {
		t.Helper()
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: stn, StationName: stn, SystemID: "sys-" + stn, SystemName: "sys-" + stn,
			CapturedAt: now, Orders: []Order{o},
		}); err != nil {
			t.Fatalf("WriteSnapshot %s: %v", stn, err)
		}
	}
	// Per-item station ids: a snapshot REPLACES that station's book, so two items
	// sharing one station would erase each other's orders.
	src, dst := "src-"+item, "dst-"+item
	write(src, Order{StationID: src, ItemID: item, Side: "sell", PriceEach: ask, Quantity: 100, CapturedAt: now})
	write(dst, Order{StationID: dst, ItemID: item, Side: "buy", PriceEach: bid, Quantity: 100, CapturedAt: now})
}

// TestScanSkipsUnbuyableItems is the load-bearing test: the scanner expires and
// rebuilds the entire pool every cycle, so a blocked item must be excluded at
// GENERATION. Expiring its rows cannot hold — the next scan would mint them again.
func TestScanSkipsUnbuyableItems(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	seedArbBook(t, c, "reactive_armor_hardener", 590, 17996)
	seedArbBook(t, c, "iron_ore", 100, 900)

	before, err := c.ScanArbitrage(ctx, ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if before.Inserted < 2 {
		t.Fatalf("both items should produce rows before the block, got %d", before.Inserted)
	}

	if err := c.MarkUnbuyable(ctx, "reactive_armor_hardener", "t", "invalid_item"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if _, err := c.ScanArbitrage(ctx, ScanOptions{}); err != nil {
		t.Fatalf("rescan: %v", err)
	}

	var blocked, kept int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM arbitrage_opportunities
		WHERE item_id='reactive_armor_hardener' AND status='available'`).Scan(&blocked); err != nil {
		t.Fatalf("count blocked: %v", err)
	}
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM arbitrage_opportunities
		WHERE item_id='iron_ore' AND status='available'`).Scan(&kept); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if blocked != 0 {
		t.Fatalf("a blocked item must generate no rows, got %d", blocked)
	}
	if kept == 0 {
		t.Fatal("blocking one item must not starve the rest of the pool")
	}
}

// TestMarkUnbuyableExpiresRowsAlreadyInFlight covers the cleanup half: rows minted
// before the block are retired immediately rather than waiting for the next cycle.
func TestMarkUnbuyableExpiresRowsAlreadyInFlight(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	seedArbBook(t, c, "reactive_armor_hardener", 590, 17996)
	if _, err := c.ScanArbitrage(ctx, ScanOptions{}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := c.MarkUnbuyable(ctx, "reactive_armor_hardener", "t", "invalid_item"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	var open int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM arbitrage_opportunities
		WHERE item_id='reactive_armor_hardener' AND status='available'`).Scan(&open); err != nil {
		t.Fatalf("count: %v", err)
	}
	if open != 0 {
		t.Fatalf("marking unbuyable must retire the rows already minted, got %d open", open)
	}
}

// TestUnbuyableBlockLapses proves the block is self-healing rather than a permanent
// blind spot: if the server later accepts the item, the fleet re-tests it.
func TestUnbuyableBlockLapses(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	if err := c.MarkUnbuyable(ctx, "reactive_armor_hardener", "t", "invalid_item"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if _, err := c.db.Exec(`UPDATE unbuyable_items SET blocked_until=? WHERE item_id=?`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), "reactive_armor_hardener"); err != nil {
		t.Fatalf("age the block: %v", err)
	}
	blocked, err := c.UnbuyableItems(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if blocked["reactive_armor_hardener"] {
		t.Fatal("a lapsed block must stop filtering the item")
	}
}

// TestMarkUnbuyableCountsRepeatReports keeps the table useful as a diagnosis record:
// the item costing the fleet the most is the one reported the most.
func TestMarkUnbuyableCountsRepeatReports(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	for range 3 {
		if err := c.MarkUnbuyable(ctx, "reactive_armor_hardener", "t", "invalid_item"); err != nil {
			t.Fatalf("mark: %v", err)
		}
	}
	var hits int
	if err := c.db.QueryRow(`SELECT hits FROM unbuyable_items WHERE item_id=?`,
		"reactive_armor_hardener").Scan(&hits); err != nil {
		t.Fatalf("read hits: %v", err)
	}
	if hits != 3 {
		t.Fatalf("hits = %d, want 3", hits)
	}
}

// TestScanRespectsBlockWithExplicitItemList covers the allow-list branch: naming an
// item explicitly is a caller's choice of what to look at, not evidence the market
// will honour it.
func TestScanRespectsBlockWithExplicitItemList(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	seedArbBook(t, c, "reactive_armor_hardener", 590, 17996)
	if err := c.MarkUnbuyable(ctx, "reactive_armor_hardener", "t", "invalid_item"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	res, err := c.ScanArbitrage(ctx, ScanOptions{Items: []string{"reactive_armor_hardener"}})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Inserted != 0 {
		t.Fatalf("an explicitly named blocked item must still be skipped, got %d rows", res.Inserted)
	}
}
