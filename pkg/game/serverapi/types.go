package serverapi

import (
	"encoding/json"
	"fmt"
)

// ============================================================================
// Server API Types
// These types mirror the game server's JSON wire format for API responses.
// They should change freely as the server evolves.
// ============================================================================

// CargoItem represents an item in cargo. Used by both events and entity types.
type CargoItem struct {
	ItemID   string  `json:"item_id"`
	Quantity float64 `json:"quantity"`
}

// Position represents 3D coordinates.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z,omitempty"`
}

// POIResource represents a minable resource at a POI.
type POIResource struct {
	ResourceID       string  `json:"resource_id"`
	Name             string  `json:"name,omitempty"`
	Richness         float64 `json:"richness"`
	Remaining        float64 `json:"remaining"`
	RemainingDisplay string  `json:"remaining_display,omitempty"`
}

// ConnectionInfo represents a connection from one system to another.
type ConnectionInfo struct {
	SystemID string `json:"system_id"`
	Name     string `json:"name"`
	Distance int    `json:"distance"`
}

// Skill represents a player's skill level and XP.
// The server may send skills as either a plain number (level only) or an object
// with level and xp fields. UnmarshalJSON handles both formats.
type Skill struct {
	Level int     `json:"level"`
	XP    float64 `json:"xp"`
}

// UnmarshalJSON handles both simple (number) and detailed (object) skill formats.
func (s *Skill) UnmarshalJSON(data []byte) error {
	// Try as number first (simple format: skill level as integer)
	var level float64
	if err := json.Unmarshal(data, &level); err == nil {
		s.Level = int(level)
		return nil
	}
	// Try as object (detailed format: {"level": N, "xp": N})
	type skillAlias Skill
	var alias skillAlias
	if err := json.Unmarshal(data, &alias); err == nil {
		*s = Skill(alias)
		return nil
	}
	return fmt.Errorf("cannot unmarshal skill from %s", string(data))
}

// PlayerStats tracks player statistics and lifetime achievements.
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
	BasesDestroyed    int     `json:"bases_destroyed"`
	DistanceTraveled  int64   `json:"distance_traveled"`
	PiratesDestroyed  int     `json:"pirates_destroyed"`
	ShipsLost         int     `json:"ships_lost"`
	TimePlayed        int64   `json:"time_played"`
}

// ModuleDefinition represents a module's definition including stats and requirements.
type ModuleDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// Player represents a player in the game as returned by the server.
type Player struct {
	ID                string                 `json:"id"`
	Username          string                 `json:"username"`
	Empire            string                 `json:"empire"`
	Credits           float64                `json:"credits"`
	CurrentSystem     string                 `json:"current_system"`
	CurrentPOI        string                 `json:"current_poi"`
	CurrentShipID     string                 `json:"current_ship_id"`
	HomeBase          string                 `json:"home_base"`
	DockedAtBase      string                 `json:"docked_at_base"`
	FactionID         string                 `json:"faction_id,omitempty"`
	FactionRank       string                 `json:"faction_rank,omitempty"`
	StatusMessage     string                 `json:"status_message,omitempty"`
	ClanTag           string                 `json:"clan_tag,omitempty"`
	PrimaryColor      string                 `json:"primary_color,omitempty"`
	SecondaryColor    string                 `json:"secondary_color,omitempty"`
	Anonymous         bool                   `json:"anonymous"`
	IsCloaked         bool                   `json:"is_cloaked"`
	Skills            map[string]Skill       `json:"skills"`
	SkillXP           map[string]float64     `json:"skill_xp,omitempty"`
	Stats             PlayerStats            `json:"stats"`
	Modules           map[string]ModuleDefinition `json:"modules,omitempty"`
	TowingWreckID     string                 `json:"towing_wreck_id,omitempty"`
	Experience        int64                  `json:"experience,omitempty"`
	DiscoveredSystems map[string]any         `json:"discovered_systems,omitempty"`
	LastActiveAt      string                 `json:"last_active_at,omitempty"`
	LastLoginAt       string                 `json:"last_login_at,omitempty"`
	CreatedAt         string                 `json:"created_at,omitempty"`
}

