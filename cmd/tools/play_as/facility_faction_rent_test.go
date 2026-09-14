package main

import (
	"strings"
	"testing"
)

// realFactionListGrandExchange is the `facility faction_list` reply for
// grand_exchange_station as rendered on 2026-09-14: ten facilities, five of
// them production hulls at 319/cycle and five services at 159/cycle.
//
// The `facility faction_list` reply carries NO rent summary block, so the total
// has to be summed client-side from the rows. (`facility list` DOES get a
// server-computed faction_rent block — see the tests below.) These ten come to
// 2,390/cycle, which the server prices at 205,540 credits a day.
const realFactionListGrandExchange = `{
  "action": "facility",
  "base_id": "grand_exchange_station",
  "faction_id": "e727c0e918d994c72db2978fe5b18edc",
  "faction_storage": {"credits": 174681, "item_types": 23, "rooms": 0},
  "faction_facilities": [
    {"active":true,"name":"Carbon Arc Furnace","type":"carbon_arc_furnace","level":1,"status":"active","capacity":0,"rent_per_cycle":319,"facility_id":"15abecf75faf6bd9996fb2d9b859c344"},
    {"active":true,"name":"Faction Fuel Bunker","type":"faction_fuel_bunker","faction_service":"faction_fuel","level":1,"status":"active","capacity":0,"rent_per_cycle":159,"facility_id":"eac00e373bbc3cdc95be2192232ad29a"},
    {"active":true,"name":"Faction Lockbox","type":"faction_lockbox","faction_service":"faction_storage","level":1,"status":"active","capacity":100000,"rent_per_cycle":159,"facility_id":"a5352d3495b87650514bea013e9ae658"},
    {"active":true,"name":"Faction Office","type":"faction_office","faction_service":"faction_admin","level":2,"status":"active","capacity":0,"rent_per_cycle":159,"facility_id":"d5b180bb72022c2e496fa27b20f865b6"},
    {"active":true,"name":"Faction Warehouse","type":"faction_warehouse","faction_service":"faction_storage","level":2,"status":"active","capacity":200000,"rent_per_cycle":159,"facility_id":"c21d6929af60f4724237785d12981fd2"},
    {"active":true,"name":"Fiber Draw Tower","type":"fiber_draw_tower","level":1,"status":"active","capacity":0,"rent_per_cycle":319,"facility_id":"d0b5b0d4f139105afba2d8e6f758163a"},
    {"active":true,"name":"H2 Fuel Combustor","type":"h2_fuel_combustor","level":1,"status":"active","capacity":0,"rent_per_cycle":319,"facility_id":"fb24fd71ecb5590893fa64e8ba9bf3ed"},
    {"active":true,"name":"Intel Terminal","type":"intel_terminal","faction_service":"faction_intel","level":1,"status":"active","capacity":0,"rent_per_cycle":159,"facility_id":"57cc8b5caf0bed5c3830a285443c1219"},
    {"active":true,"custom_name":"Bob's Iron Smeltery","name":"Iron Refinery","type":"iron_refinery","level":1,"status":"active","capacity":0,"rent_per_cycle":319,"facility_id":"f9ef7a1940efe4484fefb2e393209a53"},
    {"active":true,"name":"Polymer Extruder","type":"polymer_extruder","level":1,"status":"active","capacity":0,"rent_per_cycle":319,"facility_id":"b659c3602da933e414c9fa91a072968a"}
  ]
}`

