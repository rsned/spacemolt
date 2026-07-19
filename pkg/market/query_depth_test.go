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

func TestGetAskLadder_ExcludesSentinelPrice(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	now := time.Now().UTC()

	orders := []Order{
		// Tradeable sell orders at the latest capture.
		{StationID: "s1", ItemID: "iron_ore", Side: "sell", PriceEach: 50, Quantity: 10, CapturedAt: now,
			BucketUTC: now.Truncate(time.Hour).Format(time.RFC3339)},
		{StationID: "s1", ItemID: "iron_ore", Side: "sell", PriceEach: 100, Quantity: 5, CapturedAt: now,
			BucketUTC: now.Truncate(time.Hour).Format(time.RFC3339)},
		// Sentinel (not-for-sale) order at the same capture — must be excluded.
		{StationID: "s1", ItemID: "iron_ore", Side: "sell", PriceEach: NotForSalePrice, Quantity: 999, CapturedAt: now,
			BucketUTC: now.Truncate(time.Hour).Format(time.RFC3339)},
	}
	seedOrders(t, c, orders)

	got, err := c.GetAskLadder(ctx, "iron_ore", "s1")
	if err != nil {
		t.Fatalf("GetAskLadder: %v", err)
	}
	// Should only include the tradeable orders, not the sentinel.
	want := []AskLevel{{PriceEach: 50, Quantity: 10}, {PriceEach: 100, Quantity: 5}}
	if len(got) != len(want) {
		t.Fatalf("ladder length = %d, want %d; got = %+v, want = %+v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ladder[%d] = %+v, want %+v (sentinel price should be excluded)", i, got[i], want[i])
		}
	}
}

func TestGetReferencePrice(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// Five stations offering iron_ore ~5-10, one gouging @2000.
	seed := []Order{
		{StationID: "a", ItemID: "iron_ore", Side: "sell", PriceEach: 6, Quantity: 50, CapturedAt: now},
		{StationID: "b", ItemID: "iron_ore", Side: "sell", PriceEach: 7, Quantity: 50, CapturedAt: now},
		{StationID: "c", ItemID: "iron_ore", Side: "sell", PriceEach: 8, Quantity: 50, CapturedAt: now},
		{StationID: "d", ItemID: "iron_ore", Side: "sell", PriceEach: 9, Quantity: 50, CapturedAt: now},
		{StationID: "e", ItemID: "iron_ore", Side: "sell", PriceEach: 10, Quantity: 50, CapturedAt: now},
		{StationID: "z", ItemID: "iron_ore", Side: "sell", PriceEach: 2000, Quantity: 50, CapturedAt: now},
	}
	seedOrders(t, c, seed) // reuse the same seed helper as Task 2
	ref, ok, err := c.GetReferencePrice(ctx, "iron_ore", 24*time.Hour)
	if err != nil || !ok {
		t.Fatalf("GetReferencePrice: ok=%v err=%v", ok, err)
	}
	if ref > 12 { // 20th pct of {6,7,8,9,10,2000} must sit in the cheap cluster, not near 2000
		t.Fatalf("reference %v; expected cheap-cluster value, gouging station must be an outlier", ref)
	}
	if _, ok, _ := c.GetReferencePrice(ctx, "no_such_item", 24*time.Hour); ok {
		t.Fatalf("expected ok=false for unknown item")
	}
}
