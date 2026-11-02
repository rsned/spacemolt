---
name: reference_facility_speed_rules
description: Recipe crafting_time = hand-craft baseline; L1 facility 5x, each upgrade 3x more; faction admin facility +15%/tier at its station (2 tiers); recruitment +5% output/level; v0.656 rent = 3% build cost/day (x2 pirate)
metadata:
  type: reference
---

Operator-confirmed + measured at Conjunction Junction, 2026-10-31:

- **ticks/run = crafting_time ÷ (5 × 3^(level-1) × (1 + 0.15 × admin_tier))**
  - recipe `crafting_time` is the hand-craft baseline (Station Workshop divides by
    skill factor 1.0-5.0)
  - L1 facility 5x, L2 15x, L3 45x, L4 135x
  - CJ L1s ran exactly crafting_time ÷ 5: piping 0.36, steel 0.11, fuel 0.60,
    nodes 1.80
- **Faction admin** (Faction Desk T1, Faction Office T2; only two tiers): +15% per
  tier speed for the faction's production at that station. Haven's T2 office =
  1.30x (combustor 0.46 vs CJ 0.60).
- **Recruitment** (Hiring Board L1 → Grand Recruitment Bureau L5): +5% output per
  level. Not visible in the combustor's 100 fuel/run; how it applies is unconfirmed.
- **Rent v0.656.0:** production, faction and service facilities pay 3% of build
  cost per day, x2 at pirate stations. Tenant lockboxes at our station showed
  860/day.
- One day = 8,640 ticks.

Cost model: `.beads/003-production-cost-model.md`. Move plan:
`docs/superpowers/plans/2026-10-31-move-faction-office-to-conjunction-junction.md`.

**Rebuilding from packages (2026-10-31):** packages return MATERIALS only; the build
cost in credits is charged again. Desk (100k) + upgrade to Office (500k) at CJ cost
600k. Upgrade syntax: `facility faction_upgrade --facility_id <id> --facility_type
<target> --package_ids @file`; all three flags are required. A build with an
incomplete package set still succeeds: the server takes the missing share from
ordinary faction storage (Carbon Arc Furnace, 70 of 81 packages).

**Hand-craft queues are per agent (operator, 2026-11-01).** Every agent has its own independent hand-craft queue, so splitting hand-crafted work across several agents docked at the same station finishes it proportionally faster. Only production facilities have one fixed work queue. How to apply: don't stack long hand-craft jobs (process_water_ice, electrolyze_water, grow_sunspindle) on one bot; at Conjunction Junction split them between miningbot_0000, marketbot_home and any other officer docked there (craft input comes from own OR faction storage, never both).
