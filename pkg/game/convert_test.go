package game

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestPlayerFromAPI(t *testing.T) {
	// Use JSON that matches actual server format
	playerJSON := `{
		"id": "abc123",
		"username": "TestPlayer",
		"empire": "solarian",
		"credits": 5000,
		"current_system": "sol",
		"current_poi": "sol_station",
		"current_ship_id": "ship123",
		"home_base": "confederacy_central_command",
		"docked_at_base": "confederacy_central_command",
		"primary_color": "#FF0000",
		"secondary_color": "#00FF00",
		"anonymous": false,
		"is_cloaked": false,
		"skills": {
			"mining": {"level": 3, "xp": 150},
			"trading": 2
		},
		"skill_xp": {
			"mining": 150,
			"trading": 50
		},
		"stats": {
			"ships_destroyed": 1,
			"times_destroyed": 2,
			"ore_mined": 500,
			"credits_earned": 10000,
			"credits_spent": 5000,
			"trades_completed": 20,
			"systems_discovered": 3,
			"items_crafted": 0,
			"missions_completed": 1
		}
	}`

	var ext serverapi.Player
	if err := json.Unmarshal([]byte(playerJSON), &ext); err != nil {
		t.Fatalf("Failed to unmarshal player JSON: %v", err)
	}

	player := PlayerFromAPI(ext)

	if player.ID != "abc123" {
		t.Errorf("ID: got %q, want %q", player.ID, "abc123")
	}
	if player.Username != "TestPlayer" {
		t.Errorf("Username: got %q, want %q", player.Username, "TestPlayer")
	}
	if player.Credits != 5000 {
		t.Errorf("Credits: got %f, want %f", player.Credits, 5000.0)
	}
	if player.DockedAtBase != "confederacy_central_command" {
		t.Errorf("DockedAtBase: got %q, want %q", player.DockedAtBase, "confederacy_central_command")
	}

	// Check skills - both object and number formats
	if len(player.Skills) != 2 {
		t.Fatalf("Skills count: got %d, want 2", len(player.Skills))
	}
	if s, ok := player.Skills["mining"]; !ok || s.Level != 3 || s.XP != 150 {
		t.Errorf("mining_basic skill: got %+v", player.Skills["mining"])
	}
	if s, ok := player.Skills["trading"]; !ok || s.Level != 2 {
		t.Errorf("trading skill: got %+v, want level 2", s)
	}

	// Check skill XP
	if player.SkillXP["mining"] != 150 {
		t.Errorf("SkillXP mining_basic: got %f, want 150", player.SkillXP["mining"])
	}

	// Check stats
	if player.Stats.ShipsDestroyed != 1 {
		t.Errorf("Stats.ShipsDestroyed: got %d, want 1", player.Stats.ShipsDestroyed)
	}
	if player.Stats.OreMined != 500 {
		t.Errorf("Stats.OreMined: got %d, want 500", player.Stats.OreMined)
	}
}

