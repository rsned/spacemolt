package huntdecide

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSortieSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	want := &Sortie{
		ID:            "pirate-6-20261121T094710Z",
		Seq:           3,
		StartedAt:     time.Date(2026, 11, 21, 9, 47, 10, 0, time.UTC),
		StartTick:     12345,
		JobKind:       "hunt",
		JobID:         "job-9",
		Grounds:       []GroundVisit{{POI: "a", Quarry: true}, {POI: "b"}},
		Fights:        4,
		Kills:         2,
		ReloadsSpent:  1,
		PendingResult: ResultReturnedDry,
	}
	if err := want.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadSortie(dir)
	if err != nil {
		t.Fatalf("LoadSortie: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != SortieFile {
		t.Errorf("dir should hold only %s, got %v", SortieFile, entries)
	}
}

func TestLoadSortieMissingFileIsNil(t *testing.T) {
	s, err := LoadSortie(t.TempDir())
	if s != nil || err != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", s, err)
	}
}

func TestLoadSortieCorruptFileIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SortieFile), []byte(`{"id":`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSortie(dir)
	if s != nil {
		t.Errorf("sortie = %v, want nil", s)
	}
	if err == nil {
		t.Error("expected an error for a corrupt file")
	}
}

func TestSortieSaveErrorWhenDirUnwritable(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSortie("a", time.Now())
	if err := s.Save(notDir); err == nil {
		t.Error("expected an error when agentDir is a regular file")
	}
}

func TestNextSeqStartsAtOne(t *testing.T) {
	s := &Sortie{}
	if got := s.NextSeq(); got != 1 {
		t.Errorf("first NextSeq = %d, want 1", got)
	}
	if got := s.NextSeq(); got != 2 {
		t.Errorf("second NextSeq = %d, want 2", got)
	}
}

func TestNewSortieID(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 47, 10, 0, time.UTC)
	s := NewSortie("pirate-6", now)
	if want := "pirate-6-20261121T094710Z"; s.ID != want {
		t.Errorf("ID = %q, want %q", s.ID, want)
	}
	if !s.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", s.StartedAt, now)
	}
}

func TestSortieVisited(t *testing.T) {
	s := &Sortie{Grounds: []GroundVisit{{POI: "a"}, {POI: "b", Quarry: true}}}
	if got := s.Visited(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Visited = %v", got)
	}
}

func TestResultFailed(t *testing.T) {
	failed := []string{ResultBlockedUnarmed, ResultStranded, ResultDestroyed}
	ok := []string{ResultCompleted, ResultReturnedDry, ResultReturnedDamaged, ResultNoQuarry, ResultInterrupted}
	for _, r := range failed {
		if !ResultFailed(r) {
			t.Errorf("ResultFailed(%q) = false, want true", r)
		}
	}
	for _, r := range ok {
		if ResultFailed(r) {
			t.Errorf("ResultFailed(%q) = true, want false", r)
		}
	}
}

func TestClearSortieMissingIsNil(t *testing.T) {
	if err := ClearSortie(t.TempDir()); err != nil {
		t.Errorf("ClearSortie on missing file: %v", err)
	}
}

func TestClearSortieRemovesFile(t *testing.T) {
	dir := t.TempDir()
	if err := NewSortie("a", time.Now()).Save(dir); err != nil {
		t.Fatal(err)
	}
	if err := ClearSortie(dir); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadSortie(dir); s != nil || err != nil {
		t.Errorf("after clear got (%v, %v)", s, err)
	}
}
