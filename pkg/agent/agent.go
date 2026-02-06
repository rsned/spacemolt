package agent

import (
	"context"
	"github.com/rsned/spacemolt/pkg/game"
)

// SystemKnowledge represents knowledge about a solar system
type SystemKnowledge struct {
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

// Position represents 3D coordinates
type Position struct {
	X float64
	Y float64
	Z float64
}

// POIKnowledge represents knowledge about a Point of Interest
type POIKnowledge struct {
	ID          string
	SystemID    string
	Name        string
	Type        string
	Description string
	Position    Position
	Services    []string
	Resources   []ResourceInfo
}

// Agent represents an autonomous game-playing agent
type Agent interface {
	// Identity
	Name() string
	ID() string

	// Personality
	Personality() Personality

	// Decision Making
	Decide(ctx context.Context, state *game.State) (Decision, error)

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
	KnownSystems() []SystemKnowledge
	KnownPOIs(systemID string) []POIKnowledge
	GetUnknownConnections(systemID string) ([]string, error)

	// Memory update
	RememberSystem(ctx context.Context, system System) error
	RememberPOI(ctx context.Context, poi POI) error
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

// System represents a system for memory storage
type System struct {
	ID            string
	Name          string
	Position      Position
	SecurityLevel string
	Faction       string
	Connections   []string
	DiscoveredBy  string
}

// ResourceInfo represents resource data at a POI
type ResourceInfo struct {
	ResourceID string
	Richness   float64
	Remaining  float64
}

// POI represents a POI for memory storage
type POI struct {
	ID           string
	SystemID     string
	Name         string
	Type         string
	Position     Position
	Services     []string
	Resources    []ResourceInfo
	DiscoveredBy string
}
