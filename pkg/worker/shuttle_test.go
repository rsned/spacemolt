package worker

import (
	"context"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// fakeShuttleClient is a minimal game.GameClient for boardBestDestination tests:
// it embeds the interface (unimplemented methods panic) and records LoadPassenger
// calls, returning a per-destination canned response (default: 0 boarded).
type fakeShuttleClient struct {
	game.GameClient
	loadResults map[string]*serverapi.LoadPassengerResponse
	loadCalls   []string
}

func (f *fakeShuttleClient) LoadPassenger(_ context.Context, destination string) (*serverapi.LoadPassengerResponse, error) {
	f.loadCalls = append(f.loadCalls, destination)
	if r, ok := f.loadResults[destination]; ok {
		return r, nil
	}
	return &serverapi.LoadPassengerResponse{Count: 0}, nil
}

// boardBestDestination must try EVERY ranked destination, not a fixed top-N: when
// the high-fare destinations are out-competed (0 boarded), it falls through to a
// winnable cheaper fare deep in the list rather than giving up.
func TestBoardBestDestinationTriesAllRanked(t *testing.T) {
	ranked := make([]shuttleCandidate, 0, 9)
	for i := range 8 {
		ranked = append(ranked, shuttleCandidate{station: "contested" + string(rune('A'+i))})
	}
	ranked = append(ranked, shuttleCandidate{station: "winnable", sysName: "Cheap", jumpDist: 2})

	fc := &fakeShuttleClient{loadResults: map[string]*serverapi.LoadPassengerResponse{
		"winnable": {Count: 1, TotalFare: 900},
	}}
	c, resp, ok := boardBestDestination(context.Background(), ShuttleDeps{Client: fc}, ranked, io.Discard)
	if !ok {
		t.Fatalf("expected a board on the 9th (winnable) destination, got ok=false")
	}
	if c.station != "winnable" || resp.Count != 1 || resp.TotalFare != 900 {
		t.Fatalf("boarded wrong candidate: station=%q count=%d fare=%d", c.station, resp.Count, resp.TotalFare)
	}
	if len(fc.loadCalls) != 9 {
		t.Fatalf("expected all 9 destinations tried (old top-6 cap removed), got %d: %v", len(fc.loadCalls), fc.loadCalls)
	}
}

// boardBestDestination stops at the first destination that boards anyone; it does
// not keep trying (and tick-spending) once a fare is secured.
func TestBoardBestDestinationStopsAtFirstBoard(t *testing.T) {
	ranked := []shuttleCandidate{
		{station: "first"},
		{station: "second"},
		{station: "third"},
	}
	fc := &fakeShuttleClient{loadResults: map[string]*serverapi.LoadPassengerResponse{
		"first":  {Count: 2, TotalFare: 5000},
		"second": {Count: 1, TotalFare: 1000},
	}}
	c, resp, ok := boardBestDestination(context.Background(), ShuttleDeps{Client: fc}, ranked, io.Discard)
	if !ok || c.station != "first" || resp.Count != 2 {
		t.Fatalf("expected to board 'first' and stop: ok=%v station=%q count=%d", ok, c.station, resp.Count)
	}
	if len(fc.loadCalls) != 1 {
		t.Fatalf("expected to stop after first board, but tried %d: %v", len(fc.loadCalls), fc.loadCalls)
	}
}

// With no destination accepting anyone, boardBestDestination reports no board.
func TestBoardBestDestinationAllContested(t *testing.T) {
	ranked := []shuttleCandidate{{station: "a"}, {station: "b"}}
	fc := &fakeShuttleClient{} // every LoadPassenger returns Count 0
	_, _, ok := boardBestDestination(context.Background(), ShuttleDeps{Client: fc}, ranked, io.Discard)
	if ok {
		t.Fatalf("expected ok=false when nothing boards")
	}
	if len(fc.loadCalls) != 2 {
		t.Fatalf("expected both contested destinations tried, got %d", len(fc.loadCalls))
	}
}

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
