# Facility Catalog Implementation Plan (Spec A1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A galaxy-wide, marketbot-populated catalog of public **production** facilities, queryable as "which stations can craft recipe_id X, at what tier and fee?" — the siting foundation for the later crafting planner (Spec A2).

**Architecture:** New `public_facilities` knowledge table (dedicated, not coupled to `bases`). The existing facility-sweep capture is extended to record the currently-dropped `public_facilities` section. A KB query returns facilities by recipe. A `where_facility` play_as command surfaces it. The 43 station-parked marketbots gain a scheduled `facilities` sweep.

**Tech Stack:** Go 1.24+, SQLite (`pkg/knowledge`), existing worker capture (`pkg/worker/capture.go`), play_as (`cmd/tools/play_as`), YAML roles config.

## Global Constraints

- Go 1.24+; modern idioms (`range`-over-int, `b.Loop()` in benchmarks) where relevant.
- Every code task gated by `go build ./...` **and** `go test ./...`. New KB interface methods break mocks in `pkg/agent`/`pkg/skills` — `go build` alone misses it (`feedback_gameclient_interface_mocks`).
- New code passes `golangci-lint` with no new findings.
- JSON Schemas (if any) use Draft 2020-12. (None expected here.)
- The captured `public_facilities` payload field names are **unconfirmed** — no live sample exists in `data/game-api/latest/`. Every parse must be defensive and preserve the raw entry in a `details_json` column so a wrong field name loses no data and is a one-line fix. Confirm names against a live `facility list` payload as the first capture step (Task 3).
- Only **production**-category, **public** facilities enter the catalog.
- Do NOT restart or drive the live fleet as part of implementation. Scheduling (Task 5) is a config change that takes effect on the marketbots' next natural restart; note that, don't force it.

---

