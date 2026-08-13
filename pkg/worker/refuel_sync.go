package worker

import (
	"context"
	"fmt"
	"io"
	"strings"

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
// UPDATE 2026-08-09: the staleness account above was the wrong diagnosis of
// that incident. The refuel reply DID arrive and DID carry fuel — but `fuel`
// there is the number of units ADDED, and parseActionResult wrote it in as the
// new total. 1530-1522 = 8 credits at ~4cr/unit is 2 units added, which is
// exactly the "2/95" that was reported: the cache was not stale, it was
// wrong. Root cause fixed in pkg/game (client.go, case "refuel").
//
// This function is kept as defence in depth — a re-read is still the cheapest
// way to be certain after a mutation — but it is no longer load-bearing, and
// the extra get_status per refuel could be dropped if the round trip ever
// matters.
//
// Best-effort by design: a failed refresh is not a failed refuel, so the
// refuel's own error is the only one returned. Autopilot's station-refuel
// paths already do this inline and are left as they are.
func RefuelAndSync(ctx context.Context, client game.GameClient, out io.Writer, what string) error {
	if err := client.Refuel(ctx); err != nil {
		if !deskIsDry(err) {
			return err
		}
		// The desk has nothing to sell. Cells in the hold are reachable only by
		// naming an item_id — see game.Client.RefuelFromCargo for why a bare
		// refuel can never get to them while docked.
		fmt.Fprintf(out, "%s: station desk dry (%v); burning a fuel cell from cargo\n", what, err) //nolint:errcheck
		if cerr := client.RefuelFromCargo(ctx, fuelCellItemID, 1); cerr != nil {
			return fmt.Errorf("station desk dry (%w) and cargo cells unusable: %w", err, cerr)
		}
	}
	syncShipState(ctx, client, out, what)
	return nil
}

// fuelCellItemID is the basic cell. Naming it (rather than letting the server
// auto-pick the cheapest) keeps a premium/military cell in the hold for the case
// it was carried for, and it is the id a gift or a market buy produces.
const fuelCellItemID = "fuel_cell"

// deskIsDry reports whether a refuel failed because the STATION had no fuel to
// sell, as opposed to the ship being unable to refuel at all. Only this case is
// worth spending a cell on: a full tank, a rate limit, or a lost connection are
// all answered by waiting, and burning a cell for them wastes fuel that may be
// the only fuel available for many jumps.
func deskIsDry(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "station_fuel_empty") ||
		strings.Contains(msg, "no_fuel_source") ||
		strings.Contains(msg, "reserves are depleted")
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
