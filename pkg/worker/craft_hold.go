package worker

import (
	"context"
	"fmt"
)

// craftOutputHoldAt is the personal-storage stock of a recipe's output at
// which the scheduled `craft` verb stops making more. Station-bound drone
// hosts refine ore every hour with nothing hauling the product away: on
// 2026-11-18 marketbot_010 held 104,210 steel_plate and 119,800 copper_wiring,
// and the gas posts 300,000 purified_argon each. Past this the ore is worth
// more left as ore (it can still be sold or hauled raw).
const craftOutputHoldAt = 100000

// craftAllowance is how many of the qty output units asked for may still be
// crafted with held already in storage: all of them below the hold, the
// remainder up to it, and 0 once the hold is reached or the remainder is
// under craftMinQuantity.
func craftAllowance(held, qty int) int {
	room := craftOutputHoldAt - held
	if room >= qty {
		return qty
	}
	if room < craftMinQuantity {
		return 0
	}

	return room
}

// craftHeld crafts qty of recipe unless the output already stocked in the
// agent's own storage at this station has reached craftOutputHoldAt, trimming
// the batch to the room left. It fails open: when the output item or the
// storage cannot be read the craft runs as asked, because a hold that blocks
// on a lookup error would stop every line at once.
func (d *WorkerDispatch) craftHeld(ctx context.Context, recipe string, qty int, preset string) error {
	itemID, err := recipeOutputItemID(ctx, d.KB, recipe)
	if err != nil || itemID == "" {
		return d.craftShrinking(ctx, recipe, qty, preset)
	}
	if err = d.Client.ViewStorage(ctx); err != nil {
		return d.craftShrinking(ctx, recipe, qty, preset)
	}
	held := storageItemCount(d.Client.GetRawJSON("storage"), itemID)
	allowed := craftAllowance(held, qty)
	if allowed == 0 {
		fmt.Fprintf(d.Out, "craft: %s held: %d %s in storage (hold at %d)\n", recipe, held, itemID, craftOutputHoldAt) //nolint:errcheck
		return nil
	}
	if allowed < qty {
		fmt.Fprintf(d.Out, "craft: %s trimmed %d -> %d: %d %s in storage (hold at %d)\n", recipe, qty, allowed, held, itemID, craftOutputHoldAt) //nolint:errcheck
	}

	return d.craftShrinking(ctx, recipe, allowed, preset)
}
