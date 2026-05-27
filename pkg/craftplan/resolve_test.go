package craftplan

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestPlan_ResolveItemID(t *testing.T) {
	// Recipe outputs "titanium_alloy"; plan("titanium_alloy") should resolve
	// to its recipe.
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"alloy_titanium_ingot": recipe(
				"alloy_titanium_ingot", "Titanium Alloy", "Refining",
				[]serverapi.RecipeItem{item("iron_ore", 3)},
				[]serverapi.RecipeItem{item("titanium_alloy", 2)},
				nil,
			),
		},
		inventory: Inventory{Cargo: map[string]int{"iron_ore": 10}},
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "titanium_alloy"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Recipe.ID != "alloy_titanium_ingot" {
		t.Errorf("Resolved recipe = %q, want alloy_titanium_ingot", res.Recipe.ID)
	}
}

func TestPlan_RecipeIDWinsOverItemID(t *testing.T) {
	// Edge case: an item and a recipe share the same id. Recipe wins.
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"shared": recipe("shared", "Recipe Shared", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("recipe_out", 1)}, nil),
			"other": recipe("other", "Outputs Shared", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("shared", 1)}, nil), // also outputs "shared"
		},
		inventory: Inventory{Cargo: map[string]int{"a": 10}},
	}
	eng := New(src)
	res, _ := eng.Plan(context.Background(), PlanOpts{ID: "shared"})
	if res.Recipe.ID != "shared" {
		t.Errorf("Recipe-id tie went to %q, want shared", res.Recipe.ID)
	}
}

func TestPlan_AlternativeRecipesPickLowestSkill(t *testing.T) {
	// Two recipes output "widget". One needs crafting=30, the other crafting=5.
	// Plan("widget") should pick the lower-skill one.
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"build_widget_hard": recipe("build_widget_hard", "Hard", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("widget", 1)},
				map[string]int{"crafting": 30}),
			"build_widget_easy": recipe("build_widget_easy", "Easy", "X",
				[]serverapi.RecipeItem{item("b", 1)},
				[]serverapi.RecipeItem{item("widget", 1)},
				map[string]int{"crafting": 5}),
		},
		inventory: Inventory{Cargo: map[string]int{"a": 5, "b": 5}},
		skills:    map[string]int{"crafting": 50}, // both unlocked
	}
	eng := New(src)
	res, _ := eng.Plan(context.Background(), PlanOpts{ID: "widget"})
	if res.Recipe.ID != "build_widget_easy" {
		t.Errorf("got %q, want build_widget_easy (lower-skill alternative)", res.Recipe.ID)
	}
}

func TestSuggestCloseMatches(t *testing.T) {
	have := []string{"alloy_titanium_ingot", "assemble_advanced_repair_kit", "build_capital_gun"}
	// Typo with 1-character difference should rank top.
	got := suggestCloseMatches("alloy_titanium_inggot", have, 5)
	if len(got) == 0 || got[0] != "alloy_titanium_ingot" {
		t.Errorf("suggestions = %v, expected alloy_titanium_ingot first", got)
	}
}
