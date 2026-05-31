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
