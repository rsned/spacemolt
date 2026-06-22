package market

import "time"

// Item represents a tradeable item in the market catalog.
type Item struct {
	ItemID         string `json:"item_id"`
	ItemName       string `json:"item_name"`
	Category       string `json:"category"`
	FirstSeenUTC   string `json:"first_seen_utc"`
	LastUpdatedUTC string `json:"last_updated_utc"`
}

// Station represents a station/POI with a market.
type Station struct {
	StationID      string `json:"station_id"`
	StationName    string `json:"station_name"`
	SystemID       string `json:"system_id"`
	SystemName     string `json:"system_name"`
	FirstSeenUTC   string `json:"first_seen_utc"`
	LastUpdatedUTC string `json:"last_updated_utc"`
}

// Order represents a single buy or sell order from the market.
type Order struct {
	StationID  string    `json:"station_id"`
	ItemID     string    `json:"item_id"`
	ItemName   string    `json:"item_name"`
	Side       string    `json:"side"` // "buy" or "sell"
	PriceEach  float64   `json:"price_each"`
	Quantity   float64   `json:"quantity"`
	MyQuantity float64   `json:"my_quantity"`
	Source     string    `json:"source"`
	CapturedAt time.Time `json:"captured_at"`
	BucketUTC  string    `json:"bucket_utc"` // Truncated to hour
}

// OHLCV represents Open, High, Low, Close, Volume for a time bucket.
type OHLCV struct {
	StationID  string  `json:"station_id"`
	ItemID     string  `json:"item_id"`
	Side       string  `json:"side"`
	BucketUTC  string  `json:"bucket_utc"`
	OpenPrice  float64 `json:"open_price"`
	HighPrice  float64 `json:"high_price"`
	LowPrice   float64 `json:"low_price"`
	ClosePrice float64 `json:"close_price"`
	Volume     float64 `json:"volume"`
	TradeCount int     `json:"trade_count"`
	VWAP       float64 `json:"vwap"` // Volume-weighted average price
}

// ArbitrageOpportunity represents a profitable trading opportunity.
type ArbitrageOpportunity struct {
	ID            int     `json:"id"`
	FromStationID string  `json:"from_station_id"`
	ToStationID   string  `json:"to_station_id"`
	ItemID        string  `json:"item_id"`
	ActionType    string  `json:"action_type"` // "buy_then_sell" or "sell_then_buy"
	BuyPrice      float64 `json:"buy_price"`
	SellPrice     float64 `json:"sell_price"`
	Quantity      float64 `json:"quantity"`
	GrossProfit   float64 `json:"gross_profit"`
	FuelCost      float64 `json:"fuel_cost"`
	TravelTicks   int     `json:"travel_ticks"`
	CargoRequired float64 `json:"cargo_required"`
	RiskScore     float64 `json:"risk_score"`
	ClaimedBy     string  `json:"claimed_by"`
	ClaimedAt     string  `json:"claimed_at"`
	Status        string  `json:"status"` // "available", "claimed", "completed", "expired"
	ExpiresAt     string  `json:"expires_at"`
	DiscoveredAt  string  `json:"discovered_at"`
	DiscoveredBy  string  `json:"discovered_by"`
	Notes         string  `json:"notes"`
}

// MarketSnapshot represents a complete market state at one station.
type MarketSnapshot struct {
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	Orders      []Order   `json:"orders"`
	CapturedAt  time.Time `json:"captured_at"`
}

// BestPrice is the best available price for an item at a station, used for
// cross-station comparison.
type BestPrice struct {
	ItemID      string    `json:"item_id"`
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	Price       float64   `json:"price"`
	Quantity    float64   `json:"quantity"`
	ListingType string    `json:"listing_type"` // "buy" or "sell"
	CapturedAt  time.Time `json:"captured_at"`
}

// MarketAnalysis is LLM-generated market analysis (analyze_market output).
type MarketAnalysis struct {
	SystemID        string
	SystemName      string
	StationID       string
	StationName     string
	GameTick        int64
	CapturedAt      time.Time
	AgentID         string
	Mode            string
	SkillLevel      int
	ScanningRange   string
	StationsInRange int
	ItemsScanned    int
	TopInsights     []map[string]any
	TotalItems      int
	TotalPages      int
	Page            int
	Hint            string
	XPGained        map[string]any
	AnalysisData    map[string]any
}
