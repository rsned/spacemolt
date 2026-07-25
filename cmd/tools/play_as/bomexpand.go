package main

import (
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/pricing"
)

// bomMaxDepth bounds recipe-tree recursion. Real chains are a handful of levels
// deep; this exists so malformed data (a cycle the visiting set somehow misses,
// or a pathological chain) degrades to a truncated answer instead of a stack
// overflow in an interactive REPL.
const bomMaxDepth = 24

// qtyEpsilon is the tolerance for "these two routes cost the same". Route
// totals are sums of divisions, so exact float equality would make tie-breaks
// (facility preference, then recipe ID) fire unpredictably.
const qtyEpsilon = 1e-9

// expandedBOM is an item decomposed to base materials by walking the recipe
// tree, with EXACT fractional quantities per ONE output unit.
//
// This deliberately does not use the crafting DB's bill_of_materials table.
// That table's `quantity` column is INTEGER, so fractional per-unit values are
// ceiled at write time — ghost_rounds stores plasma_gas 2 (true 1.5, +33%) and
// titanium_ore 8 (true 7.5, +6.7%). The loss happens before we read it, so no
// consumer can undo it. It also records only the top-level recipe in
// recipe_path and does not propagate has_alternatives up the tree, so a caller
// cannot tell which sub-routes were chosen.
type expandedBOM struct {
	// Comps holds base materials per ONE output unit, sorted by ItemID.
	Comps []pricing.Component
	// Path lists the recipe IDs chosen, in the order first used.
	Path []string
	// UsesFacility is true when any chosen recipe is facility-only, so callers
	// can disclose that the costing assumes facility access.
	UsesFacility bool
	// Truncated is true when a cycle or the depth cap stopped the expansion,
	// making the result a floor rather than a complete decomposition.
	Truncated bool
}

// outputQtyFor returns how many units of itemID one run of r yields, or 0 when
// r does not produce it.
func outputQtyFor(r serverapi.Recipe, itemID string) int {
	for _, o := range r.Outputs {
		if o.ItemID == itemID {
			return o.Quantity
		}
	}
	return 0
}

// expandState carries the per-item accumulation while a candidate route is
// being costed.
type expandState struct {
	qty       map[string]float64
	path      []string
	facility  bool
	truncated bool
}

// totalQty is the route-selection metric: the sum of all base-material
// quantities needed per output unit. Lower wins, which picks steel_plate's 5:2
// route (2.5 ore/plate) over its 8:1 route (8 ore/plate) and copper_wiring's
// 2 ore/unit facility route over the 8 ore/unit hand route.
//
// This is a structural metric, not a priced one: it is deterministic, needs no
// market data (which is frequently missing or an outlier — see the
// titanium_ore 10000 ask), and matches how the operator reasons about "the
// optimal recipe form".
func (s expandState) totalQty() float64 {
	var sum float64
	for _, v := range s.qty {
		sum += v
	}
	return sum
}

// expandToBase decomposes itemID into base materials using exact fractional
// arithmetic, choosing the cheapest route at every branch. An item no recipe
// produces is a base material and expands to itself at quantity 1.
func expandToBase(itemID string, recipes map[string]serverapi.Recipe) expandedBOM {
	st := expandItem(itemID, recipes, map[string]bool{}, 0)

	comps := make([]pricing.Component, 0, len(st.qty))
	for id, q := range st.qty {
		comps = append(comps, pricing.Component{ItemID: id, Qty: q})
	}
	sort.Slice(comps, func(i, j int) bool { return comps[i].ItemID < comps[j].ItemID })

	return expandedBOM{
		Comps:        comps,
		Path:         st.path,
		UsesFacility: st.facility,
		Truncated:    st.truncated,
	}
}

// expandItem is expandToBase's recursive core. visiting holds the items on the
// current path so a recipe cycle terminates as a base material rather than
// recursing forever.
func expandItem(itemID string, recipes map[string]serverapi.Recipe, visiting map[string]bool, depth int) expandState {
	base := func(truncated bool) expandState {
		return expandState{qty: map[string]float64{itemID: 1}, truncated: truncated}
	}
	if visiting[itemID] || depth >= bomMaxDepth {
		return base(true)
	}
	candidates := resolveRecipesForOutput(recipes, itemID)
	if len(candidates) == 0 {
		return base(false)
	}

	visiting[itemID] = true
	defer delete(visiting, itemID)

	var best *expandState
	var bestRecipe serverapi.Recipe
	for _, r := range candidates {
		outQty := outputQtyFor(r, itemID)
		// A zero-output or input-less recipe cannot be costed; skipping it
		// keeps a malformed row from making the item look free.
		if outQty <= 0 || len(r.Inputs) == 0 {
			continue
		}
		cur := expandState{qty: map[string]float64{}, path: []string{r.ID}, facility: r.FacilityOnly}
		for _, in := range r.Inputs {
			sub := expandItem(in.ItemID, recipes, visiting, depth+1)
			scale := float64(in.Quantity) / float64(outQty)
			for id, q := range sub.qty {
				cur.qty[id] += q * scale
			}
			cur.path = append(cur.path, sub.path...)
			cur.facility = cur.facility || sub.facility
			cur.truncated = cur.truncated || sub.truncated
		}
		if best == nil || cheaperRoute(cur, *best, r, bestRecipe) {
			c := cur
			best = &c
			bestRecipe = r
		}
	}
	if best == nil {
		return base(false)
	}
	best.path = dedupeStrings(best.path)
	return *best
}

// cheaperRoute reports whether route a beats route b. Primary key is total base
// quantity; ties go to the facility recipe (the operator's accessible optimum),
// then to the lower recipe ID so selection is deterministic.
func cheaperRoute(a, b expandState, ra, rb serverapi.Recipe) bool {
	da, db := a.totalQty(), b.totalQty()
	if da < db-qtyEpsilon {
		return true
	}
	if da > db+qtyEpsilon {
		return false
	}
	if ra.FacilityOnly != rb.FacilityOnly {
		return ra.FacilityOnly
	}
	return ra.ID < rb.ID
}

// dedupeStrings keeps the first occurrence of each entry, preserving order. A
// recipe reached through two branches should appear once in the path.
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// bomRouteNote describes the route an expansion took, so the BOM block's
// numbers are auditable: which recipes were chosen, whether the costing assumes
// facility access (the cheapest route is often facility-only — copper_wiring is
// 2 ore/unit at a facility vs 8 by hand), and whether a cycle truncated it.
func bomRouteNote(e expandedBOM) string {
	if len(e.Path) == 0 {
		return ""
	}
	note := "route: " + strings.Join(e.Path, " → ")
	if e.UsesFacility {
		note += "\n  ⚠ assumes facility access — the cheapest route uses facility-only recipes; hand-crafting costs more"
	}
	if e.Truncated {
		note += "\n  ⚠ expansion truncated (recipe cycle or depth cap) — this cost is a floor"
	}
	return note
}
