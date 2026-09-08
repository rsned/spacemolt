---
name: reference_poi_resources_remaining_is_unreliable
description: "poi_resources.remaining=0 does NOT mean a POI is mined out — proven by Haven's commerce_fields, which reads 0 while actively yielding all five of its resources"
metadata:
  type: reference
---

**`poi_resources.remaining = 0` is a capture artifact, not depletion.** Never
route away from a POI on the strength of it.

**Proof (2026-09-08).** `commerce_fields`, the Haven asteroid belt
marketbot_haven's drones have been working continuously, reads `remaining = 0.0`
on all five of its resources. Its storage at that moment held exactly those five
— 694 `trade_crystal`, 235 `copper_ore`, 193 `iron_ore`, 96 `silicon_ore`, 72
`nickel_ore`. The belt is demonstrably producing while the table calls it empty.

Only **97 of 2,430** rows read 0, so a zero looks like a meaningful signal rather
than a gap, which is exactly what makes it dangerous. It nearly rerouted two
marketbots off `ironhearth_fields` (0 across all five ores) onto ice.

**Use `richness` instead.** It is populated everywhere and behaves sensibly —
`the_old_seam` iron_ore 85 against `ironhearth_fields` 42/35/34/30/9 correctly
ranks the single rich vein above the diverse-but-thinner belt.

`max_remaining` is 0 on every row inspected — also never captured. Belongs to the
same family as [[reference_capture_loss_taxonomy]]: a table that has never held
real data in a column looks identical to one reporting a real zero.
