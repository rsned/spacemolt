package craftbrain

import (
	"fmt"
	"sort"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// producerIndex maps item_id -> every recipe that outputs it, sorted by recipe
// ID so planning is deterministic. Items with no entry are raw.
func producerIndex(recipes map[string]knowledge.RecipeDef) map[string][]knowledge.RecipeDef {
	prod := map[string][]knowledge.RecipeDef{}
	for _, r := range recipes {
		for _, out := range r.Outputs {
			prod[out.ItemID] = append(prod[out.ItemID], r)
		}
	}
	for item := range prod {
		rs := prod[item]
		sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
		prod[item] = rs
	}
	return prod
}

// childrenOf returns every input item of every candidate recipe for item, so
// the union graph is independent of which recipe we eventually choose.
func childrenOf(item string, prod map[string][]knowledge.RecipeDef) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range prod[item] {
		for _, in := range r.Inputs {
			if in.ItemID == item || seen[in.ItemID] {
				continue // self-loop contributes nothing
			}
			seen[in.ItemID] = true
			out = append(out, in.ItemID)
		}
	}
	sort.Strings(out)
	return out
}

// topoOrder returns the items reachable from target, ordered so that every
// consumer of an item precedes that item. Demand can therefore be fully
// aggregated at an item before its runs are rounded.
//
// Cycles (refine A->B, recycle B->A) are broken by dropping the edge that
// closes them; each drop is reported as "parent->child".
func topoOrder(target string, prod map[string][]knowledge.RecipeDef) (order []string, dropped []string) {
	// Collect the reachable union subgraph: edges parent -> input.
	edges := map[string][]string{}
	indeg := map[string]int{target: 0}
	queue := []string{target}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if _, done := edges[item]; done {
			continue
		}
		kids := childrenOf(item, prod)
		edges[item] = kids
		for _, k := range kids {
			if _, seen := indeg[k]; !seen {
				indeg[k] = 0
				queue = append(queue, k)
			}
			indeg[k]++
		}
	}

	// Kahn: repeatedly emit an item whose consumers are all emitted.
	var ready []string
	for item, d := range indeg {
		if d == 0 {
			ready = append(ready, item)
		}
	}
	sort.Strings(ready)

	emitted := map[string]bool{}
	for len(emitted) < len(indeg) {
		if len(ready) == 0 {
			// Residual cycle. Break at the lowest-indegree unemitted item,
			// dropping its remaining incoming edges.
			best, bestDeg := "", 1<<30
			for item, d := range indeg {
				if emitted[item] {
					continue
				}
				if d < bestDeg || (d == bestDeg && item < best) {
					best, bestDeg = item, d
				}
			}
			if best == "" {
				break
			}
			for parent, kids := range edges {
				if emitted[parent] {
					continue
				}
				for _, k := range kids {
					if k == best {
						dropped = append(dropped, fmt.Sprintf("%s->%s", parent, best))
					}
				}
			}
			indeg[best] = 0
			ready = append(ready, best)
			continue
		}

		sort.Strings(ready)
		item := ready[0]
		ready = ready[1:]
		if emitted[item] {
			continue
		}
		emitted[item] = true
		order = append(order, item)
		for _, k := range edges[item] {
			if emitted[k] {
				continue
			}
			indeg[k]--
			if indeg[k] <= 0 {
				ready = append(ready, k)
			}
		}
	}
	sort.Strings(dropped)
	return order, dropped
}
