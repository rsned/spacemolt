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
		CurrentSystem: "SOL", CurrentPOI: "ST-9",
		Credits: 5000, Hull: 80, MaxHull: 100, Fuel: 30, MaxFuel: 50,
	}
	now := time.Unix(1000, 0)
	got := buildStatus(st, "track_station", "t-1", now)
	if got.System != "SOL" || got.POI != "ST-9" || got.Credits != 5000 {
		t.Fatalf("buildStatus wrong: %+v", got)
	}
	if got.StandingBehavior != "track_station" || got.ActiveTaskID != "t-1" {
		t.Fatalf("buildStatus labels wrong: %+v", got)
	}
	if got.Timestamp == "" {
		t.Fatalf("timestamp missing")
	}
	// Docked derived as CurrentPOI != "" && !Traveling; Traveling defaults to false.
	if !got.Docked {
		t.Fatalf("expected docked=true when POI set and not traveling, got false")
	}
	ks := buildKnownState(st, 7)
	if ks.System != "SOL" || ks.Credits != 5000 || ks.Tick != 7 {
		t.Fatalf("buildKnownState wrong: %+v", ks)
	}
	if !ks.Docked {
		t.Fatalf("expected ks.Docked=true, got false")
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
