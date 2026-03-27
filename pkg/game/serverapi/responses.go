package serverapi

// ============================================================================
// Server Response Wrappers
// These wrap the actual data returned by server commands.
// They should match the OpenAPI spec at server_docs/openapi.json.
// ============================================================================

// GetSystemResponse wraps the response from get_system command.
type GetSystemResponse struct {
	Action          string     `json:"action"`
	POI             CurrentPOI `json:"poi,omitempty"`
	SecurityStatus  string     `json:"security_status"`
	System          SystemData `json:"system"`
	FromSystem      string     `json:"from_system,omitempty"`
	InTransit       bool       `json:"in_transit,omitempty"`
	TicksRemaining  int        `json:"ticks_remaining,omitempty"`
	ToSystem        string     `json:"to_system,omitempty"`
	TransitType     string     `json:"transit_type,omitempty"`
}

// GetPOIResponse wraps the response from get_poi command.
type GetPOIResponse struct {
	Action        string          `json:"action,omitempty"`
	POI           POI             `json:"poi"`
	Base          *Base           `json:"base,omitempty"`
	PoliceDrones  int             `json:"police_drones,omitempty"`
	PoliceWarning string          `json:"police_warning,omitempty"`
	Resources     []ResourceDisplay `json:"resources,omitempty"`
	Services      []string        `json:"services,omitempty"`
}

// GetStatusResponse wraps the response from get_status command.
type GetStatusResponse struct {
	Action      string       `json:"action,omitempty"`
	Player      Player       `json:"player"`
	Ship        Ship         `json:"ship"`
	Modules     []ShipModule `json:"modules"`
	System      SystemData   `json:"system,omitempty"`
	POI         POI          `json:"poi,omitempty"`
	Nearby      []NearbyPlayer `json:"nearby,omitempty"`
	CurrentTick int64        `json:"current_tick,omitempty"`
}

// GetShipResponse wraps the response from get_ship command.
type GetShipResponse struct {
	Ship     Ship         `json:"ship"`
	Modules  []ShipModule `json:"modules"`
	CargoUsed int         `json:"cargo_used"`
	CargoMax  int         `json:"cargo_max"`
	Class    *ShipClass   `json:"class,omitempty"`
}

// GetMapResponse wraps the response from get_map command.
type GetMapResponse struct {
	Systems    []MapSystem `json:"systems"`
	TotalCount int         `json:"total_count,omitempty"`
}

// GetBaseResponse wraps the response from get_base command.
type GetBaseResponse struct {
	Action    string            `json:"action,omitempty"`
	Base      *Base             `json:"base"`
	Services  []string          `json:"services"`
	Condition *StationHealth    `json:"condition,omitempty"`
	POI       POI               `json:"poi,omitempty"`
	Resources []ResourceDisplay `json:"resources,omitempty"`
	Market    []MarketListing   `json:"market,omitempty"`
}

// GetSkillsResponse wraps the response from get_skills command.
type GetSkillsResponse struct {
	Action       string                     `json:"action,omitempty"`
	Skills       map[string]SkillDefinition `json:"skills"`
	PlayerSkills []PlayerSkill              `json:"player_skills,omitempty"`
	Message      string                     `json:"message,omitempty"`
}

// GetListingsResponse wraps the response from get_listings command.
type GetListingsResponse struct {
	Action      string          `json:"action"`
	Listings    []MarketListing `json:"listings"`
	StationID   string          `json:"station_id"`
	StationName string          `json:"station_name"`
}

// GetShipsResponse wraps the response from get_ships command (ship class catalog).
type GetShipsResponse struct {
	Action      string      `json:"action,omitempty"`
	Ships       []ShipClass `json:"ships"`
	Count       int         `json:"count,omitempty"`
	Message     string      `json:"message,omitempty"`
	StationID   string      `json:"station_id,omitempty"`
	StationName string      `json:"station_name,omitempty"`
}

// GetRecipesResponse wraps the response from get_recipes command.
type GetRecipesResponse struct {
	Action  string            `json:"action"`
	Recipes map[string]Recipe `json:"recipes"`
}

// GetWrecksResponse wraps the response from get_wrecks command.
type GetWrecksResponse struct {
	Action string  `json:"action,omitempty"`
	Wrecks []Wreck `json:"wrecks"`
	Count  int     `json:"count,omitempty"`
}

// GetDronesResponse wraps the response from get_drones command.
type GetDronesResponse struct {
	Action         string  `json:"action,omitempty"`
	Drones         []Drone `json:"drones"`
	TotalCount     int     `json:"total_count,omitempty"`
	BandwidthUsed  int     `json:"bandwidth_used,omitempty"`
	BandwidthTotal int     `json:"bandwidth_total,omitempty"`
	DroneCapacity  int     `json:"drone_capacity,omitempty"`
}

// GetCargoResponse wraps the response from get_cargo command.
type GetCargoResponse struct {
	Action    string      `json:"action,omitempty"`
	Cargo     []CargoItem `json:"cargo"`
	Used      int         `json:"used"`
	Capacity  int         `json:"capacity"`
	Available int         `json:"available"`
}

// GetNearbyResponse wraps the response from get_nearby command.
type GetNearbyResponse struct {
	Action      string         `json:"action,omitempty"`
	Nearby      []NearbyPlayer `json:"nearby"`
	Pirates     []NearbyPirate `json:"pirates,omitempty"`
	Count       int            `json:"count"`
	PirateCount int            `json:"pirate_count,omitempty"`
	POIID       string         `json:"poi_id,omitempty"`
}

// GetBattleStatusResponse wraps the response from get_battle_status command.
type GetBattleStatusResponse struct {
	BattleID      string              `json:"battle_id"`
	SystemID      string              `json:"system_id"`
	IsParticipant bool                `json:"is_participant"`
	Participants  []BattleParticipant `json:"participants,omitempty"`
	Sides         []BattleSide        `json:"sides,omitempty"`
	TickDuration  int                 `json:"tick_duration,omitempty"`
}

// BattleResponse wraps the response from battle command actions.
type BattleResponse struct {
	Action   string `json:"action"`
	BattleID string `json:"battle_id,omitempty"`
	Message  string `json:"message"`
	Stance   string `json:"stance,omitempty"`
	TargetID string `json:"target_id,omitempty"`
}

// ViewMarketResponse wraps the response from view_market command.
type ViewMarketResponse struct {
	Action     string           `json:"action"`
	Base       string           `json:"base,omitempty"`
	Items      []ViewMarketItem `json:"items"`
	Message    string           `json:"message,omitempty"`
	Categories []string         `json:"categories,omitempty"`
}

