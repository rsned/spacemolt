# Crafting Brain A2 — The Planner

**Date:** 2026-07-09
**Status:** Design approved, pending implementation plan
**Depends on:** A1 Facility Catalog (shipped 2026-07-09, `caa0478..7f0b0eb`)
**Followed by:** B — The Executor

## Goal

Take a target (item, ship, or facility) plus a quantity, compute the exact recursive
material breakdown, subtract stock the fleet already holds, decide where each craft
happens, and emit a reviewable JSON step-DAG of granular tasks: mine, buy, haul, craft.

The planner is **read-only and review-first**. It does not dispatch work. Executor B
consumes the DAG later.

Surface: a `play_as` command, alongside the existing `plan`, `craftable`, `find_item`,
and `price`.

## Findings that shaped this design

These were established against live data on 2026-07-09 and overturn several decisions
recorded during the A1 brainstorm.

### `bill_of_materials` cannot back this feature

The sibling `spacemolt-crafting-server` table stores **per-unit** quantities with
rounding, so it is not quantity-exact at scale:

- `alloy_electrum_ingot` consumes 2 `gold_ore` + 3 `silver_ore` → yields 2 `electrum_ingot`.
- `bill_of_materials` records `gold_ore 1, silver_ore 2` (1.5 rounded up).
- 2 units therefore appear to cost 4 `silver_ore`; the true cost is 3.

`ceil` does not distribute over multiplication, so **no per-unit table can ever be
quantity-exact**. The table is also not recursive-with-intermediates: 648 of its
`base_item_id` values are themselves craftable.

**Consequence:** A2 performs its own recursive expansion over `recipes`,
`recipe_inputs`, and `recipe_outputs`, computing `runs = ceil(need / output_per_run)`
at every node.

### Skills no longer gate crafting

`crafting.db.recipes` has no skill column. `serverapi.Recipe.RequiredSkills` survives as
a vestigial `omitempty` field the server no longer populates. Consequently
`craftplan`'s `skillCeiling()` returns 0 for every recipe and its `resolveRecipe`
tiebreak has silently degenerated to alphabetical, while `PlanResult.BlockedSkill`
is now always empty.

**Consequence:** A2 uses no skill logic. The dead gate in `pkg/craftplan` is noted
here as an observation; fixing it is out of scope.

### Inventory lives in personal storage, not faction storage

The A1 brainstorm locked "inventory pool = faction storage; personal storage = in-transit,
ignore." The live data says the opposite:

| Pool | Rows | Capture |
|---|---|---|
| `storage_snapshots` / `_items` | 227 snapshots, 53,898 items, 150 agents × 31 bases | Passive: `pkg/agent/storage_capture.go:13` persists **any** `view_storage` response |
| `faction_storage_items` | 2 rows, captured 2026-05-21 | Write path exists (`pkg/knowledge/faction_store.go:182`), no capture wiring |

Neither is scheduled, so freshness is opportunistic (newest snapshot 2026-07-02).

**Consequence:** the inventory pool is **both**, summed and attributed by holder
(`agent_id`, `base_id`) — attribution is what Executor B needs to say "hauler-3,
withdraw 400 iron_ore at grand_exchange". A scheduled `view_storage` standing command
keeps it fresh, mirroring exactly how A1 scheduled `facilities`; the existing passive
capture needs no new write path.

### Facility coverage is sparse and lopsided

> **Stale as written — measured before the capture bug was fixed (`8df24f8`).**
> The catalog read only the `public_facilities[]` section of a `facility list`
> payload, so station-owned and own-faction public production lines were never
> ingested (all 247 rows were faction-owned; zero station-owned; voss_redoubt
> captured 0 of its ~13 public lines). The figures below are therefore a floor,
> not a measurement. **Re-measure after a full sweep on the fixed binary and
> update this section.** True coverage is expected to be materially higher.

247 rows across **6 stations**; `confederacy_central_command` alone holds 219 of them
(145 distinct recipes). Only **81 of 317** `facility_only` recipes have a known public
facility (26%). Fees span 1 … 2,000,000 per run.

**Consequence:** a missing facility means *unknown*, not *impossible*. Blocked nodes
say so, and the plan reports catalog coverage. This conclusion does not depend on the
exact coverage figure and survives the re-measure; what may change is how often the
buy-fallback path is exercised versus a sited craft.

### Facility speed is per-instance, not derivable

`ticks_per_run` is **not** a fixed multiple of `recipes.crafting_time`. Measured
speedup (`crafting_time ÷ ticks_per_run ÷ 3^(level−1)`) across the 247 observed
facilities:

| speedup | facilities |
|---|---|
| 6.5 | 198 |
| 5.0 | 29 |
| 5.75 | 7 |
| ~1.44 | 8 |
| 1.25 / 1.625 | 5 |

**Consequence:** read `production.ticks_per_run`, `output_per_run`, `rental_fee_per_run`,
`backlog_ticks`, and `queued_runs` directly from the observed facility's `details_json`.
Use `recipes.crafting_time` for hand-crafting. Never-observed facilities are unknown, not
estimated.

