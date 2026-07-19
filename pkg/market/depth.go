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

// ConsumeAsks returns the residual ask ladder after filling qty units
// cheapest-first (the inverse bookkeeping of CostToAcquire). Levels fully
// consumed are dropped; a partially consumed level keeps its remaining
// Quantity. qty <= 0 returns the ladder unchanged; qty exceeding total
// depth returns an empty (nil) ladder. Does not mutate the input slice.
func ConsumeAsks(asks []AskLevel, qty float64) []AskLevel {
	if qty <= 0 {
		return asks
	}
	remaining := qty
	for i, lvl := range asks {
		if remaining < lvl.Quantity {
			// Level i is partially consumed: keep its remainder plus every
			// level above it. Build a fresh slice so the input backing array
			// is never mutated (the partial level is a new value; the tail is
			// copied by append).
			out := make([]AskLevel, 0, len(asks)-i)
			out = append(out, AskLevel{PriceEach: lvl.PriceEach, Quantity: lvl.Quantity - remaining})
			out = append(out, asks[i+1:]...)
			return out
		}
		remaining -= lvl.Quantity
	}
	// qty met or exceeded total depth: nothing left.
	return nil
}