// ViewOrdersResponse wraps the response from view_orders command.
type ViewOrdersResponse struct {
	Action                 string          `json:"action"`
	Base                   string          `json:"base,omitempty"`
	Orders                 []ExchangeOrder `json:"orders"`
	FactionOrders          []ExchangeOrder `json:"faction_orders,omitempty"`
	OrdersCount            int             `json:"orders_count,omitempty"`
	FactionOrdersCount     int             `json:"faction_orders_count,omitempty"`
	OrdersTruncated        bool            `json:"orders_truncated,omitempty"`
	FactionOrdersTruncated bool            `json:"faction_orders_truncated,omitempty"`
	Hint                   string          `json:"hint,omitempty"`
	HasMore                bool            `json:"has_more,omitempty"`
	Page                   int             `json:"page,omitempty"`
	PageSize               int             `json:"page_size,omitempty"`
	Scope                  string          `json:"scope,omitempty"`
	SortBy                 string          `json:"sort_by,omitempty"`
	Total                  int             `json:"total,omitempty"`
	TotalPages             int             `json:"total_pages,omitempty"`
}

// AnalyzeMarketResponse wraps the response from analyze_market command.
type AnalyzeMarketResponse struct {
	Insights   []MarketInsight `json:"insights"`
	SkillLevel int             `json:"skill_level"`
	Station    string          `json:"station,omitempty"`
	Message    string          `json:"message,omitempty"`
}

// EstimatePurchaseResponse wraps the response from estimate_purchase command.
type EstimatePurchaseResponse struct {
	Action            string      `json:"action"`
	Item              string      `json:"item"`
	QuantityRequested int         `json:"quantity_requested"`
	Available         int         `json:"available"`
	TotalCost         int         `json:"total_cost"`
	Fills             []OrderFill `json:"fills"`
	Unfilled          int         `json:"unfilled"`
	Message           string      `json:"message"`
}

// BuyResponse wraps the response from buy command.
type BuyResponse struct {
	Action            string           `json:"action"`
	Item              string           `json:"item"`
	ItemID            string           `json:"item_id"`
	Quantity          int              `json:"quantity"`
	TotalCost         int              `json:"total_cost"`
	Fills             []OrderFill      `json:"fills"`
	LevelUp           bool             `json:"level_up"`
	AutoListed        *AutoListedOrder `json:"auto_listed,omitempty"`
	DeliveredToCargo  int              `json:"delivered_to_cargo,omitempty"`
	DeliveredToStorage int             `json:"delivered_to_storage,omitempty"`
	Message           string           `json:"message,omitempty"`
	Unfilled          int              `json:"unfilled,omitempty"`
	XPGained          int              `json:"xp_gained,omitempty"`
}

// SellResponse wraps the response from sell command.
type SellResponse struct {
	Action       string           `json:"action"`
	Item         string           `json:"item"`
	ItemID       string           `json:"item_id"`
	QuantitySold int              `json:"quantity_sold"`
	TotalEarned  int              `json:"total_earned"`
	Fills        []OrderFill      `json:"fills"`
	LevelUp      bool             `json:"level_up"`
	AutoListed   *AutoListedOrder `json:"auto_listed,omitempty"`
	Message      string           `json:"message,omitempty"`
	SkillLevel   int              `json:"skill_level,omitempty"`
	Unsold       int              `json:"unsold,omitempty"`
	XPGained     int              `json:"xp_gained,omitempty"`
}

// CreateBuyOrderResponse wraps the response from create_buy_order command.
type CreateBuyOrderResponse struct {
	Action             string      `json:"action"`
	Item               string      `json:"item"`
	ItemID             string      `json:"item_id"`
	Quantity           int         `json:"quantity"`
	PriceEach          int         `json:"price_each"`
	TotalEscrowed      int         `json:"total_escrowed"`
	ListingFee         int         `json:"listing_fee"`
	Message            string      `json:"message"`
	DeliveredToCargo   int         `json:"delivered_to_cargo,omitempty"`
	DeliveredToStorage int         `json:"delivered_to_storage,omitempty"`
	EscrowRefunded     int         `json:"escrow_refunded,omitempty"`
	Fills              []OrderFill `json:"fills,omitempty"`
	OrderID            string      `json:"order_id,omitempty"`
	QuantityFilled     int         `json:"quantity_filled,omitempty"`
	QuantityListed     int         `json:"quantity_listed,omitempty"`
	QuantityNotListed  int         `json:"quantity_not_listed,omitempty"`
	RemainingEscrowed  int         `json:"remaining_escrowed,omitempty"`
	TotalSpent         int         `json:"total_spent,omitempty"`
}

// CreateSellOrderResponse wraps the response from create_sell_order command.
type CreateSellOrderResponse struct {
	Action            string      `json:"action"`
	Item              string      `json:"item"`
	ItemID            string      `json:"item_id"`
	Quantity          int         `json:"quantity"`
	PriceEach         int         `json:"price_each"`
	ListingFee        int         `json:"listing_fee"`
	Message           string      `json:"message"`
	Fills             []OrderFill `json:"fills,omitempty"`
	FromCargo         int         `json:"from_cargo,omitempty"`
	FromStorage       int         `json:"from_storage,omitempty"`
	OrderID           string      `json:"order_id,omitempty"`
	QuantityFilled    int         `json:"quantity_filled,omitempty"`
	QuantityListed    int         `json:"quantity_listed,omitempty"`
	ReturnedToStorage int         `json:"returned_to_storage,omitempty"`
	TotalEarned       int         `json:"total_earned,omitempty"`
}

// CancelOrderResponse wraps the response from cancel_order command.
type CancelOrderResponse struct {
	Action          string     `json:"action"`
	OrderID         string     `json:"order_id"`
	Message         string     `json:"message"`
	DeliveredTo     string     `json:"delivered_to,omitempty"`
	FactionOrder    bool       `json:"faction_order,omitempty"`
	ReturnedCredits int        `json:"returned_credits,omitempty"`
	ReturnedItems   *TradeItem `json:"returned_items,omitempty"`
}

// ModifyOrderResponse wraps the response from modify_order command.
type ModifyOrderResponse struct {
	Action     string `json:"action"`
	OrderID    string `json:"order_id"`
	OldPrice   int    `json:"old_price"`
	NewPrice   int    `json:"new_price"`
	Message    string `json:"message"`
	ListingFee int    `json:"listing_fee,omitempty"`
}

// GetTradesResponse wraps the response from get_trades command.
type GetTradesResponse struct {
	Action   string        `json:"action,omitempty"`
	Incoming []TradeDetail `json:"incoming"`
	Outgoing []TradeDetail `json:"outgoing"`
}

// TradeOfferResponse wraps the response from trade_offer command.
type TradeOfferResponse struct {
	TradeID string `json:"trade_id"`
	Message string `json:"message"`
}

// TradeAcceptResponse wraps the response from trade_accept command.
type TradeAcceptResponse struct {
	TradeID     string `json:"trade_id"`
	Message     string `json:"message"`
	YourCredits int    `json:"your_credits,omitempty"`
}

// GetMissionsResponse wraps the response from get_missions command.
type GetMissionsResponse struct {
	Action   string              `json:"action,omitempty"`
	Missions []MissionBoardEntry `json:"missions"`
	BaseName string              `json:"base_name"`
	BaseID   string              `json:"base_id"`
}

