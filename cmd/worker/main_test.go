package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/worker"
)

func TestBuildStatusAndKnownState(t *testing.T) {
	st := &game.State{
		CurrentSystem: "SOL", CurrentPOI: "ST-9", Doc: true,
		Credits: 5000, Hull: 80, MaxHull: 100, Fuel: 30, MaxFuel: 50,
	}
	now := time.Unix(1000, 0)
	got := buildStatus(st, "track_station", "t-1", false, now)
	if got.System != "SOL" || got.POI != "ST-9" || got.Credits != 5000 {
		t.Fatalf("buildStatus wrong: %+v", got)
	}
	if got.StandingBehavior != "track_station" || got.ActiveTaskID != "t-1" {
		t.Fatalf("buildStatus labels wrong: %+v", got)
	}
	if got.Timestamp == "" {
		t.Fatalf("timestamp missing")
	}
	// Docked reflects the authoritative st.Doc (set true at a real station) and
	// !Traveling; Traveling defaults to false here.
	if !got.Docked {
		t.Fatalf("expected docked=true when docked at a station and not traveling, got false")
	}
	ks := buildKnownState(st, 7)
	if ks.System != "SOL" || ks.Credits != 5000 || ks.Tick != 7 {
		t.Fatalf("buildKnownState wrong: %+v", ks)
	}
	if !ks.Docked {
		t.Fatalf("expected ks.Docked=true, got false")
	}
}

// buildStatus must NOT report docked when the worker is merely parked at a
// non-station POI (a gas cloud passed through mid-route) or is in transit — the
// stale-docked phantom that made haulers/shuttles look docked in station-less
// systems like Scheat and HR 8832.
func TestBuildStatusDockedOnlyAtStation(t *testing.T) {
	now := time.Unix(1000, 0)
	// Parked at a gas cloud: has a POI, but the server flag is false.
	parked := &game.State{CurrentSystem: "scheat", CurrentPOI: "scheat_haze", Doc: false}
	if buildStatus(parked, "shuttle", "", false, now).Docked {
		t.Fatal("parked at a non-station POI must report Docked=false")
	}
	// In transit: Doc may linger from the last dock, but Traveling is set.
	transit := &game.State{CurrentSystem: "sol", CurrentPOI: "sol_station", Doc: true, Traveling: true}
	if buildStatus(transit, "hauler", "", false, now).Docked {
		t.Fatal("in transit must report Docked=false even with a lingering Doc")
	}
	if buildKnownState(transit, 1).Docked {
		t.Fatal("buildKnownState in transit must report Docked=false")
	}
}

func TestBuildStatusPrefersSystemDisplayName(t *testing.T) {
	// CurrentSystem holds the raw id (as player-data responses write it); the
	// SystemData carries the matching display name. The heartbeat must report the
	// name so the fleet report is consistent rather than half ids / half names.
	st := &game.State{CurrentSystem: "the_crucible", CurrentPOI: "the_crucible_complex"}
	st.System = game.SystemData{ID: "the_crucible", Name: "The Crucible"}
	got := buildStatus(st, "hauler", "", false, time.Unix(1000, 0))
	if got.System != "The Crucible" {
		t.Fatalf("System = %q, want display name %q", got.System, "The Crucible")
	}
}

func TestBuildStatusFallsBackWhenSystemDataStale(t *testing.T) {
	// SystemData describes a different system than CurrentSystem (stale mid-travel):
	// fall back to CurrentSystem rather than mislabel the worker's location.
	st := &game.State{CurrentSystem: "krynn"}
	st.System = game.SystemData{ID: "sol", Name: "Sol"}
	got := buildStatus(st, "hauler", "", false, time.Unix(1000, 0))
	if got.System != "krynn" {
		t.Fatalf("System = %q, want fallback %q", got.System, "krynn")
	}
}

func TestBuildStatusCarriesDrained(t *testing.T) {
	st := &game.State{CurrentSystem: "sol"}
	if got := buildStatus(st, "hauler", "", true, time.Unix(1000, 0)); !got.Drained {
		t.Fatalf("Drained = false, want true")
	}
	if got := buildStatus(st, "hauler", "", false, time.Unix(1000, 0)); got.Drained {
		t.Fatalf("Drained = true, want false")
	}
}

func TestRolesConfigLoads(t *testing.T) {
	roles, err := worker.LoadRoles(filepath.Join("..", "..", "data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	if _, ok := roles["resident"]; !ok {
		t.Fatal("resident role missing from seeded roles.yaml")
	}
}

// fakeReconnectable records whether nudgeReconnect probed the connection and
// whether it asked for a reconnect.
type fakeReconnectable struct {
	connected      bool
	requestReturns bool
	requested      int
}

func (f *fakeReconnectable) IsConnected() bool { return f.connected }
func (f *fakeReconnectable) RequestReconnect() bool {
	f.requested++
	return f.requestReturns
}

func TestNudgeReconnect(t *testing.T) {
	// Connected: must NOT request a reconnect — a live worker is left alone.
	connected := &fakeReconnectable{connected: true}
	if nudgeReconnect(connected) {
		t.Fatal("connected client should not request reconnect")
	}
	if connected.requested != 0 {
		t.Fatalf("connected client requested reconnect %d times, want 0", connected.requested)
	}

	// Disconnected + dormant: re-arm a fresh burst (RequestReconnect -> true).
	dormant := &fakeReconnectable{connected: false, requestReturns: true}
	if !nudgeReconnect(dormant) {
		t.Fatal("disconnected dormant client should report it started a reconnect")
	}
	if dormant.requested != 1 {
		t.Fatalf("disconnected client requested reconnect %d times, want 1", dormant.requested)
	}

	// Disconnected but a burst is already running: still safe to call, reports
	// false (no new burst), so the heartbeat loop stays quiet.
	busy := &fakeReconnectable{connected: false, requestReturns: false}
	if nudgeReconnect(busy) {
		t.Fatal("client with an in-flight burst should report no new reconnect")
	}
	if busy.requested != 1 {
		t.Fatalf("client requested reconnect %d times, want 1", busy.requested)
	}
}
