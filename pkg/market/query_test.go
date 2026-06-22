package market

import (
	"context"
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
