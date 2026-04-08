package calllog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logger.Close() }()

	req := json.RawMessage(`{"type":"mine","payload":{"target":"asteroid-1"}}`)
	resp := json.RawMessage(`{"action":"mine","result":"ok","yield":42}`)

	if err := logger.Log(req, resp); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	// Find the log file
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("log entry is not valid JSON: %v\nraw: %s", err, data)
	}

	if entry.Timestamp == "" {
		t.Error("timestamp is empty")
	}
	// Verify ISO8601 with offset
	if _, err := time.Parse(time.RFC3339, entry.Timestamp); err != nil {
		t.Errorf("timestamp not RFC3339: %v", err)
	}

	if string(entry.Request) != string(req) {
		t.Errorf("request mismatch: got %s, want %s", entry.Request, req)
	}
	if string(entry.Response) != string(resp) {
		t.Errorf("response mismatch: got %s, want %s", entry.Response, resp)
	}
}

func TestLogMultipleEntries(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logger.Close() }()

	for range 5 {
		req := json.RawMessage(`{"type":"get_status"}`)
		resp := json.RawMessage(`{"credits":1000}`)
		if err := logger.Log(req, resp); err != nil {
			t.Fatal(err)
		}
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 log lines, got %d", len(lines))
	}

	for i, line := range lines {
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestFileNameFormat(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "auto-miner")
	if err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 4, 7, 10, 30, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return fixed }

	req := json.RawMessage(`{"type":"mine"}`)
	resp := json.RawMessage(`{"ok":true}`)
	if err := logger.Log(req, resp); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	want := "auto-miner.20260407.log"
	if entries[0].Name() != want {
		t.Errorf("filename = %q, want %q", entries[0].Name(), want)
	}
}

func TestRotationAtMidnightPST(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "auto-explorer")
	if err != nil {
		t.Fatal(err)
	}

	// Start at 11:59 PM PST on April 7
	beforeMidnight := time.Date(2026, 4, 7, 23, 59, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return beforeMidnight }

	req1 := json.RawMessage(`{"type":"travel","payload":{"dest":"alpha"}}`)
	resp1 := json.RawMessage(`{"action":"travel","ok":true}`)
	if err := logger.Log(req1, resp1); err != nil {
		t.Fatal(err)
	}

	// Advance to 12:01 AM PST on April 8
	afterMidnight := time.Date(2026, 4, 8, 0, 1, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return afterMidnight }

	req2 := json.RawMessage(`{"type":"dock","payload":{"station":"hub"}}`)
	resp2 := json.RawMessage(`{"action":"dock","ok":true}`)
	if err := logger.Log(req2, resp2); err != nil {
		t.Fatal(err)
	}

	_ = logger.Close()

	// Should have two files
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 log files after rotation, got %d", len(entries))
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names["auto-explorer.20260407.log"] {
		t.Error("missing day 1 log file: auto-explorer.20260407.log")
	}
	if !names["auto-explorer.20260408.log"] {
		t.Error("missing day 2 log file: auto-explorer.20260408.log")
	}

	// Verify day 1 file has exactly 1 entry
	day1Data, err := os.ReadFile(filepath.Join(dir, "auto-explorer.20260407.log"))
	if err != nil {
		t.Fatal(err)
	}
	day1Lines := strings.Split(strings.TrimSpace(string(day1Data)), "\n")
	if len(day1Lines) != 1 {
		t.Errorf("day 1 file has %d lines, want 1", len(day1Lines))
	}

	// Verify day 2 file has exactly 1 entry
	day2Data, err := os.ReadFile(filepath.Join(dir, "auto-explorer.20260408.log"))
	if err != nil {
		t.Fatal(err)
	}
	day2Lines := strings.Split(strings.TrimSpace(string(day2Data)), "\n")
	if len(day2Lines) != 1 {
		t.Errorf("day 2 file has %d lines, want 1", len(day2Lines))
	}

	// Verify both entries are valid JSON
	var entry1 Entry
	if err := json.Unmarshal(day1Data, &entry1); err != nil {
		t.Fatalf("day 1 entry invalid JSON: %v", err)
	}
	var entry2 Entry
	if err := json.Unmarshal(day2Data, &entry2); err != nil {
		t.Fatalf("day 2 entry invalid JSON: %v", err)
	}
}

func TestRotationUsesDateNotUTC(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test")
	if err != nil {
		t.Fatal(err)
	}

	// 11 PM PST = 7 AM next day UTC. Should still be same PST day.
	pst11pm := time.Date(2026, 4, 7, 23, 0, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return pst11pm }

	if err := logger.Log(json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	// 11:30 PM PST same day — no rotation
	pst1130pm := time.Date(2026, 4, 7, 23, 30, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return pst1130pm }

	if err := logger.Log(json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file (same PST day), got %d", len(entries))
	}
	if entries[0].Name() != "test.20260407.log" {
		t.Errorf("filename = %q, want test.20260407.log", entries[0].Name())
	}
}

func TestMultipleRotations(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "bot")
	if err != nil {
		t.Fatal(err)
	}

	for day := range 3 {
		ts := time.Date(2026, 4, 5+day, 12, 0, 0, 0, logger.loc)
		logger.nowFunc = func() time.Time { return ts }
		if err := logger.Log(json.RawMessage(`{"day":`+string(rune('0'+day))+`}`), json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 log files, got %d", len(entries))
	}
}

func TestLogCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	logger, err := New(dir, "test")
	if err != nil {
		t.Fatal(err)
	}

	if err := logger.Log(json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected log directory to be created")
	}
}

func TestCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test")
	if err != nil {
		t.Fatal(err)
	}

	if err := logger.Log(json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	// Close multiple times should not error
	if err := logger.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

func TestResponseWithMultipleJSONValues(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logger.Close() }()

	req := json.RawMessage(`{"type":"sell_all"}`)
	// Multiple JSON values wrapped as an array by caller
	resp := json.RawMessage(`[{"item":"ore","sold":10},{"item":"crystal","sold":5}]`)

	if err := logger.Log(req, resp); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if string(entry.Response) != string(resp) {
		t.Errorf("response mismatch: got %s", entry.Response)
	}
}
