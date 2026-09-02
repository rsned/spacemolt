package knowledge

import (
	"context"
	"testing"
)

func descKB(t *testing.T) *SQLiteKB {
	t.Helper()
	kb, err := NewSQLiteKB(Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	return kb
}

// v0.576.0 gave the nine starless systems chart descriptions. A description is
// server lore that only get_system carries, and several capture paths build a
// System without it — so an empty incoming description must never erase a
// stored one, exactly as is_stronghold is sticky in the same upsert.
func TestRememberSystem_EmptyDescriptionDoesNotEraseStored(t *testing.T) {
	kb := descKB(t)
	ctx := context.Background()
	const lore = "Navigators keep this dark waypoint charted for its prospecting drift."

	if err := kb.RememberSystem(ctx, System{ID: "redmarsh", Name: "Redmarsh", Description: lore, LastUpdatedTick: 10}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A later visit through a path that does not populate Description.
	if err := kb.RememberSystem(ctx, System{ID: "redmarsh", Name: "Redmarsh", LastUpdatedTick: 20}); err != nil {
		t.Fatalf("revisit: %v", err)
	}

	got, err := kb.GetSystem(ctx, "redmarsh")
	if err != nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.Description != lore {
		t.Errorf("description = %q after a description-less revisit, want it preserved as %q", got.Description, lore)
	}
}

// A non-empty description still updates: the server may reword chart text.
func TestRememberSystem_NewDescriptionOverwrites(t *testing.T) {
	kb := descKB(t)
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, System{ID: "ancha", Name: "Ancha", Description: "old", LastUpdatedTick: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := kb.RememberSystem(ctx, System{ID: "ancha", Name: "Ancha", Description: "new", LastUpdatedTick: 2}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := kb.GetSystem(ctx, "ancha")
	if err != nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.Description != "new" {
		t.Errorf("description = %q, want %q", got.Description, "new")
	}
}
