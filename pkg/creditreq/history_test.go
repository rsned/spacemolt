package creditreq

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistory_SkipsMalformedLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hist.jsonl")
	h := NewHistory(p)
	now := time.Now().UTC()
	if err := h.Append(Entry{At: now, AgentID: "a", Event: "filed"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not json{\n")
	_ = f.Close()
	if err = h.Append(Entry{At: now, AgentID: "a", Event: "paid", Amount: 5}); err != nil {
		t.Fatal(err)
	}
	got, err := h.Since(now.Add(-time.Hour))
	if err != nil || len(got) != 2 {
		t.Fatalf("Since = %d entries, %v; want 2", len(got), err)
	}
}

func TestHistory_MissingFileIsEmpty(t *testing.T) {
	got, err := NewHistory(filepath.Join(t.TempDir(), "x.jsonl")).Since(time.Time{})
	if err != nil || len(got) != 0 {
		t.Fatalf("Since = %v, %v", got, err)
	}
}

func TestPaidSince_24hWindow(t *testing.T) {
	now := time.Now().UTC()
	es := []Entry{
		{At: now.Add(-25 * time.Hour), AgentID: "a", Event: "paid", Amount: 1},
		{At: now.Add(-time.Hour), AgentID: "a", Event: "paid", Amount: 1},
		{At: now.Add(-time.Hour), AgentID: "a", Event: "filed"},
		{At: now.Add(-time.Hour), AgentID: "b", Event: "paid", Amount: 1},
	}
	if n := PaidSince(es, "a", now.Add(-24*time.Hour)); n != 1 {
		t.Fatalf("PaidSince = %d, want 1", n)
	}
}

func TestTotalPaidSince_UTCDay(t *testing.T) {
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	es := []Entry{
		{At: day.Add(-time.Minute), AgentID: "a", Event: "paid", Amount: 20000},
		{At: day.Add(time.Minute), AgentID: "b", Event: "paid", Amount: 20000},
	}
	if n := TotalPaidSince(es, day); n != 20000 {
		t.Fatalf("TotalPaidSince = %d, want 20000", n)
	}
}
