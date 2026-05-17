package knowledge

import (
	"testing"
	"time"
)

// newTestKB returns an in-memory SQLiteKB for use in seen_players tests.
func newTestKB(t *testing.T) *SQLiteKB {
	t.Helper()
	kb, err := NewSQLiteKB(Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	return kb
}

func TestSeenPlayersMigrationCreatesTables(t *testing.T) {
	kb := newTestKB(t)

	tables := []string{"seen_players", "seen_player_ships", "seen_player_sightings"}
	for _, tbl := range tables {
		var count int
		err := kb.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query for %s: %v", tbl, err)
		}
		if count != 1 {
			t.Errorf("table %s not created (count=%d)", tbl, count)
		}
	}
}

func mustRecord(t *testing.T, kb *SQLiteKB, obs ...SeenPlayer) {
	t.Helper()
	if err := kb.RecordSightings(obs); err != nil {
		t.Fatalf("RecordSightings: %v", err)
	}
}

func countRows(t *testing.T, kb *SQLiteKB, table string) int {
	t.Helper()
	var n int
	if err := kb.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestRecordSightings_FreshInsert(t *testing.T) {
	kb := newTestKB(t)
	now := time.Date(2026, 5, 17, 14, 30, 0, 0, time.UTC)

	mustRecord(t, kb, SeenPlayer{
		PlayerID:   "p1",
		Username:   "TraderUser6",
		ShipClass:  "theoria",
		FactionID:  "f-strg",
		FactionTag: "STRG",
		SystemID:   "sys-treasure",
		POIID:      "poi-haven",
		Source:     "get_nearby",
		SeenAt:     now,
	})

	if got, want := countRows(t, kb, "seen_players"), 1; got != want {
		t.Errorf("seen_players rows = %d, want %d", got, want)
	}
	if got, want := countRows(t, kb, "seen_player_ships"), 1; got != want {
		t.Errorf("seen_player_ships rows = %d, want %d", got, want)
	}
	if got, want := countRows(t, kb, "seen_player_sightings"), 1; got != want {
		t.Errorf("seen_player_sightings rows = %d, want %d", got, want)
	}
}

func TestRecordSightings_SameBucketDedup(t *testing.T) {
	kb := newTestKB(t)
	now := time.Date(2026, 5, 17, 14, 30, 0, 0, time.UTC)
	later := time.Date(2026, 5, 17, 14, 55, 0, 0, time.UTC)

	rec := SeenPlayer{
		PlayerID: "p1", Username: "u", ShipClass: "theoria",
		SystemID: "sys-A", POIID: "poi-X", Source: "get_nearby",
		SeenAt: now,
	}
	mustRecord(t, kb, rec)
	rec.SeenAt = later
	mustRecord(t, kb, rec)

	if got, want := countRows(t, kb, "seen_player_sightings"), 1; got != want {
		t.Errorf("sightings rows = %d, want %d (same hour bucket)", got, want)
	}

	var obs int
	if err := kb.db.QueryRow(
		"SELECT observation_count FROM seen_player_sightings WHERE player_id='p1'",
	).Scan(&obs); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if obs != 2 {
		t.Errorf("observation_count = %d, want 2", obs)
	}

	var sc int
	if err := kb.db.QueryRow(
		"SELECT sighting_count FROM seen_players WHERE player_id='p1'",
	).Scan(&sc); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if sc != 2 {
		t.Errorf("seen_players.sighting_count = %d, want 2", sc)
	}
}

func TestRecordSightings_DifferentBucket(t *testing.T) {
	kb := newTestKB(t)
	t1 := time.Date(2026, 5, 17, 14, 30, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 17, 15, 5, 0, 0, time.UTC)

	rec := SeenPlayer{
		PlayerID: "p1", Username: "u", ShipClass: "theoria",
		SystemID: "sys-A", POIID: "poi-X", Source: "get_nearby",
		SeenAt: t1,
	}
	mustRecord(t, kb, rec)
	rec.SeenAt = t2
	mustRecord(t, kb, rec)

	if got, want := countRows(t, kb, "seen_player_sightings"), 2; got != want {
		t.Errorf("sightings rows = %d, want %d (new hour bucket)", got, want)
	}
}

func TestRecordSightings_EmptyShipClassSkipsShipsTable(t *testing.T) {
	kb := newTestKB(t)
	mustRecord(t, kb, SeenPlayer{
		PlayerID: "p1", Username: "u", ShipClass: "",
		SystemID: "sys-A", POIID: "poi-X", Source: "chat_message",
		SeenAt: time.Now().UTC(),
	})

	if got := countRows(t, kb, "seen_player_ships"); got != 0 {
		t.Errorf("seen_player_ships rows = %d, want 0", got)
	}
}

func TestRecordSightings_IdentityOnlySkipsSightings(t *testing.T) {
	kb := newTestKB(t)
	mustRecord(t, kb, SeenPlayer{
		PlayerID: "p1", Username: "u",
		SystemID: "", POIID: "", Source: "chat_message",
		SeenAt: time.Now().UTC(),
	})

	if got := countRows(t, kb, "seen_players"); got != 1 {
		t.Errorf("seen_players rows = %d, want 1", got)
	}
	if got := countRows(t, kb, "seen_player_sightings"); got != 0 {
		t.Errorf("sightings rows = %d, want 0", got)
	}
}

func TestRecordSightings_EmptyPlayerIDDropped(t *testing.T) {
	kb := newTestKB(t)
	mustRecord(t, kb, SeenPlayer{
		PlayerID: "", Username: "u",
		SystemID: "sys-A", POIID: "poi-X", Source: "get_nearby",
		SeenAt: time.Now().UTC(),
	})

	if got := countRows(t, kb, "seen_players"); got != 0 {
		t.Errorf("seen_players rows = %d, want 0", got)
	}
}

func TestRecordSightings_EmptyFactionPreservesExisting(t *testing.T) {
	kb := newTestKB(t)
	now := time.Now().UTC()

	mustRecord(t, kb, SeenPlayer{
		PlayerID: "p1", Username: "u", FactionTag: "STRG",
		SeenAt: now,
	})
	mustRecord(t, kb, SeenPlayer{
		PlayerID: "p1", Username: "u", FactionTag: "",
		SeenAt: now.Add(time.Minute),
	})

	var tag string
	if err := kb.db.QueryRow(
		"SELECT faction_tag FROM seen_players WHERE player_id='p1'",
	).Scan(&tag); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if tag != "STRG" {
		t.Errorf("faction_tag = %q, want STRG (existing value preserved)", tag)
	}
}

func TestRecordSightings_POINullDistinctFromPopulated(t *testing.T) {
	kb := newTestKB(t)
	now := time.Now().UTC()

	// Same hour, same player, same system — one with POI, one without.
	mustRecord(t, kb,
		SeenPlayer{PlayerID: "p1", Username: "u", SystemID: "sys-A", POIID: "poi-X",
			Source: "get_nearby", SeenAt: now},
		SeenPlayer{PlayerID: "p1", Username: "u", SystemID: "sys-A", POIID: "",
			Source: "get_system_agents", SeenAt: now},
	)

	if got, want := countRows(t, kb, "seen_player_sightings"), 2; got != want {
		t.Errorf("sightings rows = %d, want %d (NULL POI distinct from populated)", got, want)
	}
}
