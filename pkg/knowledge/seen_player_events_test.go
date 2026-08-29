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
