# Smuggling Part 1 — Capture Procedural Missions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist procedural (route-generated) missions — smuggling couriers, trade-runs — into the KB mission catalog, which today skips them because they carry no `template_id`.

**Architecture:** The mission-catalog infra already exists (`mission_templates` dedup catalog + `mission_objectives` + `mission_template_locations` sighting log; `worker.KBUpdateMissions` captures a board). Three changes: (1) add a `procedural` column to `mission_templates`; (2) key procedural entries by a synthetic id derived from `mission_id` with its `~<hash>` instance suffix stripped, flagging `procedural=true`; (3) stop skipping template-less entries in `KBUpdateMissions`.

**Tech Stack:** Go 1.24, SQLite via `modernc.org/sqlite` (driver name `"sqlite"`), `database/sql`.

## Global Constraints

- Target Go 1.24+; use modern idioms (`b.Loop()` in benchmarks, range-over-int where natural).
- `golangci-lint` must pass with no new findings after each change.
- Run `go build ./...` and `go test ./...` before committing.
- Do NOT assume server/wire field names — the wire struct is `serverapi.MissionBoardEntry` (fields `MissionID`, `TemplateID`, `Type`, …), verified in `pkg/game/serverapi/types.go`.
- Editing `pkg/knowledge/initial_schema.sql` REQUIRES re-running `scripts/sql/regenerate_initialize_database.sh` and committing `scripts/sql/initialize_database.sql`, or `TestInitializeDatabaseInSync` fails.
- Stage changes explicitly (never `git add -A`); `data/*.json` churn stays unstaged.

---

## File Structure

- `pkg/knowledge/initial_schema.sql` — add `procedural` column to the `mission_templates` DDL (fresh DBs).
- `pkg/knowledge/sqlite_migrations.go` — add `ensureMissionTemplatesProceduralCol` self-healing ALTER (existing/live DBs) + wire into the migrate runner.
- `pkg/knowledge/mission_catalog.go` — new exported `MissionCatalogID` helper; `missionCatalogRow.Procedural` field; use both in `missionRowFromEntry`.
- `pkg/knowledge/sqlite_mission.go` — `insertMissionRow` writes the `procedural` column.
- `pkg/worker/capture.go` — `KBUpdateMissions` skip-guard uses the derived id instead of `TemplateID == ""`.
- `pkg/knowledge/sqlite_mission_test.go` — NEW: schema-column + procedural-upsert tests.
- `pkg/knowledge/mission_catalog_test.go` — add `MissionCatalogID` unit tests.
- `pkg/worker/capture_missions_test.go` — update the "hand-authored only" test to expect procedural capture.
- `scripts/sql/initialize_database.sql` — regenerated (not hand-edited).

---

## Task 1: Add the `procedural` column to `mission_templates`

**Files:**
- Modify: `pkg/knowledge/initial_schema.sql` (mission_templates CREATE TABLE)
- Modify: `pkg/knowledge/sqlite_migrations.go` (add ensure func + wire it in the ensures block near line 774)
- Modify (generated): `scripts/sql/initialize_database.sql` (via regenerate script)
- Test: `pkg/knowledge/sqlite_mission_test.go` (new file)

**Interfaces:**
- Produces: `func ensureMissionTemplatesProceduralCol(db *sql.DB) error` — idempotent ALTER adding `mission_templates.procedural INTEGER DEFAULT 0`.
- Produces: `mission_templates.procedural` column (0 = hand-authored template, 1 = procedural).

- [ ] **Step 1: Write the failing tests**

Create `pkg/knowledge/sqlite_mission_test.go`:

