package market

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestGetStats_Empty(t *testing.T) {
	collector, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = collector.Close() })

	stats, err := collector.GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.StationCount != 0 || stats.ItemCount != 0 || stats.OrderCount != 0 || stats.OHLCVCount != 0 {
		t.Errorf("empty DB stats nonzero: %+v", stats)
	}
	if stats.LatestCapture != "" {
		t.Errorf("LatestCapture = %q, want empty", stats.LatestCapture)
	}
}

func TestGetStats_AfterWrite(t *testing.T) {
	collector, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = collector.Close() })

	now := time.Now().UTC()
	snap := MarketSnapshot{
		StationID: "stn1", StationName: "Station One",
		SystemID: "sys1", SystemName: "System One",
		CapturedAt: now,
		Orders: []Order{
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron Ore", Side: "buy", PriceEach: 100, Quantity: 10, CapturedAt: now},
		},
	}
	if err := collector.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("WriteSnapshot failed: %v", err)
	}

	stats, err := collector.GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.StationCount != 1 || stats.ItemCount != 1 || stats.OrderCount != 1 {
		t.Errorf("stats after write = %+v, want station=1 item=1 order=1", stats)
	}
	if stats.LatestCapture == "" {
		t.Errorf("LatestCapture empty after write")
	}

	orders, err := collector.GetLatestOrders(context.Background(), "stn1", 10)
	if err != nil {
		t.Fatalf("GetLatestOrders failed: %v", err)
	}
	if len(orders) != 1 || orders[0].ItemID != "iron" {
		t.Errorf("GetLatestOrders = %+v, want 1 iron order", orders)
	}
}

// TestGetReferenceAsk verifies the live-floor price signal: best ask excludes the
// not-for-sale sentinel, depth/stations aggregate tradeable rungs across stations'
// latest captures, and a sentinel-only item reports no reference.
func TestGetReferenceAsk(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	now := time.Now().UTC()

	// Two stations sell iron; one ladder includes a sentinel rung that must be
	// ignored. Station A floor = 2 (qty 50), station B floor = 3 (qty 20).
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", SystemID: "sys1", CapturedAt: now,
		Orders: []Order{
			{StationID: "stnA", ItemID: "iron", Side: "sell", PriceEach: 2, Quantity: 50, CapturedAt: now},
			{StationID: "stnA", ItemID: "iron", Side: "sell", PriceEach: NotForSalePrice, Quantity: 9999, CapturedAt: now},
			{StationID: "stnA", ItemID: "junk", Side: "sell", PriceEach: NotForSalePrice, Quantity: 100, CapturedAt: now},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnA: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", SystemID: "sys1", CapturedAt: now,
		Orders: []Order{
			{StationID: "stnB", ItemID: "iron", Side: "sell", PriceEach: 3, Quantity: 20, CapturedAt: now},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnB: %v", err)
	}

	ra, ok, err := c.GetReferenceAsk(ctx, "iron")
	if err != nil {
		t.Fatalf("GetReferenceAsk iron: %v", err)
	}
	if !ok {
		t.Fatal("GetReferenceAsk iron: ok=false, want a reference")
	}
	if ra.BestAsk != 2 {
		t.Errorf("BestAsk = %g, want 2 (sentinel excluded)", ra.BestAsk)
	}
	if ra.Depth != 70 {
		t.Errorf("Depth = %g, want 70 (50+20, sentinel excluded)", ra.Depth)
	}
	if ra.Stations != 2 {
		t.Errorf("Stations = %d, want 2", ra.Stations)
	}
	if ra.AtAsk != 50 {
		t.Errorf("AtAsk = %g, want 50 (qty at best ask 2)", ra.AtAsk)
	}

	// junk is listed only at the sentinel → no tradeable reference.
	if _, ok, err := c.GetReferenceAsk(ctx, "junk"); err != nil {
		t.Fatalf("GetReferenceAsk junk: %v", err)
	} else if ok {
		t.Error("GetReferenceAsk junk: ok=true, want false (sentinel-only item)")
	}
}

// TestGetStats_MultiWriteCount pins the cheap GetStats path: OrderCount tracks total
// orders captured across multiple snapshots (via the AUTOINCREMENT high-water mark, not a
// COUNT(*) scan), and LatestCapture reflects the most recent capture.
func TestGetStats_MultiWriteCount(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()

	older := time.Now().UTC().Add(-2 * time.Hour)
	newer := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", SystemID: "sys1", CapturedAt: older,
		Orders: []Order{
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron", Side: "buy", PriceEach: 5, Quantity: 10, CapturedAt: older},
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron", Side: "sell", PriceEach: 7, Quantity: 10, CapturedAt: older},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot older: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", SystemID: "sys1", CapturedAt: newer,
		Orders: []Order{
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron", Side: "sell", PriceEach: 9, Quantity: 12, CapturedAt: newer},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot newer: %v", err)
	}

	stats, err := c.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.OrderCount != 3 {
		t.Errorf("OrderCount = %d, want 3 (total captured across both writes)", stats.OrderCount)
	}
	if stats.LatestCapture == "" {
		t.Errorf("LatestCapture empty, want the newer bucket")
	}
}

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
	if snap.Orders[0].ItemName != "Iron" {
		t.Errorf("expected ItemName 'Iron', got %q", snap.Orders[0].ItemName)
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

func TestFindBestPrices_BuySideRanksDescending(t *testing.T) {
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
			Orders:     []Order{{StationID: stn, ItemID: "iron", ItemName: "Iron", Side: "buy", PriceEach: price, Quantity: 10, CapturedAt: now}},
		}); err != nil {
			t.Fatalf("WriteSnapshot %s: %v", stn, err)
		}
	}
	write("stnA", 9)
	write("stnB", 4)
	write("stnC", 7)

	best, err := c.FindBestPrices(ctx, "iron", "buy", 2)
	if err != nil {
		t.Fatalf("FindBestPrices failed: %v", err)
	}
	if len(best) != 2 {
		t.Fatalf("expected 2 results, got %d", len(best))
	}
	if best[0].StationID != "stnA" || best[0].Price != 9 {
		t.Errorf("highest buy should be stnA@9, got %+v", best[0])
	}
	if best[0].ListingType != "buy" || best[0].ItemID != "iron" {
		t.Errorf("metadata not populated: %+v", best[0])
	}
}

