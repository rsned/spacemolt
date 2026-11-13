package main

import (
	"strings"
	"testing"
)

// liveLocationWithStars is the tail of a real get_location reply at
// Cloverfield (2026-11-13), trimmed to five of its fifteen star_sightings.
// Field names are copied from the payload: bearing, brightness, temperature.
const liveLocationWithStars = `{"message":"Current location","location":{
"poi_id":"cloverfield_station","poi_name":"Conjunction Junction","poi_type":"station",
"system_id":"cloverfield","system_name":"Cloverfield",
"nearby_player_count":0,"nearby_empire_npc_count":0,"nearby_pirate_count":0,
"star_sightings":[
{"bearing":165,"brightness":5,"temperature":26000},
{"bearing":105,"brightness":4,"temperature":4000},
{"bearing":330,"brightness":4,"temperature":14000},
{"bearing":50,"brightness":5,"temperature":26000},
{"bearing":125,"brightness":3,"temperature":60000}]}}`

func TestFormatLocationRendersStarSightings(t *testing.T) {
	out := formatGetLocation([]byte(liveLocationWithStars))
	if !strings.Contains(out, "Star sightings: 5") {
		t.Fatalf("no star sightings heading, got:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	var rows []string
	for _, l := range lines {
		if strings.HasSuffix(strings.TrimSpace(l), "K") && strings.Contains(l, "°") {
			rows = append(rows, l)
		}
	}
	if len(rows) != 5 {
		t.Fatalf("got %d star rows, want 5:\n%s", len(rows), out)
	}
	// Brightest first, then by bearing: the 50° and 165° brightness-5 stars lead.
	for i, want := range []string{"50°", "165°", "105°", "330°", "125°"} {
		if !strings.Contains(rows[i], want) {
			t.Errorf("row %d = %q, want bearing %s", i, rows[i], want)
		}
	}
	if !strings.Contains(rows[4], "60000 K") {
		t.Errorf("last row %q missing temperature", rows[4])
	}
}

func TestFormatLocationNoStarSightingsNoHeading(t *testing.T) {
	out := formatGetLocation([]byte(liveLocationWithResources))
	if strings.Contains(out, "Star sightings") {
		t.Errorf("heading printed with no sightings:\n%s", out)
	}
}
