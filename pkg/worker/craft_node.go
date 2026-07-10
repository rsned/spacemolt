package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// CraftOutputs crafts NUM_OUTPUTS units of RECIPE's output item at STATION.
// facility "hand" hand-crafts at the docked station's local workshop (the
// server auto-routes when facility_id is omitted from the payload — Task 0
// findings, 2026-07-10, confirmed hand/workshop dry-run and craft ARE
// supported live); any other value is passed as an explicit facility
// instance id (crafting requires being docked at that facility's station —
// a remote facility_id errors no_facility). NUM_OUTPUTS is passed directly
// as the craft command's `quantity`: the server does the ceil-divide into
// production runs itself (live-verified: quantity 6 on a 3-per-run recipe
// produced "runs": 2), so no output-per-run math is needed for the craft
// call itself.
//
// Recompute-remaining: before crafting, any of the recipe's output item
// already sitting in the worker's own storage at STATION (or already
// carried in cargo) counts against NUM_OUTPUTS, so a resumed/retried node
// does not over-produce. The recipe's output item id is looked up from the
// KB (recipe_outputs) — the craft_node task's params (RECIPE, NUM_OUTPUTS,
// STATION, FACILITY) never carry it directly.
//
// Budget gate: a live dry-run always runs first — it is both the
// have_inputs/have_credits preflight and the 2x-replan gate. The plan
// runner's budget admission already approved estFee (the planner's catalog
// fee estimate for this node); if the live dry-run's credits_total comes
// back more than 2x that, the catalog is stale and this fails with an error
// naming "replan" instead of queuing a possibly much more expensive job.
// estFee <= 0 disables the gate (no estimate to compare against).
//
// Crafting is asynchronous: the server queues the job and delivers output
// directly to the worker's own storage at STATION over subsequent ticks —
// craft nodes have no recipient and never gift or deposit manually. This
// verb returns once the job is queued, not once crafting completes.
func (d *WorkerDispatch) CraftOutputs(ctx context.Context, recipeID string, numOutputs int, station, facility string, estFee float64) error {
	if numOutputs < 1 {
		return fmt.Errorf("craft_node: numOutputs must be >= 1, got %d", numOutputs)
	}
	if facility == "" {
		facility = "hand"
	}

	sys, poi, err := resolveBase(ctx, d.KB, station)
	if err != nil {
		return fmt.Errorf("craft_node: resolve station %q: %w", station, err)
	}
	if err := d.autopilotAndDock(ctx, sys, poi); err != nil {
		return fmt.Errorf("craft_node: travel to station %q: %w", station, err)
	}

	itemID, err := recipeOutputItemID(ctx, d.KB, recipeID)
	if err != nil {
		return fmt.Errorf("craft_node: %w", err)
	}

	owned, err := d.craftOutputsOwned(ctx, itemID)
	if err != nil {
		return fmt.Errorf("craft_node: %w", err)
	}
	remaining := numOutputs - owned
	if remaining <= 0 {
		fmt.Fprintf(d.Out, "craft_node: %s already have %d of %d %s at %s — nothing to craft\n", //nolint:errcheck
			recipeID, owned, numOutputs, itemID, station)
		return nil
	}

	facilityID := ""
	if facility != "hand" {
		facilityID = facility
	}

	dry, err := d.Client.CraftDryRun(ctx, recipeID, remaining, facilityID)
	if err != nil {
		return fmt.Errorf("craft_node: dry run %s x%d: %w", recipeID, remaining, err)
	}
	if estFee > 0 && float64(dry.CreditsTotal) > 2*estFee {
		return fmt.Errorf("craft_node: %s x%d dry-run fee %d exceeds 2x planner estimate %v — stale catalog (replan)",
			recipeID, remaining, dry.CreditsTotal, estFee)
	}

	if facility == "hand" {
		if err := d.Client.CraftWithQuantity(ctx, recipeID, remaining); err != nil {
			return fmt.Errorf("craft_node: craft %s x%d: %w", recipeID, remaining, err)
		}
	} else {
		job := map[string]any{"recipe_id": recipeID, "quantity": remaining, "facility_id": facility}
		if err := d.Client.CraftBulk(ctx, []map[string]any{job}); err != nil {
			return fmt.Errorf("craft_node: craft %s x%d at facility %s: %w", recipeID, remaining, facility, err)
		}
	}
	time.Sleep(game.SleepQuick)
	fmt.Fprintf(d.Out, "craft_node: queued %s x%d at %s (%d of %d already owned)\n", //nolint:errcheck
		recipeID, remaining, station, owned, numOutputs)
	return nil
}

// recipeOutputItemID looks up the output item id produced by recipeID from
// the KB's recipe_outputs table. Requires *knowledge.SQLiteKB, mirroring
// resolveBase's requirement — there is no SQL to run against other Base
// implementations.
func recipeOutputItemID(ctx context.Context, kb knowledge.Base, recipeID string) (string, error) {
	sqliteKB, ok := kb.(*knowledge.SQLiteKB)
	if !ok || sqliteKB == nil {
		return "", fmt.Errorf("recipe output lookup for %q: requires *knowledge.SQLiteKB, got %T", recipeID, kb)
	}
	var itemID string
	err := sqliteKB.DB().QueryRowContext(ctx, `SELECT item_id FROM recipe_outputs WHERE recipe_id = ? LIMIT 1`, recipeID).Scan(&itemID)
	if err != nil {
		return "", fmt.Errorf("recipe output lookup for %q: %w", recipeID, err)
	}
	return itemID, nil
}

// craftOutputsOwned returns how many units of itemID the worker already owns
// at the current (already-docked) station — own storage plus ship cargo.
func (d *WorkerDispatch) craftOutputsOwned(ctx context.Context, itemID string) (int, error) {
	if err := d.Client.ViewStorage(ctx); err != nil {
		return 0, fmt.Errorf("view storage: %w", err)
	}
	time.Sleep(game.SleepQuick)
	storageQty := storageItemCount(d.Client.GetRawJSON("storage"), itemID)
	if err := d.Client.GetCargo(ctx); err != nil {
		return 0, fmt.Errorf("refresh cargo: %w", err)
	}
	return storageQty + cargoCount(d.Client.GetState(), itemID), nil
}

// storageItemCount decodes a view_storage raw JSON payload — the same
// {"items": [{"item_id", "quantity", ...}]} shape as
// serverapi.ViewStorageResponse and pkg/game's private getStorageItems
// decode — and returns itemID's quantity, or 0 if absent or the payload is
// missing/unparseable.
func storageItemCount(raw []byte, itemID string) int {
	if raw == nil {
		return 0
	}
	var resp struct {
		Items []struct {
			ItemID   string  `json:"item_id"`
			Quantity float64 `json:"quantity"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0
	}
	for _, item := range resp.Items {
		if item.ItemID == itemID {
			return int(item.Quantity)
		}
	}
	return 0
}
