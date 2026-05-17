# Player Sightings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist every player observed via game commands and push events into three new SQLite tables in the shared knowledge base, with hourly sighting buckets and per-ship-class history.

**Architecture:** A new `PlayerObserver` callback on `*game.Client` fires from `handleResponse()` whenever a payload contains player records. A new `RecordSightings` method on `knowledge.Base` performs three UPSERTs per record inside a single transaction. `pkg/agent` provides a small `WirePlayerObserver(client, kb)` helper used by `cmd/tools/play_as` and the auto-* agents.

**Tech Stack:** Go 1.24+, SQLite (mattn/go-sqlite3), existing `pkg/game` / `pkg/knowledge` / `pkg/agent` packages.

**Spec:** `docs/superpowers/specs/2026-05-17-player-sightings-design.md`

---

## File Structure

**Create:**
- `pkg/game/observed_player.go` — `ObservedPlayer` value type + `PlayerObserver` func type.
- `pkg/game/observed_player_test.go` — unit tests for the notifier helpers (table-driven against synthetic payloads).
- `pkg/knowledge/seen_players.go` — `SeenPlayer` value type + `RecordSightings` impl.
- `pkg/knowledge/seen_players_test.go` — in-memory KB tests for upsert behavior.
- `pkg/agent/player_capture.go` — `WirePlayerObserver(*game.Client, knowledge.Base)` helper.

**Modify:**
- `pkg/knowledge/sqlite_migrations.go` — add migration version 34 with three tables.
- `pkg/knowledge/base.go` — add `RecordSightings` to the `Base` interface.
- `pkg/knowledge/memory.go` — no-op `RecordSightings` impl on `*MemoryKB`.
- `pkg/game/client.go` — add observer field/mutex, `SetPlayerObserver`, `notifyPlayers` helpers, and wire-ins in `handleResponse`.
- `cmd/tools/play_as/main.go` — call `agent.WirePlayerObserver` after KB construction.
- `cmd/auto-explorer/main.go` — same wiring, used as the canonical example.

**Out of scope (this plan):** wiring the other 10 auto-* binaries (auto-miner, auto-trader, auto-fighter, auto-pirate, auto-craftsman, auto-salvager, auto-prophet, auto-random, auto-llm-miner, auto-recall). Repeat the auto-explorer pattern in a follow-up.

---

## Task 1: Add migration 34 — three new tables

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` — insert new migration entry after version 33.
- Test: `pkg/knowledge/seen_players_test.go` (new) — asserts the tables exist after `NewSQLiteKB`.

- [ ] **Step 1: Write the failing test**

Create `pkg/knowledge/seen_players_test.go`:

```go
package knowledge

