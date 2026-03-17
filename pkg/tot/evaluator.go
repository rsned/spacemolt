package tot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
	log.Printf("[ToT] Stage 1 prompt (%d chars) for %s", len(assessPrompt), personality.ID)
	assessRaw, err := e.llm.Generate(ctx, assessPrompt)
	if err != nil {
		return nil, fmt.Errorf("stage 1 assess: %w", err)
	}
	log.Printf("[ToT] Stage 1 raw response (%d chars): %.500s", len(assessRaw), assessRaw)

	assessed, err := parseAssessResponse(assessRaw)
	if err != nil {
		return nil, fmt.Errorf("stage 1 parse: %w", err)
	}
	tree.Situation = assessed.Situation
	log.Printf("[ToT] Stage 1 parsed: situation=%q, options=%d", assessed.Situation, len(assessed.Options))

	if len(assessed.Options) == 0 {
		return nil, fmt.Errorf("stage 1 returned no options")
	}

	// Stage 2: Evaluate sequentially
	// Ollama processes requests one at a time on a single GPU, so parallel
	// calls just queue up and risk timeouts. Sequential is actually faster
	// because there's no queue contention. When multiple GPUs are available,
	// this can be switched to parallel with a concurrency limiter.
	tree.Root = make([]*ThoughtNode, 0, len(assessed.Options))

	for i, opt := range assessed.Options {
		log.Printf("[ToT] Stage 2 evaluating option %d/%d: %s %s", i+1, len(assessed.Options), opt.Action, opt.Target)
		evalPrompt := BuildEvaluatePrompt(personality, state, assessed.Situation, opt)
		evalStart := time.Now()
		evalRaw, err := e.llm.Generate(ctx, evalPrompt)
		evalDuration := time.Since(evalStart)

		if err != nil {
			log.Printf("[ToT] Stage 2 option %d failed: %v", i+1, err)
			continue // skip failed branches instead of aborting entire pipeline
		}

		log.Printf("[ToT] Stage 2 option %d response (%d chars, %.1fs): %.200s", i+1, len(evalRaw), evalDuration.Seconds(), evalRaw)

		evalResp, parseErr := parseEvaluateResponse(evalRaw)
		if parseErr != nil {
			log.Printf("[ToT] Stage 2 option %d parse failed: %v", i+1, parseErr)
			continue
		}

		node := &ThoughtNode{
			ID:          fmt.Sprintf("node_%d", i),
			Action:      opt.Action,
			Target:      opt.Target,
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
				ID:     fmt.Sprintf("node_%d_next", i),
				Action: evalResp.NextStep.Action,
				Target: evalResp.NextStep.Target,
				Status: StatusActive,
				Depth:  1,
			}
			node.Children = append(node.Children, child)
		}

		tree.Root = append(tree.Root, node)
	}

	if len(tree.Root) == 0 {
		return nil, fmt.Errorf("stage 2: all %d options failed evaluation", len(assessed.Options))
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
		return nil, fmt.Errorf("no JSON found in assess response: %s", truncate(raw, 200))
	}
	var resp AssessResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("parse assess JSON: %w (raw: %s)", err, truncate(jsonStr, 200))
	}
	return &resp, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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

// stripThinkTags removes qwen3-style <think>...</think> tags from the response.
func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			return s
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			// Unclosed think tag — strip from <think> to end
			return s[:start]
		}
		s = s[:start] + s[end+len("</think>"):]
	}
}

// extractJSON finds the last JSON object in a string.
func extractJSON(s string) string {
	// Strip <think> tags that qwen3 models add
	s = stripThinkTags(s)

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
