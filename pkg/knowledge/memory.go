package knowledge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// MemoryKB is an in-memory knowledge base for MVP
type MemoryKB struct {
	mu              sync.RWMutex
	systems         map[string]*System
	pois            map[string]*POI
	bases           map[string]*SpaceBase
	connections     map[string][]SystemConnection // from_system -> []to_system
	experiences     map[string][]Experience       // agent_id -> experiences
	agents          map[string]*AgentInfo
	shipListings []ShipListings

	// Catalog data
	items       map[string]CatalogItem
	shipClasses map[string]ShipClassDef
	skills      map[string]Skill
	recipes     map[string]RecipeDef

	// Player state
	players      map[string]PlayerRecord
	playerSkills map[string][]PlayerSkillRecord // playerID -> skills
	ships        map[string]ShipRecord
	// Storage snapshots: key is "agentID:baseID"
	storageSnapshots map[string]StorageSnapshot

	// Change detection snapshots
	changeSnapshots []ChangeSnapshot

	// XP observations
	xpObservations []XPObservation

	// Mission catalog
	missionCatalog   map[string]missionCatalogRow                // template_id -> row
	missionLocations map[string]map[string]missionLocationRecord // template_id -> base_id -> record
}

// missionLocationRecord tracks where a mission template has been sighted.
type missionLocationRecord struct {
	BaseID        string
	SystemID      string
	FirstSeenTick int64
	LastSeenTick  int64
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
}

// NewMemoryKB creates a new in-memory knowledge base
func NewMemoryKB() *MemoryKB {
	return &MemoryKB{
		systems:          make(map[string]*System),
		pois:             make(map[string]*POI),
		bases:            make(map[string]*SpaceBase),
		connections:      make(map[string][]SystemConnection),
		experiences:      make(map[string][]Experience),
		agents:           make(map[string]*AgentInfo),
		shipListings: make([]ShipListings, 0),
		items:            make(map[string]CatalogItem),
		shipClasses:      make(map[string]ShipClassDef),
		skills:           make(map[string]Skill),
		recipes:          make(map[string]RecipeDef),
		players:          make(map[string]PlayerRecord),
		playerSkills:     make(map[string][]PlayerSkillRecord),
		ships:            make(map[string]ShipRecord),
		storageSnapshots: make(map[string]StorageSnapshot),
		missionCatalog:   make(map[string]missionCatalogRow),
		missionLocations: make(map[string]map[string]missionLocationRecord),
	}
}

func (kb *MemoryKB) Close() error {
	// Nothing to close for in-memory KB
	return nil
}

// RememberSystem stores or updates system knowledge
func (kb *MemoryKB) RememberSystem(ctx context.Context, sys System) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	if existing, ok := kb.systems[sys.ID]; ok {
		existing.Name = sys.Name
		existing.Position = sys.Position
		existing.PoliceLevel = sys.PoliceLevel
		existing.SecurityStatus = sys.SecurityStatus
		existing.Empire = sys.Empire
		existing.IsStronghold = sys.IsStronghold
		existing.LastUpdatedTick = sys.LastUpdatedTick
		if sys.LastVisitedTick > 0 {
			existing.LastVisitedTick = sys.LastVisitedTick
		}
		existing.Connections = sys.Connections
	} else {
		kb.systems[sys.ID] = &System{
			ID:              sys.ID,
			Name:            sys.Name,
			Position:        sys.Position,
			PoliceLevel:     sys.PoliceLevel,
			SecurityStatus:  sys.SecurityStatus,
			Empire:          sys.Empire,
			IsStronghold:    sys.IsStronghold,
			Connections:     sys.Connections,
			LastUpdatedTick: sys.LastUpdatedTick,
			LastVisitedTick: sys.LastVisitedTick,
		}
	}

	return nil
}

// GetSystemsWithContext returns all known systems (with context for graph building)
func (kb *MemoryKB) GetSystems(ctx context.Context) ([]System, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	systems := make([]System, 0, len(kb.systems))
	for _, sys := range kb.systems {
		systems = append(systems, *sys)
	}

	return systems, nil
}

// GetConnections returns all system connections (for graph building)
func (kb *MemoryKB) GetConnections(ctx context.Context) ([]Connection, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var connections []Connection
	for fromID, sys := range kb.systems {
		for _, conn := range sys.Connections {
			connections = append(connections, Connection{
				FromSystem:     fromID,
				ToSystem:       conn.SystemID,
				Distance:       conn.Distance,
				LastUpdatedTick: sys.LastUpdatedTick,
			})
		}
	}

	return connections, nil
}

// GetConnectionMetrics returns connection metrics (for weighted pathfinding)
func (kb *MemoryKB) GetConnectionMetrics(ctx context.Context) ([]ConnectionMetric, error) {
	// MemoryKB doesn't track connection metrics, return empty slice
	return []ConnectionMetric{}, nil
}

