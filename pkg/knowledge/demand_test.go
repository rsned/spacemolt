package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestMigration36CreatesDemandTable(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	var name string
	if err := kb.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, "market_buy_orders").Scan(&name); err != nil {
		t.Fatalf("table market_buy_orders not found: %v", err)
	}
}

func TestReplaceStationBuyOrdersRoundTrip(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	orders := []MarketBuyOrderRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: t0},
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: "", CapturedAt: t0},
		{StationID: "stn1", SystemID: "sys1", ItemID: "copper", ItemName: "Copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: t0},
	}
	if err := kb.ReplaceStationBuyOrders(ctx, "stn1", orders); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := kb.LoadMarketBuyOrders(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 orders, got %d", len(got))
	}
}

func TestReplaceStationBuyOrdersPersistsMyQuantity(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	orders := []MarketBuyOrderRow{
		{StationID: "stn1", ItemID: "iron_ore", PriceEach: 12, Quantity: 20, MyQuantity: 20, Source: "", CapturedAt: t0},
		{StationID: "stn1", ItemID: "copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: t0},
	}
	if err := kb.ReplaceStationBuyOrders(ctx, "stn1", orders); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := kb.LoadMarketBuyOrders(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	byItem := map[string]MarketBuyOrderRow{}
	for _, o := range got {
		byItem[o.ItemID] = o
	}
	if byItem["iron_ore"].MyQuantity != 20 {
		t.Errorf("iron_ore my_quantity: want 20, got %v", byItem["iron_ore"].MyQuantity)
	}
	if byItem["copper"].MyQuantity != 0 {
		t.Errorf("copper my_quantity: want 0 (station order), got %v", byItem["copper"].MyQuantity)
	}
}

func TestReplaceStationBuyOrdersPrunesAndIsolates(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	// Seed two stations.
	if err := kb.ReplaceStationBuyOrders(ctx, "stnA", []MarketBuyOrderRow{
		{StationID: "stnA", ItemID: "iron_ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: t0},
		{StationID: "stnA", ItemID: "copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: t0},
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := kb.ReplaceStationBuyOrders(ctx, "stnB", []MarketBuyOrderRow{
		{StationID: "stnB", ItemID: "iron_ore", PriceEach: 11, Quantity: 30, Source: "station", CapturedAt: t0},
	}); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	// Replace A with fewer items: copper demand vanished -> must be pruned.
	if err := kb.ReplaceStationBuyOrders(ctx, "stnA", []MarketBuyOrderRow{
		{StationID: "stnA", ItemID: "iron_ore", PriceEach: 10, Quantity: 40, Source: "station", CapturedAt: t0},
	}); err != nil {
		t.Fatalf("replace A: %v", err)
	}

	got, err := kb.LoadMarketBuyOrders(ctx)
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
