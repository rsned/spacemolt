package calllog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testMutations is a small mutation set for testing.
var testMutations = map[string]bool{
	"mine":   true,
	"dock":   true,
	"undock": true,
	"travel": true,
	"sell":   true,
}

// testSnap returns a sample StateSnapshot for use in tests.
func testSnap() StateSnapshot {
	return StateSnapshot{
		Location: LocationInfo{
			System: "sol",
			POI:    "sol_station",
			Docked: true,
		},
		Ship: ShipInfo{
			Name:          "Prospector",
			ClassID:       "mining_barge",
			Hull:          100,
			MaxHull:       100,
			Shield:        50,
			MaxShield:     50,
			Fuel:          80,
			MaxFuel:       100,
			CargoUsed:     25,
			CargoCapacity: 100,
			Modules:       []string{"Mining Laser I", "Shield Booster I"},
		},
	}
}

func TestActionGoesToActionsFile(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test", testMutations)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logger.Close() }()

	snap := testSnap()
	req := json.RawMessage(`{"type":"mine"}`)
	resp := json.RawMessage(`{"action":"mine","ok":true}`)

	if err := logger.Log("mine", snap, req, resp); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(entries), names(entries))
	}
	if !strings.Contains(entries[0].Name(), ".actions.") {
		t.Errorf("expected actions file, got %q", entries[0].Name())
	}

	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if string(entry.Request) != string(req) {
		t.Errorf("request mismatch: got %s", entry.Request)
	}
}

func TestQueryGoesToQueriesFile(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test", testMutations)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logger.Close() }()

	snap := testSnap()
	req := json.RawMessage(`{"type":"get_status"}`)
	resp := json.RawMessage(`{"credits":1000}`)

	if err := logger.Log("get_status", snap, req, resp); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(entries), names(entries))
	}
	if !strings.Contains(entries[0].Name(), ".queries.") {
		t.Errorf("expected queries file, got %q", entries[0].Name())
	}
}

func TestMixedCallsCreateBothFiles(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "bot", testMutations)
	if err != nil {
		t.Fatal(err)
	}

	snap := testSnap()

	// Action
	if err := logger.Log("mine", snap, json.RawMessage(`{"type":"mine"}`), json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	// Query
	if err := logger.Log("get_status", snap, json.RawMessage(`{"type":"get_status"}`), json.RawMessage(`{"credits":1}`)); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(entries), names(entries))
	}

	fileNames := names(entries)
	hasActions := false
	hasQueries := false
	for _, n := range fileNames {
		if strings.Contains(n, ".actions.") {
			hasActions = true
		}
		if strings.Contains(n, ".queries.") {
			hasQueries = true
		}
	}
	if !hasActions {
		t.Error("missing actions file")
	}
	if !hasQueries {
		t.Error("missing queries file")
	}
}

func TestFileNameFormat(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "auto-miner", testMutations)
	if err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 4, 7, 10, 30, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return fixed }

	if err := logger.Log("mine", testSnap(), json.RawMessage(`{"type":"mine"}`), json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := logger.Log("get_status", testSnap(), json.RawMessage(`{"type":"get_status"}`), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileNames := names(entries)
	wantActions := "auto-miner.actions.20260407.log"
	wantQueries := "auto-miner.queries.20260407.log"
	if !contains(fileNames, wantActions) {
		t.Errorf("missing %q in %v", wantActions, fileNames)
	}
	if !contains(fileNames, wantQueries) {
		t.Errorf("missing %q in %v", wantQueries, fileNames)
	}
}

func TestRotationAtMidnightPST(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "bot", testMutations)
	if err != nil {
		t.Fatal(err)
	}

	snap := testSnap()

	// Day 1: 11:59 PM PST
	beforeMidnight := time.Date(2026, 4, 7, 23, 59, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return beforeMidnight }

	if err := logger.Log("mine", snap, json.RawMessage(`{"type":"mine"}`), json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}

	// Day 2: 12:01 AM PST
	afterMidnight := time.Date(2026, 4, 8, 0, 1, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return afterMidnight }

	if err := logger.Log("mine", snap, json.RawMessage(`{"type":"mine"}`), json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Should have 2 actions files (one per day)
	if len(entries) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(entries), names(entries))
	}
	fileNames := names(entries)
	if !contains(fileNames, "bot.actions.20260407.log") {
		t.Errorf("missing day 1 actions file in %v", fileNames)
	}
	if !contains(fileNames, "bot.actions.20260408.log") {
		t.Errorf("missing day 2 actions file in %v", fileNames)
	}

	// Verify each has 1 line
	for _, fn := range fileNames {
		data, err := os.ReadFile(filepath.Join(dir, fn))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 1 {
			t.Errorf("%s has %d lines, want 1", fn, len(lines))
		}
	}
}

