package main

import (
	"strings"
	"testing"
)

// `facility owned` rows carry no "active" field, so a healthy facility must not
// read as idle.
func TestFormatFacilityOwned_HealthyIsActive(t *testing.T) {
	got := formatFacilityOwned([]byte(`{"facilities":[
	  {"facility_id":"f1","name":"Mill","type":"iron_mill","base_name":"CJ","rent_per_cycle":10}]}`))
	if strings.Contains(got, "idle") {
		t.Fatalf("a healthy facility must not read idle:\n%s", got)
	}
	if !strings.Contains(got, "active") {
		t.Fatalf("want active:\n%s", got)
	}
}

func TestFormatFacilityOwned_ShowsTheProblem(t *testing.T) {
	cases := map[string]string{
		`"damaged":true`: "damaged",
		`"damaged":true,"repair_complete_tick":2350000`: "repairing (until tick 2350000)",
		`"power_throttled":true`:                        "power-throttled",
		`"dismantling":true`:                            "dismantling",
		`"under_construction":true`:                     "building",
		`"missed_rent_cycles":3`:                        "3 missed cycle(s)",
	}
	for flags, want := range cases {
		raw := `{"facilities":[{"facility_id":"f1","name":"Mill","type":"iron_mill","base_name":"CJ",` + flags + `}]}`
		if got := formatFacilityOwned([]byte(raw)); !strings.Contains(got, want) {
			t.Errorf("%s: want %q in:\n%s", flags, want, got)
		}
	}
}

func TestFormatFacilityOwned_ShowsIDAndArrears(t *testing.T) {
	got := formatFacilityOwned([]byte(`{"facilities":[
	  {"facility_id":"abc123","name":"Mill","type":"iron_mill","base_name":"CJ","arrears_owed":450}]}`))
	for _, want := range []string{"abc123", "450"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}

func TestFormatFacilityList_FactionRowShowsDamage(t *testing.T) {
	got := formatFacilityList([]byte(`{"base_id":"cj","faction_facilities":[
	  {"facility_id":"f1","name":"Locker","type":"locker_bay","faction_service":"storage","damaged":true}]}`))
	if !strings.Contains(got, "damaged") {
		t.Fatalf("a damaged faction facility must say so:\n%s", got)
	}
}

func TestFormatFacilityList_PersonalRowHasNoFalseInactive(t *testing.T) {
	got := formatFacilityList([]byte(`{"base_id":"cj","player_facilities":[
	  {"facility_id":"p1","name":"Quarters","type":"crew_quarters","personal_service":"quarters","rent_per_cycle":5},
	  {"facility_id":"p2","name":"Bunk","type":"crew_quarters","personal_service":"quarters","power_throttled":true}]}`))
	if !strings.Contains(got, "power-throttled") {
		t.Fatalf("want the throttled row flagged:\n%s", got)
	}
	if strings.Contains(got, "| no ") {
		t.Fatalf("a row without the active field must not print no:\n%s", got)
	}
}

func TestFormatFacilityList_StationRowHasNoFalseInactive(t *testing.T) {
	showStationFacilities = true
	defer func() { showStationFacilities = false }()
	got := formatFacilityList([]byte(`{"base_id":"cj","station_facilities":[
	  {"facility_id":"s1","name":"Dock","type":"dock","category":"service","service":"refuel"},
	  {"facility_id":"s2","name":"Yard","type":"yard","category":"service","service":"shipyard","dismantling":true}]}`))
	if strings.Contains(got, "inactive") {
		t.Fatalf("a row without the active field must not print inactive:\n%s", got)
	}
	if !strings.Contains(got, "dismantling") {
		t.Fatalf("want the dismantling row flagged:\n%s", got)
	}
}

func TestFormatFacilityList_FactionProductionShowsDamage(t *testing.T) {
	got := formatFacilityList([]byte(`{"base_id":"cj","faction_facilities":[
	  {"facility_id":"f1","name":"Mill","type":"iron_mill","damaged":true,
	   "production":{"recipe_id":"refine_steel"}}]}`))
	if !strings.Contains(got, "Mill ⚠ damaged") {
		t.Fatalf("a damaged production line must say so:\n%s", got)
	}
}
