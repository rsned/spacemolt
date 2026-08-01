# Agent Capability Ledger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a local, always-fresh record of what each fleet agent *is* and *can do* — identity, skills, standings, carrier tier, owned hulls — plus a derived eligibility layer, so questions like "who is next for the smuggling wave" and "who is still stuck in freight probation" become queries instead of manual log sweeps.

**Architecture:** A new `pkg/assets` package owns a dedicated `data/assets.db` and the capture functions that fill it. Tables mirror the server's wire shapes (maps become narrow tables, structs become wide ones) so new skills and factions are new rows, not migrations. Workers capture their own profile on a schedule — a central collector is impossible because it would contend for sessions the fleet already holds. An eligibility registry in Go materialises `agent_capability` on every capture.

**Tech Stack:** Go 1.24, `modernc.org/sqlite` (driver name `"sqlite"`), existing `pkg/game` client, `pkg/worker` dispatch, `pkg/ovdash` read-only DB panels.

## Scope

This plan implements **build-order slices 1–4** of `docs/superpowers/specs/2026-08-01-agent-asset-profile-design.md`, plus the coverage surface:

- identity mapping, profile, skills, standings
- carrier profile
- owned hulls
- the eligibility registry
- worker/`play_as` wiring and an ovdash staleness panel

**Deliberately deferred to a second plan:** `agent_storage` / `agent_storage_items` (the `hint` parser and per-base sweep) and the faction tables. Those are spec slices 5–6, are by far the bulkiest, and nothing in slices 1–4 depends on them. This plan produces working, useful software on its own: after Task 9 you can query freight probation status and smuggling-wave readiness across the whole fleet.

## Global Constraints

