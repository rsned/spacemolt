# Mission Catalog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist a catalog of distinct mission templates seen at mission boards, detect field-level changes between sightings, and expose an `update_missions` command in `play_as` that is wired into `update_all`.

**Architecture:** Add SQLite migration 28 that drops/recreates `mission_templates` + `mission_objectives` with the full `MissionBoardEntry` field set and adds a new `mission_template_locations` child table. Replace the unused `StoreMissionTemplates`/`GetMissionTemplates` API with a new `UpsertMissionTemplate` method on `knowledge.Base` that returns a diff list when a stored row's fields differ from the observed entry. Implement for both `SQLiteKB` and `MemoryKB`. Add a `kbUpdateMissions` function and dispatcher case in `cmd/tools/play_as`, wired into `kbUpdateAll`.

**Tech Stack:** Go 1.24, `modernc.org/sqlite`, existing `pkg/knowledge` package, existing `pkg/game/serverapi.MissionBoardEntry` types.

**Spec:** `docs/superpowers/specs/2026-04-13-mission-catalog-design.md`

---

## File Structure

**Modify:**
- `pkg/knowledge/sqlite_migrations.go` — add migration 28.
- `pkg/knowledge/base.go` — replace old mission API with `UpsertMissionTemplate`; add `MissionUpsertResult` / `MissionFieldDiff` types.
- `pkg/knowledge/catalog.go` — remove `MissionTemplate` / `MissionObjectiveRecord` structs.
- `pkg/knowledge/memory.go` — remove `StoreMissionTemplates`/`GetMissionTemplates`, remove `missionsByBase` map, add new catalog map + `UpsertMissionTemplate`.
- `pkg/knowledge/sqlite_player.go` — remove `StoreMissionTemplates`/`GetMissionTemplates`.
- `pkg/knowledge/memory_catalog_test.go` — remove `TestMemoryKB_StoreMissionTemplates`.
- `pkg/knowledge/sqlite_player_test.go` — remove `TestSQLiteKB_StoreMissionTemplates*` tests.
- `cmd/tools/play_as/kb_update.go` — add `kbUpdateMissions`; wire into `kbUpdateAll`.
- `cmd/tools/play_as/main.go` — add `update_missions` dispatcher case + help text line.

**Create:**
- `pkg/knowledge/mission_catalog.go` — new file housing the shared mission-catalog types, JSON helpers, and diff logic used by both backends.
- `pkg/knowledge/sqlite_mission.go` — new file housing the SQLite `UpsertMissionTemplate` implementation.
- `pkg/knowledge/mission_catalog_test.go` — new shared test file driving both `MemoryKB` and a temporary `SQLiteKB` through identical scenarios via a helper.
- `pkg/knowledge/testdata/get_missions.json` — copy of `data/game-api/20260411/get_missions.json` used as a fixture by the catalog test. *(Keeps tests hermetic to the `pkg/knowledge` directory.)*

---

## Task 1: Migration 28 — drop/recreate mission tables

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` — append a new migration entry after the `version: 27` entry.

- [ ] **Step 1: Add migration 28 at the end of the migrations() slice**

Locate the closing `},` of the `version: 27` migration in `pkg/knowledge/sqlite_migrations.go` and insert the following entry immediately before the slice's closing `}`.

```go
{
    version: 28,
    name:    "mission_catalog_rebuild",
    sql: `
-- Drop the legacy unused mission_templates / mission_objectives tables.
DROP TABLE IF EXISTS mission_objectives;
DROP TABLE IF EXISTS mission_templates;

-- Mission templates: hand-authored missions observed on mission boards.
-- Primary key is the stable template_id (== mission_id for unaccepted entries).
CREATE TABLE mission_templates (
    id                  TEXT PRIMARY KEY,
    title               TEXT NOT NULL,
    description         TEXT,
    type                TEXT,
    difficulty           INTEGER DEFAULT 0,
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
    rewards_skill_xp    TEXT DEFAULT '{}',
    rewards_items       TEXT DEFAULT '{}',
    requirements        TEXT DEFAULT '{}',
    required_modules    TEXT DEFAULT '[]',
    provided_items      TEXT DEFAULT '{}',
    first_seen_tick     INTEGER DEFAULT 0,
    last_seen_tick      INTEGER DEFAULT 0,
    first_seen_at       TEXT,
    last_seen_at        TEXT
);

-- Mission objectives, in declared order.
CREATE TABLE mission_objectives (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    mission_id        TEXT NOT NULL,
    sort_order        INTEGER NOT NULL DEFAULT 0,
    type              TEXT NOT NULL,
    description       TEXT,
    item_id           TEXT,
    quantity          INTEGER DEFAULT 0,
    system_id         TEXT,
    system_name       TEXT,
    target_base_id    TEXT,
    target_base_name  TEXT,
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

CREATE INDEX idx_mission_templates_type    ON mission_templates(type);
CREATE INDEX idx_mission_templates_faction ON mission_templates(faction_id);
CREATE INDEX idx_mission_objectives_mission ON mission_objectives(mission_id);
CREATE INDEX idx_mission_locations_base    ON mission_template_locations(base_id);
`,
},
```

- [ ] **Step 2: Build to ensure the file compiles**

Run: `go build ./pkg/knowledge/...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add pkg/knowledge/sqlite_migrations.go
git commit -m "feat(knowledge): migration 28 - mission catalog schema rebuild"
```

---

## Task 2: Remove legacy mission API and tests

Deletes the old `MissionTemplate` type, `StoreMissionTemplates`/`GetMissionTemplates` methods, and their tests. We will introduce the replacement in Task 3.

**Files:**
- Modify: `pkg/knowledge/catalog.go` — delete `MissionTemplate` (`catalog.go:274-292`) and `MissionObjectiveRecord` (`catalog.go:294-299`).
- Modify: `pkg/knowledge/base.go` — delete the two mission method lines (currently around `base.go:110-111`).
- Modify: `pkg/knowledge/sqlite_player.go` — delete `StoreMissionTemplates` (starts at `sqlite_player.go:377`) and `GetMissionTemplates` (starts at `sqlite_player.go:421`) through the end of `GetMissionTemplates`.
- Modify: `pkg/knowledge/memory.go` — delete `missionsByBase` field (`memory.go:35`), its initialization (`memory.go:66`), and the two method implementations (starts at `memory.go:1035`).
- Modify: `pkg/knowledge/memory_catalog_test.go` — delete `TestMemoryKB_StoreMissionTemplates` (starts at `memory_catalog_test.go:274`).
- Modify: `pkg/knowledge/sqlite_player_test.go` — delete `TestSQLiteKB_StoreMissionTemplates`, `TestSQLiteKB_GetMissionTemplates`, and `TestSQLiteKB_StoreMissionTemplates_Replaces` (starts at `sqlite_player_test.go:406` through the end of the third test).

- [ ] **Step 1: Delete `MissionTemplate` and `MissionObjectiveRecord` from `catalog.go`**

Open `pkg/knowledge/catalog.go`, remove the entire `// MissionTemplate represents ...` block through the end of `MissionObjectiveRecord`.

