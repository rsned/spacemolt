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
func (e *Engine) resolveRecipe(id string, recs map[string]serverapi.Recipe, inv Inventory, includeFaction bool) (serverapi.Recipe, []serverapi.Recipe, error) {
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
			if ca, cb := suppliedInputs(a, inv, includeFaction), suppliedInputs(b, inv, includeFaction); ca != cb {
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

// suppliedInputs counts how many of r's distinct inputs the agent holds at
// least one of. It deliberately counts KINDS rather than whether the full
// quantity is present: a recipe we can partly feed is worth planning (the
// shortfall is what the plan is for), while one whose inputs we hold none of
// is almost always the wrong branch -- a wildlife drop, an exotic
// intermediate, or a chain we have never started.
func suppliedInputs(r serverapi.Recipe, inv Inventory, includeFaction bool) int {
	n := 0
	for _, in := range r.Inputs {
		if inv.total(in.ItemID, includeFaction) > 0 {
			n++
		}
	}
	return n
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
