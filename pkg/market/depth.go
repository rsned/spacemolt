// Package market — order-book depth helpers.
package market

// AskLevel is one price level of the sell side of the book.
type AskLevel struct {
	PriceEach float64
	Quantity  float64
}

// CostToAcquire walks asks cheapest-first, filling up to qty. asks MUST be
// sorted ascending by PriceEach. It returns the total cost of the filled
// units, how many units were fillable, the volume-weighted average price
// (0 when nothing filled), and whether the ladder had enough depth to fill
// qty in full.
func CostToAcquire(asks []AskLevel, qty float64) (totalCost, filled, avgPrice float64, enoughDepth bool) {
	remaining := qty
	for _, lvl := range asks {
		if remaining <= 0 {
			break
		}
		take := lvl.Quantity
		if take > remaining {
			take = remaining
		}
		totalCost += take * lvl.PriceEach
		filled += take
		remaining -= take
	}
	if filled > 0 {
		avgPrice = totalCost / filled
	}
	enoughDepth = filled+1e-9 >= qty
	return totalCost, filled, avgPrice, enoughDepth
}
