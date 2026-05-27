package craftplan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// resolveRecipe finds the Recipe that opts.ID refers to. Resolution order:
//  1. recipe_id exact match (wins on tie).
//  2. item_id exact match against any recipe's primary output. If multiple
//     recipes output the item, pick the one with the lowest skill ceiling
//     (max of required_skills values); ties broken alphabetically by
//     recipe_id.
//  3. No match → error with fuzzy suggestions.
//
// Replaces the stub in direct.go once this file is in the package.
func (e *Engine) resolveRecipe(id string, recs map[string]serverapi.Recipe) (serverapi.Recipe, error) {
	if r, ok := recs[id]; ok {
		return r, nil
	}

	// Item-id path: scan outputs.
	var matches []serverapi.Recipe
	for _, r := range recs {
		for _, out := range r.Outputs {
			if out.ItemID == id {
				matches = append(matches, r)
				break
			}
		}
	}
	if len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool {
			si, sj := skillCeiling(matches[i]), skillCeiling(matches[j])
			if si != sj {
				return si < sj
			}
			return matches[i].ID < matches[j].ID
		})
		return matches[0], nil
	}

	// No exact match anywhere — fall back to suggestions.
	ids := make([]string, 0, len(recs))
	for k := range recs {
		ids = append(ids, k)
	}
	suggest := suggestCloseMatches(id, ids, 5)
	if len(suggest) == 0 {
		return serverapi.Recipe{}, fmt.Errorf("no recipe %q", id)
	}
	return serverapi.Recipe{}, fmt.Errorf("no recipe %q. Did you mean: %s", id, strings.Join(suggest, ", "))
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
