---
name: reference_repeater_reflection_damage
description: "Weaponless EM 'beam' rows in hostile_attacks (repeaters) are REFLECTION damage sent back at the attacker, not a weapon; pane looks flat (max 15/hit), spire looks percent-of-incoming (0–24)"
metadata:
  node_type: memory
  type: reference
  originSessionId: 8c15f693-8825-4572-8f22-51141db1e328
  modified: 2026-11-22T01:14:43.346Z
---

The operator confirmed this on 2026-11-21, looking at battle ef64794aae144545fa7013b46a5d07fc (craftsman-1 against Frontier Repeaters at tinsel).

Rows in `hostile_attacks` with an empty `weapon_name` and `damage_type=em` are reflection, not fire from a weapon. The repeater sends part of the damage it takes back to whoever shot it.

The values match two kinds of catalog shield:
- **repeater_pane:** max 15 per hit. That matches a flat retaliation like `nebula_storm_barrier` (shock_attackers_15, "returns 15 EM after every hit").
- **repeater_spire:** 0–24 per hit. That matches a percent of incoming energy like `solarian_aegis` (reflect_energy_20, "20% of incoming energy damage as EM; caps 75%").
- **repeater_sliver:** 1 per hit.

The repeaters' hostile_loadouts list only weapons, so the reflection looks innate rather than a fitted module.

What this means:
- Energy weapons feed the spire's reflection.
- Exclude these rows from repeater offense totals.
- They count as damage taken by our ship, not as shots the repeater chose to fire.

The capture code (pkg/battlereplay `HostileAttacks`/`attacksByType`) does not label them yet. They come out with an empty weapon_name.

Related: [[reference_battle_log_api_replay_data]], [[reference_combat_damage_pipeline]]
