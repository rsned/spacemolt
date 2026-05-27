package craftplan

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

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
		// Facility-only recipes only run at a crafting facility, not a regular
		// station. They typically have no inputs (the facility provides them
		// passively) so they show ∞ can_make and flood the top of the table.
		// The server signals this two ways: a facility_only=true field (not
		// always emitted), AND the human-facing Category string "Facility Only".
		// Match either. Hide by default; opts.IncludeFacilityOnly opts in.
		// opts.OneRecipe overrides — if the operator named this specific
		// recipe, show it regardless.
		if (r.FacilityOnly || strings.EqualFold(r.Category, "Facility Only")) &&
			!opts.IncludeFacilityOnly && opts.OneRecipe == "" {
			continue
		}
		if r.Hidden && !opts.IncludeHidden && opts.OneRecipe == "" {
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

// Plan computes the gap between the agent's current inventory and the
// inputs required to craft opts.ID at opts.Quantity. Reachable mode (in
// reachable.go) re-uses these gates and replaces the input walk with a
// BOM walk.
func (e *Engine) Plan(ctx context.Context, opts PlanOpts) (*PlanResult, error) {
	if opts.Quantity == 0 {
		opts.Quantity = 1
	}
	if opts.Quantity < 1 {
		return nil, fmt.Errorf("qty must be a positive integer (got: %d)", opts.Quantity)
	}

	recs, err := e.recipes(ctx, opts.Refresh)
	if err != nil {
		return nil, err
	}
	r, err := e.resolveRecipe(opts.ID, recs)
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

	res := &PlanResult{
		Recipe:         r,
		Quantity:       opts.Quantity,
		StationID:      stationID,
		BlockedSkill:   skillGaps(r, skills),
		BlockedIllegal: illegal[r.ID],
	}

	if opts.Reachable {
		if err := e.planReachable(ctx, res, r, inv, opts); err != nil {
			return nil, err
		}
	} else {
		res.Inputs = planDirect(r, opts.Quantity, inv, opts.IncludeFaction)
	}

	res.Ready = len(res.BlockedSkill) == 0 && !res.BlockedIllegal
	for _, row := range res.Inputs {
		if row.Short > 0 {
			res.Ready = false
			break
		}
	}
	return res, nil
}

// skillGaps returns the per-skill shortfall preventing the agent from
// crafting r. Empty map = no skill block.
func skillGaps(r serverapi.Recipe, have map[string]int) map[string]int {
	gaps := map[string]int{}
	for skillID, need := range r.RequiredSkills {
		if got := have[skillID]; got < need {
			gaps[skillID] = need - got
		}
	}
	return gaps
}

// planDirect builds the per-direct-input gap rows.
func planDirect(r serverapi.Recipe, qty int, inv Inventory, includeFaction bool) []PlanInputRow {
	rows := make([]PlanInputRow, 0, len(r.Inputs))
	for _, in := range r.Inputs {
		need := in.Quantity * qty
		row := PlanInputRow{
			ItemID:      in.ItemID,
			Need:        need,
			HaveCargo:   inv.Cargo[in.ItemID],
			HaveStorage: inv.Storage[in.ItemID],
		}
		if includeFaction {
			row.HaveFaction = inv.Faction[in.ItemID]
		}
		total := row.HaveCargo + row.HaveStorage + row.HaveFaction
		if need > total {
			row.Short = need - total
		}
		rows = append(rows, row)
	}
	return rows
}


