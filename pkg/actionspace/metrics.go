package actionspace

import "sort"

// Stats holds branching factor metrics for an evaluated action space.
type Stats struct {
	TotalActions      int                      `json:"total_actions"`
	ValidActions      int                      `json:"valid_actions"`
	PrunedActions     int                      `json:"pruned_actions"`
	PrunedPercent     float64                  `json:"pruned_percent"`
	BranchingFactor   int                      `json:"branching_factor"`
	ByCategory        map[string]CategoryStats `json:"by_category"`
	TopPruningReasons []PruningReason          `json:"top_pruning_reasons"`
}

// CategoryStats holds per-category metrics.
type CategoryStats struct {
	Total           int `json:"total"`
	Valid           int `json:"valid"`
	Pruned          int `json:"pruned"`
	BranchingFactor int `json:"branching_factor"`
}

// PruningReason tracks how many actions a precondition pruned.
type PruningReason struct {
	Precondition string `json:"precondition"`
	Count        int    `json:"count"`
}

func computeStats(results []ActionResult) Stats {
	s := Stats{
		TotalActions: len(results),
		ByCategory:   make(map[string]CategoryStats),
	}

	pruneCount := make(map[string]int)

	for _, r := range results {
		cat := s.ByCategory[r.Action.Category]
		cat.Total++

		if r.Valid {
			s.ValidActions++
			s.BranchingFactor += r.BranchingCount
			cat.Valid++
			cat.BranchingFactor += r.BranchingCount
		} else {
			s.PrunedActions++
			cat.Pruned++
			for _, check := range r.FailedChecks {
				pruneCount[check]++
			}
		}

		s.ByCategory[r.Action.Category] = cat
	}

	if s.TotalActions > 0 {
		s.PrunedPercent = float64(s.PrunedActions) / float64(s.TotalActions) * 100
	}

	for name, count := range pruneCount {
		s.TopPruningReasons = append(s.TopPruningReasons, PruningReason{
			Precondition: name,
			Count:        count,
		})
	}
	sort.Slice(s.TopPruningReasons, func(i, j int) bool {
		return s.TopPruningReasons[i].Count > s.TopPruningReasons[j].Count
	})

	return s
}
