package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	_ "modernc.org/sqlite"
)

// testDBPath returns the path of a fresh, fully migrated, empty database in
// tb.TempDir(). It clones a template migrated once per process instead of
// replaying the migration chain, which costs seconds under -race.
func testDBPath(tb testing.TB) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "knowledge.db")
	if err := WriteMigratedDB(path); err != nil {
		tb.Fatalf("WriteMigratedDB: %v", err)
	}
	return path
}

func newTestSQLiteKB(t *testing.T) *SQLiteKB {
	kb, err := NewSQLiteKB(Config{
		DBPath:       testDBPath(t),
		WAL:          false,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
		BusyTimeout:  1 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create test SQLiteKB: %v", err)
	}
	return kb
}

func TestSQLiteKB_RememberSystem(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	sys := System{
		ID:          "SYS-001",
		Name:        "Test System",
		Position:    game.Position{X: 100.0, Y: 200.0, Z: 300.0},
		PoliceLevel: 3,
		Empire:      "test_empire",
		Connections: []SystemConnection{{SystemID: "SYS-002"}, {SystemID: "SYS-003"}},
	}

	if err := kb.RememberSystem(ctx, sys); err != nil {
		t.Fatalf("RememberSystem failed: %v", err)
	}

	// Retrieve and verify
	retrieved, err := kb.GetSystem(ctx, "SYS-001")
	if err != nil {
		t.Fatalf("GetSystem failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("GetSystem returned nil")
	}

	if retrieved.Name != sys.Name {
		t.Errorf("Expected name %s, got %s", sys.Name, retrieved.Name)
	}
}

// TestSQLiteKB_RememberSystem_StrongholdSticky guards against the regression
// where re-visiting a stronghold erased its is_stronghold flag. get_system does
// not carry is_stronghold (only get_map does), so a live scan decodes the field
// to false; a plain overwrite would clear a known stronghold on every visit.
// RememberSystem must keep the flag set (false->true allowed, true->false not).
func TestSQLiteKB_RememberSystem_StrongholdSticky(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// First write: known stronghold (e.g. from a map import).
	if err := kb.RememberSystem(ctx, System{
		ID: "SYS-STR", Name: "Pirate Home", IsStronghold: true, LastVisitedTick: 0,
	}); err != nil {
		t.Fatalf("RememberSystem (stronghold) failed: %v", err)
	}

	// Second write: a get_system visit, which omits is_stronghold -> false.
	if err := kb.RememberSystem(ctx, System{
		ID: "SYS-STR", Name: "Pirate Home", IsStronghold: false, LastVisitedTick: 100,
	}); err != nil {
		t.Fatalf("RememberSystem (visit) failed: %v", err)
	}

	got, err := kb.GetSystem(ctx, "SYS-STR")
	if err != nil {
		t.Fatalf("GetSystem failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetSystem returned nil")
	}
	if !got.IsStronghold {
		t.Error("is_stronghold was cleared by a re-visit; expected it to remain true")
	}
}

// TestSQLiteKB_RememberSystem_DoesNotWriteEmpire guards the root-cause fix
// for KB empire ownership rot: get_system's "empire" field means regional
// space affiliation, while the systems.empire column means ownership
// (populated only by get_map / UpsertSystemFromMap). RememberSystem — fed
// from get_system captures — must never write the empire column, on either
// the first-seen INSERT or a subsequent UPDATE, so a regional value can
// never masquerade as an ownership value.
func TestSQLiteKB_RememberSystem_DoesNotWriteEmpire(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Seed ownership via the map path, as would happen in production.
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "sys-own", Name: "Owned System", Empire: "crimson",
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap (seed): %v", err)
	}

	// A get_system visit reports a *different* value in the empire field
	// (regional affiliation, not ownership) via RememberSystem.
	if err := kb.RememberSystem(ctx, System{
		ID: "sys-own", Name: "Owned System", Empire: "nebula",
		LastUpdatedTick: 50, LastVisitedTick: 50,
	}); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}

	got, err := kb.GetSystem(ctx, "sys-own")
	if err != nil || got == nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.Empire != "crimson" {
		t.Errorf("Empire = %q after RememberSystem, want %q (RememberSystem must not touch ownership)", got.Empire, "crimson")
	}

	// Same check on a brand-new row: a first-seen system captured only via
	// get_system must not seed the ownership column with the regional value.
	if err := kb.RememberSystem(ctx, System{
		ID: "sys-fresh", Name: "Fresh System", Empire: "voidborn",
		LastUpdatedTick: 10, LastVisitedTick: 10,
	}); err != nil {
		t.Fatalf("RememberSystem (fresh): %v", err)
	}
	gotFresh, err := kb.GetSystem(ctx, "sys-fresh")
	if err != nil || gotFresh == nil {
		t.Fatalf("GetSystem (fresh): %v", err)
	}
	if gotFresh.Empire != "" {
		t.Errorf("Empire = %q for a system first seen via RememberSystem, want \"\" (no ownership knowledge)", gotFresh.Empire)
	}
}