## Task 1: `public_facilities` table migration

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` (append a new migration; use the NEXT sequential migration number — grep the file for the current highest and add one)
- Test: `pkg/knowledge/sqlite_migrations_test.go` (or the existing migrations test file)

**Interfaces:**
- Produces: table `public_facilities` with columns and index below; relied on by Tasks 2–4.

- [ ] **Step 1: Write the failing test**

Add to the migrations test (mirror how existing tables are asserted — open an in-memory/temp SQLiteKB, query `sqlite_master`):

```go
func TestPublicFacilitiesTableExists(t *testing.T) {
	kb := newTestKB(t) // use the package's existing test-KB helper
	var name string
	err := kb.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='public_facilities'`).Scan(&name)
	if err != nil || name != "public_facilities" {
		t.Fatalf("public_facilities table missing: %v", err)
	}
}
```
(If the package exposes the `*sql.DB` differently than `kb.DB()`, use that accessor — match the sibling tests.)

- [ ] **Step 2: Run it, expect failure**

Run: `go test ./pkg/knowledge/ -run TestPublicFacilitiesTableExists -v`
Expected: FAIL (no such table).

- [ ] **Step 3: Add the migration**

Append a new migration entry (next sequential number) in `pkg/knowledge/sqlite_migrations.go`:

```sql
CREATE TABLE IF NOT EXISTS public_facilities (
    station_id     TEXT NOT NULL,
    facility_id    TEXT NOT NULL,
    recipe_id      TEXT NOT NULL DEFAULT '',
    facility_name  TEXT DEFAULT '',
    category       TEXT DEFAULT '',
    level          INTEGER DEFAULT 1,
    labor_cost     INTEGER DEFAULT 0,
    owner_faction  TEXT DEFAULT '',
    public         INTEGER DEFAULT 1,
    details_json   TEXT DEFAULT '',
    last_seen_tick INTEGER DEFAULT 0,
    last_seen_utc  TEXT DEFAULT '',
    PRIMARY KEY (station_id, facility_id)
);
CREATE INDEX IF NOT EXISTS idx_public_facilities_recipe ON public_facilities(recipe_id);
```

Follow the file's existing migration format exactly (version number field, up-SQL string). Do not renumber or edit existing migrations.

- [ ] **Step 4: Run test, expect pass**

Run: `go test ./pkg/knowledge/ -run TestPublicFacilitiesTableExists -v` → PASS.

- [ ] **Step 5: Full gate + commit**

Run: `go build ./...` → clean; `go test ./pkg/knowledge/...` → PASS; `golangci-lint run ./pkg/knowledge/...` → 0 new.
```bash
git add pkg/knowledge/sqlite_migrations.go pkg/knowledge/sqlite_migrations_test.go
git commit -m "feat(kb): public_facilities table for the facility catalog

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: KB type + upsert + query

**Files:**
- Create: `pkg/knowledge/public_facilities.go`
- Test: `pkg/knowledge/public_facilities_test.go`
- Modify (only if the project exposes KB methods via the `Base` interface AND these must be on it): `pkg/knowledge/base.go` — add the two method signatures. (If callers use `*SQLiteKB` concretely, skip the interface to avoid churning mocks; prefer concrete methods unless a caller needs the interface.)

**Interfaces:**
- Produces:
  ```go
  type PublicFacility struct {
      StationID, FacilityID, RecipeID, FacilityName, Category, OwnerFaction string
      Level, LaborCost, LastSeenTick int
  }
  func (kb *SQLiteKB) UpsertPublicFacilities(ctx context.Context, rows []PublicFacility) error
  func (kb *SQLiteKB) FacilitiesForRecipe(ctx context.Context, recipeID string) ([]PublicFacility, error)
  ```
  `UpsertPublicFacilities` consumed by Task 3; `FacilitiesForRecipe` by Task 4.

- [ ] **Step 1: Write failing tests**

```go
func TestUpsertAndQueryPublicFacilities(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	rows := []PublicFacility{
		{StationID: "grand_exchange", FacilityID: "f1", RecipeID: "ceramite_plating",
			Category: "production", Level: 2, LaborCost: 40, OwnerFaction: "CRFT", LastSeenTick: 100},
		{StationID: "war_citadel", FacilityID: "f2", RecipeID: "ceramite_plating",
			Category: "production", Level: 1, LaborCost: 60, OwnerFaction: "WAR", LastSeenTick: 100},
		{StationID: "war_citadel", FacilityID: "f9", RecipeID: "reactor_core",
			Category: "production", Level: 3, LaborCost: 200, OwnerFaction: "WAR", LastSeenTick: 100},
	}
	if err := kb.UpsertPublicFacilities(ctx, rows); err != nil { t.Fatal(err) }

	got, err := kb.FacilitiesForRecipe(ctx, "ceramite_plating")
	if err != nil { t.Fatal(err) }
	if len(got) != 2 { t.Fatalf("want 2 facilities for ceramite_plating, got %d", len(got)) }

	// Upsert refresh: same PK, new level/fee overwrites.
	rows[0].Level = 4; rows[0].LaborCost = 35
	if err := kb.UpsertPublicFacilities(ctx, rows[:1]); err != nil { t.Fatal(err) }
	got, _ = kb.FacilitiesForRecipe(ctx, "ceramite_plating")
	var f1 *PublicFacility
	for i := range got { if got[i].FacilityID == "f1" { f1 = &got[i] } }
	if f1 == nil || f1.Level != 4 || f1.LaborCost != 35 {
		t.Fatalf("upsert did not refresh f1: %+v", f1)
	}
}

func TestFacilitiesForRecipeUnknownReturnsEmpty(t *testing.T) {
	kb := newTestKB(t)
	got, err := kb.FacilitiesForRecipe(context.Background(), "nonexistent")
	if err != nil { t.Fatal(err) }
	if len(got) != 0 { t.Fatalf("want empty, got %d", len(got)) }
}
```

- [ ] **Step 2: Run, expect failure** (`undefined: PublicFacility`).
Run: `go test ./pkg/knowledge/ -run 'PublicFacilit' -v` → FAIL/compile error.

- [ ] **Step 3: Implement `pkg/knowledge/public_facilities.go`**

```go
package knowledge

import "context"

type PublicFacility struct {
	StationID, FacilityID, RecipeID, FacilityName, Category, OwnerFaction string
	Level, LaborCost, LastSeenTick int
}

func (kb *SQLiteKB) UpsertPublicFacilities(ctx context.Context, rows []PublicFacility) error {
	// use kb's existing tx/exec pattern; ON CONFLICT(station_id,facility_id) DO UPDATE
	// setting recipe_id, facility_name, category, level, labor_cost, owner_faction,
	// public, details_json(if provided by caller — see Task 3), last_seen_tick/utc.
	...
}

func (kb *SQLiteKB) FacilitiesForRecipe(ctx context.Context, recipeID string) ([]PublicFacility, error) {
	// SELECT ... FROM public_facilities
	// WHERE recipe_id = ? AND public = 1 AND category = 'production'
	// ORDER BY last_seen_tick DESC
	...
}
```
Match the package's existing SQLite access idiom (how sibling methods get `*sql.DB`, build queries, scan rows, handle `context`). Filter to `public=1 AND category='production'` in `FacilitiesForRecipe`.

- [ ] **Step 4: Run tests, expect pass.**
Run: `go test ./pkg/knowledge/ -run 'PublicFacilit' -v` → PASS.

- [ ] **Step 5: Full gate + commit.**
Run: `go build ./...`; `go test ./...` (mocks!); `golangci-lint run ./pkg/knowledge/...`.
```bash
git add pkg/knowledge/public_facilities.go pkg/knowledge/public_facilities_test.go
git commit -m "feat(kb): FacilitiesForRecipe query + UpsertPublicFacilities

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Capture the `public_facilities` section

**Files:**
- Modify: `pkg/worker/capture.go` (`KBUpdateFacilities` at :564, `facilityDetail` at :550)
- Test: `pkg/worker/capture_test.go` (or a new `pkg/worker/public_facilities_capture_test.go`)

**Interfaces:**
- Consumes: `kb.UpsertPublicFacilities` (Task 2).
- Produces: after a `facility list` sweep, public production facilities are upserted into the catalog.

- [ ] **Step 1: Confirm the live payload shape FIRST**

Before coding, obtain one real `facility list` response containing a non-empty `public_facilities` array (ask the operator to run `facility list` while docked at a station that has public facilities — e.g. grand_exchange — or capture from a marketbot's raw JSON). Record the exact keys of a public-facility entry: expected `facility_id`, `recipe_id`, `level`/`tier`, `labor_cost`/fee, `owner`/`owner_faction`, `public`, `category`, `name`. Save the sample as `pkg/worker/testdata/facility_list_public.json` for the test fixture. If a field name differs from the spec's assumption, use the real name and note it.

- [ ] **Step 2: Write the failing test**

Extend the parse struct and add `PublicFacilities` handling. Test feeds the captured fixture (or a realistic synthetic one matching Step 1) through the parse+upsert path into a temp KB, then asserts rows landed:

```go
func TestCapturePublicFacilities(t *testing.T) {
	kb := newTestKB(t)              // knowledge test helper
	raw := readTestdata(t, "facility_list_public.json")
	// call the extracted parse+upsert helper directly (see Step 3) to avoid a live client:
	if err := upsertPublicFromFacilityList(context.Background(), kb, raw, /*tick*/100); err != nil {
		t.Fatal(err)
	}
	got, _ := kb.FacilitiesForRecipe(context.Background(), "ceramite_plating") // recipe present in fixture
	if len(got) == 0 { t.Fatal("expected captured public facility for ceramite_plating") }
	if got[0].StationID == "" || got[0].Level == 0 {
		t.Fatalf("public facility fields not populated: %+v", got[0])
	}
}
```

- [ ] **Step 3: Implement**

Extend `facilityDetail` (or add a `publicFacilityDetail`) with the confirmed fields: `Level int`, `LaborCost int`, `OwnerFaction string` (json tags per Step 1), plus a `public` flag. Add a testable helper that does the parse+map+upsert so the test needn't drive a live client:

```go
// upsertPublicFromFacilityList parses the public_facilities section of a raw
// `facility list` response and upserts production+public rows into the catalog.
func upsertPublicFromFacilityList(ctx context.Context, kb knowledge.Base, raw []byte, tick int) error {
	var resp struct {
		BaseID           string                   `json:"base_id"`
		PublicFacilities []map[string]any          `json:"public_facilities"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil { return err }
	var rows []knowledge.PublicFacility
	for _, m := range resp.PublicFacilities {
		// defensive field extraction w/ helpers (getString/getInt over candidate keys);
		// keep only production category; stash raw m as details_json.
		rows = append(rows, knowledge.PublicFacility{ StationID: resp.BaseID, /* ... */ })
	}
	return kb.UpsertPublicFacilities(ctx, rows)
}
```
Then call this helper from `KBUpdateFacilities` after its existing `facility list` fetch (reuse the same `client.GetRawJSON("_last")` bytes — do not issue a second `facility list`). Do NOT gate this path on `GetBase` (the public catalog must not require an observed `bases` row). Preserve the existing station/player/faction capture behavior unchanged.

- [ ] **Step 4: Run tests, expect pass.**
Run: `go test ./pkg/worker/ -run 'PublicFacilit|CapturePublic' -v` → PASS.

- [ ] **Step 5: Full gate + commit.**
Run: `go build ./...`; `go test ./...`; `golangci-lint run ./pkg/worker/...`.
```bash
git add pkg/worker/capture.go pkg/worker/*public*_test.go pkg/worker/testdata/facility_list_public.json
git commit -m "feat(worker): capture public_facilities section into the catalog

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: `where_facility <recipe>` play_as command

**Files:**
- Create: `cmd/tools/play_as/where_facility.go`
- Modify: `cmd/tools/play_as/main.go` (add a `case "where_facility":` in the command dispatch — find the existing switch, e.g. near `case "facility":` at :815), `cmd/tools/play_as/completer.go` (add `"where_facility"` to the completion list)
- Test: `cmd/tools/play_as/where_facility_test.go`

**Interfaces:**
- Consumes: `kb.FacilitiesForRecipe` (Task 2) and the existing recipe-resolution helper (id-or-name → recipe_id; reuse whatever `craftable`/`plan` commands use).

- [ ] **Step 1: Write the failing test** (format function, table output):

```go
func TestFormatWhereFacility(t *testing.T) {
	rows := []knowledge.PublicFacility{
		{StationID: "grand_exchange", Level: 2, LaborCost: 40, OwnerFaction: "CRFT", LastSeenTick: 100},
		{StationID: "war_citadel", Level: 1, LaborCost: 60, OwnerFaction: "WAR", LastSeenTick: 90},
	}
	out := formatWhereFacility("ceramite_plating", rows, /*currentTick*/120)
	for _, want := range []string{"ceramite_plating", "grand_exchange", "war_citadel", "40", "CRFT", "T2"} {
		if !strings.Contains(out, want) { t.Errorf("missing %q in:\n%s", want, out) }
	}
}

