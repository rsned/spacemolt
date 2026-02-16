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

// CargoItem represents an item in the cargo hold.
// Embedded in Ship.Cargo array in server responses.
// Server commands that include this:
//   - Any command returning Ship (logged_in, state_update, get_ship, get_status)
type CargoItem struct {
	ItemID   string  `json:"item_id"`
	Quantity float64 `json:"quantity"`
}

// POI represents a Point of Interest in a system (planets, stations, asteroid belts, etc).
// Server commands that return this struct:
//   - get_poi (single POI in payload.poi)
//   - get_system (array in payload.pois)
//   - logged_in (in payload.poi and payload.system.pois)
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
type SystemData struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Empire         string   `json:"empire"`
	PoliceLevel    int      `json:"police_level"`
	SecurityStatus string   `json:"security_status,omitempty"` // Human-readable: "High Security", "Lawless", etc.
	IsStronghold   bool     `json:"is_stronghold,omitempty"`   // True for pirate stronghold systems
	Online         int      `json:"online,omitempty"`          // Number of online players (from get_map)
	POIs           []POI    `json:"pois"`
	Connections    []string `json:"connections"`
	Discovered     bool     `json:"discovered"`
	Position       Position `json:"position"`
	DiscoveredBy   string   `json:"discovered_by,omitempty"`
	ShipPOI        string   // ID of the POI where the ship is located (internal field, not from JSON)
}

// NearbyPlayer represents another player or pirate NPC at the same POI.
// Server commands that return this struct:
//   - state_update (in payload.nearby array)
//   - get_nearby (in payload.nearby array)
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

// Duplicate declarations removed - keeping only the first set above

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

// Recipe represents a crafting recipe with inputs, outputs, and skill requirements.
// Server commands that return this struct:
//   - get_recipes (in payload.recipes array or map)
type Recipe struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Category       string         `json:"category"`
	RequiredSkills map[string]int `json:"requiredSkills,omitempty"`
	Inputs         []RecipeItem   `json:"inputs"`
	Outputs        []RecipeItem   `json:"outputs"`
	CraftingTime   int            `json:"craftingTime"`
}

// RecipeItem represents an item requirement or output in a recipe.
// Embedded in Recipe.Inputs and Recipe.Outputs arrays.
type RecipeItem struct {
	ItemID   string `json:"itemId"`
	Quantity int    `json:"quantity"`
}

// Mission represents a mission from a mission board (delivery, mining, combat, exploration).
// Server commands that return this struct:
//   - get_missions (available missions at current base)
//   - get_active_missions (accepted missions with progress tracking)
//   - logged_in (in payload.pending_trades for active missions)
type Mission struct {
	ID          string             `json:"id"`
	Type        string             `json:"type"` // delivery, mining, combat, exploration
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Giver       string             `json:"giver"` // NPC or faction name
	GiverBaseID string             `json:"giver_base_id"`
	Rewards     MissionRewards     `json:"rewards"`
	Objectives  []MissionObjective `json:"objectives"`
	Difficulty  int                `json:"difficulty"`
	ExpiresAt   string             `json:"expires_at,omitempty"`  // RFC3339 timestamp
	AcceptedAt  string             `json:"accepted_at,omitempty"` // RFC3339 timestamp (for active missions)
	Progress    map[string]int     `json:"progress,omitempty"`    // For active missions
}

// MissionRewards represents mission completion rewards (credits, items, skill XP).
// Embedded in Mission.Rewards in server responses.
type MissionRewards struct {
	Credits int            `json:"credits"`
	Items   []RecipeItem   `json:"items,omitempty"`
	SkillXP map[string]int `json:"skill_xp,omitempty"`
}

// MissionObjective represents a single objective in a mission (deliver, mine, kill, scan, travel).
// Embedded in Mission.Objectives array in server responses.
type MissionObjective struct {
	Type        string `json:"type"` // deliver, mine, kill, scan, travel
	Description string `json:"description"`
	ItemID      string `json:"item_id,omitempty"`
	Quantity    int    `json:"quantity,omitempty"`
	TargetID    string `json:"target_id,omitempty"` // POI, system, or entity ID
	Completed   bool   `json:"completed,omitempty"` // For active missions
}

// Base represents a player-owned or NPC base (outpost, station, fortress).
// Server commands that return this struct:
//   - get_base (when docked at a base)
//   - build_base (creation response with cost breakdown)
type Base struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Type         string   `json:"type"` // outpost, station, fortress
	OwnerID      string   `json:"owner_id"`
	OwnerName    string   `json:"owner_name,omitempty"`
	FactionID    string   `json:"faction_id,omitempty"`
	POIID        string   `json:"poi_id"`
	SystemID     string   `json:"system_id"`
	Services     []string `json:"services"` // refuel, repair, market, storage, crafting, cloning
	DefenseLevel int      `json:"defense_level"`
	Health       int      `json:"health,omitempty"`
	MaxHealth    int      `json:"max_health,omitempty"`
}

