---
name: index-combat-wildlife
description: Combat & wildlife: damage pipeline, flee/retreat, reload/ammo, modules, battle log/replay, wildlife scans, spar, pirate bands, holotable
metadata:
  type: reference
---

# Combat & wildlife

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐🔴 [Combat damage pipeline](reference_combat_damage_pipeline.md) — `reach` gates firing; `brace` = 0.25 dmg, 0% offense; ESCAPE is `battle stance flee`, NOT `battle retreat` (one ring, never exits); worker has NO combat code
- ⭐ [reload = get_ship module INSTANCE id + ammo item](reference_reload_weapon_ammo_pairing.md) — pairing is weapon `ammo_type` == ammo `effect.subtype`; ONE TICK PER MODULE; out_of_ammo repeats every tick. play_as resolves it (`aa511fca`), worker does not
- ⭐ [Module wear/repair REMOVED](reference_module_wear_removed.md) — refitting is consequence-free; 29 agents on starters vs 170 idle hulls
- ⭐ [Module cost scales with Engineering](reference_ship_module_costs_scale_with_engineering.md) — catalogue CPU/power are BASE, −1%/level; trains only at ≥90% fit
- ⭐ [get_battle_log has the FULL reconstruction](reference_battle_log_api_replay_data.md) — positions, stance, loadouts, pipeline, autopilot reasoning; 30 ticks = 1.5MB
- [Battle replay viewer](reference_battle_replay_viewer.md) — spacemolt.com/battles/<id>; don't scrape it
- [Scan bracket = THREAT flag, not role](reference_creature_scan_bracket_threat_flag.md) — FIXED `35ae5f4b`
- [Belt-Grazer grounds](reference_belt_grazer_hunting_grounds.md) — kochab/ironpeak, NOT khambalia
- ⭐🔴 [get_nearby on arrival reads EMPTY](reference_wildlife_arrival_race.md) — 18% false-negative; hunt fleet rejected good grounds. Fixed `238f73a7`; table is `wildlife_surveys`
- [Combat roadmap](project_combat_roadmap.md) — entered via wildlife/First Hunt, not PvP; the spar testbed exists but is dormant
- [Spar testbed](project_spar_testbed.md) — cmd/tools/spar + pkg/spar, controlled PvP harness; BUILT, dormant
- [Ship fitting calculator](project_ship_fitting_calculator.md) — pkg/fitting + cmd/tools/fit; open validation TODO
- [Pirate bands](project_pirate_bands.md) — pirate-1..15 = three 5-agent bands, DORMANT on purpose
- [play_as smart battle handler](project_play_as_smart_battle_handler.md) — DEFERRED: auto-retreat instead of dying passively
- ⭐ [Battle holotable visualizer](project_battle_holotable_visualizer.md) — P1b shipped; NEXT = P2 record sheet; battle API is CORS-open but needs a logged-in session
- [Battle visualization](project_battle_visualization.md) — longer-term top-down radar, after-the-fact then live