// GetActiveMissionsResponse wraps the response from get_active_missions command.
type GetActiveMissionsResponse struct {
	Action      string          `json:"action,omitempty"`
	Missions    []ActiveMission `json:"missions"`
	TotalCount  int             `json:"total_count,omitempty"`
	MaxMissions int             `json:"max_missions,omitempty"`
}

// AcceptMissionResponse wraps the response from accept_mission command.
type AcceptMissionResponse struct {
	Message   string `json:"message"`
	MissionID string `json:"mission_id"`
	Title     string `json:"title"`
}

// CompleteMissionResponse wraps the response from complete_mission command.
type CompleteMissionResponse struct {
	Message   string          `json:"message"`
	MissionID string          `json:"mission_id"`
	Title     string          `json:"title"`
	Rewards   *MissionRewards `json:"rewards,omitempty"`
}

// CatalogResponse wraps the response from catalog command.
type CatalogResponse struct {
	Type       string        `json:"type"`
	Items      []CatalogItem `json:"items"`
	Total      int           `json:"total"`
	Page       int           `json:"page"`
	PageSize   int           `json:"page_size"`
	TotalPages int           `json:"total_pages"`
	Message    string        `json:"message"`
}

// GetInsuranceQuoteResponse wraps the response from get_insurance_quote command.
type GetInsuranceQuoteResponse struct {
	Message string         `json:"message"`
	Quote   InsuranceQuote `json:"quote"`
	Notice  string         `json:"notice,omitempty"`
}

// BuyInsuranceResponse wraps the response from buy_insurance command.
type BuyInsuranceResponse struct {
	PolicyID         string            `json:"policy_id"`
	Coverage         int               `json:"coverage"`
	Premium          int               `json:"premium"`
	ExpiresAt        string            `json:"expires_at"`
	RemainingCredits int               `json:"remaining_credits"`
	Message          string            `json:"message"`
	Factors          []InsuranceFactor `json:"factors,omitempty"`
	RiskScore        float64           `json:"risk_score,omitempty"`
}

// ClaimInsuranceResponse wraps the response from claim_insurance command.
type ClaimInsuranceResponse struct {
	Action   string            `json:"action,omitempty"`
	Message  string            `json:"message"`
	Policies []InsurancePolicy `json:"policies"`
}

// CommissionStatusResponse wraps the response from commission_status command.
type CommissionStatusResponse struct {
	Commissions []CommissionDetail `json:"commissions"`
	Count       int                `json:"count"`
}

// CommissionQuoteResponse wraps the response from commission_quote command.
type CommissionQuoteResponse struct {
	Message                    string       `json:"message"`
	ShipClass                  string       `json:"ship_class"`
	ShipName                   string       `json:"ship_name,omitempty"`
	CanCommission              bool         `json:"can_commission"`
	CreditsOnlyTotal           int          `json:"credits_only_total"`
	ProvideMaterialsTotal      int          `json:"provide_materials_total"`
	CreditsOnlyAvailable       bool         `json:"credits_only_available"`
	CanAffordCreditsOnly       bool         `json:"can_afford_credits_only"`
	CanAffordProvideMaterials  bool         `json:"can_afford_provide_materials"`
	Blockers                   []string     `json:"blockers,omitempty"`
	BuildMaterials             []CargoItem  `json:"build_materials,omitempty"`
	BuildTime                  int          `json:"build_time,omitempty"`
	LaborCost                  int          `json:"labor_cost,omitempty"`
	MaterialCost               int          `json:"material_cost,omitempty"`
	PlayerCredits              int          `json:"player_credits,omitempty"`
	ShipyardTierHere           int          `json:"shipyard_tier_here,omitempty"`
	ShipyardTierRequired       int          `json:"shipyard_tier_required,omitempty"`
}

// CommissionShipResponse wraps the response from commission_ship command.
type CommissionShipResponse struct {
	Message      string `json:"message"`
	CommissionID string `json:"commission_id"`
	ShipClass    string `json:"ship_class"`
	ShipName     string `json:"ship_name,omitempty"`
	Status       string `json:"status"`
	CreditsPaid  int    `json:"credits_paid"`
	CreditsLeft  int    `json:"credits_left"`
	BuildTime    int    `json:"build_time,omitempty"`
	LaborCost    int    `json:"labor_cost,omitempty"`
	MaterialCost int    `json:"material_cost,omitempty"`
}

// ListShipsResponse wraps the response from list_ships command.
type ListShipsResponse struct {
	Ships           []OwnedShip `json:"ships"`
	Count           int         `json:"count"`
	ActiveShipID    string      `json:"active_ship_id,omitempty"`
	ActiveShipClass string      `json:"active_ship_class,omitempty"`
}

// BrowseShipsResponse wraps the response from browse_ships command.
type BrowseShipsResponse struct {
	Action   string              `json:"action,omitempty"`
	BaseID   string              `json:"base_id,omitempty"`
	BaseName string              `json:"base_name,omitempty"`
	Listings []ShipListingDetail `json:"listings"`
	Count    int                 `json:"count,omitempty"`
}

// ShipyardShowroomResponse wraps the response from shipyard_showroom command.
type ShipyardShowroomResponse struct {
	Action        string         `json:"action,omitempty"`
	BaseID        string         `json:"base_id,omitempty"`
	BaseName      string         `json:"base_name,omitempty"`
	Ships         []ShowroomShip `json:"ships"`
	Count         int            `json:"count,omitempty"`
	ShipyardLevel int            `json:"shipyard_level,omitempty"`
	Tip           string         `json:"tip,omitempty"`
}

// BuyShipResponse wraps the response from buy_ship command.
type BuyShipResponse struct {
	Message     string `json:"message"`
	NewShipID   string `json:"new_ship_id"`
	OldShipID   string `json:"old_ship_id"`
	ShipClass   string `json:"ship_class"`
	CreditsLeft int    `json:"credits_left"`
}

// SellShipResponse wraps the response from sell_ship command.
type SellShipResponse struct {
	Message          string           `json:"message"`
	CreditsEarned    int              `json:"credits_earned"`
	DaysOwned        int              `json:"days_owned,omitempty"`
	CreditsTotal     int              `json:"credits_total"`
	CargoNote        string           `json:"cargo_note,omitempty"`
	CargoToStorage   []CargoItem      `json:"cargo_to_storage,omitempty"`
	ModulesNote      string           `json:"modules_note,omitempty"`
	ModulesToStorage []map[string]any `json:"modules_to_storage,omitempty"`
}

// SwitchShipResponse wraps the response from switch_ship command.
type SwitchShipResponse struct {
	Message          string      `json:"message"`
	ActiveShipID     string      `json:"active_ship_id"`
	ActiveShipClass  string      `json:"active_ship_class"`
	StoredShipID     string      `json:"stored_ship_id"`
	StoredShipClass  string      `json:"stored_ship_class"`
	CargoNote        string      `json:"cargo_note,omitempty"`
	CargoToStorage   []CargoItem `json:"cargo_to_storage,omitempty"`
}

