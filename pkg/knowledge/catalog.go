package knowledge

// CatalogItem represents an item from the game catalog.
type CatalogItem struct {
	ID          string
	Name        string
	Description string
	Category    string
	Rarity      string
	Size        int
	BaseValue   int
	Stackable   bool
	Tradeable   bool

	// Module detail (non-nil when item is a ship module)
	Module *ItemModule
	// Consumable effect detail (non-nil when item has a non-ammo effect)
	ConsumableEffect *ItemConsumableEffect
	// Ammo detail (non-nil when item is ammunition)
	Ammo *ItemAmmo
}

// ItemModule holds common fields for all fitted ship modules.
type ItemModule struct {
	Type      string // "weapon", "defense", "mining", "utility"
	TypeID    string // server-assigned module identifier
	CPUUsage  int
	PowerUsage int
	Hidden    bool
	Special   string // comma-separated special ability tags

	// Subtype detail (exactly one is non-nil)
	Weapon  *ItemWeapon
	Defense *ItemDefense
	Mining  *ItemMining
	Utility *ItemUtility
}

// ItemWeapon holds weapon-specific module attributes.
type ItemWeapon struct {
	Damage      int
	DamageType  string // "kinetic", "energy", "em", "explosive", "void", "thermal"
	Range       *int   // nullable
	Reach       *int   // nullable
	Cooldown    int
	AmmoType    string // empty for energy weapons
	MagazineSize *int  // nil when AmmoType is empty
}

// ItemDefense holds defense-module-specific attributes.
type ItemDefense struct {
	ArmorBonus         *int
	HullBonus          *int
	ShieldBonus        *int
	ShieldRechargeBonus *int
	ArmorRepairRate    *int
	ResistanceBonus    map[string]float64 // e.g. {"em": 30, "kinetic": 25}
	DamageReduction    *float64
	CloakStrength      *int
	Cooldown           *int
	Damage             *int
	DamageType         string
	Range              *int
}

// ItemMining holds mining-module-specific attributes.
type ItemMining struct {
	MiningPower int
	MiningRange int
}

// ItemUtility holds utility-module-specific attributes.
type ItemUtility struct {
	SpeedBonus      *int
	CargoBonus      *int
	CloakStrength   *int
	ScannerPower    *int
	AccuracyBonus   *int
	TrackingBonus   *int
	SignatureBonus  *int
	FuelEfficiency  *float64
	DroneBandwidth  *int
	DroneCapacity   *int
	HarvestPower    *int
	HarvestRange    *int
	SurveyPower     *int
	SurveyRange     *int
	TowSpeedPenalty *int
	Cooldown        *int
}

// ItemConsumableEffect holds effect data for consumable items (non-ammo).
type ItemConsumableEffect struct {
	EffectType string // "buff", "repair", "fuel", "shield", "power", "probe", "countermeasure", "beacon", "emergency_jump"
	Subtype    string
	Amount     *int
	Duration   *int
	Stat       string
}

// ItemAmmo holds ammunition type data.
type ItemAmmo struct {
	AmmoType string // "autocannon", "missile", "railgun", "plasma", "em_charge", "torpedo", "mine", "void_core"
}

// ShipClassDef represents a ship class definition stored in the knowledge base.
type ShipClassDef struct {
	ID                 string
	Name               string
	Class              string
	Category           string
	Description        string
	Lore               string
	Faction            string
	Tier               int
	Scale              int
	Price              int
	BaseHull           int
	BaseShield         int
	BaseShieldRecharge int
	BaseArmor          int
	BaseSpeed          int
	BaseFuel           int
	CargoCapacity      int
	CPUCapacity        int
	PowerCapacity      int
	WeaponSlots        int
	DefenseSlots       int
	UtilitySlots       int
	BuildTime          int
	ShipyardTier       int
	StarterShip        bool
	TowSpeedBonus      int
	RequiredSkills     map[string]int
	DefaultModules     []string
	FlavorTags         []string
	BuildMaterials     []BuildMaterial
	LastUpdatedTick    int64
}

