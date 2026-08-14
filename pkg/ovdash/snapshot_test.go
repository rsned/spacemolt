package ovdash

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

func writeStatus(t *testing.T, dir, fleetFile, capturedAt string, ws []balances.LiveRecord) {
	t.Helper()
	b, err := json.Marshal(balances.StatusFile{CapturedAt: capturedAt, Workers: ws})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fleetFile+"-status.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSnapshotMergesAndResolvesSystems(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-5 * time.Second).Format(time.RFC3339)
	writeStatus(t, dir, "mission-learn", fresh, []balances.LiveRecord{
		{AgentID: "fighter-4", Role: "missionrunner", System: "Nova Terra",
			POI: "nova_terra_central", Docked: true, Credits: 7228,
			Hull: 350, MaxHull: 350, Healthy: true, Seen: true},
		{AgentID: "lost-1", System: "Atlantis", Seen: true, Healthy: true},
	})
	writeStatus(t, dir, "haul", fresh, []balances.LiveRecord{
		{AgentID: "hauler-0", Role: "hauler", System: "Sol", Credits: 100, Seen: true, Healthy: true},
	})

	s, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if len(s.Agents) != 2 {
		t.Fatalf("want 2 on-map agents, got %+v", s.Agents)
	}
	byID := map[string]AgentState{}
	for _, a := range s.Agents {
		byID[a.AgentID] = a
	}
	f4 := byID["fighter-4"]
	if f4.Fleet != "mission" || f4.SystemID != "nova_terra" || !f4.Docked {
		t.Fatalf("fighter-4 wrong: %+v", f4)
	}
	if h := byID["hauler-0"]; h.Fleet != "haul" || h.SystemID != "sol" {
		t.Fatalf("hauler-0 wrong: %+v", h)
	}
	// Unknown system name goes to OffMap, never dropped.
	if len(s.OffMap) != 1 || s.OffMap[0].AgentID != "lost-1" || s.OffMap[0].SystemName != "Atlantis" {
		t.Fatalf("off-map handling wrong: %+v", s.OffMap)
	}
	// Absent files are stale, present-and-fresh are not.
	stale := map[string]bool{}
	for _, f := range s.StaleFleets {
		stale[f] = true
	}
	if stale["mission"] || stale["haul"] {
		t.Fatalf("fresh fleets marked stale: %v", s.StaleFleets)
	}
	if !stale["craft"] || !stale["mb"] || !stale["assist"] || !stale["hunt"] || !stale["unlock"] {
		t.Fatalf("missing fleets must be stale: %v", s.StaleFleets)
	}
}

func TestReadSnapshotFlagsOldCaptureAsStale(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * time.Minute).Format(time.RFC3339)
	writeStatus(t, dir, "craft", old, []balances.LiveRecord{
		{AgentID: "craftsman-1", System: "Sol", Seen: true, Healthy: true},
	})
	s, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range s.StaleFleets {
		if f == "craft" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a 10-minute-old capture must be stale, got %v", s.StaleFleets)
	}
	// Stale still shows its (last-known) agents — grey-out is the frontend's job.
	if len(s.Agents) != 1 {
		t.Fatalf("stale fleet agents must still be listed, got %+v", s.Agents)
	}
}

func TestReadSnapshotSurfacesLeavingAndRemoved(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-5 * time.Second).Format(time.RFC3339)
	writeStatus(t, dir, "haul", fresh, []balances.LiveRecord{
		{AgentID: "hauler-0", Role: "hauler", System: "Sol", Credits: 100,
			Seen: true, Healthy: true, Leaving: true},
	})
	ov := supervisor.Overrides{Removed: []string{"hauler-9"}, By: "dashboard"}
	if err := supervisor.SaveOverrides(filepath.Join(dir, "haul-overrides.json"), ov); err != nil {
		t.Fatal(err)
	}

	s, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	var hauler0 AgentState
	found := false
	for _, a := range s.Agents {
		if a.AgentID == "hauler-0" {
			hauler0 = a
			found = true
		}
	}
	if !found || !hauler0.Leaving {
		t.Fatalf("hauler-0 leaving not surfaced: found=%v %+v", found, hauler0)
	}
	if got := s.Removed["haul"]; len(got) != 1 || got[0] != "hauler-9" {
		t.Fatalf("removed not surfaced: %+v", s.Removed)
	}
	// A fleet with no overrides sidecar must not appear in the map at all.
	if _, ok := s.Removed["craft"]; ok {
		t.Fatalf("fleet with no sidecar must be absent from Removed: %+v", s.Removed)
	}
}

