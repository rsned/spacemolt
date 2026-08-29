---
name: reference_kb_bom_explorer_alphabetical_default
description: "The KB Bill-of-Materials explorer picks recipes ALPHABETICALLY when an item has several producers. Treat any export without explicit r= overrides as an upper bound, not a plan."
metadata:
  node_type: memory
  type: reference
---

When an item has more than one producing recipe, the KB BoM explorer
(`kb/cmd/generate-bom-explorer`, `?r=item:recipe,...`) defaults to **alphabetical**
selection. It is not cost-aware.

**How badly this bites:** the same campaign exported two ways differed by **13x** in raw
materials. `mining_drone x800` came out at 199,476 carbon_ore because `draw_diamond…`
sorts before `spin_optical_fiber` and `fuse_diamond_glass` before `fuse_reinforced_glass`
— both diamond recipes, each diamond costing 30 carbon. The correct picks needed 7,476.

**How to spot it:** an export with a large `r=` override list was hand-tuned and is
probably fine. An export with none took the alphabetical default. The two exports for the
[[project_fleet_drone_refit]] differed exactly this way — the drone-bay one carried
overrides, the mining-drone one did not.

**Rule:** before trusting any BoM, list every alternative producer for the expensive
intermediates and compare, preferring hand-craftable (`facility_only=0`) recipes — the
cheap alternative is frequently ALSO the one that needs no facility.

```sql
SELECT ro.recipe_id, ro.quantity AS yield, r.facility_only,
       group_concat(ri.item_id||' x'||ri.quantity, ', ') AS inputs
FROM recipe_outputs ro JOIN recipes r ON r.id=ro.recipe_id
LEFT JOIN recipe_inputs ri ON ri.recipe_id=ro.recipe_id
WHERE ro.item_id='<item>' GROUP BY ro.recipe_id;
```

Also: the explorer treats `energy_crystal` as a raw/base material even though
`synthesize_energy_crystal` exists — correct, since that recipe is ruinous, but don't be
surprised when it appears under "raw materials required".
