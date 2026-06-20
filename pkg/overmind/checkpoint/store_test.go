package checkpoint

import (
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestIntentRoundTrip(t *testing.T) {
	s := openTemp(t)
	if _, ok, err := s.LoadIntent(); err != nil || ok {
		t.Fatalf("empty LoadIntent: ok=%v err=%v", ok, err)
	}
	want := Intent{StandingBehavior: "track_station", ActiveTaskID: "t-1", StepIndex: 3}
	if err := s.SaveIntent(want); err != nil {
		t.Fatalf("SaveIntent: %v", err)
	}
	got, ok, err := s.LoadIntent()
	if err != nil || !ok || got != want {
		t.Fatalf("LoadIntent got=%+v ok=%v err=%v want=%+v", got, ok, err, want)
	}
	// Upsert (single row).
	want.StepIndex = 9
	_ = s.SaveIntent(want)
	got, _, _ = s.LoadIntent()
	if got.StepIndex != 9 {
		t.Fatalf("intent not upserted: %+v", got)
	}
}

func TestKnownStateAndJournalAndCursor(t *testing.T) {
	s := openTemp(t)
	ks := KnownState{System: "SOL", POI: "ST-9", Docked: true, Credits: 12345, CargoJSON: `{"iron":20}`, Tick: 7}
	if err := s.SaveKnownState(ks); err != nil {
		t.Fatalf("SaveKnownState: %v", err)
	}
	got, ok, err := s.LoadKnownState()
	if err != nil || !ok || got != ks {
		t.Fatalf("LoadKnownState got=%+v ok=%v err=%v", got, ok, err)
	}

	now := time.Now().Truncate(time.Second)
	_ = s.AppendJournal("t-1", "done", now)
	_ = s.AppendJournal("t-2", "failed", now)
	entries, err := s.Journal(10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("Journal len=%d err=%v", len(entries), err)
	}
	if entries[0].TaskID != "t-2" { // newest first
		t.Fatalf("journal order wrong: %+v", entries)
	}

	if _, ok, _ := s.Cursor("mined_iron"); ok {
		t.Fatalf("unexpected cursor present")
	}
	_ = s.SetCursor("mined_iron", "14000")
	v, ok, err := s.Cursor("mined_iron")
	if err != nil || !ok || v != "14000" {
		t.Fatalf("Cursor v=%q ok=%v err=%v", v, ok, err)
	}
}
