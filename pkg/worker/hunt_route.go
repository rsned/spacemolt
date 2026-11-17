package worker

import (
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
