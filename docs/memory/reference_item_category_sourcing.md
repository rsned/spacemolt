---
name: reference_item_category_sourcing
description: "Item sourcing is decided by items.category — ONLY category='ore' is mineable; category='misc' is wildlife-hunt loot. Verified against poi_resources."
metadata: 
  node_type: memory
  type: reference
  originSessionId: c80d54b4-4ed6-42da-b9a0-488c730ee085
---

**`items.category` is the authoritative sourcing rule.** Confirmed by the operator and verified against the KB (`data/spacemolt-knowledge.db`) on 2026-07-12:

- **`ore` → MINEABLE.** 52 items, and *all 52* appear in `poi_resources` (100%). Despite the name, this category also contains every gas, ice, and crystal: `argon_gas`, `hydrogen_gas`, `helium_ice`, `deuterium_ice`, `energy_crystal`, `exotic_matter`, `dark_matter_residue`, `void_essence`… Do NOT infer mineability from the item's *name* or its suffix — only from `category`.
- **`misc` → WILDLIFE HUNT.** Loot dropped by creatures. Not mineable, and typically has **no market sell-side at all**.
- Every other category (`component` 135, `refined` 105, `ammo` 46, `consumable` 45, `contraband` 8, `drone` 5, `material` 2, and 210 uncategorized/modules) has **zero** mineable items → source by craft or buy.

The check is a one-liner: **mineable ⟺ `items.category = 'ore'`.** Equivalently, presence in `poi_resources` — the two agree perfectly, no exceptions.

**Why this matters:** the crafting-brain planner ([[project_crafting_brain]]) has no mineability lookup, so it will happily emit a `mine` node for an unmineable input. On 2026-07-12 it picked recipe `verdigris_smelting` (4 `verdigris_curd` → 3 `copper_piping`) and emitted a `mine` node for `verdigris_curd` — a `misc`/wildlife item with zero `poi_resources` rows and zero market sell orders anywhere. The node failed 3× and hard-blocked the `air-recycler` smoke plan; one worker (craftsman-10) stranded itself on the futile mining trip. See [[project_executor_b_rollout]].

**How to apply:** when sourcing a recipe input, classify by category first — `ore` → mine, `misc` → hunt (or treat as unobtainable for planning), else craft/buy. When two recipes produce the same output, prefer the one whose inputs are actually obtainable (`draw_copper_piping` = 12 `copper_ore` + 2 `steel_plate` is the mineable sibling of `verdigris_smelting`).
