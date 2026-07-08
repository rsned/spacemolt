package market

// NotForSalePrice is the game's "not for sale" sentinel price. The server stamps
// this on order-book rungs that are placeholders rather than genuine offers (a
// round 1000000 sibling is also documented in the wild). It is NOT a real ask.
//
// view_market exposes only resting orders — there is no executed-trade feed — so
// everything in market_orders/market_ohlcv is order-book data. A sentinel rung
// that never trades must be excluded from every aggregate: folding it into
// volume/vwap/high drags the number wildly (iron_ore's sell VWAP read 382cr
// against a ~2cr live ask before this filter). The highest genuine observed
// price is ~600k, well clear of the cutoff, so the exclusion is safe.
const NotForSalePrice = 999999.0

// tradeablePrice reports whether a price is a genuine, tradeable offer: strictly
// positive and below the not-for-sale sentinel. Use it as the single gate for
// which order-book rungs feed price/volume aggregates.
func tradeablePrice(p float64) bool {
	return p > 0 && p < NotForSalePrice
}

// notForSaleSQL is NotForSalePrice as a bare SQL numeric literal, for embedding
// in aggregate filters (e.g. "price_each < "+notForSaleSQL) that must exclude
// sentinel rungs. Kept in sync with the constant so the cutoff lives in one place.
const notForSaleSQL = "999999.0"
