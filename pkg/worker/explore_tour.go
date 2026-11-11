package worker

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/rsned/spacemolt/pkg/game"
)

// The explore step tours the system it arrives in the way play_as auto_explore
// does: every POI in nearest-neighbour order, each one captured, and at every
// station a dock, a full station capture and a refuel. The refuel is what keeps
// an explorer topped up as a matter of course; exploreFuelStop is only the
// safety net for long station-less stretches.

// planTourRoute orders pois for a visit starting at startPOI: greedy nearest
// neighbour by position. startPOI comes first when it is one of pois.
func planTourRoute(pois []game.POI, startPOI string) []game.POI {
	remaining := make([]game.POI, len(pois))
	copy(remaining, pois)
	route := make([]game.POI, 0, len(pois))

	var cur game.Position
	for i, p := range remaining {
		if p.ID == startPOI {
			route = append(route, p)
			cur = p.Position
			remaining = append(remaining[:i], remaining[i+1:]...)
			break
		}
	}
	for len(remaining) > 0 {
		best, bestD := 0, math.Inf(1)
		for i, p := range remaining {
			if d := math.Hypot(p.Position.X-cur.X, p.Position.Y-cur.Y); d < bestD {
				best, bestD = i, d
			}
		}
		route = append(route, remaining[best])
		cur = remaining[best].Position
		remaining = append(remaining[:best], remaining[best+1:]...)
	}

	return route
}

// tourSkipReason reports why the tour must not visit p, or "" to visit it.
// A hidden POI is not travelable until revealed; a hazard that doses at
// moderate severity or worse is not worth a capture.
func tourSkipReason(p game.POI) string {
	if p.Hidden {
		return "hidden"
	}
	if p.Hazard.Damaging() && p.Hazard.Severity >= hazardLeaveSeverity {
		return fmt.Sprintf("%s hazard severity %d", p.Hazard.Type, p.Hazard.Severity)
	}

	return ""
}

// tourStation reports whether p is a station the explorer can dock at:
// outposts refuse outsiders.
func tourStation(p game.POI) bool {
	return p.HasBase && p.BaseType != game.BaseTypeOutpost
}

// systemTour visits every POI of the current system. Best-effort: a failed
// hop or capture is logged and the tour moves on; it stops early only when the
// ship is out of fuel for in-system travel or ctx ends. It returns nil except
// for ctx cancellation.
func systemTour(ctx context.Context, deps ExploreDeps, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	if err := deps.Client.GetSystem(ctx); err != nil {
		fmt.Fprintf(out, "explore: tour skipped: get_system: %v\n", err) //nolint:errcheck
		return nil
	}
	state := deps.Client.GetState()
	if state == nil || len(state.System.POIs) == 0 {
		return nil
	}
	route := planTourRoute(state.System.POIs, state.CurrentPOI)
	fmt.Fprintf(out, "explore: touring %d POI(s) in %s\n", len(route), nonEmpty(state.System.Name, state.System.ID)) //nolint:errcheck

	for _, p := range route {
		if err := ctx.Err(); err != nil {
			return err
		}
		if why := tourSkipReason(p); why != "" {
			fmt.Fprintf(out, "explore: skipping %s (%s)\n", nonEmpty(p.Name, p.ID), why) //nolint:errcheck
			continue
		}
		if err := autopilotTravelToPOI(ctx, deps.Client, out, p.ID); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(out, "explore: %v\n", err) //nolint:errcheck
			if isInsufficientFuelErr(err) {
				fmt.Fprintln(out, "explore: tour stopped: out of fuel for in-system travel") //nolint:errcheck
				return nil
			}
			continue
		}
		if err := KBUpdatePOI(ctx, deps.Client, deps.KB, deps.AgentID); err != nil {
			fmt.Fprintf(out, "explore: capture %s: %v\n", nonEmpty(p.Name, p.ID), err) //nolint:errcheck
		}
		if tourStation(p) {
			exploreStationStop(ctx, deps, out, p)
		}
	}

	return nil
}

// exploreStationStop docks at the station the ship is at, captures the
// station (base, market, shipyard, facilities, missions) and fills the tank.
// A refused dock (a faction-only or stronghold station) just moves the tour on.
func exploreStationStop(ctx context.Context, deps ExploreDeps, out io.Writer, p game.POI) {
	if err := dockIdempotent(ctx, deps.Client); err != nil {
		fmt.Fprintf(out, "explore: dock at %s refused: %v\n", nonEmpty(p.Name, p.ID), err) //nolint:errcheck
		return
	}
	name := nonEmpty(p.Name, p.ID)
	if err := KBUpdateStation(ctx, deps.Client, deps.KB, deps.Market, "worker"); err != nil {
		fmt.Fprintf(out, "explore: station capture at %s: %v\n", name, err) //nolint:errcheck
	}
	if err := KBUpdateFacilities(ctx, deps.Client, deps.KB); err != nil {
		fmt.Fprintf(out, "explore: facilities capture at %s: %v\n", name, err) //nolint:errcheck
	}
	if err := KBUpdateMissions(ctx, deps.Client, deps.KB); err != nil {
		fmt.Fprintf(out, "explore: missions capture at %s: %v\n", name, err) //nolint:errcheck
	}
	if s := deps.Client.GetState(); s != nil {
		if f, m := s.GetFuel(); m > 0 && f >= m {
			return
		}
	}
	if err := deps.Client.Refuel(ctx); err != nil {
		fmt.Fprintf(out, "explore: refuel at %s failed: %v\n", name, err) //nolint:errcheck
		return
	}
	_ = deps.Client.GetStatus(ctx)
	if s := deps.Client.GetState(); s != nil {
		f, m := s.GetFuel()
		fmt.Fprintf(out, "explore: refueled at %s: %.0f/%.0f\n", name, f, m) //nolint:errcheck
	}
}
