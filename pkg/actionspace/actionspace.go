// Package actionspace evaluates game state to produce a pruned set of valid
// mutation commands with targets, precondition metadata, and branching factor
// metrics. Used by the ToT decision pipeline and a standalone graph visualizer.
package actionspace

// Precondition is a named, evaluatable check against game context.
type Precondition struct {
	Name        string                  // e.g. "requires_docked"
	Description string                  // Human-readable: "Ship must be docked at a station"
	Check       func(*GameContext) bool // Returns true if satisfied
}

// Action represents a single game mutation with its context requirements.
type Action struct {
	Name          string                     // e.g. "mine", "travel", "buy"
	Summary       string                     // From OpenAPI: "Mine resources from asteroids..."
	Category      string                     // Tag: "navigation", "trading", "combat", etc.
	IsMutation    bool                       // Always true for actions in this registry
	Preconditions []Precondition             // All must pass for action to be valid
	Targets       func(*GameContext) []string // Dynamic target generator (nil = no targets needed)
}

// ActionResult is the evaluated state of a single action.
type ActionResult struct {
	Action         Action
	Valid          bool
	Targets        []string // Populated if valid and action has targets
	FailedChecks   []string // Names of preconditions that failed
	BranchingCount int      // 1 if no targets, len(Targets) if valid, 0 if invalid
}

// ActionSpace is the full evaluated result.
type ActionSpace struct {
	Context GameContext
	Actions []ActionResult
	Stats   Stats
}

// ValidActions returns only the actions that passed all preconditions.
func (as ActionSpace) ValidActions() []ActionResult {
	var valid []ActionResult
	for _, r := range as.Actions {
		if r.Valid {
			valid = append(valid, r)
		}
	}
	return valid
}

// ValidByCategory returns valid actions filtered by category.
func (as ActionSpace) ValidByCategory(category string) []ActionResult {
	var results []ActionResult
	for _, r := range as.Actions {
		if r.Valid && r.Action.Category == category {
			results = append(results, r)
		}
	}
	return results
}