// GetSystem retrieves a system by ID
func (kb *MemoryKB) GetSystem(ctx context.Context, systemID string) (*System, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	return kb.systems[systemID], nil
}

// GetUnknownConnections finds unexplored connections from a system
func (kb *MemoryKB) GetUnknownConnections(ctx context.Context, systemID string) ([]string, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var unknown []string

	// Get all connections from this system
	connections := kb.connections[systemID]

	for _, conn := range connections {
		// Check if the connected system exists in knowledge base
		if _, ok := kb.systems[conn.SystemID]; !ok {
			unknown = append(unknown, conn.SystemID)
		}
	}

	return unknown, nil
}

// RememberConnection stores a system connection (with deduplication)
func (kb *MemoryKB) RememberConnection(ctx context.Context, fromSystem, toSystem string) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	// Check for existing connection to avoid duplicates
	for _, existing := range kb.connections[fromSystem] {
		if existing.SystemID == toSystem {
			return nil
		}
	}

	kb.connections[fromSystem] = append(kb.connections[fromSystem], SystemConnection{SystemID: toSystem})

	return nil
}

// RememberPOI stores or updates POI knowledge
func (kb *MemoryKB) RememberPOI(ctx context.Context, poi POI) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.pois[poi.ID] = &POI{
		ID:               poi.ID,
		SystemID:         poi.SystemID,
		Name:             poi.Name,
		Type:             poi.Type,
		Position:         poi.Position,
		Description:      poi.Description,
		Services:         poi.Services,
		Resources:        poi.Resources,
		Hidden:           poi.Hidden,
		RevealDifficulty: poi.RevealDifficulty,
		ExpiresAt:        poi.ExpiresAt,
		LastUpdatedTick:  poi.LastUpdatedTick,
	}

	return nil
}

// GetPOIs retrieves all POIs in a system
func (kb *MemoryKB) GetPOIs(ctx context.Context, systemID string) ([]POI, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var result []POI
	for _, poi := range kb.pois {
		if poi.SystemID == systemID {
			result = append(result, *poi)
		}
	}

	return result, nil
}

// RememberBase stores or updates space base knowledge
func (kb *MemoryKB) RememberBase(ctx context.Context, base SpaceBase) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	// Create a copy of the base
	baseCopy := base
	kb.bases[base.ID] = &baseCopy

	return nil
}

// GetBase retrieves a base by ID
func (kb *MemoryKB) GetBase(ctx context.Context, baseID string) (*SpaceBase, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if base, ok := kb.bases[baseID]; ok {
		// Return a copy
		baseCopy := *base
		return &baseCopy, nil
	}

	return nil, fmt.Errorf("base not found: %s", baseID)
}

// GetBaseByPOI retrieves a base by its POI ID
func (kb *MemoryKB) GetBaseByPOI(ctx context.Context, poiID string) (*SpaceBase, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	for _, base := range kb.bases {
		if base.POIID == poiID {
			// Return a copy
			baseCopy := *base
			return &baseCopy, nil
		}
	}

	return nil, fmt.Errorf("no base found at POI: %s", poiID)
}

// AddExperience logs an agent experience
func (kb *MemoryKB) AddExperience(ctx context.Context, agentID, expType, description, outcome, location string) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	exp := Experience{
		Time:        time.Now().Format(time.RFC3339),
		Type:        expType,
		Description: description,
		Outcome:     outcome,
		Location:    location,
	}

	kb.experiences[agentID] = append(kb.experiences[agentID], exp)

	// Keep only last 100 experiences per agent
	if len(kb.experiences[agentID]) > 100 {
		kb.experiences[agentID] = kb.experiences[agentID][1:]
	}

	return nil
}

// GetRecentExperiences retrieves recent experiences for an agent
func (kb *MemoryKB) GetRecentExperiences(ctx context.Context, agentID string, limit int) ([]Experience, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	exps := kb.experiences[agentID]
	if len(exps) > limit {
		exps = exps[len(exps)-limit:]
	}

	return exps, nil
}

// RegisterAgent registers an agent in the knowledge base
func (kb *MemoryKB) RegisterAgent(ctx context.Context, agentID, name, role, empire string, personality []byte) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.agents[agentID] = &AgentInfo{
		ID:     agentID,
		Name:   name,
		Role:   role,
		Empire: empire,
		Status: "active",
	}

	return nil
}

// GetSystems returns all known systems
// SystemConnection pairs a connected system ID with the jump distance.
type SystemConnection struct {
	SystemID string
	Distance int
}

// Connection represents a direct connection between two systems.
// Unlike SystemConnection (which is embedded in System), this is a standalone
// record for graph building queries.
type Connection struct {
	FromSystem     string
	ToSystem       string
	Distance       int
	LastUpdatedTick int64
}

