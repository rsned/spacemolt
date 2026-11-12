package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// CraftStock keeps ITEM in the docked station's faction storage at TARGET by
// crafting RECIPE for the shortfall: target − held − output already queued
// for the same recipe at this base. Run it on a schedule ("keep copper_wiring
// at 5,000") instead of a fixed-quantity craft, which queues the same amount
// every pass whether the stock needs it or not (operator, 2026-11-12).
//
// The queue is counted so an hourly run does not re-order output that an
// earlier, still-running job will deliver. When the queue cannot be read the
// pass crafts nothing: over-ordering is the failure this verb exists to stop.
// Undocked is a no-op.
func (d *WorkerDispatch) CraftStock(ctx context.Context, recipeID, itemID string, target int, preset string) error {
	st := d.Client.GetState()
	if st == nil || st.Player.DockedAtBase == "" {
		fmt.Fprintf(d.Out, "craft_stock %s: not docked; skipped\n", itemID) //nolint:errcheck
		return nil
	}
	base := st.Player.DockedAtBase
	if d.KB == nil {
		return errors.New("craft_stock: no knowledge base")
	}
	recipe, err := d.KB.GetRecipe(ctx, recipeID)
	if err != nil || recipe == nil {
		return fmt.Errorf("craft_stock: unknown recipe %q: %v", recipeID, err)
	}
	perRun := 0
	for _, o := range recipe.Outputs {
		if o.ItemID == itemID {
			perRun += o.Quantity
		}
	}
	if perRun <= 0 {
		return fmt.Errorf("craft_stock: recipe %s does not make %s", recipeID, itemID)
	}

	if err = d.Client.ViewFactionStorage(ctx); err != nil {
		return fmt.Errorf("craft_stock %s: view faction storage: %w", itemID, err)
	}
	stored, ok := consignHeld(d.Client.GetRawJSON("faction_storage"))
	if !ok {
		return fmt.Errorf("craft_stock %s: faction storage reply unreadable", itemID)
	}
	held := stored[itemID]

	if err = d.Client.RawCommand(ctx, "craft", map[string]any{"action": "queue"}); err != nil {
		return fmt.Errorf("craft_stock %s: read craft queue: %w", itemID, err)
	}
	queued, err := queuedOutput(unwrapActionResult(d.Client.GetRawJSON("_last")), recipe.Name, base, perRun)
	if err != nil {
		return fmt.Errorf("craft_stock %s: %w", itemID, err)
	}

	want := target - held - queued
	if want <= 0 {
		fmt.Fprintf(d.Out, "craft_stock %s: %d held + %d queued >= target %d; nothing to craft\n", itemID, held, queued, target) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(d.Out, "craft_stock %s: %d held + %d queued < target %d; crafting %d\n", itemID, held, queued, target, want) //nolint:errcheck
	return d.craftShrinking(ctx, recipeID, want, preset)
}

// queuedOutput is the output still to come from queued jobs of the recipe
// named recipeName at base (jobs with no base are counted: older replies
// omitted it). The queue lists recipes by display name and counts runs.
func queuedOutput(raw []byte, recipeName, base string, perRun int) (int, error) {
	var listing serverapi.CraftQueueListing
	if err := json.Unmarshal(raw, &listing); err != nil {
		return 0, fmt.Errorf("decode craft queue: %w", err)
	}
	if listing.Action != "queue" && listing.Action != "job_list" {
		return 0, fmt.Errorf("craft queue reply is not a queue listing (action %q)", listing.Action)
	}
	runs := 0
	for _, j := range listing.Jobs {
		if j.Recipe == recipeName && (j.BaseID == "" || j.BaseID == base) {
			runs += j.RunsRemaining
		}
	}
	return runs * perRun, nil
}
