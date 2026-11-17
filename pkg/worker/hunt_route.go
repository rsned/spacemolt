package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// Hunter routes stay in empire-policed space (spec: "Grounds and routes:
// empire-policed space only"). The galaxy graphs and the Autopilot avoid set
// below are what every hunter leg is planned and flown on.

// huntPolicedGraph is the jump graph a hunter may fly: a jump a->b is kept
// only when a is policed (or is src, the system the ship is leaving) and b is
// policed. With homeWaiver, b may also be huntHomeSystem -- the final hop INTO
// cloverfield, never through it (its own jumps out are dropped unless it is
// src). Unknown systems count as unpoliced.
func huntPolicedGraph(conns []knowledge.Connection, systems []knowledge.System, src string, homeWaiver bool) navigation.JumpGraph {
	policed := make(map[string]bool, len(systems))
	for _, s := range systems {
		if s.PoliceLevel >= huntPolicedMinLevel {
			policed[s.ID] = true
		}
	}
	full := navigation.JumpGraphFromConnections(conns)
	out := make(navigation.JumpGraph, len(full))
	for a, nbs := range full {
		if a != src && !policed[a] {
			continue
		}
		for _, b := range nbs {
			if policed[b] || (homeWaiver && b == huntHomeSystem) {
				out[a] = append(out[a], b)
			}
		}
	}
	return out
}

// huntAutopilot is the AutopilotDeps for a hunter leg: every Lawless system
// (police_level below huntPolicedMinLevel, unknown level included) goes on
// the route's avoid set, so find_route's plan through one is replaced by a
// policed KB detour or refused. home is the operator's home-hunting waiver:
// cloverfield itself is left off the set (the hop into it and hunting in it),
// for legs whose destination is home or the home grounds only.
func huntAutopilot(ctx context.Context, deps HuntDeps, out io.Writer, home bool) (AutopilotDeps, error) {
	ap := AutopilotDeps{Client: deps.Client, Out: out, KB: deps.KB}
	if deps.KB == nil {
		return ap, fmt.Errorf("no knowledge base to check the route's policing")
	}
	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		return ap, fmt.Errorf("systems for the policed-route check: %w", err)
	}
	ap.ExtraAvoid = make(map[string]string)
	for _, s := range systems {
		if s.ID == "" || s.PoliceLevel >= huntPolicedMinLevel || (home && huntHomeLawlessWaiver(s.ID)) {
			continue
		}
		ap.ExtraAvoid[s.ID] = "Lawless: hunters fly empire-policed routes only"
	}
	return ap, nil
}

// huntFlyPoliced autopilots a hunter leg to systemID/poiID on a policed route
// (huntAutopilot).
func huntFlyPoliced(ctx context.Context, deps HuntDeps, out io.Writer, home bool, systemID, poiID string) error {
	ap, err := huntAutopilot(ctx, deps, out, home)
	if err != nil {
		return err
	}
	return Autopilot(ctx, ap, systemID, poiID)
}
