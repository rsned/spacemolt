---
name: reference-facility-list-field-omissions
description: facility list response strips per-instance level + rent_per_cycle; default level=1 from catalog
metadata: 
  node_type: memory
  type: reference
  originSessionId: 73e356b1-e0c3-4d21-8b72-cfe61b9cf707
---

The `facility list` action's response objects (under `faction_facilities`, `player_facilities`, `station_facilities`) **omit per-instance `level` and `rent_per_cycle` fields** as of 2026-05-27. The catalog data in `data/game-api/latest/facility_faction.json` carries `level: 1` and `rent_per_cycle: N` for every facility type, but the live response only includes `active`, `category`, `description`, `facility_id`, `faction_id`, `faction_service`/`personal_service`, `maintenance_satisfied`, `name`, `type`, and optionally `capacity`.

Workaround in `cmd/tools/play_as/main.go`: `facilityLevelOrDefault(level int)` treats 0 as unset and returns 1. Rent rendering is suppressed when 0. Drop the fallback once the server starts emitting these per-instance.

Proper fix: load the catalog (`data/game-api/latest/facility_faction.json` + per-type detail files) and look up `level`/`rent_per_cycle` by `type` field. Not done yet — adds I/O + threading cost; deferred until upgrades are actually possible and level diverges from 1.

See commits `e0f7bfd` (dispatch reorder so `facility list` doesn't fall into faction_list formatter) and `076953c` (level default).
