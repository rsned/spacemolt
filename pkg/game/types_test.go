package game

import (
	"testing"
)

func TestStateClone_DeepCopiesCargo(t *testing.T) {
	original := &State{
		Cargo: []map[string]any{{"item": "ore", "qty": 10}},
	}
	clone := original.Clone()

	// Modify the clone's cargo
	if len(clone.Cargo) > 0 {
		clone.Cargo[0]["qty"] = 20
	}

	// Verify original is unchanged
	if len(original.Cargo) == 0 {
		t.Fatal("Original cargo is empty")
	}
	if original.Cargo[0]["qty"] != 10 {
		t.Errorf("Clone modified original cargo: got %v, want 10", original.Cargo[0]["qty"])
	}
}

func TestStateClone_CopiesAllFields(t *testing.T) {
	original := &State{
		Fuel:       75.5,
		MaxFuel:    100.0,
		Hull:       50.0,
		MaxHull:    100.0,
		MaxCargo:   25,
		Credits:    1000.0,
		Doc:        true,
		CurrentPOI: "station-1",
		Cargo: []map[string]any{
			{"item": "iron", "qty": 5},
			{"item": "gold", "qty": 2},
		},
	}

	clone := original.Clone()

	// Verify all fields match
	if clone.Fuel != original.Fuel {
		t.Errorf("Fuel not copied: got %f, want %f", clone.Fuel, original.Fuel)
	}
	if clone.MaxFuel != original.MaxFuel {
		t.Errorf("MaxFuel not copied: got %f, want %f", clone.MaxFuel, original.MaxFuel)
	}
	if clone.Hull != original.Hull {
		t.Errorf("Hull not copied: got %f, want %f", clone.Hull, original.Hull)
	}
	if clone.MaxHull != original.MaxHull {
		t.Errorf("MaxHull not copied: got %f, want %f", clone.MaxHull, original.MaxHull)
	}
	if clone.MaxCargo != original.MaxCargo {
		t.Errorf("MaxCargo not copied: got %d, want %d", clone.MaxCargo, original.MaxCargo)
	}
	if clone.Credits != original.Credits {
		t.Errorf("Credits not copied: got %f, want %f", clone.Credits, original.Credits)
	}
	if clone.Doc != original.Doc {
		t.Errorf("Doc not copied: got %v, want %v", clone.Doc, original.Doc)
	}
	if clone.CurrentPOI != original.CurrentPOI {
		t.Errorf("CurrentPOI not copied: got %s, want %s", clone.CurrentPOI, original.CurrentPOI)
	}
	if len(clone.Cargo) != len(original.Cargo) {
		t.Errorf("Cargo length not copied: got %d, want %d", len(clone.Cargo), len(original.Cargo))
	}
}