// Ship represents the player's ship with all stats, modules, and cargo.
type Ship struct {
	ID                       string           `json:"id"`
	OwnerID                  string           `json:"owner_id"`
	ClassID                  string           `json:"class_id"`
	Name                     string           `json:"name"`
	Hull                     float64          `json:"hull"`
	MaxHull                  float64          `json:"max_hull"`
	Shield                   float64          `json:"shield"`
	MaxShield                float64          `json:"max_shield"`
	ShieldRecharge           float64          `json:"shield_recharge"`
	Armor                    float64          `json:"armor"`
	Speed                    float64          `json:"speed"`
	Fuel                     float64          `json:"fuel"`
	MaxFuel                  float64          `json:"max_fuel"`
	CargoUsed                float64          `json:"cargo_used"`
	CargoCapacity            float64          `json:"cargo_capacity"`
	CPUUsed                  float64          `json:"cpu_used"`
	CPUCapacity              float64          `json:"cpu_capacity"`
	PowerUsed                float64          `json:"power_used"`
	PowerCapacity            float64          `json:"power_capacity"`
	WeaponSlots              int              `json:"weapon_slots"`
	DefenseSlots             int              `json:"defense_slots"`
	UtilitySlots             int              `json:"utility_slots"`
	Modules                  []string         `json:"modules"`
	Cargo                    []CargoItem      `json:"cargo"`
	ActiveBuffs              []map[string]any `json:"active_buffs,omitempty"`
	DamagePenalty            float64          `json:"damage_penalty,omitempty"`
	SpeedPenalty             float64          `json:"speed_penalty,omitempty"`
	DisruptionTicksRemaining int              `json:"disruption_ticks_remaining,omitempty"`
	DockedAtBase             string           `json:"docked_at_base,omitempty"`
	LastProcessTick          int64            `json:"last_process_tick,omitempty"`
	CreatedAt                string           `json:"created_at,omitempty"`
}