func TestFormatWhereFacilityEmpty(t *testing.T) {
	out := formatWhereFacility("reactor_core", nil, 120)
	if !strings.Contains(out, "no public facility") {
		t.Errorf("expected 'no public facility' note:\n%s", out)
	}
}
```

- [ ] **Step 2: Run, expect failure** (`undefined: formatWhereFacility`).
Run: `go test ./cmd/tools/play_as/ -run WhereFacility -v` → FAIL.

- [ ] **Step 3: Implement**

`formatWhereFacility(recipeID string, rows []knowledge.PublicFacility, currentTick int) string` — a table: station · tier (`T{level}`) · output-rate note (`×3^(level-1)`) · fee (labor_cost) · owner · staleness (`currentTick - LastSeenTick`). Empty → a clear "no public facility known for <recipe>" line. The `case "where_facility":` handler resolves the recipe arg (reuse existing resolver), calls `kb.FacilitiesForRecipe`, prints `formatWhereFacility(...)`. Match the style of sibling play_as commands (`craftable.go`, `find_item.go`).

- [ ] **Step 4: Run tests, expect pass.**
Run: `go test ./cmd/tools/play_as/ -run WhereFacility -v` → PASS.

- [ ] **Step 5: Full gate + commit.**
Run: `go build ./...`; `go test ./...`; `golangci-lint run ./cmd/tools/play_as/...`.
```bash
git add cmd/tools/play_as/where_facility.go cmd/tools/play_as/where_facility_test.go cmd/tools/play_as/main.go cmd/tools/play_as/completer.go
git commit -m "feat(play_as): where_facility <recipe> — query the facility catalog

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Schedule the `facilities` sweep on the marketbots

