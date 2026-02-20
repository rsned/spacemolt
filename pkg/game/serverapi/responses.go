package serverapi

// ============================================================================
// Server Response Wrappers
// These wrap the actual data returned by server commands.
// ============================================================================

// GetSystemResponse wraps the response from get_system command.
type GetSystemResponse struct {
	Action         string     `json:"action"`
	POI            CurrentPOI `json:"poi"`
	SecurityStatus string     `json:"security_status"`
	System         SystemData `json:"system"`
}

// GetPOIResponse wraps the response from get_poi command.
type GetPOIResponse struct {
	Action string `json:"action"`
	POI    POI    `json:"poi"`
}

// GetStatusResponse wraps the response from get_status command.
type GetStatusResponse struct {
	Action      string         `json:"action"`
	Player      Player         `json:"player"`
	Ship        Ship           `json:"ship"`
	System      SystemData     `json:"system"`
	POI         POI            `json:"poi"`
	Nearby      []NearbyPlayer `json:"nearby"`
	CurrentTick int64          `json:"current_tick"`
}

// GetMapResponse wraps the response from get_map command.
type GetMapResponse struct {
	Systems []MapSystem `json:"systems"`
}

// GetBaseResponse wraps the response from get_base command.
type GetBaseResponse struct {
	Action    string            `json:"action"`
	Base      *Base             `json:"base"`
	POI       POI               `json:"poi,omitempty"`
	Resources []ResourceDisplay `json:"resources,omitempty"`
	Services  []string          `json:"services,omitempty"`
	Market    []MarketListing   `json:"market,omitempty"`
}

// GetSkillsResponse wraps the response from get_skills command.
type GetSkillsResponse struct {
	Action       string                     `json:"action"`
	Skills       map[string]SkillDefinition `json:"skills"`
	PlayerSkills []PlayerSkill              `json:"player_skills"`
}

// GetListingsResponse wraps the response from get_listings command.
type GetListingsResponse struct {
	Action      string          `json:"action"`
	Listings    []MarketListing `json:"listings"`
	StationID   string          `json:"station_id"`
	StationName string          `json:"station_name"`
}

// GetShipsResponse wraps the response from get_ships/shipyard_showroom command.
type GetShipsResponse struct {
	Action      string      `json:"action"`
	Ships       []ShipClass `json:"ships"`
	StationID   string      `json:"station_id"`
	StationName string      `json:"station_name"`
}

// GetRecipesResponse wraps the response from get_recipes command.
type GetRecipesResponse struct {
	Action  string            `json:"action"`
	Recipes map[string]Recipe `json:"recipes"`
}

// GetWrecksResponse wraps the response from get_wrecks command.
type GetWrecksResponse struct {
	Action string  `json:"action"`
	Wrecks []Wreck `json:"wrecks"`
}

// GetDronesResponse wraps the response from get_drones command.
type GetDronesResponse struct {
	Action string  `json:"action"`
	Drones []Drone `json:"drones"`
}

// ============================================================================
// New Response Types (v0.112.0)
// ============================================================================

// GetCargoResponse wraps the response from get_cargo command.
type GetCargoResponse struct {
	Action    string      `json:"action"`
	Cargo     []CargoItem `json:"cargo"`
	Used      float64     `json:"used"`
	Capacity  float64     `json:"capacity"`
	Available float64     `json:"available"`
}

// GetBattleStatusResponse wraps the response from get_battle_status command.
type GetBattleStatusResponse struct {
	BattleID      string           `json:"battle_id"`
	SystemID      string           `json:"system_id"`
	IsParticipant bool             `json:"is_participant"`
	Participants  []map[string]any `json:"participants"`
	Sides         []map[string]any `json:"sides"`
	TickDuration  int              `json:"tick_duration"`
}

// BattleResponse wraps the response from battle command actions.
type BattleResponse struct {
	Action   string `json:"action"`
	BattleID string `json:"battle_id"`
	Message  string `json:"message"`
	Stance   string `json:"stance,omitempty"`
	TargetID string `json:"target_id,omitempty"`
}

// ViewMarketResponse wraps the response from view_market command.
type ViewMarketResponse struct {
	Action string           `json:"action"`
	Base   map[string]any   `json:"base,omitempty"`
	Items  []ViewMarketItem `json:"items"`
}

