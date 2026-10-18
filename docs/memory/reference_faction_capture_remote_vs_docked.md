---
name: reference_faction_capture_remote_vs_docked
description: "Faction STORAGE reads remotely (view_faction_storage station_id); faction FACILITY detail does NOT — `facility` has no station param, so it needs a pinned resident"
metadata:
  type: reference
---

**The two faction captures have different reach, and conflating them wastes
days.**

| what | command | remote? |
|---|---|---|
| faction storage contents | `view_faction_storage(station_id?)` | **YES** — "without being docked" |
| faction facility detail | `facility(action=faction_list)` | **NO** — no station/base param among its 33 |
| that facilities exist | `faction_info` | yes, but no detail at all |

**Storage WRITES and gifts are docked-station-local too.** `withdraw_items`,
`deposit_items` and `send_gift --source storage` have NO station parameter:
"personal station storage" means the station you are docked at, and nothing
else. Tested 2026-10-17 — craftsman-1, docked at Voss, tried to gift 1 of the
200 energy_crystal it holds at `cargo_lanes_freight_depot` and got
`insufficient_storage: You only have 0 x energy_crystal in storage.`

So an agent must FLY to a station to move what it owns there.
`view_faction_storage(station_id)` is the single remote read in this family;
do not generalise it.

**The constraint is on the SENDER, not the recipient.** `send_gift` delivers
into the recipient's storage at the sending station regardless of where the
recipient is; it sits there waiting for them to arrive. So a collection point
can hand goods to a hauler that is still days away, and no rendezvous or
simultaneous docking is needed — only the sender must be docked where the
items are.

Worked example: marketbot_alhena is pinned at `voss_redoubt` with drones
mining adamantite_ore into its storage. It can gift the ore to a hauler
immediately; the hauler collects on arrival. Factory Belt is 5 jumps from
Alhena and bids 1,600 for adamantite_ore (2x base 800), the same price as
every other buyer, so distance is the only discriminator.

So rent/cycle, level, status, recipe binding and capacity for a faction
facility are obtainable ONLY while docked at that station. **No code can
substitute for a pinned resident.** Verified against the v0.632.0 openapi
request body 2026-10-17.

## Storage capture had a seed bug (fixed `f6c92de6`)

`capture_faction` is elected to ONE member per faction —
`IsFactionCaptor` = `MIN(player_id)` among members with a profile fresher than
24h. Every other member's daily `capture_faction` is a deliberate no-op that
returns after `get_status`, which looks identical to a working capture in the
logs (338 ms, no `faction_info` call).

The seed asked `DockedAtBase` then `HomeBase`. For CRFT the captor is
craftsman-8, **homed at `crimson_war_citadel`** — a Crimson empire station
where CRFT holds nothing — and it sits UNDOCKED at an ice belt. The seed
station refused, the fetch failed, and the whole capture returned nil. It
fired daily and wrote nothing while `voss_redoubt_station`'s pocket stayed
four days stale, across two facility builds.

Now: docked station -> HomeBase -> every base the ledger knows the faction
holds, first success wins (the hint on any one enumerates them all).

**The election is unstable by design.** It is a pure function of player_id
ordering plus profile freshness, so the captorship moves silently whenever a
lower-sorting member is profiled. Do not assume a particular agent holds it.

## agent_storage_items is PERSONAL only

This cost three wrong answers in one session. `agent_storage_items`
(assets.db) is the per-player station pocket. The faction pocket is
`faction_storage_items`, and it was essentially uncaptured. Figures read off
the personal table and presented as available to a faction facility are wrong
by the whole faction pocket — `faction_build` and faction facility crafting
draw from the FACTION pocket and ignore personal storage.

Moving between pockets at one station needs no hauling:
`faction_deposit_items <item> <qty> --source storage` pulls from personal
station storage, needs no permission (only withdrawals need
`manage_treasury`).

Related: [[reference_faction_facility_rent_mechanics]] ·
[[reference_capture_loss_taxonomy]]
