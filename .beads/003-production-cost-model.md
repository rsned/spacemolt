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
   capacity rather than as a costlier product. Rent is
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

- Where does a facility's full-capacity output per day come from? The catalog
  gives no throughput, so it may have to be measured once per facility type
  (time per run on an otherwise idle facility).
- How much time cost to charge for agent hours, if any?
- Target margin per product, or one fleet-wide default?
