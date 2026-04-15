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
	Name     string  `json:"name,omitempty"`
	Quantity float64 `json:"quantity"`
	Size     int     `json:"size,omitempty"`
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
	BasesDestroyed         int   `json:"bases_destroyed"`
	BattlesFled            int   `json:"battles_fled,omitempty"`
	BattlesStarted         int   `json:"battles_started,omitempty"`
	CaptainsLogEntries     int   `json:"captains_log_entries,omitempty"`
	ChatMessagesSent       int   `json:"chat_messages_sent,omitempty"`
	CloakActivations       int   `json:"cloak_activations,omitempty"`
	ConsumablesUsed        int   `json:"consumables_used,omitempty"`
	ContrabandSold         int   `json:"contraband_sold,omitempty"`
	CreditsEarned          int64 `json:"credits_earned"`
	CreditsGifted          int64 `json:"credits_gifted,omitempty"`
	CreditsSpent           int64 `json:"credits_spent"`
	CustomsEvaded          int   `json:"customs_evaded,omitempty"`
	DamageDealt            int64 `json:"damage_dealt,omitempty"`
	DamageTaken            int64 `json:"damage_taken,omitempty"`
	DeathsByPirate         int   `json:"deaths_by_pirate,omitempty"`
	DeathsByPlayer         int   `json:"deaths_by_player,omitempty"`
	DeathsBySelfDestruct   int   `json:"deaths_by_self_destruct,omitempty"`
	DeepCorePOIsDiscovered int   `json:"deep_core_pois_discovered,omitempty"`
	DistanceTraveled       int64 `json:"distance_traveled"`
	ExchangeCreditsEarned  int64 `json:"exchange_credits_earned,omitempty"`
	ExchangeItemsBought    int   `json:"exchange_items_bought,omitempty"`
	ExchangeItemsSold      int   `json:"exchange_items_sold,omitempty"`
	FacilitiesBuilt        int   `json:"facilities_built,omitempty"`
	FacilityItemsProduced  int   `json:"facility_items_produced,omitempty"`
	ForumPostsCreated      int   `json:"forum_posts_created,omitempty"`
	GiftsReceived          int   `json:"gifts_received,omitempty"`
	GiftsSent              int   `json:"gifts_sent,omitempty"`
	InsuranceClaimsMade    int   `json:"insurance_claims_made,omitempty"`
	InsurancePayoutsRecvd  int64 `json:"insurance_payouts_received,omitempty"`
	InsurancePoliciesBought int  `json:"insurance_policies_bought,omitempty"`
	ItemsCrafted           int   `json:"items_crafted"`
	ItemsJettisoned        int   `json:"items_jettisoned,omitempty"`
	JumpsCompleted         int   `json:"jumps_completed,omitempty"`
	MissionsAbandoned      int   `json:"missions_abandoned,omitempty"`
	MissionsAccepted       int   `json:"missions_accepted,omitempty"`
	MissionsCompleted      int   `json:"missions_completed"`
	ModulesInstalled       int   `json:"modules_installed,omitempty"`
	NPCsDestroyed          int   `json:"npcs_destroyed,omitempty"`
	OreMined               int64 `json:"ore_mined"`
	PiratesDestroyed       int   `json:"pirates_destroyed"`
	RefuelsGiven           int   `json:"refuels_given,omitempty"`
	RepairsGiven           int   `json:"repairs_given,omitempty"`
	ScansPerformed         int   `json:"scans_performed,omitempty"`
	SelfDestructs          int   `json:"self_destructs,omitempty"`
	ShipsCommissioned      int   `json:"ships_commissioned,omitempty"`
	ShipsDestroyed         int   `json:"ships_destroyed"`
	ShipsLost              int   `json:"ships_lost"`
	ShipsPurchased         int   `json:"ships_purchased,omitempty"`
	SystemsExplored        int   `json:"systems_explored,omitempty"`
	TimePlayed             int64 `json:"time_played"`
	TimesDocked            int   `json:"times_docked,omitempty"`
	TradesCompleted        int   `json:"trades_completed"`
	WormholesTraversed     int   `json:"wormholes_traversed,omitempty"`
	WreckItemsLooted       int   `json:"wreck_items_looted,omitempty"`
	WrecksScrapped         int   `json:"wrecks_scrapped,omitempty"`
	WrecksSold             int   `json:"wrecks_sold,omitempty"`
}

