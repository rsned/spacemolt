package game

import (
	"sync"
	"time"
)

// ServerAPIVersion is the game server API version this client was built against.
// See server_docs/api.md for full API documentation.
// This version should match the version documented at the top of server_docs/api.md
const ServerAPIVersion = "v0.63.1"

// Player represents a player in the game.
// Server commands that return this struct:
//   - logged_in (in payload.player)
//   - state_update (in payload.player)
//   - get_status (in payload.player)
type Player struct {
	ID             string             `json:"id"`
	Username       string             `json:"username"`
	Empire         string             `json:"empire"`
	Credits        float64            `json:"credits"`
	CurrentSystem  string             `json:"current_system"`
	CurrentPOI     string             `json:"current_poi"`
	CurrentShipID  string             `json:"current_ship_id"`
	HomeBase       string             `json:"home_base"`
	DockedAtBase   string             `json:"docked_at_base"`
	FactionID      string             `json:"faction_id,omitempty"`
	FactionRank    string             `json:"faction_rank,omitempty"`
	StatusMessage  string             `json:"status_message,omitempty"`
	ClanTag        string             `json:"clan_tag,omitempty"`
	PrimaryColor   string             `json:"primary_color,omitempty"`
	SecondaryColor string             `json:"secondary_color,omitempty"`
	Anonymous      bool               `json:"anonymous"`
	IsCloaked      bool               `json:"is_cloaked"`
	Skills         map[string]Skill   `json:"skills"`
	SkillXP        map[string]float64 `json:"skill_xp,omitempty"` // Current XP toward next level
	Stats          PlayerStats        `json:"stats"`
}

// Skill represents a player's skill level and XP.
// Embedded in Player.Skills map in server responses.
// Server commands that include this:
//   - Any command returning Player (logged_in, state_update, get_status)
type Skill struct {
	Level int     `json:"level"`
	XP    float64 `json:"xp"`
}

// SkillDefinition holds static skill data (e.g. xp_per_level, max_level, bonuses).
// Server commands that return this struct:
//   - get_skills (in payload.skills map, keyed by skill_id)
type SkillDefinition struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Description   string             `json:"description,omitempty"`
	Category      string             `json:"category,omitempty"`
	MaxLevel      int                `json:"max_level"`
	XpPerLevel    []float64          `json:"xp_per_level"`              // XP required to reach level i+1 (index 0 = level 0->1)
	BonusPerLevel map[string]float64 `json:"bonus_per_level,omitempty"` // e.g. {"miningYield": 5}
}

// PlayerSkill represents a player's progress in a specific skill with XP tracking.
// Server commands that return this struct:
//   - get_skills (in payload.player_skills array)
type PlayerSkill struct {
	SkillID     string  `json:"skill_id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Level       int     `json:"level"`
	MaxLevel    int     `json:"max_level"`
	CurrentXP   float64 `json:"current_xp"`
	NextLevelXP float64 `json:"next_level_xp"`
}

// PlayerStats tracks player statistics and lifetime achievements.
// Embedded in Player.Stats in server responses.
// Server commands that include this:
//   - Any command returning Player (logged_in, state_update, get_status)
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

// Ship represents the player's ship with all stats, modules, and cargo.
// Server commands that return this struct:
//   - logged_in (in payload.ship)
//   - state_update (in payload.ship)
//   - get_ship (in payload.ship)
//   - get_status (in payload.ship)
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
	WeaponSlots    int         `json:"weapon_slots"`
	DefenseSlots   int         `json:"defense_slots"`
	UtilitySlots   int         `json:"utility_slots"`
	Modules        []string    `json:"modules"`
	Cargo          []CargoItem `json:"cargo"`
}

// ConnectionInfo represents a connection from one system to another with details.
// Embedded in Ship.Cargo array in server responses.
// Server commands that include this:
//   - Any command returning Ship (logged_in, state_update, get_ship, get_status)
type CargoItem struct {
	ItemID   string  `json:"item_id"`
	Quantity float64 `json:"quantity"`
}

// ConnectionInfo represents a connection from one system to another with details.
//   - logged_in (in payload.system.connections)
type ConnectionInfo struct {
	SystemID string `json:"system_id"`
	Name     string `json:"name"`
	Distance int    `json:"distance"`
}

// POI represents a Point of Interest in a system (planets, stations, asteroid belts, etc).
// Server commands that return this struct:
//   - get_poi (single POI in payload.poi)
//   - get_system (array in payload.pois)
//   - logged_in (in payload.poi and payload.system.pois)
//
// v0.87.1+ Enriched format includes has_base, base_name, and online fields.
type POI struct {
	ID          string        `json:"id"`
	SystemID    string        `json:"system_id"`
	Type        string        `json:"type"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Position    Position      `json:"position"`
	Resources   []POIResource `json:"resources"`
	BaseID      string        `json:"base_id,omitempty"`
	HasBase     bool          `json:"has_base,omitempty"`   // v0.87.1+: Whether this POI has a base
	BaseName    string        `json:"base_name,omitempty"`  // v0.87.1+: Name of base at this POI
	Online      int           `json:"online,omitempty"`     // v0.87.1+: Number of players at this POI
}

