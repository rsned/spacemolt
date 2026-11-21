# Recurring Station-Manager Demand Report — Design

**Date:** 2026-11-20
**Status:** Revised after review; awaiting operator approval
**Builds on:** the demand ledger + demand history (`docs/superpowers/specs/2026-05-29-demand-ledger-design.md`, `2026-05-30-demand-history-design.md`).

## Intent

NPC Station Managers (SM) post sizeable buy orders, someone fills them, and the
same orders come back. That repeat pattern is a steady revenue source we are not
tapping. This report finds it: **which items, at which stations, refill reliably,
how much they absorb per day, at what price — and which of those we could craft
easily from the resources we already hold.**

It is phase (a) of two. Phase (b), the fleet selling into these orders
automatically, is out of scope, but the analysis lives in a package so (b) and
any dashboard can reuse it.

## Goals

- Rank (station, item) pairs by estimated SM revenue per day from history.
- Show for each: how often it refills, typical open quantity, typical price,
  current open order, and how far it is from home (Conjunction Junction).
- Say whether we can craft the item from our faction storage pool, how many
  units, and what is missing if not.
- Run offline: no game login, safe to run any time (cron, ad hoc).

## Non-goals

- No selling, hauling, or order placement (phase b).
- No new data capture; no schema migration. Uses what is already recorded.
- No dashboard page (possible follow-up once we know which columns matter).

## Anti-goals

- Do not count player bids as SM demand (`source='station'` only).
- Do not present a cancel/reprice as a guaranteed sale without saying so: the
  report is a lead list, not a promise.
- Do not log in as any agent or touch live fleet processes.

## Data (verified 2026-11-20)

- **Demand series:** `market_demand_history` in `data/spacemolt-knowledge.db`.
  One row per (station_id, item_id, hourly `bucket_utc`), last observation in the
  hour wins. SM split columns: `sm_qty` (total qty of `source=station` orders),
  `sm_best_price`. Retained 14 days by `market-prune --history-retain`.
  Coverage now: 2026-11-07 → today, 46 stations, 388 items with SM demand.
- Raw `market_orders` in market.db is pruned to 4h — not usable for patterns.
- **Resource pool:** `faction_storage_items(faction_id, base_id, item_id, name,
  quantity, size, captured_at)` in `data/assets.db`, latest capture per base.
  Filter to our faction (CRFT). Default pool = all CRFT storage bases summed;
  `--base` narrows to one (e.g. CJ `c976fa7df53c87f9616165d7d411b41a`).
- **Schema notes (verified by review):** there is no KB `stations` table —
  stations live in `bases`; 18 of the 46 demand `station_id`s match only
  `bases.poi_id` (dual-named ids), so resolve names via id OR poi_id.
  `faction_id` in assets.db is a hash, not the tag — resolve CRFT's id from the
  KB (or take `--faction-id`). `system_id` in demand rows mixes ids and display
  names; normalise via the KB `systems` table.
- **Recipes:** KB tables `recipes`, `recipe_inputs`, `recipe_outputs` (the same
  source `pkg/linedash/refdata.go` loads offline).
- **Stations/systems/lanes:** KB `stations`/`systems`/`connections` for names
  and jump distance from home.

A quick pass over the series showed real signal (drops / rises over 14 days):
salvage_metal 829/581 at 12 stations, purified_water 685/831 at 33,
trade_authenticator 1502/2024 at 25, station_reactor_core 65/36 at 25.

## Approaches considered

1. **Standalone CLI `cmd/tools/sm-demand` + analysis package** — offline, no
   login, cron-able; logic reusable by phase (b) and a dashboard. **Chosen.**
2. `demand recurring` subcommand in play_as — sits beside `demand`, sees live
   cargo, but needs a logged-in session; cannot run from cron/dashboard.
3. Craft-dashboard page — always visible, but most work, and we don't yet know
   which columns earn a place. Better as a follow-up to (1).

## Design

### Package `pkg/smdemand` (pure analysis, no I/O in the core)

Per (station, item) series of hourly samples, ordered by bucket.

**How SM quantity actually moves (verified):** it creeps up hour by hour as the
Station Manager generates demand (e.g. purified_water 2→6→7→9→17→20), then drops
sharply when someone sells in, then creeps again. The creep is the volume open
to us; the drops are competitors filling it. (Operator-confirmed 2026-11-20:
the creep is the marker to track.)

