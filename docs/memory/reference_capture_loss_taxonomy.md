---
name: reference_capture_loss_taxonomy
description: Six mechanisms by which the fleet silently drops data it already received — 18 KB tables have never held a row; every mode looks identical to success
metadata:
  type: reference
---

**18 tables exist and have NEVER held a row** (measured 2026-08-29):

```
knowledge: player_skills ship_modules agent_ships ship_cargo players
           player_stats danger_zones resource_history connection_metrics
           base_market faction_orders faction_missions faction_rooms
           wildlife_kills wildlife_kill_drops knowledge_exports
assets:    agent_citizenship_petitions
market:    analyses
```

Six distinct mechanisms, and **every one fails silently and is indistinguishable
from success**:

1. **Parsed but never read.** The field is on the struct, so it looks captured.
   `WormholePredictionHint` appeared exactly ONCE in the codebase — its own
   definition. Same for `wormhole_expires_in`, `pirates[]`, `empire_npcs[]`.
2. **Schema without a writer.** The 18 above. `ship_modules` was queried and
   reasoned about for weeks while empty.
3. **Written, then erased.** `StoreShipClasses` is DELETE+INSERT, so a catalog
   refresh wipes classes the new payload omits —
   [[reference_legacy_ship_classes_erased_by_refresh]].
4. **Silent key drift.** `system_id` stored as `"Bellatrix"` vs
   `systems.id="bellatrix"`; the join returns fewer rows, never an error.
   Also [[reference_station_id_aliases]], [[reference_rawjson_key_drift]].
5. **Right-looking data, wrong semantics.** `systems.last_visited_tick` is
   non-zero for all 505 systems because get_map IMPORTS write it. It records
   importing, not visiting.
6. **Stale artifacts read as live.** A status file no process overwrites still
   parses — [[reference_fleet_status_fossil]].

**Nothing in the system ever says "you received this and did not store it."**
Every 2026-08-28/29 finding surfaced only because the operator ran `play_as` by
hand and pasted payloads.

**Cheapest detector, not yet built:** walk every `serverapi` response struct we
store, assert each field reaches a column, and flag zero-row tables as unwired.
Mechanism 2 is a five-line query; mechanism 1 is a static "struct field with no
reader" check. Those two alone cover 18 tables plus the dropped-field class.
