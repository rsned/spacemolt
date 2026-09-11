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

## ⭐ Status 2026-09-11 — the project is working, and the cap is now the story

Two days on from the 09-08 deploy.

**Skills: +18 levels in two days.** The cohort converged into a tight 35-37 band.

| bot | 09-09 | 09-11 |
|---|---:|---:|
| marketbot_haven | 26 | **39** |
| 011 / 012 / 015 / 019 | 18-19 | **37** |
| 010 / 013 / 014 / 016 / 017 / 018 | 17 | **35** |

⭐ **`011` was never a laggard** — it read low on 09-09 purely because it had
less uptime, and it is now level 37 with the leaders. Drop that label.

**Storage: 66,604 -> 480,240 units across the ten** (532,880 with haven), ~7x.
The six-bot "~9,250 cluster" I once suspected was a ceiling now sits at
70,000-74,000 each, which closes that question for good — the operator's
100,000-per-ITEM-TYPE correction was right and there was never a cap there.

**⭐🔴 The real cap is per item, and iron ore is the one to watch:**

| bot | item | qty | % of the 100k per-item cap |
|---|---|---:|---:|
| 010 | Iron Ore | 46,965 | **47%** |
| 014 | Iron Ore | 40,641 | 40% |
| 013 | Iron Ore | 38,900 | 39% |
| 017 | Neon Gas | 26,431 | 26% |

At the observed two-day rate `010` caps its iron ore in roughly **2-4 days**.
Plan a drain; do not discover it as a silent stall.

**⭐ Measurement limits, so nobody re-derives a bad rate from this table:**
- `agent_storage_items` keeps only the NEWEST capture per agent — there is no
  time series to fit. The only baseline is whatever a previous note recorded.
- Capture is daily, so any reading can be up to 24h stale (this one was 15h).
- **Do NOT `server-cmd` these bots for a live read.** They are active in the mb
  fleet and the login would `session_replaced` them out of it.

**Output mix** (all ten + haven): Iron Ore 140,822 · Copper Ore 94,441 · Neon
Gas 63,230 · Argon Gas 59,084 · Hydrogen Gas 35,375 · then a long tail incl.
Platinum 3,036 / Rhodium 1,534 / Palladium 1,367 from 015.

⭐ **No energy crystals, and that is CORRECT, not a miss.** Crystals are an
INPUT to the drone refit, mined by `random-3` out of ivorygate — see
[[project_drone_project_crystal_reserve]]. Nothing these bots produce advances
the 3,607-crystal shortfall; do not conflate the two.

Incidental: an **Advanced Drone Bay** and three **Mining Laser I** are sitting
in drone-bot storage.

See [[reference_drone_bay_is_agent_wide]] ·
[[project_drone_project_crystal_reserve]] · [[reference_station_id_aliases]]
