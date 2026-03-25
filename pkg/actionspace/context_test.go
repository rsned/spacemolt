package actionspace

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestFromStateDocked(t *testing.T) {
	state := &game.State{
		Doc:     true,
		Credits: 5000,
		Player: game.Player{
			FactionID: "faction_1",
		},
		Ship: game.Ship{
			Hull: 80, MaxHull: 100,
			Fuel: 40, MaxFuel: 100,
			CargoUsed: 10, CargoCapacity: 50,
			Cargo:   []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}},
			Modules: []string{"mining_laser_mk1"},
		},
		System: game.SystemData{
			POIs: []game.POI{
				{ID: "station_1", Type: "station", Name: "Earth Station", HasBase: true},
				{ID: "belt_1", Type: "asteroid_belt", Name: "Main Belt"},
			},
			Connections: []game.ConnectionInfo{
				{SystemID: "alpha_centauri", Name: "Alpha Centauri", Distance: 5},
			},
		},
		Nearby: []game.NearbyPlayer{{PlayerID: "p1", Username: "Player1"}},
	}

	gc := FromState(state)

	if !gc.Docked {
		t.Error("expected Docked=true")
	}
	if gc.Credits != 5000 {
		t.Errorf("Credits: got %f, want 5000", gc.Credits)
	}
	if !gc.HasFaction {
		t.Error("expected HasFaction=true")
	}
	if gc.Hull != 80 || gc.MaxHull != 100 {
		t.Errorf("Hull: got %f/%f, want 80/100", gc.Hull, gc.MaxHull)
	}
	if len(gc.SystemPOIs) != 2 {
		t.Errorf("POIs: got %d, want 2", len(gc.SystemPOIs))
	}
	if len(gc.Connections) != 1 {
		t.Errorf("Connections: got %d, want 1", len(gc.Connections))
	}
	if gc.NearbyPlayerCount != 1 {
		t.Errorf("NearbyPlayerCount: got %d, want 1", gc.NearbyPlayerCount)
	}
	if gc.CargoItemCount != 1 {
		t.Errorf("CargoItemCount: got %d, want 1", gc.CargoItemCount)
	}
}

func TestFromStateUndocked(t *testing.T) {
	state := &game.State{
		Doc: false, InCombat: true,
		Ship: game.Ship{Hull: 100, MaxHull: 100, Fuel: 50, MaxFuel: 100},
	}
	gc := FromState(state)
	if gc.Docked {
		t.Error("expected Docked=false")
	}
	if !gc.InCombat {
		t.Error("expected InCombat=true")
	}
}

func TestFromStateCurrentPOIType(t *testing.T) {
	state := &game.State{
		CurrentPOI: "belt_1",
		Ship:       game.Ship{},
		System: game.SystemData{
			POIs: []game.POI{
				{ID: "station_1", Type: "station"},
				{ID: "belt_1", Type: "asteroid_belt"},
			},
		},
	}
	gc := FromState(state)
	if gc.CurrentPOIType != "asteroid_belt" {
		t.Errorf("CurrentPOIType: got %q, want asteroid_belt", gc.CurrentPOIType)
	}
}

func TestFromStateInTransit(t *testing.T) {
	state := &game.State{Traveling: true, Ship: game.Ship{}}
	gc := FromState(state)
	if !gc.InTransit {
		t.Error("expected InTransit=true")
	}
}

func TestValidateInvalidCombinations(t *testing.T) {
	tests := []struct {
		name string
		gc   GameContext
	}{
		{"docked and in transit", GameContext{Docked: true, InTransit: true}},
		{"docked and in combat", GameContext{Docked: true, InCombat: true}},
		{"docked and in battle", GameContext{Docked: true, InBattle: true}},
		{"in transit and in combat", GameContext{InTransit: true, InCombat: true}},
		{"in transit and in battle", GameContext{InTransit: true, InBattle: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.gc.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestValidateValidStates(t *testing.T) {
	tests := []struct {
		name string
		gc   GameContext
	}{
		{"docked", GameContext{Docked: true}},
		{"undocked", GameContext{}},
		{"in combat", GameContext{InCombat: true}},
		{"in transit", GameContext{InTransit: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.gc.Validate(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
