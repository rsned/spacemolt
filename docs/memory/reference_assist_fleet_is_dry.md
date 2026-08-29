---
name: reference_assist_fleet_is_dry
description: assist-* are Tanker-class refuellers that dispense from their TANK via a default refueling_pump — 3 of 5 have run their own tanks to 0-4 and one is not in a tanker at all
metadata:
  type: reference
---

**assist-* have ALWAYS been refuelling assets** (operator, 2026-08-29).

**⭐ THIS WAS ALREADY KNOWN — see [[project_assist_fleet_refueling_pump_gap]]**,
which recorded the operator's 2026-08-15 correction (a Capacity transferred +420
fuel to salvager-10 with NO fitted pump). That file was not referenced in
MEMORY.md, so it was never recalled, and the fact was re-derived on 08-29 after
first asserting the opposite. Check that file before touching assist.

**⭐ Tankers dispense from their TANK, not from cargo.** `refueling_pump` is a
DEFAULT MODULE on the class — `capacity` ships `[refueling_pump,
shield_booster_ii]`, `reserve` ships `[refueling_pump]`. So **`cargo_used: 0` on
an assist agent is EXPECTED and means nothing**; a Tanker's 80-150 cargo was
never the dispensing medium. (I diagnosed the empty hold as the fault first —
it is not.)

State 2026-08-29:

| agent | hull | class | tank | verdict |
|---|---|---|---|---|
| assist-haven | reserve | Tanker t3 | 4000/4000 | OK, full |
| assist-frontier | capacity | Tanker t2 | 1500/1500 | OK, full |
| assist-nexus | capacity | Tanker t2 | **0/1500** | ran ITSELF dry |
| assist-krynn | siphon | Tanker | **4/140** | dry; tiny tank for a tanker |
| assist-sol | theoria | **Miner t0** | **0/100** | lost its Tanker, see below |

**Three of five are immobile**, so nothing rescued krynn or nexus because the
rescuers are stranded ([[reference_rescue_queue_blocks_launch]]). The real faults
are a tanker that ran its own tank to zero, and assist-sol having LOST its
tanker.

**⭐ assist-sol is the whole failure taxonomy acting on one agent.** Its
`capacity` Tanker (1500 fuel) was **destroyed in combat at `algol`,
police_level 0 Lawless, 2026-08-15T18:26:59Z** — one of the 24 losses, all of
which were in Lawless space. It was replaced by a `theoria`: a tier-0 **Miner**
starter with a 100-fuel tank, which cannot do a refueller's job. It then ran
dry, and has sat at 0/100 with 2 credits — unable to move OR buy fuel — for
**14 days unnoticed**, because a docked agent at zero fuel is invisible to every
health check. Chain: Lawless routing -> no combat reaction -> tier-0 starter
replacement -> ran dry -> invisible.

**assist-sol is invisible to health checks**: alive and heartbeating at 0/100
with **2 credits** — cannot move, cannot buy fuel, and `Stalled()` early-returns
while docked ([[reference_docked_zero_fuel_invisible_to_watchdog]]).

**assist-krynn** is stranded at `treasure_cache_gas_plume` — a gas cloud, NOT a
station — so dock-to-dock ship-to-ship refuel
([[reference_ship_to_ship_refuel_works_while_docked]]) cannot reach it. Likely
[[reference_gsa_ship_recovery]], then delete its rescue-queue record.

**⭐ Far bigger tankers exist and appeared in ship listings for 4 of the 5
assist agents** (operator): `plenum` 12,000 fuel · `last_drop` 11,500 (+25% fuel
efficiency) · `endowment` 11,500 · `sustenance` 11,000 · `warbarge` 10,000 —
against `reserve` 4,000 and `capacity` 1,500. An upgrade path exists and is
unbought; assist-nexus alone holds 123,345 credits.

`siphon` is Tanker class but is NOT in `catalog_ships.json` — our catalogue gap,
not a broken ship. **Every ship in the game works; if someone is flying it, it
is valid** (operator) — see [[reference_legacy_ship_classes_erased_by_refresh]].
