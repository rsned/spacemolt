package tot

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
)

// LLMGenerator is the interface for making LLM calls.
type LLMGenerator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// Evaluator orchestrates the three-stage Thought Engine pipeline.
type Evaluator struct {
	llm   LLMGenerator
	model string
}

// NewEvaluator creates a new Evaluator with the given LLM generator and model name.
func NewEvaluator(llm LLMGenerator, model string) *Evaluator {
	return &Evaluator{llm: llm, model: model}
}

// Evaluate runs the full pipeline: Assess → Evaluate (parallel) → Select.
func (e *Evaluator) Evaluate(
	ctx context.Context,
	personality agent.Personality,
	state *game.State,
	validActions []ActionOption,
	weights AxisWeights,
) (*ThoughtTree, error) {
	start := time.Now()
	tree := &ThoughtTree{
		ID:        fmt.Sprintf("tot_%d", start.UnixMilli()),
		AgentID:   personality.ID,
		Timestamp: start,
		Model:     e.model,
		Weights:   weights,
	}

	// Stage 1: Assess
	assessPrompt := BuildAssessPrompt(personality, state, validActions)
	assessRaw, err := e.llm.Generate(ctx, assessPrompt)
	if err != nil {
		return nil, fmt.Errorf("stage 1 assess: %w", err)
	}

	assessed, err := parseAssessResponse(assessRaw)
	if err != nil {
		return nil, fmt.Errorf("stage 1 parse: %w", err)
	}
	tree.Situation = assessed.Situation

	if len(assessed.Options) == 0 {
		return nil, fmt.Errorf("stage 1 returned no options")
	}

	// Stage 2: Evaluate in parallel
	nodes := make([]*ThoughtNode, len(assessed.Options))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var evalErr error

	for i, opt := range assessed.Options {
		wg.Add(1)
		go func(idx int, option AssessOption) {
			defer wg.Done()
			evalPrompt := BuildEvaluatePrompt(personality, state, assessed.Situation, option)
			evalStart := time.Now()
			evalRaw, genErr := e.llm.Generate(ctx, evalPrompt)
			evalDuration := time.Since(evalStart)

			if genErr != nil {
				mu.Lock()
				evalErr = fmt.Errorf("stage 2 evaluate %q: %w", option.Action, genErr)
				mu.Unlock()
				return
			}

			evalResp, parseErr := parseEvaluateResponse(evalRaw)
			if parseErr != nil {
				mu.Lock()
				evalErr = fmt.Errorf("stage 2 parse %q: %w", option.Action, parseErr)
				mu.Unlock()
				return
			}

			node := &ThoughtNode{
				ID:          fmt.Sprintf("node_%d", idx),
				Action:      option.Action,
				Target:      option.Target,
				Reasoning:   evalResp.Analysis,
				Scores:      evalResp.Scores,
				Combined:    weights.WeightedScore(evalResp.Scores),
				Status:      StatusActive,
				Depth:       0,
				EvalTime:    evalDuration,
				Prompt:      evalPrompt,
				RawResponse: evalRaw,
			}

			if evalResp.NextStep.Action != "" {
				child := &ThoughtNode{
					ID:     fmt.Sprintf("node_%d_next", idx),
					Action: evalResp.NextStep.Action,
					Target: evalResp.NextStep.Target,
					Status: StatusActive,
					Depth:  1,
				}
				node.Children = append(node.Children, child)
			}

			mu.Lock()
			nodes[idx] = node
			mu.Unlock()
		}(i, opt)
	}
	wg.Wait()

	if evalErr != nil {
		return nil, evalErr
	}

	tree.Root = make([]*ThoughtNode, 0, len(nodes))
	for _, n := range nodes {
		if n != nil {
			tree.Root = append(tree.Root, n)
		}
	}

	// Stage 3: Select winner
	selectWinner(tree)
	tree.Duration = time.Since(start)
	return tree, nil
}

func selectWinner(tree *ThoughtTree) {
	if len(tree.Root) == 0 {
		return
	}
	var best *ThoughtNode
	for _, n := range tree.Root {
		if best == nil || n.Combined > best.Combined {
			best = n
		}
	}
	for _, n := range tree.Root {
		if n == best {
			n.Status = StatusWinner
			tree.WinnerID = n.ID
			for _, child := range n.Children {
				child.Status = StatusWinner
			}
		} else {
			n.Status = StatusPruned
			for _, child := range n.Children {
				child.Status = StatusPruned
			}
		}
	}
}

// ToDecision converts the winning branch to an agent.Decision.
func (tree *ThoughtTree) ToDecision() agent.Decision {
	var winner *ThoughtNode
	for _, n := range tree.Root {
		if n.Status == StatusWinner {
			winner = n
			break
		}
	}
	if winner == nil {
		return agent.Decision{Action: "get_status", Reasoning: "no winner in thought tree"}
	}
	d := agent.Decision{
		Action:     winner.Action,
		Target:     winner.Target,
		Reasoning:  winner.Reasoning,
		Confidence: winner.Combined / 100.0,
	}
	for i, child := range winner.Children {
		d.PlannedActions = append(d.PlannedActions, agent.PlannedAction{
			Sequence:  i + 1,
			Action:    child.Action,
			Target:    child.Target,
			Reasoning: "next step from thought engine",
		})
	}
	return d
}

func parseAssessResponse(raw string) (*AssessResponse, error) {
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in assess response")
	}
	var resp AssessResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("parse assess JSON: %w", err)
	}
	return &resp, nil
}

func parseEvaluateResponse(raw string) (*EvaluateResponse, error) {
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in evaluate response")
	}
	var resp EvaluateResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("parse evaluate JSON: %w", err)
	}
	return &resp, nil
}

// extractJSON finds the last JSON object in a string.
func extractJSON(s string) string {
	var lastJSON string
	depth := 0
	start := -1
	for i, c := range s {
		switch c {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				lastJSON = s[start : i+1]
				start = -1
			}
		}
	}
	return lastJSON
}