func TestSQLiteKB_GetSystem_NotFound(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	sys, err := kb.GetSystem(ctx, "NONEXISTENT")
	if err != nil {
		t.Fatalf("GetSystem with non-existent ID failed: %v", err)
	}

	if sys != nil {
		t.Error("Expected nil for non-existent system, got non-nil")
	}
}

func TestSQLiteKB_RememberConnection(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Remember connection
	if err := kb.RememberConnection(ctx, "A", "B"); err != nil {
		t.Fatalf("RememberConnection failed: %v", err)
	}

	// Remember same connection again (should be idempotent)
	if err := kb.RememberConnection(ctx, "A", "B"); err != nil {
		t.Fatalf("Second RememberConnection failed: %v", err)
	}

	// Create a system and verify connections
	sys := System{
		ID:          "A",
		Name:        "System A",
		Position:    game.Position{X: 0, Y: 0, Z: 0},
		Connections: []SystemConnection{{SystemID: "B"}},
	}

	if err := kb.RememberSystem(ctx, sys); err != nil {
		t.Fatalf("RememberSystem failed: %v", err)
	}

	retrieved, err := kb.GetSystem(ctx, "A")
	if err != nil {
		t.Fatalf("GetSystem failed: %v", err)
	}

	if len(retrieved.Connections) != 1 {
		t.Errorf("Expected 1 connection, got %d", len(retrieved.Connections))
	}

	if len(retrieved.Connections) > 0 && retrieved.Connections[0].SystemID != "B" {
		t.Errorf("Expected connection to B, got %s", retrieved.Connections[0].SystemID)
	}
}

func TestSQLiteKB_GetUnknownConnections(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Create connections from A to B and C
	if err := kb.RememberConnection(ctx, "A", "B"); err != nil {
		t.Fatalf("RememberConnection A->B failed: %v", err)
	}
	if err := kb.RememberConnection(ctx, "A", "C"); err != nil {
		t.Fatalf("RememberConnection A->C failed: %v", err)
	}

	// Add system A as visited
	sysA := System{
		ID:       "A",
		Name:     "System A",
		Position: game.Position{X: 0, Y: 0, Z: 0},
	}
	if err := kb.RememberSystem(ctx, sysA); err != nil {
		t.Fatalf("RememberSystem A failed: %v", err)
	}

	// Add system B as visited (system with visit_count > 0)
	sysB := System{
		ID:       "B",
		Name:     "System B",
		Position: game.Position{X: 100, Y: 0, Z: 0},
	}
	if err := kb.RememberSystem(ctx, sysB); err != nil {
		t.Fatalf("RememberSystem B failed: %v", err)
	}

	// Get unknown connections from A
	unknown, err := kb.GetUnknownConnections(ctx, "A")
	if err != nil {
		t.Fatalf("GetUnknownConnections failed: %v", err)
	}

	// Only C should be unknown (B was visited)
	if len(unknown) != 1 {
		t.Errorf("Expected 1 unknown connection, got %d", len(unknown))
	}

	if len(unknown) > 0 && unknown[0] != "C" {
		t.Errorf("Expected unknown connection to C, got %s", unknown[0])
	}
}

