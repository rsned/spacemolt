package main

import (
	"fmt"
	"testing"
)

// ExampleTestFormatMissions demonstrates the mission formatter output.
// Run with: go test -v ./cmd/tools/play_as -run ExampleTestFormatMissions
func ExampleTestFormatMissions() {
	// Sample mission data matching the user's example
	// Includes missions with and without template_id
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
		"base_name": "Deep Range Outpost",
		"base_id": "deep_range_outpost"
	}`)

	output := formatMissions(rawJSON)
	fmt.Println(output)
}

// TestExampleFormatMissions runs the example test and verifies it produces output
func TestExampleFormatMissions(t *testing.T) {
	ExampleTestFormatMissions()
}
