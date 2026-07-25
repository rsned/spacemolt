// Package pricing computes a suggested sell price for a craftable item from
// the live market cost of its inputs plus a profit margin. It powers the
// play_as `price` command: given an item's decomposition (recipe inputs or
// bill-of-materials ore), it prices each component three ways — Nearby
// (cheapest ask within N jumps), Market-best (cheapest ask anywhere) and
// Market-wide (mean ask across stations) — rolls the costs up per output unit,
// adds a margin line, and compares the result to the finished good's own
// current market price. Best sits beside the mean because one outlier listing
// can dominate a mean and hide the real sourcing floor.
package pricing

import (
	"context"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
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
	NearbyUnit float64
	MktUnit    float64
	// MktBestUnit is the cheapest ask at ANY station, ignoring distance. It
	// answers what the mean cannot: what this actually costs if you go get it.
	// A single outlier ask can dominate the mean (titanium_ore showed a 10000
	// mkt-avg), so the two figures are reported side by side.
	MktBestUnit  float64
	NearbyFound  bool
	MktFound     bool
	MktBestFound bool
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
func rollUp(comps []PricedComponent, outputUnits int, marginPct float64) (nearby, mkt, mktBest Basis) {
	if outputUnits <= 0 {
		outputUnits = 1
	}
	nearby.Total = len(comps)
	mkt.Total = len(comps)
	mktBest.Total = len(comps)
	for _, c := range comps {
		if c.NearbyFound {
			nearby.BuildCost += c.Qty * c.NearbyUnit
			nearby.Covered++
		}
		if c.MktFound {
			mkt.BuildCost += c.Qty * c.MktUnit
			mkt.Covered++
		}
		if c.MktBestFound {
			mktBest.BuildCost += c.Qty * c.MktBestUnit
			mktBest.Covered++
		}
	}
	finish := func(b *Basis) {
		b.PerUnit = b.BuildCost / float64(outputUnits)
		b.Margin = b.PerUnit * marginPct / 100
		b.Suggested = b.PerUnit + b.Margin
	}
	finish(&nearby)
	finish(&mkt)
	finish(&mktBest)
	return nearby, mkt, mktBest
}

// askPrices is one item's per-station asks reduced to the three reported
// bases. Kept as a struct rather than a return list because three
// value+found pairs as positional results is unreadable at the call site.
type askPrices struct {
	Nearby      float64 // cheapest ask reachable within hops
	NearbyFound bool
	Mkt         float64 // mean ask across every station
	MktFound    bool
	Best        float64 // cheapest ask at ANY station, distance ignored
	BestFound   bool
}

// askStats reduces one item's per-station sell asks (as returned by
// finditem.Find) to the Nearby, Market-mean and Market-best bases. A result
// with Jumps == finditem.JumpsUnknown or Jumps >= navigation.RouteInf is
// outside "nearby" but still counts toward both market-wide figures — Best is
// a sourcing floor, not a reachability claim. Asks of 0 are ignored.
func askStats(results []finditem.Result, hops int) askPrices {
	var out askPrices
	var sum float64
	var n int
	for _, r := range results {
		if r.BestPrice <= 0 {
			continue
		}
		sum += r.BestPrice
		n++
		if !out.BestFound || r.BestPrice < out.Best {
			out.Best, out.BestFound = r.BestPrice, true
		}
		if r.Jumps >= 0 && r.Jumps <= hops {
			if !out.NearbyFound || r.BestPrice < out.Nearby {
				out.Nearby, out.NearbyFound = r.BestPrice, true
			}
		}
	}
	if n > 0 {
		out.Mkt, out.MktFound = sum/float64(n), true
	}
	return out
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

// findLimit is passed to finditem.Find so no station is truncated away before
// we compute the market-wide mean (there are only a few dozen stations).
const findLimit = 1000

// PriceReport is the full pricing result for one decomposition of one item.
type PriceReport struct {
	ItemID      string            `json:"item_id"`
	RecipeName  string            `json:"recipe_name,omitempty"`
	OutputUnits int               `json:"output_units"`
	MarginPct   float64           `json:"margin_pct"`
	Components  []PricedComponent `json:"components"`
	Nearby      Basis             `json:"nearby"`
	Mkt         Basis             `json:"market"`
	// MktBest rolls up the cheapest ask found anywhere per component — the
	// realistic sourcing floor when the mean is skewed by an outlier listing.
	MktBest Basis `json:"market_best"`

	CurAskNearby float64 `json:"cur_ask_nearby,omitempty"`
	HasAskNearby bool    `json:"has_ask_nearby"`
	CurAskMkt    float64 `json:"cur_ask_mkt,omitempty"`
	HasAskMkt    bool    `json:"has_ask_mkt"`
	// CurAskMktBest is the finished good's cheapest ask anywhere.
	CurAskMktBest float64 `json:"cur_ask_mkt_best,omitempty"`
	HasAskMktBest bool    `json:"has_ask_mkt_best"`
	CurBid        float64 `json:"cur_bid,omitempty"`
	HasBid        bool    `json:"has_bid"`
	Class         string  `json:"class,omitempty"`
}

// Report prices every component of one decomposition (comps) on both bases,
// rolls them up per output unit, prices the finished good, and classifies.
// recipeName is surfaced in the header ("" for BoM / no-recipe). outputUnits
// is the recipe's output-per-run for recipe mode, or 1 for BoM mode (its
// quantities are already per unit).
func Report(ctx context.Context, col *market.Collector, kb knowledge.Base, fromSystem string, hops int, itemID, recipeName string, outputUnits int, comps []Component, marginPct float64) (*PriceReport, error) {
	rep := &PriceReport{ItemID: itemID, RecipeName: recipeName, OutputUnits: outputUnits, MarginPct: marginPct}

	priced := make([]PricedComponent, 0, len(comps))
	for _, c := range comps {
		results, err := finditem.Find(ctx, col, kb, c.ItemID, 0, fromSystem, findLimit)
		if err != nil {
			return nil, err
		}
		a := askStats(results, hops)
		priced = append(priced, PricedComponent{
			Component:   c,
			NearbyUnit:  a.Nearby,
			MktUnit:     a.Mkt,
			MktBestUnit: a.Best,
			NearbyFound: a.NearbyFound,
			MktFound:    a.MktFound, MktBestFound: a.BestFound,
		})
	}
	rep.Components = priced
	rep.Nearby, rep.Mkt, rep.MktBest = rollUp(priced, outputUnits, marginPct)

	// Finished good's own asks (nearby cheapest + market-wide mean + best anywhere).
	goodAsks, err := finditem.Find(ctx, col, kb, itemID, 0, fromSystem, findLimit)
	if err != nil {
		return nil, err
	}
	ga := askStats(goodAsks, hops)
	rep.CurAskNearby, rep.HasAskNearby = ga.Nearby, ga.NearbyFound
	rep.CurAskMkt, rep.HasAskMkt = ga.Mkt, ga.MktFound
	rep.CurAskMktBest, rep.HasAskMktBest = ga.Best, ga.BestFound

	// Finished good's best bid anywhere (instant-sell reference).
	stationPrices, err := col.GetItemStationPrices(ctx, itemID)
	if err != nil {
		return nil, err
	}
	for _, sp := range stationPrices {
		if sp.HasBuy && sp.BestBid > rep.CurBid {
			rep.CurBid, rep.HasBid = sp.BestBid, true
		}
	}

	ref := rep.CurAskMkt
	if rep.HasAskNearby {
		ref = rep.CurAskNearby
	}
	sug := rep.Mkt.Suggested
	if rep.Nearby.Complete() {
		sug = rep.Nearby.Suggested
	} else if !rep.Mkt.Complete() {
		sug = 0 // no complete basis -> no verdict
	}
	rep.Class = classify(ref, sug)
	return rep, nil
}
