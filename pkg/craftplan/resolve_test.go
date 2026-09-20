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

// Resolution used to tie-break alphabetically, which picked recipes whose
// inputs we cannot obtain. Live 2026-09-20: `plan fuel_cell 1000` chose
// biogas_fuel_synthesis (500 crystallized_biogas, a wildlife drop we hold
// ZERO of) over craft_fuel_cell, while 38,097 liquid_hydrogen sat in storage.
// Every candidate had skill ceiling 0, so "biogas..." won on the letter b.
//
// Rank by what the agent can actually supply.
func TestResolvePrefersRecipesWeCanSupply(t *testing.T) {
	recs := map[string]serverapi.Recipe{
		"biogas_fuel_synthesis": {
			ID: "biogas_fuel_synthesis", Category: "Refining",
			Inputs:  []serverapi.RecipeItem{{ItemID: "crystallized_biogas", Quantity: 1}},
			Outputs: []serverapi.RecipeItem{{ItemID: "fuel_cell", Quantity: 2}},
		},
		"craft_fuel_cell": {
			ID: "craft_fuel_cell", Category: "Consumables",
			Inputs: []serverapi.RecipeItem{
				{ItemID: "liquid_hydrogen", Quantity: 2},
				{ItemID: "steel_plate", Quantity: 1},
			},
			Outputs: []serverapi.RecipeItem{{ItemID: "fuel_cell", Quantity: 1}},
		},
	}
	inv := Inventory{Storage: map[string]int{"liquid_hydrogen": 38097, "steel_plate": 3535}}

	e := &Engine{}
	got, alts, err := e.resolveRecipe("fuel_cell", recs, inv, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "craft_fuel_cell" {
		t.Errorf("picked %q, want craft_fuel_cell — its inputs are in storage", got.ID)
	}
	if len(alts) != 1 || alts[0].ID != "biogas_fuel_synthesis" {
		t.Errorf("alternatives = %v, want the rejected biogas recipe listed", alts)
	}
}

// Faction stock counts toward supplyability when the caller opted in, so a
// recipe fed from the lockbox is not passed over for an unobtainable one.
func TestResolveCountsFactionStockWhenIncluded(t *testing.T) {
	recs := map[string]serverapi.Recipe{
		"aaa_unobtainable": {
			ID: "aaa_unobtainable", Category: "Refining",
			Inputs:  []serverapi.RecipeItem{{ItemID: "creature_carapace", Quantity: 1}},
			Outputs: []serverapi.RecipeItem{{ItemID: "widget", Quantity: 1}},
		},
		"zzz_from_faction": {
			ID: "zzz_from_faction", Category: "Components",
			Inputs:  []serverapi.RecipeItem{{ItemID: "flex_polymer", Quantity: 4}},
			Outputs: []serverapi.RecipeItem{{ItemID: "widget", Quantity: 1}},
		},
	}
	inv := Inventory{Faction: map[string]int{"flex_polymer": 1940}}
	e := &Engine{}

	got, _, err := e.resolveRecipe("widget", recs, inv, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "zzz_from_faction" {
		t.Errorf("picked %q, want zzz_from_faction — faction stock was included", got.ID)
	}

	// Without --include-faction neither is supplyable, so the old ordering
	// (skill, then id) still applies and resolution stays deterministic.
	got, _, err = e.resolveRecipe("widget", recs, inv, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "aaa_unobtainable" {
		t.Errorf("picked %q, want the alphabetical fallback when nothing is supplyable", got.ID)
	}
}

// A "Ship Passive" is granted by a hull, never crafted. onboard_alloy_synthesis
// is hand_craftable:false with no producing facility, so offering it as a plan
// is always a dead end — exclude it outright rather than ranking it last.
func TestResolveExcludesShipPassives(t *testing.T) {
	recs := map[string]serverapi.Recipe{
		"onboard_alloy_synthesis": {
			ID: "onboard_alloy_synthesis", Category: "Ship Passive",
			Inputs:  []serverapi.RecipeItem{{ItemID: "titanium_ore", Quantity: 3}},
			Outputs: []serverapi.RecipeItem{{ItemID: "titanium_alloy", Quantity: 1}},
		},
		"forge_titanium_alloy": {
			ID: "forge_titanium_alloy", Category: "Refining", FacilityOnly: true,
			Inputs:  []serverapi.RecipeItem{{ItemID: "titanium_ore", Quantity: 3}},
			Outputs: []serverapi.RecipeItem{{ItemID: "titanium_alloy", Quantity: 1}},
		},
	}
	inv := Inventory{Storage: map[string]int{"titanium_ore": 4428}}
	e := &Engine{}
	got, alts, err := e.resolveRecipe("titanium_alloy", recs, inv, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "forge_titanium_alloy" {
		t.Errorf("picked %q, want forge_titanium_alloy — the passive needs a hull, not a craft", got.ID)
	}
	for _, a := range alts {
		if a.Category == "Ship Passive" {
			t.Errorf("Ship Passive %q offered as an alternative; it can never be crafted", a.ID)
		}
	}
}

// A Ship Passive must still resolve when asked for BY ID — the exclusion is
// about not CHOOSING one, not about hiding it from someone who named it.
func TestResolveStillAllowsExplicitShipPassiveByID(t *testing.T) {
	recs := map[string]serverapi.Recipe{
		"onboard_alloy_synthesis": {ID: "onboard_alloy_synthesis", Category: "Ship Passive"},
	}
	e := &Engine{}
	got, _, err := e.resolveRecipe("onboard_alloy_synthesis", recs, Inventory{}, false)
	if err != nil {
		t.Fatalf("explicit id must still resolve: %v", err)
	}
	if got.ID != "onboard_alloy_synthesis" {
		t.Errorf("got %q", got.ID)
	}
}