func TestFindBestPrices_UsesLatestCapturePerStation(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	older := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)

	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnX", StationName: "X", SystemID: "sys", SystemName: "S",
		CapturedAt: older,
		Orders:     []Order{{StationID: "stnX", ItemID: "iron", ItemName: "Iron", Side: "sell", PriceEach: 3, Quantity: 10, CapturedAt: older}},
	}); err != nil {
		t.Fatalf("WriteSnapshot older: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnX", StationName: "X", SystemID: "sys", SystemName: "S",
		CapturedAt: newer,
		Orders:     []Order{{StationID: "stnX", ItemID: "iron", ItemName: "Iron", Side: "sell", PriceEach: 8, Quantity: 10, CapturedAt: newer}},
	}); err != nil {
		t.Fatalf("WriteSnapshot newer: %v", err)
	}

	best, err := c.FindBestPrices(ctx, "iron", "sell", 5)
	if err != nil {
		t.Fatalf("FindBestPrices failed: %v", err)
	}
	if len(best) != 1 {
		t.Fatalf("expected 1 result (dedup by latest capture), got %d", len(best))
	}
	if best[0].Price != 8 {
		t.Errorf("should use latest capture price 8, got %f", best[0].Price)
	}
}

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
	// One book per (station, capture instant): view_market returns a station's
	// whole market in a single unpaged response, and WriteSnapshot now REPLACES
	// the book at a given captured_at rather than appending to it. Writing the
	// two items as two same-instant snapshots would drop the first.
	write("stnB", "sysB", []Order{
		{StationID: "stnB", ItemID: "iron_ore", ItemName: "Iron Ore", Category: "raw", Side: "sell", PriceEach: 7, Quantity: 4, CapturedAt: now},
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

func TestFindItemSellers(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	now := time.Now().UTC()
	write := func(stn, sys string, orders []Order) {
		for i := range orders {
			orders[i].StationID = stn
			orders[i].CapturedAt = now
		}
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: stn, StationName: stn + " Station", SystemID: sys, SystemName: sys,
			CapturedAt: now, Orders: orders,
		}); err != nil {
			t.Fatalf("WriteSnapshot %s: %v", stn, err)
		}
	}
	// stnA: two sell orders (depth 30, best 5) + a buy order that must be ignored.
	write("stnA", "sysA", []Order{
		{ItemID: "iron", Side: "sell", PriceEach: 5, Quantity: 10},
		{ItemID: "iron", Side: "sell", PriceEach: 8, Quantity: 20},
		{ItemID: "iron", Side: "buy", PriceEach: 3, Quantity: 100},
	})
	// stnB: cheap but shallow (depth 4).
	write("stnB", "sysB", []Order{{ItemID: "iron", Side: "sell", PriceEach: 2, Quantity: 4}})
	// stnC: sells a different item only.
	write("stnC", "sysC", []Order{{ItemID: "gold", Side: "sell", PriceEach: 50, Quantity: 5}})

	sellers, err := c.FindItemSellers(ctx, "iron", 0)
	if err != nil {
		t.Fatalf("FindItemSellers: %v", err)
	}
	if len(sellers) != 2 {
		t.Fatalf("expected 2 selling stations, got %d: %+v", len(sellers), sellers)
	}
	if sellers[0].StationID != "stnB" || sellers[0].BestPrice != 2 {
		t.Errorf("cheapest first: want stnB@2, got %+v", sellers[0])
	}
	a := sellers[1]
	if a.StationID != "stnA" || a.BestPrice != 5 || a.TotalQty != 30 || a.Orders != 2 {
		t.Errorf("stnA aggregate wrong (want best=5 depth=30 orders=2): %+v", a)
	}
	if a.SystemID != "sysA" || a.StationName != "stnA Station" {
		t.Errorf("station metadata not joined: %+v", a)
	}

	// Quantity floor excludes the shallow station.
	deep, err := c.FindItemSellers(ctx, "iron", 10)
	if err != nil {
		t.Fatalf("FindItemSellers(minQty=10): %v", err)
	}
	if len(deep) != 1 || deep[0].StationID != "stnA" {
		t.Errorf("minQty=10 must leave only stnA, got %+v", deep)
	}
}

