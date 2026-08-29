---
name: project_ship_fitting_calculator
description: Offline ship-fitting calculator (pkg/fitting + cmd/tools/fit) — design decisions and open validation TODO
metadata: 
  node_type: memory
  type: project
  originSessionId: 04732b6c-502d-4abf-833c-75d96cdc3a76
---

Built 2026-06-07 on branch `feat/ship-fitting-calculator`: `pkg/fitting` (reusable) + `cmd/tools/fit` CLI. Answers "how many of module X fit on ship Y" (`MaxFit`, iterative so capacity-adding reactors/rigs raise the budget) and "which ships fit N of X" (`ShipsThatFit`). Plus `CheckFit` for arbitrary loadouts.

Non-obvious decisions (not visible from code alone):
- **Data source = catalog JSON snapshots** at `data/game-api/latest/` (catalog_ships/items/skills.json, top-level `items` array), NOT the SQLite KB — the KB schema drops `slot`, `cpu_bonus`/`power_bonus`, `required_skills`.
- A **module** = any catalog_items entry with a non-empty `slot` field. type `mining` → `slot:"utility"`.
- **Engineering** skill: `bonus_per_level` keys `cpuEfficiency`/`powerEfficiency` (1%/level). Effective usage = `ceil(base * (1 - 0.01*level*effPerLevel))`, floored at 0 — server **rounds UP**.
- **Bare-hull** assumption: `default_modules` ignored (player refits from scratch). `--eng_skill_level` flag defaults 0.

**Open TODO / risk:** the ceil rounding is assumed, NOT validated against the live server. If the server floors or rounds-to-nearest, boundary counts differ. Validate against a real ship before trusting exact max counts. Live-character skill loading (load an agent) is the planned next iteration; only `--eng_skill_level` exists now.

Spec: `docs/superpowers/specs/2026-06-07-ship-fitting-calculator-design.md`. Plan: `docs/superpowers/plans/2026-06-07-ship-fitting-calculator.md`.
