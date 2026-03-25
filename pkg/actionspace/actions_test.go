package actionspace

import "testing"

func TestAllActionsHaveRequiredFields(t *testing.T) {
	for _, a := range AllActions {
		if a.Name == "" {
			t.Error("action has empty Name")
		}
		if a.Summary == "" {
			t.Errorf("action %q has empty Summary", a.Name)
		}
		if a.Category == "" {
			t.Errorf("action %q has empty Category", a.Name)
		}
		if !a.IsMutation {
			t.Errorf("action %q should be a mutation", a.Name)
		}
		if len(a.Preconditions) == 0 {
			t.Errorf("action %q has no preconditions", a.Name)
		}
	}
}

func TestAllActionsUniqueNames(t *testing.T) {
	seen := make(map[string]bool)
	for _, a := range AllActions {
		if seen[a.Name] {
			t.Errorf("duplicate action name: %q", a.Name)
		}
		seen[a.Name] = true
	}
}

func TestAllActionsValidCategories(t *testing.T) {
	validCategories := map[string]bool{
		"navigation": true, "mining": true, "trading": true,
		"exchange": true, "combat": true, "salvage": true,
		"ship": true, "cargo": true, "storage": true,
		"crafting": true, "missions": true, "faction": true,
		"social": true, "insurance": true, "exploration": true,
	}
	for _, a := range AllActions {
		if !validCategories[a.Category] {
			t.Errorf("action %q has unknown category %q", a.Name, a.Category)
		}
	}
}

func TestTargetGenerators(t *testing.T) {
	gc := &GameContext{
		CurrentPOIID: "belt_1",
		SystemPOIs: []POIInfo{
			{ID: "station_1", Name: "Earth Station", Type: "station", HasBase: true},
			{ID: "belt_1", Name: "Main Belt", Type: "asteroid_belt"},
			{ID: "planet_1", Name: "Mars", Type: "planet"},
		},
		Connections: []ConnectionInfo{
			{SystemID: "alpha", Name: "Alpha Centauri", Distance: 5},
			{SystemID: "beta", Name: "Beta Hydri", Distance: 3},
		},
	}

	for _, a := range AllActions {
		if a.Targets == nil {
			continue
		}
		targets := a.Targets(gc)
		switch a.Name {
		case "travel":
			if len(targets) != 2 {
				t.Errorf("travel targets: got %d, want 2", len(targets))
			}
		case "jump":
			if len(targets) != 2 {
				t.Errorf("jump targets: got %d, want 2", len(targets))
			}
		case "dock":
			if len(targets) != 1 {
				t.Errorf("dock targets: got %d, want 1", len(targets))
			}
		}
	}
}

func TestActionCount(t *testing.T) {
	if len(AllActions) < 40 {
		t.Errorf("expected at least 40 actions, got %d", len(AllActions))
	}
}
