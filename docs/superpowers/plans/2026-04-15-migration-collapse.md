# Migration Collapse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse `pkg/knowledge` SQLite migrations 1–30 into a single embedded `initial_schema` migration so every fresh-DB test runs one migration instead of thirty, bringing the `pkg/knowledge` test suite comfortably under the 120s race-mode pre-commit threshold.

**Architecture:** The full current schema lives in `pkg/knowledge/initial_schema.sql`, embedded into Go via `//go:embed`. `pkg/knowledge/sqlite_migrations.go` shrinks to ~80 lines with a single-entry `migrations()` slice. `runMigrations()` itself is unchanged — its `m.version <= currentVersion` gate correctly handles both fresh DBs (runs the collapsed v1) and existing DBs with any of versions 1–30 already recorded (skips).

**Tech Stack:** Go 1.24, `//go:embed`, `modernc.org/sqlite`, existing `scripts/sql/regenerate_initialize_database.sh`, SQLite 3.

**Spec:** `docs/superpowers/specs/2026-04-15-migration-collapse-design.md`

---

## File Structure

**Create:**
- `pkg/knowledge/initial_schema.sql` — canonical CREATE TABLE + CREATE INDEX statements for the full current schema, extracted from today's `scripts/sql/initialize_database.sql`.

**Modify:**
- `pkg/knowledge/sqlite_migrations.go` — shrink from ~1100 lines to ~80. Replace the 30-entry `migrations()` slice with a single entry that uses the embedded SQL. The `runMigrations()` function body is unchanged.

**Unchanged but used for verification:**
- `scripts/sql/regenerate_initialize_database.sh` — must produce byte-identical output (modulo the `Last Regenerated` date line) before and after the collapse.
- `scripts/sql/initialize_database.sql` — the committed version on main at the start of this work is the reference.

---

## Task 1: Extract the collapsed schema into `pkg/knowledge/initial_schema.sql`

Produce the canonical embedded SQL by extracting the already-committed schema dump in `scripts/sql/initialize_database.sql`. The current file has this structure:

| Lines | Content |
|---|---|
| 1–15 | Header comment |
| 17–19 | `-- TABLES` section header |
| 21–723 | `CREATE TABLE ...;` blocks (blank-line separated) |
| 725–727 | `-- INDEXES` section header |
| 729–841 | `CREATE INDEX ...;` blocks |
| 842–844 | `-- MIGRATION VERSION RECORDS` section header |
| 846–875 | `INSERT OR IGNORE INTO schema_migrations ...;` rows |

