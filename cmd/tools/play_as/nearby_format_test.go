package main

import (
	"strings"
	"testing"
)

// liveNearbyWithCreatures is a real get_nearby payload captured from craftsman-1
// at commerce_fields on 2026-08-08, trimmed to the creature-bearing parts.
//
// It is verbatim from the wire on purpose. An invented fixture is what let the
// wrong field names (`is_aggressive`, `status`) into a parallel piece of work —
// the server actually sends `in_combat` and `role`.
// (creature_count is 2 here, not the original 3, because the array was trimmed
// from three Ash-Scarabs to two; the two must agree or the header lies.)
const liveNearbyWithCreatures = `{"count":5,"creature_count":2,"creatures":[
{"creature_id":"crt_bb95d6cc79790d9905fcbba9eaa0fe02","hull":45,"in_combat":false,"max_hull":45,"name":"Ash-Scarab","role":"scavenger","species":"ash_scarab"},
{"creature_id":"crt_0ead63b7f4e62f61d92fe5a93cb66986","hull":40,"in_combat":true,"max_hull":45,"name":"Ash-Scarab","role":"scavenger","species":"ash_scarab"}],
"nearby":[],"pirate_count":0,"pirates":[],"poi_id":"commerce_fields"}`

// get_nearby listed players and pirates but silently dropped wildlife, so a
// belt full of quarry rendered as an empty POI.
func TestFormatNearbyRendersCreatures(t *testing.T) {
	out := formatNearby([]byte(liveNearbyWithCreatures))
	if out == "" {
		t.Fatal("formatNearby returned empty")
	}
	for _, want := range []string{
		"Creatures:  2",
		"Ash-Scarab",
		"ash_scarab",
		"scavenger",
		"45/45",
		"[in combat]", // the second creature is fighting
		// The creature id is the argument `hunt` takes, so it must appear in
		// full — a truncated id cannot be copied into a command.
		"crt_bb95d6cc79790d9905fcbba9eaa0fe02",
		"crt_0ead63b7f4e62f61d92fe5a93cb66986",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

// A POI with no wildlife must still say so rather than omitting the section,
// which is the difference between "nothing here" and "we did not look".
func TestFormatNearbyNoCreatures(t *testing.T) {
	out := formatNearby([]byte(`{"poi_id":"sol_station","count":0,"nearby":[],"pirates":[]}`))
	if !strings.Contains(out, "Creatures:  0") {
		t.Errorf("expected an explicit zero creature count, got:\n%s", out)
	}
}

// The count falls back to the slice length, so a server that sends creatures
// without creature_count still reports the right number.
func TestFormatNearbyCreatureCountFallback(t *testing.T) {
	const body = `{"poi_id":"p","nearby":[],"pirates":[],"creatures":[
	  {"creature_id":"c1","name":"Grazer","hull":10,"max_hull":10}]}`
	if out := formatNearby([]byte(body)); !strings.Contains(out, "Creatures:  1") {
		t.Errorf("expected fallback to the slice length, got:\n%s", out)
	}
}
