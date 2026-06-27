// Command market-prune enforces a retention window on the market_orders table.
//
// SQLite has no built-in row expiration, so this process deletes orders whose
// capture bucket is older than --retain. Run it once from cron / a systemd
// timer, or as a resident loop with --interval. Freed pages are reused by later
// inserts, so the file stabilizes near the retention-window peak; pass --vacuum
// to also shrink the file on disk (this takes an exclusive lock, so only use it
// when no writers — overminds, scanner, play_as — are touching the database).
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
)

func main() {
	dbPath := flag.String("db-path", "data/market.db", "Path to market.db")
	retain := flag.Duration("retain", 12*time.Hour, "Keep market_orders newer than this; delete older")
	interval := flag.Duration("interval", 0, "If >0, prune repeatedly at this interval; otherwise prune once and exit")
	vacuum := flag.Bool("vacuum", false, "VACUUM after pruning (exclusive lock; only when no writers are active)")
	flag.Parse()

	logger := log.New(os.Stdout, "[market-prune] ", log.LstdFlags)

	col, err := market.Open(market.Config{DBPath: *dbPath, WAL: true})
	if err != nil {
		logger.Fatalf("open %s: %v", *dbPath, err)
	}
	defer col.Close() //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Printf("signal received, shutting down")
		cancel()
	}()

	prune := func() {
		cutoff := time.Now().Add(-*retain)
		n, err := col.PruneOrders(ctx, cutoff)
		if err != nil {
			logger.Printf("prune: %v", err)
			return
		}
		logger.Printf("pruned %d order(s) older than %s (retain %s)", n, cutoff.UTC().Format(time.RFC3339), *retain)
		if *vacuum {
			if verr := col.Vacuum(ctx); verr != nil {
				logger.Printf("vacuum: %v", verr)
			} else {
				logger.Printf("vacuum complete")
			}
		}
	}

	prune()
	if *interval <= 0 {
		return
	}
	logger.Printf("resident mode: pruning every %s", *interval)
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}
