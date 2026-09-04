// Package navigation holds pure routing algorithms (BFS shortest-hops and
// Held-Karp / nearest-neighbor waypoint ordering) over a jump graph. It has no
// game-client or output dependency so it can be reused by worker autopilot,
// play_as plan_route, and the overmind tactical planner.
package navigation

import (
	"slices"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// JumpGraph is a directed adjacency list where each edge is one jump. Ordinary
// connections appear in both directions; a one-way link appears in one.
type JumpGraph map[string][]string

// JumpGraphFromConnections builds a jump graph from KB connections. Each edge
// counts as a single jump; connections with an empty endpoint are skipped, and
// duplicate edges are collapsed.
//
// An ordinary connection is added in BOTH directions even when the KB holds
// only one row for it, because RememberSystem records a system's links
// outward-only (sys.ID -> conn.SystemID): until both endpoints have been
// surveyed, a perfectly ordinary two-way route exists as a single row, and
// refusing to reverse it would strand routing in half-explored space.
//
// A connection marked OneWay is the exception and is added in its stored
// direction alone. Those are the Crimson wormholes revealed by the pirate
// reputation chains; reversing one invents a shortcut the server will not fly.
// That is how find_item came to report Cargo Lanes -> Sirius as 5 jumps when
// the server's own find_route says 14 -- BFS "returned" through iron_reach
// against the flow of two wormholes. More appear as the campaign progresses,
// so the distinction is structural rather than a special case. See
// knowledge.Connection.OneWay for how it is told apart (the giveaway is
// `distance`, which on an ordinary row equals the systems' spatial separation
// exactly and on a wormhole carries its own traversal cost).
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
		if !c.OneWay {
			add(c.ToSystem, c.FromSystem)
		}
	}
	return graph
}
