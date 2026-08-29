---
name: reference_combat_damage_pipeline
description: "The battle log's full damage pipeline, verified stance/reach/skill mechanics, and the finding that adaptive shields applied ZERO damage reduction."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 00cd813a-f76a-48cf-bb7a-0c47c76e1566
  modified: 2026-08-27T06:44:31.835Z
---

`get_battle_log` attack records expose the whole damage pipeline by name:

```
raw_damage → pre_hit_damage → stance_mult → after_stance
           → after_def_buff → flat_reduction_pct → final_damage
```

Verified live 2026-08-26 (battle `9c8a84f72703bfdbcf6d7aff50e7b888`, craftsman-1
vs a Crusher-Mantis in Goldcrest, plus fixture `log_a2619bbe`).

## ⭐🟢 2026-08-27 — v0.564.1 FIXED IT. The zero-reduction finding was a SERVER BUG.

Patch note v0.564.1, verbatim points:
- "Offense and defense modules now apply their described combat effects."
- "Adaptive Shields now provide both their listed shield capacity and adaptive
  damage resistance."
- "Damage resistance is applied in this order: **shield skill, typed resistance,
  then flat/adaptive resistance**. Bonuses within the typed and flat/adaptive
  buckets **add together up to 75% per bucket**, with **integer truncation at
  each stage**."
- "Battle logs now show exact per-weapon defense stages and final shield/hull
  damage, including mixed-weapon volleys and defense-bypassing effects."

**This answers the stacking help request the operator filed.** Stacking is
ADDITIVE within a bucket, capped at 75%/bucket, truncated to int at each stage.
So 2 x `adaptive_shield_iii` (35% each) = 70%, under the cap — and
`damage_control_system` (10% flat) lands in the SAME flat/adaptive bucket, so a
third module would hit the 75% ceiling, not 80%.

**Everything below dated 2026-08-26 describes the BUGGED server.** The
measurements were real (raw 60 -> final 60 with 2x adaptive_shield_iii fitted),
but the conclusions drawn from them are now void:
- ~~"Do NOT plan fits around adaptive resistance"~~ — adaptive resistance works now.
- ~~"all ~200 adaptive shields we hold are dead stock"~~ — they are live again.
  We hold 27 III / 101 II / 72 I.
- ~~"shield_booster_iv strictly dominates the adaptive line"~~ — booster_iv is
  still cheaper per point of POOL (8/15 vs 12/22 for the same +200 shield), but
  it carries no resistance, so the two now trade off rather than one dominating.
- The Rainbow Leviathan math below (117 incoming/tick on the Event Horizon fit)
  assumed no adaptive resistance and is now PESSIMISTIC. Recompute before using.

Re-derive any fit from scratch, and use the new per-weapon defense-stage output
in the battle log to verify rather than trusting the old fixtures.

## ⭐🔴 Adaptive shields did NOT reduce damage

craftsman-1 fought with **2 × Adaptive Shield III** fitted (confirmed in the
`modules` list of every snapshot) and took **zero** reduction:

- every mantis hit: `raw_damage 60 → final_damage 60`
- **no `flat_reduction_pct` field present at all** on those attacks
- shield stepped 600→540→480→420→360, exactly −60 each
- `damage_taken: 300` = 5 hits × 60 raw

`eviction_notice` has `base_shield` 200; in-battle `max_shield` was 600. So the
two modules contributed **+400 shield pool** (their `shield_bonus: 200` each)
and nothing else. `adaptive_resistance_35` never fired.

Do NOT plan fits around adaptive resistance. Our KB's
`item_defenses.damage_reduction` is empty for all three adaptive tiers, which is
accurate rather than a capture gap; the 35% lives only as an opaque string in
`item_modules.special` (`adaptive_resistance_35`) and does nothing observable.

**The fixture's `flat_reduction_pct: 10` was a `damage_control_system`**, not an
adaptive shield (operator-confirmed 2026-08-26). That is the ONLY module with
`damage_reduction` populated (10.0), it is **damage-type agnostic** (it reduced
energy 42×, void 4×, kinetic 3× in the fixture), and it is cheap: **4 CPU /
5 power**, `defense` slot. We hold **267**. Whether multiples stack is an open
question — a help request is filed; test by fitting two and reading
`flat_reduction_pct` (10 = no stack, 20 = additive).

