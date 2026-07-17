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

// StationFuel is the latest captured fuel price at a station. One row per
// station (upserted, never grows). FuelPriceAllIn is the per-unit cost a hauler
// actually pays; Sub-project B consumes it. CapturedAt is RFC3339.
type StationFuel struct {
	StationID      string
	FuelPrice      int
	FuelTaxPerUnit int
	FuelPriceAllIn int
	CapturedAt     string
	CapturedBy     string
}

// Order represents a single buy or sell order from the market.
type Order struct {
	StationID  string    `json:"station_id"`
	ItemID     string    `json:"item_id"`
	ItemName   string    `json:"item_name"`
	Category   string    `json:"category"`
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
	ID              int     `json:"id"`
	FromStationID   string  `json:"from_station_id"`
	ToStationID     string  `json:"to_station_id"`
	ItemID          string  `json:"item_id"`
	ActionType      string  `json:"action_type"` // "buy_then_sell" or "sell_then_buy"
	BuyPrice        float64 `json:"buy_price"`
	SellPrice       float64 `json:"sell_price"`
	Quantity        float64 `json:"quantity"`
	GrossProfit     float64 `json:"gross_profit"`
	FuelCost        float64 `json:"fuel_cost"`
	TravelTicks     int     `json:"travel_ticks"`
	CargoRequired   float64 `json:"cargo_required"`
	RiskScore       float64 `json:"risk_score"`
	ClaimedBy       string  `json:"claimed_by"`
	ClaimedAt       string  `json:"claimed_at"`
	CompletedAt     string  `json:"completed_at"` // set when status transitions to "completed"
	CyclesSeen      int     `json:"cycles_seen"`  // consecutive scan cycles this route has appeared in (1=new)
	Status          string  `json:"status"`       // "available", "claimed", "completed", "expired"
	ExpiresAt       string  `json:"expires_at"`
	DiscoveredAt    string  `json:"discovered_at"`
	DiscoveredBy    string  `json:"discovered_by"`
	Notes           string  `json:"notes"`
	FromStationName string  `json:"from_station_name"` // joined on read
	FromSystemName  string  `json:"from_system_name"`  // joined on read
	ToStationName   string  `json:"to_station_name"`   // joined on read
	ToSystemName    string  `json:"to_system_name"`    // joined on read
	ItemName        string  `json:"item_name"`         // joined on read
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

// ItemSeller summarizes one station's sell-side availability of an item,
// aggregated over that station's latest capture.
type ItemSeller struct {
	ItemID      string    `json:"item_id"`
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	BestPrice   float64   `json:"best_price"`
	TotalQty    float64   `json:"total_quantity"`
	Orders      int       `json:"orders"`
	CapturedAt  time.Time `json:"captured_at"`
}

// ItemStationPrice is one item's best ask/bid per station, from the latest capture.
// BestAsk is the cheapest sell order (where you BUY); BestBid is the highest buy
// order (where you SELL). AskQty/BidQty total the quantity of orders tying at that
// best price. The arbitrage scanner pairs these across stations.
type ItemStationPrice struct {
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	BestAsk     float64   `json:"best_ask"`
	AskQty      float64   `json:"ask_qty"`
	BestBid     float64   `json:"best_bid"`
	BidQty      float64   `json:"bid_qty"`
	HasSell     bool      `json:"has_sell"`
	HasBuy      bool      `json:"has_buy"`
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

// MatrixQuery parameterizes an items×stations matrix request.
type MatrixQuery struct {
	Category string `json:"category"` // "" = all
	Search   string `json:"search"`   // case-insensitive substring on item_id / item_name
	Page     int    `json:"page"`     // 1-based
	Limit    int    `json:"limit"`    // default 50
}

// MatrixCell is one item×station cell: aggregates over the station's latest capture.
type MatrixCell struct {
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	BestSell    float64   `json:"best_sell"`
	BestBuy     float64   `json:"best_buy"`
	VWAP        float64   `json:"vwap"`   // volume-weighted avg over sell orders
	Volume      float64   `json:"volume"` // sum of sell quantities
	OrderCount  int       `json:"order_count"`
	CapturedAt  time.Time `json:"captured_at"`
	HasSell     bool      `json:"has_sell"`
	HasBuy      bool      `json:"has_buy"`
}

// MatrixItem is one matrix row: an item across all stations.
type MatrixItem struct {
	ItemID   string       `json:"item_id"`
	ItemName string       `json:"item_name"`
	Category string       `json:"category"`
	Cells    []MatrixCell `json:"cells"` // aligned to Matrix.Stations order
}

// Matrix is a paginated items×stations snapshot.
type Matrix struct {
	Stations    []Station    `json:"stations"`
	Items       []MatrixItem `json:"items"`
	TotalItems  int          `json:"total_items"`
	Page        int          `json:"page"`
	Limit       int          `json:"limit"`
	GeneratedAt time.Time    `json:"generated_at"`
}

// ItemPricePoint is one OHLCV bucket for an item at a station.
type ItemPricePoint struct {
	StationID   string  `json:"station_id"`
	StationName string  `json:"station_name"`
	Side        string  `json:"side"`
	BucketUTC   string  `json:"bucket_utc"`
	VWAP        float64 `json:"vwap"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Volume      float64 `json:"volume"`
	TradeCount  int     `json:"trade_count"`
}

// ReferenceAsk is an item's best current tradeable ask across the market: the
// live floor a buyer would actually pay. It is the honest price signal, distinct
// from a book-VWAP (which averages the whole resting ladder).
//
// Depth and Stations exist to expose thin-book / troll conditions: a BestAsk set
// by a lone 1cr listing with Stations==1 and tiny Depth is a red flag that the
// floor is not real demand, the low-side mirror of the not-for-sale sentinel.
type ReferenceAsk struct {
	ItemID   string  `json:"item_id"`
	BestAsk  float64 `json:"best_ask"`        // cheapest sentinel-filtered current ask
	Depth    float64 `json:"depth"`           // total tradeable sell quantity at latest capture
	Stations int     `json:"stations"`        // number of stations currently offering
	AtAsk    float64 `json:"at_ask"`          // quantity available at exactly BestAsk
}

// StationCaptures summarizes one station's capture history for health checks.
type StationCaptures struct {
	StationID    string   `json:"station_id"`
	StationName  string   `json:"station_name"`
	SystemID     string   `json:"system_id"`
	SystemName   string   `json:"system_name"`
	CaptureTimes []string `json:"capture_times"` // distinct captured_at, newest first
	Count        int      `json:"count"`
	Latest       string   `json:"latest"`
	Earliest     string   `json:"earliest"`
}

// ScanOptions parameterizes an arbitrage scan. Zero-valued fields take the
// documented defaults when ScanArbitrage runs.
type ScanOptions struct {
	MinProfit   float64       `json:"min_profit"`   // gross_profit floor (default 1000)
	MinPrice    float64       `json:"min_price"`    // per-order price floor, filters basement orders (default 10)
	MinQuantity float64       `json:"min_quantity"` // per-order depth floor (default 1)
	ExpiresIn   time.Duration `json:"expires_in"`   // opportunity TTL (default 6h)
	Items       []string      `json:"items"`        // allowlist; empty = all traded items
	Limit       int           `json:"limit"`        // cap rows inserted (default 500)
}

// ScanResult reports what a ScanArbitrage run did.
type ScanResult struct {
	Expired     int       `json:"expired"`
	Inserted    int       `json:"inserted"`
	GeneratedAt time.Time `json:"generated_at"`
}

// HaulResult is the real, cargo-capped outcome of one completed haul, with per-leg
// timing in wall-time (RFC3339) and game ticks. Drives the dashboard's response-time
// and credits/jump charts and the true realized-profit column.
type HaulResult struct {
	ID             int64   `json:"id"`
	OppID          int     `json:"opp_id"`
	AgentID        string  `json:"agent_id"`
	ItemID         string  `json:"item_id"`
	Qty            float64 `json:"qty"`
	BuyPricePaid   float64 `json:"buy_price_paid"`
	SellPriceGot   float64 `json:"sell_price_got"`
	RealizedProfit float64 `json:"realized_profit"`
	JumpsTraveled  int     `json:"jumps_traveled"`
	ClaimedAt      string  `json:"claimed_at"`
	ArrivedSrcAt   string  `json:"arrived_src_at"`
	BoughtAt       string  `json:"bought_at"`
	ArrivedDstAt   string  `json:"arrived_dst_at"`
	SoldAt         string  `json:"sold_at"`
	ClaimedTick    int64   `json:"claimed_tick"`
	ArrivedSrcTick int64   `json:"arrived_src_tick"`
	BoughtTick     int64   `json:"bought_tick"`
	ArrivedDstTick int64   `json:"arrived_dst_tick"`
	SoldTick       int64   `json:"sold_tick"`
	CreatedAt      string  `json:"created_at"`
}

// MissionResult is one finished (completed or abandoned) mission's real outcome,
// the mission-fleet analogue of HaulResult. CreditsEarned is the observed wallet
// delta around complete_mission (0 for abandons); ExpectedReward is what the
// board advertised, so the dashboard can compare promised vs realized.
type MissionResult struct {
	ID             int64   `json:"id"`
	AgentID        string  `json:"agent_id"`
	MissionID      string  `json:"mission_id"`
	TemplateID     string  `json:"template_id"`
	MissionType    string  `json:"mission_type"`
	Title          string  `json:"title"`
	FromBaseID     string  `json:"from_base_id"`
	ToBaseID       string  `json:"to_base_id"`
	ItemID         string  `json:"item_id"`
	Qty            float64 `json:"qty"`
	ExpectedReward float64 `json:"expected_reward"`
	CreditsEarned  float64 `json:"credits_earned"`
	ItemCost       float64 `json:"item_cost"`
	FuelCost       float64 `json:"fuel_cost"`
	Jumps          int     `json:"jumps"`
	Outcome        string  `json:"outcome"` // completed | abandoned
	Reason         string  `json:"reason"`  // cause slug for non-completed outcomes ("" for completed)
	AcceptedAt     string  `json:"accepted_at"`
	FinishedAt     string  `json:"finished_at"`
	AcceptedTick   int64   `json:"accepted_tick"`
	FinishedTick   int64   `json:"finished_tick"`
	CreatedAt      string  `json:"created_at"`
}

// FleetSnapshot is one hauler's balance/fuel/cargo at a point in time (quarter-hourly).
type FleetSnapshot struct {
	ID            int64   `json:"id"`
	TS            string  `json:"ts"`
	AgentID       string  `json:"agent_id"`
	Role          string  `json:"role"`
	System        string  `json:"system"`
	Docked        bool    `json:"docked"`
	Credits       float64 `json:"credits"`
	Fuel          float64 `json:"fuel"`
	MaxFuel       float64 `json:"max_fuel"`
	CargoUsed     float64 `json:"cargo_used"`
	CargoCapacity float64 `json:"cargo_capacity"`
}
