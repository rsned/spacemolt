package tot

import "github.com/rsned/spacemolt/pkg/agent"

// profitMotivation returns the motivation key most associated with profit for the given personality.
// It checks known profit-related keys in order and returns the first found, falling back to Primary.
func profitMotivation(p agent.Personality) string {
	for _, key := range []string{"mine_resources", "maximize_profit", "plunder", "complete_contracts", "explore_unknown"} {
		if _, ok := p.Motivations.Weights[key]; ok {
			return key
		}
	}
	return p.Motivations.Primary
}

// trait returns the value of a named trait from the personality, defaulting to 0.5 if absent.
func trait(p agent.Personality, name string) float64 {
	if v, ok := p.Traits[name]; ok {
		return v
	}
	return 0.5
}

// blend averages the non-zero values among the provided inputs, clamped to [0, 1].
// If all values are zero, it returns 0.5.
func blend(values ...float64) float64 {
	sum := 0.0
	count := 0
	for _, v := range values {
		if v != 0 {
			sum += v
			count++
		}
	}
	if count == 0 {
		return 0.5
	}
	result := sum / float64(count)
	if result < 0 {
		return 0
	}
	if result > 1 {
		return 1
	}
	return result
}

// DeriveWeights computes AxisWeights from an agent's personality traits and motivations.
// All returned weights are in the [0, 1] range.
func DeriveWeights(p agent.Personality) AxisWeights {
	return AxisWeights{
		Survival:     blend(p.Motivations.Weights["survival"], trait(p, "caution"), 1.0-trait(p, "risk_tolerance")),
		Profit:       blend(p.Motivations.Weights[profitMotivation(p)], trait(p, "greed"), trait(p, "ambition")),
		GoalProgress: blend(p.Motivations.Weights[p.Motivations.Primary], trait(p, "determination"), trait(p, "perseverance")),
		Risk:         blend(1.0-trait(p, "risk_tolerance"), trait(p, "caution"), 1.0-trait(p, "aggression")),
		Efficiency:   blend(1.0-trait(p, "patience"), trait(p, "discipline"), 0.5),
	}
}