// Wreck represents a destroyed ship's wreckage with lootable contents.
// Server commands that return this struct:
//   - get_wrecks (wrecks at current POI)
type Wreck struct {
	ID        string      `json:"id"`
	ShipClass string      `json:"ship_class"`
	OwnerID   string      `json:"owner_id,omitempty"`
	OwnerName string      `json:"owner_name,omitempty"`
	POIID     string      `json:"poi_id"`
	Contents  []CargoItem `json:"contents"`
	ExpiresAt string      `json:"expires_at"` // RFC3339 timestamp
}

// Drone represents a deployed drone (combat, mining, or repair).
// Server commands that return this struct:
//   - get_drones (all deployed drones with status)
//   - deploy_drone (newly deployed drone response)
type Drone struct {
	ID        string `json:"id"`
	DroneType string `json:"drone_type"` // combat, mining, repair
	ItemID    string `json:"item_id"`    // combat_drone, mining_drone, etc.
	Status    string `json:"status"`     // idle, attacking, mining, assisting
	Hull      int    `json:"hull"`
	MaxHull   int    `json:"max_hull"`
	Damage    int    `json:"damage,omitempty"` // For combat drones
	TargetID  string `json:"target_id,omitempty"`
	POIID     string `json:"poi_id"`
}

// Note represents a tradeable text document (player-created content).
// Server commands that return this struct:
//   - get_notes (list of notes in inventory)
//   - read_note (full note content)
//   - create_note (newly created note response)
type Note struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Content    string `json:"content,omitempty"` // Only in read_note response
	AuthorID   string `json:"author_id"`
	AuthorName string `json:"author_name,omitempty"`
	CreatedAt  string `json:"created_at"` // RFC3339 timestamp
	Value      int    `json:"value"`      // Credits value
}

// Storage represents items and credits stored at a station (v0.45.0+).
// Server commands that return this struct:
//   - view_storage (storage at current station)
type Storage struct {
	BaseID   string      `json:"base_id"`
	BaseName string      `json:"base_name"`
	Items    []CargoItem `json:"items"`
	Credits  int         `json:"credits"`
}

// Faction represents faction information with members, diplomacy, and warfare status.
// Server commands that return this struct:
//   - faction_info (detailed faction info)
//   - faction_list (summary list of all factions)
type Faction struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Tag            string          `json:"tag"`
	LeaderID       string          `json:"leader_id"`
	LeaderName     string          `json:"leader_name,omitempty"`
	MemberCount    int             `json:"member_count"`
	Treasury       int             `json:"treasury,omitempty"`        // Only for members
	Members        []FactionMember `json:"members,omitempty"`         // Only for members
	Allies         []string        `json:"allies,omitempty"`          // Faction IDs
	Enemies        []string        `json:"enemies,omitempty"`         // Faction IDs
	AtWar          []FactionWar    `json:"at_war,omitempty"`          // War details
	PeaceProposals []PeaceProposal `json:"peace_proposals,omitempty"` // Pending proposals
}

// FactionMember represents a member of a faction with role and join date.
// Embedded in Faction.Members array in server responses.
type FactionMember struct {
	PlayerID string `json:"player_id"`
	Username string `json:"username"`
	Role     string `json:"role"`      // recruit, member, officer, leader
	JoinedAt string `json:"joined_at"` // RFC3339 timestamp
}

// FactionWar represents an active war between factions with kill tracking.
// Embedded in Faction.AtWar array in server responses.
type FactionWar struct {
	FactionID   string `json:"faction_id"`
	FactionName string `json:"faction_name"`
	StartedAt   string `json:"started_at"` // RFC3339 timestamp
	OurKills    int    `json:"our_kills"`
	TheirKills  int    `json:"their_kills"`
}

// PeaceProposal represents a peace proposal between factions.
// Embedded in Faction.PeaceProposals array in server responses.
type PeaceProposal struct {
	FromFactionID   string `json:"from_faction_id"`
	FromFactionName string `json:"from_faction_name"`
	Terms           string `json:"terms,omitempty"`
	ProposedAt      string `json:"proposed_at"` // RFC3339 timestamp
}

