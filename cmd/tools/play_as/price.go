package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
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
// and returns the output-units-per-run for itemID, used to normalize the
// roll-up to a per-unit cost. It selects the output matching itemID (a recipe
// may yield several distinct outputs); falls back to the first output, then 1.
func recipeComponents(itemID string, r serverapi.Recipe) (comps []pricing.Component, outputUnits int) {
	for _, in := range r.Inputs {
		comps = append(comps, pricing.Component{ItemID: in.ItemID, Qty: float64(in.Quantity)})
	}
	outputUnits = 1
	for _, o := range r.Outputs {
		if o.ItemID == itemID && o.Quantity > 0 {
			return comps, o.Quantity
		}
	}
	if len(r.Outputs) > 0 && r.Outputs[0].Quantity > 0 {
		outputUnits = r.Outputs[0].Quantity
	}
	return comps, outputUnits
}

// suggestedFor returns the report's headline suggested price: the Nearby basis
// when it priced every component, else the Market-wide basis when IT is
// complete. A report with no fully-priced basis returns +Inf so it never wins
// a cheapest-recipe comparison on the strength of missing (zero-cost) data.
func suggestedFor(r *pricing.PriceReport) float64 {
	switch {
	case r.Nearby.Complete():
		return r.Nearby.Suggested
	case r.Mkt.Complete():
		return r.Mkt.Suggested
	default:
		return math.Inf(1)
	}
}

// pickBestRecipe chooses the cheapest recipe by headline suggested price
// (suggestedFor treats an incomplete report as non-comparable). When a
// *different* recipe with a fully-priced market-wide basis is cheaper on that
// basis than the chosen best (which must itself have a complete market basis),
// its index is returned as altMkt so the caller can surface it; altMkt is -1
// otherwise.
func pickBestRecipe(reports []*pricing.PriceReport) (best, altMkt int) {
	best, altMkt = 0, -1
	for i, r := range reports {
		if suggestedFor(r) < suggestedFor(reports[best]) {
			best = i
		}
	}
	if !reports[best].Mkt.Complete() {
		return best, -1
	}
	bestMkt := best
	for i, r := range reports {
		if r.Mkt.Complete() && r.Mkt.Suggested < reports[bestMkt].Mkt.Suggested {
			bestMkt = i
		}
	}
	if bestMkt != best {
		altMkt = bestMkt
	}
	return best, altMkt
}

// modeReport pairs a decomposition label ("RECIPE" / "BOM (ore)") with its
// pricing result for rendering.
type modeReport struct {
	Label string
	R     *pricing.PriceReport
	// Note is rendered immediately below this block's coverage line. The BOM
	// basis uses it to disclose the route it chose and whether that route
	// assumes facility access — both invisible before 2026-07-25.
	Note string
}

