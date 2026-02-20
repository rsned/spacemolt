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
