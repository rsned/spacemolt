package knowledge

import (
	"context"
	"time"
)

// Base provides the KB interface for both SQLite and in-memory implementations
type Base interface {
	Close() error
	RememberSystem(ctx context.Context, sys System) error
	GetSystem(ctx context.Context, systemID string) (*System, error)
	GetUnknownConnections(ctx context.Context, systemID string) ([]string, error)
	RememberConnection(ctx context.Context, fromSystem, toSystem string) error
	RememberPOI(ctx context.Context, poi POI) error
	GetPOIs(ctx context.Context, systemID string) ([]POI, error)
	RememberBase(ctx context.Context, base SpaceBase) error
	GetBase(ctx context.Context, baseID string) (*SpaceBase, error)
	GetBaseByPOI(ctx context.Context, poiID string) (*SpaceBase, error)
	AddExperience(ctx context.Context, agentID, expType, description, outcome, location string) error
	GetRecentExperiences(ctx context.Context, agentID string, limit int) ([]Experience, error)
	RegisterAgent(ctx context.Context, agentID, name, role, faction string, personality []byte) error

	// GetSystems returns all known systems
	// Note: This method does not take a context for API compatibility with existing callers
	GetSystems() []System

	// Market data methods
	StoreMarketSnapshot(ctx context.Context, snapshot MarketSnapshot, agentID string) error
	GetMarketSnapshots(ctx context.Context, systemID, stationID string, limit int) ([]MarketSnapshot, error)
	GetLatestMarketSnapshot(ctx context.Context, systemID, stationID string) (*MarketSnapshot, error)
	GetMarketItems(ctx context.Context, itemType string) ([]string, error)
	HasMarketSnapshotToday(ctx context.Context, systemID, stationID string) (bool, error)

	// Market analysis methods
	StoreMarketAnalysis(ctx context.Context, analysis MarketAnalysis, agentID string) error
	GetLatestMarketAnalysis(ctx context.Context, systemID, stationID string) (*MarketAnalysis, error)
	GetMarketAnalysisHistory(ctx context.Context, systemID, stationID string, limit int) ([]MarketAnalysis, error)

	// Ship listings methods
	StoreShipListings(ctx context.Context, listings ShipListings, agentID string) error
	GetShipListings(ctx context.Context, systemID, stationID string, limit int) ([]ShipListings, error)
	GetLatestShipListings(ctx context.Context, systemID, stationID string) (*ShipListings, error)
	HasShipListingsToday(ctx context.Context, systemID, stationID string) (bool, error)

	// Enhanced analytics methods

	// Resource depletion tracking
	RecordResourceState(ctx context.Context, poiID, resourceID string, richness, remaining float64, gameTick int64, agentID string) error
	GetResourceHistory(ctx context.Context, poiID, resourceID string, limit int) ([]ResourceHistory, error)
	GetDepletingResources(ctx context.Context, threshold float64) ([]DepletingResource, error)

	// Route optimization
	RecordJourney(ctx context.Context, fromSystem, toSystem string, fuelCost, travelTime float64, agentID string) error
	GetOptimalRoute(ctx context.Context, fromSystem, toSystem string) (*ConnectionMetrics, error)
	FindCheapestRoute(ctx context.Context, fromSystem, toSystem string, maxHops int) ([]string, float64, error)

	// Anomaly detection
	RecordAnomaly(ctx context.Context, anomaly Anomaly) error
	GetActiveAnomalies(ctx context.Context, systemID string) ([]Anomaly, error)
	GetAnomaliesByType(ctx context.Context, anomalyType, severity string, limit int) ([]Anomaly, error)
	ResolveAnomaly(ctx context.Context, anomalyID int64, status string) error

	// Market price analytics
	AnalyzePriceTrends(ctx context.Context, itemID, stationID string, windowHours int) (*PriceTrend, error)
	FindBestPrices(ctx context.Context, itemID string, listingType string, limit int) ([]BestPrice, error)
	GetPriceHistory(ctx context.Context, itemID, stationID string, limit int) ([]PricePoint, error)

	// Danger zone tracking
	RecordHostileEncounter(ctx context.Context, systemID string, encounterType string, details string) error
	GetDangerZones(ctx context.Context, minDangerLevel int) ([]DangerZone, error)
	GetSystemDanger(ctx context.Context, systemID string) (*DangerZone, error)

	// Knowledge export/import
	ExportKnowledge(ctx context.Context, description string, agentID string) (*KnowledgeExport, error)
	ImportKnowledge(ctx context.Context, exportData string) error
	ListExports(ctx context.Context) ([]KnowledgeExportMeta, error)

	// Skills
	GetSkill(id string) (*Skill, error)
	GetSkills() []Skill
	StoreSkills(ctx context.Context, skills []Skill) error

	// Catalog: Items
	StoreItems(ctx context.Context, items []CatalogItem) error
	GetItem(ctx context.Context, itemID string) (*CatalogItem, error)
	GetItems(ctx context.Context) ([]CatalogItem, error)
	GetItemsByCategory(ctx context.Context, category string) ([]CatalogItem, error)

	// Catalog: Ship Classes
	StoreShipClasses(ctx context.Context, classes []ShipClassDef) error
	GetShipClass(ctx context.Context, classID string) (*ShipClassDef, error)
	GetShipClasses(ctx context.Context) ([]ShipClassDef, error)
	GetShipClassesByCategory(ctx context.Context, category string) ([]ShipClassDef, error)

	// Catalog: Recipes
	StoreRecipes(ctx context.Context, recipes []RecipeDef) error
	GetRecipe(ctx context.Context, recipeID string) (*RecipeDef, error)
	GetRecipes(ctx context.Context) ([]RecipeDef, error)
	GetRecipesByCategory(ctx context.Context, category string) ([]RecipeDef, error)

	// Player State
	StorePlayer(ctx context.Context, player PlayerRecord) error
	GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error)
	StorePlayerSkills(ctx context.Context, playerID string, skills []PlayerSkillRecord) error
	GetPlayerSkills(ctx context.Context, playerID string) ([]PlayerSkillRecord, error)
	StoreShip(ctx context.Context, ship ShipRecord) error
	GetShip(ctx context.Context, shipID string) (*ShipRecord, error)
	GetPlayerShips(ctx context.Context, playerID string) ([]ShipRecord, error)
	StoreMissionTemplates(ctx context.Context, baseID string, missions []MissionTemplate) error
	GetMissionTemplates(ctx context.Context, baseID string) ([]MissionTemplate, error)

	// Agent wallet
	UpdateAgentWalletCredits(ctx context.Context, agentID string, credits int) error

	// Storage snapshots
	StoreStorageSnapshot(ctx context.Context, snapshot StorageSnapshot) error
	GetStorageSnapshot(ctx context.Context, agentID, baseID string) (*StorageSnapshot, error)
	GetAllStorageSnapshots(ctx context.Context) ([]StorageSnapshot, error)

	// Change detection snapshots
	RecordChangeSnapshot(ctx context.Context, snapshot ChangeSnapshot) error
	GetChangeSnapshots(ctx context.Context, systemID string, limit int) ([]ChangeSnapshot, error)

	// XP tracking
	RecordXPObservation(ctx context.Context, obs XPObservation) error
	GetXPObservations(ctx context.Context, action string, limit int) ([]XPObservation, error)
	GetXPSummary(ctx context.Context) ([]XPSummaryRow, error)
}

