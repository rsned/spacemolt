# Shipping Carrier Sub-project C: Multi-Package Trips — Design

**Date:** 2026-07-22
**Status:** Approved design, pending implementation plan
**Predecessor:** Sub-project B (single-contract freight, proven 23/0 by fighter-4, rolled to wave 1 — 19 workers — 2026-07-22)

## Goal

Let a freight-enabled worker carry more than one sealed package at a time and
chain deliveries across multiple destinations, refilling at every stop, so
large holds stop idling most of their capacity on one flat-100 contract.
fighter-4 (790 free cargo = 7 packages) is the canary; most wave-1 holds fit
exactly 1 package and keep v1 behavior even with the feature enabled.

## Decisions (locked with operator 2026-07-22)

1. **Chained multi-stop**: contracts to different destinations bundle into one
   trip when they lie along an efficient route.
2. **Refill at every stop**: after delivering at a dock, the worker re-runs the
   freight gate there and accepts new contracts that fit. The hold rarely
   travels empty; the "trip" is emergent, not a planned object.
3. **fighter-4 canary**: multi-package is gated by a per-worker config; the
   other 18 wave-1 workers stay at max 1 contract until the canary proves the
   economics and the reconcile paths.
4. **Architecture A — held-set dock pass** (chosen over an accept-time route
   planner): no planner, no trip object; a per-dock invariant repeated at every
   dock produces the chain.

## Architecture: the held-set dock pass

Replace the single held contract (`missionRunState.heldFreight`,
`*ShipmentContract`) with a **held set** keyed by contract ID. Every docked
freight pass runs four steps in order:

1. **Deliver due.** For each held contract whose `DestinationBaseID` is the
   current base: settle dock, `ShippingDeliver`, record, remove from set.
   Deliveries run FIRST so cargo and liability headroom are real before any
   accept.
2. **Re-check the rest.** For each remaining held contract, re-run the
   in-flight deadline check using fresh `FindRoute` hops from the current
   dock (exact single-source distances). Any contract that can no longer make
   its deadline is returned (`returned_inflight`) — only that contract; the
   rest of the set is untouched.
3. **Accept loop.** While headroom remains, re-run the v1 gate over the board
   and accept the best candidate, then repeat. Headroom is the minimum of:
   - `freightPackagesFit(cargoFree)` (existing function, already returns the
     multi-package count),
   - server `active_contract_limit - active_contracts` (skip when
     `active_contracts_unlimited`),
   - aggregate liability: candidate `ReservedExposure <=
     remaining_aggregate_liability` (skip when `liability_unlimited`),
   - the per-worker cap `freight_max_packages` (canary gate, below).
   Each candidate is priced on **marginal** fuel — the hops its stop adds to
   the currently-held destination set (see route bound below) — and must still
   clear `freightMinNet`. Accepting must not break any held contract's
   deadline under the chain bound; fail closed.
4. **Nav to nearest.** Fly toward the held destination with the fewest hops
   from here (tie-break: earliest `DeadlineTick`). Arrival re-enters step 1.

An empty held set degenerates to exactly the v1 pass. Step 2's refill (item 3
above, "refill at every stop") applies only when `freight_max_packages` > 1;
at the default cap of 1, step 3 is skipped entirely after a clean delivery —
the pass ends there, exactly as the v1 trip did — so steps 1, 2, and 4 are
what stay behaviorally identical to today at cap 1, not a refill that
immediately finds nothing to add.

## Chain deadline bound (fail-closed without pairwise distances)

The router (`FindRoute`) prices destinations only from the worker's current
position; base-to-base leg distances are unknowable at accept time. The accept
gate therefore uses a sound upper bound: visiting order is nearest-first by
hops-from-here (h_1 <= h_2 <= ... <= h_n), and each leg between successive
stops is bounded by the round trip through the current dock:
`hops(d_i, d_{i+1}) <= h_i + h_{i+1}`. Cumulative worst-case hops to stop i is
therefore `2*(h_1 + ... + h_{i-1}) + h_i`. A contract (held or candidate) is
feasible iff

