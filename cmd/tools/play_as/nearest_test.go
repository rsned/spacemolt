package main

import (
	"context"
	"os"
	"testing"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// TestNearestCommand_WithRealData is an integration test that verifies
// the nearest command works with a real knowledge base.
// Run with: TEST_KB_PATH=/path/to/kb.db go test ./cmd/tools/play_as/...
func TestNearestCommand_WithRealData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbPath := os.Getenv("TEST_KB_PATH")
	if dbPath == "" {
		t.Skip("TEST_KB_PATH not set - skipping integration test")
	}

	// Open knowledge base
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("Failed to open KB: %v", err)
	}
	defer func() { _ = kb.Close() }()

	// Build galaxy graph
	ctx := context.Background()
	g := &galaxy.GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB failed: %v", err)
	}

	// Verify graph was built
	stats := g.Stats()
	t.Logf("Graph: %d systems, %d edges, built in %v",
		stats.NodeCount, stats.EdgeCount, stats.BuildTime)

	if stats.NodeCount == 0 {
		t.Error("Expected non-zero node count")
	}

	if stats.EdgeCount == 0 {
		t.Error("Expected non-zero edge count")
	}

	// Test FindNearest with a sample system
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		t.Fatalf("Failed to get systems: %v", err)
	}

	if len(systems) == 0 {
		t.Fatal("No systems in knowledge base")
	}

	fromSystem := systems[0].ID

	// Get all system IDs as potential targets
	var allTargets []string
	for _, sys := range systems {
		allTargets = append(allTargets, sys.ID)
	}

	// Test with limit of 3
	results, err := g.FindNearest(fromSystem, allTargets, 3)
	if err != nil {
		t.Fatalf("FindNearest failed: %v", err)
	}

	t.Logf("Found %d nearest systems from %s", len(results), fromSystem)

	// Verify results are sorted by hops
	for i := 1; i < len(results); i++ {
		if results[i].Hops < results[i-1].Hops {
			t.Errorf("Results not sorted by hops: results[%d].Hops=%d < results[%d].Hops=%d",
				i, results[i].Hops, i-1, results[i-1].Hops)
		}
	}

	// Verify limit is respected
	if len(results) > 3 {
		t.Errorf("Expected at most 3 results, got %d", len(results))
	}
}
