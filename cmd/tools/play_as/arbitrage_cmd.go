package main

import (
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// arbRow is one ranked on-the-way arbitrage opportunity.
type arbRow struct {
	Opp    market.ArbitrageOpportunity
	Detour int     // extra jumps the side-trip adds over the direct cur->dest route (>=0)
	Net    float64 // GrossProfit minus the marginal detour fuel cost
}

// rankDetourArbitrage keeps opportunities whose detour over the direct
// cur->dest route is <= budget, orders them by marginal net-of-fuel profit
// descending, and returns the first `limit` (limit<=0 = all).
//
//	detour = (cur->buy) + (buy->sell) + (sell->dest) - (cur->dest), clamped at 0
//	net    = GrossProfit - detour*fuelPerJump*priceOf(buy station)
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
		fuelCost := 0.0
		if fuelPerJump > 0 && priceOf != nil {
			fuelCost = float64(detour*fuelPerJump) * priceOf(o.FromStationID)
		}
		rows = append(rows, arbRow{Opp: o, Detour: detour, Net: o.GrossProfit - fuelCost})
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
