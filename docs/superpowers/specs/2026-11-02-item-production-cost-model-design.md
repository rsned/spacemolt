# Item production cost model

**Status:** for operator review, 2026-11-02 (builder check done; the operator's answers are folded in)
**Related:** the KB build-cost matrix (`kb/cmd/generate-build-costs`, `pkg/buildcost`), which prices builds by **buying** inputs at market; the July crafting planner (`pkg/craftbrain`, `play_as build`), which plans one build for real; the 10-30 server patch v0.656.1, which capped station bids at what an item earns the station.

## Why

We don't know what anything actually costs us to make. Two decisions need that number:

1. **Which guaranteed income is worth chasing.** Empire stations post Station Manager buy orders. For example, plasma_injector has 4 wanted at 2,230 and 150 at 1,561. We capture these bids across the galaxy. We need to know which ones make money to fill, and how much per hour of effort, counting both manufacture time and the haul to the buyer.
2. **Knowing true worth where the market has no idea.** Many items have no market price, or a misleading one. A cost floor for every item tells us what things are really worth. It also tells us when buying or outsourcing a component beats making it.

## The cost rules (decided by the operator)

- **Raw resources (ore, gas, ice) cost 0.** Every agent has a free ship and mining laser, and miners scale until input supply is never the limit. Mining has some real cost, but it amortises to 0 per unit. This is the default, and it can be edited.
- **Hand crafting costs 0 credits; it costs only time.** It uses the agent's own labor at the station workshop and advances only while the agent stays docked. A hand run takes the recipe's full `crafting_time`. Example: basic_iron_smelting is 0 cr, 0.27 per run, 1 output.
- **Every facility run pays labor.** The facility catalog's `labor_cost` is charged per run at every facility, ours, an empire station's, or another faction's. There is no way around it. Example: refine_steel at a level 1 iron_refinery is 10 cr labor per run, 2 outputs.
- **A facility we don't own adds its rental fee on top of labor.**
  - **Empire stations charge the same fee everywhere.** Verified 11-02: all 387 empire recipe-and-level combinations in `public_facilities` charge one fee each. refine_steel is 9 per run at every empire station.
  - **Public faction lines set their own fees**, each recorded per line. refine_steel faction lines charge 4–9.
  - So a rented run costs labor plus fee: refine_steel at an empire station is 10 + 9 = 19 per run.
- **Facility speed:** a level 1 facility runs 5 times faster than hand crafting. Verified: build_plasma_injector's crafting_time is 19.5, and a captured level 1 empire facility takes 3.9 ticks per run. Each upgrade adds another factor of 3. Whether that factor applies to speed, to output per run, or to both is **not confirmed**; our notes say both ways. The builder confirms it from the captured `public_facilities` details (`ticks_per_run`, `output_per_run` against `level`) before using it.
- **Haul fuel and mine fuel** are the uncertain costs. They default to 0 and are editable on the private page.
- **Haul time is measured from Conjunction Junction**, our home station in Cloverfield, to the buyer's station.
- **Which routes can be used for costing (decided 11-02).** Trees and opportunities are costed only on routes we can use today: hand, empire rental, public faction lines, and CRFT's real facilities (from `faction_facilities`). Unbuilt "own" routes are shown on the public page as information only. Building any facility is possible in principle, but it costs many credits, inputs and hours, so it is not a free alternative.
- **Restricted facilities:**
  - A catalog facility with an `empire` field, such as `haven_cipher_foundry`, exists at one empire station only and no player can build it. It gets **no own route**. Its public line, if captured, is an ordinary empire route.
  - `pirate_base_only` facilities are player-ownable, but only at pirate bases. They keep their own routes, tagged "pirate base only".
- **Inputs that are neither raw nor made by any recipe** (130 items, such as salvage_components, tungsten_nugget and wildlife parts like whale_oil) are **unpriced**. Any item that depends on one is marked "has an unpriced input" and shows a partial cost: everything except the unpriced inputs. On the private page, a toggle can price them at the market ask instead.

## What gets built

Two outputs from **one costing engine**, so the numbers always agree.

### Public: the KB production-cost section

Published on the KB site (rsned.github.io/spacemolt-kb). Uses only the default rules above and no market data.

**Baseline table: every item.**
- Columns: Item · Recipe · Route · Out/run · Credits/u · Time/u.
- An item shows **every route for every recipe that makes it**. Each item collapses to one line by default and expands to show the rest.
- **Route labels spell out labor and fee:**
  - `hand`
  - `own L1 (labor 10)`
  - `empire L1 (labor 10 + fee 9)`
  - a faction line, labelled the same way with its station and faction
- Credits/u and Time/u cover the **whole input tree**, with each input on its own cheapest route.
- **Two highlights per item:** a box around the cheapest Credits/u cell (green) and around the fastest Time/u cell (blue). They're shown only where the item has more than one route. The boxes have inner padding so they don't touch the numbers.
- **No market columns:** no ask, no bid, no buy-is-cheaper flags.

**Item detail: the cost tree.**
- For each recipe that makes the item, a route table:
  - own at each level (labor)
  - empire (labor + fee), noted as uniform at all empire stations
  - every public faction line: station, faction, level, fee
- Below the route table, the input tree: each input on its cheapest route, down to raw resources at 0.
- **Every alternative recipe is shown**, in the same layout, with the cheapest and fastest highlighted.
- Links to the item's existing KB page and its build-cost page.

### Private: our local page

Never published. Shows everything the public section shows, plus:

**Opportunities: empire Station Manager buy orders, ranked.**

These are the market orders with `source='station'`.

**The route for each opportunity is the mix with the best profit per hour (decided 11-02).** The page shows which mix it used. Make time is counted over the whole depth:
- hand runs count as docked agent time
- facility runs count their ticks per run, with a tick being 10 seconds
- Columns: Item · Station · Bid · Depth · Our cost · Margin/u · Total · Hours · Profit/hr.
  - Margin/u = bid − our cost per unit.
  - Total = margin × depth.
  - Hours = make time + haul time from Conjunction Junction to the bidding station.
  - Profit/hr = total ÷ hours.
- Sortable by any column. Each bid price level at a station is its own row, since depth differs by price.

**Baseline with market.**
- The public table plus market ask and best bid.
- A **"buy is cheaper"** flag where a component sells for less than it costs us to make.
- A **"market has no price"** flag.

**Toggles (convenience vs time).**
- Each step in a tree can switch between:
  - mine (0)
  - buy now at the market ask
  - hand
  - rent (empire or a named faction line)
  - own facility
- Example: buy 1,000 thorium ore now instead of 30 hours of mining loops.
- Settings bar:
  - raw resources: mine or buy
  - haul fuel, in credits per jump
  - mine fuel, in credits per unit
- Totals and the opportunity ranking recalculate in the page.

## Constraints

- **Recipes that use `energy_crystal` are skipped on the private page.** Crystals are reserved for the drone project. The public page may show them, since that's a statement of game facts, not our plan. Of circuit_board's recipes alone, three use crystals.
- Read-only: nothing calls the game or writes game state.
- Data sources, all already captured:
  - catalogs: `catalog_recipes.json`, `catalog_items.json`, `catalog_facilities.json` under `data/game-api/latest`
  - `public_facilities` in the knowledge DB: 2,868 lines, 61 stations, 529 recipes, refreshed hourly by the marketbots
  - `market.db` for bids and asks
  - the map for jump counts
- Every page shows the age of each data source it used.
- Code is golangci-lint clean, Go 1.24 idioms; `go build ./...` and `go test ./...` pass. Binaries go in `bin/`.

## Non-goals

- Acting on opportunities: no dispatching, no buy orders. The page informs; humans and the existing fleet act.
- Live interactivity beyond in-page recalculation. The page is static, regenerated on a schedule.
- Facility rent amortisation (the per-cycle rent of facilities we own) and build cost amortisation. Owned-route cost is labor only for now.
- Player (non-empire) buy orders in the opportunity ranking.

## Anti-goals: a failure even if it technically works

- A cost that silently counts input purchase prices on a hand-crafted or raw step under the default rules.
- Labor left out of any facility route.
- An item shown with one route when other recipes or facilities make it.
- Public pages that leak the opportunities ranking, our facilities, or our toggled assumptions.

## Calls made by Claude, not the operator

- **Where the code lives:** in the KB repo (`~/spacemolt/kb`), as a new costing package beside `pkg/buildcost` and `pkg/bom`, with one generator. The generator writes the public section into `kb/` and the private page into a git-ignored local directory outside `kb/`. One module serves both, so the engine is shared and the private page can never be published by accident with the site.
- **Haul for rented facilities elsewhere:** when an opportunity's route uses a facility at another station, Hours also counts hauling the inputs from Conjunction Junction to that station and the product on to the buyer. A large depth counts one round trip per hold-load, using a default hold size.
- **Recalculating in the page:** the Go engine precomputes every route for each item into the page's data. A small script in the page re-totals the trees when a toggle changes. Tests check the script's totals against the Go engine's for the default settings.
- **"Buy now" price:** the cheapest asks that cover the needed quantity, walking the book depth, plus the haul from that station to Conjunction Junction.
- **Route ordering within an item:** cheapest credits first, ties broken by time.
- **Cycles in the recipe graph** (an item feeding its own input chain) are broken the way `pkg/craftbrain` does it; a route that can only be completed through a cycle is shown as such, not costed as 0.
- **Recipe outputs with more than one item** (by-products): the run's cost is assigned to the primary output only. By-products are shown but not credited.
- **"Own" routes are shown at each catalog level** of the facilities that make the recipe, even ones nobody has built, since the catalog gives labor and speed for each level. They are information only; see the costing rule above.
- **Faction lines' speed:** taken from each line's captured `ticks_per_run`, which varies at the same level, rather than computed from level.

## Left to the builder

- Haul time per jump: the existing jump formula with a sensible default ship, editable on the private page.
- How the private page is served: a file opened locally, or a small server like the other dashboards.
- Regeneration cadence: the KB's normal regen for the public section; at least hourly for the private page's bids.
- Exact HTML and styling, following the KB's existing look for the public section.
