package market

import "sort"

// pricedQty is one price level and the units offered at it.
type pricedQty struct {
	price float64
	qty   float64
}

// weightedMedian returns the price at which half the UNITS sit — the
// volume-weighted median of a book — and whether one exists.
//
// It is stored beside vwap because the two together read the SHAPE of a book,
// which high/low cannot. Both are volume-based, so the gap between them is
// pure skew: a vwap well below the median means the cheap end is thin (a token
// listing under a wall of expensive asks), and a vwap above it means the
// opposite. That thin-cheap-tranche shape is precisely what made best-bid
// arbitrage pricing overstate profit roughly threefold before OptimalArbitrage
// began walking the ladder.
//
// Unlike an unweighted median it cannot be moved by a one-unit listing, which
// matters most on the buy side where bids cluster at the floor with occasional
// outliers.
//
// Ties take the cheaper side: on an exact half-and-half split the median reads
// low rather than high, the same conservative direction the anti-gouge
// reference price uses. Returns ok=false when there are no units at all —
// reporting 0.0 would read as a free item.
func weightedMedian(levels []pricedQty) (float64, bool) {
	var total float64
	for _, l := range levels {
		if l.qty > 0 {
			total += l.qty
		}
	}
	if total <= 0 {
		return 0, false
	}
	sorted := make([]pricedQty, 0, len(levels))
	for _, l := range levels {
		if l.qty > 0 {
			sorted = append(sorted, l)
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].price < sorted[j].price })

	half := total / 2
	var seen float64
	for _, l := range sorted {
		seen += l.qty
		// >= not >: reaching exactly half means this level completes the
		// cheaper half, so an even split resolves downward to its price.
		if seen >= half {
			return l.price, true
		}
	}

	return sorted[len(sorted)-1].price, true
}
