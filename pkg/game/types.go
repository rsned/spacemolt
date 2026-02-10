package game

import (
	"sync"
	"time"
)

// Player represents a player in the game
type Player struct {
	ID             string           `json:"id"`
	Username       string           `json:"username"`
	Empire         string           `json:"empire"`
	Credits        float64          `json:"credits"`
	CurrentSystem  string           `json:"current_system"`
	CurrentPOI     string           `json:"current_poi"`
	CurrentShipID  string           `json:"current_ship_id"`
	HomeBase       string           `json:"home_base"`
	DockedAtBase   string           `json:"docked_at_base"`
	FactionID      string           `json:"faction_id,omitempty"`
	FactionRank    string           `json:"faction_rank,omitempty"`
	StatusMessage  string           `json:"status_message,omitempty"`
	ClanTag        string           `json:"clan_tag,omitempty"`
	PrimaryColor   string           `json:"primary_color,omitempty"`
	SecondaryColor string           `json:"secondary_color,omitempty"`
	Anonymous      bool             `json:"anonymous"`
	IsCloaked      bool             `json:"is_cloaked"`
	Skills         map[string]Skill `json:"skills"`
	Stats          PlayerStats      `json:"stats"`
}

// Skill represents a player's skill level and XP
type Skill struct {
	Level int     `json:"level"`
	XP    float64 `json:"xp"`
}

// SkillDefinition holds static skill data from get_skills (e.g. xp_per_level).
type SkillDefinition struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	MaxLevel   int       `json:"max_level"`
	XpPerLevel []float64 `json:"xp_per_level"` // XP required to reach level i+1 (index 0 = level 0->1)
}

// PlayerStats tracks player statistics
type PlayerStats struct {
	ShipsDestroyed    int     `json:"ships_destroyed"`
	TimesDestroyed    int     `json:"times_destroyed"`
	OreMined          float64 `json:"ore_mined"`
	CreditsEarned     float64 `json:"credits_earned"`
	CreditsSpent      float64 `json:"credits_spent"`
	TradesCompleted   int     `json:"trades_completed"`
	SystemsDiscovered int     `json:"systems_discovered"`
	ItemsCrafted      int     `json:"items_crafted"`
	MissionsCompleted int     `json:"missions_completed"`
}

// Ship represents the player's ship
type Ship struct {
	ID             string      `json:"id"`
	OwnerID        string      `json:"owner_id"`
	ClassID        string      `json:"class_id"`
	Name           string      `json:"name"`
	Hull           float64     `json:"hull"`
	MaxHull        float64     `json:"max_hull"`
	Shield         float64     `json:"shield"`
	MaxShield      float64     `json:"max_shield"`
	ShieldRecharge float64     `json:"shield_recharge"`
	Armor          float64     `json:"armor"`
	Speed          float64     `json:"speed"`
	Fuel           float64     `json:"fuel"`
	MaxFuel        float64     `json:"max_fuel"`
	CargoUsed      float64     `json:"cargo_used"`
	CargoCapacity  float64     `json:"cargo_capacity"`
	CPUUsed        float64     `json:"cpu_used"`
	CPUCapacity    float64     `json:"cpu_capacity"`
	PowerUsed      float64     `json:"power_used"`
	PowerCapacity  float64     `json:"power_capacity"`
	Modules        []string    `json:"modules"`
	Cargo          []CargoItem `json:"cargo"`
}

// CargoItem represents an item in the cargo hold
type CargoItem struct {
	ItemID   string  `json:"item_id"`
	Quantity float64 `json:"quantity"`
}

// POI represents a Point of Interest in a system
type POI struct {
	ID          string        `json:"id"`
	SystemID    string        `json:"system_id"`
	Type        string        `json:"type"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Position    Position      `json:"position"`
	Resources   []POIResource `json:"resources"`
	BaseID      string        `json:"base_id,omitempty"`
}

// Position represents 3D coordinates (Z is reserved for future use)
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z,omitempty"` // Reserved for future 3D positioning
}

// POIResource represents a resource at a POI
type POIResource struct {
	ResourceID string  `json:"resource_id"`
	Richness   float64 `json:"richness"`
	Remaining  float64 `json:"remaining"`
}

// SystemData holds the current system information
type SystemData struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Empire       string   `json:"empire"`
	PoliceLevel  int      `json:"police_level"`
	POIs         []POI    `json:"pois"`
	Connections  []string `json:"connections"`
	Discovered   bool     `json:"discovered"`
	Position     Position `json:"position"`
	DiscoveredBy string   `json:"discovered_by,omitempty"`
	ShipPOI      string   // ID of the POI where the ship is located
}

// NearbyPlayer represents another player at the same POI
type NearbyPlayer struct {
	PlayerID       string `json:"player_id"`
	Username       string `json:"username"`
	ShipClass      string `json:"ship_class"`
	FactionID      string `json:"faction_id,omitempty"`
	FactionTag     string `json:"faction_tag,omitempty"`
	StatusMessage  string `json:"status_message,omitempty"`
	ClanTag        string `json:"clan_tag,omitempty"`
	PrimaryColor   string `json:"primary_color,omitempty"`
	SecondaryColor string `json:"secondary_color,omitempty"`
	Anonymous      bool   `json:"anonymous"`
	InCombat       bool   `json:"in_combat"`
}

// TravelProgress represents travel state when in transit
type TravelProgress struct {
	Progress    float64 `json:"travel_progress"`
	Destination string  `json:"travel_destination"`
	Type        string  `json:"travel_type"` // "travel" or "jump"
	ArrivalTick int64   `json:"travel_arrival_tick"`
}