// ModuleDefinition represents a module's definition including stats and requirements.
type ModuleDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	TypeID      string `json:"type_id,omitempty"` // Module type ID
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
	ActiveBuffs              []ActiveBuff     `json:"active_buffs,omitempty"`
	DamagePenalty            float64          `json:"damage_penalty,omitempty"`
	SpeedPenalty             float64          `json:"speed_penalty,omitempty"`
	GasCargoEfficiency       int              `json:"gas_cargo_efficiency,omitempty"`
	IceCargoEfficiency       int              `json:"ice_cargo_efficiency,omitempty"`
	OreCargoEfficiency       int              `json:"ore_cargo_efficiency,omitempty"`
	CustomName               string           `json:"custom_name,omitempty"`
	LoadedOnCarrierID        string           `json:"loaded_on_carrier_id,omitempty"`
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
	Class       string        `json:"class,omitempty"` // MK classification (e.g., "G2 V") or planet class
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Position    Position      `json:"position"`
	Resources   []POIResource `json:"resources"`
	BaseID      string        `json:"base_id,omitempty"`
	HasBase     bool          `json:"has_base,omitempty"`
	BaseName    string        `json:"base_name,omitempty"`
	Online           int           `json:"online,omitempty"`
	Hidden           bool          `json:"hidden,omitempty"`
	RevealDifficulty int           `json:"reveal_difficulty,omitempty"`
	ExpiresAt        string        `json:"expires_at,omitempty"` // ISO-8601 timestamp when POI expires (e.g., wormholes)
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
	ItemID       string        `json:"item_id"`
	ItemName     string        `json:"item_name"`
	Category     string        `json:"category,omitempty"`
	BestBuy      float64       `json:"best_buy"`
	BestSell     float64       `json:"best_sell"`
	BuyPrice     int           `json:"buy_price,omitempty"`
	BuyQuantity  int           `json:"buy_quantity,omitempty"`
	SellPrice    int           `json:"sell_price,omitempty"`
	SellQuantity int           `json:"sell_quantity,omitempty"`
	Spread       int           `json:"spread,omitempty"`
	BuyOrders    []MarketOrder `json:"buy_orders"`
	SellOrders   []MarketOrder `json:"sell_orders"`
}

// MarketOrder represents a single buy or sell order in the market order book.
type MarketOrder struct {
	PriceEach  float64 `json:"price_each"`
	Quantity   float64 `json:"quantity"`
	MyQuantity int     `json:"my_quantity,omitempty"`
	Source     string  `json:"source,omitempty"`
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
	HasDrones          bool            `json:"has_drones,omitempty"`
	Health             int             `json:"health,omitempty"`
	MaxHealth          int             `json:"max_health,omitempty"`
	Facilities         []string        `json:"facilities,omitempty"`
	PirateRepRequired  int             `json:"pirate_rep_required,omitempty"`
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
	FlavorTags             []string       `json:"flavor_tags,omitempty"`
	TowSpeedBonus          int            `json:"tow_speed_bonus,omitempty"`
	Hidden                 bool           `json:"hidden,omitempty"`
	BasedOn                string         `json:"based_on,omitempty"`
	InherentCapabilities   []map[string]any `json:"inherent_capabilities,omitempty"`
	Legacy                 bool           `json:"legacy,omitempty"`
	PassiveRecipes         []string       `json:"passive_recipes,omitempty"`
	PilotingRequired       int            `json:"piloting_required,omitempty"`
	RequiredItems          []map[string]any `json:"required_items,omitempty"`
	Special                string         `json:"special,omitempty"`
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
	ID                string      `json:"id"`
	Type              string      `json:"type,omitempty"`
	ShipClass         string      `json:"ship_class"`
	POIID             string      `json:"poi_id"`
	SystemID          string      `json:"system_id,omitempty"`
	VictimID          string      `json:"victim_id,omitempty"`
	VictimName        string      `json:"victim_name,omitempty"`
	KillerID          string      `json:"killer_id,omitempty"`
	KillerName        string      `json:"killer_name,omitempty"`
	Cargo             []CargoItem `json:"cargo,omitempty"`
	Modules           []string    `json:"modules,omitempty"`
	SalvageValue      int         `json:"salvage_value,omitempty"`
	InsurancePolicyID string      `json:"insurance_policy_id,omitempty"`
	TowedByPlayerID   string      `json:"towed_by_player_id,omitempty"`
	CreatedAt         string      `json:"created_at,omitempty"`
	ExpiresAt         string      `json:"expires_at,omitempty"`
	ExpireTick        int64       `json:"expire_tick,omitempty"`
}

