package worker

import (
	"context"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// dockIdempotent docks and treats an "Already docked" refusal as success.
//
// Every caller of Dock wants the same postcondition -- the ship is docked --
// and "Already docked" reports that it is. Treating that as a failure turns a
// satisfied goal into an error, and when the caller's next step only runs on
// success, the work never completes and retries forever.
//
// That is not hypothetical. mission_explore's return-dock aborted on it, so a
// finished exploration mission ("0 leg(s) remaining") could never retire:
// route -> "already at target" -> dock -> "Already docked" -> held for next
// pass -> repeat every tick. Measured 2026-08-23, 22 agents across
// mission-learn and unlock were in that loop, 1,481 and 1,468 "held for next
// pass" lines in one log tail each; the same defect burned 47 hours on
// johnny_cab and is the family in [[reference_sell_leg_dock_gap]].
//
// Deliberately narrow: only the "Already docked" refusal is swallowed. Every
// other failure -- refused permission, a destroyed ship -- still propagates,
// so this cannot become a blanket "ignore dock errors".
func dockIdempotent(ctx context.Context, c game.GameClient) error {
	if err := c.Dock(ctx); err != nil && !strings.Contains(err.Error(), "Already docked") {
		return err
	}
	return nil
}
