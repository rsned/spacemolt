package game

import "testing"

// A SINGLE create_sell_order escrows the listed items server-side, but until
// 2026-08-31 only the BULK form's action_result updated the cargo cache. The
// stale cache then fed the haul thin-demand path a phantom inventory and it
// livelocked re-listing goods it no longer had (the power_cell storm).
func TestParseActionResult_SingleSellOrderRemovesCargo(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)
	client.state.Ship.Cargo = []CargoItem{{ItemID: "power_cell", Quantity: 675}, {ItemID: "scrap", Quantity: 5}}
	client.state.Ship.CargoUsed = 680

	client.parseActionResult(map[string]any{
		"command": "create_sell_order",
		"tick":    float64(1763400),
		"result": map[string]any{
			"action":      "create_sell_order",
			"item_id":     "power_cell",
			"quantity":    float64(675),
			"price_each":  float64(30900),
			"listing_fee": float64(312862),
			"from_cargo":  float64(675),
			"message":     "Sell order created.",
		},
	})
	st := client.GetState()
	for _, c := range st.Ship.Cargo {
		if c.ItemID == "power_cell" {
			t.Fatalf("escrowed power_cell still in cargo cache: %+v", st.Ship.Cargo)
		}
	}
	if st.Ship.CargoUsed != 5 {
		t.Fatalf("CargoUsed = %v, want 5", st.Ship.CargoUsed)
	}
}

// from_cargo caps the removal: items pulled from STORAGE must not be
// subtracted from cargo.
func TestParseActionResult_SingleSellOrderFromStorageLeavesCargo(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)
	client.state.Ship.Cargo = []CargoItem{{ItemID: "power_cell", Quantity: 100}}
	client.state.Ship.CargoUsed = 100

	client.parseActionResult(map[string]any{
		"command": "create_sell_order",
		"result": map[string]any{
			"action": "create_sell_order", "item_id": "power_cell",
			"quantity": float64(50), "from_cargo": float64(0), "from_storage": float64(50),
		},
	})
	if got := client.GetState().Ship.Cargo[0].Quantity; got != 100 {
		t.Fatalf("cargo quantity = %v, want 100 (storage-sourced order)", got)
	}
}