func TestSQLiteKB_RememberPOI(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	poi := POI{
		ID:          "POI-001",
		SystemID:    "SYS-001",
		Name:        "Test Station",
		Type:        "station",
		Description: "A test station",
		Position:    game.Position{X: 10.0, Y: 20.0},
	}

	if err := kb.RememberPOI(ctx, poi); err != nil {
		t.Fatalf("RememberPOI failed: %v", err)
	}

	// Verify POI was stored (we'd need a GetPOI method for full verification)
	// For now, just ensure no error occurred
}

func TestSQLiteKB_AddExperience(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	agentID := "agent-1"

	// Add experiences
	for i := range 5 {
		if err := kb.AddExperience(ctx, agentID, "test", "description", "success", "loc"); err != nil {
			t.Fatalf("AddExperience %d failed: %v", i, err)
		}
	}

	// Get recent experiences
	exps, err := kb.GetRecentExperiences(ctx, agentID, 3)
	if err != nil {
		t.Fatalf("GetRecentExperiences failed: %v", err)
	}

	if len(exps) != 3 {
		t.Errorf("Expected 3 experiences, got %d", len(exps))
	}

	for i, exp := range exps {
		if exp.Type != "test" {
			t.Errorf("Experience %d: expected type 'test', got '%s'", i, exp.Type)
		}
	}
}

func TestSQLiteKB_AddExperience_Limit(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	agentID := "agent-limit"

	// Add 150 experiences (should keep only last 100)
	for i := range 150 {
		if err := kb.AddExperience(ctx, agentID, "test", "description", "success", "loc"); err != nil {
			t.Fatalf("AddExperience %d failed: %v", i, err)
		}
	}

	// Get all experiences (should be limited to 100)
	exps, err := kb.GetRecentExperiences(ctx, agentID, 200)
	if err != nil {
		t.Fatalf("GetRecentExperiences failed: %v", err)
	}

	if len(exps) != 100 {
		t.Errorf("Expected 100 experiences (limited), got %d", len(exps))
	}
}

func TestSQLiteKB_RegisterAgent(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	if err := kb.RegisterAgent(ctx, "agent-1", "Test Agent", "explorer", "test_faction", nil); err != nil {
		t.Fatalf("RegisterAgent failed: %v", err)
	}

	// Verify agent was registered (we'd need a GetAgent method for full verification)
	// For now, just ensure no error occurred
}

func TestSQLiteKB_GetSystems(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Add some systems
	systems := []System{
		{ID: "A", Name: "System A", Position: game.Position{X: 0, Y: 0, Z: 0}, Connections: []SystemConnection{{SystemID: "B", Distance: 10}}},
		{ID: "B", Name: "System B", Position: game.Position{X: 100, Y: 0, Z: 0}, Connections: []SystemConnection{{SystemID: "A", Distance: 10}, {SystemID: "C", Distance: 20}}},
		{ID: "C", Name: "System C", Position: game.Position{X: 200, Y: 0, Z: 0}, Connections: []SystemConnection{{SystemID: "B", Distance: 20}}},
	}

	for _, sys := range systems {
		if err := kb.RememberSystem(ctx, sys); err != nil {
			t.Fatalf("RememberSystem %s failed: %v", sys.ID, err)
		}
	}

	// Get all systems
	retrieved, err := kb.GetSystems(context.Background())
	if err != nil {
		t.Fatalf("failed to get systems: %v", err)
	}
	if len(retrieved) != 3 {
		t.Errorf("Expected 3 systems, got %d", len(retrieved))
	}

	// Create a map for easy lookup
	sysMap := make(map[string]System)
	for _, sys := range retrieved {
		sysMap[sys.ID] = sys
	}

	// Verify each system has correct connections
	if len(sysMap["A"].Connections) != 1 || sysMap["A"].Connections[0].SystemID != "B" {
		t.Error("System A has incorrect connections")
	}
	if sysMap["A"].Connections[0].Distance != 10 {
		t.Errorf("System A->B distance: got %d, want 10", sysMap["A"].Connections[0].Distance)
	}

	if len(sysMap["B"].Connections) != 2 {
		t.Error("System B should have 2 connections")
	}

	if len(sysMap["C"].Connections) != 1 || sysMap["C"].Connections[0].SystemID != "B" {
		t.Error("System C has incorrect connections")
	}
	if sysMap["C"].Connections[0].Distance != 20 {
		t.Errorf("System C->B distance: got %d, want 20", sysMap["C"].Connections[0].Distance)
	}
}

