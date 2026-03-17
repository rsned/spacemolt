package tot

import (
	"context"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
)

// RunnerAdapter wraps an Evaluator to satisfy agent.ToTEvaluator.
// This bridges the circular import between pkg/agent and pkg/tot.
type RunnerAdapter struct {
	Eval     *Evaluator
	OnUpdate func(agentID string, eventType string, data any) // wired to runner.emitEvent
}

// EvaluateToT implements agent.ToTEvaluator.
func (a *RunnerAdapter) EvaluateToT(ctx context.Context, personality agent.Personality, state *game.State) (agent.Decision, any, error) {
	validActions := ValidActions(state)
	weights := DeriveWeights(personality)

	// Wire incremental updates through to the runner's event system
	if a.OnUpdate != nil {
		a.Eval.SetOnUpdate(func(tree *ThoughtTree) {
			a.OnUpdate(personality.ID, "thought_tree_update", tree)
		})
	}

	tree, err := a.Eval.Evaluate(ctx, personality, state, validActions, weights)
	if err != nil {
		return agent.Decision{}, nil, err
	}

	return tree.ToDecision(), tree, nil
}
