package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
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
