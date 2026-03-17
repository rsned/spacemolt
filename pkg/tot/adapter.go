package tot

import (
	"context"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
)

// RunnerAdapter wraps an Evaluator to satisfy agent.ToTEvaluator.
// This bridges the circular import between pkg/agent and pkg/tot.
type RunnerAdapter struct {
	Eval *Evaluator
}

// EvaluateToT implements agent.ToTEvaluator.
func (a *RunnerAdapter) EvaluateToT(ctx context.Context, personality agent.Personality, state *game.State) (agent.Decision, any, error) {
	validActions := ValidActions(state)
	weights := DeriveWeights(personality)

	tree, err := a.Eval.Evaluate(ctx, personality, state, validActions, weights)
	if err != nil {
		return agent.Decision{}, nil, err
	}

	return tree.ToDecision(), tree, nil
}
