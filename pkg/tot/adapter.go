package tot

import (
	"context"
	"strings"

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
func (a *RunnerAdapter) EvaluateToT(ctx context.Context, personality agent.Personality, state *game.State, totCtx *agent.ToTContext) (agent.Decision, any, error) {
	validActions := ValidActions(state)
	weights := DeriveWeights(personality)

	// Build enriched prompt context from runner data
	pctx := buildPromptContext(personality, state, totCtx)

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
func buildPromptContext(p agent.Personality, state *game.State, totCtx *agent.ToTContext) *PromptContext {
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

	// System security
	pctx.SystemSecurity = securityLabel(state.System.PoliceLevel)

	// Enriched connected system info
	for _, conn := range state.System.Connections {
		pctx.ConnectedSystemInfo = append(pctx.ConnectedSystemInfo, ConnectedSystem{
			ID:       conn.SystemID,
			Name:     conn.Name,
			Security: "Unknown", // We don't have security for neighboring systems in State
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

func securityLabel(policeLevel int) string {
	switch policeLevel {
	case 0:
		return "Lawless"
	case 1:
		return "Low Security"
	case 2:
		return "Medium Security"
	case 3:
		return "High Security"
	default:
		return "Unknown"
	}
}
