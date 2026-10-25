package linedash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadLiveDedupesByFreshest(t *testing.T) {
	m, err := LoadLive("testdata/status")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 {
		t.Fatalf("len = %d", len(m))
	}
	l := m["miningbot_0000"]
	if l.Fleet != "unlock" || l.POI != "new_poi" || !l.Docked || l.Credits != 250 {
		t.Errorf("live = %+v", l)
	}
	if want := time.Date(2026, 10, 25, 10, 4, 0, 0, time.UTC); !l.LastSeen.Equal(want) {
		t.Errorf("LastSeen = %v", l.LastSeen)
	}
	if l.CapturedAt.Nanosecond() != 500_000_000 {
		t.Errorf("CapturedAt = %v", l.CapturedAt)
	}
	c := m["craftsman-8"]
	if c.Fleet != "crft-mining" || !c.Quarantined || !c.LastSeen.IsZero() {
		t.Errorf("craftsman-8 = %+v", c)
	}
}

func TestLoadLiveSkipsCorruptFileKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	good, err := os.ReadFile("testdata/status/crft-mining-status.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "crft-mining-status.json"), good, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "broken-status.json"), []byte(`{"captured_at": "2026-10`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := LoadLive(dir)
	if err == nil || !strings.Contains(err.Error(), "broken-status.json") {
		t.Fatalf("err = %v, want mention of broken-status.json", err)
	}
	if _, ok := m["craftsman-8"]; !ok || len(m) != 2 {
		t.Errorf("good agents missing: %v", m)
	}
}

func TestLoadLiveMissingDirIsEmptyNotError(t *testing.T) {
	m, err := LoadLive("testdata/nope")
	if err != nil || len(m) != 0 {
		t.Errorf("got %v, %v", m, err)
	}
}
