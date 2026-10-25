package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// stubRecordClient answers GetRawJSON from a fixed map and GetState with a
// fixed tick; everything else in game.GameClient panics if touched.
type stubRecordClient struct {
	game.GameClient
	raw  map[string][]byte
	tick int64
}

func (s stubRecordClient) GetRawJSON(key string) []byte { return s.raw[key] }
func (s stubRecordClient) GetState() *game.State {
	return &game.State{CurrentTick: s.tick}
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad JSONL line %q: %v", sc.Text(), err)
		}
		out = append(out, m)
	}
	return out
}

func TestAppendStationEvent_WritesJSONL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cloverfield")
	if err := appendStationEvent(dir, stationEvent{Command: "get_status"}); err != nil {
		t.Fatal(err)
	}
	ev := stationEvent{
		Command: "facility",
		Args:    []string{"list"},
		Tick:    123,
		Raw:     json.RawMessage(`{"facilities":[]}`),
	}
	if err := appendStationEvent(dir, ev); err != nil {
		t.Fatal(err)
	}

	f, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(f)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	var got stationEvent
	if err := json.Unmarshal(lines[1], &got); err != nil {
		t.Fatal(err)
	}
	if got.Command != "facility" || !slices.Equal(got.Args, []string{"list"}) || got.Tick != 123 {
		t.Errorf("got %+v", got)
	}
	if string(got.Raw) != `{"facilities":[]}` {
		t.Errorf("raw = %s", got.Raw)
	}
	if got.ObservedUTC == "" {
		t.Error("observed_utc not stamped")
	}
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		out = append(out, append([]byte(nil), sc.Bytes()...))
	}
	return out
}

func TestAppendStationEvent_EmptyDirNoop(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := appendStationEvent("", stationEvent{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(cwd)
	if len(entries) != 0 {
		t.Errorf("created %v with recording disabled", entries)
	}
}

func TestRawForRecord_ErrorUsesLastError(t *testing.T) {
	c := stubRecordClient{raw: map[string][]byte{
		"_last_error": []byte(`{"error":"no_faction_storage"}`),
		"facility":    []byte(`{"stale":true}`),
	}}
	got := rawForRecord(c, "facility", errors.New("boom"))
	if string(got) != `{"error":"no_faction_storage"}` {
		t.Errorf("got %s", got)
	}
}

func TestRawForRecord_NonJSONWrapped(t *testing.T) {
	c := stubRecordClient{raw: map[string][]byte{"_last": []byte("not json")}}
	got := rawForRecord(c, "build_base", nil)
	if string(got) != `"not json"` {
		t.Errorf("got %s", got)
	}
}

func TestRecordedExecute_RecordErrorDoesNotFailCommand(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := globalRecordDir
	globalRecordDir = filepath.Join(blocker, "sub") // MkdirAll fails: parent is a file
	t.Cleanup(func() { globalRecordDir = old })

	c := stubRecordClient{raw: map[string][]byte{}}
	if err := recordExecution(c, []string{"get_status"}, nil); err != nil {
		t.Errorf("recording failure leaked into command result: %v", err)
	}
	want := errors.New("server said no")
	if err := recordExecution(c, []string{"get_status"}, want); !errors.Is(err, want) {
		t.Errorf("command error not returned unchanged: %v", err)
	}
}

func TestRecordExecution_CapturesFailure(t *testing.T) {
	dir := t.TempDir()
	old := globalRecordDir
	globalRecordDir = dir
	t.Cleanup(func() { globalRecordDir = old })

	c := stubRecordClient{tick: 77, raw: map[string][]byte{
		"_last_error": []byte(`{"error":"not_docked"}`),
	}}
	_ = recordExecution(c, []string{"station", "info"}, errors.New("not_docked"))

	lines := readJSONLines(t, filepath.Join(dir, "events.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("got %d events, want 1", len(lines))
	}
	if lines[0]["error"] != "not_docked" || lines[0]["command"] != "station" {
		t.Errorf("event = %v", lines[0])
	}
}

func TestSnapshotCommands_Order(t *testing.T) {
	want := [][]string{
		{"get_status"},
		{"faction_info"},
		{"view_faction_storage"},
		{"station", "info"},
		{"facility", "list"},
		{"facility", "faction_list"},
	}
	got := snapshotCommands()
	if !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Errorf("snapshotCommands() = %v, want %v", got, want)
	}
}

func TestRunSnapshot_ContinuesAfterFailure(t *testing.T) {
	var ran []string
	var ids []string
	err := runSnapshot(func(p []string) error {
		ran = append(ran, strings.Join(p, " "))
		ids = append(ids, globalSnapshotID)
		if p[0] == "station" {
			return errors.New("not_docked")
		}
		return nil
	})
	if len(ran) != 6 {
		t.Fatalf("ran %d reads, want 6: %v", len(ran), ran)
	}
	if err == nil || !strings.Contains(err.Error(), "station info") {
		t.Errorf("err = %v, want mention of station info", err)
	}
	for i, id := range ids {
		if id == "" || id != ids[0] {
			t.Errorf("read %d snapshot id %q, want shared non-empty id", i, id)
		}
	}
	if globalSnapshotID != "" {
		t.Errorf("snapshot id left set: %q", globalSnapshotID)
	}
}

func TestParseLedger_Valid(t *testing.T) {
	e, err := parseLedgerArgs([]string{"ammo", "ferrous_slug_case", "500", "convoy", "one"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Category != "ammo" || e.Item != "ferrous_slug_case" || e.Qty != 500 || e.Note != "convoy one" || e.Credits != 0 {
		t.Errorf("got %+v", e)
	}
}

func TestParseLedger_Credits(t *testing.T) {
	e, err := parseLedgerArgs([]string{"founding", "credits", "-5000000"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Item != "credits" || e.Credits != -5000000 {
		t.Errorf("got %+v", e)
	}
}

func TestParseLedger_BadCategory(t *testing.T) {
	_, err := parseLedgerArgs([]string{"snacks", "credits", "10"})
	if err == nil || !strings.Contains(err.Error(), "usage: ledger") {
		t.Errorf("err = %v", err)
	}
}

func TestParseLedger_BadQty(t *testing.T) {
	_, err := parseLedgerArgs([]string{"ammo", "ferrous_slug_case", "lots"})
	if err == nil || !strings.Contains(err.Error(), "usage: ledger") {
		t.Errorf("err = %v", err)
	}
}

func TestLedgerCommand_NoAppendOnError(t *testing.T) {
	dir := t.TempDir()
	if err := runLedger(dir, []string{"ammo", "x"}); err == nil {
		t.Fatal("want usage error")
	}
	if _, err := os.Stat(filepath.Join(dir, "ledger.jsonl")); !os.IsNotExist(err) {
		t.Errorf("ledger.jsonl created on bad input (stat err %v)", err)
	}
	if err := runLedger("", []string{"ammo", "x", "1"}); err == nil || !strings.Contains(err.Error(), "--record-dir") {
		t.Errorf("want --record-dir error, got %v", err)
	}
	if err := runLedger(dir, []string{"haul-in", "steel_plate", "200"}); err != nil {
		t.Fatal(err)
	}
	lines := readJSONLines(t, filepath.Join(dir, "ledger.jsonl"))
	if len(lines) != 1 || lines[0]["category"] != "haul-in" || lines[0]["qty"] != float64(200) {
		t.Errorf("ledger = %v", lines)
	}
}
