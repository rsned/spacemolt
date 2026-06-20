package market

import (
	"testing"
	"time"
)

func TestParseViewMarket(t *testing.T) {
	raw := []byte(`{
		"action": "view_market",
		"base_id": "station_test",
		"items": [
			{
				"item_id": "iron",
				"item_name": "Iron Ore",
				"best_buy": 100,
				"best_sell": 110,
				"buy_orders": [
					{"price_each": 100, "quantity": 500, "my_quantity": 0, "source": "player"}
				],
				"sell_orders": [
					{"price_each": 110, "quantity": 300, "my_quantity": 0, "source": "station"}
				]
			}
		]
	}`)

	orders, err := parseViewMarket(raw, "station_test", "system_test", time.Now().UTC())
	if err != nil {
		t.Fatalf("parseViewMarket failed: %v", err)
	}

	if len(orders) != 2 {
		t.Fatalf("len(orders) = %d, want 2", len(orders))
	}

	// Check buy order
	buy := orders[0]
	if buy.Side != "buy" {
		t.Errorf("first order side = %s, want buy", buy.Side)
	}
	if buy.ItemID != "iron" {
		t.Errorf("first order item_id = %s, want iron", buy.ItemID)
	}
	if buy.ItemName != "Iron Ore" {
		t.Errorf("first order item_name = %s, want 'Iron Ore'", buy.ItemName)
	}

	// Check sell order
	sell := orders[1]
	if sell.Side != "sell" {
		t.Errorf("second order side = %s, want sell", sell.Side)
	}
	if sell.PriceEach != 110 {
		t.Errorf("second order price = %f, want 110", sell.PriceEach)
	}
}

func TestParseViewMarket_Empty(t *testing.T) {
	orders, err := parseViewMarket(nil, "stn", "sys", time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orders != nil {
		t.Errorf("expected nil orders for empty input, got %d", len(orders))
	}
}

func TestParseViewMarket_SkipsNonPositive(t *testing.T) {
	raw := []byte(`{
		"items": [
			{
				"item_id": "iron",
				"item_name": "Iron Ore",
				"buy_orders": [
					{"price_each": 0, "quantity": 500, "source": "player"},
					{"price_each": 100, "quantity": 0, "source": "player"},
					{"price_each": 100, "quantity": 500, "source": "player"}
				],
				"sell_orders": []
			}
		]
	}`)

	orders, err := parseViewMarket(raw, "stn", "sys", time.Now().UTC())
	if err != nil {
		t.Fatalf("parseViewMarket failed: %v", err)
	}
	if len(orders) != 1 {
		t.Errorf("len(orders) = %d, want 1 (only the valid order)", len(orders))
	}
}