// BuyListedShipResponse wraps the response from buy_listed_ship command.
type BuyListedShipResponse struct {
	Message     string `json:"message"`
	ShipID      string `json:"ship_id"`
	ClassID     string `json:"class_id"`
	Price       int    `json:"price"`
	CreditsLeft int    `json:"credits_left"`
}

// ListShipForSaleResponse wraps the response from list_ship_for_sale command.
type ListShipForSaleResponse struct {
	Message     string `json:"message"`
	ListingID   string `json:"listing_id"`
	ShipID      string `json:"ship_id"`
	Price       int    `json:"price"`
	Fee         int    `json:"fee"`
	CreditsLeft int    `json:"credits_left"`
}

// GetNotesResponse wraps the response from get_notes command.
type GetNotesResponse struct {
	Action     string `json:"action,omitempty"`
	Notes      []Note `json:"notes"`
	TotalCount int    `json:"total_count,omitempty"`
}

// ReadNoteResponse wraps the response from read_note command.
type ReadNoteResponse struct {
	Action string `json:"action,omitempty"`
	Note   Note   `json:"note"`
}

// CreateNoteResponse wraps the response from create_note command.
type CreateNoteResponse struct {
	NoteID        string `json:"note_id"`
	Title         string `json:"title"`
	ContentLength int    `json:"content_length"`
	Message       string `json:"message"`
}

// WriteNoteResponse wraps the response from write_note command.
type WriteNoteResponse struct {
	NoteID        string `json:"note_id"`
	ContentLength int    `json:"content_length"`
	Message       string `json:"message"`
}

// FindRouteResponse wraps the response from find_route command.
// v0.240: now accepts POI/base targets, includes fuel estimates and fleet fuel analysis.
type FindRouteResponse struct {
	Action        string           `json:"action,omitempty"`
	Found         bool             `json:"found"`
	TotalJumps    int              `json:"total_jumps"`
	TargetSystem  string           `json:"target_system,omitempty"`
	TargetPOI     string           `json:"target_poi,omitempty"`
	TargetPOIName string           `json:"target_poi_name,omitempty"`
	FuelPerJump   int              `json:"fuel_per_jump,omitempty"`
	EstimatedFuel int              `json:"estimated_fuel,omitempty"`
	FuelAvailable int              `json:"fuel_available,omitempty"`
	CargoUsed     int              `json:"cargo_used,omitempty"`
	Message       string           `json:"message,omitempty"`
	Route         []RouteStep      `json:"route,omitempty"`
	FleetFuel     []FleetFuelEntry `json:"fleet_fuel,omitempty"`
}

// FleetFuelEntry reports per-member fuel analysis for a fleet route.
type FleetFuelEntry struct {
	PlayerID      string `json:"player_id"`
	Username      string `json:"username"`
	ShipClass     string `json:"ship_class"`
	FuelPerJump   int    `json:"fuel_per_jump"`
	EstimatedFuel int    `json:"estimated_fuel"`
	FuelAvailable int    `json:"fuel_available"`
	FuelOnArrival int    `json:"fuel_on_arrival"`
	CanComplete   bool   `json:"can_complete"`
}

// SearchSystemsResponse wraps the response from search_systems command.
type SearchSystemsResponse struct {
	Action     string               `json:"action,omitempty"`
	Systems    []SystemSearchResult `json:"systems"`
	TotalFound int                  `json:"total_found,omitempty"`
	Query      string               `json:"query,omitempty"`
	Message    string               `json:"message,omitempty"`
}

// SurveySystemResponse wraps the response from survey_system command.
type SurveySystemResponse struct {
	Action          string           `json:"action,omitempty"`
	SystemID        string           `json:"system_id,omitempty"`
	SystemName      string           `json:"system_name,omitempty"`
	SurveyPower     int              `json:"survey_power,omitempty"`
	NewlyRevealed   []RevealedPOI    `json:"newly_revealed,omitempty"`
	AlreadyRevealed []RevealedPOI    `json:"already_revealed,omitempty"`
	FaintSignatures []FaintSignature `json:"faint_signatures,omitempty"`
	XPGained        map[string]int   `json:"xp_gained,omitempty"`
	Message         string           `json:"message,omitempty"`
}

// FacilityResponse wraps the response from facility command.
type FacilityResponse struct {
	Action       string           `json:"action"`
	Command      string           `json:"command,omitempty"`
	BaseID       string           `json:"base_id,omitempty"`
	FacilityID   string           `json:"facility_id,omitempty"`
	FacilityName string           `json:"facility_name,omitempty"`
	FacilityType string           `json:"facility_type,omitempty"`
	Category     string           `json:"category,omitempty"`
	Description  string           `json:"description,omitempty"`
	Hint         string           `json:"hint,omitempty"`
	Message      string           `json:"message,omitempty"`
	Page         int              `json:"page,omitempty"`
	PerPage      int              `json:"per_page,omitempty"`
	Total        int              `json:"total,omitempty"`
	TotalPages   int              `json:"total_pages,omitempty"`
	Actions      []map[string]any `json:"actions,omitempty"`
	Types        []map[string]any `json:"types,omitempty"`
	Upgrades     []map[string]any `json:"upgrades,omitempty"`
}

// FacilityListResponse wraps the response from facility list command.
type FacilityListResponse struct {
	BaseID              string           `json:"base_id"`
	StationFacilities   []map[string]any `json:"station_facilities"`
	PlayerFacilities    []map[string]any `json:"player_facilities"`
	FactionFacilities   []map[string]any `json:"faction_facilities"`
}

// ViewStorageResponse wraps the response from view_storage command.
type ViewStorageResponse struct {
	BaseID  string        `json:"base_id"`
	Credits int           `json:"credits"`
	Items   []CargoItem   `json:"items"`
	Ships   []StorageShip `json:"ships,omitempty"`
	Gifts   []StorageGift `json:"gifts,omitempty"`
	Hint    string        `json:"hint,omitempty"`
}

// WithdrawCreditsResponse wraps the response from withdraw_credits command.
type WithdrawCreditsResponse struct {
	Action         string `json:"action"`
	Amount         int    `json:"amount"`
	WalletTotal    int    `json:"wallet_total"`
	StorageCredits int    `json:"storage_credits"`
}

// DepositCreditsResponse wraps the response from deposit_credits command.
type DepositCreditsResponse struct {
	Action          string `json:"action"`
	Amount          int    `json:"amount"`
	WalletRemaining int    `json:"wallet_remaining"`
	StorageCredits  int    `json:"storage_credits"`
}

// WithdrawItemsResponse wraps the response from withdraw_items command.
type WithdrawItemsResponse struct {
	Action           string `json:"action"`
	ItemID           string `json:"item_id"`
	Quantity         int    `json:"quantity"`
	StorageRemaining int    `json:"storage_remaining"`
	CargoTotal       int    `json:"cargo_total"`
	CargoSpace       int    `json:"cargo_space"`
}