- `hours_observed` — buckets present; `days_span` — first→last bucket.
- `presence` — fraction of observed hours with `sm_qty > 0`.
- `restock_units` — sum of bucket-to-bucket rises in `sm_qty`;
  `restock_per_day` = `restock_units / days_span`. **The primary measure.**
- `fills` — count of *sharp* drops: a drop of at least 25% of the previous
  bucket's `sm_qty` (constant, flag `--fill-drop`); `fills_per_day`;
  `taken_units` — sum of those drop sizes (what competitors absorb). Small
  drops below the threshold are treated as noise/reprice and not counted.
- `typical_qty` — median `sm_qty` over hours where > 0;
  `typical_price` — median `sm_best_price` over the same hours.
- `est_cr_per_day` = `restock_per_day × typical_price`.
- `current_qty`, `current_price`, `last_seen` — the latest bucket.
- Gaps (hours with no capture) are not drops or rises: compare only consecutive
  *captured* buckets, and record `max_gap` so sparse series are visible. A rise
  across a gap is still counted (demand accrued while unobserved).

Caveat stated in the output footer: a sharp drop may be a cancel or reprice,
not a fill; hourly buckets net out movement inside one hour.

Ranking: `est_cr_per_day` desc. Default filters: `presence ≥ 0.5`,
`fills ≥ 2` (someone actually sells in, i.e. the order is live, not a stale
bid nobody fills), `days_span ≥ 2`; all overridable by flags.

Also an **item rollup**: the same metrics summed across stations per item
(total units/day, station count, best station), because craft decisions are made
per item.

### Craftability

For each ranked item, against the resource pool:

- `in_stock` — units of the item itself already in the pool (sell-now).
- `craftable_units` — how many units the pool can make, expanding recipes
  recursively through intermediates (depth cap 4) and sharing inputs
  correctly within one item's expansion.
- `limiting` — the base input(s) that cap `craftable_units`, and for a target
  of `typical_qty` the shortfall list (`missing: item×qty`).
- `depth` — recipe depth (1 = direct from pool).
- Items with no recipe show `—` (gather/loot only).
- **Facility recipes count** (operator-confirmed 2026-11-20) when the pool
  includes CJ; such rows are marked `F` so it is visible they need a station
  facility. When the pool is only bases without facilities (e.g. `--base` Beid),
  facility-only recipes are excluded. Hand recipes always count.
- **Several recipes for one item** (66 SM items): report the single recipe that
  yields the most units from the pool; never mix recipes. *(Builder's call.)*

Each item is evaluated independently against the full pool (the report does not
allocate the pool across items; it says "could make", not a plan).

### CLI `cmd/tools/sm-demand`

```
bin/sm-demand [--kb-path data/spacemolt-knowledge.db] [--assets-db-path data/assets.db]
              [--days 14] [--min-presence 0.5] [--min-fills 2] [--fill-drop 0.25]
              [--base <base_id>] [--item <id>] [--station <id>]
              [--by item|station] [--craftable-only] [--limit 40] [--json]
```

Default output (`--by station`), one row per (station, item):

```
ITEM              STATION (SYSTEM)          JMP  PRES  RESTOCK/D  FILLS/D  TYP QTY  TYP PRICE  EST CR/D   NOW QTY@PRICE   STOCK  CRAFT   MISSING
purified_water    Sol Central (sol)          12   97%      1,310     4.1    2,246       108    141,480   2,100@110       0    840F   water_ice×1,460
...
```

`--by item` prints the rollup. `--json` emits the same records for phase (b) or
a dashboard. Footer: data window, stations covered, pool used (bases + capture
time), the cancel/reprice caveat.

`JMP` = shortest jump count from Conjunction Junction's system (cloverfield)
over KB `connections`; blank if unreachable/unknown. No stronghold-avoidance
logic is needed since nothing is routed — distance is informational.

## Constraints

- Read-only on all three DBs (`mode=ro`).
- Go 1.24+, golangci-lint clean, TDD; binary goes to `bin/`.
- Check real column names against the live schemas before coding.

## Left to the builder

- Whether craft expansion reuses `pkg/craftplan` via an offline `Source`
  adapter, `pkg/craftbrain`, or a small expander over `linedash`-style recipe
  maps — pick whichever gives correct shared-input accounting with least glue.
- Exact table formatting and column widths.