// State represents the current game state
type State struct {
	Mu             sync.Mutex
	Username       string
	Password       string // Permanent password from registration (64-char hex string)
	Doc            bool
	CurrentSystem  string
	CurrentPOI     string
	Traveling      bool
	TravelProgress *TravelProgress // nil when not traveling
	ServerVersion  string          // Server API version

	// Player data
	Player  Player
	Credits float64
	SkillXP map[string]float64 `json:"skill_xp"` // Current XP toward next level for each skill

	// From get_skills response: XP required for next level per skill; nil if not yet loaded.
	SkillNextLevelXP map[string]float64
	// From get_skills response: skill definitions (xp_per_level, max_level, name); nil if not loaded.
	SkillDefinitions map[string]SkillDefinition

	// Ship data
	Ship     Ship
	Fuel     float64
	MaxFuel  float64
	Hull     float64
	MaxHull  float64
	Cargo    []map[string]any // Legacy format, use Ship.Cargo
	MaxCargo int

	// Module definitions (maps module ID to name/type)
	ModuleDefinitions map[string]ModuleDefinition `json:"modules,omitempty"`

	// System data
	System        SystemData
	CurrentTick   int64
	LastMapUpdate time.Time

	// Nearby players (from state_update)
	Nearby   []NearbyPlayer
	InCombat bool
}

// ModuleDefinition represents a module's definition
type ModuleDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // "weapon", "defense", "utility", etc.
	Description string `json:"description"`
}

// copyStringFloatMap returns a shallow copy of m; returns nil if m is nil.
func copyStringFloatMap(m map[string]float64) map[string]float64 {
	if m == nil {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// copySkillDefsMap returns a shallow copy of m; returns nil if m is nil.
func copySkillDefsMap(m map[string]SkillDefinition) map[string]SkillDefinition {
	if m == nil {
		return nil
	}
	out := make(map[string]SkillDefinition, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Clone creates a deep copy of the state for safe concurrent access
func (s *State) Clone() *State {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	cargoCopy := make([]map[string]any, len(s.Cargo))
	for i, item := range s.Cargo {
		cargoCopy[i] = make(map[string]any, len(item))
		for k, v := range item {
			cargoCopy[i][k] = v
		}
	}

	poisCopy := make([]POI, len(s.System.POIs))
	copy(poisCopy, s.System.POIs)

	connectionsCopy := make([]string, len(s.System.Connections))
	copy(connectionsCopy, s.System.Connections)

	nearbyCopy := make([]NearbyPlayer, len(s.Nearby))
	copy(nearbyCopy, s.Nearby)

	cloned := &State{
		Username:      s.Username,
		Password:      s.Password,
		Doc:           s.Doc,
		CurrentSystem: s.CurrentSystem,
		CurrentPOI:    s.CurrentPOI,
		Traveling:     s.Traveling,
		ServerVersion: s.ServerVersion,
		Credits:       s.Credits,
		Fuel:          s.Fuel,
		MaxFuel:       s.MaxFuel,
		Hull:          s.Hull,
		MaxHull:       s.MaxHull,
		Cargo:         cargoCopy,
		MaxCargo:      s.MaxCargo,
		CurrentTick:   s.CurrentTick,
		Player:        s.Player,
		Ship:          s.Ship,
		System: SystemData{
			Name:        s.System.Name,
			Description: s.System.Description,
			POIs:        poisCopy,
			Connections: connectionsCopy,
			ShipPOI:     s.System.ShipPOI,
		},
		LastMapUpdate:    s.LastMapUpdate,
		Nearby:           nearbyCopy,
		InCombat:         s.InCombat,
		SkillXP:          copyStringFloatMap(s.SkillXP),
		SkillNextLevelXP: copyStringFloatMap(s.SkillNextLevelXP),
		SkillDefinitions: copySkillDefsMap(s.SkillDefinitions),
	}

	// Clone travel progress if present
	if s.TravelProgress != nil {
		cloned.TravelProgress = &TravelProgress{
			Progress:    s.TravelProgress.Progress,
			Destination: s.TravelProgress.Destination,
			Type:        s.TravelProgress.Type,
			ArrivalTick: s.TravelProgress.ArrivalTick,
		}
	}

	return cloned
}

// IsDocked returns whether the ship is currently docked
func (s *State) IsDocked() bool {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Doc
}

// GetCurrentSystem returns the current system name
func (s *State) GetCurrentSystem() string {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.CurrentSystem
}

// GetCredits returns the current credits
func (s *State) GetCredits() float64 {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Credits
}

// GetFuel returns current and max fuel
func (s *State) GetFuel() (float64, float64) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Fuel, s.MaxFuel
}

// GetSystem returns the current system data
func (s *State) GetSystem() SystemData {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.System
}

// GetTick returns the current game tick
func (s *State) GetTick() int64 {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.CurrentTick
}

// GetNearbyPlayers returns the list of nearby players
func (s *State) GetNearbyPlayers() []NearbyPlayer {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Nearby
}

// MarketListing represents a single market listing
type MarketListing struct {
	ItemID       string  `json:"item_id"`
	ItemType     string  `json:"item_type"`
	Quantity     float64 `json:"quantity"`
	PricePerUnit float64 `json:"price_per_unit"`
	TotalPrice   float64 `json:"total_price"`
	Type         string  `json:"type"` // 'buy' or 'sell'
	ListedBy     string  `json:"listed_by,omitempty"`
}

// MarketSnapshot represents a captured market state
type MarketSnapshot struct {
	SystemID    string          `json:"system_id"`
	SystemName  string          `json:"system_name"`
	StationID   string          `json:"station_id"`
	StationName string          `json:"station_name"`
	GameTick    int64           `json:"game_tick"`
	Listings    []MarketListing `json:"listings"`
	CapturedAt  time.Time       `json:"captured_at"`
}