func TestShipFromAPI(t *testing.T) {
	shipJSON := `{
		"id": "ship123",
		"owner_id": "player456",
		"class_id": "mining_enhanced",
		"name": "Drillship",
		"hull": 140,
		"max_hull": 140,
		"shield": 55,
		"max_shield": 55,
		"shield_recharge": 1,
		"armor": 8,
		"speed": 2,
		"fuel": 100,
		"max_fuel": 130,
		"cargo_used": 30,
		"cargo_capacity": 100,
		"cpu_used": 2,
		"cpu_capacity": 16,
		"power_used": 5,
		"power_capacity": 32,
		"weapon_slots": 1,
		"defense_slots": 2,
		"utility_slots": 3,
		"modules": ["mod_abc", "mod_def"],
		"cargo": [
			{"item_id": "iron_ore", "quantity": 20},
			{"item_id": "copper_ore", "quantity": 10}
		]
	}`

	var ext serverapi.Ship
	if err := json.Unmarshal([]byte(shipJSON), &ext); err != nil {
		t.Fatalf("Failed to unmarshal ship JSON: %v", err)
	}

	ship := ShipFromAPI(ext)

	if ship.ID != "ship123" {
		t.Errorf("ID: got %q, want %q", ship.ID, "ship123")
	}
	if ship.Hull != 140 {
		t.Errorf("Hull: got %f, want 140", ship.Hull)
	}
	if ship.Fuel != 100 {
		t.Errorf("Fuel: got %f, want 100", ship.Fuel)
	}
	if len(ship.Modules) != 2 {
		t.Fatalf("Modules count: got %d, want 2", len(ship.Modules))
	}
	if ship.Modules[0] != "mod_abc" {
		t.Errorf("Modules[0]: got %q, want %q", ship.Modules[0], "mod_abc")
	}
	if len(ship.Cargo) != 2 {
		t.Fatalf("Cargo count: got %d, want 2", len(ship.Cargo))
	}
	if ship.Cargo[0].ItemID != "iron_ore" || ship.Cargo[0].Quantity != 20 {
		t.Errorf("Cargo[0]: got %+v", ship.Cargo[0])
	}
	if ship.WeaponSlots != 1 {
		t.Errorf("WeaponSlots: got %d, want 1", ship.WeaponSlots)
	}
}

func TestSystemDataFromAPI(t *testing.T) {
	sysJSON := `{
		"id": "sys123",
		"name": "Sol",
		"description": "Home system",
		"empire": "solarian",
		"police_level": 5,
		"security_status": "High Security",
		"pois": [
			{
				"id": "poi1",
				"system_id": "sys123",
				"type": "station",
				"name": "Sol Station",
				"position": {"x": 10, "y": 20},
				"resources": [
					{"resource_id": "iron_ore", "richness": 0.8, "remaining": 1000}
				],
				"has_base": true,
				"base_name": "Main Base"
			}
		],
		"connections": [
			{"system_id": "sys456", "name": "Alpha", "distance": 5}
		],
		"discovered": true,
		"position": {"x": 0, "y": 0}
	}`

	var ext serverapi.SystemData
	if err := json.Unmarshal([]byte(sysJSON), &ext); err != nil {
		t.Fatalf("Failed to unmarshal system JSON: %v", err)
	}

	sys := SystemDataFromAPI(ext)

	if sys.ID != "sys123" {
		t.Errorf("ID: got %q, want %q", sys.ID, "sys123")
	}
	if sys.Name != "Sol" {
		t.Errorf("Name: got %q, want %q", sys.Name, "Sol")
	}
	if sys.PoliceLevel != 5 {
		t.Errorf("PoliceLevel: got %d, want 5", sys.PoliceLevel)
	}
	if len(sys.POIs) != 1 {
		t.Fatalf("POIs count: got %d, want 1", len(sys.POIs))
	}
	if sys.POIs[0].Name != "Sol Station" {
		t.Errorf("POI[0].Name: got %q", sys.POIs[0].Name)
	}
	if sys.POIs[0].HasBase != true {
		t.Error("POI[0].HasBase: got false, want true")
	}
	if len(sys.POIs[0].Resources) != 1 || sys.POIs[0].Resources[0].ResourceID != "iron_ore" {
		t.Errorf("POI[0].Resources: got %+v", sys.POIs[0].Resources)
	}
	if len(sys.Connections) != 1 || sys.Connections[0].SystemID != "sys456" {
		t.Errorf("Connections: got %+v", sys.Connections)
	}
	if !sys.Discovered {
		t.Error("Discovered: got false, want true")
	}
}

