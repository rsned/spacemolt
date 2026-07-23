# Freight Probation Bootstrap — Design

**Date:** 2026-07-23
**Status:** Draft (awaiting user review)

## Problem

Worker carriers on the `/shipping` freight path are stuck at the **probationary** carrier tier and cannot escape it. Carrier standing is a 4-tier ladder — `probationary → licensed → trusted → prime` — and advancement is gated on cumulative `successful_deliveries` **and** `delivered_value` (the server reports both the requirement and the remaining, per account, in `CarrierTierProgress`). Higher tiers unlock larger, better-paying contracts (fighter-4 was observed earning ~4× at higher tiers).

The deadlock: a probationary carrier's freight boards are dominated by `licensed`/`trusted`-tier cargo it is **legally ineligible** to accept (server: `licensed cargo requires licensed carrier standing`). The contracts it *can* accept pay a flat ~100 cr carrier payout, and the client's net floor rejects them:

```
net = 100 (payout) − fuel
  fuel 48  → net +52   (rejected: below 500 floor, though PROFITABLE)
  fuel 84  → net +16   (rejected)
  fuel 160 → net −60
  fuel 441 → net −341
  fuel 588 → net −488
```

The current freight net floor (`freightMinNet = 500`, `pkg/worker/freight.go`) rejects all of these — including the *positive-net* short hauls — so the carrier completes zero deliveries, never advances, and stays probationary forever. The floor built to prevent strand losses is also the lock on the only door out.

Observed live 2026-07-23: the 42-worker mission-learn fleet sits near-idle; freight boards return "no candidate" overwhelmingly due to tier ineligibility.

## Insight

The fix is mostly **not** loss-eating. The short-haul probationary contracts are *positive-net* (+16…+52) — genuinely profitable, just under the 500 floor. The bootstrap primarily accepts contracts we already reject at a small profit, tolerating bounded losses only when solely longer hauls are eligible. Advancement is cheap because every delivery also advances `delivered_value` (cargo value, typically well above the 100 payout).

## Approach (chosen)

A single, local **gate modifier** in the freight decision path. While a worker's carrier is probationary and its per-worker loss budget is not spent, the freight net floor drops **500 → −400**, letting it accept the probationary-eligible contracts. It accumulates deliveries + delivered_value until the server flips it to **licensed**, after which the normal 500 floor resumes and the now-eligible licensed cargo (~4× payout) clears it on its own. Nothing else in the fail-closed freight path changes.

Rejected alternatives: (a) a standalone "onboarding campaign" state machine that deliberately routes a worker through the chain — more infrastructure than a floor relaxation needs; (b) relaxing the floor across all sub-max tiers — unnecessary, since licensed+ cargo already clears 500.

## Decisions (user-answered)

1. **Loss cap = BOTH** a per-contract floor **and** a per-worker cumulative budget. Belt-and-suspenders: bounds a single bad contract and total exposure.
2. **Risk level = Aggressive:** per-contract floor **net ≥ −400**; per-worker budget **3,000 cr** of cumulative loss, then stop until the tier flips. Chosen to advance fast to the 4×-paying licensed tier.
3. **Per-worker = per-account.** Every `credentials.json` is a distinct agent; there are **no shared accounts**, so carrier standing is owned exclusively by one worker. Budget is cleanly per-worker; no cross-worker coordination.
4. **Probationary tier only.** licensed/trusted/prime keep the normal 500 floor.
5. **Freight path only.** `missionMinNet` (mission-board delivery) is untouched — carrier tier advances via shipping deliveries, not mission-board missions.
6. **Positive-net contracts don't consume budget.** The budget tracks actual losses (accepted contracts with net < 0) only; positive-net probationary contracts are free advancement.

## Architecture

All changes are in `pkg/worker/freight.go` (plus its test file) and the freight deps/run-state. The version rides the **existing** freight decision path — the worker already fetches `ShippingProfile` (with `Profile CarrierProfile` + `Progression CarrierTierProgress`) at the top of the freight evaluation, right where the floor is applied.

```
freight pass (already fetches prof = ShippingProfileResponse)
  → floor := effectiveFreightFloor(prof, w.bootstrapSpent, freightProbationBudget)
  → buildFreightCand(..., floor)   // relaxed floor admits probationary contracts
  → selectFreightCand              // UNCHANGED — highest-net-first: profits before losses
  → on accept of a net<0 contract: w.bootstrapSpent += -net
```

## Components