// ConnectionMetric represents aggregated travel metrics for a connection.
// Used for weighted pathfinding (fuel cost, travel time).
type ConnectionMetric struct {
	FromSystem   string
	ToSystem     string
	AvgFuelCost  float64
	AvgTravelTime float64
	LastTraveled string // ISO-8601 timestamp
}

// System represents knowledge about a solar system
// Wraps game.SystemData with exploration metadata
type System struct {
	ID              string
	Name            string
	Description     string
	Position        game.Position
	PoliceLevel     int    // Security level 0-3 (0=none, 1=low, 2=medium, 3=high)
	SecurityStatus  string // e.g. "high_sec", "low_sec", "null_sec"
	Empire          string
	IsStronghold    bool
	Connections     []SystemConnection
	POIs            []string
	LastUpdatedTick int64
	LastVisitedTick int64 // 0 = never visited; PoliceLevel is untrusted when 0
}

// Visited reports whether the system's PoliceLevel reflects a real
// observation rather than a map-import default (i.e. the system has
// been physically visited at least once).
func (s System) Visited() bool { return s.LastVisitedTick > 0 }

// MapSystemData contains the subset of system data available from map imports.
// Used by UpsertSystemFromMap to perform partial upserts that preserve richer
// explorer-collected data (police_level, description, last_updated_tick).
type MapSystemData struct {
	ID           string
	Name         string
	Empire       string
	PositionX    float64
	PositionY    float64
	IsStronghold bool
	Connections  []string
}

// POI represents knowledge about a Point of Interest
// Extends game.POI with exploration metadata
type POI struct {
	ID               string
	SystemID         string
	Name             string
	Type             string
	Class            string // Star class (e.g., "G2 V") or planet type (e.g., "terran")
	Description      string
	Position         game.Position
	Services         []string
	Resources        []game.POIResource
	Hidden           bool
	RevealDifficulty int
	ExpiresAt        string // ISO-8601 timestamp when POI expires (e.g., wormholes)
	LastUpdatedTick  int64
	// DetectedBy is the agent id that produced this observation. Provenance for
	// the shared KB so a later capability-aware merge can rank quality; today it
	// is recorded but not used to gate overwrites.
	DetectedBy string
}

// SpaceBase represents knowledge about a space station, outpost, base, or fortress
type SpaceBase struct {
	ID                string
	POIID             string // The POI this base is located at
	Name              string
	Description       string
	Story             string // Narrative description of the station
	Empire            string
	DefenseLevel      int
	HasDrones         bool
	PublicAccess      bool
	PirateRepRequired int
	Condition         string // e.g., "thriving", "degraded", "critical"
	ConditionText     string // Human-readable condition description
	SatisfactionPct   int    // 0-100 satisfaction percentage
	SatisfiedCount    int    // Number of satisfied service/infra facilities
	TotalServiceInfra int    // Total number of service/infra facilities
	Services          map[string]bool
	Facilities        []Facility // Facilities with category and level information
	Market            []BaseMarketItem
	LastUpdatedTick   int64
}

// BaseMarketItem represents an item for sale at a base market
type BaseMarketItem struct {
	ID        string
	ItemID    string
	PriceEach float64
	Quantity  int
	IsNPC     bool
}

// Facility represents a facility at a base with category and level information.
type Facility struct {
	ID                   string // Facility type ID (e.g., "commerce_hub")
	InstanceID           string // Unique instance ID for this facility at this base
	Name                 string // Display name
	Description          string // Current description (may reflect condition)
	Category             string // "service", "infrastructure", "production", "faction", "personal"
	Level                int    // Facility level (1-5)
	Active               bool   // Whether the facility is currently active
	MaintenanceSatisfied bool   // Whether maintenance requirements are met
	Service              string // Service provided (e.g., "market", "repair")
	RecipeID             string // Crafting recipe ID for production facilities
	LastUpdated          int64  // Last updated tick for data freshness tracking
}

