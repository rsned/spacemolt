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
		Doc: true,
		Ship: game.Ship{
			Hull:          100,
			MaxHull:       100,
			Fuel:          50,
			MaxFuel:       50,
			CargoCapacity: 100,
		},
	}

	actions := ValidActions(state)
	set := actionSet(actions)

	// Docked actions should be present.
	dockedExpected := []string{
		"undock", "view_market", "buy", "sell", "sell_all",
		"view_storage", "deposit_credits", "withdraw_items",
		"list_ships", "switch_ship", "get_recipes", "craft",
		"get_missions", "complete_mission",
	}
	for _, a := range dockedExpected {
		if !set[a] {
			t.Errorf("expected docked action %q to be present", a)
		}
	}

	// Space-only actions should not be present.
	spaceOnly := []string{"travel", "jump", "mine", "scan", "dock"}
	for _, a := range spaceOnly {
		if set[a] {
			t.Errorf("space action %q should not be present when docked", a)
		}
	}

	// repair and refuel should not appear when hull and fuel are full.
	if set["repair"] {
		t.Error("repair should not be present when hull is full")
	}
	if set["refuel"] {
		t.Error("refuel should not be present when fuel is full")
	}
}

func TestValidActions_Docked_NeedsRepairAndRefuel(t *testing.T) {
	state := &game.State{
		Doc: true,
		Ship: game.Ship{
			Hull:    75,
			MaxHull: 100,
			Fuel:    20,
			MaxFuel: 50,
		},
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
		Doc: false,
		System: game.SystemData{
			POIs: []game.POI{
				{ID: "poi-station-1", Name: "Alpha Station", Type: "station"},
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

	// Should have scan.
	if !set["scan"] {
		t.Error("scan should always be present in space")
	}

	// Should have travel (POIs exist).
	if !set["travel"] {
		t.Error("travel should be present when POIs exist")
	}

	// Should have dock (station present).
	if !set["dock"] {
		t.Error("dock should be present when a station or outpost POI exists")
	}

	// Should have jump (connections exist).
	if !set["jump"] {
		t.Error("jump should be present when connections exist")
	}

	// Should have mine (asteroid_belt present).
	if !set["mine"] {
		t.Error("mine should be present when asteroid_belt POI exists")
	}

	// Verify travel targets include all POI IDs.
	var travelTargets []string
	for _, a := range actions {
		if a.Action == "travel" {
			travelTargets = a.Targets
			break
		}
	}
	if len(travelTargets) != 3 {
		t.Errorf("expected 3 travel targets, got %d", len(travelTargets))
	}

	// Verify jump targets include all connection system IDs.
	var jumpTargets []string
	for _, a := range actions {
		if a.Action == "jump" {
			jumpTargets = a.Targets
			break
		}
	}
	if len(jumpTargets) != 2 {
		t.Errorf("expected 2 jump targets, got %d", len(jumpTargets))
	}

	// Docked-only actions should not be present.
	if set["undock"] {
		t.Error("undock should not be present in space")
	}
	if set["view_market"] {
		t.Error("view_market should not be present in space")
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

	// Scan should still be present.
	if !set["scan"] {
		t.Error("scan should always be present in space")
	}

	// No travel, dock, jump, or mine without POIs/connections.
	if set["travel"] {
		t.Error("travel should not be present when there are no POIs")
	}
	if set["dock"] {
		t.Error("dock should not be present when there are no station/outpost POIs")
	}
	if set["jump"] {
		t.Error("jump should not be present when there are no connections")
	}
	if set["mine"] {
		t.Error("mine should not be present when there are no asteroid POIs")
	}
}

func TestValidActions_InCombat(t *testing.T) {
	state := &game.State{
		Doc:      false,
		InCombat: true,
		System: game.SystemData{
			POIs:        []game.POI{},
			Connections: []game.ConnectionInfo{},
		},
	}

	actions := ValidActions(state)
	set := actionSet(actions)

	// Combat actions should be present.
	combatActions := []string{"battle_advance", "battle_retreat", "battle_stance", "battle_target"}
	for _, a := range combatActions {
		if !set[a] {
			t.Errorf("expected combat action %q to be present when in combat", a)
		}
	}
}

func TestValidActions_NotInCombat_NoBattleActions(t *testing.T) {
	state := &game.State{
		Doc:      false,
		InCombat: false,
	}

	actions := ValidActions(state)
	set := actionSet(actions)

	combatActions := []string{"battle_advance", "battle_retreat", "battle_stance", "battle_target"}
	for _, a := range combatActions {
		if set[a] {
			t.Errorf("combat action %q should not be present when not in combat", a)
		}
	}
}

func TestValidActions_AlwaysIncludesQueries(t *testing.T) {
	queryActions := []string{
		"get_status", "get_system", "get_cargo", "get_skills", "get_map", "get_nearby",
	}

	// Test in docked state.
	dockedState := &game.State{Doc: true}
	dockedActions := ValidActions(dockedState)
	dockedSet := actionSet(dockedActions)
	for _, q := range queryActions {
		if !dockedSet[q] {
			t.Errorf("query action %q should always be present (docked state)", q)
		}
	}

	// Test in space state.
	spaceState := &game.State{Doc: false}
	spaceActions := ValidActions(spaceState)
	spaceSet := actionSet(spaceActions)
	for _, q := range queryActions {
		if !spaceSet[q] {
			t.Errorf("query action %q should always be present (space state)", q)
		}
	}

	// Test in combat state.
	combatState := &game.State{Doc: false, InCombat: true}
	combatActions := ValidActions(combatState)
	combatSet := actionSet(combatActions)
	for _, q := range queryActions {
		if !combatSet[q] {
			t.Errorf("query action %q should always be present (combat state)", q)
		}
	}
}