// Drone represents a deployed drone.
type Drone struct {
	DroneID   string `json:"drone_id"`
	DroneType string `json:"drone_type"`
	ItemID    string `json:"item_id,omitempty"`
	Status    string `json:"status"`
	Hull      int    `json:"hull,omitempty"`
	MaxHull   int    `json:"max_hull,omitempty"`
	Damage    int    `json:"damage,omitempty"`
	TargetID  string `json:"target_id,omitempty"`
	POIID     string `json:"poi_id,omitempty"`
}

// Note represents a tradeable text document.
type Note struct {
	NoteID     string `json:"note_id"`
	Title      string `json:"title"`
	Content    string `json:"content,omitempty"`
	AuthorID   string `json:"author_id,omitempty"`
	AuthorName string `json:"author_name,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	Value      int    `json:"value,omitempty"`
}

// Storage represents items and credits stored at a station.
type Storage struct {
	BaseID   string           `json:"base_id"`
	BaseName string           `json:"base_name"`
	Items    []CargoItem      `json:"items"`
	Credits  int              `json:"credits"`
	Ships    []StorageShip    `json:"ships,omitempty"`
	Gifts    []StorageGift    `json:"gifts,omitempty"`
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
	OrderID        string `json:"order_id"`
	OrderType      string `json:"order_type"`
	Side           string `json:"side,omitempty"`
	ItemID         string `json:"item_id"`
	ItemName       string `json:"item_name,omitempty"`
	Quantity       int    `json:"quantity"`
	Remaining      int    `json:"remaining,omitempty"`
	FilledQuantity int    `json:"filled_quantity,omitempty"`
	PriceEach      int    `json:"price_each"`
	ListingFee     int    `json:"listing_fee,omitempty"`
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by,omitempty"`
	FactionOrder   bool   `json:"faction_order,omitempty"`
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
	Credits    int            `json:"credits"`
	Items      map[string]int `json:"items,omitempty"`
	SkillXP    map[string]int `json:"skill_xp,omitempty"`
	PirateRep  int            `json:"pirate_rep,omitempty"`
	Reputation int            `json:"reputation,omitempty"`
}

// MissionObjective represents a single objective in a mission.
type MissionObjective struct {
	Type            string   `json:"type"`
	Description     string   `json:"description"`
	ItemID          string   `json:"item_id,omitempty"`
	Quantity        int      `json:"quantity,omitempty"`
	TargetID        string   `json:"target_id,omitempty"`
	TargetBaseID    string   `json:"target_base_id,omitempty"`
	TargetBaseName  string   `json:"target_base_name,omitempty"`
	SystemID        string   `json:"system_id,omitempty"`
	SystemName      string   `json:"system_name,omitempty"`
	EligiblePlayers []string `json:"eligible_players,omitempty"`
	Participants    []string `json:"participants,omitempty"`
	Completed       bool     `json:"completed,omitempty"`
}

// RouteStep represents a step in a route from find_route.
type RouteStep struct {
	SystemID string `json:"system_id"`
	Name     string `json:"name"`
	Jumps    int    `json:"jumps"`
}

// SystemSearchResult represents a system from search_systems.
type SystemSearchResult struct {
	SystemID    string   `json:"system_id"`
	Name        string   `json:"name"`
	Position    Position `json:"position,omitempty"`
	Connections []string `json:"connections,omitempty"`
}

// TravelProgress represents travel state when in transit.
type TravelProgress struct {
	Progress    float64 `json:"travel_progress"`
	Destination string  `json:"travel_destination"`
	Type        string  `json:"travel_type"`
	ArrivalTick int64   `json:"travel_arrival_tick"`
}

// ============================================================================
// Sub-types for strongly-typed response fields (replacing map[string]any)
// ============================================================================

// ActiveBuff represents a temporary buff effect on a ship.
type ActiveBuff struct {
	ItemID   string `json:"item_id,omitempty"`
	Stat     string `json:"stat,omitempty"`
	Amount   int    `json:"amount,omitempty"`
	TicksLeft int   `json:"ticks_left,omitempty"`
	ExpiresAt int   `json:"expires_at,omitempty"`
}

// BattleParticipant represents a player or NPC in a battle.
type BattleParticipant struct {
	PlayerID    string `json:"player_id"`
	Username    string `json:"username"`
	ShipClass   string `json:"ship_class"`
	SideID      string `json:"side_id"`
	Stance      string `json:"stance"`
	TargetID    string `json:"target_id,omitempty"`
	HullPct     int    `json:"hull_pct"`
	ShieldPct   int    `json:"shield_pct"`
	DamageDealt int    `json:"damage_dealt"`
	DamageTaken int    `json:"damage_taken"`
	KillCount   int    `json:"kill_count"`
	AutoPilot   bool   `json:"auto_pilot"`
	Zone        string `json:"zone,omitempty"`
}

