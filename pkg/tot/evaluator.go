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

// UpdateCallback is called with a partial ThoughtTree as each stage progresses.
type UpdateCallback func(tree *ThoughtTree)

// Evaluator orchestrates the three-stage Thought Engine pipeline.
type Evaluator struct {
	llm      LLMGenerator
	model    string
	onUpdate UpdateCallback
}

// NewEvaluator creates a new Evaluator with the given LLM generator and model name.
func NewEvaluator(llm LLMGenerator, model string) *Evaluator {
	return &Evaluator{llm: llm, model: model}
}

// SetOnUpdate sets a callback that fires with partial tree state as the pipeline progresses.
func (e *Evaluator) SetOnUpdate(cb UpdateCallback) {
	e.onUpdate = cb
}

func (e *Evaluator) emitUpdate(tree *ThoughtTree) {
	if e.onUpdate != nil {
		e.onUpdate(tree)
	}
}

// Evaluate runs the full pipeline: Assess → Evaluate (parallel) → Select.
func (e *Evaluator) Evaluate(
	ctx context.Context,
	personality agent.Personality,
	state *game.State,
	validActions []ActionOption,
	weights AxisWeights,
	pctx *PromptContext,
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
	assessPrompt := BuildAssessPrompt(personality, state, validActions, pctx)
	log.Printf("[ToT] Stage 1 prompt (%d chars) for %s:\n%s", len(assessPrompt), personality.ID, assessPrompt)
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

	// Validate and fix options — LLMs sometimes put targets as actions
	assessed.Options = fixAssessOptions(assessed.Options, validActions)
	log.Printf("[ToT] Stage 1 parsed: situation=%q, options=%d", assessed.Situation, len(assessed.Options))

	if len(assessed.Options) == 0 {
		return nil, fmt.Errorf("stage 1 returned no options")
	}

	// Create stub nodes for all options (unscored) and emit
	tree.Root = make([]*ThoughtNode, len(assessed.Options))
	for i, opt := range assessed.Options {
		tree.Root[i] = &ThoughtNode{
			ID:     fmt.Sprintf("node_%d", i),
			Action: opt.Action,
			Target: opt.Target,
			Status: StatusPending,
			Depth:  0,
		}
	}
	tree.Stage = "assessing"
	e.emitUpdate(tree)

	// Stage 2: Evaluate sequentially, emitting after each
	for i, opt := range assessed.Options {
		tree.Stage = fmt.Sprintf("evaluating %d/%d", i+1, len(assessed.Options))
		tree.Root[i].Status = StatusEvaluating
		e.emitUpdate(tree)

		evalPrompt := BuildEvaluatePrompt(personality, state, assessed.Situation, opt)
		log.Printf("[ToT] Stage 2 evaluating option %d/%d: %s %s", i+1, len(assessed.Options), opt.Action, opt.Target)
		evalStart := time.Now()
		evalRaw, err := e.llm.Generate(ctx, evalPrompt)
		evalDuration := time.Since(evalStart)

		if err != nil {
			log.Printf("[ToT] Stage 2 option %d failed: %v", i+1, err)
			tree.Root[i].Status = StatusPruned
			e.emitUpdate(tree)
			continue
		}

		log.Printf("[ToT] Stage 2 option %d response (%d chars, %.1fs): %.200s", i+1, len(evalRaw), evalDuration.Seconds(), evalRaw)

		evalResp, parseErr := parseEvaluateResponse(evalRaw)
		if parseErr != nil {
			log.Printf("[ToT] Stage 2 option %d parse failed: %v", i+1, parseErr)
			tree.Root[i].Status = StatusPruned
			e.emitUpdate(tree)
			continue
		}

		// Fill in the stub node with real scores
		node := tree.Root[i]
		node.Reasoning = evalResp.Analysis
		node.Scores = evalResp.Scores
		node.Combined = weights.WeightedScore(evalResp.Scores)
		node.Status = StatusActive
		node.EvalTime = evalDuration
		node.Prompt = evalPrompt
		node.RawResponse = evalRaw

		// Build plan from response — prefer "plan" array, fall back to legacy "next_step"
		plan := evalResp.Plan
		if len(plan) == 0 && evalResp.NextStep.Action != "" {
			plan = []PlanStep{{Action: evalResp.NextStep.Action, Target: evalResp.NextStep.Target}}
		}
		// Filter template echoes and add as child nodes
		for j, step := range plan {
			if step.Action == "" || step.Action == "next" || step.Action == "next_action" {
				continue
			}
			if step.Target == "id" || step.Target == "next_target" {
				step.Target = ""
			}
			node.Children = append(node.Children, &ThoughtNode{
				ID:     fmt.Sprintf("node_%d_plan_%d", i, j),
				Action: step.Action,
				Target: step.Target,
				Status: StatusActive,
				Depth:  1,
			})
		}

		e.emitUpdate(tree)
	}

	// Check we have at least one scored node
	hasScored := false
	for _, n := range tree.Root {
		if n.Status == StatusActive {
			hasScored = true
			break
		}
	}
	if !hasScored {
		return nil, fmt.Errorf("stage 2: all %d options failed evaluation", len(assessed.Options))
	}

	// Stage 3: Select winner
	tree.Stage = "selecting"
	selectWinner(tree)
	tree.Stage = "complete"
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

// fixAssessOptions validates and corrects LLM-generated options.
// Common issue: LLM puts a target name (like "commerce_fields") as the action
// instead of the correct action ("travel") with the target.
func fixAssessOptions(options []AssessOption, validActions []ActionOption) []AssessOption {
	actionSet := make(map[string]bool, len(validActions))
	targetToAction := make(map[string]string) // target ID → action that uses it
	for _, va := range validActions {
		actionSet[va.Action] = true
		for _, t := range va.Targets {
			targetToAction[t] = va.Action
		}
	}

	fixed := make([]AssessOption, 0, len(options))
	for _, opt := range options {
		if actionSet[opt.Action] {
			// Action is valid
			fixed = append(fixed, opt)
		} else if correctAction, ok := targetToAction[opt.Action]; ok {
			// Action is actually a target — fix it
			log.Printf("[ToT] Fixing option: action %q is a target, correcting to %q targeting %q", opt.Action, correctAction, opt.Action)
			fixed = append(fixed, AssessOption{
				Action:    correctAction,
				Target:    opt.Action,
				Rationale: opt.Rationale,
			})
		} else {
			log.Printf("[ToT] Dropping invalid option: action %q not in valid actions", opt.Action)
		}
	}
	return fixed
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