// MarketListing represents a single market listing
type MarketListing struct {
	ItemID       string
	ItemType     string
	Quantity     float64
	PricePerUnit float64
	TotalPrice   float64
	Type         string // 'buy' or 'sell'
	ListedBy     string
}

// MarketSnapshot represents a captured market state
type MarketSnapshot struct {
	SystemID    string
	SystemName  string
	StationID   string
	StationName string
	GameTick    int64
	Listings    []MarketListing
	CapturedAt  time.Time
}

// MarketAnalysis represents AI-generated market insights from analyze_market
type MarketAnalysis struct {
	SystemID        string
	SystemName      string
	StationID       string
	StationName     string
	GameTick        int64
	CapturedAt      time.Time
	Mode            string
	SkillLevel      int
	ScanningRange   string
	StationsInRange int
	ItemsScanned    int
	TopInsights     []map[string]any
	TotalItems      int
	TotalPages      int
	Page            int
	Hint            string
	XPGained        map[string]any
	AnalysisData    map[string]any
}

// ShipListing represents a single ship listing at a station
type ShipListing struct {
	ShipClass    string
	ShipName     string
	BasePrice    float64
	Description  string
	CargoSpace   int
	ModuleSlots  int
	UtilitySlots int
	WeaponSlots  int
}

// ShipListings represents captured ship listings at a station
type ShipListings struct {
	SystemID    string
	SystemName  string
	StationID   string
	StationName string
	GameTick    int64
	Listings    []ShipListing
	CapturedAt  time.Time
}

// ResourceHistory tracks resource depletion over time
type ResourceHistory struct {
	POIID      string
	ResourceID string
	Richness   float64
	Remaining  float64
	GameTick   int64
	RecordedAt time.Time
	AgentID    string
}

