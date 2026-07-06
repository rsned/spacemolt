package main

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/pricing"
)

func TestResolveRecipesForOutput(t *testing.T) {
	recipes := map[string]serverapi.Recipe{
		"r_widget_a": {ID: "r_widget_a", Name: "A", Outputs: []serverapi.RecipeItem{{ItemID: "widget", Quantity: 2}}},
		"r_widget_b": {ID: "r_widget_b", Name: "B", Outputs: []serverapi.RecipeItem{{ItemID: "widget", Quantity: 1}}},
		"r_other":    {ID: "r_other", Name: "O", Outputs: []serverapi.RecipeItem{{ItemID: "gadget", Quantity: 1}}},
	}
	got := resolveRecipesForOutput(recipes, "widget")
	if len(got) != 2 {
		t.Fatalf("want 2 recipes for widget, got %d", len(got))
	}
	// deterministic order by recipe ID
	if got[0].ID != "r_widget_a" || got[1].ID != "r_widget_b" {
		t.Fatalf("unsorted: %s %s", got[0].ID, got[1].ID)
	}
	if len(resolveRecipesForOutput(recipes, "nonexistent")) != 0 {
		t.Fatalf("expected none for nonexistent")
	}
}

func TestRecipeComponents(t *testing.T) {
	r := serverapi.Recipe{
		ID:      "r_widget",
		Inputs:  []serverapi.RecipeItem{{ItemID: "iron_ore", Quantity: 20}, {ItemID: "copper_ore", Quantity: 8}},
		Outputs: []serverapi.RecipeItem{{ItemID: "widget", Quantity: 5}},
	}
	comps, units := recipeComponents(r)
	if units != 5 {
		t.Fatalf("outputUnits want 5 got %d", units)
	}
	if len(comps) != 2 || comps[0].ItemID != "iron_ore" || comps[0].Qty != 20 {
		t.Fatalf("comps wrong: %+v", comps)
	}
	// no declared output quantity -> defaults to 1
	r2 := serverapi.Recipe{Outputs: []serverapi.RecipeItem{{ItemID: "x"}}}
	if _, u := recipeComponents(r2); u != 1 {
		t.Fatalf("default outputUnits want 1 got %d", u)
	}
}

func TestPickBestRecipe(t *testing.T) {
	reports := []*pricing.PriceReport{
		{RecipeName: "A", Nearby: pricing.Basis{Suggested: 200, Covered: 2, Total: 2}, Mkt: pricing.Basis{Suggested: 150, Covered: 2, Total: 2}},
		{RecipeName: "B", Nearby: pricing.Basis{Suggested: 180, Covered: 2, Total: 2}, Mkt: pricing.Basis{Suggested: 190, Covered: 2, Total: 2}},
	}
	best, alt := pickBestRecipe(reports)
	if best != 1 { // B cheaper on nearby (180 < 200)
		t.Fatalf("best want 1 got %d", best)
	}
	if alt != 0 { // A cheaper on market basis (150 < 190) and differs from best
		t.Fatalf("altMkt want 0 got %d", alt)
	}
}

func TestPickBestRecipeSingle(t *testing.T) {
	best, alt := pickBestRecipe([]*pricing.PriceReport{{RecipeName: "only"}})
	if best != 0 || alt != -1 {
		t.Fatalf("single: best=%d alt=%d", best, alt)
	}
}

func TestRenderPriceTextUnderpriced(t *testing.T) {
	rep := &pricing.PriceReport{
		ItemID: "widget", RecipeName: "recipe_widget", OutputUnits: 5, MarginPct: 20,
		Components: []pricing.PricedComponent{
			{Component: pricing.Component{ItemID: "iron_ore", Qty: 20}, NearbyUnit: 8, MktUnit: 14, NearbyFound: true, MktFound: true},
			{Component: pricing.Component{ItemID: "rare_ore", Qty: 2}, MktUnit: 100, MktFound: true}, // no nearby
		},
		Nearby: pricing.Basis{BuildCost: 160, PerUnit: 32, Margin: 6.4, Suggested: 38.4, Covered: 1, Total: 2},
		Mkt:    pricing.Basis{BuildCost: 480, PerUnit: 96, Margin: 19.2, Suggested: 115.2, Covered: 2, Total: 2},
		CurAskNearby: 500, HasAskNearby: true, CurAskMkt: 520, HasAskMkt: true, CurBid: 400, HasBid: true,
		Class: pricing.ClassUnder,
	}
	out := renderPriceText("widget", "sol", 2, 20, []modeReport{{Label: "RECIPE", R: rep}}, "")

	for _, want := range []string{
		"widget", "RECIPE", "recipe_widget", "5 units/run",
		"iron_ore", "rare_ore", "—", // rare_ore has no nearby price -> dash
		"+ 20% margin", "SUGGESTED", "115.20",
		"1/2 priced nearby", "rare_ore", // feasibility line names the gap
		"CURRENT MARKET", "500", "400", "UNDERPRICED",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}