- [ ] **Step 2: Delete the two interface lines from `base.go`**

In `pkg/knowledge/base.go`, remove:
```go
StoreMissionTemplates(ctx context.Context, baseID string, missions []MissionTemplate) error
GetMissionTemplates(ctx context.Context, baseID string) ([]MissionTemplate, error)
```

- [ ] **Step 3: Delete the SQLite implementations**

In `pkg/knowledge/sqlite_player.go`, delete both `StoreMissionTemplates` and `GetMissionTemplates` in full. If the `encoding/json` import becomes unused after this deletion, the compile step below will tell us — leave it for now and let the compiler guide it.

- [ ] **Step 4: Delete the in-memory implementations**

In `pkg/knowledge/memory.go`:
- Remove the field `missionsByBase map[string][]MissionTemplate` from the `MemoryKB` struct.
- Remove the initialization line `missionsByBase: make(map[string][]MissionTemplate),` from `NewMemoryKB`.
- Delete both method bodies (`StoreMissionTemplates`, `GetMissionTemplates`).

- [ ] **Step 5: Delete the tests**

- In `pkg/knowledge/memory_catalog_test.go`, delete `TestMemoryKB_StoreMissionTemplates`.
- In `pkg/knowledge/sqlite_player_test.go`, delete `TestSQLiteKB_StoreMissionTemplates`, `TestSQLiteKB_GetMissionTemplates`, and `TestSQLiteKB_StoreMissionTemplates_Replaces`. Also delete any test-only imports that become unused.

- [ ] **Step 6: Verify the package builds cleanly**

Run: `go build ./pkg/knowledge/... && go vet ./pkg/knowledge/...`
Expected: no errors. If the compiler reports an unused import in either file, remove it.

- [ ] **Step 7: Run the remaining knowledge tests**

Run: `go test ./pkg/knowledge/...`
Expected: PASS with fewer tests than before.

- [ ] **Step 8: Commit**

```bash
git add pkg/knowledge/
git commit -m "refactor(knowledge): remove unused mission_templates API"
```

---

## Task 3: Add catalog types + diffing helpers

Creates the shared file with public types, a `missionCatalogRow` internal struct (the normalized form used for both in-memory and SQLite storage), JSON marshal helpers, and a pure-function diff routine.

**Files:**
- Create: `pkg/knowledge/mission_catalog.go`
- Test: `pkg/knowledge/mission_catalog_test.go` (diffing unit tests — full-stack tests come in Task 6)

- [ ] **Step 1: Write the failing diff test**

Create `pkg/knowledge/mission_catalog_test.go` with the following content:

```go
package knowledge

import (
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestDiffMissionRows_NoChange(t *testing.T) {
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Type:       "mining",
		Title:      "Iron Supply Run",
		Difficulty: 1,
	}
	row := missionRowFromEntry(entry)
	diffs := diffMissionRows(row, row)
	if len(diffs) != 0 {
		t.Fatalf("expected no diffs, got %+v", diffs)
	}
}

func TestDiffMissionRows_TitleChanged(t *testing.T) {
	a := missionRowFromEntry(serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	})
	b := missionRowFromEntry(serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Run (updated)",
	})
	diffs := diffMissionRows(a, b)
	if len(diffs) != 1 || diffs[0].Field != "title" {
		t.Fatalf("expected single title diff, got %+v", diffs)
	}
	if diffs[0].OldValue != "Iron Supply Run" || diffs[0].NewValue != "Iron Run (updated)" {
		t.Fatalf("unexpected diff values: %+v", diffs[0])
	}
}

func TestDiffMissionRows_ObjectivesChanged(t *testing.T) {
	a := missionRowFromEntry(serverapi.MissionBoardEntry{
		MissionID:  "m",
		TemplateID: "m",
		Title:      "t",
		Objectives: []serverapi.MissionObjective{
			{Type: "mine_resource", Description: "Mine 30 iron", ItemID: "iron_ore", Quantity: 30},
		},
	})
	b := missionRowFromEntry(serverapi.MissionBoardEntry{
		MissionID:  "m",
		TemplateID: "m",
		Title:      "t",
		Objectives: []serverapi.MissionObjective{
			{Type: "mine_resource", Description: "Mine 40 iron", ItemID: "iron_ore", Quantity: 40},
		},
	})
	diffs := diffMissionRows(a, b)
	if len(diffs) != 1 || diffs[0].Field != "objectives" {
		t.Fatalf("expected single objectives diff, got %+v", diffs)
	}
}

func TestMissionRowFromEntry_EncodesMaps(t *testing.T) {
	entry := serverapi.MissionBoardEntry{
		MissionID:  "m",
		TemplateID: "m",
		Title:      "t",
		Rewards: &serverapi.MissionRewards{
			Credits: 1000,
			SkillXP: map[string]int{"mining": 15},
			Items:   map[string]int{"fuel": 5},
		},
	}
	row := missionRowFromEntry(entry)
	if row.RewardsCredits != 1000 {
		t.Fatalf("credits: got %d", row.RewardsCredits)
	}
	var xp map[string]int
	if err := jsonUnmarshalString(row.RewardsSkillXP, &xp); err != nil {
		t.Fatalf("unmarshal xp: %v", err)
	}
	if !reflect.DeepEqual(xp, map[string]int{"mining": 15}) {
		t.Fatalf("xp mismatch: %+v", xp)
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

Run: `go test ./pkg/knowledge/ -run TestDiffMissionRows -count=1`
Expected: FAIL with `undefined: missionRowFromEntry` / `undefined: diffMissionRows` / `undefined: jsonUnmarshalString`.

- [ ] **Step 3: Create `pkg/knowledge/mission_catalog.go`**

Create the file with the following content:

```go
package knowledge

