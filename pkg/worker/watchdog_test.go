package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

func buyOrder(price, qty float64) market.Order {
	return market.Order{Side: "buy", PriceEach: price, Quantity: qty}
}

func TestAbsorbableProceedsWalksBidsHighFirst(t *testing.T) {
	orders := []market.Order{buyOrder(100, 5), buyOrder(120, 3), {Side: "sell", PriceEach: 999, Quantity: 10}}
	// hold 6: take 3@120 then 3@100 = 360 + 300 = 660; sell orders ignored.
	if got := absorbableProceeds(orders, 6); got != 660 {
		t.Errorf("got %v, want 660", got)
	}
	// hold 10 but only 8 demand: 3@120 + 5@100 = 860 (capped by book).
	if got := absorbableProceeds(orders, 10); got != 860 {
		t.Errorf("partial sellout: got %v, want 860", got)
	}
}

func TestArrivalDecision(t *testing.T) {
	cfg := watchdogConfig{} // defaults: 10% loss tolerance
	healthy := []market.Order{buyOrder(130, 50)}
	if got := arrivalDecision(healthy, 10, 1000, cfg); got != sellAtMarket {
		t.Errorf("healthy demand: got %v, want sellAtMarket", got)
	}
	// no buyers -> post cost order
	if got := arrivalDecision(nil, 10, 1000, cfg); got != postCostOrder {
		t.Errorf("no buyers: got %v, want postCostOrder", got)
	}
	// proceeds 800 vs buyCost 1000, floor 900 -> loss too deep -> cost order
	deep := []market.Order{buyOrder(80, 10)}
	if got := arrivalDecision(deep, 10, 1000, cfg); got != postCostOrder {
		t.Errorf("deep loss: got %v, want postCostOrder", got)
	}
	// proceeds 950 vs buyCost 1000, floor 900 -> small loss -> sell
	small := []market.Order{buyOrder(95, 10)}
	if got := arrivalDecision(small, 10, 1000, cfg); got != sellAtMarket {
		t.Errorf("small loss: got %v, want sellAtMarket", got)
	}
}

func TestBestRerouteChoosesReachableProfitableBuyer(t *testing.T) {
	held, unitBuy := 10.0, 100.0 // buyCost 1000; default margin 150; continueNet 50 -> threshold 200
	prices := []market.BestPrice{
		{ListingType: "buy", SystemID: "g", StationID: "g-stn", Price: 130, Quantity: 50},  // net 300 ✓
		{ListingType: "buy", SystemID: "f", StationID: "f-stn", Price: 200, Quantity: 50},  // unreachable
		{ListingType: "buy", SystemID: "w", StationID: "w-stn", Price: 115, Quantity: 50},  // net 150 < threshold
		{ListingType: "sell", SystemID: "g", StationID: "g-stn", Price: 999, Quantity: 50}, // ignored (ask)
	}
	jumps := map[string]int{"g": 2, "f": navigation.RouteInf, "w": 1}
	got, ok := bestReroute(held, unitBuy, prices, jumps, 50, rerouteConfig{})
	if !ok || got.SystemID != "g" {
		t.Fatalf("want reachable buyer g, got %+v ok=%v", got, ok)
	}
}

func TestBestRerouteRejectsWhenNoneBeatMargin(t *testing.T) {
	prices := []market.BestPrice{
		{ListingType: "buy", SystemID: "w", StationID: "w-stn", Price: 115, Quantity: 50}, // net 150 < 200
		{ListingType: "buy", SystemID: "p", StationID: "p-stn", Price: 300, Quantity: 3},  // partial: net -100
	}
	jumps := map[string]int{"w": 1, "p": 1}
	if _, ok := bestReroute(10, 100, prices, jumps, 50, rerouteConfig{}); ok {
		t.Fatalf("no candidate beats continueNet+margin; want ok=false")
	}
}

func TestBestRerouteRespectsJumpBudget(t *testing.T) {
	prices := []market.BestPrice{{ListingType: "buy", SystemID: "g", StationID: "g-stn", Price: 130, Quantity: 50}}
	jumps := map[string]int{"g": 9} // beyond the default budget of 5
	if _, ok := bestReroute(10, 100, prices, jumps, 50, rerouteConfig{}); ok {
		t.Fatalf("candidate beyond jump budget must be rejected")
	}
	if _, ok := bestReroute(10, 100, prices, jumps, 50, rerouteConfig{MaxExtraJumps: 10}); !ok {
		t.Fatalf("candidate within an enlarged budget should be accepted")
	}
}