func TestMarketListingFromAPI_FieldVariants(t *testing.T) {
	// Test with price_each (server format) instead of price_per_unit
	listingJSON := `{
		"item_id": "iron_ore",
		"quantity": 50,
		"price_each": 10,
		"total": 500,
		"type": "sell",
		"seller": "NPC_Vendor"
	}`

	var ext serverapi.MarketListing
	if err := json.Unmarshal([]byte(listingJSON), &ext); err != nil {
		t.Fatalf("Failed to unmarshal listing JSON: %v", err)
	}

	listing := MarketListingFromAPI(ext)

	if listing.ItemID != "iron_ore" {
		t.Errorf("ItemID: got %q", listing.ItemID)
	}
	if listing.PricePerUnit != 10 {
		t.Errorf("PricePerUnit: got %f, want 10 (from price_each)", listing.PricePerUnit)
	}
	if listing.TotalPrice != 500 {
		t.Errorf("TotalPrice: got %f, want 500 (from total)", listing.TotalPrice)
	}
	if listing.ListedBy != "NPC_Vendor" {
		t.Errorf("ListedBy: got %q, want %q (from seller)", listing.ListedBy, "NPC_Vendor")
	}
}

func TestNearbyPlayerFromAPI(t *testing.T) {
	nearbyJSON := `{
		"player_id": "p1",
		"username": "RivalPlayer",
		"ship_class": "fighter_heavy",
		"primary_color": "#FF0000",
		"anonymous": false,
		"in_combat": true
	}`

	var ext serverapi.NearbyPlayer
	if err := json.Unmarshal([]byte(nearbyJSON), &ext); err != nil {
		t.Fatalf("Failed to unmarshal nearby JSON: %v", err)
	}

	nearby := NearbyPlayerFromAPI(ext)

	if nearby.PlayerID != "p1" {
		t.Errorf("PlayerID: got %q", nearby.PlayerID)
	}
	if nearby.Username != "RivalPlayer" {
		t.Errorf("Username: got %q", nearby.Username)
	}
	if nearby.ShipClass != "fighter_heavy" {
		t.Errorf("ShipClass: got %q", nearby.ShipClass)
	}
	if !nearby.InCombat {
		t.Error("InCombat: got false, want true")
	}
}

func TestSkillUnmarshalJSON_BothFormats(t *testing.T) {
	// Object format
	var s1 serverapi.Skill
	if err := json.Unmarshal([]byte(`{"level": 5, "xp": 200}`), &s1); err != nil {
		t.Fatalf("Object format: %v", err)
	}
	if s1.Level != 5 || s1.XP != 200 {
		t.Errorf("Object format: got %+v, want level=5, xp=200", s1)
	}

	// Number format
	var s2 serverapi.Skill
	if err := json.Unmarshal([]byte(`3`), &s2); err != nil {
		t.Fatalf("Number format: %v", err)
	}
	if s2.Level != 3 {
		t.Errorf("Number format: got level=%d, want 3", s2.Level)
	}
}

func TestUnmarshalPayloadKey(t *testing.T) {
	payload := map[string]any{
		"player": map[string]any{
			"id":       "abc",
			"username": "test",
			"credits":  float64(1000),
			"skills": map[string]any{
				"mining": float64(2),
			},
			"stats": map[string]any{},
		},
	}

	var ext serverapi.Player
	if !unmarshalPayloadKey(payload, "player", &ext) {
		t.Fatal("unmarshalPayloadKey returned false for existing key")
	}
	if ext.ID != "abc" {
		t.Errorf("ID: got %q, want %q", ext.ID, "abc")
	}
	if ext.Credits != 1000 {
		t.Errorf("Credits: got %f, want 1000", ext.Credits)
	}
	// Skills as number format should work via custom unmarshaler
	if s, ok := ext.Skills["mining"]; !ok || s.Level != 2 {
		t.Errorf("Skills[mining]: got %+v", ext.Skills["mining"])
	}

	// Non-existent key
	var ext2 serverapi.Player
	if unmarshalPayloadKey(payload, "nonexistent", &ext2) {
		t.Error("unmarshalPayloadKey returned true for non-existent key")
	}
}