import (
	"encoding/json"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// MissionFieldDiff records one field whose value changed between the stored
// catalog row and a newly-observed entry.
type MissionFieldDiff struct {
	Field    string
	OldValue string
	NewValue string
}

// MissionUpsertResult summarizes the outcome of UpsertMissionTemplate.
//
//   - Inserted is true when a brand new template_id was stored.
//   - Diffs is non-empty when an existing row had different values; the row
//     is always updated to the new values before returning.
type MissionUpsertResult struct {
	Inserted bool
	Diffs    []MissionFieldDiff
}

// missionCatalogRow is the normalized, backend-agnostic representation of a
// mission template as stored in the catalog. JSON blobs are kept as their
// canonical string form so diffing is a trivial string comparison.
type missionCatalogRow struct {
	ID              string
	Title           string
	Description     string
	Type            string
	Difficulty      int
	GiverName       string
	GiverTitle      string
	FactionID       string
	FactionName     string
	DialogOffer     string
	DialogAccept    string
	DialogDecline   string
	DialogComplete  string
	ChainNext       string
	Repeatable      bool
	ExpiresInTicks  int
	RewardsCredits  int
	RewardsSkillXP  string // JSON object
	RewardsItems    string // JSON object
	Requirements    string // JSON object
	RequiredModules string // JSON array
	ProvidedItems   string // JSON object
	Objectives      string // JSON array of objectiveRow
}

// objectiveRow is the catalog-side representation of a mission objective.
// Stored as JSON inside missionCatalogRow.Objectives for diffing; expanded
// into the mission_objectives table on SQLite writes.
type objectiveRow struct {
	SortOrder      int    `json:"sort_order"`
	Type           string `json:"type"`
	Description    string `json:"description,omitempty"`
	ItemID         string `json:"item_id,omitempty"`
	Quantity       int    `json:"quantity,omitempty"`
	SystemID       string `json:"system_id,omitempty"`
	SystemName     string `json:"system_name,omitempty"`
	TargetBaseID   string `json:"target_base_id,omitempty"`
	TargetBaseName string `json:"target_base_name,omitempty"`
}

// jsonMarshalString marshals v, returning "{}" / "[]" for nil maps/slices
// so comparisons remain stable.
func jsonMarshalString(v any, empty string) string {
	if v == nil {
		return empty
	}
	b, err := json.Marshal(v)
	if err != nil {
		return empty
	}
	return string(b)
}

// jsonUnmarshalString is a thin wrapper for tests.
func jsonUnmarshalString(s string, v any) error {
	if s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}

// missionRowFromEntry converts a game-protocol MissionBoardEntry to the
// catalog row form. Callers are responsible for filtering out entries with
// empty TemplateID before calling this.
func missionRowFromEntry(e serverapi.MissionBoardEntry) missionCatalogRow {
	row := missionCatalogRow{
		ID:              e.TemplateID,
		Title:           e.Title,
		Description:     e.Description,
		Type:            e.Type,
		Difficulty:      e.Difficulty,
		GiverName:       e.Giver.Name,
		GiverTitle:      e.Giver.Title,
		FactionID:       e.FactionID,
		FactionName:     e.FactionName,
		ChainNext:       e.ChainNext,
		Repeatable:      e.Repeatable,
		ExpiresInTicks:  e.ExpiresInTicks,
		RewardsSkillXP:  "{}",
		RewardsItems:    "{}",
		Requirements:    "{}",
		RequiredModules: "[]",
		ProvidedItems:   "{}",
	}
	if e.Dialog != nil {
		row.DialogOffer = e.Dialog.Offer
		row.DialogAccept = e.Dialog.Accept
		row.DialogDecline = e.Dialog.Decline
		row.DialogComplete = e.Dialog.Complete
	}
	if e.Rewards != nil {
		row.RewardsCredits = e.Rewards.Credits
		row.RewardsSkillXP = jsonMarshalString(e.Rewards.SkillXP, "{}")
		row.RewardsItems = jsonMarshalString(e.Rewards.Items, "{}")
	}
	if e.Requirements != nil {
		row.Requirements = jsonMarshalString(e.Requirements, "{}")
	}
	if len(e.RequiredModules) > 0 {
		row.RequiredModules = jsonMarshalString(e.RequiredModules, "[]")
	}
	if len(e.ProvidedItems) > 0 {
		row.ProvidedItems = jsonMarshalString(e.ProvidedItems, "{}")
	}
	objs := make([]objectiveRow, len(e.Objectives))
	for i, o := range e.Objectives {
		objs[i] = objectiveRow{
			SortOrder:      i,
			Type:           o.Type,
			Description:    o.Description,
			ItemID:         o.ItemID,
			Quantity:       o.Quantity,
			SystemID:       o.SystemID,
			SystemName:     o.SystemName,
			TargetBaseID:   o.TargetBaseID,
			TargetBaseName: o.TargetBaseName,
		}
	}
	row.Objectives = jsonMarshalString(objs, "[]")
	return row
}

// objectivesFromRow decodes the JSON-encoded objectives list. Used by the
// SQLite backend to populate mission_objectives rows.
func objectivesFromRow(row missionCatalogRow) []objectiveRow {
	var out []objectiveRow
	_ = json.Unmarshal([]byte(row.Objectives), &out)
	return out
}

// diffMissionRows returns the list of fields that differ between old and new.
// A nil-or-empty return means the rows are equal for catalog purposes.
func diffMissionRows(old, new missionCatalogRow) []MissionFieldDiff {
	var diffs []MissionFieldDiff
	add := func(field, o, n string) {
		if o != n {
			diffs = append(diffs, MissionFieldDiff{Field: field, OldValue: o, NewValue: n})
		}
	}
	addInt := func(field string, o, n int) {
		if o != n {
			diffs = append(diffs, MissionFieldDiff{
				Field:    field,
				OldValue: itoa(o),
				NewValue: itoa(n),
			})
		}
	}
	addBool := func(field string, o, n bool) {
		if o != n {
			diffs = append(diffs, MissionFieldDiff{
				Field:    field,
				OldValue: btoa(o),
				NewValue: btoa(n),
			})
		}
	}

	add("title", old.Title, new.Title)
	add("description", old.Description, new.Description)
	add("type", old.Type, new.Type)
	addInt("difficulty", old.Difficulty, new.Difficulty)
	add("giver_name", old.GiverName, new.GiverName)
	add("giver_title", old.GiverTitle, new.GiverTitle)
	add("faction_id", old.FactionID, new.FactionID)
	add("faction_name", old.FactionName, new.FactionName)
	add("dialog_offer", old.DialogOffer, new.DialogOffer)
	add("dialog_accept", old.DialogAccept, new.DialogAccept)
	add("dialog_decline", old.DialogDecline, new.DialogDecline)
	add("dialog_complete", old.DialogComplete, new.DialogComplete)
	add("chain_next", old.ChainNext, new.ChainNext)
	addBool("repeatable", old.Repeatable, new.Repeatable)
	addInt("expires_in_ticks", old.ExpiresInTicks, new.ExpiresInTicks)
	addInt("rewards_credits", old.RewardsCredits, new.RewardsCredits)
	add("rewards_skill_xp", old.RewardsSkillXP, new.RewardsSkillXP)
	add("rewards_items", old.RewardsItems, new.RewardsItems)
	add("requirements", old.Requirements, new.Requirements)
	add("required_modules", old.RequiredModules, new.RequiredModules)
	add("provided_items", old.ProvidedItems, new.ProvidedItems)
	add("objectives", old.Objectives, new.Objectives)

	return diffs
}

func itoa(n int) string {
	// strconv via fmt would be overkill; use a tiny loop.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func btoa(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
```

- [ ] **Step 4: Run the test and verify it passes**

Run: `go test ./pkg/knowledge/ -run 'TestDiffMissionRows|TestMissionRowFromEntry' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/knowledge/mission_catalog.go pkg/knowledge/mission_catalog_test.go
git commit -m "feat(knowledge): mission catalog types and diffing helpers"
```

---

## Task 4: Add `UpsertMissionTemplate` to the Base interface and MemoryKB

Defines the public API on `Base` and implements it for the in-memory backend. The SQLite implementation follows in Task 5.

**Files:**
- Modify: `pkg/knowledge/base.go` — add the interface method.
- Modify: `pkg/knowledge/memory.go` — add field, initialization, method.
- Modify: `pkg/knowledge/mission_catalog_test.go` — add MemoryKB round-trip tests.

- [ ] **Step 1: Write the failing MemoryKB test**

Append these tests to `pkg/knowledge/mission_catalog_test.go`:

```go
func TestMemoryKB_UpsertMissionTemplate_Insert(t *testing.T) {
	kb := NewMemoryKB()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
		Type:       "mining",
	}
	res, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !res.Inserted || len(res.Diffs) != 0 {
		t.Fatalf("expected insert with no diffs, got %+v", res)
	}
}

func TestMemoryKB_UpsertMissionTemplate_UnchangedReinsert(t *testing.T) {
	kb := NewMemoryKB()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	res, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 200)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if res.Inserted {
		t.Fatalf("expected Inserted=false on second call")
	}
	if len(res.Diffs) != 0 {
		t.Fatalf("expected no diffs, got %+v", res.Diffs)
	}
}

