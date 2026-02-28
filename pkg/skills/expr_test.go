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