func TestSQLiteKB_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sysID := fmt.Sprintf("SYS-%d", idx)
			sys := System{
				ID:       sysID,
				Name:     fmt.Sprintf("System %d", idx),
				Position: game.Position{X: float64(idx), Y: 0, Z: 0},
			}
			_ = kb.RememberSystem(ctx, sys)
		}(i)
	}

	wg.Wait()

	// Verify all systems were added
	systems, err := kb.GetSystems(context.Background())
	if err != nil {
		t.Fatalf("failed to get systems: %v", err)
	}
	if len(systems) < 10 {
		t.Errorf("Expected at least 10 systems after concurrent writes, got %d", len(systems))
	}
}

// BenchmarkSQLiteKB_RememberSystem benchmarks RememberSystem
func BenchmarkSQLiteKB_RememberSystem(b *testing.B) {
	kb, err := NewSQLiteKB(Config{
		DBPath:       testDBPath(b),
		WAL:          false,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
		BusyTimeout:  1 * time.Second,
	})
	if err != nil {
		b.Fatalf("Failed to create SQLiteKB: %v", err)
	}
	defer func() { _ = kb.Close() }()

	ctx := context.Background()
	sys := System{
		ID:       "BENCH-001",
		Name:     "Bench System",
		Position: game.Position{X: 0, Y: 0, Z: 0},
	}

	b.ResetTimer()
	for b.Loop() {
		if err := kb.RememberSystem(ctx, sys); err != nil {
			b.Fatalf("RememberSystem failed: %v", err)
		}
	}
}

// BenchmarkMemoryKB_RememberSystem benchmarks MemoryKB for comparison
func BenchmarkMemoryKB_RememberSystem(b *testing.B) {
	kb := NewMemoryKB()
	ctx := context.Background()
	sys := System{
		ID:       "BENCH-001",
		Name:     "Bench System",
		Position: game.Position{X: 0, Y: 0, Z: 0},
	}

	b.ResetTimer()
	for b.Loop() {
		if err := kb.RememberSystem(ctx, sys); err != nil {
			b.Fatalf("RememberSystem failed: %v", err)
		}
	}
}

func TestSQLiteKB_Close(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)

	if err := kb.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Double close should be safe
	if err := kb.Close(); err != nil {
		t.Fatalf("Second Close failed: %v", err)
	}
}

