package worker

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultHaulPoolLimit caps how many available opportunities a hauler considers.
const DefaultHaulPoolLimit = 50

// haulNearTieFraction: opportunities within this fraction of the top gross profit
// are reordered by proximity/chaining rather than raw profit.
const haulNearTieFraction = 0.10

// buildNameToID maps system display names to system ids from the KB. The arbitrage
// rows carry system *names*; the jump graph keys on *ids*. Last write wins on the
// (rare) duplicate name. Used by later tasks in the hauler implementation.
//nolint:unused
func buildNameToID(systems []knowledge.System) map[string]string {
	m := make(map[string]string, len(systems))
	for _, s := range systems {
		if s.Name != "" {
			m[s.Name] = s.ID
		}
	}
	return m
}

// rankedOpp pairs an opportunity with its resolved routing facts.
type rankedOpp struct {
	opp       market.ArbitrageOpportunity
	buySysID  string
	sellSysID string // "" if unresolved
	jumps     int    // current -> buySys
	chain     bool   // sellSys at/adjacent to another opp's buySys
}

// RankHaulOpportunities orders available opportunities best-first for a hauler at
// currentSystemID. Primary order is gross_profit descending; opportunities within
// haulNearTieFraction of the top gross are instead ordered by reposition cost
// (jumps current->buy), then a chaining bonus (sell at/adjacent to another opp's
// buy), then id. Opportunities whose buy-system name does not resolve to a known
// system id, or whose buy-system is unreachable, are dropped.
func RankHaulOpportunities(opps []market.ArbitrageOpportunity, currentSystemID string, nameToID map[string]string, graph navigation.JumpGraph) []market.ArbitrageOpportunity {
	resolved := make([]rankedOpp, 0, len(opps))
	buyTargets := make([]string, 0, len(opps))
	for _, o := range opps {
		buyID, ok := nameToID[o.FromSystemName]
		if !ok || buyID == "" {
			continue // can't route to the buy station
		}
		resolved = append(resolved, rankedOpp{opp: o, buySysID: buyID, sellSysID: nameToID[o.ToSystemName]})
		buyTargets = append(buyTargets, buyID)
	}
	if len(resolved) == 0 {
		return nil
	}

	dist := navigation.BFSJumps(graph, currentSystemID, buyTargets)

	reach := make([]rankedOpp, 0, len(resolved))
	for _, r := range resolved {
		d, ok := dist[r.buySysID]
		if !ok || d >= navigation.RouteInf {
			continue
		}
		r.jumps = d
		reach = append(reach, r)
	}
	if len(reach) == 0 {
		return nil
	}

	for i := range reach {
		reach[i].chain = sellChains(reach[i], reach, graph)
	}

	maxGross := 0.0
	for _, r := range reach {
		if r.opp.GrossProfit > maxGross {
			maxGross = r.opp.GrossProfit
		}
	}
	threshold := maxGross * (1 - haulNearTieFraction)

	band := make([]rankedOpp, 0, len(reach))
	rest := make([]rankedOpp, 0, len(reach))
	for _, r := range reach {
		if r.opp.GrossProfit >= threshold {
			band = append(band, r)
		} else {
			rest = append(rest, r)
		}
	}

	sort.SliceStable(band, func(i, j int) bool {
		if band[i].jumps != band[j].jumps {
			return band[i].jumps < band[j].jumps
		}
		if band[i].chain != band[j].chain {
			return band[i].chain // chaining opp sorts first
		}
		return band[i].opp.ID < band[j].opp.ID
	})
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].opp.GrossProfit != rest[j].opp.GrossProfit {
			return rest[i].opp.GrossProfit > rest[j].opp.GrossProfit
		}
		if rest[i].jumps != rest[j].jumps {
			return rest[i].jumps < rest[j].jumps
		}
		return rest[i].opp.ID < rest[j].opp.ID
	})

	out := make([]market.ArbitrageOpportunity, 0, len(reach))
	for _, r := range band {
		out = append(out, r.opp)
	}
	for _, r := range rest {
		out = append(out, r.opp)
	}
	return out
}

// sellChains reports whether r's sell-system is at or within one jump of any OTHER
// opportunity's buy-system (so the next run starts near r's drop-off).
func sellChains(r rankedOpp, all []rankedOpp, graph navigation.JumpGraph) bool {
	if r.sellSysID == "" {
		return false
	}
	for _, other := range all {
		if other.opp.ID == r.opp.ID {
			continue
		}
		if other.buySysID == r.sellSysID {
			return true
		}
		for _, nb := range graph[r.sellSysID] {
			if nb == other.buySysID {
				return true
			}
		}
	}
	return false
}

// sizeBuy returns how many units to buy: the opportunity quantity, capped by free
// cargo space and by what credits afford at askEach. Returns 0 when nothing is
// affordable or askEach is non-positive.
//nolint:unused
func sizeBuy(opp market.ArbitrageOpportunity, cargoFree, credits, askEach float64) float64 {
	if askEach <= 0 {
		return 0
	}
	qty := opp.Quantity
	if cargoFree < qty {
		qty = cargoFree
	}
	if affordable := math.Floor(credits / askEach); affordable < qty {
		qty = affordable
	}
	if qty < 0 {
		qty = 0
	}
	return qty
}

// OpportunityStore is the subset of *market.Collector the hauler needs. Defining it
// here keeps the engine testable with a fake and leaves pkg/market unmodified.
type OpportunityStore interface {
	GetOpportunities(ctx context.Context, status string, limit int) ([]market.ArbitrageOpportunity, error)
	ClaimOpportunity(ctx context.Context, id int, agentID string) (bool, error)
	CompleteOpportunity(ctx context.Context, id int, agentID string) (bool, error)
	ScanArbitrage(ctx context.Context, opts market.ScanOptions) (market.ScanResult, error)
}

// loadAvailable returns available opportunities, running one ScanArbitrage to
// refresh the pool when it is empty (haulers are the periodic scan trigger). Scan
// uses default options; it is idempotent under the write lock, so a redundant scan
// from concurrent haulers is harmless.
//nolint:unused
func loadAvailable(ctx context.Context, store OpportunityStore, limit int) ([]market.ArbitrageOpportunity, error) {
	opps, err := store.GetOpportunities(ctx, "available", limit)
	if err != nil {
		return nil, fmt.Errorf("haul: get opportunities: %w", err)
	}
	if len(opps) > 0 {
		return opps, nil
	}
	if _, err := store.ScanArbitrage(ctx, market.ScanOptions{}); err != nil {
		return nil, fmt.Errorf("haul: scan arbitrage: %w", err)
	}
	opps, err = store.GetOpportunities(ctx, "available", limit)
	if err != nil {
		return nil, fmt.Errorf("haul: get opportunities (post-scan): %w", err)
	}
	return opps, nil
}

// claimBest claims the first opportunity in ranked order still available. ok=false
// means every candidate was taken by another hauler first.
//nolint:unused
func claimBest(ctx context.Context, store OpportunityStore, ranked []market.ArbitrageOpportunity, agentID string) (market.ArbitrageOpportunity, bool, error) {
	for _, o := range ranked {
		ok, err := store.ClaimOpportunity(ctx, o.ID, agentID)
		if err != nil {
			return market.ArbitrageOpportunity{}, false, fmt.Errorf("haul: claim %d: %w", o.ID, err)
		}
		if ok {
			return o, true, nil
		}
	}
	return market.ArbitrageOpportunity{}, false, nil
}