// ShipClass represents a ship class definition with stats, price, and requirements.
// Server commands that return this struct:
//   - get_ships (all available ship classes at current station)
type ShipClass struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Price          int            `json:"price"`
	Hull           int            `json:"hull"`
	Shield         int            `json:"shield,omitempty"`
	ShieldRecharge int            `json:"shield_recharge,omitempty"`
	Armor          int            `json:"armor,omitempty"`
	Speed          int            `json:"speed"`
	FuelCapacity   int            `json:"fuel_capacity"`
	CargoCapacity  int            `json:"cargo_capacity"`
	CPUCapacity    int            `json:"cpu_capacity"`
	PowerCapacity  int            `json:"power_capacity"`
	WeaponSlots    int            `json:"weapon_slots"`
	DefenseSlots   int            `json:"defense_slots"`
	UtilitySlots   int            `json:"utility_slots"`
	RequiredSkills map[string]int `json:"required_skills,omitempty"`
}

// ExchangeOrder represents a buy or sell order on the station exchange (v0.49.0+).
// Server commands that return this struct:
//   - view_orders (your active orders at current station)
//   - view_market (order book aggregated by price level)
type ExchangeOrder struct {
	ID             string `json:"id"`
	Type           string `json:"type"` // buy or sell
	ItemID         string `json:"item_id"`
	Quantity       int    `json:"quantity"`
	QuantityFilled int    `json:"quantity_filled,omitempty"`
	PriceEach      int    `json:"price_each"`
	SellerID       string `json:"seller_id,omitempty"` // For sell orders
	BuyerID        string `json:"buyer_id,omitempty"`  // For buy orders
	BaseID         string `json:"base_id"`
	CreatedAt      string `json:"created_at"` // RFC3339 timestamp
}

// ChatMessage represents a chat message in any channel (system, local, faction, private).
// Server commands that return this struct:
//   - get_chat_history (paginated chat history)
//
// Server events that send this struct:
//   - chat_message (real-time notification)
type ChatMessage struct {
	ID           string `json:"id"`
	Channel      string `json:"channel"` // system, local, faction, private
	SenderID     string `json:"sender_id"`
	Sender       string `json:"sender"`
	Content      string `json:"content"`
	TargetID     string `json:"target_id,omitempty"`   // For private messages
	TargetName   string `json:"target_name,omitempty"` // For private messages
	TimestampUTC string `json:"timestamp_utc"`         // RFC3339 timestamp
	Timestamp    string `json:"timestamp,omitempty"`   // Legacy field
}

// CaptainsLogEntry represents an entry in the captain's log (max 20 entries, 30KB each).
// Server commands that return this struct:
//   - captains_log_list (paginated entries, 1 at a time as of v0.63.1)
//   - captains_log_get (specific entry by index)
//   - logged_in (most recent entry only)
type CaptainsLogEntry struct {
	Index     int    `json:"index"` // 0 = newest
	Entry     string `json:"entry"`
	CreatedAt string `json:"created_at"` // Timestamp string
}

// RouteStep represents a step in a route from find_route (BFS pathfinding).
// Server commands that return this struct:
//   - find_route (array of steps from current system to destination)
type RouteStep struct {
	SystemID string `json:"system_id"`
	Name     string `json:"name"`
	Jumps    int    `json:"jumps"` // Number of jumps from start
}

// SystemSearchResult represents a system from search_systems (partial name match).
// Server commands that return this struct:
//   - search_systems (case-insensitive search, max 20 results)
type SystemSearchResult struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Position    Position `json:"position"`
	Connections []string `json:"connections"`
}

// ============================================================================
// Server Event Messages (notifications, not responses to commands)
// ============================================================================

// CombatUpdate represents a combat event (player vs player or player vs pirate).
// Server event type: combat_update
// Real-time notification sent during combat encounters.
type CombatUpdate struct {
	Tick       int64   `json:"tick"`
	Attacker   string  `json:"attacker"`
	Target     string  `json:"target"`
	Damage     float64 `json:"damage"`
	DamageType string  `json:"damage_type"` // kinetic, energy, explosive
	ShieldHit  float64 `json:"shield_hit"`
	HullHit    float64 `json:"hull_hit"`
	Destroyed  bool    `json:"destroyed"`
}

// PlayerDied represents ship destruction and respawn in an Escape Pod.
// Server event type: player_died
// Real-time notification when your ship is destroyed (respawn with infinite fuel escape pod).
type PlayerDied struct {
	KillerID        string `json:"killer_id"`
	KillerName      string `json:"killer_name"`
	RespawnBase     string `json:"respawn_base"`
	CloneCost       int    `json:"clone_cost"`
	InsurancePayout int    `json:"insurance_payout"`
	ShipLost        string `json:"ship_lost"`      // Ship class ID
	NewShipClass    string `json:"new_ship_class"` // Usually "escape_pod"
	WreckID         string `json:"wreck_id"`
}

