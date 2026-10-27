package game

import (
	"context"
	"slices"
	"testing"
)

// faction_deposit_all is deposit_all aimed at faction storage: every cargo
// line goes through faction_deposit_items, at its current quantity.
func TestDepositAllWith_DepositsEveryCargoLine(t *testing.T) {
	c := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	c.SetDebugLogging(false)
	c.state.Doc = true
	c.state.Ship.Cargo = []CargoItem{{ItemID: "control_node", Quantity: 400}, {ItemID: "steel_plate", Quantity: 12}}

	var got []string
	err := c.depositAllWith(context.Background(), "faction_deposit_all", "faction storage",
		func(_ context.Context, itemID string, qty float64) error {
			got = append(got, itemID)
			c.removeShipCargo(itemID, qty)
			return nil
		})
	if err != nil {
		t.Fatalf("depositAllWith: %v", err)
	}
	if want := []string{"control_node", "steel_plate"}; !slices.Equal(got, want) {
		t.Errorf("deposited %v, want %v", got, want)
	}
}
