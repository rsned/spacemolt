package main

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestComputePnLUsesActualFills proves the reconciliation core: P&L is summed
// from the real exchange_fill totals (buyer = spent, seller = earned), refuel
// total_cost is the fuel deduction, and unrelated events (rent) are ignored. The
// fixture is the real salvager-6 round trip — bought 31 navigation_core @6330,
// sold @6060 — which is a genuine loss the old quote-based recorder hid.
func TestComputePnLUsesActualFills(t *testing.T) {
	entries := []serverapi.ActionLogEntry{
		{EventType: "trading.exchange_fill", CreatedAt: "2026-06-27T14:29:27Z",
			Data: map[string]any{"role": "buyer", "total": float64(196230), "item_id": "navigation_core", "quantity": float64(31), "price": float64(6330)}},
		{EventType: "trading.exchange_fill", CreatedAt: "2026-06-27T14:43:27Z",
			Data: map[string]any{"role": "seller", "total": float64(187860), "item_id": "navigation_core", "quantity": float64(31), "price": float64(6060)}},
		{EventType: "ship.refuel", CreatedAt: "2026-06-27T14:20:00Z",
			Data: map[string]any{"total_cost": float64(600)}},
		{EventType: "other.rent_paid", CreatedAt: "2026-06-27T00:00:00Z",
			Data: map[string]any{"total_cost": float64(99999)}}, // unrelated: must be ignored
	}
	p := computePnL("salvager-6", entries)
	if p.Bought != 196230 || p.Sold != 187860 {
		t.Fatalf("bought/sold: got %d/%d want 196230/187860", p.Bought, p.Sold)
	}
	if p.Buys != 1 || p.Sells != 1 {
		t.Fatalf("counts: got buys=%d sells=%d want 1/1", p.Buys, p.Sells)
	}
	if p.FuelCost != 600 {
		t.Fatalf("fuel: got %d want 600", p.FuelCost)
	}
	if got := p.TrueNet(false); got != 187860-196230 {
		t.Errorf("net ex-fuel: got %d want %d", got, 187860-196230)
	}
	if got := p.TrueNet(true); got != 187860-196230-600 {
		t.Errorf("net inc-fuel: got %d want %d", got, 187860-196230-600)
	}
	if p.FirstFill != "2026-06-27T14:29:27Z" || p.LastFill != "2026-06-27T14:43:27Z" {
		t.Errorf("fill window from fills only: first=%s last=%s", p.FirstFill, p.LastFill)
	}
}