import (
	"testing"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestSeenPlayersMigrationCreatesTables -v`
Expected: FAIL — tables don't exist yet.

- [ ] **Step 3: Add migration 34**

Edit `pkg/knowledge/sqlite_migrations.go`. After the version 33 entry inside the slice returned by `migrations()`, append:

```go
		{
			version: 34,
			name:    "add_seen_players_tables",
			sql: `
				CREATE TABLE seen_players (
					player_id        TEXT PRIMARY KEY,
					username         TEXT NOT NULL,
					faction_id       TEXT,
					faction_tag      TEXT,
					clan_tag         TEXT,
					primary_color    TEXT,
					secondary_color  TEXT,
					status_message   TEXT,
					anonymous        INTEGER NOT NULL DEFAULT 0,
					first_seen_utc   TEXT NOT NULL,
					last_seen_utc    TEXT NOT NULL,
					sighting_count   INTEGER NOT NULL DEFAULT 1
				);
				CREATE INDEX seen_players_username  ON seen_players(username);
				CREATE INDEX seen_players_faction   ON seen_players(faction_id);
				CREATE INDEX seen_players_last_seen ON seen_players(last_seen_utc);

				CREATE TABLE seen_player_ships (
					player_id       TEXT NOT NULL,
					ship_class      TEXT NOT NULL,
					first_seen_utc  TEXT NOT NULL,
					last_seen_utc   TEXT NOT NULL,
					sighting_count  INTEGER NOT NULL DEFAULT 1,
					PRIMARY KEY (player_id, ship_class)
				);
				CREATE INDEX seen_player_ships_class ON seen_player_ships(ship_class);

				CREATE TABLE seen_player_sightings (
					player_id         TEXT NOT NULL,
					system_id         TEXT NOT NULL,
					poi_id            TEXT,
					bucket_hour_utc   TEXT NOT NULL,
					ship_class        TEXT,
					source            TEXT NOT NULL,
					in_combat         INTEGER NOT NULL DEFAULT 0,
					first_seen_utc    TEXT NOT NULL,
					last_seen_utc     TEXT NOT NULL,
					observation_count INTEGER NOT NULL DEFAULT 1,
					PRIMARY KEY (player_id, system_id, poi_id, bucket_hour_utc)
				);
				CREATE INDEX seen_sightings_system ON seen_player_sightings(system_id, bucket_hour_utc);
				CREATE INDEX seen_sightings_last   ON seen_player_sightings(last_seen_utc);
			`,
		},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestSeenPlayersMigrationCreatesTables -v`
Expected: PASS.

Also: `go test ./pkg/knowledge/...` to confirm no other migration-related tests regressed.

- [ ] **Step 5: Commit**

```bash
git add pkg/knowledge/sqlite_migrations.go pkg/knowledge/seen_players_test.go
git commit -m "feat(knowledge): migration 34 — seen_players, seen_player_ships, seen_player_sightings"
```

---

## Task 2: `SeenPlayer` type + `RecordSightings` (TDD)

**Files:**
- Create: `pkg/knowledge/seen_players.go`
- Modify: `pkg/knowledge/seen_players_test.go` (append new tests)

- [ ] **Step 1: Write the failing test — basic insert**

Append to `pkg/knowledge/seen_players_test.go`:

```go
import (
	"testing"
	"time"
)

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
		PlayerID:    "p1",
		Username:    "TraderUser6",
		ShipClass:   "theoria",
		FactionID:   "f-strg",
		FactionTag:  "STRG",
		SystemID:    "sys-treasure",
		POIID:       "poi-haven",
		Source:      "get_nearby",
		SeenAt:      now,
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestRecordSightings_FreshInsert -v`
Expected: FAIL — `SeenPlayer` and `RecordSightings` undefined.

- [ ] **Step 3: Implement `SeenPlayer` + `RecordSightings`**

Create `pkg/knowledge/seen_players.go`:

```go
package knowledge

import (
	"fmt"
	"time"
)

// SeenPlayer is a single player observation. Mirrors the shape of
// game.ObservedPlayer but lives in pkg/knowledge so this package does not
// import pkg/game. Callers adapt one to the other.
type SeenPlayer struct {
	PlayerID       string
	Username       string
	ShipClass      string
	FactionID      string
	FactionTag     string
	ClanTag        string
	PrimaryColor   string
	SecondaryColor string
	StatusMessage  string
	Anonymous      bool
	InCombat       bool

	SystemID string    // "" => identity-only, no sightings row
	POIID    string    // "" => system-scope sighting (NULL in DB)
	Source   string    // "get_nearby" | "get_system_agents" | "battle_alert" | ...
	SeenAt   time.Time
}

// RecordSightings inserts/updates rows in seen_players, seen_player_ships,
// and seen_player_sightings for each observation. All writes share a single
// transaction. Records with an empty PlayerID are silently dropped.
func (kb *SQLiteKB) RecordSightings(obs []SeenPlayer) error {
	if len(obs) == 0 {
		return nil
	}

	tx, err := kb.db.Begin()
	if err != nil {
		return fmt.Errorf("knowledge: begin sightings tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, o := range obs {
		if o.PlayerID == "" {
			continue
		}
		seenStr := o.SeenAt.UTC().Format(time.RFC3339)
		anon := boolToIntKB(o.Anonymous)

		if _, err := tx.Exec(`
INSERT INTO seen_players
	(player_id, username, faction_id, faction_tag, clan_tag,
	 primary_color, secondary_color, status_message, anonymous,
	 first_seen_utc, last_seen_utc, sighting_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(player_id) DO UPDATE SET
	username        = excluded.username,
	faction_id      = COALESCE(NULLIF(excluded.faction_id, ''), faction_id),
	faction_tag     = COALESCE(NULLIF(excluded.faction_tag, ''), faction_tag),
	clan_tag        = COALESCE(NULLIF(excluded.clan_tag, ''), clan_tag),
	primary_color   = COALESCE(NULLIF(excluded.primary_color, ''), primary_color),
	secondary_color = COALESCE(NULLIF(excluded.secondary_color, ''), secondary_color),
	status_message  = COALESCE(NULLIF(excluded.status_message, ''), status_message),
	anonymous       = excluded.anonymous,
	last_seen_utc   = excluded.last_seen_utc,
	sighting_count  = sighting_count + 1`,
			o.PlayerID, o.Username, o.FactionID, o.FactionTag, o.ClanTag,
			o.PrimaryColor, o.SecondaryColor, o.StatusMessage, anon,
			seenStr, seenStr,
		); err != nil {
			return fmt.Errorf("knowledge: upsert seen_players: %w", err)
		}

		if o.ShipClass != "" {
			if _, err := tx.Exec(`
INSERT INTO seen_player_ships
	(player_id, ship_class, first_seen_utc, last_seen_utc, sighting_count)
VALUES (?, ?, ?, ?, 1)
ON CONFLICT(player_id, ship_class) DO UPDATE SET
	last_seen_utc  = excluded.last_seen_utc,
	sighting_count = sighting_count + 1`,
				o.PlayerID, o.ShipClass, seenStr, seenStr,
			); err != nil {
				return fmt.Errorf("knowledge: upsert seen_player_ships: %w", err)
			}
		}

		if o.SystemID != "" {
			bucket := o.SeenAt.UTC().Truncate(time.Hour).Format(time.RFC3339)
			var poi any
			if o.POIID != "" {
				poi = o.POIID
			} else {
				poi = nil
			}
			if _, err := tx.Exec(`
INSERT INTO seen_player_sightings
	(player_id, system_id, poi_id, bucket_hour_utc, ship_class, source,
	 in_combat, first_seen_utc, last_seen_utc, observation_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(player_id, system_id, poi_id, bucket_hour_utc) DO UPDATE SET
	last_seen_utc     = excluded.last_seen_utc,
	in_combat         = excluded.in_combat,
	observation_count = observation_count + 1`,
				o.PlayerID, o.SystemID, poi, bucket, o.ShipClass, o.Source,
				boolToIntKB(o.InCombat), seenStr, seenStr,
			); err != nil {
				return fmt.Errorf("knowledge: upsert seen_player_sightings: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("knowledge: commit sightings: %w", err)
	}
	return nil
}

func boolToIntKB(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetSeenPlayer returns the stored row for a player_id, or (nil, nil)
// if no such row exists. The returned struct populates only the
// persistent identity fields (Username, FactionID, etc.) and the
// observation aggregates exposed via the schema — not Source / SeenAt,
// which are per-observation rather than per-player.
func (kb *SQLiteKB) GetSeenPlayer(playerID string) (*SeenPlayer, error) {
	if playerID == "" {
		return nil, nil
	}
	var (
		out     SeenPlayer
		factID  sql.NullString
		factTag sql.NullString
		clan    sql.NullString
		pcol    sql.NullString
		scol    sql.NullString
		status  sql.NullString
		anonInt int
		first   string
		last    string
	)
	err := kb.db.QueryRow(`
SELECT player_id, username, faction_id, faction_tag, clan_tag,
       primary_color, secondary_color, status_message, anonymous,
       first_seen_utc, last_seen_utc
FROM seen_players WHERE player_id = ?`, playerID,
	).Scan(
		&out.PlayerID, &out.Username, &factID, &factTag, &clan,
		&pcol, &scol, &status, &anonInt, &first, &last,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("knowledge: get seen_player %s: %w", playerID, err)
	}
	out.FactionID = nullStringValue(factID)
	out.FactionTag = nullStringValue(factTag)
	out.ClanTag = nullStringValue(clan)
	out.PrimaryColor = nullStringValue(pcol)
	out.SecondaryColor = nullStringValue(scol)
	out.StatusMessage = nullStringValue(status)
	out.Anonymous = anonInt != 0
	if t, perr := time.Parse(time.RFC3339, last); perr == nil {
		out.SeenAt = t
	}
	return &out, nil
}

func nullStringValue(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}
```

Add to the imports at the top of `pkg/knowledge/seen_players.go`:

```go
import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)
```

(If `pkg/knowledge/base_helpers.go` already exposes a comparable
`nullStringValue` / `stringOrEmpty`, reuse it and drop the local one.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestRecordSightings_FreshInsert -v`
Expected: PASS.

- [ ] **Step 5: Add the remaining behavior tests**

Append to `pkg/knowledge/seen_players_test.go`:

```go
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
```

- [ ] **Step 6: Run all new tests**

Run: `go test ./pkg/knowledge/ -run TestRecordSightings -v`
Expected: PASS for all six sub-tests.

- [ ] **Step 7: Run golangci-lint**

Run: `golangci-lint run ./pkg/knowledge/...`
Expected: no new findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/knowledge/seen_players.go pkg/knowledge/seen_players_test.go
git commit -m "feat(knowledge): SeenPlayer + RecordSightings with upsert semantics"
```

---

## Task 3: Add `RecordSightings` to `Base` interface + `MemoryKB` no-op

**Files:**
- Modify: `pkg/knowledge/base.go`
- Modify: `pkg/knowledge/memory.go`

- [ ] **Step 1: Inspect current Base interface**

Run: `grep -n "type Base interface" pkg/knowledge/base.go`
Note the line. Read 30 lines after to see existing methods so the new entry follows the same style.

- [ ] **Step 2: Add to interface**

In `pkg/knowledge/base.go`, inside the `Base` interface block, add (alphabetical order is not enforced — append to the bottom of the interface):

```go
	// RecordSightings persists a batch of player observations. Empty
	// PlayerIDs are dropped. Implementations may choose to no-op (e.g.
	// the in-memory KB).
	RecordSightings(obs []SeenPlayer) error
```

- [ ] **Step 3: Add no-op on MemoryKB**

In `pkg/knowledge/memory.go`, append:

```go
// RecordSightings is a no-op for the in-memory KB — sightings are
// persistent-only state and the memory backend exists for tests/agents
// that don't care to retain them.
func (m *MemoryKB) RecordSightings(_ []SeenPlayer) error {
	return nil
}
```

- [ ] **Step 4: Verify build + tests**

Run: `go build ./pkg/knowledge/... && go test ./pkg/knowledge/...`
Expected: PASS, no compile errors.

- [ ] **Step 5: Commit**

```bash
git add pkg/knowledge/base.go pkg/knowledge/memory.go
git commit -m "feat(knowledge): add RecordSightings to Base interface (no-op on MemoryKB)"
```

---

## Task 4: `ObservedPlayer` + `SetPlayerObserver` on `*game.Client`

**Files:**
- Create: `pkg/game/observed_player.go`
- Modify: `pkg/game/client.go` — add field + setter
- Create: `pkg/game/observed_player_test.go`

- [ ] **Step 1: Write the failing test for SetPlayerObserver**

Create `pkg/game/observed_player_test.go`:

```go
package game

import (
	"sync"
	"testing"
)

func TestSetPlayerObserver_StoresCallback(t *testing.T) {
	c := &Client{}

	var mu sync.Mutex
	var fired bool
	c.SetPlayerObserver(func(_ []ObservedPlayer) {
		mu.Lock()
		fired = true
		mu.Unlock()
	})

	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()

	if cb == nil {
		t.Fatal("playerObserver not registered")
	}
	cb(nil)

	mu.Lock()
	defer mu.Unlock()
	if !fired {
		t.Error("callback not invoked")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestSetPlayerObserver -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Create `observed_player.go`**

Create `pkg/game/observed_player.go`:

```go
package game

import "time"

// ObservedPlayer is a single player record extracted from a server
// response or push event. It is the input to a PlayerObserver callback.
//
// Fields ShipClass, POIID may be empty when the source doesn't carry
// them (e.g. chat_message has no ship/POI). SystemID is empty when the
// observation has no spatial context (chat) — the recorder uses that to
// decide whether to write a sightings row.
type ObservedPlayer struct {
	PlayerID       string
	Username       string
	ShipClass      string
	FactionID      string
	FactionTag     string
	ClanTag        string
	PrimaryColor   string
	SecondaryColor string
	StatusMessage  string
	Anonymous      bool
	InCombat       bool

	SystemID string
	POIID    string
	Source   string
	SeenAt   time.Time
}

// PlayerObserver receives batches of player observations as the game
// client parses incoming server messages. Implementations must not
// block — the callback is invoked from the response-handling goroutine.
type PlayerObserver func(obs []ObservedPlayer)
```

- [ ] **Step 4: Add observer field + setter to Client**

In `pkg/game/client.go`, after the `onChatMessage`/`onChatMu` fields (around line 124), add:

```go
	// Player observer callback — fired when handleResponse parses a
	// payload containing player records (get_nearby, get_system_agents,
	// battle alerts, chat). See pkg/game/observed_player.go.
	playerObserver   PlayerObserver
	playerObserverMu sync.RWMutex
```

After the `SetOnChatMessage` method (around line 355), add:

```go
// SetPlayerObserver registers a callback that fires when handleResponse
// parses a payload containing player records. Used by consumers (play_as
// REPL, agent runners) to persist sightings into a knowledge base.
func (c *Client) SetPlayerObserver(fn PlayerObserver) {
	c.playerObserverMu.Lock()
	defer c.playerObserverMu.Unlock()
	c.playerObserver = fn
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestSetPlayerObserver -v`
Expected: PASS.

- [ ] **Step 6: Build the whole tree to catch unrelated breakage**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 7: Commit**

```bash
git add pkg/game/observed_player.go pkg/game/observed_player_test.go pkg/game/client.go
git commit -m "feat(game): ObservedPlayer + SetPlayerObserver callback on *Client"
```

---

## Task 5: `notifyPlayers` helpers on `*Client`

**Files:**
- Modify: `pkg/game/client.go` (or add a new file `pkg/game/player_notify.go` — see Step 3)
- Modify: `pkg/game/observed_player_test.go`

- [ ] **Step 1: Write failing tests for notifier behavior**

Append to `pkg/game/observed_player_test.go`:

```go
import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func captureObserver(t *testing.T, c *Client) *[]ObservedPlayer {
	t.Helper()
	var captured []ObservedPlayer
	c.SetPlayerObserver(func(obs []ObservedPlayer) {
		captured = append(captured, obs...)
	})
	return &captured
}

func TestNotifyPlayers_StampsContextFields(t *testing.T) {
	c := &Client{}
	c.state.SystemID = "sys-treasure"
	got := captureObserver(t, c)

	players := []serverapi.NearbyPlayer{
		{PlayerID: "p1", Username: "u1", ShipClass: "theoria", FactionTag: "STRG"},
		{PlayerID: "p2", Username: "u2", ShipClass: "viper"},
	}
	c.notifyPlayers("get_nearby", players, "poi-haven")

	if len(*got) != 2 {
		t.Fatalf("got %d observations, want 2", len(*got))
	}
	for _, o := range *got {
		if o.SystemID != "sys-treasure" {
			t.Errorf("SystemID=%q, want sys-treasure", o.SystemID)
		}
		if o.POIID != "poi-haven" {
			t.Errorf("POIID=%q, want poi-haven", o.POIID)
		}
		if o.Source != "get_nearby" {
			t.Errorf("Source=%q, want get_nearby", o.Source)
		}
		if o.SeenAt.IsZero() {
			t.Error("SeenAt is zero")
		}
	}
}

func TestNotifyPlayers_NoObserverIsNoOp(t *testing.T) {
	c := &Client{}
	// Should not panic.
	c.notifyPlayers("get_nearby", []serverapi.NearbyPlayer{{PlayerID: "p1"}}, "")
}

func TestNotifyPlayersFromBattle_MarksInCombat(t *testing.T) {
	c := &Client{}
	c.state.SystemID = "sys-A"
	got := captureObserver(t, c)

	parts := []serverapi.BattleParticipant{
		{PlayerID: "p1", Username: "u1", ShipClass: "theoria", FactionTag: "STRG"},
	}
	c.notifyPlayersFromBattle("battle_alert", parts)

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	if !(*got)[0].InCombat {
		t.Error("InCombat=false, want true for battle source")
	}
	if (*got)[0].Source != "battle_alert" {
		t.Errorf("Source=%q, want battle_alert", (*got)[0].Source)
	}
}

func TestNotifyPlayerFromChat_NoShipNoPOI(t *testing.T) {
	c := &Client{}
	c.state.SystemID = "sys-A"
	got := captureObserver(t, c)

	c.notifyPlayerFromChat(serverapi.ChatMessage{
		SenderID:   "p1",
		Sender:     "Director-General Darya Lim",
		FactionTag: "EMPIRE",
	})

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	o := (*got)[0]
	if o.ShipClass != "" {
		t.Errorf("ShipClass=%q, want empty", o.ShipClass)
	}
	if o.POIID != "" {
		t.Errorf("POIID=%q, want empty", o.POIID)
	}
	if o.Source != "chat_message" {
		t.Errorf("Source=%q, want chat_message", o.Source)
	}
}

// payloadMarshal is a test helper that JSON-roundtrips a value through
// map[string]any so it matches the shape handleResponse receives.
func payloadMarshal(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./pkg/game/ -run "TestNotifyPlayer" -v`
Expected: FAIL — `notifyPlayers`, `notifyPlayersFromBattle`, `notifyPlayerFromChat` undefined.

- [ ] **Step 3: Create the notifier helpers**

Create `pkg/game/player_notify.go`:

```go
package game

import (
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// notifyPlayers builds ObservedPlayer records from a NearbyPlayer slice,
// stamps system/POI/source/time context, and dispatches to the registered
// observer. Silent no-op when no observer is registered.
func (c *Client) notifyPlayers(source string, players []serverapi.NearbyPlayer, poiID string) {
	if len(players) == 0 {
		return
	}
	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()
	if cb == nil {
		return
	}

	c.mu.RLock()
	systemID := c.state.SystemID
	c.mu.RUnlock()

	now := time.Now().UTC()
	out := make([]ObservedPlayer, 0, len(players))
	for _, p := range players {
		out = append(out, ObservedPlayer{
			PlayerID:       p.PlayerID,
			Username:       p.Username,
			ShipClass:      p.ShipClass,
			FactionID:      p.FactionID,
			FactionTag:     p.FactionTag,
			ClanTag:        p.ClanTag,
			PrimaryColor:   p.PrimaryColor,
			SecondaryColor: p.SecondaryColor,
			StatusMessage:  p.StatusMessage,
			Anonymous:      p.Anonymous,
			InCombat:       p.InCombat,
			SystemID:       systemID,
			POIID:          poiID,
			Source:         source,
			SeenAt:         now,
		})
	}
	cb(out)
}

// notifyPlayersFromBattle adapts BattleParticipant records (which lack
// some NearbyPlayer fields and always imply InCombat=true).
func (c *Client) notifyPlayersFromBattle(source string, parts []serverapi.BattleParticipant) {
	if len(parts) == 0 {
		return
	}
	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()
	if cb == nil {
		return
	}

	c.mu.RLock()
	systemID := c.state.SystemID
	c.mu.RUnlock()

	now := time.Now().UTC()
	out := make([]ObservedPlayer, 0, len(parts))
	for _, p := range parts {
		out = append(out, ObservedPlayer{
			PlayerID:   p.PlayerID,
			Username:   p.Username,
			ShipClass:  p.ShipClass,
			FactionTag: p.FactionTag,
			InCombat:   true,
			SystemID:   systemID,
			Source:     source,
			SeenAt:     now,
		})
	}
	cb(out)
}

// notifyPlayerFromChat emits a single identity-only ObservedPlayer for
// the sender of a chat_message push. ShipClass / POIID / SystemID are
// intentionally left empty so the recorder upserts seen_players only and
// skips the sightings table — per the spec's identity-only decision for
// chat. (ChatMessage does carry SystemID/POIID but we deliberately
// ignore them here.)
func (c *Client) notifyPlayerFromChat(msg serverapi.ChatMessage) {
	if msg.SenderID == "" {
		return
	}
	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()
	if cb == nil {
		return
	}

	cb([]ObservedPlayer{{
		PlayerID: msg.SenderID,
		Username: msg.Sender,
		Source:   "chat_message",
		SeenAt:   time.Now().UTC(),
	}})
}
```

(`serverapi.ChatMessage` does not carry a `FactionTag` field — verified — so the chat notifier only populates player_id and username.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/game/ -run "TestNotifyPlayer|TestSetPlayerObserver" -v`
Expected: PASS for all four.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/player_notify.go pkg/game/observed_player_test.go
git commit -m "feat(game): notifyPlayers / notifyPlayersFromBattle / notifyPlayerFromChat helpers"
```

---

## Task 6: Wire `notifyPlayers` into `handleResponse` — `get_nearby` and `get_system_agents`

**Files:**
- Modify: `pkg/game/client.go`
- Modify: `pkg/game/observed_player_test.go`

- [ ] **Step 1: Write failing integration tests**

Append to `pkg/game/observed_player_test.go`:

```go
import (
	"github.com/rsned/spacemolt/internal/protocol"
)

func TestHandleResponse_FiresOnGetNearby(t *testing.T) {
	c := &Client{}
	c.state.SystemID = "sys-A"
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"action": "get_nearby",
		"poi_id": "poi-haven",
		"nearby": []serverapi.NearbyPlayer{
			{PlayerID: "p1", Username: "u1", ShipClass: "theoria"},
		},
	})
	c.handleResponse(protocol.Message{Type: protocol.TypeOK, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("observer got %d, want 1", len(*got))
	}
	if (*got)[0].POIID != "poi-haven" {
		t.Errorf("POIID=%q, want poi-haven", (*got)[0].POIID)
	}
	if (*got)[0].Source != "get_nearby" {
		t.Errorf("Source=%q, want get_nearby", (*got)[0].Source)
	}
}

func TestHandleResponse_FiresOnGetSystemAgents(t *testing.T) {
	c := &Client{}
	c.state.SystemID = "sys-A"
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"agents": []serverapi.NearbyPlayer{
			{PlayerID: "p1", Username: "u1", ShipClass: "viper"},
			{PlayerID: "p2", Username: "u2", ShipClass: "theoria"},
		},
		"system_id": "sys-A",
		"count":     2,
	})
	c.handleResponse(protocol.Message{Type: protocol.TypeOK, Payload: payload})

	if len(*got) != 2 {
		t.Fatalf("observer got %d, want 2", len(*got))
	}
	if (*got)[0].Source != "get_system_agents" {
		t.Errorf("Source=%q, want get_system_agents", (*got)[0].Source)
	}
	if (*got)[0].POIID != "" {
		t.Errorf("POIID=%q, want empty (system-scope)", (*got)[0].POIID)
	}
}
```

- [ ] **Step 2: Run tests to confirm failure**

Run: `go test ./pkg/game/ -run "TestHandleResponse_FiresOn" -v`
Expected: FAIL — observer not yet wired.

- [ ] **Step 3: Wire the two cases inside `handleResponse`**

In `pkg/game/client.go`, locate the existing `nearby` storeKey block around line 3870. Immediately after the existing `shouldStore = true` for `nearby`, add the notifier dispatch:

```go
			// Store nearby players (from get_nearby response)
			if _, hasNearby := resp.Payload["nearby"]; hasNearby {
				if storeKey == "" {
					storeKey = "nearby"
				}
				shouldStore = true

				var players []serverapi.NearbyPlayer
				if unmarshalPayloadKey(resp.Payload, "nearby", &players) {
					poiID, _ := resp.Payload["poi_id"].(string)
					c.notifyPlayers("get_nearby", players, poiID)
				}
			}
```

Locate the existing `get_system_agents` detection block around line 3748 inside `handleResponse` (`if _, hasAgents := resp.Payload["agents"]; hasAgents`). Append after the `shouldStore = true`:

```go
		if _, hasAgents := resp.Payload["agents"]; hasAgents {
			if _, hasSystemID := resp.Payload["system_id"]; hasSystemID {
				if storeKey == "" {
					storeKey = "get_system_agents"
				}
				shouldStore = true

				var players []serverapi.NearbyPlayer
				if unmarshalPayloadKey(resp.Payload, "agents", &players) {
					c.notifyPlayers("get_system_agents", players, "")
				}
			}
		}
```

- [ ] **Step 4: Run tests to confirm pass**

Run: `go test ./pkg/game/ -run "TestHandleResponse_FiresOn|TestNotifyPlayer|TestSetPlayerObserver" -v`
Expected: PASS all.

Also: `go test ./pkg/game/...` — confirm nothing else regressed.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client.go pkg/game/observed_player_test.go
git commit -m "feat(game): emit player sightings from get_nearby & get_system_agents"
```

---

## Task 7: Wire battle + combat update + chat into `handleResponse`

**Files:**
- Modify: `pkg/game/client.go`
- Modify: `pkg/game/observed_player_test.go`

- [ ] **Step 1: Failing tests for the three new wire-ins**

Append to `pkg/game/observed_player_test.go`:

```go
func TestHandleResponse_FiresOnBattleAlert(t *testing.T) {
	c := &Client{}
	c.state.SystemID = "sys-A"
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"battle_id": "b1",
		"system_id": "sys-A",
		"participants": []serverapi.BattleParticipant{
			{PlayerID: "p1", Username: "u1", ShipClass: "viper", FactionTag: "PIRATE"},
		},
	})
	c.handleResponse(protocol.Message{Type: protocol.TypeBattleAlert, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	if !(*got)[0].InCombat {
		t.Error("expected InCombat=true")
	}
}

func TestHandleResponse_FiresOnChatMessage(t *testing.T) {
	c := &Client{}
	c.state.SystemID = "sys-A"
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"channel":   "system",
		"sender":    "Director-General Darya Lim",
		"sender_id": "p1",
		"content":   "Federation notice ...",
	})
	c.handleResponse(protocol.Message{Type: protocol.TypeChatMessage, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	if (*got)[0].ShipClass != "" {
		t.Errorf("ShipClass=%q, want empty for chat", (*got)[0].ShipClass)
	}
	if (*got)[0].SystemID != "" {
		t.Errorf("SystemID=%q, want empty for identity-only chat", (*got)[0].SystemID)
	}
	if (*got)[0].Source != "chat_message" {
		t.Errorf("Source=%q, want chat_message", (*got)[0].Source)
	}
}

func TestHandleResponse_FiresOnGetSystemOnlinePlayers(t *testing.T) {
	c := &Client{}
	c.state.SystemID = "sys-A"
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"system_id": "sys-A",
		"name":      "Treasure Cache",
		"online_players": []serverapi.NearbyPlayer{
			{PlayerID: "p1", Username: "u1", ShipClass: "theoria"},
		},
	})
	c.handleResponse(protocol.Message{Type: protocol.TypeOK, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("got %d, want 1", len(*got))
	}
	if (*got)[0].Source != "get_system" {
		t.Errorf("Source=%q, want get_system", (*got)[0].Source)
	}
	if (*got)[0].POIID != "" {
		t.Errorf("POIID=%q, want empty (system-scope)", (*got)[0].POIID)
	}
}
```

- [ ] **Step 2: Run tests to confirm failure**

Run: `go test ./pkg/game/ -run "TestHandleResponse_FiresOnBattleAlert|TestHandleResponse_FiresOnChatMessage" -v`
Expected: FAIL.

- [ ] **Step 3: Add notifier call in `TypeBattleAlert`**

In `pkg/game/client.go`, modify the `case protocol.TypeBattleAlert:` block (around line 2322) to also extract participants and fire the notifier:

```go
		case protocol.TypeBattleAlert:
			msg, _ := resp.Payload["message"].(string)
			battleID, _ := resp.Payload["battle_id"].(string)
			systemID, _ := resp.Payload["system_id"].(string)
			c.debugLogger.Printf("[BATTLE ALERT] %s (battle=%s system=%s)", msg, battleID, systemID)

			var parts []serverapi.BattleParticipant
			if unmarshalPayloadKey(resp.Payload, "participants", &parts) {
				c.notifyPlayersFromBattle("battle_alert", parts)
			}
```

- [ ] **Step 4: Add notifier call in `TypeCombatUpdate`**

Locate `case protocol.TypeCombatUpdate:` (around line 2314). After whatever processing is already there (read the existing block first to keep order), append:

```go
			var parts []serverapi.BattleParticipant
			if unmarshalPayloadKey(resp.Payload, "participants", &parts) {
				c.notifyPlayersFromBattle("combat_update", parts)
			}
```

- [ ] **Step 4b: Wire `online_players` on `get_system` responses**

Still inside `handleResponse`'s `TypeOK` branch (the same area that already
handles `nearby`/`agents`), add a sibling detection block:

```go
		if _, hasOnline := resp.Payload["online_players"]; hasOnline {
			var players []serverapi.NearbyPlayer
			if unmarshalPayloadKey(resp.Payload, "online_players", &players) {
				c.notifyPlayers("get_system", players, "")
			}
		}
```

(No `storeKey` change — this is an additive notifier only; the get_system
response is already stored under its own existing key.)

- [ ] **Step 5: Add notifier call in `TypeChatMessage`**

Inside the existing `case protocol.TypeChatMessage:` block (around line 2331), inside the existing `if err := json.Unmarshal(...)` success branch where `cb` is already invoked, add a parallel observer call. Easier: after the existing dispatch, add:

```go
		case protocol.TypeChatMessage:
			var chatMsg serverapi.ChatMessage
			if data, err := json.Marshal(resp.Payload); err == nil {
				if err := json.Unmarshal(data, &chatMsg); err == nil {
					c.onChatMu.RLock()
					cb := c.onChatMessage
					c.onChatMu.RUnlock()
					if cb != nil {
						cb(chatMsg)
					}
					c.notifyPlayerFromChat(chatMsg)
				}
			}
			// ... existing debug logger block unchanged ...
```

- [ ] **Step 6: Run tests to confirm pass**

Run: `go test ./pkg/game/ -run "TestHandleResponse_" -v`
Expected: PASS all four.

Then `go test ./pkg/game/...` for full-package sanity.

- [ ] **Step 7: Run golangci-lint**

Run: `golangci-lint run ./pkg/game/...`
Expected: no new findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/game/client.go pkg/game/observed_player_test.go
git commit -m "feat(game): emit player sightings from battle_alert, combat_update, chat_message"
```

---

## Task 8: `WirePlayerObserver` helper in `pkg/agent`

**Files:**
- Create: `pkg/agent/player_capture.go`
- Create: `pkg/agent/player_capture_test.go`

- [ ] **Step 1: Failing test**

Create `pkg/agent/player_capture_test.go`:

```go
package agent

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestWirePlayerObserver_RecordsThroughKB(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })

	c := &game.Client{}
	WirePlayerObserver(c, kb)

	cb := c.PlayerObserver()
	if cb == nil {
		t.Fatal("WirePlayerObserver did not register an observer")
	}
	cb([]game.ObservedPlayer{{
		PlayerID:  "p1",
		Username:  "TraderUser6",
		ShipClass: "theoria",
		SystemID:  "sys-A",
		POIID:     "poi-X",
		Source:    "get_nearby",
		SeenAt:    time.Now().UTC(),
	}})

	got, err := kb.GetSeenPlayer("p1")
	if err != nil {
		t.Fatalf("GetSeenPlayer: %v", err)
	}
	if got == nil {
		t.Fatal("GetSeenPlayer returned nil — observer did not record")
	}
	if got.Username != "TraderUser6" {
		t.Errorf("Username = %q, want TraderUser6", got.Username)
	}
}
```

This test relies on two small surface additions to existing types so
external packages can verify wiring without poking unexported fields:

**`pkg/game/observed_player.go`** — add an accessor:

```go
// PlayerObserver returns the currently registered observer (nil if none).
// Exposed for external callers (notably tests and runtime introspection)
// to verify wiring without reaching into unexported fields.
func (c *Client) PlayerObserver() PlayerObserver {
	c.playerObserverMu.RLock()
	defer c.playerObserverMu.RUnlock()
	return c.playerObserver
}
```

No new imports required.

(No additional method needed on `knowledge.SQLiteKB` — the test verifies
the wired callback ran by triggering a second `RecordSightings` and
relying on the upsert semantics already tested in Task 2.)

- [ ] **Step 2: Run test to confirm failure**

Run: `go test ./pkg/agent/ -run TestWirePlayerObserver -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement helpers**

First add the two test helpers from Step 1's note to their respective files.

Then create `pkg/agent/player_capture.go`:

```go
package agent

import (
	"log"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// WirePlayerObserver registers a PlayerObserver on the given game client
// that persists each batch of observations to the knowledge base. Errors
// from RecordSightings are logged and swallowed — a failed write must
// never break the game response path.
func WirePlayerObserver(c *game.Client, kb knowledge.Base) {
	if c == nil || kb == nil {
		return
	}
	c.SetPlayerObserver(func(obs []game.ObservedPlayer) {
		if len(obs) == 0 {
			return
		}
		seen := make([]knowledge.SeenPlayer, 0, len(obs))
		for _, o := range obs {
			seen = append(seen, knowledge.SeenPlayer{
				PlayerID:       o.PlayerID,
				Username:       o.Username,
				ShipClass:      o.ShipClass,
				FactionID:      o.FactionID,
				FactionTag:     o.FactionTag,
				ClanTag:        o.ClanTag,
				PrimaryColor:   o.PrimaryColor,
				SecondaryColor: o.SecondaryColor,
				StatusMessage:  o.StatusMessage,
				Anonymous:      o.Anonymous,
				InCombat:       o.InCombat,
				SystemID:       o.SystemID,
				POIID:          o.POIID,
				Source:         o.Source,
				SeenAt:         o.SeenAt,
			})
		}
		if err := kb.RecordSightings(seen); err != nil {
			log.Printf("[seen] RecordSightings: %v", err)
		}
	})
}
```

- [ ] **Step 4: Run test**

Run: `go test ./pkg/agent/ -run TestWirePlayerObserver -v`
Expected: PASS.

Also: `go test ./pkg/agent/...` for full-package sanity, and `go build ./...`.

- [ ] **Step 5: Lint**

Run: `golangci-lint run ./pkg/agent/... ./pkg/game/... ./pkg/knowledge/...`
Expected: no new findings.

- [ ] **Step 6: Commit**

```bash
git add pkg/agent/player_capture.go pkg/agent/player_capture_test.go \
        pkg/game/observed_player.go pkg/knowledge/seen_players.go
git commit -m "feat(agent): WirePlayerObserver helper bridging Client -> knowledge.Base"
```

---

## Task 9: Wire `play_as` REPL

**Files:**
- Modify: `cmd/tools/play_as/main.go`

- [ ] **Step 1: Locate the existing client + kb construction**

Run: `grep -n "globalClient\s*=\|kb\s*[:=]\|knowledge.NewSQLiteKB\|knowledge.Base" cmd/tools/play_as/main.go | head -20`

Identify the line where both `globalClient` (a `*game.Client`) and the KB are available together. (If they're constructed in different places, pick the spot where the KB becomes known to the client — typically just after the second of the two is set up.)

- [ ] **Step 2: Add the wiring call**

Insert immediately after the KB is constructed and `globalClient` is the concrete `*game.Client`:

```go
import (
	"github.com/rsned/spacemolt/pkg/agent"
	// ... existing imports
)

// Persist every encountered player to the shared KB so REPL queries and
// agents can mine sighting history. See spec
// docs/superpowers/specs/2026-05-17-player-sightings-design.md.
agent.WirePlayerObserver(globalClient, kb)
```

If `globalClient` is typed as `game.GameClient` (interface), type-assert: `if c, ok := globalClient.(*game.Client); ok { agent.WirePlayerObserver(c, kb) }`.

- [ ] **Step 3: Build**

Run: `go build ./cmd/tools/play_as/...`
Expected: clean.

- [ ] **Step 4: Smoke test (manual, optional)**

Run the REPL against a live session: `go run ./cmd/tools/play_as/ <login flags>`, log in, run `get_nearby` or `get_system_agents`, exit, then:

```bash
sqlite3 ~/.spacemolt/knowledge.db 'SELECT player_id, username, last_seen_utc FROM seen_players ORDER BY last_seen_utc DESC LIMIT 10;'
```

Expected: rows present for each player seen in the session. (Skip if no live access — covered by Task 8's unit test.)

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "feat(play_as): record every observed player to the shared KB"
```

---

## Task 10: Wire `auto-explorer` as the canonical agent example

**Files:**
- Modify: `cmd/auto-explorer/main.go`

- [ ] **Step 1: Locate Client + KB construction**

Run: `grep -n "wsClient\b\|knowledge.NewSQLiteKB\|knowledge.Base" cmd/auto-explorer/main.go | head`

From the scouting: the `*game.Client` is named `wsClient` around line 1606, and the KB is constructed around line 1699.

- [ ] **Step 2: Insert the wiring call**

After both are constructed and non-nil, add (with appropriate import of `pkg/agent` if not already present):

```go
if wsClient != nil && kb != nil {
	agent.WirePlayerObserver(wsClient, kb)
}
```

- [ ] **Step 3: Build**

Run: `go build ./cmd/auto-explorer/...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add cmd/auto-explorer/main.go
git commit -m "feat(auto-explorer): record every observed player to the shared KB"
```

---

## Task 11: Final verification

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 2: Run the full lint**

Run: `golangci-lint run ./...`
Expected: no new findings introduced by this branch.

- [ ] **Step 3: Build everything**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 4: Skim git log**

Run: `git log --oneline main..HEAD`
Expected: 10 focused commits (one per task) — easy to review or revert individually.

---

## Follow-up (NOT in scope)

- Wire `WirePlayerObserver` into the other 10 auto-* binaries (auto-miner, auto-trader, auto-fighter, auto-pirate, auto-craftsman, auto-salvager, auto-prophet, auto-random, auto-llm-miner, auto-recall). Same one-line pattern as Task 10.
- Add `play_as` REPL commands to query sightings (`players show <username>`, `players seen-in <system>`, `players ships <username>`).
- Add agent decision hooks that read from `seen_players` (e.g., auto-pirate "avoid known-strong fighters", auto-trader "find frequent traders").
- Retention/pruning policy if sightings tables grow large.
