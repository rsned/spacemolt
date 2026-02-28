package skills

import (
	"testing"
	"time"
)

func TestRouteProgress_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	route := &RouteProgress{
		DestinationSystem: "haven",
		DestinationPOI:    "Haven Station",
		Route: []RouteStep{
			{SystemID: "sol", Name: "Sol", Jumps: 1},
			{SystemID: "haven", Name: "Haven", Jumps: 0},
		},
		CurrentStep: 0,
		Timestamp:   time.Now(),
	}

	// Save
	err := SaveRouteProgress(tmpDir, agentID, route)
	if err != nil {
		t.Fatalf("SaveRouteProgress failed: %v", err)
	}

	// Load
	loaded, err := LoadRouteProgress(tmpDir, agentID)
	if err != nil {
		t.Fatalf("LoadRouteProgress failed: %v", err)
	}

	if loaded.DestinationSystem != route.DestinationSystem {
		t.Errorf("DestinationSystem = %s, want %s", loaded.DestinationSystem, route.DestinationSystem)
	}
	if loaded.DestinationPOI != route.DestinationPOI {
		t.Errorf("DestinationPOI = %s, want %s", loaded.DestinationPOI, route.DestinationPOI)
	}
	if len(loaded.Route) != len(route.Route) {
		t.Errorf("Route length = %d, want %d", len(loaded.Route), len(route.Route))
	}
	if loaded.CurrentStep != route.CurrentStep {
		t.Errorf("CurrentStep = %d, want %d", loaded.CurrentStep, route.CurrentStep)
	}
}

func TestRouteProgress_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	route := &RouteProgress{
		DestinationSystem: "haven",
		Route:             []RouteStep{{SystemID: "sol", Name: "Sol", Jumps: 1}},
		CurrentStep:       0,
		Timestamp:         time.Now(),
	}

	if err := SaveRouteProgress(tmpDir, agentID, route); err != nil {
		t.Fatalf("SaveRouteProgress failed: %v", err)
	}
	if err := ClearRouteProgress(tmpDir, agentID); err != nil {
		t.Fatalf("ClearRouteProgress failed: %v", err)
	}

	// Should return error
	_, err := LoadRouteProgress(tmpDir, agentID)
	if err == nil {
		t.Error("Expected error after clearing route")
	}
}

func TestRouteProgress_HasRouteProgress(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	// Initially no route
	if HasRouteProgress(tmpDir, agentID) {
		t.Error("Expected false for non-existent route")
	}

	// Save a route
	route := &RouteProgress{
		DestinationSystem: "haven",
		Route:             []RouteStep{{SystemID: "sol", Name: "Sol", Jumps: 1}},
		CurrentStep:       0,
		Timestamp:         time.Now(),
	}
	if err := SaveRouteProgress(tmpDir, agentID, route); err != nil {
		t.Fatalf("SaveRouteProgress failed: %v", err)
	}

	// Now should have route
	if !HasRouteProgress(tmpDir, agentID) {
		t.Error("Expected true after saving route")
	}

	// Clear it
	if err := ClearRouteProgress(tmpDir, agentID); err != nil {
		t.Fatalf("ClearRouteProgress failed: %v", err)
	}

	// Should be gone
	if HasRouteProgress(tmpDir, agentID) {
		t.Error("Expected false after clearing route")
	}
}

func TestRouteProgress_SaveNilRoute(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	err := SaveRouteProgress(tmpDir, agentID, nil)
	if err == nil {
		t.Error("Expected error when saving nil route")
	}
}

func TestRouteProgress_LoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "non-existent-agent"

	_, err := LoadRouteProgress(tmpDir, agentID)
	if err == nil {
		t.Error("Expected error when loading non-existent route")
	}
}
