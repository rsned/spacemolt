package agentstate

import (
	"maps"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// StateSnapshot is a fully serializable copy of the unified state.
// Used for prompt generation, observer broadcasts, logging, and debugging.
type StateSnapshot struct {
	// Timing.
	Tick      int64     `json:"tick"`
	Refreshed time.Time `json:"refreshed"`

	// Player.
	Username string  `json:"username"`
	Empire   string  `json:"empire"`
	Credits  float64 `json:"credits"`

	// Location.
	CurrentSystem  string `json:"current_system"`
	CurrentPOI     string `json:"current_poi"`
	CurrentPOIType string `json:"current_poi_type"`
	Docked         bool   `json:"docked"`
	Traveling      bool   `json:"traveling"`
	SystemSecurity string `json:"system_security"`

	// Ship.
	ShipClass     string           `json:"ship_class"`
	ShipName      string           `json:"ship_name"`
	Fuel          float64          `json:"fuel"`
	MaxFuel       float64          `json:"max_fuel"`
	Hull          float64          `json:"hull"`
	MaxHull       float64          `json:"max_hull"`
	CargoUsed     float64          `json:"cargo_used"`
	CargoCapacity float64          `json:"cargo_capacity"`
	Cargo         []game.CargoItem `json:"cargo"`

	// Combat.
	InCombat   bool   `json:"in_combat"`
	InBattle   bool   `json:"in_battle"`
	PirateName string `json:"pirate_name,omitempty"`
	PirateTier string `json:"pirate_tier,omitempty"`

	// Enrichment.
	Neighbors         []EnrichedNeighbor            `json:"neighbors"`
	SystemDangerLevel int                            `json:"system_danger_level"`
	ResourceDepletion []knowledge.DepletingResource  `json:"resource_depletion,omitempty"`
	FuelToNeighbors   map[string]float64             `json:"fuel_to_neighbors"`

	// Actions.
	ValidActionCount int      `json:"valid_action_count"`
	ValidActionNames []string `json:"valid_action_names"`

	// Agent context (omitted if no agent attached).
	Goal          *agent.Goal           `json:"goal,omitempty"`
	Focus         string                `json:"focus,omitempty"`
	Constraints   []string              `json:"constraints,omitempty"`
	QueuedPlan    []agent.PlannedAction `json:"queued_plan,omitempty"`
	RecentActions []agent.HistoryEntry  `json:"recent_actions,omitempty"`
}

// Snapshot produces a flat, JSON-serializable copy of the full unified state.
func (s *AgentState) Snapshot() StateSnapshot {
	state := s.game.Clone()

	snap := StateSnapshot{
		Tick:      state.CurrentTick,
		Refreshed: s.lastRefresh,

		Username: state.Player.Username,
		Empire:   state.Player.Empire,
		Credits:  state.Credits,

		CurrentSystem:  state.CurrentSystem,
		CurrentPOI:     state.CurrentPOI,
		CurrentPOIType: s.enriched.CurrentPOIType,
		Docked:         state.Doc,
		Traveling:      state.Traveling,
		SystemSecurity: s.enriched.SystemSecurity,

		ShipClass:     state.Ship.ClassID,
		ShipName:      state.Ship.Name,
		Fuel:          state.Ship.Fuel,
		MaxFuel:       state.Ship.MaxFuel,
		Hull:          state.Ship.Hull,
		MaxHull:       state.Ship.MaxHull,
		CargoUsed:     state.Ship.CargoUsed,
		CargoCapacity: state.Ship.CargoCapacity,
		Cargo:         state.Ship.Cargo,

		InCombat:   state.InCombat,
		InBattle:   state.InBattle,
		PirateName: state.PirateName,
		PirateTier: state.PirateTier,

		Neighbors:         s.enriched.Neighbors,
		ResourceDepletion: s.enriched.ResourceDepletion,
	}

	// Danger level.
	if s.enriched.SystemDanger != nil {
		snap.SystemDangerLevel = s.enriched.SystemDanger.DangerLevel
	}

	// Fuel map copy.
	if len(s.enriched.FuelToNeighbors) > 0 {
		snap.FuelToNeighbors = make(map[string]float64, len(s.enriched.FuelToNeighbors))
		maps.Copy(snap.FuelToNeighbors, s.enriched.FuelToNeighbors)
	}

	// Valid action names.
	snap.ValidActionCount = len(s.actions.ValidActions)
	snap.ValidActionNames = make([]string, len(s.actions.ValidActions))
	for i, a := range s.actions.ValidActions {
		snap.ValidActionNames[i] = a.Action.Name
	}

	// Agent context.
	if s.agent != nil {
		snap.Goal = s.agent.Goal
		snap.RecentActions = s.agent.RecentActions
		snap.QueuedPlan = s.agent.QueuedPlan
		if s.agent.Priority != nil {
			snap.Focus = s.agent.Priority.Focus
			snap.Constraints = s.agent.Priority.Constraints
		}
	}

	return snap
}
