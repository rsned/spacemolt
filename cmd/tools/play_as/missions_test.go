package main

import (
	"encoding/json"
	"testing"
)

func TestFormatMissions(t *testing.T) {
	// Test data matching the structure from the user's example
	// Includes a mission with template_id and one without (only mission_id)
	rawJSON := []byte(`{
		"missions": [
			{
				"mission_id": "deep_core_prospecting",
				"template_id": "deep_core_prospecting",
				"type": "mining",
				"title": "Deep Core Prospecting",
				"description": "Foreman Voss has heard rumors of hidden mineral deposits deep in lawless space. He needs an experienced miner to help investigate.",
				"difficulty": 5,
				"giver": {
					"name": "Foreman Voss",
					"title": "Deep Core Research Initiative"
				},
				"chain_next": "exotic_crystal_synthesis",
				"expires_in_ticks": 0,
				"rewards": {
					"credits": 5000,
					"items": {
						"exotic_matter": 10
					},
					"skill_xp": {
						"mining": 15,
						"deep_core_mining": 25
					}
				},
				"objectives": [
					{
						"type": "mine_resource",
						"description": "Mine 20 units of Platinum Ore",
						"item_id": "platinum_ore",
						"quantity": 20
					}
				]
			},
			{
				"mission_id": "trade_deep_range_outpost_crimson_war_citadel_condensed_dark_matter",
				"template_id": "trade_deep_range_outpost_crimson_war_citadel_condensed_dark_matter",
				"type": "trading",
				"title": "Trade Run: Condensed Dark Matter to Crimson War Citadel",
				"description": "Condensed Dark Matter is in demand at Crimson War Citadel. Buy 23 units here for approximately 1078 credits each, then haul them to Crimson War Citadel (31 jumps) and sell for approximately 3520 credits each. Estimated profit: ~56166 credits.\n\nThis route crosses empire borders. Watch for pirates in lawless space between empires.",
				"difficulty": 7,
				"expires_in_ticks": 609,
				"giver": {
					"name": "Market Analyst Keth",
					"title": "Exchange Services"
				},
				"issuing_base": "Deep Range Outpost",
				"issuing_base_id": "deep_range_outpost",
				"issuing_system_id": "deep_range",
				"issuing_system_name": "Deep Range",
				"rewards": {
					"credits": 0,
					"skill_xp": {
						"trading": 40
					}
				},
				"objectives": [
					{
						"type": "sell_item",
						"description": "Sell 23 Condensed Dark Matter at Crimson War Citadel",
						"item_id": "condensed_dark_matter",
						"quantity": 23,
						"system_id": "krynn",
						"system_name": "Krynn",
						"target_base_id": "crimson_war_citadel",
						"target_base_name": "Crimson War Citadel"
					}
				]
			},
			{
				"mission_id": "trade_deep_range_outpost_ramens_rest_dark_matter_thruster",
				"type": "trading",
				"title": "Trade Run: Dark Matter Thruster to Ramen's Rest",
				"description": "Dark Matter Thruster is in demand at Ramen's Rest. Buy 3 units here for approximately 7685 credits each, then haul them to Ramen's Rest (4 jumps) and sell for approximately 11700 credits each. Estimated profit: ~12045 credits.",
				"difficulty": 3,
				"rewards": {
					"credits": 0,
					"skill_xp": {
						"trading": 20
					}
				},
				"expires_in_ticks": 49,
				"issuing_base": "Deep Range Outpost",
				"issuing_base_id": "deep_range_outpost",
				"issuing_system_id": "deep_range",
				"issuing_system_name": "Deep Range",
				"giver": {
					"name": "Trade Coordinator Riggs",
					"title": "Station Commerce Division"
				},
				"objectives": [
					{
						"type": "sell_item",
						"description": "Sell 3 Dark Matter Thruster at Ramen's Rest",
						"system_id": "last_light",
						"system_name": "Last Light",
						"target_base_id": "ramens_rest",
						"target_base_name": "Ramen's Rest",
						"item_id": "dark_matter_thruster",
						"quantity": 3
					}
				]
			}
		],
		"base_name": "Test Station",
		"base_id": "test_station"
	}`)

	output := formatMissions(rawJSON)

	// Check that output contains expected sections
	if len(output) == 0 {
		t.Fatal("formatMissions returned empty string")
	}

	// Check for mission count header
	if !contains(output, "Missions (3)") {
		t.Errorf("Expected mission count header, got: %s", output)
	}

	// Check that types are grouped (mining should come before trading alphabetically)
	miningPos := indexOf(output, "MINING")
	tradingPos := indexOf(output, "TRADING")
	if miningPos == -1 || tradingPos == -1 {
		t.Fatalf("Expected MINING and TRADING headers, got: %s", output)
	}
	if miningPos > tradingPos {
		t.Errorf("Expected MINING before TRADING (alphabetical order), got MINING at %d, TRADING at %d", miningPos, tradingPos)
	}

	// Check for mission titles
	if !contains(output, "Deep Core Prospecting") {
		t.Errorf("Expected 'Deep Core Prospecting' title, got: %s", output)
	}
	if !contains(output, "Trade Run: Condensed Dark Matter to Crimson War Citadel") {
		t.Errorf("Expected trading mission title, got: %s", output)
	}

	// Check for template IDs
	if !contains(output, "(deep_core_prospecting)") {
		t.Errorf("Expected template_id, got: %s", output)
	}

	// Check that mission without template_id uses mission_id instead
	if !contains(output, "(trade_deep_range_outpost_ramens_rest_dark_matter_thruster)") {
		t.Errorf("Expected mission_id to be used when template_id is missing, got: %s", output)
	}

	// Check for chain mission indicator
	if !contains(output, "Chain Mission") {
		t.Errorf("Expected 'Chain Mission' indicator, got: %s", output)
	}

	// Check for difficulty stars (should have ★ characters)
	if !contains(output, "★★★★★") {
		t.Errorf("Expected difficulty stars, got: %s", output)
	}

	// Check for rewards section
	if !contains(output, "Rewards:") {
		t.Errorf("Expected 'Rewards:' section, got: %s", output)
	}

	// Check for credits reward
	if !contains(output, "+5000 cr") {
		t.Errorf("Expected credits reward, got: %s", output)
	}

	// Check for item rewards
	if !contains(output, "exotic_matter") {
		t.Errorf("Expected item reward, got: %s", output)
	}

	// Check for skill XP rewards
	if !contains(output, "mining") || !contains(output, "deep_core_mining") {
		t.Errorf("Expected skill XP rewards, got: %s", output)
	}
}

