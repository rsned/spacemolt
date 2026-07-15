package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// arbRow is one ranked on-the-way arbitrage opportunity.
type arbRow struct {
	Opp        market.ArbitrageOpportunity
	Detour     int     // extra jumps the side-trip adds over the direct cur->dest route (>=0)
	BuyOut     int     // jumps from current system to the buy system (how far until you can pick up)
	SellToDest int     // jumps from the sell system to the destination
	Net        float64 // GrossProfit minus the marginal detour fuel cost
}

// rankDetourArbitrage keeps opportunities that are anchored near an endpoint of
// the operator's trip — the buy system within `near` jumps of the current
// system OR the sell system within `near` jumps of the destination — and whose
// detour over the direct cur->dest route is <= budget. It orders the survivors
// by marginal net-of-fuel profit descending and returns the first `limit`
// (limit<=0 = all).
//
//	detour = (cur->buy) + (buy->sell) + (sell->dest) - (cur->dest), clamped at 0
//	keep   = detour <= budget  AND  (near < 0  OR  cur->buy <= near  OR  sell->dest <= near)
//	net    = GrossProfit - detour*fuelPerJump*priceOf(buy station)
//
// The near-endpoint gate exists because detour-total alone is a poor proxy for
// "actionable": a haul buried in a pocket that happens to sit near the straight
// line between cur and dest (e.g. a low-degree cul-de-sac forming a tight
// triangle) scores a tiny detour yet has its pickup many jumps out and its
// dropoff many jumps from dest. Requiring proximity to an endpoint surfaces
// hauls the operator can actually start soon or finish at their destination.
// near < 0 disables the gate (detour-budget only). A future "scenic route" mode
// could instead accept mid-route hauls within +/-N of the traveled path.
//
// fuelPerJump<=0 or a nil priceOf disables the fuel term (net == gross), the
// graceful-degradation path. nameToID maps a lowercased system NAME to its
// canonical id. Opportunities whose buy/sell system name does not resolve, or
// whose legs are unreachable, are skipped and counted. If dest is unreachable
// from cur, no rows are returned (the caller reports that separately).
func rankDetourArbitrage(
	opps []market.ArbitrageOpportunity,
	curSys, destSys string,
	graph navigation.JumpGraph,
	nameToID map[string]string,
	budget int,
	near int,
	fuelPerJump int,
	priceOf func(stationID string) float64,
	limit int,
) (rows []arbRow, skipped int) {
	baseline := arbJumps(graph, curSys, destSys)
	if baseline < 0 {
		return nil, 0
	}
	for _, o := range opps {
		buySys, ok1 := nameToID[strings.ToLower(o.FromSystemName)]
		sellSys, ok2 := nameToID[strings.ToLower(o.ToSystemName)]
		if !ok1 || !ok2 || buySys == "" || sellSys == "" {
			skipped++
			continue
		}
		a := arbJumps(graph, curSys, buySys)
		b := arbJumps(graph, buySys, sellSys)
		c := arbJumps(graph, sellSys, destSys)
		if a < 0 || b < 0 || c < 0 {
			skipped++
			continue
		}
		detour := max(a+b+c-baseline, 0)
		if detour > budget {
			continue
		}
		// Near-endpoint gate: buy soon after departure OR sell close to dest.
		if near >= 0 && a > near && c > near {
			continue
		}
		fuelCost := 0.0
		if fuelPerJump > 0 && priceOf != nil {
			fuelCost = float64(detour*fuelPerJump) * priceOf(o.FromStationID)
		}
		rows = append(rows, arbRow{Opp: o, Detour: detour, BuyOut: a, SellToDest: c, Net: o.GrossProfit - fuelCost})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Net != rows[j].Net {
			return rows[i].Net > rows[j].Net
		}
		if rows[i].Detour != rows[j].Detour {
			return rows[i].Detour < rows[j].Detour
		}
		return rows[i].Opp.ID < rows[j].Opp.ID
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, skipped
}

// arbJumps returns the jump count from `from` to `to`, or -1 if unreachable or
// either id is empty.
func arbJumps(graph navigation.JumpGraph, from, to string) int {
	if from == "" || to == "" {
		return -1
	}
	if from == to {
		return 0
	}
	d := navigation.BFSJumps(graph, from, []string{to})
	j, ok := d[to]
	if !ok || j >= navigation.RouteInf {
		return -1
	}
	return j
}

// arbFreeFuelStations are stations that refuel for free (the databot faction's
// ally pump); fuel priced at one of these costs 0. Mirrors
// worker.haulFreeFuelStations. A future refinement can data-drive this.
var arbFreeFuelStations = map[string]bool{
	"grand_exchange_station": true,
}

// arbFuelPriceSource is the market subset used to price fuel (satisfied by
// *market.Collector).
type arbFuelPriceSource interface {
	GetStationFuelPrice(ctx context.Context, stationID string) (allIn int, capturedAt time.Time, ok bool, err error)
	MedianStationFuelAllIn(ctx context.Context) (median int, ok bool, err error)
}

// buildArbPriceOf returns a station->creditsPerUnit fuel resolver: 0 for
// free-pump stations, the captured all-in when present, else the galaxy median
// (probed once here), else 0. A nil source yields a constant-0 resolver.
// Mirrors worker.buildPriceOf.
func buildArbPriceOf(ctx context.Context, src arbFuelPriceSource) func(string) float64 {
	if src == nil {
		return func(string) float64 { return 0 }
	}
	median, medianOK, _ := src.MedianStationFuelAllIn(ctx)
	return func(stationID string) float64 {
		if arbFreeFuelStations[stationID] {
			return 0
		}
		if allIn, _, ok, err := src.GetStationFuelPrice(ctx, stationID); err == nil && ok {
			return float64(allIn)
		}
		if medianOK {
			return float64(median)
		}
		return 0
	}
}

// runFindArbitrage lists available arbitrage opportunities that are a minimal
// detour from the operator's current system toward <dest>.
//
// Usage: find_arbitrage <dest> [--detour N] [--limit N]
func runFindArbitrage(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	if globalMarketCollector == nil {
		return fmt.Errorf("find_arbitrage requires the market database, which is not available")
	}
	if globalKB == nil {
		return fmt.Errorf("find_arbitrage requires the knowledge base, which is not available")
	}

	positional, flags := partitionFlags(parts[1:])
	if len(positional) == 0 {
		return fmt.Errorf("usage: find_arbitrage <dest> [--detour N] [--limit N]")
	}
	destToken := strings.Join(positional, " ")
	budget := 3
	if v, ok := flags["detour"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			budget = n
		}
	}
	limit := 10
	if v, ok := flags["limit"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	// near-endpoint gate: keep only opps whose buy is within `near` jumps of
	// current OR whose sell is within `near` of dest. --near -1 disables it.
	near := 3
	if v, ok := flags["near"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			near = n
		}
	}

	state := client.GetState()
	if state == nil || state.System.ID == "" {
		return fmt.Errorf("cannot determine current system; try get_status first")
	}
	curID := state.System.ID

	systems, err := globalKB.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("load systems: %w", err)
	}
	byID := make(map[string]string, len(systems))
	byName := make(map[string]string, len(systems))
	nameOf := make(map[string]string, len(systems))
	for _, s := range systems {
		byID[strings.ToLower(s.ID)] = s.ID
		if s.Name != "" {
			byName[strings.ToLower(s.Name)] = s.ID
		}
		nameOf[s.ID] = s.Name
	}
	destID, ok := resolveSystemToken(destToken, byID, byName)
	if !ok {
		return fmt.Errorf("unknown destination system: %q", destToken)
	}

	conns, err := globalKB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("load connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)

	baseline := arbJumps(graph, curID, destID)
	if baseline < 0 {
		return fmt.Errorf("no known jump route from %s to %s",
			displayName(curID, nameOf), displayName(destID, nameOf))
	}

	opps, err := globalMarketCollector.GetOpportunities(ctx, "available", 300)
	if err != nil {
		return fmt.Errorf("load opportunities: %w", err)
	}

	fuelPerJump, _ := currentJumpFuel(client, ctx, destID)
	priceOf := buildArbPriceOf(ctx, globalMarketCollector)

	rows, skipped := rankDetourArbitrage(opps, curID, destID, graph, byName, budget, near, fuelPerJump, priceOf, limit)

	renderArbitrage(rows, skipped, baseline, budget, near, curID, destID, nameOf, format)
	return nil
}

