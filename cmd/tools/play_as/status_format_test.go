package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// sampleStatusPayload is a trimmed live get_status OK frame (completed_missions
// shortened; otherwise field-for-field as the server sends it).
const sampleStatusPayload = `{"modules":[{"id":"5534fe2d520212ac4fa14e8ab18f5740","name":"Shield Booster II"}],"player":{"citizenships":["nebula"],"completed_missions":{"a_complimentary_bottle":"2026-06-09T09:03:10Z","a_word_in_private":"2026-04-13T20:00:36Z"},"created_at":"2026-02-08T08:50:13Z","credits":1420770,"current_poi":"cargo_lanes_gas_cloud","current_ship_id":"5d8fcb3546e1f9639649f08780bef3be","current_system":"cargo_lanes","empire":"nebula","faction_id":"e727c0e918d994c72db2978fe5b18edc","faction_rank":"leader","home_base":"grand_exchange_station","id":"a50924913cef881c5e4d14257589d9ba","revealed_pois":{"prismatic_gas_pocket":true,"rainbow_nebulite_vein":true,"wh_entrance_826d1efd":true,"wh_exit_5d528b92":true,"wh_exit_826d1efd":true,"wh_exit_a626d806":true},"skills":{"corporation_management":9,"crafting":26,"deep_core_mining":17,"drone_control":20,"engineering":18,"exploration":3,"mining":13,"navigation":12,"nebula_attunement":2,"piloting":28,"refining":19,"scanning":12,"smuggling":4,"trading":13},"standings":{"crimson":{"baseline":10,"outstanding_bounty":0,"reputation":10},"nebula":{"baseline":20,"outstanding_bounty":0,"reputation":26},"outerrim":{"baseline":10,"outstanding_bounty":0,"reputation":10},"pirates":{"baseline":10,"outstanding_bounty":0,"reputation":11},"solarian":{"baseline":10,"outstanding_bounty":0,"reputation":10},"voidborn":{"baseline":10,"outstanding_bounty":0,"reputation":10}},"stats":{"chat_messages_sent":13,"credits_earned":6208096,"credits_gifted":400000,"credits_spent":4278319,"customs_evaded":2,"damage_dealt":20,"damage_taken":822,"deaths_by_pirate":3,"deep_core_pois_discovered":6,"distance_traveled":256813,"exchange_credits_earned":5067143,"exchange_items_bought":10154,"exchange_items_sold":14551,"facilities_built":10,"gifts_received":4,"gifts_sent":13,"items_crafted":29225,"jumps_completed":487,"missions_abandoned":4,"missions_accepted":44,"missions_completed":32,"ore_mined":1008544,"pirates_destroyed":0,"scans_performed":4,"ships_lost":3,"systems_explored":155,"time_played":3642540,"times_docked":230,"trades_completed":598,"wormholes_traversed":1,"wreck_items_looted":25},"username":"Arthur 'Artificer' Artis"},"ship":{"class_id":"arbitrage","id":"5d8fcb3546e1f9639649f08780bef3be","name":"Arbitrage"}}`

func TestFormatGetStatus(t *testing.T) {
	out := formatGetStatus([]byte(sampleStatusPayload))
	if out == "" {
		t.Fatal("formatGetStatus returned empty")
	}

	// Header basics, the curated stats, and the three right-column boxes must
	// all appear.
	for _, want := range []string{
		"Arthur 'Artificer' Artis",
		"a50924913cef881c5e4d14257589d9ba",
		"e727c0e918d994c72db2978fe5b18edc (leader)",
		"1,420,770 cr",
		"cargo_lanes / cargo_lanes_gas_cloud",
		"Arbitrage [arbitrage]",
		"Stats", "Reputation", "Skills", "Missions",
		"6,208,096",       // credits earned
		"1,008,544",       // ore mined
		"42d 3h",          // time played
		"nebula",          // standing, own empire first
		"piloting",        // top skill
		"Hidden revealed", // revealed POI count label
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}

	// Every line must fit an 80-column terminal.
	for line := range strings.Lines(out) {
		if w := runewidth.StringWidth(strings.TrimRight(line, "\n")); w > 80 {
			t.Errorf("line exceeds 80 cols (%d): %q", w, line)
		}
	}

	// Own empire must sort ahead of the others in the Reputation box.
	if i, j := strings.Index(out, "nebula "), strings.Index(out, "crimson"); i < 0 || j < 0 || i > j {
		t.Errorf("expected own empire (nebula) before crimson in reputation box")
	}
}

func TestFormatGetStatusActionResultWrapped(t *testing.T) {
	wrapped := `{"action":"get_status","result":` + sampleStatusPayload + `}`
	if formatGetStatus([]byte(wrapped)) == "" {
		t.Fatal("formatGetStatus failed on action_result-wrapped frame")
	}
}

func TestFormatGetStatusBadPayload(t *testing.T) {
	if got := formatGetStatus([]byte(`{"player":{}}`)); got != "" {
		t.Errorf("expected empty for player with no id, got %q", got)
	}
	if got := formatGetStatus([]byte(`not json`)); got != "" {
		t.Errorf("expected empty for invalid json, got %q", got)
	}
}

func TestCommaInt(t *testing.T) {
	cases := map[int64]string{
		0: "0", 12: "12", 999: "999", 1000: "1,000",
		1420770: "1,420,770", -2500: "-2,500",
	}
	for in, want := range cases {
		if got := commaInt(in); got != want {
			t.Errorf("commaInt(%d) = %q, want %q", in, got, want)
		}
	}
}
