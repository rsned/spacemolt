package skills

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestEvalExpr(t *testing.T) {
	state := &game.State{
		Doc:        true,
		Credits:    1500.0,
		Fuel:       80.0,
		MaxFuel:    100.0,
		Hull:       90.0,
		MaxHull:    100.0,
		CurrentPOI: "poi-123",
		Ship: game.Ship{
			CargoUsed:     45.0,
			CargoCapacity: 50.0,
			Cargo:         []game.CargoItem{{ItemID: "iron_ore", Quantity: 45}},
			Modules:       []string{"mod-1"},
		},
		ModuleDefinitions: map[string]game.ModuleDefinition{
			"mod-1": {ID: "mod-1", Name: "Mining Laser", Type: "mining"},
		},
		System: game.SystemData{
			Name: "Alpha",
			POIs: []game.POI{
				{ID: "poi-123", Type: "asteroid_belt"},
				{ID: "poi-456", Type: "station"},
			},
		},
	}

	tests := []struct {
		expr string
		want bool
	}{
		// Bare booleans
		{"docked", true},
		{"has_cargo", true},
		{"cargo_full", false},
		{"fuel_low", false},

		// Negation
		{"not docked", false},
		{"not fuel_low", true},

		// Comparisons
		{"fuel_pct > 0.5", true},
		{"fuel_pct < 0.5", false},
		{"cargo_pct >= 0.9", true},
		{"cargo_pct < 0.97", true},
		{"hull_pct >= 0.9", true},
		{"credits >= 1000", true},
		{"credits < 1000", false},
		{"cargo_count > 0", true},
		{"cargo_count == 1", true},

		// String comparisons
		{"current_poi == poi-123", true},
		{"current_poi != poi-456", true},
		{"current_poi_type == asteroid_belt", true},
		{"system_name == Alpha", true},

		// Default (always true)
		{"default", true},

		// Function-style
		{"at_poi_type(asteroid_belt, asteroid_field)", true},
		{"at_poi_type(station)", false},
		{"has_module_type(mining)", true},
		{"has_module_type(weapon)", false},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := EvalExpr(tt.expr, state)
			if err != nil {
				t.Fatalf("EvalExpr(%q) error: %v", tt.expr, err)
			}
			if got != tt.want {
				t.Errorf("EvalExpr(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestEvalExpr_Undocked(t *testing.T) {
	state := &game.State{
		Doc:     false,
		Fuel:    5.0,
		MaxFuel: 100.0,
		Ship: game.Ship{
			CargoUsed:     0,
			CargoCapacity: 50.0,
		},
	}

	if got, _ := EvalExpr("docked", state); got {
		t.Error("docked should be false")
	}
	if got, _ := EvalExpr("not docked", state); !got {
		t.Error("not docked should be true")
	}
	if got, _ := EvalExpr("fuel_low", state); !got {
		t.Error("fuel_low should be true when fuel at 5%")
	}
}

func TestEvalExpr_ZeroDivision(t *testing.T) {
	state := &game.State{
		Fuel:    0,
		MaxFuel: 0,
		Hull:    0,
		MaxHull: 0,
		Ship: game.Ship{
			CargoUsed:     0,
			CargoCapacity: 0,
		},
	}

	// Should not panic on zero division
	got, err := EvalExpr("fuel_pct < 0.5", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("fuel_pct should be 0 (< 0.5)")
	}
}

func TestEvalExpr_InvalidExpr(t *testing.T) {
	state := &game.State{}
	_, err := EvalExpr("unknown_var > 5", state)
	if err == nil {
		t.Error("expected error for unknown variable")
	}
}

func TestEvalExpr_EmptyPOI(t *testing.T) {
	state := &game.State{
		CurrentPOI: "poi-999",
		System:     game.SystemData{},
	}

	got, err := EvalExpr("current_poi_type == station", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("should be false when POI not found in system")
	}
}

func TestEvalExpr_HasModuleType_NoDefinitions(t *testing.T) {
	state := &game.State{
		Ship: game.Ship{
			Modules: []string{"mod-1"},
		},
		// ModuleDefinitions is nil
	}

	got, err := EvalExpr("has_module_type(mining)", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("should be false when module definitions are nil")
	}
}

func TestExpressionVariables_CurrentSystem(t *testing.T) {
	state := &game.State{
		System: game.SystemData{ID: "test-system-1"},
	}
	result, err := EvalExpr("current_system == test-system-1", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if !result {
		t.Error("Expected current_system to match")
	}
}

func TestExpressionVariables_PlayerEmpire(t *testing.T) {
	state := &game.State{
		Player: game.Player{Empire: "Solarian"},
	}
	result, err := EvalExpr("player_empire == solarian", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if !result {
		t.Error("Expected player_empire to be lowercased")
	}
}

func TestExpressionVariables_FuelMaxJumps(t *testing.T) {
	state := &game.State{
		Fuel: 15.0,
	}
	result, err := EvalExpr("fuel_max_jumps == 5", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if !result {
		t.Error("Expected fuel_max_jumps to be 5 (15/3)")
	}
}

func TestExpressionVariables_CapitalSystemID(t *testing.T) {
	state := &game.State{
		Player: game.Player{Empire: "Solarian"},
	}
	result, err := EvalExpr("capital_system_id == sol", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if !result {
		t.Error("Expected capital_system_id to be 'sol' for Solarian")
	}
}

func TestFunction_FuelSufficientForJumps(t *testing.T) {
	state := &game.State{Fuel: 12.0}

	// Exactly enough
	result, err := EvalExpr("fuel_sufficient_for_jumps(4)", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if !result {
		t.Error("Expected 12 fuel to be sufficient for 4 jumps")
	}

	// Not enough
	result, err = EvalExpr("fuel_sufficient_for_jumps(5)", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if result {
		t.Error("Expected 12 fuel to be insufficient for 5 jumps")
	}
}

func TestFunction_AtSystem(t *testing.T) {
	state := &game.State{
		System: game.SystemData{ID: "sol"},
	}
	result, err := EvalExpr("at_system('sol')", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if !result {
		t.Error("Expected at_system to return true for matching system")
	}

	result, err = EvalExpr("at_system('haven')", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if result {
		t.Error("Expected at_system to return false for different system")
	}
}

func TestFunction_AtSystemWithVariable(t *testing.T) {
	state := &game.State{
		System: game.SystemData{ID: "nexus_prime"},
		Player: game.Player{Empire: "Voidborn"}, // Nexus Prime is Voidborn capital
	}
	result, err := EvalExpr("at_system(capital_system_id)", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if !result {
		t.Error("Expected at_system(capital_system_id) to return true when in capital system")
	}

	// Test with different system
	state.System.ID = "sol"
	result, err = EvalExpr("at_system(capital_system_id)", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if result {
		t.Error("Expected at_system(capital_system_id) to return false when not in capital system")
	}
}

func TestFunction_POIIsDockable(t *testing.T) {
	state := &game.State{
		Player: game.Player{DockedAtBase: "station-1"},
		System: game.SystemData{
			POIs: []game.POI{
				{ID: "station-1", Type: "station"},
			},
		},
	}

	result, err := EvalExpr("poi_is_dockable()", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if !result {
		t.Error("Expected poi_is_dockable to return true for station")
	}
}

func TestRouteExpressionVariables_NoRoute(t *testing.T) {
	state := &game.State{
		System: game.SystemData{ID: "sol"},
	}

	tests := []struct {
		expr string
	}{
		{"route_destination_system == sol"},
		{"route_destination_poi == station"},
		{"route_step_count > 0"},
		{"route_current_step == 0"},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			_, err := EvalExpr(tt.expr, state)
			if err == nil {
				t.Errorf("Expected error for %q with no route", tt.expr)
			}
		})
	}
}

func TestRouteExpressionVariables_WithRoute(t *testing.T) {
	state := &game.State{
		System: game.SystemData{ID: "sol"},
	}

	route := &RouteProgress{
		DestinationSystem: "haven",
		DestinationPOI:    "station-alpha",
		Route: []RouteStep{
			{SystemID: "sol", Name: "Sol", Jumps: 0},
			{SystemID: "system-2", Name: "System 2", Jumps: 1},
			{SystemID: "haven", Name: "Haven", Jumps: 1},
		},
		CurrentStep: 1,
	}

	tests := []struct {
		expr string
		want bool
	}{
		{"route_destination_system == haven", true},
		{"route_destination_system == sol", false},
		{"route_destination_poi == station-alpha", true},
		{"route_destination_poi == station-beta", false},
		{"route_step_count == 3", true},
		{"route_step_count > 2", true},
		{"route_step_count < 3", false},
		{"route_current_step == 1", true},
		{"route_current_step < route_step_count", true},
		{"route_current_step >= route_step_count", false},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := EvalExprWithRoute(tt.expr, state, route, nil)
			if err != nil {
				t.Fatalf("EvalExprWithRoute(%q) error: %v", tt.expr, err)
			}
			if got != tt.want {
				t.Errorf("EvalExprWithRoute(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestRouteFunction_HasRouteProgress(t *testing.T) {
	state := &game.State{
		System: game.SystemData{ID: "sol"},
	}

	// No route
	result, err := EvalExpr("has_route_progress()", state)
	if err != nil {
		t.Fatalf("EvalExpr failed: %v", err)
	}
	if result {
		t.Error("Expected has_route_progress to return false when no route")
	}

	// With route
	route := &RouteProgress{
		DestinationSystem: "haven",
		Route:             []RouteStep{{SystemID: "sol", Name: "Sol", Jumps: 0}},
		CurrentStep:       0,
	}

	result, err = EvalExprWithRoute("has_route_progress()", state, route, nil)
	if err != nil {
		t.Fatalf("EvalExprWithRoute failed: %v", err)
	}
	if !result {
		t.Error("Expected has_route_progress to return true with route")
	}
}

func TestRouteExpressionVariables_EmptyRoute(t *testing.T) {
	state := &game.State{
		System: game.SystemData{ID: "sol"},
	}

	route := &RouteProgress{
		DestinationSystem: "haven",
		Route:             []RouteStep{}, // Empty route
		CurrentStep:       0,
	}

	// Empty route should still allow destination access but step count is 0
	result, err := EvalExprWithRoute("route_destination_system == haven", state, route, nil)
	if err != nil {
		t.Fatalf("EvalExprWithRoute failed: %v", err)
	}
	if !result {
		t.Error("Expected route_destination_system to be accessible with empty route")
	}

	result, err = EvalExprWithRoute("route_step_count == 0", state, route, nil)
	if err != nil {
		t.Fatalf("EvalExprWithRoute failed: %v", err)
	}
	if !result {
		t.Error("Expected route_step_count to be 0 for empty route")
	}
}
