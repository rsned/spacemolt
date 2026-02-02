package game

import (
	"testing"
)

func TestStateClone_DeepCopiesCargo(t *testing.T) {
	original := &State{
		Cargo: []map[string]any{{"item_id": "iron_ore", "quantity": 10}},
	}
	clone := original.Clone()

	// Modify the clone's cargo
	if len(clone.Cargo) > 0 {
		clone.Cargo[0]["quantity"] = 20
	}

	// Verify original is unchanged
	if len(original.Cargo) == 0 {
		t.Fatal("Original cargo is empty")
	}
	if original.Cargo[0]["quantity"] != 10 {
		t.Errorf("Clone modified original cargo: got %v, want 10", original.Cargo[0]["quantity"])
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
			{"item_id": "iron", "quantity": 5},
			{"item_id": "gold", "quantity": 2},
		},
		Player: Player{
			ID:       "player-123",
			Username: "TestPlayer",
			Empire:   "solarian",
		},
		Ship: Ship{
			ID:      "ship-456",
			ClassID: "starter_ship",
			Name:    "Test Ship",
		},
		Nearby: []NearbyPlayer{
			{PlayerID: "player-789", Username: "NearbyPlayer"},
		},
		InCombat: false,
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
	if clone.Player.ID != original.Player.ID {
		t.Errorf("Player.ID not copied: got %s, want %s", clone.Player.ID, original.Player.ID)
	}
	if clone.Ship.ID != original.Ship.ID {
		t.Errorf("Ship.ID not copied: got %s, want %s", clone.Ship.ID, original.Ship.ID)
	}
	if len(clone.Nearby) != len(original.Nearby) {
		t.Errorf("Nearby length not copied: got %d, want %d", len(clone.Nearby), len(original.Nearby))
	}
	if clone.InCombat != original.InCombat {
		t.Errorf("InCombat not copied: got %v, want %v", clone.InCombat, original.InCombat)
	}
}

func TestStateClone_TravelProgress(t *testing.T) {
	original := &State{
		Traveling: true,
		TravelProgress: &TravelProgress{
			Progress:    0.6,
			Destination: "Asteroid Belt Alpha",
			Type:        "travel",
			ArrivalTick: 15238,
		},
	}

	clone := original.Clone()

	if clone.TravelProgress == nil {
		t.Fatal("TravelProgress was not copied")
	}
	if clone.TravelProgress.Progress != 0.6 {
		t.Errorf("TravelProgress.Progress not copied: got %f, want 0.6", clone.TravelProgress.Progress)
	}
	if clone.TravelProgress.Destination != "Asteroid Belt Alpha" {
		t.Errorf("TravelProgress.Destination not copied: got %s, want Asteroid Belt Alpha", clone.TravelProgress.Destination)
	}
	if clone.TravelProgress.Type != "travel" {
		t.Errorf("TravelProgress.Type not copied: got %s, want travel", clone.TravelProgress.Type)
	}
	if clone.TravelProgress.ArrivalTick != 15238 {
		t.Errorf("TravelProgress.ArrivalTick not copied: got %d, want 15238", clone.TravelProgress.ArrivalTick)
	}

	// Modify clone and verify original is unchanged
	clone.TravelProgress.Progress = 0.8
	if original.TravelProgress.Progress != 0.6 {
		t.Errorf("Clone modified original TravelProgress: got %f, want 0.6", original.TravelProgress.Progress)
	}
}

func TestStateClone_NilTravelProgress(t *testing.T) {
	original := &State{
		Traveling:     false,
		TravelProgress: nil,
	}

	clone := original.Clone()

	if clone.TravelProgress != nil {
		t.Errorf("TravelProgress should be nil, got %+v", clone.TravelProgress)
	}
}
