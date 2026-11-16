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
	// Prefer a station known to sell fuel; fall back to any usable one when
	// the KB has no service list for this system's stations.
	poi := refuelStationPOI(ctx, kb, systemID, unlocked)
	if poi == "" {
		poi = haulUsableStationPOI(ctx, kb, systemID, unlocked)
	}
	if poi == "" {
		return
	}
	// Jump arrivals do not refresh the cached fuel, so re-read it here, where
	// a stop is possible (trader-8, 11-15: every check read its departure 89
	// and it flew a 34-jump route past five refuel stations to 5/110).
	_ = client.GetStatus(ctx)
	st := client.GetState()
	if st == nil {
		return
	}
	fuel, maxFuel := st.GetFuel()
	if fuel >= float64(need) || fuel >= maxFuel {
		return
	}
	tried[systemID] = true
	fmt.Fprintf(out, "  Fuel %.0f is short of the %d the rest of the route needs; refuelling at %s\n", fuel, need, poi) //nolint:errcheck
	if !st.IsDocked() {
		waitOutHold(ctx, out, client)
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

// routeFuelFeasible reports whether a route of len(refuelAt) jumps at perJump
// fuel each can be flown starting with fuel in a tank of tank, when the ship
// fills up wherever refuelAt[i] is true (the system it is in before jump i;
// refuelAt[0] is the origin). Reaching the station is an in-system hop, so a
// stop only counts when the ship arrives with at least perJump fuel left (the
// same one-hop allowance routeReserve makes).
func routeFuelFeasible(fuel, tank, perJump int, refuelAt []bool) bool {
	for _, stop := range refuelAt {
		if stop && fuel >= perJump {
			fuel = tank
		}
		fuel -= perJump
		if fuel < 0 {
			return false
		}
	}
	return true
}

// refuelStationPOI is a station POI in systemID the ship may dock at whose
// base is KNOWN to sell fuel (the "refuel" service). "" when there is none.
func refuelStationPOI(ctx context.Context, kb knowledge.Base, systemID string, unlocked bool) string {
	if kb == nil {
		return ""
	}
	pois, err := kb.GetPOIs(ctx, systemID)
	if err != nil {
		return ""
	}
	for _, p := range pois {
		if p.Type != "station" {
			continue
		}
		base, berr := kb.GetBaseByPOI(ctx, p.ID)
		if berr != nil || base == nil || base.IsOutpost() || (!base.PublicAccess && !unlocked) {
			continue
		}
		if base.Services["refuel"] {
			return p.ID
		}
	}
	return ""
}
