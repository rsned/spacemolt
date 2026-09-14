package main

import (
	"strings"
	"testing"
)

// realFactionOwned is the `facility faction_owned` reply captured live on
// 2026-09-14, the reply that made the API monitor complain the action had no
// serverapi struct at all. Fourteen facilities across two stations totalling
// 4,973/cycle — more than double the 2,390 that grand_exchange_station alone
// reports, which is the whole reason this cross-station view exists.
//
// Note what is NOT here: no "active" field. The old formatter read one, so
// every facility rendered as inactive. Status has to come from the pause flags.
const realFactionOwned = `{
  "action": "faction_owned",
  "faction_id": "e727c0e918d994c72db2978fe5b18edc",
  "total_rent_per_cycle": 4973,
  "facilities": [
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"15abecf75faf6bd9996fb2d9b859c344","labor_per_run":8,"name":"Carbon Arc Furnace","rent_per_cycle":319,"rental_fee_per_run":75,"system_id":"haven","type":"carbon_arc_furnace"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"57cc8b5caf0bed5c3830a285443c1219","labor_per_run":0,"name":"Intel Terminal","rent_per_cycle":159,"system_id":"haven","type":"intel_terminal"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"a5352d3495b87650514bea013e9ae658","labor_per_run":0,"name":"Faction Lockbox","rent_per_cycle":159,"system_id":"haven","type":"faction_lockbox"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"b659c3602da933e414c9fa91a072968a","labor_per_run":8,"name":"Polymer Extruder","rent_per_cycle":319,"rental_fee_per_run":60,"system_id":"haven","type":"polymer_extruder"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"c21d6929af60f4724237785d12981fd2","labor_per_run":0,"name":"Faction Warehouse","rent_per_cycle":159,"system_id":"haven","type":"faction_warehouse"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"d0b5b0d4f139105afba2d8e6f758163a","labor_per_run":7,"name":"Fiber Draw Tower","rent_per_cycle":319,"rental_fee_per_run":20,"system_id":"haven","type":"fiber_draw_tower"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"d5b180bb72022c2e496fa27b20f865b6","labor_per_run":0,"name":"Faction Office","rent_per_cycle":159,"system_id":"haven","type":"faction_office"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"eac00e373bbc3cdc95be2192232ad29a","labor_per_run":0,"name":"Faction Fuel Bunker","rent_per_cycle":159,"system_id":"haven","type":"faction_fuel_bunker"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","custom_name":"Bob's Iron Smeltery","facility_id":"f9ef7a1940efe4484fefb2e393209a53","labor_per_run":10,"name":"Iron Refinery","rent_per_cycle":319,"rental_fee_per_run":3,"system_id":"haven","type":"iron_refinery"},
    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"fb24fd71ecb5590893fa64e8ba9bf3ed","labor_per_run":8,"name":"H2 Fuel Combustor","rent_per_cycle":319,"rental_fee_per_run":40,"system_id":"haven","type":"h2_fuel_combustor"},
    {"base_id":"voss_redoubt_station","base_name":"Voss Redoubt Station","facility_id":"0e95cf157eda387dec55a1e67b5114cc","labor_per_run":15,"name":"Backstreet Chem Lab","rent_per_cycle":738,"rental_fee_per_run":100,"system_id":"alhena","type":"backstreet_chem_lab"},
    {"base_id":"voss_redoubt_station","base_name":"Voss Redoubt Station","facility_id":"73463241439d395a9ec7d6af039c75b9","labor_per_run":15,"name":"Bootleg Cell Shop","rent_per_cycle":738,"rental_fee_per_run":100,"system_id":"alhena","type":"bootleg_cell_shop"},
    {"base_id":"voss_redoubt_station","base_name":"Voss Redoubt Station","facility_id":"d3e782fd18f87f25bf1390ea60746b52","labor_per_run":0,"name":"Faction Lockbox","rent_per_cycle":369,"system_id":"alhena","type":"faction_lockbox"},
    {"base_id":"voss_redoubt_station","base_name":"Voss Redoubt Station","facility_id":"fe73e183f424d7646fe73bfce9a1a081","labor_per_run":10,"name":"Backwater Still","rent_per_cycle":738,"system_id":"alhena","type":"backwater_still"}
  ],
  "note": "Faction facilities pay rent from the treasury each cycle.",
  "hint": "Use action 'faction_list' while docked for full per-facility detail at that station."
}`

// The point of the cross-station view: the faction's whole rent bill. 4,973 a
// cycle is 427,678 a day at the server's 86 cycles.
func TestFormatFacilityFactionOwned_TotalsRent(t *testing.T) {
	out := formatFacilityFactionOwned([]byte(realFactionOwned))
	for _, want := range []string{"4973", "427,678"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

// Rent is per-station in practice — the treasury drains wherever facilities
// sit, and Voss Redoubt is the more expensive half despite having fewer
// facilities. A per-station subtotal is what makes that visible.
func TestFormatFacilityFactionOwned_SubtotalsPerStation(t *testing.T) {
	out := formatFacilityFactionOwned([]byte(realFactionOwned))
	for _, want := range []string{"2390", "2583"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing per-station subtotal %q:\n%s", want, out)
		}
	}
}

// The server sends no "active" field for this action. Reading one made every
// facility render as inactive; status must be derived from the pause flags.
func TestFormatFacilityFactionOwned_NoActiveFieldMeansActive(t *testing.T) {
	out := formatFacilityFactionOwned([]byte(realFactionOwned))
	if strings.Contains(out, "inactive") {
		t.Errorf("a facility with no pause flag rendered as inactive:\n%s", out)
	}
}

func TestFormatFacilityFactionOwned_DerivesPausedStatus(t *testing.T) {
	raw := `{"action":"faction_owned","faction_id":"f","total_rent_per_cycle":0,"facilities":[
	  {"base_id":"b","base_name":"B","facility_id":"1","name":"Wrecked","type":"t","rent_per_cycle":100,"labor_per_run":0,"damaged":true},
	  {"base_id":"b","base_name":"B","facility_id":"2","name":"Building","type":"t","rent_per_cycle":100,"labor_per_run":0,"under_construction":true},
	  {"base_id":"b","base_name":"B","facility_id":"3","name":"Leaving","type":"t","rent_per_cycle":100,"labor_per_run":0,"dismantling":true}]}`
	out := formatFacilityFactionOwned([]byte(raw))
	for _, want := range []string{"damaged", "building", "dismantling"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing status %q:\n%s", want, out)
		}
	}
}

// custom_name is what the operator named it; the generic type name is not.
func TestFormatFacilityFactionOwned_PrefersCustomName(t *testing.T) {
	out := formatFacilityFactionOwned([]byte(realFactionOwned))
	if !strings.Contains(out, "Bob's Iron Smeltery") {
		t.Errorf("custom_name was dropped:\n%s", out)
	}
}

// Arrears are how facilities get repossessed, so they must not be buried.
func TestFormatFacilityFactionOwned_WarnsOnArrears(t *testing.T) {
	raw := `{"action":"faction_owned","faction_id":"f","total_rent_per_cycle":100,"arrears_owed":5000,"grace_cycles":260,
	  "facilities":[{"base_id":"b","base_name":"B","facility_id":"1","name":"N","type":"t","rent_per_cycle":100,"labor_per_run":0,"missed_rent_cycles":3}]}`
	out := formatFacilityFactionOwned([]byte(raw))
	for _, want := range []string{"5000", "260"} {
		if !strings.Contains(out, want) {
			t.Errorf("arrears warning is missing %q:\n%s", want, out)
		}
	}
}
