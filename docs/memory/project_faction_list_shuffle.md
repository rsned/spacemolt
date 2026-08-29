---
name: project_faction_list_shuffle
description: faction_list returns an unstable (shuffled) order; faction_list --seed loops-until-coverage to work around it
metadata: 
  node_type: memory
  type: project
  originSessionId: d888eb9c-31e9-4e93-8e93-be4e146853a7
---

The server's `faction_list` returns factions in a **non-deterministic / shuffled order** across calls, so `offset`-based pagination overlaps heavily and a single 0/50/100 sweep enumerates only ~60 of (e.g.) 136 distinct factions. A **bug was filed to the dev team** about the shuffling (2026-06-02).

**Workaround in `seedFactionsFromList` (`cmd/tools/play_as/kb_update.go`):** `faction_list --seed` polls repeatedly, rotating the offset, and accumulates **distinct** faction ids into a set until `len(seen) >= total_count` or coverage plateaus (`factionSeedDryStreak=6` consecutive no-new pages), capped at `factionSeedMaxPages=40`. Each new faction is upserted via `knowledge.SQLiteKB.UpsertFactionListEntry`.

**Status:** SHIPPED 2026-06-03 (commit d2ae74c on main). The server shuffle bug was still open, but the loop-until-coverage workaround tolerates it, so user opted to ship rather than keep holding.

**Why:** trusting offset paging silently dropped ~half the factions; the seed reported "136 seeded" while only ~60 distinct rows landed.

**How to apply:** if re-touching this, don't assume offset paginates a stable list. Re-running `faction_list --seed` picks up stragglers (it warns when coverage < total_count). Related: `UpsertFactionListEntry` inserts new rows with a stale `captured_utc` sentinel (`1970-01-01`) so the [[project_faction_info_backfill]] still enriches them later, and refreshes only the lightweight columns on conflict.

**DB & WAL gotcha:** play_as `--db-path` defaults to the *relative* `data/spacemolt-knowledge.db`. craftsman-1 runs from the repo cwd `/home/robert/spacemolt/spacemolt` and writes to that repo DB (verified via `/proc/<pid>/cwd` + `/proc/<pid>/fd` showing fd→`data/spacemolt-knowledge.db`). The DB is **WAL mode**: committed writes live in `...-wal` and the main `.db` file's **mtime lags until a checkpoint** — do NOT infer "the agent didn't write" from a stale main-file mtime (this misled an earlier diagnosis). To see true state, query via `sqlite3` (it reads through the WAL) or inspect the agent's open fds, not the mtime. Note: `full` is a reserved word — alias it in SELECTs.
