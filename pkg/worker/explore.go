package worker

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultExploreStaleTicks is the age, in game ticks, past which a visited
// system is worth re-capturing. ~1 day at 10s/tick (the KB system-freshness
// convention): 86400s / 10s = 8640 ticks.
const DefaultExploreStaleTicks int64 = 8640

// exploreClass ranks why a system is worth visiting; lower is higher priority.
type exploreClass int

const (
	classFrontier  exploreClass = iota // connection endpoint not yet a known system
	classUnvisited                     // known but never visited (LastVisitedTick == 0)
	classStale                         // known, visited, but stale
)

// NextExploreTarget picks the next system an explorer should visit from
// currentSystem, ranked by jump distance, preferring (1) undiscovered frontier
// systems — connection endpoints not yet in the KB's systems table — then
// (2) known-but-unvisited systems (LastVisitedTick == 0), then (3) stale known
// systems (nowTick-LastUpdatedTick > staleTicks). ok is false when nothing
// within reach is worth visiting, in which case target is "".
func NextExploreTarget(ctx context.Context, kb knowledge.Base, currentSystem string, staleTicks, nowTick int64) (string, bool, error) {
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return "", false, fmt.Errorf("explore: get connections: %w", err)
	}
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return "", false, fmt.Errorf("explore: get systems: %w", err)
	}

	graph := navigation.JumpGraphFromConnections(conns)

	// Node universe: current system, every graph endpoint, every known system.
	nodeSet := map[string]bool{currentSystem: true}
	for from, tos := range graph {
		nodeSet[from] = true
		for _, to := range tos {
			nodeSet[to] = true
		}
	}
	known := make(map[string]knowledge.System, len(systems))
	for _, s := range systems {
		known[s.ID] = s
		nodeSet[s.ID] = true
	}
	nodes := make([]string, 0, len(nodeSet))
	for id := range nodeSet {
		nodes = append(nodes, id)
	}

	dist := navigation.BFSJumps(graph, currentSystem, nodes)

	var bestID string
	var bestClass exploreClass
	bestDist := navigation.RouteInf
	have := false
	for _, id := range nodes {
		if id == currentSystem {
			continue
		}
		d, ok := dist[id]
		if !ok || d >= navigation.RouteInf {
			continue // unreachable
		}
		sys, isKnown := known[id]
		var class exploreClass
		switch {
		case !isKnown:
			class = classFrontier
		case sys.LastVisitedTick == 0:
			class = classUnvisited
		case nowTick-sys.LastUpdatedTick > staleTicks:
			class = classStale
		default:
			continue // known and fresh — skip
		}
		if !have || outranks(class, d, id, bestClass, bestDist, bestID) {
			bestID, bestClass, bestDist, have = id, class, d, true
		}
	}
	if !have {
		return "", false, nil
	}
	return bestID, true, nil
}

// outranks reports whether (class,d,id) beats the current best: lower class
// first, then smaller jump distance, then smaller system id (deterministic).
func outranks(class exploreClass, d int, id string, bClass exploreClass, bDist int, bID string) bool {
	if class != bClass {
		return class < bClass
	}
	if d != bDist {
		return d < bDist
	}
	return id < bID
}