**Files:**
- Modify: `data/overmind/roles.yaml` (the `resident`, `resident_gas`, `resident_ice` role `schedule` blocks)
- Possibly modify: the marketbots' `data/agents/marketbot_*/schedule.json` (see Step 1)

**Interfaces:** none (config). Effect: marketbots periodically run the existing `facilities` worker command → catalog stays fresh galaxy-wide.

- [ ] **Step 1: Resolve which scheduler actually drives marketbot recurring commands**

Read `pkg/worker/standing.go:80` (role.Schedule usage) and `cmd/worker/main.go:263,273` (`LoadRoles` vs `LoadScheduler` from `data/agents/<id>/schedule.json`). Determine at runtime which one fires `kb_update`/`update_market` for a marketbot (role schedule, per-agent schedule.json, or both). Document the finding in the commit message. This decides whether editing `roles.yaml` alone is sufficient or the per-agent `schedule.json` files must also gain the entry.

- [ ] **Step 2: Add the `facilities` entry**

In `data/overmind/roles.yaml`, add to each of the `resident`, `resident_gas`, `resident_ice` roles' `schedule`:
```yaml
      - { every: hourly, command: "facilities" }
```
If Step 1 shows the per-agent `schedule.json` is authoritative (pre-seeded, not re-read from roles.yaml), add an equivalent entry to the marketbots' `schedule.json` files (a small script over `data/agents/marketbot_*/schedule.json` appending an hourly `facilities` command with a fresh id), OR document that the marketbots must be restarted to re-seed. Do NOT restart the live fleet as part of this task — note the activation step for the operator.