// money renders a price with two decimals, or an em dash when absent.
func money(v float64, found bool) string {
	if !found {
		return "—"
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// renderPriceText renders the human-readable price report(s). The CURRENT
// MARKET block is item-level and identical across modes, so it is printed once
// from the first mode's report.
func renderPriceText(itemID, fromSystem string, hops int, marginPct float64, modes []modeReport, altNote string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "price %s   (margin %.0f%%, nearby = local + ≤%d hops", itemID, marginPct, hops)
	if fromSystem != "" {
		fmt.Fprintf(&b, " from %s", fromSystem)
	}
	b.WriteString(")\n")

	for _, m := range modes {
		r := m.R
		fmt.Fprintf(&b, "\n%s", m.Label)
		if r.RecipeName != "" {
			fmt.Fprintf(&b, "  %s", r.RecipeName)
		}
		if r.OutputUnits > 1 {
			fmt.Fprintf(&b, " → %d units/run", r.OutputUnits)
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s %10s\n", "COMPONENT", "QTY", "NEARBY", "MKT-BEST", "MKT-AVG")
		// Track which components lack a price on each basis so coverage can be
		// reported (and the SUGGESTED total flagged as partial) per basis.
		var nearbyMissing, mktMissing []string
		for _, c := range r.Components {
			fmt.Fprintf(&b, "  %-20s %8s %10s %10s %10s\n", c.ItemID,
				strconv.FormatFloat(c.Qty, 'f', -1, 64),
				money(c.NearbyUnit, c.NearbyFound), money(c.MktBestUnit, c.MktBestFound), money(c.MktUnit, c.MktFound))
			if !c.NearbyFound {
				nearbyMissing = append(nearbyMissing, c.ItemID)
			}
			if !c.MktFound {
				mktMissing = append(mktMissing, c.ItemID)
			}
		}
		costLabel := "build cost/run"
		if r.OutputUnits <= 1 {
			costLabel = "build cost"
		}
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s %10s\n", "---- "+costLabel, "", money(r.Nearby.BuildCost, r.Nearby.Covered > 0), money(r.MktBest.BuildCost, r.MktBest.Covered > 0), money(r.Mkt.BuildCost, r.Mkt.Covered > 0))
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s %10s\n", "---- per unit", "", money(r.Nearby.PerUnit, r.Nearby.Covered > 0), money(r.MktBest.PerUnit, r.MktBest.Covered > 0), money(r.Mkt.PerUnit, r.Mkt.Covered > 0))
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s %10s\n", fmt.Sprintf("---- + %.0f%% margin", marginPct), "", money(r.Nearby.Margin, r.Nearby.Covered > 0), money(r.MktBest.Margin, r.MktBest.Covered > 0), money(r.Mkt.Margin, r.Mkt.Covered > 0))
		// SUGGESTED renders whenever a basis priced at least one component,
		// consistent with the cost rows above. A basis that priced some but not
		// all components yields a partial floor, flagged below.
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s %10s\n", "= SUGGESTED", "", money(r.Nearby.Suggested, r.Nearby.Covered > 0), money(r.MktBest.Suggested, r.MktBest.Covered > 0), money(r.Mkt.Suggested, r.Mkt.Covered > 0))
		fmt.Fprintf(&b, "  coverage: nearby %d/%d", r.Nearby.Covered, r.Nearby.Total)
		if len(nearbyMissing) > 0 {
			fmt.Fprintf(&b, " (missing: %s)", strings.Join(nearbyMissing, ", "))
		}
		fmt.Fprintf(&b, "; mkt-avg %d/%d", r.Mkt.Covered, r.Mkt.Total)
		if len(mktMissing) > 0 {
			fmt.Fprintf(&b, " (missing: %s)", strings.Join(mktMissing, ", "))
		}
		b.WriteString("\n")
		if (r.Nearby.Covered > 0 && !r.Nearby.Complete()) || (r.Mkt.Covered > 0 && !r.Mkt.Complete()) {
			b.WriteString("  ⚠ a partial SUGGESTED (coverage < full) excludes unpriced components — the true price is higher\n")
		}
		if m.Note != "" {
			fmt.Fprintf(&b, "  %s\n", m.Note)
		}
	}

	if len(modes) > 0 {
		r := modes[0].R
		b.WriteString("\nCURRENT MARKET  " + itemID + "\n")
		fmt.Fprintf(&b, "  nearby ask %s   mkt-best ask %s   mkt-avg ask %s   best bid %s\n",
			money(r.CurAskNearby, r.HasAskNearby), money(r.CurAskMktBest, r.HasAskMktBest),
			money(r.CurAskMkt, r.HasAskMkt), money(r.CurBid, r.HasBid))
		if r.Class != "" {
			fmt.Fprintf(&b, "  → %s\n", r.Class)
		}
	}
	if altNote != "" {
		b.WriteString("\n" + altNote + "\n")
	}
	return b.String()
}

// handlePrice implements: price <item_id> [--margin=20] [--hops=2] [--mode=both|recipe|bom] [--json]
func handlePrice(client game.GameClient, ctx context.Context, parts []string, craftingDB *sql.DB, format outputFormat) error {
	_ = format
	if globalMarketCollector == nil {
		return fmt.Errorf("price: market DB not available (run with --market-db-path)")
	}
	args := parts[1:]
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return fmt.Errorf("usage: price <item_id> [--margin=20] [--hops=2] [--mode=both|recipe|bom] [--json]")
	}
	itemID := args[0]
	flags, err := parseFlagArgs(args[1:], "margin", "hops", "mode", "json")
	if err != nil {
		return err
	}
	margin := priceFlagFloat(flags["margin"], 20)
	hops := 2
	if n, ok := flagInt(flags["hops"]); ok {
		hops = n
	}
	mode := "both"
	if s, ok := flagString(flags["mode"]); ok && s != "" {
		mode = s
	}
	switch mode {
	case "both", "recipe", "bom":
	default:
		return fmt.Errorf("price: --mode must be one of both|recipe|bom, got %q", mode)
	}
	asJSON := flagBool(flags["json"])

	fromSystem := ""
	if st := client.GetState(); st != nil {
		fromSystem = st.System.ID
	}

	src := newPlayAsSource(client, craftingDB)
	recipes, err := src.Recipes(ctx, false)
	if err != nil {
		return fmt.Errorf("price: load recipes: %w", err)
	}
	candidates := resolveRecipesForOutput(recipes, itemID)

	var modes []modeReport
	var altNote string

	if mode == "both" || mode == "recipe" {
		if len(candidates) == 0 {
			fmt.Printf("price %s: not craftable — no recipe produces it.\n", itemID)
		} else {
			reports := make([]*pricing.PriceReport, 0, len(candidates))
			for _, r := range candidates {
				comps, units := recipeComponents(itemID, r)
				rep, rerr := pricing.Report(ctx, globalMarketCollector, globalKB, fromSystem, hops, itemID, r.ID, units, comps, margin)
				if rerr != nil {
					return rerr
				}
				reports = append(reports, rep)
			}
			best, alt := pickBestRecipe(reports)
			modes = append(modes, modeReport{Label: "RECIPE", R: reports[best]})
			if alt >= 0 {
				altNote = fmt.Sprintf("note: on the market-wide basis, recipe %s is cheaper (%s vs %s).",
					reports[alt].RecipeName, money(reports[alt].Mkt.Suggested, true), money(reports[best].Mkt.Suggested, true))
			}
		}
	}

	if mode == "both" || mode == "bom" {
		// Expand the recipe tree directly rather than reading the crafting DB's
		// bill_of_materials: that table's quantity column is INTEGER, so it
		// ceils fractional per-unit values at write time (ghost_rounds stored
		// plasma_gas 2 for a true 1.5, titanium_ore 8 for a true 7.5) and
		// records neither the sub-recipes chosen nor that a choice existed.
		exp := expandToBase(itemID, recipes)
		if len(exp.Path) == 0 {
			fmt.Printf("price %s: base material — no recipe produces it, so it has no bill of materials.\n", itemID)
		} else {
			// exp.Comps is already exact per ONE output unit -> outputUnits = 1.
			rep, rerr := pricing.Report(ctx, globalMarketCollector, globalKB, fromSystem, hops, itemID, "", 1, exp.Comps, margin)
			if rerr != nil {
				return rerr
			}
			modes = append(modes, modeReport{Label: "BOM (ore)", R: rep, Note: bomRouteNote(exp)})
		}
	}

	if len(modes) == 0 {
		return nil // messages already printed
	}
	if asJSON {
		payload := make([]*pricing.PriceReport, 0, len(modes))
		for _, m := range modes {
			payload = append(payload, m.R)
		}
		out, jerr := json.MarshalIndent(payload, "", "  ")
		if jerr != nil {
			return jerr
		}
		fmt.Println(string(out))
		return nil
	}
	fmt.Print(renderPriceText(itemID, fromSystem, hops, margin, modes, altNote))
	return nil
}

// priceFlagFloat reads a parseFlagArgs value as a float with a default.
// parseFlagArgs stores whole numbers as int and everything else as string
// (so `--margin=20` arrives as int 20 and `--margin=17.5` as "17.5"). The
// existing flagInt/flagString/flagBool helpers cover the other flag types.
func priceFlagFloat(v any, def float64) float64 {
	switch tv := v.(type) {
	case int:
		return float64(tv)
	case string:
		if f, err := strconv.ParseFloat(tv, 64); err == nil {
			return f
		}
	}
	return def
}