### 1. `effectiveFreightFloor(prof, spent, budget) float64` (new, pure)
- Returns `freightProbationFloor` (−400) when `prof.Progression.CurrentTier == "probationary"` **and** `spent < budget`.
- Otherwise returns `freightMinNet` (500). Covers licensed/trusted/prime, budget-exhausted, and empty/legacy/unknown tier.
- Pure and table-testable; no I/O.

### 2. `buildFreightCand` — floor parameter
- Replace the package-const reference `freightMinNet` in the `net < freightMinNet` check with a `floor float64` parameter passed by the caller.
- `selectFreightCand` is **unchanged** — it already selects the candidate with the highest `Net`, so positive-net contracts are preferred and losses are taken only when nothing better is eligible.

### 3. Per-worker loss tally
- A `bootstrapSpent float64` field on the freight/mission run-state (in-memory, per worker).
- When a contract is **accepted** with `net < 0` under the relaxed floor, add `−net`.
- Positive-net accepts do not change it.
- When `bootstrapSpent ≥ freightProbationBudget`, `effectiveFreightFloor` reverts to 500 until the tier flips (advancement moots the budget).

### 4. Constants + toggle
- `freightProbationFloor = -400.0`
- `freightProbationBudget = 3000.0`
- `FreightBootstrap bool` on freight deps (default **on** wherever `EnableFreight` is set) — a cheap kill switch; when false, `effectiveFreightFloor` always returns 500.

## Data flow / ordering

- Ordering is unchanged from the existing freight pass; the only new step is computing `floor` before the candidate loop and accruing `bootstrapSpent` after a negative-net accept.
- The tier check reads the **live** `prof.Progression.CurrentTier` each pass, so a worker stops bootstrapping the pass after the server flips it to licensed — self-synchronizing on the server's own signal.

## Error handling

| Case | Behavior |
|---|---|
| `ShippingProfile` fetch fails | No bootstrap; existing error path returns (floor never relaxed). |
| Debt blocks acceptance / headroom / liability / reconcile-gap | Unchanged — all existing fail-closed gates still apply; bootstrap only widens the net floor. |
| Tier empty/unknown/legacy (old server or missing field) | Treated as non-probationary → normal 500 floor (fail safe, no relaxation). |
| Budget exhausted while still probationary | Floor reverts to 500; worker idles on freight until the tier flips or an operator intervenes. |
| Worker restart | `bootstrapSpent` resets to 0 (in-memory). Bounded: `CurrentTier == probationary` still gates, and advancement is typically fast. Persisting it is a fast-follow (captains_log-style), not v1. |

## Out of scope (YAGNI)

- Relaxing the floor above probationary (licensed+ cargo clears 500 on its own).
- Persisting `bootstrapSpent` across restarts (fast-follow).
- A dedicated onboarding/campaign state machine or routing a worker toward eligible cargo (the marketbot freight-demand scan, a separate project, addresses "where are eligible contracts").
- Touching the mission-board (`missionMinNet`) path — carrier tier is freight-only.
- Any change to `selectFreightCand` selection ranking.

## Testing

- **`effectiveFreightFloor`:** probationary + budget-remaining → −400; probationary + budget-exhausted → 500; licensed/trusted/prime → 500; empty/legacy tier → 500; `FreightBootstrap=false` → 500.
- **`buildFreightCand`:** accepts a net=−300 contract under floor −400; rejects net=−450 under floor −400; still rejects net=+300 under floor 500 (normal-tier behavior unchanged).
- **Loss tally:** accrues only on negative-net accepts; a positive-net accept leaves it unchanged; trips to 500-floor once `bootstrapSpent ≥ 3000`.
- **Integration:** probationary fixture board with mixed contracts (+52, −300, −450) → highest-net-first takes +52 (no budget spent), then −300 (budget += 300), rejects −450; after enough negative accepts to reach 3000, the floor reverts and further sub-500 contracts are rejected.

## Rollout note

Ships in `pkg/worker/freight.go`. Reporting is additive and the toggle defaults on for freight-enabled workers. Deploy requires a worker-binary rebuild + fleet redeploy (mission-learn fleet first) — which is also the natural moment the v0.1.0 build stamp lands (see fleet-version-visibility). Validate the first live run against a real `CarrierTierProgress` (the exact `RequiredSuccessfulDeliveries`/`RequiredDeliveredValue` for probationary→licensed are server-reported and were not directly observable from logs; the −400 floor / 3,000 budget are the aggressive-profile defaults and are tunable constants).