// FacilityCategoryMapping maps facility IDs to their metadata
// This is populated from game data since the API doesn't provide categories
var FacilityCategoryMapping = map[string]Facility{
	// === SOLARIAN CONFEDERACY (confederacy_central_command) ===
	// Service facilities (commercial/visitor services)
	"grand_solarian_exchange":           {ID: "grand_solarian_exchange", Name: "Grand Solarian Exchange", Category: "service", Level: 5},
	"confederacy_administrative_bureau": {ID: "confederacy_administrative_bureau", Name: "Solarian Admin Bureau", Category: "service", Level: 5},
	"confederacy_bonded_warehouse":      {ID: "confederacy_bonded_warehouse", Name: "Solarian Bonded Warehouse", Category: "service", Level: 5},
	"solarian_precision_drydock":        {ID: "solarian_precision_drydock", Name: "Precision Drydock", Category: "service", Level: 5},
	"solarian_naval_shipyard":           {ID: "solarian_naval_shipyard", Name: "Naval Shipyard", Category: "service", Level: 5},
	"solarian_research_labs":            {ID: "solarian_research_labs", Name: "Research Labs", Category: "service", Level: 5},

	// Infrastructure facilities (station systems)
	"solarian_fusion_plant": {ID: "solarian_fusion_plant", Name: "Solarian Fusion Plant", Category: "infrastructure", Level: 5},
	"solarian_biosphere":    {ID: "solarian_biosphere", Name: "Solarian Life Support", Category: "infrastructure", Level: 5},

	// Production facilities (manufacturing)
	"iron_refinery":              {ID: "iron_refinery", Name: "Iron Refinery", Category: "production", Level: 1},
	"circuit_fabricator":         {ID: "circuit_fabricator", Name: "Circuit Fabricator", Category: "production", Level: 1},
	"copper_wire_mill":           {ID: "copper_wire_mill", Name: "Copper Wire Mill", Category: "production", Level: 1},
	"polymer_synthesizer":        {ID: "polymer_synthesizer", Name: "Polymer Synthesizer", Category: "production", Level: 1},
	"fuel_cell_plant":            {ID: "fuel_cell_plant", Name: "Fuel Cell Plant", Category: "production", Level: 1},
	"repair_kit_factory":         {ID: "repair_kit_factory", Name: "Repair Kit Factory", Category: "production", Level: 1},
	"power_cell_assembler":       {ID: "power_cell_assembler", Name: "Power Cell Assembler", Category: "production", Level: 1},
	"sensor_assembly_line":       {ID: "sensor_assembly_line", Name: "Sensor Assembly", Category: "production", Level: 2},
	"solarian_biosphere_kitchen": {ID: "solarian_biosphere_kitchen", Name: "Solarian Galley", Category: "production", Level: 1},
	"solarian_fuel_grid":         {ID: "solarian_fuel_grid", Name: "Solarian Fuel Grid", Category: "production", Level: 5},

	// === NEBULA COLLECTIVE (grand_exchange_station) ===
	// Service facilities
	"haven_grand_bazaar":     {ID: "haven_grand_bazaar", Name: "Grand Bazaar", Category: "service", Level: 5},
	"haven_promenade":        {ID: "haven_promenade", Name: "Promenade", Category: "service", Level: 5},
	"haven_repair_complex":   {ID: "haven_repair_complex", Name: "Repair Complex", Category: "service", Level: 5},
	"haven_fuel_plaza":       {ID: "haven_fuel_plaza", Name: "Fuel Plaza", Category: "service", Level: 5},
	"haven_ship_showroom":    {ID: "haven_ship_showroom", Name: "Ship Showroom", Category: "service", Level: 5},
	"haven_makers_market":    {ID: "haven_makers_market", Name: "Makers Market", Category: "service", Level: 5},
	"haven_trade_commission": {ID: "haven_trade_commission", Name: "Trade Commission", Category: "service", Level: 5},
	"haven_premium_storage":  {ID: "haven_premium_storage", Name: "Premium Storage", Category: "service", Level: 5},
	"haven_cipher_foundry":   {ID: "haven_cipher_foundry", Name: "Trade Cipher Foundry", Category: "service", Level: 5},

	// Infrastructure facilities
	"nebula_solar_array": {ID: "nebula_solar_array", Name: "Nebula Solar Array", Category: "infrastructure", Level: 5},
	"haven_ecosync":      {ID: "haven_ecosync", Name: "Nebula Life Support", Category: "infrastructure", Level: 5},

	// === VOIDBORN (central_nexus) ===
	// Service facilities
	"void_nexus_exchange": {ID: "void_nexus_exchange", Name: "Void Nexus Exchange", Category: "service", Level: 5},

	// Infrastructure facilities (Voidborn station systems)
	"null_energy_tap":           {ID: "null_energy_tap", Name: "Null Energy Tap", Category: "infrastructure", Level: 5},
	"null_atmosphere_processor": {ID: "null_atmosphere_processor", Name: "Voidborn Atmosphere", Category: "infrastructure", Level: 5},
	"dimensional_vault":         {ID: "dimensional_vault", Name: "Dimensional Vault", Category: "infrastructure", Level: 5},
	"void_energy_dispenser":     {ID: "void_energy_dispenser", Name: "Energy Dispenser", Category: "infrastructure", Level: 5},
	"neural_growth_chamber":     {ID: "neural_growth_chamber", Name: "Neural Forge", Category: "infrastructure", Level: 5},
	"reconstruction_matrix":     {ID: "reconstruction_matrix", Name: "Reconstruction Matrix", Category: "infrastructure", Level: 5},

	// Faction facilities (Voidborn unique)
	"pattern_council_terminal":    {ID: "pattern_council_terminal", Name: "Pattern Council", Category: "faction", Level: 5},
	"crystalline_cradle":          {ID: "crystalline_cradle", Name: "Crystalline Cradle", Category: "faction", Level: 5},
	"null_energy_shaping_chamber": {ID: "null_energy_shaping_chamber", Name: "Shaping Chamber", Category: "faction", Level: 5},

	// Production facilities
	"crystal_refinery":      {ID: "crystal_refinery", Name: "Crystal Refinery", Category: "production", Level: 5},
	"null_matter_processor": {ID: "null_matter_processor", Name: "Null Matter Processor", Category: "production", Level: 5},

	// === CRIMSON (crimson_war_citadel) ===
	// Service facilities
	"fleet_command": {ID: "fleet_command", Name: "Crimson Fleet Command", Category: "service", Level: 5},
	"war_market":    {ID: "war_market", Name: "War Market", Category: "service", Level: 5},

	// Infrastructure facilities
	"fleet_life_support":     {ID: "fleet_life_support", Name: "Crimson Life Support", Category: "infrastructure", Level: 5},
	"military_grade_reactor": {ID: "military_grade_reactor", Name: "Crimson Reactor", Category: "infrastructure", Level: 5},

	// Production facilities (Crimson war manufacturing)
	"alloy_foundry":          {ID: "alloy_foundry", Name: "Alloy Foundry", Category: "production", Level: 5},
	"crimson_armor_works":    {ID: "crimson_armor_works", Name: "Crimson Armor Works", Category: "production", Level: 5},
	"fleet_forge_distillery": {ID: "fleet_forge_distillery", Name: "Crimson Distillery", Category: "production", Level: 5},
	"fleet_fuel_bunker":      {ID: "fleet_fuel_bunker", Name: "Crimson Fuel Bunker", Category: "production", Level: 5},
	"fleet_munitions_vault":  {ID: "fleet_munitions_vault", Name: "Munitions Vault", Category: "production", Level: 5},
	"crimson_war_forge":      {ID: "crimson_war_forge", Name: "Crimson War Forge", Category: "production", Level: 5},
	"fleet_weapons_forge":    {ID: "fleet_weapons_forge", Name: "Weapons Forge", Category: "production", Level: 5},

	// === FRONTIER (frontier_station) ===
	// Service facilities
	"frontier_exchange":  {ID: "frontier_exchange", Name: "Frontier Exchange", Category: "service", Level: 1},
	"the_notice_board":   {ID: "the_notice_board", Name: "Notice Board", Category: "service", Level: 1},
	"the_magazine_still": {ID: "the_magazine_still", Name: "Still", Category: "service", Level: 1},

	// Infrastructure facilities
	"frontier_fuel_siphon": {ID: "frontier_fuel_siphon", Name: "Fuel Siphon", Category: "infrastructure", Level: 1},
	"frontier_recycler":    {ID: "frontier_recycler", Name: "Life Support", Category: "infrastructure", Level: 1},
	"salvage_reactor":      {ID: "salvage_reactor", Name: "Salvage Reactor", Category: "infrastructure", Level: 1},

	// Production facilities (Frontier scavenging/manufacturing)
	"hull_lockers":          {ID: "hull_lockers", Name: "Hull Lockers", Category: "production", Level: 1},
	"frontier_machine_shop": {ID: "frontier_machine_shop", Name: "Machine Shop", Category: "production", Level: 1},
	"frontier_salvage_yard": {ID: "frontier_salvage_yard", Name: "Salvage Yard", Category: "production", Level: 1},
	"frontier_weld_shop":    {ID: "frontier_weld_shop", Name: "Weld Shop", Category: "production", Level: 1},
}

