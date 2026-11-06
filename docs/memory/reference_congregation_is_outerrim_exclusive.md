---
name: reference_congregation_is_outerrim_exclusive
description: "congregation (1900 cargo, the cheap bulk freighter) is an OUTERRIM-EXCLUSIVE design: it can only be bought or commissioned at the 7 outerrim shipyards, and shipyard_tier 0 / no-skill-gate does NOT mean buildable anywhere"
metadata:
  node_type: memory
  type: reference
  originSessionId: 1b7fc3b3-6ee8-4585-a6a2-3682b47901b2
  modified: 2026-11-03T03:23:06.486Z
---

**Found 2026-10-10.** `commission_quote congregation` at The Levy Customs
Station (a tier-capable shipyard) returned:

> `Congregation is a outerrim-exclusive design. Commission it at a outerrim
> shipyard, or license and build it at your own faction station.`

**UPDATE 2026-11-02: CRFT OWNS A CONGREGATION LICENSE.** craftsman-1 ran
`buy_ship_license congregation` at tick 2301382: 1,000,000 cr from the
treasury (tiers: T1=1M T2=5M T3=10M T4=20M T5=50M; prayer is T1,
frankenhauler T4). CRFT can now build congregations at its own stations
(Conjunction Junction, [[project_cloverfield_home_station]]). Each build
also pays a 10% royalty on the hull's raw material value (~662 cr, so
materials are worth ~6.6k) to the outerrim treasury, on top of materials
and labor. The outerrim-only geography below no longer applies to CRFT
re-hulls. Run `commission_quote congregation` at CJ before the first build.
**First CJ build delivered 11-02:**
- The faction-funded commission charged the treasury 1,000 labor + 723 royalty, with materials drawn from faction storage.
- The finished hull went to craftsman-1. `faction_deposit_items <ship>` then stored it in the **faction garage** (`faction_garage_store`, CJ capacity 20), ship_id 5a814eec70e655de0930da2ec3e85a53.
- Any member docked at CJ can take it with `switch_ship`, which claims ownership. This is the way to hand a fresh hull to a hauler.

Prayer is deliberately NOT licensed (operator 2026-11-02): both are T1 and
congregation carries ~3.5x the cargo (1900 vs 540). Prayer's only edge is
speed (4 vs 6 ticks/jump) and range (33 vs 18 jumps); revisit only if a
time-critical or long-range line needs it.

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
