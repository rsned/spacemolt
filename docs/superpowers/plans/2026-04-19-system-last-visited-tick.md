# System `last_visited_tick` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `last_visited_tick` column to the `systems` table so consumers can distinguish a confirmed-Lawless system (`police_level=0`, `last_visited_tick>0`) from an unexplored one (`police_level=0`, `last_visited_tick=0`).

**Architecture:** Additive schema change (one new column, default `0`) plus a same-migration backfill from `poi_resources`. The `systems` write path in `SQLiteKB.RememberSystem` (and `MemoryKB.RememberSystem`) starts writing the tick on real visits; `SQLiteKB.UpsertSystemFromMap` continues to leave the column alone. Read-side consumers (Go `System` struct, WS payloads, frontend) gain the field and treat `0` as "Unexplored".

**Tech Stack:** Go 1.24, SQLite (modernc.org/sqlite), React 19 + TypeScript, WebSocket protocol.

**Spec:** `docs/superpowers/specs/2026-04-18-system-last-visited-tick-design.md`

**Out of scope for this plan:** Changes to the sibling `spacemolt-kb` repo (generate-items-kb). That repo reads the shared DB and will be updated separately once this migration lands.

---

## File Structure

**Created:**
- (none)

**Modified — schema & Go:**
- `pkg/knowledge/sqlite_migrations.go` — add migration 3 with `ALTER TABLE` + backfill
- `pkg/knowledge/memory.go` — add `LastVisitedTick int64` to `System`; add `Explored()` helper; propagate through `MemoryKB.RememberSystem`
- `pkg/knowledge/sqlite.go` — read/write the new column in `RememberSystem`, `GetSystem`, `GetSystems`; leave `UpsertSystemFromMap` untouched for the new column
- `pkg/knowledge/sqlite_test.go` — new tests for backfill, write-path preservation, and `Explored()`
- `pkg/knowledge/initial_schema.sql` — add the column to the baseline so fresh DBs get it via migration 1 too (belt-and-suspenders; migration 3 is idempotent-guarded)
- `cmd/tools/play_as/kb_update.go` — set `LastVisitedTick` on visited systems
- `cmd/tools/play_as/main.go` — update security-status display line to show "Unexplored" when `LastVisitedTick == 0`

**Modified — observer / WS payload:**
- `pkg/observe/` (specific file located in Task 6) — include `last_visited_tick` on system payloads sent to the frontend

**Modified — frontend:**
- `frontend/src/types/game.ts` — extend `PoliceLevel` union with `'unknown'`; add `lastVisitedTick` to system shape
- `frontend/src/lib/useObserver.ts` — carry `last_visited_tick` through WS message shapes
- `frontend/src/lib/useSystemDetails.ts` — state + reset
- `frontend/src/lib/useSystemMap.ts` — state + mapping
- `frontend/src/lib/useGalaxyMap.ts` — state + mapping
- System-display React components that render security (exact file located in Task 8)

---