- Go 1.24. Use modern idioms: `for i := range n`, `b.Loop()` in benchmarks.
- All new code must pass `golangci-lint` with **no new findings**. Run it after each task.
- Run `go build ./...` and `go test ./...` before every commit.
- Any sleep or pause must use a predefined constant from `pkg/game/constants.go`. This plan introduces none.
- Never assume server response field names — every struct field used here is verified against `pkg/game/serverapi`.
- Commit messages end with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`
- **Stage files explicitly.** Never `git add -A` — `data/*.json` is churn from the live fleet.
- Compiled binaries go in `bin/`, never the project root.
- The pre-commit race gate times out under fleet load. If it does, `--no-verify` is pre-approved, but you must then run `go build ./...`, `go test ./pkg/assets/ ./pkg/worker/`, and `golangci-lint` manually as the substitute gate.

## Verified facts this plan depends on

Do not re-derive these; they were confirmed against the codebase during design.

**Raw-JSON cache keys** (`client.GetRawJSON(key)`, set by `storeRawJSON` in `pkg/game/client.go`):

| Command | Key |
|---|---|
| `list_ships` | `owned_ships` |
| `shipping action=profile` | `shipping_profile` |
| `view_storage` | `storage` |
| `faction_info` | `faction_info` |
| `view_faction_storage` | `faction_storage` |

**`get_status` does not use the raw cache** — the client parses it into `State.Player`. Read `client.GetState().Player`.

**🔴 `Standings` ride only on a FULL player payload.** `pkg/game/client.go:2967` preserves the previous map when a partial payload arrives. So capture must call `GetStatus(ctx)` explicitly and must not rely on ambient state, or standings may be arbitrarily old while everything else is fresh.

**Existing constants to mirror (do not redefine):**
- `pkg/worker/mission_select.go:186` — `skillSmuggling = "smuggling"`
- `pkg/worker/mission_standing.go:7` — `pirateFactionID = "pirates"`
- `pkg/worker/mission_standing.go` — `smugglingXPExemptLevel = 3`
- `pkg/worker/freight.go:22` — `freightPackageFootprint = 100.0`

These are unexported in `pkg/worker`. `pkg/assets` must **not** import `pkg/worker` (it would create a cycle once `pkg/worker` imports `pkg/assets`). Redeclare the values in `pkg/assets` with a comment naming the `pkg/worker` original, and accept the documented divergence: `agent_capability` is a screening filter, and the worker's own gate stays authoritative at accept time.

## File Structure

**Create:**

| File | Responsibility |
|---|---|
| `pkg/assets/store.go` | `Config`, `Open`, `Close`, DSN, retry helper |
| `pkg/assets/schema.sql` | embedded DDL, `CREATE TABLE IF NOT EXISTS` |
| `pkg/assets/migrations.go` | `runMigrations`, `ensureColumn` |
| `pkg/assets/types.go` | `Identity`, `Profile`, `SkillRow`, `StandingRow`, `Carrier`, `Hull`, `Capability`, `Snapshot` |
| `pkg/assets/write_identity.go` | `UpsertIdentity` |
| `pkg/assets/write_profile.go` | `UpsertProfile`, `ReplaceSkills`, `ReplaceStandings` |
| `pkg/assets/write_hulls.go` | `ReplaceHulls` |
| `pkg/assets/write_carrier.go` | `UpsertCarrier` |
| `pkg/assets/parse.go` | `ParseCurrentMax`, `HullsFrom`, `CarrierFrom` |
| `pkg/assets/capability.go` | rule registry, `Evaluate`, `ReplaceCapabilities` |
| `pkg/assets/capture.go` | `CaptureProfile` orchestration |
| `pkg/assets/coverage.go` | `Coverage` staleness query |
| `pkg/ovdash/assets.go` | read-only coverage panel |

**Modify:**

| File | Change |
|---|---|
| `pkg/worker/dispatch.go` | `Assets` field on `WorkerDispatch`; `capture_profile` case; `supported` map entry |
| `cmd/worker/main.go` | `--assets-db-path` flag, open store, pass to dispatch |
| `cmd/tools/play_as/main.go` | `capture_profile` REPL command |
| `data/overmind/roles.yaml` | `capture_profile` schedule line per role |
| `pkg/ovdash/snapshot.go` | `AssetCoverage` field on `Snapshot` |

---

### Task 1: Store skeleton, schema, and the identity table

**Files:**
- Create: `pkg/assets/store.go`, `pkg/assets/schema.sql`, `pkg/assets/migrations.go`, `pkg/assets/types.go`, `pkg/assets/write_identity.go`
- Test: `pkg/assets/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `assets.Config`, `assets.DefaultConfig() Config`, `assets.Open(Config) (*Store, error)`, `(*Store).Close() error`, `(*Store).DB() *sql.DB`, `assets.Identity{PlayerID, AgentID, Username string}`, `(*Store).UpsertIdentity(ctx context.Context, id Identity, now time.Time) error`, `(*Store).LookupIdentity(ctx context.Context, playerID string) (Identity, bool, error)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/assets/store_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/assets/ -run 'TestIdentity|TestUsername|TestOpenIsIdempotent' -v`
Expected: FAIL — the package does not exist yet (`no Go files in .../pkg/assets`).

- [ ] **Step 3: Write the schema**

Create `pkg/assets/schema.sql`:

```sql
-- Agent asset + capability ledger. Keyed on the server's stable hex player_id.
-- Tables mirror the wire shapes: maps become narrow tables, structs become wide
-- ones, so a new skill or faction is a new ROW and needs no migration.
--
-- No foreign keys to the item/ship catalogs on purpose: the legacy agent_ships
-- table declares FOREIGN KEY(class_id) REFERENCES ships(id) and 96% of observed
-- class_id values do not resolve (prospector/prospect, excavator/excavation).
-- Store class_id verbatim and resolve at read time so a mismatch is visible
-- rather than fatal.

CREATE TABLE IF NOT EXISTS agents (
    player_id  TEXT PRIMARY KEY,
    agent_id   TEXT NOT NULL DEFAULT '',
    username   TEXT NOT NULL DEFAULT '',
    first_seen TEXT NOT NULL DEFAULT '',
    last_seen  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_agents_agent_id ON agents(agent_id);
```

- [ ] **Step 4: Write the store**

Create `pkg/assets/store.go`:

```go
// Package assets is the local ledger of what each agent is and owns: identity,
// skills, standings, carrier tier, and hulls, plus a derived eligibility layer.
//
// It lives in its own database (data/assets.db) for blast radius, not size.
// spacemolt-knowledge.db is 1.4GB and shared with the sibling spacemolt-kb
// repo, and market.db has already cost a full day of recovery from write
// contention. A separate file means an asset capture can never stall the fleet.
package assets

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

// Config holds configuration for the assets database.
type Config struct {
	DBPath       string
	WAL          bool
	MaxOpenConns int
	MaxIdleConns int
	BusyTimeout  time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		DBPath:       filepath.Join("data", "assets.db"),
		WAL:          true,
		MaxOpenConns: 10,
		MaxIdleConns: 2,
		BusyTimeout:  5 * time.Second,
	}
}

// Store owns the assets database handle.
type Store struct {
	db *sql.DB
}

// sqliteDSN builds the connection string. Pragmas go through the DSN, NOT
// db.Exec: an Exec pragma lands on whichever pooled connection it happens to
// get, whereas DSN pragmas run on every connection the pool opens. With ~110
// worker processes sharing this database, every connection must inherit
// busy_timeout and WAL or contention surfaces as an immediate SQLITE_BUSY
// instead of a clean blocking wait. Same reasoning as pkg/market/collector.go.
func sqliteDSN(cfg Config) string {
	dsn := cfg.DBPath + "?_pragma=busy_timeout(" + strconv.Itoa(int(cfg.BusyTimeout.Milliseconds())) + ")"
	if cfg.WAL {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	// Take the write lock at BEGIN rather than upgrading read->write mid
	// transaction. Every write here is a whole-set replacement inside one
	// transaction, so IMMEDIATE is correct for all of them.
	dsn += "&_txlock=immediate"

	return dsn
}

// Open creates a store against the assets database, running migrations.
func Open(cfg Config) (*Store, error) {
	if cfg.DBPath == "" {
		cfg = DefaultConfig()
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = DefaultConfig().MaxOpenConns
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = DefaultConfig().MaxIdleConns
	}
	if cfg.BusyTimeout == 0 {
		cfg.BusyTimeout = DefaultConfig().BusyTimeout
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, fmt.Errorf("assets: create dir: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(cfg))
	if err != nil {
		return nil, fmt.Errorf("assets: open database: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	if err := runMigrations(db); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("assets: run migrations: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database handle. A nil store closes cleanly so callers can
// treat "assets disabled" and "assets configured" the same way.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

// DB exposes the handle for read-only queries and tests.
func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}

	return s.db
}

// rfc3339 renders a timestamp in the format every captured_at column uses.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
```

Create `pkg/assets/migrations.go`:

```go
package assets

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var schemaSQL string

// runMigrations creates all tables and indexes. Idempotent — every worker
// calls Open on startup.
func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("assets: run schema: %w", err)
	}

	return nil
}

// ensureColumn adds column (with the given type) to table if not already
// present. SQLite has no "ADD COLUMN IF NOT EXISTS", so existence is checked
// via PRAGMA table_info first. Idempotent. schema.sql uses CREATE TABLE IF NOT
// EXISTS, so a column added to an existing table does not apply to databases
// created before that column existed — add those here.
func ensureColumn(db *sql.DB, table, column, colType string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("assets: table_info(%s): %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid        int
			name, typ  string
			notNull    int
			dflt       sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &primaryKey); err != nil {
			return fmt.Errorf("assets: scan table_info(%s): %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("assets: iterate table_info(%s): %w", table, err)
	}
	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colType)); err != nil {
		return fmt.Errorf("assets: add column %s.%s: %w", table, column, err)
	}

	return nil
}
```

Create `pkg/assets/types.go`:

```go
package assets

// Identity is the three-way agent identity map. None of the three is derivable
// from the others:
//   - PlayerID is the server's stable hex id (Player.ID). Never changes.
//   - Username is the in-game display name. CAN change.
//   - AgentID is our local label (engineer-3) from data/agents and the fleet
//     YAMLs. Not a game concept at all.
//
// PlayerID is the primary key everywhere in this package.
type Identity struct {
	PlayerID string
	AgentID  string
	Username string
}
```

Create `pkg/assets/write_identity.go`:

```go
package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// UpsertIdentity records the player_id -> (agent_id, username) mapping.
// A rename updates in place against the stable player_id; first_seen is set
// once and never overwritten.
func (s *Store) UpsertIdentity(ctx context.Context, id Identity, now time.Time) error {
	if s == nil || s.db == nil || id.PlayerID == "" {
		return nil
	}
	ts := rfc3339(now)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (player_id, agent_id, username, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(player_id) DO UPDATE SET
			agent_id  = excluded.agent_id,
			username  = excluded.username,
			last_seen = excluded.last_seen`,
		id.PlayerID, id.AgentID, id.Username, ts, ts)
	if err != nil {
		return fmt.Errorf("assets: upsert identity %s: %w", id.PlayerID, err)
	}

	return nil
}

// LookupIdentity returns the mapping for one player id. ok is false when the
// agent has never been captured.
func (s *Store) LookupIdentity(ctx context.Context, playerID string) (Identity, bool, error) {
	if s == nil || s.db == nil {
		return Identity{}, false, nil
	}
	id := Identity{PlayerID: playerID}
	err := s.db.QueryRowContext(ctx,
		`SELECT agent_id, username FROM agents WHERE player_id = ?`, playerID).
		Scan(&id.AgentID, &id.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, false, nil
	}
	if err != nil {
		return Identity{}, false, fmt.Errorf("assets: lookup identity %s: %w", playerID, err)
	}

	return id, true, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/assets/ -v`
Expected: PASS — all three tests.

- [ ] **Step 6: Lint and build**

Run: `go build ./... && golangci-lint run ./pkg/assets/`
Expected: no findings.

- [ ] **Step 7: Commit**

```bash
git add pkg/assets/store.go pkg/assets/schema.sql pkg/assets/migrations.go \
        pkg/assets/types.go pkg/assets/write_identity.go pkg/assets/store_test.go
git commit -m "feat(assets): store skeleton and the player_id identity map

player_id (the server's stable hex) is the key, not agent_id or username:
none of the three is derivable from the others, and usernames change.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Profile, skills, and standings

**Files:**
- Create: `pkg/assets/write_profile.go`
- Modify: `pkg/assets/schema.sql`, `pkg/assets/types.go`
- Test: `pkg/assets/write_profile_test.go`

**Interfaces:**
- Consumes: `*Store`, `rfc3339`, `openTestStore` from Task 1.
- Produces: `assets.Profile`, `assets.SkillRow{Skill string; Level int; XP float64}`, `assets.StandingRow{Faction string; Reputation, Baseline, OutstandingBounty int; JailedUntil string}`, `(*Store).UpsertProfile(ctx, Profile) error`, `(*Store).ReplaceSkills(ctx, playerID string, rows []SkillRow, now time.Time) error`, `(*Store).ReplaceStandings(ctx, playerID string, rows []StandingRow, now time.Time) error`.

- [ ] **Step 1: Write the failing test**

Create `pkg/assets/write_profile_test.go`:

```go
package assets

import (
	"context"
	"testing"
	"time"
)

// TestReplaceSkillsDropsVanishedRows pins the replacement invariant: a skill
// absent from a later capture must be DELETED, not left behind. Stale rows are
// phantom capability — the ledger would report an agent as eligible on the
// strength of data the server no longer reports.
func TestReplaceSkillsDropsVanishedRows(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	first := []SkillRow{{Skill: "smuggling", Level: 2, XP: 40}, {Skill: "trading", Level: 5, XP: 10}}
	if err := st.ReplaceSkills(ctx, "abc123", first, now); err != nil {
		t.Fatalf("first ReplaceSkills: %v", err)
	}
	second := []SkillRow{{Skill: "smuggling", Level: 3, XP: 0}}
	if err := st.ReplaceSkills(ctx, "abc123", second, now.Add(time.Hour)); err != nil {
		t.Fatalf("second ReplaceSkills: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_skills WHERE player_id = ?`, "abc123").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_skills rows = %d, want 1 (trading must be deleted)", n)
	}
	var level int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT level FROM agent_skills WHERE player_id = ? AND skill = ?`,
		"abc123", "smuggling").Scan(&level); err != nil {
		t.Fatalf("read smuggling: %v", err)
	}
	if level != 3 {
		t.Errorf("smuggling level = %d, want 3", level)
	}
}

// TestReplaceSkillsIsScopedToOneAgent pins that replacing one agent's skills
// leaves every other agent alone. A DELETE missing its player_id predicate
// would wipe the fleet on the next capture.
func TestReplaceSkillsIsScopedToOneAgent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceSkills(ctx, "agent-a", []SkillRow{{Skill: "trading", Level: 4}}, now); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := st.ReplaceSkills(ctx, "agent-b", []SkillRow{{Skill: "mining", Level: 1}}, now); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	if err := st.ReplaceSkills(ctx, "agent-b", []SkillRow{{Skill: "mining", Level: 2}}, now); err != nil {
		t.Fatalf("replace b: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_skills WHERE player_id = ?`, "agent-a").Scan(&n); err != nil {
		t.Fatalf("count a: %v", err)
	}
	if n != 1 {
		t.Errorf("agent-a rows = %d, want 1 (untouched)", n)
	}
}

// TestReplaceStandingsKeepsBaseline pins that baseline survives the round trip.
// Baseline, not reputation, is the durable signal: reputation floats above it
// from missions and decays back toward it, so stronghold access is
// pirates.baseline >= 10 and gating on reputation would flip back later.
func TestReplaceStandingsKeepsBaseline(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	rows := []StandingRow{{Faction: "pirates", Reputation: 42, Baseline: 10, OutstandingBounty: 0}}
	if err := st.ReplaceStandings(ctx, "abc123", rows, now); err != nil {
		t.Fatalf("ReplaceStandings: %v", err)
	}

	var rep, base int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT reputation, baseline FROM agent_standings WHERE player_id = ? AND faction = ?`,
		"abc123", "pirates").Scan(&rep, &base); err != nil {
		t.Fatalf("read: %v", err)
	}
	if rep != 42 || base != 10 {
		t.Errorf("got reputation=%d baseline=%d, want 42/10", rep, base)
	}
}

// TestUpsertProfileRoundTrip pins the scalar columns and that a second capture
// updates rather than duplicating.
func TestUpsertProfileRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	p := Profile{
		PlayerID: "abc123", Username: "Tester", Empire: "haven", Credits: 15135,
		HomeBase: "grand_exchange_station", DockedAtBase: "treasure_cache_trading_post",
		CurrentSystem: "voss", CurrentPOI: "treasure_cache_trading_post",
		ActiveShipID: "ship-1", FactionID: "databot", FactionRank: "leader",
		Experience: 900, CapturedAt: now,
	}
	if err := st.UpsertProfile(ctx, p); err != nil {
		t.Fatalf("first: %v", err)
	}
	p.Credits = 22000
	p.CapturedAt = now.Add(time.Hour)
	if err := st.UpsertProfile(ctx, p); err != nil {
		t.Fatalf("second: %v", err)
	}

	var (
		n       int
		credits float64
		base    string
	)
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_profile`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_profile rows = %d, want 1", n)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT credits, home_base FROM agent_profile WHERE player_id = ?`, "abc123").
		Scan(&credits, &base); err != nil {
		t.Fatalf("read: %v", err)
	}
	if credits != 22000 || base != "grand_exchange_station" {
		t.Errorf("got credits=%v home_base=%q, want 22000/grand_exchange_station", credits, base)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/assets/ -run 'TestReplaceSkills|TestReplaceStandings|TestUpsertProfile' -v`
Expected: FAIL — `undefined: SkillRow`, `undefined: StandingRow`, `undefined: Profile`.

- [ ] **Step 3: Add the tables**

Append to `pkg/assets/schema.sql`:

```sql
-- captured_at is PER TABLE, not per agent. The sources refresh on different
-- cadences (carrier profile is two free queries; a storage sweep is N calls),
-- and one agent-level timestamp would make a 20-minute-old skill level and a
-- 6-day-old holding indistinguishable.
CREATE TABLE IF NOT EXISTS agent_profile (
    player_id      TEXT PRIMARY KEY,
    username       TEXT NOT NULL DEFAULT '',
    empire         TEXT NOT NULL DEFAULT '',
    credits        REAL NOT NULL DEFAULT 0,
    home_base      TEXT NOT NULL DEFAULT '',
    docked_at_base TEXT NOT NULL DEFAULT '',
    current_system TEXT NOT NULL DEFAULT '',
    current_poi    TEXT NOT NULL DEFAULT '',
    active_ship_id TEXT NOT NULL DEFAULT '',
    faction_id     TEXT NOT NULL DEFAULT '',
    faction_rank   TEXT NOT NULL DEFAULT '',
    experience     INTEGER NOT NULL DEFAULT 0,
    captured_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_agent_profile_faction ON agent_profile(faction_id);

CREATE TABLE IF NOT EXISTS agent_skills (
    player_id   TEXT NOT NULL,
    skill       TEXT NOT NULL,
    level       INTEGER NOT NULL DEFAULT 0,
    xp          REAL NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, skill)
);
CREATE INDEX IF NOT EXISTS idx_agent_skills_skill ON agent_skills(skill, level);

-- baseline is the load-bearing column, not reputation: reputation floats above
-- baseline from missions and decays back toward it when idle, so baseline is
-- what makes an unlock permanent (an_introduction raises the pirate baseline
-- from -30 to 10, which is what makes stronghold docking stick).
CREATE TABLE IF NOT EXISTS agent_standings (
    player_id          TEXT NOT NULL,
    faction            TEXT NOT NULL,
    reputation         INTEGER NOT NULL DEFAULT 0,
    baseline           INTEGER NOT NULL DEFAULT 0,
    outstanding_bounty INTEGER NOT NULL DEFAULT 0,
    jailed_until       TEXT NOT NULL DEFAULT '',
    captured_at        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, faction)
);
CREATE INDEX IF NOT EXISTS idx_agent_standings_faction ON agent_standings(faction, baseline);
```

- [ ] **Step 4: Add the types**

Append to `pkg/assets/types.go`:

```go
import "time"

// Profile is the scalar half of get_status: one row per agent.
type Profile struct {
	PlayerID      string
	Username      string
	Empire        string
	Credits       float64
	HomeBase      string
	DockedAtBase  string
	CurrentSystem string
	CurrentPOI    string
	ActiveShipID  string
	FactionID     string
	FactionRank   string
	Experience    int64
	CapturedAt    time.Time
}

// SkillRow is one entry of Player.Skills.
type SkillRow struct {
	Skill string
	Level int
	XP    float64
}

// StandingRow is one entry of Player.Standings. Baseline is the decay target
// and therefore the durable signal; Reputation floats above it.
type StandingRow struct {
	Faction           string
	Reputation        int
	Baseline          int
	OutstandingBounty int
	JailedUntil       string
}
```

Note: `types.go` from Task 1 has no imports. Add the `import "time"` line at the top of the file, below the `package assets` line, rather than in the middle.

- [ ] **Step 5: Write the writers**

Create `pkg/assets/write_profile.go`:

```go
package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UpsertProfile writes the scalar half of get_status.
func (s *Store) UpsertProfile(ctx context.Context, p Profile) error {
	if s == nil || s.db == nil || p.PlayerID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_profile (player_id, username, empire, credits, home_base,
			docked_at_base, current_system, current_poi, active_ship_id,
			faction_id, faction_rank, experience, captured_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(player_id) DO UPDATE SET
			username=excluded.username, empire=excluded.empire,
			credits=excluded.credits, home_base=excluded.home_base,
			docked_at_base=excluded.docked_at_base,
			current_system=excluded.current_system, current_poi=excluded.current_poi,
			active_ship_id=excluded.active_ship_id, faction_id=excluded.faction_id,
			faction_rank=excluded.faction_rank, experience=excluded.experience,
			captured_at=excluded.captured_at`,
		p.PlayerID, p.Username, p.Empire, p.Credits, p.HomeBase,
		p.DockedAtBase, p.CurrentSystem, p.CurrentPOI, p.ActiveShipID,
		p.FactionID, p.FactionRank, p.Experience, rfc3339(p.CapturedAt))
	if err != nil {
		return fmt.Errorf("assets: upsert profile %s: %w", p.PlayerID, err)
	}

	return nil
}

// replaceSet runs one whole-set replacement inside a single transaction:
// delete every row for this agent, then insert the new set. Upserting row by
// row would leave rows behind for entries the server no longer reports, and
// phantom data is exactly what would make this ledger untrustworthy.
func (s *Store) replaceSet(ctx context.Context, delSQL, playerID string, insert func(*sql.Tx) error) error {
	if s == nil || s.db == nil || playerID == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("assets: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, delSQL, playerID); err != nil {
		return fmt.Errorf("assets: clear rows for %s: %w", playerID, err)
	}
	if err := insert(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("assets: commit: %w", err)
	}

	return nil
}

// ReplaceSkills swaps in the agent's full skill set. Skills absent from rows
// are deleted.
func (s *Store) ReplaceSkills(ctx context.Context, playerID string, rows []SkillRow, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_skills WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO agent_skills (player_id, skill, level, xp, captured_at) VALUES (?,?,?,?,?)`,
				playerID, r.Skill, r.Level, r.XP, ts); err != nil {
				return fmt.Errorf("assets: insert skill %s/%s: %w", playerID, r.Skill, err)
			}
		}

		return nil
	})
}

// ReplaceStandings swaps in the agent's full standings set.
func (s *Store) ReplaceStandings(ctx context.Context, playerID string, rows []StandingRow, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_standings WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO agent_standings (player_id, faction, reputation, baseline,
					outstanding_bounty, jailed_until, captured_at) VALUES (?,?,?,?,?,?,?)`,
				playerID, r.Faction, r.Reputation, r.Baseline,
				r.OutstandingBounty, r.JailedUntil, ts); err != nil {
				return fmt.Errorf("assets: insert standing %s/%s: %w", playerID, r.Faction, err)
			}
		}

		return nil
	})
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./pkg/assets/ -v`
Expected: PASS — all Task 1 and Task 2 tests.

- [ ] **Step 7: Lint and commit**

```bash
go build ./... && golangci-lint run ./pkg/assets/
git add pkg/assets/schema.sql pkg/assets/types.go pkg/assets/write_profile.go \
        pkg/assets/write_profile_test.go
git commit -m "feat(assets): profile, skills, and standings tables

Skills and standings are whole-set replacements inside one transaction: a row
the server stops reporting must be DELETED, not left behind as phantom
capability. baseline is stored alongside reputation because baseline is the
decay target and therefore the durable unlock signal.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: The `current/max` parser and hull decoding

**Files:**
- Create: `pkg/assets/parse.go`
- Modify: `pkg/assets/types.go`
- Test: `pkg/assets/parse_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `assets.Hull` struct, `assets.ParseCurrentMax(s string) (cur, max int, ok bool)`, `assets.HullsFrom(raw []byte) ([]Hull, error)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/assets/parse_test.go`:

```go
package assets

import "testing"

// TestParseCurrentMax pins the OwnedShip hull/fuel format. These arrive as
// STRINGS ("1020/1020", "150/200"), not numbers. The partial case is the one
// that matters: a hull that exists but is not ready to fly.
func TestParseCurrentMax(t *testing.T) {
	for _, tc := range []struct {
		in            string
		cur, max      int
		ok            bool
	}{
		{"1020/1020", 1020, 1020, true},
		{"150/200", 150, 200, true},
		{"0/200", 0, 200, true},
		{"", 0, 0, false},
		{"340", 0, 0, false},
		{"a/b", 0, 0, false},
		{"1/2/3", 0, 0, false},
	} {
		cur, max, ok := ParseCurrentMax(tc.in)
		if cur != tc.cur || max != tc.max || ok != tc.ok {
			t.Errorf("ParseCurrentMax(%q) = (%d,%d,%v), want (%d,%d,%v)",
				tc.in, cur, max, ok, tc.cur, tc.max, tc.ok)
		}
	}
}

// TestHullsFromGoldenPayload decodes a real list_ships body. The raw hull and
// fuel strings are retained alongside the parsed ints so a server-side format
// change surfaces as a mismatch instead of silent zeros.
func TestHullsFromGoldenPayload(t *testing.T) {
	raw := []byte(`{"action":"list_ships","count":2,"active_ship_id":"aaa",
	 "ships":[
	  {"class_id":"survey_vessel","class_name":"Survey Vessel","fuel":"1020/1020",
	   "hull":"340/340","is_active":false,"location":"stored at Grand Exchange Station",
	   "location_base_id":"grand_exchange_station","modules":3,
	   "ship_id":"74aeb79e64d9a12f682a2ee6daad79e4"},
	  {"class_id":"reclaim","class_name":"Reclaim","fuel":"150/200","hull":"180/180",
	   "is_active":true,"location":"stored at Grand Exchange Station",
	   "location_base_id":"grand_exchange_station","modules":2,
	   "ship_id":"67ef4a3e25dc336829d7d3e25736fe61"}]}`)

	hulls, err := HullsFrom(raw)
	if err != nil {
		t.Fatalf("HullsFrom: %v", err)
	}
	if len(hulls) != 2 {
		t.Fatalf("got %d hulls, want 2", len(hulls))
	}

	h := hulls[0]
	if h.ShipID != "74aeb79e64d9a12f682a2ee6daad79e4" || h.ClassID != "survey_vessel" {
		t.Errorf("hull[0] identity = %q/%q", h.ShipID, h.ClassID)
	}
	if h.FuelCurrent != 1020 || h.FuelMax != 1020 || h.FuelRaw != "1020/1020" {
		t.Errorf("hull[0] fuel = %d/%d raw=%q", h.FuelCurrent, h.FuelMax, h.FuelRaw)
	}
	if h.LocationBaseID != "grand_exchange_station" || h.Modules != 3 {
		t.Errorf("hull[0] base=%q modules=%d", h.LocationBaseID, h.Modules)
	}
	if h.IsActive {
		t.Error("hull[0] must not be active")
	}

	h = hulls[1]
	if h.FuelCurrent != 150 || h.FuelMax != 200 {
		t.Errorf("hull[1] fuel = %d/%d, want 150/200", h.FuelCurrent, h.FuelMax)
	}
	if !h.IsActive {
		t.Error("hull[1] must be active")
	}
}

// TestHullsFromEmptyIsNotAnError pins that an absent cache entry yields no
// hulls and no error: capture must degrade to "nothing this pass", never fail.
func TestHullsFromEmptyIsNotAnError(t *testing.T) {
	hulls, err := HullsFrom(nil)
	if err != nil || len(hulls) != 0 {
		t.Fatalf("HullsFrom(nil) = %v, %v; want empty, nil", hulls, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/assets/ -run 'TestParseCurrentMax|TestHullsFrom' -v`
Expected: FAIL — `undefined: ParseCurrentMax`, `undefined: HullsFrom`.

- [ ] **Step 3: Add the Hull type**

Append to `pkg/assets/types.go`:

```go
// Hull is one owned ship from list_ships. ListShips reports EVERY owned ship
// and its location, and serverapi.OwnedShip is a strict superset of
// StorageShip, so one free call replaces a per-base ship sweep.
//
// HullRaw/FuelRaw retain the server's "current/max" strings so a format change
// shows up as a mismatch rather than as silent zeros.
type Hull struct {
	ShipID         string
	ClassID        string
	ClassName      string
	IsActive       bool
	HullCurrent    int
	HullMax        int
	HullRaw        string
	FuelCurrent    int
	FuelMax        int
	FuelRaw        string
	CargoUsed      int
	Location       string
	LocationBaseID string
	Modules        int
	ListingID      string
	ListingPrice   int64
	ListingBaseID  string
}
```

- [ ] **Step 4: Write the parser**

Create `pkg/assets/parse.go`:

```go
package assets

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ParseCurrentMax splits the server's "current/max" strings (OwnedShip.Hull,
// OwnedShip.Fuel) into two ints. ok is false for anything that is not exactly
// two integers separated by one slash — callers keep the raw string in that
// case rather than recording a misleading zero.
func ParseCurrentMax(s string) (cur, max int, ok bool) {
	before, after, found := strings.Cut(s, "/")
	if !found || strings.Contains(after, "/") {
		return 0, 0, false
	}
	c, err := strconv.Atoi(strings.TrimSpace(before))
	if err != nil {
		return 0, 0, false
	}
	m, err := strconv.Atoi(strings.TrimSpace(after))
	if err != nil {
		return 0, 0, false
	}

	return c, m, true
}

// HullsFrom decodes a raw list_ships body (cache key "owned_ships") into hull
// rows. An empty body yields no hulls and no error: a missing cache entry means
// "nothing captured this pass", which must never fail the pass.
func HullsFrom(raw []byte) ([]Hull, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var resp serverapi.ListShipsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("assets: decode list_ships: %w", err)
	}
	out := make([]Hull, 0, len(resp.Ships))
	for _, s := range resp.Ships {
		h := Hull{
			ShipID:         s.ShipID,
			ClassID:        s.ClassID,
			ClassName:      s.ClassName,
			IsActive:       s.IsActive,
			HullRaw:        s.Hull,
			FuelRaw:        s.Fuel,
			CargoUsed:      s.CargoUsed,
			Location:       s.Location,
			LocationBaseID: s.LocationBaseID,
			Modules:        s.Modules,
			ListingID:      s.ListingID,
			ListingPrice:   s.ListingPrice,
			ListingBaseID:  s.ListingBaseID,
		}
		h.HullCurrent, h.HullMax, _ = ParseCurrentMax(s.Hull)
		h.FuelCurrent, h.FuelMax, _ = ParseCurrentMax(s.Fuel)
		out = append(out, h)
	}

	return out, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/assets/ -v`
Expected: PASS.

- [ ] **Step 6: Lint and commit**

```bash
go build ./... && golangci-lint run ./pkg/assets/
git add pkg/assets/parse.go pkg/assets/types.go pkg/assets/parse_test.go
git commit -m "feat(assets): decode list_ships hulls, including current/max strings

OwnedShip.Hull and .Fuel arrive as strings (\"150/200\"), not numbers. Parsed
into ints with the raw string retained so a format change is visible rather
than silently zeroing every hull.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: The `agent_hulls` table

**Files:**
- Create: `pkg/assets/write_hulls.go`
- Modify: `pkg/assets/schema.sql`
- Test: `pkg/assets/write_hulls_test.go`

**Interfaces:**
- Consumes: `Hull` (Task 3), `replaceSet` (Task 2), `openTestStore` (Task 1).
- Produces: `(*Store).ReplaceHulls(ctx context.Context, playerID string, rows []Hull, now time.Time) error`.

- [ ] **Step 1: Write the failing test**

Create `pkg/assets/write_hulls_test.go`:

```go
package assets

import (
	"context"
	"testing"
	"time"
)

// TestReplaceHullsDropsSoldShips pins the replacement invariant for hulls: a
// ship the agent no longer owns must disappear. A phantom hull would make the
// ledger claim capacity the agent does not have.
func TestReplaceHullsDropsSoldShips(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	first := []Hull{
		{ShipID: "s1", ClassID: "survey_vessel", FuelCurrent: 1020, FuelMax: 1020, FuelRaw: "1020/1020"},
		{ShipID: "s2", ClassID: "reclaim", FuelCurrent: 150, FuelMax: 200, FuelRaw: "150/200"},
	}
	if err := st.ReplaceHulls(ctx, "abc123", first, now); err != nil {
		t.Fatalf("first ReplaceHulls: %v", err)
	}
	if err := st.ReplaceHulls(ctx, "abc123", first[:1], now.Add(time.Hour)); err != nil {
		t.Fatalf("second ReplaceHulls: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_hulls WHERE player_id = ?`, "abc123").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_hulls rows = %d, want 1 (s2 must be deleted)", n)
	}
}

// TestReplaceHullsKeepsRawStrings pins that the raw current/max strings are
// persisted next to the parsed ints.
func TestReplaceHullsKeepsRawStrings(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	rows := []Hull{{
		ShipID: "s1", ClassID: "reclaim", ClassName: "Reclaim", IsActive: true,
		HullCurrent: 180, HullMax: 180, HullRaw: "180/180",
		FuelCurrent: 150, FuelMax: 200, FuelRaw: "150/200",
		LocationBaseID: "grand_exchange_station", Modules: 2,
	}}
	if err := st.ReplaceHulls(ctx, "abc123", rows, now); err != nil {
		t.Fatalf("ReplaceHulls: %v", err)
	}

	var (
		fuelRaw string
		fuelCur int
		base    string
	)
	if err := st.DB().QueryRowContext(ctx,
		`SELECT fuel_raw, fuel_current, location_base_id FROM agent_hulls
		 WHERE player_id = ? AND ship_id = ?`, "abc123", "s1").
		Scan(&fuelRaw, &fuelCur, &base); err != nil {
		t.Fatalf("read: %v", err)
	}
	if fuelRaw != "150/200" || fuelCur != 150 || base != "grand_exchange_station" {
		t.Errorf("got fuel_raw=%q fuel_current=%d base=%q", fuelRaw, fuelCur, base)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/assets/ -run TestReplaceHulls -v`
Expected: FAIL — `st.ReplaceHulls undefined`.

- [ ] **Step 3: Add the table**

Append to `pkg/assets/schema.sql`:

```sql
-- NO foreign key on class_id: see the header note. Also no agent_storage_ships
-- table — list_ships already reports every owned hull AND its location, and
-- OwnedShip is a strict superset of StorageShip, so view_storage's ships array
-- is a cross-check rather than a second source.
CREATE TABLE IF NOT EXISTS agent_hulls (
    player_id        TEXT NOT NULL,
    ship_id          TEXT NOT NULL,
    class_id         TEXT NOT NULL DEFAULT '',
    class_name       TEXT NOT NULL DEFAULT '',
    is_active        INTEGER NOT NULL DEFAULT 0,
    hull_current     INTEGER NOT NULL DEFAULT 0,
    hull_max         INTEGER NOT NULL DEFAULT 0,
    hull_raw         TEXT NOT NULL DEFAULT '',
    fuel_current     INTEGER NOT NULL DEFAULT 0,
    fuel_max         INTEGER NOT NULL DEFAULT 0,
    fuel_raw         TEXT NOT NULL DEFAULT '',
    cargo_used       INTEGER NOT NULL DEFAULT 0,
    location         TEXT NOT NULL DEFAULT '',
    location_base_id TEXT NOT NULL DEFAULT '',
    modules          INTEGER NOT NULL DEFAULT 0,
    listing_id       TEXT NOT NULL DEFAULT '',
    listing_price    INTEGER NOT NULL DEFAULT 0,
    listing_base_id  TEXT NOT NULL DEFAULT '',
    captured_at      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, ship_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_hulls_base ON agent_hulls(location_base_id);
CREATE INDEX IF NOT EXISTS idx_agent_hulls_class ON agent_hulls(class_id);
```

- [ ] **Step 4: Write the writer**

Create `pkg/assets/write_hulls.go`:

```go
package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ReplaceHulls swaps in the agent's full owned-ship set. Ships absent from rows
// are deleted — a hull the agent has sold must not linger as phantom capacity.
func (s *Store) ReplaceHulls(ctx context.Context, playerID string, rows []Hull, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_hulls WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, h := range rows {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO agent_hulls (player_id, ship_id, class_id, class_name, is_active,
					hull_current, hull_max, hull_raw, fuel_current, fuel_max, fuel_raw,
					cargo_used, location, location_base_id, modules,
					listing_id, listing_price, listing_base_id, captured_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				playerID, h.ShipID, h.ClassID, h.ClassName, h.IsActive,
				h.HullCurrent, h.HullMax, h.HullRaw, h.FuelCurrent, h.FuelMax, h.FuelRaw,
				h.CargoUsed, h.Location, h.LocationBaseID, h.Modules,
				h.ListingID, h.ListingPrice, h.ListingBaseID, ts); err != nil {
				return fmt.Errorf("assets: insert hull %s/%s: %w", playerID, h.ShipID, err)
			}
		}

		return nil
	})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/assets/ -v`
Expected: PASS.

- [ ] **Step 6: Lint and commit**

```bash
go build ./... && golangci-lint run ./pkg/assets/
git add pkg/assets/schema.sql pkg/assets/write_hulls.go pkg/assets/write_hulls_test.go
git commit -m "feat(assets): agent_hulls table with whole-set replacement

No FK on class_id: the legacy agent_ships table has one and 96% of observed
class_id values fail to resolve against the catalog, which is a plausible
reason it has never had a row written.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Carrier profile

**Files:**
- Create: `pkg/assets/write_carrier.go`
- Modify: `pkg/assets/schema.sql`, `pkg/assets/types.go`, `pkg/assets/parse.go`
- Test: `pkg/assets/write_carrier_test.go`

**Interfaces:**
- Consumes: `*Store`, `rfc3339`, `openTestStore`.
- Produces: `assets.Carrier` struct, `assets.CarrierFrom(raw []byte) (Carrier, bool, error)`, `(*Store).UpsertCarrier(ctx context.Context, playerID string, c Carrier, now time.Time) error`.

- [ ] **Step 1: Write the failing test**

Create `pkg/assets/write_carrier_test.go`:

```go
package assets

import (
	"context"
	"testing"
	"time"
)

// TestCarrierFromGoldenPayload decodes a real shipping action=profile body.
// The tier-progress block is the part that matters most: remaining_* is what
// turns "still in probation" into "needs 3 more deliveries and 180 value".
func TestCarrierFromGoldenPayload(t *testing.T) {
	raw := []byte(`{"action":"profile","debt_blocks_acceptance":false,"debt_block_reason":"",
	 "profile":{"actor":{"kind":"player","id":"engineer-2"},"tier":"probationary",
	  "successful_deliveries":2,"delivered_value":70,"priority_deliveries":0,
	  "returns":0,"breaches":0,"defaults":0,"active_contracts":1,
	  "active_liability":500,"outstanding_debt":0,"updated_at":"2026-08-01T00:00:00Z"},
	 "capacity":{"active_contracts":1,"active_contracts_unlimited":false,
	  "active_contract_limit":3,"active_liability":500,"liability_unlimited":false,
	  "aggregate_liability_limit":10000,"remaining_aggregate_liability":9500,
	  "single_package_liability_limit":2000},
	 "progression":{"current_tier":"probationary","next_tier":"licensed",
	  "at_maximum_tier":false,"successful_deliveries":2,
	  "required_successful_deliveries":5,"remaining_successful_deliveries":3,
	  "delivered_value":70,"required_delivered_value":250,
	  "remaining_delivered_value":180}}`)

	c, ok, err := CarrierFrom(raw)
	if err != nil || !ok {
		t.Fatalf("CarrierFrom: ok=%v err=%v", ok, err)
	}
	if c.Tier != "probationary" || c.SuccessfulDeliveries != 2 {
		t.Errorf("tier=%q deliveries=%d", c.Tier, c.SuccessfulDeliveries)
	}
	if c.RemainingSuccessfulDeliveries != 3 || c.RemainingDeliveredValue != 180 {
		t.Errorf("remaining = %d deliveries / %d value, want 3 / 180",
			c.RemainingSuccessfulDeliveries, c.RemainingDeliveredValue)
	}
	if c.RemainingAggregateLiability != 9500 || c.ActiveContractLimit != 3 {
		t.Errorf("capacity = %d liability / %d contracts",
			c.RemainingAggregateLiability, c.ActiveContractLimit)
	}
	if c.DebtBlocksAcceptance {
		t.Error("debt_blocks_acceptance must be false")
	}
}

// TestCarrierFromEmptyIsNotAnError pins that an absent cache entry means
// "not captured", not a failure.
func TestCarrierFromEmptyIsNotAnError(t *testing.T) {
	c, ok, err := CarrierFrom(nil)
	if err != nil || ok {
		t.Fatalf("CarrierFrom(nil) = %+v, %v, %v; want zero, false, nil", c, ok, err)
	}
}

// TestUpsertCarrierRoundTrip pins persistence and that a re-capture updates
// rather than duplicating.
func TestUpsertCarrierRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	c := Carrier{Tier: "probationary", SuccessfulDeliveries: 2, RemainingSuccessfulDeliveries: 3}
	if err := st.UpsertCarrier(ctx, "abc123", c, now); err != nil {
		t.Fatalf("first: %v", err)
	}
	c.Tier = "licensed"
	c.RemainingSuccessfulDeliveries = 0
	if err := st.UpsertCarrier(ctx, "abc123", c, now.Add(time.Hour)); err != nil {
		t.Fatalf("second: %v", err)
	}

	var (
		n    int
		tier string
	)
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_carrier`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_carrier rows = %d, want 1", n)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT tier FROM agent_carrier WHERE player_id = ?`, "abc123").Scan(&tier); err != nil {
		t.Fatalf("read: %v", err)
	}
	if tier != "licensed" {
		t.Errorf("tier = %q, want licensed", tier)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/assets/ -run TestCarrier -v`
Expected: FAIL — `undefined: CarrierFrom`, `undefined: Carrier`.

- [ ] **Step 3: Add the table**

Append to `pkg/assets/schema.sql`:

```sql
-- CarrierProfile + CarrierCapacity + CarrierTierProgress flattened into one
-- row. The remaining_* columns are what make "who is still in probation and
-- how far off are they" a query instead of a worker-log sweep.
CREATE TABLE IF NOT EXISTS agent_carrier (
    player_id                       TEXT PRIMARY KEY,
    tier                            TEXT NOT NULL DEFAULT '',
    successful_deliveries           INTEGER NOT NULL DEFAULT 0,
    delivered_value                 INTEGER NOT NULL DEFAULT 0,
    priority_deliveries             INTEGER NOT NULL DEFAULT 0,
    returns                         INTEGER NOT NULL DEFAULT 0,
    breaches                        INTEGER NOT NULL DEFAULT 0,
    defaults                        INTEGER NOT NULL DEFAULT 0,
    active_contracts                INTEGER NOT NULL DEFAULT 0,
    active_liability                INTEGER NOT NULL DEFAULT 0,
    outstanding_debt                INTEGER NOT NULL DEFAULT 0,
    debt_blocks_acceptance          INTEGER NOT NULL DEFAULT 0,
    next_tier                       TEXT NOT NULL DEFAULT '',
    at_maximum_tier                 INTEGER NOT NULL DEFAULT 0,
    required_successful_deliveries  INTEGER NOT NULL DEFAULT 0,
    remaining_successful_deliveries INTEGER NOT NULL DEFAULT 0,
    required_delivered_value        INTEGER NOT NULL DEFAULT 0,
    remaining_delivered_value       INTEGER NOT NULL DEFAULT 0,
    active_contract_limit           INTEGER NOT NULL DEFAULT 0,
    active_contracts_unlimited      INTEGER NOT NULL DEFAULT 0,
    aggregate_liability_limit       INTEGER NOT NULL DEFAULT 0,
    remaining_aggregate_liability   INTEGER NOT NULL DEFAULT 0,
    single_package_liability_limit  INTEGER NOT NULL DEFAULT 0,
    liability_unlimited             INTEGER NOT NULL DEFAULT 0,
    captured_at                     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_agent_carrier_tier ON agent_carrier(tier);
```

- [ ] **Step 4: Add the type and decoder**

Append to `pkg/assets/types.go`:

```go
// Carrier is the freight carrier standing: CarrierProfile, CarrierCapacity and
// CarrierTierProgress flattened. Remaining* is progress toward the next tier.
type Carrier struct {
	Tier                          string
	SuccessfulDeliveries          int
	DeliveredValue                int64
	PriorityDeliveries            int
	Returns                       int
	Breaches                      int
	Defaults                      int
	ActiveContracts               int
	ActiveLiability               int64
	OutstandingDebt               int64
	DebtBlocksAcceptance          bool
	NextTier                      string
	AtMaximumTier                 bool
	RequiredSuccessfulDeliveries  int
	RemainingSuccessfulDeliveries int
	RequiredDeliveredValue        int64
	RemainingDeliveredValue       int64
	ActiveContractLimit           int
	ActiveContractsUnlimited      bool
	AggregateLiabilityLimit       int64
	RemainingAggregateLiability   int64
	SinglePackageLiabilityLimit   int64
	LiabilityUnlimited            bool
}
```

Append to `pkg/assets/parse.go`:

```go
// CarrierFrom decodes a raw shipping action=profile body (cache key
// "shipping_profile"). ok is false for an empty body — "not captured this
// pass", not an error.
func CarrierFrom(raw []byte) (Carrier, bool, error) {
	if len(raw) == 0 {
		return Carrier{}, false, nil
	}
	var resp serverapi.ShippingProfileResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Carrier{}, false, fmt.Errorf("assets: decode shipping profile: %w", err)
	}
	// NOTE the field is Progression (json "progression"), NOT TierProgress.
	// Verified against pkg/game/serverapi/responses_shipping.go:184. Decoding
	// against the wrong key succeeds with every field zero, which would read as
	// "no progress toward the next tier" rather than as an error.
	p, capy, tp := resp.Profile, resp.Capacity, resp.Progression

	return Carrier{
		Tier:                          p.Tier,
		SuccessfulDeliveries:          p.SuccessfulDeliveries,
		DeliveredValue:                p.DeliveredValue,
		PriorityDeliveries:            p.PriorityDeliveries,
		Returns:                       p.Returns,
		Breaches:                      p.Breaches,
		Defaults:                      p.Defaults,
		ActiveContracts:               p.ActiveContracts,
		ActiveLiability:               p.ActiveLiability,
		OutstandingDebt:               p.OutstandingDebt,
		DebtBlocksAcceptance:          resp.DebtBlocksAcceptance,
		NextTier:                      tp.NextTier,
		AtMaximumTier:                 tp.AtMaximumTier,
		RequiredSuccessfulDeliveries:  tp.RequiredSuccessfulDeliveries,
		RemainingSuccessfulDeliveries: tp.RemainingSuccessfulDeliveries,
		RequiredDeliveredValue:        tp.RequiredDeliveredValue,
		RemainingDeliveredValue:       tp.RemainingDeliveredValue,
		ActiveContractLimit:           capy.ActiveContractLimit,
		ActiveContractsUnlimited:      capy.ActiveContractsUnlimited,
		AggregateLiabilityLimit:       capy.AggregateLiabilityLimit,
		RemainingAggregateLiability:   capy.RemainingAggregateLiability,
		SinglePackageLiabilityLimit:   capy.SinglePackageLiabilityLimit,
		LiabilityUnlimited:            capy.LiabilityUnlimited,
	}, true, nil
}
```

The local is `capy`, not `cap`: `cap` is a Go builtin and shadowing it trips the
linter.

Field names above are verified against `pkg/game/serverapi/responses_shipping.go:184`:
`Action`, `Profile`, `Capacity`, **`Progression`**, `Debts`,
`DebtBlocksAcceptance`, `DebtBlockReason`. There is no `TierProgress` field —
using that name, or the JSON key `tier_progress`, decodes to an all-zero struct
*successfully*, so the mistake surfaces as "no tier progress" rather than as an
error.

- [ ] **Step 5: Write the writer**

Create `pkg/assets/write_carrier.go`:

```go
package assets

import (
	"context"
	"fmt"
	"time"
)

// UpsertCarrier writes the agent's freight carrier standing.
func (s *Store) UpsertCarrier(ctx context.Context, playerID string, c Carrier, now time.Time) error {
	if s == nil || s.db == nil || playerID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_carrier (player_id, tier, successful_deliveries, delivered_value,
			priority_deliveries, returns, breaches, defaults, active_contracts,
			active_liability, outstanding_debt, debt_blocks_acceptance, next_tier,
			at_maximum_tier, required_successful_deliveries, remaining_successful_deliveries,
			required_delivered_value, remaining_delivered_value, active_contract_limit,
			active_contracts_unlimited, aggregate_liability_limit,
			remaining_aggregate_liability, single_package_liability_limit,
			liability_unlimited, captured_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(player_id) DO UPDATE SET
			tier=excluded.tier, successful_deliveries=excluded.successful_deliveries,
			delivered_value=excluded.delivered_value,
			priority_deliveries=excluded.priority_deliveries, returns=excluded.returns,
			breaches=excluded.breaches, defaults=excluded.defaults,
			active_contracts=excluded.active_contracts,
			active_liability=excluded.active_liability,
			outstanding_debt=excluded.outstanding_debt,
			debt_blocks_acceptance=excluded.debt_blocks_acceptance,
			next_tier=excluded.next_tier, at_maximum_tier=excluded.at_maximum_tier,
			required_successful_deliveries=excluded.required_successful_deliveries,
			remaining_successful_deliveries=excluded.remaining_successful_deliveries,
			required_delivered_value=excluded.required_delivered_value,
			remaining_delivered_value=excluded.remaining_delivered_value,
			active_contract_limit=excluded.active_contract_limit,
			active_contracts_unlimited=excluded.active_contracts_unlimited,
			aggregate_liability_limit=excluded.aggregate_liability_limit,
			remaining_aggregate_liability=excluded.remaining_aggregate_liability,
			single_package_liability_limit=excluded.single_package_liability_limit,
			liability_unlimited=excluded.liability_unlimited,
			captured_at=excluded.captured_at`,
		playerID, c.Tier, c.SuccessfulDeliveries, c.DeliveredValue,
		c.PriorityDeliveries, c.Returns, c.Breaches, c.Defaults, c.ActiveContracts,
		c.ActiveLiability, c.OutstandingDebt, c.DebtBlocksAcceptance, c.NextTier,
		c.AtMaximumTier, c.RequiredSuccessfulDeliveries, c.RemainingSuccessfulDeliveries,
		c.RequiredDeliveredValue, c.RemainingDeliveredValue, c.ActiveContractLimit,
		c.ActiveContractsUnlimited, c.AggregateLiabilityLimit,
		c.RemainingAggregateLiability, c.SinglePackageLiabilityLimit,
		c.LiabilityUnlimited, rfc3339(now))
	if err != nil {
		return fmt.Errorf("assets: upsert carrier %s: %w", playerID, err)
	}

	return nil
}
```

- [ ] **Step 6: Run tests, lint, commit**

```bash
go test ./pkg/assets/ -v && go build ./... && golangci-lint run ./pkg/assets/
git add pkg/assets/schema.sql pkg/assets/types.go pkg/assets/parse.go \
        pkg/assets/write_carrier.go pkg/assets/write_carrier_test.go
git commit -m "feat(assets): carrier profile, tier progress, and liability headroom

remaining_successful_deliveries / remaining_delivered_value turn 'still in
probation' into a number, which is the question that currently needs a manual
worker-log sweep.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: The eligibility registry

**Files:**
- Create: `pkg/assets/capability.go`
- Modify: `pkg/assets/schema.sql`
- Test: `pkg/assets/capability_test.go`

**Interfaces:**
- Consumes: `Profile`, `SkillRow`, `StandingRow`, `Carrier`, `Hull`, `replaceSet`.
- Produces: `assets.AgentSnapshot{Profile Profile; Skills map[string]SkillRow; Standings map[string]StandingRow; Carrier Carrier; CarrierKnown bool; Hulls []Hull}`, `assets.Capability{Capability string; Eligible bool; BlockingReason string}`, `assets.Rules() map[string]func(AgentSnapshot) (bool, string)`, `assets.Evaluate(AgentSnapshot) []Capability`, `(*Store).ReplaceCapabilities(ctx, playerID string, rows []Capability, now time.Time) error`.

- [ ] **Step 1: Write the failing test**

Create `pkg/assets/capability_test.go`:

```go
package assets

import (
	"context"
	"testing"
	"time"
)

// snapWith builds a snapshot with one active hull of the given cargo capacity
// proxy (cargo_used is not capacity; the active hull's presence is what the
// v1 rules key on) plus the given skills and standings.
func snapWith(smuggling int, pirateBaseline int, tier string, debt int64) AgentSnapshot {
	return AgentSnapshot{
		Profile: Profile{PlayerID: "abc123", Credits: 100000},
		Skills:  map[string]SkillRow{"smuggling": {Skill: "smuggling", Level: smuggling}},
		Standings: map[string]StandingRow{
			"pirates": {Faction: "pirates", Baseline: pirateBaseline},
		},
		Carrier:      Carrier{Tier: tier, OutstandingDebt: debt},
		CarrierKnown: tier != "",
		Hulls:        []Hull{{ShipID: "s1", IsActive: true, FuelCurrent: 400, FuelMax: 400}},
	}
}

// TestSmugglingEligibilityBoundary pins the L3 threshold, mirroring
// pkg/worker's smugglingXPExemptLevel. Level 3 unlocks the chain-2 reputation
// mission, which is the whole point of the climb.
func TestSmugglingEligibilityBoundary(t *testing.T) {
	for _, tc := range []struct {
		level int
		want  bool
	}{{0, false}, {2, false}, {3, true}, {7, true}} {
		caps := capsByName(Evaluate(snapWith(tc.level, -30, "licensed", 0)))
		got := caps["smuggling"]
		if got.Eligible != tc.want {
			t.Errorf("smuggling level %d: eligible = %v, want %v (reason %q)",
				tc.level, got.Eligible, tc.want, got.BlockingReason)
		}
		if !tc.want && got.BlockingReason == "" {
			t.Errorf("smuggling level %d: ineligible must carry a blocking reason", tc.level)
		}
	}
}

// TestStrongholdAccessUsesBaselineNotReputation pins that the gate reads
// baseline. Reputation floats above baseline and decays back, so gating on it
// would report eligible during the float and flip back later.
func TestStrongholdAccessUsesBaselineNotReputation(t *testing.T) {
	// Baseline 9 with a high floating reputation must still be ineligible.
	s := snapWith(3, 9, "licensed", 0)
	st := s.Standings["pirates"]
	st.Reputation = 95
	s.Standings["pirates"] = st
	if got := capsByName(Evaluate(s))["stronghold_access"]; got.Eligible {
		t.Error("baseline 9 with reputation 95 must be ineligible")
	}

	if got := capsByName(Evaluate(snapWith(3, 10, "licensed", 0)))["stronghold_access"]; !got.Eligible {
		t.Errorf("baseline 10 must be eligible, reason=%q", got.BlockingReason)
	}
}

// TestFreightBlockedByDebt pins that outstanding debt blocks freight and says
// so, since debt is the thing that silently stops contract acceptance.
func TestFreightBlockedByDebt(t *testing.T) {
	got := capsByName(Evaluate(snapWith(3, 10, "licensed", 4200)))["freight"]
	if got.Eligible {
		t.Error("outstanding debt must block freight")
	}
	if got.BlockingReason == "" {
		t.Error("debt block must carry a reason")
	}
}

// TestUnknownCarrierIsIneligibleNotEligible pins the safe direction: an agent
// whose carrier profile has never been captured is NOT freight-eligible. The
// screening filter must not invent capability from missing data.
func TestUnknownCarrierIsIneligibleNotEligible(t *testing.T) {
	s := snapWith(3, 10, "", 0)
	if got := capsByName(Evaluate(s))["freight"]; got.Eligible {
		t.Error("uncaptured carrier profile must not read as freight-eligible")
	}
}

// TestEvaluateCoversEveryRegisteredRule pins that Evaluate emits one row per
// registered capability, so a new rule cannot silently produce no output.
func TestEvaluateCoversEveryRegisteredRule(t *testing.T) {
	caps := Evaluate(snapWith(3, 10, "licensed", 0))
	if len(caps) != len(Rules()) {
		t.Fatalf("Evaluate returned %d rows, want %d (one per rule)", len(caps), len(Rules()))
	}
}

// TestReplaceCapabilitiesIsAWholeSetSwap pins that a capability that stops
// being emitted disappears from the table.
func TestReplaceCapabilitiesIsAWholeSetSwap(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	first := []Capability{{Capability: "haul", Eligible: true}, {Capability: "freight", Eligible: false, BlockingReason: "debt"}}
	if err := st.ReplaceCapabilities(ctx, "abc123", first, now); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := st.ReplaceCapabilities(ctx, "abc123", first[:1], now.Add(time.Hour)); err != nil {
		t.Fatalf("second: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_capability WHERE player_id = ?`, "abc123").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_capability rows = %d, want 1", n)
	}
}

// capsByName indexes an Evaluate result for assertion.
func capsByName(caps []Capability) map[string]Capability {
	m := make(map[string]Capability, len(caps))
	for _, c := range caps {
		m[c.Capability] = c
	}

	return m
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/assets/ -run 'TestSmuggling|TestStronghold|TestFreight|TestUnknownCarrier|TestEvaluate|TestReplaceCapabilities' -v`
Expected: FAIL — `undefined: AgentSnapshot`, `undefined: Evaluate`, `undefined: Rules`.

- [ ] **Step 3: Add the table**

Append to `pkg/assets/schema.sql`:

```sql
-- Derived, never hand-written: materialised by pkg/assets rules on every
-- capture. A TABLE rather than a SQL view because the rules live in Go;
-- expressing them in SQL too would fork the definitions and let them drift.
--
-- This is a SCREENING FILTER, not a promise. The workers' own gates
-- (buildMissionCandidate, freightCandidate, haulGate) are per-pass and
-- live-priced and remain authoritative at accept time.
CREATE TABLE IF NOT EXISTS agent_capability (
    player_id       TEXT NOT NULL,
    capability      TEXT NOT NULL,
    eligible        INTEGER NOT NULL DEFAULT 0,
    blocking_reason TEXT NOT NULL DEFAULT '',
    as_of           TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, capability)
);
CREATE INDEX IF NOT EXISTS idx_agent_capability_lookup ON agent_capability(capability, eligible);
```

- [ ] **Step 4: Write the registry**

Create `pkg/assets/capability.go`:

```go
package assets

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Thresholds mirrored from pkg/worker. They are redeclared rather than
// imported because pkg/worker will import pkg/assets, and the reverse edge
// would be a cycle. The divergence is deliberate and documented in the spec:
// agent_capability is a SCREENING FILTER and the worker's own gate stays
// authoritative at accept time.
const (
	skillSmuggling  = "smuggling"          // pkg/worker/mission_select.go
	pirateFactionID = "pirates"             // pkg/worker/mission_standing.go
	smugglingLevel3 = 3                     // pkg/worker: smugglingXPExemptLevel
	strongholdBase  = 10                    // pkg/worker: smugglingUnlocked baseline
	haulMinCredits  = 20000                 // buying power for an arbitrage leg
)

// pkg/worker's freightPackageFootprint (100) is deliberately NOT mirrored yet:
// the v1 freight rule has no ship-class capacity to compare it against, and an
// unused constant fails the linter. It comes back with the catalog lookup in
// follow-on item 3.

// AgentSnapshot is everything the rules see. CarrierKnown distinguishes "no
// debt" from "never captured" — missing data must never read as capability.
type AgentSnapshot struct {
	Profile      Profile
	Skills       map[string]SkillRow
	Standings    map[string]StandingRow
	Carrier      Carrier
	CarrierKnown bool
	Hulls        []Hull
}

// activeHull returns the agent's currently flown hull.
func (s AgentSnapshot) activeHull() (Hull, bool) {
	for _, h := range s.Hulls {
		if h.IsActive {
			return h, true
		}
	}

	return Hull{}, false
}

// Capability is one derived eligibility verdict.
type Capability struct {
	Capability     string
	Eligible       bool
	BlockingReason string
}

// Rules is the eligibility registry. Adding a capability is adding a function
// and a key: no schema change, no migration. This is the layer that grows as
// needs change, wrapping tables that stay pinned to the wire format.
func Rules() map[string]func(AgentSnapshot) (bool, string) {
	return map[string]func(AgentSnapshot) (bool, string){
		"haul": func(s AgentSnapshot) (bool, string) {
			if _, ok := s.activeHull(); !ok {
				return false, "no active hull captured"
			}
			if s.Profile.Credits < haulMinCredits {
				return false, fmt.Sprintf("credits %.0f < %d", s.Profile.Credits, haulMinCredits)
			}

			return true, ""
		},
		"freight": func(s AgentSnapshot) (bool, string) {
			if !s.CarrierKnown {
				return false, "carrier profile not captured"
			}
			if s.Carrier.DebtBlocksAcceptance || s.Carrier.OutstandingDebt > 0 {
				return false, fmt.Sprintf("outstanding_debt %d", s.Carrier.OutstandingDebt)
			}
			if _, ok := s.activeHull(); !ok {
				return false, "no active hull captured"
			}
			// v1 does NOT check cargo capacity: OwnedShip carries CargoUsed,
			// not capacity, and resolving capacity needs a ship-class catalog
			// lookup (follow-on 3). Until then this rule over-reports freight
			// eligibility for tiny hulls. That is the documented weakness of
			// the screening filter; freightCandidate still refuses them.

			return true, ""
		},
		"mission_delivery": func(s AgentSnapshot) (bool, string) {
			if _, ok := s.activeHull(); !ok {
				return false, "no active hull captured"
			}

			return true, ""
		},
		skillSmuggling: func(s AgentSnapshot) (bool, string) {
			lvl := s.Skills[skillSmuggling].Level
			if lvl < smugglingLevel3 {
				return false, fmt.Sprintf("level %d, needs %d", lvl, smugglingLevel3)
			}

			return true, ""
		},
		"stronghold_access": func(s AgentSnapshot) (bool, string) {
			base := s.Standings[pirateFactionID].Baseline
			if base < strongholdBase {
				return false, fmt.Sprintf("baseline %d, needs %d", base, strongholdBase)
			}

			return true, ""
		},
	}
}

// Evaluate runs every registered rule, returning one verdict per capability in
// a deterministic order.
func Evaluate(s AgentSnapshot) []Capability {
	rules := Rules()
	names := make([]string, 0, len(rules))
	for name := range rules {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Capability, 0, len(names))
	for _, name := range names {
		ok, reason := rules[name](s)
		out = append(out, Capability{Capability: name, Eligible: ok, BlockingReason: reason})
	}

	return out
}

// ReplaceCapabilities swaps in the agent's full verdict set.
func (s *Store) ReplaceCapabilities(ctx context.Context, playerID string, rows []Capability, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_capability WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, c := range rows {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO agent_capability (player_id, capability, eligible, blocking_reason, as_of)
				 VALUES (?,?,?,?,?)`,
				playerID, c.Capability, c.Eligible, c.BlockingReason, ts); err != nil {
				return fmt.Errorf("assets: insert capability %s/%s: %w", playerID, c.Capability, err)
			}
		}

		return nil
	})
}
```

Note: `packageCargo` is declared but unused in the v1 `freight` rule. `golangci-lint` will flag an unused constant. Either use it in the rule (compare against a real capacity once the catalog lookup exists) or delete the constant for now — **delete it** and add it back with the catalog lookup in the follow-on. Same for any other constant the linter flags.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/assets/ -v`
Expected: PASS.

- [ ] **Step 6: Lint and commit**

```bash
go build ./... && golangci-lint run ./pkg/assets/
git add pkg/assets/schema.sql pkg/assets/capability.go pkg/assets/capability_test.go
git commit -m "feat(assets): eligibility registry with blocking reasons

A table materialised from Go rules, not a SQL view: expressing the predicates
in SQL as well would fork the definitions and let them drift. Missing data
reads as INELIGIBLE so the filter never invents capability.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: `CaptureProfile` orchestration

**Files:**
- Create: `pkg/assets/capture.go`
- Test: `pkg/assets/capture_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–6, plus `game.GameClient`.
- Produces: `assets.CaptureProfile(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error`.

- [ ] **Step 1: Write the failing test**

Create `pkg/assets/capture_test.go`:

```go
package assets

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// fakeClient implements only the GameClient methods CaptureProfile uses.
// Embedding game.GameClient means the unused methods panic if ever called,
// which is the behaviour we want from a test double.
type fakeClient struct {
	game.GameClient
	state       *game.State
	statusErr   error
	shippingErr error
	shipsErr    error
	raw         map[string][]byte
	calls       []string
}

func (f *fakeClient) GetStatus(context.Context) error {
	f.calls = append(f.calls, "get_status")

	return f.statusErr
}

func (f *fakeClient) ShippingProfile(context.Context) error {
	f.calls = append(f.calls, "shipping_profile")

	return f.shippingErr
}

func (f *fakeClient) ListShips(context.Context) error {
	f.calls = append(f.calls, "list_ships")

	return f.shipsErr
}

func (f *fakeClient) GetState() *game.State { return f.state }

func (f *fakeClient) GetRawJSON(key string) []byte { return f.raw[key] }

func newFakeClient() *fakeClient {
	st := &game.State{}
	st.Player.ID = "abc123"
	st.Player.Username = "Arthur 'Artificer' Artis"
	st.Player.Credits = 15135
	st.Player.Empire = "haven"
	st.Player.HomeBase = "grand_exchange_station"
	st.Player.Skills = map[string]game.Skill{"smuggling": {Level: 3, XP: 12}}
	st.Player.Standings = map[string]game.EmpireStanding{
		"pirates": {Reputation: 42, Baseline: 10},
	}

	return &fakeClient{
		state: st,
		raw: map[string][]byte{
			"owned_ships": []byte(`{"action":"list_ships","ships":[
				{"ship_id":"s1","class_id":"reclaim","is_active":true,
				 "hull":"180/180","fuel":"150/200","location_base_id":"grand_exchange_station"}]}`),
			"shipping_profile": []byte(`{"action":"profile",
				"profile":{"tier":"licensed","successful_deliveries":6},
				"tier_progress":{"current_tier":"licensed","next_tier":"trusted"}}`),
		},
	}
}

// TestCaptureProfileWritesEverySource pins the happy path end to end.
func TestCaptureProfileWritesEverySource(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c := newFakeClient()

	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("CaptureProfile: %v", err)
	}

	for _, q := range []struct {
		name string
		sql  string
	}{
		{"agents", `SELECT COUNT(*) FROM agents WHERE player_id='abc123' AND agent_id='engineer-3'`},
		{"agent_profile", `SELECT COUNT(*) FROM agent_profile WHERE player_id='abc123'`},
		{"agent_skills", `SELECT COUNT(*) FROM agent_skills WHERE player_id='abc123'`},
		{"agent_standings", `SELECT COUNT(*) FROM agent_standings WHERE player_id='abc123'`},
		{"agent_carrier", `SELECT COUNT(*) FROM agent_carrier WHERE player_id='abc123'`},
		{"agent_hulls", `SELECT COUNT(*) FROM agent_hulls WHERE player_id='abc123'`},
	} {
		var n int
		if err := st.DB().QueryRowContext(ctx, q.sql).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q.name, err)
		}
		if n == 0 {
			t.Errorf("%s: no rows written", q.name)
		}
	}

	// Capabilities derived from the above: smuggling L3 and pirate baseline 10
	// are both eligible.
	var eligible int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT eligible FROM agent_capability WHERE player_id='abc123' AND capability='smuggling'`).
		Scan(&eligible); err != nil {
		t.Fatalf("capability: %v", err)
	}
	if eligible != 1 {
		t.Error("smuggling L3 must be eligible")
	}
}

// TestCaptureProfileSurvivesShippingFailure pins partial-capture honesty: when
// the shipping profile call fails, the profile is still written and the
// carrier row is simply absent — not written with zeroes, which would read as
// a debt-free probationary carrier.
func TestCaptureProfileSurvivesShippingFailure(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c := newFakeClient()
	c.shippingErr = errors.New("boom")
	delete(c.raw, "shipping_profile")

	if err := CaptureProfile(ctx, c, st, "engineer-3", now); err != nil {
		t.Fatalf("CaptureProfile must not fail on a shipping error: %v", err)
	}

	var profiles, carriers int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_profile WHERE player_id='abc123'`).Scan(&profiles); err != nil {
		t.Fatalf("profiles: %v", err)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_carrier WHERE player_id='abc123'`).Scan(&carriers); err != nil {
		t.Fatalf("carriers: %v", err)
	}
	if profiles != 1 {
		t.Errorf("agent_profile rows = %d, want 1", profiles)
	}
	if carriers != 0 {
		t.Errorf("agent_carrier rows = %d, want 0 (uncaptured, not zeroed)", carriers)
	}
}

// TestCaptureProfileCallsGetStatusExplicitly pins that we re-read status
// rather than trusting ambient state. Standings ride ONLY on a full player
// payload (pkg/game/client.go preserves the old map on a partial one), so
// skipping the call can silently persist arbitrarily stale standings.
func TestCaptureProfileCallsGetStatusExplicitly(t *testing.T) {
	st := openTestStore(t)
	c := newFakeClient()
	if err := CaptureProfile(context.Background(), c, st, "engineer-3",
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CaptureProfile: %v", err)
	}
	found := false
	for _, call := range c.calls {
		if call == "get_status" {
			found = true
		}
	}
	if !found {
		t.Error("CaptureProfile must call GetStatus explicitly")
	}
}

// TestCaptureProfileNilStoreIsANoOp pins that an unconfigured store disables
// capture rather than erroring — assets must never be a new way for a worker
// pass to fail.
func TestCaptureProfileNilStoreIsANoOp(t *testing.T) {
	if err := CaptureProfile(context.Background(), newFakeClient(), nil, "engineer-3",
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Errorf("nil store must be a no-op, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/assets/ -run TestCaptureProfile -v`
Expected: FAIL — `undefined: CaptureProfile`.

- [ ] **Step 3: Write the orchestration**

Create `pkg/assets/capture.go`:

```go
package assets

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// CaptureProfile refreshes one agent's identity, profile, skills, standings,
// carrier standing and hulls, then materialises its capabilities.
//
// Every source degrades independently: a failed call means that table keeps
// its previous captured_at and is simply not refreshed. Nothing here returns
// an error for a source failure, because asset capture must never become a new
// way for a worker pass to fail (the same rule pkg/worker/mission.go states for
// freight). Only a store-write failure propagates.
func CaptureProfile(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}

	// GetStatus must be called explicitly rather than trusting ambient state:
	// Standings ride only on a FULL player payload, and the client preserves
	// the previous map when a partial one arrives. Skipping this call would
	// persist arbitrarily stale standings under a fresh timestamp.
	_ = client.GetStatus(ctx)

	state := client.GetState()
	if state == nil || state.Player.ID == "" {
		return nil // nothing identifiable to record
	}
	p := state.Player
	playerID := p.ID

	if err := st.UpsertIdentity(ctx, Identity{
		PlayerID: playerID, AgentID: agentID, Username: p.Username,
	}, now); err != nil {
		return err
	}

	if err := st.UpsertProfile(ctx, Profile{
		PlayerID: playerID, Username: p.Username, Empire: p.Empire,
		Credits: p.Credits, HomeBase: p.HomeBase, DockedAtBase: p.DockedAtBase,
		CurrentSystem: state.CurrentSystem, CurrentPOI: state.CurrentPOI,
		ActiveShipID: p.CurrentShipID, FactionID: p.FactionID,
		FactionRank: p.FactionRank, Experience: p.Experience, CapturedAt: now,
	}); err != nil {
		return err
	}

	skills := make([]SkillRow, 0, len(p.Skills))
	skillMap := make(map[string]SkillRow, len(p.Skills))
	for name, sk := range p.Skills {
		row := SkillRow{Skill: name, Level: sk.Level, XP: sk.XP}
		skills = append(skills, row)
		skillMap[name] = row
	}
	if err := st.ReplaceSkills(ctx, playerID, skills, now); err != nil {
		return err
	}

	standings := make([]StandingRow, 0, len(p.Standings))
	standingMap := make(map[string]StandingRow, len(p.Standings))
	for name, sd := range p.Standings {
		row := StandingRow{
			Faction: name, Reputation: sd.Reputation, Baseline: sd.Baseline,
			OutstandingBounty: sd.OutstandingBounty, JailedUntil: sd.JailedUntil,
		}
		standings = append(standings, row)
		standingMap[name] = row
	}
	if err := st.ReplaceStandings(ctx, playerID, standings, now); err != nil {
		return err
	}

	// Carrier: a failed call or an undecodable body leaves agent_carrier
	// untouched. Writing a zero row instead would read as a debt-free
	// probationary carrier, which is worse than no data.
	var (
		carrier      Carrier
		carrierKnown bool
	)
	if err := client.ShippingProfile(ctx); err == nil {
		if c, ok, derr := CarrierFrom(client.GetRawJSON("shipping_profile")); derr == nil && ok {
			carrier, carrierKnown = c, true
			if err := st.UpsertCarrier(ctx, playerID, c, now); err != nil {
				return err
			}
		}
	}

	var hulls []Hull
	if err := client.ListShips(ctx); err == nil {
		if hs, derr := HullsFrom(client.GetRawJSON("owned_ships")); derr == nil && len(hs) > 0 {
			hulls = hs
			if err := st.ReplaceHulls(ctx, playerID, hs, now); err != nil {
				return err
			}
		}
	}

	return st.ReplaceCapabilities(ctx, playerID, Evaluate(AgentSnapshot{
		Profile:      Profile{PlayerID: playerID, Credits: p.Credits},
		Skills:       skillMap,
		Standings:    standingMap,
		Carrier:      carrier,
		CarrierKnown: carrierKnown,
		Hulls:        hulls,
	}), now)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/assets/ -v`
Expected: PASS.

If `fakeClient` fails to satisfy `game.GameClient`, check the exact method signatures on the interface at `pkg/game/interface.go` — `GetStatus`, `ShippingProfile`, `ListShips`, `GetState`, `GetRawJSON` — and match them. Do not change the production code to fit the fake.

- [ ] **Step 5: Lint and commit**

```bash
go build ./... && golangci-lint run ./pkg/assets/
git add pkg/assets/capture.go pkg/assets/capture_test.go
git commit -m "feat(assets): CaptureProfile orchestration with partial-capture honesty

A failed source leaves its table untouched rather than writing zeroes: a zeroed
carrier row would read as a debt-free probationary carrier, which is worse than
no data. GetStatus is called explicitly because Standings ride only on a full
player payload.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Wire `capture_profile` into the worker and `play_as`

**Files:**
- Modify: `pkg/worker/dispatch.go`, `cmd/worker/main.go`, `cmd/tools/play_as/main.go`, `data/overmind/roles.yaml`
- Test: `pkg/worker/dispatch_test.go`

**Interfaces:**
- Consumes: `assets.CaptureProfile`, `assets.Open`, `assets.DefaultConfig`.
- Produces: `WorkerDispatch.Assets *assets.Store` field; the `capture_profile` command in the worker vocabulary and in `play_as`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/worker/dispatch_test.go`:

```go
// TestCaptureProfileIsSupported pins that capture_profile is in the curated
// worker vocabulary. roles_test.go enforces that every command named in
// data/overmind/roles.yaml appears in the supported map, so a schedule line
// without this entry fails the build.
func TestCaptureProfileIsSupported(t *testing.T) {
	d := &WorkerDispatch{}
	if !d.Supports("capture_profile") {
		t.Error("capture_profile must be in the supported command set")
	}
}

// TestCaptureProfileWithoutStoreIsANoOp pins that a worker launched without
// --assets-db-path runs the command harmlessly rather than erroring every
// scheduled pass.
func TestCaptureProfileWithoutStoreIsANoOp(t *testing.T) {
	d := &WorkerDispatch{Client: &fakeClient{state: &game.State{}}, Out: io.Discard}
	if err := d.Run(context.Background(), []string{"capture_profile"}); err != nil {
		t.Errorf("capture_profile without a store must be a no-op, got %v", err)
	}
}
```

`fakeClient` is the existing double declared at `pkg/worker/dispatch_test.go:19`
and constructed throughout that file as `&fakeClient{state: &game.State{}}`.
Reuse it — do not add a second fake.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestCaptureProfile -v`
Expected: FAIL — `capture_profile must be in the supported command set`.

- [ ] **Step 3: Add the dispatch field, case, and vocabulary entry**

In `pkg/worker/dispatch.go`, add to the `WorkerDispatch` struct after the `Market` field:

```go
	// Assets is the agent asset + capability ledger (data/assets.db). Nil
	// disables capture entirely: asset capture is strictly less important than
	// any paying activity and must never be a new way for a pass to fail.
	Assets *assets.Store
```

Add `"capture_profile": true,` to the `supported` map (same line group as `"capture_fuel"`).

Add the case to `Run`'s switch, next to `capture_fuel`:

```go
	case "capture_profile":
		// Nil store = capture disabled; not an error. See the Assets field.
		return assets.CaptureProfile(ctx, d.Client, d.Assets, d.AgentID, time.Now())
```

Add `"github.com/rsned/spacemolt/pkg/assets"` and `"time"` to the imports if not already present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestCaptureProfile -v`
Expected: PASS.

Then run the whole worker suite, which is slow: `go test ./pkg/worker/`
Expected: PASS (takes ~150s).

- [ ] **Step 5: Wire the worker binary**

In `cmd/worker/main.go`, following the existing `--market-db-path` flag exactly:

```go
	assetsDBPath := flag.String("assets-db-path", "",
		"path to the agent asset ledger (data/assets.db); empty disables asset capture")
```

After the market collector is opened, add:

```go
	var assetStore *assets.Store
	if *assetsDBPath != "" {
		cfg := assets.DefaultConfig()
		cfg.DBPath = *assetsDBPath
		var err error
		assetStore, err = assets.Open(cfg)
		if err != nil {
			// Non-fatal: a worker that cannot open the asset ledger must still
			// do its paying job.
			log.Printf("assets: open %s failed, capture disabled: %v", *assetsDBPath, err)
			assetStore = nil
		} else {
			defer func() { _ = assetStore.Close() }()
		}
	}
```

and set `Assets: assetStore` on the `WorkerDispatch` literal.

- [ ] **Step 6: Add the `play_as` command**

In `cmd/tools/play_as/main.go`, add a case next to the existing `update_all` case (~line 8529):

```go
	case "capture_profile":
		if globalAssets == nil {
			fmt.Println("capture_profile: no assets DB configured (use --assets-db-path)")

			return nil
		}

		return assets.CaptureProfile(ctx, client, globalAssets, globalAgentID, time.Now())
```

Add the help line next to the existing `update_all` entry (~line 9549):

```go
	fmt.Println("  capture_profile           - Capture this agent's profile, skills, standings, carrier tier and hulls")
```

Declare `var globalAssets *assets.Store` next to `globalMarketCollector` (`cmd/tools/play_as/main.go:75`), and open it in the same init block that assigns `globalMarketCollector` (`main.go:245`), using the same `--assets-db-path` flag name as the worker. `globalAgentID` is already set at `main.go:107`, so it is in scope for the capture call.

- [ ] **Step 7: Add the schedule lines**

In `data/overmind/roles.yaml`, add to the `missionrunner`, `hauler`, `craftsman` and `resident` role schedules:

```yaml
      - { every: hourly, command: "capture_profile" }
```

`hauler` and `assist` currently have no `schedule:` block — add one containing just this line for `hauler`. Leave `assist` alone: those five workers are pinned rescue agents whose capability never changes, and adding a call to them buys nothing.

- [ ] **Step 8: Verify the roles/vocabulary invariant**

Run: `go test ./pkg/worker/ -run TestRoles -v`
Expected: PASS — `roles_test.go` enforces that every command named in `roles.yaml` exists in `supported`. If it fails, the `supported` entry from Step 3 is missing or misspelled.

- [ ] **Step 9: Build, lint, commit**

```bash
go build ./... && golangci-lint run ./pkg/worker/ ./cmd/worker/ ./cmd/tools/play_as/
git add pkg/worker/dispatch.go pkg/worker/dispatch_test.go cmd/worker/main.go \
        cmd/tools/play_as/main.go data/overmind/roles.yaml
git commit -m "feat(assets): wire capture_profile into the worker and play_as

Workers capture their own profile because a central collector cannot: it needs
a session the fleet already holds, and contending for one causes reconnect
thrash. That is why daily-summary has been silent since 2026-07-02.

play_as gets the same command so the operator's interactive character — the
largest asset holder — reports through the same path.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

**Do not stage `data/agents/*/schedule.json`** — those are live-fleet churn.

---

### Task 9: Coverage query and the ovdash staleness panel

**Files:**
- Create: `pkg/assets/coverage.go`, `pkg/ovdash/assets.go`
- Modify: `pkg/ovdash/snapshot.go`
- Test: `pkg/assets/coverage_test.go`

**Interfaces:**
- Consumes: `*Store`, `openTestStore`.
- Produces: `assets.CoverageRow{Source string; Agents int; Oldest string; Stale int}`, `assets.Coverage(ctx context.Context, db *sql.DB, now time.Time, staleAfter time.Duration) ([]CoverageRow, error)`, `ovdash.LoadAssetCoverage(ctx context.Context, dbPath string, now time.Time, staleAfter time.Duration) ([]assets.CoverageRow, error)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/assets/coverage_test.go`:

```go
package assets