// Experience represents a significant event
type Experience struct {
	Time            string
	Type            string
	Description     string
	Outcome         string
	Location        string
	LastUpdatedTick int64
}

// AgentInfo holds agent metadata
type AgentInfo struct {
	ID     string
	Name   string
	Role   string
	Empire string
	Status string
}

// Enhanced analytics methods - stub implementations for in-memory KB
// These return "not implemented" errors as the in-memory KB is for testing only

func (kb *MemoryKB) RecordResourceState(ctx context.Context, poiID, resourceID string, richness, remaining float64, gameTick int64, agentID string) error {
	return fmt.Errorf("RecordResourceState not implemented for in-memory KB")
}

func (kb *MemoryKB) GetResourceHistory(ctx context.Context, poiID, resourceID string, limit int) ([]ResourceHistory, error) {
	return nil, fmt.Errorf("GetResourceHistory not implemented for in-memory KB")
}

func (kb *MemoryKB) GetDepletingResources(ctx context.Context, threshold float64) ([]DepletingResource, error) {
	return nil, fmt.Errorf("GetDepletingResources not implemented for in-memory KB")
}

func (kb *MemoryKB) RecordJourney(ctx context.Context, fromSystem, toSystem string, fuelCost, travelTime float64, agentID string) error {
	return fmt.Errorf("RecordJourney not implemented for in-memory KB")
}

