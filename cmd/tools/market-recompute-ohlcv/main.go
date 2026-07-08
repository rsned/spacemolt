// Command market-recompute-ohlcv repairs market_ohlcv rows poisoned by the
// game's "not for sale" sentinel price (999999).
//
// view_market exposes only resting orders, so market_ohlcv is order-book data;
// an earlier pipeline folded sentinel rungs into volume/vwap/high, inflating the
// sell-side reference wildly (iron_ore read 382cr against a ~2cr live floor). The
// producer no longer does this, but historical rows remain.
//
// This tool rebuilds each contaminated bucket (high_price >= sentinel) from the
// surviving raw market_orders when they exist, and deletes the row when the raw
// orders have been pruned away (retention is short, so most old rows are
// unrecoverable). It is idempotent and only touches contaminated rows — clean
// history is left alone. Safe to run while the fleet writes (uses busy-timeout
// retries), though it takes a write lock per batch.
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/market"
)

func main() {
	dbPath := flag.String("db-path", "data/market.db", "Path to market.db")
	batch := flag.Int("batch", 500, "Buckets repaired per transaction")
	flag.Parse()

	logger := log.New(os.Stdout, "[recompute-ohlcv] ", log.LstdFlags)

	col, err := market.Open(market.Config{DBPath: *dbPath, WAL: true})
	if err != nil {
		logger.Fatalf("open %s: %v", *dbPath, err)
	}
	defer col.Close() //nolint:errcheck

	stats, err := col.RecomputeContaminatedOHLCV(context.Background(), *batch)
	if err != nil {
		logger.Fatalf("recompute: %v", err)
	}
	logger.Printf("done: %d contaminated bucket(s) — %d recomputed from surviving raw orders, %d deleted as unrecoverable",
		stats.Contaminated, stats.Recomputed, stats.Deleted)
}
