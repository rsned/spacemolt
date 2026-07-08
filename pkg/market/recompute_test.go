package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestRecomputeContaminatedOHLCV verifies the historical repair: a contaminated
// bucket with surviving raw orders is rebuilt cleanly, a contaminated bucket with
// no recoverable raw orders is deleted, clean rows are untouched, and the run is
// idempotent.
func TestRecomputeContaminatedOHLCV(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()

	// Seed a directly-inserted contaminated OHLCV row whose raw orders survive
	// (recoverable), a contaminated row with NO raw orders (unrecoverable), and a
	// clean row that must be left alone.
	insertOHLCV := func(station, item, side, bucket string, high, vwap, vol float64) {
		t.Helper()
		if _, err := c.db.ExecContext(ctx, `
			INSERT INTO market_ohlcv (station_id,item_id,side,bucket_utc,open_price,high_price,low_price,close_price,volume,trade_count,vwap)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			station, item, side, bucket, 5.0, high, 5.0, high, vol, 3, vwap); err != nil {
			t.Fatalf("insert ohlcv: %v", err)
		}
	}
	// Station rows are needed for FK on market_orders inserts.
	mustStation := func(id string) {
		t.Helper()
		if _, err := c.db.ExecContext(ctx, `INSERT OR IGNORE INTO stations (station_id) VALUES (?)`, id); err != nil {
			t.Fatalf("insert station: %v", err)
		}
	}
	mustItem := func(id string) {
		t.Helper()
		if _, err := c.db.ExecContext(ctx, `INSERT OR IGNORE INTO items (item_id) VALUES (?)`, id); err != nil {
			t.Fatalf("insert item: %v", err)
		}
	}
	mustStation("recov")
	mustStation("prune")
	mustStation("clean")
	mustItem("iron")

	insertOHLCV("recov", "iron", "sell", "2026-07-08T04:00:00Z", NotForSalePrice, 382.0, 1000) // recoverable
	insertOHLCV("prune", "iron", "sell", "2026-06-21T16:00:00Z", NotForSalePrice, 700.0, 900)   // unrecoverable
	insertOHLCV("clean", "iron", "sell", "2026-07-08T04:00:00Z", 8.0, 6.5, 20)                   // clean, must survive

	// Raw orders only for the recoverable bucket: real rungs 5 & 8 plus a sentinel.
	cap1 := time.Date(2026, 7, 8, 4, 30, 0, 0, time.UTC).Format(time.RFC3339)
	for _, o := range []Order{
		{StationID: "recov", ItemID: "iron", Side: "sell", PriceEach: 5, Quantity: 10},
		{StationID: "recov", ItemID: "iron", Side: "sell", PriceEach: 8, Quantity: 4},
		{StationID: "recov", ItemID: "iron", Side: "sell", PriceEach: NotForSalePrice, Quantity: 999},
	} {
		if _, err := c.db.ExecContext(ctx, `
			INSERT INTO market_orders (station_id,item_id,side,price_each,quantity,captured_at,bucket_utc)
			VALUES (?,?,?,?,?,?,?)`,
			o.StationID, o.ItemID, o.Side, o.PriceEach, o.Quantity, cap1, "2026-07-08T04:00:00Z"); err != nil {
			t.Fatalf("insert order: %v", err)
		}
	}

	stats, err := c.RecomputeContaminatedOHLCV(ctx, 0)
	if err != nil {
		t.Fatalf("RecomputeContaminatedOHLCV: %v", err)
	}
	if stats.Contaminated != 2 || stats.Recomputed != 1 || stats.Deleted != 1 {
		t.Errorf("stats = %+v, want {Contaminated:2 Recomputed:1 Deleted:1}", stats)
	}

	// Recoverable bucket now holds the sentinel-free recomputation.
	var high, vwap, vol float64
	if err := c.db.QueryRowContext(ctx,
		`SELECT high_price, vwap, volume FROM market_ohlcv WHERE station_id='recov'`).Scan(&high, &vwap, &vol); err != nil {
		t.Fatalf("query recov row: %v", err)
	}
	if high != 8 || vol != 14 || vwap != float64(5*10+8*4)/14 {
		t.Errorf("recov row = high %g vwap %g vol %g, want high 8 vwap %g vol 14", high, vwap, vol, float64(5*10+8*4)/14)
	}

	// Unrecoverable bucket deleted; clean bucket untouched; zero contaminated left.
	assertCount := func(query string, want int) {
		t.Helper()
		var n int
		if err := c.db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", query, err)
		}
		if n != want {
			t.Errorf("count %q = %d, want %d", query, n, want)
		}
	}
	assertCount("SELECT COUNT(*) FROM market_ohlcv WHERE station_id='prune'", 0)
	assertCount("SELECT COUNT(*) FROM market_ohlcv WHERE station_id='clean'", 1)
	assertCount("SELECT COUNT(*) FROM market_ohlcv WHERE high_price >= 999999", 0)

	// Idempotent: a second run finds nothing to do.
	stats2, err := c.RecomputeContaminatedOHLCV(ctx, 0)
	if err != nil {
		t.Fatalf("second RecomputeContaminatedOHLCV: %v", err)
	}
	if stats2 != (RecomputeStats{}) {
		t.Errorf("second run stats = %+v, want zero", stats2)
	}
}
