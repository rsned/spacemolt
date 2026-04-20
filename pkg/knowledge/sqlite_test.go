package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	_ "modernc.org/sqlite"
)

// testDBPath is the path for the test database
const testDBPath = ":memory:"

func newTestSQLiteKB(t *testing.T) *SQLiteKB {
	kb, err := NewSQLiteKB(Config{
		DBPath:       testDBPath,
		WAL:          false, // Disable WAL for in-memory tests
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
		ID:           "SYS-001",
		Name:         "Test System",
		Position:     game.Position{X: 100.0, Y: 200.0, Z: 300.0},
		PoliceLevel:  3,
		Empire:       "test_empire",
		Connections:  []SystemConnection{{SystemID: "SYS-002"}, {SystemID: "SYS-003"}},
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
		ID:           "POI-001",
		SystemID:     "SYS-001",
		Name:         "Test Station",
		Type:         "station",
		Description:  "A test station",
		Position:     game.Position{X: 10.0, Y: 20.0},
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
		DBPath:       ":memory:",
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

func TestSQLiteKB_Migration3_LastVisitedTickBackfill(t *testing.T) {
	// Build a DB that simulates the pre-migration-3 state: systems,
	// pois, and poi_resources tables exist with the minimal columns
	// needed to exercise migration 3, but without last_visited_tick.
	// Migrations 1 and 2 are marked as already applied so runMigrations
	// only runs migration 3.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
		INSERT INTO schema_migrations (version, applied_at) VALUES
			(1, datetime('now')),
			(2, datetime('now'));

		-- Pre-migration-3 systems table: no last_visited_tick column.
		CREATE TABLE systems (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			position_x REAL NOT NULL DEFAULT 0,
			position_y REAL NOT NULL DEFAULT 0,
			police_level INTEGER NOT NULL DEFAULT 0,
			security_status TEXT DEFAULT '',
			empire TEXT NOT NULL DEFAULT '',
			is_stronghold BOOLEAN DEFAULT 0,
			last_updated_tick INTEGER DEFAULT 0
		);

		CREATE TABLE pois (
			id TEXT PRIMARY KEY,
			system_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			position_x REAL NOT NULL DEFAULT 0,
			position_y REAL NOT NULL DEFAULT 0,
			last_updated_tick INTEGER DEFAULT 0
		);

		CREATE TABLE poi_resources (
			poi_id TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			richness REAL NOT NULL,
			remaining REAL NOT NULL,
			last_updated_tick INTEGER DEFAULT 0,
			PRIMARY KEY (poi_id, resource_id)
		);

		-- Fixture data:
		--   sys-a: POI with a resource at tick 100 (visited, scannable)
		--   sys-b: POI with no resources (visited, but nothing to scan)
		--   sys-c: no POIs at all (never visited / map-only)
		INSERT INTO systems (id, name) VALUES
			('sys-a', 'Alpha'),
			('sys-b', 'Beta'),
			('sys-c', 'Gamma');
		INSERT INTO pois (id, system_id, name, type, last_updated_tick) VALUES
			('poi-a', 'sys-a', 'AsteroidA', 'asteroid', 100),
			('poi-b', 'sys-b', 'StationB',  'station',  100);
		INSERT INTO poi_resources (poi_id, resource_id, richness, remaining, last_updated_tick) VALUES
			('poi-a', 'iron_ore', 1, 1000, 100);
	`); err != nil {
		t.Fatalf("build pre-migration fixture: %v", err)
	}

	// Precondition: last_visited_tick column must not exist yet.
	var colCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('systems') WHERE name='last_visited_tick'`).Scan(&colCount); err != nil {
		t.Fatalf("pragma_table_info (pre): %v", err)
	}
	if colCount != 0 {
		t.Fatalf("last_visited_tick column unexpectedly present before migration 3")
	}

	// Run migration 3.
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	// Column now exists.
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('systems') WHERE name='last_visited_tick'`).Scan(&colCount); err != nil {
		t.Fatalf("pragma_table_info (post): %v", err)
	}
	if colCount != 1 {
		t.Fatalf("last_visited_tick column missing after migration 3")
	}

	// Backfill values.
	verify := func(id string, want int64) {
		t.Helper()
		var got int64
		if err := db.QueryRow(`SELECT last_visited_tick FROM systems WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if got != want {
			t.Errorf("%s last_visited_tick = %d, want %d", id, got, want)
		}
	}
	verify("sys-a", 100)
	verify("sys-b", 0)
	verify("sys-c", 0)

	// Migration 3 recorded.
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 31`).Scan(&rows); err != nil {
		t.Fatalf("schema_migrations count: %v", err)
	}
	if rows != 1 {
		t.Errorf("schema_migrations rows for version 31 = %d, want 1", rows)
	}

	// Idempotency: re-running migrations is a no-op. Exercises the
	// "column already exists" guard in runMigrations.
	if err := runMigrations(db); err != nil {
		t.Fatalf("second runMigrations: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 31`).Scan(&rows); err != nil {
		t.Fatalf("schema_migrations count after re-run: %v", err)
	}
	if rows != 1 {
		t.Errorf("schema_migrations rows for version 31 after re-run = %d, want 1 (idempotency broken)", rows)
	}
}

