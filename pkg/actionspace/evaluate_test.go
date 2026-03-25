package actionspace

import "testing"

func TestEvaluateDockedBasic(t *testing.T) {
	gc := GameContext{
		Docked: true, Credits: 5000,
		Hull: 80, MaxHull: 100, Fuel: 40, MaxFuel: 100,
		CargoUsed: 10, CargoCapacity: 50, CargoItemCount: 2,
		SystemPOIs: []POIInfo{
			{ID: "station_1", Type: "station", HasBase: true},
			{ID: "belt_1", Type: "asteroid_belt"},
		},
		Connections: []ConnectionInfo{{SystemID: "alpha", Name: "Alpha Centauri", Distance: 5}},
	}

	as := Evaluate(gc)

	if len(as.Actions) != len(AllActions) {
		t.Errorf("expected %d results, got %d", len(AllActions), len(as.Actions))
	}

	for _, r := range as.Actions {
		if r.Action.Name == "undock" && !r.Valid {
			t.Errorf("undock should be valid when docked, failed: %v", r.FailedChecks)
		}
		if r.Action.Name == "mine" && r.Valid {
			t.Error("mine should be invalid when docked")
		}
	}

	if as.Stats.TotalActions == 0 || as.Stats.ValidActions == 0 || as.Stats.PrunedActions == 0 {
		t.Error("Stats should be populated")
	}
	if as.Stats.BranchingFactor == 0 {
		t.Error("BranchingFactor should not be 0")
	}
}

func TestEvaluateUndockedAtMiningPOI(t *testing.T) {
	gc := GameContext{
		CurrentPOIType: "asteroid_belt", CurrentPOIID: "belt_1",
		Fuel: 50, MaxFuel: 100, CargoUsed: 0, CargoCapacity: 50,
		SystemPOIs: []POIInfo{
			{ID: "station_1", Type: "station", HasBase: true},
			{ID: "belt_1", Type: "asteroid_belt"},
		},
		Connections: []ConnectionInfo{{SystemID: "alpha", Name: "Alpha Centauri"}},
	}

	as := Evaluate(gc)

	for _, r := range as.Actions {
		if r.Action.Name == "mine" && !r.Valid {
			t.Errorf("mine should be valid at asteroid_belt, failed: %v", r.FailedChecks)
		}
		if r.Action.Name == "buy" && r.Valid {
			t.Error("buy should be invalid when undocked")
		}
	}
}

func TestEvaluateInCombat(t *testing.T) {
	gc := GameContext{
		InCombat: true, CurrentPOIType: "asteroid_belt",
		Hull: 50, MaxHull: 100, Fuel: 50, MaxFuel: 100,
		CargoUsed: 0, CargoCapacity: 50,
	}

	as := Evaluate(gc)

	for _, r := range as.Actions {
		if r.Action.Name == "battle_advance" && !r.Valid {
			t.Errorf("battle_advance should be valid in combat, failed: %v", r.FailedChecks)
		}
		if r.Action.Name == "travel" && r.Valid {
			t.Error("travel should be invalid in combat")
		}
	}
}

func TestEvaluateStats(t *testing.T) {
	gc := GameContext{
		Docked: true, Credits: 1000,
		Hull: 100, MaxHull: 100, Fuel: 100, MaxFuel: 100,
		CargoUsed: 10, CargoCapacity: 50, CargoItemCount: 1,
	}

	as := Evaluate(gc)

	if as.Stats.ValidActions+as.Stats.PrunedActions != as.Stats.TotalActions {
		t.Error("valid + pruned should equal total")
	}
	if len(as.Stats.ByCategory) == 0 {
		t.Error("ByCategory should be populated")
	}
	if len(as.Stats.TopPruningReasons) == 0 {
		t.Error("TopPruningReasons should be populated")
	}
	for i := 1; i < len(as.Stats.TopPruningReasons); i++ {
		if as.Stats.TopPruningReasons[i].Count > as.Stats.TopPruningReasons[i-1].Count {
			t.Error("TopPruningReasons should be sorted descending")
		}
	}
}

func TestEvaluateInTransit(t *testing.T) {
	gc := GameContext{InTransit: true, Fuel: 50, MaxFuel: 100, Hull: 100, MaxHull: 100}

	as := Evaluate(gc)

	if as.Stats.ValidActions > 5 {
		t.Errorf("expected very few valid actions in transit, got %d", as.Stats.ValidActions)
	}
}
