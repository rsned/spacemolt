package worker

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
)

// The 2026-08-31 power_cell livelock: client state said 137 cells were aboard,
// the server said 0 (the goods were already sold/escrowed but the cargo cache
// never learned), and the thin-demand path retried "list at cost ->
// insufficient_items -> leave claimed" every pass for twenty minutes across
// ten workers. insufficient_items is the server saying THE GOODS ARE GONE:
// the claim has nothing left to deliver and must be completed, not left.
func TestHaulSellLegCompletesClaimWhenCostOrderFindsNoGoods(t *testing.T) {
	o := opp(7, "b", "a", 100)
	fc := &fakeClient{state: &game.State{
		System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
	}, route: []game.RouteStep{{SystemID: "a", Name: "A"}},
		sellOrderErr: errors.New("insufficient_items: You have 0 x Iron Ore available (0 cargo, 0 storage). Need 10.")}
	f := &fakeStore{orders: []market.Order{{Side: "buy", PriceEach: 50, Quantity: 2}}} // thin -> cost order
	m := &haulMetrics{buyPrice: 100, qty: 10}
	if err := haulSellLeg(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, "a", m, 3); err != nil {
		t.Fatal(err)
	}
	if len(f.completed) != 1 || f.completed[0] != 7 {
		t.Fatalf("desynced cargo must complete the claim (nothing left to deliver), got completed=%v", f.completed)
	}
	if len(f.bookClaimsCompleted) != 1 || f.bookClaimsCompleted[0] != 3 {
		t.Fatalf("book claim must be freed too, got %v", f.bookClaimsCompleted)
	}
}

// The market-sell twin: explorer-1's "sell failed: insufficient_items: You
// have 0 but tried to sell 210; leaving claimed". Any OTHER sell failure must
// still leave the claim for a retry.
func TestHaulSellLegCompletesClaimWhenSellFindsNoGoods(t *testing.T) {
	o := opp(7, "b", "a", 100)
	base := func(sellErr error) (*fakeClient, *fakeStore) {
		return &fakeClient{state: &game.State{
				System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100,
				Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
			}, route: []game.RouteStep{{SystemID: "a", Name: "A"}}, sellErr: sellErr},
			&fakeStore{orders: []market.Order{{Side: "buy", PriceEach: 200, Quantity: 100}}} // healthy -> market sell
	}
	fc, f := base(errors.New("insufficient_items: Not enough Iron Ore in cargo. You have 0 but tried to sell 10."))
	if err := haulSellLeg(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, "a", &haulMetrics{buyPrice: 100, qty: 10}, 0); err != nil {
		t.Fatal(err)
	}
	if len(f.completed) != 1 {
		t.Fatalf("desynced market-sell must complete the claim, got %v", f.completed)
	}
	// A transient failure is NOT completion.
	fc2, f2 := base(errors.New("server busy"))
	if err := haulSellLeg(context.Background(), HaulDeps{Client: fc2, Market: f2, AgentID: "t"}, io.Discard, o, "a", &haulMetrics{buyPrice: 100, qty: 10}, 0); err != nil {
		t.Fatal(err)
	}
	if len(f2.completed) != 0 {
		t.Fatalf("transient sell failure must leave the claim, got completed=%v", f2.completed)
	}
}
