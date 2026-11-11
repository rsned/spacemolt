package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/galaxy"
)

// Explore used to refuel only by accident: Autopilot's pre-route top-up docks
// where the ship already is, and an explorer is almost never at a station —
// it flies frontier to frontier. So the tank only went down. explorer-8 (1,020
// fuel) was tanker-rescued to a full tank and stranded again ~2.5 h later
// (2026-11-10), log full of "Fuel low and not at a station to refuel;
// continuing". The fuel stop below runs before each explore step and turns
// the explorer toward the nearest accessible station while it can still get
// there.

// exploreRefuelFrac is the tank fraction below which an explorer stops
// exploring and heads to the nearest station to refuel. Generous on purpose:
// the nearest accessible station can be several jumps from a frontier, a
// jump's real cost runs above find_route's estimate, and the alternative is a
// tanker rescue.
const exploreRefuelFrac = 0.30

// exploreStationCandidates is how many of the nearest station systems the
// fuel stop considers, enough to skip ones with no usable station POI.
const exploreStationCandidates = 8

// exploreNeedsFuelStop reports whether the tank is low enough to refuel
// before exploring further. False when capacity is unknown.
func exploreNeedsFuelStop(fuel, maxFuel float64) bool {
	return maxFuel > 0 && fuel/maxFuel < exploreRefuelFrac
}

// exploreFuelStop refuels a low explorer: at once when docked, else by flying
// to the nearest accessible (non-stronghold, non-outpost) station and docking
// there. handled is true when it took over this explore step (refueled, or
// flew toward a station), so the caller must not also explore; it is false
// when fuel is fine or no station is known, and exploring goes on. A route the
// ship cannot fuel returns Autopilot's ErrInsufficientRouteFuel, which leaves
// the ship parked for the overmind's fuel-dead rescue.
func exploreFuelStop(ctx context.Context, deps ExploreDeps, out io.Writer, currentID string) (bool, error) {
	state := deps.Client.GetState()
	if state == nil {
		return false, nil
	}
	fuel, maxFuel := state.GetFuel()
	if !exploreNeedsFuelStop(fuel, maxFuel) {
		return false, nil
	}
	if state.IsDocked() {
		return true, exploreRefuelDocked(ctx, deps, out)
	}

	graph := &galaxy.GalaxyGraph{}
	if err := graph.BuildFromDB(ctx, deps.KB); err != nil {
		fmt.Fprintf(out, "explore: fuel %.0f/%.0f low, but graph build failed: %v; exploring on\n", fuel, maxFuel, err) //nolint:errcheck
		return false, nil
	}
	near, err := galaxy.FindNearestByPOIType(ctx, deps.KB, graph, currentID, "station", exploreStationCandidates)
	if err != nil {
		fmt.Fprintf(out, "explore: fuel %.0f/%.0f low, but nearest-station lookup failed: %v; exploring on\n", fuel, maxFuel, err) //nolint:errcheck
		return false, nil
	}
	for _, dest := range near {
		pois, err := deps.KB.GetPOIs(ctx, dest.SystemID)
		if err != nil {
			continue
		}
		station := freshestStation(withoutOutposts(ctx, deps.KB, pois))
		if station == "" {
			continue
		}
		name := dest.SystemName
		if name == "" {
			name = dest.SystemID
		}
		fmt.Fprintf(out, "explore: fuel %.0f/%.0f below %.0f%%; refueling at %s in %s (%d jump(s))\n", //nolint:errcheck
			fuel, maxFuel, exploreRefuelFrac*100, station, name, dest.Hops)
		if state.CurrentPOI != station {
			if err := Autopilot(ctx, AutopilotDeps{
				Client:     deps.Client,
				Out:        out,
				KB:         deps.KB,
				OnWaypoint: func(ctx context.Context) error { return KBWaypointCapture(ctx, deps.Client, deps.KB) },
			}, dest.SystemID, station); err != nil {
				return true, fmt.Errorf("explore: refuel trip to %s: %w", name, err)
			}
		}
		if err := dockIdempotent(ctx, deps.Client); err != nil {
			fmt.Fprintf(out, "explore: dock at %s failed: %v\n", station, err) //nolint:errcheck
			return true, nil
		}
		return true, exploreRefuelDocked(ctx, deps, out)
	}
	fmt.Fprintf(out, "explore: fuel %.0f/%.0f low and no accessible station known; exploring on\n", fuel, maxFuel) //nolint:errcheck
	return false, nil
}

// exploreRefuelDocked buys a full tank at the station the ship is docked at.
// A refusal is logged, not returned: the next pass retries.
func exploreRefuelDocked(ctx context.Context, deps ExploreDeps, out io.Writer) error {
	if err := deps.Client.Refuel(ctx); err != nil {
		fmt.Fprintf(out, "explore: station refuel failed: %v\n", err) //nolint:errcheck
		return nil
	}
	_ = deps.Client.GetStatus(ctx)
	if s := deps.Client.GetState(); s != nil {
		f, m := s.GetFuel()
		fmt.Fprintf(out, "explore: refueled: %.0f/%.0f\n", f, m) //nolint:errcheck
	}
	return nil
}