// DepositItemsResponse wraps the response from deposit_items command.
type DepositItemsResponse struct {
	Action         string `json:"action"`
	ItemID         string `json:"item_id"`
	Quantity       int    `json:"quantity"`
	StorageTotal   int    `json:"storage_total"`
	CargoRemaining int    `json:"cargo_remaining"`
	CargoSpace     int    `json:"cargo_space"`
}

// GetCommandsResponse wraps the response from get_commands command.
type GetCommandsResponse struct {
	Action   string        `json:"action,omitempty"`
	Commands []GameCommand `json:"commands"`
}

// HelpResponse wraps the response from help command.
type HelpResponse struct {
	Commands    []GameCommand `json:"commands"`
	Topic       string        `json:"topic,omitempty"`
	Description string        `json:"description,omitempty"`
}

// GetGuideResponse wraps the response from get_guide command.
type GetGuideResponse struct {
	Action  string           `json:"action,omitempty"`
	Guide   string           `json:"guide,omitempty"`
	Content string           `json:"content,omitempty"`
	Guides  []GuideEntry     `json:"guides,omitempty"`
	Hint    string           `json:"hint,omitempty"`
}

// GuideEntry represents a guide in the guide listing.
type GuideEntry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// SearchChangelogResponse wraps the response from search_changelog command.
type SearchChangelogResponse struct {
	Action   string             `json:"action,omitempty"`
	Releases []ChangelogVersion `json:"releases"`
}

// ChatHistoryResponse wraps the response from get_chat_history command.
type ChatHistoryResponse struct {
	Action     string        `json:"action,omitempty"`
	Messages   []ChatMessage `json:"messages"`
	Channel    string        `json:"channel"`
	TotalCount int           `json:"total_count,omitempty"`
	HasMore    bool          `json:"has_more,omitempty"`
}

// ChatResponse wraps the response from the chat command.
type ChatResponse struct {
	Action  string `json:"action"`
	Warning string `json:"warning"`
}

// ForumListResponse wraps the response from forum_list command.
type ForumListResponse struct {
	Action                string             `json:"action,omitempty"`
	Threads               []ForumThreadSummary `json:"threads"`
	Page                  int                `json:"page,omitempty"`
	Total                 int                `json:"total,omitempty"`
	PerPage               int                `json:"per_page,omitempty"`
	HasMore               bool               `json:"has_more,omitempty"`
	Categories            []string           `json:"categories,omitempty"`
	CategoryDescriptions  map[string]string  `json:"category_descriptions,omitempty"`
}

// ForumThreadResponse wraps the response from forum_get_thread command.
type ForumThreadResponse struct {
	Action  string             `json:"action,omitempty"`
	Thread  ForumThreadSummary `json:"thread"`
	Replies []ForumReplyDetail `json:"replies"`
}

// ForumCreateThreadResponse wraps the response from forum_create_thread command.
type ForumCreateThreadResponse struct {
	Message  string `json:"message"`
	ThreadID string `json:"thread_id"`
	Title    string `json:"title"`
}

// ForumReplyResponse wraps the response from forum_reply command.
type ForumReplyResponse struct {
	Message  string `json:"message"`
	ReplyID  string `json:"reply_id"`
	ThreadID string `json:"thread_id"`
}

// SendGiftResponse wraps the response from send_gift command.
type SendGiftResponse struct {
	Action          string `json:"action"`
	Recipient       string `json:"recipient"`
	BaseID          string `json:"base_id,omitempty"`
	CargoRemaining  int    `json:"cargo_remaining,omitempty"`
	CreditsSent     int    `json:"credits_sent,omitempty"`
	ItemID          string `json:"item_id,omitempty"`
	Message         string `json:"message,omitempty"`
	Quantity        int    `json:"quantity,omitempty"`
	WalletRemaining int    `json:"wallet_remaining,omitempty"`
}

// FactionInfoResponse wraps the response from faction_info command.
type FactionInfoResponse struct {
	Action         string                `json:"action,omitempty"`
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Tag            string                `json:"tag"`
	LeaderID       string                `json:"leader_id"`
	LeaderUsername string                `json:"leader_username"`
	MemberCount    int                   `json:"member_count"`
	OwnedBases     int                   `json:"owned_bases"`
	IsMember       bool                  `json:"is_member"`
	IsAlly         bool                  `json:"is_ally"`
	IsEnemy        bool                  `json:"is_enemy"`
	AtWar          bool                  `json:"at_war"`
	Description    string                `json:"description,omitempty"`
	Charter        string                `json:"charter,omitempty"`
	Emblem         string                `json:"emblem,omitempty"`
	PrimaryColor   string                `json:"primary_color,omitempty"`
	SecondaryColor string                `json:"secondary_color,omitempty"`
	Treasury       int                   `json:"treasury,omitempty"`
	CreatedAt      string                `json:"created_at,omitempty"`
	Members        []FactionMemberDetail `json:"members,omitempty"`
	Allies         []FactionSummary      `json:"allies,omitempty"`
	Enemies        []FactionSummary      `json:"enemies,omitempty"`
	Wars           []FactionWarDetail    `json:"wars,omitempty"`
	PeaceProposals []PeaceProposal       `json:"peace_proposals,omitempty"`
}

// FactionListResponse wraps the response from faction_list command.
type FactionListResponse struct {
	Factions   []FactionSummary `json:"factions"`
	TotalCount int              `json:"total_count"`
	Offset     int              `json:"offset,omitempty"`
	Limit      int              `json:"limit,omitempty"`
}

// CreateFactionResponse wraps the response from create_faction command.
type CreateFactionResponse struct {
	Action    string `json:"action"`
	FactionID string `json:"faction_id"`
	Name      string `json:"name"`
}

// FactionInviteResponse wraps the response from faction_invite command.
type FactionInviteResponse struct {
	Message  string `json:"message"`
	PlayerID string `json:"player_id"`
}

// FactionKickResponse wraps the response from faction_kick command.
type FactionKickResponse struct {
	Message  string `json:"message"`
	PlayerID string `json:"player_id"`
}

// FactionPromoteResponse wraps the response from faction_promote command.
type FactionPromoteResponse struct {
	Message  string `json:"message"`
	PlayerID string `json:"player_id"`
	NewRole  string `json:"new_role"`
}

// FactionDeclareWarResponse wraps the response from faction_declare_war command.
type FactionDeclareWarResponse struct {
	Message         string `json:"message"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
	Reason          string `json:"reason"`
}

// FactionProposePeaceResponse wraps the response from faction_propose_peace.
type FactionProposePeaceResponse struct {
	Message         string `json:"message"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
	Terms           string `json:"terms"`
}

