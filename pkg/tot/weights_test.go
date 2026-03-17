package tot

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/agent"
)

func TestDeriveWeights_Miner(t *testing.T) {
	p := agent.Personality{
		Name: "Miner",
		Role: "miner",
		Traits: map[string]float64{
			"caution":         0.5,
			"risk_tolerance":  0.5,
		},
		Motivations: agent.Motivations{
			Primary: "mine_resources",
			Weights: map[string]float64{
				"mine_resources": 0.9,
				"survival":       0.65,
			},
		},
	}

	w := DeriveWeights(p)

	if w.Survival < 0.4 || w.Survival > 0.8 {
		t.Errorf("Survival = %v, want in [0.4, 0.8]", w.Survival)
	}
	if w.Profit < 0.5 {
		t.Errorf("Profit = %v, want >= 0.5", w.Profit)
	}
	for name, val := range map[string]float64{
		"Survival":     w.Survival,
		"Profit":       w.Profit,
		"GoalProgress": w.GoalProgress,
		"Risk":         w.Risk,
		"Efficiency":   w.Efficiency,
	} {
		if val < 0 || val > 1 {
			t.Errorf("%s = %v, want in [0, 1]", name, val)
		}
	}
}

func TestDeriveWeights_Pirate(t *testing.T) {
	p := agent.Personality{
		Name: "Pirate",
		Role: "pirate",
		Traits: map[string]float64{
			"aggression":      0.9,
			"risk_tolerance":  0.8,
			"greed":           0.85,
		},
		Motivations: agent.Motivations{
			Primary: "plunder",
			Weights: map[string]float64{
				"plunder":  0.9,
				"survival": 0.6,
			},
		},
	}

	w := DeriveWeights(p)

	if w.Survival >= 0.5 {
		t.Errorf("Survival = %v, want < 0.5 for high-risk pirate", w.Survival)
	}
	if w.Risk >= 0.4 {
		t.Errorf("Risk = %v, want < 0.4 for high-aggression/high-risk-tolerance pirate", w.Risk)
	}
	for name, val := range map[string]float64{
		"Survival":     w.Survival,
		"Profit":       w.Profit,
		"GoalProgress": w.GoalProgress,
		"Risk":         w.Risk,
		"Efficiency":   w.Efficiency,
	} {
		if val < 0 || val > 1 {
			t.Errorf("%s = %v, want in [0, 1]", name, val)
		}
	}
}

func TestWeightedScore(t *testing.T) {
	w := AxisWeights{
		Survival:     0.5,
		Profit:       0.5,
		GoalProgress: 0.5,
		Risk:         0.5,
		Efficiency:   0.5,
	}
	s := AxisScores{
		Survival:     80,
		Profit:       60,
		GoalProgress: 40,
		Risk:         70,
		Efficiency:   50,
	}

	got := w.WeightedScore(s)
	want := 60.0
	if got != want {
		t.Errorf("WeightedScore = %v, want %v", got, want)
	}
}

func TestWeightedScore_ZeroWeights(t *testing.T) {
	w := AxisWeights{}
	s := AxisScores{
		Survival:     80,
		Profit:       60,
		GoalProgress: 40,
		Risk:         70,
		Efficiency:   50,
	}

	got := w.WeightedScore(s)
	if got != 0 {
		t.Errorf("WeightedScore with zero weights = %v, want 0", got)
	}
}