- [ ] **Step 3: Verify the command is valid + config parses**

The `facilities` command already exists in `pkg/worker/dispatch.go` — no code change. Verify `roles.yaml` still loads:
```bash
go test ./pkg/worker/ -run Roles -v   # if a LoadRoles test exists; else:
go run ./cmd/worker --help >/dev/null   # sanity that config wiring compiles
```
If a `LoadRoles`/roles-validation test exists, ensure it still passes with the new entry.

- [ ] **Step 4: Commit.**
```bash
git add data/overmind/roles.yaml data/agents/marketbot_*/schedule.json
git commit -m "chore(overmind): schedule hourly facilities sweep on marketbots

Populates the public_facilities catalog galaxy-wide. Activation: takes
effect on each marketbot's next restart (do not force-restart the fleet).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review notes

- **Spec coverage:** table → Task 1; capture of dropped public_facilities → Task 3; `FacilitiesForRecipe` query → Task 2; `where_facility` command → Task 4; marketbot sweep → Task 5. All spec sections mapped.
- **Unconfirmed payload shape:** handled by Task 3 Step 1 (capture a live sample first) + defensive parse + `details_json` safety net (Global Constraints). Not a placeholder — it is an explicit first step with a fallback that makes a wrong guess non-destructive.
- **Scheduler ambiguity:** Task 5 Step 1 resolves roles.yaml-vs-schedule.json at implementation rather than guessing.
- **Type consistency:** `PublicFacility`, `UpsertPublicFacilities`, `FacilitiesForRecipe`, `formatWhereFacility`, `upsertPublicFromFacilityList` used consistently across tasks.
- **Mock risk:** Tasks 2/3 keep methods on `*SQLiteKB` (concrete) unless a caller needs the `Base` interface, minimizing mock churn; `go test ./...` in every gate catches any break.
