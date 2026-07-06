// Package pricing computes a suggested sell price for a craftable item from
// the live market cost of its inputs plus a profit margin. It powers the
// play_as `price` command: given an item's decomposition (recipe inputs or
// bill-of-materials ore), it prices each component two ways — Nearby (cheapest
// ask within N jumps) and Market-wide (mean ask across stations) — rolls the
// costs up per output unit, adds a margin line, and compares the result to the
// finished good's own current market price.
package pricing

import (
	"github.com/rsned/spacemolt/pkg/finditem"
)

// Component is one input to price: a recipe input or a BoM base ore.
type Component struct {
	ItemID string
	Qty    float64
}

// PricedComponent annotates a Component with its resolved unit prices on each
// basis. A *Found flag is false when no station offered the component on that
// basis; the unit price is then zero and excluded from the roll-up.
type PricedComponent struct {
	Component
	NearbyUnit  float64
	MktUnit     float64
	NearbyFound bool
	MktFound    bool
}

// Basis is the rolled-up cost on one pricing basis (Nearby or Market-wide).
type Basis struct {
	BuildCost float64 // Σ qty×unit over components priced on this basis
	PerUnit   float64 // BuildCost / outputUnits
	Margin    float64 // PerUnit × marginPct/100
	Suggested float64 // PerUnit + Margin
	Covered   int     // components priced on this basis
	Total     int     // total components
}

// Complete reports whether every component was priced on this basis.
func (b Basis) Complete() bool { return b.Total > 0 && b.Covered == b.Total }

// rollUp turns priced components into the Nearby and Market-wide bases.
// outputUnits <= 0 is treated as 1 so the per-unit conversion degrades to
// identity.
func rollUp(comps []PricedComponent, outputUnits int, marginPct float64) (nearby, mkt Basis) {
	if outputUnits <= 0 {
		outputUnits = 1
	}
	nearby.Total = len(comps)
	mkt.Total = len(comps)
	for _, c := range comps {
		if c.NearbyFound {
			nearby.BuildCost += c.Qty * c.NearbyUnit
			nearby.Covered++
		}
		if c.MktFound {
			mkt.BuildCost += c.Qty * c.MktUnit
			mkt.Covered++
		}
	}
	finish := func(b *Basis) {
		b.PerUnit = b.BuildCost / float64(outputUnits)
		b.Margin = b.PerUnit * marginPct / 100
		b.Suggested = b.PerUnit + b.Margin
	}
	finish(&nearby)
	finish(&mkt)
	return nearby, mkt
}

// askStats reduces one item's per-station sell asks (as returned by
// finditem.Find) to a Nearby unit price (cheapest ask reachable within hops)
// and a Market-wide unit price (mean ask across every station). A result with
// Jumps == finditem.JumpsUnknown or Jumps >= navigation.RouteInf is outside
// "nearby" but still counts toward the market-wide mean. Asks of 0 are ignored.
func askStats(results []finditem.Result, hops int) (nearbyUnit float64, nearbyFound bool, mktUnit float64, mktFound bool) {
	var sum float64
	var n int
	for _, r := range results {
		if r.BestPrice <= 0 {
			continue
		}
		sum += r.BestPrice
		n++
		if r.Jumps >= 0 && r.Jumps <= hops {
			if !nearbyFound || r.BestPrice < nearbyUnit {
				nearbyUnit, nearbyFound = r.BestPrice, true
			}
		}
	}
	if n > 0 {
		mktUnit, mktFound = sum/float64(n), true
	}
	return nearbyUnit, nearbyFound, mktUnit, mktFound
}

// Verdicts comparing the finished good's current market ask to the
// cost-plus-margin suggestion.
const (
	ClassUnder = "UNDERPRICED"
	ClassOver  = "OVERPRICED"
	ClassFair  = "FAIRLY PRICED"
)

// classify compares the finished good's current market ask to the suggested
// price. Thresholds are cosmetic: ask >= 1.3× suggested is underpriced (parts
// cheap vs sale), ask <= 0.9× is overpriced (you'd list above the market).
// Returns "" when either figure is missing.
func classify(marketAsk, suggested float64) string {
	if marketAsk <= 0 || suggested <= 0 {
		return ""
	}
	ratio := marketAsk / suggested
	switch {
	case ratio >= 1.3:
		return ClassUnder
	case ratio <= 0.9:
		return ClassOver
	default:
		return ClassFair
	}
}