// Position represents 3D coordinates (Z is reserved for future use).
// Embedded in POI and SystemData in server responses.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z,omitempty"` // Reserved for future 3D positioning
}

// POIResource represents a minable resource at a POI.
// Embedded in POI.Resources array in server responses.
type POIResource struct {
	ResourceID string  `json:"resource_id"`
	Richness   float64 `json:"richness"`
	Remaining  float64 `json:"remaining"`
}

// SystemData holds complete system information including POIs, connections, and security status.
// Server commands that return this struct:
//   - get_system (in payload.system)
//   - logged_in (in payload.system)
//   - get_map (array of systems when called without system_id parameter)
//
// v0.87.1+ Enriched format:
//   - POIs are objects with has_base/base_name/online instead of bare IDs
//   - Connections are ConnectionInfo objects instead of bare system IDs
type SystemData struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Empire         string          `json:"empire"`
	PoliceLevel    int             `json:"police_level"`
	SecurityStatus string          `json:"security_status,omitempty"` // Human-readable: "High Security", "Lawless", etc.
	IsStronghold   bool            `json:"is_stronghold,omitempty"`   // True for pirate stronghold systems
	Online         int             `json:"online,omitempty"`          // Number of online players (from get_map)
	POIs           []POI           `json:"pois"`                     // v0.87.1+: Enriched POI objects
	Connections    []ConnectionInfo `json:"connections"`              // v0.87.1+: ConnectionInfo objects instead of system IDs
	Discovered     bool            `json:"discovered"`
	Position       Position        `json:"position"`
	DiscoveredBy   string          `json:"discovered_by,omitempty"`
	ShipPOI        string          // ID of the POI where the ship is located (internal field, not from JSON)
}

// NearbyPlayer represents another player or pirate NPC at the same POI.
// Server commands that return this struct:
//   - state_update (in payload.nearby array)
//   - get_nearby (in payload.nearby array)
//
// Note: As of v0.93.0, the server only sends player_id, username, ship_class,
// primary_color, secondary_color, anonymous, and in_combat. Other fields
// (faction_id, faction_tag, status_message, clan_tag) are present in the
// documentation but not sent by the current server.
type NearbyPlayer struct {
	PlayerID       string `json:"player_id"`
	Username       string `json:"username"`
	ShipClass      string `json:"ship_class"`
	PrimaryColor   string `json:"primary_color,omitempty"`
	SecondaryColor string `json:"secondary_color,omitempty"`
	Anonymous      bool   `json:"anonymous"`
	InCombat       bool   `json:"in_combat"`
	// Deprecated: Server no longer sends these fields as of v0.93.0
	FactionID     string `json:"faction_id,omitempty"`
	FactionTag     string `json:"faction_tag,omitempty"`
	StatusMessage  string `json:"status_message,omitempty"`
	ClanTag        string `json:"clan_tag,omitempty"`
}

// TravelProgress represents travel state when in transit (travel or jump).
// Server commands that return this struct:
//   - state_update (when traveling, as separate fields in payload)
//
// Fields: travel_progress, travel_destination, travel_type, travel_arrival_tick
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

	// Pirate combat information
	PirateName string
	PirateTier string
	PirateID   string
	LastDamage float64
}

// ModuleDefinition represents a module's definition including stats and requirements.
// Server commands that return this struct:
//   - get_ship (in payload.modules array with full stats)
//   - logged_in (embedded in player.modules map)
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

	connectionsCopy := make([]ConnectionInfo, len(s.System.Connections))
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
			ID:             s.System.ID,
			Name:           s.System.Name,
			Description:    s.System.Description,
			Empire:         s.System.Empire,
			PoliceLevel:    s.System.PoliceLevel,
			SecurityStatus: s.System.SecurityStatus,
			IsStronghold:   s.System.IsStronghold,
			Online:         s.System.Online,
			POIs:           poisCopy,
			Connections:    connectionsCopy,
			Discovered:     s.System.Discovered,
			Position:       s.System.Position,
			DiscoveredBy:   s.System.DiscoveredBy,
			ShipPOI:        s.System.ShipPOI,
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

// MarketListing represents a single market listing (NPC or player exchange order).
// Server commands that return this struct:
//   - get_listings (in payload.listings array)
type MarketListing struct {
	ItemID       string  `json:"item_id"`
	ItemType     string  `json:"item_type"`
	Quantity     float64 `json:"quantity"`
	PricePerUnit float64 `json:"price_per_unit"`
	TotalPrice   float64 `json:"total_price"`
	Type         string  `json:"type"` // 'buy' or 'sell'
	ListedBy     string  `json:"listed_by,omitempty"`
}

// MarketSnapshot represents a captured market state.
// This is an internal client-side structure for data collection, NOT a server response.
type MarketSnapshot struct {
	SystemID    string          `json:"system_id"`
	SystemName  string          `json:"system_name"`
	StationID   string          `json:"station_id"`
	StationName string          `json:"station_name"`
	GameTick    int64           `json:"game_tick"`
	Listings    []MarketListing `json:"listings"`
	CapturedAt  time.Time       `json:"captured_at"`
}

