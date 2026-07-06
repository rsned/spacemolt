// Package pricing computes a suggested sell price for a craftable item from
// the live market cost of its inputs plus a profit margin. It powers the
// play_as `price` command: given an item's decomposition (recipe inputs or
// bill-of-materials ore), it prices each component two ways — Nearby (cheapest
// ask within N jumps) and Market-wide (mean ask across stations) — rolls the
// costs up per output unit, adds a margin line, and compares the result to the
// finished good's own current market price.
package pricing

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