func writeStatusOv(t *testing.T, dir, fleetFile, capturedAt string, ov balances.OvermindBuild, ws []balances.LiveRecord) {
	t.Helper()
	sf := balances.StatusFile{
		CapturedAt: capturedAt, Workers: ws,
		OvermindVersion: ov.Version, OvermindCommit: ov.Commit, OvermindBuiltAt: ov.BuiltAt,
		OvermindCodeDirty: ov.CodeDirty, OvermindModified: ov.Modified,
	}
	b, err := json.Marshal(sf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fleetFile+"-status.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSnapshotClassifiesVersionTiers(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-5 * time.Second).Format(time.RFC3339)
	newest := "2026-07-23T10:00:00Z" // the current build's built_at
	older := "2026-07-22T09:00:00Z"

	// haul overmind IS the current build (v0.3.0 @ newest).
	writeStatusOv(t, dir, "haul", fresh,
		balances.OvermindBuild{Version: "v0.3.0", BuiltAt: newest},
		[]balances.LiveRecord{
			{AgentID: "hauler-green", System: "Sol", Seen: true, Healthy: true,
				Version: "v0.3.0", BuiltAt: newest},
			{AgentID: "hauler-yellow-patch", System: "Sol", Seen: true, Healthy: true,
				Version: "v0.3.0-2-g8016cd8", BuiltAt: "2026-07-23T09:00:00Z"},
			{AgentID: "hauler-yellow-dirty", System: "Sol", Seen: true, Healthy: true,
				Version: "v0.3.0", BuiltAt: newest, CodeDirty: true},
			{AgentID: "hauler-red-minor", System: "Sol", Seen: true, Healthy: true,
				Version: "v0.2.9", BuiltAt: older},
			{AgentID: "hauler-red-legacy", System: "Sol", Seen: true, Healthy: true},
		})

	s, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	byID := map[string]AgentState{}
	for _, a := range s.Agents {
		byID[a.AgentID] = a
	}
	want := map[string]Tier{
		"hauler-green":        TierGreen,
		"hauler-yellow-patch": TierYellow,
		"hauler-yellow-dirty": TierYellow,
		"hauler-red-minor":    TierRed,
		"hauler-red-legacy":   TierRed,
	}
	for id, wt := range want {
		if got := byID[id].Tier; got != wt {
			t.Errorf("%s tier = %q, want %q", id, got, wt)
		}
	}
	ov, ok := s.Overminds["haul"]
	if !ok || ov.Version != "v0.3.0" || ov.Tier != TierGreen {
		t.Fatalf("haul overmind info wrong: %+v (ok=%v)", ov, ok)
	}
	// Rolled-up fleet tier = worst worker present = red (legacy + minor-behind).
	if ov.FleetTier != TierRed {
		t.Fatalf("haul FleetTier = %q, want red", ov.FleetTier)
	}
}

// The two binaries roll out independently, so a merged "current" hides the
// common case this dashboard exists to surface: a fleet whose overmind is
// current while its workers are stale. Each must be reported on its own newest
// build, chosen by build time rather than by SemVer ordering.
func TestSnapshotReportsCurrentOvermindAndWorkerSeparately(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 27, 20, 0, 0, 0, time.UTC)
	fresh := now.Add(-5 * time.Second).Format(time.RFC3339)
	write := func(file, ovVer, ovAt, wVer, wAt string) {
		t.Helper()
		sf := balances.StatusFile{
			CapturedAt:      fresh,
			OvermindVersion: ovVer,
			OvermindBuiltAt: ovAt,
			Workers: []balances.LiveRecord{{
				AgentID: "a-1", System: "Sol", Seen: true, Healthy: true,
				Version: wVer, BuiltAt: wAt,
			}},
		}
		b, err := json.Marshal(sf)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file+"-status.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The overmind moved on; the workers did not. The newest WORKER build is
	// older than the newest overmind build, so a build-time merge across both
	// would report the overmind's version for both fields.
	write("haul", "v0.2.7", "2026-07-27T19:00:00Z", "v0.2.5", "2026-07-20T10:00:00Z")
	write("mission-learn", "v0.2.6", "2026-07-26T19:00:00Z", "v0.2.6", "2026-07-26T19:00:00Z")

	s, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if s.CurrentOvermind != "v0.2.7" {
		t.Errorf("CurrentOvermind = %q, want v0.2.7", s.CurrentOvermind)
	}
	if s.CurrentWorker != "v0.2.6" {
		t.Errorf("CurrentWorker = %q, want v0.2.6 (newest WORKER build, not newest build overall)", s.CurrentWorker)
	}
}
