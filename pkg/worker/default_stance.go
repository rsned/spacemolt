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
// BattleGateClient for the role: nil for hunt, which fights on purpose.
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