## Task 1: Add `last_visited_tick` column + backfill migration

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go`
- Modify: `pkg/knowledge/initial_schema.sql`
- Test: `pkg/knowledge/sqlite_test.go`

- [ ] **Step 1.1: Write the failing migration test**

Append to `pkg/knowledge/sqlite_test.go`:

```go
func TestSQLiteKB_Migration3_LastVisitedTickBackfill(t *testing.T) {
	ctx := context.Background()

	// Fresh DB — migrations run to latest on NewSQLiteKB.
	kb, cleanup := newTestKB(t)
	defer cleanup()

	// Seed: system A has a POI with a resource at tick 100 (visited).
	// System B has a POI without resources (visited but nothing to scan).
	// System C has no POIs at all (never visited / map-only).
	sysA := System{ID: "sys-a", Name: "Alpha", LastUpdatedTick: 50}
	sysB := System{ID: "sys-b", Name: "Beta", LastUpdatedTick: 50}
	sysC := System{ID: "sys-c", Name: "Gamma", LastUpdatedTick: 50}
	for _, s := range []System{sysA, sysB, sysC} {
		if err := kb.RememberSystem(ctx, s); err != nil {
			t.Fatalf("RememberSystem %s: %v", s.ID, err)
		}
	}

	poiA := POI{ID: "poi-a", SystemID: "sys-a", Name: "AsteroidA", Type: "asteroid", LastUpdatedTick: 100,
		Resources: []game.POIResource{{ResourceID: "iron_ore", Richness: 1, Remaining: 1000}}}
	poiB := POI{ID: "poi-b", SystemID: "sys-b", Name: "StationB", Type: "station", LastUpdatedTick: 100}
	if err := kb.RememberPOI(ctx, poiA); err != nil {
		t.Fatalf("RememberPOI A: %v", err)
	}
	if err := kb.RememberPOI(ctx, poiB); err != nil {
		t.Fatalf("RememberPOI B: %v", err)
	}

	// Simulate pre-migration state: zero out the last_visited_tick we just
	// wrote, then re-run the backfill SQL directly.
	if _, err := kb.db.ExecContext(ctx, `UPDATE systems SET last_visited_tick = 0`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := kb.db.ExecContext(ctx, `
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
		)
	`); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	got, err := kb.GetSystem(ctx, "sys-a")
	if err != nil || got == nil {
		t.Fatalf("GetSystem sys-a: %v", err)
	}
	if got.LastVisitedTick != 100 {
		t.Errorf("sys-a LastVisitedTick = %d, want 100", got.LastVisitedTick)
	}

	got, err = kb.GetSystem(ctx, "sys-b")
	if err != nil || got == nil {
		t.Fatalf("GetSystem sys-b: %v", err)
	}
	if got.LastVisitedTick != 0 {
		t.Errorf("sys-b LastVisitedTick = %d, want 0 (no resources)", got.LastVisitedTick)
	}

	got, err = kb.GetSystem(ctx, "sys-c")
	if err != nil || got == nil {
		t.Fatalf("GetSystem sys-c: %v", err)
	}
	if got.LastVisitedTick != 0 {
		t.Errorf("sys-c LastVisitedTick = %d, want 0 (no POIs)", got.LastVisitedTick)
	}
}
```

Note: `newTestKB` is the existing helper in `sqlite_test.go`. If it has a different name in the file, reuse whatever existing tests use.

- [ ] **Step 1.2: Run the test and verify it fails**

Run: `go test ./pkg/knowledge/ -run TestSQLiteKB_Migration3_LastVisitedTickBackfill -v`

Expected: FAIL — either a compilation error (`LastVisitedTick` undefined on `System`) or a SQL error (`no such column: last_visited_tick`). Either proves the column and field don't yet exist.

- [ ] **Step 1.3: Add migration 3 to `sqlite_migrations.go`**

In `pkg/knowledge/sqlite_migrations.go`, inside the `migrations()` return list, append after the existing version 2 entry:

```go
{
    version: 3,
    name:    "add_last_visited_tick_to_systems",
    sql: `
        ALTER TABLE systems ADD COLUMN last_visited_tick INTEGER NOT NULL DEFAULT 0;

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
    `,
},
```

Then locate the "Special case for migration 2" block (around line 78, the code that checks `pragma_table_info` for an already-present column) and add a parallel idempotency guard so re-running on a DB where the column already exists (e.g., one that picked up the column via an updated `initial_schema.sql`) does not error:

```go
// Special case for migration 3: skip if column already exists.
if m.version == 3 {
    var colCount int
    err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('systems') WHERE name='last_visited_tick'").Scan(&colCount)
    if err != nil {
        return fmt.Errorf("check last_visited_tick column: %w", err)
    }
    if colCount > 0 {
        // Column already present — record the migration as applied and move on.
        if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
            m.version, time.Now().Format(time.RFC3339)); err != nil {
            return fmt.Errorf("record migration 3: %w", err)
        }
        continue
    }
}
```

(Match the exact shape of the existing version-2 special-case block — copy its structure, including any `time` import already present.)

- [ ] **Step 1.4: Update `initial_schema.sql`**

In `pkg/knowledge/initial_schema.sql`, find the `CREATE TABLE systems` statement (around line 691 — look for the `is_stronghold BOOLEAN DEFAULT 0, security_status TEXT DEFAULT ''` tail). Replace the statement's trailing portion to include the new column:

Before:
```sql
, is_stronghold BOOLEAN DEFAULT 0, security_status TEXT DEFAULT '');
```

After:
```sql
, is_stronghold BOOLEAN DEFAULT 0, security_status TEXT DEFAULT '', last_visited_tick INTEGER NOT NULL DEFAULT 0);
```

- [ ] **Step 1.5: Add the `LastVisitedTick` field to `System`**

In `pkg/knowledge/memory.go`, in the `System` struct (starts around line 367), add the new field after `LastUpdatedTick`:

```go
type System struct {
    ID              string
    Name            string
    Description     string
    Position        game.Position
    PoliceLevel     int    // Security level 0-3 (0=none, 1=low, 2=medium, 3=high)
    SecurityStatus  string // e.g. "high_sec", "low_sec", "null_sec"
    Empire          string
    IsStronghold    bool
    Connections     []SystemConnection
    POIs            []string
    LastUpdatedTick int64
    LastVisitedTick int64 // 0 = never visited; PoliceLevel is untrusted when 0
}

