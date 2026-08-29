---
name: reference_assist_tanker_migration
description: How Tanker-class hulls work and the 2026-08-14 migration where the operator hand-flew four assist pilots to buy their tankers — purchase leg vs the switch_ship "commissioning" leg, tier gates, prices, and the assist-nexus never-boarded trap
metadata:
  type: reference
---

**RECONSTRUCTED 2026-08-29 from session transcripts** (`db74e972` 08-14,
`9acefb51` 08-15) after the operator noticed the memory was gone. It was never
saved at the time — the 08-14 session discussed all of this and wrote nothing;
08-15 appended only a short correction to
[[project_assist_fleet_refueling_pump_gap]]. Do not let that happen again:
when the operator hand-flies pilots, write the memory THAT session.

## How a Tanker works (operator-confirmed)

- **`refueling_pump` is BUILT IN** — a default module on every Tanker-class
  hull (`capacity` = `[refueling_pump, shield_booster_ii]`, `reserve` =
  `[refueling_pump]`). Never buy or fit one for a tanker. Proven live 08-14:
  assist-frontier's Capacity gave salvager-10 **+420 fuel** with nothing fitted.
- **It dispenses from its TANK** via `refuel --target <username> --quantity N`,
  not from cargo. `cargo_used: 0` on an assist agent is normal.
  [[reference_assist_fleet_is_dry]]
- **A bought tanker arrives FULL** — buying one is itself a refuel.
- **One big rescue can drain it**: the +420 transfer took frontier from ~483 to
  ~53, and its home desk (`mobile_capital`) was dry, so the rescuer became the
  strandee. Size transfers against the home reserve
  ([[project_refueler_ship_roadmap]]); know the nearest WET desk
  ([[project_station_fuel_reserve_capture]]).
- **Tier is gated by Piloting**: tier 2 (Capacity/Last Call/Long Haul/
  Morningstar, 1,500 fuel, 75-80 cargo) needs 10; tier 3 (Reserve, 4,000 fuel,
  150 cargo) needs 20; tier 4 (Plenum 12,000 · Endowment/Last Drop 11,500 ·
  Sustenance 11,000 · Warbarge 10,000) needs 30. All five assist agents were
  Piloting 27 on 08-14 — tier 3 eligible, three levels short of tier 4.
- Burn is scale-based, not flat: predicted ~6/jump Capacity, ~11/jump Reserve
  (formula runs pessimistic) — the pipeline's flat `rescue.FuelPerJump=5`
  misprices tanker trips. [[reference_ship_jump_time_and_fuel_formulas]]
- `siphon` (assist-krynn's 140-fuel hull) IS Tanker class — a small legacy one
  missing from `catalog_ships.json`. It works; it is just tiny.

## The 08-14 migration — what the operator actually did

**Supply**: only **4 tanker listings existed galaxy-wide** (marketbots'
`browse_ships` capture, matched to live by listing id). Player-station
shipyards are invisible to `BrowseShips`, so there was no fifth to find.

| buyer | hull | price | where | jumps |
|---|---|---|---|---|
| assist-haven | **Reserve** (t3, 4,000) | 336,384 | Market Prime Exchange | 1 |
| assist-sol | Capacity (t2, 1,500) | 104,622 | Nova Terra Central | 1 |
| assist-nexus | Capacity | 104,045 | Procyon Colonial (solarian) | 9 |
| assist-frontier | Capacity | 105,260 | Alpha Centauri Colonial | 12 |
| assist-krynn | — none left; "save it for next available" (operator) | | | 18+ |

All three Capacities were in **Solarian** space, which is why assist-nexus had
to leave Nexus Prime to buy its own.

**Procedure** (each step was a real gate that day):
1. **Fund** with `send_gift` from an agent whose session is free — explorer-1
   was quarantined so it needed no fleet stop. Gifts sized hull + fuel +
   working capital: 380k haven, 150k each sol/nexus/frontier, 50k krynn.
   Recipients are usernames (`shipside_assist_<x>`). Sender must be docked.
2. **Drain/stop the assist overmind** before any `play_as`, or the worker and
   the operator fight for the session (`session_replaced`).
3. **Fuel the pilot first** — frontier sat at 0/120, krynn at 1/140; a dead
   pilot cannot go collect a hull. `services` listing `refuel` means the desk
   EXISTS, not that it has stock (Frontier Station, Ironlight, Carnegie Hall
   were all dry that day).
4. Operator ran one `play_as <agent> < buy-<agent>.txt` per pilot: autopilot
   to the shipyard, `buy_listed_ship <listing-id>`, ending in `list_ships`.
   Captured prices drift up to ~8% within an hour; budget for it.
5. **`switch_ship <new-ship-id>`** — the id exists only after purchase, so
   this is a SEPARATE step after `list_ships`. **This is the step that got
   skipped for assist-nexus**: it bought the Capacity in Solarian space,
   flew it home, parked it at `central_nexus`, and the worker resumed on the
   old 95-fuel Threshold for a month. Found 08-15 only because the roster
   page showed it on a starter; fixed with one `switch_ship` in a SIGSTOP
   safe window. **The purchase leg is not the commissioning leg — verify with
   a login line reading `Ship: Capacity`.**
6. Relaunch the assist overmind ([[reference_overmind_launch_commands]]).

Timeline: 08-14 11:21 "sol, haven, nexus have gotten tankers"; 16:28
frontier bought its Capacity and returned to `mobile_capital`; 08-15 nexus
switched. Outcome by 08-29: frontier/nexus/sol Capacity, haven Reserve, krynn
still Siphon; **sol's Capacity was then destroyed at algol** (stronghold,
pirate-locked) — see [[feedback_stronghold_routing_requires_pirate_unlock]] —
and it respawned in a Theoria Miner.

**Why:** the assist fleet exists only to deliver fuel; 95-140-fuel starters
could spare ~70 per rescue and stranded themselves doing it.
**How to apply:** replacing a lost tanker = repeat the six steps; check the
`ship_listings` table for Tanker class first, and never skip step 5.
