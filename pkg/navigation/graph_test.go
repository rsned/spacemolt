package navigation

import (
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// conn is a terse constructor for the table below.
func conn(from, to string) knowledge.Connection {
	return knowledge.Connection{FromSystem: from, ToSystem: to}
}

// hole is a one-way link: a Crimson wormhole, flyable only from -> to.
func hole(from, to string) knowledge.Connection {
	return knowledge.Connection{FromSystem: from, ToSystem: to, OneWay: true}
}

// TestJumpGraphFromConnections_HonoursOneWayLinks pins the wormhole case. An
// ordinary connection is stored as a row in each direction and must be
// traversable both ways; a Crimson wormhole is stored once and must NOT gain a
// reverse edge, or BFS invents a shortcut the server refuses to fly.
func TestJumpGraphFromConnections_HonoursOneWayLinks(t *testing.T) {
	g := JumpGraphFromConnections([]knowledge.Connection{
		conn("ashford", "farpoint"), conn("farpoint", "ashford"), // ordinary, both rows
		conn("bunda", "copernicus"), // ordinary, only ONE row: half-surveyed
		hole("iron_reach", "ashford"),
		hole("iron_reach", "sirius"),
	})

	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"ashford", "farpoint", true},
		{"farpoint", "ashford", true},
		// A half-surveyed ordinary link must still be flyable both ways, or
		// routing dies at the frontier.
		{"bunda", "copernicus", true},
		{"copernicus", "bunda", true},
		{"iron_reach", "ashford", true},  // forward: allowed
		{"ashford", "iron_reach", false}, // reverse of a wormhole: must not exist
		{"iron_reach", "sirius", true},
		{"sirius", "iron_reach", false},
	} {
		if got := slices.Contains(g[tc.from], tc.to); got != tc.want {
			t.Errorf("edge %s -> %s present = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

// TestJumpGraphFromConnections_ShortestPathRespectsDirection is the regression
// proper: the real Cargo Lanes -> Sirius shape. Reversing the two iron_reach
// wormholes yields a 3-jump path; honouring them forces the long way round,
// which is what the server's find_route reports.
func TestJumpGraphFromConnections_ShortestPathRespectsDirection(t *testing.T) {
	both := func(a, b string) []knowledge.Connection {
		return []knowledge.Connection{conn(a, b), conn(b, a)}
	}
	var conns []knowledge.Connection
	for _, pair := range [][2]string{
		{"cargo_lanes", "ashford"},
		{"cargo_lanes", "bunda"}, {"bunda", "copernicus"},
		{"copernicus", "sol"}, {"sol", "sirius"},
	} {
		conns = append(conns, both(pair[0], pair[1])...)
	}
	// The wormholes: reachable only outward from iron_reach.
	conns = append(conns, hole("iron_reach", "ashford"), hole("iron_reach", "sirius"))

	dist := BFSJumps(JumpGraphFromConnections(conns), "cargo_lanes", []string{"sirius"})
	if got, want := dist["sirius"], 4; got != want {
		t.Errorf("cargo_lanes -> sirius = %d jumps, want %d "+
			"(3 would mean BFS travelled a wormhole backwards via iron_reach)", got, want)
	}
}