// DepletingResource identifies resources running low
type DepletingResource struct {
	POIID         string
	POIName       string
	SystemID      string
	SystemName    string
	ResourceID    string
	CurrentRem    float64
	PreviousRem   float64
	DepletionRate float64 // per game tick
	EstimatedLife int64   // ticks until exhausted
}

// ConnectionMetrics stores route optimization data
type ConnectionMetrics struct {
	FromSystem    string
	ToSystem      string
	TravelCount   int
	AvgFuelCost   float64
	AvgTravelTime float64
	LastTraveled  time.Time
	TraveledBy    string
}

// Anomaly represents an unusual or noteworthy discovery
type Anomaly struct {
	ID              int64
	Type            string // 'rich_deposit', 'hostile_zone', 'rare_resource', 'empty_station', 'price_anomaly'
	Severity        string // 'info', 'warning', 'critical', 'opportunity'
	SystemID        string
	POIID           string
	Description     string
	Details         string // JSON with additional data
	DetectedAt      time.Time
	DetectedBy      string
	Status          string // 'active', 'resolved', 'obsolete'
	LastUpdatedTick int64
}

// PriceTrend represents aggregate price data over a time window
type PriceTrend struct {
	ItemID       string
	ItemType     string
	StationID    string
	SystemID     string
	ListingType  string // 'buy' or 'sell'
	AvgPrice     float64
	MinPrice     float64
	MaxPrice     float64
	CurrentPrice float64
	PriceChange  float64 // percentage change
	SampleCount  int
	WindowStart  time.Time
	WindowEnd    time.Time
}

// BestPrice identifies the best buy/sell opportunity for an item
type BestPrice struct {
	ItemID      string
	StationID   string
	StationName string
	SystemID    string
	SystemName  string
	Price       float64
	Quantity    float64
	ListingType string
	CapturedAt  time.Time
}

// PricePoint represents a single price observation
type PricePoint struct {
	Price      float64
	Quantity   float64
	GameTick   int64
	CapturedAt time.Time
}

// DangerZone tracks hostile activity in a system
type DangerZone struct {
	SystemID          string
	SystemName        string
	DangerLevel       int // 0-10 scale
	HostileEncounters int
	PlayerKills       int
	NPCKills          int
	LastIncident      time.Time
	LastUpdated       time.Time
}

// KnowledgeExport represents exported knowledge bundle
type KnowledgeExport struct {
	ID               string
	CreatedAt        time.Time
	CreatedBy        string
	Description      string
	SystemsCount     int
	POIsCount        int
	ExperiencesCount int
	ExportData       string // JSON snapshot
}

// StorageSnapshot represents a captured station storage state for an agent
type StorageSnapshot struct {
	AgentID    string
	BaseID     string
	Credits    int
	Items      []StorageSnapshotItem
	Ships      []StorageSnapshotShip
	CapturedAt time.Time
}

// StorageSnapshotItem represents an item in a storage snapshot
type StorageSnapshotItem struct {
	ItemID   string
	Name     string
	Quantity float64
	Size     int
}

// StorageSnapshotShip represents a ship in a storage snapshot
type StorageSnapshotShip struct {
	ShipID    string
	ClassID   string
	ClassName string
	CargoUsed int
}

// KnowledgeExportMeta is metadata about an export
type KnowledgeExportMeta struct {
	ID               string
	CreatedAt        time.Time
	CreatedBy        string
	Description      string
	SystemsCount     int
	POIsCount        int
	ExperiencesCount int
}

// XPObservation records a skill XP change from a single command execution.
type XPObservation struct {
	ID          int64
	AgentID     string
	Action      string
	Target      string
	Source      string // "action", "mission_reward"
	SkillID     string
	XPDelta     float64
	LevelDelta  int
	LevelBefore int
	LevelAfter  int
	GameTick    int64
	CreatedAt   time.Time
	MissionID   string // empty for non-mission sources
}

// XPSummaryRow aggregates XP observations by action and skill.
type XPSummaryRow struct {
	Action     string
	SkillID    string
	Source     string
	AvgXPDelta float64
	Count      int
}

// ChangeSnapshot records old data when a system, POI, or base change is detected.
type ChangeSnapshot struct {
	ID              int64
	EntityType      string // "system", "poi", "base"
	EntityID        string
	SystemID        string
	ChangeSummary   string // human-readable summary of what changed
	OldData         string // JSON snapshot of old values
	DetectedBy      string // agent ID
	DetectedAtTick  int64
	DetectedAt      time.Time
}
