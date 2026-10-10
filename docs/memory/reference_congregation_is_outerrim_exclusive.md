---
name: reference_congregation_is_outerrim_exclusive
description: "congregation (1900 cargo, the cheap bulk freighter) is an OUTERRIM-EXCLUSIVE design: it can only be bought or commissioned at the 7 outerrim shipyards, and shipyard_tier 0 / no-skill-gate does NOT mean buildable anywhere"
metadata:
  type: reference
---

**Found 2026-10-10.** `commission_quote congregation` at The Levy Customs
Station (a tier-capable shipyard) returned:

> `Congregation is a outerrim-exclusive design. Commission it at a outerrim
> shipyard, or license and build it at your own faction station.`

**The `ships` table does NOT expose this.** congregation reads
`shipyard_tier 0`, `required_skills {}`, `piloting_required 0`,
`required_reputation` empty — so a catalog read says "buildable by anyone
anywhere". I asserted that and was wrong. There is no `empire`/`faction`
column on `ships` in the live KB that carries the lock; the only way I found
it was the server's refusal. **Always `commission_quote` before routing an
agent to a yard.**

## The 7 outerrim shipyards

Join `base_services` (service_name='shipyard', available=1) to `bases` on
`base_id`, then `pois` on `bases.poi_id`, and filter `bases.empire='outerrim'`:

| system | poi id |
|---|---|
| starfall | starfall_salvage_station |
| first_step | first_step_memorial_station |
| deep_range | deep_range_outpost |
| unknown_edge | unknown_edge_waystation |
| last_light | ramens_rest |
| void_gate | void_gate_outpost |
| horizon | mobile_capital (Frontier Station) |

These are the SAME stations that list congregations for sale — an
outerrim-exclusive hull is sold and built only in outerrim. So the
~6k listings and the commission path share one geography, and
[[reference_outer_rim_mobile_capital_and_marketbot_homes]] applies to the
Horizon one (mobile capital).

**Consequence for re-hulling** ([[project_haul_fleet_rehull_2026_10]]): an
agent far from outerrim cannot get a congregation cheaply at all. Distances
to the nearest outerrim yard on 2026-10-10: trader-8 0 jumps (already at
Unknown Edge), salvager-1 8, salvager-2 10, **trader-7 25**. For the far ones,
prefer waiting for a listing restock at a yard they pass, or pick a
non-exclusive hull.

`bases` has no `system_id` — join through `poi_id`. `base_services` columns
are `base_id, service_name, available`.
