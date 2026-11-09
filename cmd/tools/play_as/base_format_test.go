package main

import (
	"os"
	"strings"
	"testing"
)

// A get_base reply shaped like Conjunction Junction's: base.fuel/max_fuel carry
// the tank, so fuel sits at 39%.
const cjBaseReply = `{"action":"get_base","base":{"id":"c976fa7df53c87f9616165d7d411b41a","name":"Conjunction Junction",
"hull":85000,"max_hull":85000,"shield":12000,"max_shield":12000,"weapon_dps":30,"weapon_reach":1,"armor":60,
"fuel":21293,"max_fuel":55000},"condition":{"condition":"thriving","satisfaction_pct":100},
"dining_points":0,"leisure_points":2,"tourism_revenue":7638,
"life_support":{"supply":12,"demand":10},
"power":{"supply":40,"current_draw":0,"battery_capacity":0,"battery_stored":0}}`

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../pkg/assets/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestFormatBase_CJSections(t *testing.T) {
	got := formatBase([]byte(cjBaseReply), readFixture(t, "cj_facility_list.json"))
	for _, want := range []string{
		"Conjunction Junction — THRIVING (satisfaction 100%)", "STRAINED",
		"Hull", "85,000 / 85,000", "armor 60",
		"Shield", "12,000 / 12,000", "1 shell",
		"30 dps", "reach 1", "point_defense_battery L1",
		"battery none", "12 / 10 (120%)",
		"21,293 / 55,000 (39%)", "fuel 39% < 40%",
		"7,638 cr lifetime", "leisure 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatBase_IgnoresOtherBaseFacilities(t *testing.T) {
	other := strings.Replace(string(readFixture(t, "cj_facility_list.json")),
		"c976fa7df53c87f9616165d7d411b41a", "some_other_base", 1)
	got := formatBase([]byte(cjBaseReply), []byte(other))
	if strings.Contains(got, "point_defense_battery") {
		t.Fatalf("another base's facilities must not show:\n%s", got)
	}
	if got := formatBase([]byte(cjBaseReply), nil); !strings.Contains(got, "Hull") {
		t.Fatalf("a nil facility list must still render:\n%s", got)
	}
}

func TestFormatBase_BacklogAndMaintenance(t *testing.T) {
	fac := `{"base_id":"c976fa7df53c87f9616165d7d411b41a","faction_facilities":[
{"facility_id":"f1","type":"iron_refinery","name":"Iron Refinery","category":"production","level":1,
"production":{"recipe":"r","queued_runs":3,"queued_items":3,"backlog_ticks":120,"items_per_hour":1}},
{"facility_id":"f2","type":"refueling_dock","name":"Refueling Dock","category":"service","level":1,"maintenance_satisfied":false}]}`
	got := formatBase([]byte(cjBaseReply), []byte(fac))
	for _, want := range []string{"Production  Iron Refinery: 3 queued runs, 120 ticks backlog", "Maintenance refueling_dock L1: unmet"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// Without a base id the reply is not a base, so the plain JSON view stands.
func TestFormatBase_DeclinesNonBase(t *testing.T) {
	if got := formatBase([]byte(`{"base":{"name":"Conjunction Junction"}}`), nil); got != "" {
		t.Fatalf("want no styled view, got:\n%s", got)
	}
}

func TestFormatBase_MissingDenominatorsShowDash(t *testing.T) {
	reply := `{"action":"get_base","base":{"id":"b1","name":"Bare","hull":5,"shield":3,"fuel":2},
"condition":{"condition":"thriving","satisfaction_pct":100},
"life_support":{"supply":1,"demand":0},"power":{"supply":0,"current_draw":0}}`
	got := formatBase([]byte(reply), nil)
	if !strings.Contains(got, "(—)") {
		t.Errorf("want dash for absent percentages in:\n%s", got)
	}
	if strings.Contains(got, "(0%)") {
		t.Errorf("zero denominators must not print 0%% in:\n%s", got)
	}
}
