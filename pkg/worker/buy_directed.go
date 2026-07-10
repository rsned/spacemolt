package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// BuyDirected buys ITEM xQTY at STATION with a per-unit price ceiling
// (0 = no ceiling), then hands to RECIPIENT (gift, or deposit when self).
// Recompute-remaining: counts recipient-bound goods already in cargo first.
// The live ask is checked once against maxUnit right after docking; buying
// then proceeds in cargo-capacity-sized chunks, handing each chunk off to
// RECIPIENT before buying the next (freeing cargo room) — no second travel
// leg, since the handoff happens at STATION where the buy already occurred.
func (d *WorkerDispatch) BuyDirected(ctx context.Context, itemID string, qty int, station string, maxUnit float64, recipient string) error {
	if qty < 1 {
		return fmt.Errorf("buy_directed: qty must be >= 1, got %d", qty)
	}
	sys, poi, err := resolveBase(ctx, d.KB, station)
	if err != nil {
		return fmt.Errorf("buy_directed: resolve station %q: %w", station, err)
	}
	if err := d.autopilotAndDock(ctx, sys, poi); err != nil {
		return fmt.Errorf("buy_directed: travel to station %q: %w", station, err)
	}

	if err := d.Client.ViewMarket(ctx, map[string]any{}); err != nil {
		return fmt.Errorf("buy_directed: view market at %s: %w", station, err)
	}
	ask, haveAsk := lowestSellPrice(d.Client.GetMarketListings(), itemID)
	if !haveAsk || ask <= 0 {
		return fmt.Errorf("buy_directed: no live ask for %s at %s", itemID, station)
	}
	if maxUnit > 0 && ask > maxUnit {
		return fmt.Errorf("buy_directed: price %v exceeds ceiling %v (replan)", ask, maxUnit)
	}

	remaining := qty
	for remaining > 0 {
		carrying := cargoCount(d.Client.GetState(), itemID)
		if carrying < remaining {
			carrying, err = d.buyIntoCargo(ctx, itemID, remaining-carrying, station)
			if err != nil {
				return err
			}
		}

		// Cap at remaining, mirroring Deliver: carrying can exceed remaining
		// when the ship walked in already holding recipient-bound goods from
		// a prior interrupted run — the buy above is skipped in that case.
		deliverQty := min(carrying, remaining)
		if err := giftOrDeposit(ctx, d, itemID, deliverQty, recipient); err != nil {
			return fmt.Errorf("buy_directed: %w", err)
		}
		time.Sleep(game.SleepQuick)
		remaining -= deliverQty
	}
	return nil
}

// buyIntoCargo buys up to want units of itemID (capped by free cargo space)
// at the current, already-docked station, then refreshes cargo — the live
// client's parseActionResult has no case that updates state.Ship.Cargo for
// "buy" (unlike "deposit_items"), so a successful Buy does NOT make the
// purchased goods visible in cargo on its own, exactly like withdraw in
// deliverWithdraw. Returns the total now carried.
func (d *WorkerDispatch) buyIntoCargo(ctx context.Context, itemID string, want int, stationLabel string) (int, error) {
	free := cargoFreeSpace(d.Client.GetState())
	if want > free {
		want = free
	}
	if want <= 0 {
		return 0, fmt.Errorf("buy_directed: cargo full — no room to buy %s at %s", itemID, stationLabel)
	}
	if err := d.Client.Buy(ctx, itemID, float64(want)); err != nil {
		return 0, fmt.Errorf("buy_directed: buy %s at %s: %w", itemID, stationLabel, err)
	}
	if err := d.Client.GetCargo(ctx); err != nil {
		return 0, fmt.Errorf("buy_directed: refresh cargo after buy %s at %s: %w", itemID, stationLabel, err)
	}
	time.Sleep(game.SleepQuick)
	return cargoCount(d.Client.GetState(), itemID), nil
}

// lowestSellPrice returns the cheapest live "sell" listing (the price a Buy
// would pay) for itemID, mirroring the lowestSellPrice scan in
// game.GetMarketPricesForCargo. ok is false when there is no sell listing for
// the item at all.
func lowestSellPrice(listings []game.MarketListing, itemID string) (price float64, ok bool) {
	for _, l := range listings {
		if l.ItemID != itemID || l.Type != "sell" {
			continue
		}
		if !ok || l.PricePerUnit < price {
			price, ok = l.PricePerUnit, true
		}
	}
	return price, ok
}
