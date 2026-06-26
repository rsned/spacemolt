package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
)

// watchConfig holds parsed watch flags: the scan parameters plus the cadence.
type watchConfig struct {
	scan     scanConfig
	interval time.Duration // boundary spacing (default 30m, matches marketbot half_hourly capture)
	offset   time.Duration // delay past each boundary so the capture has settled
}

func parseWatchArgs(args []string) (watchConfig, error) {
	cfg := watchConfig{scan: defaultScanArgs(), interval: 30 * time.Minute, offset: 5 * time.Minute}
	var items string
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.StringVar(&cfg.scan.dbPath, "market-db-path", cfg.scan.dbPath, "path to market SQLite database")
	fs.Float64Var(&cfg.scan.opts.MinProfit, "min-profit", cfg.scan.opts.MinProfit, "gross-profit floor")
	fs.Float64Var(&cfg.scan.opts.MinPrice, "min-price", cfg.scan.opts.MinPrice, "per-order price floor (filters basement orders)")
	fs.Float64Var(&cfg.scan.opts.MinQuantity, "min-quantity", cfg.scan.opts.MinQuantity, "per-order depth floor")
	fs.DurationVar(&cfg.scan.opts.ExpiresIn, "expires", cfg.scan.opts.ExpiresIn, "opportunity TTL")
	fs.StringVar(&items, "items", "", "comma-separated item allowlist (default: all traded items)")
	fs.IntVar(&cfg.scan.opts.Limit, "limit", cfg.scan.opts.Limit, "cap on inserted rows")
	fs.DurationVar(&cfg.interval, "interval", cfg.interval, "scan cadence; aligns to UTC boundaries (e.g. 30m → :00/:30)")
	fs.DurationVar(&cfg.offset, "offset", cfg.offset, "delay past each boundary so the marketbot capture has settled")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	cfg.scan.opts.Items = splitItems(items)
	if cfg.interval <= 0 {
		return cfg, fmt.Errorf("--interval must be positive")
	}
	if cfg.offset < 0 {
		return cfg, fmt.Errorf("--offset must not be negative")
	}
	return cfg, nil
}

// nextScanAt returns the soonest scan time strictly after now: the current
// interval boundary (aligned to the top of the hour in UTC, since interval is
// expected to divide an hour) plus offset, rolling forward to the next boundary
// when that instant has already passed.
func nextScanAt(now time.Time, interval, offset time.Duration) time.Time {
	now = now.UTC()
	base := now.Truncate(interval)
	t := base.Add(offset)
	if !t.After(now) {
		t = base.Add(interval).Add(offset)
	}
	return t
}

// runWatch scans on a recurring, boundary-aligned schedule until interrupted.
// It keeps a single collector open for the lifetime of the loop; WAL mode lets
// the live fleet keep claiming/completing concurrently.
func runWatch(args []string) error {
	cfg, err := parseWatchArgs(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := market.Open(market.Config{DBPath: cfg.scan.dbPath})
	if err != nil {
		return fmt.Errorf("open %s: %w", cfg.scan.dbPath, err)
	}
	defer func() { _ = c.Close() }()

	fmt.Printf("arbitrage-scanner watch: every %s at boundary+%s (db=%s)\n", cfg.interval, cfg.offset, cfg.scan.dbPath)
	for {
		target := nextScanAt(time.Now(), cfg.interval, cfg.offset)
		fmt.Printf("next scan at %s (in %s)\n", target.Format(time.RFC3339), time.Until(target).Round(time.Second))
		if err := sleepUntil(ctx, target); err != nil {
			fmt.Println("watch: shutting down")
			return nil
		}
		res, err := c.ScanArbitrage(ctx, cfg.scan.opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan error: %v (will retry at next boundary)\n", err)
			continue
		}
		fmt.Printf("scan @ %s: expired %d available, inserted %d\n",
			res.GeneratedAt.Format(time.RFC3339), res.Expired, res.Inserted)
	}
}

// sleepUntil blocks until t, or returns ctx.Err() if the context ends first.
func sleepUntil(ctx context.Context, t time.Time) error {
	d := time.Until(t)
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