## ⭐ shield_booster_iv strictly dominates the adaptive line

Same +200 shield at **8/15** vs adaptive_shield_iii's **12/22**. The pattern
holds at every tier (shield_booster_iii = 100 shield at 5/10 vs
adaptive_shield_i = 60 shield at the same 5/10). Since adaptive resistance does
nothing, **all ~200 adaptive shields we hold are dead stock**. We own 432 ×
shield_booster_iv, 209 × iii, 548 × i, 85 × ii.

## Rainbow Leviathan fight math

2200 hull, 0 shield, **130 energy/tick, hits at zone_distance 4** (unlike the
mantis, range does NOT protect you). No energy hardener exists, so
`damage_control_system` is the only mitigation. Ticks to kill = 2200/DPT;
damage taken = 130 × ticks. An `eviction_notice` (1,080 EHP) needs 2,600 and
dies around tick 8 of 20.

Working solo fit on a **voidborn_event_horizon** (tier 4, 900 hull / 1000
shield, 85 CPU / 210 power, 4 weapon / 5 defense / 5 utility slots, piloting 30
required — craftsman-1 has exactly 30), at engineering 28:
4 × void_laser (40 CPU) + 4 × shield_booster_iv (24) + 1 × damage_control_system
(3) = **67/85 CPU**, EHP 2,700, incoming 117/tick, 17 ticks → 1,989 taken,
~711 spare. Note 67/85 = 79%, BELOW the 90% Engineering-training threshold —
fill ~10 more CPU of utilities to keep the passive skill running.

`void_laser` is the only weapon with reach 5 AND top DPT (32.5; cooldown
matters — `pulse_laser_iii` is 28.0 DPT and `piercing_railgun_ii` only 21.7).
Craftable via `build_void_laser` (Legendary, facility_only); stock caps us at
**4**, bound by exotic_crystal (21) and refined_quantum_matrix (16).

## Verified mechanics

- **Stance**: values are `fire` and `brace`. `stance_mult` is 1 under fire,
  **0.25 under brace** — but brace also means **0% outgoing damage**, so you can
  turtle indefinitely and never win. That makes brace ideal for UNARMED hulls
  (miners, haulers), which give up nothing, and useless for fighters. Pair with
  retreat; snapshots carry a `flee_counter`.
- **Reach gates firing entirely.** `item_weapons.reach` is the max
  `zone_distance` a weapon can fire at. `pulse_laser_iii` reach 3 → at
  `zone_distance 4` craftsman-1 made **no attack at all**, and opened up only at
  2. Long-reach options we own: `piercing_railgun_ii` (65 kinetic, reach 5, 7 of
  them at frontier_station), `piercing_railgun_i` (reach 5),
  `heavy_pulse_laser` (reach 3). Catalogue tops out at reach 6 (torpedo/missile
  launchers) and 5 (siege_railgun, heavy_mass_driver, both 120 damage).
- **Distance sets hit chance for both sides.** Mantis hit chance 0.30 at
  distance 4, 0.60 at 2, **0.85 at 0**. Closing triples incoming fire.
- **weapons + gunnery are additive at 1%/level.** craftsman-1 has weapons 2 +
  gunnery 2 = +4%: `4 × Pulse Laser III = 112 raw → 116 final`, exact
  (`112 × 1.04 = 116.48`). Gunnery or weapons alone would give 114, so both
  summing is proven. The log's own `weapon_skill_pct` field reads **0** despite
  the bonus applying — that field is unreliable.
- Creatures get no skill bonus: mantis `weapon_skill_pct 0`, raw 60 = final 60,
  perfectly flat every hit.

## Crusher-Mantis

