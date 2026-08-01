package assets

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// openTestStore opens a throwaway assets DB in a temp dir.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DBPath = filepath.Join(t.TempDir(), "assets.db")
	st, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return st
}

// TestIdentityRoundTrip pins the three-way identity map: player_id is the key,
// agent_id is our local label, username is the mutable in-game display name.
func TestIdentityRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	id := Identity{
		PlayerID: "a50924913cef881c5e4d14257589d9ba",
		AgentID:  "engineer-3",
		Username: "Arthur 'Artificer' Artis",
	}
	if err := st.UpsertIdentity(ctx, id, now); err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}

	got, ok, err := st.LookupIdentity(ctx, id.PlayerID)
	if err != nil || !ok {
		t.Fatalf("LookupIdentity: ok=%v err=%v", ok, err)
	}
	if got.AgentID != "engineer-3" || got.Username != "Arthur 'Artificer' Artis" {
		t.Errorf("round trip = %+v, want %+v", got, id)
	}
}

// TestUsernameChangeUpdatesInPlace pins that a renamed player keeps one row
// keyed on the stable player_id. Usernames change; the hex id does not.
func TestUsernameChangeUpdatesInPlace(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	id := Identity{PlayerID: "abc123", AgentID: "engineer-3", Username: "Old Name"}
	if err := st.UpsertIdentity(ctx, id, now); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	id.Username = "New Name"
	if err := st.UpsertIdentity(ctx, id, now.Add(time.Hour)); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM agents`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agents rows = %d, want 1 (rename must update in place)", n)
	}
	got, _, err := st.LookupIdentity(ctx, "abc123")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Username != "New Name" {
		t.Errorf("username = %q, want %q", got.Username, "New Name")
	}
}

// TestOpenIsIdempotent pins that reopening an existing DB re-runs migrations
// without error — workers restart constantly and each one calls Open.
func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DBPath = filepath.Join(dir, "assets.db")
	for i := range 3 {
		st, err := Open(cfg)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i, err)
		}
	}
}
