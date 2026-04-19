# System `last_visited_tick` — Distinguishing Lawless from Unexplored

**Date:** 2026-04-18
**Status:** Approved design, pending implementation plan

## Problem

The `systems` table stores an integer `police_level` (0–3) that doubles as the
security status: `0 = Lawless`, `1 = low`, `2 = medium`, `3 = high`. The value
is populated in two very different code paths:

1. **`get_map` import** — returns only `id`, `name`, and `connections[]` for
   every system in the galaxy. Since no security info is included, the row is
   inserted with Go's zero value for `police_level`, i.e. `0`.
2. **Explorer visit** — when a client actually travels to a system, the real
   observed police level (which may also be `0`) is written back to the same
   column.

After both paths run, a row with `police_level = 0` is ambiguous: it may mean
"we visited and confirmed this is a lawless system" or "we have never been
there and the value is just the import default." Consumers (spacemolt,
spacemolt-kb's `generate-items-kb`, the frontend galaxy map) cannot tell the
difference and incorrectly label unexplored systems as Lawless.

## Goal

Make "have we actually visited this system?" explicit in the schema, so that
`police_level = 0` can be interpreted correctly by every consumer — including
the sibling **spacemolt-kb** repo, which shares this SQLite database.

## Non-Goals

- Changing the type or range of `police_level` (stays `int`, 0–3).
- Making `police_level` nullable.
- Replacing or repurposing the existing `last_updated_tick` column, which
  tracks arbitrary writes (including map imports) and is used for freshness
  logic. `last_visited_tick` is strictly about "did a player/agent go there".
- Tracking *multiple* visits, visit history, or per-POI visit state (POIs
  already track their own `last_updated_tick`).

## Design

### Schema change

Add one column to the `systems` table:

```sql
ALTER TABLE systems ADD COLUMN last_visited_tick INTEGER NOT NULL DEFAULT 0;
```

Semantics:

| Value | Meaning |
|------:|---------|
| `0`   | Never visited. `police_level` is **not trustworthy** — treat security as `"unknown"`. |
| `> 0` | Last game tick at which the system was visited and its state refreshed from a real observation. `police_level` reflects ground truth (including `0 = Lawless`). |

No index is added at this time. If query plans show sequential scans becoming
a problem for either repo, an index on `last_visited_tick` can be added later.

### Backfill

In the same migration, backfill `last_visited_tick` for every system that has
at least one scanned POI resource — those are systems we can prove were
visited, because a POI resource row only gets written after travelling to the
POI and scanning it:

```sql
UPDATE systems
SET last_visited_tick = (
    SELECT MAX(pr.last_updated_tick)
    FROM poi_resources pr
    JOIN pois p ON pr.poi_id = p.id
    WHERE p.system_id = systems.id
)
WHERE EXISTS (
    SELECT 1
    FROM pois p
    JOIN poi_resources pr ON pr.poi_id = p.id
    WHERE p.system_id = systems.id
);
```

Systems without any `poi_resources` rows keep `last_visited_tick = 0` and are
correctly flagged as unexplored. Some systems that were visited but had no
scannable resources (e.g., systems containing only stations) will also be
left at `0`; they will be upgraded naturally on the next visit once the write
path is in place. This is an acceptable trade-off — the alternative is to
invent a heuristic for "visited without resource scans" that would be noisier
than the zero-default.

### Go types

In `pkg/knowledge/memory.go`, extend `System`:

```go
type System struct {
    // ... existing fields ...
    LastUpdatedTick int64
    LastVisitedTick int64 // 0 = never visited; PoliceLevel is untrusted when 0
}

// Explored reports whether the system's PoliceLevel reflects a real
// observation rather than a map-import default.
func (s System) Explored() bool { return s.LastVisitedTick > 0 }
```

All `SELECT` and `INSERT`/`UPSERT` statements in `pkg/knowledge/sqlite.go`
that touch the `systems` table gain the new column.

### Write path

| Code path | Behavior |
|-----------|----------|
| `UpsertSystemFromMap` (map import) | **Does not touch** `last_visited_tick`. Preserves existing value via `excluded.last_visited_tick`-style guard or by simply omitting the column from the `INSERT ... ON CONFLICT DO UPDATE SET` clause. |
| `RememberSystem` / explorer-driven updates | Sets `last_visited_tick` to the current game tick (same value used for `LastUpdatedTick` on a real visit). |

The exact call sites to update will be enumerated in the implementation plan;
at minimum, anywhere `PoliceLevel` is written from observed game state needs
to also set `LastVisitedTick`.

### `SecurityStatus` derivation

Wherever `SecurityStatus` is derived from `PoliceLevel` (currently a string
field on `System` with values like `"high_sec"`, `"low_sec"`, `"null_sec"`),
add an `"unknown"` case that is returned when `!Explored()` regardless of the
numeric police level. The human-readable equivalent is `"Unknown"`.

### spacemolt-kb integration

`spacemolt-kb/cmd/generate-items-kb` (and any other generator that reads the
shared DB) must be updated to:

- Select `last_visited_tick` alongside `police_level` when reading systems.
- Render security as `"Unknown"` (or omit the line entirely, at the
  implementer's discretion) when `last_visited_tick = 0`.

This change lives in the sibling repo and will be tracked as a follow-on
task; the spacemolt migration lands first so the column exists for
spacemolt-kb to consume.

### Frontend

- `frontend/src/types/game.ts`: extend `PoliceLevel` string-union type with
  `'unknown'`, and add `lastVisitedTick: number` to the system shape.
- WS payloads that currently ship `police_level` for a system must also ship
  `last_visited_tick` so the client can make the same determination without
  round-tripping.
- System cards, galaxy map, and `useSystemDetails` render `"?"` or
  `"Unexplored"` instead of `"Lawless"` when `last_visited_tick === 0`.

## Migration plan

1. Add migration file under `scripts/sql/migrations/` (numbering per existing
   convention) containing the `ALTER TABLE` and backfill `UPDATE` shown above.
2. Register the migration in `pkg/knowledge/sqlite_migrations.go`.
3. Update the Go `System` struct, SQL statements, and `SecurityStatus`
   derivation in the same PR.
4. Update write-path call sites so explorer observations set
   `last_visited_tick`.
5. Update WS payloads and frontend types/renderers.
6. Ship a parallel spacemolt-kb change that reads `last_visited_tick` and
   renders "Unknown" for unexplored systems.

## Testing

- Unit test around `System.Explored()` and `SecurityStatus` derivation: zero
  tick → `"unknown"`; non-zero tick with `PoliceLevel=0` → `"null_sec"` /
  `"Lawless"`.
- Migration test: seed a DB with pre-migration schema containing (a) a system
  with `poi_resources`, (b) a system with only a stub row, (c) a system with
  a POI but no resources. Run the migration. Assert `last_visited_tick`
  equals the max resource tick for (a), `0` for (b) and (c).
- Write-path test: `UpsertSystemFromMap` on a visited system does not clobber
  `last_visited_tick`; a subsequent `RememberSystem` call updates it.

## Open questions

None at time of writing. Indexing deferred until a performance problem is
observed.