func TestMemoryKB_UpsertMissionTemplate_ChangedTitle(t *testing.T) {
	kb := NewMemoryKB()
	ctx := t.Context()
	original := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, original, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	changed := original
	changed.Title = "Iron Run (updated)"
	res, err := kb.UpsertMissionTemplate(ctx, changed, "grand_exchange_station", "haven", 200)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if res.Inserted {
		t.Fatalf("expected Inserted=false")
	}
	if len(res.Diffs) != 1 || res.Diffs[0].Field != "title" {
		t.Fatalf("expected title diff, got %+v", res.Diffs)
	}
}

func TestMemoryKB_UpsertMissionTemplate_SecondLocation(t *testing.T) {
	kb := NewMemoryKB()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first: %v", err)
	}
	res, err := kb.UpsertMissionTemplate(ctx, entry, "market_prime_exchange", "market_prime", 200)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.Inserted || len(res.Diffs) != 0 {
		t.Fatalf("expected unchanged re-sighting at a new base, got %+v", res)
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./pkg/knowledge/ -run TestMemoryKB_UpsertMissionTemplate -count=1`
Expected: FAIL with `kb.UpsertMissionTemplate undefined`.

- [ ] **Step 3: Add the method to the Base interface**

In `pkg/knowledge/base.go`, add an import for `"github.com/rsned/spacemolt/pkg/game/serverapi"` if it isn't already present, then add this line inside the `Base` interface block (place it in the "Player state" area, next to the removed StoreMissionTemplates line):

```go
// Mission catalog: stores a global catalog of hand-authored mission templates
// observed at mission boards, keyed by template_id. Returns diffs when an
// existing row's catalog fields have changed.
UpsertMissionTemplate(
    ctx context.Context,
    entry serverapi.MissionBoardEntry,
    baseID, systemID string,
    tick int64,
) (*MissionUpsertResult, error)
```

- [ ] **Step 4: Add the in-memory backing store**

In `pkg/knowledge/memory.go`:

1. Add imports (if not present): `"time"` is already imported; add `"github.com/rsned/spacemolt/pkg/game/serverapi"`.
2. Add to the `MemoryKB` struct (anywhere in the declaration block):

```go
missionCatalog   map[string]missionCatalogRow                  // template_id -> row
missionLocations map[string]map[string]missionLocationRecord   // template_id -> base_id -> record
```

3. In the same file, add this type near the top of the file (below the struct block):

```go
type missionLocationRecord struct {
    BaseID        string
    SystemID      string
    FirstSeenTick int64
    LastSeenTick  int64
    FirstSeenAt   time.Time
    LastSeenAt    time.Time
}
```

4. In `NewMemoryKB`, add:

```go
missionCatalog:   make(map[string]missionCatalogRow),
missionLocations: make(map[string]map[string]missionLocationRecord),
```

- [ ] **Step 5: Implement `UpsertMissionTemplate` on MemoryKB**

Append this method to `pkg/knowledge/memory.go`:

```go
// UpsertMissionTemplate implements Base.
func (kb *MemoryKB) UpsertMissionTemplate(
	ctx context.Context,
	entry serverapi.MissionBoardEntry,
	baseID, systemID string,
	tick int64,
) (*MissionUpsertResult, error) {
	_ = ctx
	kb.mu.Lock()
	defer kb.mu.Unlock()

	row := missionRowFromEntry(entry)
	res := &MissionUpsertResult{}
	now := time.Now().UTC()

	if existing, ok := kb.missionCatalog[row.ID]; ok {
		if diffs := diffMissionRows(existing, row); len(diffs) > 0 {
			res.Diffs = diffs
			kb.missionCatalog[row.ID] = row
		}
	} else {
		res.Inserted = true
		kb.missionCatalog[row.ID] = row
	}

	locs := kb.missionLocations[row.ID]
	if locs == nil {
		locs = make(map[string]missionLocationRecord)
		kb.missionLocations[row.ID] = locs
	}
	loc, ok := locs[baseID]
	if !ok {
		loc = missionLocationRecord{
			BaseID:        baseID,
			SystemID:      systemID,
			FirstSeenTick: tick,
			FirstSeenAt:   now,
		}
	}
	loc.LastSeenTick = tick
	loc.LastSeenAt = now
	locs[baseID] = loc

	return res, nil
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./pkg/knowledge/ -run 'TestMemoryKB_UpsertMissionTemplate|TestDiffMissionRows|TestMissionRowFromEntry' -count=1`
Expected: PASS.

- [ ] **Step 7: Run the full knowledge suite to confirm nothing else broke**

Run: `go test ./pkg/knowledge/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/knowledge/base.go pkg/knowledge/memory.go pkg/knowledge/mission_catalog_test.go
git commit -m "feat(knowledge): UpsertMissionTemplate memory implementation"
```

---

## Task 5: SQLite implementation of `UpsertMissionTemplate`

**Files:**
- Create: `pkg/knowledge/sqlite_mission.go`
- Modify: `pkg/knowledge/mission_catalog_test.go` — add a matching SQLite test that exercises the same scenarios against a temp DB.

- [ ] **Step 1: Write the failing SQLite test**

Append to `pkg/knowledge/mission_catalog_test.go`:

```go
func newTestSQLiteKB(t *testing.T) *SQLiteKB {
	t.Helper()
	path := t.TempDir() + "/test.db"
	kb, err := NewSQLiteKB(path)
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	return kb
}

func TestSQLiteKB_UpsertMissionTemplate_InsertAndReinsert(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
		Type:       "mining",
		Difficulty: 1,
		Objectives: []serverapi.MissionObjective{
			{Type: "mine_resource", Description: "Mine 30 iron", ItemID: "iron_ore", Quantity: 30},
		},
		Rewards: &serverapi.MissionRewards{
			Credits: 1500,
			SkillXP: map[string]int{"mining": 15},
		},
	}

	res, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100)
	if err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	if !res.Inserted || len(res.Diffs) != 0 {
		t.Fatalf("expected insert with no diffs, got %+v", res)
	}

	res2, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 200)
	if err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	if res2.Inserted || len(res2.Diffs) != 0 {
		t.Fatalf("expected unchanged re-insert, got %+v", res2)
	}
}

