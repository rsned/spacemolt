package main

import (
	"sort"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/pricing"
)

// resolveRecipesForOutput returns every recipe whose outputs include itemID,
// sorted by recipe ID for deterministic selection. recipe.ID is not the output
// item_id, so this scans outputs rather than keying by id.
func resolveRecipesForOutput(recipes map[string]serverapi.Recipe, itemID string) []serverapi.Recipe {
	var out []serverapi.Recipe
	for _, r := range recipes {
		for _, o := range r.Outputs {
			if o.ItemID == itemID {
				out = append(out, r)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// recipeComponents converts a recipe's per-run inputs into pricing components
// and returns the output-units-per-run used to normalize the roll-up to a
// per-unit cost (defaults to 1 when no output quantity is declared).
func recipeComponents(r serverapi.Recipe) (comps []pricing.Component, outputUnits int) {
	for _, in := range r.Inputs {
		comps = append(comps, pricing.Component{ItemID: in.ItemID, Qty: float64(in.Quantity)})
	}
	outputUnits = 1
	if len(r.Outputs) > 0 && r.Outputs[0].Quantity > 0 {
		outputUnits = r.Outputs[0].Quantity
	}
	return comps, outputUnits
}

// suggestedFor returns the report's headline suggested price: the Nearby basis
// when it priced every component, else the Market-wide basis.
func suggestedFor(r *pricing.PriceReport) float64 {
	if r.Nearby.Complete() {
		return r.Nearby.Suggested
	}
	return r.Mkt.Suggested
}

// pickBestRecipe chooses the cheapest recipe by headline suggested price and,
// when a *different* recipe is cheaper on the market-wide basis, returns its
// index as altMkt so the caller can surface it; altMkt is -1 otherwise.
func pickBestRecipe(reports []*pricing.PriceReport) (best, altMkt int) {
	best, altMkt = 0, -1
	for i, r := range reports {
		if suggestedFor(r) < suggestedFor(reports[best]) {
			best = i
		}
	}
	bestMkt := best
	for i, r := range reports {
		if r.Mkt.Suggested < reports[bestMkt].Mkt.Suggested {
			bestMkt = i
		}
	}
	if bestMkt != best {
		altMkt = bestMkt
	}
	return best, altMkt
}