// MiningYield represents successful mining action with resource extraction.
// Server event type: mining_yield
// Real-time notification after mining command succeeds.
type MiningYield struct {
	ResourceID string  `json:"resource_id"`
	Quantity   float64 `json:"quantity"`
	Remaining  float64 `json:"remaining"` // Resources remaining at POI
}

// ScanResult represents the results of scanning a player (reveals info based on scan power).
// Server event type: scan_result
// Real-time notification after scan command with revealed information.
type ScanResult struct {
	TargetID     string   `json:"target_id"`
	Success      bool     `json:"success"`
	RevealedInfo []string `json:"revealed_info"` // Fields revealed: username, ship_class, hull, shield, cloaked
	Username     string   `json:"username,omitempty"`
	ShipClass    string   `json:"ship_class,omitempty"`
	Hull         float64  `json:"hull,omitempty"`
	Shield       float64  `json:"shield,omitempty"`
	Cloaked      bool     `json:"cloaked,omitempty"`
}

// ScanDetected is sent when you are scanned by another player (v0.12.1+).
// Server event type: scan_detected
// Real-time notification when another player scans you, showing what they learned.
type ScanDetected struct {
	ScannerID        string   `json:"scanner_id"`
	ScannerUsername  string   `json:"scanner_username"`
	ScannerShipClass string   `json:"scanner_ship_class"`
	RevealedInfo     []string `json:"revealed_info"`
	Message          string   `json:"message"`
}

// TradeOffer represents an incoming trade offer from another player.
// Server event type: trade_offer_received
// Real-time notification when another player proposes a trade.
type TradeOffer struct {
	TradeID        string      `json:"trade_id"`
	FromPlayer     string      `json:"from_player"`
	FromName       string      `json:"from_name"`
	OfferItems     []CargoItem `json:"offer_items"`
	OfferCredits   int         `json:"offer_credits"`
	RequestItems   []CargoItem `json:"request_items"`
	RequestCredits int         `json:"request_credits"`
}

// PilotlessShip represents a player who disconnected during combat (attackable).
// Server event type: pilotless_ship
// Broadcast to players at POI when someone goes pilotless (potential target).
type PilotlessShip struct {
	PlayerID       string `json:"player_id"`
	PlayerUsername string `json:"player_username"`
	ShipID         string `json:"ship_id"`
	ShipClass      string `json:"ship_class"`
	SystemID       string `json:"system_id"`
	POIID          string `json:"poi_id"`
	ExpireTick     int64  `json:"expire_tick"`
	TicksRemaining int    `json:"ticks_remaining"`
}

// ReconnectedMessage is sent when you reconnect after disconnecting during combat.
// Server event type: reconnected
// Real-time notification upon reconnecting to regain control of your ship.
type ReconnectedMessage struct {
	Message        string `json:"message"`
	WasPilotless   bool   `json:"was_pilotless"`
	TicksRemaining int    `json:"ticks_remaining"`
}

// POIArrival is broadcast when a player arrives at your POI (situational awareness).
// Server event type: poi_arrival (v0.41.19+)
// Real-time notification when non-anonymous players arrive at your location.
type POIArrival struct {
	Username string `json:"username"`
	ClanTag  string `json:"clan_tag"`
	POIName  string `json:"poi_name"`
	POIID    string `json:"poi_id"`
}

// POIDeparture is broadcast when a player leaves your POI (destination not revealed).
// Server event type: poi_departure (v0.41.19+)
// Real-time notification when non-anonymous players depart from your location.
type POIDeparture struct {
	Username string `json:"username"`
	ClanTag  string `json:"clan_tag"`
	POIName  string `json:"poi_name"`
	POIID    string `json:"poi_id"`
}

// PoliceWarning is sent when you commit a crime in policed space (security response imminent).
// Server event type: police_warning
// Real-time notification with response delay based on police level.
type PoliceWarning struct {
	Message       string `json:"message"`
	PoliceLevel   int    `json:"police_level"`
	ResponseTicks int    `json:"response_ticks"` // How many ticks until police arrive
	System        string `json:"system"`
}

// PoliceSpawn is sent when police drones arrive to engage hostile.
// Server event type: police_spawn
// Real-time notification when security forces spawn at your location.
type PoliceSpawn struct {
	Message   string `json:"message"`
	NumDrones int    `json:"num_drones"`
	Target    string `json:"target"` // Player ID
}

