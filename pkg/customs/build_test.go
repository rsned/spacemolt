package customs

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

var testNow = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() }) //nolint:errcheck
	if _, err := db.Exec(`CREATE TABLE action_log_events (
		player_id TEXT NOT NULL, event_id INTEGER NOT NULL, event_type TEXT NOT NULL,
		category TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT '',
		data_json TEXT NOT NULL DEFAULT '{}', PRIMARY KEY (player_id, event_id))`); err != nil {
		t.Fatal(err)
	}
	return db
}

var nextEventID int

func addEvent(t *testing.T, db *sql.DB, typ string, at time.Time, data map[string]string) {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	nextEventID++
	if _, err := db.Exec(`INSERT INTO action_log_events (player_id, event_id, event_type, created_at, data_json) VALUES (?, ?, ?, ?, ?)`,
		"p1", nextEventID, typ, at.UTC().Format(time.RFC3339), string(b)); err != nil {
		t.Fatal(err)
	}
}

func arrive(t *testing.T, db *sql.DB, sys string, at time.Time) {
	addEvent(t, db, "navigation.jumped", at, map[string]string{"from_system": "elsewhere", "to_system": sys})
}

func inspect(t *testing.T, db *sql.DB, sys, empire string, at time.Time) {
	addEvent(t, db, "combat.customs_inspection", at, map[string]string{"system_id": sys, "empire_id": empire, "poi_id": sys + "_belt", "outcome": "clear"})
}

func build(t *testing.T, db *sql.DB) map[string]SystemStat {
	t.Helper()
	got, err := Build(context.Background(), db, testNow, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestBuildMonitoredFromRecentInspection(t *testing.T) {
	db := testDB(t)
	for i := range 3 {
		arrive(t, db, "grumium", testNow.Add(-time.Duration(50+i)*time.Hour))
	}
	inspect(t, db, "grumium", "solarian", testNow.Add(-48*time.Hour))
	s := build(t, db)["grumium"]
	if s.Class != Monitored || s.Inspections != 1 || s.Empire != "solarian" || s.Arrivals != 3 {
		t.Fatalf("grumium = %+v, want monitored, 1 inspection, solarian, 3 arrivals", s)
	}
}

func TestBuildAgesOutAfterCleanArrivals(t *testing.T) {
	db := testDB(t)
	last := testNow.Add(-72 * time.Hour)
	inspect(t, db, "procyon", "solarian", last)
	for i := range 20 {
		arrive(t, db, "procyon", last.Add(time.Duration(i+1)*time.Hour))
	}
	s := build(t, db)["procyon"]
	if s.Class != Clear || s.ArrivalsSince != 20 {
		t.Fatalf("procyon = %+v, want clear after 20 clean arrivals", s)
	}
}

func TestBuildIgnoresEventsOutsideWindow(t *testing.T) {
	db := testDB(t)
	inspect(t, db, "node_beta", "voidborn", testNow.Add(-15*24*time.Hour))
	for i := range 12 {
		arrive(t, db, "node_beta", testNow.Add(-time.Duration(i+1)*time.Hour))
	}
	s := build(t, db)["node_beta"]
	if s.Class != Clear || s.Inspections != 0 {
		t.Fatalf("node_beta = %+v, want clear with the old inspection out of window", s)
	}
}

func TestBuildUnknownBelowMinArrivals(t *testing.T) {
	db := testDB(t)
	for i := range 4 {
		arrive(t, db, "timberline", testNow.Add(-time.Duration(i+1)*time.Hour))
	}
	if s := build(t, db)["timberline"]; s.Class != Unknown {
		t.Fatalf("timberline = %+v, want unknown", s)
	}
}

func TestBuildCountsSeizuresAndItems(t *testing.T) {
	db := testDB(t)
	inspect(t, db, "the_experiment", "voidborn", testNow.Add(-time.Hour))
	addEvent(t, db, "combat.customs_seizure", testNow.Add(-time.Hour), map[string]string{
		"system_id": "the_experiment", "empire_id": "voidborn", "seized_items": `["3x Void Dust"]`,
	})
	s := build(t, db)["the_experiment"]
	if s.Seizures != 1 || !slices.Contains(s.SeizedItems, "3x Void Dust") {
		t.Fatalf("the_experiment = %+v, want 1 seizure of 3x Void Dust", s)
	}
}

func TestBuildEmpireFallsBackToEmpireOf(t *testing.T) {
	db := testDB(t)
	for i := range 10 {
		arrive(t, db, "timberline", testNow.Add(-time.Duration(i+1)*time.Hour))
	}
	o := DefaultOptions()
	o.EmpireOf = func(string) string { return "solarian" }
	got, err := Build(context.Background(), db, testNow, o)
	if err != nil {
		t.Fatal(err)
	}
	if s := got["timberline"]; s.Empire != "solarian" || s.Class != Clear {
		t.Fatalf("timberline = %+v, want clear solarian", s)
	}
}
