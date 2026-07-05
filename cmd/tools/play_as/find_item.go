package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// runFindItem implements the find_item REPL command:
//
//	find_item <item_id> [quantity] [--limit=N]
//
// It searches the market DB for stations selling the item (optionally with
// at least the given sell-side depth) and ranks them by jump distance from
// the current system, closest first.
func runFindItem(client game.GameClient, ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: find_item <item_id> [quantity] [--limit=N]")
	}
	if globalMarketCollector == nil {
		return fmt.Errorf("find_item: market DB not available (run with --market-db-path)")
	}

	itemID := args[0]
	var minQty float64
	limit := finditem.DefaultLimit
	for _, arg := range args[1:] {
		switch {
		case strings.HasPrefix(arg, "--limit="):
			n, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil || n <= 0 {
				return fmt.Errorf("find_item: --limit must be a positive integer")
			}
			limit = n
		default:
			q, err := strconv.ParseFloat(arg, 64)
			if err != nil || q < 0 {
				return fmt.Errorf("find_item: quantity must be a non-negative number, got %q", arg)
			}
			minQty = q
		}
	}

	fromSystem := ""
	if state := client.GetState(); state != nil {
		fromSystem = state.System.ID
	}

	results, err := finditem.Find(ctx, globalMarketCollector, globalKB, itemID, minQty, fromSystem, limit)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		if minQty > 0 {
			fmt.Printf("No stations in the market DB sell %s with ≥%s in stock.\n", itemID, trimFloat(minQty))
			fmt.Println("Try without the quantity to see shallower sellers.")
		} else {
			fmt.Printf("No stations in the market DB sell %s. (Coverage comes from marketbot captures — the item may exist but be untracked.)\n", itemID)
		}
		return nil
	}

	if minQty > 0 {
		fmt.Printf("Stations selling %s (≥%s in stock), closest first", itemID, trimFloat(minQty))
	} else {
		fmt.Printf("Stations selling %s, closest first", itemID)
	}
	if fromSystem != "" {
		fmt.Printf(" from %s", fromSystem)
	}
	fmt.Println(":")

	fmt.Printf("  %-5s %-12s %-10s %-28s %-20s %s\n", "HOPS", "PRICE", "QTY", "STATION", "SYSTEM", "CAPTURED")
	for _, r := range results {
		hops := "?"
		switch {
		case r.Jumps == finditem.JumpsUnknown:
		case r.Jumps >= navigation.RouteInf:
			hops = "∞"
		default:
			hops = strconv.Itoa(r.Jumps)
		}
		system := r.SystemName
		if system == "" {
			system = r.SystemID
		}
		fmt.Printf("  %-5s %-12s %-10s %-28s %-20s %s\n",
			hops, trimFloat(r.BestPrice), trimFloat(r.TotalQty),
			fmt.Sprintf("%s (%s)", r.StationName, r.StationID),
			system, captureAge(r.CapturedAt))
	}
	return nil
}

// trimFloat renders a float without trailing zeros (12 not 12.00, 3.5 kept).
func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// captureAge renders how long ago a capture happened, coarsely.
func captureAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
