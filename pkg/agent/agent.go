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
	Resources   []string
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

	// Lifecycle
	Start(ctx context.Context) error
	Stop() error
	Status() Status
}

// Personality defines an agent's traits and motivations
type Personality struct {
	Name        string            `yaml:"name"`
	ID          string            `yaml:"id"`
	Role        string            `yaml:"role"`
	Traits      map[string]float64 `yaml:"traits"`
	Skills      map[string]string `yaml:"skills"`
	Motivations Motivations       `yaml:"motivations"`
	Biography   string            `yaml:"biography"`
	Faction     string            `yaml:"faction,omitempty"`
}

// Motivations drives agent behavior
type Motivations struct {
	Primary   string                 `yaml:"primary"`
	Secondary string                 `yaml:"secondary"`
	Tertiary  string                 `yaml:"tertiary"`
	Weights   map[string]float64     `yaml:"weights,omitempty"`
}

// Decision represents a chosen action
type Decision struct {
	Action      string
	Target      string
	Reasoning   string
	Confidence  float64
	Alternatives []string
}

// ActionResult represents the result of taking an action
type ActionResult struct {
	Action      string  // The action that was taken
	Target      string  // The target of the action (if any)
	Success     bool
	Message     string
	NewState    *game.State
	Reward      float64 // For reinforcement learning
	Error       error
}

// Status represents the current status of an agent
type Status struct {
	State       AgentState
	CurrentAction string
	LastUpdate  string
	Error       error
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

// POI represents a POI for memory storage
type POI struct {
	ID            string
	SystemID      string
	Name          string
	Type          string
	Position      Position
	Services      []string
	Resources     []string
	DiscoveredBy  string
}
