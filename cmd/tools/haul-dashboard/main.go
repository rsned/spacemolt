// Command haul-dashboard renders a self-contained hauler dashboard HTML file
// from the durable haul_results + fleet_timeseries history in market.db and the
// live fleet-status.json. It is a one-shot generator — run it on a timer to keep
// the page fresh (Phase 2 of the hauler-dashboard design spec).
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/huddash"
	"github.com/rsned/spacemolt/pkg/market"
)

func main() {
	dbPath := flag.String("market-db-path", "data/market.db", "Path to market.db")
	statusFile := flag.String("status-file", "data/overmind/haul-status.json", "Path to a fleet status file (haul fleet by default)")
	periodName := flag.String("period", "hour", "Chart bucketing: hour|half_day|day")
	window := flag.Duration("window", 48*time.Hour, "How far back to include history")
	out := flag.String("out", "dashboard.html", "Output HTML file")
	flag.Parse()

	logger := log.New(os.Stderr, "[haul-dashboard] ", log.LstdFlags)

	period, err := huddash.ParsePeriod(*periodName)
	if err != nil {
		logger.Fatalf("%v", err)
	}

	col, err := market.Open(market.Config{DBPath: *dbPath, WAL: true})
	if err != nil {
		logger.Fatalf("open %s: %v", *dbPath, err)
	}
	defer col.Close() //nolint:errcheck

	ctx := context.Background()
	in, err := huddash.LoadInput(ctx, col, *statusFile, period, *window, time.Now())
	if err != nil {
		logger.Fatalf("load: %v", err)
	}

	html := huddash.Render(in)
	if err := os.WriteFile(*out, []byte(html), 0o644); err != nil { //nolint:gosec // operator-controlled output path
		logger.Fatalf("write %s: %v", *out, err)
	}
	logger.Printf("wrote %s (%d haulers, %d bytes, period %s, window %s)",
		*out, len(in.Agents), len(html), period.Name, window.String())
}
