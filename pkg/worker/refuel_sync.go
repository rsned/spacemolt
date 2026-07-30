package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
)

// RefuelAndSync station-refuels and then re-reads ship state, so the cached
// fuel reflects the refuel that just happened.
//
// The client caches State.Fuel and only updates it when a message carrying
// fuel arrives; nothing in the standing loop re-reads ship state each pass.
// So a worker that refuels and then trusts its cache keeps the PRE-refuel
// value indefinitely — and because the pre-jump fuel guard reads that cached
// value, a worker with a full tank can believe it is empty and refuse to move.
// It then idles at a station with nothing in the log to say why, which is
// indistinguishable from being genuinely stranded.
//
// Live 2026-07-29: assist-nexus refuelled to 95/95 (credits 1530 -> 1522) and
// reported fuel 2/95 for over two minutes across three status captures, until
// a reconnect forced a fresh read.
//
// Best-effort by design: a failed refresh is not a failed refuel, so the
// refuel's own error is the only one returned. Autopilot's station-refuel
// paths already do this inline and are left as they are.
func RefuelAndSync(ctx context.Context, client game.GameClient, out io.Writer, what string) error {
	if err := client.Refuel(ctx); err != nil {
		return err
	}
	syncShipState(ctx, client, out, what)
	return nil
}

// syncShipState re-reads ship state after a mutation that the cache would
// otherwise miss, and reports the refreshed fuel. Errors are logged rather
// than returned: the caller's action already succeeded, and a stale cache is
// better recovered from on a later pass than turned into a spurious failure.
func syncShipState(ctx context.Context, client game.GameClient, out io.Writer, what string) {
	if err := client.GetStatus(ctx); err != nil {
		fmt.Fprintf(out, "%s: state refresh failed: %v (cached fuel may be stale)\n", what, err) //nolint:errcheck
		return
	}
	if s := client.GetState(); s != nil {
		fuel, maxFuel := s.GetFuel()
		fmt.Fprintf(out, "%s: refueled, now %.0f/%.0f\n", what, fuel, maxFuel) //nolint:errcheck
	}
}