// BattleSide represents a side in a battle.
type BattleSide struct {
	SideID      string `json:"side_id"`
	FactionID   string `json:"faction_id,omitempty"`
	FactionName string `json:"faction_name,omitempty"`
	FactionTag  string `json:"faction_tag,omitempty"`
	PlayerCount int    `json:"player_count"`
}

// MarketInsight represents a market analysis insight.
type MarketInsight struct {
	Category string `json:"category"`
	Item     string `json:"item"`
	ItemID   string `json:"item_id"`
	Message  string `json:"message"`
	Priority int    `json:"priority"`
}

// TradeDetail represents a pending trade offer (incoming or outgoing).
type TradeDetail struct {
	TradeID        string      `json:"trade_id"`
	OffererName    string      `json:"offerer_name,omitempty"`
	TargetName     string      `json:"target_name,omitempty"`
	OfferCredits   int         `json:"offer_credits"`
	RequestCredits int         `json:"request_credits"`
	OfferItems     []TradeItem `json:"offer_items,omitempty"`
	RequestItems   []TradeItem `json:"request_items,omitempty"`
	ExpiresAt      string      `json:"expires_at,omitempty"`
}

// TradeItem represents an item in a trade offer.
type TradeItem struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

// InsuranceQuote represents an insurance quote for a ship.
type InsuranceQuote struct {
	Coverage    int              `json:"coverage"`
	Premium     int              `json:"premium"`
	FittedValue int             `json:"fitted_value,omitempty"`
	RiskScore   float64          `json:"risk_score,omitempty"`
	Refused     bool             `json:"refused,omitempty"`
	ExpiresIn   int              `json:"expires_in,omitempty"`
	Factors     []InsuranceFactor `json:"factors,omitempty"`
}

// InsuranceFactor represents a factor in an insurance quote calculation.
type InsuranceFactor struct {
	Name       string  `json:"name"`
	Detail     string  `json:"detail"`
	Multiplier float64 `json:"multiplier"`
}

// InsurancePolicy represents an active insurance policy.
type InsurancePolicy struct {
	PolicyID            string            `json:"policy_id"`
	ShipClass           string            `json:"ship_class,omitempty"`
	BaseID              string            `json:"base_id,omitempty"`
	Coverage            int               `json:"coverage"`
	Premium             int               `json:"premium"`
	RiskScore           float64           `json:"risk_score,omitempty"`
	ExpiresAt           string            `json:"expires_at,omitempty"`
	SelfDestructExcluded bool            `json:"self_destruct_excluded,omitempty"`
	RiskFactors         []InsuranceFactor `json:"risk_factors,omitempty"`
}

// CommissionDetail represents an active ship commission.
type CommissionDetail struct {
	CommissionID        string         `json:"commission_id"`
	ShipClassID         string         `json:"ship_class_id"`
	ShipName            string         `json:"ship_name,omitempty"`
	Status              string         `json:"status"`
	BaseID              string         `json:"base_id,omitempty"`
	BaseName            string         `json:"base_name,omitempty"`
	CreditsPaid         int            `json:"credits_paid"`
	EarmarkedCredits    int            `json:"earmarked_credits,omitempty"`
	MaterialCostEstimate int           `json:"material_cost_estimate,omitempty"`
	MaterialsProvided   bool           `json:"materials_provided,omitempty"`
	RequiredMaterials   map[string]int `json:"required_materials,omitempty"`
	MaterialsGathered   map[string]int `json:"materials_gathered,omitempty"`
	BuildStartTick      int64          `json:"build_start_tick,omitempty"`
	BuildCompleteTick   int64          `json:"build_complete_tick,omitempty"`
	TicksRemaining      int            `json:"ticks_remaining,omitempty"`
	BuiltShipID         string         `json:"built_ship_id,omitempty"`
	CreatedAt           string         `json:"created_at,omitempty"`
}

// OwnedShip represents a player-owned ship in the list_ships response.
type OwnedShip struct {
	ShipID         string `json:"ship_id"`
	ClassID        string `json:"class_id"`
	ClassName      string `json:"class_name,omitempty"`
	IsActive       bool   `json:"is_active"`
	Hull           string `json:"hull,omitempty"`
	Fuel           string `json:"fuel,omitempty"`
	CargoUsed      int    `json:"cargo_used,omitempty"`
	Location       string `json:"location,omitempty"`
	LocationBaseID string `json:"location_base_id,omitempty"`
	Modules        int    `json:"modules,omitempty"`
}

