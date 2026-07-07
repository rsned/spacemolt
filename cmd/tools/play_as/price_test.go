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
	comps, units := recipeComponents("widget", r)
	if units != 5 {
		t.Fatalf("outputUnits want 5 got %d", units)
	}
	if len(comps) != 2 || comps[0].ItemID != "iron_ore" || comps[0].Qty != 20 {
		t.Fatalf("comps wrong: %+v", comps)
	}
	// no declared output quantity -> defaults to 1
	r2 := serverapi.Recipe{Outputs: []serverapi.RecipeItem{{ItemID: "x"}}}
	if _, u := recipeComponents("x", r2); u != 1 {
		t.Fatalf("default outputUnits want 1 got %d", u)
	}
	// multi-output recipe: divisor must match the item actually being priced.
	r3 := serverapi.Recipe{
		ID:      "r_electrolyze_water",
		Outputs: []serverapi.RecipeItem{{ItemID: "a", Quantity: 4}, {ItemID: "b", Quantity: 2}},
	}
	if _, u := recipeComponents("b", r3); u != 2 {
		t.Fatalf("multi-output outputUnits for b want 2 got %d", u)
	}
	if _, u := recipeComponents("a", r3); u != 4 {
		t.Fatalf("multi-output outputUnits for a want 4 got %d", u)
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

func TestPickBestRecipeIgnoresIncompleteBasis(t *testing.T) {
	reports := []*pricing.PriceReport{
		{ // A: fully priced on both bases
			RecipeName: "A",
			Nearby:     pricing.Basis{Suggested: 200, Covered: 2, Total: 2},
			Mkt:        pricing.Basis{Suggested: 150, Covered: 2, Total: 2},
		},
		{ // B: incomplete on both bases — artificially cheap because missing
			// components contribute 0 cost, must not win.
			RecipeName: "B",
			Nearby:     pricing.Basis{Suggested: 50, Covered: 1, Total: 2},
			Mkt:        pricing.Basis{Suggested: 40, Covered: 1, Total: 2},
		},
	}
	best, alt := pickBestRecipe(reports)
	if best != 0 {
		t.Fatalf("best want 0 (A) got %d — incomplete report B must not win", best)
	}
	if alt != -1 {
		t.Fatalf("altMkt want -1 (B's Mkt basis is incomplete) got %d", alt)
	}
}

func TestPriceFlagFloat(t *testing.T) {
	const def = 20.0
	tests := []struct {
		name string
		v    any
		want float64
	}{
		{"int value", 17, 17},
		{"numeric string", "17.5", 17.5},
		{"missing flag (nil)", nil, def},
		{"non-numeric string falls back to default", "abc", def},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := priceFlagFloat(tc.v, def); got != tc.want {
				t.Fatalf("priceFlagFloat(%v, %v) = %v, want %v", tc.v, def, got, tc.want)
			}
		})
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
		"+ 20% margin", "SUGGESTED", "115.20", // mkt basis complete -> full suggested
		"38.40",                          // nearby basis partial -> floor still shown (was blank before)
		"nearby 1/2 (missing: rare_ore)", // coverage names the gap per basis
		"mkt-avg 2/2",
		"⚠",                                                // partial-basis warning
		"CURRENT MARKET", "500", "400", "UNDERPRICED",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}

// TestRenderPriceTextPartialMktBasisShowsFloor locks the reported bug: when one
// component has no market price at all (like ghost_rounds' hot_cell), the market
// basis is incomplete but the SUGGESTED total must still render (as a flagged
// partial floor), not blank out. The nearby basis here priced nothing, so it
// stays a dash.
func TestRenderPriceTextPartialMktBasisShowsFloor(t *testing.T) {
	rep := &pricing.PriceReport{
		ItemID: "ghost_rounds", RecipeName: "forge_ghost_rounds", OutputUnits: 2, MarginPct: 20,
		Components: []pricing.PricedComponent{
			{Component: pricing.Component{ItemID: "hot_cell", Qty: 2}},                                            // no price anywhere
			{Component: pricing.Component{ItemID: "titanium_ingot", Qty: 3}, MktUnit: 823.5, MktFound: true},      // mkt only
		},
		// mkt: only titanium_ingot priced -> 3*823.5=2470.5 /2 units =1235.25 +20% =1482.30 (partial)
		Nearby: pricing.Basis{Covered: 0, Total: 2},
		Mkt:    pricing.Basis{BuildCost: 2470.5, PerUnit: 1235.25, Margin: 247.05, Suggested: 1482.30, Covered: 1, Total: 2},
	}
	out := renderPriceText("ghost_rounds", "alhena", 2, 20, []modeReport{{Label: "RECIPE", R: rep}}, "")

	for _, want := range []string{
		"1482.30",                          // partial mkt SUGGESTED is shown, not blank
		"mkt-avg 1/2 (missing: hot_cell)",  // coverage explains the gap
		"nearby 0/2",                       // nearby priced nothing
		"⚠",                                // partial-basis warning present
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
	// The nearby SUGGESTED (0 components priced) must NOT fabricate a number.
	if strings.Contains(out, "= SUGGESTED") && !strings.Contains(out, "—") {
		t.Fatalf("expected a dash for the empty nearby basis:\n%s", out)
	}
}