// POI represents a Point of Interest in a system.
type POI struct {
	ID          string        `json:"id"`
	SystemID    string        `json:"system_id"`
	Type        string        `json:"type"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Position    Position      `json:"position"`
	Resources   []POIResource `json:"resources"`
	BaseID      string        `json:"base_id,omitempty"`
	HasBase     bool          `json:"has_base,omitempty"`
	BaseName    string        `json:"base_name,omitempty"`
	Online      int           `json:"online,omitempty"`
	Hidden      bool          `json:"hidden,omitempty"`
}

// SystemData holds complete system information.
type SystemData struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Description    string           `json:"description"`
	Empire         string           `json:"empire"`
	PoliceLevel    int              `json:"police_level"`
	SecurityStatus string           `json:"security_status,omitempty"`
	IsStronghold   bool             `json:"is_stronghold,omitempty"`
	Online         int              `json:"online,omitempty"`
	POIs           []POI            `json:"pois"`
	Connections    []ConnectionInfo `json:"connections"`
	Discovered     bool             `json:"discovered"`
	Position       Position         `json:"position"`
	DiscoveredBy   string           `json:"discovered_by,omitempty"`
}

// NearbyPlayer represents another player or pirate NPC at the same POI.
type NearbyPlayer struct {
	PlayerID       string `json:"player_id"`
	Username       string `json:"username"`
	ShipClass      string `json:"ship_class"`
	PrimaryColor   string `json:"primary_color,omitempty"`
	SecondaryColor string `json:"secondary_color,omitempty"`
	Anonymous      bool   `json:"anonymous"`
	InCombat       bool   `json:"in_combat"`
	FactionID      string `json:"faction_id,omitempty"`
	FactionTag     string `json:"faction_tag,omitempty"`
	StatusMessage  string `json:"status_message,omitempty"`
	ClanTag        string `json:"clan_tag,omitempty"`
}

// CurrentPOI represents the current POI with minimal info (from get_system response).
type CurrentPOI struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	HasBase  bool     `json:"has_base"`
	BaseID   string   `json:"base_id,omitempty"`
	BaseName string   `json:"base_name,omitempty"`
	Online   int      `json:"online"`
	Position Position `json:"position,omitempty"`
}

// MapSystem represents a system in the galaxy map (from get_map).
type MapSystem struct {
	SystemID    string   `json:"system_id"`
	Name        string   `json:"name"`
	Position    Position `json:"position"`
	Connections []string `json:"connections"`
	POICount    int      `json:"poi_count"`
	Online      int      `json:"online"`
	Visited     bool     `json:"visited"`
	VisitedAt   string   `json:"visited_at"`
	Empire      string   `json:"empire,omitempty"`
}

// MarketListing represents a single market listing.
type MarketListing struct {
	ItemID       string  `json:"item_id"`
	ItemType     string  `json:"item_type"`
	Quantity     float64 `json:"quantity"`
	PricePerUnit float64 `json:"price_per_unit"`
	PriceEach    float64 `json:"price_each"`
	TotalPrice   float64 `json:"total_price"`
	Total        float64 `json:"total"`
	Type         string  `json:"type"`
	ListedBy     string  `json:"listed_by,omitempty"`
	Seller       string  `json:"seller,omitempty"`
}

// ViewMarketItem represents an item in the aggregated market order book
// returned by the view_market command. Each item shows the best buy/sell
// prices and full order book.
type ViewMarketItem struct {
	ItemID     string        `json:"item_id"`
	ItemName   string        `json:"item_name"`
	BestBuy    float64       `json:"best_buy"`
	BestSell   float64       `json:"best_sell"`
	BuyOrders  []MarketOrder `json:"buy_orders"`
	SellOrders []MarketOrder `json:"sell_orders"`
}

// MarketOrder represents a single buy or sell order in the market order book.
type MarketOrder struct {
	PriceEach float64 `json:"price_each"`
	Quantity  float64 `json:"quantity"`
}

// Base represents a player-owned or NPC base.
type Base struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Type         string          `json:"type"`
	OwnerID      string          `json:"owner_id"`
	OwnerName    string          `json:"owner_name,omitempty"`
	FactionID    string          `json:"faction_id,omitempty"`
	Empire       string          `json:"empire,omitempty"`
	POIID        string          `json:"poi_id"`
	SystemID     string          `json:"system_id"`
	Services     map[string]bool `json:"services"`
	PublicAccess bool            `json:"public_access"`
	DefenseLevel int             `json:"defense_level"`
	HasDrones    bool            `json:"has_drones,omitempty"`
	Health       int             `json:"health,omitempty"`
	MaxHealth    int             `json:"max_health,omitempty"`
	Facilities   []string        `json:"facilities,omitempty"`
}

// ResourceDisplay represents a resource with display information.
type ResourceDisplay struct {
	ResourceID       string  `json:"resource_id"`
	Name             string  `json:"name"`
	Richness         float64 `json:"richness"`
	Remaining        float64 `json:"remaining"`
	RemainingDisplay string  `json:"remaining_display"`
}

// SkillDefinition holds static skill data.
type SkillDefinition struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Description    string             `json:"description,omitempty"`
	Category       string             `json:"category,omitempty"`
	MaxLevel       int                `json:"max_level"`
	XpPerLevel     []float64          `json:"xp_per_level"`
	BonusPerLevel  map[string]float64 `json:"bonus_per_level,omitempty"`
	RequiredSkills map[string]int     `json:"required_skills,omitempty"`
	TrainingSource string             `json:"training_source,omitempty"`
}

// CatalogItem represents an item from the catalog API.
type CatalogItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Rarity      string `json:"rarity,omitempty"`
	Size        int    `json:"size,omitempty"`
	BaseValue   int    `json:"base_value,omitempty"`
	Stackable   bool   `json:"stackable,omitempty"`
	Tradeable   bool   `json:"tradeable,omitempty"`
}

// PlayerSkill represents a player's progress in a specific skill.
type PlayerSkill struct {
	SkillID     string  `json:"skill_id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Level       int     `json:"level"`
	MaxLevel    int     `json:"max_level"`
	CurrentXP   float64 `json:"current_xp"`
	NextLevelXP float64 `json:"next_level_xp"`
}