## Decisions

| Question | Decision |
|---|---|
| Inventory pool | Both personal + faction storage, attributed by holder; schedule `view_storage` to refresh |
| Subtract rule | Subtract early; if stock is remote, emit `haul` nodes |
| Siting | Cheapest by `rental_fee_per_run × runs`, tie-break lower `ticks_per_run` (higher level), then `facility_id` |
| Hand vs facility | Prefer hand-craftable; switch to facility when `runs × crafting_time + plan_time_so_far > --max-hand-ticks` (default 360). Threshold exceeded but no facility known → hand anyway, tagged `slow` |
| Recipe alternatives | Resolved by the hand-vs-facility rule above; no skill input |
| Dead ends | Fall back to `pkg/finditem` buy node; if unbuyable, `BLOCKED` (rest of DAG intact) |
| Mine vs buy (raw leaves) | **Operator's choice at review.** Planner reports both options per leaf |
| Output | Canonical JSON step-DAG + human formatter |

**Accepted trade-off:** siting scores fee only, not haul cost. A cheap-but-distant
facility can therefore produce a long haul leg. Mitigation: the plan prints total haul
distance so the operator sees it at review. Revisit if it bites.

## Architecture

New package `pkg/craftbrain`, mirroring `pkg/craftplan`'s Engine+Source shape. The engine
never opens a database; all reads go through one interface, so the expander is testable
against an in-memory fake.

```go
type Source interface {
    Recipe(itemID string) ([]serverapi.Recipe, error)        // all recipes producing itemID
    RecipeByID(recipeID string) (serverapi.Recipe, error)
    Facilities(recipeID string) ([]knowledge.PublicFacility, error)
    OnHand(itemID string) ([]Holding, error)                 // {Holder, BaseID, Qty, CapturedAt}
    Buyable(ctx context.Context, itemID string, qty int) ([]finditem.Result, error)
    SystemOf(stationID string) (string, error)               // stations live in systems; jumps are between systems
    Jumps(fromSystem string, toSystems []string) map[string]int
}

type Options struct {
    MaxHandTicks int           // default 360 (~1h; a tick is ~10s, so ~360 ticks/hour)
    MaxStockAge  time.Duration // default 24h
}

func (e *Engine) Plan(ctx context.Context, target string, qty int, opts Options) (*Plan, error)
```

Nodes carry `station_id`; haul distance is computed by mapping each station to its system
via `SystemOf` and then calling `Jumps`. `SystemOf` is the `Source`'s job because the
station→system mapping lives in the KB, and 27 `market.db` station rows are known to carry
display-name `system_id`s (see `project_find_item_command`) — the SQL `Source` normalizes
that in one place rather than leaking it into the expander.

Files, one job each:

| File | Responsibility |
|---|---|
| `expand.go` | Topological BOM walk — the only interesting algorithm |
| `site.go` | Facility choice + hand-vs-facility threshold |
| `inventory.go` | Subtract-early, holder attribution, haul-leg emission |
| `plan.go` | Types: `Plan`, `Node`, `Edge`, `Holding`, `Options` |
| `format.go` | Human rendering |

`cmd/tools/play_as/source_sql.go` wires the three real databases:

- `crafting.db` — `recipes`, `recipe_inputs`, `recipe_outputs`, `crafting_time`, `facility_only`
- `spacemolt-knowledge.db` — `public_facilities`, `storage_snapshots(_items)`, `faction_storage_items`, connections
- `market.db` (via `pkg/market.Collector`) — behind `pkg/finditem` and `pkg/pricing`

Reused, not reimplemented: `pkg/finditem` (where to buy), `pkg/pricing` (what it costs),
`pkg/navigation` (`BFSJumps` over a graph from KB connections).

## The expander

Three hazards drive the algorithm.

**1. Shared intermediates must aggregate before rounding.** If `copper_wiring` is consumed
by three parents needing 4 each, a naive DFS computes `ceil(4/3)=2` runs three times → 18
units. Aggregating first gives `ceil(12/3)=4` runs → 12 units. The
`sensor_array_production_line` BOM shows the same base item arriving via multiple paths,
so this is the common case.

**Therefore the expander is not a tree walk.** It processes items in topological order:
when we reach item X, every consumer of X is already decided, so X's demand is final.

**2. Rounding creates reusable surplus.** `electrum_ingot` yields 2/run; demand 3 forces 2
runs → 4 units → 1 spare. Topological order means that spare, and any *secondary* outputs
(`recipe_outputs` may have several rows), flow into the same on-hand pool later items draw
from. Surplus is reported, never discarded — it is real inventory the executor ends up
holding.

**3. The recipe graph can contain cycles** (refine A→B, recycle B→A). Build the union of
all candidate recipes reachable from the target — bounded by the 666-recipe catalog, so
cheap — run Kahn's algorithm, and break any cycle by dropping the edge that closes it,
recording the drop in diagnostics rather than failing.

Recipe choice at X depends only on X's demand, which comes only from X's ancestors.
So **one topological pass suffices**; no fixpoint iteration. The running time accumulator
for the hand-vs-facility threshold is threaded along that same order.

