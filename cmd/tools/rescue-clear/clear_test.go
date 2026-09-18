package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/rescue"
)

// seedQueue writes recs to a fresh queue file and returns its path.
func seedQueue(t *testing.T, recs []rescue.Record) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rescue-queue.json")
	raw, err := json.Marshal(recs)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	return path
}

func TestClearRecordsRemovesOnlyNamedAgents(t *testing.T) {
	qPath := seedQueue(t, []rescue.Record{
		{AgentID: "trader-3", Fleet: "haul"},
		{AgentID: "hauler-0", Fleet: "haul"},
		{AgentID: "salvager-1", Fleet: "haul"},
	})
	hist := filepath.Join(filepath.Dir(qPath), "rescue-history.jsonl")
	q := rescue.NewQueue(qPath)

	var out strings.Builder
	n, err := clearRecords(q, hist, []string{"trader-3", "salvager-1"}, &out)
	if err != nil {
		t.Fatalf("clearRecords: %v", err)
	}
	if n != 2 {
		t.Errorf("removed = %d, want 2", n)
	}

	left, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(left) != 1 || left[0].AgentID != "hauler-0" {
		t.Errorf("remaining = %+v, want only hauler-0", left)
	}
}

func TestClearRecordsArchivesToHistory(t *testing.T) {
	qPath := seedQueue(t, []rescue.Record{{AgentID: "trader-3", Fleet: "haul", System: "Winterhold"}})
	hist := filepath.Join(filepath.Dir(qPath), "rescue-history.jsonl")
	q := rescue.NewQueue(qPath)

	var out strings.Builder
	if _, err := clearRecords(q, hist, []string{"trader-3"}, &out); err != nil {
		t.Fatalf("clearRecords: %v", err)
	}

	f, err := os.Open(hist)
	if err != nil {
		t.Fatalf("open history: %v", err)
	}
	defer f.Close() //nolint:errcheck
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatal("history file has no lines")
	}
	var got rescue.Record
	if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal history line: %v", err)
	}
	if got.AgentID != "trader-3" || got.System != "Winterhold" {
		t.Errorf("archived = %+v, want trader-3 @ Winterhold", got)
	}
}

func TestClearRecordsUnknownAgentIsReportedNotFatal(t *testing.T) {
	qPath := seedQueue(t, []rescue.Record{{AgentID: "trader-3", Fleet: "haul"}})
	hist := filepath.Join(filepath.Dir(qPath), "rescue-history.jsonl")
	q := rescue.NewQueue(qPath)

	var out strings.Builder
	n, err := clearRecords(q, hist, []string{"nobody-9", "trader-3"}, &out)
	if err != nil {
		t.Fatalf("clearRecords: %v", err)
	}
	if n != 1 {
		t.Errorf("removed = %d, want 1", n)
	}
	if !strings.Contains(out.String(), "nobody-9") {
		t.Errorf("output does not mention the missing agent:\n%s", out.String())
	}
}
