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

// TestPruneOrders_BatchesLargeDeletes covers the case that has taken the fleet
// down twice: a backlog larger than one batch. The delete must complete across
// several transactions rather than one, so the write lock is released in between
// and marketbot captures can interleave.
func TestPruneOrders_BatchesLargeDeletes(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "batch.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = c.Close() }()
	ctx := context.Background()

	// One batch plus a remainder, so a single-statement implementation and a
	// batched one are distinguishable by row count alone.
	const total = pruneBatchSize + 1234
	old := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO market_orders
		(station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at, bucket_utc)
		VALUES (?, ?, ?, ?, ?, 0, 'test', ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	stamp := old.Format(time.RFC3339)
	for i := range total {
		if _, err := stmt.Exec("stn1", "iron_ore", "sell", float64(i%97), 1, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// A row inside the window must survive the loop.
	keep := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
		CapturedAt: keep,
		Orders:     []Order{{StationID: "stn1", ItemID: "copper_ore", Side: "buy", PriceEach: 3, Quantity: 1, CapturedAt: keep}},
	}); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	n, err := c.PruneOrders(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneOrders: %v", err)
	}
	if n != total {
		t.Errorf("deleted = %d, want %d — the loop stopped before draining the backlog", n, total)
	}

	var remaining int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM market_orders`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Errorf("remaining = %d, want the single in-window row", remaining)
	}
}