func (kb *MemoryKB) GetOptimalRoute(ctx context.Context, fromSystem, toSystem string) (*ConnectionMetrics, error) {
	return nil, fmt.Errorf("GetOptimalRoute not implemented for in-memory KB")
}

func (kb *MemoryKB) FindCheapestRoute(ctx context.Context, fromSystem, toSystem string, maxHops int) ([]string, float64, error) {
	return nil, 0, fmt.Errorf("FindCheapestRoute not implemented for in-memory KB")
}

func (kb *MemoryKB) RecordAnomaly(ctx context.Context, anomaly Anomaly) error {
	return fmt.Errorf("RecordAnomaly not implemented for in-memory KB")
}

func (kb *MemoryKB) GetActiveAnomalies(ctx context.Context, systemID string) ([]Anomaly, error) {
	return nil, fmt.Errorf("GetActiveAnomalies not implemented for in-memory KB")
}

func (kb *MemoryKB) GetAnomaliesByType(ctx context.Context, anomalyType, severity string, limit int) ([]Anomaly, error) {
	return nil, fmt.Errorf("GetAnomaliesByType not implemented for in-memory KB")
}

func (kb *MemoryKB) ResolveAnomaly(ctx context.Context, anomalyID int64, status string) error {
	return fmt.Errorf("ResolveAnomaly not implemented for in-memory KB")
}

func (kb *MemoryKB) RecordHostileEncounter(ctx context.Context, systemID string, encounterType string, details string) error {
	return fmt.Errorf("RecordHostileEncounter not implemented for in-memory KB")
}

func (kb *MemoryKB) GetDangerZones(ctx context.Context, minDangerLevel int) ([]DangerZone, error) {
	return nil, fmt.Errorf("GetDangerZones not implemented for in-memory KB")
}

func (kb *MemoryKB) GetSystemDanger(ctx context.Context, systemID string) (*DangerZone, error) {
	return nil, fmt.Errorf("GetSystemDanger not implemented for in-memory KB")
}

func (kb *MemoryKB) ExportKnowledge(ctx context.Context, description string, agentID string) (*KnowledgeExport, error) {
	return nil, fmt.Errorf("ExportKnowledge not implemented for in-memory KB")
}

func (kb *MemoryKB) ImportKnowledge(ctx context.Context, exportData string) error {
	return fmt.Errorf("ImportKnowledge not implemented for in-memory KB")
}

func (kb *MemoryKB) ListExports(ctx context.Context) ([]KnowledgeExportMeta, error) {
	return nil, fmt.Errorf("ListExports not implemented for in-memory KB")
}

// StoreShipListings stores the latest ship-listing snapshot for a station,
// replacing any prior snapshot for that station (mirrors SQLiteKB).
func (kb *MemoryKB) StoreShipListings(ctx context.Context, listings ShipListings, agentID string) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	if listings.CapturedAt.IsZero() {
		listings.CapturedAt = time.Now()
	}

	kept := kb.shipListings[:0]
	for _, l := range kb.shipListings {
		if l.StationID != listings.StationID {
			kept = append(kept, l)
		}
	}
	kb.shipListings = append(kept, listings)
	return nil
}

// GetShipListings retrieves historical ship listings
func (kb *MemoryKB) GetShipListings(ctx context.Context, systemID, stationID string, limit int) ([]ShipListings, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var result []ShipListings
	count := 0

	for i := len(kb.shipListings) - 1; i >= 0; i-- {
		listings := kb.shipListings[i]
		if listings.SystemID == systemID && listings.StationID == stationID {
			result = append(result, listings)
			count++
			if count >= limit {
				break
			}
		}
	}

	return result, nil
}

// GetLatestShipListings retrieves the most recent ship listings
func (kb *MemoryKB) GetLatestShipListings(ctx context.Context, systemID, stationID string) (*ShipListings, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	for i := len(kb.shipListings) - 1; i >= 0; i-- {
		listings := kb.shipListings[i]
		if listings.SystemID == systemID && listings.StationID == stationID {
			return &listings, nil
		}
	}

	return nil, nil // Not found
}

// HasShipListingsToday checks if ship listings were captured today for a station
func (kb *MemoryKB) HasShipListingsToday(ctx context.Context, systemID, stationID string) (bool, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	today := time.Now().Format("2006-01-02")
	for _, listings := range kb.shipListings {
		if listings.SystemID == systemID && listings.StationID == stationID && listings.CapturedAt.Format("2006-01-02") == today {
			return true, nil
		}
	}
	return false, nil
}