// BuildMaterial represents a material required to build a ship class.
type BuildMaterial struct {
	ItemID   string
	Quantity int
}

// RecipeDef represents a crafting recipe stored in the knowledge base.
type RecipeDef struct {
	ID              string
	Name            string
	Description     string
	Category        string
	CraftingTime    int
	BaseQuality     int
	SkillQualityMod int
	RequiredSkills  map[string]int
	Inputs          []RecipeIngredient
	Outputs         []RecipeProduct
	LastUpdatedTick int64
}

// RecipeIngredient represents an input item for a recipe.
type RecipeIngredient struct {
	ItemID   string
	Quantity int
}

// RecipeProduct represents an output item from a recipe.
type RecipeProduct struct {
	ItemID     string
	Quantity   int
	QualityMod bool
}

// PlayerRecord represents a player's core data stored in the knowledge base.
type PlayerRecord struct {
	ID            string
	Username      string
	Empire        string
	Credits       float64
	CurrentSystem string
	CurrentPOI    string
	CurrentShipID string
	HomeBase      string
	DockedAtBase  string
	FactionID     string
	FactionRank   string
	Experience    int64
	Stats         PlayerStatsRecord
	LastUpdatedTick int64
}

// PlayerStatsRecord holds player statistics.
type PlayerStatsRecord struct {
	ShipsDestroyed    int
	TimesDestroyed    int
	OreMined          float64
	CreditsEarned     float64
	CreditsSpent      float64
	TradesCompleted   int
	SystemsDiscovered int
	ItemsCrafted      int
	MissionsCompleted int
	BasesDestroyed    int
	DistanceTraveled  int64
	PiratesDestroyed  int
	ShipsLost         int
	TimePlayed        int64
	LastUpdatedTick   int64
}

// PlayerSkillRecord represents a player's progress in a specific skill.
type PlayerSkillRecord struct {
	SkillID   string
	Level     int
	CurrentXP float64
}

// ShipRecord represents a player-owned ship stored in the knowledge base.
type ShipRecord struct {
	ID              string
	OwnerID         string
	ClassID         string
	Name            string
	Hull            float64
	MaxHull         float64
	Shield          float64
	MaxShield       float64
	ShieldRecharge  float64
	Armor           float64
	Speed           float64
	Fuel            float64
	MaxFuel         float64
	CargoUsed       float64
	CargoCapacity   float64
	CPUUsed         float64
	CPUCapacity     float64
	PowerUsed       float64
	PowerCapacity   float64
	WeaponSlots     int
	DefenseSlots    int
	UtilitySlots    int
	DockedAtBase    string
	Cargo           []CargoEntry
	Modules         []ShipModuleRecord
	LastUpdatedTick int64
}

// CargoEntry represents an item in a ship's cargo hold.
type CargoEntry struct {
	ItemID   string
	Quantity float64
}

// ShipModuleRecord represents a module fitted on a ship.
type ShipModuleRecord struct {
	ID              string
	TypeID          string
	Name            string
	Type            string
	CPUUsage        int
	PowerUsage      int
	Quality         float64
	QualityGrade    string
	Wear            float64
	WearStatus      string
	LastUpdatedTick int64
}

// MissionTemplate represents an available mission from a mission board.
type MissionTemplate struct {
	ID              string
	Title           string
	Description     string
	Type            string
	Difficulty      int
	BaseID          string
	GiverName       string
	GiverTitle      string
	DialogOffer     string
	ChainNext       string
	ExpiresInTicks  int
	RewardsCredits  int
	RewardsSkillXP  map[string]int
	Requirements    map[string]any
	Objectives      []MissionObjectiveRecord
	LastUpdatedTick int64
}

// MissionObjectiveRecord represents a single objective in a mission template.
type MissionObjectiveRecord struct {
	Type        string
	Description string
	SortOrder   int
}
