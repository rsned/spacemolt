package market

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
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

	orders, err := parseViewMarket(raw, "station_test", time.Now().UTC())
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
	orders, err := parseViewMarket(nil, "stn", time.Now().UTC())
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

	orders, err := parseViewMarket(raw, "stn", time.Now().UTC())
	if err != nil {
		t.Fatalf("parseViewMarket failed: %v", err)
	}
	if len(orders) != 1 {
		t.Errorf("len(orders) = %d, want 1 (only the valid order)", len(orders))
	}
}

// snapshotFromState must map identity fields from the reliable State fields:
// System.ID/System.Name for the system (state.CurrentSystem is polluted with
// display names by get_system merges) and the POI's base/POI name for
// station_name (state.CurrentPOI is a slug, or a hash for player stations).
func TestSnapshotFromState_UsesSystemIDAndPOIName(t *testing.T) {
	state := &game.State{
		CurrentPOI:    "98eba8b1a7ad0520d6a7c8ea44b2d6aa",
		CurrentSystem: "Dheneb", // display-name pollution
	}
	state.System.ID = "dheneb"
	state.System.Name = "Dheneb"
	state.System.POIs = []game.POI{
		{ID: "dheneb_belt", Name: "Dheneb Belt"},
		{ID: "98eba8b1a7ad0520d6a7c8ea44b2d6aa", Name: "Hex Star", BaseName: "Hex Star Station"},
	}

	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	snap := snapshotFromState(state, []Order{{ItemID: "iron"}}, now)

	if snap.SystemID != "dheneb" {
		t.Errorf("SystemID = %q, want %q (must use System.ID, not CurrentSystem)", snap.SystemID, "dheneb")
	}
	if snap.SystemName != "Dheneb" {
		t.Errorf("SystemName = %q, want %q", snap.SystemName, "Dheneb")
	}
	if snap.StationID != "98eba8b1a7ad0520d6a7c8ea44b2d6aa" {
		t.Errorf("StationID = %q, want the POI id", snap.StationID)
	}
	if snap.StationName != "Hex Star Station" {
		t.Errorf("StationName = %q, want %q (BaseName preferred)", snap.StationName, "Hex Star Station")
	}
	if !snap.CapturedAt.Equal(now) || len(snap.Orders) != 1 {
		t.Errorf("CapturedAt/Orders not carried through: %v, %d orders", snap.CapturedAt, len(snap.Orders))
	}
}

// Degraded state must fall back rather than produce empty identity fields.
func TestSnapshotFromState_Fallbacks(t *testing.T) {
	state := &game.State{
		CurrentPOI:    "some_station",
		CurrentSystem: "krynn",
	}
	// System.ID empty, POI list doesn't contain the current POI.
	state.System.POIs = []game.POI{{ID: "other_poi", Name: "Other"}}

	snap := snapshotFromState(state, nil, time.Now())

	if snap.SystemID != "krynn" {
		t.Errorf("SystemID = %q, want CurrentSystem fallback %q", snap.SystemID, "krynn")
	}
	if snap.SystemName != "krynn" {
		t.Errorf("SystemName = %q, want fallback %q", snap.SystemName, "krynn")
	}
	if snap.StationName != "some_station" {
		t.Errorf("StationName = %q, want CurrentPOI fallback", snap.StationName)
	}
}

// A POI whose BaseName is empty falls back to the POI's own Name.
func TestSnapshotFromState_POINameWithoutBaseName(t *testing.T) {
	state := &game.State{CurrentPOI: "war_citadel", CurrentSystem: "Krynn"}
	state.System.ID = "krynn"
	state.System.Name = "Krynn"
	state.System.POIs = []game.POI{{ID: "war_citadel", Name: "War Citadel"}}

	snap := snapshotFromState(state, nil, time.Now())

	if snap.StationName != "War Citadel" {
		t.Errorf("StationName = %q, want POI Name fallback %q", snap.StationName, "War Citadel")
	}
	if snap.SystemID != "krynn" {
		t.Errorf("SystemID = %q, want %q", snap.SystemID, "krynn")
	}
}
