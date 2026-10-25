package worker

import (
	"context"
	"fmt"
	"strings"
)

// craftMinQuantity is the smallest batch the shrinking craft falls back to.
// Ten output units is the operator's floor ("at the least it could do a batch
// of craft distill_moonshine 10"): small enough to run on a trickle of
// inputs, large enough that the per-command cost stays sane.
const craftMinQuantity = 10

// craftNotEnoughMaterials is the server's cannot_craft refusal when station
// storage (own or faction) cannot cover the whole job. The server is
// all-or-nothing per job, so a 60-unit request fails while 10 would succeed.
const craftNotEnoughMaterials = "Not enough materials"

// craftShrinking crafts qty output units of recipe, and when the server refuses
// for lack of materials retries at half the quantity, down to
// craftMinQuantity, so the facility runs on whatever inputs exist instead of
// idling for the whole ten-minute schedule tick. Live 2026-10-25: both Voss
// crafters refused every `craft distill_moonshine 60` for hours while faction
// storage held 95 ice + 35 culture — enough for 12 units. Any other error, and
// a refusal at the floor, propagate unchanged. preset "" means the server
// default (CraftWithQuantity), as before.
func (d *WorkerDispatch) craftShrinking(ctx context.Context, recipe string, qty int, preset string) error {
	requested := qty
	for {
		err := d.craftOnce(ctx, recipe, qty, preset)
		if err == nil {
			if qty != requested {
				fmt.Fprintf(d.Out, "craft: %s %d refused (not enough materials); crafted %d\n", recipe, requested, qty) //nolint:errcheck
			}

			return nil
		}
		if !strings.Contains(err.Error(), craftNotEnoughMaterials) || qty <= craftMinQuantity {
			return err
		}
		qty = max(qty/2, craftMinQuantity)
	}
}

func (d *WorkerDispatch) craftOnce(ctx context.Context, recipe string, qty int, preset string) error {
	if preset != "" {
		return d.Client.CraftWithPreset(ctx, recipe, qty, preset)
	}

	return d.Client.CraftWithQuantity(ctx, recipe, qty)
}
