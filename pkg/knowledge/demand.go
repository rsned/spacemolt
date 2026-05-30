package knowledge

import "time"

// MarketBuyOrderRow is a single buy order captured from a view_market response,
// carrying Source so the report can distinguish Station Manager ("station")
// orders from player orders ("" — null source in the compact response).
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
