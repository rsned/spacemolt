package craftplan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// resolveRecipe finds the Recipe that id refers to, plus the other candidates
// that produce the same item. Resolution order:
//  1. recipe_id exact match (wins outright, and is never filtered -- naming a
//     recipe explicitly is always honoured).
//  2. item_id match against any recipe's primary output, ranked by:
//     a. SUPPLYABILITY -- how many distinct inputs the agent actually holds.
//     b. hand-craftable before facility_only (usable anywhere).
//     c. lowest skill ceiling, then recipe_id, for determinism.
//  3. No match → error with fuzzy suggestions.
//
// (a) is the whole point. Ranking by skill-then-alphabet picked recipes whose
// inputs cannot be obtained: on 2026-09-20 `plan fuel_cell 1000` chose
// biogas_fuel_synthesis, needing 500 crystallized_biogas (a wildlife drop we
// hold none of), over craft_fuel_cell with 38,097 liquid_hydrogen in storage --
// purely because every candidate had skill ceiling 0 and "biogas" sorts first.
// Scoring by held inputs generalises past that one case: it demotes wildlife
// drops, exotic intermediates and anything else we cannot feed, without having
// to enumerate what those are.
//
// "Ship Passive" recipes are excluded from the item-id path entirely. They are
// granted by a hull (onboard_alloy_synthesis comes with the alloy_synthesizer),
// are hand_craftable:false with no producing facility, and so can never be
// planned -- offering one is always a dead end. They remain reachable by
// explicit id.
//
// The returned alternatives are the rejected candidates in ranked order, so a
// caller can show what else exists rather than making the operator remember
// recipe ids.
func (e *Engine) resolveRecipe(id string, recs map[string]serverapi.Recipe, inv Inventory, includeFaction bool, quantity int) (serverapi.Recipe, []serverapi.Recipe, error) {
	if r, ok := recs[id]; ok {
		return r, nil, nil
	}

	// Item-id path: scan outputs.
	var matches []serverapi.Recipe
	for _, r := range recs {
		if isShipPassive(r) {
			continue
		}
		for _, out := range r.Outputs {
			if out.ItemID == id {
				matches = append(matches, r)
				break
			}
		}
	}
	if len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool {
			a, b := matches[i], matches[j]
			if ca, cb := supplyScore(a, inv, quantity), supplyScore(b, inv, quantity); ca != cb {
				return ca > cb
			}
			if a.FacilityOnly != b.FacilityOnly {
				return !a.FacilityOnly
			}
			if si, sj := skillCeiling(a), skillCeiling(b); si != sj {
				return si < sj
			}
			return a.ID < b.ID
		})
		return matches[0], matches[1:], nil
	}

	// No exact match anywhere — fall back to suggestions.
	ids := make([]string, 0, len(recs))
	for k := range recs {
		ids = append(ids, k)
	}
	suggest := suggestCloseMatches(id, ids, 5)
	if len(suggest) == 0 {
		return serverapi.Recipe{}, nil, fmt.Errorf("no recipe %q", id)
	}
	return serverapi.Recipe{}, nil, fmt.Errorf("no recipe %q. Did you mean: %s", id, strings.Join(suggest, ", "))
}

// isShipPassive reports whether r is granted by a hull rather than crafted.
// Such a recipe runs automatically on a ship that has it (the catalog marks
// them hand_craftable:false with an empty produced_by_facility_ids), so it can
// never be queued by an agent that does not fly that hull.
func isShipPassive(r serverapi.Recipe) bool {
	return strings.EqualFold(r.Category, "Ship Passive")
}

// supplyScore rates how much of r's input requirement for `quantity` output
// units the agent can actually cover, as the fraction held of the SCARCEST
// input, bucketed into percentage points so sorting is stable.
//
// Two things it deliberately does NOT do, both learned from getting them
// wrong on 2026-09-20:
//
// It does not score by whether we hold ANY of each input. That ranked
// drain_fuel_reserves top for 1,000 fuel cells -- it needs 2,500
// salvage_components, we held 12, and as its only input that counted as
// full coverage. Twelve of 2,500 is not a supply.
//
// It does not honour includeFaction. Faction stock is one
// withdraw_items --source=faction --target=self away (a single call, no
// cargo), so it is genuinely available for planning even when the caller
// asked to display only personal holdings. Ignoring it ranked
// craft_fuel_cell at zero while 38,097 liquid_hydrogen sat in the lockbox.
//
// The scarcest input decides, because a recipe is exactly as makeable as its
// worst-covered ingredient: holding 100k of one input does not help when
// another is missing outright.
func supplyScore(r serverapi.Recipe, inv Inventory, quantity int) int {
	if len(r.Inputs) == 0 {
		return 100
	}
	runs := runsFor(r, quantity)
	worst := 100
	for _, in := range r.Inputs {
		need := in.Quantity * runs
		if need <= 0 {
			continue
		}
		have := inv.total(in.ItemID, true)
		pct := have * 100 / need
		if pct > 100 {
			pct = 100
		}
		if pct < worst {
			worst = pct
		}
	}
	return worst
}

// facilityOnlyNoAlternative reports whether r is facility_only and no other
// recipe producing r's primary output can be hand-crafted (every recipe for
// that output is facility_only). In that case the item cannot be made at a
// Station Workshop at all — a facility is required. Returns false when r is
// not facility_only, has no outputs, or a non-facility_only alternative exists.
func facilityOnlyNoAlternative(r serverapi.Recipe, recs map[string]serverapi.Recipe) bool {
	if !r.FacilityOnly || len(r.Outputs) == 0 {
		return false
	}
	target := r.Outputs[0].ItemID
	for _, cand := range recs {
		if cand.FacilityOnly {
			continue
		}
		for _, out := range cand.Outputs {
			if out.ItemID == target {
				return false // a hand-craftable alternative exists
			}
		}
	}
	return true
}

// skillCeiling returns the highest required_skills value for r, or 0 if r
// has no skill prereqs.
func skillCeiling(r serverapi.Recipe) int {
	max := 0
	for _, lvl := range r.RequiredSkills {
		if lvl > max {
			max = lvl
		}
	}
	return max
}

// suggestCloseMatches ranks candidates by Levenshtein distance to needle and
// returns up to maxResults. Implementation is intentionally direct (no
// external dep); 528 candidates × needle length keeps total work trivial.
func suggestCloseMatches(needle string, candidates []string, maxResults int) []string {
	type scored struct {
		s string
		d int
	}
	out := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, scored{c, levenshtein(needle, c)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].d != out[j].d {
			return out[i].d < out[j].d
		}
		return out[i].s < out[j].s
	})
	// Drop matches with distance ≥ len(needle) — those are noise.
	cutoff := len(needle)
	if cutoff < 3 {
		cutoff = 3
	}
	res := make([]string, 0, maxResults)
	for _, sc := range out {
		if sc.d >= cutoff {
			break
		}
		res = append(res, sc.s)
		if len(res) == maxResults {
			break
		}
	}
	return res
}

// levenshtein returns the standard edit distance between a and b.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