import (
	"context"
	"testing"
	"time"
)

// TestCoverageCountsStalePerSource pins the anti-rot surface. Every previous
// unsupervised capture job in this codebase died silently — daily-summary for
// 25 days, market-prune until the DB hit 62GB, the arbitrage scanner mimicking
// "no opportunities". Coverage is how this one gets noticed instead.
func TestCoverageCountsStalePerSource(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-10 * time.Minute)
	old := now.Add(-72 * time.Hour)

	if err := st.UpsertProfile(ctx, Profile{PlayerID: "fresh-1", CapturedAt: fresh}); err != nil {
		t.Fatalf("fresh profile: %v", err)
	}
	if err := st.UpsertProfile(ctx, Profile{PlayerID: "stale-1", CapturedAt: old}); err != nil {
		t.Fatalf("stale profile: %v", err)
	}
	if err := st.UpsertCarrier(ctx, "fresh-1", Carrier{Tier: "licensed"}, fresh); err != nil {
		t.Fatalf("carrier: %v", err)
	}

	rows, err := Coverage(ctx, st.DB(), now, 24*time.Hour)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	by := map[string]CoverageRow{}
	for _, r := range rows {
		by[r.Source] = r
	}

	if got := by["agent_profile"]; got.Agents != 2 || got.Stale != 1 {
		t.Errorf("agent_profile = %d agents / %d stale, want 2 / 1", got.Agents, got.Stale)
	}
	if got := by["agent_carrier"]; got.Agents != 1 || got.Stale != 0 {
		t.Errorf("agent_carrier = %d agents / %d stale, want 1 / 0", got.Agents, got.Stale)
	}
}