// ShipClass represents a ship class definition with stats, price, and requirements.
type ShipClass struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Class              string         `json:"class,omitempty"`
	Category           string         `json:"category,omitempty"`
	Description        string         `json:"description,omitempty"`
	Lore               string         `json:"lore,omitempty"`
	Faction            string         `json:"faction,omitempty"`
	Tier               int            `json:"tier,omitempty"`
	Scale              int            `json:"scale,omitempty"`
	Price              int            `json:"price"`
	BaseHull           int            `json:"base_hull"`
	BaseShield         int            `json:"base_shield,omitempty"`
	BaseShieldRecharge int            `json:"base_shield_recharge,omitempty"`
	BaseArmor          int            `json:"base_armor,omitempty"`
	BaseSpeed          int            `json:"base_speed"`
	BaseFuel           int            `json:"base_fuel"`
	CargoCapacity      int            `json:"cargo_capacity"`
	CPUCapacity        int            `json:"cpu_capacity"`
	PowerCapacity      int            `json:"power_capacity"`
	WeaponSlots        int            `json:"weapon_slots"`
	DefenseSlots       int            `json:"defense_slots"`
	UtilitySlots       int            `json:"utility_slots"`
	BuildTime          int            `json:"build_time,omitempty"`
	ShipyardTier       int            `json:"shipyard_tier,omitempty"`
	StarterShip        bool           `json:"starter_ship,omitempty"`
	RequiredSkills     map[string]int `json:"required_skills,omitempty"`
	DefaultModules     []string       `json:"default_modules,omitempty"`
	BuildMaterials     []RecipeItem   `json:"build_materials,omitempty"`
	FlavorTags         []string       `json:"flavor_tags,omitempty"`
	TowSpeedBonus      int            `json:"tow_speed_bonus,omitempty"`
}

// Recipe represents a crafting recipe.
type Recipe struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	Category        string         `json:"category"`
	RequiredSkills  map[string]int `json:"required_skills,omitempty"`
	Inputs          []RecipeItem   `json:"inputs"`
	Outputs         []RecipeItem   `json:"outputs"`
	CraftingTime    int            `json:"crafting_time"`
	BaseQuality     int            `json:"base_quality,omitempty"`
	SkillQualityMod int            `json:"skill_quality_mod,omitempty"`
}

// RecipeItem represents an item requirement or output in a recipe.
type RecipeItem struct {
	ItemID     string `json:"item_id"`
	Quantity   int    `json:"quantity"`
	QualityMod bool   `json:"quality_mod,omitempty"`
}

// Wreck represents a destroyed ship's wreckage.
type Wreck struct {
	ID        string      `json:"id"`
	ShipClass string      `json:"ship_class"`
	OwnerID   string      `json:"owner_id,omitempty"`
	OwnerName string      `json:"owner_name,omitempty"`
	POIID     string      `json:"poi_id"`
	Contents  []CargoItem `json:"contents"`
	ExpiresAt string      `json:"expires_at"`
}

// Drone represents a deployed drone.
type Drone struct {
	ID        string `json:"id"`
	DroneType string `json:"drone_type"`
	ItemID    string `json:"item_id"`
	Status    string `json:"status"`
	Hull      int    `json:"hull"`
	MaxHull   int    `json:"max_hull"`
	Damage    int    `json:"damage,omitempty"`
	TargetID  string `json:"target_id,omitempty"`
	POIID     string `json:"poi_id"`
}

// Note represents a tradeable text document.
type Note struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Content    string `json:"content,omitempty"`
	AuthorID   string `json:"author_id"`
	AuthorName string `json:"author_name,omitempty"`
	CreatedAt  string `json:"created_at"`
	Value      int    `json:"value"`
}

// Storage represents items and credits stored at a station.
type Storage struct {
	BaseID   string           `json:"base_id"`
	BaseName string           `json:"base_name"`
	Items    []CargoItem      `json:"items"`
	Credits  int              `json:"credits"`
	Ships    []map[string]any `json:"ships,omitempty"`
	Gifts    []map[string]any `json:"gifts,omitempty"`
}

