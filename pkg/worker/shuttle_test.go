package worker

import (
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/navigation"
)

func TestRankShuttleDestinations(t *testing.T) {
	// sol --- ac --- procyon
	//  |  \
	//  |   den (stronghold)
	//  f1 --- f2 --- f3
	graph := navigation.JumpGraph{
		"sol":     {"ac", "den", "f1"},
		"ac":      {"sol", "procyon"},
		"procyon": {"ac"},
		"den":     {"sol"},
		"f1":      {"sol", "f2"},
		"f2":      {"f1", "f3"},
		"f3":      {"f2"},
	}
	strongholds := map[string]bool{"den": true}

	waiting := []serverapi.StationPassenger{
		{Destination: "ac_station", DestinationSystem: "ac", DestinationName: "Alpha Centauri", EstimatedFare: 500},
		{Destination: "ac_station", DestinationSystem: "ac", EstimatedFare: 300},
		{Destination: "proc_station", DestinationSystem: "procyon", DestinationName: "Procyon", EstimatedFare: 1000},
		{Destination: "den_station", DestinationSystem: "den", DestinationName: "Pirate Den", EstimatedFare: 5000},
		{Destination: "far_station", DestinationSystem: "f3", DestinationName: "Far Reach", EstimatedFare: 2000},
		{Destination: "ghost_station", DestinationSystem: "nowhere", EstimatedFare: 9000},
	}

	// maxJumps=2 keeps ac (1) and procyon (2); excludes f3 (3 jumps).
	ranked := rankShuttleDestinations(waiting, "sol", graph, map[string]string{}, strongholds, 2, io.Discard)

	if len(ranked) != 2 {
		t.Fatalf("ranked = %d candidates, want 2 (got %+v)", len(ranked), ranked)
	}
	// Highest total fare first: Procyon (1000) over Alpha Centauri (800).
	if ranked[0].station != "proc_station" || ranked[0].fare != 1000 || ranked[0].jumpDist != 2 {
		t.Fatalf("ranked[0] = %+v, want proc_station fare 1000 @2 jumps", ranked[0])
	}
	if ranked[1].station != "ac_station" || ranked[1].fare != 800 || ranked[1].pax != 2 || ranked[1].jumpDist != 1 {
		t.Fatalf("ranked[1] = %+v, want ac_station fare 800 pax 2 @1 jump", ranked[1])
	}
	for _, c := range ranked {
		if c.station == "den_station" {
			t.Fatal("stronghold destination den_station must be excluded")
		}
		if c.station == "ghost_station" {
			t.Fatal("unresolvable-system destination ghost_station must be excluded")
		}
		if c.station == "far_station" {
			t.Fatal("over-budget destination far_station must be excluded")
		}
	}

	// A generous budget admits the far destination too.
	wide := rankShuttleDestinations(waiting, "sol", graph, map[string]string{}, strongholds, 8, io.Discard)
	if len(wide) != 3 {
		t.Fatalf("wide budget ranked = %d, want 3 (ac, procyon, far)", len(wide))
	}
}

func TestResolveShuttleSystemID(t *testing.T) {
	graph := navigation.JumpGraph{"sol": {"ac"}, "ac": {"sol"}}
	nameToID := map[string]string{"Alpha Centauri": "ac"}

	if got := resolveShuttleSystemID("ac", graph, nameToID); got != "ac" {
		t.Fatalf("raw id: got %q, want ac", got)
	}
	if got := resolveShuttleSystemID("Alpha Centauri", graph, nameToID); got != "ac" {
		t.Fatalf("display name: got %q, want ac", got)
	}
	if got := resolveShuttleSystemID("nowhere", graph, nameToID); got != "" {
		t.Fatalf("unknown: got %q, want empty", got)
	}
}