// Explored reports whether the system's PoliceLevel reflects a real
// observation rather than a map-import default.
func (s System) Explored() bool { return s.LastVisitedTick > 0 }
```

- [ ] **Step 1.6: Wire the column through `SQLiteKB.RememberSystem`, `GetSystem`, `GetSystems`**

In `pkg/knowledge/sqlite.go`:

Replace the `INSERT INTO systems` block in `RememberSystem` (around lines 112–126) with:

```go
_, err = tx.ExecContext(ctx, `
    INSERT INTO systems (id, name, description, position_x, position_y, police_level, security_status, empire, is_stronghold, last_updated_tick, last_visited_tick)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET
        name = excluded.name,
        description = excluded.description,
        position_x = CASE WHEN excluded.position_x != 0 THEN excluded.position_x ELSE systems.position_x END,
        position_y = CASE WHEN excluded.position_y != 0 THEN excluded.position_y ELSE systems.position_y END,
        police_level = excluded.police_level,
        security_status = excluded.security_status,
        empire = excluded.empire,
        is_stronghold = excluded.is_stronghold,
        last_updated_tick = excluded.last_updated_tick,
        last_visited_tick = CASE WHEN excluded.last_visited_tick > 0 THEN excluded.last_visited_tick ELSE systems.last_visited_tick END
`, sys.ID, sys.Name, sys.Description, sys.Position.X, sys.Position.Y,
    sys.PoliceLevel, sys.SecurityStatus, sys.Empire, sys.IsStronghold, sys.LastUpdatedTick, sys.LastVisitedTick)
```

The `CASE` on the update path ensures a `RememberSystem` call that happens to pass `LastVisitedTick=0` (e.g., test fixtures) never clobbers a previously-set non-zero value — real visits always pass a positive tick.

Replace the `GetSystem` query (around lines 196–203):

```go
err := kb.db.QueryRowContext(ctx, `
    SELECT id, name, COALESCE(description, ''), position_x, position_y, police_level, COALESCE(security_status, ''), empire, is_stronghold, last_updated_tick, last_visited_tick
    FROM systems
    WHERE id = ?
`, systemID).Scan(
    &sys.ID, &sys.Name, &sys.Description, &sys.Position.X, &sys.Position.Y,
    &sys.PoliceLevel, &sys.SecurityStatus, &sys.Empire, &sys.IsStronghold, &sys.LastUpdatedTick, &sys.LastVisitedTick,
)
```

Replace the `GetSystems` query (around lines 820–836):

```go
rows, err := kb.db.QueryContext(ctx, `
    SELECT id, name, COALESCE(description, ''), position_x, position_y, police_level, COALESCE(security_status, ''), empire, is_stronghold, last_updated_tick, last_visited_tick
    FROM systems
`)
// ... defer rows.Close() ...
for rows.Next() {
    var sys System
    if err := rows.Scan(
        &sys.ID, &sys.Name, &sys.Description, &sys.Position.X, &sys.Position.Y,
        &sys.PoliceLevel, &sys.SecurityStatus, &sys.Empire, &sys.IsStronghold, &sys.LastUpdatedTick, &sys.LastVisitedTick,
    ); err != nil {
        return nil, fmt.Errorf("scan system: %w", err)
    }
    systems = append(systems, sys)
}
```

(Keep the surrounding code — the connection-loading loop below — unchanged.)

**Do not touch `UpsertSystemFromMap`.** Its existing `ON CONFLICT DO UPDATE SET` clause already lists only the columns it overwrites; since `last_visited_tick` is absent from that list, map imports correctly leave it alone. This is the core invariant — a separate task (Task 3) will add a test to pin this behavior.

- [ ] **Step 1.7: Run the test and verify it passes**

Run: `go test ./pkg/knowledge/ -run TestSQLiteKB_Migration3_LastVisitedTickBackfill -v`

Expected: PASS.

- [ ] **Step 1.8: Run the full knowledge package tests**

Run: `go test ./pkg/knowledge/ -v`

Expected: all existing tests still pass. If any fail due to the new struct field, they're almost certainly constructing `System{}` literals that don't need the new field — the zero value is correct. Only real failures indicate something that needs fixing.

- [ ] **Step 1.9: Commit**

```bash
git add pkg/knowledge/sqlite_migrations.go pkg/knowledge/initial_schema.sql pkg/knowledge/memory.go pkg/knowledge/sqlite.go pkg/knowledge/sqlite_test.go
git commit -m "feat(knowledge): add last_visited_tick to systems table

