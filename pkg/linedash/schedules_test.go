package linedash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTaskCraft(t *testing.T) {
	s, ok := ParseTask("craft carbon_arc_circuit_etching 1000 prefer_own")
	if !ok || s.Verb != "craft" || s.Recipe != "carbon_arc_circuit_etching" || s.Qty != 1000 {
		t.Errorf("ParseTask = %+v, %v", s, ok)
	}
}

func TestParseTaskDeliver(t *testing.T) {
	s, ok := ParseTask("deliver oxygen_gas 1900 gold_run_extraction_hub 862e07fa76571d0a76cab3cbaf4046e7 miningbot_0000")
	if !ok || s.Verb != "deliver" || s.Item != "oxygen_gas" || s.Qty != 1900 || s.From != "gold_run_extraction_hub" ||
		s.To != "862e07fa76571d0a76cab3cbaf4046e7" || s.Recipient != "miningbot_0000" {
		t.Errorf("ParseTask = %+v, %v", s, ok)
	}
}

func TestParseTaskBuyDirected(t *testing.T) {
	s, ok := ParseTask("buy_directed pharmaceutical_culture 850 sirius_observatory_station 90 self")
	if !ok || s.Verb != "buy_directed" || s.Item != "pharmaceutical_culture" || s.Qty != 850 ||
		s.From != "sirius_observatory_station" || s.MaxPrice != 90 || s.Recipient != "self" {
		t.Errorf("ParseTask = %+v, %v", s, ok)
	}
}

func TestParseTaskFactionDeposit(t *testing.T) {
	s, ok := ParseTask("faction_deposit oxygen_gas")
	if !ok || s.Verb != "faction_deposit" || s.Item != "oxygen_gas" {
		t.Errorf("ParseTask = %+v, %v", s, ok)
	}
}

func TestParseTaskOtherVerbs(t *testing.T) {
	for _, cmd := range []string{"capture_storage", "craft", "deliver oxygen_gas", "buy_directed x 1 s notanumber self", ""} {
		if s, ok := ParseTask(cmd); ok {
			t.Errorf("ParseTask(%q) = %+v, want not ok", cmd, s)
		}
	}
}

func writeSchedule(t *testing.T, dir, agent, body string) {
	t.Helper()
	d := filepath.Join(dir, agent)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "schedule.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSchedulesSkipsCorruptKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	writeSchedule(t, dir, "trader-10", `[{"id":10,"frequency":"daily","command":"buy_directed pharmaceutical_culture 850 sirius_observatory_station 90 self"}]`)
	writeSchedule(t, dir, "broken", `[{"id":`)
	got, err := LoadSchedules(dir)
	if err == nil {
		t.Error("corrupt schedule produced no error")
	}
	if ts := got["trader-10"]; len(ts) != 1 || ts[0].ID != 10 || ts[0].Frequency != "daily" {
		t.Errorf("trader-10 = %+v", ts)
	}
	if _, ok := got["broken"]; ok {
		t.Error("corrupt agent kept")
	}
}

func TestLoadSchedulesAcceptsWrappedShape(t *testing.T) {
	dir := t.TempDir()
	writeSchedule(t, dir, "old", `{"tasks":[{"id":3,"frequency":"hourly","command":"craft refine_steel 500"}]}`)
	got, err := LoadSchedules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ts := got["old"]; len(ts) != 1 || ts[0].Command != "craft refine_steel 500" {
		t.Errorf("old = %+v", ts)
	}
}

func TestLoadSchedulesMissingDirIsEmpty(t *testing.T) {
	got, err := LoadSchedules(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(got) != 0 {
		t.Errorf("LoadSchedules(missing) = %v, %v", got, err)
	}
}
