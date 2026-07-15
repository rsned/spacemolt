# Haul-fleet earnings-per-jump on the overmind status page

**Date:** 2026-07-15
**Status:** Approved design, pending implementation plan
**Sub-project:** 1 of several in the "fleet earnings-per-jump" effort.
Reads only the existing `haul_results`; no worker change, no schema change, no
fleet redeploy. Sequenced follow-ons (each its own spec → plan → build):
**SP-task** = per-worker current-task line + route-progress bar (needs the
worker to publish its active opportunity); **SP-loss** = per-agent ship-loss
counter (nothing tracks deaths today); **SP2** = sweetspot / outlier gate;
**SP3** = shuttle fare recorder + parity panel.

## Problem

Haulers record every completed haul to `haul_results` (6,490 rows across 21
agents: realized profit, jumps traveled, per-leg timing), and huddash already
computes credits-per-jump from it. But huddash is a **dormant one-shot HTML
generator** — nothing runs it, no page is served, and its cr/jump is **gross of
fuel**. What the operator actually watches is `overmind-status` (`pkg/ovstatus`,
served on :8087 by `scripts/start-overmind-status.sh`) — a singleton page that
aggregates every overmind's status JSON into one worker table per overmind.

Two things are missing there:
1. No live view of **how much the fleet earns per jump, net of fuel**.
2. The per-worker rows show position/credits/status but not the worker's own
   **career throughput** (how many hauls, how many jumps, at what cr/jump).

## Goal

On the existing `overmind-status` page, add:

1. A **"Haul fleet efficiency"** headline panel — fleet gross / fuel / **net**
   cr/jump over a recent window, plus a ranked per-agent net-cr/jump table.
