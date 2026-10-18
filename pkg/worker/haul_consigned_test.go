package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
)

// A CONSIGNED opportunity is one whose goods are already in the hauler's
// storage at the source station, put there by a colleague's send_gift. There
// is no buy leg: the hauler withdraws, flies, and sells.
//
// Modelling it as an ordinary row with buy_price 0 does not work and is
// actively harmful. sizeBuy returns 0 for any ask <= 0 and then divides
// credits by it, and haulGate reports "no live ask at buy station" -- a reason
// the run treats as proof the source book is gone, so it calls
// abandonCollapsed and EXPIRES the opportunity and its sibling destinations.
// A synthetic zero-price row would poison the pool for every hauler.
//
// Hence its own ActionType, which routes past the ask gate entirely.
func TestHaulGate_ConsignedNeedsNoAsk(t *testing.T) {
	opp := market.ArbitrageOpportunity{
		ID: 1, ActionType: market.ActionWithdrawThenSell,
		FromStationID: "voss_redoubt", ToStationID: "factory_belt_manufacturing_hub",
		ItemID: "adamantite_ore", BuyPrice: 0, SellPrice: 1600, Quantity: 260,
	}
	// Only a BID at the destination. No ask anywhere -- the goods are ours.
	prices := []market.ItemStationPrice{
		{StationID: "factory_belt_manufacturing_hub", HasBuy: true, BestBid: 1600},
	}

	qty, _, sellBid, ok, reason := haulGate(opp, prices, 1900, 1900, 0, 120, nil)
	if !ok {
		t.Fatalf("gate rejected a consigned opportunity: %s", reason)
	}
	if sellBid != 1600 {
		t.Errorf("sellBid = %v, want 1600", sellBid)
	}
	// Sized by what is held and the hold, never by credits: there is nothing
	// to pay for, and a hauler with 0 credits must still be able to run it.
	if qty != 260 {
		t.Errorf("qty = %v, want 260 (held quantity, cargo permitting)", qty)
	}
}

// Still gated on real demand: a consigned row with no bid at the destination
// is worthless and must be rejected like any other.
func TestHaulGate_ConsignedStillNeedsABid(t *testing.T) {
	opp := market.ArbitrageOpportunity{
		ID: 2, ActionType: market.ActionWithdrawThenSell,
		FromStationID: "voss_redoubt", ToStationID: "factory_belt_manufacturing_hub",
		ItemID: "adamantite_ore", SellPrice: 1600, Quantity: 260,
	}
	if _, _, _, ok, reason := haulGate(opp, nil, 1900, 1900, 0, 120, nil); ok {
		t.Error("want rejection with no bid at the sell station")
	} else if reason != "no live bid at sell station" {
		t.Errorf("reason = %q, want the missing-bid reason", reason)
	}
}

// The hold still caps it.
func TestHaulGate_ConsignedCappedByCargo(t *testing.T) {
	opp := market.ArbitrageOpportunity{
		ID: 3, ActionType: market.ActionWithdrawThenSell,
		FromStationID: "a", ToStationID: "b", ItemID: "adamantite_ore",
		SellPrice: 1600, Quantity: 5000,
	}
	prices := []market.ItemStationPrice{{StationID: "b", HasBuy: true, BestBid: 1600}}

	qty, _, _, ok, reason := haulGate(opp, prices, 950, 1900, 0, 120, nil)
	if !ok {
		t.Fatalf("rejected: %s", reason)
	}
	if qty != 950 {
		t.Errorf("qty = %v, want 950 (cargo-capped)", qty)
	}
}

// An ordinary buy_then_sell row is unaffected: it still demands a live ask.
func TestHaulGate_OrdinaryRowStillDemandsAnAsk(t *testing.T) {
	opp := market.ArbitrageOpportunity{
		ID: 4, ActionType: "buy_then_sell", FromStationID: "a", ToStationID: "b",
		ItemID: "iron_ore", BuyPrice: 10, SellPrice: 30, Quantity: 100,
	}
	prices := []market.ItemStationPrice{{StationID: "b", HasBuy: true, BestBid: 30}}

	if _, _, _, ok, reason := haulGate(opp, prices, 1900, 1900, 10000, 10, nil); ok {
		t.Error("an ordinary row with no ask must still be rejected")
	} else if reason != "no live ask at buy station" {
		t.Errorf("reason = %q, want the missing-ask reason", reason)
	}
}

// IsConsigned is the single predicate the run branches on, so the ActionType
// string is not spread across the engine.
func TestIsConsigned(t *testing.T) {
	consigned := market.ArbitrageOpportunity{ActionType: market.ActionWithdrawThenSell}
	if !consigned.IsConsigned() {
		t.Error("withdraw_then_sell should report consigned")
	}
	for _, at := range []string{"buy_then_sell", "sell_then_buy", ""} {
		if (market.ArbitrageOpportunity{ActionType: at}).IsConsigned() {
			t.Errorf("%q should not report consigned", at)
		}
	}
}
