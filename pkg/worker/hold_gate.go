package worker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// holdWaitTicks bounds waitOutHold in ticks. State.Held already lapses after
// game.HoldStaleAfter; this is the same window counted in waits, so the loop
// ends even when the sleep is a test stand-in that takes no time.
const holdWaitTicks = int(game.HoldStaleAfter / game.SleepTick)

// stateReader is the slice of the game client waitOutHold needs.
type stateReader interface {
	GetState() *game.State
}

// waitOutHold blocks while the ship is held (a customs warp scrambler: the
// server refuses jump, travel and dock until the inspection ends), one tick
// at a time, and returns once the hold clears, lapses, or ctx ends. It is a
// no-op when the ship is not held or there is no client.
func waitOutHold(ctx context.Context, out io.Writer, client stateReader) {
	if client == nil {
		return
	}
	st := client.GetState()
	if st == nil || !st.Held() {
		return
	}
	h := st.ActiveHold
	fmt.Fprintf(out, "🛃 hold: %s holds the ship (%s); waiting for it to clear\n", h.HolderName, h.Source) //nolint:errcheck
	start := time.Now()
	for range holdWaitTicks {
		settle(ctx, game.SleepTick)
		if ctx.Err() != nil {
			return
		}
		if s := client.GetState(); s == nil || !s.Held() {
			fmt.Fprintf(out, "🛃 hold: cleared after %s\n", time.Since(start).Round(time.Second)) //nolint:errcheck
			return
		}
	}
	fmt.Fprintf(out, "🛃 hold: still flagged after %d ticks; trying anyway\n", holdWaitTicks) //nolint:errcheck
}

// holdsMovement reports whether a command moves the ship and so must wait
// out a hold: the plain movement verbs plus autopilot.
func holdsMovement(tokens []string) bool {
	return isMovement(tokens) || (len(tokens) > 0 && (tokens[0] == "autopilot" || tokens[0] == "ap"))
}
