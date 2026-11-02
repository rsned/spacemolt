package worker

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// FactionRestock lists all of itemID held in faction storage, at the station
// the agent is docked at, as a faction sell order at priceEach. Proceeds go to
// the faction treasury.
//
// It keeps Conjunction Junction's contraband shelves stocked as haulers bring
// goods in (operator, 2026-11-01): moonshine from Voss arrives hourly, and a
// one-off listing sold out and left new stock sitting unlisted. A sell order
// escrows its goods, so what faction storage still holds is exactly what is
// not yet listed; an order at the same price consolidates server-side, so a
// scheduled run just lists what has arrived since the last one. The price is
// the operator's, fixed per schedule entry -- reprice by editing the entry.
//
// Undocked or nothing held is a no-op, not an error. Returns units listed.
func FactionRestock(ctx context.Context, client game.GameClient, itemID string, priceEach float64) (int, error) {
	state := client.GetState()
	if state == nil || state.Player.DockedAtBase == "" {
		return 0, nil
	}
	if err := client.ViewFactionStorage(ctx); err != nil {
		return 0, fmt.Errorf("faction_restock %s: view faction storage: %w", itemID, err)
	}
	held, ok := consignHeld(client.GetRawJSON("faction_storage"))
	if !ok || held[itemID] <= 0 {
		return 0, nil
	}
	if err := client.FactionCreateSellOrder(ctx, itemID, priceEach, held[itemID]); err != nil {
		return 0, fmt.Errorf("faction_restock %s x%d @%.0f: %w", itemID, held[itemID], priceEach, err)
	}
	return held[itemID], nil
}