Per item, in topological order:

```
demand  = Σ parent demands  −  on-hand (subtract early, nearest base first)
if demand == 0                    → done (emit haul nodes if stock was remote)
if item is raw (no recipe)        → leaf: report mine-and-buy options, operator decides
choose recipe R (candidates = all recipes outputting item):
    if a hand recipe (non-facility_only) exists:
        use it, UNLESS runs × crafting_time + plan_time_so_far > --max-hand-ticks
                  AND a facility for some candidate recipe is known
            → then: cheapest facility by fee × runs,
                    tie-break lower ticks_per_run, then facility_id
        (threshold exceeded but no facility known → hand anyway, tagged `slow`)
    else (facility_only only):
        facility known → cheapest facility, same tie-break
        no facility     → finditem buy node → if unbuyable, BLOCKED
runs    = ceil(demand / output_per_run(R))
surplus = runs × output_per_run(R) − demand      → back into on-hand pool
push demand for each input of R × runs
```

## Plan schema

```
Node  { id, kind, item_id, qty, runs, station_id, facility_id,
        fee_total, ticks_est, holder, status }
Edge  depends_on: []NodeID

kind   ∈ { mine, buy, haul, craft, blocked }
status ∈ { ok, blocked, stale, slow }
```

`slow` marks a craft that blew the `MaxHandTicks` budget but had no facility to escape to —
the operator's cue that a facilities sweep might pay for itself.

Raw leaves carry **both** options — mineable at node? market price and availability? —
per the mine-vs-buy decision above.

## Failure, staleness, degradation

The planner reads four sources of wildly different freshness, and its output is only as
honest as its worst input. Nothing degrades silently; every weakened assumption becomes a
visible annotation.

**Staleness is per-node.** Holdings carry `captured_at`. Snapshots are opportunistic and
`UNIQUE(agent_id, base_id)` — latest-only, no history. A holding older than
`--max-stock-age` (default 24h) is still subtracted, but its node is tagged `stale` and
the footer counts them. `public_facilities.last_seen_tick` is treated the same way: an
unswept facility is sited but flagged, since fee, level, and backlog may all have moved.

**BLOCKED means unknown, not impossible.** Absence of a facility is overwhelmingly absence
of evidence — the catalog is swept opportunistically, and a capture bug (`8df24f8`) hid an
entire class of public facility until 2026-07-09. Blocked nodes say so, and the footer
prints the live coverage fraction (stations swept, `facility_only` recipes covered) so the
operator can distinguish "run a facilities sweep" from "this genuinely cannot be built."

**Abort vs. annotate.** Abort with a clear message on: unresolvable target, target with no
recipe, `qty < 1`. Annotate everything else: a broken cycle, an unseen facility, an empty
`finditem` result, an unreachable `market.Collector` (buy fallback degrades to BLOCKED
rather than killing the plan). The engine never writes anything — read-only by
construction, which is why A2 precedes B.

**Two footers**, both cheap, both preventing bad executions:

1. Total haul distance across the plan (where fee-only siting exposes itself).
2. Total fee, and estimated makespan in ticks.

## Testing

The arithmetic is the risk and it is all pure, so it is all unit-tested against a fake
`Source` — no DB, no network. Table-driven, one per hazard:

- **ceil-per-run** — `electrum_ingot` qty 2 → 2 `gold_ore` + 3 `silver_ore`. This is the
  exact case `bill_of_materials` gets wrong; it is the reason the package exists.
- **shared-intermediate aggregation** — one item consumed by three parents → 4 runs, not 6.
- **surplus reuse** — odd demand against a 2-per-run recipe leaves 1 spare a later sibling consumes.
- **subtract-early + haul** — stock split across two bases emits two haul legs, zero craft nodes.
- **hand-vs-facility threshold** — just under → hand; just over → cheapest facility; ties → lower `ticks_per_run`.
- **slow fallback** — threshold exceeded, no facility known → hand recipe, tagged `slow`, not `blocked`.
- **dead end** — facility_only with no facility → buy node; not buyable → BLOCKED, rest of DAG intact.
- **cycle** — A→B→A terminates and records the broken edge.
- **golden plan** — `sensor_array_production_line` end-to-end against a fixture `Source`
  snapshotted from today's real data; a regression in any stage shows up as a DAG diff.

Integration coverage stays deliberately thin: one test that the `play_as` wiring opens all
three DBs and produces a plan for a known-good target.

`go build ./... && go test ./...` and `golangci-lint` per repo convention.

## Scope boundaries

**In scope:** `pkg/craftbrain`, the `play_as` command and its SQL `Source`, and the
scheduled `view_storage` standing command that keeps the inventory pool fresh.

**Out of scope:** dispatching work (Executor B); building a facility when none exists
(surfaced as BLOCKED, never auto-recursed); haul-aware siting; fixing `craftplan`'s dead
skill gate; adding faction-storage capture wiring.

## Related

`project_crafting_brain`, `project_overmind_fleet_manager`,
`reference_catalog_recipes_shape`, `project_find_item_command`
