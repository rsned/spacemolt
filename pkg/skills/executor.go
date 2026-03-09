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
	return e.RunWithParams(ctx, skillName, nil)
}

// RunWithParams executes a skill with parameters.
func (e *Executor) RunWithParams(ctx context.Context, skillName string, params map[string]string) error {
	skill := e.registry.Get(skillName)
	if skill == nil {
		return fmt.Errorf("unknown skill: %q", skillName)
	}

	// Set parameters on dispatcher if it's a ClientDispatcher
	if dispatcher, ok := e.dispatcher.(*ClientDispatcher); ok {
		if dispatcher.SkillParams == nil {
			dispatcher.SkillParams = make(map[string]string)
		}
		// Clear previous params and set new ones
		for k := range dispatcher.SkillParams {
			delete(dispatcher.SkillParams, k)
		}
		for k, v := range params {
			dispatcher.SkillParams[k] = v
		}
	}

	return e.runSkill(ctx, skill)
}

// isFatalError returns true for errors that should not be retried via on_error
// because retrying would be pointless (e.g. no connection to the server).
func isFatalError(err error) bool {
	msg := err.Error()
	switch {
	case msg == "not connected":
		return true
	case strings.Contains(msg, "context canceled"):
		return true
	default:
		return false
	}
}

func (e *Executor) runSkill(ctx context.Context, skill *Skill) error {
	if e.depth >= maxNestingDepth {
		return fmt.Errorf("skill nesting depth exceeded (%d) -- possible circular reference", maxNestingDepth)
	}
	e.depth++
	defer func() { e.depth-- }()

	indent := strings.Repeat("  ", e.depth-1)
	e.logger.Printf("%s▶ skill:%s started", indent, skill.Name)

	maxSteps := e.MaxSteps
	if maxSteps == 0 {
		maxSteps = defaultMaxSteps
	}

	stepID := skill.FirstStepID()
	stepsExecuted := 0

	for stepID != "" {
		if err := ctx.Err(); err != nil {
			e.logger.Printf("%s✗ skill:%s cancelled", indent, skill.Name)
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
			e.logger.Printf("%s✓ skill:%s completed (%d steps)", indent, skill.Name, stepsExecuted)
			return nil
		}

		nextID, err := e.executeStep(ctx, skill, step)
		if err != nil {
			if step.OnError != "" && !isFatalError(err) {
				e.logger.Printf("%s  ⚠ %s error: %v → on_error: %s", indent, step.ID, err, step.OnError)
				nextID = step.OnError
			} else {
				e.logger.Printf("%s✗ skill:%s step:%s error: %v", indent, skill.Name, step.ID, err)
				return fmt.Errorf("skill %q step %q: %w", skill.Name, step.ID, err)
			}
		}

		stepsExecuted++
		stepID = nextID
	}

	e.logger.Printf("%s✓ skill:%s completed (%d steps)", indent, skill.Name, stepsExecuted)
	return nil
}

func (e *Executor) executeStep(ctx context.Context, skill *Skill, step *Step) (string, error) {
	state := e.dispatcher.GetState()
	indent := strings.Repeat("  ", e.depth-1)

	// Check node -- evaluate conditions, no action.
	if step.Check {
		nextID, err := e.evalConditions(step.Conditions, state)
		if err != nil {
			return "", err
		}
		e.logger.Printf("%s  ◇ %s → %s", indent, step.ID, nextID)
		return nextID, nil
	}

	// Sub-skill invocation.
	if step.Skill != "" {
		subSkill := e.registry.Get(step.Skill)
		if subSkill == nil {
			return "", fmt.Errorf("unknown sub-skill: %q", step.Skill)
		}

		// Build sub-skill parameters
		params := e.buildSubSkillParams(step)

		// Create a new executor for the sub-skill to avoid depth conflicts
		subExec := &Executor{
			registry:   e.registry,
			dispatcher: e.dispatcher,
			logger:     e.logger,
			MaxSteps:   e.MaxSteps,
			depth:      e.depth, // Inherit current depth
		}

		if err := subExec.RunWithParams(ctx, step.Skill, params); err != nil {
			return "", fmt.Errorf("sub-skill %q: %w", step.Skill, err)
		}
		return step.Next, nil
	}

	// Action node.
	if step.Action != "" {
		// Inject step args into dispatcher's SkillParams so the dispatcher can access them.
		if len(step.Args) > 0 {
			if dispatcher, ok := e.dispatcher.(*ClientDispatcher); ok {
				for k, v := range step.Args {
					dispatcher.SkillParams["args."+k] = v
				}
			}
		}

		target, err := e.resolveTarget(step.Target, skill, state)
		if err != nil {
			return "", err
		}

		// Handle repeat/while loop.
		if step.Repeat != nil {
			return e.executeRepeat(ctx, step, target)
		}

		// Single action execution.
		if target != "" {
			e.logger.Printf("%s  ■ %s → %s (target: %s)", indent, step.ID, step.Action, target)
		} else {
			e.logger.Printf("%s  ■ %s → %s", indent, step.ID, step.Action)
		}
		if err := e.dispatcher.Dispatch(ctx, step.Action, target); err != nil {
			return "", err
		}

		// After action, evaluate conditions if present.
		if len(step.Conditions) > 0 {
			state = e.dispatcher.GetState()
			nextID, err := e.evalConditions(step.Conditions, state)
			if err != nil {
				return "", err
			}
			e.logger.Printf("%s    → %s", indent, nextID)
			return nextID, nil
		}

		return step.Next, nil
	}

	return "", fmt.Errorf("step has no action, skill, check, or terminal")
}

