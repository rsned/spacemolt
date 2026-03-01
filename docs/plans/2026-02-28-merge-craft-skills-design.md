# Merge Craft Skills Design

**Date:** 2026-02-28
**Status:** Approved

## Problem

Two separate craft skills (`craft_items` and `craft_from_storage`) exist with nearly identical structure. The only difference is the material source: cargo vs station storage. Since crafting should always pull from station storage first then ship cargo, a single unified skill is sufficient.

## Design

### Unified Behavior

The merged `craft_items` skill always:
1. Deposits current cargo to station storage (if any)
2. Queries craftable recipes from the combined storage pool
3. Withdraws materials, crafts in batches, deposits results
4. Loops until nothing more can be crafted (max 10 iterations)

This is the existing `CraftFromStorage` behavior, which already handles the full workflow.

### Changes

#### Layer 1: Go implementation (`pkg/game/crafting.go`)
- Rename `CraftFromStorage` to `CraftItems`
- Remove `CraftFromCargo` (absorbed into `CraftItems`)
- Keep `QueryCraftableRecipes` and `QueryCraftableFromComponents` unchanged

#### Layer 2: Dispatcher (`pkg/skills/client_dispatcher.go`)
- Replace `craft_from_cargo` and `craft_from_storage` dispatch cases with single `craft_items` case calling `CraftItems`
- Update `isTickAction` to list `craft_items` instead of the two old names

#### Layer 3: Callers (`pkg/game/mining.go`)
- Update `StationActionCraftAndSell` and `StationActionCraftAndDeposit` to call `CraftItems` instead of `CraftFromCargo`

#### Layer 4: Skill YAML (`data/skills/`)
- Update `craft_items.yaml` action from `craft_from_cargo` to `craft_items`
- Delete `craft_from_storage.yaml`, `.dot`, `.svg`
- Update `SKILLS.md` and `skills_next_steps.md`
