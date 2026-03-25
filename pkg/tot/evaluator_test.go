package tot

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
)

// mockLLM returns responses keyed by a substring of the prompt.
// If no key matches, it falls back to the sequential responses list.
type mockLLM struct {
	mu        sync.Mutex
	responses []string
	idx       int
	// keyed maps a prompt substring to a fixed response.
	keyed map[string]string
}

func (m *mockLLM) Generate(_ context.Context, prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.keyed {
		if len(prompt) >= len(k) {
			for i := 0; i <= len(prompt)-len(k); i++ {
				if prompt[i:i+len(k)] == k {
					return v, nil
				}
			}
		}
	}
	if m.idx >= len(m.responses) {
		return m.responses[len(m.responses)-1], nil
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func TestEvaluator_FullPipeline(t *testing.T) {
	assessResp := AssessResponse{
		Situation: "Mining in low-sec, pirates nearby",
		Options: []AssessOption{
			{Action: "travel", Target: "jump_gate_01", Rationale: "Flee to safety"},
			{Action: "mine", Target: "", Rationale: "Keep mining"},
		},
	}
	assessJSON, _ := json.Marshal(assessResp)

	evalResp1 := EvaluateResponse{
		Action:   "travel",
		Target:   "jump_gate_01",
		Analysis: "Fleeing preserves cargo",
		Scores:   AxisScores{Survival: 90, Profit: 40, GoalProgress: 30, Risk: 85, Efficiency: 35},
	}
	evalResp1.NextStep.Action = "jump"
	evalResp1.NextStep.Target = "safe_sys"
	evalJSON1, _ := json.Marshal(evalResp1)

	evalResp2 := EvaluateResponse{
		Action:   "mine",
		Analysis: "Risky but profitable",
		Scores:   AxisScores{Survival: 30, Profit: 80, GoalProgress: 70, Risk: 20, Efficiency: 60},
	}
	evalJSON2, _ := json.Marshal(evalResp2)

	// The assess call has no option-specific keyword; eval calls include the action name in the prompt.
	// Use keyed responses so parallel goroutines get the correct eval JSON regardless of ordering.
	llm := &mockLLM{
		responses: []string{string(assessJSON)},
		keyed: map[string]string{
			"You are considering: travel": string(evalJSON1),
			"You are considering: mine":   string(evalJSON2),
		},
	}

	personality := agent.Personality{
		Name: "Test Miner",
		ID:   "miner-test",
		Role: "Miner",
		Traits: map[string]float64{
			"caution":        0.7,
			"risk_tolerance": 0.3,
		},
		Motivations: agent.Motivations{
			Primary: "mine_resources",
			Weights: map[string]float64{
				"mine_resources": 0.9,
				"survival":       0.7,
			},
		},
	}

	weights := DeriveWeights(personality)
	eval := NewEvaluator(llm, "test-model")

	state := &game.State{
		System: game.SystemData{Name: "Test System", ID: "test_sys"},
	}
	validActions := []ActionOption{
		{Action: "travel", Description: "Travel"},
		{Action: "mine", Description: "Mine"},
		{Action: "jump", Description: "Jump"},
	}

	tree, err := eval.Evaluate(context.Background(), personality, state, validActions, weights, nil)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if tree.WinnerID == "" {
		t.Error("expected a winner to be selected")
	}
	if len(tree.Root) != 2 {
		t.Errorf("expected 2 root branches, got %d", len(tree.Root))
	}

	// With high survival weights, travel should win
	var winner *ThoughtNode
	for _, n := range tree.Root {
		if n.Status == StatusWinner {
			winner = n
		}
	}
	if winner == nil {
		t.Fatal("no winner node found")
	}
	if winner.Action != "travel" {
		t.Errorf("expected travel to win for cautious miner, got %s", winner.Action)
	}

	// Test ToDecision
	d := tree.ToDecision()
	if d.Action != "travel" {
		t.Errorf("ToDecision action = %s, want travel", d.Action)
	}
	if d.Confidence <= 0 || d.Confidence > 1.0 {
		t.Errorf("ToDecision confidence = %.2f, should be in (0, 1]", d.Confidence)
	}
	if len(d.PlannedActions) != 1 || d.PlannedActions[0].Action != "jump" {
		t.Errorf("expected 1 planned action (jump), got %v", d.PlannedActions)
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"clean json", `{"a":"b"}`, `{"a":"b"}`},
		{"with prefix", `here is the json: {"a":"b"}`, `{"a":"b"}`},
		{"multiple objects picks last", `{"first":"one"} ok {"second":"two"}`, `{"second":"two"}`},
		{"nested braces", `{"a":{"b":"c"}}`, `{"a":{"b":"c"}}`},
		{"no json", "no json here", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSelectWinner_EmptyTree(t *testing.T) {
	tree := &ThoughtTree{}
	selectWinner(tree)
	if tree.WinnerID != "" {
		t.Error("empty tree should have no winner")
	}
}

func TestToDecision_NoWinner(t *testing.T) {
	tree := &ThoughtTree{}
	d := tree.ToDecision()
	if d.Action != "get_status" {
		t.Errorf("no-winner fallback action = %s, want get_status", d.Action)
	}
}