// FactionAcceptPeaceResponse wraps the response from faction_accept_peace.
type FactionAcceptPeaceResponse struct {
	Message         string `json:"message"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// FactionDepositCreditsResponse wraps the response from faction_deposit_credits.
type FactionDepositCreditsResponse struct {
	Action         string `json:"action"`
	Amount         int    `json:"amount"`
	PlayerCredits  int    `json:"player_credits"`
	FactionCredits int    `json:"faction_credits"`
}

// FactionWithdrawCreditsResponse wraps the response from faction_withdraw_credits.
type FactionWithdrawCreditsResponse struct {
	Action         string `json:"action"`
	Amount         int    `json:"amount"`
	PlayerCredits  int    `json:"player_credits"`
	FactionCredits int    `json:"faction_credits"`
}

// FactionSubmitIntelResponse wraps the response from faction_submit_intel.
type FactionSubmitIntelResponse struct {
	Status  string `json:"status"`
	Count   int    `json:"count"`
	Message string `json:"message"`
}

// ViewFactionStorageResponse wraps the response from view_faction_storage.
type ViewFactionStorageResponse struct {
	FactionID      string           `json:"faction_id"`
	FactionName    string           `json:"faction_name"`
	FactionTag     string           `json:"faction_tag"`
	BaseID         string           `json:"base_id"`
	Credits        int              `json:"credits"`
	Items          []CargoItem      `json:"items"`
	RecentActivity []map[string]any `json:"recent_activity,omitempty"`
}

// TravelResponse wraps the response from travel command.
type TravelResponse struct {
	Action              string         `json:"action"`
	POI                 string         `json:"poi,omitempty"`
	POIID               string         `json:"poi_id,omitempty"`
	ArrivalTick         int64          `json:"arrival_tick,omitempty"`
	Destination         string         `json:"destination,omitempty"`
	OnlinePlayersCount  int            `json:"online_players_count,omitempty"`
	OnlinePlayersTruncated bool        `json:"online_players_truncated,omitempty"`
	OnlinePlayers       []NearbyPlayer `json:"online_players,omitempty"`
}

// JumpResponse wraps the response from jump command.
type JumpResponse struct {
	Action      string `json:"action"`
	ArrivalTick int64  `json:"arrival_tick,omitempty"`
	Destination string `json:"destination,omitempty"`
	IsWormhole  bool   `json:"is_wormhole,omitempty"`
}

// JumpedResponse wraps the response from jumped event after jump completes.
type JumpedResponse struct {
	Action       string `json:"action"`
	FromSystem   string `json:"from_system,omitempty"`
	NavigationXP int    `json:"navigation_xp,omitempty"`
	POI          string `json:"poi,omitempty"`
	System       string `json:"system,omitempty"`
	SystemID     string `json:"system_id,omitempty"`
}

// DockResponse wraps the response from dock command.
type DockResponse struct {
	Action              string          `json:"action,omitempty"`
	Base                *Base           `json:"base,omitempty"`
	StationCondition    *StationHealth  `json:"station_condition,omitempty"`
	Story               string          `json:"story,omitempty"`
	OpenOrders          []ExchangeOrder `json:"open_orders,omitempty"`
	OpenOrdersCount     int             `json:"open_orders_count,omitempty"`
	OpenOrdersTruncated bool            `json:"open_orders_truncated,omitempty"`
	StorageItems        []CargoItem     `json:"storage_items,omitempty"`
	StoredShips         []StorageShip   `json:"stored_ships,omitempty"`
	StoredShipsCount    int             `json:"stored_ships_count,omitempty"`
}

// UndockResponse wraps the response from undock command.
type UndockResponse struct {
	Action string `json:"action"`
}

// RefuelResponse wraps the response from refuel command.
type RefuelResponse struct {
	Action   string `json:"action"`
	Source   string `json:"source,omitempty"`
	Fuel     int    `json:"fuel,omitempty"`
	FuelNow  int    `json:"fuel_now,omitempty"`
	FuelMax  int    `json:"fuel_max,omitempty"`
	Cost     int    `json:"cost,omitempty"`
	CellsUsed int  `json:"cells_used,omitempty"`
	ItemID   string `json:"item_id,omitempty"`
	ItemName string `json:"item_name,omitempty"`
}

// RepairResponse wraps the response from repair command.
// Supports station repair (credits), in-space repair (kits), and repairing other ships (repair arm + kits).
type RepairResponse struct {
	Action           string              `json:"action"`
	Source           string              `json:"source,omitempty"`  // "station", "kits", etc.
	Repaired         int                 `json:"repaired"`
	Cost             int                 `json:"cost,omitempty"`
	ItemID           string              `json:"item_id,omitempty"`
	ItemName         string              `json:"item_name,omitempty"`
	KitsUsed         int                 `json:"kits_used,omitempty"`
	Hull             int                 `json:"hull,omitempty"`
	MaxHull          int                 `json:"max_hull,omitempty"`
	TargetPlayerID   string              `json:"target_player_id,omitempty"`
	TargetPlayerName string              `json:"target_player_name,omitempty"`
	TargetHullNow    int                 `json:"target_hull_now,omitempty"`
	TargetHullMax    int                 `json:"target_hull_max,omitempty"`
	HasArm           bool                `json:"has_arm,omitempty"`
	FleetID          string              `json:"fleet_id,omitempty"`
	Members          []FleetMemberStatus `json:"members,omitempty"`
	Message          string              `json:"message,omitempty"`
}

// FleetMemberStatus reports the hull/shield status of a fleet member.
type FleetMemberStatus struct {
	PlayerID  string `json:"player_id"`
	Username  string `json:"username"`
	ShipClass string `json:"ship_class"`
	Hull      int    `json:"hull"`
	MaxHull   int    `json:"max_hull"`
	HullPct   int    `json:"hull_pct"`
	Shield    int    `json:"shield"`
	MaxShield int    `json:"max_shield"`
	IsLeader  bool   `json:"is_leader"`
	IsYou     bool   `json:"is_you"`
}

// MineResponse wraps the response from mine command.
type MineResponse struct {
	Action string `json:"action,omitempty"`
}

// CraftResponse wraps the response from craft command.
// v0.240: restructured — outputs is now an array, inputs tracked via from_storage.
type CraftResponse struct {
	Action             string            `json:"action"`
	Recipe             string            `json:"recipe"`
	Quantity           int               `json:"quantity"`
	Outputs            []CraftOutput     `json:"outputs"`
	FromStorage        []CraftSourceItem `json:"from_storage,omitempty"`
	FromFactionStorage []CraftSourceItem `json:"from_faction_storage,omitempty"`
	ToStorage          []CraftSourceItem `json:"to_storage,omitempty"`
	ToFactionStorage   []CraftSourceItem `json:"to_faction_storage,omitempty"`
	LevelUp            bool              `json:"level_up,omitempty"`
	LeveledUpSkills    []string          `json:"leveled_up_skills,omitempty"`
	SkillLevel         int               `json:"skill_level,omitempty"`
	XPGained           map[string]int    `json:"xp_gained,omitempty"`
	Message            string            `json:"message"`
}

// CraftOutput represents one output item from crafting.
type CraftOutput struct {
	ItemID        string `json:"item_id"`
	Name          string `json:"name"`
	Quantity      int    `json:"quantity"`
	BonusQuantity int    `json:"bonus_quantity,omitempty"`
}

// CraftSourceItem represents an item consumed from or delivered to storage.
type CraftSourceItem struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// CloakResponse wraps the response from cloak command.
type CloakResponse struct {
	Enabled       bool   `json:"enabled"`
	CloakStrength int    `json:"cloak_strength"`
	Message       string `json:"message"`
}

// ScanResponse wraps the response from scan command.
type ScanResponse struct {
	TargetID     string   `json:"target_id"`
	Success      bool     `json:"success"`
	Cloaked      bool     `json:"cloaked,omitempty"`
	RevealedInfo []string `json:"revealed_info"`
	FactionID    string   `json:"faction_id,omitempty"`
	Hull         int      `json:"hull,omitempty"`
	Shield       int      `json:"shield,omitempty"`
	ShipClass    string   `json:"ship_class,omitempty"`
	Username     string   `json:"username,omitempty"`
}

// JettisonResponse wraps the response from jettison command.
type JettisonResponse struct {
	Action    string `json:"action"`
	ItemID    string `json:"item_id"`
	ItemName  string `json:"item_name"`
	Quantity  int    `json:"quantity"`
	CargoUsed int    `json:"cargo_used"`
	CargoSpace int   `json:"cargo_space"`
}

// InstallModResponse wraps the response from install_mod command.
type InstallModResponse struct {
	Message      string `json:"message"`
	ModuleID     string `json:"module_id"`
	Quality      float64 `json:"quality,omitempty"`
	QualityGrade string `json:"quality_grade,omitempty"`
	CPUUsed      int    `json:"cpu_used"`
	PowerUsed    int    `json:"power_used"`
	CurrentAmmo  int    `json:"current_ammo,omitempty"`
	LoadedAmmo   string `json:"loaded_ammo,omitempty"`
	MagazineSize int    `json:"magazine_size,omitempty"`
}

// UninstallModResponse wraps the response from uninstall_mod command.
type UninstallModResponse struct {
	Message        string  `json:"message"`
	ModuleID       string  `json:"module_id"`
	CPUUsed        int     `json:"cpu_used"`
	PowerUsed      int     `json:"power_used"`
	Destroyed      bool    `json:"destroyed,omitempty"`
	Quality        float64 `json:"quality,omitempty"`
	QualityGrade   string  `json:"quality_grade,omitempty"`
	UninstallCount int     `json:"uninstall_count,omitempty"`
	Wear           float64 `json:"wear,omitempty"`
	WearStatus     string  `json:"wear_status,omitempty"`
}

// ReloadResponse wraps the response from reload command.
type ReloadResponse struct {
	Action         string `json:"action"`
	WeaponName     string `json:"weapon_name"`
	WeaponID       string `json:"weapon_id"`
	AmmoName       string `json:"ammo_name"`
	AmmoID         string `json:"ammo_id"`
	CurrentAmmo    int    `json:"current_ammo"`
	MagazineSize   int    `json:"magazine_size"`
	PreviousAmmo   string `json:"previous_ammo,omitempty"`
	RoundsDiscarded int   `json:"rounds_discarded,omitempty"`
}

// UseItemResponse wraps the response from use_item command.
type UseItemResponse struct {
	Action            string       `json:"action"`
	EffectType        string       `json:"effect_type"`
	ItemID            string       `json:"item_id"`
	ItemName          string       `json:"item_name"`
	QuantityUsed      int          `json:"quantity_used"`
	QuantityRemaining int          `json:"quantity_remaining"`
	ActiveBuffs       []ActiveBuff `json:"active_buffs,omitempty"`
	Amount            int          `json:"amount,omitempty"`
	Duration          int          `json:"duration,omitempty"`
	ExpiresAt         int          `json:"expires_at,omitempty"`
	Hull              int          `json:"hull,omitempty"`
	HullRestored      int          `json:"hull_restored,omitempty"`
	MaxHull           int          `json:"max_hull,omitempty"`
	Shield            int          `json:"shield,omitempty"`
	ShieldRestored    int          `json:"shield_restored,omitempty"`
	MaxShield         int          `json:"max_shield,omitempty"`
	Stat              string       `json:"stat,omitempty"`
	Message           string       `json:"message,omitempty"`
	DestinationPOI    string       `json:"destination_poi,omitempty"`
	DestinationSystem string       `json:"destination_system,omitempty"`
	OriginPOI         string       `json:"origin_poi,omitempty"`
	OriginSystem      string       `json:"origin_system,omitempty"`
	DockedAt          string       `json:"docked_at,omitempty"`
	HomeBaseName      string       `json:"home_base_name,omitempty"`
}

// LootWreckResponse wraps the response from loot_wreck command.
type LootWreckResponse struct {
	Quantity     int     `json:"quantity"`
	WreckEmpty   bool    `json:"wreck_empty"`
	ItemID       string  `json:"item_id,omitempty"`
	ModuleID     string  `json:"module_id,omitempty"`
	ModuleTypeID string  `json:"module_type_id,omitempty"`
	Quality      float64 `json:"quality,omitempty"`
	Wear         float64 `json:"wear,omitempty"`
}

// SalvageWreckResponse wraps the response from salvage_wreck command.
type SalvageWreckResponse struct {
	MetalScrap    int  `json:"metal_scrap"`
	Components    int  `json:"components"`
	RareMaterials int  `json:"rare_materials"`
	TotalValue    int  `json:"total_value"`
	XPGained      int  `json:"xp_gained"`
	LevelUp       bool `json:"level_up,omitempty"`
}

// TowWreckResponse wraps the response from tow_wreck command.
type TowWreckResponse struct {
	Action       string `json:"action"`
	WreckID      string `json:"wreck_id"`
	Insured      bool   `json:"insured"`
	Message      string `json:"message"`
	CargoCount   int    `json:"cargo_count,omitempty"`
	ModuleCount  int    `json:"module_count,omitempty"`
	SalvageValue int    `json:"salvage_value,omitempty"`
	ShipClass    string `json:"ship_class,omitempty"`
	SpeedPenalty string `json:"speed_penalty,omitempty"`
}

// SellWreckResponse wraps the response from sell_wreck command.
type SellWreckResponse struct {
	Action       string `json:"action"`
	WreckID      string `json:"wreck_id"`
	TotalPayout  int    `json:"total_payout"`
	NewBalance   int    `json:"new_balance"`
	Message      string `json:"message"`
	CargoValue   int    `json:"cargo_value,omitempty"`
	SalvageValue int    `json:"salvage_value,omitempty"`
	ShipClass    string `json:"ship_class,omitempty"`
}

// DeployDroneResponse wraps the response from deploy_drone command.
type DeployDroneResponse struct {
	DroneID        string `json:"drone_id"`
	DroneType      string `json:"drone_type"`
	Hull           int    `json:"hull"`
	MaxHull        int    `json:"max_hull"`
	Damage         int    `json:"damage"`
	Status         string `json:"status"`
	BandwidthUsed  int    `json:"bandwidth_used"`
	BandwidthTotal int    `json:"bandwidth_total"`
	Message        string `json:"message"`
}

// BuildBaseResponse wraps the response from build_base command.
type BuildBaseResponse struct {
	BaseID       string         `json:"base_id"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	POIID        string         `json:"poi_id"`
	SystemID     string         `json:"system_id"`
	CreditsSpent int            `json:"credits_spent"`
	ItemsUsed    map[string]int `json:"items_used"`
	DefenseLevel int            `json:"defense_level"`
	Message      string         `json:"message"`
}

