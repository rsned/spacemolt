package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// sampleStatusPayload is a trimmed live get_status OK frame (completed_missions
// shortened; otherwise field-for-field as the server sends it).
//
// standings carries all 14 counterparties the server tracks — the five empires
// plus one per pirate stronghold — because that count is what drives the
// two-column layout. pirate_voss carries an outstanding bounty so the "!" suffix
// stays covered.
const sampleStatusPayload = `{"modules":[{"id":"5534fe2d520212ac4fa14e8ab18f5740","name":"Shield Booster II"}],"player":{"citizenships":["nebula"],"completed_missions":{"a_complimentary_bottle":"2026-06-09T09:03:10Z","a_word_in_private":"2026-04-13T20:00:36Z"},"created_at":"2026-02-08T08:50:13Z","credits":1420770,"current_poi":"cargo_lanes_gas_cloud","current_ship_id":"5d8fcb3546e1f9639649f08780bef3be","current_system":"cargo_lanes","empire":"nebula","faction_id":"e727c0e918d994c72db2978fe5b18edc","faction_rank":"leader","home_base":"grand_exchange_station","id":"a50924913cef881c5e4d14257589d9ba","revealed_pois":{"prismatic_gas_pocket":true,"rainbow_nebulite_vein":true,"wh_entrance_826d1efd":true,"wh_exit_5d528b92":true,"wh_exit_826d1efd":true,"wh_exit_a626d806":true},"skills":{"corporation_management":9,"crafting":26,"deep_core_mining":17,"drone_control":20,"engineering":18,"exploration":3,"mining":13,"navigation":12,"nebula_attunement":2,"piloting":28,"refining":19,"scanning":12,"smuggling":4,"trading":13},"standings":{"crimson":{"baseline":10,"outstanding_bounty":0,"reputation":10},"nebula":{"baseline":20,"outstanding_bounty":0,"reputation":26},"outerrim":{"baseline":10,"outstanding_bounty":0,"reputation":10},"pirate_crix":{"baseline":10,"outstanding_bounty":0,"reputation":8},"pirate_dross":{"baseline":10,"outstanding_bounty":0,"reputation":10},"pirate_kael":{"baseline":10,"outstanding_bounty":0,"reputation":11},"pirate_korr":{"baseline":10,"outstanding_bounty":0,"reputation":10},"pirate_mera":{"baseline":10,"outstanding_bounty":0,"reputation":10},"pirate_nyx":{"baseline":10,"outstanding_bounty":0,"reputation":10},"pirate_sable":{"baseline":10,"outstanding_bounty":0,"reputation":10},"pirate_thane":{"baseline":10,"outstanding_bounty":0,"reputation":10},"pirate_voss":{"baseline":10,"outstanding_bounty":2500,"reputation":4},"solarian":{"baseline":10,"outstanding_bounty":0,"reputation":10},"voidborn":{"baseline":10,"outstanding_bounty":0,"reputation":10}},"stats":{"chat_messages_sent":13,"credits_earned":6208096,"credits_gifted":400000,"credits_spent":4278319,"customs_evaded":2,"damage_dealt":20,"damage_taken":822,"deaths_by_pirate":3,"deep_core_pois_discovered":6,"distance_traveled":256813,"exchange_credits_earned":5067143,"exchange_items_bought":10154,"exchange_items_sold":14551,"facilities_built":10,"gifts_received":4,"gifts_sent":13,"items_crafted":29225,"jumps_completed":487,"missions_abandoned":4,"missions_accepted":44,"missions_completed":32,"ore_mined":1008544,"pirates_destroyed":0,"scans_performed":4,"ships_lost":3,"systems_explored":155,"time_played":3642540,"times_docked":230,"trades_completed":598,"wormholes_traversed":1,"wreck_items_looted":25},"username":"Arthur 'Artificer' Artis"},"ship":{"class_id":"arbitrage","id":"5d8fcb3546e1f9639649f08780bef3be","name":"Arbitrage"}}`

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

	// Own empire must sort ahead of the others in the Reputation box. Scope the
	// search to that box: "nebula" also appears in the header's Empire field and
	// in the nebula_attunement skill, so an unscoped Index would pass no matter
	// how the box were ordered.
	repBox := boxBody(out, "Reputation")
	if repBox == "" {
		t.Fatal("no Reputation box in output")
	}
	if i, j := strings.Index(repBox, "nebula"), strings.Index(repBox, "crimson"); i < 0 || j < 0 || i > j {
		t.Errorf("expected own empire (nebula) before crimson in reputation box, got:\n%s", repBox)
	}
	// An outstanding bounty is flagged inline.
	if !strings.Contains(repBox, "!2500") {
		t.Errorf("expected outstanding bounty marker in reputation box, got:\n%s", repBox)
	}
}

// boxBody returns the text of the titled box, from its header line to its
// closing border. Used to assert on one box without matching text elsewhere in
// the dashboard.
func boxBody(out, title string) string {
	start := strings.Index(out, "┌─ "+title+" ")
	if start < 0 {
		return ""
	}
	end := strings.Index(out[start:], "┘")
	if end < 0 {
		return out[start:]
	}
	return out[start : start+end+len("┘")]
}

// The Reputation box grew from 6 rows to 14 when the server split pirate
// standings per stronghold, which pushed the right column well past the left.
// Missions moved to the left column to compensate; this pins that balance so a
// future box addition does not silently re-skew the dashboard.
func TestFormatGetStatusColumnBalance(t *testing.T) {
	out := formatGetStatus([]byte(sampleStatusPayload))

	// Missions must render in the left column, i.e. flush at column 0.
	if !strings.Contains(out, "\n┌─ Missions ") {
		t.Error("Missions box is not flush left")
	}
	// Reputation and Skills stay on the right, i.e. indented past the left column.
	for _, title := range []string{"Reputation", "Skills"} {
		if strings.Contains(out, "\n┌─ "+title+" ") {
			t.Errorf("%s box should be in the right column, found it flush left", title)
		}
	}

	// Neither column should overhang the other by more than a box's worth of
	// rows. Measure by the last line that carries content in each column.
	var lastLeft, lastRight int
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, "│") || strings.HasPrefix(line, "┌") || strings.HasPrefix(line, "└") {
			lastLeft = i
		}
		if runewidth.StringWidth(line) > statusLeftW+statusGap {
			lastRight = i
		}
	}
	if d := lastLeft - lastRight; d < -6 || d > 6 {
		t.Errorf("columns unbalanced by %d rows (left ends %d, right ends %d):\n%s",
			d, lastLeft, lastRight, out)
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
