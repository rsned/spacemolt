package knowledge

import (
	"context"
	"sync"
	"time"
)

// MemoryKB is an in-memory knowledge base for MVP
type MemoryKB struct {
	mu            sync.RWMutex
	systems       map[string]*System
	pois          map[string]*POI
	connections   map[string][]string // from_system -> []to_system
	experiences   map[string][]Experience // agent_id -> experiences
	agents        map[string]*AgentInfo
}

// NewMemoryKB creates a new in-memory knowledge base
func NewMemoryKB() *MemoryKB {
	return &MemoryKB{
		systems:     make(map[string]*System),
		pois:        make(map[string]*POI),
		connections: make(map[string][]string),
		experiences: make(map[string][]Experience),
		agents:      make(map[string]*AgentInfo),
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
