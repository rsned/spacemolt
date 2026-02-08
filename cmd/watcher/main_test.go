package main

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestUpdateShipState_AllFields(t *testing.T) {
	state := &game.State{}
	ship := map[string]any{
		"fuel":           75.5,
		"max_fuel":       100.0,
		"hull":           50.0,
		"max_hull":       100.0,
		"cargo_capacity": float64(50),
		"cargo": []any{
			map[string]any{"item": "iron", "qty": 10},
			map[string]any{"item": "gold", "qty": 5},
		},
	}

	updateShipState(state, ship, "test")

	if state.Fuel != 75.5 {
		t.Errorf("Expected fuel 75.5, got %f", state.Fuel)
	}
	if state.MaxFuel != 100.0 {
		t.Errorf("Expected max_fuel 100.0, got %f", state.MaxFuel)
	}
	if state.Hull != 50.0 {
		t.Errorf("Expected hull 50.0, got %f", state.Hull)
	}
	if state.MaxHull != 100.0 {
		t.Errorf("Expected max_hull 100.0, got %f", state.MaxHull)
	}
	if state.MaxCargo != 50 {
		t.Errorf("Expected cargo_capacity 50, got %d", state.MaxCargo)
	}
	if len(state.Cargo) != 2 {
		t.Errorf("Expected 2 cargo items, got %d", len(state.Cargo))
	}
}

func TestUpdateShipState_PartialFields(t *testing.T) {
	state := &game.State{
		Fuel:    50.0,
		MaxFuel: 100.0,
	}
	ship := map[string]any{
		"fuel": 25.0,
		// max_fuel not provided, should remain unchanged
	}

	updateShipState(state, ship, "test")

	if state.Fuel != 25.0 {
		t.Errorf("Expected fuel 25.0, got %f", state.Fuel)
	}
	if state.MaxFuel != 100.0 {
		t.Errorf("Expected max_fuel unchanged at 100.0, got %f", state.MaxFuel)
	}
}

func TestUpdateShipState_CargoCapacityTypes(t *testing.T) {
	tests := []struct {
		name          string
		cargoCapacity any
		expectedMax   int
	}{
		{"float64", float64(50), 50},
		{"int64", int64(75), 75},
		{"int", int(100), 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &game.State{}
			ship := map[string]any{
				"cargo_capacity": tt.cargoCapacity,
			}

			updateShipState(state, ship, "test")

			if state.MaxCargo != tt.expectedMax {
				t.Errorf("Expected MaxCargo %d, got %d", tt.expectedMax, state.MaxCargo)
			}
		})
	}
}

func TestUpdateShipState_ReplacesCargo(t *testing.T) {
	state := &game.State{
		Cargo: []map[string]any{{"item": "old", "qty": 1}},
	}
	ship := map[string]any{
		"cargo": []any{
			map[string]any{"item": "new", "qty": 2},
		},
	}

	updateShipState(state, ship, "test")

	if len(state.Cargo) != 1 {
		t.Errorf("Expected 1 cargo item after replacement, got %d", len(state.Cargo))
	}
	if state.Cargo[0]["item"] != "new" {
		t.Errorf("Expected cargo item 'new', got %v", state.Cargo[0]["item"])
	}
}
