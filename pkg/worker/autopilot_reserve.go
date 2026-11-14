package worker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// routeReserve is the fuel a route should keep beyond its jump estimate: one
// spare jump, plus one more for the in-system hop to targetPOI when there is
// one (find_route does not price it). Zero when the jump cost is unknown.
func routeReserve(fuelPerJump int, targetPOI string) int {
	if fuelPerJump <= 0 {
		return 0
	}
	if targetPOI != "" {
		return 2 * fuelPerJump
	}
	return fuelPerJump
}

// pendingRetries bounds waitOutPending: a jump settles within a couple of
// ticks, so five tick-long waits clear anything but a genuinely stuck action.
const pendingRetries = 5

// waitOutPending runs act, and while the server answers that another action
// is still pending (the jump into this system has not finished settling),
// waits a tick and tries again. Any other error, or the last pending one,
// is returned.
func waitOutPending(ctx context.Context, act func() error) error {
	var err error
	for range pendingRetries {
		if err = act(); err == nil || !strings.Contains(err.Error(), "action pending") {
			return err
		}
		settle(ctx, game.SleepTick)
		if ctx.Err() != nil {
			return err
		}
	}
	return err
}

// refuelStopIfShort tops up at the station in systemID when the ship holds
// less than need, the fuel the rest of the route plus its reserve calls for.
// Best effort: no usable station, a dry desk or a failed hop leaves the ship
// to fly on as before. Each system is tried at most once per route.
func refuelStopIfShort(ctx context.Context, kb knowledge.Base, client game.GameClient, out io.Writer, systemID string, need int, unlocked bool, tried map[string]bool) {
	if kb == nil || systemID == "" || tried[systemID] {
		return
	}
	st := client.GetState()
	if st == nil {
		return
	}
	fuel, maxFuel := st.GetFuel()
	if fuel >= float64(need) || fuel >= maxFuel {
		return
	}
	tried[systemID] = true
	poi := haulUsableStationPOI(ctx, kb, systemID, unlocked)
	if poi == "" {
		return
	}
	fmt.Fprintf(out, "  Fuel %.0f is short of the %d the rest of the route needs; refuelling at %s\n", fuel, need, poi) //nolint:errcheck
	if !st.IsDocked() {
		if err := waitOutPending(ctx, func() error { _, err := client.Travel(ctx, poi); return err }); err != nil {
			fmt.Fprintf(out, "  refuel stop: travel to %s failed: %v; flying on\n", poi, err) //nolint:errcheck
			return
		}
		if err := waitOutPending(ctx, func() error { return client.Dock(ctx) }); err != nil {
			fmt.Fprintf(out, "  refuel stop: dock at %s failed: %v; flying on\n", poi, err) //nolint:errcheck
			return
		}
	}
	if err := waitOutPending(ctx, func() error { return client.Refuel(ctx) }); err != nil {
		fmt.Fprintf(out, "  refuel stop: refuel at %s failed: %v; flying on\n", poi, err) //nolint:errcheck
		return
	}
	_ = client.GetStatus(ctx)
	if s := client.GetState(); s != nil {
		f2, m2 := s.GetFuel()
		fmt.Fprintf(out, "  Refueled at %s: %.0f/%.0f\n", poi, f2, m2) //nolint:errcheck
	}
}
