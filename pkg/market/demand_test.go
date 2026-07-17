package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadCurrentBuyOrders verifies the demand-report data source: only the
// latest capture per (station, item) survives, sell/zero-price/zero-quantity
// rows are filtered, station/item joins fill system_id and item_name, and
// orders within a pair come back best-price-first.
func TestLoadCurrentBuyOrders(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	t1 := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(30 * time.Minute)

	// Older capture at stn1: should be entirely superseded for iron.
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", StationName: "Station One",
		SystemID: "sys1", SystemName: "System One",
		CapturedAt: t1,
		Orders: []Order{
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron Ore", Side: "buy", PriceEach: 90, Quantity: 5, CapturedAt: t1},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot t1 failed: %v", err)
	}

	// Newer capture at stn1: two buy rungs plus rows the query must filter.
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", StationName: "Station One",
		SystemID: "sys1", SystemName: "System One",
		CapturedAt: t2,
		Orders: []Order{
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron Ore", Side: "buy", PriceEach: 100, Quantity: 10, Source: "station", CapturedAt: t2},
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron Ore", Side: "buy", PriceEach: 120, Quantity: 3, MyQuantity: 2, CapturedAt: t2},
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron Ore", Side: "sell", PriceEach: 150, Quantity: 20, CapturedAt: t2},
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron Ore", Side: "buy", PriceEach: 0, Quantity: 7, CapturedAt: t2},
			{StationID: "stn1", ItemID: "iron", ItemName: "Iron Ore", Side: "buy", PriceEach: 80, Quantity: 0, CapturedAt: t2},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot t2 failed: %v", err)
	}

	// Second station, single capture — must coexist with stn1's latest.
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn2", StationName: "Station Two",
		SystemID: "sys2", SystemName: "System Two",
		CapturedAt: t1,
		Orders: []Order{
			{StationID: "stn2", ItemID: "copper", ItemName: "Copper Ore", Side: "buy", PriceEach: 40, Quantity: 8, CapturedAt: t1},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot stn2 failed: %v", err)
	}

	got, err := c.LoadCurrentBuyOrders(ctx)
	if err != nil {
		t.Fatalf("LoadCurrentBuyOrders failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d orders, want 3: %+v", len(got), got)
	}

	// ORDER BY station, item, price DESC → stn1 iron 120, stn1 iron 100, stn2 copper 40.
	first, second, third := got[0], got[1], got[2]
	if first.StationID != "stn1" || first.PriceEach != 120 || first.MyQuantity != 2 || first.Source != "" {
		t.Errorf("first = %+v, want stn1 iron @120 my_qty=2 player-source", first)
	}
	if second.PriceEach != 100 || second.Source != "station" || second.Quantity != 10 {
		t.Errorf("second = %+v, want stn1 iron @100 qty=10 source=station", second)
	}
	if third.StationID != "stn2" || third.ItemID != "copper" || third.PriceEach != 40 {
		t.Errorf("third = %+v, want stn2 copper @40", third)
	}

	// Joins and timestamps.
	if first.SystemID != "sys1" || first.ItemName != "Iron Ore" {
		t.Errorf("join fields = system %q item %q, want sys1 / Iron Ore", first.SystemID, first.ItemName)
	}
	if third.SystemID != "sys2" || third.ItemName != "Copper Ore" {
		t.Errorf("stn2 join fields = system %q item %q, want sys2 / Copper Ore", third.SystemID, third.ItemName)
	}
	if !first.CapturedAt.Equal(t2) {
		t.Errorf("first.CapturedAt = %v, want %v (latest capture)", first.CapturedAt, t2)
	}
	if !third.CapturedAt.Equal(t1) {
		t.Errorf("third.CapturedAt = %v, want %v", third.CapturedAt, t1)
	}

	// The t1 iron rung (90) must not leak through.
	for _, o := range got {
		if o.PriceEach == 90 {
			t.Errorf("stale t1 capture leaked into results: %+v", o)
		}
	}
}
