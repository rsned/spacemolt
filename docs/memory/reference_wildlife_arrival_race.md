---
name: reference_wildlife_arrival_race
description: get_nearby on arrival reads 0 creatures at a populated POI — a server-side tick-ordering race, measured at an 18% false-negative rate; fix pending, workaround removable when patch notes land
metadata:
  type: reference
---

**A `get_nearby` issued in the same second as an arrival reads ZERO creatures at
a POI that is populated.** Not creature churn — an ordering race inside one tick.

**Cause, confirmed by the server team 2026-08-28:** wildlife is materialised at a
POI only while a pilot is there, and that step runs LATE in the tick, while the
arrival confirmation goes out at the START of the same tick. A client that
queries on arrival reads the location before the herd is placed. **A server fix
is planned** that places the wildlife before announcing arrival.

Observed, craftsman-1, both POIs of one pass:

| POI | arrived | 1st look | 2nd look |
|---|---|---|---|
| alrescha_emission_nebula | 17:20:55 | 0 | **18** (17:21:01) |
| alrescha_ice_fields | 17:21:25 | 0 | **8** (17:21:31) |

**Measured false-negative rate = 18%** — at POIs where creatures have EVER been
seen, 20 of 111 looks came back empty. Across all `get_nearby` looks, 127 of 218
are empty (58.3%), but that pools genuinely barren POIs. `survey_system`'s
census does NOT suffer the race: only 10.7% empty.

**⭐ The coverage table is named `wildlife_surveys`, NOT `wildlife_coverage`.**
`RecordWildlifeCoverage` writes into it. A `.tables | grep coverage` finds
nothing and wrongly suggests the table was never built — it was.

**⭐ The cost is not just missing data.** `huntFindQuarry` treated an empty list
as "this ground holds nothing", abandoned the belt and flew to the next one, so
the race made the hunt fleet REJECT the grounds its missions needed, one real
journey at a time. Fixed `238f73a7`: re-read after `game.SleepTick` when the
first look is empty.

**PENDING — remove the workarounds when the fix appears in patch notes**
([[reference_patch_notes_source]]):

- `cmd/tools/play_as/explore.go` `captureWildlifeSecondLook` — **UNCONDITIONAL**,
  so it costs a call per POI forever. This is the one that must be deleted.
- `pkg/worker/hunt_wildlife.go` `huntNearbyCreatures` — **conditional** on an
  empty first look, so a fixed server simply stops triggering it. Retires
  itself; removal optional.

Supersedes the old "get_nearby is a transient snapshot" reading, which explained
the same observations as creatures wandering and was wrong.

Related: [[reference_wildlife_second_look_species_yield]] ·
[[reference_idle_loop_ran_3x_per_tick]] (call volume is what trips the per-IP
limiter, which is why the fleet-side retry is conditional)
