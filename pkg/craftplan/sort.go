package craftplan

import "sort"

// sortRows orders rows in place per the requested SortMode. All sorts are
// stable, and unknown / zero modes fall back to the default:
// can_make DESC, depth ASC, recipe_id ASC.
func sortRows(rows []CraftableRow, mode SortMode) {
	switch mode {
	case SortCanMakeAsc:
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if a.CanMake != b.CanMake {
				return a.CanMake < b.CanMake
			}
			return a.Recipe.ID < b.Recipe.ID
		})
	case SortName:
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if a.Recipe.Name != b.Recipe.Name {
				return a.Recipe.Name < b.Recipe.Name
			}
			return a.CanMake > b.CanMake
		})
	case SortCategory:
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if a.Recipe.Category != b.Recipe.Category {
				return a.Recipe.Category < b.Recipe.Category
			}
			return a.CanMake > b.CanMake
		})
	case SortRecipeID:
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i].Recipe.ID < rows[j].Recipe.ID
		})
	default:
		// Default: can_make DESC, depth ASC (direct first), recipe_id ASC.
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if a.CanMake != b.CanMake {
				return a.CanMake > b.CanMake
			}
			if a.Depth != b.Depth {
				return a.Depth < b.Depth
			}
			return a.Recipe.ID < b.Recipe.ID
		})
	}
}