// renderArbitrage prints the ranked opportunities as a styled table (or JSON).
func renderArbitrage(rows []arbRow, skipped, baseline, budget, near int, curID, destID string, nameOf map[string]string, format outputFormat) {
	if format != formatStyled {
		type outRow struct {
			ID             int     `json:"id"`
			Item           string  `json:"item"`
			Quantity       float64 `json:"quantity"`
			BuyAt          string  `json:"buy_at"`
			SellAt         string  `json:"sell_at"`
			BuyJumpsOut    int     `json:"buy_jumps_out"`
			SellJumpsToDst int     `json:"sell_jumps_to_dest"`
			Detour         int     `json:"detour_jumps"`
			Gross          float64 `json:"gross_profit"`
			Net            float64 `json:"net_of_fuel"`
		}
		out := struct {
			BaselineJumps int      `json:"baseline_jumps"`
			DetourBudget  int      `json:"detour_budget"`
			NearEndpoint  int      `json:"near_endpoint"`
			Skipped       int      `json:"skipped"`
			Rows          []outRow `json:"rows"`
		}{BaselineJumps: baseline, DetourBudget: budget, NearEndpoint: near, Skipped: skipped}
		for _, r := range rows {
			out.Rows = append(out.Rows, outRow{
				ID: r.Opp.ID, Item: r.Opp.ItemName, Quantity: r.Opp.Quantity,
				BuyAt: r.Opp.FromStationName, SellAt: r.Opp.ToStationName,
				BuyJumpsOut: r.BuyOut, SellJumpsToDst: r.SellToDest,
				Detour: r.Detour, Gross: r.Opp.GrossProfit, Net: r.Net,
			})
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			fmt.Printf("{\"error\":%q}\n", err.Error())
			return
		}
		fmt.Println(string(b))
		return
	}

	nearNote := fmt.Sprintf(", buy≤%dj out or sell≤%dj to dest", near, near)
	if near < 0 {
		nearNote = ""
	}
	fmt.Printf("\nArbitrage on the way: %s → %s (%d jump%s direct, detour ≤ %d%s)\n",
		displayName(curID, nameOf), displayName(destID, nameOf), baseline, plural(baseline), budget, nearNote)
	if len(rows) == 0 {
		fmt.Printf("  No opportunities near either endpoint within a %d-jump detour.\n", budget)
	}
	for _, r := range rows {
		item := r.Opp.ItemName
		if item == "" {
			item = r.Opp.ItemID
		}
		fmt.Printf("  #%d  %s x%.0f  buy@%s (%dj out) → sell@%s (%dj to dest)  +%d detour  gross %.0f  net %.0f\n",
			r.Opp.ID, item, r.Opp.Quantity, r.Opp.FromStationName, r.BuyOut,
			r.Opp.ToStationName, r.SellToDest, r.Detour, r.Opp.GrossProfit, r.Net)
	}
	if skipped > 0 {
		fmt.Printf("  (%d opportunit%s skipped: unresolved or unreachable systems)\n",
			skipped, map[bool]string{true: "y", false: "ies"}[skipped == 1])
	}
	fmt.Println("  Claim one with: claim_arbitrage <id>")
}

