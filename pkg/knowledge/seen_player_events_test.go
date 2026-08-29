package knowledge

import (
	"testing"
	"time"
)

// A per-observation event row is the backtracking timeline: which of our
// agents saw whom, where, at what tick. The hour-bucketed sightings table
// cannot answer "who was in Nashira at tick 1731534".
func TestRecordSightingsWritesEventRow(t *testing.T) {
	kb := newTestKB(t)
	at := time.Date(2026, 8, 28, 6, 21, 0, 0, time.UTC)
	mustRecord(t, kb, SeenPlayer{
		PlayerID: "molten", Username: "MoltenOne", ShipClass: "underwriter",
		SystemID: "nashira", POIID: "nashira_belt", Source: "get_system_agents",
		SeenAt: at, Tick: 1731534, ObserverID: "our_agent",
	})

	var observer, system, poi, ship, source, seenAt string
	var tick int64
	err := kb.db.QueryRow(`SELECT observer_id, system_id, poi_id, ship_class, source, tick, seen_at_utc
		FROM seen_player_events WHERE player_id='molten'`).
		Scan(&observer, &system, &poi, &ship, &source, &tick, &seenAt)
	if err != nil {
		t.Fatalf("query seen_player_events: %v", err)
	}
	if observer != "our_agent" || system != "nashira" || poi != "nashira_belt" || ship != "underwriter" ||
		source != "get_system_agents" || tick != 1731534 || seenAt != "2026-08-28T06:21:00Z" {
		t.Errorf("event row = %q %q %q %q %q %d %q", observer, system, poi, ship, source, tick, seenAt)
	}
}

// Identity-only observations (no system) never produce a timeline row.
func TestRecordSightingsSkipsEventWithoutSystem(t *testing.T) {
	kb := newTestKB(t)
	mustRecord(t, kb, SeenPlayer{PlayerID: "p1", Username: "u1", Source: "chat", SeenAt: time.Now()})
	if got := countRows(t, kb, "seen_player_events"); got != 0 {
		t.Errorf("seen_player_events rows = %d, want 0", got)
	}
}

// Residents run get_system_agents and get_nearby back to back. The same
// player seen by the same observer in the same system at the same tick is
// ONE observation: the POI-level call upgrades the system-wide row with its
// poi_id instead of adding a second row.
func TestRecordSightingsDedupsSameObserverPlayerTick(t *testing.T) {
	kb := newTestKB(t)
	at := time.Date(2026, 8, 29, 19, 37, 0, 0, time.UTC)
	base := SeenPlayer{PlayerID: "molten", Username: "MoltenOne", ShipClass: "portfolio",
		SystemID: "haven", Source: "get_system_agents", SeenAt: at, Tick: 1744900, ObserverID: "mb_haven"}
	mustRecord(t, kb, base)
	near := base
	near.POIID, near.Source, near.SeenAt = "haven_station", "get_nearby", at.Add(3*time.Second)
	mustRecord(t, kb, near)

	if got := countRows(t, kb, "seen_player_events"); got != 1 {
		t.Fatalf("seen_player_events rows = %d, want 1 (deduped)", got)
	}
	var poi, source string
	if err := kb.db.QueryRow(`SELECT poi_id, source FROM seen_player_events WHERE player_id='molten'`).Scan(&poi, &source); err != nil {
		t.Fatal(err)
	}
	if poi != "haven_station" || source != "get_nearby" {
		t.Errorf("merged row poi=%q source=%q, want the POI-level values", poi, source)
	}
}

// A system-wide re-read must not erase the POI a POI-level read already gave us.
func TestRecordSightingsKeepsPOIWhenSystemWideRowArrivesSecond(t *testing.T) {
	kb := newTestKB(t)
	at := time.Date(2026, 8, 29, 19, 37, 0, 0, time.UTC)
	near := SeenPlayer{PlayerID: "p", SystemID: "haven", POIID: "haven_station", Source: "get_nearby", SeenAt: at, Tick: 5, ObserverID: "o"}
	wide := near
	wide.POIID, wide.Source = "", "get_system_agents"
	mustRecord(t, kb, near)
	mustRecord(t, kb, wide)
	var poi string
	if err := kb.db.QueryRow(`SELECT poi_id FROM seen_player_events WHERE player_id='p'`).Scan(&poi); err != nil {
		t.Fatal(err)
	}
	if poi != "haven_station" {
		t.Errorf("poi=%q, want haven_station kept", poi)
	}
}

// Without a tick there is no way to know two rows are the same moment.
func TestRecordSightingsNeverMergesUnknownTick(t *testing.T) {
	kb := newTestKB(t)
	obs := SeenPlayer{PlayerID: "p", SystemID: "haven", Source: "get_nearby", SeenAt: time.Now(), ObserverID: "o"}
	mustRecord(t, kb, obs)
	mustRecord(t, kb, obs)
	if got := countRows(t, kb, "seen_player_events"); got != 2 {
		t.Errorf("tick-0 rows = %d, want 2 (never merged)", got)
	}
}
