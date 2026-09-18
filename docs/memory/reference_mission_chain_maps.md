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
- ⭐🔴 **`frontier_extension` has NEVER been observed** — zero hits across
  630,257 action-log events, and it is absent from `mission_templates`. Its
  `chain_next` is learnable ONLY from the reply when someone completes it.
  **craftsman-1 is two completions away.** Capture it live or the link is gone.
- `closing_the_circuit` exists with no `chain_next` and no predecessor, so at
  least one step between `frontier_extension` and it is also unmapped.