// PoliceCombat represents police drone combat (damage dealt each tick).
// Server event type: police_combat
// Real-time notification each tick when police drones attack you.
type PoliceCombat struct {
	Tick       int64   `json:"tick"`
	DroneID    string  `json:"drone_id"`
	TargetID   string  `json:"target_id"`
	Damage     float64 `json:"damage"`
	DamageType string  `json:"damage_type"`
	Destroyed  bool    `json:"destroyed"`
}

// PirateWarning is sent when a pirate NPC detects you at their POI (v0.50.0+).
// Server event type: pirate_warning
// Real-time notification with attack delay (0 for stronghold bosses, 3 ticks for regulars).
type PirateWarning struct {
	PirateID      string `json:"pirate_id"`
	PirateName    string `json:"pirate_name"`
	Tier          string `json:"tier"` // small, medium, large, boss
	IsBoss        bool   `json:"is_boss"`
	AttackInTicks int    `json:"attack_in_ticks"`
	Message       string `json:"message"`
}

// PirateCombat represents pirate NPC combat (damage dealt each tick, v0.50.0+).
// Server event type: pirate_combat
// Real-time notification each tick when pirate NPCs attack you.
type PirateCombat struct {
	Tick         int64   `json:"tick"`
	PirateID     string  `json:"pirate_id"`
	PirateName   string  `json:"pirate_name"`
	Damage       float64 `json:"damage"`
	DamageType   string  `json:"damage_type"`
	PlayerHull   float64 `json:"player_hull"`
	PlayerShield float64 `json:"player_shield"`
	Destroyed    bool    `json:"destroyed"`
}

// PirateDestroyed is sent when you destroy a pirate NPC (rewards and wreck, v0.50.0+).
// Server event type: pirate_destroyed
// Real-time notification with credits reward, XP gained, and wreck ID for looting.
type PirateDestroyed struct {
	PirateID      string  `json:"pirate_id"`
	PirateName    string  `json:"pirate_name"`
	Tier          string  `json:"tier"`
	IsBoss        bool    `json:"is_boss"`
	CreditsReward int     `json:"credits_reward"`
	XPGained      float64 `json:"xp_gained"`
	WreckID       string  `json:"wreck_id"`
	Message       string  `json:"message"`
}

// PirateSpawn is sent when a pirate NPC respawns at your current POI (v0.50.0+).
// Server event type: pirate_spawn
// Real-time notification when new pirates appear at your location.
type PirateSpawn struct {
	PirateID   string `json:"pirate_id"`
	PirateName string `json:"pirate_name"`
	Tier       string `json:"tier"`
	IsBoss     bool   `json:"is_boss"`
	Message    string `json:"message"`
}

// DroneUpdate represents drone combat activity (damage dealt by your drones).
// Server event type: drone_update
// Real-time notification each tick when your deployed drones deal damage.
type DroneUpdate struct {
	Tick      int64   `json:"tick"`
	DroneID   string  `json:"drone_id"`
	TargetID  string  `json:"target_id"`
	Damage    float64 `json:"damage"`
	Destroyed bool    `json:"destroyed"`
}

// DroneDestroyed is sent when one of your drones is destroyed in combat.
// Server event type: drone_destroyed
// Real-time notification when your deployed drone is destroyed.
type DroneDestroyed struct {
	DroneID   string `json:"drone_id"`
	DroneType string `json:"drone_type"`
	Message   string `json:"message"`
}

// BaseRaidUpdate is sent during a base raid (progress and damage tracking).
// Server event type: base_raid_update
// Real-time notification each tick to attackers and base owner.
type BaseRaidUpdate struct {
	BaseID        string `json:"base_id"`
	BaseName      string `json:"base_name"`
	CurrentHealth int    `json:"current_health"`
	MaxHealth     int    `json:"max_health"`
	DamagePerTick int    `json:"damage_per_tick"`
	AttackerCount int    `json:"attacker_count"`
	Message       string `json:"message"`
}

// BaseDestroyed is sent when a base is destroyed (wreck created for looting).
// Server event type: base_destroyed
// Real-time notification when base destruction completes.
type BaseDestroyed struct {
	BaseID    string `json:"base_id"`
	BaseName  string `json:"base_name"`
	OwnerID   string `json:"owner_id"`
	OwnerName string `json:"owner_name"`
	WreckID   string `json:"wreck_id"`
	Message   string `json:"message"`
}

// SkillLevelUp is sent when you level up a skill through passive training.
// Server event type: skill_level_up
// Real-time notification when you gain enough XP to level up a skill.
type SkillLevelUp struct {
	SkillID  string  `json:"skill_id"`
	NewLevel int     `json:"new_level"`
	XPGained float64 `json:"xp_gained"`
}
