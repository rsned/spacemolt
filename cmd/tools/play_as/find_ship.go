package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/findship"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// runFindShip implements the find_ship REPL command:
//
//	find_ship <class or name> [--limit=N]
//
// It searches the captured station ship listings (the KB's ship_listings,
// latest capture per station) for a class id or ship name containing the
// query and ranks them by jump distance from the current system.
func runFindShip(client game.GameClient, ctx context.Context, args []string) error {
	limit := findship.DefaultLimit
	var words []string
	for _, arg := range args {
		if v, ok := strings.CutPrefix(arg, "--limit="); ok {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fmt.Errorf("find_ship: --limit must be a positive integer")
			}
			limit = n
			continue
		}
		words = append(words, arg)
	}
	query := strings.Join(words, " ")
	if query == "" {
		return fmt.Errorf("usage: find_ship <class or name> [--limit=N]")
	}
	kb, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok || kb == nil {
		return fmt.Errorf("find_ship: knowledge base not available")
	}
	listings, err := kb.SearchShipListings(ctx, query)
	if err != nil {
		return err
	}
	if len(listings) == 0 {
		fmt.Printf("No captured ship listings match %q. (Coverage is the stations our workers have browsed; each keeps only its latest capture.)\n", query)
		return nil
	}

	fromSystem := ""
	if state := client.GetState(); state != nil {
		fromSystem = state.System.ID
	}
	results := findship.Rank(ctx, globalKB, listings, fromSystem, limit)

	fmt.Printf("Ship listings matching %q (%d found), closest first", query, len(listings))
	if fromSystem != "" {
		fmt.Printf(" from %s", fromSystem)
	}
	fmt.Println(":")
	fmt.Printf("  %-5s %-10s %-22s %-4s %-9s %-4s %-30s %-16s %-10s %s\n",
		"HOPS", "PRICE", "CLASS", "TIER", "HULL", "MODS", "STATION", "SYSTEM", "CAPTURED", "SELLER")
	for _, r := range results {
		hops := "?"
		switch {
		case r.Jumps == findship.JumpsUnknown:
		case r.Jumps >= navigation.RouteInf:
			hops = "∞"
		default:
			hops = strconv.Itoa(r.Jumps)
		}
		hull := strconv.Itoa(r.MaxHull)
		if r.Hull >= 0 {
			hull = fmt.Sprintf("%d/%d", r.Hull, r.MaxHull)
		}
		fmt.Printf("  %-5s %-10d %-22s %-4d %-9s %-4d %-30s %-16s %-10s %s\n",
			hops, r.Price, r.ClassID, r.Tier, hull, r.ModulesCount,
			r.StationName, r.SystemName, captureAge(r.CapturedAt), r.Seller)
	}
	if len(listings) > len(results) {
		fmt.Printf("  … %d more; use --limit=N\n", len(listings)-len(results))
	}
	return nil
}
