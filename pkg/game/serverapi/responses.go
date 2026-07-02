package serverapi

import "encoding/json"

// ============================================================================
// Server Response Wrappers
// These wrap the actual data returned by server commands.
// They should match the OpenAPI spec at server_docs/openapi.json.
// ============================================================================

// GetSystemResponse wraps the response from get_system command.
type GetSystemResponse struct {
	Action         string        `json:"action"`
	POI            CurrentPOI    `json:"poi,omitempty"`
	SecurityStatus string        `json:"security_status"`
	System         SystemData    `json:"system"`
	ActiveBattle   *ActiveBattle `json:"active_battle,omitempty"`
	FromSystem     string        `json:"from_system,omitempty"`
	InTransit      bool          `json:"in_transit,omitempty"`
	TicksRemaining int           `json:"ticks_remaining,omitempty"`
	ToSystem       string        `json:"to_system,omitempty"`
	TransitType    string        `json:"transit_type,omitempty"`
}

// GetPOIResponse wraps the response from get_poi command.
type GetPOIResponse struct {
	Action        string            `json:"action,omitempty"`
	POI           POI               `json:"poi"`
	Base          *Base             `json:"base,omitempty"`
	PoliceDrones  int               `json:"police_drones,omitempty"`
	PoliceWarning string            `json:"police_warning,omitempty"`
	Resources     []ResourceDisplay `json:"resources,omitempty"`
	Services      []string          `json:"services,omitempty"`
}

// GetStatusResponse wraps the response from get_status command.
type GetStatusResponse struct {
	Action      string         `json:"action,omitempty"`
	Player      Player         `json:"player"`
	Ship        Ship           `json:"ship"`
	Modules     []ShipModule   `json:"modules"`
	System      SystemData     `json:"system,omitempty"`
	POI         POI            `json:"poi,omitempty"`
	Nearby      []NearbyPlayer `json:"nearby,omitempty"`
	CurrentTick int64          `json:"current_tick,omitempty"`
}

