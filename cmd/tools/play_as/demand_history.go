package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// demandHistoryDefaultLimit caps the visible buckets per station (one day of
// hourly samples) when --limit is not supplied.
const demandHistoryDefaultLimit = 24

// demandHistoryOptions are the parsed flags for `demand history`.
type demandHistoryOptions struct {
	item    string
	station string
	limit   int
}

// parseDemandHistoryOptions parses `demand history <item> [--station id] [--limit N]`.
// The first non-flag argument is the required (lower-cased) item id.
func parseDemandHistoryOptions(args []string) (demandHistoryOptions, error) {
	opts := demandHistoryOptions{limit: demandHistoryDefaultLimit}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			if opts.item == "" {
				opts.item = strings.ToLower(arg)
				continue
			}
			return opts, fmt.Errorf("demand history: unexpected argument %q", arg)
		}
		key, val, hasEq := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		next := func() (string, error) {
			if hasEq {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("demand history: %s requires a value", arg)
			}
			i++
			return args[i], nil
		}
		switch key {
		case "station":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.station = v
		case "limit":
			v, err := next()
			if err != nil {
				return opts, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return opts, fmt.Errorf("demand history: --limit: %w", err)
			}
			opts.limit = n
		default:
			return opts, fmt.Errorf("demand history: unknown flag %q", arg)
		}
	}
	if opts.item == "" {
		return opts, fmt.Errorf("usage: demand history <item> [--station <id>] [--limit N]")
	}
	return opts, nil
}

// runDemandHistory loads the demand-history time series for an item from the
// ledger and renders it grouped per station with a trend-direction summary. It
// reads purely from the KB (no live game state needed).
func runDemandHistory(ctx context.Context, args []string, format outputFormat) error {
	if globalKB == nil {
		return fmt.Errorf("demand history: no knowledge DB configured (start play_as with --db)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("demand history: knowledge DB is not SQLite-backed")
	}
	opts, err := parseDemandHistoryOptions(args)
	if err != nil {
		return err
	}
	samples, err := sqlite.LoadDemandHistory(ctx, opts.item, opts.station, opts.limit)
	if err != nil {
		return fmt.Errorf("demand history: load: %w", err)
	}
	switch format {
	case formatStyled:
		fmt.Print(renderDemandHistoryStyled(opts.item, samples))
	default:
		fmt.Print(renderDemandHistoryJSON(samples))
	}
	return nil
}

// trendDirection compares the first and last value in a window and returns an
// arrow: ↑ rising, ↓ falling, → flat.
func trendDirection(first, last float64) string {
	switch {
	case last > first:
		return "↑"
	case last < first:
		return "↓"
	default:
		return "→"
	}
}

func renderDemandHistoryJSON(samples []knowledge.DemandHistorySample) string {
	b, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\":%q}\n", err.Error())
	}
	return string(b) + "\n"
}

func renderDemandHistoryStyled(item string, samples []knowledge.DemandHistorySample) string {
	var sb strings.Builder
	if len(samples) == 0 {
		fmt.Fprintf(&sb, "No demand history for %q. Visit stations and run view_market to accumulate samples.\n", item)
		return sb.String()
	}
	fmt.Fprintf(&sb, "Demand history — %s\n", item)
	// Samples arrive ordered by (station, bucket asc); group consecutive stations.
	for i := 0; i < len(samples); {
		j := i
		for j < len(samples) && samples[j].StationID == samples[i].StationID {
			j++
		}
		group := samples[i:j]
		priceDir := trendDirection(group[0].BestPrice, group[len(group)-1].BestPrice)
		qtyDir := trendDirection(group[0].TotalQty, group[len(group)-1].TotalQty)
		fmt.Fprintf(&sb, "\nStation %s  (price %s, demand %s)\n", group[0].StationID, priceDir, qtyDir)
		fmt.Fprintf(&sb, "%-16s %10s %10s %10s %10s\n", "BUCKET", "PRICE", "DEMAND", "SM PRICE", "SM QTY")
		for _, s := range group {
			fmt.Fprintf(&sb, "%-16s %10.0f %10.0f %10.0f %10.0f\n",
				s.BucketAt.Format("2006-01-02 15h"), s.BestPrice, s.TotalQty, s.SMBestPrice, s.SMQty)
		}
		i = j
	}
	return sb.String()
}
