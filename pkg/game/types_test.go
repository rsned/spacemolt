package game

import (
	"fmt"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"reflect"
	"testing"
	"time"
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
		Traveling:      false,
		TravelProgress: nil,
	}

	clone := original.Clone()

	if clone.TravelProgress != nil {
		t.Errorf("TravelProgress should be nil, got %+v", clone.TravelProgress)
	}
}

// TestStateClone_AllFieldsCopied uses reflection to verify that Clone() copies
// every field in State, SystemData, and TravelProgress. If a new field is added
// to any of these structs but not included in Clone(), this test will fail.
func TestStateClone_AllFieldsCopied(t *testing.T) {
	// Build a State with every field set to a non-zero value.
	// This lets reflection detect any field that Clone() fails to copy
	// (it would remain at its zero value in the clone).
	original := &State{
		Username:      "TestPlayer",
		Password:      "secret-password-hash",
		Doc:           true,
		CurrentSystem: "sol",
		CurrentPOI:    "sol_station",
		Traveling:     true,
		TravelProgress: &TravelProgress{
			Progress:    0.75,
			Destination: "Alpha Centauri",
			Type:        "jump",
			ArrivalTick: 99999,
		},
		ServerVersion: "0.100.3",
		Player: Player{
			ID:            "player-abc",
			Username:      "TestPlayer",
			Empire:        "solarian",
			Credits:       5000,
			CurrentSystem: "sol",
			CurrentPOI:    "sol_station",
		},
		Credits: 5000,
		SkillXP: map[string]float64{
			"mining": 450,
		},
		SkillNextLevelXP: map[string]float64{
			"mining": 1000,
		},
		SkillDefinitions: map[string]SkillDefinition{
			"mining": {ID: "mining", Name: "Mining", MaxLevel: 10},
		},
		Ship: Ship{
			ID:      "ship-xyz",
			ClassID: "explorer",
			Name:    "Test Ship",
			Hull:    200,
			MaxHull: 350,
			Fuel:    150,
			MaxFuel: 200,
		},
		Fuel:     150,
		MaxFuel:  200,
		Hull:     200,
		MaxHull:  350,
		Cargo:    []map[string]any{{"item_id": "iron_ore", "quantity": float64(50)}},
		MaxCargo: 400,
		ModuleDefinitions: map[string]ModuleDefinition{
			"mod1": {ID: "mod1", Name: "Mining Laser", Type: "mining"},
		},
		System: SystemData{
			ID:             "sol",
			Name:           "Sol",
			Description:    "The birthplace of humanity",
			Empire:         "solarian",
			PoliceLevel:    100,
			SecurityStatus: "Maximum Security",
			IsStronghold:   true,
			Online:         42,
			POIs: []POI{
				{ID: "sol_sun", Name: "Sol", Type: "sun"},
				{ID: "sol_earth", Name: "Earth", Type: "planet"},
			},
			Connections: []ConnectionInfo{
				{SystemID: "alpha_centauri", Name: "Alpha Centauri", Distance: 4},
			},
			Discovered:      true,
			Position:        Position{X: 10.5, Y: 20.3, Z: 1.0},
			DiscoveredBy:    "TestPlayer",
			ShipPOI:         "sol_station",
			LastVisitedTick: 106000,
		},
		CurrentTick:     106000,
		LastKill:        serverapi.PlayerKill{Victim: "MoltenOne", WreckID: "wreck-1", WreckHasCargo: true, WreckHasModules: true},
		LastCapture:     serverapi.ShipCaptured{BattleID: "battle-123", BoardingOperationID: "op-1", CaptorID: "MoltenOne", ShipID: "ship-7"},
		LastPrizeUpdate: serverapi.PrizeUpdate{PrizeID: "prize-1", Status: "delivered"},
		ServerTimestamp: 1773883811,
		LastMapUpdate:   time.Date(2026, 2, 18, 9, 0, 0, 0, time.UTC),
		Nearby: []NearbyPlayer{
			{PlayerID: "other-1", Username: "Rival", ShipClass: "fighter"},
		},
		InCombat: true,
		InBattle: true,
		BattleState: &BattleState{
			BattleID: "battle-123",
			SystemID: "sol",
		},
		LastChatHistory: []ChatMessage{
			{ID: "msg-1", Channel: "system", SenderID: "other-1", Sender: "Rival", Content: "help", Timestamp: "2026-02-28T10:00:00Z"},
		},
		LastDockStory: "A bustling trade hub at the heart of Sol system.",
		PendingTrades: []map[string]any{
			{"trade_id": "trade-1", "partner": "Rival"},
		},
		PirateName:   "Blackbeard",
		PirateTier:   "elite",
		PirateID:     "pirate-001",
		LastDamage:   25.5,
		LastBattleID: "13ec2c95ba853a657b00385cf2683d49",
	}

	clone := original.Clone()

	// Use reflection to compare every field except Mu (sync.Mutex).
	t.Run("State", func(t *testing.T) {
		checkAllFields(t, "State", reflect.ValueOf(original).Elem(), reflect.ValueOf(clone).Elem(), map[string]bool{
			"Mu": true, // sync.Mutex is intentionally not cloned
		})
	})

	t.Run("SystemData", func(t *testing.T) {
		checkAllFields(t, "System", reflect.ValueOf(original.System), reflect.ValueOf(clone.System), nil)
	})

	t.Run("TravelProgress", func(t *testing.T) {
		if clone.TravelProgress == nil {
			t.Fatal("TravelProgress is nil in clone")
		}
		checkAllFields(t, "TravelProgress",
			reflect.ValueOf(original.TravelProgress).Elem(),
			reflect.ValueOf(clone.TravelProgress).Elem(), nil)
	})
}

