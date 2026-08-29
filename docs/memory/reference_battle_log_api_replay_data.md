---
name: reference_battle_log_api_replay_data
description: "get_battle_log returns a COMPLETE tick-by-tick reconstruction of any battle (x/y positions, zones, hull/shield/fuel, stance, targets, module loadouts, full damage pipeline, autopilot reasoning) — far more than spacemolt.com's own battle page renders; enough to build a custom visualizer"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 9acefb51-d097-42a4-bd75-27e0bb652f32
  modified: 2026-08-16T17:42:06.335Z
---

**Measured 2026-08-16 against battle `a2619bbe328676445828b4e1007fe9aa`** (11 v
30 + a station, Node Beta, 30 ticks, 42 participants, 10,293 damage).

**Yes — there is enough data to build your own visualizer, and the same API the
website uses is open to us.** `get_battle_log(battle_id, limit?, tick_start?,
tick_end?)` is documented as "the same detail spectators see on the website" and
works for **any** battle, active or completed, yours or not.

**Volume:** the entire 30-tick battle came back in ONE call at `limit: 200`
(max) — **1.5 MB of JSON**, `has_more:false`. Budget ~50 KB per tick per ~28
live participants; a 200-tick brawl would need pagination via `tick_start`.

**Per-tick `BattleLogEntry` sections** (row counts from this battle):
`snapshots` 840 · `autopilot` 840 · `zone_moves` 405 · `attacks` 371 · `regen`
33 · `kills` 14 · plus `joins`, `flee`, `fuel`, `burns`, `commands`,
`battle_ended`.

**`ParticipantSnapshot` — one per participant per tick, the visualizer's spine:**
`x`, `y` (floats; observed 0.50..2.66 / -1.07..1.87), `zone`
(**outer/mid/inner/engaged**), `hull`/`max_hull`, `shield`/`max_shield`,
`fuel`/`max_fuel`, `stance`, `target_id` (draw the targeting lines),
`auto_pilot`, `kind` (**player/pirate/police/drone/creature/station**),
`side_id`, `ship_class`, `username`, `player_id`, `damage_dealt`,
`damage_taken`, `kill_count`, `flee_counter`, and **`modules`** (fitted loadout,
name + category).

**`AttackLogEntry` is a full damage pipeline**, not a total: `hit_chance` +
`hit_roll` + `hit_success`, per-weapon `WeaponFireDetail` (`base_damage`,
`ammo_used`, `crit_chance`/`crit_roll`/`crit_fired`, `after_disruption`),
`raw_damage` → `pre_hit_damage` → `shield_resist_pct` / `type_resist_pct` /
`flat_reduction_pct` / `stance_mult` / `off_buff_pct` / `def_buff_pct` /
`capital_bonus_pct` → `shield_damage` + `hull_damage` = `final_damage`, plus
`zone_distance` (observed 0..5), `splash`, `disrupted`, `damage_type`.

**`autopilot` gives the AI's REASONING** — `chosen_target` + `reason` per
participant per tick. That is strictly more than the website shows and is the
real prize for debugging combat behaviour ([[project_pirate_bands]]).

**The website page itself is thin:** the rendered DOM is ~2,900 chars — a
roster (name / ship class / damage) plus totals. It is a Next.js RSC app
(no `__NEXT_DATA__`, no `/api/` XHR to scrape), so **do not scrape it — call the
API**. `get_battle_summary` gives the same headline block (sides, participants,
outcome, `has_station`, `top_damage`, `destroyed_names`, `duration_ticks`).

**Live streaming:** `status` is `active`|`completed`, so poll
`get_battle_log(tick_start=<last+1>)` while active. Mind the 429 in the spec.

**Gap to fill first:** our Go client has NO typed `GetBattleLog` — only
passthrough (`command_coverage_test.go` calls it a stopgap). Add the command per
CLAUDE.md's pattern (client method → interface → runner dispatch →
`isActionCommand` false, it is a read) before building anything on it.
Probe used for this write-up: `RawCommand(ctx, "get_battle_log", …)` +
`GetRawJSON("_last")`. See [[reference_battle_replay_viewer]].
