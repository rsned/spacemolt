package prompts

import "github.com/rsned/spacemolt/pkg/game"

// TemplateContext holds all data available to templates
type TemplateContext struct {
	AgentID      string
	AgentName    string
	Role         string
	Personality  *PersonalityContext
	State        *StateContext
	Knowledge    *KnowledgeContext
	History      *HistoryContext
	LastFeedback *FeedbackContext
	System       *SystemContext
}

// PersonalityContext contains agent personality information
type PersonalityContext struct {
	Name         string
	Role         string
	Traits       []string
	Motivations  []string
	Skills       []string
	Background   string
}

// StateContext contains current game state information
type StateContext struct {
	SystemName   string
	SystemID     string
	Location     string // e.g., "Sol-Station-Alpha"
	IsDocked     bool
	DockedAt     string
	Fuel         float64
	MaxFuel      float64
	FuelPercent  float64
	Hull         float64
	MaxHull      float64
	HullPercent  float64
	Cargo        []CargoItem
	CargoCount   int
	MaxCargo     int
	Credits      float64
	Tick         int64
}

// CargoItem represents an item in cargo
type CargoItem struct {
	Type     string
	Quantity int
}

// KnowledgeContext contains what the agent knows
type KnowledgeContext struct {
	KnownSystems     []SystemInfo
	POIsInSystem     []POIInfo
	Connections      []string
	UnknownConnections []string
}

// SystemInfo represents knowledge about a system
type SystemInfo struct {
	ID            string
	Name          string
	SecurityLevel string
	Faction       string
	VisitCount    int
}

// POIInfo represents knowledge about a point of interest
type POIInfo struct {
	ID       string
	Name     string
	Type     string
	Position string
}

// HistoryContext contains recent experiences
type HistoryContext struct {
	RecentActions    []ActionRecord
	RecentExperiences []ExperienceRecord
}

// ActionRecord represents a past action
type ActionRecord struct {
	Action    string
	Target    string
	Success   bool
	Message   string
	Tick      int
}

// ExperienceRecord represents a past experience
type ExperienceRecord struct {
	Time        string
	Type        string
	Description string
	Outcome     string
	Location    string
}

// FeedbackContext contains feedback from the last action
type FeedbackContext struct {
	Success   bool
	Action    string
	Target    string
	Message   string
	Error     string
	ErrorType string // e.g., "validation", "execution", "timeout"
}

// SystemContext contains system-level information
type SystemContext struct {
	AvailableActions []ActionInfo
	CurrentTick      int64
	ServerTime       string
}

// ActionInfo describes an available action
type ActionInfo struct {
	Name        string
	Description string
	RequiresTarget bool
	TargetType  string // "poi_id", "system_name", "none"
	Requirements string // e.g., "Must be docked", "Requires fuel > 10%"
}

// NewTemplateContext creates a TemplateContext from game state and agent data
func NewTemplateContext(
	agentID string,
	agentName string,
	role string,
	personality map[string]interface{},
	state *game.State,
	knowledge *KnowledgeContext,
	history *HistoryContext,
	lastFeedback *FeedbackContext,
) *TemplateContext {
	return &TemplateContext{
		AgentID:      agentID,
		AgentName:    agentName,
		Role:         role,
		Personality:  buildPersonalityContext(agentName, role, personality),
		State:        buildStateContext(state),
		Knowledge:    knowledge,
		History:      history,
		LastFeedback: lastFeedback,
		System:       buildSystemContext(state),
	}
}

// buildPersonalityContext extracts personality data
func buildPersonalityContext(name, role string, p map[string]interface{}) *PersonalityContext {
	pc := &PersonalityContext{
		Name: name,
		Role: role,
	}

	if traits, ok := p["traits"].([]interface{}); ok {
		pc.Traits = make([]string, len(traits))
		for i, t := range traits {
			if s, ok := t.(string); ok {
				pc.Traits[i] = s
			}
		}
	}

	if motivations, ok := p["motivations"].([]interface{}); ok {
		pc.Motivations = make([]string, len(motivations))
		for i, m := range motivations {
			if s, ok := m.(string); ok {
				pc.Motivations[i] = s
			}
		}
	}

	if skills, ok := p["skills"].([]interface{}); ok {
		pc.Skills = make([]string, len(skills))
		for i, sk := range skills {
			if s, ok := sk.(string); ok {
				pc.Skills[i] = s
			}
		}
	}

	if bg, ok := p["background"].(string); ok {
		pc.Background = bg
	}

	return pc
}

// buildStateContext extracts game state data
func buildStateContext(state *game.State) *StateContext {
	if state == nil {
		return &StateContext{}
	}

	sc := &StateContext{
		SystemName:  state.GetCurrentSystem(),
		SystemID:    state.System.ID,
		Location:    state.CurrentPOI,
		IsDocked:    state.IsDocked(),
		Fuel:        state.Fuel,
		MaxFuel:     state.MaxFuel,
		FuelPercent: (state.Fuel / state.MaxFuel) * 100,
		Hull:        state.Hull,
		MaxHull:     state.MaxHull,
		HullPercent: (state.Hull / state.MaxHull) * 100,
		CargoCount:  len(state.Ship.Cargo),
		MaxCargo:    state.MaxCargo,
		Credits:     state.Credits,
		Tick:        state.GetTick(),
	}

	// If docked, set the docked location
	if sc.IsDocked {
		sc.DockedAt = state.CurrentPOI
	}

	// Convert cargo to template-friendly format
	sc.Cargo = make([]CargoItem, len(state.Ship.Cargo))
	for i, item := range state.Ship.Cargo {
		sc.Cargo[i] = CargoItem{
			Type:     item.ItemID,
			Quantity: int(item.Quantity),
		}
	}

	return sc
}

// buildSystemContext creates system-level context
func buildSystemContext(state *game.State) *SystemContext {
	return &SystemContext{
		AvailableActions: getAvailableActions(),
		CurrentTick:      state.GetTick(),
	}
}

// getAvailableActions returns the list of available actions
func getAvailableActions() []ActionInfo {
	return []ActionInfo{
		{
			Name:           "undock",
			Description:    "Leave the current station",
			RequiresTarget: false,
			TargetType:     "none",
			Requirements:   "Must be docked",
		},
		{
			Name:           "dock",
			Description:    "Dock at the current POI",
			RequiresTarget: false,
			TargetType:     "none",
			Requirements:   "Must be at a POI, not already docked",
		},
		{
			Name:           "travel",
			Description:    "Travel to a POI in the current system",
			RequiresTarget: true,
			TargetType:     "poi_id",
			Requirements:   "Requires fuel",
		},
		{
			Name:           "jump",
			Description:    "Jump to another system",
			RequiresTarget: true,
			TargetType:     "system_name",
			Requirements:   "Requires fuel, must know connection",
		},
		{
			Name:           "mine",
			Description:    "Mine resources at current location",
			RequiresTarget: false,
			TargetType:     "none",
			Requirements:   "Must be at mineable location",
		},
		{
			Name:           "scan",
			Description:    "Scan the current area for POIs",
			RequiresTarget: false,
			TargetType:     "none",
			Requirements:   "None",
		},
		{
			Name:           "get_status",
			Description:    "Get player status information",
			RequiresTarget: false,
			TargetType:     "none",
			Requirements:   "None",
		},
		{
			Name:           "get_system",
			Description:    "Get current system information",
			RequiresTarget: false,
			TargetType:     "none",
			Requirements:   "None",
		},
		{
			Name:           "wait",
			Description:    "Wait and do nothing this turn",
			RequiresTarget: false,
			TargetType:     "none",
			Requirements:   "None",
		},
	}
}