func TestSQLiteKB_Persistence(t *testing.T) {
	t.Parallel()
	// Create a temporary database file
	tmpFile, err := os.CreateTemp("", "spacemolt-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_ = tmpFile.Close()

	ctx := context.Background()

	// Create KB and add data
	kb1, err := NewSQLiteKB(Config{DBPath: tmpFile.Name(), WAL: false})
	if err != nil {
		t.Fatalf("Failed to create first SQLiteKB: %v", err)
	}

	sys := System{
		ID:       "PERSIST-001",
		Name:     "Persistent System",
		Position: game.Position{X: 42, Y: 42, Z: 42},
	}

	if err := kb1.RememberSystem(ctx, sys); err != nil {
		t.Fatalf("RememberSystem failed: %v", err)
	}

	if err := kb1.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen and verify
	kb2, err := NewSQLiteKB(Config{DBPath: tmpFile.Name(), WAL: false})
	if err != nil {
		t.Fatalf("Failed to create second SQLiteKB: %v", err)
	}
	defer func() { _ = kb2.Close() }()

	retrieved, err := kb2.GetSystem(ctx, "PERSIST-001")
	if err != nil {
		t.Fatalf("GetSystem failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("System was not persisted")
	}

	if retrieved.Name != "Persistent System" {
		t.Errorf("Expected name 'Persistent System', got '%s'", retrieved.Name)
	}

	if retrieved.Position.X != 42 {
		t.Errorf("Expected X=42, got %f", retrieved.Position.X)
	}
}

func TestMemoryKB_RememberSystem_PersistsLastVisitedTick(t *testing.T) {
	ctx := context.Background()
	kb := NewMemoryKB()

	sys := System{ID: "sys-a", Name: "Alpha", LastUpdatedTick: 50, LastVisitedTick: 100}
	if err := kb.RememberSystem(ctx, sys); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}

	got, err := kb.GetSystem(ctx, "sys-a")
	if err != nil || got == nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.LastVisitedTick != 100 {
		t.Errorf("LastVisitedTick = %d, want 100", got.LastVisitedTick)
	}
	if !got.Visited() {
		t.Error("Visited() = false, want true")
	}

	// Overwrite with zero tick — should preserve the previous non-zero value.
	sys2 := System{ID: "sys-a", Name: "Alpha", LastUpdatedTick: 60, LastVisitedTick: 0}
	if err := kb.RememberSystem(ctx, sys2); err != nil {
		t.Fatalf("RememberSystem (2): %v", err)
	}
	got, err = kb.GetSystem(ctx, "sys-a")
	if err != nil || got == nil {
		t.Fatalf("GetSystem (2): %v", err)
	}
	if got.LastVisitedTick != 100 {
		t.Errorf("LastVisitedTick after zero-tick overwrite = %d, want 100 (preserved)", got.LastVisitedTick)
	}
}

func TestSQLiteKB_UpsertSystemFromMap_PreservesLastVisitedTick(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	visited := System{
		ID: "sys-visited", Name: "Visited", Empire: "solarian",
		PoliceLevel: 2, SecurityStatus: "medium_sec",
		LastUpdatedTick: 100, LastVisitedTick: 100,
	}
	if err := kb.RememberSystem(ctx, visited); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}

	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "sys-visited", Name: "Visited",
		PositionX: 10, PositionY: 20, Empire: "solarian",
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap: %v", err)
	}

	got, err := kb.GetSystem(ctx, "sys-visited")
	if err != nil || got == nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.LastVisitedTick != 100 {
		t.Errorf("LastVisitedTick = %d, want 100 (map import must not clobber)", got.LastVisitedTick)
	}
	if !got.Visited() {
		t.Error("Visited() = false after map re-import, want true")
	}
}

// TestSQLiteKB_UpsertSystemFromMap_SetsAndClearsEmpire pins get_map (via
// UpsertSystemFromMap) as the sole authority for systems.empire (ownership).
// A single get_map fetch enumerates every system, including ones that have
// been de-owned since the last import (empire omitted -> ""), and
// cmd/data/import-map-data upserts every system in that response — so, unlike
// the sticky is_stronghold field, empire must be an authoritative overwrite:
// re-importing with an empty empire must clear a previously known owner.
func TestSQLiteKB_UpsertSystemFromMap_SetsAndClearsEmpire(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "sys-owned", Name: "Owned", Empire: "solarian",
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap (set): %v", err)
	}
	got, err := kb.GetSystem(ctx, "sys-owned")
	if err != nil || got == nil {
		t.Fatalf("GetSystem (set): %v", err)
	}
	if got.Empire != "solarian" {
		t.Fatalf("Empire = %q, want %q", got.Empire, "solarian")
	}

	// Re-import from a fresh get_map fetch where this system is no longer
	// owned (empire omitted -> decodes to "").
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "sys-owned", Name: "Owned",
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap (clear): %v", err)
	}
	got, err = kb.GetSystem(ctx, "sys-owned")
	if err != nil || got == nil {
		t.Fatalf("GetSystem (clear): %v", err)
	}
	if got.Empire != "" {
		t.Errorf("Empire = %q after re-import with no owner, want \"\" (map import must clear stale ownership)", got.Empire)
	}
}

