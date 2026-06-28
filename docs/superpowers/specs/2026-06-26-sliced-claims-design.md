# Sliced Claims — Design Spec

**Goal:** Let a single large arbitrage opportunity be fulfilled by multiple haulers, each
taking a cargo-hold-sized slice, instead of one ship claiming the whole row and only moving
a fraction of it. As a side effect, fix the realized-profit metric (it currently credits one
ship the full order-quantity gross — the 9.9M "phantom").

## Background — the opp 4001 lesson

Opp 4001 (silicon_ore, war_citadel 500 → sol_central 720, qty 45,080, gross 9.9M) exposed two
separate problems:
1. **Capacity blindness** — `gross_profit` is computed on the full order quantity, never capped
   by a ship's hold. One 50-unit hauler "completed" it and was credited 9.9M; actual liquid Δ ~0.
2. **Stale sell price** — the 720 sell was stale; sol_central's real top bid is 370 (below the
   500 buy cost), so the trade is currently *inverted*. The live gate correctly skipped it.

Sliced claims fixes (1) directly. (2) — pricing quantity against **real live bid depth** — is a
related scanner-accuracy concern, scoped as a fast-follow (see Scope).

## Data model

- `arbitrage_opportunities`: add `remaining_quantity REAL` (init = `quantity`). The row is
  **claimable while `remaining_quantity > 0`**, not exclusively locked by the first taker.
- New table `arbitrage_claims` (one row per hauler slice):
  - `id INTEGER PK`, `opp_id INTEGER`, `agent_id TEXT`, `quantity REAL`,
    `status TEXT` (`claimed` | `completed` | `released`),
    `claimed_at TEXT`, `completed_at TEXT`.
- Migrate via idempotent `ensureColumn` (existing pattern) + `CREATE TABLE IF NOT EXISTS`.
- `arbitrage_opportunities.status` becomes an **aggregate** of its slices:
  `available` (remaining > 0), `claimed` (remaining == 0, slices in flight),
  `completed` (remaining == 0 AND all slices completed), `expired` (TTL passed).

## Operations (pkg/market)

- `ClaimSlice(ctx, oppID, agentID, holdQty) -> (claimID, qty, ok)`: atomically
  `qty = min(holdQty, remaining_quantity)`; if `qty <= 0` return `ok=false`; insert claim row
  (`claimed`), `remaining_quantity -= qty`; if `remaining_quantity == 0` set opp `status='claimed'`.
  Atomic under the existing `writeRetry`/WAL pattern.
- `CompleteSlice(ctx, claimID, agentID)`: claim `status='completed'`, stamp `completed_at`.
  If the opp now has `remaining_quantity == 0` and **no** non-completed slices, set opp
  `status='completed'`.
- `ReleaseSlice(ctx, claimID, agentID)`: owner-only; claim `status='released'`,
  `remaining_quantity += qty`; if opp was `claimed`, revert to `available`.
- `GetClaimedSlicesByAgent(ctx, agentID)`: open (`claimed`) slices for resume.

**Granularity (decided):** slice = `min(hold, remaining)`. A big-hold ship may take the whole
claim in one slice — that is acceptable and desirable (no artificial per-ship cap).

## Worker integration (pkg/worker/haul.go)

- Hauler claims a slice sized to **its own cargo capacity** (`state.Ship.CargoCapacity - used`),
  not the opp's full quantity.
- Buys/hauls/sells that slice; the pre-buy live gate stays per-slice (so successive haulers
  draining one buyer self-correct as the bid walks down — later slices get gate-rejected).
- Completes its own slice via `CompleteSlice`. Resume path uses `GetClaimedSlicesByAgent`.
- Abandon-before-buy releases the slice (`ReleaseSlice`); abandon-after-buy keeps it for resume
  (mirrors today's release-on-abandon vs resume split).

## Metric fix (free with this change)

Realized profit = sum over **completed slices** of `slice.quantity × per-unit spread` (or actual
fill if tracked), inherently capped by each ship's hold. `fleet-report` reads completed slices,
not the opp's full-quantity gross. The 9.9M-phantom class disappears.

## Scope

**v1 (this spec):** the claim model (remaining_quantity + arbitrage_claims + slice
claim/complete/release/resume), worker wiring, and the metric fix.

**Fast-follow (separate):** size opp `quantity` from **real live bid depth** (min of supply depth
at the buy ask and demand depth above cost+margin), so a sliced claim only opens when the large
opportunity is genuinely deep on both sides — kills the stale/inverted opps (like 4001 today) that
currently waste fuel.

## Fix #2 — freshness & stability (BUILT 2026-06-26, replaces the "depth sizing" framing)

Investigation (2026-06-26) disproved the "naive sizing" premise: `ScanArbitrage` already sizes
`qty = min(real ask depth, real bid depth)` and dedupes to the latest capture per station, so
opp 4001's 45,080 was *real at scan* and merely **stale by arrival**. The full-log gate breakdown
(~1,261 rejects) confirmed it: ~37% inverted-on-arrival, ~33% buy-side supply gone, ~27% fat-margin
but net-too-small for a tiny hold, ~4% genuine thin margin. So the levers are freshness + stability,
not depth math. Decisions (user-approved):

- **TTL is moot** — every scan already `UPDATE … SET status='expired' WHERE status='available'`,
  so unclaimed opps live ≤ one cycle by construction. The only freshness knob is the **cycle
  length**: shortened 30 → **15 min** via a new `quarter_hourly` frequency (`pkg/worker/schedule.go`
  ValidFrequencies/CurrentBoundary/NextBoundary; marketbot `update_market` schedules; scanner
  `watch --interval 15m`).
- **Stability = ranking BOOST, not a filter.** New `cycles_seen` column on
  `arbitrage_opportunities` (migration via `ensureColumn`): `ScanArbitrage` reads the prior cycle's
  per-route `(item, from, to)` streak from the still-in-play (available|claimed) rows *before*
  expiring, and carries it forward (+1, or restart at 1 when the route was absent). The hauler ranks
  on `effectiveGross = GrossProfit × stabilityBoost(cycles_seen)`, where the boost is
  `1 + min(cycles_seen-1, 5) × 0.10` (capped at +50%). A one-shot spread still competes — it just
  starts from behind a durable route. Selection-time only; never changes credits earned.

The small-hold ~27% bucket is handled separately by the interim gate fix (250cr net floor for
holds < 100) plus manual cargo-expander refits.

## Interactions / dependencies

- **Fuel-aware routing** (next thread): big supply pools are often far (war_citadel→sol_central is
  a long haul); fan-out strands the same way until the haul engine refuels on its travel legs
  (preliminary root cause: `travel`-to-station legs bypass `ensureRouteFuel`).
- **Ship upgrades** (capacity lever): `cargo_expander_iii` (+100/unit, most starter hulls fit ≥2)
  turns 50–80-hold starters into 250+-hold haulers, so each slice is bigger and fewer ships are
  needed per big claim. Amplifies this feature; orthogonal to build.

## Open decisions

- Track **actual fill price** per slice (truest P&L) vs. sized gross (simpler)? Recommend sized
  gross in v1, actual-fill as a refinement.
- Back-compat: keep legacy `claimed_by`/`claimed_at`/`completed_at` columns on the opp for the
  single-slice common case, or fully migrate readers to `arbitrage_claims`? Recommend: keep the
  columns nullable for transition, make `arbitrage_claims` the source of truth.
