package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/pricing"
)

// ghostRoundsRecipes mirrors the live crafting data for ghost_rounds (verified
// against spacemolt-crafting-server 2026-07-25), including BOTH copper_wiring
// routes so route selection is exercised:
//
//	forge_ghost_rounds     2 ghost_rounds  <- 2 hot_cell + 3 titanium_ingot
//	crack_hot_cells        4 hot_cell      <- 4 copper_wiring + 6 plasma_gas
//	process_copper_wiring  2 copper_wiring <- 4 copper_ore   (2 ore/wiring, facility)
//	basic_copper_processing 1 copper_wiring<- 8 copper_ore   (8 ore/wiring, hand)
//	refine_titanium        1 titanium_ingot<- 5 titanium_ore
func ghostRoundsRecipes() map[string]serverapi.Recipe {
	return map[string]serverapi.Recipe{
		"forge_ghost_rounds": {
			ID: "forge_ghost_rounds", Name: "Forge Ghost Rounds", FacilityOnly: true,
			Inputs:  []serverapi.RecipeItem{{ItemID: "hot_cell", Quantity: 2}, {ItemID: "titanium_ingot", Quantity: 3}},
			Outputs: []serverapi.RecipeItem{{ItemID: "ghost_rounds", Quantity: 2}},
		},
		"crack_hot_cells": {
			ID: "crack_hot_cells", Name: "Crack Hot Cells", FacilityOnly: true,
			Inputs:  []serverapi.RecipeItem{{ItemID: "copper_wiring", Quantity: 4}, {ItemID: "plasma_gas", Quantity: 6}},
			Outputs: []serverapi.RecipeItem{{ItemID: "hot_cell", Quantity: 4}},
		},
		"process_copper_wiring": {
			ID: "process_copper_wiring", Name: "Process Copper Wiring", FacilityOnly: true,
			Inputs:  []serverapi.RecipeItem{{ItemID: "copper_ore", Quantity: 4}},
			Outputs: []serverapi.RecipeItem{{ItemID: "copper_wiring", Quantity: 2}},
		},
		"basic_copper_processing": {
			ID: "basic_copper_processing", Name: "Basic Copper Processing",
			Inputs:  []serverapi.RecipeItem{{ItemID: "copper_ore", Quantity: 8}},
			Outputs: []serverapi.RecipeItem{{ItemID: "copper_wiring", Quantity: 1}},
		},
		"refine_titanium": {
			ID: "refine_titanium", Name: "Refine Titanium",
			Inputs:  []serverapi.RecipeItem{{ItemID: "titanium_ore", Quantity: 5}},
			Outputs: []serverapi.RecipeItem{{ItemID: "titanium_ingot", Quantity: 1}},
		},
	}
}

func qtyOf(e expandedBOM, itemID string) (float64, bool) {
	for _, c := range e.Comps {
		if c.ItemID == itemID {
			return c.Qty, true
		}
	}
	return 0, false
}

// The whole point of expanding from recipes instead of bill_of_materials: the
// stored table is INTEGER, so it ceils 1.5 plasma_gas to 2 and 7.5 titanium_ore
// to 8 at write time (+33% / +6.7%, unrecoverable). Exact expansion must keep
// the fractions. Rounding any quantity fails this test.
func TestExpandToBaseKeepsFractionalQuantities(t *testing.T) {
	got := expandToBase("ghost_rounds", ghostRoundsRecipes())

	for _, want := range []struct {
		item string
		qty  float64
	}{
		{"plasma_gas", 1.5},
		{"titanium_ore", 7.5},
		{"copper_ore", 2},
	} {
		q, ok := qtyOf(got, want.item)
		if !ok {
			t.Fatalf("%s missing from expansion %+v", want.item, got.Comps)
		}
		if q != want.qty {
			t.Errorf("%s = %v, want %v (per ONE ghost_rounds)", want.item, q, want.qty)
		}
	}
	if len(got.Comps) != 3 {
		t.Errorf("want exactly 3 base components, got %+v", got.Comps)
	}
}

// copper_wiring has two routes: 2 ore/unit (facility) and 8 ore/unit (hand).
// The cheapest must win, so copper_ore lands at 2 per ghost_rounds, not 8.
// Operator decision 2026-07-25: facility routes are the accessible optimum.
func TestExpandToBasePicksCheapestRoute(t *testing.T) {
	got := expandToBase("ghost_rounds", ghostRoundsRecipes())

	q, ok := qtyOf(got, "copper_ore")
	if !ok {
		t.Fatal("copper_ore missing from expansion")
	}
	if q != 2 {
		t.Errorf("copper_ore = %v, want 2 (cheapest route: process_copper_wiring at 2 ore/wiring, not basic_copper_processing at 8)", q)
	}
	if !slices.Contains(got.Path, "process_copper_wiring") {
		t.Errorf("path must record the chosen route, got %v", got.Path)
	}
	if slices.Contains(got.Path, "basic_copper_processing") {
		t.Errorf("path must not include the rejected route, got %v", got.Path)
	}
	if !got.UsesFacility {
		t.Error("expansion routes through facility-only recipes; UsesFacility must be true so the caller can disclose it")
	}
}

