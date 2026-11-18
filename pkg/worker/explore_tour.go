package worker

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/rsned/spacemolt/pkg/explore"
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

// systemTour explores the current system through the shared pkg/explore:
// survey first (so the POIs it reveals are in the route), then every POI in
// nearest-neighbour order, each one captured, every station docked at,
// captured and refuelled, and every relic interacted with once. POIs with a
// hazard of moderate severity or worse are skipped, and outposts are visited
// but not docked at. Best-effort: a failed hop or capture is logged and the
// tour moves on. It returns nil except for ctx cancellation.
func systemTour(ctx context.Context, deps ExploreDeps, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	deps.Out = out
	if err := deps.Client.GetSystem(ctx); err != nil {
		fmt.Fprintf(out, "explore: tour skipped: get_system: %v\n", err) //nolint:errcheck
		return nil
	}
	state := deps.Client.GetState()
	if state == nil {
		return nil
	}
	// A system listing no POIs is still surveyed: all of them may be hidden.
	reportWormholes(ctx, deps, out, state.System, planTourRoute(state.System.POIs, state.CurrentPOI))

	res, err := explore.System(ctx, workerExploreDeps(deps), explore.Options{
		RefuelAtStations: true, Relics: true, SkipHazardSeverity: hazardLeaveSeverity, SkipOutposts: true,
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		fmt.Fprintf(out, "explore: tour of %s ended: %v\n", nonEmpty(state.System.Name, state.System.ID), err) //nolint:errcheck
		return nil
	}
	if res.Stopped != "" {
		fmt.Fprintf(out, "explore: tour stopped at %s: %s\n", res.StoppedAt, res.Stopped) //nolint:errcheck
	}
	return nil
}

// Wormhole POI types.
const (
	poiWormholeEntrance = "wormhole_entrance"
	poiWormholeExit     = "wormhole_exit"
)

// reportWormholes logs every wormhole entrance in the system, flagging ones
// the knowledge base had not recorded yet as NEW. Report only: the explorer
// never enters one (operator decision 2026-11-10). It runs before the tour so
// an entrance the tour skips (hazard) is still reported.
func reportWormholes(ctx context.Context, deps ExploreDeps, out io.Writer, sys game.SystemData, pois []game.POI) {
	known := map[string]bool{}
	if deps.KB != nil {
		if rows, err := deps.KB.GetPOIs(ctx, sys.ID); err == nil {
			for _, r := range rows {
				known[r.ID] = true
			}
		}
	}
	for _, p := range pois {
		if p.Type != poiWormholeEntrance {
			continue
		}
		tag := "known"
		if !known[p.ID] {
			tag = "NEW"
		}
		fmt.Fprintf(out, "🌀 WORMHOLE ENTRANCE (%s): %s [%s] in %s — %s\n", //nolint:errcheck
			tag, nonEmpty(p.Name, p.ID), p.ID, nonEmpty(sys.Name, sys.ID), wormholeDetail(p))
	}
}

// wormholeDetail summarizes what is known of a wormhole's far side and life.
func wormholeDetail(p game.POI) string {
	parts := []string{"destination uncharted"}
	if p.WormholeDestination != "" {
		parts[0] = "destination " + p.WormholeDestination
	}
	if p.WormholePredictionNeeded > 0 {
		parts = append(parts, fmt.Sprintf("prediction needed %d", p.WormholePredictionNeeded))
	}
	if p.ExpiresAt != "" {
		parts = append(parts, "expires "+p.ExpiresAt)
	}
	return strings.Join(parts, ", ")
}
