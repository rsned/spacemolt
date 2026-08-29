---
name: project_wildlife_combat_intelligence
description: "Wildlife attack capture (wildlife_attacks) wired 3dea78d7 — battle logs mined for damage type/amount; Rainbow Leviathan = 130 energy/tick beam, and NO energy hardener exists in the catalog"
metadata: 
  node_type: memory
  type: project
  originSessionId: 9acefb51-d097-42a4-bd75-27e0bb652f32
  modified: 2026-08-18T00:31:24.224Z
---

**Goal (operator, 2026-08-17): full knowledge of how to DEFEND against wildlife
and how to DEFEAT it.** Damage type is the hinge — it is what a resistance
module is chosen against.

## SHIPPED `3dea78d7` — the capture
`wildlife_attacks` and `battlereplay.WildlifeAttacks` both already existed and
the table had **zero rows**: nothing joined them. Now:
- `State.LastBattleID` survives the end of a battle. **A death clears
  BattleState, and there is NO way to enumerate your own past battles** — the log
  of the fight that killed you is reachable only through an id you kept.
- Kills record the battle id, which makes "battles with a kill and no attack
  rows" a **derived queue** — no queue table, idempotent re-run.
- Worker command **`capture_wildlife_attacks`** drains it (budget 5 battles) or
  reads one named battle. Log fetching is deliberately NOT in the fight loop:
  each battle is a paged `get_battle_log` + summary, every page waits to settle.
- Creatures missing from the caller's get_nearby map resolve via display names
  already in `wildlife_species` — that is what makes an **ambush** usable, since
  a death happens without any get_nearby. Unknown names are skipped, never
  slugged into a phantom species.

**NOT yet scheduled on any agent.** Needs a schedule entry + worker restart, same
as [[project_action_log_capture]].

## ⭐🔴 Rainbow Leviathan — the full profile
From battle `13ec2c95ba853a657b00385cf2683d49` (salvager-7's shard, Goldcrest):

| | |
|---|---|
| hull / shield | **2200** / 0 |
| weapon | "Rainbow Leviathan (natural)", **beam** |
| damage | **130 energy per tick**, 2 shots 2 hits |
| damage taken | **zero — the player never got a shot off** |

- **Dead in 2 ticks = 20 seconds.** The action log said arrived 02:59:49Z,
  destroyed 03:00:09Z. Two independent sources agree exactly.
- It **closes and drags you in**: outer→mid→inner, `reason: advance` for it and
  **`reason: pulled_closer`** for the player, every tick. Player autopilot chose
  `hold`. Starting `zone_distance` was 4 — it out-ranges a starter loadout, so
  the player was in range of the beam while never being in range to reply.
- Shields **do** absorb energy (`shield_damage: 40` on shot 1), so a
  `shield_hit: 0` in a live push means the shield was already down, not that
  energy bypasses shields. Armor mitigates only a few points (shot 2: 130 →
  `hull_damage` 124; shard `base_armor` 8). **Do not derive the formula from 2
  shots** — shot 1's arithmetic does not reconcile cleanly.

## ⭐🔴 There is NO energy hardener
Weapons deal 6 damage types: kinetic 18, **energy 14**, em 10, explosive 9,
thermal 8, void 7. `item_defenses.resistance_bonus` is a JSON map and hardeners
exist for **five**: em/explosive/kinetic/thermal/void (hull 30%, shield 25%,
void 20%). **Energy — the second most common type, and the Leviathan's — has no
resistance module at all.**

- Only type-agnostic mitigation: `damage_control_system`, **flat 10**. 130 → 120.
- Biggest EHP adds: `quantum_entanglement_shield` / `quantum_shield_iv` +250
  shield, `nanocarbon_hull_lattice` +250 hull, `adaptive_shield_iii` +200.
- Theoria = 100 hull + 55 shield; Shard = 110 + 40. **Both die to 2 beams.**

⚠ **Possible catalog gap, not confirmed game truth:** `reactive_armor_hardener`
and the `adaptive_shield_*` line have an **EMPTY** `resistance_bonus`. If those
are adaptive hardeners, they may be the energy answer and our catalog simply
lacks their data. Verify before concluding energy is unresistable.
[[reference_catalog_items_tradeable_drift]]

## ⭐ What a Leviathan is WORTH (2 kills, reported by another player)
Same four items both times; only the quantities move:

| item | kill A | kill B | best bid | where |
|---|---|---|---|---|
| **prismatic_carapace** | **1** | **1** | **3,680** (56 deep) | **Hex Star, Dheneb** |
| rainbow_leviathan_flesh | 53 | 36 | 41 (515 deep) | sol_central |
| void_ichor | 6 | 8 | 41 | sol_central |
| creature_carapace | 12 | 8 | 22 (257 deep) | sol_central |

**≈ 5,600–6,400 credits per kill**, and **prismatic_carapace is ~60% of it at
exactly 1 per kill** — the quantity never varied, so it looks like a guaranteed
single drop rather than a roll. Needs two stations to liquidate.

- `prismatic_carapace` (legendary, base 3,200) feeds `pan_galactic_alloy_synthesis`
  → `pan_galactic_matrix` (base **2,600**) along with 2× prism_charged_ichor and
  2× void_charged_ichor (620 each). **Crafting it destroys value — sell raw.**
  Note the recipe wants the CHARGED ichors; the drop is raw `void_ichor`, and no
  recipe in the KB produces the charged form ([[reference_bom_table_lossy]]).
- **`leviathan_heartcore` (legendary, 3,500) did NOT drop in either kill** but
  exists and feeds `leviathan_core_reactor`. There is a rarer tier we have not
  seen; true EV is above the figure above.
- **`rainbow_leviathan_flesh` is missing from the items catalog** even though it
  has a 515-unit bid — a real catalog gap
  ([[reference_catalog_items_tradeable_drift]]). Its 100,000 ask is fantasy: 34
  sell orders, best bid 41.

⚠ **Deliberately NOT inserted into `wildlife_kill_drops`.** These are another
player's screenshots, and looted quantities are capped by hold space — the drop
table records carcass CONTENTS with `CarcassRead` precisely to keep the
denominator honest. Hearsay would corrupt the one thing that design protects.

**Operational conclusion so far: you cannot tank a Leviathan, and you cannot
out-range it. Defence is not being in the fight** — which matches
[[reference_belt_grazer_hunting_grounds]] and the per-arrival risk found in
[[project_action_log_capture]]. See also
[[reference_creature_scan_bracket_threat_flag]] for the hull table.