Distinguishes confirmed-lawless (police_level=0, visited) from
unexplored (police_level=0, never visited). Migration 3 adds the
column and backfills from poi_resources.last_updated_tick."
```

---

## Task 2: Update `MemoryKB` to propagate `LastVisitedTick`

**Files:**
- Modify: `pkg/knowledge/memory.go:91-117`
- Test: `pkg/knowledge/sqlite_test.go` (MemoryKB tests live here too; if a dedicated `memory_test.go` exists, use it)

- [ ] **Step 2.1: Write the failing test**

Append to `pkg/knowledge/sqlite_test.go` (or `memory_test.go` if present):

```go
func TestMemoryKB_RememberSystem_PersistsLastVisitedTick(t *testing.T) {
	ctx := context.Background()
	kb := NewMemoryKB()

	sys := System{ID: "sys-a", Name: "Alpha", LastUpdatedTick: 50, LastVisitedTick: 100}
	if err := kb.RememberSystem(ctx, sys); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}

	got, err := kb.GetSystem(ctx, "sys-a")
	if err != nil || got == nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.LastVisitedTick != 100 {
		t.Errorf("LastVisitedTick = %d, want 100", got.LastVisitedTick)
	}
	if !got.Explored() {
		t.Error("Explored() = false, want true")
	}

	// Overwrite with zero tick — should preserve the previous non-zero value.
	sys2 := System{ID: "sys-a", Name: "Alpha", LastUpdatedTick: 60, LastVisitedTick: 0}
	if err := kb.RememberSystem(ctx, sys2); err != nil {
		t.Fatalf("RememberSystem (2): %v", err)
	}
	got, err = kb.GetSystem(ctx, "sys-a")
	if err != nil || got == nil {
		t.Fatalf("GetSystem (2): %v", err)
	}
	if got.LastVisitedTick != 100 {
		t.Errorf("LastVisitedTick after zero-tick overwrite = %d, want 100 (preserved)", got.LastVisitedTick)
	}
}
```

- [ ] **Step 2.2: Run and verify it fails**

Run: `go test ./pkg/knowledge/ -run TestMemoryKB_RememberSystem_PersistsLastVisitedTick -v`

Expected: FAIL — `LastVisitedTick` on retrieved system is `0` because `MemoryKB.RememberSystem` doesn't copy it.

- [ ] **Step 2.3: Update `MemoryKB.RememberSystem`**

In `pkg/knowledge/memory.go`, around lines 91-117, replace the function body with:

```go
func (kb *MemoryKB) RememberSystem(ctx context.Context, sys System) error {
    kb.mu.Lock()
    defer kb.mu.Unlock()

    if existing, ok := kb.systems[sys.ID]; ok {
        existing.Name = sys.Name
        existing.Position = sys.Position
        existing.PoliceLevel = sys.PoliceLevel
        existing.SecurityStatus = sys.SecurityStatus
        existing.Empire = sys.Empire
        existing.IsStronghold = sys.IsStronghold
        existing.LastUpdatedTick = sys.LastUpdatedTick
        if sys.LastVisitedTick > 0 {
            existing.LastVisitedTick = sys.LastVisitedTick
        }
        existing.Connections = sys.Connections
    } else {
        kb.systems[sys.ID] = &System{
            ID:              sys.ID,
            Name:            sys.Name,
            Position:        sys.Position,
            PoliceLevel:     sys.PoliceLevel,
            SecurityStatus:  sys.SecurityStatus,
            Empire:          sys.Empire,
            IsStronghold:    sys.IsStronghold,
            Connections:     sys.Connections,
            LastUpdatedTick: sys.LastUpdatedTick,
            LastVisitedTick: sys.LastVisitedTick,
        }
    }

    return nil
}
```

- [ ] **Step 2.4: Run and verify it passes**

Run: `go test ./pkg/knowledge/ -run TestMemoryKB_RememberSystem_PersistsLastVisitedTick -v`

Expected: PASS.

- [ ] **Step 2.5: Commit**

```bash
git add pkg/knowledge/memory.go pkg/knowledge/sqlite_test.go
git commit -m "feat(knowledge): propagate LastVisitedTick through MemoryKB"
```

---

## Task 3: Pin the `UpsertSystemFromMap` invariant with a test

**Files:**
- Test: `pkg/knowledge/sqlite_test.go`

Goal: a regression test that proves `UpsertSystemFromMap` never clobbers a non-zero `last_visited_tick`. This invariant is what keeps map re-imports from un-marking systems as explored.

- [ ] **Step 3.1: Write the failing test**

Append to `pkg/knowledge/sqlite_test.go`:

```go
func TestSQLiteKB_UpsertSystemFromMap_PreservesLastVisitedTick(t *testing.T) {
	ctx := context.Background()
	kb, cleanup := newTestKB(t)
	defer cleanup()

	// Simulate a visited system.
	visited := System{
		ID: "sys-visited", Name: "Visited", Empire: "solarian",
		PoliceLevel: 2, SecurityStatus: "medium_sec",
		LastUpdatedTick: 100, LastVisitedTick: 100,
	}
	if err := kb.RememberSystem(ctx, visited); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}

	// A subsequent get_map import for the same system must NOT reset the tick.
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "sys-visited", Name: "Visited",
		PositionX: 10, PositionY: 20, Empire: "solarian",
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap: %v", err)
	}

	got, err := kb.GetSystem(ctx, "sys-visited")
	if err != nil || got == nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.LastVisitedTick != 100 {
		t.Errorf("LastVisitedTick = %d, want 100 (map import must not clobber)", got.LastVisitedTick)
	}
	if !got.Explored() {
		t.Error("Explored() = false after map re-import, want true")
	}
}

