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
	Action       string                      `json:"action"`
	Skills       map[string]SkillDefinition  `json:"skills"`
	PlayerSkills []PlayerSkill               `json:"player_skills"`
}

// GetListingsResponse wraps the response from get_listings command.
type GetListingsResponse struct {
	Action      string          `json:"action"`
	Listings    []MarketListing `json:"listings"`
	StationID   string          `json:"station_id"`
	StationName string          `json:"station_name"`
}

// GetShipsResponse wraps the response from get_ships command.
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
