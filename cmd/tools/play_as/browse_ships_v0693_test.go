package main

import (
	"strings"
	"testing"
)

// v0.693.0 all_stations=true: listings from many stations, each naming its own.
func TestFormatBrowseShipsAllStationsShowsStation(t *testing.T) {
	raw := `{"listings":[
		{"listing_id":"l1","ship_name":"Rust Bucket","class_id":"cobble","tier":1,"price":9000,"seller":"x","base_id":"haven_b","base_name":"Grand Exchange"},
		{"listing_id":"l2","ship_name":"Swift","class_id":"survey_vessel","tier":2,"price":4000,"seller":"y","base_id":"sol_b","base_name":"Sol Central"}]}`
	got := formatBrowseShips([]byte(raw))
	for _, want := range []string{"across charted stations (2)", "Station", "Grand Exchange", "Sol Central"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Index(got, "Swift") > strings.Index(got, "Rust Bucket") {
		t.Errorf("not cheapest first:\n%s", got)
	}
}

// A single-station reply keeps the old layout, no Station column.
func TestFormatBrowseShipsSingleStationUnchanged(t *testing.T) {
	raw := `{"base_name":"Sol Central","listings":[{"listing_id":"l2","ship_name":"Swift","class_id":"survey_vessel","price":4000,"base_name":"Sol Central"}]}`
	got := formatBrowseShips([]byte(raw))
	if !strings.Contains(got, `Station:  "Sol Central"`) || strings.Contains(got, "| Station") {
		t.Errorf("single-station layout changed:\n%s", got)
	}
}
