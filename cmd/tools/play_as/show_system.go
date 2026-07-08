package main

import (
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// suggestSystems returns up to three system ids that plausibly match an unknown
// query, for a "did you mean" hint. Candidates are ranked by substring match
// first (id or name, case-insensitive), then by Levenshtein distance ≤ 2. It is
// pure — no I/O — so it is directly testable.
func suggestSystems(query string, systems []knowledge.System) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	type cand struct {
		id    string
		rank  int // 0 = substring, 1 = fuzzy
		dist  int
	}
	var cands []cand
	for _, s := range systems {
		id := strings.ToLower(s.ID)
		name := strings.ToLower(s.Name)
		switch {
		case strings.Contains(id, q) || (name != "" && strings.Contains(name, q)):
			cands = append(cands, cand{s.ID, 0, 0})
		default:
			d := levenshtein(q, id)
			if name != "" {
				if dn := levenshtein(q, name); dn < d {
					d = dn
				}
			}
			if d <= 2 {
				cands = append(cands, cand{s.ID, 1, d})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].rank != cands[j].rank {
			return cands[i].rank < cands[j].rank
		}
		return cands[i].dist < cands[j].dist
	})
	var out []string
	for _, c := range cands {
		out = append(out, c.id)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// levenshtein returns the edit distance between two strings.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev = cur
	}
	return prev[len(rb)]
}
