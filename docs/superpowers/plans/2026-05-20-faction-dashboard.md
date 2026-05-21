# Faction Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collect comprehensive per-faction data into the shared knowledge base and render a tabbed, static HTML dashboard (one page per faction + index), with the same collection reusable from a `play_as` command.

**Architecture:** A shared `pkg/faction` Collector reads a connected `game.GameClient` and persists faction data to `*knowledge.SQLiteKB` (new migration v35, replace-within-scope upserts). A new `cmd/tools/faction-dashboard` CLI connects faction member agents, runs the Collector, then renders static HTML via `html/template`. A `play_as` REPL command `update_faction_data` calls the same Collector ad hoc.

**Tech Stack:** Go 1.24+, `modernc.org/sqlite`, `html/template`, existing `pkg/game` WebSocket client, `pkg/knowledge` KB.

**Phasing:** Three phases, each ending at a working, committable, tested milestone:
- **Phase 1 — Persistence** (Tasks 1–4): schema + types + store/load on `*SQLiteKB`.
- **Phase 2 — Collection** (Tasks 5–9): serverapi structs, `pkg/faction` Collector, `play_as` command.
- **Phase 3 — Rendering** (Tasks 10–14): `faction-dashboard` renderer, index, CLI, README.

**Key decisions (from the design spec `docs/superpowers/specs/2026-05-20-faction-dashboard-design.md`):**
- Current-state only (no dated history); each row carries `captured_utc`.
- Best-effort collection, no agent travel; the KB is the merge point (station-scoped data keyed by `(faction_id, base_id)`).
- Faction store/load methods live on `*knowledge.SQLiteKB` only (NOT the `Base` interface, to avoid bloating `MemoryKB`). `pkg/faction` depends on a small consumer-defined interface satisfied by `*SQLiteKB`.
- The Collector uses submit-and-read against the concrete `*game.Client` (WS transport), matching daily-summary's storage-capture pattern.

---

## Phase 1 — Persistence

### Task 1: KB migration v35 (faction tables)

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` (append one entry to the slice returned by `migrations()`, after the `version: 34` entry near line 163)

- [ ] **Step 1: Add the migration entry**

Insert this entry as the last element of the slice in `migrations()` (immediately before the closing `}` of the returned `[]Migration{...}`):

```go
		{
			version: 35,
			name:    "add_faction_dashboard_tables",
			sql: `
				CREATE TABLE factions (
					faction_id      TEXT PRIMARY KEY,
					name            TEXT,
					tag             TEXT,
					leader_id       TEXT,
					leader_username TEXT,
					treasury        INTEGER,
					member_count    INTEGER,
					owned_bases     INTEGER,
					description     TEXT,
					charter         TEXT,
					emblem          TEXT,
					primary_color   TEXT,
					secondary_color TEXT,
					founded_utc     TEXT,
					intel_systems   INTEGER,
					intel_trade     INTEGER,
					captured_utc    TEXT NOT NULL
				);

				CREATE TABLE faction_members (
					faction_id    TEXT NOT NULL,
					player_id     TEXT NOT NULL,
					username      TEXT,
					role          TEXT,
					joined_utc    TEXT,
					last_seen_utc TEXT,
					is_online     INTEGER NOT NULL DEFAULT 0,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, player_id)
				);

				CREATE TABLE faction_relations (
					faction_id        TEXT NOT NULL,
					target_faction_id TEXT NOT NULL,
					target_name       TEXT,
					target_tag        TEXT,
					kind              TEXT NOT NULL,
					reason            TEXT,
					terms             TEXT,
					our_kills         INTEGER NOT NULL DEFAULT 0,
					their_kills       INTEGER NOT NULL DEFAULT 0,
					started_utc       TEXT,
					captured_utc      TEXT NOT NULL,
					PRIMARY KEY (faction_id, target_faction_id, kind)
				);

				CREATE TABLE faction_bases (
					faction_id    TEXT NOT NULL,
					base_id       TEXT NOT NULL,
					base_name     TEXT,
					system_id     TEXT,
					system_name   TEXT,
					poi_id        TEXT,
					services_json TEXT,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id)
				);

				CREATE TABLE faction_facilities (
					faction_id    TEXT NOT NULL,
					base_id       TEXT NOT NULL,
					facility_id   TEXT NOT NULL,
					facility_type TEXT,
					category      TEXT,
					level         INTEGER NOT NULL DEFAULT 0,
					status        TEXT,
					recipe_id     TEXT,
					details_json  TEXT,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, facility_id)
				);

				CREATE TABLE faction_storage (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					credits      INTEGER NOT NULL DEFAULT 0,
					item_count   INTEGER NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id)
				);

				CREATE TABLE faction_storage_items (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					item_id      TEXT NOT NULL,
					name         TEXT,
					quantity     REAL NOT NULL DEFAULT 0,
					size         INTEGER NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, item_id)
				);

				CREATE TABLE faction_orders (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					order_id     TEXT NOT NULL,
					side         TEXT,
					item_id      TEXT,
					item_name    TEXT,
					price_each   REAL NOT NULL DEFAULT 0,
					quantity     REAL NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, order_id)
				);

				CREATE TABLE faction_missions (
					faction_id         TEXT NOT NULL,
					base_id            TEXT NOT NULL,
					mission_id         TEXT NOT NULL,
					title              TEXT,
					type               TEXT,
					description        TEXT,
					giver_name         TEXT,
					rewards_json       TEXT,
					objectives_json    TEXT,
					assigned_player_id TEXT,
					expiration_utc     TEXT,
					captured_utc       TEXT NOT NULL,
					PRIMARY KEY (faction_id, mission_id)
				);

				CREATE TABLE faction_rooms (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					room_id      TEXT NOT NULL,
					name         TEXT,
					access       TEXT,
					description  TEXT,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, room_id)
				);

				CREATE INDEX faction_members_faction   ON faction_members(faction_id);
				CREATE INDEX faction_relations_faction  ON faction_relations(faction_id);
				CREATE INDEX faction_facilities_faction ON faction_facilities(faction_id);
				CREATE INDEX faction_storage_faction    ON faction_storage(faction_id);
				CREATE INDEX faction_orders_faction     ON faction_orders(faction_id);
				CREATE INDEX faction_missions_faction   ON faction_missions(faction_id);
				CREATE INDEX faction_rooms_faction      ON faction_rooms(faction_id);
			`,
		},
```

- [ ] **Step 2: Verify the migration applies cleanly**

Run: `go test ./pkg/knowledge/ -run TestMigrations -v` (if no such test exists, run the build + a quick smoke instead: `go build ./pkg/knowledge/`)
Expected: PASS / build succeeds. The migration runner in `runMigrations` applies version 35 on a fresh DB without error.

- [ ] **Step 3: Smoke-test schema creation against a temp DB**

Create a throwaway check (do NOT commit it) or rely on Task 3's roundtrip test. Quick manual check:

Run: `go run ./cmd/tools/play_as --help` is not relevant here; instead confirm via Task 3. Skip if Task 3 follows immediately.

- [ ] **Step 4: Commit**

```bash
git add pkg/knowledge/sqlite_migrations.go
git commit -m "feat(knowledge): add faction dashboard tables (migration v35)"
```

---

### Task 2: Faction data types

**Files:**
- Create: `pkg/knowledge/faction.go`

- [ ] **Step 1: Define the types**

Create `pkg/knowledge/faction.go`:

```go
package knowledge

import "time"

// FactionRecord is the current-state header row for a faction.
type FactionRecord struct {
	FactionID      string
	Name           string
	Tag            string
	LeaderID       string
	LeaderUsername string
	Treasury       int
	MemberCount    int
	OwnedBases     int
	Description    string
	Charter        string
	Emblem         string
	PrimaryColor   string
	SecondaryColor string
	FoundedUTC     string
	IntelSystems   int
	IntelTrade     int
	CapturedAt     time.Time
}

// FactionMember is one member of a faction.
type FactionMember struct {
	FactionID   string
	PlayerID    string
	Username    string
	Role        string
	JoinedUTC   string
	LastSeenUTC string
	IsOnline    bool
	CapturedAt  time.Time
}

// FactionRelation is an ally/enemy/war/peace_proposal edge.
type FactionRelation struct {
	FactionID       string
	TargetFactionID string
	TargetName      string
	TargetTag       string
	Kind            string // "ally" | "enemy" | "war" | "peace_proposal"
	Reason          string
	Terms           string
	OurKills        int
	TheirKills      int
	StartedUTC      string
	CapturedAt      time.Time
}

// FactionBaseRow is an owned base / location.
type FactionBaseRow struct {
	FactionID    string
	BaseID       string
	BaseName     string
	SystemID     string
	SystemName   string
	POIID        string
	ServicesJSON string
	CapturedAt   time.Time
}

// FactionFacilityRow is a faction facility at a base.
type FactionFacilityRow struct {
	FactionID    string
	BaseID       string
	FacilityID   string
	FacilityType string
	Category     string
	Level        int
	Status       string
	RecipeID     string
	DetailsJSON  string
	CapturedAt   time.Time
}

// FactionStorageItem is one item in faction storage at a base.
type FactionStorageItem struct {
	ItemID   string
	Name     string
	Quantity float64
	Size     int
}

// FactionStorageRow is faction storage at a single base (header + items).
type FactionStorageRow struct {
	FactionID  string
	BaseID     string
	Credits    int
	ItemCount  int
	Items      []FactionStorageItem
	CapturedAt time.Time
}

// FactionOrderRow is a faction market buy/sell order.
type FactionOrderRow struct {
	FactionID  string
	BaseID     string
	OrderID    string
	Side       string // "buy" | "sell"
	ItemID     string
	ItemName   string
	PriceEach  float64
	Quantity   float64
	CapturedAt time.Time
}

// FactionMissionRow is a posted faction mission.
type FactionMissionRow struct {
	FactionID        string
	BaseID           string
	MissionID        string
	Title            string
	Type             string
	Description      string
	GiverName        string
	RewardsJSON      string
	ObjectivesJSON   string
	AssignedPlayerID string
	ExpirationUTC    string
	CapturedAt       time.Time
}

// FactionRoomRow is a faction common-space room at a base.
type FactionRoomRow struct {
	FactionID   string
	BaseID      string
	RoomID      string
	Name        string
	Access      string
	Description string
	CapturedAt  time.Time
}

