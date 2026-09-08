---
name: reference_live_kb_schema_drift
description: "2026-09-08 audit + fix: live data/spacemolt-knowledge.db drifted on 4 tables because migration v35 was edited IN PLACE. Migration 60 reconciles (d4350708); chain 1..59 collapsed into a baseline with a floor guard (3c4536c6). Live is at 59 and gets 60 on first open by a new build. Never edit an applied migration."
metadata:
  type: reference
---

**Master vs migrations: in sync.** `scripts/sql/initialize_database.sql` is
regenerated from the Go runner; a regeneration on 09-08 produced 0 diff lines
and `TestInitializeDatabaseSQLInSync` guards it. So "master == sum of all
changes" holds.

**Master vs LIVE (`data/spacemolt-knowledge.db`, 4.6 GB, WAL): NOT exact.**
Live's ledger is `2,4..30,31..59` (it went through the pre-collapse chain
4-30 that no longer exists in code); fresh is `1,2,31..59`. Same 74 tables
(live has +2: `poi_metadata_planets/stars`, owned by spacemolt-kb — expected),
identical indexes/views/triggers, but 4 tables differ at column level:

| table | fresh | live | impact |
|---|---|---|---|
| faction_missions | PK (faction_id, base_id, mission_id) | PK (faction_id, mission_id) | commit `fff8e9cb` (05-20) edited migration **v35 in place** after live had applied it; live never got the new PK. Code does DELETE-per-(faction,base) then INSERT, so the same mission at two bases collides on live. Both tables have **0 rows** on live. |
| faction_orders | PK (faction_id, base_id, order_id) | PK (faction_id, order_id) | same |
| base_market | quantity NOT NULL, is_npc DEFAULT 1 | quantity DEFAULT 0, is_npc DEFAULT 0 | documented in initial_schema.sql; every INSERT supplies both; 0 rows |
| pois.class | no default (NULL) | DEFAULT '' (29 rows '') | readers COALESCE(class,''); harmless |

**Why:** an applied migration was edited instead of a new one being added,
so fresh DBs and live diverged silently; nothing compares live to fresh.
**How to apply:** NEVER edit a migration that has run on live — add v60+.
Fix for the faction tables = a v60 that drops+recreates them (empty on
live). To re-audit, build a fresh DB and diff column-level
`pragma_table_info` for every `sqlite_master` table against
`sqlite3 -readonly data/spacemolt-knowledge.db` (the 09-08 session did this
in a scratch shell function; worth shipping as scripts/sql/schema-diff.sh).
See [[reference_capture_loss_taxonomy]] — empty faction tables may be one of
the silent-drop modes. Related: [[reference_ships_table_migration_trap]].

**Resolution (same day).** `d4350708` migration 60 rebuilds the three tables
by copy. `3c4536c6` collapses 1..59 into `initial_schema.sql` (a dump of a
fresh post-60 DB, ledger version 59) and adds `collapseFloor`: a DB with
ledger 1..58 is refused naming commit d4350708 as the last build that can
upgrade it. Fresh ledger = {59,60}; live = {2,4..59} + 60 once a new build
opens it (milliseconds, three empty tables — no fleet stop needed, but apply
it with ONE process first, e.g. `kb-drift-audit`, so no worker meets it
cold). Deleted: 11 chain-only tests, six per-open self-heal helpers, two
legacy xp SQL scripts, the dangling schema_crafting.sql symlink. The old
`spacemolt-agent-server` checkout's v4 DB copy was removed 09-08; that
checkout (branch feature/agent-server, 8 commits not on main, 4 dirty
files) still exists and `cmd/agent-server` no longer exists in the repo
(CLAUDE.md is stale about it).
