package customs

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyForcesClassAndAddsUnknownSystem(t *testing.T) {
	stats := map[string]SystemStat{"windmere": {Class: Monitored, Inspections: 28}}
	Apply(stats, Overrides{
		Clear:     map[string]string{"Windmere": "v0.734.1: Crimson customs Rampart only"},
		Monitored: map[string]string{"timberline": "operator: patrol seen"},
	})
	if s := stats["windmere"]; s.Class != Clear || s.Reason == "" || s.Inspections != 28 {
		t.Errorf("windmere = %+v, want forced clear keeping its evidence", s)
	}
	if s, ok := stats["timberline"]; !ok || s.Class != Monitored || s.Reason != "operator: patrol seen" {
		t.Errorf("timberline = %+v (present %v), want forced monitored with no evidence", s, ok)
	}
}

func TestWriteLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map.json")
	m := Map{GeneratedAt: testNow, WindowDays: 14, Systems: map[string]SystemStat{
		"grumium": {Class: Monitored, Empire: "solarian", Inspections: 3, LastInspection: testNow.Add(-time.Hour)},
	}}
	if err := Write(path, m); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.GeneratedAt.Equal(m.GeneratedAt) || got.Systems["grumium"].Inspections != 3 || got.Systems["grumium"].Class != Monitored {
		t.Errorf("round trip = %+v", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "none.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
	o, err := LoadOverrides(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || len(o.Clear)+len(o.Monitored) != 0 {
		t.Errorf("missing overrides = %+v, %v; want empty, nil", o, err)
	}
}

func TestMonitoredListsOnlyMonitored(t *testing.T) {
	m := Map{Systems: map[string]SystemStat{
		"grumium":    {Class: Monitored, Inspections: 2, LastInspection: testNow},
		"timberline": {Class: Clear},
		"azmidi":     {Class: Unknown},
	}}
	got := m.Monitored()
	if len(got) != 1 || got["grumium"] == "" {
		t.Errorf("Monitored() = %v, want only grumium", got)
	}
}
