package main

import "slices"

// orderRung is one price level of a buy-order book: Qty units wanted at Price
// each. It is the source-agnostic input to fillOrderBook, shared by `sellable`
// (which builds rungs from serverapi.MarketOrder) and `demand` (which builds
// them from knowledge.MarketBuyOrderRow).
type orderRung struct {
	Price float64
	Qty   float64
}

// fillOrderBook walks rungs highest-price-first, selling up to `supply` units,
// taking min(remaining, rung.Qty) from each rung until supply is exhausted or
// the book runs out. This models a real market sale, where you fill the best
// offers first — as opposed to assuming the whole supply clears at the single
// top price.
//
// Returns total filled quantity, total proceeds (Σ price*qty), the
// proceeds-weighted average price (0 when nothing fills), and the per-rung
// fills in fill order. Rungs with Price<=0 or Qty<=0 are skipped so bad data
// can't manufacture phantom credits or burn supply on no-op fills. The sort is
// stable, so equal-price rungs keep their input order. Pure: no I/O, no
// globals.
func fillOrderBook(supply float64, rungs []orderRung) (qty, proceeds, avg float64, fills []sellableFill) {
	if supply <= 0 || len(rungs) == 0 {
		return 0, 0, 0, nil
	}
	sorted := slices.Clone(rungs)
	slices.SortStableFunc(sorted, func(a, b orderRung) int {
		// Higher price first; stable so input order breaks ties.
		switch {
		case a.Price > b.Price:
			return -1
		case a.Price < b.Price:
			return 1
		default:
			return 0
		}
	})
	remaining := supply
	for _, o := range sorted {
		if remaining <= 0 {
			break
		}
		if o.Price <= 0 || o.Qty <= 0 {
			continue
		}
		take := o.Qty
		if take > remaining {
			take = remaining
		}
		fills = append(fills, sellableFill{Price: o.Price, Qty: take, Proceeds: take * o.Price})
		qty += take
		proceeds += take * o.Price
		remaining -= take
	}
	if qty > 0 {
		avg = proceeds / qty
	}
	return qty, proceeds, avg, fills
}
