package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestMigration44CreatesSupplyTables(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	for _, tbl := range []string{"market_sell_orders", "market_supply_history"} {
		var name string
		if err := kb.db.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name); err != nil {
			t.Fatalf("table %s not found: %v", tbl, err)
		}
	}
}

func TestReplaceStationSellOrdersRoundTrip(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)

	orders := []MarketSellOrderRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 15, Quantity: 50, MyQuantity: 20, Source: "station", CapturedAt: t0},
		{StationID: "stn1", SystemID: "sys1", ItemID: "copper", ItemName: "Copper", PriceEach: 9, Quantity: 100, Source: "station", CapturedAt: t0},
	}
	if err := kb.ReplaceStationSellOrders(ctx, "stn1", orders); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := kb.LoadMarketSellOrders(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 orders, got %d", len(got))
	}
	byItem := map[string]MarketSellOrderRow{}
	for _, o := range got {
		byItem[o.ItemID] = o
	}
	if byItem["iron_ore"].MyQuantity != 20 {
		t.Errorf("iron_ore my_quantity: want 20, got %v", byItem["iron_ore"].MyQuantity)
	}
}

func TestReplaceStationSellOrdersPrunesAndIsolates(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)

	if err := kb.ReplaceStationSellOrders(ctx, "stnA", []MarketSellOrderRow{
		{StationID: "stnA", ItemID: "iron_ore", PriceEach: 15, Quantity: 50, Source: "station", CapturedAt: t0},
		{StationID: "stnA", ItemID: "copper", PriceEach: 9, Quantity: 100, Source: "station", CapturedAt: t0},
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := kb.ReplaceStationSellOrders(ctx, "stnB", []MarketSellOrderRow{
		{StationID: "stnB", ItemID: "iron_ore", PriceEach: 16, Quantity: 30, Source: "station", CapturedAt: t0},
	}); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	// Replace A with fewer items: copper supply vanished -> must be pruned.
	if err := kb.ReplaceStationSellOrders(ctx, "stnA", []MarketSellOrderRow{
		{StationID: "stnA", ItemID: "iron_ore", PriceEach: 14, Quantity: 40, Source: "station", CapturedAt: t0},
	}); err != nil {
		t.Fatalf("replace A: %v", err)
	}

	got, err := kb.LoadMarketSellOrders(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	counts := map[string]int{}
	for _, o := range got {
		counts[o.StationID]++
		if o.StationID == "stnA" && o.ItemID == "copper" {
			t.Errorf("stnA copper should have been pruned, but survived")
		}
	}
	if counts["stnA"] != 1 {
		t.Errorf("stnA: want 1 order after prune, got %d", counts["stnA"])
	}
	if counts["stnB"] != 1 {
		t.Errorf("stnB: want 1 order (isolated from A replace), got %d", counts["stnB"])
	}
}

func TestRecordAndLoadSupplyHistory(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	bucket := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)

	samples := []SupplyHistorySample{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", BucketAt: bucket, CapturedAt: bucket, BestPrice: 13, TotalQty: 70, SMBestPrice: 15, SMQty: 50, OrderCount: 2},
	}
	if err := kb.RecordSupplyHistory(ctx, samples); err != nil {
		t.Fatalf("record: %v", err)
	}
	// Re-record in the same bucket with new values: upsert in place.
	samples[0].BestPrice = 11
	samples[0].TotalQty = 80
	if err := kb.RecordSupplyHistory(ctx, samples); err != nil {
		t.Fatalf("record2: %v", err)
	}

	got, err := kb.LoadSupplyHistory(ctx, "iron_ore", "stn1", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row (upsert in same bucket), got %d", len(got))
	}
	if got[0].BestPrice != 11 || got[0].TotalQty != 80 {
		t.Errorf("upsert did not take latest values: %+v", got[0])
	}
}