// ViewOrdersResponse wraps the response from view_orders command.
type ViewOrdersResponse struct {
	Action string          `json:"action"`
	Orders []ExchangeOrder `json:"orders"`
}

// AnalyzeMarketResponse wraps the response from analyze_market command.
type AnalyzeMarketResponse struct {
	Mode            string           `json:"mode"`
	SkillLevel      int              `json:"skill_level"`
	ScanningRange   string           `json:"scanning_range"`
	StationsInRange int              `json:"stations_in_range,omitempty"`
	ItemsScanned    int              `json:"items_scanned,omitempty"`
	TopInsights     []map[string]any `json:"top_insights,omitempty"`
	TotalItems      int              `json:"total_items,omitempty"`
	TotalPages      int              `json:"total_pages,omitempty"`
	Page            int              `json:"page,omitempty"`
	Hint            string           `json:"hint,omitempty"`
	XPGained        map[string]any   `json:"xp_gained,omitempty"`
	Analysis        map[string]any   `json:"analysis,omitempty"`
}

// EstimatePurchaseResponse wraps the response from estimate_purchase command.
type EstimatePurchaseResponse struct {
	Action    string  `json:"action"`
	ItemID    string  `json:"item_id"`
	Quantity  int     `json:"quantity"`
	TotalCost float64 `json:"total_cost"`
	Available int     `json:"available"`
	Message   string  `json:"message"`
}

// GetTradesResponse wraps the response from get_trades command.
type GetTradesResponse struct {
	Action string           `json:"action"`
	Trades []map[string]any `json:"trades"`
}

// GetMissionsResponse wraps the response from get_missions command.
type GetMissionsResponse struct {
	Action   string           `json:"action"`
	Missions []map[string]any `json:"missions"`
	BaseName string           `json:"base_name"`
	BaseID   string           `json:"base_id"`
}

// GetActiveMissionsResponse wraps the response from get_active_missions command.
type GetActiveMissionsResponse struct {
	Action   string           `json:"action"`
	Missions []map[string]any `json:"missions"`
}

