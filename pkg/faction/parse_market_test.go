package faction

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestParseFactionOrders(t *testing.T) {
	resp := serverapi.ViewOrdersResponse{
		Base: "b1",
		FactionOrders: []serverapi.ExchangeOrder{
			{OrderID: "o1", Side: "buy", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 100},
			{OrderID: "o2", OrderType: "sell", ItemID: "alloy", ItemName: "Alloy", PriceEach: 50, Quantity: 5},
		},
	}
	rows := parseFactionOrders("f1", "b1", resp)
	if len(rows) != 2 {
		t.Fatalf("want 2, got %d", len(rows))
	}
	if rows[0].Side != "buy" || rows[0].PriceEach != 10 {
		t.Errorf("row0 wrong: %+v", rows[0])
	}
	// Side falls back to OrderType when Side is empty.
	if rows[1].Side != "sell" {
		t.Errorf("row1 side fallback wrong: %+v", rows[1])
	}
}
