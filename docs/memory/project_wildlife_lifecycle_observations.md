---
name: project_wildlife_lifecycle_observations
description: wildlife_observations (migration 62) is the per-individual, per-tick creature table; how the per-stage histogram is counted and why
metadata:
  type: project
---

**Migration 62, `wildlife_observations`**, applied to the live KB 2026-10-16
(37 ms, fleet live, v61 -> v62). One row per INDIVIDUAL creature per game tick.
Backfilled from the JSONL dump: 5,958 rows, 0 skipped, 5,262 distinct sightings
(~700 double-looks collapsed), 45 stage populations.

**Why it exists.** `wildlife_species` holds one row per species with a
`max_hull` that only ratchets UP. From 2026-10-14 that stopped meaning
anything: leviathan-class hull varies per individual, and creatures are sighted
at a growth stage carried in the display name. hoarfrost reads `max_hull` 769
while the hatchling we killed was 312 — anything using it for threat assessment
overestimates a hatchling by 2.5x. See
[[reference_wildlife_update_20261014_lev_ids_variable_hull]].

**The PK is the whole design: `(creature_id, game_tick)`.**
- The explore loop looks twice per POI on purpose, to beat the arrival race,
  and both looks land in the SAME tick — so the key collapses the duplicate
  while a genuine re-sighting at a later tick stays its own row. A raw row
  count reports a population at twice its real size.
- `creature_id` is STABLE as an animal grows, so an individual accumulates rows
  whose stage and hull change over its life. The transition is just the row
  where `stage` differs from the previous one.

**Counting rule: DISTINCT creature_id WITHIN a stage.** A matured animal counts
in EVERY stage it was observed at. Deduping per creature globally was the bug
in the old JSONL report (it kept the widest hull and the name at that hull), and
it drains the young bands over time: every survivor migrates upward, leaving
them populated only by animals that died or stopped being sighted. It looked
right only because nothing had grown yet.

**`stage = ''` means MATURE, not unknown.** Derived from `name` via
`knowledge.StageWord` at write time so queries need no name parsing; `name` is
kept raw beside it so history is recomputable if the vocabulary gains a word.

**Stage ORDER is never hardcoded.** The bands do not overlap within a species,
so `ORDER BY hull_min` gives growth order for free — which matters because the
ladder DIFFERS by species: hoarfrost has a hatchling below its juvenile,
cloudwhale a calf, and no cloudwhale hatchling has ever been seen.

Ladder as of 2026-10-16, each transition ~x2.2-2.4:
```
hoarfrost_leviathan  hatchling 243-324   juvenile  669-769
cloudwhale           calf      515-735   young  1287-1767   mature 3354-3419
molt_leviathan       juvenile  481-691
```
Adults vary per individual too (3 cloudwhales, 3354-3419), so growth does not
stop at a fixed ceiling.

**ZERO of 1,971 tracked individuals has been observed growing.** An empty
`WildlifeGrowthObserved` is the EXPECTED answer and must not be read as a
capture failure. We therefore still do not know the growth timescale, nor
whether `hull` or `max_hull` moves first.

Commands (`play_as`, local only, no server call):
- `wildlife_growth` — histogram from the JSONL dump (the long-running copy)
- `wildlife_growth --db` — the same histogram from the table
- `wildlife_growth --import` — load the dump into the table; idempotent upsert

**Both sinks still run on purpose.** The JSONL is append-only and gitignored and
was the only copy of the lifecycle data. Retirement criterion: the two
renderers agree. Verified 2026-10-16 — 45 stage populations, 0 mismatches — but
the file stays until it has been re-verified after more collection.

Related: [[reference_capture_loss_taxonomy]] ·
[[project_wildlife_combat_intelligence]]