func TestSQLiteKB_UpsertSystemFromMap_LeavesFreshSystemUnexplored(t *testing.T) {
	ctx := context.Background()
	kb, cleanup := newTestKB(t)
	defer cleanup()

	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "sys-new", Name: "New", PositionX: 1, PositionY: 2,
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap: %v", err)
	}

	got, err := kb.GetSystem(ctx, "sys-new")
	if err != nil || got == nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.LastVisitedTick != 0 {
		t.Errorf("LastVisitedTick = %d, want 0 (map-only system)", got.LastVisitedTick)
	}
	if got.Explored() {
		t.Error("Explored() = true for map-only system")
	}
}
```

- [ ] **Step 3.2: Run and verify**

Run: `go test ./pkg/knowledge/ -run TestSQLiteKB_UpsertSystemFromMap_ -v`

Expected: PASS on both. (They should pass immediately because Task 1's SQL already preserves the column — these tests pin the behavior.) If either fails, the issue is in Task 1's SQL — revisit the `ON CONFLICT` clause in `UpsertSystemFromMap` and ensure `last_visited_tick` is NOT listed.

- [ ] **Step 3.3: Commit**

```bash
git add pkg/knowledge/sqlite_test.go
git commit -m "test(knowledge): pin UpsertSystemFromMap last_visited_tick invariant"
```

---

## Task 4: Set `LastVisitedTick` on the `play_as` visit path

**Files:**
- Modify: `cmd/tools/play_as/kb_update.go:110-123`
- Modify: `cmd/tools/play_as/main.go:1857`

`kb_update.go`'s `kbUpdateSystem` is the main place a real visit is written to the KB. It already calls `client.GetSystem` (which requires being in the system) and then `RememberSystem`. This IS a visit — so it must set `LastVisitedTick`.

- [ ] **Step 4.1: Update `kbUpdateSystem`**

In `cmd/tools/play_as/kb_update.go`, in the `kbSystem` literal around line 110, add `LastVisitedTick`:

```go
kbSystem := knowledge.System{
    ID:              state.System.ID,
    Name:            state.System.Name,
    PoliceLevel:     state.System.PoliceLevel,
    SecurityStatus:  state.System.SecurityStatus,
    Empire:          state.System.Empire,
    IsStronghold:    state.System.IsStronghold,
    Connections:     extractConnections(state.System.Connections),
    LastUpdatedTick: currentTick(state),
    LastVisitedTick: currentTick(state),
    Position: game.Position{
        X: state.System.Position.X,
        Y: state.System.Position.Y,
    },
}
```

- [ ] **Step 4.2: Update the security-status display in `main.go`**

In `cmd/tools/play_as/main.go`, find the line (around 1857):

```go
fmt.Fprintf(&b, "Security Status: %d - %s\n", sys.PoliceLevel, sys.SecurityStatus)
```

The `sys` here is a local JSON-decoded struct (around line 1838) that currently doesn't carry `last_visited_tick`. Two cases:

**Case A — if `sys` is decoded from the KB's `System` (i.e., it has `LastVisitedTick int64`):**

Replace the line with:

```go
if sys.LastVisitedTick == 0 {
    fmt.Fprintf(&b, "Security Status: Unexplored\n")
} else {
    fmt.Fprintf(&b, "Security Status: %d - %s\n", sys.PoliceLevel, sys.SecurityStatus)
}
```

**Case B — if `sys` is decoded from a map-import JSON (no visit info):**

Look at the enclosing function (the decoded struct at line 1838 shows `PoliceLevel int` only). If it's a display of raw game server data (not KB data), leave the line alone — the "Unexplored" concept only applies at the KB consumer layer. Add a comment:

```go
// NOTE: this renders server-live system data, not KB state. Unexplored
// status is determined by the KB read path.
fmt.Fprintf(&b, "Security Status: %d - %s\n", sys.PoliceLevel, sys.SecurityStatus)
```

Read the surrounding 30 lines of `main.go` starting at line 1820 to confirm which case applies. Commit whichever change matches.

- [ ] **Step 4.3: Build**

Run: `go build ./...`

Expected: success.

- [ ] **Step 4.4: Commit**

```bash
git add cmd/tools/play_as/kb_update.go cmd/tools/play_as/main.go
git commit -m "feat(play_as): set LastVisitedTick on KB system writes"
```

---

## Task 5: Audit other `RememberSystem` callers

**Files:**
- Search across the codebase

There may be additional callers of `RememberSystem` that should also set `LastVisitedTick`. The goal of this task is an audit + targeted fixes.

- [ ] **Step 5.1: Enumerate all non-test `RememberSystem` callers**

Run:

```bash
grep -rn "RememberSystem(" --include="*.go" | grep -v "_test.go" | grep -v "mockKB"
```

Expected output includes at minimum `cmd/tools/play_as/kb_update.go` (already updated in Task 4) and the interface declaration at `pkg/knowledge/base.go`. Any other hit is a candidate for inspection.

- [ ] **Step 5.2: For each remaining caller, decide**

For each call site found, ask: "is this code reachable only after the player/agent is physically in the system?" If yes, add `LastVisitedTick: currentTick(...)` (or the equivalent field) to the `System{}` literal. If no (e.g., it's seeding from map data), leave `LastVisitedTick` as the zero value.

Document the decision in the commit message (list each file and the verdict).

- [ ] **Step 5.3: Build and run tests**

Run:

```bash
go build ./...
go test ./...
```

Expected: all pass.

- [ ] **Step 5.4: Commit**

```bash
git add -A
git commit -m "feat: set LastVisitedTick at all visit-path RememberSystem callers