// --- Catalog: Items ---

func (kb *MemoryKB) StoreItems(ctx context.Context, items []CatalogItem) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.items = make(map[string]CatalogItem, len(items))
	for _, item := range items {
		kb.items[item.ID] = item
	}
	return nil
}

func (kb *MemoryKB) GetItem(ctx context.Context, itemID string) (*CatalogItem, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if item, ok := kb.items[itemID]; ok {
		return &item, nil
	}
	return nil, nil
}

func (kb *MemoryKB) GetItems(ctx context.Context) ([]CatalogItem, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	items := make([]CatalogItem, 0, len(kb.items))
	for _, item := range kb.items {
		items = append(items, item)
	}
	return items, nil
}

func (kb *MemoryKB) GetItemsByCategory(ctx context.Context, category string) ([]CatalogItem, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var items []CatalogItem
	for _, item := range kb.items {
		if item.Category == category {
			items = append(items, item)
		}
	}
	return items, nil
}

// --- Catalog: Ship Classes ---

func (kb *MemoryKB) StoreShipClasses(ctx context.Context, classes []ShipClassDef) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.shipClasses = make(map[string]ShipClassDef, len(classes))
	for _, sc := range classes {
		kb.shipClasses[sc.ID] = sc
	}
	return nil
}

func (kb *MemoryKB) GetShipClass(ctx context.Context, classID string) (*ShipClassDef, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if sc, ok := kb.shipClasses[classID]; ok {
		return &sc, nil
	}
	return nil, nil
}

func (kb *MemoryKB) GetShipClasses(ctx context.Context) ([]ShipClassDef, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	classes := make([]ShipClassDef, 0, len(kb.shipClasses))
	for _, sc := range kb.shipClasses {
		classes = append(classes, sc)
	}
	return classes, nil
}

func (kb *MemoryKB) GetShipClassesByCategory(ctx context.Context, category string) ([]ShipClassDef, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var classes []ShipClassDef
	for _, sc := range kb.shipClasses {
		if sc.Class == category {
			classes = append(classes, sc)
		}
	}
	return classes, nil
}

// --- Catalog: Recipes ---

func (kb *MemoryKB) StoreRecipes(ctx context.Context, recipes []RecipeDef) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.recipes = make(map[string]RecipeDef, len(recipes))
	for _, r := range recipes {
		kb.recipes[r.ID] = r
	}
	return nil
}

func (kb *MemoryKB) GetRecipe(ctx context.Context, recipeID string) (*RecipeDef, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if r, ok := kb.recipes[recipeID]; ok {
		return &r, nil
	}
	return nil, nil
}

func (kb *MemoryKB) GetRecipes(ctx context.Context) ([]RecipeDef, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	recipes := make([]RecipeDef, 0, len(kb.recipes))
	for _, r := range kb.recipes {
		recipes = append(recipes, r)
	}
	return recipes, nil
}

func (kb *MemoryKB) GetRecipesByCategory(ctx context.Context, category string) ([]RecipeDef, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var recipes []RecipeDef
	for _, r := range kb.recipes {
		if r.Category == category {
			recipes = append(recipes, r)
		}
	}
	return recipes, nil
}

// --- Player State ---

func (kb *MemoryKB) StorePlayer(ctx context.Context, player PlayerRecord) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.players[player.ID] = player
	return nil
}

func (kb *MemoryKB) GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if p, ok := kb.players[playerID]; ok {
		return &p, nil
	}
	return nil, nil
}

func (kb *MemoryKB) StorePlayerSkills(ctx context.Context, playerID string, skills []PlayerSkillRecord) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.playerSkills[playerID] = skills
	return nil
}

func (kb *MemoryKB) GetPlayerSkills(ctx context.Context, playerID string) ([]PlayerSkillRecord, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	return kb.playerSkills[playerID], nil
}

func (kb *MemoryKB) StoreShip(ctx context.Context, ship ShipRecord) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.ships[ship.ID] = ship
	return nil
}

func (kb *MemoryKB) GetShip(ctx context.Context, shipID string) (*ShipRecord, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if s, ok := kb.ships[shipID]; ok {
		return &s, nil
	}
	return nil, nil
}

func (kb *MemoryKB) GetPlayerShips(ctx context.Context, playerID string) ([]ShipRecord, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var ships []ShipRecord
	for _, s := range kb.ships {
		if s.OwnerID == playerID {
			ships = append(ships, s)
		}
	}
	return ships, nil
}

func (kb *MemoryKB) StoreStorageSnapshot(_ context.Context, snapshot StorageSnapshot) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now()
	}
	key := snapshot.AgentID + ":" + snapshot.BaseID
	kb.storageSnapshots[key] = snapshot
	return nil
}

