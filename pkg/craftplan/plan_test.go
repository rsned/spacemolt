package craftplan

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestPlan_Direct_Ready(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"alloy_titanium_ingot": recipe(
				"alloy_titanium_ingot", "Titanium Alloy Ingot", "Refining",
				[]serverapi.RecipeItem{item("iron_ore", 3), item("titanium_ore", 2)},
				[]serverapi.RecipeItem{item("titanium_alloy", 2)},
				nil,
			),
		},
		inventory: Inventory{
			Cargo:   map[string]int{"iron_ore": 12},
			Storage: map[string]int{"titanium_ore": 380, "iron_ore": 450},
		},
		stationID: "market_prime",
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "alloy_titanium_ingot", Quantity: 10})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !res.Ready {
		t.Errorf("expected Ready=true, got false; inputs: %+v", res.Inputs)
	}
	if res.Quantity != 10 {
		t.Errorf("Quantity = %d, want 10", res.Quantity)
	}
	if len(res.Inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(res.Inputs))
	}
	for _, row := range res.Inputs {
		if row.Short != 0 {
			t.Errorf("input %s: Short=%d, want 0", row.ItemID, row.Short)
		}
	}
}

func TestPlan_Direct_Short(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"assemble_advanced_repair_kit": recipe(
				"assemble_advanced_repair_kit", "Advanced Repair Kit", "Consumables",
				[]serverapi.RecipeItem{
					item("circuit_board", 1),
					item("flex_polymer", 3),
					item("titanium_alloy", 3),
				},
				[]serverapi.RecipeItem{item("advanced_repair_kit", 1)},
				nil,
			),
		},
		inventory: Inventory{
			Cargo:   map[string]int{"titanium_alloy": 2},
			Storage: map[string]int{"circuit_board": 3, "flex_polymer": 20, "titanium_alloy": 18},
		},
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "assemble_advanced_repair_kit", Quantity: 5})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Ready {
		t.Error("expected Ready=false for short plan")
	}

	// need 5 circuit_board, have 3 → short 2
	// need 15 flex_polymer, have 20 → short 0
	// need 15 titanium_alloy, have 20 (2+18) → short 0
	byID := map[string]PlanInputRow{}
	for _, r := range res.Inputs {
		byID[r.ItemID] = r
	}
	if byID["circuit_board"].Short != 2 {
		t.Errorf("circuit_board short = %d, want 2", byID["circuit_board"].Short)
	}
	if byID["flex_polymer"].Short != 0 {
		t.Errorf("flex_polymer short = %d, want 0", byID["flex_polymer"].Short)
	}
	if byID["titanium_alloy"].Short != 0 {
		t.Errorf("titanium_alloy short = %d (cargo %d, storage %d), want 0",
			byID["titanium_alloy"].Short, byID["titanium_alloy"].HaveCargo, byID["titanium_alloy"].HaveStorage)
	}
}

func TestPlan_DefaultQuantity(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"r": recipe("r", "R", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("r_out", 1)}, nil),
		},
		inventory: Inventory{Cargo: map[string]int{"a": 5}},
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "r"}) // Quantity omitted → 1
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Quantity != 1 {
		t.Errorf("Quantity = %d, want 1", res.Quantity)
	}
}

func TestPlan_QuantityValidation(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"r": recipe("r", "R", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("r_out", 1)}, nil),
		},
	}
	eng := New(src)
	if _, err := eng.Plan(context.Background(), PlanOpts{ID: "r", Quantity: -1}); err == nil {
		t.Error("expected error for negative Quantity")
	}
}

func TestPlan_RecipeNotFound(t *testing.T) {
	src := &fakeSource{recipes: map[string]serverapi.Recipe{}}
	eng := New(src)
	_, err := eng.Plan(context.Background(), PlanOpts{ID: "nonexistent"})
	if err == nil {
		t.Error("expected error for missing recipe")
	}
}

func TestPlan_SkillBlocked(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"r": recipe("r", "R", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("r_out", 1)},
				map[string]int{"crafting": 50}),
		},
		inventory: Inventory{Cargo: map[string]int{"a": 5}},
		skills:    map[string]int{"crafting": 5},
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "r"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Ready {
		t.Error("expected Ready=false when skill-blocked")
	}
	if got := res.BlockedSkill["crafting"]; got != 45 {
		t.Errorf("BlockedSkill[crafting] = %d, want 45", got)
	}
}
