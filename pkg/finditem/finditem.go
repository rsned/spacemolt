// Package finditem answers "where can I buy this item?" by composing the
// market database's sell-side availability with jump distances from the
// knowledge base's system graph. It is the shared core behind the play_as
// find_item command and is callable from worker roles and tools.
package finditem

import (
	"context"
	"fmt"
	"sort"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultLimit is how many stations Find returns when the caller passes
// limit <= 0.
const DefaultLimit = 5

// JumpsUnknown marks a result whose distance could not be computed (no
// knowledge base, or the station's system is absent from the graph).
const JumpsUnknown = -1

// Result is one station selling the item, ranked by distance then price.
type Result struct {
	market.ItemSeller
	Jumps int `json:"jumps"` // JumpsUnknown if not computable
}

// Find returns the stations selling itemID with at least minQty total
// sell-side depth (0 = no floor), sorted by jump distance from fromSystem,
// then by price, and truncated to limit (DefaultLimit if <= 0).
//
// kb may be nil: results then carry JumpsUnknown and sort by price alone.
// Stations in systems unreachable from fromSystem sort last.
func Find(ctx context.Context, col *market.Collector, kb knowledge.Base, itemID string, minQty float64, fromSystem string, limit int) ([]Result, error) {
	if col == nil {
		return nil, fmt.Errorf("finditem: market collector is required")
	}
	if limit <= 0 {
		limit = DefaultLimit
	}

	sellers, err := col.FindItemSellers(ctx, itemID, minQty)
	if err != nil {
		return nil, fmt.Errorf("finditem: %w", err)
	}
	if len(sellers) == 0 {
		return nil, nil
	}

	dist := jumpDistances(ctx, kb, fromSystem, sellers)

	results := make([]Result, len(sellers))
	for i, s := range sellers {
		results[i] = Result{ItemSeller: s, Jumps: JumpsUnknown}
		if dist != nil && s.SystemID != "" {
			if d, ok := dist[s.SystemID]; ok {
				results[i].Jumps = d
			}
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Jumps != results[j].Jumps {
			return jumpRank(results[i].Jumps) < jumpRank(results[j].Jumps)
		}
		if results[i].BestPrice != results[j].BestPrice {
			return results[i].BestPrice < results[j].BestPrice
		}
		return results[i].StationID < results[j].StationID
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// jumpDistances computes BFS jump counts from fromSystem to each seller's
// system, or returns nil when distances cannot be computed. The returned map
// is keyed by the seller's SystemID exactly as stored in the market DB.
func jumpDistances(ctx context.Context, kb knowledge.Base, fromSystem string, sellers []market.ItemSeller) map[string]int {
	if kb == nil || fromSystem == "" {
		return nil
	}
	conns, err := kb.GetConnections(ctx)
	if err != nil || len(conns) == 0 {
		return nil
	}
	graph := navigation.JumpGraphFromConnections(conns)

	// Some market.db station rows store the system's display name ("Sol",
	// "Trader's Rest") instead of its id ("sol", "traders_rest"), depending
	// on which capturer wrote them. Resolve names to ids via the KB so those
	// stations still rank by distance.
	nameToID := make(map[string]string)
	if systems, sysErr := kb.GetSystems(ctx); sysErr == nil {
		for _, s := range systems {
			if s.Name != "" {
				nameToID[s.Name] = s.ID
			}
		}
	}
	resolve := func(sysID string) string {
		if _, ok := graph[sysID]; ok {
			return sysID
		}
		if id, ok := nameToID[sysID]; ok {
			return id
		}
		return sysID
	}

	seen := make(map[string]bool, len(sellers))
	targets := make([]string, 0, len(sellers))
	for _, s := range sellers {
		if s.SystemID == "" {
			continue
		}
		if canonical := resolve(s.SystemID); !seen[canonical] {
			seen[canonical] = true
			targets = append(targets, canonical)
		}
	}
	dist := navigation.BFSJumps(graph, resolve(fromSystem), targets)

	out := make(map[string]int, len(sellers))
	for _, s := range sellers {
		if s.SystemID == "" {
			continue
		}
		if d, ok := dist[resolve(s.SystemID)]; ok {
			out[s.SystemID] = d
		}
	}
	return out
}

// jumpRank orders distances so known-near < known-far < unreachable < unknown.
func jumpRank(jumps int) int {
	if jumps == JumpsUnknown {
		return navigation.RouteInf + 1
	}
	return jumps
}
