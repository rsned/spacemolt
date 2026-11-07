package worker

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// shuttleMap seeds systems linked in a line and a public station in each named
// station system.
func shuttleMap(t *testing.T, line []string, stations ...string) knowledge.Base {
	t.Helper()
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	t.Cleanup(func() { _ = kb.Close() })
	for i, id := range line {
		var conns []knowledge.SystemConnection
		if i > 0 {
			conns = append(conns, knowledge.SystemConnection{SystemID: line[i-1], Distance: 1})
		}
		if i < len(line)-1 {
			conns = append(conns, knowledge.SystemConnection{SystemID: line[i+1], Distance: 1})
		}
		if err := kb.RememberSystem(ctx, knowledge.System{ID: id, Name: id, Connections: conns}); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range stations {
		if err := kb.RememberPOI(ctx, knowledge.POI{ID: s + "-stn", SystemID: s, Type: "station"}); err != nil {
			t.Fatal(err)
		}
		if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: s + "-base", POIID: s + "-stn", PublicAccess: true}); err != nil {
			t.Fatal(err)
		}
	}

	return kb
}

func shuttleClient(system string) *fakeClient {
	st := &game.State{}
	st.System.ID = system

	return &fakeClient{state: st, route: []game.RouteStep{{SystemID: system}}}
}

// Live 2026-11-04, johnnycab: a dock timed out at Saiph's station, and recovery
// read "at the station POI" as a pending dock for 45 hours.
func TestShuttleRecoverDocksWhenAtStationPOI(t *testing.T) {
	kb := shuttleMap(t, []string{"saiph"}, "saiph")
	f := shuttleClient("saiph")

	if !shuttleRecoverIfStranded(context.Background(), ShuttleDeps{Client: f, KB: kb}, io.Discard, "saiph", "saiph-stn") {
		t.Fatal("a re-dock is a recovery and must end the pass")
	}
	if !slices.Contains(f.calls, "dock") {
		t.Fatalf("must retry the dock: %v", f.calls)
	}
	if !f.state.Doc {
		t.Fatal("not docked")
	}
}

// Live 2026-11-04, johnny_ca: the rotating pick landed on station-less
// neighbors and stayed put every time.
func TestRepositionSkipsNeighborsWithoutStations(t *testing.T) {
	kb := shuttleMap(t, []string{"empty", "here", "town"}, "here", "town")
	f := shuttleClient("here")
	deps := ShuttleDeps{Client: f, KB: kb, State: &shuttleState{}}

	// One pass: the cursor starts on "empty", which sorts first.
	repositionShuttle(context.Background(), deps, io.Discard, "here")
	if !slices.Contains(f.calls, "find_route:town") {
		t.Fatalf("want the neighbor with a station: %v", f.calls)
	}
}

func TestRepositionReachesPastStationlessNeighbors(t *testing.T) {
	kb := shuttleMap(t, []string{"here", "empty", "town"}, "here", "town")
	f := shuttleClient("here")

	repositionShuttle(context.Background(), ShuttleDeps{Client: f, KB: kb, State: &shuttleState{}}, io.Discard, "here")
	if !slices.Contains(f.calls, "find_route:town") {
		t.Fatalf("want the nearest station beyond the neighbors: %v", f.calls)
	}
}

// Live 2026-11-06, johnny_cab at Ashborne Reach: the nearest station refused
// the dock, and retrying it every pass kept the shuttle there for good.
func TestShuttleRecoverMovesOnWhenDockDenied(t *testing.T) {
	kb := shuttleMap(t, []string{"alula", "town"}, "alula", "town")
	f := shuttleClient("alula")
	f.dockErr = errors.New("Access denied")

	if !shuttleRecoverIfStranded(context.Background(), ShuttleDeps{Client: f, KB: kb}, io.Discard, "alula", "alula-stn") {
		t.Fatal("a relocation is a recovery and must end the pass")
	}
	if !slices.Contains(f.calls, "find_route:town") {
		t.Fatalf("want the next station: %v", f.calls)
	}
}

// A transient dock failure says nothing about the station, so the shuttle
// stays and retries rather than flying off.
func TestShuttleRecoverStaysOnTransientDockFailure(t *testing.T) {
	kb := shuttleMap(t, []string{"alula", "town"}, "alula", "town")
	f := shuttleClient("alula")
	f.dockErr = errors.New("connection reset")

	if !shuttleRecoverIfStranded(context.Background(), ShuttleDeps{Client: f, KB: kb}, io.Discard, "alula", "alula-stn") {
		t.Fatal("a re-dock attempt ends the pass")
	}
	if slices.Contains(f.calls, "find_route:town") {
		t.Fatalf("a transient failure must not relocate: %v", f.calls)
	}
}

// closedStationMap is empty - closed - town, where closed holds a player
// station that has refused the shuttle.
func closedStationMap(t *testing.T) (knowledge.Base, *StationAccess) {
	t.Helper()
	const closed = "fbdce01eaa4492be64ca3957c27cf56d"
	ctx := context.Background()
	kb := shuttleMap(t, []string{"empty", "closed", "town"}, "town")
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: closed, SystemID: "closed", Type: "station"}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: closed + "-base", POIID: closed, PublicAccess: true}); err != nil {
		t.Fatal(err)
	}
	access := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))
	access.RecordDock(closed, errors.New("Access denied"))

	return kb, access
}

func TestShuttleRecoverSkipsDeniedStation(t *testing.T) {
	kb, access := closedStationMap(t)
	f := shuttleClient("empty")

	shuttleRecoverIfStranded(context.Background(), ShuttleDeps{Client: f, KB: kb, Access: access}, io.Discard, "empty", "")
	if slices.Contains(f.calls, "find_route:closed") || !slices.Contains(f.calls, "find_route:town") {
		t.Fatalf("want town, not the denied station: %v", f.calls)
	}
}

func TestRepositionSkipsDeniedStation(t *testing.T) {
	kb, access := closedStationMap(t)
	f := shuttleClient("empty")
	deps := ShuttleDeps{Client: f, KB: kb, Access: access, State: &shuttleState{}}

	repositionShuttle(context.Background(), deps, io.Discard, "empty")
	if slices.Contains(f.calls, "find_route:closed") || !slices.Contains(f.calls, "find_route:town") {
		t.Fatalf("want town, not the denied station: %v", f.calls)
	}
}
