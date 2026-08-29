---
name: reference_bom_table_lossy
description: crafting.db bill_of_materials ceils fractional quantities to INTEGER and does not propagate has_alternatives — never trust it for costing
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2f3b8937-e63d-42aa-8015-c67d52bc5fd2
  modified: 2026-07-25T23:31:10.713Z
---

**`bill_of_materials` in the sibling `spacemolt-crafting-server` DB is lossy in two ways. Do not cost anything from it.**

**1. Quantities are ceiled at write time.** The column is `quantity INTEGER NOT NULL`, so true fractional per-output-unit values are rounded UP when the row is written. The loss happens before any consumer reads it and cannot be undone downstream. For `ghost_rounds`:

| component | true per unit | stored | error |
|---|---|---|---|
| plasma_gas | 1.5 | 2 | +33% |
| titanium_ore | 7.5 | 8 | +6.7% |
| copper_ore | 2 | 2 | — |

(Rows ARE per **output unit**, not per run — that part is correct and easy to misread. `forge_ghost_rounds` yields 2/run, and the stored row is the per-unit figure.)

**2. `has_alternatives` does NOT propagate up the tree, and `recipe_path` records only the top-level recipe.** `copper_wiring` has `has_alternatives=1` and `recipe_path=["process_copper_wiring"]`, but `hot_cell` and `ghost_rounds` — whose expansions pass straight through that same choice — both store `has_alternatives=0` and a single-entry path. So a consumer cannot tell that a route decision was made, or which way it went. 43,580 of 48,015 rows carry the flag, so this is the common case, not an edge case.

The practical consequence: the BoM silently resolved `copper_wiring` via the **facility-only** `process_copper_wiring` (2 ore/unit) when a hand-crafter needs `basic_copper_processing` (8 ore/unit) — a **4× understatement**, undisclosed. Combined with the ceiling it can overstate and understate simultaneously.

**Fix already applied to `price`** (2026-07-25, `484f3c7`): `cmd/tools/play_as/bomexpand.go` expands the recipe tree directly from `catalog_recipes.json` with exact fractional arithmetic, picks the cheapest route per branch (ties → facility recipe → recipe ID), and prints the chosen route plus a facility-access warning. **Anything else reading `bill_of_materials` still has this bug.**

Cross-check source: `data/game-api/latest/catalog_recipes.json` — full `Recipe` objects under `items` (see [[reference_catalog_recipes_shape]]). Facility routes are the accessible optimum for this operation because craftsman-1 owns most production facilities [[project_smuggling_enablement]].

Root defect lives in the crafting-server repo, not this one — schema change + transitive alternatives flag would be needed there. [[reference_spacemolt_kb_shared_db]] [[project_crafting_brain]]
