# Mission Catalog Design

**Date:** 2026-04-13
**Status:** Approved

## Goal

Persist a catalog of distinct mission templates seen at mission boards across
all stations. Detect when a known template's fields have changed since last
sighting and surface warnings with a field-level diff. Provide a `play_as`
command (`update_missions`) to capture the catalog on demand and wire it into
`update_all`.

## Background

- `get_missions` returns a list of `MissionBoardEntry` objects for the current
  base (see `pkg/game/serverapi/types.go:988`). Each entry has a stable
  `template_id` for hand-authored missions and a per-instance `mission_id`.
  For hand-authored templates the two are equal; for procedurally-generated
  missions (trade runs, `faction_<hex>` missions) the `template_id` field is
  empty.
- Once a mission is accepted, the `mission_id` changes to a hex instance id
  while the `template_id` keeps the original identifier — so the catalog key
  is `template_id` (equivalently `mission_id` for unaccepted board entries).
- A `mission_templates` / `mission_objectives` schema already exists in
  migration 13 (`pkg/knowledge/sqlite_migrations.go:611`) but is empty
  (`select count(*) from mission_templates` → 0) and no code writes to it.
  The existing shape is missing several fields present on
  `MissionBoardEntry` — so migration 15 drops and recreates it rather than
  layering ALTERs.
- The raw `get_missions` JSON is already cached to
  `data/game-api/YYYYMMDD/get_missions.json` by the client's response
  handler (`pkg/game/client.go:2984`).

## Non-goals

- Persisting per-instance accepted/completed missions (handled elsewhere).
- Catalog entries for procedural missions (trade runs, faction UUID missions).
  These are skipped in this pass; revisit later if useful.
- Any frontend/observer UI for the change warnings. Warnings are log-only
  (stderr) plus the summary printed by `play_as update_missions`.

## Schema — migration 15

Drop and recreate `mission_templates` and `mission_objectives`; add a new
`mission_template_locations` child table.

```sql
-- Drop existing (empty) tables
DROP TABLE IF EXISTS mission_objectives;
DROP TABLE IF EXISTS mission_templates;

-- Mission templates (hand-authored missions seen on mission boards).
-- Primary key is the stable template_id (== mission_id for unaccepted entries).
CREATE TABLE mission_templates (
    id                  TEXT PRIMARY KEY,
    title               TEXT NOT NULL,
    description         TEXT,
    type                TEXT,
    difficulty          INTEGER DEFAULT 0,

    giver_name          TEXT,
    giver_title         TEXT,

    faction_id          TEXT,
    faction_name        TEXT,

    dialog_offer        TEXT,
    dialog_accept       TEXT,
    dialog_decline      TEXT,
    dialog_complete     TEXT,

    chain_next          TEXT,
    repeatable          INTEGER DEFAULT 0,
    expires_in_ticks    INTEGER DEFAULT 0,

    rewards_credits     INTEGER DEFAULT 0,
    rewards_skill_xp    TEXT DEFAULT '{}',   -- JSON object: {skill: xp}
    rewards_items       TEXT DEFAULT '{}',   -- JSON object: {item_id: qty}

    requirements        TEXT DEFAULT '{}',   -- JSON blob
    required_modules    TEXT DEFAULT '[]',   -- JSON array of module ids
    provided_items      TEXT DEFAULT '{}',   -- JSON object: {item_id: qty}

    first_seen_tick     INTEGER DEFAULT 0,
    last_seen_tick      INTEGER DEFAULT 0,
    first_seen_at       TEXT,                -- RFC3339 timestamp
    last_seen_at        TEXT                 -- RFC3339 timestamp
);

-- Objectives for each mission template, in declared order.
CREATE TABLE mission_objectives (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    mission_id      TEXT NOT NULL,
    sort_order      INTEGER NOT NULL DEFAULT 0,
    type            TEXT NOT NULL,
    description     TEXT,
    item_id         TEXT,
    quantity        INTEGER DEFAULT 0,
    system_id       TEXT,
    system_name     TEXT,
    target_base_id  TEXT,
    target_base_name TEXT,
    FOREIGN KEY (mission_id) REFERENCES mission_templates(id) ON DELETE CASCADE
);

-- One row per (mission_id, base_id) sighting.
CREATE TABLE mission_template_locations (
    mission_id       TEXT NOT NULL,
    base_id          TEXT NOT NULL,
    system_id        TEXT,
    first_seen_tick  INTEGER DEFAULT 0,
    last_seen_tick   INTEGER DEFAULT 0,
    first_seen_at    TEXT,
    last_seen_at     TEXT,
    PRIMARY KEY (mission_id, base_id),
    FOREIGN KEY (mission_id) REFERENCES mission_templates(id) ON DELETE CASCADE
);

CREATE INDEX idx_mission_templates_type ON mission_templates(type);
CREATE INDEX idx_mission_templates_faction ON mission_templates(faction_id);
CREATE INDEX idx_mission_objectives_mission ON mission_objectives(mission_id);
CREATE INDEX idx_mission_locations_base ON mission_template_locations(base_id);
```

## Knowledge Base API

Added to `pkg/knowledge/base.go` `Base` interface, implemented by both
`SQLiteKB` and `MemoryKB`:

