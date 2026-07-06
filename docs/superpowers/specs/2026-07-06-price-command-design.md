# `price` command — suggested sell price from component/ore market rates

**Date:** 2026-07-06
**Status:** Approved design, pending implementation plan
**Component:** New `play_as` REPL command + `pkg/pricing` (pure core).

## 1. Purpose

Answer, for any craftable **item**: *what should I charge in `create_sell_order`?* — by pricing the item's inputs at live marketbot rates and adding a profit margin. As a side effect it surfaces **underpriced** items (parts cost ≪ current market price), which is what motivated the feature: a KB "did_you_know" pass found ~87% of items can't be built from market supply today, and among those that can, many finished goods sell for far more than their part cost.

Reader goals:
1. **Suggested price** — component/ore market cost + a separate 20% margin line → the per-unit price to hand to `create_sell_order`.
2. **Underpricing signal** — compare the suggestion against the item's own current market price.
3. **Feasibility** — flag components not actually sold within reach.

## 2. Command

```
price <item_id> [--margin=20] [--hops=2] [--mode=both|recipe|bom] [--json]
```

- **Per-unit output.** `create_sell_order` takes a per-unit price, so every build cost is divided by the recipe's output-units-per-run to yield a per-unit figure.
- **Defaults:** margin **20%**, nearby radius **2 hops**, mode **both**.
- **Scope:** **items only** for MVP. Ships' recipe parts (`comp_*`/`refined_*`) are not market-traded (0/54 per the kb build-cost analysis), so only ship-BoM-from-ore would be priceable; passing a ship prints a "ships not supported (recipe parts untraded)" note and stops. Deferred.
- If the item has **no recipe** (raw ore / uncraftable): print "not craftable — no recipe" and show only the current-market section.

## 3. Cost model

For every component we compute two independent price bases, then roll them up.

### 3.1 Two cost bases (per component)

- **Nearby** — cheapest sell **ask** at any station within `--hops` jumps of the current system. Reuses `finditem.Find` (already returns sellers ranked by jumps then price); take the cheapest whose `Jumps <= hops`. If none within the radius → the component is a **shortfall** (nearby cost is partial, feasibility flags it).
- **Market-wide avg** — mean of each station's best ask across the latest captures, via `market.Collector.GetItemStationPrices(itemID)` (its `BestAsk` per station). Location-agnostic fair value; never a shortfall as long as any station lists it.

Pricing rule (MVP): **single best ask × required qty** — NOT an order-book depth-walk. The 20% margin absorbs thin-book slop. Revisit depth-walk later (tracked in memory `project_price_command_depthwalk`); the kb `cmd/generate-build-costs` matrix depth-walk is the reference implementation if we upgrade.

### 3.2 Two decompositions (both shown by default)

- **Recipe mode** — one-level recipe inputs (`recipe.Inputs`). For items with **multiple recipes** (46 of them), evaluate each recipe and let the **cheapest-feasible win per basis**; the chosen recipe id/name is named in the section header (the winner can differ between Nearby and Market-wide, and that's surfaced).
- **BoM mode** — fully-decomposed base ores via the crafting DB (`playAsSource.BOM([recipeID])` → `[]craftplan.BOMRow`).

### 3.3 Roll-up (per basis, per decomposition)

```
build_cost/run  = Σ (component_qty × unit_price)         [shortfall components contribute 0 + flag]
per_unit        = build_cost/run ÷ output_units_per_run
margin_amount   = per_unit × margin_pct/100              [shown as its own line]
suggested_price = per_unit + margin_amount
```

## 4. Output

Four reference lines, all requested: item's current market price, margin-vs-current delta, per-component breakdown, feasibility/shortfall.

```
price hull_plating   (margin 20%, nearby = ≤2 hops from sol)

RECIPE  recipe_hull_plating → 5 units/run          [nearby winner; mkt winner if different is noted]
  COMPONENT        QTY    NEARBY    MKT-AVG
  iron_ore          20      12.0       14.2
  copper_ore         8      30.0       28.5
  ---- build cost/run       540.0      598.0
  ---- per unit (÷5)        108.0      119.6
  ---- + 20% margin          21.6       23.9
  = SUGGESTED PRICE         129.6      143.5     ← create_sell_order
  feasibility (nearby): 2/2 components sold within 2 hops ✓

BOM (ore)   … same shape; feasibility over the ore leaves …

CURRENT MARKET  hull_plating
  nearby   ask 450   bid 400        mkt-avg ask 470
  → market ask is 3.5× your suggested price — UNDERPRICED
```

- **Classification:** compare current market ask to the suggested (nearby) price. `> ~1.3×` → `UNDERPRICED`; `< ~0.9×` → `OVERPRICED` (you'd sell below cost+margin); otherwise `FAIRLY PRICED`. Thresholds are cosmetic and tunable.
- **`--json`** emits the structured `PriceReport` for scripting instead of the table.

## 5. Code structure

Mirrors the `find_item` split (pure core in a package, thin REPL glue in the command).

### `pkg/pricing` (pure, unit-tested)
Deps: `market.Collector`, `knowledge.Base`, `navigation`, `finditem`. **No** `game.GameClient` or `craftplan` dependency — decomposition is passed in.

- `type Component struct { ItemID string; Qty float64 }`
- `type PricedComponent struct { Component; NearbyUnit, MktUnit float64; NearbyFound, MktFound bool }`
- `type Basis struct { BuildCost, PerUnit, Margin, Suggested float64; Covered, Total int }` (one for nearby, one for mkt)
- `type PriceReport struct { ItemID, RecipeName string; OutputUnits int; Components []PricedComponent; Nearby, Mkt Basis; CurAskNearby, CurBidNearby, CurAskMkt float64; Class string }`
- `func Report(ctx, col *market.Collector, kb knowledge.Base, fromSystem string, hops int, itemID, recipeName string, outputUnits int, comps []Component, marginPct float64) (*PriceReport, error)`
  - Prices each component nearby (`finditem.Find` filtered to `hops`) and market-wide (`GetItemStationPrices`), rolls up both bases, fetches the finished good's current price, classifies. Called once per decomposition.

### `cmd/tools/play_as/price.go` (REPL glue)
- Parse args/flags.
- Resolve the recipe(s) producing `item_id` (reverse output→recipe lookup over the cached catalog via the existing `craftplan`/`playAsSource` plumbing). Multi-recipe: build a component list per candidate recipe; `pkg/pricing` picks cheapest per basis (glue loops candidates, keeps the min).
- Build the **recipe-input** component list (`recipe.Inputs`) and the **BoM-ore** component list (`playAsSource.BOM`).
- Call `pricing.Report` for each enabled mode; render tables (or `--json`).
- Register `case "price":` in `main.go` dispatch + a help line in the usage text.

## 6. Testing

- `pkg/pricing` unit tests with a fake/seeded `market.Collector` + in-memory KB: roll-up math, per-unit division, margin line, shortfall handling (missing nearby component → covered<total, contributes 0), multi-basis, classification thresholds. Table-driven.
- `cmd/tools/play_as` format test for the rendered table (golden-ish, following `craft_format_test.go` / `find_item` patterns), and the no-recipe and ship-rejection paths.

## 7. Out of scope (MVP)

- Order-book depth-walk pricing (memory `project_price_command_depthwalk`).
- Ships.
- Persisting/scheduling suggestions; this is an on-demand query only.
