package knowledge

import "time"

// MarketBuyOrderRow is a single buy order captured from a view_market response,
// carrying Source so the report can distinguish Station Manager ("station")
// orders from player orders ("" — null source in the compact response).
// MyQuantity is the slice of Quantity owned by the capturing player (from the
// compact response's my_quantity), letting the demand report skip rows whose
// demand is entirely the player's own buy orders.
type MarketBuyOrderRow struct {
	StationID  string
	SystemID   string
	ItemID     string
	ItemName   string
	PriceEach  float64
	Quantity   float64
	MyQuantity float64
	Source     string
	CapturedAt time.Time
}

// DemandHistorySample is one (station, item, hourly bucket) aggregate of
// buy-order demand, persisted to market_demand_history to build a time series.
// BucketAt is the capture time truncated to the bucket size (the upsert key);
// CapturedAt is the actual last observation time within that bucket. SMBestPrice
// and SMQty are the Station-Manager (source=="station") slice of the demand.
type DemandHistorySample struct {
	StationID   string    `json:"station_id"`
	SystemID    string    `json:"system_id"`
	ItemID      string    `json:"item_id"`
	ItemName    string    `json:"item_name"`
	BucketAt    time.Time `json:"bucket_at"`
	CapturedAt  time.Time `json:"captured_at"`
	BestPrice   float64   `json:"best_price"`
	TotalQty    float64   `json:"total_qty"`
	SMBestPrice float64   `json:"sm_best_price"`
	SMQty       float64   `json:"sm_qty"`
	OrderCount  int       `json:"order_count"`
}
