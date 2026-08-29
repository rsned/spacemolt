---
name: reference_mining_supported_power_gate
description: Each POI resource has a supported_power cap; mining succeeds only while your summed mining-module power is BELOW it
metadata:
  type: reference
---

Every resource on a POI carries a **`supported_power`** value. When you `mine`,
the server sums the power of your fitted mining/harvesting modules. **Mining
succeeds only while that sum is LESS than the resource's supported_power.**

So it is a **ceiling gate, not an efficiency score**: an over-fitted miner is
locked OUT of low-capacity deposits. Live example, Frostmarket Flats (haven,
ice_field) 2026-08-28:

| resource | richness | remaining | supported_power |
|---|---|---|---|
| nitrogen_ice | 60 | 36,212 | 1810 |
| water_ice | 75 | 29,906 | 1495 |
| carbon_dioxide_ice | 18 | 7,999 | 399 |

A rig summing >399 cannot take the CO2 ice at that site. Combined with
[[reference_mining_yield_is_random_from_site_mix]], this likely explains wasted
mine actions: the draw lands on a resource the fit is gated out of.

**We capture NEITHER side.** `supported_power` is in every `get_location` reply
(already issued by `update_poi`) but is absent from `serverapi.POIResource`,
`game.POIResource`, and the `poi_resources` table. Per-module mining power lives
in the fitted-module data from `get_ship` (cf. `survey_power: 60` on
survey_scanner_ii) and `items.power_bonus` is 0 for every mining module, so the
catalog cannot supply it either — see [[reference_ship_modules_never_captured]].

`richness` here is a **0-100** scale (75/60/18), which may differ from what
`get_poi` returns — check before writing both into one column.
