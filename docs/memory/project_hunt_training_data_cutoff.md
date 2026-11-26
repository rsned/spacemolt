---
name: hunt-training-data-cutoff
description: hunt decision-capture records before 2026-11-24 16:27 PT are pre-fix; train only on records at or after that cutoff
metadata:
  node_type: memory
  type: project
  originSessionId: 734f5d40-f51d-46b3-9086-72af3ca8b239
  modified: 2026-11-25T23:26:30.649Z
---

Hunt decision capture lives in `data/agents/pirate-N/hunt-decisions/<UTC date>.jsonl` (one file per agent per day; each record has an `at` timestamp).

**Cutoff: 2026-11-24 16:27 PT (2026-11-25T00:27Z).** Operator decision 2026-11-24: start the training set fresh from here by cutoff; do not delete older records.

**Why:** the ~5,281 earlier records are mostly bug artefacts (pirate-13's ~390 zero-ground avoid-list loop sorties, 255 blocked_unarmed from rearm bugs, the hive stall, restarts) and were made under old conditions (policed-only, two-gun fits). At 16:27 the last hunters restarted on the fixed build with all seven cleared for Lawless space.

**How to apply:** filter on the record `at`, not the file date (the cutoff falls inside the 2026-11-25 UTC file). `at` is written with a local `-08:00` offset, so PARSE it; a string compare against a `Z` time is 8 hours off (I made this mistake 11-25). Per-job money comes from `data/assets.db` `action_log_events` (UTC; mission.completed, ship.refuel, trading.exchange_fill), captured twice daily. Also drop zero-ground sorties. Known issues still shaping post-cutoff data: hunters crowd the same quarry, needs accepted at long range, no repair-kit buying; a fix to any of these moves the cutoff. A build-version stamp on each record was offered and declined for now.

Related: [[index_combat_wildlife]], [[wildlife-loot-drops-vary]]
