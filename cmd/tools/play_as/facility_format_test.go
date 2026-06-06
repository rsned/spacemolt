package main

import (
	"strings"
	"testing"
)

// TestFormatFacilityFactionList_ShowsFacilityID guards that the facility_id
// (needed as an argument to other facility commands) appears in the rendered
// faction-facility table. Uses the real server shape.
func TestFormatFacilityFactionList_ShowsFacilityID(t *testing.T) {
	raw := []byte(`{"base_id":"grand_exchange_station","faction_id":"e727c0e918d994c72db2978fe5b18edc","faction_facilities":[` +
		`{"active":true,"category":"faction","facility_id":"57cc8b5caf0bed5c3830a285443c1219","faction_service":"faction_intel","name":"Intel Terminal","type":"intel_terminal"},` +
		`{"active":true,"category":"faction","facility_id":"077a6773b401e4bd4737ab9649726f12","faction_service":"faction_admin","name":"Faction Desk","type":"faction_desk"}` +
		`]}`)
	out := formatFacilityFactionList(raw)
	if !strings.Contains(out, "Facility ID") {
		t.Errorf("header missing 'Facility ID' column:\n%s", out)
	}
	for _, id := range []string{"57cc8b5caf0bed5c3830a285443c1219", "077a6773b401e4bd4737ab9649726f12"} {
		if !strings.Contains(out, id) {
			t.Errorf("output missing facility_id %s:\n%s", id, out)
		}
	}
}

// TestFormatFacilityList_ShowsFacilityID guards the facility_id column in both
// the Personal and Faction sections of a plain `facility list`.
func TestFormatFacilityList_ShowsFacilityID(t *testing.T) {
	raw := []byte(`{"base_id":"grand_exchange_station",` +
		`"player_facilities":[{"active":true,"facility_id":"aaa111bbb222ccc333ddd444eee555ff","name":"Faction Quarters","personal_service":"faction_commons","rent_per_cycle":120,"rent_paid_until_tick":964000,"type":"faction_quarters"}],` +
		`"faction_facilities":[{"active":true,"facility_id":"57cc8b5caf0bed5c3830a285443c1219","faction_service":"faction_intel","name":"Intel Terminal","type":"intel_terminal"}]}`)
	out := formatFacilityList(raw)
	if strings.Count(out, "Facility ID") != 2 {
		t.Errorf("expected a 'Facility ID' column header in both sections:\n%s", out)
	}
	for _, id := range []string{"aaa111bbb222ccc333ddd444eee555ff", "57cc8b5caf0bed5c3830a285443c1219"} {
		if !strings.Contains(out, id) {
			t.Errorf("output missing facility_id %s:\n%s", id, out)
		}
	}
}

// TestFormatFacilityList_StationFacilitiesToggle guards that station-owned
// facilities are hidden by default and only rendered when the
// --show_station_facilities flag (showStationFacilities) is set.
func TestFormatFacilityList_StationFacilitiesToggle(t *testing.T) {
	raw := []byte(`{"base_id":"voss_redoubt_station","player_facilities":[],"faction_facilities":[],"station_facilities":[` +
		`{"active":true,"category":"service","facility_id":"a961e393a55e9f58961577940bf0a6ba","level":2,"maintenance_satisfied":false,"name":"Maintenance Deck","service":"repair","type":"maintenance_deck"},` +
		`{"active":true,"category":"production","facility_id":"77c6d42fa7fcfb7bfad637d26d1a5e7e","idle_reason":"no_inputs","level":1,"maintenance_satisfied":true,"name":"Fuel Reclamation Still","recipe_id":"scavenge_fuel_cells","type":"fuel_reclamation_still"}]}`)

	// Default: station facilities hidden, and with no player/faction facilities
	// the list reports none.
	showStationFacilities = false
	out := formatFacilityList(raw)
	if strings.Contains(out, "Station (") || strings.Contains(out, "Maintenance Deck") {
		t.Errorf("station facilities should be hidden by default:\n%s", out)
	}
	if !strings.Contains(out, "(no facilities)") {
		t.Errorf("expected '(no facilities)' when only station facilities exist and toggle off:\n%s", out)
	}

	// Toggle on: station section renders with status (idle reason) surfaced.
	showStationFacilities = true
	defer func() { showStationFacilities = false }()
	out = formatFacilityList(raw)
	for _, want := range []string{"Station (2):", "Maintenance Deck", "Fuel Reclamation Still", "idle: no_inputs"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q with toggle on:\n%s", want, out)
		}
	}
}

// TestFormatFacilityForSale_Empty renders the no-listings case for
// `facility browse_for_sale` rather than falling back to a stale facility list.
func TestFormatFacilityForSale_Empty(t *testing.T) {
	raw := []byte(`{"action":"browse_for_sale","base_id":"grand_exchange_station","base_name":"Grand Exchange Station","count":0,"listings":[]}`)
	out := formatFacility(raw)
	if !strings.Contains(out, "No facilities listed for sale at Grand Exchange Station") {
		t.Errorf("empty browse_for_sale should report none for sale, got:\n%s", out)
	}
	if strings.Contains(out, "Personal:") || strings.Contains(out, "Faction:") {
		t.Errorf("must not render a facility list for browse_for_sale:\n%s", out)
	}
}

// TestFormatFacilityForSale_Listings renders listings generically, surfacing the
// facility_id (needed by buy_listing/cancel_listing) first.
func TestFormatFacilityForSale_Listings(t *testing.T) {
	raw := []byte(`{"action":"browse_for_sale","base_name":"Grand Exchange Station","count":1,"listings":[` +
		`{"facility_id":"57cc8b5caf0bed5c3830a285443c1219","type":"workbench","name":"Workbench","price":5000,"seller":"craftsman-1"}]}`)
	out := formatFacility(raw)
	for _, want := range []string{
		"Facilities for sale at Grand Exchange Station (1)",
		"facility_id: 57cc8b5caf0bed5c3830a285443c1219",
		"name: Workbench",
		"seller: craftsman-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
