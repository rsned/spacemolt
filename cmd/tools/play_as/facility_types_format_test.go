package main

import (
	"strings"
	"testing"
)

// A bare `facility types` (no filters) gets the server's discovery index:
// per-category counts and how to filter, no types list. Live 2026-10-25.
const facilityTypesDiscovery = `{"action":"types","categories":{"defense":{"buildable":12,"count":14,"description":"defense"},"faction":{"buildable":24,"count":56,"description":"Faction facilities that gate faction features at a station (storage, trading post, etc.)"},"infrastructure":{"buildable":28,"count":103,"description":"Power and life support systems"},"production":{"buildable":816,"count":2625,"description":"Manufacturing facilities that run crafting recipes automatically"},"ranch":{"count":3,"description":"Wildlife ranches"}},"hint":"Use filters to browse.","kind":"discovery","total":2910}`

func TestFormatFacilityTypes_DiscoveryListsCategories(t *testing.T) {
	out := formatFacilityTypes([]byte(facilityTypesDiscovery))
	if strings.Contains(out, "no facility types") {
		t.Fatalf("discovery reply rendered as empty:\n%s", out)
	}
	for _, want := range []string{"infrastructure", "28", "103", "Power and life support systems", "production", "816", "ranch", "Use filters to browse."} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Categories sort alphabetically: defense before ranch.
	if strings.Index(out, "defense") > strings.Index(out, "ranch") {
		t.Errorf("categories not sorted:\n%s", out)
	}
}

func TestFormatFacilityTypes_EmptyListStillSaysNone(t *testing.T) {
	out := formatFacilityTypes([]byte(`{"action":"types","types":[],"total":0}`))
	if !strings.Contains(out, "no facility types") {
		t.Errorf("empty filtered list should say none, got:\n%s", out)
	}
}