320 hull, **0 shield**, 60 kinetic per hit (raw), `Crusher-Mantis (natural)`
weapon, habitat `asteroid_belt`, role predator, "hunts ships". Died in 6 ticks
to 4 pulse lasers. Dangerous only to unarmed hulls — 60/hit clears a ~100-shield
miner in two hits. Contrast [[project_wildlife_combat_intelligence]]'s Leviathan.

## The live worker has NO combat code

Everything in `pkg/worker` touching battles is capture-only (`hunt_wildlife.go`
reads `LastBattleID`; `wildlife_attacks.go` drains a queue). The `advance`
commands come from the **server-side autopilot** (`auto_pilot: true`, server
supplies its own `reason`). Making a miner brace and hold range would mean
building a worker combat loop that does not exist. `pkg/spar` has policies
(`aggressor`/`skirmisher`/`retreater`/`dummy`) but is a test harness nothing
live consumes. Unresolved: whether client battle commands override the server
autopilot at all — probe before designing.

Related: [[reference_battle_log_api_replay_data]] ·
[[reference_creature_scan_bracket_threat_flag]] ·
[[reference_ship_module_costs_scale_with_engineering]]

## ⭐🟢 2026-08-27 — v0.566.0 unhid 26 modules. Re-derive every fit.

The catalog refresh (item_modules 210 -> 235) exposed these. Highlights, with
CPU/power and base value:

**Defence / sustain**
- `nebula_bio_regenerator` 6/10, **1,600cr** — +40 hull AND **repairs 8 hull per
  tick**. Cheap sustain; this changes any attrition fight far more than a flat
  resistance point, because it scales with fight LENGTH.
- `event_horizon_plating` 8/6, 40,000 — +48 armor, +100 max hull, no speed penalty.
- `nebula_storm_barrier` 8/14, 18,000 — +80 max shield, returns 15 EM after a hit.
- `emergency_cloaking_system` 8/12, 4,700 — triggers after shields hit 0.

**`ecm_jammer` 8/12, 3,800, reach 3, jam_strength 40, needs scanning 4**
NEW item, not established as a replacement for anything. `ecm_jammer_pod`
(consumable, 170cr) did vanish from the catalog in the same patch, but that is
two separate facts — nothing in the notes ties them, and no migration or refund
was announced for the pod. Treat them as unrelated until the devs say otherwise.
"Each fitted jammer independently has a 40% chance per battle tick to
break your selected target's lock within reach 3. Multiple jammers roll
independently: two give a 64% chance." So 3 = 78.4%. **Preventing the shot beats
reducing it** — but note reach 3, and the Rainbow Leviathan attacks at
zone_distance 4, so jamming it means closing to 3 where its hit chance rises.
Unverified whether that trade nets out.

**Weapons** — `void_lance_i` 10/20, 55 void, **ignores 50% of typed resistance**
(newly meaningful now resistance actually applies); `graviton_beam_ii` 12/22,
50 kinetic +50% conditional; `energy_siphon` 12/15, drains shields and returns
half as energy; `entropy_beam` 9/18; `storm_lance` 10/18, 40 EM repeating.

**Fitting headroom** — `quantum_computer_core` **0 CPU**/8 power adds "massive
CPU"; `void_reactor` 5 CPU/**0 power**; `power_reactor_upgrade` 2/0. These break
the CPU/power ceiling that capped earlier fits, and one ecm_jammer (8 CPU) is
exactly the ~10 CPU the Event Horizon fit needed to cross the 90% Engineering
training threshold [[reference_ship_module_costs_scale_with_engineering]].

**Also new:** `crystal_mining_probe` 5/10 extracts energy crystals — the drone
refit's only shortfall was 3,607 energy_crystal [[project_fleet_drone_refit]].
`modulated_mining_laser`, `outer_rim_salvage_array`, `salvage_module`,
`tractor_beam`, `ship_maintenance_bay` (refit/repair in space),
`sensor_dampener`, `crimson_berserker_core`. Four leisure modules
(`onboard_galley` 9k, `promenade_lounge` 26k, `grand_dining_hall` 68k,
`stellar_observation_deck` 140k) suggest a passenger economy —
[[project_passenger_demand_intel]].