// AttackBaseResponse wraps the response from attack_base command.
type AttackBaseResponse struct {
	BaseID        string `json:"base_id"`
	BaseName      string `json:"base_name"`
	CurrentHealth int    `json:"current_health"`
	MaxHealth     int    `json:"max_health"`
	Message       string `json:"message"`
}

// LoginResponse wraps the response from the logged_in message.
type LoginResponse struct {
	Message       string             `json:"message,omitempty"`
	Player        Player             `json:"player"`
	Ship          Ship               `json:"ship"`
	System        SystemData         `json:"system"`
	POI           POI                `json:"poi"`
	CaptainsLog   []CaptainsLogEntry `json:"captains_log,omitempty"`
	PendingTrades []PendingTradeInfo `json:"pending_trades,omitempty"`
	ReleaseInfo   *ReleaseInfo       `json:"release_info,omitempty"`
	UnreadChat    *UnreadChat        `json:"unread_chat,omitempty"`
}

// RegisterResponse wraps the response from the register command.
type RegisterResponse struct {
	Message       string             `json:"message"`
	Password      string             `json:"password"`
	PlayerID      string             `json:"player_id"`
	Player        Player             `json:"player"`
	Ship          Ship               `json:"ship"`
	System        SystemData         `json:"system"`
	POI           POI                `json:"poi"`
	CaptainsLog   []CaptainsLogEntry `json:"captains_log,omitempty"`
	PendingTrades []PendingTradeInfo `json:"pending_trades,omitempty"`
	ReleaseInfo   *ReleaseInfo       `json:"release_info,omitempty"`
	UnreadChat    *UnreadChat        `json:"unread_chat,omitempty"`
}

