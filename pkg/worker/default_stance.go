package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
)

// ensureDefaultStanceFlee makes a non-combat ship open every fight it is
// pulled into at flee (v0.721.0 battle action=default_stance). Under the
// server default (auto) an armed ship opens at fire, so a hauler, explorer or
// assist ship with a turret or repairer would stand and fight. bc is the
// DefaultStanceClient for the role (every role, hunt included; nil skips).
// It is a no-op when the ship already defaults to flee.
func ensureDefaultStanceFlee(ctx context.Context, out io.Writer, ship *game.Ship, bc BattleClient) error {
	if bc == nil || ship == nil || ship.DefaultStance == battleStanceFlee {
		return nil
	}
	if err := bc.Battle(ctx, "default_stance", map[string]any{"stance": battleStanceFlee}); err != nil {
		return fmt.Errorf("battle: set default stance flee: %w", err)
	}
	fmt.Fprintf(out, "⚔️  battle: default stance set to flee (was %q)\n", nonEmpty(ship.DefaultStance, "auto")) //nolint:errcheck
	ship.DefaultStance = battleStanceFlee

	return nil
}

// EnsureDefaultStanceFlee is ensureDefaultStanceFlee for cmd/worker's start-up.
func EnsureDefaultStanceFlee(ctx context.Context, out io.Writer, ship *game.Ship, bc BattleClient) error {
	return ensureDefaultStanceFlee(ctx, out, ship, bc)
}

// DefaultStanceClient is the client cmd/worker's start-up sets the default
// stance through: c for every role, hunt included. default_stance governs
// only fights the ship did NOT choose (pirate, police or player attack, jump
// or undock into a fight); a fight the ship starts opens at fire, and the
// hunt role's own fight loop (huntPolicy) sets its stance every tick. So a
// hunter defaulting to flee still fights the creatures it engages, and
// escapes the ones it does not. Unlike BattleGateClient, hunt is not exempt.
func DefaultStanceClient(_ string, c BattleClient) BattleClient {
	return c
}