// Faction represents faction information.
type Faction struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Tag            string          `json:"tag"`
	LeaderID       string          `json:"leader_id"`
	LeaderName     string          `json:"leader_name,omitempty"`
	MemberCount    int             `json:"member_count"`
	Treasury       int             `json:"treasury,omitempty"`
	Members        []FactionMember `json:"members,omitempty"`
	Allies         []string        `json:"allies,omitempty"`
	Enemies        []string        `json:"enemies,omitempty"`
	AtWar          []FactionWar    `json:"at_war,omitempty"`
	PeaceProposals []PeaceProposal `json:"peace_proposals,omitempty"`
}

// FactionMember represents a member of a faction.
type FactionMember struct {
	PlayerID string `json:"player_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

// FactionWar represents an active war between factions.
type FactionWar struct {
	FactionID   string `json:"faction_id"`
	FactionName string `json:"faction_name"`
	StartedAt   string `json:"started_at"`
	OurKills    int    `json:"our_kills"`
	TheirKills  int    `json:"their_kills"`
}

// PeaceProposal represents a peace proposal between factions.
type PeaceProposal struct {
	FromFactionID   string `json:"from_faction_id"`
	FromFactionName string `json:"from_faction_name"`
	Terms           string `json:"terms,omitempty"`
	ProposedAt      string `json:"proposed_at"`
}

// ExchangeOrder represents a buy or sell order on the station exchange.
type ExchangeOrder struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	ItemID         string `json:"item_id"`
	Quantity       int    `json:"quantity"`
	QuantityFilled int    `json:"quantity_filled,omitempty"`
	PriceEach      int    `json:"price_each"`
	SellerID       string `json:"seller_id,omitempty"`
	BuyerID        string `json:"buyer_id,omitempty"`
	BaseID         string `json:"base_id"`
	CreatedAt      string `json:"created_at"`
}

// ChatMessage represents a chat message.
type ChatMessage struct {
	ID           string `json:"id"`
	Channel      string `json:"channel"`
	SenderID     string `json:"sender_id"`
	Sender       string `json:"sender"`
	Content      string `json:"content"`
	TargetID     string `json:"target_id,omitempty"`
	TargetName   string `json:"target_name,omitempty"`
	TimestampUTC string `json:"timestamp_utc"`
	Timestamp    string `json:"timestamp,omitempty"`
}

// CaptainsLogEntry represents an entry in the captain's log.
type CaptainsLogEntry struct {
	Index     int    `json:"index"`
	Entry     string `json:"entry"`
	CreatedAt string `json:"created_at"`
}

// Mission represents a mission from a mission board.
type Mission struct {
	ID          string             `json:"id"`
	Type        string             `json:"type"`
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Giver       string             `json:"giver"`
	GiverBaseID string             `json:"giver_base_id"`
	Rewards     MissionRewards     `json:"rewards"`
	Objectives  []MissionObjective `json:"objectives"`
	Difficulty  int                `json:"difficulty"`
	ExpiresAt   string             `json:"expires_at,omitempty"`
	AcceptedAt  string             `json:"accepted_at,omitempty"`
	Progress    map[string]int     `json:"progress,omitempty"`
}

// MissionRewards represents mission completion rewards.
type MissionRewards struct {
	Credits int            `json:"credits"`
	Items   []RecipeItem   `json:"items,omitempty"`
	SkillXP map[string]int `json:"skill_xp,omitempty"`
}

// MissionObjective represents a single objective in a mission.
type MissionObjective struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	ItemID      string `json:"item_id,omitempty"`
	Quantity    int    `json:"quantity,omitempty"`
	TargetID    string `json:"target_id,omitempty"`
	Completed   bool   `json:"completed,omitempty"`
}

// RouteStep represents a step in a route from find_route.
type RouteStep struct {
	SystemID string `json:"system_id"`
	Name     string `json:"name"`
	Jumps    int    `json:"jumps"`
}

// SystemSearchResult represents a system from search_systems.
type SystemSearchResult struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Position    Position `json:"position"`
	Connections []string `json:"connections"`
}

// TravelProgress represents travel state when in transit.
type TravelProgress struct {
	Progress    float64 `json:"travel_progress"`
	Destination string  `json:"travel_destination"`
	Type        string  `json:"travel_type"`
	ArrivalTick int64   `json:"travel_arrival_tick"`
}