// ShipListingDetail represents a ship listed for sale (browse_ships).
type ShipListingDetail struct {
	ListingID    string `json:"listing_id"`
	ShipID       string `json:"ship_id"`
	ShipName     string `json:"ship_name,omitempty"`
	ClassID      string `json:"class_id"`
	Category     string `json:"category,omitempty"`
	Tier         int    `json:"tier,omitempty"`
	Scale        int    `json:"scale,omitempty"`
	Hull         int    `json:"hull,omitempty"`
	MaxHull      int    `json:"max_hull,omitempty"`
	Shield       int    `json:"shield,omitempty"`
	Price        int    `json:"price"`
	Seller       string `json:"seller,omitempty"`
	ModulesCount int    `json:"modules_count,omitempty"`
	ListedAt     string `json:"listed_at,omitempty"`
}

// ShowroomShip represents a ship in a shipyard showroom.
type ShowroomShip struct {
	ShipID        string `json:"ship_id,omitempty"`
	ClassID       string `json:"class_id"`
	Name          string `json:"name"`
	Category      string `json:"category,omitempty"`
	Tier          int    `json:"tier,omitempty"`
	Scale         int    `json:"scale,omitempty"`
	Hull          int    `json:"hull,omitempty"`
	Shield        int    `json:"shield,omitempty"`
	Speed         int    `json:"speed,omitempty"`
	Cargo         int    `json:"cargo,omitempty"`
	ShowroomPrice int    `json:"showroom_price,omitempty"`
	LaborCost     int    `json:"labor_cost,omitempty"`
	MaterialCost  int    `json:"material_cost,omitempty"`
}

// RevealedPOI represents a POI revealed by a system survey.
type RevealedPOI struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description,omitempty"`
	Resources   []SurveyResource  `json:"resources,omitempty"`
}

// SurveyResource represents a resource found during a survey.
type SurveyResource struct {
	ResourceID       string  `json:"resource_id"`
	Name             string  `json:"name,omitempty"`
	Richness         float64 `json:"richness,omitempty"`
	Remaining        float64 `json:"remaining,omitempty"`
	MaxRemaining     float64 `json:"max_remaining,omitempty"`
	RemainingDisplay string  `json:"remaining_display,omitempty"`
	DepletionPercent float64 `json:"depletion_percent,omitempty"`
}

// FaintSignature represents an unresolved survey signature.
type FaintSignature struct {
	Type       string `json:"type"`
	Hint       string `json:"hint,omitempty"`
	Difficulty int    `json:"difficulty,omitempty"`
}

// StorageShip represents a ship stored at a station.
type StorageShip struct {
	ShipID    string `json:"ship_id"`
	ClassID   string `json:"class_id"`
	ClassName string `json:"class_name,omitempty"`
	CargoUsed int    `json:"cargo_used,omitempty"`
	Modules   int    `json:"modules,omitempty"`
}

// StorageGift represents a gift in station storage.
type StorageGift struct {
	SenderID  string        `json:"sender_id"`
	Sender    string        `json:"sender"`
	Message   string        `json:"message,omitempty"`
	Credits   int           `json:"credits,omitempty"`
	Items     []StorageGiftItem `json:"items,omitempty"`
	Ships     []StorageGiftShip `json:"ships,omitempty"`
	Timestamp string        `json:"timestamp,omitempty"`
}

// StorageGiftItem represents an item within a gift.
type StorageGiftItem struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name,omitempty"`
	Quantity int    `json:"quantity"`
}

// StorageGiftShip represents a ship within a gift.
type StorageGiftShip struct {
	ShipID    string `json:"ship_id"`
	ClassID   string `json:"class_id"`
	ClassName string `json:"class_name,omitempty"`
}