func (e *Executor) executeRepeat(ctx context.Context, step *Step, target string) (string, error) {
	indent := strings.Repeat("  ", e.depth-1)
	maxSteps := e.MaxSteps
	if maxSteps == 0 {
		maxSteps = defaultMaxSteps
	}

	if target != "" {
		e.logger.Printf("%s  ⟳ %s → %s loop (target: %s)", indent, step.ID, step.Action, target)
	} else {
		e.logger.Printf("%s  ⟳ %s → %s loop", indent, step.ID, step.Action)
	}

	iteration := 0
	for range maxSteps {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		// Check while conditions before each iteration.
		state := e.dispatcher.GetState()
		allTrue := true
		for _, condExpr := range step.Repeat.While {
			result, err := e.evalExprWithRoute(condExpr, state)
			if err != nil {
				return "", fmt.Errorf("while condition %q: %w", condExpr, err)
			}
			if !result {
				e.logger.Printf("%s    [%d] exit: %s no longer true", indent, iteration+1, condExpr)
				allTrue = false
				break
			}
		}
		if !allTrue {
			break
		}

		iteration++
		if err := e.dispatcher.Dispatch(ctx, step.Action, target); err != nil {
			return "", err
		}

		if iteration%5 == 0 {
			e.logger.Printf("%s    [%d] %s continuing...", indent, iteration, step.Action)
		}
	}

	e.logger.Printf("%s  ⟳ %s loop done (%d iterations)", indent, step.ID, iteration)
	return step.Next, nil
}

func (e *Executor) evalConditions(conditions ConditionList, state *game.State) (string, error) {
	var defaultTarget string

	// Pre-scan for default target so we can treat unknown-variable errors
	// as false when a default fallback exists.
	for _, cond := range conditions {
		if strings.TrimSpace(cond.Expr) == "default" {
			defaultTarget = strings.TrimPrefix(strings.TrimSpace(cond.Goto), "goto ")
			break
		}
	}

	for _, cond := range conditions {
		target := strings.TrimPrefix(strings.TrimSpace(cond.Goto), "goto ")

		if strings.TrimSpace(cond.Expr) == "default" {
			continue
		}

		result, err := e.evalExprWithRoute(cond.Expr, state)
		if err != nil {
			// If there's a default fallback, treat unresolvable conditions as
			// false rather than failing. This allows skills to use domain-
			// specific conditions (e.g. "found_request") that the executor
			// cannot evaluate yet, falling through to the default.
			if defaultTarget != "" {
				indent := strings.Repeat("  ", e.depth-1)
				e.logger.Printf("%s    condition %q unresolvable (%v), skipping", indent, cond.Expr, err)
				continue
			}
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

func (e *Executor) evalExprWithRoute(expr string, state *game.State) (bool, error) {
	// Get route and custom vars from dispatcher if it's a ClientDispatcher
	var route *RouteProgress
	var customVars map[string]float64
	if dispatcher, ok := e.dispatcher.(*ClientDispatcher); ok {
		route = dispatcher.Route
		customVars = dispatcher.CustomVars()
	}
	return EvalExprWithRoute(expr, state, route, customVars)
}

func (e *Executor) resolveTarget(target string, skill *Skill, state *game.State) (string, error) {
	if target == "" {
		return "", nil
	}

	if !strings.HasPrefix(target, "$") {
		return target, nil
	}

	varName := strings.TrimPrefix(target, "$")

	// Check if it's a defined target variable (POI type reference)
	t, ok := skill.Targets[varName]
	if !ok {
		// Not a target variable - pass through to dispatcher
		// The dispatcher will handle parameter resolution (e.g., $destination_system)
		return target, nil
	}

	// Resolve target variable by finding POI of matching type
	for _, poi := range state.System.POIs {
		if slices.Contains(t.POIType, poi.Type) {
			return poi.ID, nil
		}
	}

	return "", fmt.Errorf("no POI of type %v found in current system", t.POIType)
}


// buildSubSkillParams resolves parameter values for sub-skill invocation.
func (e *Executor) buildSubSkillParams(step *Step) map[string]string {
	state := e.dispatcher.GetState()
	params := make(map[string]string)

	for k, v := range step.SkillParams {
		if strings.HasPrefix(v, "$") {
			// Variable reference - resolve from skill params or state
			varName := strings.TrimPrefix(v, "$")

			// First check if it's a special variable
			resolved := e.resolveVariable(varName, state)
			if resolved != "" {
				params[k] = resolved
			} else {
				// Try to get from current skill params
				if dispatcher, ok := e.dispatcher.(*ClientDispatcher); ok {
					if val, ok := dispatcher.SkillParams[varName]; ok {
						params[k] = val
					} else {
						params[k] = v // Keep original if not found
					}
				} else {
					params[k] = v
				}
			}
		} else {
			params[k] = v
		}
	}

	return params
}

// resolveVariable resolves special variables like capital_system_id
func (e *Executor) resolveVariable(varName string, state *game.State) string {
	switch varName {
	case "capital_system_id":
		return empireCapitalSystem(state.Player.Empire)
	case "found_poi":
		if dispatcher, ok := e.dispatcher.(*ClientDispatcher); ok {
			return dispatcher.FoundPOI
		}
		return ""
	default:
		return ""
	}
}
