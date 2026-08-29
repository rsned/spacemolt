---
name: reference_ships_table_migration_trap
description: "A numbered migration that does ALTER TABLE ships fails on pre-collapse DBs — the ships table isn't created until AFTER the migration loop"
metadata: 
  node_type: memory
  type: reference
  originSessionId: aa93e47f-4e5a-48b3-bf98-dbc6dbaa1204
---

**Do not add a `ships` column via a numbered migration in `pkg/knowledge/sqlite_migrations.go`.** It fails with `no such table: ships` on pre-collapse DBs.

**Why:** `runMigrations` runs the numbered-migration loop first, and only *afterwards* calls the self-healing `ensureCollapseMissingTables(db)` — which is what creates `ships` (plus `agent_ships`, `ship_build_materials`, `base_facilities`) on DBs that predate the 2026-04-15 migration collapse. Those DBs carry `schema_migrations` rows for the old versions 2–30, so `currentVersion` is already high and the migrations that would have created the table are skipped. A new migration numbered above `currentVersion` therefore runs *before* the table exists.

Migration 42 (`add_catalog_stat_fields`) does bare `ALTER TABLE ships ...` and looks like precedent — it is **not**. It survives only because those tests/DBs already have it marked applied, so it never re-runs. A *new* high-numbered migration does run, and dies. Four tests catch this immediately (`TestSQLiteKB_Migration3_LastVisitedTickBackfill`, `..._Migration31_SelfHealsPreCollapseDBs`, `TestEnsureCollapseMissingTables_CreatesOnPreCollapseDB`, `..._Migration32_PassiveSkillBackfill`) — trust them over the apparent precedent.

**How to apply:** add the column in **two** places instead, following `ensureShipClassPrestigeCols` (added 2026-07-13) and the older `ensurePublicFacilitiesRentalCol`:
1. the `CREATE TABLE ships` DDL inside `ensureCollapseMissingTables`, so freshly-created tables have it; and
2. an idempotent `ensure*Cols(db)` function — `pragma_table_info` check, then `ALTER` if absent — called after `ensureCollapseMissingTables` in `runMigrations`.

This covers all three cohorts (fresh DBs, current DBs, pre-collapse DBs) uniformly.

Then **regenerate `scripts/sql/initialize_database.sql`** (`./scripts/sql/regenerate_initialize_database.sh`) or `TestInitializeDatabaseSQLInSync` goes red — it diffs that generated file against the schema the migration runner actually produces.
