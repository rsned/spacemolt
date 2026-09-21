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

// BidLevel is one price level of the buy side of the book. Distinct from
// AskLevel so a caller cannot pass a ladder to the wrong walker: the two sort
// in opposite directions and mixing them silently misprices a route.
type BidLevel struct {
	PriceEach float64
	Quantity  float64
}

// ProceedsFromSale walks bids highest-first, selling up to qty. bids MUST be
// sorted descending by PriceEach. It is the mirror of CostToAcquire: total
// revenue for the units actually sold, how many were sellable, the
// volume-weighted average price received (0 when nothing sold), and whether
// the ladder was deep enough to absorb qty in full.
func ProceedsFromSale(bids []BidLevel, qty float64) (totalRevenue, filled, avgPrice float64, enoughDepth bool) {
	remaining := qty
	for _, lvl := range bids {
		if remaining <= 0 {
			break
		}
		take := lvl.Quantity
		if take > remaining {
			take = remaining
		}
		totalRevenue += take * lvl.PriceEach
		filled += take
		remaining -= take
	}
	if filled > 0 {
		avgPrice = totalRevenue / filled
	}
	enoughDepth = filled+1e-9 >= qty
	return totalRevenue, filled, avgPrice, enoughDepth
}

// OptimalArbitrage walks both books together and returns the profit-maximising
// trade: buy cheapest-first, sell highest-first, and stop at the unit where the
// bid no longer beats the ask. asks ascend by price, bids descend.
//
// This replaces valuing a route as (bestBid-bestAsk)*qty, which assumes every
// unit clears at the top of both books. Real ladders are stepped, so that
// overstates revenue and — worse — keeps counting "profit" on units whose bid
// has already fallen below the ask. Live on 2026-09-20, opportunity #1224069
// advertised 516,810 gross on platinum_ore at 140->350; the destination's best
// bid was 252 with only 1,366 behind it, and the realised figure on a
// 1,900-unit load was 171,682, 43% of the advertised pro-rata. Past ~4,366
// units the bids sat at 105, BELOW the 140 ask, so the naive maths was pricing
// loss-making volume as gain.
//
// Returned quantity is a ceiling to aim at, not a promise: both ladders are a
// snapshot, and a hauler with a smaller hold simply takes the cheapest prefix,
// which is the most profitable part of the run.
func OptimalArbitrage(asks []AskLevel, bids []BidLevel) (qty, cost, revenue, profit float64) {
	i, j := 0, 0
	askLeft, bidLeft := 0.0, 0.0
	if len(asks) > 0 {
		askLeft = asks[0].Quantity
	}
	if len(bids) > 0 {
		bidLeft = bids[0].Quantity
	}
	for i < len(asks) && j < len(bids) {
		// The marginal unit must earn more than it costs; once it does not,
		// every later unit is worse (asks only rise, bids only fall).
		if bids[j].PriceEach <= asks[i].PriceEach {
			break
		}
		take := min(askLeft, bidLeft)
		qty += take
		cost += take * asks[i].PriceEach
		revenue += take * bids[j].PriceEach
		askLeft -= take
		bidLeft -= take
		if askLeft <= 0 {
			if i++; i < len(asks) {
				askLeft = asks[i].Quantity
			}
		}
		if bidLeft <= 0 {
			if j++; j < len(bids) {
				bidLeft = bids[j].Quantity
			}
		}
	}
	return qty, cost, revenue, revenue - cost
}

// AskLadderDepth is the total number of units offered across every level of an
// ask ladder.
//
// This is the book's SUPPLY, deliberately including levels too expensive to be
// part of today's profitable arbitrage: it is what the allocator divides by
// cargo capacity to decide how many haulers a book can support, and a level
// that is unprofitable at this instant is still stock sitting at that station.
func AskLadderDepth(asks []AskLevel) float64 {
	var total float64
	for _, a := range asks {
		total += a.Quantity
	}
	return total
}
