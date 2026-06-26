# Hauler Dashboard — Design Spec

**Goal:** A self-contained, regenerated `dashboard.html` showing per-hauler performance over
time: credit-balance line, hauls-per-period bars, credits-per-jump per period (vs fleet), and
avg per-phase response time — with each hauler's current fuel and cargo capacity under its name.

**Delivery (decided):** a Go CLI tool reads durable data and writes one self-contained HTML file
with **inline SVG charts (no JavaScript, no chart library)**; regenerated on a timer. A `--period`
flag (`hour|half_day|day`) picks the bucketing; changing period means regenerating.

## Why instrumentation comes first

A data-readiness audit (2026-06-26) found only **hauls/period** and **total claim time** are
derivable today (from `arbitrage_opportunities.completed_at`/`claimed_at`). Everything else —
credit-balance-over-time, per-jump credits, the per-phase response breakdown, and cargo capacity —
is **not recorded anywhere durable**. So the build is phased: stand up the recorders first (history
accrues immediately), then build the generator (it renders whatever has accrued).

## Components

Three well-bounded units, each independently testable.

### A. Per-haul result recorder — worker → `market.db` table `haul_results`

On each *completed* haul the worker writes one row capturing the **real outcome** (not the scan
estimate). New `market.Collector` method `RecordHaulResult(ctx, HaulResult)`; new table:

| column | meaning |
|--------|---------|
| `id` | PK |
| `opp_id`, `agent_id`, `item_id`, `qty` | identity |
| `buy_price_paid`, `sell_price_got` | per-unit prices actually transacted |
| `realized_profit` | `(sell_price_got − buy_price_paid) × qty` — the **true** profit |
| `jumps_traveled` | total jumps over the buy + sell legs |
| `claimed_at, arrived_src_at, bought_at, arrived_dst_at, sold_at` | RFC3339 wall-time leg stamps |
| `claimed_tick, arrived_src_tick, bought_tick, arrived_dst_tick, sold_tick` | game-tick leg stamps |
| `created_at` | row write time |

Per-phase durations derive from adjacent stamps (in both seconds and ticks):
`travel_src = arrived_src − claimed`, `buy = bought − arrived_src`,
`travel_dst = arrived_dst − bought`, `sell = sold − arrived_dst`; `total = sold − claimed`.

**Worker instrumentation** (`pkg/worker/haul.go`): the haul execution path records the wall-time +
`GameClock` tick at each boundary (claim, arrived-at-buy, post-buy, arrived-at-sell, post-sell) and
calls `RecordHaulResult` on success. `jumps_traveled` comes from the route lengths the worker
already computes for the two legs.

**Bonus:** `realized_profit` here is the true number, so the dashboard's realized column finally
retires the inflated scan-`gross_profit` (the salvager-4 45k-unit phantom).

### B. Periodic fleet snapshot recorder — → `market.db` table `fleet_timeseries`

A quarter-hourly row per hauler, written by the overmind's existing balances recorder
(`pkg/overmind/balances`) which already snapshots the fleet to `fleet-status.json`:

| column | meaning |
|--------|---------|
| `id`, `ts` | PK, snapshot time (UTC) |
| `agent_id`, `role`, `system`, `docked` | identity / location |
| `credits` | balance (drives the credit-over-time line) |
| `fuel`, `max_fuel` | already on WorkerInfo |
| `cargo_used`, `cargo_capacity` | **NEW on WorkerInfo** (see below) |

**WorkerInfo change:** add `cargo_used` + `cargo_capacity` to the worker `control.Status` message,
`supervisor.WorkerInfo`, and `fleet-status.json` (fuel is already plumbed this way — mirror it).
Cadence: `quarter_hourly`, aligned to the boundary, matching the scan cycle.

### C. Dashboard generator — `cmd/tools/haul-dashboard` → `dashboard.html`

New binary (built to `bin/`). Flags: `--market-db-path` (default `data/market.db`),
`--status-file` (default `data/overmind/fleet-status.json`), `--period hour|half_day|day`
(default `hour`), `--window` (default `48h`), `--out` (default `dashboard.html`).

Reads A + B + `fleet-status.json`; emits one self-contained HTML file:

- **Top summary table** — one row per hauler mirroring `fleet-report` (credits, location, Δ, hauls),
  but with **realized profit from `haul_results`** (real, not inflated).
- **Per-hauler section**, each with the agent name and, beneath it, **current fuel (cur/max) and
  cargo capacity** (from `fleet-status.json`), then four inline-SVG charts:
  1. **Credit-balance line** over the window (from `fleet_timeseries`).
  2. **Hauls-per-period bars** (from `haul_results`, bucketed by `--period` on `sold_at`).
  3. **Credits-per-jump per period** — `Σ realized_profit / Σ jumps_traveled` per bucket, with a
     dashed fleet-average reference line for comparison.
  4. **Avg per-phase response time** — a stacked bar per period (travel-src / buy / travel-dst /
     sell), in ticks (seconds available on hover-title).

A small internal SVG helper renders line + grouped/stacked bar charts (no external dependency).
Regenerated periodically by invoking the tool on a timer (cron-lite / loop) — the tool itself is a
one-shot generator, not a daemon.

## Storage

Both `haul_results` and `fleet_timeseries` live in `market.db` (durable, queryable, already central
to the fleet) via the existing `ensureColumn`/`CREATE TABLE IF NOT EXISTS` migration pattern. This
also removes reliance on the ephemeral `/tmp` scratchpad logs for history.

## Phasing

- **Phase 1 (recorders) — deploy with the next fleet restart:** table A + table B + the WorkerInfo
  `cargo` fields + worker leg-stamping + the quarter-hourly snapshot. History accrues from deploy.
  Hauls and total-time backfill from existing `completed_at`; legs/jumps/realized/balances/cargo
  accrue forward only.
- **Phase 2 (generator):** `cmd/tools/haul-dashboard`. Buildable anytime; renders sparse until A/B
  fill in.

## Testing

- **A:** unit-test `RecordHaulResult` round-trips and that per-phase durations derive correctly from
  the five leg stamps; assert `realized_profit` math.
- **B:** test the snapshot writer emits one `fleet_timeseries` row per hauler with the new cargo
  fields populated.
- **C:** golden-structure test — generate from fixture rows, assert the HTML contains the expected
  per-hauler sections, SVG elements, and bucket counts (no live DB needed).

## Decisions (resolved)

- Delivery = standalone generated HTML, inline SVG, no JS; `--period` fixed/regenerated.
- Response time = full per-phase breakdown (needs worker leg-stamping), in ticks + seconds.
- `realized_profit` and the credits/jump numerator both use the **real** transacted profit.
- Storage = `market.db` tables, not scratchpad JSONL.
- Recorders (A+B) ship before the generator (C).

## Out of scope (YAGNI)

- Interactive client-side period switching (chose fixed/regenerated).
- Integration into the React frontend / observe server (chose standalone).
- A long-running generator daemon (invoke the one-shot tool on a timer instead).
