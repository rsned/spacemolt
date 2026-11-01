package outcomes

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var (
	t0 = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
)

func TestOutcomeRecordAndLoad(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	l.Record("task:7", "craft x 1", nil, t0)
	l.Record("task:8", "buy y", errors.New("price 70 exceeds ceiling 50 (replan)"), t1)

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d outcomes, want 2: %v", len(got), got)
	}
	if o := got["task:8"]; o.Status != StatusFailed || o.Reason != "price 70 exceeds ceiling 50 (replan)" || o.Command != "buy y" {
		t.Errorf("task:8 = %+v", o)
	}
	if o := got["task:7"]; o.Status != StatusOK || o.Reason != "" || !o.At.Equal(t0) {
		t.Errorf("task:7 = %+v", o)
	}
}

func TestOutcomeRecordOverwritesSameKey(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	l.Record("task:7", "craft x 1", errors.New("cannot_craft"), t0)
	l.Record("task:7", "craft x 1", nil, t1)

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["task:7"].Status != StatusOK || !got["task:7"].At.Equal(t1) {
		t.Errorf("got %+v, want one ok outcome at t1", got)
	}
}

func TestOutcomeLoadMissingFileIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Errorf("Load on empty dir = %v, %v; want empty, nil", got, err)
	}
}

func TestOutcomeLogWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	New(dir).Record("idle:ensure_home", "ensure_home", nil, t0)

	tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("temp files left behind: %v", tmps)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]Outcome
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Errorf("file does not parse: %v", err)
	}
}

func TestReasonOneLineAndCapped(t *testing.T) {
	if got := Reason(errors.New("a\nb")); got != "a; b" {
		t.Errorf("Reason = %q, want %q", got, "a; b")
	}
	long := Reason(errors.New(strings.Repeat("é", 400)))
	if n := utf8.RuneCountInString(long); n != 300 {
		t.Errorf("long reason has %d runes, want 300", n)
	}
	if Reason(nil) != "" {
		t.Error("Reason(nil) should be empty")
	}
}