// GetShipResponse wraps the response from get_ship command.
type GetShipResponse struct {
	Ship      Ship         `json:"ship"`
	Modules   []ShipModule `json:"modules"`
	CargoUsed int          `json:"cargo_used"`
	CargoMax  int          `json:"cargo_max"`
	Class     *ShipClass   `json:"class,omitempty"`
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

	FactionFuelCapacity int   `json:"faction_fuel_capacity,omitempty"`
	FactionFuelReserve  int   `json:"faction_fuel_reserve,omitempty"`
	FuelPrice           int64 `json:"fuel_price,omitempty"`
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

// GetCargoResponse wraps the response from get_cargo command.
type GetCargoResponse struct {
	Action    string      `json:"action,omitempty"`
	Cargo     []CargoItem `json:"cargo"`
	Used      int         `json:"used"`
	Capacity  int         `json:"capacity"`
	Available int         `json:"available"`

	BayCapacity  int             `json:"bay_capacity,omitempty"`
	BayUsed      int             `json:"bay_used,omitempty"`
	CarriedShips json.RawMessage `json:"carried_ships,omitempty"`
}

// GetNearbyResponse wraps the response from get_nearby command.
type GetNearbyResponse struct {
	Action           string          `json:"action,omitempty"`
	Nearby           []NearbyPlayer  `json:"nearby"`
	Pirates          []NearbyPirate  `json:"pirates,omitempty"`
	Count            int             `json:"count"`
	PirateCount      int             `json:"pirate_count,omitempty"`
	POIID            string          `json:"poi_id,omitempty"`
	EmpireNPCCount   int             `json:"empire_npc_count,omitempty"`
	EmpireNPCs       json.RawMessage `json:"empire_npcs,omitempty"`
	OfflineCollapsed int             `json:"offline_collapsed,omitempty"`
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

// BattleAlertParticipant is a slimmer form of BattleParticipant used in
// battle_alert push events. The alert only carries identity fields
// (no hp/damage/kill counts); side_id is an integer here, unlike the
// string-typed SideID on BattleParticipant.
type BattleAlertParticipant struct {
	PlayerID  string `json:"player_id"`
	Username  string `json:"username"`
	ShipClass string `json:"ship_class"`
	ShipName  string `json:"ship_name,omitempty"`
	SideID    int    `json:"side_id"`
	Stance    string `json:"stance,omitempty"`
	Zone      string `json:"zone,omitempty"`
}

// BattleAlertSide mirrors BattleSide but uses an integer side_id to match
// the battle_alert payload shape.
type BattleAlertSide struct {
	SideID      int    `json:"side_id"`
	FactionID   string `json:"faction_id,omitempty"`
	PlayerCount int    `json:"player_count"`
}

// BattleAlertResponse wraps the payload of a battle_alert push event —
// broadcast to clients in the same system when a battle begins. It is
// informational; no action required from the receiver.
type BattleAlertResponse struct {
	BattleID     string                   `json:"battle_id"`
	SystemID     string                   `json:"system_id,omitempty"`
	Message      string                   `json:"message,omitempty"`
	Participants []BattleAlertParticipant `json:"participants,omitempty"`
	Sides        []BattleAlertSide        `json:"sides,omitempty"`
}

// RetreatResponse wraps the response from retreat action during combat.
type RetreatResponse struct {
	Action  string `json:"action"`
	Message string `json:"message"`
}

// ViewMarketResponse wraps the response from view_market command.
type ViewMarketResponse struct {
	Action      string           `json:"action"`
	Base        string           `json:"base,omitempty"`
	BaseID      string           `json:"base_id,omitempty"`
	Items       []ViewMarketItem `json:"items"`
	Message     string           `json:"message,omitempty"`
	Categories  []string         `json:"categories,omitempty"`
	CurrentTick int64            `json:"current_tick,omitempty"`
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
	ItemFilter             string          `json:"item_filter,omitempty"`
	OrderType              string          `json:"order_type,omitempty"`
	SearchTerm             string          `json:"search_term,omitempty"`
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
	SalesTax          int64       `json:"sales_tax,omitempty"`
	SalesTaxRateBps   int         `json:"sales_tax_rate_bps,omitempty"`
	Subtotal          int64       `json:"subtotal,omitempty"`
}

// BuyResponse wraps the response from buy command.
type BuyResponse struct {
	Action             string           `json:"action"`
	Item               string           `json:"item"`
	ItemID             string           `json:"item_id"`
	Quantity           int              `json:"quantity"`
	TotalCost          int              `json:"total_cost"`
	Fills              []OrderFill      `json:"fills"`
	LevelUp            bool             `json:"level_up"`
	AutoListed         *AutoListedOrder `json:"auto_listed,omitempty"`
	DeliveredToCargo   int              `json:"delivered_to_cargo,omitempty"`
	DeliveredToStorage int              `json:"delivered_to_storage,omitempty"`
	Message            string           `json:"message,omitempty"`
	Unfilled           int              `json:"unfilled,omitempty"`
	XPGained           int              `json:"xp_gained,omitempty"`
}

// SellResponse wraps the response from sell command.
type SellResponse struct {
	Action           string           `json:"action"`
	Item             string           `json:"item"`
	ItemID           string           `json:"item_id"`
	QuantitySold     int              `json:"quantity_sold"`
	TotalEarned      int              `json:"total_earned"`
	Fills            []OrderFill      `json:"fills"`
	LevelUp          bool             `json:"level_up"`
	AutoListed       *AutoListedOrder `json:"auto_listed,omitempty"`
	Message          string           `json:"message,omitempty"`
	SkillLevel       int              `json:"skill_level,omitempty"`
	Unsold           int              `json:"unsold,omitempty"`
	XPGained         int              `json:"xp_gained,omitempty"`
	SmugglingLevelUp bool             `json:"smuggling_level_up,omitempty"`
	SmugglingXP      int64            `json:"smuggling_xp,omitempty"`
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
	Mode               string      `json:"mode,omitempty"`
	Results            any         `json:"results,omitempty"`
	Summary            any         `json:"summary,omitempty"`
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
	Consolidated      bool        `json:"consolidated,omitempty"`
	SmugglingLevelUp  bool        `json:"smuggling_level_up,omitempty"`
	SmugglingXP       int         `json:"smuggling_xp,omitempty"`
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
	Mode            string     `json:"mode,omitempty"`
	Results         []any      `json:"results,omitempty"`
	Summary         any        `json:"summary,omitempty"`
}

// ModifyOrderResponse wraps the response from modify_order command.
type ModifyOrderResponse struct {
	Action     string `json:"action"`
	OrderID    string `json:"order_id"`
	OldPrice   int    `json:"old_price"`
	NewPrice   int    `json:"new_price"`
	Message    string `json:"message"`
	ListingFee int    `json:"listing_fee,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Results    []any  `json:"results,omitempty"`
	Summary    any    `json:"summary,omitempty"`
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
	Message    string `json:"message"`
	MissionID  string `json:"mission_id"`
	Title      string `json:"title"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	TemplateID string `json:"template_id,omitempty"`
	Type       string `json:"type,omitempty"`
}

// CompleteMissionResponse is returned by complete_mission. The server sends
// reward fields at the top level (not nested under "rewards").
//   - complete_mission
type CompleteMissionResponse struct {
	Message              string          `json:"message"`
	MissionID            string          `json:"mission_id"`
	Title                string          `json:"title"`
	ChainNext            string          `json:"chain_next,omitempty"`
	CreditsEarned        int64           `json:"credits_earned,omitempty"`
	ItemsReceived        json.RawMessage `json:"items_received,omitempty"`
	SkillXPGained        json.RawMessage `json:"skill_xp_gained,omitempty"`
	CommunityContributed json.RawMessage `json:"community_contributed,omitempty"`
	CommunityProgress    json.RawMessage `json:"community_progress,omitempty"`
	CommunityPercent     float64         `json:"community_percent,omitempty"`
}

// CatalogResponse wraps the response from catalog command.
type CatalogResponse struct {
	Type                 string          `json:"type"`
	Items                []CatalogItem   `json:"items"`
	Total                int             `json:"total"`
	Page                 int             `json:"page"`
	PageSize             int             `json:"page_size"`
	TotalPages           int             `json:"total_pages"`
	Message              string          `json:"message"`
	Analysis             json.RawMessage `json:"analysis,omitempty"`
	PassiveRecipeDetails json.RawMessage `json:"passive_recipe_details,omitempty"`
	Recipes              json.RawMessage `json:"recipes,omitempty"`
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

// Achievement is a single entry in a get_achievements response. Locked entries
// carry a Progress pointer (nil when the achievement has no measurable
// progress); hidden/secret entries report Name/Description as "???" until
// earned.
type Achievement struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Category    string               `json:"category"`
	Description string               `json:"description"`
	Points      int                  `json:"points"`
	Earned      bool                 `json:"earned"`
	Hidden      bool                 `json:"hidden"`
	EarnedAt    string               `json:"earned_at,omitempty"`
	ShareURL    string               `json:"share_url,omitempty"`
	Progress    *AchievementProgress `json:"progress,omitempty"`
}

// AchievementProgress is the current/target counter for an unearned achievement.
type AchievementProgress struct {
	Current int `json:"current"`
	Target  int `json:"target"`
}

// AchievementSummary is the roll-up at the foot of a get_achievements response.
type AchievementSummary struct {
	Earned int `json:"earned"`
	Points int `json:"points"`
	Total  int `json:"total"`
}

// GetAchievementsResponse wraps the response from get_achievements command.
type GetAchievementsResponse struct {
	Achievements []Achievement      `json:"achievements"`
	Summary      AchievementSummary `json:"summary"`
	Message      string             `json:"message"`
}

// CommissionStatusResponse wraps the response from commission_status command.
type CommissionStatusResponse struct {
	Commissions []CommissionDetail `json:"commissions"`
	Count       int                `json:"count"`
}

// CommissionQuoteResponse wraps the response from commission_quote command.
type CommissionQuoteResponse struct {
	Message                   string      `json:"message"`
	ShipClass                 string      `json:"ship_class"`
	ShipName                  string      `json:"ship_name,omitempty"`
	CanCommission             bool        `json:"can_commission"`
	CreditsOnlyTotal          int         `json:"credits_only_total"`
	ProvideMaterialsTotal     int         `json:"provide_materials_total"`
	CreditsOnlyAvailable      bool        `json:"credits_only_available"`
	CanAffordCreditsOnly      bool        `json:"can_afford_credits_only"`
	CanAffordProvideMaterials bool        `json:"can_afford_provide_materials"`
	Blockers                  []string    `json:"blockers,omitempty"`
	BuildMaterials            []CargoItem `json:"build_materials,omitempty"`
	BuildTime                 int         `json:"build_time,omitempty"`
	LaborCost                 int         `json:"labor_cost,omitempty"`
	MaterialCost              int         `json:"material_cost,omitempty"`
	PlayerCredits             int         `json:"player_credits,omitempty"`
	ShipyardTierHere          int         `json:"shipyard_tier_here,omitempty"`
	ShipyardTierRequired      int         `json:"shipyard_tier_required,omitempty"`
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
	Message         string      `json:"message"`
	ActiveShipID    string      `json:"active_ship_id"`
	ActiveShipClass string      `json:"active_ship_class"`
	StoredShipID    string      `json:"stored_ship_id"`
	StoredShipClass string      `json:"stored_ship_class"`
	CargoNote       string      `json:"cargo_note,omitempty"`
	CargoToStorage  []CargoItem `json:"cargo_to_storage,omitempty"`
}

// BuyListedShipResponse wraps the response from buy_listed_ship command.
type BuyListedShipResponse struct {
	Message     string `json:"message"`
	ShipID      string `json:"ship_id"`
	ClassID     string `json:"class_id"`
	Price       int    `json:"price"`
	CreditsLeft int    `json:"credits_left"`
	OldShipID   string `json:"old_ship_id,omitempty"`
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

// ReadNoteResponse is returned by read_note. The server sends note fields at
// the top level (not nested), so they are flattened here.
//   - read_note
type ReadNoteResponse struct {
	Action    string `json:"action,omitempty"`
	NoteID    string `json:"note_id"`
	Title     string `json:"title"`
	Content   string `json:"content,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Value     int    `json:"value,omitempty"`
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
	AnomalyHint     string           `json:"anomaly_hint,omitempty"`
}

// FacilityResponse wraps the response from facility command.
// FacilityTypeInfo describes a facility type. Used by both the paginated
// types list (which uses "id") and the single-type detail (which uses "type_id").
type FacilityTypeInfo struct {
	ID                   string      `json:"id,omitempty"`      // paginated list identifier
	TypeID               string      `json:"type_id,omitempty"` // detail response identifier
	Name                 string      `json:"name,omitempty"`
	Category             string      `json:"category,omitempty"`
	Description          string      `json:"description,omitempty"`
	SatisfiedDescription string      `json:"satisfied_description,omitempty"`
	DegradedDescription  string      `json:"degraded_description,omitempty"`
	Level                int         `json:"level,omitempty"`
	Buildable            bool        `json:"buildable,omitempty"`
	BuildCost            int         `json:"build_cost,omitempty"`
	BuildMaterials       []CargoItem `json:"build_materials,omitempty"`
	BuildTime            int         `json:"build_time,omitempty"`
	LaborCost            int         `json:"labor_cost,omitempty"`
	MaintenancePerCycle  []CargoItem `json:"maintenance_per_cycle,omitempty"`
	RentPerCycle         int         `json:"rent_per_cycle,omitempty"`
	PersonalService      string      `json:"personal_service,omitempty"`
	Service              string      `json:"service,omitempty"`
	FactionService       string      `json:"faction_service,omitempty"`
	FactionCap           int         `json:"faction_cap,omitempty"`
	BonusType            string      `json:"bonus_type,omitempty"`
	BonusValue           float64     `json:"bonus_value,omitempty"`
	Lore                 string      `json:"lore,omitempty"`
	RecipeID             string      `json:"recipe_id,omitempty"`
	Recipe               string      `json:"recipe,omitempty"`
	RecipeMultiplier     float64     `json:"recipe_multiplier,omitempty"`
	Hint                 string      `json:"hint,omitempty"`
	UpgradesTo           string      `json:"upgrades_to,omitempty"`
	UpgradesToName       string      `json:"upgrades_to_name,omitempty"`
	UpgradesFrom         string      `json:"upgrades_from,omitempty"`
	UpgradesFromName     string      `json:"upgrades_from_name,omitempty"`
	RequiresServiceName  string      `json:"requires_service_name,omitempty"`
	RequiresServiceType  string      `json:"requires_service_type,omitempty"`
}

type FacilityResponse struct {
	Action       string             `json:"action"`
	Command      string             `json:"command,omitempty"`
	BaseID       string             `json:"base_id,omitempty"`
	FacilityID   string             `json:"facility_id,omitempty"`
	FacilityName string             `json:"facility_name,omitempty"`
	FacilityType string             `json:"facility_type,omitempty"`
	Category     string             `json:"category,omitempty"`
	Description  string             `json:"description,omitempty"`
	Hint         string             `json:"hint,omitempty"`
	Message      string             `json:"message,omitempty"`
	Page         int                `json:"page,omitempty"`
	PerPage      int                `json:"per_page,omitempty"`
	Total        int                `json:"total,omitempty"`
	TotalPages   int                `json:"total_pages,omitempty"`
	Actions      []map[string]any   `json:"actions,omitempty"`
	Types        []FacilityTypeInfo `json:"types,omitempty"`
	Upgrades     []map[string]any   `json:"upgrades,omitempty"`
}

// FacilityForSaleListing is one row in a browse_for_sale response. Field set
// inferred from the request schema (list_for_sale takes price; buy_listing
// keys off listing_id); other fields are likely-present based on the domain
// and will be filled in as real (non-empty) responses arrive. Unknown server
// fields are ignored by encoding/json so this struct can grow defensively.
type FacilityForSaleListing struct {
	ListingID      string `json:"listing_id"`
	FacilityID     string `json:"facility_id,omitempty"`
	FacilityType   string `json:"facility_type,omitempty"`
	FacilityName   string `json:"facility_name,omitempty"`
	Category       string `json:"category,omitempty"`
	Price          int    `json:"price,omitempty"`
	SellerID       string `json:"seller_id,omitempty"`
	SellerName     string `json:"seller_name,omitempty"`
	OwnedByFaction bool   `json:"owned_by_faction,omitempty"`
	FactionID      string `json:"faction_id,omitempty"`
	FactionName    string `json:"faction_name,omitempty"`
	ListedAt       string `json:"listed_at,omitempty"`
}

// BrowseForSaleResponse wraps the response from facility(action="browse_for_sale").
// Returns the facility listings available at the current station.
type BrowseForSaleResponse struct {
	Action   string                   `json:"action"`
	BaseID   string                   `json:"base_id"`
	BaseName string                   `json:"base_name"`
	Count    int                      `json:"count"`
	Listings []FacilityForSaleListing `json:"listings"`
}

// OwnedFacility is one entry in a FacilityOwnedResponse.
type OwnedFacility struct {
	FacilityID        string `json:"facility_id"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	BaseID            string `json:"base_id"`
	BaseName          string `json:"base_name"`
	SystemID          string `json:"system_id"`
	RentPerCycle      int    `json:"rent_per_cycle"`
	LaborPerRun       int    `json:"labor_per_run,omitempty"`
	ArrearsOwed       int    `json:"arrears_owed,omitempty"`
	MissedRentCycles  int    `json:"missed_rent_cycles,omitempty"`
	Active            bool   `json:"active"`
	UnderConstruction bool   `json:"under_construction,omitempty"`
}

// FacilityRentSummary is the rent summary block in a FacilityOwnedResponse.
type FacilityRentSummary struct {
	Facilities        int    `json:"facilities"`
	TotalRentPerCycle int    `json:"total_rent_per_cycle"`
	EstRentPerDay     int    `json:"est_rent_per_day"`
	ArrearsOwed       int    `json:"arrears_owed,omitempty"`
	GraceCycles       int    `json:"grace_cycles,omitempty"`
	Note              string `json:"note,omitempty"`
}

// FacilityOwnedResponse models the `facility action=owned` OK frame. Top-level
// keys (action, facilities, hint, rent) must be covered so the api-monitor does
// not flag it. Mirrors the local struct in play_as formatFacilityOwned.
type FacilityOwnedResponse struct {
	Action     string              `json:"action"`
	Facilities []OwnedFacility     `json:"facilities"`
	Hint       string              `json:"hint,omitempty"`
	Rent       FacilityRentSummary `json:"rent"`
}

// FacilityTypesResponse wraps the response from facility action="types" which
// returns a paginated list of facility type details.
type FacilityTypesResponse struct {
	Action     string             `json:"action"`
	Hint       string             `json:"hint,omitempty"`
	Page       int                `json:"page,omitempty"`
	PerPage    int                `json:"per_page,omitempty"`
	Total      int                `json:"total,omitempty"`
	TotalPages int                `json:"total_pages,omitempty"`
	Types      []FacilityTypeInfo `json:"types,omitempty"`
	// Single type detail fields (returned when querying a specific type)
	TypeID               string      `json:"type_id,omitempty"`
	Name                 string      `json:"name,omitempty"`
	Category             string      `json:"category,omitempty"`
	Description          string      `json:"description,omitempty"`
	SatisfiedDescription string      `json:"satisfied_description,omitempty"`
	DegradedDescription  string      `json:"degraded_description,omitempty"`
	Level                int         `json:"level,omitempty"`
	Buildable            bool        `json:"buildable,omitempty"`
	BuildCost            int         `json:"build_cost,omitempty"`
	BuildMaterials       []CargoItem `json:"build_materials,omitempty"`
	BuildTime            int         `json:"build_time,omitempty"`
	LaborCost            int         `json:"labor_cost,omitempty"`
	MaintenancePerCycle  []CargoItem `json:"maintenance_per_cycle,omitempty"`
	RentPerCycle         int         `json:"rent_per_cycle,omitempty"`
	PersonalService      string      `json:"personal_service,omitempty"`
	Service              string      `json:"service,omitempty"`
	FactionService       string      `json:"faction_service,omitempty"`
	FactionCap           int         `json:"faction_cap,omitempty"`
	BonusType            string      `json:"bonus_type,omitempty"`
	BonusValue           float64     `json:"bonus_value,omitempty"`
	Lore                 string      `json:"lore,omitempty"`
	RecipeID             string      `json:"recipe_id,omitempty"`
	Recipe               string      `json:"recipe,omitempty"`
	RecipeMultiplier     float64     `json:"recipe_multiplier,omitempty"`
	UpgradesTo           string      `json:"upgrades_to,omitempty"`
	UpgradesToName       string      `json:"upgrades_to_name,omitempty"`
	UpgradesFrom         string      `json:"upgrades_from,omitempty"`
	UpgradesFromName     string      `json:"upgrades_from_name,omitempty"`
	RequiresServiceName  string      `json:"requires_service_name,omitempty"`
	RequiresServiceType  string      `json:"requires_service_type,omitempty"`
	// Pagination/filter envelope fields (added when action="types" returns
	// a list — the server now includes a categories index, applied filters,
	// and a pagination block alongside the page/per_page/total fields above).
	Categories []string       `json:"categories,omitempty"`
	Filters    map[string]any `json:"filters,omitempty"`
	Pagination map[string]any `json:"pagination,omitempty"`
}

// FacilityListResponse wraps the response from the `facility action=list`
// command, which the server returns with action="list". Alongside the
// per-section facility lists it carries a station-wide construction queue and
// a power-grid summary block.
type FacilityListResponse struct {
	Action            string           `json:"action,omitempty"`
	BaseID            string           `json:"base_id"`
	StationFacilities []map[string]any `json:"station_facilities"`
	PlayerFacilities  []map[string]any `json:"player_facilities"`
	FactionFacilities []map[string]any `json:"faction_facilities"`
	PublicFacilities  []map[string]any `json:"public_facilities"`
	Construction      map[string]any   `json:"construction,omitempty"`
	Power             map[string]any   `json:"power,omitempty"`
}

// ViewStorageResponse wraps the response from view_storage command.
type ViewStorageResponse struct {
	BaseID  string        `json:"base_id"`
	Credits int           `json:"credits"`
	Items   []CargoItem   `json:"items"`
	Ships   []StorageShip `json:"ships,omitempty"`
	Gifts   []StorageGift `json:"gifts,omitempty"`
	Hint    string        `json:"hint,omitempty"`

	Messages json.RawMessage `json:"messages,omitempty"`
}

// WithdrawItemsResponse wraps the response from withdraw_items command, which
// transfers items between cargo, station storage, and faction storage. The
// server returns a generic transfer shape: source/destination plus the resulting
// totals (dest_total at the destination, source_remaining at the source). Legacy
// storage->cargo responses also carry storage_remaining / cargo_total / cargo_space.
type WithdrawItemsResponse struct {
	Action           string `json:"action"`
	ItemID           string `json:"item_id"`
	Quantity         int    `json:"quantity"`
	Source           string `json:"source,omitempty"`
	Destination      string `json:"destination,omitempty"`
	DestTotal        int    `json:"dest_total,omitempty"`
	SourceRemaining  int    `json:"source_remaining,omitempty"`
	StorageRemaining int    `json:"storage_remaining,omitempty"`
	CargoTotal       int    `json:"cargo_total,omitempty"`
	CargoSpace       int    `json:"cargo_space,omitempty"`
}

// DepositItemsResponse wraps the response from deposit_items command, which
// transfers items between cargo, station storage, and faction storage. The
// server returns a generic transfer shape: source/destination plus the resulting
// totals (dest_total at the destination, source_remaining at the source). Legacy
// cargo->storage responses also carry storage_total / cargo_remaining / cargo_space.
type DepositItemsResponse struct {
	Action          string `json:"action"`
	ItemID          string `json:"item_id"`
	Quantity        int    `json:"quantity"`
	Source          string `json:"source,omitempty"`
	Destination     string `json:"destination,omitempty"`
	DestTotal       int    `json:"dest_total,omitempty"`
	SourceRemaining int    `json:"source_remaining,omitempty"`
	StorageTotal    int    `json:"storage_total,omitempty"`
	CargoRemaining  int    `json:"cargo_remaining,omitempty"`
	CargoSpace      int    `json:"cargo_space,omitempty"`
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
	Action  string       `json:"action,omitempty"`
	Guide   string       `json:"guide,omitempty"`
	Content string       `json:"content,omitempty"`
	Guides  []GuideEntry `json:"guides,omitempty"`
	Hint    string       `json:"hint,omitempty"`
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
	Channel string `json:"channel,omitempty"`
}

// ForumListResponse wraps the response from forum_list command.
type ForumListResponse struct {
	Action               string               `json:"action,omitempty"`
	Threads              []ForumThreadSummary `json:"threads"`
	Page                 int                  `json:"page,omitempty"`
	Total                int                  `json:"total,omitempty"`
	PerPage              int                  `json:"per_page,omitempty"`
	HasMore              bool                 `json:"has_more,omitempty"`
	Categories           []string             `json:"categories,omitempty"`
	CategoryDescriptions map[string]string    `json:"category_descriptions,omitempty"`
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
	Action           string `json:"action"`
	Recipient        string `json:"recipient"`
	BaseID           string `json:"base_id,omitempty"`
	CargoRemaining   int    `json:"cargo_remaining,omitempty"`
	CreditsSent      int    `json:"credits_sent,omitempty"`
	ItemID           string `json:"item_id,omitempty"`
	Message          string `json:"message,omitempty"`
	Quantity         int    `json:"quantity,omitempty"`
	Source           string `json:"source,omitempty"`
	StorageRemaining int    `json:"storage_remaining,omitempty"`
	WalletRemaining  int    `json:"wallet_remaining,omitempty"`
}

// GiftShipResponse wraps the response from gift_ship action (via send_gift with ship).
type GiftShipResponse struct {
	Action    string `json:"action"`
	BaseID    string `json:"base_id,omitempty"`
	ClassID   string `json:"class_id,omitempty"`
	ClassName string `json:"class_name,omitempty"`
	Recipient string `json:"recipient,omitempty"`
	ShipID    string `json:"ship_id,omitempty"`
}

// FactionInfoResponse wraps the response from faction_info command.
type FactionInfoResponse struct {
	Action           string                `json:"action,omitempty"`
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	Tag              string                `json:"tag"`
	LeaderID         string                `json:"leader_id"`
	LeaderUsername   string                `json:"leader_username"`
	MemberCount      int                   `json:"member_count"`
	OwnedBases       int                   `json:"owned_bases"`
	IsMember         bool                  `json:"is_member"`
	IsAlly           bool                  `json:"is_ally"`
	IsEnemy          bool                  `json:"is_enemy"`
	AtWar            bool                  `json:"at_war"`
	Description      string                `json:"description,omitempty"`
	Charter          string                `json:"charter,omitempty"`
	Emblem           string                `json:"emblem,omitempty"`
	PrimaryColor     string                `json:"primary_color,omitempty"`
	SecondaryColor   string                `json:"secondary_color,omitempty"`
	Treasury         int                   `json:"treasury,omitempty"`
	CreatedAt        string                `json:"created_at,omitempty"`
	Members          []FactionMemberDetail `json:"members,omitempty"`
	MembersLimit     int                   `json:"members_limit,omitempty"`
	MembersOffset    int                   `json:"members_offset,omitempty"`
	MembersTruncated bool                  `json:"members_truncated,omitempty"`
	Allies           []FactionSummary      `json:"allies,omitempty"`
	Enemies          []FactionSummary      `json:"enemies,omitempty"`
	Wars             []FactionWarDetail    `json:"wars,omitempty"`
	PeaceProposals   []PeaceProposal       `json:"peace_proposals,omitempty"`

	AllianceProposals json.RawMessage `json:"alliance_proposals,omitempty"`
	Facilities        json.RawMessage `json:"facilities,omitempty"`
	Roles             json.RawMessage `json:"roles,omitempty"`

	// FuelBunkers is the galaxy-wide fuel-bunker summary added in gameserver
	// v0.346.0: one entry per faction base with a bunker. The faction-wide
	// total reserve/capacity is the sum across entries (no top-level total in
	// the response).
	FuelBunkers []FactionFuelBunker `json:"fuel_bunkers,omitempty"`
}

// FactionFuelBunker is one base's fuel-bunker status within a faction_info
// response (gameserver v0.346.0+).
type FactionFuelBunker struct {
	BaseID       string `json:"base_id"`
	BaseName     string `json:"base_name,omitempty"`
	FuelReserve  int    `json:"fuel_reserve"`
	FuelCapacity int    `json:"fuel_capacity"`
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

	FactionFuelCapacity int    `json:"faction_fuel_capacity,omitempty"`
	FactionFuelReserve  int    `json:"faction_fuel_reserve,omitempty"`
	Hint                string `json:"hint,omitempty"`
}

// FactionRoom is one room in a faction's common space (faction_rooms).
type FactionRoom struct {
	RoomID      string `json:"room_id"`
	Name        string `json:"name"`
	Access      string `json:"access,omitempty"`
	Description string `json:"description,omitempty"`
}

// FactionRoomsResponse wraps the response from faction_rooms.
type FactionRoomsResponse struct {
	Action string        `json:"action,omitempty"`
	BaseID string        `json:"base_id,omitempty"`
	Rooms  []FactionRoom `json:"rooms"`
}

// FactionMission is one posted faction mission (faction_list_missions).
type FactionMission struct {
	MissionID        string          `json:"mission_id,omitempty"`
	TemplateID       string          `json:"template_id,omitempty"`
	Title            string          `json:"title"`
	Type             string          `json:"type,omitempty"`
	Description      string          `json:"description,omitempty"`
	GiverName        string          `json:"giver_name,omitempty"`
	Rewards          json.RawMessage `json:"rewards,omitempty"`
	Objectives       json.RawMessage `json:"objectives,omitempty"`
	AssignedPlayerID string          `json:"assigned_player_id,omitempty"`
	ExpiresAt        string          `json:"expires_at,omitempty"`
}

// FactionListMissionsResponse wraps the response from faction_list_missions.
type FactionListMissionsResponse struct {
	Action   string           `json:"action,omitempty"`
	BaseID   string           `json:"base_id,omitempty"`
	Missions []FactionMission `json:"missions"`
}

// TravelResponse wraps the response from travel command.
//
// Also covers the "arrived" completion event (see client_api_monitor.go —
// both "travel" and "arrived" map to this struct). Fields populated on the
// initial ACK vs the arrival completion differ; XPGained only appears on
// arrived (navigation + piloting skill XP).
type TravelResponse struct {
	Action                 string         `json:"action"`
	POI                    string         `json:"poi,omitempty"`
	POIID                  string         `json:"poi_id,omitempty"`
	ArrivalTick            int64          `json:"arrival_tick,omitempty"`
	Destination            string         `json:"destination,omitempty"`
	AutoUndocked           bool           `json:"auto_undocked,omitempty"`
	OnlinePlayersCount     int            `json:"online_players_count,omitempty"`
	OnlinePlayersTruncated bool           `json:"online_players_truncated,omitempty"`
	OnlinePlayers          []NearbyPlayer `json:"online_players,omitempty"`
	OfflineCollapsed       int            `json:"offline_collapsed,omitempty"`
	XPGained               map[string]int `json:"xp_gained,omitempty"`
}

// JumpResponse wraps the response from jump command.
type JumpResponse struct {
	Action      string `json:"action"`
	Command     string `json:"command,omitempty"`
	Pending     bool   `json:"pending,omitempty"`
	ArrivalTick int64  `json:"arrival_tick,omitempty"`
	Destination string `json:"destination,omitempty"`
	IsWormhole  bool   `json:"is_wormhole,omitempty"`
}

// JumpedResponse wraps the response from jumped event after jump completes.
type JumpedResponse struct {
	Action        string `json:"action"`
	FromSystem    string `json:"from_system,omitempty"`
	NavigationXP  int    `json:"navigation_xp,omitempty"`
	ExplorationXP int    `json:"exploration_xp,omitempty"`
	PilotingXP    int    `json:"piloting_xp,omitempty"`
	POI           string `json:"poi,omitempty"`
	System        string `json:"system,omitempty"`
	SystemID      string `json:"system_id,omitempty"`
	AutoUndocked  bool   `json:"auto_undocked,omitempty"`
}

// DockResponse wraps the response from dock command.
type DockResponse struct {
	Action              string          `json:"action,omitempty"`
	Command             string          `json:"command,omitempty"`
	Pending             bool            `json:"pending,omitempty"`
	Base                *Base           `json:"base,omitempty"`
	StationCondition    *StationHealth  `json:"station_condition,omitempty"`
	FuelWarning         string          `json:"fuel_warning,omitempty"`
	Story               string          `json:"story,omitempty"`
	OpenOrders          []ExchangeOrder `json:"open_orders,omitempty"`
	OpenOrdersCount     int             `json:"open_orders_count,omitempty"`
	OpenOrdersTruncated bool            `json:"open_orders_truncated,omitempty"`
	StorageItems        []CargoItem     `json:"storage_items,omitempty"`
	StoredShips         []StorageShip   `json:"stored_ships,omitempty"`
	StoredShipsCount    int             `json:"stored_ships_count,omitempty"`
	UnreadChat          int             `json:"unread_chat,omitempty"`
	UnreadChatNote      string          `json:"unread_chat_note,omitempty"`
	Gifts               []StorageGift   `json:"gifts,omitempty"`
	GiftsCount          int             `json:"gifts_count,omitempty"`
	GiftsNote           string          `json:"gifts_note,omitempty"`
	TradeFills          []TradeFill     `json:"trade_fills,omitempty"`
	TradeFillsCount     int             `json:"trade_fills_count,omitempty"`
	TradeFillsTruncated bool            `json:"trade_fills_truncated,omitempty"`
	// CommissionsReady lists ships you commissioned at this station that
	// are ready for pickup; CommissionsNote is the human-readable summary.
	// Permissive map shape until the per-entry schema is confirmed from a
	// real (non-empty) dock response.
	CommissionsNote  string           `json:"commissions_note,omitempty"`
	CommissionsReady []map[string]any `json:"commissions_ready,omitempty"`
	// YourFacilities lists the player's personal facilities at this base
	// (mirrors player_facilities from `facility list`); FacilityNote is the
	// human-readable summary. Permissive map shape pending confirmation.
	FacilityNote   string           `json:"facility_note,omitempty"`
	YourFacilities []map[string]any `json:"your_facilities,omitempty"`
	// AutoDocked is set when the dock was performed automatically as a
	// precursor to another command (a command requiring the docked state was
	// issued while in space) — see the "Auto-dock/undock" note in the API docs.
	AutoDocked bool `json:"auto_docked,omitempty"`
	// PassengerArrivals reports any aboard passengers bound for this station
	// who were automatically delivered (and their fares collected) on docking.
	PassengerArrivals *PassengerArrivals `json:"passenger_arrivals,omitempty"`
}

// PassengerArrivals summarizes passengers delivered when docking at their
// destination station: the per-passenger list, the total fare collected, and
// any faction reputation gained from the deliveries.
type PassengerArrivals struct {
	Delivered         []DeliveredPassenger `json:"delivered,omitempty"`
	FareCollected     int                  `json:"fare_collected,omitempty"`
	ReputationChanges map[string]int       `json:"reputation_changes,omitempty"`
}

// PassengerRecord is the identity subset of a passenger shared across every
// passenger-bearing response (station waiting list, aboard manifest, load
// result, dock arrivals). Used to populate the knowledge-base passenger
// catalog; per-listing fields (destination, fare, ticks) are intentionally
// omitted. Citizenship/Bio are absent from some sources and unmarshal empty.
type PassengerRecord struct {
	CitizenID   string `json:"citizen_id"`
	Name        string `json:"name"`
	Citizenship string `json:"citizenship"`
	Bio         string `json:"bio"`
	Class       string `json:"class"`
}

// DeliveredPassenger is a single passenger delivered on docking.
type DeliveredPassenger struct {
	CitizenID         string `json:"citizen_id,omitempty"`
	Name              string `json:"name,omitempty"`
	Class             string `json:"class,omitempty"`
	Bio               string `json:"bio,omitempty"`
	Destination       string `json:"destination,omitempty"`
	DestinationName   string `json:"destination_name,omitempty"`
	DestinationSystem string `json:"destination_system,omitempty"`
	// BaseFare (server field "base_fare", renamed from "fare") is the
	// passenger's base fare on delivery.
	BaseFare int `json:"base_fare,omitempty"`
	// SpeedBonus is the on-time delivery bonus paid on top of BaseFare; the two
	// sum into the response's fare_collected total.
	SpeedBonus     int `json:"speed_bonus,omitempty"`
	TicksRemaining int `json:"ticks_remaining,omitempty"`
}

// StationPassenger is one NPC waiting at a station for transport, as returned in
// the list_station_passengers "waiting" array. Fare is a per-passenger estimate
// that scales with distance/hops; DestinationSystem is the system that contains
// the Destination station POI.
type StationPassenger struct {
	CitizenID         string `json:"citizen_id"`
	Name              string `json:"name"`
	Citizenship       string `json:"citizenship"`
	Class             string `json:"class"`
	Destination       string `json:"destination"`
	DestinationName   string `json:"destination_name"`
	DestinationSystem string `json:"destination_system"`
	EstimatedFare     int    `json:"estimated_fare"`
}

// ListStationPassengersResponse wraps the list_station_passengers response: the
// queried station and the passengers waiting there. Station is empty when the
// query defaulted to the currently docked station.
type ListStationPassengersResponse struct {
	Action  string             `json:"action"`
	Station string             `json:"station"`
	Count   int                `json:"count"`
	Waiting []StationPassenger `json:"waiting"`
}

// AboardPassenger is one passenger currently aboard the ship, as returned in the
// list_passengers "passengers" array. Unlike StationPassenger (a fare still
// waiting at a station) it carries the live fare-guarantee countdown: Fare is the
// base fare due on delivery, SpeedBonus the extra paid for prompt delivery (it
// decays toward 0 as TicksRemaining runs down), and TicksRemaining the ticks left
// before the guarantee expires. Destination/DestinationSystem locate the drop-off.
type AboardPassenger struct {
	CitizenID         string `json:"citizen_id"`
	Name              string `json:"name"`
	Bio               string `json:"bio"`
	Class             string `json:"class"`
	Destination       string `json:"destination"`
	DestinationName   string `json:"destination_name"`
	DestinationSystem string `json:"destination_system"`
	Fare              int    `json:"fare"`
	SpeedBonus        int    `json:"speed_bonus"`
	TicksRemaining    int    `json:"ticks_remaining"`
}

// ListPassengersResponse wraps the list_passengers response: the passengers
// currently aboard plus the ship's total passenger berths by class, reported as
// gauge strings (e.g. "2/3"). Returned by GameClient.ListPassengers.
type ListPassengersResponse struct {
	Action         string            `json:"action,omitempty"`
	Count          int               `json:"count"`
	EconomyBerths  string            `json:"economy_berths"`
	BusinessBerths string            `json:"business_berths"`
	FirstBerths    string            `json:"first_berths"`
	Passengers     []AboardPassenger `json:"passengers"`
}

// LoadedPassenger is a single passenger actually boarded by load_passenger.
type LoadedPassenger struct {
	Name            string `json:"name"`
	Class           string `json:"class"`
	Destination     string `json:"destination"`
	DestinationName string `json:"destination_name"`
	Fare            int    `json:"fare"`
	TicksRemaining  int    `json:"ticks_remaining"`
}

// LoadPassengerResponse wraps the load_passenger response. Fewer passengers may
// board than were waiting for the destination: berth class and capacity limit who
// fits (a free upgrade is allowed, but first-class is never downgraded to fit), so
// Loaded/Count/TotalFare — not the waiting list — are the source of truth for who
// is actually aboard and the booked fare.
type LoadPassengerResponse struct {
	Action    string            `json:"action"`
	Message   string            `json:"message"`
	Loaded    []LoadedPassenger `json:"loaded"`
	Count     int               `json:"count"`
	TotalFare int               `json:"total_fare"`
	// Berth occupancy after the load, each formatted "occupied/capacity". A
	// Count of 0 with free capacity here means the passengers were taken by
	// another ship, not that they failed to fit.
	EconomyBerths  string `json:"economy_berths"`
	BusinessBerths string `json:"business_berths"`
	FirstBerths    string `json:"first_berths"`
}

// UndockResponse wraps the response from undock command.
//
// AutoUndocked is set when the undock was performed automatically as a
// precursor to another command (travel/jump issued while docked) — see the
// "Auto-dock/undock" note in the API docs.
type UndockResponse struct {
	Action       string `json:"action"`
	AutoUndocked bool   `json:"auto_undocked,omitempty"`
}

// RefuelResponse wraps the response from refuel command.
type RefuelResponse struct {
	Action           string `json:"action"`
	Source           string `json:"source,omitempty"`
	Fuel             int    `json:"fuel,omitempty"`
	FuelNow          int    `json:"fuel_now,omitempty"`
	FuelMax          int    `json:"fuel_max,omitempty"`
	Cost             int    `json:"cost,omitempty"`
	CellsUsed        int    `json:"cells_used,omitempty"`
	ItemID           string `json:"item_id,omitempty"`
	ItemName         string `json:"item_name,omitempty"`
	TargetFuelMax    int    `json:"target_fuel_max,omitempty"`
	TargetFuelNow    int    `json:"target_fuel_now,omitempty"`
	TargetPlayerID   string `json:"target_player_id,omitempty"`
	TargetPlayerName string `json:"target_player_name,omitempty"`

	AllyFactionID   string          `json:"ally_faction_id,omitempty"`
	AllyFactionTag  string          `json:"ally_faction_tag,omitempty"`
	AllyFuel        int             `json:"ally_fuel,omitempty"`
	FactionFuel     int             `json:"faction_fuel,omitempty"`
	FleetID         string          `json:"fleet_id,omitempty"`
	HasPump         bool            `json:"has_pump,omitempty"`
	Members         json.RawMessage `json:"members,omitempty"`
	RescueCompleted bool            `json:"rescue_completed,omitempty"`
	RescueReward    int64           `json:"rescue_reward,omitempty"`
	TaxAmount       int64           `json:"tax_amount,omitempty"`
}

// RepairResponse wraps the response from repair command.
// Supports station repair (credits), in-space repair (kits), and repairing other ships (repair arm + kits).
type RepairResponse struct {
	Action           string              `json:"action"`
	Source           string              `json:"source,omitempty"` // "station", "kits", etc.
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
	Action  string `json:"action,omitempty"`
	Command string `json:"command,omitempty"`
	Pending bool   `json:"pending,omitempty"`
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

// EscrowInput is a single item held in escrow for a craft job.
type EscrowInput struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// EscrowCost describes the credits and materials escrowed when a craft job is queued.
type EscrowCost struct {
	Fee    int           `json:"fee"`
	Labor  int           `json:"labor"`
	Inputs []EscrowInput `json:"inputs"`
}

// CraftProduces is a single output item listed on a queued or dry-run craft job.
type CraftProduces struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// CraftJobQueued is the response when a single craft (or recycle) job is accepted and queued.
// Corresponds to the first oneOf variant of CraftJobResponse in the openapi spec.
type CraftJobQueued struct {
	Action              string          `json:"action"`
	JobID               string          `json:"job_id"`
	Recipe              string          `json:"recipe"`
	Mode                string          `json:"mode"`
	Venue               string          `json:"venue"`
	VenueType           string          `json:"venue_type"`
	FacilityID          string          `json:"facility_id"`
	Runs                int             `json:"runs"`
	EffectiveTimePerRun float64         `json:"effective_time_per_run"`
	EstCompletionTick   int             `json:"est_completion_tick"`
	Escrowed            EscrowCost      `json:"escrowed"`
	Message             string          `json:"message"`
	Produces            []CraftProduces `json:"produces,omitempty"`
	External            bool            `json:"external,omitempty"`
}

// CraftJobEntry is a single job returned in a queue listing.
type CraftJobEntry struct {
	JobID         string          `json:"job_id"`
	Recipe        string          `json:"recipe"`
	Mode          string          `json:"mode"`
	RunsTotal     int             `json:"runs_total"`
	RunsDone      int             `json:"runs_done"`
	RunsRemaining int             `json:"runs_remaining"`
	Progress      float64         `json:"progress"`
	ETATicks      int             `json:"eta_ticks"`
	Position      int             `json:"position"`
	Orderer       string          `json:"orderer"`
	Status        string          `json:"status"`
	FacilityID    string          `json:"facility_id"`
	External      bool            `json:"external,omitempty"`
	Venue         string          `json:"venue,omitempty"`
	Produces      []CraftProduces `json:"produces,omitempty"`
}

// CraftQueueListing is the response when listing queued craft jobs.
// Corresponds to the second oneOf variant of CraftJobResponse in the openapi spec.
type CraftQueueListing struct {
	Action string          `json:"action"`
	Jobs   []CraftJobEntry `json:"jobs"`
}

// CraftBulkResult is the outcome of a single job in a bulk craft request.
type CraftBulkResult struct {
	Index     int    `json:"index"`
	Success   bool   `json:"success"`
	JobID     string `json:"job_id,omitempty"`
	Recipe    string `json:"recipe,omitempty"`
	Runs      int    `json:"runs,omitempty"`
	Venue     string `json:"venue,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// CraftBulkSummary summarises the outcomes of a bulk craft request.
type CraftBulkSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// CraftBulkResponse is the response when submitting multiple craft jobs in one request.
// Corresponds to the third oneOf variant of CraftJobResponse in the openapi spec.
type CraftBulkResponse struct {
	Action  string            `json:"action"`
	Mode    string            `json:"mode"`
	Results []CraftBulkResult `json:"results"`
	Summary CraftBulkSummary  `json:"summary"`
}

// CraftDryRunResponse is the response for a dry-run (quote) craft request.
// Corresponds to the fourth oneOf variant of CraftJobResponse in the openapi spec.
type CraftDryRunResponse struct {
	Action              string          `json:"action"`
	DryRun              bool            `json:"dry_run"`
	Recipe              string          `json:"recipe"`
	Mode                string          `json:"mode"`
	Quantity            int             `json:"quantity"`
	Runs                int             `json:"runs"`
	Venue               string          `json:"venue"`
	VenueType           string          `json:"venue_type"`
	FacilityID          string          `json:"facility_id"`
	Cost                EscrowCost      `json:"cost"`
	CreditsTotal        int             `json:"credits_total"`
	HaveInputs          bool            `json:"have_inputs"`
	HaveCredits         bool            `json:"have_credits"`
	EffectiveTimePerRun float64         `json:"effective_time_per_run"`
	EstCompletionTick   int             `json:"est_completion_tick"`
	Message             string          `json:"message"`
	Produces            []CraftProduces `json:"produces,omitempty"`
	External            bool            `json:"external,omitempty"`
}

// CraftingUpdateDeposit is an item deposited to storage when a craft run completes.
type CraftingUpdateDeposit struct {
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	Quantity int    `json:"quantity"`
}

// CraftingUpdateJob is a single job entry in a crafting_update event.
type CraftingUpdateJob struct {
	JobID         string                  `json:"job_id"`
	Recipe        string                  `json:"recipe"`
	Mode          string                  `json:"mode"`
	Venue         string                  `json:"venue"`
	Storage       string                  `json:"storage"`
	Deposited     []CraftingUpdateDeposit `json:"deposited"`
	RunsDone      int                     `json:"runs_done"`
	RunsRemaining int                     `json:"runs_remaining"`
	Completed     bool                    `json:"completed"`
}

// CraftingUpdateEvent is the payload of a crafting_update server push event.
// Corresponds to Notification_crafting_update in the openapi spec.
type CraftingUpdateEvent struct {
	Tick int                 `json:"tick"`
	Jobs []CraftingUpdateJob `json:"jobs"`
}

// RecycleResponse reuses the queued-job shape; recycle has no preset and no XP.
type RecycleResponse = CraftJobQueued

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
	Action     string `json:"action"`
	ItemID     string `json:"item_id"`
	ItemName   string `json:"item_name"`
	Quantity   int    `json:"quantity"`
	CargoUsed  int    `json:"cargo_used"`
	CargoSpace int    `json:"cargo_space"`

	ContainerID string `json:"container_id,omitempty"`
}

// InstallModResponse wraps the response from install_mod command.
type InstallModResponse struct {
	Message      string  `json:"message"`
	ModuleID     string  `json:"module_id"`
	Quality      float64 `json:"quality,omitempty"`
	QualityGrade string  `json:"quality_grade,omitempty"`
	CPUUsed      int     `json:"cpu_used"`
	PowerUsed    int     `json:"power_used"`
	CurrentAmmo  int     `json:"current_ammo,omitempty"`
	LoadedAmmo   string  `json:"loaded_ammo,omitempty"`
	MagazineSize int     `json:"magazine_size,omitempty"`
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
	Damaged        bool    `json:"damaged,omitempty"`
}

// ReloadResponse wraps the response from reload command.
type ReloadResponse struct {
	Action          string `json:"action"`
	WeaponName      string `json:"weapon_name"`
	WeaponID        string `json:"weapon_id"`
	AmmoName        string `json:"ammo_name"`
	AmmoID          string `json:"ammo_id"`
	CurrentAmmo     int    `json:"current_ammo"`
	MagazineSize    int    `json:"magazine_size"`
	PreviousAmmo    string `json:"previous_ammo,omitempty"`
	RoundsDiscarded int    `json:"rounds_discarded,omitempty"`
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
	FuelAdded         int          `json:"fuel_added,omitempty"`
	FuelMax           int          `json:"fuel_max,omitempty"`
	FuelNow           int          `json:"fuel_now,omitempty"`
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

// SetDroneNameResponse wraps the response from set_drone_name command.
type SetDroneNameResponse struct {
	DroneID string `json:"drone_id"`
	Name    string `json:"name"`
	Message string `json:"message"`
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
	HasMore     bool               `json:"has_more,omitempty"`
	SearchTerm  string             `json:"search_term,omitempty"`
}

// Notification represents a single notification from the server.
type Notification struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	MsgType   string `json:"msg_type"`
	Data      any    `json:"data"`
}

// GetNotificationsResponse wraps the response from get_notifications command.
type GetNotificationsResponse struct {
	Action        string         `json:"action,omitempty"`
	Notifications []Notification `json:"notifications"`
	Count         int            `json:"count"`
	Remaining     int            `json:"remaining"`
	CurrentTick   int            `json:"current_tick"`
	Timestamp     int            `json:"timestamp"`
	Throttled     bool           `json:"throttled,omitempty"`
	RetryAfter    int            `json:"retry_after,omitempty"`
}

// RefitShipResponse wraps the response from refit_ship command.
type RefitShipResponse struct {
	Message         string `json:"message"`
	ShipID          string `json:"ship_id"`
	ClassID         string `json:"class_id"`
	ModulesReturned int    `json:"modules_returned"`
	CargoReturned   int    `json:"cargo_returned"`
	DefaultModules  int    `json:"default_modules"`
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
	Entries    []ActionLogEntry `json:"entries"`
	HasMore    bool             `json:"has_more"`
	Category   string           `json:"category,omitempty"`
	Limit      int              `json:"limit,omitempty"`
	FactionID  string           `json:"faction_id,omitempty"`
	Page       int              `json:"page,omitempty"`
	PageSize   int              `json:"page_size,omitempty"`
	Total      int              `json:"total,omitempty"`
	TotalPages int              `json:"total_pages,omitempty"`
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

// MobileCapitalTransitResponse is delivered when the Mobile Capital jumps,
// relocating the player to a new system.
//   - mobile_capital_transit
type MobileCapitalTransitResponse struct {
	Action  string `json:"action"`
	Message string `json:"message"`
	System  string `json:"system,omitempty"`
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

// GetDroneResponse is returned by get_drone — details of a single drone.
//   - get_drone
type GetDroneResponse struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Status        string          `json:"status"`
	Hull          int             `json:"hull"`
	MaxHull       int             `json:"max_hull"`
	Cargo         json.RawMessage `json:"cargo,omitempty"`
	CargoCapacity int             `json:"cargo_capacity"`
	CargoUsed     int             `json:"cargo_used"`
	ItemID        string          `json:"item_id,omitempty"`
	POIID         string          `json:"poi_id,omitempty"`
	SystemID      string          `json:"system_id,omitempty"`
	DeployedAt    string          `json:"deployed_at,omitempty"`
	LoadedAt      string          `json:"loaded_at,omitempty"`
	Script        string          `json:"script,omitempty"`
	Memory        json.RawMessage `json:"memory,omitempty"`
	TravelTo      string          `json:"travel_to,omitempty"`
	TravelTicks   int             `json:"travel_ticks,omitempty"`
}

// GetDronesResponse is returned by get_drones — drone bay summary and roster.
//   - get_drones
type GetDronesResponse struct {
	BandwidthTotal int             `json:"bandwidth_total"`
	BandwidthUsed  int             `json:"bandwidth_used"`
	BayCapacity    int             `json:"bay_capacity"`
	BayCount       int             `json:"bay_count"`
	DeployedCount  int             `json:"deployed_count"`
	Drones         json.RawMessage `json:"drones,omitempty"`
}

// LoadDroneResponse is returned by load_drone.
//   - load_drone
type LoadDroneResponse struct {
	BayCapacity int    `json:"bay_capacity"`
	BayCount    int    `json:"bay_count"`
	DroneID     string `json:"drone_id"`
	DroneType   string `json:"drone_type"`
	Hull        int    `json:"hull"`
	Message     string `json:"message"`
	Status      string `json:"status"`
}

// UnloadDroneResponse is returned by unload_drone.
//   - unload_drone
type UnloadDroneResponse struct {
	DroneID string `json:"drone_id"`
	ItemID  string `json:"item_id"`
	Message string `json:"message"`
}

// RecallDroneResponse is returned by recall_drone.
//   - recall_drone
type RecallDroneResponse struct {
	Message  string `json:"message"`
	Recalled int    `json:"recalled"`
	Skipped  int    `json:"skipped"`
}

// UploadDroneScriptResponse is returned by upload_drone_script.
//   - upload_drone_script
type UploadDroneScriptResponse struct {
	DroneID   string `json:"drone_id"`
	Message   string `json:"message"`
	ScriptLen int    `json:"script_len"`
}

// FactionAcceptInviteResponse is returned by faction_accept_invite.
//   - faction_accept_invite
type FactionAcceptInviteResponse struct {
	Faction   string `json:"faction"`
	FactionID string `json:"faction_id"`
	Message   string `json:"message"`
}

// FactionWithdrawInviteResponse is returned by faction_withdraw_invite.
//   - faction_withdraw_invite
type FactionWithdrawInviteResponse struct {
	Message  string `json:"message"`
	PlayerID string `json:"player_id"`
}

// FactionRemoveEnemyResponse is returned by faction_remove_enemy.
//   - faction_remove_enemy
type FactionRemoveEnemyResponse struct {
	Message         string `json:"message"`
	Removed         bool   `json:"removed"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// CitizenshipResponse is returned by the citizenship action, which handles
// petitioning for, granting, renouncing, and querying empire citizenship.
// Field presence varies by the citizenship sub-action requested.
//   - citizenship
type CitizenshipResponse struct {
	Citizenship      json.RawMessage `json:"citizenship,omitempty"`
	Citizenships     json.RawMessage `json:"citizenships,omitempty"`
	EmpireID         string          `json:"empire_id,omitempty"`
	Empires          json.RawMessage `json:"empires,omitempty"`
	FeePaid          int64           `json:"fee_paid,omitempty"`
	FeeRefunded      int64           `json:"fee_refunded,omitempty"`
	Message          string          `json:"message,omitempty"`
	Origin           string          `json:"origin,omitempty"`
	PendingPetitions json.RawMessage `json:"pending_petitions,omitempty"`
	Petition         json.RawMessage `json:"petition,omitempty"`
	PetitionID       string          `json:"petition_id,omitempty"`
	RecentDecisions  json.RawMessage `json:"recent_decisions,omitempty"`
	Renounced        json.RawMessage `json:"renounced,omitempty"`
	Rules            json.RawMessage `json:"rules,omitempty"`
	Status           string          `json:"status,omitempty"`
}

// GetEmpireInfoResponse is returned by get_empire_info.
//   - get_empire_info
type GetEmpireInfoResponse struct {
	Action  string          `json:"action"`
	Empires json.RawMessage `json:"empires,omitempty"`
}

// PetitionResponse is returned by petition (submit a citizenship petition).
//   - petition
type PetitionResponse struct {
	EmpireID   string `json:"empire_id"`
	EmpireName string `json:"empire_name"`
	Message    string `json:"message"`
}

// GetTaxEstimateResponse is returned by get_tax_estimate.
//   - get_tax_estimate
type GetTaxEstimateResponse struct {
	Action                      string          `json:"action"`
	AssessedPropertyByShip      json.RawMessage `json:"assessed_property_by_ship,omitempty"`
	AssessedPropertyValue       int64           `json:"assessed_property_value"`
	IncomeTax                   json.RawMessage `json:"income_tax,omitempty"`
	IncomeTaxTotal              int64           `json:"income_tax_total"`
	LastAssessedAt              int64           `json:"last_assessed_at,omitempty"`
	LastPropertyAssessedAt      int64           `json:"last_property_assessed_at,omitempty"`
	NextAssessmentApproxSeconds int64           `json:"next_assessment_approx_seconds,omitempty"`
	Note                        string          `json:"note,omitempty"`
	PropertyTax                 json.RawMessage `json:"property_tax,omitempty"`
	PropertyTaxTotal            int64           `json:"property_tax_total"`
	SalesTaxRates               json.RawMessage `json:"sales_tax_rates,omitempty"`
	TaxCollectionActive         bool            `json:"tax_collection_active"`
	TaxableIncomeBySource       json.RawMessage `json:"taxable_income_by_source,omitempty"`
	TaxableIncomeToDate         int64           `json:"taxable_income_to_date"`
}

// ViewInsuranceResponse is returned by view_insurance.
//   - view_insurance
type ViewInsuranceResponse struct {
	Message  string          `json:"message,omitempty"`
	Policies json.RawMessage `json:"policies,omitempty"`
}

// ScrapShipResponse is returned by scrap_ship.
//   - scrap_ship
type ScrapShipResponse struct {
	CargoNote        string          `json:"cargo_note,omitempty"`
	CargoToStorage   json.RawMessage `json:"cargo_to_storage,omitempty"`
	Message          string          `json:"message"`
	ModulesNote      string          `json:"modules_note,omitempty"`
	ModulesToStorage json.RawMessage `json:"modules_to_storage,omitempty"`
	ScrappedClass    string          `json:"scrapped_class,omitempty"`
	ScrappedShipID   string          `json:"scrapped_ship_id"`
}

// CompletedMissionsResponse is returned by completed_missions.
//   - completed_missions
type CompletedMissionsResponse struct {
	Missions   json.RawMessage `json:"missions,omitempty"`
	TotalCount int             `json:"total_count"`
}

// DeleteNoteResponse is returned by delete_note.
//   - delete_note
type DeleteNoteResponse struct {
	Message string `json:"message"`
	NoteID  string `json:"note_id"`
	Title   string `json:"title,omitempty"`
}

// CaptainsLogDeleteResponse is returned by captains_log_delete.
//   - captains_log_delete
type CaptainsLogDeleteResponse struct {
	Index          int    `json:"index"`
	Message        string `json:"message"`
	RemainingCount int    `json:"remaining_count"`
}

// AbandonMissionResponse — abandon_mission
type AbandonMissionResponse struct {
	Message   string `json:"message"`
	MissionID string `json:"mission_id"`
	Title     string `json:"title"`
}

// DeclineMissionResponse — decline_mission
type DeclineMissionResponse struct {
	Message    string          `json:"message"`
	Giver      json.RawMessage `json:"giver,omitempty"`
	TemplateID string          `json:"template_id"`
	Title      string          `json:"title"`
}

// ViewCompletedMissionResponse — view_completed_mission
type ViewCompletedMissionResponse struct {
	TemplateID     string          `json:"template_id"`
	Title          string          `json:"title"`
	Type           string          `json:"type,omitempty"`
	ChainNext      string          `json:"chain_next,omitempty"`
	CompletionTime string          `json:"completion_time,omitempty"`
	Difficulty     int             `json:"difficulty,omitempty"`
	Objectives     json.RawMessage `json:"objectives,omitempty"`
	Rewards        json.RawMessage `json:"rewards,omitempty"`
	Repeatable     bool            `json:"repeatable,omitempty"`
	Giver          json.RawMessage `json:"giver,omitempty"`
	Dialog         json.RawMessage `json:"dialog,omitempty"`
	Description    string          `json:"description,omitempty"`
}

// CaptainsLogAddResponse — captains_log_add
type CaptainsLogAddResponse struct {
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
	Index     int    `json:"index"`
}

// CaptainsLogGetResponse — captains_log_get
type CaptainsLogGetResponse struct {
	Entry     string `json:"entry"`
	CreatedAt string `json:"created_at"`
	Index     int    `json:"index"`
}

// CaptainsLogListResponse — captains_log_list
type CaptainsLogListResponse struct {
	Entry      json.RawMessage `json:"entry,omitempty"`
	HasNext    bool            `json:"has_next"`
	HasPrev    bool            `json:"has_prev"`
	Index      int             `json:"index"`
	MaxEntries int             `json:"max_entries"`
	TotalCount int             `json:"total_count"`
}

// AttackResponse — attack (ack frame)
type AttackResponse struct {
	Command string `json:"command,omitempty"`
	Message string `json:"message"`
	Pending bool   `json:"pending,omitempty"`
}

// NameShipResponse — name_ship
type NameShipResponse struct {
	Message  string `json:"message"`
	ShipID   string `json:"ship_id"`
	ShipName string `json:"ship_name"`
}

// ReleaseTowResponse — release_tow
type ReleaseTowResponse struct {
	Action  string `json:"action,omitempty"`
	Message string `json:"message"`
	WreckID string `json:"wreck_id"`
}

// RepairModuleResponse — repair_module
type RepairModuleResponse struct {
	Message      string          `json:"message"`
	ModuleID     string          `json:"module_id"`
	RepairAmount float64         `json:"repair_amount,omitempty"`
	WearAfter    float64         `json:"wear_after,omitempty"`
	WearBefore   float64         `json:"wear_before,omitempty"`
	WearStatus   string          `json:"wear_status,omitempty"`
	XPGained     json.RawMessage `json:"xp_gained,omitempty"`
}

// ScrapWreckResponse — scrap_wreck
type ScrapWreckResponse struct {
	Action     string          `json:"action,omitempty"`
	Message    string          `json:"message"`
	Materials  json.RawMessage `json:"materials,omitempty"`
	ShipClass  string          `json:"ship_class,omitempty"`
	StoredAt   string          `json:"stored_at,omitempty"`
	TotalValue int64           `json:"total_value,omitempty"`
	WreckID    string          `json:"wreck_id"`
}

// CancelShipListingResponse — cancel_ship_listing
type CancelShipListingResponse struct {
	ClassID string `json:"class_id"`
	Message string `json:"message"`
	ShipID  string `json:"ship_id"`
}

// CancelCommissionResponse — cancel_commission
type CancelCommissionResponse struct {
	Message           string          `json:"message"`
	CreditsTotal      int64           `json:"credits_total,omitempty"`
	MaterialsNote     string          `json:"materials_note,omitempty"`
	MaterialsReturned json.RawMessage `json:"materials_returned,omitempty"`
	Refund            int64           `json:"refund,omitempty"`
}

// SupplyCommissionResponse — supply_commission
type SupplyCommissionResponse struct {
	Message          string          `json:"message"`
	AllSourced       bool            `json:"all_sourced,omitempty"`
	CommissionID     string          `json:"commission_id"`
	CommissionStatus string          `json:"commission_status,omitempty"`
	Credits          int64           `json:"credits,omitempty"`
	ItemID           string          `json:"item_id,omitempty"`
	ItemName         string          `json:"item_name,omitempty"`
	Materials        json.RawMessage `json:"materials,omitempty"`
	Supplied         int             `json:"supplied,omitempty"`
}

// ForumDeleteReplyResponse — forum_delete_reply
type ForumDeleteReplyResponse struct {
	Message string `json:"message"`
	ReplyID string `json:"reply_id"`
}

// ForumDeleteThreadResponse — forum_delete_thread
type ForumDeleteThreadResponse struct {
	Message  string `json:"message"`
	ThreadID string `json:"thread_id"`
}

// ForumUpvoteResponse — forum_upvote
type ForumUpvoteResponse struct {
	Message  string `json:"message"`
	ReplyID  string `json:"reply_id,omitempty"`
	ThreadID string `json:"thread_id,omitempty"`
}

// GetSystemAgentsResponse — get_system_agents
type GetSystemAgentsResponse struct {
	Agents           json.RawMessage `json:"agents,omitempty"`
	Count            int             `json:"count,omitempty"`
	OfflineCollapsed int             `json:"offline_collapsed,omitempty"`
	SystemID         string          `json:"system_id,omitempty"`
}

// JoinFactionResponse — join_faction
type JoinFactionResponse struct {
	Faction   string `json:"faction"`
	FactionID string `json:"faction_id"`
	Message   string `json:"message"`
}

// FactionDeclineInviteResponse — faction_decline_invite
type FactionDeclineInviteResponse struct {
	FactionID string `json:"faction_id"`
	Message   string `json:"message"`
}

// FactionGetInvitesResponse — faction_get_invites
type FactionGetInvitesResponse struct {
	Invites json.RawMessage `json:"invites,omitempty"`
}

// FactionCreateRoleResponse — faction_create_role
type FactionCreateRoleResponse struct {
	Message  string `json:"message"`
	Name     string `json:"name"`
	Priority int    `json:"priority,omitempty"`
	RoleID   string `json:"role_id"`
}

// FactionDeleteRoleResponse — faction_delete_role
type FactionDeleteRoleResponse struct {
	Message         string `json:"message"`
	ReassignedCount int    `json:"reassigned_count,omitempty"`
	RoleID          string `json:"role_id"`
}

// FactionEditRoleResponse — faction_edit_role
type FactionEditRoleResponse struct {
	Message string          `json:"message"`
	RoleID  string          `json:"role_id"`
	Updates json.RawMessage `json:"updates,omitempty"`
}

// FactionEditResponse — faction_edit
type FactionEditResponse struct {
	Hint    string          `json:"hint,omitempty"`
	Message string          `json:"message"`
	Updates json.RawMessage `json:"updates,omitempty"`
}

// FactionDeleteRoomResponse — faction_delete_room
type FactionDeleteRoomResponse struct {
	Action  string `json:"action,omitempty"`
	Message string `json:"message"`
	RoomID  string `json:"room_id"`
}

// FactionVisitRoomResponse — faction_visit_room
type FactionVisitRoomResponse struct {
	Action      string `json:"action,omitempty"`
	Access      string `json:"access,omitempty"`
	Author      string `json:"author,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	Description string `json:"description,omitempty"`
	Name        string `json:"name,omitempty"`
	RoomID      string `json:"room_id"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// FactionWriteRoomResponse — faction_write_room
type FactionWriteRoomResponse struct {
	Action  string `json:"action,omitempty"`
	Access  string `json:"access,omitempty"`
	Faction string `json:"faction,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Message string `json:"message"`
	Name    string `json:"name,omitempty"`
	RoomID  string `json:"room_id"`
}

// FactionAcceptAllyResponse — faction_accept_ally
type FactionAcceptAllyResponse struct {
	Message         string `json:"message"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// FactionProposeAllyResponse — faction_propose_ally
type FactionProposeAllyResponse struct {
	Message         string `json:"message"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// FactionRemoveAllyResponse — faction_remove_ally
type FactionRemoveAllyResponse struct {
	Message         string `json:"message"`
	Removed         bool   `json:"removed,omitempty"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// FactionSetEnemyResponse — faction_set_enemy
type FactionSetEnemyResponse struct {
	Message         string `json:"message"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// FactionIntelStatusResponse — faction_intel_status
type FactionIntelStatusResponse struct {
	IntelLevel      int             `json:"intel_level,omitempty"`
	Reports24h      int             `json:"reports_24h,omitempty"`
	TopContributors json.RawMessage `json:"top_contributors,omitempty"`
	TotalReports    int             `json:"total_reports,omitempty"`
	UniquePlayers   int             `json:"unique_players,omitempty"`
	UniqueSystems   int             `json:"unique_systems,omitempty"`
}

// FactionQueryIntelResponse — faction_query_intel
type FactionQueryIntelResponse struct {
	Message    string          `json:"message,omitempty"`
	Count      int             `json:"count,omitempty"`
	Entries    json.RawMessage `json:"entries,omitempty"`
	IntelLevel int             `json:"intel_level,omitempty"`
	Limit      int             `json:"limit,omitempty"`
	Offset     int             `json:"offset,omitempty"`
	Total      int             `json:"total,omitempty"`
}

// FactionQueryTradeIntelResponse — faction_query_trade_intel
type FactionQueryTradeIntelResponse struct {
	Entries    json.RawMessage `json:"entries,omitempty"`
	IntelLevel int             `json:"intel_level,omitempty"`
	Limit      int             `json:"limit,omitempty"`
	Offset     int             `json:"offset,omitempty"`
	Showing    int             `json:"showing,omitempty"`
	Total      int             `json:"total,omitempty"`
}

// FactionSubmitTradeIntelResponse — faction_submit_trade_intel
type FactionSubmitTradeIntelResponse struct {
	Message         string `json:"message"`
	StationsUpdated int    `json:"stations_updated,omitempty"`
	Status          string `json:"status,omitempty"`
}

// FactionTradeIntelStatusResponse — faction_trade_intel_status
type FactionTradeIntelStatusResponse struct {
	IntelLevel      int             `json:"intel_level,omitempty"`
	Reports24h      int             `json:"reports_24h,omitempty"`
	TopContributors json.RawMessage `json:"top_contributors,omitempty"`
	TotalReports    int             `json:"total_reports,omitempty"`
	UniqueItems     int             `json:"unique_items,omitempty"`
	UniqueStations  int             `json:"unique_stations,omitempty"`
}

// FactionCreateBuyOrderResponse — faction_create_buy_order
type FactionCreateBuyOrderResponse struct {
	Action        string `json:"action,omitempty"`
	Consolidated  bool   `json:"consolidated,omitempty"`
	FactionID     string `json:"faction_id,omitempty"`
	FactionTag    string `json:"faction_tag,omitempty"`
	Item          string `json:"item,omitempty"`
	ItemID        string `json:"item_id,omitempty"`
	ListingFee    int64  `json:"listing_fee,omitempty"`
	Message       string `json:"message"`
	OrderID       string `json:"order_id,omitempty"`
	PriceEach     int64  `json:"price_each,omitempty"`
	Quantity      int    `json:"quantity,omitempty"`
	TotalEscrowed int64  `json:"total_escrowed,omitempty"`
}

// FactionCreateSellOrderResponse — faction_create_sell_order
type FactionCreateSellOrderResponse struct {
	Action       string `json:"action,omitempty"`
	Consolidated bool   `json:"consolidated,omitempty"`
	FactionID    string `json:"faction_id,omitempty"`
	FactionTag   string `json:"faction_tag,omitempty"`
	Item         string `json:"item,omitempty"`
	ItemID       string `json:"item_id,omitempty"`
	ListingFee   int64  `json:"listing_fee,omitempty"`
	Message      string `json:"message"`
	OrderID      string `json:"order_id,omitempty"`
	PriceEach    int64  `json:"price_each,omitempty"`
	Quantity     int    `json:"quantity,omitempty"`
}

// FactionPostMissionResponse — faction_post_mission
type FactionPostMissionResponse struct {
	Escrowed   json.RawMessage `json:"escrowed,omitempty"`
	Message    string          `json:"message"`
	Status     string          `json:"status,omitempty"`
	TemplateID string          `json:"template_id"`
	Title      string          `json:"title"`
}

// FactionCancelMissionResponse — faction_cancel_mission
type FactionCancelMissionResponse struct {
	Message string `json:"message"`
	Status  string `json:"status,omitempty"`
}
