package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// testCollector opens a throwaway collector against a temp database.
func testCollector(t *testing.T) *Collector {
	t.Helper()
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if cerr := c.Close(); cerr != nil {
			t.Errorf("Close: %v", cerr)
		}
	})

	return c
}

// bookSnapshot builds a one-station snapshot whose sell book has a single price
// level, so a duplicated write is visible as doubled depth rather than as a
// changed price.
func bookSnapshot(station string, at time.Time, qty float64) MarketSnapshot {
	return MarketSnapshot{
		StationID: station, StationName: "Test Station",
		SystemID: "sys", SystemName: "Sys", CapturedAt: at,
		Orders: []Order{
			{
				StationID: station, ItemID: "rotgut", ItemName: "Rotgut", Side: "sell",
				PriceEach: 1876, Quantity: qty, Source: "station", CapturedAt: at,
			},
			{
				StationID: station, ItemID: "rotgut", ItemName: "Rotgut", Side: "buy",
				PriceEach: 900, Quantity: 40, Source: "", CapturedAt: at,
			},
		},
	}
}

// Two marketbots docked at the same station capture it in the same second, and
// captured_at is RFC3339 (second resolution), so both books land under one
// timestamp. With a plain INSERT that stored the book twice: live on 2026-08-12
// Frontier Station carried 8 copies and Central Nexus 2, which inflated raw
// station-manager sell value from 8.37B to 21.72B and — because
// GetItemStationPrices sums `AskQty += qty` over every row at the station's
// latest capture — handed the arbitrage scanner up to 8x the real book depth.
// A snapshot write must therefore be idempotent per (station, captured_at).
func TestWriteSnapshotIsIdempotentPerStationCapture(t *testing.T) {
	c := testCollector(t)
	ctx := context.Background()
	at := time.Date(2026, 8, 12, 16, 20, 24, 0, time.UTC)

	for range 3 {
		if err := c.WriteSnapshot(ctx, bookSnapshot("the_core", at, 209659)); err != nil {
			t.Fatalf("WriteSnapshot: %v", err)
		}
	}

	var rows int
	var depth float64
	if err := c.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(quantity),0) FROM market_orders
		 WHERE station_id='the_core' AND captured_at=? AND side='sell'`,
		at.Format(time.RFC3339)).Scan(&rows, &depth); err != nil {
		t.Fatalf("query: %v", err)
	}
	if rows != 1 {
		t.Errorf("sell rows = %d, want 1 (the book was stored %dx)", rows, rows)
	}
	if depth != 209659 {
		t.Errorf("book depth = %.0f, want 209659 (phantom depth inflates bookCap)", depth)
	}
}

// The guard is scoped to one (station, captured_at). A later capture of the same
// station is a NEW observation and must be kept — collapsing those would destroy
// the price history every trend query reads.
func TestWriteSnapshotKeepsDistinctCaptures(t *testing.T) {
	c := testCollector(t)
	ctx := context.Background()
	first := time.Date(2026, 8, 12, 16, 20, 24, 0, time.UTC)
	second := first.Add(time.Second)

	if err := c.WriteSnapshot(ctx, bookSnapshot("the_core", first, 209706)); err != nil {
		t.Fatalf("WriteSnapshot first: %v", err)
	}
	if err := c.WriteSnapshot(ctx, bookSnapshot("the_core", second, 209659)); err != nil {
		t.Fatalf("WriteSnapshot second: %v", err)
	}

	var captures int
	if err := c.db.QueryRow(
		`SELECT COUNT(DISTINCT captured_at) FROM market_orders WHERE station_id='the_core'`).
		Scan(&captures); err != nil {
		t.Fatalf("query: %v", err)
	}
	if captures != 2 {
		t.Errorf("distinct captures = %d, want 2", captures)
	}
}

// Another station captured in the same second is an unrelated book: the guard
// must not delete it.
func TestWriteSnapshotDoesNotDisturbOtherStations(t *testing.T) {
	c := testCollector(t)
	ctx := context.Background()
	at := time.Date(2026, 8, 12, 16, 20, 24, 0, time.UTC)

	if err := c.WriteSnapshot(ctx, bookSnapshot("the_core", at, 100)); err != nil {
		t.Fatalf("WriteSnapshot the_core: %v", err)
	}
	if err := c.WriteSnapshot(ctx, bookSnapshot("mobile_capital", at, 200)); err != nil {
		t.Fatalf("WriteSnapshot mobile_capital: %v", err)
	}

	var stations int
	if err := c.db.QueryRow(`SELECT COUNT(DISTINCT station_id) FROM market_orders`).Scan(&stations); err != nil {
		t.Fatalf("query: %v", err)
	}
	if stations != 2 {
		t.Errorf("distinct stations = %d, want 2", stations)
	}
}

// A re-capture in the same second REPLACES the book rather than merging it: the
// second observation is the better one, and a book that shed a price level must
// not keep the stale level alive.
func TestWriteSnapshotReplacesRatherThanMerges(t *testing.T) {
	c := testCollector(t)
	ctx := context.Background()
	at := time.Date(2026, 8, 12, 16, 20, 24, 0, time.UTC)

	full := bookSnapshot("the_core", at, 500)
	full.Orders = append(full.Orders, Order{
		StationID: "the_core", ItemID: "rotgut", ItemName: "Rotgut", Side: "sell",
		PriceEach: 2680, Quantity: 250, Source: "station", CapturedAt: at,
	})
	if err := c.WriteSnapshot(ctx, full); err != nil {
		t.Fatalf("WriteSnapshot full: %v", err)
	}
	if err := c.WriteSnapshot(ctx, bookSnapshot("the_core", at, 500)); err != nil {
		t.Fatalf("WriteSnapshot thin: %v", err)
	}

	var levels int
	if err := c.db.QueryRow(
		`SELECT COUNT(*) FROM market_orders WHERE station_id='the_core' AND side='sell'`).
		Scan(&levels); err != nil {
		t.Fatalf("query: %v", err)
	}
	if levels != 1 {
		t.Errorf("sell levels = %d, want 1 (stale level survived the replacement)", levels)
	}
}

// A snapshot that arrived with no orders carries no observation to replace the
// book with, so it must leave the recorded book alone rather than erase it.
func TestWriteSnapshotWithNoOrdersLeavesBookIntact(t *testing.T) {
	c := testCollector(t)
	ctx := context.Background()
	at := time.Date(2026, 8, 12, 16, 20, 24, 0, time.UTC)

	if err := c.WriteSnapshot(ctx, bookSnapshot("the_core", at, 500)); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	empty := bookSnapshot("the_core", at, 500)
	empty.Orders = nil
	if err := c.WriteSnapshot(ctx, empty); err != nil {
		t.Fatalf("WriteSnapshot empty: %v", err)
	}

	var rows int
	if err := c.db.QueryRow(
		`SELECT COUNT(*) FROM market_orders WHERE station_id='the_core'`).Scan(&rows); err != nil {
		t.Fatalf("query: %v", err)
	}
	if rows != 2 {
		t.Errorf("rows = %d, want 2 (an empty capture erased a good book)", rows)
	}
}

// Genuinely identical orders WITHIN one book are two real orders that happen to
// tie on price and size. They are not the bug, and the guard must not collapse
// them — that would under-report depth the scanner is entitled to see.
func TestWriteSnapshotKeepsTiedOrdersWithinOneBook(t *testing.T) {
	c := testCollector(t)
	ctx := context.Background()
	at := time.Date(2026, 8, 12, 16, 20, 24, 0, time.UTC)

	snap := bookSnapshot("the_core", at, 500)
	snap.Orders = append(snap.Orders, Order{
		StationID: "the_core", ItemID: "rotgut", ItemName: "Rotgut", Side: "sell",
		PriceEach: 1876, Quantity: 500, Source: "station", CapturedAt: at,
	})
	if err := c.WriteSnapshot(ctx, snap); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	var depth float64
	if err := c.db.QueryRow(
		`SELECT COALESCE(SUM(quantity),0) FROM market_orders
		 WHERE station_id='the_core' AND side='sell'`).Scan(&depth); err != nil {
		t.Fatalf("query: %v", err)
	}
	if depth != 1000 {
		t.Errorf("depth = %.0f, want 1000 (two tied orders are real depth)", depth)
	}
}
