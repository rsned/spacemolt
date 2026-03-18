package tot

import "github.com/rsned/spacemolt/pkg/agent"

// PromptContext carries additional context beyond personality and game state
// that enriches the ToT prompts with memory, goals, and strategic context.
type PromptContext struct {
	// RecentActions is the last N actions taken (most recent first).
	RecentActions []agent.HistoryEntry

	// Goal is the agent's current strategic goal.
	Goal *agent.Goal

	// Priority is the agent's current priority and constraints.
	Priority *agent.Priority

	// BiographyExcerpt is a short excerpt from the personality biography
	// that captures the agent's character and decision-making style.
	BiographyExcerpt string

	// SystemSecurity is the security level of the current system
	// ("High Security", "Medium Security", "Low Security", "Lawless").
	SystemSecurity string

	// QueuedPlan is the current action queue (from a previous ToT evaluation).
	// Shown in the prompt so the LLM knows what's already planned.
	QueuedPlan []agent.PlannedAction

	// ConnectedSystemInfo provides enriched info about neighboring systems.
	ConnectedSystemInfo []ConnectedSystem
}

// ConnectedSystem holds enriched info about a system reachable by jump.
type ConnectedSystem struct {
	ID       string
	Name     string
	Security string // "High Security", "Medium Security", "Low Security", "Lawless"
	Distance int
	Notes    string // e.g., "has asteroid belts", "visited before", "unknown"
}