func TestSQLiteKB_UpsertMissionTemplate_DetectsChanges(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first: %v", err)
	}
	entry.Title = "Iron Run (updated)"
	entry.Objectives = []serverapi.MissionObjective{
		{Type: "mine_resource", Description: "Mine 40 iron", ItemID: "iron_ore", Quantity: 40},
	}
	res, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 200)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.Inserted {
		t.Fatalf("expected update, not insert")
	}
	fields := map[string]bool{}
	for _, d := range res.Diffs {
		fields[d.Field] = true
	}
	if !fields["title"] || !fields["objectives"] {
		t.Fatalf("expected title and objectives diffs, got %+v", res.Diffs)
	}

	// Verify title persisted.
	var stored string
	row := kb.db.QueryRow("SELECT title FROM mission_templates WHERE id = ?", "iron_supply_run")
	if err := row.Scan(&stored); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stored != "Iron Run (updated)" {
		t.Fatalf("title not persisted, got %q", stored)
	}

	// Verify objectives were replaced.
	var count int
	if err := kb.db.QueryRow("SELECT COUNT(*) FROM mission_objectives WHERE mission_id = ?", "iron_supply_run").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 objective row, got %d", count)
	}
}

func TestSQLiteKB_UpsertMissionTemplate_SecondLocation(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID:  "iron_supply_run",
		TemplateID: "iron_supply_run",
		Title:      "Iron Supply Run",
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "market_prime_exchange", "market_prime", 200); err != nil {
		t.Fatalf("second: %v", err)
	}
	var count int
	if err := kb.db.QueryRow("SELECT COUNT(*) FROM mission_template_locations WHERE mission_id = ?", "iron_supply_run").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 location rows, got %d", count)
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./pkg/knowledge/ -run TestSQLiteKB_UpsertMissionTemplate -count=1`
Expected: FAIL — method undefined.

- [ ] **Step 3: Create `pkg/knowledge/sqlite_mission.go`**

```go
package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// UpsertMissionTemplate implements Base for the SQLite backend.
func (kb *SQLiteKB) UpsertMissionTemplate(
	ctx context.Context,
	entry serverapi.MissionBoardEntry,
	baseID, systemID string,
	tick int64,
) (*MissionUpsertResult, error) {
	row := missionRowFromEntry(entry)
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	existing, existed, err := loadMissionRow(ctx, tx, row.ID)
	if err != nil {
		return nil, err
	}

	res := &MissionUpsertResult{}
	if existed {
		diffs := diffMissionRows(existing, row)
		if len(diffs) > 0 {
			res.Diffs = diffs
			if err := updateMissionRow(ctx, tx, row, tick, now); err != nil {
				return nil, err
			}
			if err := replaceMissionObjectives(ctx, tx, row); err != nil {
				return nil, err
			}
		} else {
			// Still bump last_seen_tick / last_seen_at.
			if _, err := tx.ExecContext(ctx, `
				UPDATE mission_templates
				SET last_seen_tick = ?, last_seen_at = ?
				WHERE id = ?
			`, tick, now, row.ID); err != nil {
				return nil, fmt.Errorf("touch mission: %w", err)
			}
		}
	} else {
		res.Inserted = true
		if err := insertMissionRow(ctx, tx, row, tick, now); err != nil {
			return nil, err
		}
		if err := replaceMissionObjectives(ctx, tx, row); err != nil {
			return nil, err
		}
	}

	if err := upsertMissionLocation(ctx, tx, row.ID, baseID, systemID, tick, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

func loadMissionRow(ctx context.Context, tx *sql.Tx, id string) (missionCatalogRow, bool, error) {
	var r missionCatalogRow
	var repeatableInt int
	err := tx.QueryRowContext(ctx, `
		SELECT id, title, COALESCE(description, ''), COALESCE(type, ''), difficulty,
		       COALESCE(giver_name, ''), COALESCE(giver_title, ''),
		       COALESCE(faction_id, ''), COALESCE(faction_name, ''),
		       COALESCE(dialog_offer, ''), COALESCE(dialog_accept, ''),
		       COALESCE(dialog_decline, ''), COALESCE(dialog_complete, ''),
		       COALESCE(chain_next, ''), repeatable, expires_in_ticks,
		       rewards_credits, rewards_skill_xp, rewards_items,
		       requirements, required_modules, provided_items
		FROM mission_templates WHERE id = ?
	`, id).Scan(
		&r.ID, &r.Title, &r.Description, &r.Type, &r.Difficulty,
		&r.GiverName, &r.GiverTitle,
		&r.FactionID, &r.FactionName,
		&r.DialogOffer, &r.DialogAccept, &r.DialogDecline, &r.DialogComplete,
		&r.ChainNext, &repeatableInt, &r.ExpiresInTicks,
		&r.RewardsCredits, &r.RewardsSkillXP, &r.RewardsItems,
		&r.Requirements, &r.RequiredModules, &r.ProvidedItems,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("select mission: %w", err)
	}
	r.Repeatable = repeatableInt != 0

	// Rebuild Objectives JSON from mission_objectives rows so the diff
	// comparison works symmetrically with missionRowFromEntry.
	rows, err := tx.QueryContext(ctx, `
		SELECT sort_order, type, COALESCE(description, ''),
		       COALESCE(item_id, ''), quantity,
		       COALESCE(system_id, ''), COALESCE(system_name, ''),
		       COALESCE(target_base_id, ''), COALESCE(target_base_name, '')
		FROM mission_objectives WHERE mission_id = ? ORDER BY sort_order
	`, id)
	if err != nil {
		return r, false, fmt.Errorf("query objectives: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var objs []objectiveRow
	for rows.Next() {
		var o objectiveRow
		if err := rows.Scan(
			&o.SortOrder, &o.Type, &o.Description,
			&o.ItemID, &o.Quantity,
			&o.SystemID, &o.SystemName,
			&o.TargetBaseID, &o.TargetBaseName,
		); err != nil {
			return r, false, fmt.Errorf("scan objective: %w", err)
		}
		objs = append(objs, o)
	}
	r.Objectives = jsonMarshalString(objs, "[]")
	return r, true, nil
}

func insertMissionRow(ctx context.Context, tx *sql.Tx, r missionCatalogRow, tick int64, now string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO mission_templates (
			id, title, description, type, difficulty,
			giver_name, giver_title, faction_id, faction_name,
			dialog_offer, dialog_accept, dialog_decline, dialog_complete,
			chain_next, repeatable, expires_in_ticks,
			rewards_credits, rewards_skill_xp, rewards_items,
			requirements, required_modules, provided_items,
			first_seen_tick, last_seen_tick, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, r.Title, r.Description, r.Type, r.Difficulty,
		r.GiverName, r.GiverTitle, r.FactionID, r.FactionName,
		r.DialogOffer, r.DialogAccept, r.DialogDecline, r.DialogComplete,
		r.ChainNext, boolToInt(r.Repeatable), r.ExpiresInTicks,
		r.RewardsCredits, r.RewardsSkillXP, r.RewardsItems,
		r.Requirements, r.RequiredModules, r.ProvidedItems,
		tick, tick, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert mission: %w", err)
	}
	return nil
}

func updateMissionRow(ctx context.Context, tx *sql.Tx, r missionCatalogRow, tick int64, now string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE mission_templates SET
			title = ?, description = ?, type = ?, difficulty = ?,
			giver_name = ?, giver_title = ?, faction_id = ?, faction_name = ?,
			dialog_offer = ?, dialog_accept = ?, dialog_decline = ?, dialog_complete = ?,
			chain_next = ?, repeatable = ?, expires_in_ticks = ?,
			rewards_credits = ?, rewards_skill_xp = ?, rewards_items = ?,
			requirements = ?, required_modules = ?, provided_items = ?,
			last_seen_tick = ?, last_seen_at = ?
		WHERE id = ?
	`,
		r.Title, r.Description, r.Type, r.Difficulty,
		r.GiverName, r.GiverTitle, r.FactionID, r.FactionName,
		r.DialogOffer, r.DialogAccept, r.DialogDecline, r.DialogComplete,
		r.ChainNext, boolToInt(r.Repeatable), r.ExpiresInTicks,
		r.RewardsCredits, r.RewardsSkillXP, r.RewardsItems,
		r.Requirements, r.RequiredModules, r.ProvidedItems,
		tick, now, r.ID,
	)
	if err != nil {
		return fmt.Errorf("update mission: %w", err)
	}
	return nil
}

func replaceMissionObjectives(ctx context.Context, tx *sql.Tx, r missionCatalogRow) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM mission_objectives WHERE mission_id = ?`, r.ID); err != nil {
		return fmt.Errorf("delete objectives: %w", err)
	}
	for _, o := range objectivesFromRow(r) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mission_objectives (
				mission_id, sort_order, type, description,
				item_id, quantity, system_id, system_name,
				target_base_id, target_base_name
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			r.ID, o.SortOrder, o.Type, o.Description,
			o.ItemID, o.Quantity, o.SystemID, o.SystemName,
			o.TargetBaseID, o.TargetBaseName,
		); err != nil {
			return fmt.Errorf("insert objective: %w", err)
		}
	}
	return nil
}

func upsertMissionLocation(ctx context.Context, tx *sql.Tx, missionID, baseID, systemID string, tick int64, now string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO mission_template_locations (
			mission_id, base_id, system_id,
			first_seen_tick, last_seen_tick, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(mission_id, base_id) DO UPDATE SET
			last_seen_tick = excluded.last_seen_tick,
			last_seen_at   = excluded.last_seen_at,
			system_id      = excluded.system_id
	`, missionID, baseID, systemID, tick, tick, now, now)
	if err != nil {
		return fmt.Errorf("upsert location: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Run the SQLite tests**

Run: `go test ./pkg/knowledge/ -run TestSQLiteKB_UpsertMissionTemplate -count=1`
Expected: PASS.

- [ ] **Step 5: Run the full knowledge suite**

Run: `go test ./pkg/knowledge/...`
Expected: PASS.

- [ ] **Step 6: Build the whole repo to confirm no external consumers broke**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add pkg/knowledge/sqlite_mission.go pkg/knowledge/mission_catalog_test.go
git commit -m "feat(knowledge): UpsertMissionTemplate SQLite implementation"
```

---

## Task 6: Fixture test with the real `get_missions.json` payload

Loads the captured 2026-04-11 payload and asserts the catalog absorbs hand-authored missions while skipping procedural ones.

**Files:**
- Create: `pkg/knowledge/testdata/get_missions.json` — copy of `data/game-api/20260411/get_missions.json`.
- Modify: `pkg/knowledge/mission_catalog_test.go` — add fixture test.

- [ ] **Step 1: Copy the fixture into testdata**

Run:
```bash
mkdir -p pkg/knowledge/testdata
cp data/game-api/20260411/get_missions.json pkg/knowledge/testdata/get_missions.json
```

- [ ] **Step 2: Write the failing fixture test**

Append to `pkg/knowledge/mission_catalog_test.go`:

```go
import (
	// add at top of file if not yet present
	_ "embed"
	"encoding/json"
	"os"
)

func TestUpsertMissionTemplate_RealFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/get_missions.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var resp serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.BaseID == "" {
		t.Fatalf("fixture missing base_id")
	}

	kb := NewMemoryKB()
	ctx := t.Context()

	var inserted, skipped int
	for _, entry := range resp.Missions {
		if entry.TemplateID == "" {
			skipped++
			continue
		}
		res, err := kb.UpsertMissionTemplate(ctx, entry, resp.BaseID, "haven", 100)
		if err != nil {
			t.Fatalf("upsert %s: %v", entry.MissionID, err)
		}
		if res.Inserted {
			inserted++
		}
	}
	if inserted == 0 {
		t.Fatalf("expected at least one non-procedural mission to be inserted")
	}
	if skipped == 0 {
		t.Fatalf("expected at least one procedural mission to be skipped")
	}

	// Re-run and confirm everything is stable (no diffs, no inserts).
	for _, entry := range resp.Missions {
		if entry.TemplateID == "" {
			continue
		}
		res, err := kb.UpsertMissionTemplate(ctx, entry, resp.BaseID, "haven", 200)
		if err != nil {
			t.Fatalf("re-upsert %s: %v", entry.MissionID, err)
		}
		if res.Inserted {
			t.Fatalf("%s: unexpected insert on re-run", entry.MissionID)
		}
		if len(res.Diffs) != 0 {
			t.Fatalf("%s: unexpected diffs on re-run: %+v", entry.MissionID, res.Diffs)
		}
	}
}
```

Note: the `_ "embed"` and `os` imports may already be absent — only add what the test actually uses (`encoding/json`, `os`). If the top of `mission_catalog_test.go` already has an import block, fold these in rather than adding a second block.

- [ ] **Step 3: Run the fixture test**

Run: `go test ./pkg/knowledge/ -run TestUpsertMissionTemplate_RealFixture -count=1 -v`
Expected: PASS, with output showing the test ran (no assertion failures).

- [ ] **Step 4: Run the full knowledge suite one more time**

Run: `go test ./pkg/knowledge/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/knowledge/testdata/get_missions.json pkg/knowledge/mission_catalog_test.go
git commit -m "test(knowledge): fixture test for mission catalog upsert"
```

---

## Task 7: `kbUpdateMissions` in play_as

**Files:**
- Modify: `cmd/tools/play_as/kb_update.go` — add `kbUpdateMissions`; call it from `kbUpdateAll`.
- Modify: `cmd/tools/play_as/main.go` — add `update_missions` dispatcher case and help text line.

- [ ] **Step 1: Add `kbUpdateMissions` to `kb_update.go`**

Append this function to `cmd/tools/play_as/kb_update.go` (below `kbUpdateFacilities`):

```go
// kbUpdateMissions fetches the mission board at the current station and upserts
// each hand-authored (non-procedural) entry into the knowledge-base mission
// catalog. Procedural missions (empty template_id) are counted and skipped.
func kbUpdateMissions(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	state := client.GetState()
	if !state.Doc {
		fmt.Println("(Not docked — skipping missions update)")
		return nil
	}

	if err := client.GetMissions(ctx); err != nil {
		return fmt.Errorf("get_missions: %w", err)
	}
	time.Sleep(game.SleepQuick)

	raw := client.GetRawJSON("missions")
	if len(raw) == 0 {
		return fmt.Errorf("get_missions returned no data")
	}

	var resp serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse get_missions response: %w", err)
	}

	baseID := resp.BaseID
	if baseID == "" {
		baseID = state.CurrentPOI
	}
	systemID := state.System.ID
	tick := currentTick(state)

	var inserted, unchanged, changed, skipped int
	for _, entry := range resp.Missions {
		if entry.TemplateID == "" {
			skipped++
			continue
		}
		res, err := globalKB.UpsertMissionTemplate(ctx, entry, baseID, systemID, tick)
		if err != nil {
			fmt.Printf("Warning: upsert %s: %v\n", entry.MissionID, err)
			continue
		}
		switch {
		case res.Inserted:
			inserted++
		case len(res.Diffs) > 0:
			changed++
			fmt.Printf("Mission template %q changed at %s:\n", entry.TemplateID, baseID)
			for _, d := range res.Diffs {
				fmt.Printf("  %s: %q -> %q\n", d.Field, d.OldValue, d.NewValue)
				fmt.Fprintf(os.Stderr, "mission template %s changed at base %s: field=%s old=%q new=%q\n",
					entry.TemplateID, baseID, d.Field, d.OldValue, d.NewValue)
			}
		default:
			unchanged++
		}
	}

	fmt.Printf("update_missions: %d new, %d unchanged, %d changed, %d procedural skipped\n",
		inserted, unchanged, changed, skipped)
	return nil
}
```

Add `"os"` to the import block in the same file if it isn't already present.

- [ ] **Step 2: Wire `kbUpdateMissions` into `kbUpdateAll`**

In `cmd/tools/play_as/kb_update.go`, inside `kbUpdateAll`, after the `kbUpdateFacilities` call inside the `if state.Doc { ... }` block, add:

```go
if err := kbUpdateMissions(client, ctx); err != nil {
    fmt.Printf("Warning: update_missions: %v\n", err)
}
```

- [ ] **Step 3: Add the dispatcher case in main.go**

In `cmd/tools/play_as/main.go`, locate the switch cases around line 3434:

```go
case "update_facilities":
    return kbUpdateFacilities(client, ctx)
case "update_all":
    return kbUpdateAll(client, ctx)
```

Insert a new case between `update_facilities` and `update_all`:

```go
case "update_missions":
    return kbUpdateMissions(client, ctx)
```

- [ ] **Step 4: Add the help text line**

In `cmd/tools/play_as/main.go` around line 3849, next to the existing `update_all` help line, add:

```go
fmt.Println("  update_missions           - Save mission board templates to KB")
```

Place it immediately above the `update_all` help line.

- [ ] **Step 5: Build the binary**

Run: `go build ./cmd/tools/play_as/...`
Expected: no errors.

- [ ] **Step 6: Lint**

Run: `golangci-lint run ./pkg/knowledge/... ./cmd/tools/play_as/...`
Expected: no new findings.

- [ ] **Step 7: Run the full test and build suite**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/tools/play_as/kb_update.go cmd/tools/play_as/main.go
git commit -m "feat(play_as): update_missions command, wired into update_all"
```

---

## Self-Review

- **Spec coverage:**
  - Migration 28 → Task 1.
  - Removal of legacy API → Task 2.
  - Types + diffing → Task 3.
  - Base interface + MemoryKB → Task 4.
  - SQLiteKB → Task 5.
  - Fixture test → Task 6.
  - `update_missions` + `update_all` wiring + help text → Task 7.
  - "No changes to `pkg/game/client.go`" → respected (Tasks 2 and 7 only touch `pkg/knowledge` and `cmd/tools/play_as`).
- **Placeholder scan:** no TBDs, no "similar to…", every code step has exact code.
- **Type consistency:** `MissionUpsertResult` / `MissionFieldDiff` / `UpsertMissionTemplate(ctx, entry, baseID, systemID, tick)` are used with the same signature and field names in every task that references them.
