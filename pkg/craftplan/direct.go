package craftplan

import (
	"context"
	"math"
	"sort"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Craftable returns the list of recipes the agent can build right now (or,
// with opts.Reachable, can build via intermediate crafts). Sort:
// can_make DESC, depth ASC, recipe_id ASC.
func (e *Engine) Craftable(ctx context.Context, opts CraftableOpts) ([]CraftableRow, error) {
	recs, err := e.recipes(ctx, opts.Refresh)
	if err != nil {
		return nil, err
	}
	inv, err := e.src.Inventory(ctx, opts.IncludeFaction)
	if err != nil {
		return nil, err
	}
	skills, err := e.src.Skills(ctx)
	if err != nil {
		return nil, err
	}
	stationID, err := e.src.CurrentStationID(ctx)
	if err != nil {
		return nil, err
	}
	illegal, err := e.src.IllegalAt(ctx, stationID)
	if err != nil {
		return nil, err
	}

	// First pass: every recipe that passes skill + legality gates becomes a
	// candidate. The direct-material gate runs next; reachable mode (handled
	// in reachable.go) runs as a separate pass on the same candidate set.
	candidates := make([]serverapi.Recipe, 0, len(recs))
	for _, r := range recs {
		if !meetsSkills(r, skills) {
			continue
		}
		if illegal[r.ID] {
			continue
		}
		if opts.OneRecipe != "" && r.ID != opts.OneRecipe {
			continue
		}
		if !rowMatchesFilter(r, opts) {
			continue
		}
		candidates = append(candidates, r)
	}

	var rows []CraftableRow
	if opts.Reachable {
		rows, err = e.craftableReachable(ctx, candidates, inv, opts.IncludeFaction)
		if err != nil {
			return nil, err
		}
	} else {
		rows = craftableDirect(candidates, inv, opts.IncludeFaction)
	}

	// Sort: can_make DESC, depth ASC (direct first), recipe_id ASC.
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

	// --max cap. opts.OneRecipe overrides Max because the caller asked for
	// a specific recipe.
	max := opts.Max
	if max == 0 {
		max = defaultMax
	}
	if opts.OneRecipe == "" && len(rows) > max {
		rows = rows[:max]
	}
	return rows, nil
}

func meetsSkills(r serverapi.Recipe, have map[string]int) bool {
	for skillID, lvl := range r.RequiredSkills {
		if have[skillID] < lvl {
			return false
		}
	}
	return true
}

func rowMatchesFilter(r serverapi.Recipe, opts CraftableOpts) bool {
	if !stringMatch(r.Category, opts.CategoryFilter) {
		return false
	}
	if opts.SearchFilter == "" {
		return true
	}
	if stringMatch(r.Name, opts.SearchFilter) {
		return true
	}
	for _, o := range r.Outputs {
		if stringMatch(o.ItemID, opts.SearchFilter) {
			return true
		}
	}
	return false
}

// craftableDirect computes can_make per recipe using direct inputs only.
func craftableDirect(candidates []serverapi.Recipe, inv Inventory, includeFaction bool) []CraftableRow {
	out := make([]CraftableRow, 0, len(candidates))
	for _, r := range candidates {
		cm := canMakeDirect(r, inv, includeFaction)
		if cm < 1 {
			continue
		}
		out = append(out, makeRow(r, cm, 1))
	}
	return out
}

func canMakeDirect(r serverapi.Recipe, inv Inventory, includeFaction bool) int {
	if len(r.Inputs) == 0 {
		return math.MaxInt
	}
	best := math.MaxInt
	for _, in := range r.Inputs {
		if in.Quantity <= 0 {
			continue
		}
		have := inv.total(in.ItemID, includeFaction)
		max := have / in.Quantity
		if max < best {
			best = max
		}
	}
	return best
}

func makeRow(r serverapi.Recipe, canMake, depth int) CraftableRow {
	row := CraftableRow{
		Recipe:  r,
		CanMake: canMake,
		Depth:   depth,
	}
	if len(r.Outputs) > 0 {
		row.OutputItemID = r.Outputs[0].ItemID
		row.OutputQuantity = r.Outputs[0].Quantity
	}
	return row
}

// craftableReachable is implemented in reachable.go (Task 6). Until then a
// stub keeps the build green; tests that don't pass opts.Reachable never
// reach it.
func (e *Engine) craftableReachable(ctx context.Context, candidates []serverapi.Recipe, inv Inventory, includeFaction bool) ([]CraftableRow, error) {
	return nil, nil
}
