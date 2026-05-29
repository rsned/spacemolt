package knowledge

import "time"

// MarketDemandRow is the compact best-buy demand for one item at one station,
// captured from a view_market summary (no item_id). One row per (station, item).
type MarketDemandRow struct {
	StationID    string
	SystemID     string
	ItemID       string
	ItemName     string
	BestBuyPrice float64
	BuyQuantity  float64
	CapturedAt   time.Time
}

// MarketBuyOrderRow is a single buy order from a deep scan (view_market with an
// item_id), carrying Source so the report can distinguish Station Manager
// ("station") orders from player orders.
type MarketBuyOrderRow struct {
	StationID  string
	SystemID   string
	ItemID     string
	ItemName   string
	PriceEach  float64
	Quantity   float64
	Source     string
	CapturedAt time.Time
}
