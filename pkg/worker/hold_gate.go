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

// restartWaitTicks bounds waitOutRestart in ticks: State.RestartPending lapses
// after game.SleepServerRestartMaxWait, and this counts the same window in
// waits so the loop ends even when the sleep is a test stand-in.
const restartWaitTicks = int(game.SleepServerRestartMaxWait / game.SleepTick)

// waitOutRestart blocks while an announced server restart is pending (a
// server_restart_warning push, cleared by the reconnect after it), one tick at
// a time, so a worker does not start a jump or travel into the restart: one
// started there hangs for the full jump timeout and drops the route (live
// 2026-11-22, v0.733.0). It returns once the restart is over, the cap lapses,
// or ctx ends, and is a no-op when no restart is pending.
func waitOutRestart(ctx context.Context, out io.Writer, client stateReader) {
	if client == nil {
		return
	}
	st := client.GetState()
	if st == nil || !st.RestartPending(time.Now()) {
		return
	}
	fmt.Fprintf(out, "⏸ server restart to v%s: pausing before the next jump/travel\n", st.ServerRestart.Target) //nolint:errcheck
	start := time.Now()
	for range restartWaitTicks {
		settle(ctx, game.SleepTick)
		if ctx.Err() != nil {
			return
		}
		if s := client.GetState(); s == nil || !s.RestartPending(time.Now()) {
			fmt.Fprintf(out, "⏸ server restart over after %s; resuming\n", time.Since(start).Round(time.Second)) //nolint:errcheck
			return
		}
	}
	fmt.Fprintf(out, "⏸ server restart still pending after %d ticks; resuming anyway\n", restartWaitTicks) //nolint:errcheck
}
