package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestMigration36CreatesDemandTables(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	for _, table := range []string{"market_buy_demand", "market_buy_orders"} {
		var name string
		err := kb.db.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %q not found: %v", table, err)
		}
	}
}

func TestUpsertMarketDemandReplacesByKey(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	if err := kb.UpsertMarketDemand(ctx, []MarketDemandRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", BestBuyPrice: 10, BuyQuantity: 100, CapturedAt: t0},
	}); err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	// Re-capture same (station,item) with a higher price -> row is replaced, not duplicated.
	if err := kb.UpsertMarketDemand(ctx, []MarketDemandRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", BestBuyPrice: 12, BuyQuantity: 80, CapturedAt: t0.Add(time.Hour)},
	}); err != nil {
		t.Fatalf("upsert2: %v", err)
	}

	summary, _, err := kb.LoadMarketDemand(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("want 1 demand row, got %d", len(summary))
	}
	if summary[0].BestBuyPrice != 12 || summary[0].BuyQuantity != 80 {
		t.Fatalf("want price 12 qty 80, got %v / %v", summary[0].BestBuyPrice, summary[0].BuyQuantity)
	}
}

func TestReplaceMarketBuyOrdersRoundTrip(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	orders := []MarketBuyOrderRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: t0},
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: "player", CapturedAt: t0},
	}
	if err := kb.ReplaceMarketBuyOrders(ctx, "stn1", "iron_ore", orders); err != nil {
		t.Fatalf("replace1: %v", err)
	}
	// Replacing again with one order leaves exactly one row for that key.
	if err := kb.ReplaceMarketBuyOrders(ctx, "stn1", "iron_ore", orders[:1]); err != nil {
		t.Fatalf("replace2: %v", err)
	}
	_, deep, err := kb.LoadMarketDemand(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(deep) != 1 {
		t.Fatalf("want 1 deep order after replace, got %d", len(deep))
	}
	if deep[0].Source != "station" || deep[0].PriceEach != 10 {
		t.Fatalf("unexpected deep order: %+v", deep[0])
	}
}
