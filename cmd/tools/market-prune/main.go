// Command market-prune enforces retention windows on the market data tables.
//
// SQLite has no built-in row expiration, so this process deletes rows older
// than their window: market_orders snapshots beyond --retain (default 4h), and
// the hourly history — market_ohlcv in market.db plus market_demand_history and
// market_supply_history in the knowledge DB — beyond --history-retain (default
// 14 days). Pass --kb-path "" to skip the knowledge DB. Run it once from cron / a systemd
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

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

func main() {
	dbPath := flag.String("db-path", "data/market.db", "Path to market.db")
	kbPath := flag.String("kb-path", "data/spacemolt-knowledge.db", `Path to the knowledge DB holding market demand/supply history ("" skips it)`)
	retain := flag.Duration("retain", 4*time.Hour, "Keep market_orders snapshots newer than this; delete older")
	historyRetain := flag.Duration("history-retain", 14*24*time.Hour, "Keep market_ohlcv and KB demand/supply history newer than this; delete older")
	interval := flag.Duration("interval", 0, "If >0, prune repeatedly at this interval; otherwise prune once and exit")
	vacuum := flag.Bool("vacuum", false, "VACUUM after pruning (exclusive lock; only when no writers are active)")
	flag.Parse()

	logger := log.New(os.Stdout, "[market-prune] ", log.LstdFlags)

	col, err := market.Open(market.Config{DBPath: *dbPath, WAL: true})
	if err != nil {
		logger.Fatalf("open %s: %v", *dbPath, err)
	}
	defer col.Close() //nolint:errcheck

	var kb *knowledge.SQLiteKB
	if *kbPath != "" {
		kb, err = knowledge.NewSQLiteKB(knowledge.Config{DBPath: *kbPath, WAL: true, MaxOpenConns: 1, MaxIdleConns: 1})
		if err != nil {
			logger.Fatalf("open %s: %v", *kbPath, err)
		}
		defer kb.Close() //nolint:errcheck
	}

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
		now := time.Now()
		cutoff := now.Add(-*retain)
		n, err := col.PruneOrders(ctx, cutoff)
		if err != nil {
			logger.Printf("prune orders: %v", err)
			return
		}
		logger.Printf("pruned %d order(s) older than %s (retain %s)", n, cutoff.UTC().Format(time.RFC3339), *retain)

		histCutoff := now.Add(-*historyRetain)
		if n, err = col.PruneOHLCV(ctx, histCutoff); err != nil {
			logger.Printf("prune ohlcv: %v", err)
		} else {
			logger.Printf("pruned %d ohlcv candle(s) older than %s (history-retain %s)", n, histCutoff.UTC().Format(time.RFC3339), *historyRetain)
		}
		if kb != nil {
			d, s, kerr := kb.PruneMarketHistory(ctx, histCutoff)
			if kerr != nil {
				logger.Printf("prune kb market history: %v (removed demand=%d supply=%d before failing)", kerr, d, s)
			} else {
				logger.Printf("pruned kb market history older than %s: demand=%d supply=%d", histCutoff.UTC().Format(time.RFC3339), d, s)
			}
		}

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
