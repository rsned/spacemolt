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
