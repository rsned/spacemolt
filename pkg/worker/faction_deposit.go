package worker

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// FactionDeposit moves this agent's whole personal-storage stock of itemID, at
// the station it is docked at, into faction storage.
//
// It exists for collection points: goods gifted to a faction officer land in
// the officer's own storage, while a craft draws all of a recipe's inputs from
// one storage. On 2026-10-30 Conjunction Junction held titanium alloy in
// miningbot_0000's storage and liquid hydrogen and steel in faction storage, so
// build_fuel_tank refused every hour; the platinum control-node crafts failed
// the same way. A scheduled sweep per item keeps the inputs in one place.
//
// One item per call, named explicitly: depositing is irreversible for anyone
// below officer rank, and the same storage holds reserved goods such as energy
// crystals, so there is deliberately no "deposit everything" form.
//
// Undocked or nothing held is a no-op, not an error.
func FactionDeposit(ctx context.Context, client game.GameClient, itemID string) error {
	state := client.GetState()
	if state == nil || state.Player.DockedAtBase == "" {
		return nil
	}
	if err := client.ViewStorage(ctx); err != nil {
		return fmt.Errorf("faction_deposit %s: view_storage: %w", itemID, err)
	}
	held, ok := consignHeld(client.GetRawJSON("storage"))
	if !ok || held[itemID] <= 0 {
		return nil
	}
	if err := client.FactionDepositItemsPayload(ctx, map[string]any{
		"item_id":  itemID,
		"quantity": held[itemID],
		"source":   "storage",
	}); err != nil {
		return fmt.Errorf("faction_deposit %s x%d: %w", itemID, held[itemID], err)
	}

	return nil
}
