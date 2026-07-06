package craftplan

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ErrBOMUnavailable wraps any error returned by Source.BOM when --reachable
// is requested. Callers (REPL) should check via errors.Is and surface the
// "install/update the crafting DB" hint.
var ErrBOMUnavailable = errors.New("BOM unavailable")

// craftableReachable computes can_make and depth for the candidate recipes
// using their flattened BOM rows. Pre-condition: candidates already passed
// skill + legality gates.
func (e *Engine) craftableReachable(ctx context.Context, candidates []serverapi.Recipe, inv Inventory, includeFaction bool) ([]CraftableRow, error) {
	ids := make([]string, len(candidates))
	for i, r := range candidates {
		ids[i] = r.ID
	}
	bom, err := e.src.BOM(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBOMUnavailable, err)
	}

	out := make([]CraftableRow, 0, len(candidates))
	for _, r := range candidates {
		rows, ok := bom[r.ID]
		if !ok || len(rows) == 0 {
			// BOM table doesn't know about this recipe — fall back to direct
			// algorithm so the recipe still shows up if buildable.
			if cm := canMakeDirect(r, inv, includeFaction); cm >= 1 {
				out = append(out, makeRow(r, cm, 1))
			}
			continue
		}
		cm, depth := canMakeReachable(rows, inv, includeFaction)
		if cm < 1 {
			continue
		}
		out = append(out, makeRow(r, cm, depth))
	}
	return out, nil
}

// canMakeReachable returns the max batches buildable plus the deepest path
// depth across all BOM rows for one recipe.
func canMakeReachable(rows []BOMRow, inv Inventory, includeFaction bool) (canMake, depth int) {
	canMake = math.MaxInt
	for _, row := range rows {
		if row.Quantity <= 0 {
			continue
		}
		have := inv.total(row.BaseItemID, includeFaction)
		if maxForBase := have / row.Quantity; maxForBase < canMake {
			canMake = maxForBase
		}
		// Depth is the number of unique recipe_ids in the path. The recipe
		// itself is included in RecipePath, so depth == len(unique).
		if d := uniqueLen(row.RecipePath); d > depth {
			depth = d
		}
	}
	if depth == 0 {
		depth = 1
	}
	return canMake, depth
}

func uniqueLen(strs []string) int {
	seen := map[string]struct{}{}
	for _, s := range strs {
		seen[s] = struct{}{}
	}
	return len(seen)
}

// planReachable populates res.Inputs (base-material shortfall) and
// res.Intermediates from BOM rows. Replaces the stub in direct.go.
// runs is the number of crafting runs of the target recipe (converted from
// output units by runsFor); BOM base quantities are per-run, so material need
// scales with runs.
func (e *Engine) planReachable(ctx context.Context, res *PlanResult, r serverapi.Recipe, inv Inventory, runs int, opts PlanOpts) error {
	bomMap, err := e.src.BOM(ctx, []string{r.ID})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBOMUnavailable, err)
	}
	rows, ok := bomMap[r.ID]
	if !ok || len(rows) == 0 {
		// Fall back to direct inputs; tag the result so callers can render
		// the "BOM doesn't know about this recipe — showing direct inputs"
		// message. The PlanResult shape already supports this implicitly:
		// Intermediates stays empty.
		res.Inputs = planDirect(r, runs, inv, opts.IncludeFaction)
		return nil
	}

	res.Inputs = make([]PlanInputRow, 0, len(rows))
	for _, row := range rows {
		need := row.Quantity * runs
		pir := PlanInputRow{
			ItemID:      row.BaseItemID,
			Need:        need,
			HaveCargo:   inv.Cargo[row.BaseItemID],
			HaveStorage: inv.Storage[row.BaseItemID],
		}
		if opts.IncludeFaction {
			pir.HaveFaction = inv.Faction[row.BaseItemID]
		}
		total := pir.HaveCargo + pir.HaveStorage + pir.HaveFaction
		if need > total {
			pir.Short = need - total
		}
		res.Inputs = append(res.Inputs, pir)
	}

	res.Intermediates = collectIntermediates(rows, r.ID, runs, e.catalog)
	return nil
}

// collectIntermediates walks RecipePath entries (excluding the target itself)
// and returns one IntermediateCraft per unique sub-recipe. Output ordering
// matches the order they first appear in BOM rows so the renderer can present
// them as "in order".
func collectIntermediates(rows []BOMRow, targetID string, batches int, catalog map[string]serverapi.Recipe) []IntermediateCraft {
	seen := map[string]struct{}{}
	out := []IntermediateCraft{}
	for _, row := range rows {
		for _, rid := range row.RecipePath {
			if rid == targetID {
				continue
			}
			if _, dup := seen[rid]; dup {
				continue
			}
			seen[rid] = struct{}{}
			ic := IntermediateCraft{
				RecipeID:      rid,
				BatchesNeeded: batches, // best-effort; precise per-step batching is out of v1 scope
			}
			if r, ok := catalog[rid]; ok && len(r.Outputs) > 0 {
				ic.OutputItemID = r.Outputs[0].ItemID
				ic.OutputQuantity = r.Outputs[0].Quantity
			}
			out = append(out, ic)
		}
	}
	return out
}
