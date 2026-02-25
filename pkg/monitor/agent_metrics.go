package monitor

import "time"

// AgentMetric holds per-agent runtime metrics.
type AgentMetric struct {
	AgentID       string        `json:"agent_id"`
	Type          string        `json:"type"` // "llm" or "strategy"
	Strategy      string        `json:"strategy,omitempty"`
	Status        string        `json:"status"`
	ActionsTotal  int64         `json:"actions_total"`
	ErrorsTotal   int64         `json:"errors_total"`
	LastAction    time.Time     `json:"last_action,omitzero"`
	LLMLatency    time.Duration `json:"llm_latency_ms,omitzero"`
	Team          string        `json:"team,omitempty"`
	System        string        `json:"system,omitempty"`
	Docked        bool          `json:"docked"`
}
