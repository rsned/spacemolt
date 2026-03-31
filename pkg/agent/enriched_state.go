package agent

import (
	"context"

	"github.com/rsned/spacemolt/pkg/game"
)

// rawEnrichedState is a minimal EnrichedState that wraps a cloned game state
// with no KB enrichment. Used as a fallback when no agentstate is configured.
type rawEnrichedState struct {
	state *game.State
}

// NewRawEnrichedState creates a minimal EnrichedState wrapping a cloned game
// state. All enrichment accessors return safe zero values.
func NewRawEnrichedState(state *game.State) EnrichedState {
	return &rawEnrichedState{state: state}
}

func (r *rawEnrichedState) Refresh(context.Context) {}

func (r *rawEnrichedState) GameState() *game.State {
	return r.state
}

func (r *rawEnrichedState) SystemSecurity() string {
	switch r.state.System.PoliceLevel {
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

func (r *rawEnrichedState) CanDo(string) bool {
	return true // No action filtering without enrichment
}
