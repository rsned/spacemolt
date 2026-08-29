---
name: reference_ship_module_costs_scale_with_engineering
description: "Module CPU/power costs in the catalogue are BASE values reduced 1% per Engineering level, and Engineering only trains while fitting is >=90% of CPU or Power."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 00cd813a-f76a-48cf-bb7a-0c47c76e1566
  modified: 2026-08-27T02:49:37.691Z
---

Catalogue CPU/power are **base** figures. Effective cost is per-pilot:

```
effective = round(base × (1 − engineering_level/100))
```

Verified 2026-08-26: `adaptive_shield_iii` base **12 CPU / 22 power**; on a
pilot with Engineering 28 the live instance reported **9 / 16**
(`12 × 0.72 = 8.64 → 9`, `22 × 0.72 = 15.84 → 16`). My first guess — that
`quality`/`wear` explained the gap — was wrong.

Fleet spread is wide, so a fitting plan must be per-agent, not global:

| agent | eng | adaptive_shield_iii cost |
|---|---|---|
| fighter-5 | 44 | 7 CPU / 12 power |
| miner-4 | 35 | 8 / 14 |
| craftsman-1 | 28 | 9 / 16 |
| most of the fleet | 12 | 11 / 19 |

A level-44 pilot fits the same module for **58% less power** than a level-12
one. Only **36 of 161 agents have Engineering at all** — the other 125 pay full
base cost. `agent_skills` coverage itself is fine (159/161 agents, refreshed
daily by `CaptureProfile` → `ReplaceSkills`); a missing skill row means the
agent genuinely lacks that skill.

## Engineering is passive and self-limiting

It grows **1 XP/tick whenever ship fitting is ≥90% of CPU or Power**. That
creates a trap: as Engineering rises, every module gets cheaper, so a fixed
loadout drops below 90% and XP stops. To keep training you must keep adding
modules. This is almost certainly why the fleet clusters at exactly level 12 and
only two agents ever passed 35.

Hulls are badly under-fitted — **~1.4–1.7 modules** against 4–6 slots. A single
`adaptive_shield_iii` at base cost on a `prospect` (13 CPU / 26 power) is 92% of
CPU, over the line immediately. We hold ~200 idle adaptive shields
(27 III / 101 II / 72 I), so fitting them would both raise shield pool and
restart Engineering XP on 125 agents. Tier choice runs *inversely* to skill: a
high-Engineering pilot needs a BIGGER module to stay over 90%.

## Where the numbers live

- `items.size` — populated for all 210 modules (adaptive shields and
  `magnetic_ore_separator` are all size 10). NOT joined into `item_modules`,
  which is why it looks missing.
- `ships.cpu_capacity` / `ships.power_capacity` — hull budgets.
  `eviction_notice`: 35 CPU / 85 power, 5 weapon / 2 defense / 3 utility slots.
  `prospect`: 13 / 26.
- `agent_hulls.modules` — a **count**, not a list; tells you how many modules a
  hull carries, never which.
- `ship_modules` — still **0 rows**, always [[reference_ship_modules_never_captured]].
  A `get_ship` dump is the only way to see what is actually fitted, with
  quality/wear and true per-pilot cost.
- `agent_skills.xp` is unreliable — 23 non-zero values in 1,185 rows. Levels are
  trustworthy; XP is not.

Related: [[reference_combat_damage_pipeline]] · [[project_fleet_drone_refit]]
