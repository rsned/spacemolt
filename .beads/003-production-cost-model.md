# Bead 003: Production cost model — price items so a line never sells at a loss

**Status:** Proposed
**Date:** 2026-10-31
**Triggered by:** the Voss contraband line plan (`docs/superpowers/plans/2026-10-31-voss-contraband-line.md`)
needs shopkeep floors. Moonshine sold down to 135 cr while the still cost
~329k/day in rent (2026-10-29), and nobody could say what a unit cost to make.

---

## Request

For any item we produce, estimate its **unit cost**: what one unit costs us to
make and deliver. Use it to set selling floors, so a production line never
lists below cost. It should appear in the production-line interface (the line
dashboard on :8093) and feed the shopkeep floors.

## Where it fits today

- `pkg/craftbrain` (A2 planner, shipped 2026-07-10) already expands a target
  into a recursive bill of materials, subtracts what we hold, and sites each
  craft by fee and time. It has no credit valuation. This bead adds that layer.
- The KB BoM explorer chooses between recipes alphabetically, not by cost. One
  campaign read 13x too expensive. A cost model fixes the choice too: pick the
  cheapest recipe.
- Shopkeep rules take a fixed `item:ask:floor` today (`marketbot_alhena`
  `pirate_moonshine:3000:1500`), set by hand.

## Cost methodology (operator, 2026-10-31)

Unit cost is a **production cost accumulated stage by stage**, from the raw
unit to the finished one. It is not a market valuation. Every stage that
touches the item adds its cost per unit of that stage's output; the next stage
inherits it through its inputs.

For each stage, per unit of output:

1. **Rent per unit.** The facility's rent per day ÷ its output per day **at
   full capacity** (operator, 2026-10-31), not its measured runs per day. That
   keeps the cost stable and makes an idle facility's rent show up as unused
   capacity rather than as a costlier product. **Capacity** comes from the
   recipe. `crafting_time` is the **hand-craft baseline** in ticks per run
   (the Station Workshop divides it by a skill factor of 1.0-5.0). An **L1
   facility runs 5× faster**, and **each upgrade adds another 3×** (operator,
   2026-10-31). So ticks/run(level) = crafting_time ÷ (5 × 3^(level-1)):
   L1 = 5×, L2 = 15×, L3 = 45×, L4 = 135× the hand-craft speed.
   Then runs/day = 8,640 ÷ ticks/run, and output/day = runs/day × outputs per
   run.

   Measured at Conjunction Junction's L1 facilities on 2026-10-31 (exactly
   crafting_time ÷ 5):

   | Facility | Recipe | Per run | Per hour |
   |---|---|---|---|
   | Copper Drawing Mill | 1.8 | 0.36 | 4,000 piping |
   | H2 Fuel Combustor | 3.0 | 0.60 | 60,000 fuel |
   | Iron Refinery | 0.54 | 0.11 | 6,666 steel |
   | Platinum Contact Assembly Line | 9.0 | 1.80 | 200 nodes |

   **Station bonuses** (operator, 2026-10-31):
   - **Faction admin facility** (`faction_admin`: Faction Desk T1, Faction
     Office T2; only two tiers) speeds up the faction's own production
     facilities **at that station** by **+15% per tier**, so +30% at most.
   - **Faction recruitment facility** (Hiring Board L1 → Recruitment Desk →
     Recruitment Center → Guild Hall → Grand Recruitment Bureau L5) adds
     **+5% output per level**.

   Full rate: ticks/run = crafting_time ÷ (5 × 3^(level-1) × (1 + 0.15 ×
   admin_tier)).

   Measured: Haven (Faction Office T2, Hiring Board L1) runs the L1 H2 Fuel
   Combustor at 0.46 ticks/run, 78,000 fuel/hr, against Conjunction Junction's
   0.60 and 60,000 (no admin facility): exactly 1 + 0.15 × 2 = 1.30×. Haven
   still shows 100 fuel per run, so the recruitment output bonus did not appear
   as more fuel per run; how it applies is still to be confirmed.

   Example: distill_moonshine (13.5 ticks, 6 out) on a Backwater Still (L1) is
   13.5 ÷ 5 = 2.7 ticks/run, 3,200 runs = 19,200 moonshine per day. A Moonshine
   Works (L3) is 172,800 per day.

   Rent per unit at Voss falls slightly by level (moonshine L1-L4: 0.34 / 0.30
   / 0.27 / 0.29), so an upgrade does not make output much cheaper. **It buys
   capacity, and that is where it pays off for intermediates that feed other
   lines** (operator, 2026-10-31). A shared upstream stage such as gutter_flux,
   circuit_board or chlorine_compound should be sized to the sum of all its
   downstream consumers, so it is never the bottleneck. A final product sold
   into a thin market (moonshine) should be sized to demand instead. The model
   should report, per stage, capacity/day against downstream demand/day, to
   show which stage limits the chain and whether an upgrade would unblock it.
   Rent is
   3% of build cost per day, doubled at pirate stations (v0.656.0). A public
   facility's rental fee per run counts here too. Our own station's facilities
   may carry 0 rent but still count their labour.
2. **Labour per unit.** The fixed `labor_cost` per run ÷ outputs per run.
3. **Haul fuel per unit, both directions.** Assume haulers **cannot refuel free
   at their home station**, so every leg is paid at the station fuel price:
   (outbound jumps + return jumps) × fuel per jump × fuel price ÷ units carried.
   An empty return leg is charged to the load that caused it.
4. **Mining fuel per base unit.** For a raw input we mine or harvest:
   round-trip fuel (POI ↔ drop station) × fuel price ÷ units per hold.
5. **Bought inputs.** Purchase price plus sales tax (about 3% on 10-29/30).

A finished unit's cost is the sum over the whole chain:

```
unit_cost(item) = Σ inputs (qty_per_unit × unit_cost(input))
                + rent_per_unit + labour_per_unit + haul_fuel_per_unit
unit_cost(raw)  = mining_fuel_per_unit            (or purchase price + tax)
```

**Every stage's cost is computed once and reused.** The model keeps one cost
per intermediate item, so any product built from it starts from that number.
For example, gutter_flux is costed once at the Voss Chem Lab, and void_dust,
nerve_burn, red_mist and trade_cipher all inherit it as an input. The same
goes for chlorine_compound, circuit_board, platinum_wiring and so on. Adding a
new product means costing only its own stage; everything upstream is already
known. When an upstream stage's rent, labour or fuel changes, every product
downstream updates with it.

**Selling costs** (sales tax, listing fees) are added once at the end, to give
the floor below which a sale loses money.

**Reported alongside, not part of the cost:**
- the market bid for each input (to show when selling it raw would earn more)
- the facility build cost spread over its expected life (a payback view)

**Output per item:** `unit_cost` with a per-stage breakdown (rent, labour,
haul fuel, mining fuel, purchases), the recipe path chosen, the cheapest
alternative, and a suggested floor of `(unit_cost + selling costs) × (1 + margin)`.

## Design notes

- **Price source:** `market.db` order books, same freshness rules as haul.
  Stale prices are flagged, not trusted silently.
- **Treat it as an estimate:** this follows the craftbrain lesson that bugs
  "fail toward looking good". When an input has no price, mark the cost a lower
  bound and say which input; never report it as zero.
- **Interface:** a cost column per line on the line dashboard, and
  `play_as cost <item> [--qty N]` for a one-off quote.
- **Floors:** shopkeep floors can come from the model (`floor = cost × margin`),
  with a manual override.

## Open questions

- How much time cost to charge for agent hours, if any?
- Target margin per product, or one fleet-wide default?
