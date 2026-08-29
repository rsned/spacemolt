---
name: reference_supported_power_is_derived
description: supported_power = floor(remaining/20) — derived, not independent data, so the mining ceiling FALLS as a deposit depletes
metadata:
  type: reference
---

**`supported_power` = `floor(remaining / 20)`.** Confirmed on six data points,
two readings 47 minutes apart at Frostmarket Flats (haven), 2026-08-28:

| remaining | /20 | reported |
|---|---|---|
| 29,906 | 1495.3 | 1495 |
| 36,212 | 1810.6 | 1810 |
| 7,999 | 399.95 | 399 |
| 30,141 | 1507.05 | 1507 |
| 36,447 | 1822.35 | 1822 |
| 8,046 | 402.3 | 402 |

**Do NOT add a column for it** — it is computable from `remaining`, which
`poi_resources` already stores.

**Consequence:** since mining needs summed module power BELOW supported_power
([[reference_mining_supported_power_gate]]), the ceiling **falls as a deposit
depletes and rises as it regenerates**. A heavy rig is not permanently locked
out of a site, it is locked out below a threshold: an 8x ice_harvester_iv fit
(480) can work a deposit only while it holds >9,600 units.

Regeneration was visible in the same pair of readings: 29,906 -> 30,141 in 47
minutes, all three resources rising. See [[reference_ore_deposits_regenerate]].

**Scale settled:** `richness` is 0-100 in BOTH get_poi and get_location (75/60/18
for the same deposits), so both may write the same column.

`get_poi` uniquely adds `max_remaining` (the regeneration ceiling: 50000/50000/
9000 here) and `depletion_percent`, plus class/description/position.
