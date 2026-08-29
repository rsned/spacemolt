---
name: craft-batch-skill-based
description: Craft batch size is now based on player's Crafting skill level instead of hardcoded 10
type: project
---

Updated craft batch sizing on 2026-03-19.

**Why:** Server changed to allow batch size equal to Crafting skill level (min 1 for level 0-1), replacing the old fixed max of 10.

**How to apply:**
- `MaxCraftBatchSize(state)` in `pkg/game/crafting.go` returns the skill-based limit
- Used in `CraftWithQuantity()` validation, `craftRecipe()` loop, and `CraftItems()` storage loop
- Skill ID is `"crafting"` in `state.Player.Skills` map