func TestFindItemSellers_UsesLatestCapturePerStation(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	old := time.Now().UTC().Add(-2 * time.Hour)
	now := time.Now().UTC()
	snap := func(ts time.Time, price, qty float64) MarketSnapshot {
		return MarketSnapshot{
			StationID: "stn", StationName: "Stn", SystemID: "sys", SystemName: "Sys",
			CapturedAt: ts,
			Orders:     []Order{{StationID: "stn", ItemID: "iron", Side: "sell", PriceEach: price, Quantity: qty, CapturedAt: ts}},
		}
	}
	if err := c.WriteSnapshot(ctx, snap(old, 1, 999)); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteSnapshot(ctx, snap(now, 7, 12)); err != nil {
		t.Fatal(err)
	}

	sellers, err := c.FindItemSellers(ctx, "iron", 0)
	if err != nil {
		t.Fatalf("FindItemSellers: %v", err)
	}
	if len(sellers) != 1 {
		t.Fatalf("one station must yield one row, got %d", len(sellers))
	}
	if sellers[0].BestPrice != 7 || sellers[0].TotalQty != 12 {
		t.Errorf("must use latest capture only (7@12), got %+v", sellers[0])
	}
}

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
	if len(none) != 0 {
		t.Errorf("absent = %v, want empty", none)
	}
}

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

func TestGetCaptureHealth(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	// GetCaptureHealth reports the current capture period only (latest hourly bucket), so
	// the dashboard never scans the full order history. Seed an older bucket (10:00) that
	// must be EXCLUDED, plus two captures in the latest bucket (11:00, 11:30) that the
	// report must return newest-first.
	older := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 6, 21, 11, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 21, 11, 30, 0, 0, time.UTC)
	for _, at := range []time.Time{older, t1, t2} {
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
	// Only the latest bucket's two captures count; the 10:00 capture is excluded.
	if h.StationID != "stn1" || h.Count != 2 {
		t.Errorf("health = %+v, want stn1 count 2 (latest bucket only)", h)
	}
	if h.Latest < h.Earliest {
		t.Errorf("latest %s < earliest %s", h.Latest, h.Earliest)
	}
	if len(h.CaptureTimes) != 2 || h.CaptureTimes[0] < h.CaptureTimes[1] {
		t.Errorf("capture times not newest-first: %v", h.CaptureTimes)
	}
	for _, ct := range h.CaptureTimes {
		if ct == older.Format(time.RFC3339) {
			t.Errorf("older-bucket capture %s must be excluded, got %v", ct, h.CaptureTimes)
		}
	}
}