// GetVersionResponse wraps the response from get_version command.
type GetVersionResponse struct {
	Action      string             `json:"action,omitempty"`
	Version     string             `json:"version"`
	ReleaseDate string             `json:"release_date"`
	Notes       []string           `json:"notes"`
	Page        int                `json:"page,omitempty"`
	PerPage     int                `json:"per_page,omitempty"`
	Total       int                `json:"total,omitempty"`
	TotalPages  int                `json:"total_pages,omitempty"`
	Versions    []ChangelogVersion `json:"versions,omitempty"`
}

// SetAnonymousResponse wraps the response from set_anonymous command.
type SetAnonymousResponse struct {
	Action    string `json:"action"`
	Anonymous bool   `json:"anonymous"`
}

// SetColorsResponse wraps the response from set_colors command.
type SetColorsResponse struct {
	Action string `json:"action"`
}

// SetStatusResponse wraps the response from set_status command.
type SetStatusResponse struct {
	Action string `json:"action"`
}

// SetHomeBaseResponse wraps the response from set_home_base command.
type SetHomeBaseResponse struct {
	Message     string `json:"message"`
	HomeBase    string `json:"home_base"`
	OldHomeBase string `json:"old_home_base,omitempty"`
}

// GetActionLogResponse wraps the response from get_action_log command.
type GetActionLogResponse struct {
	Entries  []ActionLogEntry `json:"entries"`
	HasMore  bool             `json:"has_more"`
	Category string           `json:"category,omitempty"`
	Limit    int              `json:"limit,omitempty"`
}

// ActionLogEntry represents a single entry in the action log.
type ActionLogEntry struct {
	ID        int            `json:"id"`
	EventType string         `json:"event_type"`
	Category  string         `json:"category,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
}

// GetBaseCostResponse wraps the response from get_base_cost command.
type GetBaseCostResponse struct {
	StationTypes []StationType  `json:"station_types"`
	Requirements []string       `json:"requirements"`
	BaseCost     map[string]int `json:"base_cost,omitempty"`
	Message      string         `json:"message,omitempty"`
}

// StationType represents a type of station that can be built.
type StationType struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Cost        int    `json:"cost"`
}

// RaidStatusResponse wraps the response from raid_status command.
type RaidStatusResponse struct {
	ActiveRaids []ActiveRaid `json:"active_raids"`
	Message     string       `json:"message,omitempty"`
}

// ActiveRaid represents an ongoing base raid.
type ActiveRaid struct {
	BaseID        string `json:"base_id"`
	BaseName      string `json:"base_name"`
	OwnerID       string `json:"owner_id,omitempty"`
	OwnerName     string `json:"owner_name,omitempty"`
	CurrentHealth int    `json:"current_health"`
	MaxHealth     int    `json:"max_health"`
	DamagePerTick int    `json:"damage_per_tick,omitempty"`
	AttackerCount int    `json:"attacker_count,omitempty"`
	IsAttacker    bool   `json:"is_attacker,omitempty"`
}

// PendingActionResponse wraps the response from a pending action.
type PendingActionResponse struct {
	Pending bool   `json:"pending"`
	Command string `json:"command,omitempty"`
	Message string `json:"message"`
}

// MessageResponse is a generic response with just a message field.
type MessageResponse struct {
	Message string `json:"message"`
}

// FleetResponse wraps the response from fleet command.
// v0.240: new command for creating and managing player fleets.
type FleetResponse struct {
	Action        string           `json:"action"`
	FleetID       string           `json:"fleet_id,omitempty"`
	InFleet       bool             `json:"in_fleet,omitempty"`
	IsLeader      bool             `json:"is_leader,omitempty"`
	Leader        string           `json:"leader,omitempty"`
	LeaderName    string           `json:"leader_name,omitempty"`
	Members       []map[string]any `json:"members,omitempty"`
	MaxSize       int              `json:"max_size,omitempty"`
	SystemID      string           `json:"system_id,omitempty"`
	POIID         string           `json:"poi_id,omitempty"`
	PendingInvite bool             `json:"pending_invite,omitempty"`
	InviteFrom    string           `json:"invite_from,omitempty"`
	InviteFleet   string           `json:"invite_fleet,omitempty"`
	Invites       []map[string]any `json:"invites,omitempty"`
	Message       string           `json:"message"`
}

// DistressSignalResponse wraps the response from distress_signal command.
// v0.240: now accepts distress_type parameter.
type DistressSignalResponse struct {
	Action         string `json:"action"`
	DistressType   string `json:"distress_type,omitempty"`
	System         string `json:"system,omitempty"`
	SystemName     string `json:"system_name,omitempty"`
	POI            string `json:"poi,omitempty"`
	POIName        string `json:"poi_name,omitempty"`
	MissionsSent   int    `json:"missions_sent,omitempty"`
	ExpiresSeconds int    `json:"expires_seconds,omitempty"`
	Message        string `json:"message"`
}
