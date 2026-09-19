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
✅ **FULLY MAPPED AND COMPLETED 2026-09-18 by craftsman-1.**
```
first_links -> crossing_borders -> frontier_extension -> closing_the_loop
  diff 3         4                  5                    5  (TERMINUS, no chain_next)
  4000cr         5500cr*            8000cr               10000cr
  * crossing_borders paid only 2557 of 5500 (shortfall 2943); the other
    three paid in full.
```
Givers: `frontier_extension` and `closing_the_loop` share the SAME giver NPC,
**Route Planner Maren (Federation Commerce Bureau)**, at DIFFERENT stations
(alpha_centauri_colonial_station vs starfall_salvage_station) — a giver name
does not identify a station.
**A SEPARATE chain, not part of this one** (corrected 2026-09-18 — they were
previously conflated because `closing_the_circuit` looked like an orphan):
```
neural_matrix_delivery -> sensor_data_exchange -> federation_payment
  -> closing_the_circuit (TERMINUS, no chain_next)   difficulty 4->5->5->6
```
Note `closing_the_loop` != `closing_the_circuit`. Two different missions with
confusable names; only the latter has a template row.
- `first_links` completed by **craftsman-1, 2026-09-17 17:29** (+4000cr,
  navigation 15, trading 25, nebula rep +2). Its giver flavour names
  **Market Prime and Cargo Lanes** as the first two links, and says the next
  test is "extending the route beyond Federation space".
- ⭐ **`frontier_extension` is given at `alpha_centauri_colonial_station`**
  (system `alpha_centauri`, solarian, police 80, is_stronghold=0) — learned
  2026-09-18 from the REFUSAL, see the technique below. **RECORDED** in the DB
  2026-09-18 22:02 via `bin/mission-bind` (the first and so far only row with
  `exclusive_base_id` set); source stamped `accept_mission_refusal`, tick
  1916322. It now HAS a full `mission_templates` row plus 2
  `mission_objectives` rows (8 power_battery -> deep_range_outpost,
  12 silicon_ore -> starfall_salvage_station), giver **Route Planner Maren,
  Federation Commerce Bureau**, 8000cr / navigation 25 / trading 55.
- ⭐🔴 **A refusal-learned constraint is lost once the mission is accepted.**
  `recordMissionRefusal` only fires on the refusal path, so a mission accepted
  before the binding was written can never be bound automatically — it will
  never be refused again. That is why frontier_extension needed
  `bin/mission-bind -mission X -station "<display name>" -tick N` by hand.
  The tool resolves the display name to a base id and refuses to write if it
  does not resolve, so a bad name cannot become a binding.
- Route 2026-09-18: craftsman-1 was at `cargo_lanes`, **12 jumps out**
  (cargo_lanes → bunda → copernicus → keelbreak → zibal → gsc_0009 → alfirk
  → dubhe → maplevale → miaplacidus → mimosa → tau_ceti → alpha_centauri).
  No stronghold on the route, and craftsman-1 holds the pirate unlock
  (baseline 10 on all nine), so the routing rule does not bite here.
- `frontier_extension` COMPLETED by craftsman-1 2026-09-18 23:30 (tick
  1918673): full 8000cr (no shortfall), navigation 25 / trading 55,
  **solarian +3**. chain_next confirmed `closing_the_loop`.
- ✅ **`closing_the_loop` RECORDED** (source accept_mission_refusal, tick
  1918684) once craftsman-1 saw it on the Starfall board, which finally
  created its `mission_templates` row. Completed for 10,000cr. The
  "giver resolved but no template row" deadlock resolves itself the moment
  an agent visits the giving station — you do not need a stub row, you need
  a visit.
- (historical) **`closing_the_loop` is given at `starfall_salvage_station`**
  (system `starfall`, OUTER RIM) — learned 2026-09-18 23:32 from the refusal
  at tick 1918684. **NOT YET IN THE DB**: it has no `mission_templates` row,
  so `mission-bind` fails with ErrMissionUnknown and the binding lives only
  here. To record it: get an agent to Starfall Salvage Station so the board
  capture creates the row, THEN run
  `bin/mission-bind -mission closing_the_loop -station "Starfall Salvage Station"
   -source accept_mission_refusal -tick 1918684` BEFORE accepting — once
  accepted it will never be refused again.
- 🔴 **Do not read completion flavour as the giver.** The frontier_extension
  completion said "bring it home to Grand Exchange" and the next mission is
  given at STARFALL, not Grand Exchange. The flavour names the DELIVERY
  target of the next leg, not where to pick it up. Only the refusal is
  authoritative about the giver.
- ⭐ **`replacement_survey_lens` is locked to `starfall_salvage_station`**
  (type equipment, difficulty 1, giver **Sinter**, no chain_next) — operator-
  supplied 2026-09-18, RECORDED with source `operator` (not
  accept_mission_refusal: the refusal text was not observed here). One
  location row, starfall only, continuously 2026-09-01..09-18.
- So **Starfall Salvage Station gives at least two exclusive missions**
  (`closing_the_loop`, `replacement_survey_lens`). Outer Rim stations are
  worth probing for more.
- 🔴 Single-location is NOT evidence of exclusivity: 11,365 of 11,389
  templates have exactly one `mission_template_locations` row, so one row is
  the DEFAULT. Only a refusal (or the operator) is authoritative.
- This is the THIRD time the "giver resolved but no template row" gap has
  bitten (frontier_extension, then closing_the_loop). The template row only
  appears once someone SEES the mission on a board, but the refusal that
  names the giver happens when you are NOT there. The two facts are
  structurally never available at the same moment.

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