func TestRotationUsesDateNotUTC(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test", testMutations)
	if err != nil {
		t.Fatal(err)
	}

	snap := testSnap()

	// 11 PM PST = 7 AM next day UTC. Should still be same PST day.
	pst11pm := time.Date(2026, 4, 7, 23, 0, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return pst11pm }
	if err := logger.Log("mine", snap, json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	pst1130pm := time.Date(2026, 4, 7, 23, 30, 0, 0, logger.loc)
	logger.nowFunc = func() time.Time { return pst1130pm }
	if err := logger.Log("mine", snap, json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file (same PST day), got %d", len(entries))
	}
}

func TestMultipleRotations(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "bot", testMutations)
	if err != nil {
		t.Fatal(err)
	}

	snap := testSnap()
	for day := range 3 {
		ts := time.Date(2026, 4, 5+day, 12, 0, 0, 0, logger.loc)
		logger.nowFunc = func() time.Time { return ts }
		if err := logger.Log("mine", snap, json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	_ = logger.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 files, got %d", len(entries))
	}
}

func TestLogCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	logger, err := New(dir, "test", testMutations)
	if err != nil {
		t.Fatal(err)
	}

	if err := logger.Log("mine", testSnap(), json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected log directory to be created")
	}
}

func TestCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test", testMutations)
	if err != nil {
		t.Fatal(err)
	}

	if err := logger.Log("mine", testSnap(), json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	if err := logger.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

func TestStateSnapshotInOutput(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, "test", testMutations)
	if err != nil {
		t.Fatal(err)
	}

	snap := StateSnapshot{
		Location: LocationInfo{
			System:    "alpha-centauri",
			POI:       "asteroid_belt_3",
			Docked:    false,
			Traveling: true,
		},
		Ship: ShipInfo{
			Name:          "Orca",
			ClassID:       "heavy_miner",
			Hull:          450,
			MaxHull:       500,
			Fuel:          30,
			MaxFuel:       200,
			CargoUsed:     180,
			CargoCapacity: 200,
			Modules:       []string{"Strip Mining Laser", "Shield Booster II", "Cargo Expander I"},
		},
	}

	if err := logger.Log("mine", snap, json.RawMessage(`{"type":"mine"}`), json.RawMessage(`{"ok":true}`)); err != nil {
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

	if entry.State.Location.System != "alpha-centauri" {
		t.Errorf("system = %q", entry.State.Location.System)
	}
	if !entry.State.Location.Traveling {
		t.Error("expected traveling=true")
	}
	if entry.State.Ship.Hull != 450 {
		t.Errorf("hull = %v, want 450", entry.State.Ship.Hull)
	}
	if len(entry.State.Ship.Modules) != 3 {
		t.Errorf("modules = %d, want 3", len(entry.State.Ship.Modules))
	}
}

func TestMutationsSetContainsExpectedEntries(t *testing.T) {
	// Spot-check that the exported Mutations set has key entries
	for _, cmd := range []string{"mine", "dock", "undock", "travel", "jump", "sell", "buy", "craft", "attack"} {
		if !Mutations[cmd] {
			t.Errorf("Mutations missing %q", cmd)
		}
	}
	// And that queries are not in it
	for _, cmd := range []string{"get_status", "get_ship", "get_system", "get_map", "get_skills"} {
		if Mutations[cmd] {
			t.Errorf("Mutations should not contain query %q", cmd)
		}
	}
}

// helpers

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
