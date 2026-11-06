---
name: project_hull_production_costs
description: "Private hull-costs page (KB private/production-costs/hulls.html) merged 2026-11-03; shipyard quote/license capture via migration 66 — NOT yet applied to the live DB, so labor shows ? and licenses not owned until it is"
metadata:
  node_type: memory
  type: project
  originSessionId: 1b7fc3b3-6ee8-4585-a6a2-3682b47901b2
  modified: 2026-11-03T10:00:09.505Z
---

**Shipped 2026-11-03, local main in both repos. Not pushed.** Spec and plan: `docs/superpowers/specs/2026-11-02-hull-production-costs-design.md` and `docs/superpowers/plans/2026-11-02-hull-production-costs.md`.

**The page:** `go run ./cmd/generate-production-costs` (KB repo) writes `private/production-costs/hulls.html`. It has one row per hull (all 370):
- **Our make cost** for the materials, under the prodcost rules: ore 0, hand 0, labor and fee counted. Crystal recipes are allowed and flagged. Facility-only steps with no usable line are costed at L1 labor and flagged "facility not built".
- **Salvage:** flagged and left unpriced, so the total is a floor ("≥").
- **Other columns:** labor (by quote, else tier quote/range, else "?"), royalty (10% of OUR make cost), license with its tier cost, materials at the cheapest asks, and the cheapest listing within 3 days with its count.

**Why:** the operator wants to know build-vs-buy per hull now that CRFT can license designs ([[reference_congregation_is_outerrim_exclusive]]).

**How to apply:**
- **Migration 66** (`shipyard_quotes`, `ship_licenses`, plus the seeded CRFT congregation license) is merged but NOT applied to the live DB. Apply it deliberately with `bin/kb-migrate -db data/spacemolt-knowledge.db` before workers restart onto the new code.
- **Capture:** from then on, every `commission_quote`, `commission_ship` and `buy_ship_license` reply from play_as, workers or auto-explorer is recorded. Labor fills in per tier as agents quote at capitals and strongholds. Quotes are location-dependent: a yard only quotes hulls it is licensed for.
- **Live check 11-02:** the Congregation row matches the hand worked example (materials 984 = sheet 400 + containers 288 + tanks 185 + plating 111, royalty 98).
- **Partial hulls:** 46 of 370 are partial. 42 list salvage directly. 3 come from trade_crystal (catalog "ore" with no extracted_by): ascendancy, exchange, hegemon. 1 is larder, which lists creature_carapace directly.
- **Parked:** unbuilt routes are promoted only when an item has no usable route. Ideally that would read "no complete usable route", but completeness can't be judged in `settle()` without order dependence. This is latent with today's data.