We want lines 17 through 841 (tables + indexes sections, inclusive of their section header comments — they're harmless SQL comments). We do NOT want the header comment block (lines 1–15) or the schema_migrations seeding (lines 842–875) because the runner handles version-recording itself.

**Files:**
- Read: `scripts/sql/initialize_database.sql`
- Create: `pkg/knowledge/initial_schema.sql`

- [ ] **Step 1: Confirm the file boundaries are correct on the current HEAD**

Run:
```bash
grep -n "^-- =====\|^-- MIGRATION VERSION" scripts/sql/initialize_database.sql
```
Expected output:
```
17:-- ============================================================================
19:-- ============================================================================
725:-- ============================================================================
727:-- ============================================================================
842:-- ============================================================================
843:-- MIGRATION VERSION RECORDS
844:-- ============================================================================
```

If the line numbers differ, someone has edited the file since the plan was written; re-check and adjust the `sed` range in step 2 to match the new boundaries. The important markers are: the `TABLES` header starts around line 17; the `MIGRATION VERSION RECORDS` header starts around line 842. We want everything from the start of the TABLES header through the last line before the MIGRATION VERSION RECORDS header.

- [ ] **Step 2: Extract the SQL into the new file**

Run:
```bash
sed -n '17,841p' scripts/sql/initialize_database.sql > pkg/knowledge/initial_schema.sql
```

- [ ] **Step 3: Sanity-check the extracted file**

Run:
```bash
head -5 pkg/knowledge/initial_schema.sql
tail -5 pkg/knowledge/initial_schema.sql
grep -c '^CREATE TABLE' pkg/knowledge/initial_schema.sql
grep -c '^CREATE INDEX' pkg/knowledge/initial_schema.sql
grep -c 'INSERT OR IGNORE INTO schema_migrations' pkg/knowledge/initial_schema.sql
```
Expected:
- `head -5` starts with `-- ============================================================================` / `-- TABLES` / `-- ============================================================================` / blank / (blank or first `CREATE TABLE`)
- `tail -5` ends with the last `CREATE INDEX ...;` — no `INSERT OR IGNORE` rows present
- `CREATE TABLE` count: around 29 (one for each data table in the current schema)
- `CREATE INDEX` count: around 49
- `INSERT OR IGNORE INTO schema_migrations` count: **0** (critical — if nonzero, the sed range is wrong)

- [ ] **Step 4: Verify SQLite can parse the extracted file in isolation**

Run:
```bash
rm -f /tmp/mig-collapse-probe.db
sqlite3 /tmp/mig-collapse-probe.db < pkg/knowledge/initial_schema.sql
sqlite3 /tmp/mig-collapse-probe.db "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
sqlite3 /tmp/mig-collapse-probe.db "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND sql IS NOT NULL"
```
Expected:
- First `sqlite3` command exits 0 (no SQL errors).
- Table count matches the value you saw in step 3 (~29).
- Index count matches the value you saw in step 3 (~49).

If this fails, it means the extracted SQL has a syntax error introduced by the `sed` slice. Re-inspect the boundaries — most likely the `17,841` range clipped something unintentionally.

- [ ] **Step 5: Commit**

```bash
git add pkg/knowledge/initial_schema.sql
git commit -m "feat(knowledge): extract collapsed initial schema to embedded file

Preparation for collapsing all 30 SQLite migrations into a single
embedded initial_schema migration. File is a verbatim extract of the
table and index sections from scripts/sql/initialize_database.sql at
schema version 30, with the file header and schema_migrations seed
rows stripped. Not yet referenced from any Go code — Task 2 wires it
in via //go:embed."
```

This commit adds an unreferenced file; `go build` and tests are unaffected.

---

## Task 2: Collapse the migration runner to use the embedded schema

Replace the 30-entry `migrations()` slice in `pkg/knowledge/sqlite_migrations.go` with a single entry that references the embedded SQL from Task 1. Preserve `runMigrations()` unchanged.

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go`

- [ ] **Step 1: Inspect the current file boundaries**

Run:
```bash
grep -n "^func migrations\|^func runMigrations\|^}" pkg/knowledge/sqlite_migrations.go | head
```
Expected:
```
19:func migrations() []Migration {
1030:func runMigrations(db *sql.DB) error {
```
(plus many `}` lines — we care about the two `func` lines)

So `migrations()` starts at line 19 and `runMigrations()` starts at line 1030. Lines 19 through 1029 are the entire migrations slice — that's the chunk we're replacing. The file length is around 1160 lines total.

If these line numbers differ significantly, a new migration landed between the plan being written and now — before proceeding, re-run Task 1 to regenerate `pkg/knowledge/initial_schema.sql` against the updated file (someone needs to run `./scripts/sql/regenerate_initialize_database.sh` first to capture the new schema version), otherwise the embedded SQL will be stale.

- [ ] **Step 2: Rewrite the top of the file, replacing lines 1–29 with the new header + embed + collapsed `migrations()` function**

Use the Edit tool (or manual sed) to replace the content from the start of the file through the closing `}` of `migrations()` (line 1029) with this:

```go
package knowledge

import (
	_ "embed"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Migration represents a database schema migration
type Migration struct {
	version      int
	name         string
	sql          string
	ignoreErrors bool // If true, SQL errors are logged but don't fail the migration.
}

// initialSchemaSQL holds the complete schema produced by the original
// 30-migration chain (versions 1–30), collapsed into a single migration
// for faster test startup. Future schema changes should be added as new
// migration entries (version: 2, 3, ...) rather than edited into this file.
//
//go:embed initial_schema.sql
var initialSchemaSQL string

// migrations returns all migrations in order.
//
// This used to contain 30 individual migration entries (one per historical
// schema change); they were collapsed into a single initial_schema migration
// on 2026-04-15 because 74 TestSQLiteKB_* tests were paying ~1.5s each in
// migration overhead under -race, pushing the suite past the pre-commit
// hook's 120s timeout. An existing DB with schema_migrations rows for any
// of versions 1–30 will skip the collapsed entry (since 1 ≤ max_version).
func migrations() []Migration {
	return []Migration{
		{
			version: 1,
			name:    "initial_schema",
			sql:     initialSchemaSQL,
		},
	}
}
```

Leave everything from `func runMigrations(db *sql.DB) error {` (current line 1030) through the end of the file untouched.

- [ ] **Step 3: Format and confirm the file compiles**

Run:
```bash
gofmt -l pkg/knowledge/sqlite_migrations.go
go vet ./pkg/knowledge/...
go build ./...
```
All three must produce no output / exit 0. If `gofmt -l` prints the file path, run `gofmt -w pkg/knowledge/sqlite_migrations.go` to fix formatting.

- [ ] **Step 4: Confirm the file shrank as expected**

Run:
```bash
wc -l pkg/knowledge/sqlite_migrations.go
```
Expected: ~180–200 lines total (down from ~1160). If the file is still over 300 lines, you didn't actually delete the 30 old entries — go back to step 2.

- [ ] **Step 5: Run the `pkg/knowledge` test suite (no race)**

Run:
```bash
go test ./pkg/knowledge/... -count=1
```
Expected: `ok github.com/rsned/spacemolt/pkg/knowledge` with all tests passing. If any test fails, the most likely cause is a mismatch between `initial_schema.sql` (Task 1) and what the runner was previously producing — investigate by diffing the schema of a fresh test DB against the current `scripts/sql/initialize_database.sql`.

- [ ] **Step 6: Run the `pkg/knowledge` test suite under `-race` and time it**

Run:
```bash
time go test -race ./pkg/knowledge/... -count=1 -timeout=300s
```
Record the elapsed time in your task notes. Expected: well under 60s (down from ~112s pre-collapse). If it's still over 100s, the migration collapse didn't reduce per-test overhead as expected — stop and investigate before committing. The entire point of this work is the speedup.

- [ ] **Step 7: Commit**

```bash
git add pkg/knowledge/sqlite_migrations.go
git commit -m "refactor(knowledge): collapse 30 migrations into one embedded schema

Replaces the 30-entry migrations() slice with a single
initial_schema entry that reads pkg/knowledge/initial_schema.sql via
//go:embed. The file shrinks from ~1160 lines to ~190.

runMigrations() is unchanged. Its m.version <= currentVersion gate
(line 1051 previously, now shifted up) already handles both fresh DBs
(applies collapsed v1) and existing DBs with any of versions 1–30
recorded (skips collapsed v1, since 1 <= max_version).

Motivation: 74 TestSQLiteKB_* tests were paying ~1.5s each in migration
overhead under -race, pushing the pkg/knowledge suite past the 120s
pre-commit hook timeout. After this change the suite should run
comfortably under 60s in race mode."
```

---

## Task 3: Round-trip verification via `initialize_database.sql`

The spec's exit criterion 1: `scripts/sql/initialize_database.sql` regenerated post-collapse must be byte-identical to the pre-collapse version modulo the `Last Regenerated` header date.

**Files:**
- Read: `scripts/sql/initialize_database.sql` (at git main HEAD, pre-collapse reference)
- Regenerate (temporarily): `scripts/sql/initialize_database.sql`

- [ ] **Step 1: Capture the pre-collapse reference from git**

Run:
```bash
git show main:scripts/sql/initialize_database.sql > /tmp/mig-collapse-before.sql
wc -l /tmp/mig-collapse-before.sql
```
Expected: ~875 lines.

- [ ] **Step 2: Regenerate `initialize_database.sql` post-collapse**

Run:
```bash
./scripts/sql/regenerate_initialize_database.sh
```
Expected: output ends with `Wrote scripts/sql/initialize_database.sql (schema version 30, NNN lines)`.

- [ ] **Step 3: Diff the two, ignoring the `Last Regenerated` date line**

Run:
```bash
diff \
  <(grep -v '^-- Last Regenerated:' /tmp/mig-collapse-before.sql) \
  <(grep -v '^-- Last Regenerated:' scripts/sql/initialize_database.sql)
```
Expected: **no output** (empty diff). This is a hard exit criterion.

If there is any diff, the collapsed `initial_schema.sql` is wrong and this work is not ready to merge. STOP and investigate: run the failing `diff` with context (`diff -u ...`) to see exactly which tables/indexes drifted. Most likely causes:
- The Task 1 `sed` range clipped a CREATE TABLE or CREATE INDEX statement.
- The Task 1 extraction accidentally included or excluded some blank lines or comments that `sqlite3 .schema` normalizes differently than a human-written file.

Fix and re-run from Task 1 step 4.

- [ ] **Step 4: Discard the regenerated file from the working tree**

The regenerate script modified `scripts/sql/initialize_database.sql`, but the only diff from main is the `Last Regenerated` header date. We don't want to commit a date-only change. Reset the file:

```bash
git checkout -- scripts/sql/initialize_database.sql
git status
```
Expected: working tree clean.

- [ ] **Step 5: No commit for this task** — it's pure verification. Continue to Task 4.

---

## Task 4: Live DB smoke test

The spec's exit criterion 2: a real spacemolt-knowledge.db that's currently at v30 must start cleanly under the collapsed runner without the runner attempting to re-apply the collapsed v1.

**Files:**
- Back up: `data/spacemolt-knowledge.db`
- Read: same

- [ ] **Step 1: Back up the live DB**

Run:
```bash
cp data/spacemolt-knowledge.db /tmp/spacemolt-knowledge.db.bak
sqlite3 data/spacemolt-knowledge.db "SELECT MAX(version) FROM schema_migrations"
```
Expected: `30`. If it's something else, the `spacemolt-server` wasn't restarted after PR #119 merged and migration 30 isn't applied yet — STOP, restart the server to catch up, then retry.

- [ ] **Step 2: Open the live DB via the collapsed runner**

We reuse the same small runner the regenerate script uses internally: it calls `NewSQLiteKB` against the given path, which runs `runMigrations`. Against the live DB this should be a no-op.

```bash
cat > /tmp/open-live.go <<'EOF'
package main

import (
	"fmt"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func main() {
	cfg := knowledge.DefaultConfig()
	cfg.DBPath = os.Args[1]
	cfg.WAL = false
	kb, err := knowledge.NewSQLiteKB(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	_ = kb.Close()
	fmt.Println("opened and closed:", os.Args[1])
}
EOF
go run /tmp/open-live.go data/spacemolt-knowledge.db
```
Expected output: `opened and closed: data/spacemolt-knowledge.db` (no migration error, no panic).

- [ ] **Step 3: Confirm no new schema_migrations row was inserted**

Run:
```bash
sqlite3 data/spacemolt-knowledge.db "SELECT MAX(version) FROM schema_migrations"
sqlite3 data/spacemolt-knowledge.db "SELECT COUNT(*) FROM schema_migrations WHERE version = 1"
```
Expected:
- First query: `30` (unchanged from step 1).
- Second query: `1` (the original row for version 1 — still there. If this is 2, the collapsed v1 ran a second time, which means the gate failed — investigate immediately).

- [ ] **Step 4: Confirm migration 30's effects are still present**

Run:
```bash
sqlite3 data/spacemolt-knowledge.db "SELECT name FROM sqlite_master WHERE name='mission_template_locations'"
sqlite3 data/spacemolt-knowledge.db "SELECT sql FROM sqlite_master WHERE name='pois'" | grep -o "expires_at[^,)]*"
```
Expected:
- First query: `mission_template_locations` (table exists — from migration 30, mission catalog).
- Second query: `expires_at TEXT` (column exists — from migration 28, poi_expires_at).

- [ ] **Step 5: Restore the backup and confirm clean state**

Run:
```bash
diff data/spacemolt-knowledge.db /tmp/spacemolt-knowledge.db.bak >/dev/null && echo "unchanged" || echo "CHANGED"
```
Expected: `unchanged`. If the live DB has any diff from the backup, something in step 2 mutated it — investigate before continuing. The expected outcome is zero modification because the runner should have been a no-op.

If it reports `CHANGED`, restore the backup:
```bash
cp /tmp/spacemolt-knowledge.db.bak data/spacemolt-knowledge.db
```

Clean up:
```bash
rm /tmp/open-live.go /tmp/spacemolt-knowledge.db.bak
```

- [ ] **Step 6: No commit for this task** — pure verification.

---

## Task 5: Push and open PR

**Files:** none (git operations only)

- [ ] **Step 1: Final build + test confirmation**

Run:
```bash
go build ./...
go test ./... -count=1
```
Expected: both succeed.

- [ ] **Step 2: Push the branch**

```bash
git log --oneline main..HEAD
```
Expected: two commits — Task 1's `feat(knowledge): extract collapsed initial schema to embedded file` and Task 2's `refactor(knowledge): collapse 30 migrations into one embedded schema`.

Push:
```bash
git push -u origin <branch-name>
```
(Substitute whatever branch name this work is on.)

- [ ] **Step 3: Open the PR**

Use `gh pr create` with a title of `refactor(knowledge): collapse 30 migrations into one embedded initial schema` and a body that explains:
- The motivation (test suite race-mode timeout)
- The before/after test timing recorded in Task 2 step 6
- The verification done (Task 3 round-trip, Task 4 live DB smoke test)
- A note that `//go:embed` is the new mechanism and future schema changes should still be added as new migrations (version 2, 3, ...) rather than edited into `initial_schema.sql`

---

## Post-merge cleanup (manual, not part of the PR)

After this PR merges:

1. **Revert the local pre-commit hook timeout.** The worktree-local `.git/hooks/pre-commit` was bumped from `-timeout=120s` to `-timeout=300s` earlier as a band-aid. Revert it:
   ```bash
   sed -i 's|-timeout=300s|-timeout=120s|' /home/robert/spacemolt/spacemolt/.git/hooks/pre-commit
   ```
   Confirm the test suite still passes under the tighter threshold:
   ```bash
   go test -race -count=1 -timeout=120s ./pkg/knowledge/
   ```

2. **Update memory.** The `project_migration_collapse.md` memory note can be deleted or updated to "done" so future sessions don't treat it as an open follow-up.

---

## Self-Review

**Spec coverage:**

- Spec §Architecture → Tasks 1 + 2.
- Spec §Source of truth (one canonical `initial_schema.sql`) → Task 1.
- Spec §Runner changes (collapsed `migrations()`, unchanged `runMigrations()`) → Task 2.
- Spec §How `initial_schema.sql` is produced → Task 1 (sed extraction from the current generated file, which itself was produced by the 30-migration chain; same net result as "run migrations + dump schema + strip headers").
- Spec §Verification 1 (round-trip `initialize_database.sql`) → Task 3.
- Spec §Verification 2 (live DB unchanged) → Task 4.
- Spec §Testing + speedup benchmark → Task 2 step 6.
- Spec §Risks (migration-during-work race) → Task 2 step 1 check for updated line numbers.
- Spec §Follow-ups → unchanged, already captured in `project_migration_collapse.md` memory note. Not in this plan.

**Placeholder scan:** none of the No-Placeholder red flags present. Every code block is complete, every command has expected output, every edit shows the full replacement text. The Task 2 step 2 edit shows the full file header + embed + `migrations()` replacement; the rest of the file from `runMigrations()` onward is preserved verbatim.

**Type consistency:** `initialSchemaSQL` is consistently named across Task 2 step 2's embed directive and the `migrations()` return. The `Migration` struct keeps its existing fields (`version`, `name`, `sql`, `ignoreErrors`) so nothing else in the runner body needs updating.
