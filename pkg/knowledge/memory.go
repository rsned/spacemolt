package knowledge

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryKB is an in-memory knowledge base for MVP
type MemoryKB struct {
	mu               sync.RWMutex
	systems          map[string]*System
	pois             map[string]*POI
	connections      map[string][]string // from_system -> []to_system
	experiences      map[string][]Experience // agent_id -> experiences
	agents           map[string]*AgentInfo
	marketSnapshots  []MarketSnapshot
	marketItems      map[string]struct{} // set of unique item IDs
}

// NewMemoryKB creates a new in-memory knowledge base
func NewMemoryKB() *MemoryKB {
	return &MemoryKB{
		systems:         make(map[string]*System),
		pois:            make(map[string]*POI),
		connections:     make(map[string][]string),
		experiences:     make(map[string][]Experience),
		agents:          make(map[string]*AgentInfo),
		marketSnapshots: make([]MarketSnapshot, 0),
		marketItems:     make(map[string]struct{}),
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
		existing.SecurityLevel = sys.SecurityLevel
		existing.Faction = sys.Faction
		existing.VisitCount++
		existing.LastVisited = time.Now().Format(time.RFC3339)
	} else {
		kb.systems[sys.ID] = &System{
			ID:            sys.ID,
			Name:          sys.Name,
			Position:      sys.Position,
			SecurityLevel: sys.SecurityLevel,
			Faction:       sys.Faction,
			Connections:   sys.Connections,
			VisitCount:     1,
			DiscoveredBy:  sys.DiscoveredBy,
			LastVisited:   time.Now().Format(time.RFC3339),
		}
	}

	return nil
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

	for _, connID := range connections {
		// Check if the connected system has been visited
		if sys, ok := kb.systems[connID]; !ok || sys.VisitCount == 0 {
			unknown = append(unknown, connID)
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
		if existing == toSystem {
			return nil
		}
	}

	kb.connections[fromSystem] = append(kb.connections[fromSystem], toSystem)

	return nil
}

// RememberPOI stores or updates POI knowledge
func (kb *MemoryKB) RememberPOI(ctx context.Context, poi POI) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.pois[poi.ID] = &POI{
		ID:       poi.ID,
		SystemID: poi.SystemID,
		Name:     poi.Name,
		Type:     poi.Type,
		Position: poi.Position,
		Services: poi.Services,
		Resources: poi.Resources,
		DiscoveredBy: poi.DiscoveredBy,
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
func (kb *MemoryKB) RegisterAgent(ctx context.Context, agentID, name, role, faction string, personality []byte) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.agents[agentID] = &AgentInfo{
		ID:     agentID,
		Name:   name,
		Role:   role,
		Faction: faction,
		Status: "active",
	}

	return nil
}

// GetSystems returns all known systems
func (kb *MemoryKB) GetSystems() []System {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	systems := make([]System, 0, len(kb.systems))
	for _, sys := range kb.systems {
		systems = append(systems, *sys)
	}

	return systems
}

// System represents knowledge about a solar system
type System struct {
	ID            string
	Name          string
	Position      Position
	SecurityLevel string
	Faction       string
	Connections   []string
	POIs          []string
	LastVisited   string
	VisitCount    int
	DiscoveredBy  string
}

// ResourceInfo represents resource data at a POI
type ResourceInfo struct {
	ResourceID string
	Richness   float64
	Remaining  float64
}

// POI represents knowledge about a Point of Interest
type POI struct {
	ID            string
	SystemID      string
	Name          string
	Type          string
	Description   string
	Position      Position
	Services      []string
	Resources     []ResourceInfo
	DiscoveredBy  string
}

// Position represents 3D coordinates
type Position struct {
	X float64
	Y float64
	Z float64
}

// Experience represents a significant event
type Experience struct {
	Time        string
	Type        string
	Description string
	Outcome     string
	Location    string
}

// AgentInfo holds agent metadata
type AgentInfo struct {
	ID      string
	Name    string
	Role    string
	Faction string
	Status  string
}

// StoreMarketSnapshot stores a market snapshot with its listings
func (kb *MemoryKB) StoreMarketSnapshot(ctx context.Context, snapshot MarketSnapshot, agentID string) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	// Set capture time if not set
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now()
	}

	// Add snapshot to storage (keep most recent 1000)
	kb.marketSnapshots = append(kb.marketSnapshots, snapshot)
	if len(kb.marketSnapshots) > 1000 {
		kb.marketSnapshots = kb.marketSnapshots[1:]
	}

	// Track unique items
	for _, listing := range snapshot.Listings {
		kb.marketItems[listing.ItemID] = struct{}{}
	}

	return nil
}

// GetMarketSnapshots retrieves historical market snapshots
func (kb *MemoryKB) GetMarketSnapshots(ctx context.Context, systemID, stationID string, limit int) ([]MarketSnapshot, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var result []MarketSnapshot
	count := 0

	// Iterate in reverse order (most recent first)
	for i := len(kb.marketSnapshots) - 1; i >= 0; i-- {
		snap := kb.marketSnapshots[i]
		if snap.SystemID == systemID && snap.StationID == stationID {
			result = append(result, snap)
			count++
			if count >= limit {
				break
			}
		}
	}

	return result, nil
}

// GetLatestMarketSnapshot retrieves the most recent market snapshot
func (kb *MemoryKB) GetLatestMarketSnapshot(ctx context.Context, systemID, stationID string) (*MarketSnapshot, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	// Search in reverse order (most recent first)
	for i := len(kb.marketSnapshots) - 1; i >= 0; i-- {
		snap := kb.marketSnapshots[i]
		if snap.SystemID == systemID && snap.StationID == stationID {
			return &snap, nil
		}
	}

	return nil, nil // Not found
}

// GetMarketItems retrieves unique item IDs optionally filtered by type
func (kb *MemoryKB) GetMarketItems(ctx context.Context, itemType string) ([]string, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if itemType == "" {
		// Return all unique items
		items := make([]string, 0, len(kb.marketItems))
		for itemID := range kb.marketItems {
			items = append(items, itemID)
		}
		return items, nil
	}

	// Filter by type - need to scan snapshots
	seen := make(map[string]struct{})
	for _, snap := range kb.marketSnapshots {
		for _, listing := range snap.Listings {
			if listing.ItemType == itemType {
				seen[listing.ItemID] = struct{}{}
			}
		}
	}

	items := make([]string, 0, len(seen))
	for itemID := range seen {
		items = append(items, itemID)
	}
	return items, nil
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

func (kb *MemoryKB) AnalyzePriceTrends(ctx context.Context, itemID, stationID string, windowHours int) (*PriceTrend, error) {
	return nil, fmt.Errorf("AnalyzePriceTrends not implemented for in-memory KB")
}

func (kb *MemoryKB) FindBestPrices(ctx context.Context, itemID string, listingType string, limit int) ([]BestPrice, error) {
	return nil, fmt.Errorf("FindBestPrices not implemented for in-memory KB")
}

func (kb *MemoryKB) GetPriceHistory(ctx context.Context, itemID, stationID string, limit int) ([]PricePoint, error) {
	return nil, fmt.Errorf("GetPriceHistory not implemented for in-memory KB")
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
