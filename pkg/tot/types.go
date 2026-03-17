package tot

import "time"

// NodeStatus represents the state of a thought node in the tree.
type NodeStatus string

const (
	StatusActive NodeStatus = "active"
	StatusPruned NodeStatus = "pruned"
	StatusWinner NodeStatus = "winner"
)

// AxisScores holds multi-criteria evaluation scores (0-100 scale).
type AxisScores struct {
	Survival     float64 `json:"survival"`
	Profit       float64 `json:"profit"`
	GoalProgress float64 `json:"goal_progress"`
	Risk         float64 `json:"risk"`
	Efficiency   float64 `json:"efficiency"`
}

// AxisWeights holds personality-derived weights for scoring (0-1 scale).
type AxisWeights struct {
	Survival     float64 `json:"survival"`
	Profit       float64 `json:"profit"`
	GoalProgress float64 `json:"goal_progress"`
	Risk         float64 `json:"risk"`
	Efficiency   float64 `json:"efficiency"`
}

// WeightedScore computes the combined score for an AxisScores using these weights.
func (w AxisWeights) WeightedScore(s AxisScores) float64 {
	total := w.Survival + w.Profit + w.GoalProgress + w.Risk + w.Efficiency
	if total == 0 {
		return 0
	}
	raw := s.Survival*w.Survival + s.Profit*w.Profit +
		s.GoalProgress*w.GoalProgress + s.Risk*w.Risk +
		s.Efficiency*w.Efficiency
	return raw / total
}

// ThoughtNode represents one option in the decision tree.
type ThoughtNode struct {
	ID          string         `json:"id"`
	Action      string         `json:"action"`
	Target      string         `json:"target"`
	Reasoning   string         `json:"reasoning"`
	Scores      AxisScores     `json:"scores"`
	Combined    float64        `json:"combined"`
	Status      NodeStatus     `json:"status"`
	Children    []*ThoughtNode `json:"children,omitempty"`
	Depth       int            `json:"depth"`
	EvalTime    time.Duration  `json:"eval_time_ms"`
	Prompt      string         `json:"prompt,omitempty"`
	RawResponse string         `json:"raw_response,omitempty"`
}

// ThoughtTree holds the complete decision tree for one cycle.
type ThoughtTree struct {
	ID        string         `json:"id"`
	AgentID   string         `json:"agent_id"`
	Timestamp time.Time      `json:"timestamp"`
	Situation string         `json:"situation"`
	Root      []*ThoughtNode `json:"root"`
	WinnerID  string         `json:"winner_id"`
	Duration  time.Duration  `json:"duration_ms"`
	Model     string         `json:"model"`
	Weights   AxisWeights    `json:"weights"`
}

// ActionOption represents a valid action the agent can take right now.
type ActionOption struct {
	Action      string   `json:"action"`
	Description string   `json:"description"`
	Targets     []string `json:"targets,omitempty"`
}