func TestFormatMissions_Empty(t *testing.T) {
	rawJSON := []byte(`{
		"missions": [],
		"base_name": "Test Station",
		"base_id": "test_station"
	}`)

	output := formatMissions(rawJSON)

	if output == "" {
		t.Fatal("formatMissions returned empty string for empty mission list")
	}

	if !contains(output, "No missions available") {
		t.Errorf("Expected 'No missions available' message, got: %s", output)
	}
}

func TestFormatMissions_InvalidJSON(t *testing.T) {
	rawJSON := []byte(`invalid json`)

	output := formatMissions(rawJSON)

	if output != "" {
		t.Errorf("Expected empty string for invalid JSON, got: %s", output)
	}
}

// Helper functions
func contains(s, substr string) bool {
	return indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	return indexOfStr(s, substr, 0)
}

func indexOfStr(s, substr string, start int) int {
	if start < 0 {
		start = 0
	}
	if start >= len(s) {
		return -1
	}
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Test that the function handles the actual response structure from the game
func TestFormatMissions_RealResponseStructure(t *testing.T) {
	// This matches the actual GetMissionsResponse structure
	var resp struct {
		Missions []json.RawMessage `json:"missions"`
		BaseName string            `json:"base_name"`
		BaseID   string            `json:"base_id"`
	}

	resp.Missions = []json.RawMessage{
		[]byte(`{
			"mission_id": "test_mission",
			"template_id": "test_mission",
			"type": "mining",
			"title": "Test Mission",
			"description": "A test mission",
			"difficulty": 3
		}`),
	}
	resp.BaseName = "Test Base"
	resp.BaseID = "test_base"

	rawJSON, _ := json.Marshal(resp)
	output := formatMissions(rawJSON)

	if !contains(output, "Test Mission") {
		t.Errorf("Expected mission title in output, got: %s", output)
	}

	if !contains(output, "MINING") {
		t.Errorf("Expected MINING type header, got: %s", output)
	}
}
