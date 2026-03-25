package tot

import (
	"github.com/rsned/spacemolt/pkg/actionspace"
	"github.com/rsned/spacemolt/pkg/game"
)

// ValidActions returns the set of actions that are valid for the current game state.
// Action names MUST match the cases in pkg/agent/runner.go executeDecision().
func ValidActions(state *game.State) []ActionOption {
	gc := actionspace.FromState(state)
	as := actionspace.Evaluate(gc)

	// Convert actionspace results to tot.ActionOption format.
	asOpts := as.ToActionOptions()
	opts := make([]ActionOption, 0, len(asOpts)+5)
	for _, o := range asOpts {
		opts = append(opts, ActionOption{
			Action:      o.Action,
			Description: o.Description,
			Targets:     o.Targets,
		})
	}

	// Always include query actions (no tick cost).
	opts = append(opts, queryActions()...)
	return opts
}

// queryActions returns informational actions that are always available (no tick cost).
func queryActions() []ActionOption {
	return []ActionOption{
		{Action: "get_status", Description: "Get current player and ship status"},
		{Action: "get_system", Description: "Get information about the current system"},
		{Action: "get_cargo", Description: "View current cargo hold contents"},
		{Action: "get_skills", Description: "View player skills and XP progress"},
		{Action: "get_nearby", Description: "List nearby players and objects"},
	}
}
