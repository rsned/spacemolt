---
name: project_pirate_bands
description: "pirate-1..15 are three deliberate 5-agent brigand bands, dormant on purpose until combat dynamics and fleet combat exist"
metadata: 
  node_type: memory
  type: project
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-08T17:55:56.749Z
---

`data/agents/pirate-1` … `pirate-15` are **three roving bands of five**, designed to
role-play space pirates hunting ships to loot:

- **pirate-1..5** — symbol names (`⚔️`, `☠`, `☢`, `☣`, `⚀ ⚀`); these were the
  non-ASCII naming probes (the server has since disallowed non-ASCII in string
  values, so they are grandfathered)
- **pirate-6..10** — classic brigands (Rocco 'Razor' Redgrave, Blade 'Buccaneer'
  Blackwell); aggression 0.6–0.95
- **pirate-11..15** — a crewed ship, one role each: Captain 'Vortex' Vash,
  Navigator 'Drift' Dax, Engineer 'Salvage' Soren, Tactician 'Vector' Vega, Scout
  'Wraith' Kara. Complementary traits imply coordinated behaviour, not five
  parallel loops.

**They are dormant ON PURPOSE — do not treat them as abandoned or delete them.**
All 15 are registered (credentials, personality, mbox, play_as history) and have
been played by hand. They are blocked on two capabilities that do not exist yet:
understanding/excelling at **combat dynamics**, and fighting as a **fleet**.

**Why it matters:** the fleet currently LOSES to pirates — craftsman-1 alone shows
damage_dealt 20 vs damage_taken 822, pirates_destroyed 0, 3 deaths by pirate.
Running your own raiders means first beating the NPCs that already beat you.

**Band composition (operator intent):** one **electronic-warfare** agent doing
scrambling/disrupting, the rest damage dealers.

**The game supports this fully — verified 2026-08-08 against the catalog and
openapi:**

| Module | Cost | Skill | Effect |
|---|---:|---|---|
| `warp_scrambler` | 4,500 | scanning 3 | "Prevents target from jumping to hyperspace" |
| `warp_disruptor` | 2,900 | scanning 3 | long-range warp prevention |
| `em_disruptor_i` | 3,900 | weapons 3 | speed + damage-output debuff |
| `sensor_jammer` | 460 | — (consumable) | sustained field disrupting targeting locks |
| `ecm_jammer_pod` | 170 | — (consumable) | ECM pulse disrupting targeting |

Mechanics per openapi: warp disruption makes **escape impossible until the tackle
is removed** (`flee` fields are omitted entirely while disrupted); **warp core
stabilizers** are the counter; a **stasis webifier** raises flee ticks and makes
you easier to hit; EM disruption applies timed speed + damage-reduction
multipliers. Tackle keys on **scanning**, NOT weapons — a cheap role to field.

**⭐ The combat-dynamics prerequisite already has its data source: `get_battle_log`
returns per-tick entries for ANY battle, yours or not** — full weapon pipeline
(hit/crit rolls, resist %, damage breakdown), burns, shield/hull regen, fuel burned
evading, flee. Combat can therefore be studied by mining OTHER players' battles
without risking a hull. Companions: `get_battle_status`, `get_battle_summary`
(gained `has_station` in v0.552.0).

**How to apply:** what's missing is behaviour, not config — no `pirate` role in
`roles.yaml`, no raid script in `data/scripts/`, no fleet yaml. The primitives exist
(`get_wrecks`, `loot_wreck`, `salvage_wreck`, `tow_wreck`, battle commands, customs
evasion). Two concrete prerequisites: **`agent_hulls.modules` is a COUNT, not the
loadout** — weapons/shields must be captured before any targeting rule can be
written; and observation first ([[project_battle_visualization]],
[[project_play_as_smart_battle_handler]]), since combat dynamics can't be tuned
unobserved. [[reference_v0536_wildlife_combat]] is the safe place to build weapons
XP. Brainstorm before coding; target selection is the hard part (raiding your own
fleet is self-harm).