<list of files + per-file rationale>"
```

(If no changes beyond Task 4 were needed, skip this commit and note that in the PR description.)

---

## Task 6: Include `last_visited_tick` in observer WS payloads

**Files:**
- Modify: `pkg/observe/*.go` (locate the system-payload serializer)

- [ ] **Step 6.1: Find the current serialization site**

Run:

```bash
grep -rn "police_level\|PoliceLevel" pkg/observe/ --include="*.go"
```

Identify the file(s) that serialize system state into WS messages. There should be a struct with a `json:"police_level"` tag (or similar) that is sent on `state_update`/`game_message`/`agent_status` events.

- [ ] **Step 6.2: Add the `last_visited_tick` field**

Wherever the system shape is emitted to the frontend, add an `int64` field tagged `json:"last_visited_tick"` and populate it from `state.System.LastVisitedTick` if the server's in-memory `State` has it, or from `knowledge.System.LastVisitedTick` if the observer reads from the KB.

Check `pkg/game/types.go` around line 208 (`SystemData`) to see whether the live game state carries a tick. If not, thread it in:

```go
type SystemData struct {
    // ... existing fields ...
    LastVisitedTick int64 `json:"last_visited_tick,omitempty"`
}
```

And in `pkg/game/client.go:2300`-area where `c.state.System.SecurityStatus = sys.SecurityStatus` is set, add:

```go
c.state.System.LastVisitedTick = sys.LastVisitedTick
```

…if the server `sys` carries that. If it doesn't (the server likely doesn't know/care), set it to `currentTick(state)` on successful `get_system` response handling, since the successful handling means the client is physically in the system.

- [ ] **Step 6.3: Build**

Run: `go build ./...`

Expected: success.

- [ ] **Step 6.4: Commit**

```bash
git add -A
git commit -m "feat(observe): include last_visited_tick in system WS payload"
```

---

## Task 7: Frontend types

**Files:**
- Modify: `frontend/src/types/game.ts:21-66`
- Modify: `frontend/src/lib/useObserver.ts:59-242`
- Modify: `frontend/src/lib/useSystemDetails.ts`
- Modify: `frontend/src/lib/useSystemMap.ts`
- Modify: `frontend/src/lib/useGalaxyMap.ts`

- [ ] **Step 7.1: Extend the `PoliceLevel` type**

In `frontend/src/types/game.ts`:

```ts
export type PoliceLevel = 'unknown' | 'lawless' | 'policed';
```

Add a `lastVisitedTick` field to the system type (around line 21, the struct that currently has `policeLevel: PoliceLevel`):

```ts
export interface System /* or whatever the existing name is */ {
  // ... existing fields ...
  policeLevel: PoliceLevel;
  lastVisitedTick: number; // 0 = unexplored
}
```

- [ ] **Step 7.2: Thread through `useObserver.ts`**

Find the two inline system shapes in `frontend/src/lib/useObserver.ts` (around lines 59 and the fallback around 106 and 200). Add `last_visited_tick: number` to each:

```ts
police_level: number;
security_status: string;
last_visited_tick: number;
```

For the fallback object literals at lines 106 and 200:

```ts
police_level: 0,
last_visited_tick: 0,
```

- [ ] **Step 7.3: `useSystemDetails.ts`**

Add `lastVisitedTick` state and reset logic mirroring the existing `policeLevel` handling:

```ts
const [lastVisitedTick, setLastVisitedTick] = useState(0);

