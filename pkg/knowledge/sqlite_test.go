package knowledge

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
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
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	sys := System{
		ID:           "SYS-001",
		Name:         "Test System",
		Position:     game.Position{X: 100.0, Y: 200.0, Z: 300.0},
		PoliceLevel:  3,
		Empire:       "test_empire",
		Connections:  []string{"SYS-002", "SYS-003"},
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
		Connections: []string{"B"},
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

	if len(retrieved.Connections) > 0 && retrieved.Connections[0] != "B" {
		t.Errorf("Expected connection to B, got %s", retrieved.Connections[0])
	}
}

func TestSQLiteKB_GetUnknownConnections(t *testing.T) {
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
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Add some systems
	systems := []System{
		{ID: "A", Name: "System A", Position: game.Position{X: 0, Y: 0, Z: 0}, Connections: []string{"B"}},
		{ID: "B", Name: "System B", Position: game.Position{X: 100, Y: 0, Z: 0}, Connections: []string{"A", "C"}},
		{ID: "C", Name: "System C", Position: game.Position{X: 200, Y: 0, Z: 0}, Connections: []string{"B"}},
	}

	for _, sys := range systems {
		if err := kb.RememberSystem(ctx, sys); err != nil {
			t.Fatalf("RememberSystem %s failed: %v", sys.ID, err)
		}
	}

	// Get all systems
	retrieved := kb.GetSystems()
	if len(retrieved) != 3 {
		t.Errorf("Expected 3 systems, got %d", len(retrieved))
	}

	// Create a map for easy lookup
	sysMap := make(map[string]System)
	for _, sys := range retrieved {
		sysMap[sys.ID] = sys
	}

	// Verify each system has correct connections
	if len(sysMap["A"].Connections) != 1 || sysMap["A"].Connections[0] != "B" {
		t.Error("System A has incorrect connections")
	}

	if len(sysMap["B"].Connections) != 2 {
		t.Error("System B should have 2 connections")
	}

	if len(sysMap["C"].Connections) != 1 || sysMap["C"].Connections[0] != "B" {
		t.Error("System C has incorrect connections")
	}
}

func TestSQLiteKB_ConcurrentAccess(t *testing.T) {
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
	systems := kb.GetSystems()
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