// FactionView is the full assembled current state for one faction, used by the
// dashboard renderer.
type FactionView struct {
	Faction    FactionRecord
	Members    []FactionMember
	Relations  []FactionRelation
	Bases      []FactionBaseRow
	Facilities []FactionFacilityRow
	Storage    []FactionStorageRow
	Orders     []FactionOrderRow
	Missions   []FactionMissionRow
	Rooms      []FactionRoomRow
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/knowledge/`
Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add pkg/knowledge/faction.go
git commit -m "feat(knowledge): add faction dashboard data types"
```

---

### Task 3: Faction store methods (replace-within-scope)

**Files:**
- Create: `pkg/knowledge/faction_store.go`
- Test: `pkg/knowledge/faction_store_test.go`

- [ ] **Step 1: Write the failing roundtrip test**

Create `pkg/knowledge/faction_store_test.go`:

```go
package knowledge

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestKB(t *testing.T) *SQLiteKB {
	t.Helper()
	kb, err := NewSQLiteKB(Config{DBPath: filepath.Join(t.TempDir(), "fac.db"), WAL: true})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	return kb
}

func TestStoreAndLoadFaction(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	now := time.Now()

	rec := FactionRecord{
		FactionID: "f1", Name: "Crafters Union", Tag: "CRFT",
		LeaderID: "p1", LeaderUsername: "boss", Treasury: 1000,
		MemberCount: 2, OwnedBases: 1, Description: "lore", Charter: "rules",
		FoundedUTC: "2026-01-01T00:00:00Z", CapturedAt: now,
	}
	if err := kb.StoreFaction(ctx, rec); err != nil {
		t.Fatalf("StoreFaction: %v", err)
	}

	if err := kb.ReplaceFactionMembers(ctx, "f1", []FactionMember{
		{FactionID: "f1", PlayerID: "p1", Username: "boss", Role: "Leader", IsOnline: true, CapturedAt: now},
		{FactionID: "f1", PlayerID: "p2", Username: "grunt", Role: "Member", CapturedAt: now},
	}); err != nil {
		t.Fatalf("ReplaceFactionMembers: %v", err)
	}

	if err := kb.ReplaceFactionStorage(ctx, FactionStorageRow{
		FactionID: "f1", BaseID: "b1", Credits: 500, ItemCount: 1,
		Items:      []FactionStorageItem{{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 42, Size: 1}},
		CapturedAt: now,
	}); err != nil {
		t.Fatalf("ReplaceFactionStorage: %v", err)
	}

	view, err := kb.LoadFactionView(ctx, "f1")
	if err != nil {
		t.Fatalf("LoadFactionView: %v", err)
	}
	if view.Faction.Tag != "CRFT" || view.Faction.Treasury != 1000 {
		t.Errorf("faction header wrong: %+v", view.Faction)
	}
	if len(view.Members) != 2 {
		t.Fatalf("want 2 members, got %d", len(view.Members))
	}
	if len(view.Storage) != 1 || len(view.Storage[0].Items) != 1 || view.Storage[0].Items[0].Quantity != 42 {
		t.Fatalf("storage roundtrip wrong: %+v", view.Storage)
	}

	// Replace-within-scope: re-store members with only one; the removed one must vanish.
	if err := kb.ReplaceFactionMembers(ctx, "f1", []FactionMember{
		{FactionID: "f1", PlayerID: "p1", Username: "boss", Role: "Leader", IsOnline: true, CapturedAt: now},
	}); err != nil {
		t.Fatalf("ReplaceFactionMembers (2): %v", err)
	}
	view, err = kb.LoadFactionView(ctx, "f1")
	if err != nil {
		t.Fatalf("LoadFactionView (2): %v", err)
	}
	if len(view.Members) != 1 {
		t.Errorf("want 1 member after replace, got %d", len(view.Members))
	}

	ids, err := kb.ListFactionIDs(ctx)
	if err != nil || len(ids) != 1 || ids[0] != "f1" {
		t.Errorf("ListFactionIDs wrong: %v err=%v", ids, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestStoreAndLoadFaction -v`
Expected: FAIL — `kb.StoreFaction` / `kb.ReplaceFactionMembers` / `kb.ReplaceFactionStorage` / `kb.LoadFactionView` / `kb.ListFactionIDs` undefined.

- [ ] **Step 3: Implement the store methods**

Create `pkg/knowledge/faction_store.go`:

```go
package knowledge

import (
	"context"
	"fmt"
	"time"
)

func utc(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format(time.RFC3339)
}

// StoreFaction upserts the faction header row.
func (kb *SQLiteKB) StoreFaction(ctx context.Context, r FactionRecord) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO factions (faction_id, name, tag, leader_id, leader_username,
			treasury, member_count, owned_bases, description, charter, emblem,
			primary_color, secondary_color, founded_utc, intel_systems, intel_trade, captured_utc)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(faction_id) DO UPDATE SET
			name=excluded.name, tag=excluded.tag, leader_id=excluded.leader_id,
			leader_username=excluded.leader_username, treasury=excluded.treasury,
			member_count=excluded.member_count, owned_bases=excluded.owned_bases,
			description=excluded.description, charter=excluded.charter, emblem=excluded.emblem,
			primary_color=excluded.primary_color, secondary_color=excluded.secondary_color,
			founded_utc=excluded.founded_utc, intel_systems=excluded.intel_systems,
			intel_trade=excluded.intel_trade, captured_utc=excluded.captured_utc`,
		r.FactionID, r.Name, r.Tag, r.LeaderID, r.LeaderUsername, r.Treasury,
		r.MemberCount, r.OwnedBases, r.Description, r.Charter, r.Emblem,
		r.PrimaryColor, r.SecondaryColor, r.FoundedUTC, r.IntelSystems, r.IntelTrade, utc(r.CapturedAt))
	if err != nil {
		return fmt.Errorf("store faction: %w", err)
	}
	return nil
}

// ReplaceFactionMembers replaces all members for a faction.
func (kb *SQLiteKB) ReplaceFactionMembers(ctx context.Context, factionID string, members []FactionMember) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_members WHERE faction_id=?`, factionID); err != nil {
			return err
		}
		for _, m := range members {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_members (faction_id, player_id, username, role, joined_utc, last_seen_utc, is_online, captured_utc)
				VALUES (?,?,?,?,?,?,?,?)`,
				factionID, m.PlayerID, m.Username, m.Role, m.JoinedUTC, m.LastSeenUTC, boolToInt(m.IsOnline), utc(m.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionRelations replaces all relations for a faction.
func (kb *SQLiteKB) ReplaceFactionRelations(ctx context.Context, factionID string, rels []FactionRelation) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_relations WHERE faction_id=?`, factionID); err != nil {
			return err
		}
		for _, r := range rels {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_relations (faction_id, target_faction_id, target_name, target_tag, kind, reason, terms, our_kills, their_kills, started_utc, captured_utc)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
				factionID, r.TargetFactionID, r.TargetName, r.TargetTag, r.Kind, r.Reason, r.Terms, r.OurKills, r.TheirKills, r.StartedUTC, utc(r.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// StoreFactionBase upserts a single owned base.
func (kb *SQLiteKB) StoreFactionBase(ctx context.Context, b FactionBaseRow) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO faction_bases (faction_id, base_id, base_name, system_id, system_name, poi_id, services_json, captured_utc)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(faction_id, base_id) DO UPDATE SET
			base_name=excluded.base_name, system_id=excluded.system_id, system_name=excluded.system_name,
			poi_id=excluded.poi_id, services_json=excluded.services_json, captured_utc=excluded.captured_utc`,
		b.FactionID, b.BaseID, b.BaseName, b.SystemID, b.SystemName, b.POIID, b.ServicesJSON, utc(b.CapturedAt))
	if err != nil {
		return fmt.Errorf("store faction base: %w", err)
	}
	return nil
}

