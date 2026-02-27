package skills

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// ActionDispatcher executes a single game action. This interface decouples the
// executor from the game client, making testing possible and allowing the
// agent runner to provide its own dispatch logic.
type ActionDispatcher interface {
	Dispatch(ctx context.Context, action, target string) error
	GetState() *game.State
}

// Executor walks a skill state machine, evaluating conditions and dispatching
// actions through the ActionDispatcher.
type Executor struct {
	registry   *Registry
	dispatcher ActionDispatcher
	logger     *log.Logger
	MaxSteps   int // Safety limit; 0 = default (1000).
	depth      int // Current sub-skill nesting depth.
}

const (
	defaultMaxSteps = 1000
	maxNestingDepth = 10
)

// NewExecutor creates a skill executor.
func NewExecutor(registry *Registry, dispatcher ActionDispatcher, logger *log.Logger) *Executor {
	return &Executor{
		registry:   registry,
		dispatcher: dispatcher,
		logger:     logger,
	}
}

// Run executes a skill by name to completion.
func (e *Executor) Run(ctx context.Context, skillName string) error {
	skill := e.registry.Get(skillName)
	if skill == nil {
		return fmt.Errorf("unknown skill: %q", skillName)
	}
	return e.runSkill(ctx, skill)
}

func (e *Executor) runSkill(ctx context.Context, skill *Skill) error {
	if e.depth >= maxNestingDepth {
		return fmt.Errorf("skill nesting depth exceeded (%d) -- possible circular reference", maxNestingDepth)
	}
	e.depth++
	defer func() { e.depth-- }()

	maxSteps := e.MaxSteps
	if maxSteps == 0 {
		maxSteps = defaultMaxSteps
	}

	stepID := skill.FirstStepID()
	stepsExecuted := 0

	for stepID != "" {
		if err := ctx.Err(); err != nil {
			return err
		}

		if stepsExecuted >= maxSteps {
			return fmt.Errorf("skill %q exceeded max steps (%d)", skill.Name, maxSteps)
		}

		step := skill.StepByID(stepID)
		if step == nil {
			return fmt.Errorf("skill %q: step %q not found", skill.Name, stepID)
		}

		if step.Terminal {
			return nil
		}

		nextID, err := e.executeStep(ctx, skill, step)
		if err != nil {
			return fmt.Errorf("skill %q step %q: %w", skill.Name, step.ID, err)
		}

		stepsExecuted++
		stepID = nextID
	}

	return nil
}

func (e *Executor) executeStep(ctx context.Context, skill *Skill, step *Step) (string, error) {
	state := e.dispatcher.GetState()

	// Check node -- evaluate conditions, no action.
	if step.Check {
		return e.evalConditions(step.Conditions, state)
	}

	// Sub-skill invocation.
	if step.Skill != "" {
		subSkill := e.registry.Get(step.Skill)
		if subSkill == nil {
			return "", fmt.Errorf("unknown sub-skill: %q", step.Skill)
		}
		if err := e.runSkill(ctx, subSkill); err != nil {
			return "", fmt.Errorf("sub-skill %q: %w", step.Skill, err)
		}
		return step.Next, nil
	}

	// Action node.
	if step.Action != "" {
		target, err := e.resolveTarget(step.Target, skill, state)
		if err != nil {
			return "", err
		}

		// Handle repeat/while loop.
		if step.Repeat != nil {
			return e.executeRepeat(ctx, step, target)
		}

		// Single action execution.
		if err := e.dispatcher.Dispatch(ctx, step.Action, target); err != nil {
			return "", err
		}

		// After action, evaluate conditions if present.
		if len(step.Conditions) > 0 {
			state = e.dispatcher.GetState()
			return e.evalConditions(step.Conditions, state)
		}

		return step.Next, nil
	}

	return "", fmt.Errorf("step has no action, skill, check, or terminal")
}

func (e *Executor) executeRepeat(ctx context.Context, step *Step, target string) (string, error) {
	maxSteps := e.MaxSteps
	if maxSteps == 0 {
		maxSteps = defaultMaxSteps
	}

	for range maxSteps {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		// Check while conditions before each iteration.
		state := e.dispatcher.GetState()
		allTrue := true
		for _, cond := range step.Repeat.While {
			result, err := EvalExpr(cond, state)
			if err != nil {
				return "", fmt.Errorf("while condition %q: %w", cond, err)
			}
			if !result {
				allTrue = false
				break
			}
		}
		if !allTrue {
			break
		}

		if err := e.dispatcher.Dispatch(ctx, step.Action, target); err != nil {
			return "", err
		}
	}

	return step.Next, nil
}

func (e *Executor) evalConditions(conditions ConditionList, state *game.State) (string, error) {
	var defaultTarget string

	for _, cond := range conditions {
		target := strings.TrimPrefix(strings.TrimSpace(cond.Goto), "goto ")

		if strings.TrimSpace(cond.Expr) == "default" {
			defaultTarget = target
			continue
		}

		result, err := EvalExpr(cond.Expr, state)
		if err != nil {
			return "", fmt.Errorf("condition %q: %w", cond.Expr, err)
		}
		if result {
			return target, nil
		}
	}

	if defaultTarget != "" {
		return defaultTarget, nil
	}

	return "", fmt.Errorf("no condition matched and no default")
}

func (e *Executor) resolveTarget(target string, skill *Skill, state *game.State) (string, error) {
	if target == "" {
		return "", nil
	}

	if !strings.HasPrefix(target, "$") {
		return target, nil
	}

	varName := strings.TrimPrefix(target, "$")
	t, ok := skill.Targets[varName]
	if !ok {
		return "", fmt.Errorf("unknown target variable: %q", varName)
	}

	for _, poi := range state.System.POIs {
		if slices.Contains(t.POIType, poi.Type) {
			return poi.ID, nil
		}
	}

	return "", fmt.Errorf("no POI of type %v found in current system", t.POIType)
}
