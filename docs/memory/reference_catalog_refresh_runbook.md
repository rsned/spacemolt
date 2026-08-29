---
name: reference_catalog_refresh_runbook
description: How to refresh knowledge.db game catalogs (ships/items/recipes/skills) from a data-scraper snapshot; last done 2026-07-07 vs server v0.473.0
metadata: 
  node_type: memory
  type: reference
  originSessionId: 364c6370-661d-49b2-8613-76f294f500c9
  modified: 2026-08-13T02:33:47.169Z
---

**⭐ GAP FOUND 2026-08-12: mining hulls are in NO catalogue.** `drillship`,
`excavator`, `prospector` and `deeprock_harvester` appear in neither
`data/game-api/latest/catalog_ships.json` (335 classes) nor the KB `ships` table
(332) — yet **18 of the 42 mission-learn agents actively fly them**. So any
capacity or fuel planning silently skips those agents: a naive join drops them,
which is how a mission-learn cargo census came back 24/42 and undercounted the
freight-capable pool. Refresh must cover them, and consumers should LOG
unresolved class_ids rather than `continue` past them.

**Catalog refresh runbook** (last run 2026-07-07, server v0.473.0 → ships 332, items 650, recipes 666, skills 30; counts unchanged from 2026-07-04/v0.471.3):

Gotcha: the earlier `jq`/pagination-check step does `cd data/game-api/latest` — that persists cwd in the Bash tool, so run the `go run ./cmd/data/import-catalog-*` lines from the repo root (absolute cd) or they resolve relative to the snapshot dir and fail with "directory not found".

1. Snapshots live in `data/game-api/YYYYMMDD/` (`latest` symlink); created by data-scraper — check `get_version.json` for server version and `jq '.total, .total_pages'` on each catalog file to confirm no pagination truncation (all were pages=1).
2. Import into the LIVE knowledge.db (safe while fleet runs; upserts + WAL):
   - `go run ./cmd/data/import-catalog-ships data/game-api/latest/catalog_ships.json`
   - `go run ./cmd/data/import-catalog-items data/game-api/latest/catalog_items.json` (modules are folded into items now — `catalog_modules.json` returns total=0 since ~v0.471)
   - `go run ./cmd/data/import-catalog-recipes …` / `import-catalog-skills …`
   - DB path override via `SPACEMOLT_DB` env; default `data/spacemolt-knowledge.db`.
3. No importer exists for `catalog_facilities.json` (2554 entries) — facilities come from the catalog JSON at runtime.

Gotcha: `BuiltForAPIVersion` (pkg/version/checker.go) is a separate concern — still v0.397.0 vs server 0.471.x; see [[project_api_currentness_round]].

## ⭐ 2026-08-27 refresh — server v0.566.2 (ran clean)

Counts: **ships 338→341, items 650→814, recipes 666→828, skills 30 (flat),
item_modules 210→235** (weapons 72 / defense 47 / mining 13 / utility 103).
`catalog_items` category `ore` now holds **63** rows — the 11 newly seeded
mineable resources landed. Snapshot integrity checked first: every catalog file
reported `total_pages: 1`, so nothing was truncated.

**The legacy-hull trap did NOT fire.** `withLegacyShipClasses` held: all six
mining classes (prospector, drillship, excavator, deeprock_harvester,
mining_barge, mining_cruiser) survived. Only the known 5 classes are still
uncovered — sparrow(3), dusthound(1), rubble(1), shadow_dancer(1), siphon(1) =
7 hulls. No regression. Verify with:
`attach 'data/assets.db' as a; select h.class_id, count(*) from a.agent_hulls h
 left join ships s on s.id=h.class_id where s.id is null group by h.class_id;`

Back up the rewritten tables first — the DB is **2.8 GB**, so a whole-file copy
is wasteful. Dump just these 13 and keep the .sql:
`ships ship_build_materials items item_modules item_weapons item_defenses
 item_mining item_ammo item_consumable_effects recipes recipe_inputs
 recipe_outputs skills` (9,086 rows, 1.3 MB). That backup is also the ONLY way
to recover the base_value of an item the server just deleted.

### ⭐🔴 The patch note's removal list was INCOMPLETE

v0.566.0 named 3 drones + 8 modules. Four more items silently vanished — they
were in our catalog before the import and are absent from the new JSON:

| item | held | base_value |
|---|---|---|
| chaff_bundle | 16,869 | 9 |
| thermal_flare | 171 | 74 |
| ecm_jammer_pod | 67 | 170 |
| sensor_jammer | 9 | 460 |

All four are combat consumables (chaff/flare/ECM/jamming). **None were
announced**, so there is NO stated refund for them — the note's "10x former
base-value credit refund" clause is scoped to the enumerated drones and modules
only. Of our holdings, just 1 `force_field_generator_ii` is actually covered.
Do not compute a payout from the unannounced four.

Worth noting the mismatch: the note says the list was "found in live player or
faction holdings", yet we hold 16,869 chaff_bundle and it is unnamed — so
either consumables are not "property" for this purpose, or they were removed by
a different mechanism. Unresolved.

**Rule: after every refresh, diff held items against the catalog** —
`agent_storage_items LEFT JOIN items WHERE items.id IS NULL` (exclude
`package:%`, those are freight packages, not catalog rows). The announcement is
not a reliable list of what was removed. The pre-import table backup is the ONLY
way to recover a deleted item's base_value afterwards.