```go
// MissionFieldDiff records one field whose value changed between the stored
// catalog row and the newly-observed entry.
type MissionFieldDiff struct {
    Field    string
    OldValue string
    NewValue string
}

// MissionUpsertResult summarizes the outcome of UpsertMissionTemplate.
type MissionUpsertResult struct {
    Inserted bool                // true if this was a brand new template_id
    Diffs    []MissionFieldDiff  // non-empty iff an existing row had different values
}

// UpsertMissionTemplate inserts or updates a mission template in the catalog.
// If the template already exists and any catalog field differs, the row is
// updated and the changed fields are returned as Diffs. The sighting is also
// recorded in mission_template_locations.
//
// Callers must skip entries with empty TemplateID before invoking this method;
// procedural missions are not stored in the catalog.
UpsertMissionTemplate(
    ctx context.Context,
    entry serverapi.MissionBoardEntry,
    baseID, systemID string,
    tick int64,
) (*MissionUpsertResult, error)
```

### Fields compared for diffing

All scalar columns on `mission_templates` except `first_seen_*` and
`last_seen_*`. The serialized JSON blobs (`rewards_skill_xp`,
`rewards_items`, `requirements`, `required_modules`, `provided_items`) are
compared as their canonical JSON strings. Objectives are compared as a whole:
if the list differs in length or any field of any objective differs, a single
diff entry `{Field: "objectives", OldValue: <old JSON>, NewValue: <new JSON>}`
is emitted and the objectives rows are replaced. Location rows are always
upserted and never produce diffs.

### SQLite implementation

- Transaction per upsert.
- SELECT the existing row (+ objectives ordered by sort_order). If absent,
  INSERT new row, INSERT objectives, INSERT location, return
  `{Inserted: true}`.
- If present, compute diffs. If non-empty, UPDATE the template row and
  DELETE+INSERT the objectives. Always upsert the location row
  (update `last_seen_*`, set `first_seen_*` on insert). Return diffs.

### Memory implementation

Mirrors the SQLite logic against an in-memory map keyed by template id. Used
by tests and by agents running without a SQLite backend.

## Wiring: `pkg/game/client.go`

In the existing `get_missions` handling path, after the raw JSON has been
cached, unmarshal `resp.Payload` into `serverapi.GetMissionsResponse` and,
for each entry with non-empty `TemplateID`, call
`c.knowledge.UpsertMissionTemplate` with `resp.Payload["base_id"]` and the
current system id, using the client's current tick.

On any result with non-empty `Diffs`, log a warning via the client's logger:

```
mission template %s changed at base %s: field=%s old=%q new=%q
```

One log line per field diff. Procedural entries (empty `TemplateID`) are
silently skipped (counted only in a debug log).

The client already holds a knowledge base reference; if this particular
client build has no KB configured, the upsert call is skipped.

## `play_as` command: `update_missions`

New function in `cmd/tools/play_as/kb_update.go`:

```go
func kbUpdateMissions(client game.GameClient, ctx context.Context) error
```

Behavior:

1. Require `globalKB != nil` (else return "knowledge base not configured").
2. Require the player is docked; otherwise print
   `(Not docked — skipping missions update)` and return nil (consistent
   with how `kbUpdateAll` treats station-scoped updates).
3. Call `client.GetMissions(ctx)` and unmarshal the cached response.
4. Iterate the `Missions` slice:
   - If `TemplateID == ""`, increment `skipped` counter, continue.
   - Else call `globalKB.UpsertMissionTemplate(...)`.
   - Tally `inserted`, `unchanged`, `changed` from the result.
   - For each changed entry, print the diff list to stdout and emit a
     warning to stderr.
5. Print summary line:
   `update_missions: N new, M unchanged, K changed, P procedural skipped`

Dispatcher update in `cmd/tools/play_as/main.go:3434`:

```go
case "update_missions":
    return kbUpdateMissions(client, ctx)
```

Wire into `kbUpdateAll` inside the `state.Doc` branch, after
`kbUpdateFacilities`:

```go
if err := kbUpdateMissions(client, ctx); err != nil {
    fmt.Printf("Warning: update_missions: %v\n", err)
}
```

Help text in `main.go:3849`:

```
  update_missions           - Save mission board templates to KB
```

## Testing

- **Unit — `pkg/knowledge/memory_mission_test.go`:**
  - Insert new template → `Inserted=true`, no diffs.
  - Re-insert identical entry → `Inserted=false`, no diffs.
  - Re-insert with a changed title → one diff entry for `title`.
  - Re-insert with an added objective → one diff entry for `objectives`.
  - Re-insert at a different base → same template, second location row.
- **Fixture — `pkg/knowledge/sqlite_mission_test.go`:**
  - Load `data/game-api/20260411/get_missions.json`, run UpsertMissionTemplate
    for each entry with a `template_id`, assert expected non-procedural count
    (hand-count from fixture), and that all locations point to
    `grand_exchange_station`.
- **`cmd/tools/play_as` — no dedicated test** (existing command handlers are
  not unit-tested); smoke-tested by running against a live game client.

## Open questions resolved

1. **Procedural missions:** skip for now (no catalog row).
2. **Warning surface:** stderr log + stdout summary in `play_as`. No
   frontend event.
3. **Extend existing table:** yes, via drop/recreate in migration 15 since
   the table is empty in production.
4. **Location multiplicity:** separate child table
   `mission_template_locations`, since SQLite has no repeated columns.
