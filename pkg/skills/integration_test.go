package skills

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// TestRouteProgress_SaveAndLoadRoundTrip tests route persistence.
func TestRouteProgress_SaveAndLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	original := &RouteProgress{
		DestinationSystem: "haven",
		DestinationPOI:    "Haven Station",
		Route: []RouteStep{
			{SystemID: "sol", Name: "Sol", Jumps: 0},
			{SystemID: "alpha", Name: "Alpha Centauri", Jumps: 1},
			{SystemID: "haven", Name: "Haven", Jumps: 1},
		},
		CurrentStep: 1,
	}

	err := SaveRouteProgress(tmpDir, "test-agent", original)
	if err != nil {
		t.Fatalf("SaveRouteProgress failed: %v", err)
	}

	loaded, err := LoadRouteProgress(tmpDir, "test-agent")
	if err != nil {
		t.Fatalf("LoadRouteProgress failed: %v", err)
	}

	if loaded.DestinationSystem != original.DestinationSystem {
		t.Errorf("DestinationSystem = %q, want %q", loaded.DestinationSystem, original.DestinationSystem)
	}
	if loaded.DestinationPOI != original.DestinationPOI {
		t.Errorf("DestinationPOI = %q, want %q", loaded.DestinationPOI, original.DestinationPOI)
	}
	if loaded.CurrentStep != original.CurrentStep {
		t.Errorf("CurrentStep = %d, want %d", loaded.CurrentStep, original.CurrentStep)
	}
	if len(loaded.Route) != len(original.Route) {
		t.Fatalf("Route length = %d, want %d", len(loaded.Route), len(original.Route))
	}
	for i := range original.Route {
		if loaded.Route[i].SystemID != original.Route[i].SystemID {
			t.Errorf("Route[%d].SystemID = %q, want %q", i, loaded.Route[i].SystemID, original.Route[i].SystemID)
		}
		if loaded.Route[i].Name != original.Route[i].Name {
			t.Errorf("Route[%d].Name = %q, want %q", i, loaded.Route[i].Name, original.Route[i].Name)
		}
		if loaded.Route[i].Jumps != original.Route[i].Jumps {
			t.Errorf("Route[%d].Jumps = %d, want %d", i, loaded.Route[i].Jumps, original.Route[i].Jumps)
		}
	}
}

// TestRouteProgress_ClearRemovesFile tests that clearing route removes the file.
func TestRouteProgress_ClearRemovesFile(t *testing.T) {
	tmpDir := t.TempDir()

	route := &RouteProgress{
		DestinationSystem: "haven",
		Route: []RouteStep{
			{SystemID: "haven", Name: "Haven", Jumps: 1},
		},
		CurrentStep: 0,
	}

	err := SaveRouteProgress(tmpDir, "test-agent", route)
	if err != nil {
		t.Fatalf("SaveRouteProgress failed: %v", err)
	}

	// Verify file exists
	if !HasRouteProgress(tmpDir, "test-agent") {
		t.Error("HasRouteProgress returned false after save")
	}

	err = ClearRouteProgress(tmpDir, "test-agent")
	if err != nil {
		t.Fatalf("ClearRouteProgress failed: %v", err)
	}

	// Verify file is gone
	if HasRouteProgress(tmpDir, "test-agent") {
		t.Error("HasRouteProgress returned true after clear")
	}
}

// TestRouteExpressionVariables_InIntegration tests route-related expression variables.
func TestRouteExpressionVariables_InIntegration(t *testing.T) {
	route := &RouteProgress{
		DestinationSystem: "haven",
		DestinationPOI:    "Haven Station",
		Route: []RouteStep{
			{SystemID: "sol", Name: "Sol", Jumps: 0},
			{SystemID: "alpha", Name: "Alpha", Jumps: 1},
			{SystemID: "haven", Name: "Haven", Jumps: 1},
		},
		CurrentStep: 1,
	}

	tests := []struct {
		name     string
		expr     string
		expected bool
	}{
		{"route_destination_system", "route_destination_system == 'haven'", true},
		{"route_destination_system false", "route_destination_system == 'sol'", false},
		{"route_destination_poi", "route_destination_poi == 'Haven Station'", true},
		{"route_step_count", "route_step_count == 3", true},
		{"route_current_step", "route_current_step == 1", true},
		{"route_not_complete", "route_current_step < route_step_count", true},
		{"route_complete false", "route_current_step >= route_step_count", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EvalExprWithRoute(tt.expr, &game.State{}, route, nil)
			if err != nil {
				t.Fatalf("EvalExprWithRoute(%q) error: %v", tt.expr, err)
			}
			if result != tt.expected {
				t.Errorf("EvalExprWithRoute(%q) = %v, want %v", tt.expr, result, tt.expected)
			}
		})
	}
}