// ReplaceFactionFacilities replaces facilities at one base.
func (kb *SQLiteKB) ReplaceFactionFacilities(ctx context.Context, factionID, baseID string, fs []FactionFacilityRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_facilities WHERE faction_id=? AND base_id=?`, factionID, baseID); err != nil {
			return err
		}
		for _, f := range fs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_facilities (faction_id, base_id, facility_id, facility_type, category, level, status, recipe_id, details_json, captured_utc)
				VALUES (?,?,?,?,?,?,?,?,?,?)`,
				factionID, baseID, f.FacilityID, f.FacilityType, f.Category, f.Level, f.Status, f.RecipeID, f.DetailsJSON, utc(f.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionStorage replaces storage header + items at one base.
func (kb *SQLiteKB) ReplaceFactionStorage(ctx context.Context, s FactionStorageRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_storage_items WHERE faction_id=? AND base_id=?`, s.FactionID, s.BaseID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO faction_storage (faction_id, base_id, credits, item_count, captured_utc)
			VALUES (?,?,?,?,?)
			ON CONFLICT(faction_id, base_id) DO UPDATE SET
				credits=excluded.credits, item_count=excluded.item_count, captured_utc=excluded.captured_utc`,
			s.FactionID, s.BaseID, s.Credits, s.ItemCount, utc(s.CapturedAt)); err != nil {
			return err
		}
		for _, it := range s.Items {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_storage_items (faction_id, base_id, item_id, name, quantity, size, captured_utc)
				VALUES (?,?,?,?,?,?,?)`,
				s.FactionID, s.BaseID, it.ItemID, it.Name, it.Quantity, it.Size, utc(s.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionOrders replaces orders at one base.
func (kb *SQLiteKB) ReplaceFactionOrders(ctx context.Context, factionID, baseID string, orders []FactionOrderRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_orders WHERE faction_id=? AND base_id=?`, factionID, baseID); err != nil {
			return err
		}
		for _, o := range orders {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_orders (faction_id, base_id, order_id, side, item_id, item_name, price_each, quantity, captured_utc)
				VALUES (?,?,?,?,?,?,?,?,?)`,
				factionID, baseID, o.OrderID, o.Side, o.ItemID, o.ItemName, o.PriceEach, o.Quantity, utc(o.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionMissions replaces missions at one base.
func (kb *SQLiteKB) ReplaceFactionMissions(ctx context.Context, factionID, baseID string, ms []FactionMissionRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_missions WHERE faction_id=? AND base_id=?`, factionID, baseID); err != nil {
			return err
		}
		for _, m := range ms {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_missions (faction_id, base_id, mission_id, title, type, description, giver_name, rewards_json, objectives_json, assigned_player_id, expiration_utc, captured_utc)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
				factionID, baseID, m.MissionID, m.Title, m.Type, m.Description, m.GiverName, m.RewardsJSON, m.ObjectivesJSON, m.AssignedPlayerID, m.ExpirationUTC, utc(m.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionRooms replaces rooms at one base.
func (kb *SQLiteKB) ReplaceFactionRooms(ctx context.Context, factionID, baseID string, rooms []FactionRoomRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_rooms WHERE faction_id=? AND base_id=?`, factionID, baseID); err != nil {
			return err
		}
		for _, r := range rooms {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_rooms (faction_id, base_id, room_id, name, access, description, captured_utc)
				VALUES (?,?,?,?,?,?,?)`,
				factionID, baseID, r.RoomID, r.Name, r.Access, r.Description, utc(r.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}
```

- [ ] **Step 4: Add the small tx + bool helpers**

If `boolToInt`, `txer`, or `inTx` do not already exist in `pkg/knowledge`, add this file `pkg/knowledge/faction_tx.go` (first check with `grep -rn "func boolToInt\|type txer\|func (kb \*SQLiteKB) inTx" pkg/knowledge/` and only add the missing ones):

```go
package knowledge

import (
	"context"
	"database/sql"
)

// txer is the subset of *sql.Tx used by faction store helpers.
type txer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// inTx runs fn inside a transaction, rolling back on error.
func (kb *SQLiteKB) inTx(ctx context.Context, fn func(tx txer) error) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

- [ ] **Step 5: Run the test (still fails — load methods missing)**

Run: `go test ./pkg/knowledge/ -run TestStoreAndLoadFaction -v`
Expected: FAIL — `kb.LoadFactionView` / `kb.ListFactionIDs` undefined. (Store methods now compile.) Proceed to Task 4 before re-running.

---

### Task 4: Faction load methods

**Files:**
- Create: `pkg/knowledge/faction_load.go`

- [ ] **Step 1: Implement the load methods**

Create `pkg/knowledge/faction_load.go`:

```go
package knowledge

import (
	"context"
	"database/sql"
	"fmt"
)

// ListFactionIDs returns all faction IDs that have a header row.
func (kb *SQLiteKB) ListFactionIDs(ctx context.Context) ([]string, error) {
	rows, err := kb.db.QueryContext(ctx, `SELECT faction_id FROM factions ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// LoadFactionView assembles the full current state for a faction. Returns
// (nil, nil) if no faction header row exists.
func (kb *SQLiteKB) LoadFactionView(ctx context.Context, factionID string) (*FactionView, error) {
	v := &FactionView{}
	row := kb.db.QueryRowContext(ctx, `
		SELECT faction_id, name, tag, leader_id, leader_username, treasury, member_count,
			owned_bases, description, charter, emblem, primary_color, secondary_color,
			founded_utc, intel_systems, intel_trade, captured_utc
		FROM factions WHERE faction_id=?`, factionID)
	var capturedUTC string
	r := &v.Faction
	if err := row.Scan(&r.FactionID, &r.Name, &r.Tag, &r.LeaderID, &r.LeaderUsername,
		&r.Treasury, &r.MemberCount, &r.OwnedBases, &r.Description, &r.Charter, &r.Emblem,
		&r.PrimaryColor, &r.SecondaryColor, &r.FoundedUTC, &r.IntelSystems, &r.IntelTrade, &capturedUTC); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load faction: %w", err)
	}
	r.CapturedAt = parseUTC(capturedUTC)

	var err error
	if v.Members, err = kb.loadMembers(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Relations, err = kb.loadRelations(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Bases, err = kb.loadBases(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Facilities, err = kb.loadFacilities(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Storage, err = kb.loadStorage(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Orders, err = kb.loadOrders(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Missions, err = kb.loadMissions(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Rooms, err = kb.loadRooms(ctx, factionID); err != nil {
		return nil, err
	}
	return v, nil
}

func (kb *SQLiteKB) loadMembers(ctx context.Context, fid string) ([]FactionMember, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT player_id, username, role, joined_utc, last_seen_utc, is_online, captured_utc
		FROM faction_members WHERE faction_id=? ORDER BY role, username`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionMember
	for rows.Next() {
		m := FactionMember{FactionID: fid}
		var online int
		var cap string
		if err := rows.Scan(&m.PlayerID, &m.Username, &m.Role, &m.JoinedUTC, &m.LastSeenUTC, &online, &cap); err != nil {
			return nil, err
		}
		m.IsOnline = online != 0
		m.CapturedAt = parseUTC(cap)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadRelations(ctx context.Context, fid string) ([]FactionRelation, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT target_faction_id, target_name, target_tag, kind, reason, terms, our_kills, their_kills, started_utc, captured_utc
		FROM faction_relations WHERE faction_id=? ORDER BY kind, target_tag`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionRelation
	for rows.Next() {
		rel := FactionRelation{FactionID: fid}
		var cap string
		if err := rows.Scan(&rel.TargetFactionID, &rel.TargetName, &rel.TargetTag, &rel.Kind, &rel.Reason, &rel.Terms, &rel.OurKills, &rel.TheirKills, &rel.StartedUTC, &cap); err != nil {
			return nil, err
		}
		rel.CapturedAt = parseUTC(cap)
		out = append(out, rel)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadBases(ctx context.Context, fid string) ([]FactionBaseRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, base_name, system_id, system_name, poi_id, services_json, captured_utc
		FROM faction_bases WHERE faction_id=? ORDER BY base_name`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionBaseRow
	for rows.Next() {
		b := FactionBaseRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&b.BaseID, &b.BaseName, &b.SystemID, &b.SystemName, &b.POIID, &b.ServicesJSON, &cap); err != nil {
			return nil, err
		}
		b.CapturedAt = parseUTC(cap)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadFacilities(ctx context.Context, fid string) ([]FactionFacilityRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, facility_id, facility_type, category, level, status, recipe_id, details_json, captured_utc
		FROM faction_facilities WHERE faction_id=? ORDER BY base_id, facility_type`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionFacilityRow
	for rows.Next() {
		f := FactionFacilityRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&f.BaseID, &f.FacilityID, &f.FacilityType, &f.Category, &f.Level, &f.Status, &f.RecipeID, &f.DetailsJSON, &cap); err != nil {
			return nil, err
		}
		f.CapturedAt = parseUTC(cap)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadStorage(ctx context.Context, fid string) ([]FactionStorageRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, credits, item_count, captured_utc
		FROM faction_storage WHERE faction_id=? ORDER BY base_id`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionStorageRow
	for rows.Next() {
		s := FactionStorageRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&s.BaseID, &s.Credits, &s.ItemCount, &cap); err != nil {
			return nil, err
		}
		s.CapturedAt = parseUTC(cap)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		items, err := kb.loadStorageItems(ctx, fid, out[i].BaseID)
		if err != nil {
			return nil, err
		}
		out[i].Items = items
	}
	return out, nil
}

func (kb *SQLiteKB) loadStorageItems(ctx context.Context, fid, baseID string) ([]FactionStorageItem, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT item_id, name, quantity, size FROM faction_storage_items
		WHERE faction_id=? AND base_id=? ORDER BY name`, fid, baseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionStorageItem
	for rows.Next() {
		var it FactionStorageItem
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Quantity, &it.Size); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadOrders(ctx context.Context, fid string) ([]FactionOrderRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, order_id, side, item_id, item_name, price_each, quantity, captured_utc
		FROM faction_orders WHERE faction_id=? ORDER BY base_id, side, item_name`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionOrderRow
	for rows.Next() {
		o := FactionOrderRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&o.BaseID, &o.OrderID, &o.Side, &o.ItemID, &o.ItemName, &o.PriceEach, &o.Quantity, &cap); err != nil {
			return nil, err
		}
		o.CapturedAt = parseUTC(cap)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadMissions(ctx context.Context, fid string) ([]FactionMissionRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, mission_id, title, type, description, giver_name, rewards_json, objectives_json, assigned_player_id, expiration_utc, captured_utc
		FROM faction_missions WHERE faction_id=? ORDER BY base_id, title`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionMissionRow
	for rows.Next() {
		m := FactionMissionRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&m.BaseID, &m.MissionID, &m.Title, &m.Type, &m.Description, &m.GiverName, &m.RewardsJSON, &m.ObjectivesJSON, &m.AssignedPlayerID, &m.ExpirationUTC, &cap); err != nil {
			return nil, err
		}
		m.CapturedAt = parseUTC(cap)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadRooms(ctx context.Context, fid string) ([]FactionRoomRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, room_id, name, access, description, captured_utc
		FROM faction_rooms WHERE faction_id=? ORDER BY base_id, name`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionRoomRow
	for rows.Next() {
		rm := FactionRoomRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&rm.BaseID, &rm.RoomID, &rm.Name, &rm.Access, &rm.Description, &cap); err != nil {
			return nil, err
		}
		rm.CapturedAt = parseUTC(cap)
		out = append(out, rm)
	}
	return out, rows.Err()
}
```

- [ ] **Step 2: Add the `parseUTC` helper**

Add to `pkg/knowledge/faction_tx.go` (or `faction_load.go`) — first check it doesn't already exist with `grep -rn "func parseUTC" pkg/knowledge/`:

```go
import "time"

// parseUTC parses an RFC3339 timestamp, returning the zero time on failure.
func parseUTC(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
```

- [ ] **Step 3: Run the test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestStoreAndLoadFaction -v`
Expected: PASS.

- [ ] **Step 4: Run lint + full knowledge tests**

Run: `golangci-lint run ./pkg/knowledge/... && go test ./pkg/knowledge/...`
Expected: no new findings; tests pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/knowledge/faction_store.go pkg/knowledge/faction_load.go pkg/knowledge/faction_tx.go pkg/knowledge/faction_store_test.go
git commit -m "feat(knowledge): faction store/load methods with replace-within-scope upserts"
```

**Phase 1 milestone:** the KB can persist and retrieve a full faction view. ✅

---

## Phase 2 — Collection

### Task 5: serverapi response structs for rooms & missions

**Files:**
- Modify: `pkg/game/serverapi/responses.go` (append near the other Faction* responses, after `ViewFactionStorageResponse` ~line 994)

> NOTE (open item from spec): the exact server field names for `faction_rooms` and `faction_list_missions` are not in the repo. Before relying on them, run them live once and inspect the JSON: in `play_as`, with a faction member docked, type `faction_rooms` then `faction_list_missions` and read the raw payload (use `format raw`). Adjust the json tags below to match. The structs below use the field names implied by the command params (`faction_write_room(name, access, description, room_id)` and `faction_post_mission(title, type, description, objectives, rewards, giver_name, expiration_hours)`).

- [ ] **Step 1: Add the structs**

```go
// FactionRoom is one room in a faction's common space (faction_rooms).
type FactionRoom struct {
	RoomID      string `json:"room_id"`
	Name        string `json:"name"`
	Access      string `json:"access,omitempty"`
	Description string `json:"description,omitempty"`
}

// FactionRoomsResponse wraps the response from faction_rooms.
type FactionRoomsResponse struct {
	Action string        `json:"action,omitempty"`
	BaseID string        `json:"base_id,omitempty"`
	Rooms  []FactionRoom `json:"rooms"`
}

// FactionMission is one posted faction mission (faction_list_missions).
type FactionMission struct {
	MissionID        string          `json:"mission_id,omitempty"`
	TemplateID       string          `json:"template_id,omitempty"`
	Title            string          `json:"title"`
	Type             string          `json:"type,omitempty"`
	Description      string          `json:"description,omitempty"`
	GiverName        string          `json:"giver_name,omitempty"`
	Rewards          json.RawMessage `json:"rewards,omitempty"`
	Objectives       json.RawMessage `json:"objectives,omitempty"`
	AssignedPlayerID string          `json:"assigned_player_id,omitempty"`
	ExpiresAt        string          `json:"expires_at,omitempty"`
}

// FactionListMissionsResponse wraps the response from faction_list_missions.
type FactionListMissionsResponse struct {
	Action   string           `json:"action,omitempty"`
	BaseID   string           `json:"base_id,omitempty"`
	Missions []FactionMission `json:"missions"`
}
```

- [ ] **Step 2: Ensure `encoding/json` is imported**

Check the top of `responses.go`; if `encoding/json` is not imported, add it. Run: `go build ./pkg/game/serverapi/`
Expected: builds.

- [ ] **Step 3: Commit**

```bash
git add pkg/game/serverapi/responses.go
git commit -m "feat(serverapi): faction rooms and missions response structs"
```

---

### Task 6: pkg/faction Collector — faction-wide data + parsing test

**Files:**
- Create: `pkg/faction/collector.go`
- Create: `pkg/faction/parse.go`
- Test: `pkg/faction/parse_test.go`

- [ ] **Step 1: Write the failing parse test**

Create `pkg/faction/parse_test.go`:

```go
package faction

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestParseFactionInfo(t *testing.T) {
	info := serverapi.FactionInfoResponse{
		ID: "f1", Name: "Crafters Union", Tag: "CRFT",
		LeaderID: "p1", LeaderUsername: "boss", Treasury: 1000,
		MemberCount: 2, OwnedBases: 1, Description: "lore", Charter: "rules",
		CreatedAt: "2026-01-01T00:00:00Z",
		Members: []serverapi.FactionMemberDetail{
			{PlayerID: "p1", Username: "boss", Role: "Leader", IsOnline: true},
			{PlayerID: "p2", Username: "grunt", Role: "Member"},
		},
		Allies:  []serverapi.FactionSummary{{ID: "f2", Name: "Allies Inc", Tag: "ALLY"}},
		Enemies: []serverapi.FactionSummary{{ID: "f3", Name: "Bad Guys", Tag: "EVIL"}},
		Wars: []serverapi.FactionWarDetail{
			{TargetFactionID: "f3", TargetFactionName: "Bad Guys", TargetFactionTag: "EVIL", OurKills: 3, TheirKills: 1, Reason: "honor"},
		},
	}

	rec, members, rels := parseFactionInfo(info)
	if rec.Tag != "CRFT" || rec.Treasury != 1000 || rec.FoundedUTC != "2026-01-01T00:00:00Z" {
		t.Errorf("record wrong: %+v", rec)
	}
	if len(members) != 2 || members[0].Role != "Leader" || !members[0].IsOnline {
		t.Errorf("members wrong: %+v", members)
	}
	// 1 ally + 1 enemy + 1 war = 3 relations
	if len(rels) != 3 {
		t.Fatalf("want 3 relations, got %d: %+v", len(rels), rels)
	}
	var kinds = map[string]int{}
	for _, r := range rels {
		kinds[r.Kind]++
	}
	if kinds["ally"] != 1 || kinds["enemy"] != 1 || kinds["war"] != 1 {
		t.Errorf("relation kinds wrong: %v", kinds)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/faction/ -run TestParseFactionInfo -v`
Expected: FAIL — package/`parseFactionInfo` does not exist.

- [ ] **Step 3: Implement the parsers**

Create `pkg/faction/parse.go`:

```go
// Package faction collects comprehensive faction data from a connected game
// client and persists it to the knowledge base for the faction dashboard.
package faction

import (
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseFactionInfo converts a faction_info response into the KB header record,
// members, and relation edges (allies, enemies, wars, peace proposals).
func parseFactionInfo(info serverapi.FactionInfoResponse) (knowledge.FactionRecord, []knowledge.FactionMember, []knowledge.FactionRelation) {
	now := time.Now()
	rec := knowledge.FactionRecord{
		FactionID: info.ID, Name: info.Name, Tag: info.Tag,
		LeaderID: info.LeaderID, LeaderUsername: info.LeaderUsername,
		Treasury: info.Treasury, MemberCount: info.MemberCount, OwnedBases: info.OwnedBases,
		Description: info.Description, Charter: info.Charter, Emblem: info.Emblem,
		PrimaryColor: info.PrimaryColor, SecondaryColor: info.SecondaryColor,
		FoundedUTC: info.CreatedAt, CapturedAt: now,
	}

	members := make([]knowledge.FactionMember, 0, len(info.Members))
	for _, m := range info.Members {
		members = append(members, knowledge.FactionMember{
			FactionID: info.ID, PlayerID: m.PlayerID, Username: m.Username, Role: m.Role,
			JoinedUTC: m.JoinedAt, LastSeenUTC: m.LastSeen, IsOnline: m.IsOnline, CapturedAt: now,
		})
	}

	var rels []knowledge.FactionRelation
	for _, a := range info.Allies {
		rels = append(rels, knowledge.FactionRelation{
			FactionID: info.ID, TargetFactionID: a.ID, TargetName: a.Name, TargetTag: a.Tag,
			Kind: "ally", CapturedAt: now,
		})
	}
	for _, e := range info.Enemies {
		rels = append(rels, knowledge.FactionRelation{
			FactionID: info.ID, TargetFactionID: e.ID, TargetName: e.Name, TargetTag: e.Tag,
			Kind: "enemy", CapturedAt: now,
		})
	}
	for _, w := range info.Wars {
		rels = append(rels, knowledge.FactionRelation{
			FactionID: info.ID, TargetFactionID: w.TargetFactionID, TargetName: w.TargetFactionName,
			TargetTag: w.TargetFactionTag, Kind: "war", Reason: w.Reason,
			OurKills: w.OurKills, TheirKills: w.TheirKills, StartedUTC: w.StartedAt, CapturedAt: now,
		})
	}
	for _, p := range info.PeaceProposals {
		rels = append(rels, knowledge.FactionRelation{
			FactionID: info.ID, TargetFactionID: p.TargetFactionID, TargetName: p.TargetName,
			Kind: "peace_proposal", Terms: p.Terms, CapturedAt: now,
		})
	}
	return rec, members, rels
}
```

> Before running: confirm the `serverapi.PeaceProposal` field names (`TargetFactionID`, `TargetName`, `Terms`) by reading `pkg/game/serverapi/types.go:548`. Adjust if they differ.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/faction/ -run TestParseFactionInfo -v`
Expected: PASS.

- [ ] **Step 5: Add the Collector skeleton + faction-wide collection**

Create `pkg/faction/collector.go`:

```go
package faction

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Store is the subset of *knowledge.SQLiteKB the Collector persists through.
// Satisfied by *knowledge.SQLiteKB.
type Store interface {
	StoreFaction(ctx context.Context, r knowledge.FactionRecord) error
	ReplaceFactionMembers(ctx context.Context, factionID string, members []knowledge.FactionMember) error
	ReplaceFactionRelations(ctx context.Context, factionID string, rels []knowledge.FactionRelation) error
	StoreFactionBase(ctx context.Context, b knowledge.FactionBaseRow) error
	ReplaceFactionFacilities(ctx context.Context, factionID, baseID string, fs []knowledge.FactionFacilityRow) error
	ReplaceFactionStorage(ctx context.Context, s knowledge.FactionStorageRow) error
	ReplaceFactionOrders(ctx context.Context, factionID, baseID string, orders []knowledge.FactionOrderRow) error
	ReplaceFactionMissions(ctx context.Context, factionID, baseID string, ms []knowledge.FactionMissionRow) error
	ReplaceFactionRooms(ctx context.Context, factionID, baseID string, rooms []knowledge.FactionRoomRow) error
}

// Collector gathers faction data from a connected game client and persists it.
type Collector struct {
	kb     Store
	logger *log.Logger
}

// NewCollector returns a Collector that writes to kb.
func NewCollector(kb Store, logger *log.Logger) *Collector {
	if logger == nil {
		logger = log.Default()
	}
	return &Collector{kb: kb, logger: logger}
}

// Collect gathers faction data from the connected client's vantage point.
// When includeFactionWide is true, it also collects faction_info-derived data
// (header, members, relations, intel). Station-scoped data (facilities, storage,
// orders, missions, rooms, bases) is always collected for the current station
// and known bases. Best-effort: sub-query failures are logged, not fatal.
func (c *Collector) Collect(ctx context.Context, client game.GameClient, includeFactionWide bool) error {
	state := client.GetState()
	factionID := state.Player.FactionID
	if factionID == "" {
		return fmt.Errorf("agent is not in a faction")
	}
	wsClient, ok := client.(*game.Client)
	if !ok {
		return fmt.Errorf("faction collection requires the WebSocket client (*game.Client)")
	}

	if includeFactionWide {
		c.collectFactionInfo(ctx, wsClient, factionID)
	}
	c.collectStation(ctx, wsClient, factionID, state)
	return nil
}

// submitAndRead sends a command and returns its response payload, mirroring the
// daily-summary storage-capture pattern. Returns nil payload on server error.
func submitAndRead(ctx context.Context, c *game.Client, msgType string, payload map[string]any) (map[string]any, error) {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      msgType,
		Timestamp: time.Now().UnixMilli(),
		Payload:   payload,
	}, game.WithAckOnly(), game.WithTimeout(10*time.Second))
	if err != nil {
		return nil, err
	}
	resp, err := h.Result(ctx)
	if err != nil {
		return nil, err
	}
	if resp.Type == protocol.TypeError || resp.Type == protocol.TypeActionError {
		if msg, ok := resp.Payload["message"].(string); ok {
			return nil, fmt.Errorf("server error: %s", msg)
		}
		return nil, fmt.Errorf("server returned error response")
	}
	return resp.Payload, nil
}

// readInto submits a command and unmarshals the payload into out.
func readInto(ctx context.Context, c *game.Client, msgType string, payload map[string]any, out any) error {
	p, err := submitAndRead(ctx, c, msgType, payload)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (c *Collector) collectFactionInfo(ctx context.Context, client *game.Client, factionID string) {
	var info serverapi.FactionInfoResponse
	if err := readInto(ctx, client, "faction_info", map[string]any{"limit": 200}, &info); err != nil {
		c.logger.Printf("  faction_info failed: %v", err)
		return
	}
	rec, members, rels := parseFactionInfo(info)
	if rec.FactionID == "" {
		rec.FactionID = factionID
	}
	// Intel coverage (best-effort).
	rec.IntelSystems, rec.IntelTrade = c.collectIntel(ctx, client)

	if err := c.kb.StoreFaction(ctx, rec); err != nil {
		c.logger.Printf("  StoreFaction failed: %v", err)
	}
	if err := c.kb.ReplaceFactionMembers(ctx, rec.FactionID, members); err != nil {
		c.logger.Printf("  ReplaceFactionMembers failed: %v", err)
	}
	if err := c.kb.ReplaceFactionRelations(ctx, rec.FactionID, rels); err != nil {
		c.logger.Printf("  ReplaceFactionRelations failed: %v", err)
	}
	c.logger.Printf("  Faction %s: treasury=%d members=%d relations=%d", rec.Tag, rec.Treasury, len(members), len(rels))
}

// collectIntel reads intel coverage counts; returns (systems, trade), 0 on error.
func (c *Collector) collectIntel(ctx context.Context, client *game.Client) (int, int) {
	var systems, trade int
	if p, err := submitAndRead(ctx, client, "faction_intel_status", nil); err == nil {
		systems = intFromAny(p["systems_covered"], p["count"], p["total"])
	}
	if p, err := submitAndRead(ctx, client, "faction_trade_intel_status", nil); err == nil {
		trade = intFromAny(p["stations_covered"], p["count"], p["total"])
	}
	return systems, trade
}

// intFromAny returns the first numeric value found among candidates as an int.
func intFromAny(candidates ...any) int {
	for _, v := range candidates {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}
```

> NOTE: `collectStation` is implemented in Task 7. To compile this task standalone, add a temporary stub at the end of `collector.go` and DELETE it in Task 7:
> ```go
> func (c *Collector) collectStation(ctx context.Context, client *game.Client, factionID string, state *game.State) {}
> ```
> Verify the `state` param type by checking `client.GetState()`'s return in `pkg/game/interface.go` (it is `*game.State`; adjust the signature if it returns a value).

- [ ] **Step 6: Verify build + intel field names**

Run: `go build ./pkg/faction/`
Expected: builds. (Intel field names `systems_covered`/`stations_covered` are best-effort guesses tolerated by `intFromAny`; confirm against live `faction_intel_status` output during manual testing and adjust the candidate keys.)

- [ ] **Step 7: Commit**

```bash
git add pkg/faction/collector.go pkg/faction/parse.go pkg/faction/parse_test.go
git commit -m "feat(faction): collector skeleton + faction_info parsing"
```

---

### Task 7: Collector — station-scoped facilities, bases, storage

**Files:**
- Modify: `pkg/faction/collector.go` (replace the `collectStation` stub)
- Create: `pkg/faction/parse_facility.go`
- Test: `pkg/faction/parse_facility_test.go`

- [ ] **Step 1: Write the failing facility-parse test**

Create `pkg/faction/parse_facility_test.go`:

```go
package faction

import "testing"

func TestParseFacilities(t *testing.T) {
	raw := []map[string]any{
		{"facility_id": "fac1", "facility_type": "refinery", "category": "production", "level": float64(2), "status": "active", "recipe_id": "refine_iron", "base_id": "b1"},
		{"facility_id": "fac2", "facility_type": "vault", "category": "storage", "level": float64(1), "base_id": "b1"},
	}
	rows := parseFacilities("f1", "b1", raw)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].FacilityType != "refinery" || rows[0].Level != 2 || rows[0].RecipeID != "refine_iron" {
		t.Errorf("row0 wrong: %+v", rows[0])
	}
	if !isStorageFacility("vault") || isStorageFacility("refinery") {
		t.Errorf("isStorageFacility classification wrong")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./pkg/faction/ -run TestParseFacilities -v`
Expected: FAIL — `parseFacilities` / `isStorageFacility` undefined.

- [ ] **Step 3: Implement facility parsing**

Create `pkg/faction/parse_facility.go`:

```go
package faction

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseFacilities converts raw faction facility maps into KB rows. baseID is the
// fallback when a facility map omits its own base_id.
func parseFacilities(factionID, baseID string, raw []map[string]any) []knowledge.FactionFacilityRow {
	now := time.Now()
	out := make([]knowledge.FactionFacilityRow, 0, len(raw))
	for _, f := range raw {
		row := knowledge.FactionFacilityRow{FactionID: factionID, BaseID: baseID, CapturedAt: now}
		if v, ok := f["facility_id"].(string); ok {
			row.FacilityID = v
		}
		if v, ok := f["facility_type"].(string); ok {
			row.FacilityType = v
		}
		if v, ok := f["category"].(string); ok {
			row.Category = v
		}
		if v, ok := f["status"].(string); ok {
			row.Status = v
		}
		if v, ok := f["recipe_id"].(string); ok {
			row.RecipeID = v
		}
		if v, ok := f["base_id"].(string); ok && v != "" {
			row.BaseID = v
		}
		if v, ok := f["level"].(float64); ok {
			row.Level = int(v)
		}
		if b, err := json.Marshal(f); err == nil {
			row.DetailsJSON = string(b)
		}
		out = append(out, row)
	}
	return out
}

// isStorageFacility reports whether a facility type holds shared storage.
func isStorageFacility(facilityType string) bool {
	t := strings.ToLower(facilityType)
	for _, st := range []string{"lockbox", "vault", "warehouse", "depot", "storage"} {
		if strings.Contains(t, st) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/faction/ -run TestParseFacilities -v`
Expected: PASS.

- [ ] **Step 5: Replace the `collectStation` stub with the real implementation**

In `pkg/faction/collector.go`, delete the temporary `collectStation` stub and add this (plus the storage parser). Append to `collector.go`:

```go
// collectStation collects all station-scoped data from the agent's current
// vantage: facilities at the current station, faction storage at the current
// station and every base discovered via facilities, plus orders/missions/rooms
// (Task 8). Best-effort throughout.
func (c *Collector) collectStation(ctx context.Context, client *game.Client, factionID string, state *game.State) {
	currentBase := state.CurrentPOI
	knownBases := map[string]bool{}
	if currentBase != "" {
		knownBases[currentBase] = true
	}

	// Facilities at the current station.
	var facResp serverapi.FacilityListResponse
	if err := readInto(ctx, client, "facility", map[string]any{"action": "faction_list"}, &facResp); err != nil {
		c.logger.Printf("  facility faction_list failed: %v", err)
	} else {
		base := facResp.BaseID
		if base == "" {
			base = currentBase
		}
		rows := parseFacilities(factionID, base, facResp.FactionFacilities)
		if base != "" {
			if err := c.kb.ReplaceFactionFacilities(ctx, factionID, base, rows); err != nil {
				c.logger.Printf("  ReplaceFactionFacilities failed: %v", err)
			}
		}
		for _, r := range rows {
			if r.BaseID != "" {
				knownBases[r.BaseID] = true
			}
		}
	}

	// Persist known bases + collect faction storage at each (remote query supported).
	for baseID := range knownBases {
		c.persistBase(ctx, factionID, baseID, currentBase, state)
		c.collectStorage(ctx, client, factionID, baseID)
	}

	// Orders / missions / rooms at the current station (Task 8).
	c.collectOrders(ctx, client, factionID, currentBase)
	c.collectMissions(ctx, client, factionID, currentBase)
	c.collectRooms(ctx, client, factionID, currentBase)
}

// persistBase records a faction base. The current station is enriched with
// system info from state; remote bases are recorded by ID only (the renderer
// falls back to the base ID when the name is empty).
func (c *Collector) persistBase(ctx context.Context, factionID, baseID, currentBase string, state *game.State) {
	row := knowledge.FactionBaseRow{FactionID: factionID, BaseID: baseID, CapturedAt: time.Now()}
	if baseID == currentBase {
		row.SystemID = state.System.ID
		row.SystemName = state.System.Name
		row.POIID = currentBase
	}
	if err := c.kb.StoreFactionBase(ctx, row); err != nil {
		c.logger.Printf("  StoreFactionBase failed: %v", err)
	}
}

func (c *Collector) collectStorage(ctx context.Context, client *game.Client, factionID, baseID string) {
	var resp serverapi.ViewFactionStorageResponse
	if err := readInto(ctx, client, "view_faction_storage", map[string]any{"station_id": baseID}, &resp); err != nil {
		c.logger.Printf("  faction storage at %s failed: %v", baseID, err)
		return
	}
	row := knowledge.FactionStorageRow{
		FactionID: factionID, BaseID: baseID, Credits: resp.Credits, CapturedAt: time.Now(),
	}
	for _, it := range resp.Items {
		row.Items = append(row.Items, knowledge.FactionStorageItem{
			ItemID: it.ItemID, Name: it.Name, Quantity: it.Quantity, Size: it.Size,
		})
	}
	row.ItemCount = len(row.Items)
	if err := c.kb.ReplaceFactionStorage(ctx, row); err != nil {
		c.logger.Printf("  ReplaceFactionStorage failed: %v", err)
	}
}
```

> NOTE: `collectOrders`, `collectMissions`, `collectRooms` are implemented in Task 8. Add temporary stubs now and replace in Task 8:
> ```go
> func (c *Collector) collectOrders(ctx context.Context, client *game.Client, factionID, baseID string)  {}
> func (c *Collector) collectMissions(ctx context.Context, client *game.Client, factionID, baseID string) {}
> func (c *Collector) collectRooms(ctx context.Context, client *game.Client, factionID, baseID string)   {}
> ```

- [ ] **Step 6: Verify build + tests**

Run: `go build ./pkg/faction/ && go test ./pkg/faction/ -v`
Expected: builds; parse tests PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/faction/collector.go pkg/faction/parse_facility.go pkg/faction/parse_facility_test.go
git commit -m "feat(faction): collect facilities and storage (station-scoped)"
```

---

### Task 8: Collector — orders, missions, rooms

**Files:**
- Modify: `pkg/faction/collector.go` (replace the three stubs)
- Create: `pkg/faction/parse_market.go`
- Test: `pkg/faction/parse_market_test.go`

- [ ] **Step 1: Write the failing orders-parse test**

Create `pkg/faction/parse_market_test.go`:

```go
package faction

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestParseFactionOrders(t *testing.T) {
	resp := serverapi.ViewOrdersResponse{
		Base: "b1",
		FactionOrders: []serverapi.ExchangeOrder{
			{OrderID: "o1", Side: "buy", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 100},
			{OrderID: "o2", OrderType: "sell", ItemID: "alloy", ItemName: "Alloy", PriceEach: 50, Quantity: 5},
		},
	}
	rows := parseFactionOrders("f1", "b1", resp)
	if len(rows) != 2 {
		t.Fatalf("want 2, got %d", len(rows))
	}
	if rows[0].Side != "buy" || rows[0].PriceEach != 10 {
		t.Errorf("row0 wrong: %+v", rows[0])
	}
	// Side falls back to OrderType when Side is empty.
	if rows[1].Side != "sell" {
		t.Errorf("row1 side fallback wrong: %+v", rows[1])
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./pkg/faction/ -run TestParseFactionOrders -v`
Expected: FAIL — `parseFactionOrders` undefined.

- [ ] **Step 3: Implement order/mission parsing**

Create `pkg/faction/parse_market.go`:

```go
package faction

import (
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseFactionOrders converts the faction_orders of a view_orders response into
// KB rows. Side falls back to order_type when side is empty.
func parseFactionOrders(factionID, baseID string, resp serverapi.ViewOrdersResponse) []knowledge.FactionOrderRow {
	now := time.Now()
	base := resp.Base
	if base == "" {
		base = baseID
	}
	out := make([]knowledge.FactionOrderRow, 0, len(resp.FactionOrders))
	for _, o := range resp.FactionOrders {
		side := o.Side
		if side == "" {
			side = o.OrderType
		}
		out = append(out, knowledge.FactionOrderRow{
			FactionID: factionID, BaseID: base, OrderID: o.OrderID, Side: side,
			ItemID: o.ItemID, ItemName: o.ItemName,
			PriceEach: float64(o.PriceEach), Quantity: float64(o.Quantity), CapturedAt: now,
		})
	}
	return out
}

// parseFactionMissions converts a faction_list_missions response into KB rows.
func parseFactionMissions(factionID, baseID string, resp serverapi.FactionListMissionsResponse) []knowledge.FactionMissionRow {
	now := time.Now()
	base := resp.BaseID
	if base == "" {
		base = baseID
	}
	out := make([]knowledge.FactionMissionRow, 0, len(resp.Missions))
	for _, m := range resp.Missions {
		id := m.MissionID
		if id == "" {
			id = m.TemplateID
		}
		out = append(out, knowledge.FactionMissionRow{
			FactionID: factionID, BaseID: base, MissionID: id, Title: m.Title, Type: m.Type,
			Description: m.Description, GiverName: m.GiverName,
			RewardsJSON: string(m.Rewards), ObjectivesJSON: string(m.Objectives),
			AssignedPlayerID: m.AssignedPlayerID, ExpirationUTC: m.ExpiresAt, CapturedAt: now,
		})
	}
	return out
}

// parseFactionRooms converts a faction_rooms response into KB rows.
func parseFactionRooms(factionID, baseID string, resp serverapi.FactionRoomsResponse) []knowledge.FactionRoomRow {
	now := time.Now()
	base := resp.BaseID
	if base == "" {
		base = baseID
	}
	out := make([]knowledge.FactionRoomRow, 0, len(resp.Rooms))
	for _, r := range resp.Rooms {
		out = append(out, knowledge.FactionRoomRow{
			FactionID: factionID, BaseID: base, RoomID: r.RoomID, Name: r.Name,
			Access: r.Access, Description: r.Description, CapturedAt: now,
		})
	}
	return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/faction/ -run TestParseFactionOrders -v`
Expected: PASS.

- [ ] **Step 5: Replace the three stubs in collector.go**

Delete the temporary `collectOrders`/`collectMissions`/`collectRooms` stubs and append:

```go
func (c *Collector) collectOrders(ctx context.Context, client *game.Client, factionID, baseID string) {
	if baseID == "" {
		return
	}
	var resp serverapi.ViewOrdersResponse
	if err := readInto(ctx, client, "view_orders", nil, &resp); err != nil {
		c.logger.Printf("  view_orders failed: %v", err)
		return
	}
	rows := parseFactionOrders(factionID, baseID, resp)
	if err := c.kb.ReplaceFactionOrders(ctx, factionID, baseID, rows); err != nil {
		c.logger.Printf("  ReplaceFactionOrders failed: %v", err)
	}
}

func (c *Collector) collectMissions(ctx context.Context, client *game.Client, factionID, baseID string) {
	if baseID == "" {
		return
	}
	var resp serverapi.FactionListMissionsResponse
	if err := readInto(ctx, client, "faction_list_missions", nil, &resp); err != nil {
		c.logger.Printf("  faction_list_missions failed: %v", err)
		return
	}
	rows := parseFactionMissions(factionID, baseID, resp)
	if err := c.kb.ReplaceFactionMissions(ctx, factionID, baseID, rows); err != nil {
		c.logger.Printf("  ReplaceFactionMissions failed: %v", err)
	}
}

func (c *Collector) collectRooms(ctx context.Context, client *game.Client, factionID, baseID string) {
	if baseID == "" {
		return
	}
	var resp serverapi.FactionRoomsResponse
	if err := readInto(ctx, client, "faction_rooms", nil, &resp); err != nil {
		c.logger.Printf("  faction_rooms failed: %v", err)
		return
	}
	rows := parseFactionRooms(factionID, baseID, resp)
	if err := c.kb.ReplaceFactionRooms(ctx, factionID, baseID, rows); err != nil {
		c.logger.Printf("  ReplaceFactionRooms failed: %v", err)
	}
}
```

- [ ] **Step 6: Verify build, full faction tests, lint**

Run: `go build ./pkg/faction/ && go test ./pkg/faction/... && golangci-lint run ./pkg/faction/...`
Expected: builds; tests PASS; no new lint findings.

- [ ] **Step 7: Commit**

```bash
git add pkg/faction/collector.go pkg/faction/parse_market.go pkg/faction/parse_market_test.go
git commit -m "feat(faction): collect orders, missions, and rooms"
```

---

### Task 9: play_as `update_faction_data` command

**Files:**
- Modify: `cmd/tools/play_as/kb_update.go` (add `kbUpdateFaction`)
- Modify: `cmd/tools/play_as/main.go` (add dispatch case near line 4885, after `update_all`)

- [ ] **Step 1: Add the command function**

Append to `cmd/tools/play_as/kb_update.go`:

```go
// kbUpdateFaction collects comprehensive faction data for the current agent's
// faction and persists it to the knowledge base.
func kbUpdateFaction(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("faction collection requires a SQLite knowledge base")
	}
	c := faction.NewCollector(sqlite, log.Default())
	if err := c.Collect(ctx, client, true); err != nil {
		return fmt.Errorf("faction collection failed: %w", err)
	}
	fmt.Println("Faction data updated.")
	return nil
}
```

- [ ] **Step 2: Add imports**

Ensure `cmd/tools/play_as/kb_update.go` imports `"log"` and `"github.com/rsned/spacemolt/pkg/faction"`. (`knowledge` and `game` are already imported there.) Run `goimports -w cmd/tools/play_as/kb_update.go` or add manually.

- [ ] **Step 3: Add the dispatch case**

In `cmd/tools/play_as/main.go`, immediately after the `case "update_all":` block (line ~4885), add:

```go
		case "update_faction_data", "update_faction":
			return kbUpdateFaction(client, ctx)
```

- [ ] **Step 4: Register in the command completer (if present)**

Check `cmd/tools/play_as/completer.go` for the list of `update_*` commands; if `update_all` is listed there, add `"update_faction_data"` alongside it. Run: `grep -n "update_all" cmd/tools/play_as/completer.go`. If found, add the new command to the same slice.

- [ ] **Step 5: Build**

Run: `go build ./cmd/tools/play_as/`
Expected: builds with no errors.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/kb_update.go cmd/tools/play_as/main.go cmd/tools/play_as/completer.go
git commit -m "feat(play_as): add update_faction_data command"
```

**Phase 2 milestone:** faction data can be collected into the KB from `play_as` (`--db-path ... ` then `update_faction_data`). ✅ Manually verify once: connect a faction member, run the command, and inspect the DB:
`sqlite3 data/spacemolt-knowledge.db "SELECT tag, treasury, member_count FROM factions;"`

---

## Phase 3 — Rendering

### Task 10: Renderer — view model, template scaffold, Overview tab + golden test

**Files:**
- Create: `cmd/tools/faction-dashboard/render.go`
- Create: `cmd/tools/faction-dashboard/template.go`
- Test: `cmd/tools/faction-dashboard/render_test.go` (substring assertions, not a golden file)

- [ ] **Step 1: Write the failing render test**

Create `cmd/tools/faction-dashboard/render_test.go`:

```go
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func sampleView() *knowledge.FactionView {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	return &knowledge.FactionView{
		Faction: knowledge.FactionRecord{
			FactionID: "f1", Name: "Crafters Union", Tag: "CRFT",
			LeaderUsername: "boss", Treasury: 1240500, MemberCount: 2, OwnedBases: 1,
			Description: "We build things.", Charter: "Be excellent.",
			PrimaryColor: "#34d399", FoundedUTC: "2026-01-01T00:00:00Z", CapturedAt: now,
		},
		Members: []knowledge.FactionMember{
			{PlayerID: "p1", Username: "boss", Role: "Leader", IsOnline: true, CapturedAt: now},
			{PlayerID: "p2", Username: "grunt", Role: "Member", CapturedAt: now},
		},
		Storage: []knowledge.FactionStorageRow{
			{BaseID: "b1", Credits: 500, ItemCount: 1, CapturedAt: now,
				Items: []knowledge.FactionStorageItem{{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 42, Size: 1}}},
		},
	}
}

func TestRenderFactionHTML(t *testing.T) {
	html, err := renderFactionHTML(sampleView())
	if err != nil {
		t.Fatalf("renderFactionHTML: %v", err)
	}
	for _, want := range []string{"CRFT", "Crafters Union", "Be excellent.", "Iron Ore", "data-tab=\"overview\"", "data-tab=\"storage\""} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q", want)
		}
	}
	// User content must be escaped (no raw script injection path).
	if strings.Contains(html, "<script>alert") {
		t.Errorf("unexpected unescaped content")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/tools/faction-dashboard/ -run TestRenderFactionHTML -v`
Expected: FAIL — `renderFactionHTML` undefined / package missing.

- [ ] **Step 3: Implement the view model + render entry point**

Create `cmd/tools/faction-dashboard/render.go`:

```go
package main

import (
	"bytes"
	"fmt"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// renderFactionHTML renders one faction's dashboard page to a self-contained
// HTML string.
func renderFactionHTML(v *knowledge.FactionView) (string, error) {
	var buf bytes.Buffer
	if err := factionTemplate.Execute(&buf, v); err != nil {
		return "", fmt.Errorf("execute faction template: %w", err)
	}
	return buf.String(), nil
}
```

- [ ] **Step 4: Implement the template (Overview + Storage + tab scaffold)**

Create `cmd/tools/faction-dashboard/template.go`. This is the full tabbed page with the aurora theme; subsequent tasks add the remaining tab panels:

```go
package main

import (
	"fmt"
	"html/template"
)

var factionFuncs = template.FuncMap{
	"comma": func(n int) string {
		s := fmt.Sprintf("%d", n)
		out := ""
		for i, c := range s {
			if i > 0 && (len(s)-i)%3 == 0 {
				out += ","
			}
			out += string(c)
		}
		return out
	},
}

var factionTemplate = template.Must(template.New("faction").Funcs(factionFuncs).Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Faction.Tag}} — Faction Dashboard</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600&display=swap');
  :root{
    --s0:hsl(222,47%,11%);--s1:hsl(222,47%,14%);--s2:hsl(222,45%,18%);--s3:hsl(222,43%,22%);
    --tp:hsl(220,15%,95%);--ts:hsl(220,12%,70%);--tm:hsl(220,10%,50%);
    --green:hsl(150,70%,55%);--blue:hsl(200,70%,55%);--red:hsl(0,70%,60%);
  }
  *{margin:0;padding:0;box-sizing:border-box}
  body{font-family:'JetBrains Mono',monospace;background:var(--s0);color:var(--ts);line-height:1.6;padding:1.5rem}
  .container{max-width:1100px;margin:0 auto}
  .banner{background:linear-gradient(90deg,var(--s2),var(--s0));border-left:4px solid var(--green);
    padding:1rem 1.25rem;border-radius:6px;margin-bottom:1rem}
  .banner h1{color:var(--tp);font-size:1.5rem}
  .banner .meta{color:var(--ts);font-size:.9rem;margin-top:.25rem}
  .banner .meta b{color:var(--green)}
  .tabs{display:flex;gap:4px;flex-wrap:wrap;border-bottom:1px solid var(--s2);margin-bottom:1rem}
  .tab{background:var(--s1);color:var(--ts);border:none;border-radius:6px 6px 0 0;padding:.5rem .9rem;
    font-family:inherit;font-size:.85rem;cursor:pointer}
  .tab.active{background:var(--green);color:var(--s0);font-weight:600}
  .panel{display:none}
  .panel.active{display:block}
  .kpis{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:.75rem;margin-bottom:1rem}
  .kpi{background:var(--s1);border:1px solid var(--s2);border-radius:6px;padding:.9rem;text-align:center}
  .kpi .n{color:var(--green);font-size:1.4rem;font-weight:600}
  .kpi small{color:var(--tm);text-transform:uppercase;letter-spacing:.08em;font-size:.7rem}
  .card{background:var(--s1);border:1px solid var(--s2);border-radius:6px;padding:1rem;margin-bottom:1rem}
  .card h3{color:var(--tp);font-size:1rem;margin-bottom:.5rem}
  .lore{white-space:pre-wrap;color:var(--ts)}
  table{width:100%;border-collapse:collapse;font-size:.85rem}
  th,td{text-align:left;padding:.4rem .6rem;border-bottom:1px solid var(--s2)}
  th{color:var(--blue);font-weight:500}
  details{background:var(--s1);border:1px solid var(--s2);border-radius:6px;margin-bottom:.5rem}
  summary{padding:.6rem .9rem;cursor:pointer;color:var(--tp)}
  .empty{color:var(--tm);font-style:italic;padding:.5rem 0}
  .online{color:var(--green)} .offline{color:var(--tm)}
  .footer{margin-top:2rem;padding-top:1rem;border-top:1px solid var(--s2);color:var(--tm);font-size:.8rem}
  a{color:var(--blue)}
</style></head>
<body><div class="container">

<div class="banner">
  <h1>⬢ {{.Faction.Tag}} — {{.Faction.Name}}</h1>
  <div class="meta">💰 <b>{{comma .Faction.Treasury}}</b> &nbsp;·&nbsp; 👥 {{.Faction.MemberCount}} members &nbsp;·&nbsp; 🏠 {{.Faction.OwnedBases}} bases &nbsp;·&nbsp; Leader: {{.Faction.LeaderUsername}}</div>
</div>

<div class="tabs">
  <button class="tab active" data-tab="overview" onclick="showTab(event,'overview')">Overview</button>
  <button class="tab" data-tab="members" onclick="showTab(event,'members')">Members</button>
  <button class="tab" data-tab="diplomacy" onclick="showTab(event,'diplomacy')">Diplomacy</button>
  <button class="tab" data-tab="bases" onclick="showTab(event,'bases')">Bases</button>
  <button class="tab" data-tab="production" onclick="showTab(event,'production')">Production</button>
  <button class="tab" data-tab="storage" onclick="showTab(event,'storage')">Storage</button>
  <button class="tab" data-tab="market" onclick="showTab(event,'market')">Market</button>
  <button class="tab" data-tab="missions" onclick="showTab(event,'missions')">Missions</button>
  <button class="tab" data-tab="rooms" onclick="showTab(event,'rooms')">Rooms</button>
  <button class="tab" data-tab="intel" onclick="showTab(event,'intel')">Intel</button>
</div>

<div class="panel active" data-tab="overview">
  <div class="kpis">
    <div class="kpi"><div class="n">{{comma .Faction.Treasury}}</div><small>Treasury</small></div>
    <div class="kpi"><div class="n">{{.Faction.MemberCount}}</div><small>Members</small></div>
    <div class="kpi"><div class="n">{{.Faction.OwnedBases}}</div><small>Bases</small></div>
    <div class="kpi"><div class="n">{{.Faction.IntelSystems}}</div><small>Intel systems</small></div>
  </div>
  <div class="card"><h3>Charter</h3><div class="lore">{{if .Faction.Charter}}{{.Faction.Charter}}{{else}}<span class="empty">No charter set.</span>{{end}}</div></div>
  <div class="card"><h3>Description</h3><div class="lore">{{if .Faction.Description}}{{.Faction.Description}}{{else}}<span class="empty">No description set.</span>{{end}}</div></div>
  <div class="card"><h3>Identity</h3>
    <table>
      <tr><th>Founded</th><td>{{if .Faction.FoundedUTC}}{{.Faction.FoundedUTC}}{{else}}—{{end}}</td></tr>
      <tr><th>Leader</th><td>{{.Faction.LeaderUsername}}</td></tr>
      <tr><th>Colors</th><td>{{.Faction.PrimaryColor}} / {{.Faction.SecondaryColor}}</td></tr>
      <tr><th>Last collected</th><td>{{.Faction.CapturedAt}}</td></tr>
    </table>
  </div>
</div>

<div class="panel" data-tab="members">
  {{if .Members}}
  <table><tr><th>Member</th><th>Role</th><th>Status</th><th>Joined</th><th>Last seen</th></tr>
  {{range .Members}}<tr>
    <td>{{.Username}}</td><td>{{.Role}}</td>
    <td>{{if .IsOnline}}<span class="online">online</span>{{else}}<span class="offline">offline</span>{{end}}</td>
    <td>{{if .JoinedUTC}}{{.JoinedUTC}}{{else}}—{{end}}</td>
    <td>{{if .LastSeenUTC}}{{.LastSeenUTC}}{{else}}—{{end}}</td>
  </tr>{{end}}</table>
  {{else}}<p class="empty">No members collected.</p>{{end}}
</div>

<div class="panel" data-tab="diplomacy"><p class="empty">Diplomacy — added in Task 11.</p></div>
<div class="panel" data-tab="bases"><p class="empty">Bases — added in Task 11.</p></div>
<div class="panel" data-tab="production"><p class="empty">Production — added in Task 11.</p></div>

<div class="panel" data-tab="storage">
  {{if .Storage}}
  {{range .Storage}}
  <details><summary>{{.BaseID}} — 💰 {{comma .Credits}} · {{.ItemCount}} item types</summary>
    {{if .Items}}<table><tr><th>Item</th><th>Qty</th><th>Size</th></tr>
      {{range .Items}}<tr><td>{{if .Name}}{{.Name}}{{else}}{{.ItemID}}{{end}}</td><td>{{.Quantity}}</td><td>{{.Size}}</td></tr>{{end}}
    </table>{{else}}<p class="empty">Empty.</p>{{end}}
  </details>
  {{end}}
  {{else}}<p class="empty">No storage collected.</p>{{end}}
</div>

<div class="panel" data-tab="market"><p class="empty">Market — added in Task 11.</p></div>
<div class="panel" data-tab="missions"><p class="empty">Missions — added in Task 11.</p></div>
<div class="panel" data-tab="rooms"><p class="empty">Rooms — added in Task 11.</p></div>
<div class="panel" data-tab="intel">
  <div class="card"><h3>Intel coverage</h3>
    <table>
      <tr><th>Systems covered</th><td>{{.Faction.IntelSystems}}</td></tr>
      <tr><th>Trade stations covered</th><td>{{.Faction.IntelTrade}}</td></tr>
    </table>
  </div>
</div>

<div class="footer">Generated by SpaceMolt faction-dashboard · {{.Faction.CapturedAt}}</div>

</div>
<script>
function showTab(e,name){
  document.querySelectorAll('.tab').forEach(function(t){t.classList.toggle('active',t.dataset.tab===name)});
  document.querySelectorAll('.panel').forEach(function(p){p.classList.toggle('active',p.dataset.tab===name)});
}
</script>
</body></html>
`))
```

- [ ] **Step 5: Run the test, then capture the golden file**

Run: `go test ./cmd/tools/faction-dashboard/ -run TestRenderFactionHTML -v`
Expected: PASS (the test asserts substrings, not the full golden file). The golden fixture is optional for this task; create it in Task 11 once all tabs are present.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/faction-dashboard/render.go cmd/tools/faction-dashboard/template.go cmd/tools/faction-dashboard/render_test.go
git commit -m "feat(faction-dashboard): renderer scaffold with Overview, Members, Storage tabs"
```

---

### Task 11: Renderer — remaining tabs (Diplomacy, Bases, Production, Market, Missions, Rooms)

**Files:**
- Modify: `cmd/tools/faction-dashboard/template.go` (replace the six placeholder panels)
- Modify: `cmd/tools/faction-dashboard/render_test.go` (extend fixture + assertions)

- [ ] **Step 1: Extend the test fixture and assertions**

In `render_test.go`, extend `sampleView()` to include relations, bases, facilities, orders, missions, rooms, then assert their content. Replace the `Storage` field block's closing with these additional fields before the final `}`:

```go
		Relations: []knowledge.FactionRelation{
			{TargetTag: "ALLY", TargetName: "Allies Inc", Kind: "ally", CapturedAt: now},
			{TargetTag: "EVIL", TargetName: "Bad Guys", Kind: "war", OurKills: 3, TheirKills: 1, CapturedAt: now},
		},
		Bases: []knowledge.FactionBaseRow{
			{BaseID: "b1", BaseName: "Forge Station", SystemName: "Sol-3", CapturedAt: now},
		},
		Facilities: []knowledge.FactionFacilityRow{
			{BaseID: "b1", FacilityID: "fac1", FacilityType: "refinery", Category: "production", Level: 2, Status: "active", RecipeID: "refine_iron", CapturedAt: now},
		},
		Orders: []knowledge.FactionOrderRow{
			{BaseID: "b1", OrderID: "o1", Side: "buy", ItemName: "Iron Ore", PriceEach: 10, Quantity: 100, CapturedAt: now},
		},
		Missions: []knowledge.FactionMissionRow{
			{BaseID: "b1", MissionID: "m1", Title: "Haul Ore", Type: "delivery", Description: "Move ore.", CapturedAt: now},
		},
		Rooms: []knowledge.FactionRoomRow{
			{BaseID: "b1", RoomID: "r1", Name: "War Room", Access: "officers", Description: "Strategy here.", CapturedAt: now},
		},
```

And add to the `want` slice in `TestRenderFactionHTML`: `"Allies Inc", "Forge Station", "refinery", "Haul Ore", "War Room"`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/tools/faction-dashboard/ -run TestRenderFactionHTML -v`
Expected: FAIL — rendered HTML missing "Allies Inc" etc. (placeholders still present).

- [ ] **Step 3: Replace the six placeholder panels**

In `template.go`, replace each placeholder `<div class="panel" ...>...added in Task 11.</p></div>` with the real panel:

```html
<div class="panel" data-tab="diplomacy">
  {{$rels := .Relations}}
  {{if $rels}}
  <table><tr><th>Kind</th><th>Faction</th><th>Detail</th></tr>
  {{range $rels}}<tr>
    <td>{{.Kind}}</td><td>{{if .TargetTag}}[{{.TargetTag}}] {{end}}{{.TargetName}}</td>
    <td>{{if eq .Kind "war"}}kills {{.OurKills}}–{{.TheirKills}}{{if .Reason}} · {{.Reason}}{{end}}{{else if eq .Kind "peace_proposal"}}{{.Terms}}{{else}}—{{end}}</td>
  </tr>{{end}}</table>
  {{else}}<p class="empty">No diplomatic relations collected.</p>{{end}}
</div>

<div class="panel" data-tab="bases">
  {{if .Bases}}
  {{range .Bases}}
  <details><summary>{{if .BaseName}}{{.BaseName}}{{else}}{{.BaseID}}{{end}} {{if .SystemName}}({{.SystemName}}){{end}}</summary>
    <table>
      <tr><th>Base ID</th><td>{{.BaseID}}</td></tr>
      <tr><th>System</th><td>{{.SystemName}} {{.SystemID}}</td></tr>
      <tr><th>POI</th><td>{{.POIID}}</td></tr>
    </table>
  </details>
  {{end}}
  {{else}}<p class="empty">No bases collected.</p>{{end}}
</div>

<div class="panel" data-tab="production">
  {{if .Facilities}}
  <table><tr><th>Base</th><th>Facility</th><th>Category</th><th>Level</th><th>Status</th><th>Recipe</th></tr>
  {{range .Facilities}}<tr>
    <td>{{.BaseID}}</td><td>{{.FacilityType}}</td><td>{{.Category}}</td><td>{{.Level}}</td>
    <td>{{if .Status}}{{.Status}}{{else}}—{{end}}</td><td>{{if .RecipeID}}{{.RecipeID}}{{else}}—{{end}}</td>
  </tr>{{end}}</table>
  {{else}}<p class="empty">No facilities collected.</p>{{end}}
</div>

<div class="panel" data-tab="market">
  {{if .Orders}}
  <table><tr><th>Base</th><th>Side</th><th>Item</th><th>Price each</th><th>Qty</th></tr>
  {{range .Orders}}<tr>
    <td>{{.BaseID}}</td><td>{{.Side}}</td><td>{{if .ItemName}}{{.ItemName}}{{else}}{{.ItemID}}{{end}}</td>
    <td>{{.PriceEach}}</td><td>{{.Quantity}}</td>
  </tr>{{end}}</table>
  {{else}}<p class="empty">No faction orders collected.</p>{{end}}
</div>

<div class="panel" data-tab="missions">
  {{if .Missions}}
  {{range .Missions}}
  <details><summary>{{.Title}} {{if .Type}}· {{.Type}}{{end}}</summary>
    <div class="lore">{{.Description}}</div>
    <table>
      <tr><th>Base</th><td>{{.BaseID}}</td></tr>
      <tr><th>Giver</th><td>{{if .GiverName}}{{.GiverName}}{{else}}—{{end}}</td></tr>
      <tr><th>Assigned</th><td>{{if .AssignedPlayerID}}{{.AssignedPlayerID}}{{else}}unassigned{{end}}</td></tr>
      <tr><th>Expires</th><td>{{if .ExpirationUTC}}{{.ExpirationUTC}}{{else}}—{{end}}</td></tr>
    </table>
  </details>
  {{end}}
  {{else}}<p class="empty">No faction missions collected.</p>{{end}}
</div>

<div class="panel" data-tab="rooms">
  {{if .Rooms}}
  {{range .Rooms}}
  <details><summary>{{.Name}} {{if .Access}}· {{.Access}}{{end}}</summary>
    <div class="lore">{{if .Description}}{{.Description}}{{else}}<span class="empty">No description.</span>{{end}}</div>
  </details>
  {{end}}
  {{else}}<p class="empty">No rooms collected.</p>{{end}}
</div>
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/tools/faction-dashboard/ -run TestRenderFactionHTML -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/faction-dashboard/template.go cmd/tools/faction-dashboard/render_test.go
git commit -m "feat(faction-dashboard): render diplomacy, bases, production, market, missions, rooms tabs"
```

---

### Task 12: Index page renderer

**Files:**
- Create: `cmd/tools/faction-dashboard/index.go`
- Test: `cmd/tools/faction-dashboard/index_test.go`

- [ ] **Step 1: Write the failing index test**

Create `cmd/tools/faction-dashboard/index_test.go`:

```go
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestRenderIndexHTML(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	cards := []indexCard{
		{Tag: "CRFT", Name: "Crafters Union", Treasury: 1240500, Members: 12, CapturedAt: now},
		{Tag: "XPLR", Name: "Explorers", Treasury: 50000, Members: 4, CapturedAt: now},
	}
	html, err := renderIndexHTML(cards)
	if err != nil {
		t.Fatalf("renderIndexHTML: %v", err)
	}
	for _, want := range []string{"CRFT", "Crafters Union", "XPLR", "faction-CRFT.html", "faction-XPLR.html"} {
		if !strings.Contains(html, want) {
			t.Errorf("index HTML missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/tools/faction-dashboard/ -run TestRenderIndexHTML -v`
Expected: FAIL — `indexCard` / `renderIndexHTML` undefined.

- [ ] **Step 3: Implement the index renderer**

Create `cmd/tools/faction-dashboard/index.go`:

```go
package main

import (
	"bytes"
	"fmt"
	"html/template"
	"time"
)

// indexCard is a summary row on the faction index page.
type indexCard struct {
	Tag        string
	Name       string
	Treasury   int
	Members    int
	CapturedAt time.Time
}

var indexTemplate = template.Must(template.New("index").Funcs(factionFuncs).Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Faction Dashboards</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600&display=swap');
  :root{--s0:hsl(222,47%,11%);--s1:hsl(222,47%,14%);--s2:hsl(222,45%,18%);
    --tp:hsl(220,15%,95%);--ts:hsl(220,12%,70%);--tm:hsl(220,10%,50%);--green:hsl(150,70%,55%)}
  *{margin:0;padding:0;box-sizing:border-box}
  body{font-family:'JetBrains Mono',monospace;background:var(--s0);color:var(--ts);padding:2rem}
  .container{max-width:800px;margin:0 auto}
  h1{color:var(--tp);font-size:1.6rem;margin-bottom:1.5rem}
  .card{display:block;background:var(--s1);border:1px solid var(--s2);border-radius:6px;
    padding:1rem 1.25rem;margin-bottom:.75rem;text-decoration:none;color:var(--ts)}
  .card:hover{background:var(--s2)}
  .card .tag{color:var(--green);font-weight:600;font-size:1.1rem}
  .card .name{color:var(--tp)} .card .meta{color:var(--tm);font-size:.8rem;margin-top:.25rem}
  .footer{margin-top:2rem;color:var(--tm);font-size:.8rem}
</style></head><body><div class="container">
<h1>Faction Dashboards</h1>
{{range .}}
<a class="card" href="faction-{{.Tag}}.html">
  <span class="tag">{{.Tag}}</span> <span class="name">{{.Name}}</span>
  <div class="meta">💰 {{comma .Treasury}} · 👥 {{.Members}} members · collected {{.CapturedAt}}</div>
</a>
{{end}}
<div class="footer">Generated by SpaceMolt faction-dashboard</div>
</div></body></html>
`))

// renderIndexHTML renders the faction index page.
func renderIndexHTML(cards []indexCard) (string, error) {
	var buf bytes.Buffer
	if err := indexTemplate.Execute(&buf, cards); err != nil {
		return "", fmt.Errorf("execute index template: %w", err)
	}
	return buf.String(), nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/tools/faction-dashboard/ -run TestRenderIndexHTML -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/faction-dashboard/index.go cmd/tools/faction-dashboard/index_test.go
git commit -m "feat(faction-dashboard): index page renderer"
```

---

### Task 13: CLI main — connect agents, collect, render

**Files:**
- Create: `cmd/tools/faction-dashboard/main.go`

- [ ] **Step 1: Implement main**

Create `cmd/tools/faction-dashboard/main.go`. It reuses daily-summary's agent resolution + connection idioms (`resolveAgents` logic via `data/agents/`, `game.InitializeAgent`). It groups agents by faction (founder = lowest numeric suffix collects faction-wide), runs the Collector, then renders.

```go
// Command faction-dashboard collects comprehensive faction data from member
// agents into the shared knowledge base and renders static HTML dashboards.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/faction"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func main() {
	kbPath := flag.String("kb", "data/spacemolt-knowledge.db", "Shared knowledge base SQLite path")
	outputDir := flag.String("output", "data/reports/factions", "Output directory for HTML")
	agentsFlag := flag.String("agents", "", "Comma-separated agent filter (default: all in data/agents/)")
	delay := flag.Int("delay", 3, "Seconds between agent connections")
	debug := flag.Bool("debug", false, "Game client debug logging")
	renderOnly := flag.Bool("render-only", false, "Skip collection; render from existing KB data")
	flag.Parse()

	logger := log.New(os.Stdout, "[faction-dashboard] ", log.LstdFlags)

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *kbPath, WAL: true})
	if err != nil {
		logger.Fatalf("open KB: %v", err)
	}
	defer func() { _ = kb.Close() }()

	if !*renderOnly {
		agents, err := resolveAgents(*agentsFlag)
		if err != nil {
			logger.Fatalf("resolve agents: %v", err)
		}
		collectAll(kb, agents, *delay, *debug, logger)
	}

	if err := renderAll(kb, *outputDir, logger); err != nil {
		logger.Fatalf("render: %v", err)
	}
}

// resolveAgents returns agent IDs to process (those with credentials.json).
func resolveAgents(filter string) ([]string, error) {
	if filter != "" {
		return strings.Split(filter, ","), nil
	}
	entries, err := os.ReadDir("data/agents")
	if err != nil {
		return nil, err
	}
	var agents []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join("data", "agents", e.Name(), "credentials.json")); err == nil {
			agents = append(agents, e.Name())
		}
	}
	slices.Sort(agents)
	return agents, nil
}

// agentNumber extracts the numeric suffix from an agent ID (founder = lowest).
func agentNumber(agentID string) int {
	parts := strings.Split(agentID, "-")
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return n
		}
	}
	return 1 << 30
}

// collectAll connects each agent and runs the Collector. The first agent seen
// per faction (lowest number, processed in sorted order) collects faction-wide
// data; all agents collect station-scoped data.
func collectAll(kb *knowledge.SQLiteKB, agents []string, delaySec int, debug bool, logger *log.Logger) {
	collector := faction.NewCollector(kb, logger)
	factionWideDone := map[string]bool{}

	for i, agentID := range agents {
		if i > 0 {
			time.Sleep(time.Duration(delaySec) * time.Second)
		}
		logger.Printf("[%d/%d] %s", i+1, len(agents), agentID)
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			client, _, err := game.InitializeAgent(agentID, logger, ctx, debug)
			if err != nil {
				logger.Printf("  connect failed: %v", err)
				return
			}
			defer func() { _ = client.Close() }()

			if err := client.GetStatus(ctx); err != nil {
				logger.Printf("  get_status failed: %v", err)
			} else {
				time.Sleep(game.SleepQuick)
			}

			factionID := client.GetState().Player.FactionID
			if factionID == "" {
				logger.Printf("  not in a faction; skipping")
				return
			}
			includeWide := !factionWideDone[factionID]
			if err := collector.Collect(ctx, client, includeWide); err != nil {
				logger.Printf("  collect failed: %v", err)
				return
			}
			if includeWide {
				factionWideDone[factionID] = true
			}
		}()
	}
}

// renderAll writes one HTML page per faction plus an index.
func renderAll(kb *knowledge.SQLiteKB, outputDir string, logger *log.Logger) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	ctx := context.Background()
	ids, err := kb.ListFactionIDs(ctx)
	if err != nil {
		return err
	}
	var cards []indexCard
	for _, id := range ids {
		view, err := kb.LoadFactionView(ctx, id)
		if err != nil {
			logger.Printf("load %s: %v", id, err)
			continue
		}
		if view == nil {
			continue
		}
		html, err := renderFactionHTML(view)
		if err != nil {
			logger.Printf("render %s: %v", id, err)
			continue
		}
		path := filepath.Join(outputDir, "faction-"+view.Faction.Tag+".html")
		if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
			return err
		}
		logger.Printf("wrote %s", path)
		cards = append(cards, indexCard{
			Tag: view.Faction.Tag, Name: view.Faction.Name,
			Treasury: view.Faction.Treasury, Members: view.Faction.MemberCount,
			CapturedAt: view.Faction.CapturedAt,
		})
	}
	indexHTML, err := renderIndexHTML(cards)
	if err != nil {
		return err
	}
	indexPath := filepath.Join(outputDir, "index.html")
	if err := os.WriteFile(indexPath, []byte(indexHTML), 0o644); err != nil {
		return err
	}
	logger.Printf("wrote %s (%d factions)", indexPath, len(cards))
	return nil
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./cmd/tools/faction-dashboard/`
Expected: builds. (Confirm `game.InitializeAgent`'s parameter order matches `pkg/game/agent.go:111` — `(agentID string, logger *log.Logger, ctx context.Context, debug bool)`.)

- [ ] **Step 3: Run render-only against a populated KB (manual smoke)**

Run: `go run ./cmd/tools/faction-dashboard -render-only -kb data/spacemolt-knowledge.db -output /tmp/fac`
Expected: writes `/tmp/fac/faction-*.html` + `/tmp/fac/index.html` for any factions already in the KB (from the Task 9 manual collection). Open in a browser to eyeball tabs.

- [ ] **Step 4: Commit**

```bash
git add cmd/tools/faction-dashboard/main.go
git commit -m "feat(faction-dashboard): CLI to collect and render faction dashboards"
```

---

### Task 14: README + full verification

**Files:**
- Create: `cmd/tools/faction-dashboard/README.md`

- [ ] **Step 1: Write the README**

Create `cmd/tools/faction-dashboard/README.md`:

```markdown
# faction-dashboard

Collects comprehensive per-faction data from member agents into the shared
knowledge base and renders a tabbed static HTML dashboard per faction, plus an
index page.

## Usage

    # Collect from all agents in data/agents/ and render to data/reports/factions/
    go run ./cmd/tools/faction-dashboard

    # Render only (no collection) from existing KB data
    go run ./cmd/tools/faction-dashboard -render-only

    # Limit to specific agents
    go run ./cmd/tools/faction-dashboard -agents craftsman-1,explorer-1

## Flags

- `-kb`          shared knowledge base path (default `data/spacemolt-knowledge.db`)
- `-output`      output directory (default `data/reports/factions`)
- `-agents`      comma-separated agent filter (default: all with credentials.json)
- `-delay`       seconds between agent connections (default 3)
- `-render-only` skip collection; render from existing KB data
- `-debug`       game client debug logging

## Ad hoc collection from play_as

In the `play_as` REPL (started with `--db-path <kb>`), run `update_faction_data`
to collect the current agent's faction into the same KB.

## Data

Tabs: Overview, Members, Diplomacy, Bases, Production, Storage, Market,
Missions, Rooms, Intel. Collection is best-effort and station-scoped (no agent
travel); the KB merges per-station data across members. Current-state only —
day-over-day diffs remain the job of `daily-summary`.

## Known limitations

- Custom role permission matrices are not readable via the API; only member role
  names are shown.
- "Tasks" are interpreted as faction missions (linked to members when the API
  exposes an assignment).
```

- [ ] **Step 2: Check the README is not gitignored**

Run: `git check-ignore cmd/tools/faction-dashboard/README.md && echo IGNORED || echo ok`
Expected: `ok`. (If IGNORED, add a negation rule to `.gitignore`.)

- [ ] **Step 3: Full build, test, lint**

Run: `go build ./... && go test ./... && golangci-lint run`
Expected: builds; all tests pass; no new lint findings.

- [ ] **Step 4: Commit**

```bash
git add cmd/tools/faction-dashboard/README.md
git commit -m "docs(faction-dashboard): add README"
```

**Phase 3 milestone:** running `faction-dashboard` produces a browsable tabbed dashboard per faction + index. ✅

---

## Final manual end-to-end verification

1. Start with at least one faction member agent online and docked at a faction station.
2. `go run ./cmd/tools/faction-dashboard -agents <member-1>` (collect + render).
3. Open `data/reports/factions/index.html` → click into a faction → verify each tab shows data or a clean empty-state.
4. Confirm the open-item field names (rooms, missions, intel) populated correctly; if any tab is unexpectedly empty, capture the raw JSON via `play_as` (`format raw`) and adjust the corresponding `serverapi` struct tags / `intFromAny` keys, then re-run.