2. A **per-worker stats line** under each haul-fleet worker row: lifetime
   `hauls · jumps · <ship losses> · avg cr/jump` (gross, simple — deliberately
   distinct from the panel's net-of-fuel math).

Both read `haul_results` through one aggregator. Fuel (panel only) uses a flat
estimate. **No schema change, no worker change, no fleet redeploy, no backfill.**

## Non-goals (separate sub-projects)

- **Current-task line + route bar** ("Handling opp #804122 · 204 Trade
  Authenticators · gold_run --*--> iron_heart") — **SP-task**. The worker knows
  its active opportunity but does not publish it; `LiveRecord` has only an
  internal `ActiveTaskID`. Rendering this needs the worker to emit opp
  id/item/qty/buy/sell into its heartbeat + new `LiveRecord` fields + a fleet
  redeploy. Out of scope here.
- **Ship losses** — **SP-loss**. Nothing tracks deaths (no deaths table, the
  `player_died` event isn't persisted). SP1 renders the losses field as `—`
  (an honest "unknown"), never a fabricated `0`. SP-loss adds a per-agent death
  counter that fills it.
- **Sweetspot band / outlier gate** — **SP2** (SP1's fleet net cr/jump is its
  reference line).
- **Shuttles** — **SP3** (shuttles log no fare outcomes; a recorder must exist
  first). SP1 is haul-fleet only.
- **Reviving/serving huddash** — left as-is; may be retired later.

## Design

### Integration point

`overmind-status` is a singleton that already aggregates all overminds, so both
additions are made once and cover the whole fleet with no per-overmind wiring.

- **`cmd/tools/overmind-status/main.go`** gains flags: `--market-db-path`
  (default `data/market.db`), `--eff-window` (default `48h`),
  `--fuel-cr-per-unit` (default `5`), `--fuel-per-jump` (default `9`). On
  startup it attempts a read-only `market.Open`. Failure or blank path → the DB
  handle is nil and the page renders exactly as today (panel omitted, no
  per-worker line). Logged once at startup.
- On each request (the page auto-refreshes ~300s) the handler builds the
  efficiency inputs from the DB and passes them to `ovstatus.Render`. A small
  time cache (recompute at most every ~30s) is optional; the queries are single
  grouped scans.
- **`pkg/ovstatus`**: `Render` gains one parameter, an optional `*HaulStats`
  bundle (nil = no panel, no per-worker lines). Signature:
  `Render(sources []Source, hs *HaulStats, refresh int, now time.Time) string`.
  The single caller in `main.go` is updated.

### Data source + aggregator

One aggregation in `pkg/market` (beside `HaulResultTotals`):

```go
// HaulEfficiencyRow is one agent's haul aggregates over a window.
type HaulEfficiencyRow struct {
    AgentID   string
    Hauls     int
    SumProfit float64 // Σ realized_profit
    SumJumps  int64   // Σ jumps_traveled
}

// HaulEfficiencySince returns per-agent aggregates over haul_results with
// sold_at >= since and jumps_traveled > 0, plus the summed fleet row. A zero
// `since` (time.Time{}) means all-time (used for the lifetime per-worker line).
func (c *Collector) HaulEfficiencySince(ctx context.Context, since time.Time) (
    perAgent []HaulEfficiencyRow, fleet HaulEfficiencyRow, err error)
```

`SELECT agent_id, COUNT(*), COALESCE(SUM(realized_profit),0),
COALESCE(SUM(jumps_traveled),0) FROM haul_results WHERE sold_at >= ? AND
jumps_traveled > 0 GROUP BY agent_id`. Called twice per render:

- `since = now − eff-window` → the **panel** (windowed, net-of-fuel).
- `since = time.Time{}` → a **lifetime per-agent map** for the per-worker line.

Rows with `jumps_traveled = 0` are excluded (degenerate, and a zero divisor).

### Fuel model (panel only — simple, render-time)

Grounded in the live `station_fuel_prices` feed (real `fuel_price_all_in` is
4–8 cr/unit, avg ~6) and ~9 fuel/jump observed for a hauler:

```
fuelCrPerUnit = 5   // flat, --fuel-cr-per-unit; future: median(station_fuel_prices)
fuelPerJump   = 9   // flat, --fuel-per-jump;    future: per ship tier
fuelCost(j)   = float64(j) * fuelPerJump * fuelCrPerUnit
net           = sumProfit − fuelCost(sumJumps)
grossPerJump  = sumProfit / sumJumps
fuelPerJumpCr = fuelPerJump * fuelCrPerUnit          // constant cr/jump
netPerJump    = net / sumJumps
```

At ~6 cr/unit the drag is ~50 cr/jump vs ~2,400 gross — small but visible and
honest. When `sumJumps == 0` the panel shows "no hauls in window" (no divide).

The **per-worker line uses gross only** — `avgPerJump = sumProfit / sumJumps`
over the agent's lifetime rows — no fuel term, per the operator's "distinct from
the net-of-fuel computations."

### Rendering

`main.go` assembles one bundle; `ovstatus` renders it:

```go
type HaulStats struct {
    Panel    *EffPanel                 // nil → panel omitted
    Lifetime map[string]AgentLifetime  // agent_id → lifetime line data
}
type EffPanel struct {
    WindowLabel  string
    Hauls        int
    GrossPerJump float64
    FuelPerJump  float64
    NetPerJump   float64
    Agents       []PanelAgent // ranked NetPerJump desc
}
type PanelAgent struct { AgentID string; Hauls int; NetPerJump float64 }
type AgentLifetime struct { Hauls int; Jumps int64; AvgPerJump float64 }
```

- **Panel** renders above the per-overmind sections (styled to the page CSS):

  ```
  Haul fleet efficiency (48h)
    gross 2,391  −  fuel 52  =  NET 2,339 cr/jump   ·   1,204 hauls
    ── per agent ──   salvager-10 178h 4,050 · trader-1 279h 3,955 · … · trader-3 41h 880
  ```

- **Per-worker line**: `renderRow` emits its normal worker `<tr>`, then — only
  when `hs.Lifetime[agentID]` exists — a second `<tr>` with a spanning cell:

  ```
  281 hauls · 1,405 jumps · — losses · avg 2,391 cr/jump
  ```

  Agents absent from `haul_results` (non-haulers) get no extra line. Losses is
  the literal `—` until SP-loss. The task line + route bar slot is left for
  SP-task (a third sub-line).

### Graceful degradation

- No/blank `--market-db-path` or `market.Open` fails → `hs = nil` → page
  byte-identical to today. Logged once at startup, not per request.
- DB present, zero hauls in window → panel header + "no hauls in the last 48h";
  agents with no lifetime rows simply get no per-worker line.
- A query error on a request → log it, pass `nil` for that request (panel +
  lines omitted), never 500 the page.

## Files

- **New** `pkg/market/haul_efficiency.go` — `HaulEfficiencyRow`,
  `HaulEfficiencySince`. (+ test)
- **Modify** `pkg/ovstatus/ovstatus.go` — `HaulStats` / `EffPanel` /
  `PanelAgent` / `AgentLifetime` types, `renderEffPanel`, the per-worker line in
  `renderRow`, and the `Render` signature (add `hs *HaulStats`). (+ tests:
  panel, per-worker line, nil case)
- **Modify** `cmd/tools/overmind-status/main.go` — the four flags; open
  market.db; two `HaulEfficiencySince` calls; assemble `HaulStats` (apply fuel
  constants for the panel, gross for lifetime); pass to `Render`; graceful nil.

## Testing

- **Aggregator:** seed `haul_results` across two agents and two `sold_at`
  times; assert `HaulEfficiencySince` sums profit/jumps/count per agent,
  excludes out-of-window and `jumps_traveled = 0` rows, totals the fleet row,
  and that a zero `since` returns all rows.
- **Panel math (pure helper):** from (sumProfit, sumJumps, fpj, cru) assert
  `net = profit − jumps*fpj*cru`, the three per-jump values, and the
  `sumJumps == 0` guard (no divide, "no hauls").
- **Ranking:** `PanelAgent` list renders net/jump descending.
- **Per-worker line:** `renderRow` for an agent present in `Lifetime` emits the
  extra row with hauls/jumps/`—`/avg; an agent absent emits only its normal row.
- **Render nil:** `Render(sources, nil, …)` == today's page (no panel, no extra
  rows); with a bundle, the panel appears above sections and lines under matching
  rows.

## Scope boundaries (YAGNI)

- Flat fuel constants (flags), not per-tier/per-station.
- Panel = windowed net; per-worker line = lifetime gross. No charts (huddash has
  them if wanted), no lane/item breakdown.
- No persistence, no tick cost, no worker/server change.

## Future work (sequenced sub-projects)

- **SP-task:** worker publishes its active opportunity (id/item/qty/buy/sell +
  progress) into the heartbeat; `LiveRecord` gains fields; `renderRow` adds the
  task line + `buy --*--> sell` progress bar. Worker change + fleet redeploy.
- **SP-loss:** persist `player_died` / ship-destruction per agent (minimal
  count-only v1) so the per-worker line shows real losses instead of `—`. Ties
  to the queued per-death loss-capture work.
- **SP2:** sweetspot band (e.g. IQR around fleet median net/jump) + flag
  opportunities (find_arbitrage rows, a hauler's next pick) far above/below it.
- **SP3:** `shuttle_results` fare recorder + a "Shuttle fleet efficiency" panel
  and per-worker line (fare − fuel per jump).
- Per-ship-tier `fuelPerJump`; median-based `fuelCrPerUnit`; lane/item breakdown.
