package main

import (
	"strings"
	"testing"
)

// A trimmed but structurally faithful get_state payload (the undocumented
// server command), based on a live craftsman-1 response.
const sampleGetState = `{
  "cargo":[
    {"item_id":"graphene_sheet","item_name":"Graphene Sheet","quantity":24,"size":1},
    {"item_id":"steel_plate","item_name":"Steel Plate","quantity":603,"size":1}
  ],
  "location":{
    "connections":["market_prime","traders_rest"],
    "docked_at":"grand_exchange_station","empire":"nebula",
    "nearby_empire_npc_count":44,"nearby_pirate_count":0,"nearby_player_count":54,
    "offline_collapsed":506,"poi_id":"grand_exchange","poi_name":"Grand Exchange",
    "poi_type":"station","resources":[],
    "security_status":"Maximum Security (empire capital)",
    "system_id":"haven","system_name":"Haven"
  },
  "message":"Current game state",
  "missions":{"active":[],"max_missions":5},
  "player":{
    "credits":2495218,"empire":"nebula","faction_id":"e727c0e9","faction_rank":"leader",
    "id":"a50924913cef881c5e4d14257589d9ba","is_cloaked":false,"username":"Arthur 'Artificer' Artis"
  },
  "queue":{"has_pending":false},
  "ship":{
    "armor":12,"cargo_capacity":650,"cargo_used":650,"class_id":"bonanza_king",
    "class_name":"Bonanza King","cpu_capacity":38,"cpu_used":36,"defense_slots":1,
    "fuel":214,"hull":380,"max_fuel":280,"max_hull":380,"max_shield":170,
    "name":"Bonanza King","power_capacity":85,"power_used":44,"shield":170,
    "speed":1,"utility_slots":6,"weapon_slots":0
  },
  "skills":{
    "crafting":{"level":26,"max_level":100,"name":"Crafting","next_level_xp":25540,"xp":17540},
    "piloting":{"level":28,"max_level":100,"name":"Piloting","next_level_xp":29460,"xp":4079}
  },
  "version":"0.373.3"
}`

func TestFormatGetState(t *testing.T) {
	out := formatGetState([]byte(sampleGetState))
	if out == "" {
		t.Fatal("formatGetState returned empty for a valid payload")
	}
	// Spot-check fields unique to / important in the get_state shape.
	wants := []string{
		"Arthur 'Artificer' Artis",        // header title
		"Bonanza King [Bonanza King]",     // ship + class
		"Haven / Grand Exchange",          // nested location
		"Docked at grand_exchange_station", // derived status line
		"2,495,218 cr",                    // comma'd credits
		"v0.373.3",                        // version (get_status lacks this)
		"380/380",                         // hull
		"214/280",                         // fuel
		"Steel Plate",                     // cargo manifest
		"Maximum Security",                // security (may be truncated)
		"piloting",                        // skill sorted by level
		"0 / 5",                           // missions active/max
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("dashboard missing %q\n---\n%s", w, out)
		}
	}
	// Skills are level-sorted: piloting (28) must appear before crafting (26).
	if strings.Index(out, "piloting") > strings.Index(out, "crafting") {
		t.Errorf("skills not level-sorted (piloting should precede crafting)\n%s", out)
	}
}

func TestFormatGetStateBadPayloadFallsBack(t *testing.T) {
	if got := formatGetState([]byte(`{"not":"a state"}`)); got != "" {
		t.Errorf("want empty (caller falls back to JSON), got %q", got)
	}
}
