package game

import (
	"sync"
	"time"
)

// POI represents a Point of Interest in a system
type POI struct {
	ID          string
	Name        string
	Type        string
	Description string
	X           float64
	Y           float64
	SystemID    string
	Resources   []map[string]any
}

// SystemData holds the current system information
type SystemData struct {
	Name        string
	Description string
	POIs        []POI
	Connections []string
	ShipPOI     string // ID of the POI where the ship is located
}

// State represents the current game state
type State struct {
	Mu            sync.Mutex
	Username      string
	Token         string
	Doc           bool
	CurrentSystem string
	CurrentPOI    string
	Traveling     bool
	Credits       float64
	Fuel          float64
	MaxFuel       float64
	Hull          float64
	MaxHull       float64
	Cargo         []map[string]any
	MaxCargo      int
	CurrentTick   int64
	System        SystemData
	LastMapUpdate time.Time
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

	connectionsCopy := make([]string, len(s.System.Connections))
	copy(connectionsCopy, s.System.Connections)

	return &State{
		Username:      s.Username,
		Token:         s.Token,
		Doc:           s.Doc,
		CurrentSystem: s.CurrentSystem,
		CurrentPOI:    s.CurrentPOI,
		Traveling:     s.Traveling,
		Credits:       s.Credits,
		Fuel:          s.Fuel,
		MaxFuel:       s.MaxFuel,
		Hull:          s.Hull,
		MaxHull:       s.MaxHull,
		Cargo:         cargoCopy,
		MaxCargo:      s.MaxCargo,
		CurrentTick:   s.CurrentTick,
		System: SystemData{
			Name:        s.System.Name,
			Description: s.System.Description,
			POIs:        poisCopy,
			Connections: connectionsCopy,
			ShipPOI:     s.System.ShipPOI,
		},
		LastMapUpdate: s.LastMapUpdate,
	}
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
