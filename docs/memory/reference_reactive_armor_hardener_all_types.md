---
name: reference-reactive-armor-hardener-all-types
description: reactive_armor_hardener = +25% resist to EVERY damage type incl. energy, in the TYPED bucket (multiplies with adaptive/flat bucket); our KB shows it blank (operator-pasted catalog 2026-11-24)
metadata:
  type: reference
---
Catalog entry (operator-pasted 2026-11-24): `reactive_armor_hardener`, defense slot, 6 CPU / 10 power, size 10, base_value 8400, `reactive_resistance: 25`. Description: "Adds 25% resistance to every incoming damage type. Typed and reactive module resistances add and cap at 75%; resistance bypass reduces that total."

- It is the ONLY resist that covers **energy** damage (no energy hardener exists). All frontier repeaters (spire/pane/monolith/sliver) fire energy beams.
- It sits in the TYPED bucket; adaptive shields and damage_control_system sit in the flat/adaptive bucket. Buckets apply in sequence, so they MULTIPLY: reactive 25 + adaptive_shield_ii 20 = 0.75 x 0.80 = 60% damage taken.
- Our KB `item_defenses.resistance_bonus` is empty for it (the field `reactive_resistance` is not captured). Treat it as 25 all-type until the capture is fixed.

Frontier miner plan (11-24): Harmonic + pathfinder_drive + 3 mining_laser_iv, defense = reactive + adaptive_shield_ii + shield_recharger_ii (65/65 CPU).

Related: [[reference-pathfinder-drive-mechanics]]
