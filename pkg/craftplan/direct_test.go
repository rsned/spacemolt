package craftplan

import (
	"context"
	"math"
	"sort"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// fakeSource is the test double used across this package's tests. It returns
// canned fixtures from its fields so each test can construct exactly the
// world it wants without database setup.
type fakeSource struct {
	recipes   map[string]serverapi.Recipe
	inventory Inventory
	skills    map[string]int
	stationID string
	illegal   map[string]bool
	bom       map[string][]BOMRow
	bomErr    error // if non-nil, BOM() returns this (simulates missing crafting DB)
}

func (f *fakeSource) Recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error) {
	return f.recipes, nil
}
func (f *fakeSource) Inventory(ctx context.Context, includeFaction bool) (Inventory, error) {
	inv := f.inventory
	if !includeFaction {
		inv.Faction = nil
	}
	return inv, nil
}
func (f *fakeSource) Skills(ctx context.Context) (map[string]int, error) {
	return f.skills, nil
}
func (f *fakeSource) CurrentStationID(ctx context.Context) (string, error) {
	return f.stationID, nil
}
func (f *fakeSource) IllegalAt(ctx context.Context, stationID string) (map[string]bool, error) {
	return f.illegal, nil
}
func (f *fakeSource) BOM(ctx context.Context, recipeIDs []string) (map[string][]BOMRow, error) {
	if f.bomErr != nil {
		return nil, f.bomErr
	}
	return f.bom, nil
}

// recipe constructs a minimal Recipe for tests.
func recipe(id, name, category string, inputs []serverapi.RecipeItem, outputs []serverapi.RecipeItem, skills map[string]int) serverapi.Recipe {
	return serverapi.Recipe{
		ID:             id,
		Name:           name,
		Category:       category,
		Inputs:         inputs,
		Outputs:        outputs,
		RequiredSkills: skills,
		CraftingTime:   6,
	}
}

func item(id string, qty int) serverapi.RecipeItem {
	return serverapi.RecipeItem{ItemID: id, Quantity: qty}
}

func TestCraftable_Direct_AllGates(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"alloy_titanium_ingot": recipe(
				"alloy_titanium_ingot", "Titanium Alloy Ingot", "Refining",
				[]serverapi.RecipeItem{item("iron_ore", 3), item("titanium_ore", 2)},
				[]serverapi.RecipeItem{item("titanium_alloy", 2)},
				nil,
			),
			"build_capital_gun": recipe(
				"build_capital_gun", "Capital Gun", "Weapons",
				[]serverapi.RecipeItem{item("titanium_alloy", 10)},
				[]serverapi.RecipeItem{item("capital_gun", 1)},
				map[string]int{"crafting": 30},
			),
			"build_smuggle_box": recipe(
				"build_smuggle_box", "Smuggle Box", "Contraband",
				[]serverapi.RecipeItem{item("titanium_alloy", 1)},
				[]serverapi.RecipeItem{item("smuggle_box", 1)},
				nil,
			),
			"refining_pure": recipe(
				"refining_pure", "Pure Refining", "Refining",
				nil, // no inputs
				[]serverapi.RecipeItem{item("nothing", 1)},
				nil,
			),
		},
		inventory: Inventory{
			Cargo:   map[string]int{"iron_ore": 6},
			Storage: map[string]int{"titanium_ore": 4, "titanium_alloy": 5},
		},
		skills:    map[string]int{"crafting": 5}, // gates out build_capital_gun (needs 30)
		stationID: "market_prime",
		illegal:   map[string]bool{"build_smuggle_box": true}, // gates out smuggle_box
	}

	eng := New(src)
	rows, _, err := eng.Craftable(context.Background(), CraftableOpts{})
	if err != nil {
		t.Fatalf("Craftable: %v", err)
	}

	got := map[string]int{}
	for _, r := range rows {
		got[r.Recipe.ID] = r.CanMake
	}

	want := map[string]int{
		"alloy_titanium_ingot": 2,           // min(6/3, 4/2) = min(2, 2) = 2
		"refining_pure":        math.MaxInt, // no inputs → ∞
	}

	if len(got) != len(want) {
		t.Errorf("got %d rows, want %d: %#v", len(got), len(want), got)
	}
	for id, wantCM := range want {
		if got[id] != wantCM {
			t.Errorf("recipe %s: can_make = %d, want %d", id, got[id], wantCM)
		}
	}
	if _, ok := got["build_capital_gun"]; ok {
		t.Error("build_capital_gun should be filtered by skill gate")
	}
	if _, ok := got["build_smuggle_box"]; ok {
		t.Error("build_smuggle_box should be filtered by legality gate")
	}
}

func TestCraftable_Direct_SortStability(t *testing.T) {
	// Three recipes with the same can_make should sort alphabetically by recipe_id.
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"c_recipe": recipe("c_recipe", "C", "Cat", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("c_out", 1)}, nil),
			"a_recipe": recipe("a_recipe", "A", "Cat", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("a_out", 1)}, nil),
			"b_recipe": recipe("b_recipe", "B", "Cat", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("b_out", 1)}, nil),
		},
		inventory: Inventory{Cargo: map[string]int{"ore": 5}},
	}

	eng := New(src)
	rows, _, _ := eng.Craftable(context.Background(), CraftableOpts{})
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.Recipe.ID
	}
	want := []string{"a_recipe", "b_recipe", "c_recipe"}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("rows not sorted: %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("position %d: got %s, want %s", i, ids[i], want[i])
		}
	}
}

func TestCraftable_Direct_FilterCategory(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"a": recipe("a", "A", "Refining", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("a_out", 1)}, nil),
			"b": recipe("b", "B", "Weapons", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("b_out", 1)}, nil),
		},
		inventory: Inventory{Cargo: map[string]int{"ore": 5}},
	}

	eng := New(src)
	rows, _, _ := eng.Craftable(context.Background(), CraftableOpts{CategoryFilter: "weap"})
	if len(rows) != 1 || rows[0].Recipe.ID != "b" {
		t.Errorf("CategoryFilter='weap' returned %v, want [b]", rows)
	}
}

func TestCraftable_Direct_MaxCap(t *testing.T) {
	recs := map[string]serverapi.Recipe{}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		recs[id] = recipe(id, id, "X", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item(id+"_out", 1)}, nil)
	}
	src := &fakeSource{
		recipes:   recs,
		inventory: Inventory{Cargo: map[string]int{"ore": 100}},
	}
	eng := New(src)
	rows, _, _ := eng.Craftable(context.Background(), CraftableOpts{Max: 2})
	if len(rows) != 2 {
		t.Errorf("Max=2 returned %d rows, want 2", len(rows))
	}
}
