package agent

import (
	"context"

	"github.com/rsned/spacemolt/pkg/game"
)

// EnrichedState provides enriched game state for agent decision cycles.
// The primary implementation is *agentstate.AgentState which layers KB data,
// action space evaluation, and agent context on top of live game state.
// When no enriched state is available, rawEnrichedState provides a minimal
// fallback wrapping a cloned *game.State.
type EnrichedState interface {
	// Refresh rebuilds enrichment layers from current game state and KB.
	// Called once per decision cycle.
	Refresh(ctx context.Context)

	// GameState returns the underlying game state pointer.
	GameState() *game.State

	// SystemSecurity returns a human-readable security label for the current system.
	SystemSecurity() string

	// CanDo returns whether a specific action is valid given current state.
	CanDo(action string) bool
}

// Agent represents an autonomous game-playing agent
type Agent interface {
	// Identity
	Name() string
	ID() string

	// Personality
	Personality() Personality

	// Decision Making
	Decide(ctx context.Context, es EnrichedState) (Decision, error)

	// Learning
	Learn(result ActionResult) error
	Memory() Memory

	// Tactical Action Queue
	EnqueueActions(actions []PlannedAction)
	DequeueAction() (*PlannedAction, bool)
	GetActionQueue() []PlannedAction
	ClearActionQueue(reason string)
	SetUsingQueuedAction(using bool)
	IsUsingQueuedAction() bool

	// Route Home
	SetRouteHome(route []game.RouteStep, fromSystem string)
	GetRouteHome() ([]game.RouteStep, string)

	// Lifecycle
	Start(ctx context.Context) error
	Stop() error
	Status() Status
}

// Personality defines an agent's traits and motivations
type Personality struct {
	Name        string             `yaml:"name"`
	ID          string             `yaml:"id"`
	Role        string             `yaml:"role"`
	Traits      map[string]float64 `yaml:"traits"`
	Skills      map[string]string  `yaml:"skills"`
	Motivations Motivations        `yaml:"motivations"`
	Biography   string             `yaml:"biography"`
	Faction     string             `yaml:"faction,omitempty"`
	ServiceName     string             `yaml:"service_name,omitempty" json:"service_name,omitempty"`
	PrimarySkill    string             `yaml:"primary_skill,omitempty" json:"primary_skill,omitempty"`
	GameSkills      []string           `yaml:"game_skills,omitempty" json:"game_skills,omitempty"`
	BackgroundSkill string             `yaml:"background_skill,omitempty" json:"background_skill,omitempty"`
	DecisionMode    string             `yaml:"decision_mode,omitempty" json:"decision_mode,omitempty"`
}

// Motivations drives agent behavior
type Motivations struct {
	Primary   string             `yaml:"primary"`
	Secondary string             `yaml:"secondary"`
	Tertiary  string             `yaml:"tertiary"`
	Weights   map[string]float64 `yaml:"weights,omitempty"`
}

// PlannedAction represents a future action in the tactical queue
type PlannedAction struct {
	Sequence   int               `json:"sequence"`             // 1-5, order in the plan
	Action     string            `json:"action"`               // Action name (travel, mine, wait, etc.)
	Target     string            `json:"target,omitempty"`     // Target for the action (POI ID, system name)
	Parameters map[string]string `json:"parameters,omitempty"` // Additional parameters
	Reasoning  string            `json:"reasoning"`            // Why this step is needed
	Condition  string            `json:"condition,omitempty"`  // Condition for execution ("after_arrival", "if_cargo_full")
}

// Decision represents a chosen action
type Decision struct {
	Action         string          `json:"action"`
	Target         string          `json:"target,omitempty"`
	Reasoning      string          `json:"reasoning"`
	Confidence     float64         `json:"confidence,omitempty"`
	Alternatives   []string        `json:"alternatives,omitempty"`
	PlannedActions []PlannedAction `json:"planned_actions,omitempty"` // NEW: 5-step tactical plan
}

// ActionResult represents the result of taking an action
type ActionResult struct {
	Action   string // The action that was taken
	Target   string // The target of the action (if any)
	Success  bool
	Message  string
	NewState *game.State
	Reward   float64 // For reinforcement learning
	Error    error
}

// Goal represents a strategic objective for an agent
type Goal struct {
	Type      string  // Goal type: "wealth", "skill", "exploration", "resource", "reputation"
	Target    string  // Specific target (e.g., "Mining_5", "10000_credits", "Sol", "iron")
	Progress  float64 // Progress towards goal (0.0 to 1.0)
	Priority  int     // Priority level (1-10, higher is more important)
	Reasoning string  // Why this goal was set
}

// Priority represents the agent's current strategic focus and constraints
type Priority struct {
	Focus       string   // Current strategic focus (e.g., "mining", "trading", "exploring", "combat")
	Constraints []string // Active constraints preventing certain actions (e.g., "low_fuel", "cargo_full", "no_credits")
	Urgency     int      // Urgency level (1-10, higher means more urgent action needed)
}

// Status represents the current status of an agent
type Status struct {
	State         AgentState
	CurrentAction string
	LastUpdate    string
	Error         error
}

type AgentState int

const (
	AgentStateIdle AgentState = iota
	AgentStateDeciding
	AgentStateActing
	AgentStateWaiting
	AgentStateError
	AgentStateStopped
)

// String returns the string representation of AgentState
func (s AgentState) String() string {
	switch s {
	case AgentStateIdle:
		return "Idle"
	case AgentStateDeciding:
		return "Deciding"
	case AgentStateActing:
		return "Acting"
	case AgentStateWaiting:
		return "Waiting"
	case AgentStateError:
		return "Error"
	case AgentStateStopped:
		return "Stopped"
	default:
		return "Unknown"
	}
}

// Memory stores agent knowledge
type Memory interface {
	// Knowledge access
	KnownSystems() []game.SystemData
	KnownPOIs(systemID string) []game.POI
	GetUnknownConnections(systemID string) ([]string, error)

	// Memory update
	RememberSystem(ctx context.Context, system game.SystemData) error
	RememberPOI(ctx context.Context, poi game.POI) error
	RememberConnection(ctx context.Context, fromSystem, toSystem string) error

	// Experience
	AddExperience(ctx context.Context, exp Experience) error
	GetRecentExperiences(count int) ([]Experience, error)
}

// Experience represents a significant event in the agent's history
type Experience struct {
	Time        string
	Type        string
	Description string
	Outcome     string
	Location    string
}

// ResourceInfo represents resource data at a POI
type ResourceInfo struct {
	ResourceID string
	Richness   float64
	Remaining  float64
}
