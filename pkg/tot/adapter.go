package tot

import (
	"context"
	"strings"

	"github.com/rsned/spacemolt/pkg/agent"
)

// RunnerAdapter wraps an Evaluator to satisfy agent.ToTEvaluator.
// This bridges the circular import between pkg/agent and pkg/tot.
type RunnerAdapter struct {
	Eval     *Evaluator
	OnUpdate func(agentID string, eventType string, data any) // wired to runner.emitEvent
}

// EvaluateToT implements agent.ToTEvaluator.
func (a *RunnerAdapter) EvaluateToT(ctx context.Context, personality agent.Personality, es agent.EnrichedState, totCtx *agent.ToTContext) (agent.Decision, any, error) {
	state := es.GameState()
	validActions := ValidActions(state)
	weights := DeriveWeights(personality)

	// Build enriched prompt context — prefer enriched state accessors when available.
	pctx := buildPromptContext(personality, es, totCtx)

	// Wire incremental updates through to the runner's event system
	if a.OnUpdate != nil {
		a.Eval.SetOnUpdate(func(tree *ThoughtTree) {
			a.OnUpdate(personality.ID, "thought_tree_update", tree)
		})
	}

	tree, err := a.Eval.Evaluate(ctx, personality, state, validActions, weights, pctx)
	if err != nil {
		return agent.Decision{}, nil, err
	}

	return tree.ToDecision(), tree, nil
}

// buildPromptContext assembles the enriched prompt context from runner data.
func buildPromptContext(p agent.Personality, es agent.EnrichedState, totCtx *agent.ToTContext) *PromptContext {
	state := es.GameState()
	pctx := &PromptContext{}

	// Short-term memory
	if totCtx != nil {
		pctx.RecentActions = totCtx.RecentActions
		pctx.Goal = totCtx.Goal
		pctx.Priority = totCtx.Priority
	}

	// Biography excerpt — first 2 sentences or 200 chars, whichever is shorter
	if p.Biography != "" {
		pctx.BiographyExcerpt = excerptBiography(p.Biography, 200)
	}

	// System security — prefer enriched state over raw police level.
	pctx.SystemSecurity = es.SystemSecurity()

	// Enriched connected system info from game state connections.
	for _, conn := range state.System.Connections {
		pctx.ConnectedSystemInfo = append(pctx.ConnectedSystemInfo, ConnectedSystem{
			ID:       conn.SystemID,
			Name:     conn.Name,
			Security: "Unknown", // Will be enriched when agentstate provides neighbor data
			Distance: conn.Distance,
		})
	}

	return pctx
}

func excerptBiography(bio string, maxLen int) string {
	// Take first two sentences
	sentences := 0
	for i, c := range bio {
		if c == '.' || c == '!' || c == '?' {
			sentences++
			if sentences >= 2 || i >= maxLen {
				return strings.TrimSpace(bio[:i+1])
			}
		}
	}
	if len(bio) > maxLen {
		// Find last space before maxLen
		idx := strings.LastIndex(bio[:maxLen], " ")
		if idx > 0 {
			return bio[:idx] + "..."
		}
		return bio[:maxLen] + "..."
	}
	return bio
}

