---
name: project_drone_marketbots
description: "The ten drone marketbots (010-019) — drone_control 17-19, 66.6k units mined; drone mining trains drone_control NOT mining, and six bots sit at a ~9,250-unit ceiling"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-09-09T06:00:33.490Z
---

Ten resident marketbots (`marketbot_010`..`019`) posted to remote belt/gas/ice
stations run uploaded DroneLang scripts from `data/scripts/drones/*.ds`
(one template per site; `fit_drones` substitutes `$STATION$` /
`$ASTEROID_BELT$` / `$ICE_FIELD$` / `$GAS_CLOUD$` at upload time). Script +
deploy pass completed 2026-09-08; all ten are live in the 64-worker mb fleet.

## Status 2026-09-09

**⭐ Drone mining trains `drone_control`, NOT `mining`.** None of the ten has a
`mining` skill row at all — the first place I looked and the wrong one. Compare
`miner-1` (mining 19, deep_core_mining 19) with `marketbot_010` (drone_control
17, no mining row).

| bot | drone_control | units mined | notes |
|---|---:|---:|---|
| marketbot_haven | **26** | — | the original canary, well ahead |
| 012 | 18 | 3,961 | nickel |
| 019 | 18 | 4,200 | zosma ice |
| 011 | 18 | 1,750 | silicon/neodymium — laggard |
| 015 | **19** | 1,500 | ironhearth "struggling" scout, but mines the most valuable spread (platinum/rhodium/palladium) |
| 013 / 010 / 014 / 018 / 016 / 017 | 17 | ~9,029–9,273 each | the tight cluster explained below |

Total **66,604 units** across the ten (storage capture 02:17–02:19Z, fresh).
`craftsman-1` shows drone_control 100 with 0 xp — an anomaly, not a real level.

**Six of ten cluster at 9,029–9,273 units — this is NOT a cap.** I flagged it as
a probable storage ceiling; the operator corrected it 2026-09-09: **personal
storage holds 100,000 units PER ITEM TYPE**, so these bots are at ~9% of one
item's limit. The clustering is just similar yield over similar uptime since the
09-08 deploy. There is a lot of headroom — the constraint on this project is not
storage.

**`xp` in `agent_skills` is xp-INTO-CURRENT-LEVEL, not cumulative** —
marketbot_015 reads level 19 / 160 xp because it had just levelled, while
marketbot_012 at 18 / 10,300 is nearly there. Rank by level, never by xp.

**`xp_observations` is unusable for drone_control rate.** Every row is sourced
from a `get_skills` or `login` snapshot diff (avg +15,446 xp, some rows
NEGATIVE) — the known spurious-XP problem, see `scripts/analyze_spurious_xp.sql`
/ `cleanup_spurious_xp.sql`. Level from `agent_skills` is the only trustworthy
progress measure.

**No drone lines appear in `mb-overmind.log` and that is expected** — the worker
uploads the script once and the drones execute server-side, so absence of log
output is not absence of production. Verify via `agent_storage_items` growth.

See [[reference_drone_bay_is_agent_wide]] ·
[[project_drone_project_crystal_reserve]] · [[reference_station_id_aliases]]
