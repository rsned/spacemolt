package knowledge

import (
	"testing"
)

func TestBaseDataFromRawJSON(t *testing.T) {
	t.Parallel()
	rawJSON := []byte(`{
		"base": {
			"id": "base-1",
			"poi_id": "poi-1",
			"name": "Test Station",
			"description": "A test station",
			"empire": "solarian",
			"defense_level": 3,
			"has_drones": true,
			"public_access": true,
			"services": {
				"market": true,
				"repair": true,
				"refuel": false
			},
			"facilities": ["grand_solarian_exchange", "sol_admin_bureau", "unknown_facility"],
			"market": [
				{"id": "listing-1", "item_id": "iron_ore", "price_each": 10.5, "quantity": 100, "is_npc": true},
				{"id": "listing-2", "item_id": "copper_ore", "price_each": 12.0, "quantity": 50, "is_npc": false}
			]
		}
	}`)

	base, err := BaseDataFromRawJSON(rawJSON, "agent-1", 42)
	if err != nil {
		t.Fatalf("BaseDataFromRawJSON failed: %v", err)
	}

	if base.ID != "base-1" {
		t.Errorf("Expected ID 'base-1', got '%s'", base.ID)
	}
	if base.POIID != "poi-1" {
		t.Errorf("Expected POIID 'poi-1', got '%s'", base.POIID)
	}
	if base.Name != "Test Station" {
		t.Errorf("Expected name 'Test Station', got '%s'", base.Name)
	}
	if base.Empire != "solarian" {
		t.Errorf("Expected empire 'solarian', got '%s'", base.Empire)
	}
	if base.DefenseLevel != 3 {
		t.Errorf("Expected defense level 3, got %d", base.DefenseLevel)
	}
	if !base.HasDrones {
		t.Error("Expected HasDrones=true")
	}
	if !base.PublicAccess {
		t.Error("Expected PublicAccess=true")
	}

	// Check services
	if !base.Services["market"] {
		t.Error("Expected market service to be true")
	}
	if !base.Services["repair"] {
		t.Error("Expected repair service to be true")
	}
	if base.Services["refuel"] {
		t.Error("Expected refuel service to be false")
	}

	// Check facilities
	if len(base.Facilities) != 3 {
		t.Fatalf("Expected 3 facilities, got %d", len(base.Facilities))
	}

	// Known facility should have category
	found := false
	for _, f := range base.Facilities {
		if f.ID == "grand_solarian_exchange" {
			found = true
			if f.Category != "service" {
				t.Errorf("Expected category 'service', got '%s'", f.Category)
			}
			if f.Level != 5 {
				t.Errorf("Expected level 5, got %d", f.Level)
			}
		}
	}
	if !found {
		t.Error("Expected to find grand_solarian_exchange in facilities")
	}

	// Unknown facility should have category "unknown"
	for _, f := range base.Facilities {
		if f.ID == "unknown_facility" {
			if f.Category != "unknown" {
				t.Errorf("Expected category 'unknown' for unknown facility, got '%s'", f.Category)
			}
		}
	}

	// Check market items
	if len(base.Market) != 2 {
		t.Fatalf("Expected 2 market items, got %d", len(base.Market))
	}
	if base.Market[0].ItemID != "iron_ore" {
		t.Errorf("Expected first market item iron_ore, got %s", base.Market[0].ItemID)
	}
	if base.Market[0].PriceEach != 10.5 {
		t.Errorf("Expected price 10.5, got %f", base.Market[0].PriceEach)
	}

	if base.LastUpdatedTick != 42 {
		t.Errorf("Expected LastUpdatedTick 42, got %d", base.LastUpdatedTick)
	}
}

func TestBaseDataFromRawJSON_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := BaseDataFromRawJSON([]byte("invalid json"), "agent-1", 0)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestBaseDataFromRawJSON_EmptyBase(t *testing.T) {
	t.Parallel()
	rawJSON := []byte(`{"base": {"id": "", "poi_id": "", "name": "", "description": "", "empire": "", "defense_level": 0, "has_drones": false, "public_access": false, "services": {}, "facilities": [], "market": []}}`)

	base, err := BaseDataFromRawJSON(rawJSON, "agent-1", 0)
	if err != nil {
		t.Fatalf("BaseDataFromRawJSON with empty base failed: %v", err)
	}
	if base == nil {
		t.Fatal("Expected non-nil base")
	}
	if len(base.Facilities) != 0 {
		t.Errorf("Expected 0 facilities, got %d", len(base.Facilities))
	}
	if len(base.Market) != 0 {
		t.Errorf("Expected 0 market items, got %d", len(base.Market))
	}
}

func TestBaseDataFromRawJSON_NilInput(t *testing.T) {
	t.Parallel()
	_, err := BaseDataFromRawJSON(nil, "agent-1", 0)
	if err == nil {
		t.Error("Expected error for nil input")
	}
}

func TestBaseDataFromRawJSON_InvalidMarketItem(t *testing.T) {
	t.Parallel()
	// Market contains an invalid item that should be skipped
	rawJSON := []byte(`{
		"base": {
			"id": "base-1",
			"poi_id": "poi-1",
			"name": "Test",
			"description": "",
			"empire": "",
			"defense_level": 0,
			"has_drones": false,
			"public_access": false,
			"services": {},
			"facilities": [],
			"market": [
				"not a valid market item",
				{"id": "listing-1", "item_id": "iron_ore", "price_each": 10.0, "quantity": 5, "is_npc": true}
			]
		}
	}`)

	base, err := BaseDataFromRawJSON(rawJSON, "agent-1", 0)
	if err != nil {
		t.Fatalf("BaseDataFromRawJSON failed: %v", err)
	}
	// Invalid item should be skipped, leaving only the valid one
	if len(base.Market) != 1 {
		t.Errorf("Expected 1 market item (invalid skipped), got %d", len(base.Market))
	}
}
