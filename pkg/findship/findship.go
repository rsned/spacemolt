// Package findship answers "where can I buy this ship?" from the captured
// station ship listings, ranked by jump distance from the caller's system.
// It is the core behind the play_as find_ship command.
package findship

import (
	"cmp"
	"context"
	"slices"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// JumpsUnknown marks a result whose distance could not be computed.
const JumpsUnknown = finditem.JumpsUnknown

// DefaultLimit is how many listings find_ship shows without --limit.
const DefaultLimit = 10

// Result is one listing with its jump distance from the caller.
type Result struct {
	knowledge.StationShipListing
	Jumps int // JumpsUnknown if not computable; >= navigation.RouteInf if unreachable
}

// Rank orders listings nearest first, then cheapest, then by station id, and
// keeps the first limit (all when limit <= 0). kb may be nil: every result
// then carries JumpsUnknown and the order is by price.
func Rank(ctx context.Context, kb knowledge.Base, listings []knowledge.StationShipListing, fromSystem string, limit int) []Result {
	systems := make([]string, 0, len(listings))
	for _, l := range listings {
		systems = append(systems, l.SystemID)
	}
	dist := finditem.JumpsTo(ctx, kb, fromSystem, systems)

	out := make([]Result, len(listings))
	for i, l := range listings {
		out[i] = Result{StationShipListing: l, Jumps: JumpsUnknown}
		if dist == nil {
			continue
		}
		if d, ok := dist[l.SystemID]; ok {
			out[i].Jumps = d
		} else {
			out[i].Jumps = navigation.RouteInf
		}
	}
	slices.SortStableFunc(out, func(a, b Result) int {
		return cmp.Or(
			cmp.Compare(jumpRank(a.Jumps), jumpRank(b.Jumps)),
			cmp.Compare(a.Price, b.Price),
			cmp.Compare(a.StationID, b.StationID),
		)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// jumpRank orders known-near < known-far < unreachable < unknown.
func jumpRank(j int) int {
	if j == JumpsUnknown {
		return navigation.RouteInf + 1
	}
	return min(j, navigation.RouteInf)
}