// TestFormatFacilityFactionList_TotalsRent is the ask: `facility list` sums
// personal rent, `facility faction_list` did not. Five production hulls at 319
// plus five services at 159 is 2,390/cycle and 205,540/day.
func TestFormatFacilityFactionList_TotalsRent(t *testing.T) {
	out := formatFacilityFactionList([]byte(realFactionListGrandExchange))
	for _, want := range []string{
		"2390",    // per-cycle total
		"205,540", // per-day total, thousands-separated like the table cells
		"10 facilities",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

// The personal summary says "all your facilities" because the server computes
// it across every station. Ours is summed from one station's rows, so it must
// not claim to be fleet-wide.
func TestFormatFacilityFactionList_RentTotalIsScopedToThisStation(t *testing.T) {
	out := formatFacilityFactionList([]byte(realFactionListGrandExchange))
	if !strings.Contains(out, "at this station") {
		t.Errorf("rent total does not say it covers only this station:\n%s", out)
	}
}

// A faction with no facilities must not grow a 0/cycle summary line.
func TestFormatFacilityFactionList_NoFacilitiesNoRentLine(t *testing.T) {
	out := formatFacilityFactionList([]byte(
		`{"base_id":"b","faction_id":"f","faction_facilities":[]}`))
	if strings.Contains(out, "Rent") {
		t.Errorf("rendered a rent summary for a faction with no facilities:\n%s", out)
	}
}

// Rows whose rent the server omits must not be silently counted as free: the
// facility count in the summary has to match what was actually summed.
func TestFormatFacilityFactionList_CountsOnlyRentedRows(t *testing.T) {
	out := formatFacilityFactionList([]byte(
		`{"base_id":"b","faction_id":"f","faction_facilities":[` +
			`{"name":"A","type":"a","rent_per_cycle":159,"facility_id":"a1"},` +
			`{"name":"B","type":"b","facility_id":"b1"}]}`))
	if !strings.Contains(out, "1 facility") {
		t.Errorf("a row with no rent was counted as rented:\n%s", out)
	}
}

// The Faction section of a plain `facility list` shows the same table, so it
// gets the same total — the two views are meant to stay visually consistent.
func TestFormatFacilityList_FactionSectionTotalsRent(t *testing.T) {
	raw := []byte(`{"base_id":"grand_exchange_station","faction_facilities":[` +
		`{"name":"Intel Terminal","type":"intel_terminal","faction_service":"faction_intel","rent_per_cycle":159,"facility_id":"57cc8b5caf0bed5c3830a285443c1219"},` +
		`{"name":"Polymer Extruder","type":"polymer_extruder","rent_per_cycle":319,"facility_id":"b659c3602da933e414c9fa91a072968a"}]}`)
	out := formatFacilityList(raw)
	if !strings.Contains(out, "478") {
		t.Errorf("faction section is missing the 478/cycle total:\n%s", out)
	}
	if !strings.Contains(out, "2 facilities") {
		t.Errorf("faction section is missing the facility count:\n%s", out)
	}
}

// TestDailyRent_MatchesTheServer pins the cycles-per-day constant against two
// independently observed server figures from grand_exchange_station on
// 2026-09-14: player_rent said 159/cycle is 13,674/day, and faction_rent said
// 2,390/cycle is 205,540/day. Both are exactly ×86.
//
// We had been using 86.4 (86,400s/day ÷ a 1000s cycle), which overstated every
// Rent/day cell by 0.47%. The server evidently floors the cycle count.
func TestDailyRent_MatchesTheServer(t *testing.T) {
	for _, tc := range []struct {
		perCycle, wantPerDay int64
	}{
		{159, 13674},
		{2390, 205540},
	} {
		if got := dailyRent(tc.perCycle); got != tc.wantPerDay {
			t.Errorf("dailyRent(%d) = %d, want %d (server-observed)",
				tc.perCycle, got, tc.wantPerDay)
		}
	}
}

// realFacilityListFactionRent is the faction_rent block `facility list` carries
// for grand_exchange_station. It is server-computed and authoritative — its
// note says totals EXCLUDE facilities whose rent is paused while damaged, under
// construction or dismantling, which a naive client-side sum of the rows cannot
// know. So when the block is present it must win over our own arithmetic.
const realFacilityListFactionRent = `{
  "base_id": "grand_exchange_station",
  "faction_facilities": [
    {"name":"Carbon Arc Furnace","type":"carbon_arc_furnace","rent_per_cycle":319,"facility_id":"15abecf75faf6bd9996fb2d9b859c344"},
    {"name":"Intel Terminal","type":"intel_terminal","faction_service":"faction_intel","rent_per_cycle":159,"facility_id":"57cc8b5caf0bed5c3830a285443c1219"}
  ],
  "faction_rent": {"est_rent_per_day": 205540, "facilities": 10, "total_rent_per_cycle": 2390, "grace_cycles": 260}
}`

func TestFormatFacilityList_PrefersServerFactionRent(t *testing.T) {
	out := formatFacilityList([]byte(realFacilityListFactionRent))
	// The server's figures cover all 10 facilities at the station; the two rows
	// echoed in this reply sum to only 478/cycle. The server must win.
	for _, want := range []string{"2390", "205,540", "10 facilities"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing server-computed %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "478") {
		t.Errorf("client-side sum overrode the server's faction_rent:\n%s", out)
	}
}
