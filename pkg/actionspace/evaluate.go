package actionspace

// Evaluate computes the full action space for a given game context.
func Evaluate(gc GameContext) ActionSpace {
	results := make([]ActionResult, 0, len(AllActions))

	for _, action := range AllActions {
		result := ActionResult{Action: action}

		allPassed := true
		for _, pre := range action.Preconditions {
			if !pre.Check(&gc) {
				result.FailedChecks = append(result.FailedChecks, pre.Name)
				allPassed = false
			}
		}

		result.Valid = allPassed
		if allPassed && action.Targets != nil {
			result.Targets = action.Targets(&gc)
			if len(result.Targets) == 0 {
				// Action requires targets but none available — invalid.
				result.Valid = false
				result.FailedChecks = append(result.FailedChecks, "no_valid_targets")
			} else {
				result.BranchingCount = len(result.Targets)
			}
		} else if allPassed {
			result.BranchingCount = 1
		}

		results = append(results, result)
	}

	as := ActionSpace{Context: gc, Actions: results}
	as.Stats = computeStats(results)
	return as
}
