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
	Goal         *GoalContext
}

// PersonalityContext contains agent personality information
type PersonalityContext struct {
	Name        string
	Role        string
	Traits      []string
	Motivations []string
	Skills      []string
	Background  string
}

// CargoItem represents an item in cargo
type CargoItem struct {
	Type     string
	Quantity int
}

// NearbyPlayerInfo represents a nearby player
type NearbyPlayerInfo struct {
	Username   string
	ShipClass  string
	FactionTag string
	InCombat   bool
	ClanTag    string
}

// TravelProgressContext represents travel state
type TravelProgressContext struct {
	Progress    float64
	Destination string
	Type        string // "travel" or "jump"
	ArrivalTick int64
	ETA         int64 // Ticks remaining
}

// StateContext contains current game state information
type StateContext struct {
	SystemName  string
	SystemID    string
	Location    string // e.g., "Sol-Station-Alpha"
	IsDocked    bool
	DockedAt    string
	Fuel        float64
	MaxFuel     float64
	FuelPercent float64
	Hull        float64
	MaxHull     float64
	HullPercent float64
	Cargo       []CargoItem
	CargoCount  int
	MaxCargo    int
	Credits     float64
	Tick        int64

	// Combat & Tactical
	Shield         float64
	MaxShield      float64
	ShieldPercent  float64
	ShieldRecharge float64
	Armor          float64
	InCombat       bool
	NearbyPlayers  int
	NearbyHostiles int
	NearbyList     []NearbyPlayerInfo

	// Ship Technical
	CPUUsed       float64
	CPUCapacity   float64
	CPUPercent    float64
	PowerUsed     float64
	PowerCapacity float64
	PowerPercent  float64
	Speed         float64
	ShipClass     string
	Modules       []string

	// Cargo Details
	CargoUsed    float64
	CargoPercent float64

	// Travel & Location
	Traveling      bool
	TravelProgress *TravelProgressContext
	POIDescription string
	SystemSecurity string
	SystemEmpire   string
}

// KnowledgeContext contains what the agent knows
type KnowledgeContext struct {
	KnownSystems       []SystemInfo
	POIsInSystem       []POIInfo
	Connections        []string
	UnknownConnections []string
}

// SystemInfo represents knowledge about a system
type SystemInfo struct {
	ID          string
	Name        string
	PoliceLevel int
	Empire      string
	VisitCount  int
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
	RecentActions     []ActionRecord
	RecentExperiences []ExperienceRecord
}

// ActionRecord represents a past action
type ActionRecord struct {
	Action  string
	Target  string
	Success bool
	Message string
	Tick    int
}

// ExperienceRecord represents a past experience
type ExperienceRecord struct {
	Time        string
	Type        string
	Description string
	Outcome     string
	Location    string
}

// GoalContext contains the agent's current goal and priorities
type GoalContext struct {
	Type        string   // Goal type: "wealth", "skill", "exploration", "resource", "reputation"
	Target      string   // Specific target (e.g., "Mining_5", "10000_credits")
	Progress    float64  // Progress towards goal (0.0 to 1.0)
	Priority    int      // Priority level (1-10)
	Reasoning   string   // Why this goal was set
	Focus       string   // Current strategic focus
	Constraints []string // Active constraints
	Urgency     int      // Urgency level (1-10)
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
	Name           string
	Description    string
	RequiresTarget bool
	TargetType     string // "poi_id", "system_name", "none"
	Requirements   string // e.g., "Must be docked", "Requires fuel > 10%"
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
	goal *GoalContext,
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
		Goal:         goal,
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

		// Combat & Tactical
		Shield:         state.Ship.Shield,
		MaxShield:      state.Ship.MaxShield,
		ShieldRecharge: state.Ship.ShieldRecharge,
		Armor:          state.Ship.Armor,
		InCombat:       state.InCombat,
		NearbyPlayers:  len(state.Nearby),

		// Ship Technical
		CPUUsed:       state.Ship.CPUUsed,
		CPUCapacity:   state.Ship.CPUCapacity,
		PowerUsed:     state.Ship.PowerUsed,
		PowerCapacity: state.Ship.PowerCapacity,
		Speed:         state.Ship.Speed,
		ShipClass:     state.Ship.ClassID,
		Modules:       state.Ship.Modules,

		// Cargo Details
		CargoUsed: state.Ship.CargoUsed,

		// Travel & Location
		Traveling:    state.Traveling,
		SystemEmpire: state.System.Empire,
	}

	// Calculate percentages
	if sc.MaxShield > 0 {
		sc.ShieldPercent = (sc.Shield / sc.MaxShield) * 100
	}
	if sc.CPUCapacity > 0 {
		sc.CPUPercent = (sc.CPUUsed / sc.CPUCapacity) * 100
	}
	if sc.PowerCapacity > 0 {
		sc.PowerPercent = (sc.PowerUsed / sc.PowerCapacity) * 100
	}
	if state.Ship.CargoCapacity > 0 {
		sc.CargoPercent = (sc.CargoUsed / state.Ship.CargoCapacity) * 100
	}

	// Map security level
	switch state.System.PoliceLevel {
	case 0:
		sc.SystemSecurity = "None"
	case 1:
		sc.SystemSecurity = "Low"
	case 2:
		sc.SystemSecurity = "Medium"
	case 3:
		sc.SystemSecurity = "High"
	default:
		sc.SystemSecurity = "Unknown"
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

	// Process nearby players
	sc.NearbyList = make([]NearbyPlayerInfo, 0, len(state.Nearby))
	nearbyHostiles := 0
	for _, p := range state.Nearby {
		sc.NearbyList = append(sc.NearbyList, NearbyPlayerInfo{
			Username:   p.Username,
			ShipClass:  p.ShipClass,
			FactionTag: p.FactionTag,
			InCombat:   p.InCombat,
			ClanTag:    p.ClanTag,
		})
		// Count hostiles (in combat or different faction)
		if p.InCombat {
			nearbyHostiles++
		}
	}
	sc.NearbyHostiles = nearbyHostiles

	// Add travel progress if traveling
	if state.Traveling && state.TravelProgress != nil {
		tp := state.TravelProgress
		eta := tp.ArrivalTick - state.CurrentTick
		if eta < 0 {
			eta = 0
		}
		sc.TravelProgress = &TravelProgressContext{
			Progress:    tp.Progress,
			Destination: tp.Destination,
			Type:        tp.Type,
			ArrivalTick: tp.ArrivalTick,
			ETA:         eta,
		}
	}

	// Find current POI description
	for _, poi := range state.System.POIs {
		if poi.ID == state.CurrentPOI {
			sc.POIDescription = poi.Description
			break
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
