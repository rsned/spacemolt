---
name: reference_legacy_ship_classes_erased_by_refresh
description: "StoreShipClasses is DELETE+INSERT, so retired ship classes vanish on every catalog refresh — including the four most-flown hulls in the fleet"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T04:30:14.799Z
---

`SQLiteKB.StoreShipClasses` clears `ships` and `ship_build_materials` before
inserting, so **any class the live catalog stops publishing is erased on the next
refresh and never returns**. The classes that fell out are the retired mining hulls,
and they are not a long tail — they are the fleet's four most-flown:

    prospector 89 hulls · drillship 86 · excavator 57 · deeprock_harvester 29

That is **261 of 413 hulls (63%) with no catalog row at all**, so nothing could
compute their jump fuel (`ceil(scale^1.5 x speed)`), jump time, or bare-hull cargo.

**Recovery source:** dated pulls under `data/game-api/*/get_ship.json` and
`catalog_ships.json` carry the full class block with the server's own
`"legacy": true` marker — exactly six mining classes (the four above plus
`mining_barge` and `mining_cruiser`, which are stat-identical renames of excavator
and deeprock_harvester). Captured in `pkg/knowledge/catalogdata/legacy_ship_classes.json`
and folded in by `withLegacyShipClasses` on every store (`59c6764f`). A class the
server republishes wins — the recovered copy is only a fallback.

**Corroboration:** recovered `base_fuel` matches the live `fuel_max` on all 261 hulls
(prospector 100 / drillship 130 / excavator 150 / deeprock 200).

**Still missing (7 hulls, no definition anywhere):** dusthound, shadow_dancer, siphon,
sparrow, and rubble — rubble appears in one 2026-03-27 pull but was never
legacy-flagged, so it was left out rather than muddy the seed's meaning.

**Rule:** after any catalog refresh ([[reference_catalog_refresh_runbook]]), check
coverage with a join of `assets.db agent_hulls.class_id` against
`spacemolt-knowledge.db ships.id` — a silent drop is invisible otherwise.

Related: [[reference_ships_table_migration_trap]] — do NOT add a `ships` column via a
numbered migration; the legacy rows are marked with the existing `special` column.