// TestCoverageOnEmptyDBIsNotAnError pins that a brand-new ledger reports zeroes
// rather than failing the dashboard that reads it.
func TestCoverageOnEmptyDBIsNotAnError(t *testing.T) {
	st := openTestStore(t)
	rows, err := Coverage(context.Background(), st.DB(),
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), 24*time.Hour)
	if err != nil {
		t.Fatalf("Coverage on empty DB: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("Coverage must report a row per source even when empty")
	}
	for _, r := range rows {
		if r.Agents != 0 || r.Stale != 0 {
			t.Errorf("%s: got %d agents / %d stale on an empty DB", r.Source, r.Agents, r.Stale)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/assets/ -run TestCoverage -v`
Expected: FAIL — `undefined: Coverage`, `undefined: CoverageRow`.

- [ ] **Step 3: Write the coverage query**

Create `pkg/assets/coverage.go`:

```go
package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CoverageRow is one source's freshness across the fleet.
type CoverageRow struct {
	Source string `json:"source"`
	Agents int    `json:"agents"`
	Oldest string `json:"oldest"`
	Stale  int    `json:"stale"`
}

// coverageSources are the tables Coverage reports on, in display order.
var coverageSources = []string{"agent_profile", "agent_carrier", "agent_hulls", "agent_skills"}

// Coverage reports how many agents each source knows about and how many of
// those are older than staleAfter.
//
// This exists because every previous unsupervised capture job here died
// silently. Capture rides the supervised worker schedule rather than a new
// daemon, and this query is how a stall becomes visible on a dashboard the
// operator already watches, instead of a cron whose silence means nothing.
func Coverage(ctx context.Context, db *sql.DB, now time.Time, staleAfter time.Duration) ([]CoverageRow, error) {
	if db == nil {
		return nil, nil
	}
	cutoff := rfc3339(now.Add(-staleAfter))
	out := make([]CoverageRow, 0, len(coverageSources))
	for _, table := range coverageSources {
		row := CoverageRow{Source: table}
		var oldest sql.NullString
		// #nosec G201 -- table comes from the fixed coverageSources list, never user input.
		q := fmt.Sprintf(`SELECT COUNT(DISTINCT player_id), MIN(captured_at),
			COALESCE(SUM(CASE WHEN captured_at < ? THEN 1 ELSE 0 END), 0) FROM %s`, table)
		if err := db.QueryRowContext(ctx, q, cutoff).Scan(&row.Agents, &oldest, &row.Stale); err != nil {
			return nil, fmt.Errorf("assets: coverage %s: %w", table, err)
		}
		row.Oldest = oldest.String
		out = append(out, row)
	}

	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/assets/ -v`
Expected: PASS.

Note: `agent_skills` and `agent_hulls` count `DISTINCT player_id` because they hold many rows per agent. Verify the `agent_profile` case still reports 2, not 2 × row count.

- [ ] **Step 5: Add the ovdash panel**

Create `pkg/ovdash/assets.go`, mirroring `pkg/ovdash/accounting.go:44`'s read-only open:

```go
package ovdash

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

// LoadAssetCoverage reads asset-ledger freshness from the assets DB
// (read-only). A missing or unreadable DB yields no rows and no error: the
// dashboard must render whether or not asset capture is deployed.
func LoadAssetCoverage(ctx context.Context, dbPath string, now time.Time, staleAfter time.Duration) ([]assets.CoverageRow, error) {
	if dbPath == "" {
		return nil, nil
	}
	db, err := sql.Open(sqliteDriver, "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open assets db: %w", err)
	}
	defer db.Close() //nolint:errcheck

	rows, err := assets.Coverage(ctx, db, now, staleAfter)
	if err != nil {
		// An absent ledger is the normal pre-deploy state, not a dashboard
		// failure. Report empty and let the panel show "not deployed".
		return nil, nil
	}

	return rows, nil
}
```

In `pkg/ovdash/snapshot.go`, add to the `Snapshot` struct:

```go
	// AssetCoverage is per-source freshness of the agent asset ledger. Empty
	// when the ledger is not deployed.
	AssetCoverage []assets.CoverageRow `json:"asset_coverage,omitempty"`
```

Wire it in `cmd/overmind-dashboard/main.go`: add `AssetsPath` to the `serverConfig` struct (`main.go:30`, alongside `KBPath, MarketPath, StatusDir, DistDir`), add an `--assets-db` flag and pass it at `main.go:181`, then call `LoadAssetCoverage` next to the existing `ovdash.LoadEarnings` call at `main.go:80` with a `staleAfter` of 24h, assigning the result to `snapshot.AssetCoverage`.

- [ ] **Step 6: Build, lint, commit**

```bash
go build ./... && go test ./pkg/assets/ ./pkg/ovdash/ && golangci-lint run ./pkg/assets/ ./pkg/ovdash/
git add pkg/assets/coverage.go pkg/assets/coverage_test.go pkg/ovdash/assets.go pkg/ovdash/snapshot.go
git commit -m "feat(assets): coverage query and ovdash staleness panel

No new daemon: capture rides the supervised worker schedule, and staleness
becomes a number on a dashboard the operator already watches. Every previous
unsupervised capture job in this repo died silently.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Rollout

Capture is inert until a worker is launched with `--assets-db-path`. Roll it out the way freight was:

1. **Canary one worker.** Add `--assets-db-path data/assets.db` to a single mission-learn worker, let one hourly boundary pass, then confirm rows:
   ```bash
   sqlite3 data/assets.db "SELECT agent_id, username, credits FROM agents
     JOIN agent_profile USING(player_id);"
   ```
2. **Confirm the eligibility output is sane** against something already known — e.g. that the agents you know are still probationary show `freight` with a non-empty `blocking_reason`, and that the smuggling canaries at L3+ show `smuggling` eligible.
3. **Roll to one fleet**, then the rest. Relaunching a fleet requires `kill -TERM` on the overmind, waiting for workers to exit, `rm -f` the sock, and relaunching with `--stagger 10s`. A `SIGUSR1` drain is **not** a deploy — it is time-bounded and aborts if workers stay busy. Verify the rollout by comparing worker process start times against `bin/worker`'s mtime; worker count and `overmind_commit` both lie.
4. **Watch the ovdash coverage panel** for a source that stops advancing.

## Follow-on work

1. `agent_storage` / `agent_storage_items` — the `hint` parser and per-base sweep (spec slices 5–6), plus the faction tables. Second plan.
2. `spread: true` scheduler flag so tier-2 tasks jitter instead of firing on the boundary together.
3. Ship-class capacity lookup, so the `freight` and `haul` rules can compare real cargo capacity against `freightPackageFootprint` instead of merely requiring an active hull. This is the largest known weakness of the v1 rules.
4. Module/fitting capture, to answer "can this agent be refitted for the role".
5. Faction ship garages, once one is built.
6. Assignment: overmind-held capability flags reading `agent_capability`.
