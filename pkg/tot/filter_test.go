package tot

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// actionSet converts a slice of ActionOption to a set for easy membership testing.
func actionSet(actions []ActionOption) map[string]bool {
	set := make(map[string]bool, len(actions))
	for _, a := range actions {
		set[a.Action] = true
	}
	return set
}

func TestValidActions_Docked(t *testing.T) {
	state := &game.State{
		Doc:     true,
		Credits: 1000,
		Ship: game.Ship{
			Hull: 100, MaxHull: 100,
			Fuel: 50, MaxFuel: 50,
			CargoUsed: 10, CargoCapacity: 100,
			Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}},
		},
	}

	actions := ValidActions(state)
	set := actionSet(actions)

	dockedExpected := []string{"undock", "buy", "sell", "withdraw_items", "deposit_items", "craft"}
	for _, a := range dockedExpected {
		if !set[a] {
			t.Errorf("expected docked action %q to be present", a)
		}
	}

	spaceOnly := []string{"travel", "jump", "mine", "dock"}
	for _, a := range spaceOnly {
		if set[a] {
			t.Errorf("space action %q should not be present when docked", a)
		}
	}

	if set["repair"] {
		t.Error("repair should not be present when hull is full")
	}
	if set["refuel"] {
		t.Error("refuel should not be present when fuel is full")
	}
}

func TestValidActions_Docked_NeedsRepairAndRefuel(t *testing.T) {
	state := &game.State{
		Doc:  true,
		Ship: game.Ship{Hull: 75, MaxHull: 100, Fuel: 20, MaxFuel: 50},
	}

	actions := ValidActions(state)
	set := actionSet(actions)

	if !set["repair"] {
		t.Error("repair should be present when hull < max hull")
	}
	if !set["refuel"] {
		t.Error("refuel should be present when fuel < max fuel")
	}
}

func TestValidActions_InSpace(t *testing.T) {
	state := &game.State{
		Doc:        false,
		CurrentPOI: "poi-asteroid-1",
		Ship: game.Ship{
			Fuel: 50, MaxFuel: 100,
			CargoUsed: 0, CargoCapacity: 100,
		},
		Nearby: []game.NearbyPlayer{{PlayerID: "p1"}},
		System: game.SystemData{
			POIs: []game.POI{
				{ID: "poi-station-1", Name: "Alpha Station", Type: "station", HasBase: true},
				{ID: "poi-asteroid-1", Name: "Ring Belt", Type: "asteroid_belt"},
				{ID: "poi-planet-1", Name: "Kepler IV", Type: "planet"},
			},
			Connections: []game.ConnectionInfo{
				{SystemID: "sys-beta", Name: "Beta System", Distance: 5},
				{SystemID: "sys-gamma", Name: "Gamma System", Distance: 8},
			},
		},
	}

	actions := ValidActions(state)
	set := actionSet(actions)

	if !set["scan"] {
		t.Error("scan should be present with nearby players")
	}
	if !set["travel"] {
		t.Error("travel should be present when POIs exist and has fuel")
	}
	if !set["dock"] {
		t.Error("dock should be present when a station with base exists")
	}
	if !set["jump"] {
		t.Error("jump should be present when connections exist and has fuel")
	}
	if !set["mine"] {
		t.Error("mine should be present at asteroid_belt with cargo space")
	}

	// Travel targets exclude current POI.
	for _, a := range actions {
		if a.Action == "travel" {
			if len(a.Targets) != 2 {
				t.Errorf("expected 2 travel targets, got %d", len(a.Targets))
			}
			break
		}
	}

	for _, a := range actions {
		if a.Action == "jump" {
			if len(a.Targets) != 2 {
				t.Errorf("expected 2 jump targets, got %d", len(a.Targets))
			}
			break
		}
	}

	if set["undock"] {
		t.Error("undock should not be present in space")
	}
}

func TestValidActions_InSpace_NoPOIs(t *testing.T) {
	state := &game.State{
		Doc: false,
		System: game.SystemData{
			POIs:        []game.POI{},
			Connections: []game.ConnectionInfo{},
		},
	}

	actions := ValidActions(state)
	set := actionSet(actions)

	for _, a := range []string{"travel", "dock", "jump", "mine"} {
		if set[a] {
			t.Errorf("%s should not be present without POIs/connections", a)
		}
	}
}

func TestValidActions_InCombat(t *testing.T) {
	state := &game.State{
		Doc: false, InCombat: true,
		System: game.SystemData{},
	}

	actions := ValidActions(state)
	set := actionSet(actions)

	for _, a := range []string{"battle_advance", "battle_retreat"} {
		if !set[a] {
			t.Errorf("expected %q in combat", a)
		}
	}
}

func TestValidActions_NotInCombat_NoBattleActions(t *testing.T) {
	state := &game.State{Doc: false}
	set := actionSet(ValidActions(state))

	for _, a := range []string{"battle_advance", "battle_retreat"} {
		if set[a] {
			t.Errorf("%q should not be present when not in combat", a)
		}
	}
}

func TestValidActions_AlwaysIncludesQueries(t *testing.T) {
	queries := []string{"get_status", "get_system", "get_cargo", "get_skills", "get_nearby"}

	for _, s := range []*game.State{
		{Doc: true},
		{Doc: false},
		{Doc: false, InCombat: true},
	} {
		set := actionSet(ValidActions(s))
		for _, q := range queries {
			if !set[q] {
				t.Errorf("query %q should always be present", q)
			}
		}
	}
}
