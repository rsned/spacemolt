// Package navigation holds pure routing algorithms (BFS shortest-hops and
// Held-Karp / nearest-neighbor waypoint ordering) over a jump graph. It has no
// game-client or output dependency so it can be reused by worker autopilot,
// play_as plan_route, and the overmind tactical planner.
package navigation

import (
	"slices"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// JumpGraph is an undirected adjacency list where each edge is one jump.
type JumpGraph map[string][]string

// JumpGraphFromConnections builds an undirected jump graph from KB connections.
// Each edge counts as a single jump; connections with an empty endpoint are
// skipped, and duplicate edges are collapsed.
func JumpGraphFromConnections(conns []knowledge.Connection) JumpGraph {
	graph := make(JumpGraph)
	add := func(a, b string) {
		if slices.Contains(graph[a], b) {
			return
		}
		graph[a] = append(graph[a], b)
	}
	for _, c := range conns {
		if c.FromSystem == "" || c.ToSystem == "" {
			continue
		}
		add(c.FromSystem, c.ToSystem)
		add(c.ToSystem, c.FromSystem)
	}
	return graph
}
