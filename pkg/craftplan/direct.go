package craftplan

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Craftable returns the list of recipes the agent can build right now (or,
// with opts.Reachable, can build via intermediate crafts). Sort:
// can_make DESC, depth ASC, recipe_id ASC. The second return value is the
// pre-truncation total so callers can render "showing N / TOTAL" honestly.
func (e *Engine) Craftable(ctx context.Context, opts CraftableOpts) ([]CraftableRow, int, error) {
	recs, err := e.recipes(ctx, opts.Refresh)
	if err != nil {
		return nil, 0, err
	}
	inv, err := e.src.Inventory(ctx, opts.IncludeFaction)
	if err != nil {
		return nil, 0, err
	}
	skills, err := e.src.Skills(ctx)
	if err != nil {
		return nil, 0, err
	}
	stationID, err := e.src.CurrentStationID(ctx)
	if err != nil {
		return nil, 0, err
	}
	illegal, err := e.src.IllegalAt(ctx, stationID)
	if err != nil {
		return nil, 0, err
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
		// "Ship Passive" recipes run automatically on ships that have the
		// capability built in — they can't be crafted manually (the server
		// rejects with `invalid_recipe`). Hide by default so the table
		// (and the plan tool's "ready to craft" prompt) doesn't surface a
		// recipe the operator can't actually invoke. OneRecipe still
		// overrides so an explicit `plan onboard_X` shows the breakdown.
		if strings.EqualFold(r.Category, "Ship Passive") &&
			!opts.IncludeShipPassive && opts.OneRecipe == "" {
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
			return nil, 0, err
		}
	} else {
		rows = craftableDirect(candidates, inv, opts.IncludeFaction)
	}

	sortRows(rows, opts.SortBy)

	total := len(rows)

	// --max cap. opts.OneRecipe overrides Max because the caller asked for
	// a specific recipe.
	max := opts.Max
	if max == 0 {
		max = defaultMax
	}
	if opts.OneRecipe == "" && len(rows) > max {
		rows = rows[:max]
	}
	return rows, total, nil
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

	// The requested quantity is a count of output UNITS. A single crafting
	// run yields outputPerRun(r) of them, so the materials scale with the
	// number of RUNS, not the unit count. (Server craft restructure, 2026-07:
	// recipes now yield N>1 units per run — e.g. barrel_assembly yields 2,
	// h2_fuel_combustor yields 100.)
	runs := runsFor(r, opts.Quantity)

	res := &PlanResult{
		Recipe:         r,
		Quantity:       opts.Quantity,
		Runs:           runs,
		StationID:      stationID,
		BlockedSkill:   skillGaps(r, skills),
		BlockedIllegal:    illegal[r.ID],
		BlockedPassive:    strings.EqualFold(r.Category, "Ship Passive"),
		FacilityOnlyNoAlt: facilityOnlyNoAlternative(r, recs),
	}

	if opts.Reachable {
		if err := e.planReachable(ctx, res, r, inv, runs, opts); err != nil {
			return nil, err
		}
	} else {
		res.Inputs = planDirect(r, runs, inv, opts.IncludeFaction)
	}

	res.Ready = len(res.BlockedSkill) == 0 && !res.BlockedIllegal && !res.BlockedPassive
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

// outputPerRun returns how many primary-output units one crafting run of r
// yields. Defaults to 1 for recipes with no declared output quantity so the
// units→runs conversion degrades to identity.
func outputPerRun(r serverapi.Recipe) int {
	if len(r.Outputs) > 0 && r.Outputs[0].Quantity > 0 {
		return r.Outputs[0].Quantity
	}
	return 1
}

// runsFor converts a requested count of output units into the number of
// crafting runs needed, rounding up (a partial run costs a full run's
// materials). Always returns ≥ 1.
func runsFor(r serverapi.Recipe, units int) int {
	per := outputPerRun(r)
	runs := (units + per - 1) / per // ceil(units / per)
	return max(runs, 1)
}

// planDirect builds the per-direct-input gap rows. runs is the number of
// crafting runs (already converted from output units by runsFor).
func planDirect(r serverapi.Recipe, runs int, inv Inventory, includeFaction bool) []PlanInputRow {
	rows := make([]PlanInputRow, 0, len(r.Inputs))
	for _, in := range r.Inputs {
		need := in.Quantity * runs
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


