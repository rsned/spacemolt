package market

import (
	"context"
	"testing"
	"time"
)

// seedOrders inserts order rows through the collector's real insert path (a
// transaction + insertOrders), so tests exercise the same SQL production
// writes use rather than a hand-rolled INSERT. Reused by TestGetAskLadder
// here and by Task 3's TestGetReferencePrice.
func seedOrders(t *testing.T, c *Collector, orders []Order) {
	t.Helper()
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := c.insertOrders(tx, orders); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insertOrders: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

func TestGetAskLadder(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	now := time.Now().UTC()
	older := now.Add(-time.Hour)

	orders := []Order{
		// latest capture, sell side — belongs in the ladder.
		{StationID: "s1", ItemID: "iron_ore", Side: "sell", PriceEach: 2000, Quantity: 100, CapturedAt: now,
			BucketUTC: now.Truncate(time.Hour).Format(time.RFC3339)},
		{StationID: "s1", ItemID: "iron_ore", Side: "sell", PriceEach: 10, Quantity: 3, CapturedAt: now,
			BucketUTC: now.Truncate(time.Hour).Format(time.RFC3339)},
		// latest capture, buy side — must be excluded (sell-only).
		{StationID: "s1", ItemID: "iron_ore", Side: "buy", PriceEach: 5, Quantity: 50, CapturedAt: now,
			BucketUTC: now.Truncate(time.Hour).Format(time.RFC3339)},
		// stale capture at the same station/item — must be excluded (latest-capture only).
		{StationID: "s1", ItemID: "iron_ore", Side: "sell", PriceEach: 1, Quantity: 999, CapturedAt: older,
			BucketUTC: older.Truncate(time.Hour).Format(time.RFC3339)},
		// different station — must not leak into s1's ladder.
		{StationID: "s2", ItemID: "iron_ore", Side: "sell", PriceEach: 1, Quantity: 999, CapturedAt: now,
			BucketUTC: now.Truncate(time.Hour).Format(time.RFC3339)},
	}
	seedOrders(t, c, orders)

	got, err := c.GetAskLadder(ctx, "iron_ore", "s1")
	if err != nil {
		t.Fatalf("GetAskLadder: %v", err)
	}
	want := []AskLevel{{PriceEach: 10, Quantity: 3}, {PriceEach: 2000, Quantity: 100}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ladder = %+v, want %+v (ascending price, sell-side only, latest capture)", got, want)
	}
}

func TestGetAskLadder_NoSellOrders(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()

	got, err := c.GetAskLadder(ctx, "no_such_item", "s1")
	if err != nil {
		t.Fatalf("GetAskLadder: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ladder = %+v, want empty slice for item with no orders", got)
	}
}
