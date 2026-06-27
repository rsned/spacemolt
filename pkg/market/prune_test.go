package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneOrders(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = c.Close() }()
	ctx := context.Background()

	old := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 6, 26, 18, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{old, recent} {
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
			CapturedAt: at,
			Orders:     []Order{{StationID: "stn1", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 1, CapturedAt: at}},
		}); err != nil {
			t.Fatalf("WriteSnapshot %v: %v", at, err)
		}
	}

	// Prune everything before 2026-06-26 00:00: drops the 06-25 bucket, keeps 06-26.
	cutoff := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	n, err := c.PruneOrders(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneOrders: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1", n)
	}

	var remaining int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM market_orders`).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining = %d, want 1", remaining)
	}

	// Idempotent: pruning again at the same cutoff removes nothing.
	n2, err := c.PruneOrders(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneOrders #2: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second prune deleted = %d, want 0", n2)
	}
}