func TestSQLiteKB_UpsertSystemFromMap_LeavesFreshSystemUnexplored(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "sys-new", Name: "New", PositionX: 1, PositionY: 2,
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap: %v", err)
	}

	got, err := kb.GetSystem(ctx, "sys-new")
	if err != nil || got == nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.LastVisitedTick != 0 {
		t.Errorf("LastVisitedTick = %d, want 0 (map-only system)", got.LastVisitedTick)
	}
	if got.Visited() {
		t.Error("Visited() = true for map-only system")
	}
}

// TestSQLiteKB_UpsertSystemFromMap_PrunesStaleConnections pins the invariant
// that map imports are authoritative for a system's outgoing connections:
// any (from=data.ID, to=X) row where X is not in the new Connections list
// must be deleted. Regression for phantom shortcut edges left over from
// earlier galaxy topologies, which corrupt BFS hop counts.
func TestSQLiteKB_UpsertSystemFromMap_PrunesStaleConnections(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	// Seed a stale topology: A connects to B, C, D.
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "a", Name: "A", Connections: []string{"b", "c", "d"},
	}); err != nil {
		t.Fatalf("seed UpsertSystemFromMap: %v", err)
	}

	// New map says A only connects to B.
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "a", Name: "A", Connections: []string{"b"},
	}); err != nil {
		t.Fatalf("re-import UpsertSystemFromMap: %v", err)
	}

	got, err := kb.GetSystem(ctx, "a")
	if err != nil || got == nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if len(got.Connections) != 1 || got.Connections[0].SystemID != "b" {
		t.Errorf("connections = %+v, want exactly [b]", got.Connections)
	}
}

// TestSQLiteKB_UpsertSystemFromMap_EmptyConnectionsClearsAll verifies that
// a re-import with an empty connections list removes all outgoing edges for
// the system (a system can legitimately lose all its jump gates).
func TestSQLiteKB_UpsertSystemFromMap_EmptyConnectionsClearsAll(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "a", Name: "A", Connections: []string{"b", "c"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "a", Name: "A", Connections: nil,
	}); err != nil {
		t.Fatalf("re-import: %v", err)
	}

	got, err := kb.GetSystem(ctx, "a")
	if err != nil || got == nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if len(got.Connections) != 0 {
		t.Errorf("connections = %+v, want empty", got.Connections)
	}
}

// TestSQLiteKB_UpsertSystemFromMap_DoesNotTouchIncomingConnections verifies
// that pruning is scoped to outgoing edges only; (other -> data.ID) rows
// are owned by the other system's own import and must not be deleted.
func TestSQLiteKB_UpsertSystemFromMap_DoesNotTouchIncomingConnections(t *testing.T) {
	ctx := context.Background()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "a", Name: "A", Connections: []string{"b"},
	}); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "b", Name: "B", Connections: []string{"a"},
	}); err != nil {
		t.Fatalf("seed b: %v", err)
	}

	// Re-import A with only a connection to C. This must not delete (b -> a).
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "a", Name: "A", Connections: []string{"c"},
	}); err != nil {
		t.Fatalf("re-import a: %v", err)
	}

	gotB, err := kb.GetSystem(ctx, "b")
	if err != nil || gotB == nil {
		t.Fatalf("GetSystem(b): %v", err)
	}
	found := false
	for _, c := range gotB.Connections {
		if c.SystemID == "a" {
			found = true
		}
	}
	if !found {
		t.Errorf("b's outgoing connection to a was deleted; b.Connections = %+v", gotB.Connections)
	}
}