// TestSQLiteKB_Migration31_SelfHealsPreCollapseDBs simulates a DB that predates
// the 2026-04-15 migration collapse: schema_migrations claims versions up
// through 30 are applied but the `systems` table has no last_visited_tick
// column. The self-healing safeguard in runMigrations must add the column
// regardless of what schema_migrations says.
func TestSQLiteKB_Migration31_SelfHealsPreCollapseDBs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
		INSERT INTO schema_migrations (version, applied_at) VALUES
			(2, datetime('now')), (4, datetime('now')), (5, datetime('now')),
			(10, datetime('now')), (20, datetime('now')), (30, datetime('now'));

		CREATE TABLE systems (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			police_level INTEGER NOT NULL DEFAULT 0,
			empire TEXT NOT NULL DEFAULT '',
			last_updated_tick INTEGER DEFAULT 0
		);
		CREATE TABLE pois (
			id TEXT PRIMARY KEY,
			system_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			last_updated_tick INTEGER DEFAULT 0
		);
		CREATE TABLE poi_resources (
			poi_id TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			richness REAL NOT NULL,
			remaining REAL NOT NULL,
			last_updated_tick INTEGER DEFAULT 0,
			PRIMARY KEY (poi_id, resource_id)
		);

		INSERT INTO systems (id, name) VALUES ('sys-a', 'Alpha');
		INSERT INTO pois (id, system_id, name, type, last_updated_tick) VALUES ('poi-a', 'sys-a', 'AsteroidA', 'asteroid', 100);
		INSERT INTO poi_resources (poi_id, resource_id, richness, remaining, last_updated_tick) VALUES ('poi-a', 'iron_ore', 1, 1000, 100);
	`); err != nil {
		t.Fatalf("build pre-collapse fixture: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	var colCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('systems') WHERE name='last_visited_tick'`).Scan(&colCount); err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	if colCount != 1 {
		t.Fatalf("last_visited_tick column missing after safeguard ran (currentVersion=30, migration 31 would have been skipped without safeguard)")
	}

	var tick int64
	if err := db.QueryRow(`SELECT last_visited_tick FROM systems WHERE id = 'sys-a'`).Scan(&tick); err != nil {
		t.Fatalf("query last_visited_tick: %v", err)
	}
	if tick != 100 {
		t.Errorf("sys-a last_visited_tick = %d, want 100 (backfill should have run)", tick)
	}
}

// TestEnsureCollapseMissingTables_CreatesOnPreCollapseDB verifies that a DB
// which claims to have run migrations 1–30 but is missing the tables
// agent_ships, ships, ship_build_materials, and base_facilities gets them
// created by the safeguard pass.
func TestEnsureCollapseMissingTables_CreatesOnPreCollapseDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Minimal pre-collapse fixture: schema_migrations rows suggesting
	// migrations 1–30 applied, plus the systems table (so migration 31's
	// ALTER TABLE has something to target) and bases/players (referenced
	// by FKs in the missing tables). No agent_ships / ships /
	// ship_build_materials / base_facilities.
	if _, err := db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
		INSERT INTO schema_migrations (version, applied_at) VALUES
			(2, datetime('now')), (10, datetime('now')), (30, datetime('now'));

		CREATE TABLE systems (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			last_updated_tick INTEGER DEFAULT 0
		);
		CREATE TABLE pois (id TEXT PRIMARY KEY, system_id TEXT NOT NULL, name TEXT, type TEXT, last_updated_tick INTEGER DEFAULT 0);
		CREATE TABLE poi_resources (poi_id TEXT NOT NULL, resource_id TEXT NOT NULL, richness REAL, remaining REAL, last_updated_tick INTEGER DEFAULT 0, PRIMARY KEY (poi_id, resource_id));
		CREATE TABLE bases (id TEXT PRIMARY KEY, name TEXT);
		CREATE TABLE players (id TEXT PRIMARY KEY, username TEXT);
	`); err != nil {
		t.Fatalf("seed pre-collapse fixture: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	for _, table := range []string{"agent_ships", "ships", "ship_build_materials", "base_facilities"} {
		var count int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?`, table,
		).Scan(&count); err != nil {
			t.Fatalf("pragma %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %s not created by safeguard (count=%d)", table, count)
		}
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