// CatalogResponse wraps the response from catalog command.
type CatalogResponse struct {
	Type       string           `json:"type"`
	Items      []map[string]any `json:"items"`
	Total      int              `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int              `json:"total_pages"`
	Message    string           `json:"message,omitempty"`
}

// GetInsuranceQuoteResponse wraps the response from get_insurance_quote command.
type GetInsuranceQuoteResponse struct {
	Message string         `json:"message"`
	Quote   map[string]any `json:"quote"`
}

// ClaimInsuranceResponse wraps the response from claim_insurance command.
type ClaimInsuranceResponse struct {
	Action   string           `json:"action"`
	Policies []map[string]any `json:"policies"`
}

// CommissionStatusResponse wraps the response from commission_status command.
type CommissionStatusResponse struct {
	Commissions []map[string]any `json:"commissions"`
	Count       int              `json:"count"`
}

// CommissionQuoteResponse wraps the response from commission_quote command.
type CommissionQuoteResponse struct {
	ShipClass string         `json:"ship_class"`
	Quote     map[string]any `json:"quote"`
	Message   string         `json:"message"`
}

// ListShipsResponse wraps the response from list_ships command.
type ListShipsResponse struct {
	Ships          []map[string]any `json:"ships"`
	Count          int              `json:"count"`
	ActiveShipID   string           `json:"active_ship_id"`
	ActiveShipClass string          `json:"active_ship_class"`
}

// BrowseShipsResponse wraps the response from browse_ships command.
type BrowseShipsResponse struct {
	Action   string           `json:"action"`
	Listings []map[string]any `json:"listings"`
}

// ShipyardShowroomResponse wraps the response from shipyard_showroom command.
type ShipyardShowroomResponse struct {
	Action string           `json:"action"`
	Ships  []map[string]any `json:"ships"`
}

// GetNotesResponse wraps the response from get_notes command.
type GetNotesResponse struct {
	Action string `json:"action"`
	Notes  []Note `json:"notes"`
}

// ReadNoteResponse wraps the response from read_note command.
type ReadNoteResponse struct {
	Action string `json:"action"`
	Note   Note   `json:"note"`
}

// FindRouteResponse wraps the response from find_route command.
type FindRouteResponse struct {
	Action string      `json:"action"`
	Route  []RouteStep `json:"route"`
	Jumps  int         `json:"jumps"`
}

// SearchSystemsResponse wraps the response from search_systems command.
type SearchSystemsResponse struct {
	Action  string               `json:"action"`
	Results []SystemSearchResult `json:"results"`
}

// SurveySystemResponse wraps the response from survey_system command.
type SurveySystemResponse struct {
	Action    string         `json:"action"`
	Message   string         `json:"message"`
	Deposits  []map[string]any `json:"deposits,omitempty"`
	XPGained  map[string]any `json:"xp_gained,omitempty"`
}

// FacilityResponse wraps the response from facility command.
type FacilityResponse struct {
	Action       string           `json:"action"`
	Command      string           `json:"command,omitempty"`
	BaseID       string           `json:"base_id,omitempty"`
	FacilityID   string           `json:"facility_id,omitempty"`
	FacilityName string           `json:"facility_name,omitempty"`
	FacilityType string           `json:"facility_type,omitempty"`
	Description  string           `json:"description,omitempty"`
	Hint         string           `json:"hint,omitempty"`
	Message      string           `json:"message,omitempty"`
	Actions      []map[string]any `json:"actions,omitempty"`
	Types        []map[string]any `json:"types,omitempty"`
	Upgrades     []map[string]any `json:"upgrades,omitempty"`
}

// ViewStorageResponse wraps the response from view_storage command.
type ViewStorageResponse struct {
	BaseID  string           `json:"base_id"`
	Credits int              `json:"credits"`
	Items   []CargoItem      `json:"items"`
	Ships   []map[string]any `json:"ships,omitempty"`
	Gifts   []map[string]any `json:"gifts,omitempty"`
}

// GetCommandsResponse wraps the response from get_commands command.
type GetCommandsResponse struct {
	Action   string           `json:"action"`
	Commands []map[string]any `json:"commands"`
}

// GetGuideResponse wraps the response from get_guide command.
type GetGuideResponse struct {
	Action string `json:"action"`
	Guide  string `json:"guide"`
	Title  string `json:"title"`
}

// SearchChangelogResponse wraps the response from search_changelog command.
type SearchChangelogResponse struct {
	Action  string           `json:"action"`
	Results []map[string]any `json:"results"`
}

// ChatHistoryResponse wraps the response from get_chat_history command.
type ChatHistoryResponse struct {
	Action   string        `json:"action"`
	Messages []ChatMessage `json:"messages"`
	Channel  string        `json:"channel"`
}

// ForumListResponse wraps the response from forum_list command.
type ForumListResponse struct {
	Action  string           `json:"action"`
	Threads []map[string]any `json:"threads"`
	Page    int              `json:"page"`
}

// ForumThreadResponse wraps the response from forum_get_thread command.
type ForumThreadResponse struct {
	Action  string           `json:"action"`
	Thread  map[string]any   `json:"thread"`
	Replies []map[string]any `json:"replies"`
}

// SendGiftResponse wraps the response from send_gift command.
type SendGiftResponse struct {
	Action    string `json:"action"`
	Message   string `json:"message"`
	Recipient string `json:"recipient"`
}

// FactionInfoResponse wraps the response from faction_info command.
type FactionInfoResponse struct {
	Action  string         `json:"action"`
	Faction map[string]any `json:"faction,omitempty"`
}

// LoginResponse wraps the response from the logged_in message.
type LoginResponse struct {
	Player        Player           `json:"player"`
	Ship          Ship             `json:"ship"`
	System        SystemData       `json:"system"`
	POI           POI              `json:"poi"`
	CaptainsLog   []map[string]any `json:"captains_log,omitempty"`
	PendingTrades []map[string]any `json:"pending_trades,omitempty"`
}

// GetVersionResponse wraps the response from get_version command.
type GetVersionResponse struct {
	Action       string           `json:"action"`
	Version      string           `json:"version"`
	ReleaseDate  string           `json:"release_date"`
	ReleaseNotes []string         `json:"release_notes"`
	Changelog    []map[string]any `json:"changelog,omitempty"`
}