// In the reset path:
setLastVisitedTick(0);

// In the populate path (where setPoliceLevel(data.system?.police_level ?? 0) is):
setLastVisitedTick(data.system?.last_visited_tick ?? 0);
```

Export `lastVisitedTick` alongside `policeLevel` in the hook's return value.

- [ ] **Step 7.4: `useSystemMap.ts` and `useGalaxyMap.ts`**

In each, locate the inline system shape (with `police_level: number`) and add `last_visited_tick: number`. In the mapping step (around `useSystemMap.ts:101`), thread it into the returned object:

```ts
policeLevel: detail.system.police_level,
lastVisitedTick: detail.system.last_visited_tick,
```

- [ ] **Step 7.5: Build**

```bash
cd frontend && npm run build
```

Expected: success. If TypeScript errors surface in consumer components, fix them by showing "Unexplored" when `lastVisitedTick === 0` (see Task 8).

- [ ] **Step 7.6: Commit**

```bash
git add frontend/src/types/game.ts frontend/src/lib/useObserver.ts frontend/src/lib/useSystemDetails.ts frontend/src/lib/useSystemMap.ts frontend/src/lib/useGalaxyMap.ts
git commit -m "feat(frontend): thread last_visited_tick through system hooks"
```

---

## Task 8: Render "Unexplored" in system-display components

**Files:**
- Modify: any React component that displays `policeLevel` or `securityStatus` for a system

- [ ] **Step 8.1: Find the rendering sites**

```bash
grep -rn "policeLevel\|police_level\|securityStatus\|security_status" frontend/src/components/
```

- [ ] **Step 8.2: Update each render site**

For each component that displays the security/police value, add a guard:

```tsx
{lastVisitedTick === 0 ? (
  <span className="text-muted">Unexplored</span>
) : (
  <span>{renderSecurity(policeLevel, securityStatus)}</span>
)}
```

(Reuse whatever existing helper renders the security text — this plan doesn't dictate presentational details. The key is: `lastVisitedTick === 0` → "Unexplored" regardless of other values.)

For the galaxy-map color legend, map `lastVisitedTick === 0` to a dedicated "unexplored" color/symbol distinct from the lawless color.

- [ ] **Step 8.3: Visual verification**

Start the dev server:

```bash
cd frontend && npm run dev
```

Open the system details panel for a known Lawless system (`police_level === 0`, `last_visited_tick > 0`) and confirm it renders as "Lawless" (or whatever the existing label is). Open an unexplored system from the galaxy map and confirm it renders as "Unexplored".

- [ ] **Step 8.4: Commit**

```bash
git add frontend/src/components/
git commit -m "feat(frontend): render Unexplored for systems with last_visited_tick=0"
```

---

## Task 9: End-to-end verification

- [ ] **Step 9.1: Full backend build and test**

```bash
go build ./...
go test ./...
```

Expected: all pass.

- [ ] **Step 9.2: Golangci-lint**

```bash
golangci-lint run ./...
```

Expected: no new findings.

- [ ] **Step 9.3: Frontend build**

```bash
cd frontend && npm run build
```

Expected: success.

- [ ] **Step 9.4: Manual DB migration smoke test**

Point the server at a real (backed-up copy of the) production-style DB and start it. Confirm logs show migration 3 running and `last_visited_tick` column present:

```bash
sqlite3 /path/to/copy.db "PRAGMA table_info(systems);" | grep last_visited_tick
sqlite3 /path/to/copy.db "SELECT COUNT(*) FROM systems WHERE last_visited_tick > 0;"
sqlite3 /path/to/copy.db "SELECT COUNT(*) FROM systems WHERE last_visited_tick = 0;"
```

Sanity-check: the "visited" count roughly matches the number of systems that have `poi_resources` rows.

```bash
sqlite3 /path/to/copy.db "SELECT COUNT(DISTINCT p.system_id) FROM pois p JOIN poi_resources pr ON pr.poi_id = p.id;"
```

The two "visited" numbers should match.

- [ ] **Step 9.5: Final commit / PR prep**

If everything passes, the branch is ready to merge. Note in the PR description that `spacemolt-kb`'s `generate-items-kb` tool is a follow-on task that should read the new column and render "Unknown" for unexplored systems.

---

## Notes for the implementer

- `currentTick(state)` is the existing helper in `cmd/tools/play_as/`. Elsewhere, the current tick lives on `state.Tick` or is obtained through the same helper — use whatever the nearest code already does.
- Don't panic if `SecurityStatus` appears underived in Go — it's a string sent straight from the server. The "Unexplored" semantic lives entirely at the consumer layer, keyed off `LastVisitedTick`.
- The migration is additive and idempotent. Running it twice (or running it against a DB where `initial_schema.sql` already added the column) is a no-op thanks to the special-case guard in Task 1 Step 1.3.
- No index on `last_visited_tick` — per the spec, defer until query plans demand it.