```go
package knowledge

import (
	"database/sql"
	"testing"
)

func TestMissionTemplatesHasProceduralColumn(t *testing.T) {
	kb := newTestSQLiteKB(t)
	var n int
	if err := kb.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('mission_templates') WHERE name='procedural'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("mission_templates.procedural column missing (got %d)", n)
	}
}

func TestEnsureMissionTemplatesProceduralCol_AddsToLegacy(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Legacy table shape: no `procedural` column.
	if _, err := db.Exec(`CREATE TABLE mission_templates (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}

	if err := ensureMissionTemplatesProceduralCol(db); err != nil {
		t.Fatalf("ensure (add): %v", err)
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('mission_templates') WHERE name='procedural'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("procedural column not added (got %d)", n)
	}
	// Idempotent: second run is a no-op, not an error.
	if err := ensureMissionTemplatesProceduralCol(db); err != nil {
		t.Fatalf("ensure (idempotent): %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/knowledge/ -run 'TestMissionTemplatesHasProceduralColumn|TestEnsureMissionTemplatesProceduralCol' -v`
Expected: FAIL — `TestMissionTemplatesHasProceduralColumn` gets 0 (column absent); `TestEnsureMissionTemplatesProceduralCol_AddsToLegacy` fails to compile until `ensureMissionTemplatesProceduralCol` exists.

- [ ] **Step 3: Add the column to the fresh-DB schema**

In `pkg/knowledge/initial_schema.sql`, in the `CREATE TABLE mission_templates (…)` block, add the `procedural` line immediately after the `provided_items` line:

```sql
    provided_items      TEXT DEFAULT '{}',
    procedural          INTEGER DEFAULT 0,
    first_seen_tick     INTEGER DEFAULT 0,
```

- [ ] **Step 4: Add the self-healing ensure function**

In `pkg/knowledge/sqlite_migrations.go`, add this function next to `ensureShipClassPrestigeCols`:

```go
// ensureMissionTemplatesProceduralCol adds the mission_templates.procedural
// column (0 = hand-authored template, 1 = route-generated/procedural) to DBs
// that predate it. Self-healing ensure rather than a numbered migration so it
// runs AFTER ensureCollapseMissingTables guarantees the table exists — mirrors
// ensureShipClassPrestigeCols.
func ensureMissionTemplatesProceduralCol(db *sql.DB) error {
	var tableCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mission_templates'`,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("check mission_templates table: %w", err)
	}
	if tableCount == 0 {
		return nil // table not created yet; nothing to reconcile
	}
	var present int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('mission_templates') WHERE name='procedural'`,
	).Scan(&present); err != nil {
		return fmt.Errorf("check mission_templates.procedural column: %w", err)
	}
	if present > 0 {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE mission_templates ADD COLUMN procedural INTEGER DEFAULT 0`); err != nil {
		return fmt.Errorf("add mission_templates.procedural column: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Wire the ensure into the migrate runner**

In `pkg/knowledge/sqlite_migrations.go`, immediately after the existing `ensureShipClassPrestigeCols(db)` block (near line 774), add:

```go
	if err := ensureMissionTemplatesProceduralCol(db); err != nil {
		return fmt.Errorf("ensure mission_templates procedural column: %w", err)
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/knowledge/ -run 'TestMissionTemplatesHasProceduralColumn|TestEnsureMissionTemplatesProceduralCol' -v`
Expected: PASS (both).

- [ ] **Step 7: Regenerate the reference schema and verify the sync test**

Run: `./scripts/sql/regenerate_initialize_database.sh`
Then: `go test ./pkg/knowledge/ -run TestInitializeDatabaseInSync -v`
Expected: PASS (regenerated `scripts/sql/initialize_database.sql` now contains `procedural`).

- [ ] **Step 8: Lint + commit**

Run: `golangci-lint run ./pkg/knowledge/...`
Expected: no new findings.

```bash
git add pkg/knowledge/initial_schema.sql pkg/knowledge/sqlite_migrations.go pkg/knowledge/sqlite_mission_test.go scripts/sql/initialize_database.sql
git commit -m "feat(knowledge): add mission_templates.procedural column"
```

---

## Task 2: Synthetic id + procedural flag in the catalog row

**Files:**
- Modify: `pkg/knowledge/mission_catalog.go` (import `strings`; add `Procedural` field; add `MissionCatalogID`; use in `missionRowFromEntry`)
- Modify: `pkg/knowledge/sqlite_mission.go` (`insertMissionRow` writes `procedural`)
- Test: `pkg/knowledge/mission_catalog_test.go` (add `MissionCatalogID` cases)
- Test: `pkg/knowledge/sqlite_mission_test.go` (add courier-upsert case)

**Interfaces:**
- Consumes: `mission_templates.procedural` column (Task 1).
- Produces: `func MissionCatalogID(e serverapi.MissionBoardEntry) (id string, procedural bool)` — returns `(TemplateID, false)` when a template_id is present, else `(mission_id-before-"~", true)`, else `("", true)` when both are empty.
- Produces: `missionCatalogRow.Procedural bool`, persisted to `mission_templates.procedural` on insert.

- [ ] **Step 1: Write the failing unit tests for `MissionCatalogID`**

Append to `pkg/knowledge/mission_catalog_test.go`:

```go
func TestMissionCatalogID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		entry          serverapi.MissionBoardEntry
		wantID         string
		wantProcedural bool
	}{
		{
			name:   "hand-authored template",
			entry:  serverapi.MissionBoardEntry{MissionID: "no_questions_asked", TemplateID: "no_questions_asked"},
			wantID: "no_questions_asked", wantProcedural: false,
		},
		{
			name:   "procedural courier strips hash suffix",
			entry:  serverapi.MissionBoardEntry{MissionID: "smuggling_courier_a_b_red_mist~3a980627043000ee"},
			wantID: "smuggling_courier_a_b_red_mist", wantProcedural: true,
		},
		{
			name:   "procedural without hash",
			entry:  serverapi.MissionBoardEntry{MissionID: "trade_a_b_gutter_flux"},
			wantID: "trade_a_b_gutter_flux", wantProcedural: true,
		},
		{
			name:   "no key at all",
			entry:  serverapi.MissionBoardEntry{},
			wantID: "", wantProcedural: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, proc := MissionCatalogID(c.entry)
			if id != c.wantID || proc != c.wantProcedural {
				t.Fatalf("got (%q, %v), want (%q, %v)", id, proc, c.wantID, c.wantProcedural)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestMissionCatalogID -v`
Expected: FAIL — `MissionCatalogID` undefined (compile error).

- [ ] **Step 3: Add `MissionCatalogID`, the `Procedural` field, and wire `missionRowFromEntry`**

In `pkg/knowledge/mission_catalog.go`, add `"strings"` to the import block:

```go
import (
	"encoding/json"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)
```

Add the `Procedural` field to `missionCatalogRow` (immediately after the `ID string` field):

```go
type missionCatalogRow struct {
	ID              string
	Procedural      bool
	Title           string
	// … rest unchanged …
```

Add the exported helper (place it just above `missionRowFromEntry`):

```go
// MissionCatalogID returns the catalog key for a board entry and whether it is
// a procedural (route-generated) mission. Hand-authored missions carry a stable
// template_id and are keyed by it (procedural=false). Procedural missions
// (couriers, trade-runs) carry no template_id; their mission_id embeds a
// per-instance "~<hash>" suffix, so the key is the mission_id with that suffix
// stripped (procedural=true) — which dedups repeat sightings of the same route.
// id is "" when the entry has neither a template_id nor a mission_id (caller
// must skip such entries).
func MissionCatalogID(e serverapi.MissionBoardEntry) (id string, procedural bool) {
	if e.TemplateID != "" {
		return e.TemplateID, false
	}
	id = e.MissionID
	if i := strings.IndexByte(id, '~'); i >= 0 {
		id = id[:i]
	}
	return id, true
}
```

Update `missionRowFromEntry` — replace the doc comment's "Callers must skip entries with empty TemplateID before calling this." with "Callers must skip entries whose MissionCatalogID is empty before calling this." and change the row init to set `ID`/`Procedural`:

```go
func missionRowFromEntry(e serverapi.MissionBoardEntry) missionCatalogRow {
	id, procedural := MissionCatalogID(e)
	row := missionCatalogRow{
		ID:              id,
		Procedural:      procedural,
		Title:           e.Title,
		Description:     e.Description,
		Type:            e.Type,
		// … remaining fields unchanged (drop the old `ID: e.TemplateID,` line) …
```

- [ ] **Step 4: Make `insertMissionRow` persist `procedural`**

In `pkg/knowledge/sqlite_mission.go`, update `insertMissionRow` — add `procedural` to the column list (after `provided_items`), add one `?`, and add the value `missionBoolToInt(r.Procedural)` after `r.ProvidedItems`:

```go
	_, err := tx.ExecContext(ctx, `
		INSERT INTO mission_templates (
			id, title, description, type, difficulty,
			giver_name, giver_title, faction_id, faction_name,
			dialog_offer, dialog_accept, dialog_decline, dialog_complete,
			chain_next, repeatable, expires_in_ticks,
			rewards_credits, rewards_skill_xp, rewards_items,
			requirements, required_modules, provided_items, procedural,
			first_seen_tick, last_seen_tick, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, r.Title, r.Description, r.Type, r.Difficulty,
		r.GiverName, r.GiverTitle, r.FactionID, r.FactionName,
		r.DialogOffer, r.DialogAccept, r.DialogDecline, r.DialogComplete,
		r.ChainNext, missionBoolToInt(r.Repeatable), r.ExpiresInTicks,
		r.RewardsCredits, r.RewardsSkillXP, r.RewardsItems,
		r.Requirements, r.RequiredModules, r.ProvidedItems, missionBoolToInt(r.Procedural),
		tick, tick, now, now,
	)
```

(No change needed to `loadMissionRow`, `updateMissionRow`, or `diffMissionRows`: `procedural` is an identity attribute set once at insert — it is neither re-read for diffing nor mutated on update.)

- [ ] **Step 5: Write the failing SQLite courier-upsert test**

In `pkg/knowledge/sqlite_mission_test.go`, first update the existing import block (added in Task 1) to the merged set:

```go
import (
	"context"
	"database/sql"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)
```

Then append the test:

```go
func TestUpsertMissionTemplate_ProceduralCourier(t *testing.T) {
	kb := newTestSQLiteKB(t)
	entry := serverapi.MissionBoardEntry{
		MissionID: "smuggling_courier_treasure_cache_trading_post_frontier_station_pirate_moonshine~57df1053f32e",
		Type:      "smuggling",
		Title:     "Border Job: Pirate Moonshine to Frontier Station",
		Rewards:   &serverapi.MissionRewards{Credits: 300, SkillXP: map[string]int{"smuggling": 50}},
		ProvidedItems: map[string]int{"pirate_moonshine": 5},
		Objectives: []serverapi.MissionObjective{{
			Type: "deliver_item", ItemID: "pirate_moonshine", Quantity: 5,
			TargetBaseID: "frontier_station", SystemID: "altais",
		}},
	}
	res, err := kb.UpsertMissionTemplate(context.Background(), entry, "treasure_cache_trading_post", "treasure_cache", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Inserted {
		t.Fatalf("expected insert, got %+v", res)
	}

	const wantID = "smuggling_courier_treasure_cache_trading_post_frontier_station_pirate_moonshine"
	var procedural int
	if err := kb.db.QueryRow(
		`SELECT procedural FROM mission_templates WHERE id = ?`, wantID,
	).Scan(&procedural); err != nil {
		t.Fatalf("synthetic-id row not found: %v", err)
	}
	if procedural != 1 {
		t.Fatalf("want procedural=1, got %d", procedural)
	}

	var locs int
	if err := kb.db.QueryRow(
		`SELECT COUNT(*) FROM mission_template_locations WHERE mission_id = ? AND base_id = ?`,
		wantID, "treasure_cache_trading_post",
	).Scan(&locs); err != nil {
		t.Fatal(err)
	}
	if locs != 1 {
		t.Fatalf("want 1 sighting row, got %d", locs)
	}
}
```

- [ ] **Step 6: Run all Task-2 tests**

Run: `go test ./pkg/knowledge/ -run 'TestMissionCatalogID|TestUpsertMissionTemplate_ProceduralCourier|TestDiffMissionRows' -v`
Expected: PASS (and the existing `TestDiffMissionRows*` still pass — `Procedural` is not diffed).

- [ ] **Step 7: Full knowledge-package test + lint**

Run: `go test ./pkg/knowledge/...` then `golangci-lint run ./pkg/knowledge/...`
Expected: PASS, no new findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/knowledge/mission_catalog.go pkg/knowledge/sqlite_mission.go pkg/knowledge/mission_catalog_test.go pkg/knowledge/sqlite_mission_test.go
git commit -m "feat(knowledge): key procedural missions by synthetic id + procedural flag"
```

---

## Task 3: Capture procedural missions in `KBUpdateMissions`

**Files:**
- Modify: `pkg/worker/capture.go` (`KBUpdateMissions` skip-guard + counter/log)
- Test: `pkg/worker/capture_missions_test.go` (update the "hand-authored only" assertion)

**Interfaces:**
- Consumes: `knowledge.MissionCatalogID` (Task 2).

- [ ] **Step 1: Update the test to expect procedural capture**

In `pkg/worker/capture_missions_test.go`, give the procedural entry a realistic hashed `mission_id`, rename the test, and assert BOTH entries are now upserted. Replace `missionsRaw` and `TestKBUpdateMissionsUpsertsHandAuthoredOnly`:

```go
const missionsRaw = `{"base_id":"nyx_nexus_station","base_name":"Nyx Nexus Station","missions":[
 {"mission_id":"m1","template_id":"tpl_courier_1","title":"Courier Run"},
 {"mission_id":"smuggling_courier_nyx_haven_red_mist~deadbeef","template_id":"","title":"Procedural Haul","type":"smuggling"}
]}`

func TestKBUpdateMissionsCapturesProceduralToo(t *testing.T) {
	st := &game.State{Doc: true}
	st.System.ID = "nyx"
	st.CurrentPOI = "nyx_nexus_station"
	client := &fakeClient{state: st, raw: map[string][]byte{"missions": []byte(missionsRaw)}}
	kb := &recordingMissionKB{Base: knowledge.NewMemoryKB()}

	if err := KBUpdateMissions(context.Background(), client, kb); err != nil {
		t.Fatal(err)
	}
	if len(kb.upserts) != 2 {
		t.Fatalf("want both hand-authored + procedural upserted, got %d: %+v", len(kb.upserts), kb.upserts)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/worker/ -run TestKBUpdateMissionsCapturesProceduralToo -v`
Expected: FAIL — got 1 upsert (procedural still skipped by the current guard).

- [ ] **Step 3: Flip the skip-guard in `KBUpdateMissions`**

In `pkg/worker/capture.go`, inside `KBUpdateMissions`, replace the per-entry guard:

```go
	for _, entry := range resp.Missions {
		if entry.TemplateID == "" {
			skipped++
			continue
		}
```

with a guard on the derived catalog id (only truly keyless entries are skipped):

```go
	for _, entry := range resp.Missions {
		if id, _ := knowledge.MissionCatalogID(entry); id == "" {
			skipped++
			continue
		}
```

And update the summary line's label from `procedural skipped` to `unkeyed skipped`:

```go
	fmt.Printf("update_missions: %d new, %d unchanged, %d changed, %d unkeyed skipped\n",
		inserted, unchanged, changed, skipped)
```

Update the function doc comment (lines ~752-755): change "Procedural missions (empty template_id) are skipped." to "Procedural missions (no template_id) are keyed by a synthetic id (mission_id with the ~<hash> suffix stripped) and captured; only entries with neither a template_id nor a mission_id are skipped."

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/worker/ -run TestKBUpdateMissionsCapturesProceduralToo -v`
Expected: PASS.

- [ ] **Step 5: Full worker-package test + lint**

Run: `go test ./pkg/worker/...` then `golangci-lint run ./pkg/worker/...`
Expected: PASS, no new findings. (Confirms `TestKBUpdateMissionsRequiresDock` and the rest still pass.)

- [ ] **Step 6: Whole-repo build + test**

Run: `go build ./...` then `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/worker/capture.go pkg/worker/capture_missions_test.go
git commit -m "feat(worker): capture procedural missions in KBUpdateMissions"
```

---

## Manual verification (after merge, optional but recommended)

Using the parked pilot engineer-2 (or any docked worker via play_as at a station with couriers):

```
go run ./cmd/tools/play_as engineer-2
# in REPL, docked at treasure_cache_trading_post:
update_missions        # expect "N new … 0 unkeyed skipped" (was "N procedural skipped")
```
Then confirm the couriers landed in the catalog:
```bash
sqlite3 data/spacemolt-knowledge.db \
 "SELECT id, procedural FROM mission_templates WHERE procedural = 1 LIMIT 10;"
```
Expect the `smuggling_courier_*` / `trade_*` synthetic-id rows with `procedural = 1`.

## Notes for later parts (out of scope here)
- Part 2 (enable `category=smuggling` selection) and Part 3 (level-2 bootstrap) are separate plans.
- The captured procedural rows are what Part 3's bootstrap queries to find the nearest chain-entry / courier stations.