// checkAllFields compares every exported field of two struct values using
// reflection. For each field, it verifies:
//  1. The field is set to a non-zero value in the original (test setup coverage).
//  2. The cloned value matches the original via reflect.DeepEqual.
//
// Fields listed in skip are excluded from comparison (e.g., sync.Mutex).
func checkAllFields(t *testing.T, prefix string, orig, clone reflect.Value, skip map[string]bool) {
	t.Helper()
	typ := orig.Type()

	for i := range typ.NumField() {
		field := typ.Field(i)
		if skip[field.Name] {
			continue
		}
		if !field.IsExported() {
			continue
		}

		origVal := orig.Field(i)
		cloneVal := clone.Field(i)
		fieldPath := fmt.Sprintf("%s.%s", prefix, field.Name)

		// Verify the test itself set this field to a non-zero value.
		// If it's zero, the test data is incomplete and won't catch a missing copy.
		if origVal.IsZero() {
			t.Errorf("%s has zero value in test data - add a non-zero value to the test original so Clone coverage is meaningful", fieldPath)
			continue
		}

		if !reflect.DeepEqual(origVal.Interface(), cloneVal.Interface()) {
			t.Errorf("%s not copied by Clone(): original=%v, clone=%v", fieldPath, origVal.Interface(), cloneVal.Interface())
		}
	}
}