```
DeadlineTick - nowTick >= cumulative_i * freightTicksPerHop * freightDeadlineSlack
```

for its position i in that order. This is conservative — it under-accepts,
never over-accepts — and every subsequent dock re-runs step 2 with exact
fresh distances, so the bound tightens as the chain progresses. A contract that
degrades anyway exits through the free `return` path, which sub-project B
proved safe.

## State, reconcile, and failure semantics

- **Held set**: `missionRunState` gains `heldFreightSet map[string]*ShipmentContract`
  (the single-contract accessors become set operations; `clearHeldFreight`
  clears one ID). In-memory only, same as v1 — `captains_log` resume remains a
  separate queued project.
- **Reconcile** generalizes from "profile says 1, do I hold 1" to set
  reconciliation: profile `active_contracts` vs `len(heldFreightSet)`. When the
  profile reports MORE than we hold (restart lost memory), the existing
  "UNRECOVERABLE without operator rescue" branch applies unchanged — the board
  never lists our own in_transit contracts. When counts match, proceed.
- **Per-contract failure isolation**: a `returned_inflight` or
  `returned_infeasible` on one contract never touches the others. The two
  park-the-worker outcomes stay global, because both mean an undischarged
  liability we cannot reason about: `return_failed` (freightStepStuck) and
  the reconcile mismatch above.
- **Withdraw-at-origin** (package storage -> hold) happens per contract at its
  accept dock, exactly as v1; a withdraw failure returns THAT contract only.
- **Telemetry**: `freight_results` stays one row per contract (no schema
  change). Log lines gain the held count (`freight: holding 3/7 packages`) so
  canary reading shows chain depth. Fuel attribution: each contract records the
  marginal fuel it was priced with at accept (`freightCand.FuelCost` already
  carries it).

## Canary gating

New worker flag `--freight-max-packages` (int, default `1`), forwarded from
fleet yaml key `freight_max_packages` via WorkerSpec, same pass-through
pattern as `enable_freight`. Default 1 = v1 behavior for all existing workers
with zero config change. Canary rollout: fighter-4 gets `freight_max_packages: 3`
first, raised toward 7 after green rounds. The flag is a cap layered on the
server/cargo headroom gates, never a target.

## Error handling

- All freight failures remain "skip the pass / leave in flight / return the
  contract" — freight never becomes a new way for the mission pass to error.
- Accept-reply decode failure: return that contract (v1 rule), keep the set.
- Deliver failure at a due stop: leave that contract in flight (v1 rule); the
  worker does NOT accept new contracts at that dock in the same pass (headroom
  cannot be trusted mid-failure); nav continues to the next held destination.
- Board `Eligible=false` listings are skipped with reason, as v1.

## Testing

Unit tests (no live server), following the existing freight test style:

- `freightPackagesFit` multi-count already covered; add headroom-min tests for
  the accept loop (cargo vs contract limit vs liability vs cap; unlimited
  flags).
- Chain bound: nearest-first ordering, cumulative `2*sum + h_i` math, accept
  rejected when any HELD contract would break (not just the candidate).
- Step 2 re-check: fresh hops shrink the bound; degraded contract returns
  alone, set survives.
- Reconcile: set-vs-profile counts (equal / we-hold-fewer / we-hold-more).
- Per the missions vacuous-test trap (`reference_missions_vacuous_test_trap`):
  every new test must be proven discriminating — neuter the target line and
  observe red — because a bare `&game.State{}` can early-return the pass
  before the code under test.

## Out of scope

- `captains_log` server-side resume (separate queued project).
- Pairwise jump cache (would tighten the chain bound; queued separately as
  `project_pairwise_jump_cache`).
- Multi-package pickup batching at ONE accept call (server API accepts one
  contract per call; the accept loop is inherently sequential).
- Wave-2 rollout (explorers/randoms) and ship upgrades for small holds.