func (kb *MemoryKB) GetStorageSnapshot(_ context.Context, agentID, baseID string) (*StorageSnapshot, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	key := agentID + ":" + baseID
	s, ok := kb.storageSnapshots[key]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (kb *MemoryKB) GetAllStorageSnapshots(_ context.Context) ([]StorageSnapshot, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	snapshots := make([]StorageSnapshot, 0, len(kb.storageSnapshots))
	for _, s := range kb.storageSnapshots {
		snapshots = append(snapshots, s)
	}
	return snapshots, nil
}

func (kb *MemoryKB) UpdateAgentWalletCredits(_ context.Context, agentID string, credits int) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	if a, ok := kb.agents[agentID]; ok {
		_ = a // in-memory KB doesn't track wallet credits
	}
	return nil
}

func (kb *MemoryKB) RecordChangeSnapshot(_ context.Context, snapshot ChangeSnapshot) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	kb.changeSnapshots = append(kb.changeSnapshots, snapshot)
	return nil
}

func (kb *MemoryKB) GetChangeSnapshots(_ context.Context, systemID string, limit int) ([]ChangeSnapshot, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var result []ChangeSnapshot
	// Iterate in reverse for newest-first order
	for i := len(kb.changeSnapshots) - 1; i >= 0 && len(result) < limit; i-- {
		if kb.changeSnapshots[i].SystemID == systemID {
			result = append(result, kb.changeSnapshots[i])
		}
	}
	return result, nil
}

func (kb *MemoryKB) RecordXPObservation(_ context.Context, obs XPObservation) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	obs.ID = int64(len(kb.xpObservations) + 1)
	if obs.CreatedAt.IsZero() {
		obs.CreatedAt = time.Now()
	}
	kb.xpObservations = append(kb.xpObservations, obs)
	return nil
}

func (kb *MemoryKB) GetXPObservations(_ context.Context, action string, limit int) ([]XPObservation, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var result []XPObservation
	for i := len(kb.xpObservations) - 1; i >= 0 && len(result) < limit; i-- {
		if action == "" || kb.xpObservations[i].Action == action {
			result = append(result, kb.xpObservations[i])
		}
	}
	return result, nil
}

func (kb *MemoryKB) GetXPSummary(_ context.Context) ([]XPSummaryRow, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	type key struct{ action, skillID, source string }
	agg := make(map[key]struct {
		totalXP float64
		count   int
	})
	for _, o := range kb.xpObservations {
		k := key{o.Action, o.SkillID, o.Source}
		v := agg[k]
		v.totalXP += o.XPDelta
		v.count++
		agg[k] = v
	}

	var result []XPSummaryRow
	for k, v := range agg {
		result = append(result, XPSummaryRow{
			Action:     k.action,
			SkillID:    k.skillID,
			Source:     k.source,
			AvgXPDelta: v.totalXP / float64(v.count),
			Count:      v.count,
		})
	}
	return result, nil
}

// UpsertMissionTemplate implements Base.
func (kb *MemoryKB) UpsertMissionTemplate(
	ctx context.Context,
	entry serverapi.MissionBoardEntry,
	baseID, systemID string,
	tick int64,
) (*MissionUpsertResult, error) {
	_ = ctx
	kb.mu.Lock()
	defer kb.mu.Unlock()

	row := missionRowFromEntry(entry)
	res := &MissionUpsertResult{}
	now := time.Now().UTC()

	if existing, ok := kb.missionCatalog[row.ID]; ok {
		if diffs := diffMissionRows(existing, row); len(diffs) > 0 {
			res.Diffs = diffs
			kb.missionCatalog[row.ID] = row
		}
	} else {
		res.Inserted = true
		kb.missionCatalog[row.ID] = row
	}

	locs := kb.missionLocations[row.ID]
	if locs == nil {
		locs = make(map[string]missionLocationRecord)
		kb.missionLocations[row.ID] = locs
	}
	loc, ok := locs[baseID]
	if !ok {
		loc = missionLocationRecord{
			BaseID:        baseID,
			SystemID:      systemID,
			FirstSeenTick: tick,
			FirstSeenAt:   now,
		}
	}
	loc.LastSeenTick = tick
	loc.LastSeenAt = now
	locs[baseID] = loc

	return res, nil
}

// RecordSightings is a no-op for the in-memory KB — sightings are
// persistent-only state and the memory backend exists for tests/agents
// that don't care to retain them.
func (kb *MemoryKB) RecordSightings(_ []SeenPlayer) error {
	return nil
}

// RecordPassengers is a no-op for the in-memory KB — the passenger catalog is
// persistent-only state, mirroring RecordSightings.
func (kb *MemoryKB) RecordPassengers(_ []SeenPassenger) error {
	return nil
}