// A base material (nothing produces it) expands to itself at qty 1 — the
// recursion's floor, and what makes an ore BOM terminate.
func TestExpandToBaseOnRawMaterial(t *testing.T) {
	got := expandToBase("copper_ore", ghostRoundsRecipes())
	if len(got.Comps) != 1 || got.Comps[0].ItemID != "copper_ore" || got.Comps[0].Qty != 1 {
		t.Fatalf("a raw material must expand to itself at qty 1, got %+v", got.Comps)
	}
}

// A recipe cycle (A needs B, B needs A) must terminate and say so rather than
// recursing until the stack dies.
func TestExpandToBaseSurvivesRecipeCycle(t *testing.T) {
	cyclic := map[string]serverapi.Recipe{
		"make_a": {
			ID:      "make_a",
			Inputs:  []serverapi.RecipeItem{{ItemID: "b", Quantity: 1}},
			Outputs: []serverapi.RecipeItem{{ItemID: "a", Quantity: 1}},
		},
		"make_b": {
			ID:      "make_b",
			Inputs:  []serverapi.RecipeItem{{ItemID: "a", Quantity: 1}},
			Outputs: []serverapi.RecipeItem{{ItemID: "b", Quantity: 1}},
		},
	}
	got := expandToBase("a", cyclic)
	if !got.Truncated {
		t.Error("a recipe cycle must set Truncated so the caller can warn")
	}
}

// The BOM basis silently assumed facility access and hid which sub-routes it
// chose (2026-07-25: copper_wiring resolved via the facility-only
// process_copper_wiring at 2 ore/unit, while a hand-crafter needs 8 — a 4x
// understatement with no disclosure). The block must now name the route and
// flag the facility assumption.
func TestRenderPriceTextShowsBomRouteNote(t *testing.T) {
	rep := &pricing.PriceReport{
		ItemID:      "ghost_rounds",
		OutputUnits: 1,
		Components: []pricing.PricedComponent{
			{Component: pricing.Component{ItemID: "copper_ore", Qty: 2}, NearbyUnit: 8, NearbyFound: true, MktUnit: 27.05, MktFound: true},
		},
		Nearby: pricing.Basis{BuildCost: 16, PerUnit: 16, Margin: 3.2, Suggested: 19.2, Covered: 1, Total: 1},
		Mkt:    pricing.Basis{BuildCost: 54.1, PerUnit: 54.1, Margin: 10.82, Suggested: 64.92, Covered: 1, Total: 1},
	}
	note := "route: forge_ghost_rounds → process_copper_wiring\n  ⚠ assumes facility access"

	out := renderPriceText("ghost_rounds", "sol", 2, 20, []modeReport{{Label: "BOM (ore)", R: rep, Note: note}}, "")

	for _, want := range []string{"route:", "process_copper_wiring", "assumes facility access"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}

// Exercises the expander against the REAL recipe catalog rather than a
// hand-built fixture, so a shape change in the shipped data (or a new cheaper
// route) surfaces here instead of in a live price quote. Skips when the catalog
// snapshot is absent so the package still tests without data/ checked out.
//
// catalog_recipes.json holds full Recipe objects under "items" (not
// CatalogItem) — see the catalog decode quirk in the API notes.
func TestExpandToBaseAgainstRealCatalog(t *testing.T) {
	raw, err := os.ReadFile("../../../data/game-api/latest/catalog_recipes.json")
	if err != nil {
		t.Skipf("recipe catalog not available: %v", err)
	}
	var payload struct {
		Items []serverapi.Recipe `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	recipes := make(map[string]serverapi.Recipe, len(payload.Items))
	for _, r := range payload.Items {
		recipes[r.ID] = r
	}

	got := expandToBase("ghost_rounds", recipes)

	// Hand-derived from the catalog: 1 ghost_rounds = 1 hot_cell + 1.5
	// titanium_ingot; hot_cell = 1.5 plasma_gas + 1 copper_wiring; copper_wiring
	// = 2 copper_ore via process_copper_wiring; titanium_ingot = 5 titanium_ore.
	for _, want := range []struct {
		item string
		qty  float64
	}{
		{"plasma_gas", 1.5},
		{"titanium_ore", 7.5},
		{"copper_ore", 2},
	} {
		q, ok := qtyOf(got, want.item)
		if !ok {
			t.Fatalf("%s missing from real-catalog expansion %+v", want.item, got.Comps)
		}
		if q != want.qty {
			t.Errorf("%s = %v, want %v", want.item, q, want.qty)
		}
	}
	if got.Truncated {
		t.Error("real ghost_rounds chain must expand fully, not truncate")
	}
	if !got.UsesFacility {
		t.Error("the real chain is facility-only end to end; UsesFacility must be true")
	}
}
