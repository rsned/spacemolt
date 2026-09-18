---
name: reference_mission_chain_maps
description: "Known mission chains and their givers; chain_next arrives ONLY in the complete_mission reply, so an unobserved link is lost permanently unless someone reads it live"
metadata:
  type: reference
---

## ⭐🔴 `chain_next` has exactly ONE source
Verified 2026-09-16/17 against both stores:
- `action_log_events` (`data/assets.db`) carries **no chain key** on any of the
  five `mission.*` event types.
- `mission_templates.chain_next` is populated on **76 of 11,080 rows (0.7%)**,
  from board listings (`MissionBoardEntry.ChainNext`).

`play_as` renders the hint in `formatCompleteMission` then **discards the bytes**
— there is no capture wiring in play_as. Every other completion fact survives the
next `capture_action_log`; the chain link does not. **Read it when it appears or
lose it.** [[project_action_log_capture]]

Gotcha: a completion reply's `mission_id` is the PROCEDURAL INSTANCE hash
(e.g. `77aa3261caa24814213489e53114c98f`), not the template slug, and it carries
no `template_id`. The only join back to `mission_templates` is the **title**.
Also logged as `complete_mission (unhandled)` in the action switch
([[reference_rawjson_key_drift]]).

## Smuggling chain — the pirate unlock (givers operator-confirmed)
```
treasure_cache (is_stronghold=0, police 30 — a -30 agent CAN dock)
  no_questions_asked -> across_the_line -> an_introduction -> supply_run
    -> expanding_operations -> building_trust -> a_meeting_at_sable_port
barnard_44 / sable_port_station   (ONLY giver)
  through_the_fire -> leap_of_faith
orphan link: smugglers_route -> end_of_the_line
```
`an_introduction` IS the unlock (baseline -30 -> 10), so only the first three
steps matter and all three are at treasure_cache.
[[project_pirate_reputation_unlock_campaign]]

## Grand Circuit — trading/delivery, nebula reputation
```
first_links (HEAD, nothing points to it) -> crossing_borders
  -> frontier_extension -> ??? UNKNOWN
  ... -> closing_the_circuit (orphan TERMINUS, nothing points to it)
```
- `first_links` completed by **craftsman-1, 2026-09-17 17:29** (+4000cr,
  navigation 15, trading 25, nebula rep +2). Its giver flavour names
  **Market Prime and Cargo Lanes** as the first two links, and says the next
  test is "extending the route beyond Federation space".
- ⭐ **`frontier_extension` is given at `alpha_centauri_colonial_station`**
  (system `alpha_centauri`, solarian, police 80, is_stronghold=0) — learned
  2026-09-18 from the REFUSAL, see the technique below. It is still absent
  from `mission_templates` and has 0 rows in `mission_template_locations`;
  its `chain_next` is learnable only from the completion reply.
- Route 2026-09-18: craftsman-1 was at `cargo_lanes`, **12 jumps out**
  (cargo_lanes → bunda → copernicus → keelbreak → zibal → gsc_0009 → alfirk
  → dubhe → maplevale → miaplacidus → mimosa → tau_ceti → alpha_centauri).
  No stronghold on the route, and craftsman-1 holds the pirate unlock
  (baseline 10 on all nine), so the routing rule does not bite here.
- `closing_the_circuit` exists with no `chain_next` and no predecessor, so at
  least one step between `frontier_extension` and it is also unmapped.

## ⭐ TECHNIQUE: `mission_not_available` names the giver
`accept_mission <id>` for a mission you are not standing at is refused with
`code: mission_not_available` and a message that **names the station**:

> "This mission is only available at Alpha Centauri Colonial Station."

That is a free, one-tick probe for any mission id, and the ONLY way we have
found a giver for a mission that has never appeared on an observed board.
`mission_template_locations` is populated from board captures only, so a
mission nobody has seen listed has no location row — but it will still answer
this probe. The reply names the station in PROSE (display name, not the id);
resolve it via `pois.name` ([[reference_station_id_aliases]]).

Nothing captures these refusals today.