// TestStateClone_Independence verifies that modifying the clone does not affect
// the original for reference types (slices, maps, pointers).
func TestStateClone_Independence(t *testing.T) {
	original := &State{
		Cargo: []map[string]any{
			{"item_id": "iron_ore", "quantity": float64(10)},
		},
		System: SystemData{
			POIs:        []POI{{ID: "poi1", Name: "Station"}},
			Connections: []ConnectionInfo{{SystemID: "sys1", Name: "Neighbor"}},
		},
		Nearby: []NearbyPlayer{{PlayerID: "p1", Username: "Other"}},
		SkillXP: map[string]float64{
			"mining": 100,
		},
		SkillNextLevelXP: map[string]float64{
			"mining": 500,
		},
		SkillDefinitions: map[string]SkillDefinition{
			"mining": {ID: "mining", Name: "Mining"},
		},
		TravelProgress: &TravelProgress{
			Progress:    0.5,
			Destination: "Mars",
			Type:        "travel",
			ArrivalTick: 100,
		},
	}

	clone := original.Clone()

	// Mutate every reference-type field in the clone
	clone.Cargo[0]["quantity"] = float64(999)
	clone.System.POIs[0].Name = "MUTATED"
	clone.System.Connections[0].SystemID = "MUTATED"
	clone.Nearby[0].Username = "MUTATED"
	clone.SkillXP["mining"] = 999
	clone.SkillNextLevelXP["mining"] = 999
	clone.SkillDefinitions["mining"] = SkillDefinition{ID: "MUTATED"}
	clone.TravelProgress.Progress = 999

	// Verify original is untouched
	if original.Cargo[0]["quantity"] != float64(10) {
		t.Error("Clone mutation leaked to original Cargo")
	}
	if original.System.POIs[0].Name != "Station" {
		t.Error("Clone mutation leaked to original System.POIs")
	}
	if original.System.Connections[0].SystemID != "sys1" {
		t.Error("Clone mutation leaked to original System.Connections")
	}
	if original.Nearby[0].Username != "Other" {
		t.Error("Clone mutation leaked to original Nearby")
	}
	if original.SkillXP["mining"] != 100 {
		t.Error("Clone mutation leaked to original SkillXP")
	}
	if original.SkillNextLevelXP["mining"] != 500 {
		t.Error("Clone mutation leaked to original SkillNextLevelXP")
	}
	if original.SkillDefinitions["mining"].ID != "mining" {
		t.Error("Clone mutation leaked to original SkillDefinitions")
	}
	if original.TravelProgress.Progress != 0.5 {
		t.Error("Clone mutation leaked to original TravelProgress")
	}
}

// TestStateClone_DeepCopiesShipCargo pins the aliasing bug that made deposit_all
// skip items. Clone() copies Ship by value, so before this fix Ship.Cargo — a
// slice — shared its backing array with live state. removeShipCargo() compacts
// that array IN PLACE (`kept := c.state.Ship.Cargo[:0]`), so every deposit
// shifted the caller's snapshot left underneath it: DepositAllItems ranged over
// a 5-item header while the contents slid, visiting items 0, 2, 4 and then
// reading two stale tail entries. Live 2026-09-02 on battle_bot1, which left
// missile_launcher_i and standard_rounds_box in the hold while reporting
// "3 successful, 0 failed".
func TestStateClone_DeepCopiesShipCargo(t *testing.T) {
	original := &State{
		Ship: Ship{
			Cargo: []CargoItem{
				{ItemID: "pulse_laser_i", Quantity: 3},
				{ItemID: "missile_launcher_i", Quantity: 2},
				{ItemID: "autocannon_i", Quantity: 2},
				{ItemID: "standard_rounds_box", Quantity: 19},
				{ItemID: "afterburner_ii", Quantity: 1},
			},
			Modules: []string{"shield_i", "cargo_expander_i"},
		},
	}
	snapshot := original.Clone()

	// Simulate removeShipCargo's in-place compaction of the LIVE hold: drop the
	// head entry by rewriting the same backing array.
	kept := original.Ship.Cargo[:0]
	for _, item := range original.Ship.Cargo {
		if item.ItemID == "pulse_laser_i" {
			continue
		}
		kept = append(kept, item)
	}
	original.Ship.Cargo = kept

	want := []string{
		"pulse_laser_i", "missile_launcher_i", "autocannon_i",
		"standard_rounds_box", "afterburner_ii",
	}
	if len(snapshot.Ship.Cargo) != len(want) {
		t.Fatalf("snapshot cargo length = %d, want %d", len(snapshot.Ship.Cargo), len(want))
	}
	for i, id := range want {
		if got := snapshot.Ship.Cargo[i].ItemID; got != id {
			t.Errorf("snapshot.Ship.Cargo[%d].ItemID = %q, want %q (live compaction shifted the clone)", i, got, id)
		}
	}

	// Modules is a slice on the same value-copied struct and aliases the same way.
	original.Ship.Modules[0] = "MUTATED"
	if snapshot.Ship.Modules[0] != "shield_i" {
		t.Errorf("snapshot.Ship.Modules[0] = %q, want %q", snapshot.Ship.Modules[0], "shield_i")
	}
}
