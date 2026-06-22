// Command market-stats prints summary statistics for the market intelligence
// database (default: ~/spacemolt/spacemolt/data/market.db). Used to verify that
// station agents are feeding data into the market DB.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/rsned/spacemolt/pkg/market"
)

func main() {
	collector, err := market.Open(market.DefaultConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening market DB: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = collector.Close() }()

	stats, err := collector.GetStats(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting stats: %v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, line := range []string{
		"Market Database Statistics\n",
		"========================\n",
		fmt.Sprintf("Stations:\t%d\n", stats.StationCount),
		fmt.Sprintf("Items:\t%d\n", stats.ItemCount),
		fmt.Sprintf("Orders:\t%d\n", stats.OrderCount),
		fmt.Sprintf("OHLCV records:\t%d\n", stats.OHLCVCount),
		fmt.Sprintf("Latest capture:\t%s\n", stats.LatestCapture),
	} {
		if _, err := io.WriteString(w, line); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(1)
		}
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(1)
	}
}
