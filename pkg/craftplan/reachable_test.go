package craftplan

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Three-recipe chain:
//
//	iron_ore + titanium_ore → titanium_alloy   (alloy_titanium_ingot)
//	titanium_alloy + circuit_board → drone     (build_drone)
//	silicon_ore → circuit_board                (assemble_circuit_board)
func chainFixtures() *fakeSource {
	return &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"alloy_titanium_ingot": recipe(
				"alloy_titanium_ingot", "Titanium Alloy", "Refining",
				[]serverapi.RecipeItem{item("iron_ore", 3), item("titanium_ore", 2)},
				[]serverapi.RecipeItem{item("titanium_alloy", 2)},
				nil,
			),
			"assemble_circuit_board": recipe(
				"assemble_circuit_board", "Circuit Board", "Components",
				[]serverapi.RecipeItem{item("silicon_ore", 4)},
				[]serverapi.RecipeItem{item("circuit_board", 1)},
				nil,
			),
			"build_drone": recipe(
				"build_drone", "Build Drone", "Drones",
				[]serverapi.RecipeItem{item("titanium_alloy", 2), item("circuit_board", 1)},
				[]serverapi.RecipeItem{item("drone", 1)},
				nil,
			),
		},
		// Flattened BOM for build_drone — these are the rows the engine reads.
		bom: map[string][]BOMRow{
			"alloy_titanium_ingot": {
				{BaseItemID: "iron_ore", Quantity: 3, RecipePath: []string{"alloy_titanium_ingot"}},
				{BaseItemID: "titanium_ore", Quantity: 2, RecipePath: []string{"alloy_titanium_ingot"}},
			},
			"assemble_circuit_board": {
				{BaseItemID: "silicon_ore", Quantity: 4, RecipePath: []string{"assemble_circuit_board"}},
			},
			"build_drone": {
				// Per batch of build_drone (yields 1 drone) we need 2 titanium_alloy
				// (each from 3 iron_ore + 2 titanium_ore) and 1 circuit_board (from
				// 4 silicon_ore). BOM rows are pre-flattened so we list the totals.
				{BaseItemID: "iron_ore", Quantity: 3, RecipePath: []string{"build_drone", "alloy_titanium_ingot"}},
				{BaseItemID: "titanium_ore", Quantity: 2, RecipePath: []string{"build_drone", "alloy_titanium_ingot"}},
				{BaseItemID: "silicon_ore", Quantity: 4, RecipePath: []string{"build_drone", "assemble_circuit_board"}},
			},
		},
	}
}

func TestCraftable_Reachable_Depth(t *testing.T) {
	src := chainFixtures()
	src.inventory = Inventory{Storage: map[string]int{
		"iron_ore":     30,
		"titanium_ore": 20,
		"silicon_ore":  40,
	}}

	eng := New(src)
	rows, _, err := eng.Craftable(context.Background(), CraftableOpts{Reachable: true})
	if err != nil {
		t.Fatalf("Craftable: %v", err)
	}

	depthByID := map[string]int{}
	cmByID := map[string]int{}
	for _, r := range rows {
		depthByID[r.Recipe.ID] = r.Depth
		cmByID[r.Recipe.ID] = r.CanMake
	}

	if depthByID["alloy_titanium_ingot"] != 1 {
		t.Errorf("alloy_titanium_ingot depth = %d, want 1 (direct)", depthByID["alloy_titanium_ingot"])
	}
	if depthByID["assemble_circuit_board"] != 1 {
		t.Errorf("assemble_circuit_board depth = %d, want 1 (direct)", depthByID["assemble_circuit_board"])
	}
	if depthByID["build_drone"] != 2 {
		t.Errorf("build_drone depth = %d, want 2 (+1 craft)", depthByID["build_drone"])
	}

	// build_drone can_make: min(30/3, 20/2, 40/4) = min(10, 10, 10) = 10
	if cmByID["build_drone"] != 10 {
		t.Errorf("build_drone can_make = %d, want 10", cmByID["build_drone"])
	}
}

func TestPlan_Reachable_Shortfall(t *testing.T) {
	src := chainFixtures()
	src.inventory = Inventory{Storage: map[string]int{
		"iron_ore":     30, // OK
		"titanium_ore": 1,  // short — need 2
		"silicon_ore":  4,  // OK
	}}

	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "build_drone", Quantity: 1, Reachable: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Ready {
		t.Error("expected Ready=false")
	}
	byID := map[string]int{}
	for _, r := range res.Inputs {
		byID[r.ItemID] = r.Short
	}
	if byID["titanium_ore"] != 1 {
		t.Errorf("titanium_ore short = %d, want 1", byID["titanium_ore"])
	}
	if byID["iron_ore"] != 0 {
		t.Errorf("iron_ore short = %d, want 0", byID["iron_ore"])
	}

	// Intermediate crafts should list alloy_titanium_ingot and assemble_circuit_board.
	gotInter := map[string]bool{}
	for _, ic := range res.Intermediates {
		gotInter[ic.RecipeID] = true
	}
	if !gotInter["alloy_titanium_ingot"] {
		t.Error("expected alloy_titanium_ingot in Intermediates")
	}
	if !gotInter["assemble_circuit_board"] {
		t.Error("expected assemble_circuit_board in Intermediates")
	}
}

func TestCraftable_Reachable_BOMUnavailable(t *testing.T) {
	src := chainFixtures()
	src.bomErr = errors.New("crafting DB not configured")

	eng := New(src)
	_, _, err := eng.Craftable(context.Background(), CraftableOpts{Reachable: true})
	if err == nil {
		t.Fatal("expected BOM unavailable error")
	}
	if !errors.Is(err, ErrBOMUnavailable) {
		t.Errorf("expected ErrBOMUnavailable, got %v", err)
	}
}
