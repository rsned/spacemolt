package market

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestOrdersFromListings(t *testing.T) {
	now := time.Now().UTC()
	in := []game.MarketListing{{ItemID: "iron", Type: "sell", PricePerUnit: 5, Quantity: 10}}
	out := OrdersFromListings("stn1", in, "play_as", now)
	if len(out) != 1 {
		t.Fatalf("expected 1 order, got %d", len(out))
	}
	o := out[0]
	if o.StationID != "stn1" || o.ItemID != "iron" || o.Side != "sell" || o.PriceEach != 5 || o.Quantity != 10 || o.Source != "play_as" {
		t.Errorf("bad conversion: %+v", o)
	}
}

func TestOrdersFromListingsEmpty(t *testing.T) {
	out := OrdersFromListings("stn1", nil, "agent", time.Now().UTC())
	if len(out) != 0 {
		t.Fatalf("expected 0 orders, got %d", len(out))
	}
}

func TestOrdersFromListingsMultiple(t *testing.T) {
	now := time.Now().UTC()
	in := []game.MarketListing{
		{ItemID: "iron", Type: "sell", PricePerUnit: 5, Quantity: 10},
		{ItemID: "gold", Type: "buy", PricePerUnit: 100, Quantity: 3},
	}
	out := OrdersFromListings("stn2", in, "worker", now)
	if len(out) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(out))
	}
	if out[0].ItemID != "iron" || out[1].ItemID != "gold" {
		t.Errorf("order mismatch: %+v", out)
	}
	if out[1].Side != "buy" {
		t.Errorf("expected buy side, got %q", out[1].Side)
	}
}
