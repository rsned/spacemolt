package craftbrain

import (
	"cmp"
	"context"
	"slices"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// siting is the decision for one craft node: which recipe, where, how many
// runs, what it costs in fees and time.
type siting struct {
	recipe   knowledge.RecipeDef
	facility *Facility // nil = hand-crafted at any docked station
	runs     int
	feeTotal int
	ticks    float64
	status   Status
	reason   string
}

// ceilDiv returns ceil(a/b) for positive b. Integer runs are why
// bill_of_materials' per-unit table can never be exact.
func ceilDiv(a, b int) int {
	if b <= 0 {
		b = 1
	}
	return (a + b - 1) / b
}

// outputPerRun returns how many units of item one run of r yields.
func outputPerRun(r knowledge.RecipeDef, item string) int {
	for _, o := range r.Outputs {
		if o.ItemID == item && o.Quantity > 0 {
			return o.Quantity
		}
	}
	return 1
}

// cheapestFacility picks the lowest total rent for runs, tie-breaking on
// faster ticks_per_run (a higher-level line) and then facility_id. The
// tiebreak chain is total: facility_id is unique, so ties never fall through
// to iteration order.
//
// NOTE: this scores fee only, not haul cost. A cheap-but-distant facility can
// therefore add a long haul leg; the plan's total-haul-jumps footer is how the
// operator catches that at review.
//
// fs is never sorted in place — it is a caller-owned slice (often the fake or
// real Source's backing storage), so it is copied before sorting.
func cheapestFacility(fs []Facility, runs int) *Facility {
	if len(fs) == 0 {
		return nil
	}
	sorted := make([]Facility, len(fs))
	copy(sorted, fs)
	slices.SortFunc(sorted, func(a, b Facility) int {
		if c := cmp.Compare(a.RentalFeePerRun*runs, b.RentalFeePerRun*runs); c != 0 {
			return c
		}
		if c := cmp.Compare(a.TicksPerRun, b.TicksPerRun); c != 0 {
			return c
		}
		return cmp.Compare(a.FacilityID, b.FacilityID)
	})
	best := sorted[0]
	return &best
}

// chooseSiting decides how to make demand units of item.
//
// Skills play no part: the server no longer gates crafting on them. The trade
// is fee versus time — hand-crafting is free but slow, a facility charges rent
// and runs several times faster, tripling again with each level.
func (e *Engine) chooseSiting(ctx context.Context, item string, demand int, cands []knowledge.RecipeDef, planTicks float64, opts Options) (siting, bool, error) {
	if len(cands) == 0 {
		return siting{}, false, nil
	}

	// Gather facilities per candidate once.
	facs := make(map[string][]Facility, len(cands))
	for _, r := range cands {
		fs, err := e.src.Facilities(ctx, r.ID)
		if err != nil {
			return siting{}, false, err
		}
		facs[r.ID] = fs
	}

	// Best hand candidate: lowest crafting_time, then recipe id, giving a
	// deterministic pick among non-facility_only candidates.
	var hand *knowledge.RecipeDef
	for i := range cands {
		if cands[i].FacilityOnly {
			continue
		}
		if hand == nil {
			hand = &cands[i]
			continue
		}
		switch cmp.Compare(cands[i].CraftingTime, hand.CraftingTime) {
		case -1:
			hand = &cands[i]
		case 0:
			if cands[i].ID < hand.ID {
				hand = &cands[i]
			}
		}
	}

	// Best facility candidate across all recipes. Each candidate is scored at
	// its own facility's output_per_run (runs is not shared across
	// facilities), then the whole set is sorted with a total tiebreak chain
	// — fee, ticks, facility_id, recipe_id — so the winner never depends on
	// map or slice iteration order.
	facCands := make([]siting, 0, len(cands))
	for _, r := range cands {
		fs := facs[r.ID]
		if len(fs) == 0 {
			continue
		}
		// Runs depend on the facility's own output_per_run, not the recipe's.
		probe := cheapestFacility(fs, 1)
		runs := ceilDiv(demand, probe.OutputPerRun)
		f := cheapestFacility(fs, runs)
		runs = ceilDiv(demand, f.OutputPerRun)
		facCands = append(facCands, siting{
			recipe:   r,
			facility: f,
			runs:     runs,
			feeTotal: f.RentalFeePerRun * runs,
			ticks:    float64(runs)*f.TicksPerRun + f.BacklogTicks,
			status:   StatusOK,
		})
	}
	haveFacility := len(facCands) > 0
	var bestSiting siting
	if haveFacility {
		slices.SortFunc(facCands, func(a, b siting) int {
			if c := cmp.Compare(a.feeTotal, b.feeTotal); c != 0 {
				return c
			}
			if c := cmp.Compare(a.facility.TicksPerRun, b.facility.TicksPerRun); c != 0 {
				return c
			}
			if c := cmp.Compare(a.facility.FacilityID, b.facility.FacilityID); c != 0 {
				return c
			}
			return cmp.Compare(a.recipe.ID, b.recipe.ID)
		})
		bestSiting = facCands[0]
	}

	if hand != nil {
		runs := ceilDiv(demand, outputPerRun(*hand, item))
		handTicks := float64(runs) * hand.CraftingTime
		overBudget := handTicks+planTicks > opts.MaxHandTicks
		switch {
		case overBudget && haveFacility:
			return bestSiting, true, nil
		case overBudget:
			return siting{
				recipe: *hand, runs: runs, ticks: handTicks,
				status: StatusSlow,
				reason: "hand-craft exceeds time budget; no public facility known",
			}, true, nil
		default:
			return siting{recipe: *hand, runs: runs, ticks: handTicks, status: StatusOK}, true, nil
		}
	}

	if haveFacility {
		return bestSiting, true, nil
	}
	return siting{}, false, nil // facility_only, nowhere to make it
}