// GameCommand represents a command in the help/commands listing.
type GameCommand struct {
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	Format      string `json:"format,omitempty"`
	IsMutation  bool   `json:"is_mutation,omitempty"`
	RequiresAuth bool  `json:"requires_auth,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// ChangelogVersion represents a version entry in the changelog.
type ChangelogVersion struct {
	Version     string   `json:"version"`
	ReleaseDate string   `json:"release_date"`
	Notes       []string `json:"notes"`
}

// ForumThreadSummary represents a thread in the forum listing.
type ForumThreadSummary struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Author          string `json:"author"`
	AuthorID        string `json:"author_id,omitempty"`
	AuthorEmpire    string `json:"author_empire,omitempty"`
	AuthorFactionTag string `json:"author_faction_tag,omitempty"`
	IsDevTeam       bool   `json:"is_dev_team,omitempty"`
	Category        string `json:"category,omitempty"`
	Content         string `json:"content,omitempty"`
	ReplyCount      int    `json:"reply_count,omitempty"`
	Upvotes         int    `json:"upvotes,omitempty"`
	Pinned          bool   `json:"pinned,omitempty"`
	Locked          bool   `json:"locked,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

// ForumReplyDetail represents a reply in a forum thread.
type ForumReplyDetail struct {
	ID              string `json:"id"`
	ThreadID        string `json:"thread_id,omitempty"`
	Author          string `json:"author"`
	AuthorID        string `json:"author_id,omitempty"`
	AuthorEmpire    string `json:"author_empire,omitempty"`
	AuthorFactionTag string `json:"author_faction_tag,omitempty"`
	IsDevTeam       bool   `json:"is_dev_team,omitempty"`
	Content         string `json:"content"`
	Upvotes         int    `json:"upvotes,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
}

// PendingTradeInfo represents a summary of a pending trade in the login response.
type PendingTradeInfo struct {
	TradeID        string `json:"trade_id"`
	OffererName    string `json:"offerer_name,omitempty"`
	OfferCredits   int    `json:"offer_credits,omitempty"`
	RequestCredits int    `json:"request_credits,omitempty"`
}

// OrderFill represents a single fill in a buy/sell order.
type OrderFill struct {
	Counterparty string `json:"counterparty,omitempty"`
	Source       string `json:"source,omitempty"`
	Quantity     int    `json:"quantity"`
	PriceEach    int    `json:"price_each"`
	Subtotal     int    `json:"subtotal"`
}

// NearbyPirate represents a pirate NPC at the current POI.
type NearbyPirate struct {
	PirateID string `json:"pirate_id"`
	Name     string `json:"name"`
	Tier     string `json:"tier,omitempty"`
	IsBoss   bool   `json:"is_boss,omitempty"`
	Hull     int    `json:"hull"`
	MaxHull  int    `json:"max_hull"`
	Shield   int    `json:"shield,omitempty"`
	MaxShield int   `json:"max_shield,omitempty"`
	Status   string `json:"status,omitempty"`
}

// StationHealth represents the condition of a station.
type StationHealth struct {
	Condition          string `json:"condition"`
	ConditionText      string `json:"condition_text"`
	SatisfactionPct    int    `json:"satisfaction_pct"`
	SatisfiedCount     int    `json:"satisfied_count"`
	TotalServiceInfra  int    `json:"total_service_infra"`
}

// MissionGiver represents the NPC giving a mission.
type MissionGiver struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
}

// MissionBoardEntry represents a mission on a mission board.
// MissionDialog contains NPC dialog text for mission interactions.
type MissionDialog struct {
	Offer    string `json:"offer,omitempty"`
	Accept   string `json:"accept,omitempty"`
	Decline  string `json:"decline,omitempty"`
	Complete string `json:"complete,omitempty"`
}

type MissionBoardEntry struct {
	MissionID         string               `json:"mission_id"`
	TemplateID        string               `json:"template_id,omitempty"`
	Type              string               `json:"type"`
	Title             string               `json:"title"`
	Description       string               `json:"description,omitempty"`
	Difficulty        int                  `json:"difficulty,omitempty"`
	Giver             MissionGiver         `json:"giver,omitempty"`
	IssuingBase       string               `json:"issuing_base,omitempty"`
	IssuingBaseID     string               `json:"issuing_base_id,omitempty"`
	IssuingSystemID   string               `json:"issuing_system_id,omitempty"`
	IssuingSystemName string               `json:"issuing_system_name,omitempty"`
	FactionID         string               `json:"faction_id,omitempty"`
	FactionName       string               `json:"faction_name,omitempty"`
	Repeatable        bool                 `json:"repeatable,omitempty"`
	Community         bool                 `json:"community,omitempty"`
	CommunityPercent  float64              `json:"community_percent,omitempty"`
	CommunityProgress map[string]string    `json:"community_progress,omitempty"`
	ChainNext         string               `json:"chain_next,omitempty"`
	ExpiresInTicks    int                  `json:"expires_in_ticks,omitempty"`
	Dialog            *MissionDialog       `json:"dialog,omitempty"`
	ProvidedItems     map[string]int       `json:"provided_items,omitempty"`
	RequiredModules   []string             `json:"required_modules,omitempty"`
	Warnings          []string             `json:"warnings,omitempty"`
	Requirements      *MissionRequirements `json:"requirements,omitempty"`
	Rewards           *MissionRewards      `json:"rewards,omitempty"`
	Objectives        []MissionObjective   `json:"objectives,omitempty"`
}

// MissionRequirements represents requirements to complete a mission.
type MissionRequirements struct {
	DeliverItemID      string `json:"deliver_item_id,omitempty"`
	DeliverItemName    string `json:"deliver_item_name,omitempty"`
	DeliverQuantity    int    `json:"deliver_quantity,omitempty"`
	DeliverToBaseID    string `json:"deliver_to_base_id,omitempty"`
	DeliverToBaseName  string `json:"deliver_to_base_name,omitempty"`
	MineItemID         string `json:"mine_item_id,omitempty"`
	MineItemName       string `json:"mine_item_name,omitempty"`
	MineQuantity       int    `json:"mine_quantity,omitempty"`
	KillCount          int    `json:"kill_count,omitempty"`
	TargetPlayerID     string `json:"target_player_id,omitempty"`
	VisitSystemCount   int    `json:"visit_system_count,omitempty"`
}

// ActiveMission represents a mission the player has accepted.
type ActiveMission struct {
	MissionID      string              `json:"mission_id"`
	TemplateID     string              `json:"template_id,omitempty"`
	Type           string              `json:"type"`
	Title          string              `json:"title"`
	Description    string              `json:"description,omitempty"`
	Difficulty     int                 `json:"difficulty,omitempty"`
	Giver          MissionGiver        `json:"giver,omitempty"`
	IssuingBase    string              `json:"issuing_base,omitempty"`
	AcceptedAt     string              `json:"accepted_at,omitempty"`
	ExpiresInTicks int                 `json:"expires_in_ticks,omitempty"`
	Requirements   *MissionRequirements `json:"requirements,omitempty"`
	Rewards        *MissionRewards     `json:"rewards,omitempty"`
	Objectives     []ActiveMissionObjective `json:"objectives,omitempty"`
	Progress       *ActiveMissionProgress   `json:"progress,omitempty"`
}

// ActiveMissionObjective extends MissionObjective with progress tracking.
type ActiveMissionObjective struct {
	Type           string `json:"type"`
	Description    string `json:"description"`
	Completed      bool   `json:"completed,omitempty"`
	Current        int    `json:"current,omitempty"`
	Required       int    `json:"required,omitempty"`
	ItemID         string `json:"item_id,omitempty"`
	ItemName       string `json:"item_name,omitempty"`
	SystemID       string `json:"system_id,omitempty"`
	SystemName     string `json:"system_name,omitempty"`
	TargetBase     string `json:"target_base,omitempty"`
	TargetBaseName string `json:"target_base_name,omitempty"`
	InCargo        int    `json:"in_cargo,omitempty"`
	InStorage      int    `json:"in_storage,omitempty"`
}

// ActiveMissionProgress tracks overall mission progress.
type ActiveMissionProgress struct {
	PercentComplete int `json:"percent_complete,omitempty"`
	ItemsDelivered  int `json:"items_delivered,omitempty"`
	ItemsMined      int `json:"items_mined,omitempty"`
	KillsAchieved   int `json:"kills_achieved,omitempty"`
	SystemsVisited  int `json:"systems_visited,omitempty"`
}

// FactionSummary represents a faction in lists and references.
type FactionSummary struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Tag            string `json:"tag"`
	LeaderUsername string `json:"leader_username,omitempty"`
	MemberCount    int    `json:"member_count,omitempty"`
	OwnedBases     int    `json:"owned_bases,omitempty"`
	PrimaryColor   string `json:"primary_color,omitempty"`
	SecondaryColor string `json:"secondary_color,omitempty"`
}

// FactionMemberDetail represents a member in the faction info response.
type FactionMemberDetail struct {
	PlayerID string `json:"player_id"`
	Username string `json:"username"`
	Role     string `json:"role,omitempty"`
	JoinedAt string `json:"joined_at,omitempty"`
	LastSeen string `json:"last_seen,omitempty"`
	IsOnline bool   `json:"is_online,omitempty"`
}

// FactionWarDetail represents a war in the faction info response.
type FactionWarDetail struct {
	TargetFactionID   string `json:"target_faction_id"`
	TargetFactionName string `json:"target_faction_name"`
	TargetFactionTag  string `json:"target_faction_tag,omitempty"`
	DeclaredBy        string `json:"declared_by,omitempty"`
	Reason            string `json:"reason,omitempty"`
	StartedAt         string `json:"started_at,omitempty"`
	OurKills          int    `json:"our_kills,omitempty"`
	TheirKills        int    `json:"their_kills,omitempty"`
}

// ShipModule represents an installed module on a ship.
type ShipModule struct {
	ID                 string             `json:"id"`
	TypeID             string             `json:"type_id,omitempty"`
	Name               string             `json:"name"`
	Type               string             `json:"type"`
	Slot               string             `json:"slot,omitempty"`
	Size               int                `json:"size,omitempty"`
	Quality            float64            `json:"quality,omitempty"`
	QualityGrade       string             `json:"quality_grade,omitempty"`
	Wear               float64            `json:"wear,omitempty"`
	WearStatus         string             `json:"wear_status,omitempty"`
	CPUUsage           int                `json:"cpu_usage,omitempty"`
	PowerUsage         int                `json:"power_usage,omitempty"`
	Damage             int                `json:"damage,omitempty"`
	DamageType         string             `json:"damage_type,omitempty"`
	Range              int                `json:"range,omitempty"`
	Reach              int                `json:"reach,omitempty"`
	Cooldown           int                `json:"cooldown,omitempty"`
	MagazineSize       int                `json:"magazine_size,omitempty"`
	CurrentAmmo        int                `json:"current_ammo,omitempty"`
	AmmoType           string             `json:"ammo_type,omitempty"`
	LoadedAmmoID       string             `json:"loaded_ammo_id,omitempty"`
	LoadedAmmoName     string             `json:"loaded_ammo_name,omitempty"`
	AccuracyBonus      int                `json:"accuracy_bonus,omitempty"`
	ArmorBonus         int                `json:"armor_bonus,omitempty"`
	ArmorBypassBonus   float64            `json:"armor_bypass_bonus,omitempty"`
	ArmorRepairRate    int                `json:"armor_repair_rate,omitempty"`
	ShieldBonus        int                `json:"shield_bonus,omitempty"`
	ShieldBypassBonus  float64            `json:"shield_bypass_bonus,omitempty"`
	ShieldRechargeBonus int               `json:"shield_recharge_bonus,omitempty"`
	HullBonus          int                `json:"hull_bonus,omitempty"`
	HullPenalty        int                `json:"hull_penalty,omitempty"`
	SpeedBonus         int                `json:"speed_bonus,omitempty"`
	SpeedPenalty       int                `json:"speed_penalty,omitempty"`
	CargoBonus         int                `json:"cargo_bonus,omitempty"`
	CPUBonus           int                `json:"cpu_bonus,omitempty"`
	PowerBonus         int                `json:"power_bonus,omitempty"`
	MaxFuelBonus       int                `json:"max_fuel_bonus,omitempty"`
	DamageReduction    int                `json:"damage_reduction,omitempty"`
	MiningPower        int                `json:"mining_power,omitempty"`
	SalvageBonus       int                `json:"salvage_bonus,omitempty"`
	ScannerPower       int                `json:"scanner_power,omitempty"`
	SurveyPower        int                `json:"survey_power,omitempty"`
	SurveyRange        int                `json:"survey_range,omitempty"`
	SignatureBonus     int                `json:"signature_bonus,omitempty"`
	TrackingBonus      int                `json:"tracking_bonus,omitempty"`
	TowSpeedPenalty    int                `json:"tow_speed_penalty,omitempty"`
	CloakStrength      int                `json:"cloak_strength,omitempty"`
	FuelEfficiency     int                `json:"fuel_efficiency,omitempty"`
	DroneBandwidth     int                `json:"drone_bandwidth,omitempty"`
	DroneCapacity      int                `json:"drone_capacity,omitempty"`
	ResistanceBonus    map[string]int     `json:"resistance_bonus,omitempty"`
	Special            string             `json:"special,omitempty"`
}

// AutoListedOrder represents an automatically created sell order from a buy.
type AutoListedOrder struct {
	OrderID    string `json:"order_id,omitempty"`
	Quantity   int    `json:"quantity,omitempty"`
	PriceEach  int    `json:"price_each,omitempty"`
	ListingFee int    `json:"listing_fee,omitempty"`
	Escrow     int    `json:"escrow,omitempty"`
}

// CombatLogEntry represents an entry in a combat log.
type CombatLogEntry struct {
	Tick       int64   `json:"tick,omitempty"`
	Attacker   string  `json:"attacker,omitempty"`
	Target     string  `json:"target,omitempty"`
	Damage     float64 `json:"damage,omitempty"`
	DamageType string  `json:"damage_type,omitempty"`
	Destroyed  bool    `json:"destroyed,omitempty"`
}

// ReleaseInfo represents version release info sent at login.
type ReleaseInfo struct {
	Version     string   `json:"version"`
	ReleaseDate string   `json:"release_date"`
	Notes       []string `json:"notes,omitempty"`
}

// UnreadChat represents unread message counts by channel.
type UnreadChat struct {
	Local   int `json:"local,omitempty"`
	System  int `json:"system,omitempty"`
	Faction int `json:"faction,omitempty"`
	Private int `json:"private,omitempty"`
}
