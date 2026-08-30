package main

import (
	"strings"
	"testing"
	"time"
)

// A tau_bootis-shaped reply, 2026-08-30: ship wrecks carry module OBJECTS
// (which the old []string field failed to parse, silently dumping 45 wrecks
// as raw JSON), killers, and timestamps. One towed wreck has no killer.
func sampleWrecksJSON(t *testing.T) []byte {
	t.Helper()
	now := time.Now().UTC()
	old := now.Add(-3 * time.Hour).Format(time.RFC3339)
	fresh := now.Add(-2 * time.Minute).Format(time.RFC3339)
	return []byte(`{"count":4,"wrecks":[
 {"id":"609ef19c7495cd0192916408c7b433c3","type":"ship","ship_class":"prospect","victim_name":"VoidMoth_Juno",
  "killer_name":"MoltenOne","salvage_value":2684,"created_at":"` + old + `",
  "cargo":[{"item_id":"iron_ore","quantity":30},{"item_id":"copper_ore","quantity":20},{"item_id":"gold_ore","quantity":11}],
  "modules":[{"id":"e26747","name":"Mining Laser I","type":"mining","type_id":"mining_laser_i"},
             {"id":"92ce3c","name":"Pulse Laser I","type":"weapon","type_id":"pulse_laser_i"}]},
 {"id":"3e2b97b8b8d2ac09ed395773353116f2","type":"ship","ship_class":"underwriter","victim_name":"MoltenOne",
  "killer_name":"zh0ul-001","salvage_value":4290,"created_at":"` + fresh + `","cargo":null,"modules":[]},
 {"id":"70a7b9176d384ca5590138d96e8e133d","type":"ship","ship_class":"bulk_terms","victim_name":"storgio14",
  "salvage_value":3042,"towed_by_player_id":"f9f513","created_at":"2026-05-16T13:03:44Z","cargo":[],"modules":[]},
 {"id":"cansiter1","type":"jettison","victim_name":"Dropper",
  "cargo":[{"item_id":"iron_ore","quantity":5}],"modules":[]}]}`)
}

func TestFormatWrecks_CompactTable(t *testing.T) {
	out := formatWrecks(sampleWrecksJSON(t))
	if out == "" {
		t.Fatal("formatWrecks returned \"\" (parse failure) — module objects must decode")
	}
	if !strings.Contains(out, "Ship Wrecks: 3") {
		t.Errorf("missing ship count header:\n%s", out)
	}
	// One compact row per ship wreck: id, salvage, class, elided cargo and
	// module lists (first two entries, then +N).
	for _, want := range []string{
		"prospect", "underwriter", "bulk_terms (towed)",
		"2,684", "4,290", "609ef19c7495cd0192916408c7b433c3",
		"iron_ore 30, copper_ore 20, +1", "Mining Laser I, Pulse Laser I",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// Ordered by salvage value, richest first: 4,290 > 3,042 > 2,684.
	if strings.Index(out, "underwriter") > strings.Index(out, "prospect") ||
		strings.Index(out, "bulk_terms") > strings.Index(out, "prospect") {
		t.Errorf("rows not salvage-ordered:\n%s", out)
	}
	// Compact means bounded rows: id+salvage+class+two elided lists must fit
	// a wide terminal line.
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 140 {
			t.Errorf("line over 140 chars (%d): %q", len(line), line)
		}
	}
	// The jettison branch keeps its actionable loot lines.
	if !strings.Contains(out, "loot_wreck cansiter1 iron_ore 5") {
		t.Errorf("jettison loot line lost:\n%s", out)
	}
}

func TestFormatWrecks_Empty(t *testing.T) {
	if got := formatWrecks([]byte(`{"count":0,"wrecks":[]}`)); got != "No wrecks found." {
		t.Errorf("empty = %q", got)
	}
}
