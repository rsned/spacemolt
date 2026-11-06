package worker

import (
	"context"
	"errors"
	"fmt"
	"sort"

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
// One item per call, named explicitly. FactionDepositAll is the deliberate
// exception for the Cloverfield dumping ground, with reserved goods excluded.
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

// factionDepositReserved are items FactionDepositAll never moves: energy
// crystals are reserved for the drone project (never sold or crafted without
// the operator), so they stay where they are.
var factionDepositReserved = map[string]bool{"energy_crystal": true}

// FactionDepositAll moves every item in this agent's personal storage at the
// station it is docked at into faction storage, except factionDepositReserved
// and anything in skip, the agent's own consign offers, which have to stay in
// personal storage for the offer to mean anything.
//
// It exists for the Cloverfield dumping ground (operator, 2026-11-01): haulers
// gift leftover cargo -- failed arbitrage legs, odds and ends -- to the
// resident CRFT member at Conjunction Junction, where it lands in that
// member's own storage; this sweep puts it in faction storage, where any CJ
// craft can draw on it. Returns how many item types it moved. A refused item
// is reported and the rest still move.
func FactionDepositAll(ctx context.Context, client game.GameClient, skip map[string]bool) (int, error) {
	state := client.GetState()
	if state == nil || state.Player.DockedAtBase == "" {
		return 0, nil
	}
	if err := client.ViewStorage(ctx); err != nil {
		return 0, fmt.Errorf("faction_deposit_all: view_storage: %w", err)
	}
	held, ok := consignHeld(client.GetRawJSON("storage"))
	if !ok {
		return 0, nil
	}
	items := make([]string, 0, len(held))
	for id, q := range held {
		if q > 0 && !factionDepositReserved[id] && !skip[id] {
			items = append(items, id)
		}
	}
	sort.Strings(items)
	moved := 0
	var failed []error
	for _, id := range items {
		if err := client.FactionDepositItemsPayload(ctx, map[string]any{
			"item_id": id, "quantity": held[id], "source": "storage",
		}); err != nil {
			failed = append(failed, fmt.Errorf("%s x%d: %w", id, held[id], err))
			continue
		}
		moved++
	}
	if len(failed) > 0 {
		return moved, fmt.Errorf("faction_deposit_all: %d refused: %w", len(failed), errors.Join(failed...))
	}
	return moved, nil
}