// runClaimArbitrage claims an opportunity for this operator so the hauler fleet
// skips it. Usage: claim_arbitrage <id>
func runClaimArbitrage(ctx context.Context, parts []string) error {
	if globalMarketCollector == nil {
		return fmt.Errorf("claim_arbitrage requires the market database, which is not available")
	}
	if len(parts) < 2 {
		return fmt.Errorf("usage: claim_arbitrage <id>")
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid opportunity id %q (must be an integer)", parts[1])
	}
	ok, err := globalMarketCollector.ClaimOpportunity(ctx, id, globalAgentID)
	if err != nil {
		return fmt.Errorf("claim opportunity %d: %w", id, err)
	}
	if !ok {
		fmt.Printf("Could not claim #%d — already claimed, completed, or gone.\n", id)
		return nil
	}
	fmt.Printf("Claimed #%d — the hauler fleet will now skip it. Release with: release_arbitrage %d\n", id, id)
	return nil
}

// runReleaseArbitrage releases an opportunity this operator previously claimed.
// Usage: release_arbitrage <id>
func runReleaseArbitrage(ctx context.Context, parts []string) error {
	if globalMarketCollector == nil {
		return fmt.Errorf("release_arbitrage requires the market database, which is not available")
	}
	if len(parts) < 2 {
		return fmt.Errorf("usage: release_arbitrage <id>")
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid opportunity id %q (must be an integer)", parts[1])
	}
	ok, err := globalMarketCollector.ReleaseOpportunity(ctx, id, globalAgentID)
	if err != nil {
		return fmt.Errorf("release opportunity %d: %w", id, err)
	}
	if !ok {
		fmt.Printf("Nothing to release for #%d — not held by this agent.\n", id)
		return nil
	}
	fmt.Printf("Released #%d — it is available to the fleet again.\n", id)
	return nil
}
