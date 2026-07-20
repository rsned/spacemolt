# Freight Carrier — Sub-project B (carrier behavior) — Design

**Date:** 2026-07-20
**Status:** Design approved, pending spec review
**Server:** live ≥ v0.531.4; client `BuiltForAPIVersion` = v0.531.4
**Supersedes:** the "Sub-project B" section of `2026-07-19-shipping-carrier-design.md`
(that section's pre-accept deadline gate was invalidated by the live smoke — see
"What the smoke changed" below)
**Related:** `2026-07-19-shipping-carrier-design.md` (parent design + full smoke RESULTS),
`project_shipping_carrier`, `project_kind_discriminator_drift`,
`reference_craft_action_result_wrapping`

## Problem

Sub-project A landed a **dormant** `/shipping` client in `pkg/game`: 18 serverapi structs,
`storeRawJSON` caching under `shipping_<action>`, and 10 `GameClient` methods. Nothing calls
it. Sub-project B makes an agent actually earn freight income: find eligible NPC freight at
the current dock, take the best one, deliver it, get paid, and — above all — never breach.

**Breach is the central risk.** Missing the tick-deadline or opening the seal forfeits
payment, damages carrier standing, and creates freight-debt that blocks all new acceptances
until paid.

## What the live smoke changed

The 2026-07-19 `play_as` smoke (craftsman-1 self-ship, v0.531.4) invalidated two load-bearing
assumptions in the parent design. Both are encoded here:

1. **`deadline_tick` does not exist before accept.** A posted listing carries no
   `deadline_tick` or `target_tick`; the server sets them *at accept* (observed: standard
   service, `route_hops` 3 → `target` = accepted+90, `deadline` = accepted+180). The parent
   design's `route_ticks × 1.5 ≤ deadline_tick − now` pre-accept gate is therefore
   unimplementable. Replaced by **accept-then-verify** (below).
2. **The package cargo footprint is a flat 100**, not contents-summed and not the empty
   container's size of 4 — the container's 100-item capacity is reserved whole. Ten
   size-1 `iron_ore` in a package still measured 100. So the fit check is a **constant**,
   knowable before any server call.

Also confirmed and relied on here: `return` is debt-free (status `returned`/`returned_intact`,
full `shipper_refund`, liability released, `outstanding_debt` stays 0); `deliver` at the
destination pays `carrier_payout` and deposits the sealed package to recipient storage;
a 3-hop trip took 56 ticks (~19/hop).

## Goals

- A freight income path for agents whose current ship can hold a package, evaluated co-equally
  with the mission board on every docked pass.
- **Zero breaches caused by our own negligence.** Every failure path ends in "skip the pass" or
  "`return` the contract" — never in riding into a deadline.
- Restart-safe: a worker that restarts mid-freight resumes or returns its contract rather than
  orphaning the package.
- Measurable: per-contract telemetry with breach-rate as the canary's pass/fail signal.

## Non-Goals (v1)

- **Shipper side** (`quote`/`post`) — deferred.
- **Haulers** — phase 2, after the mission-runner carrier is validated live.
- **Stacking** freight with other freight or with same-destination missions — v1 is a
  standalone freight trip, one contract per run. Deferred, but **explicitly viable and worth
  building next** (see below).
- **Auto-`pay_debt`** — explicitly rejected (see Debt policy).
- **The generalist opportunity selector.** The long-term intent is that no agent is *just* a
  mission-runner or *just* a miner — each agent weighs every path open to it at each juncture,
  gated by where it is and what ship it has (freight needs ≥100 free cargo; a hauler can size
  to any hold). B does not build that selector. It builds freight as a cleanly-scored candidate
  with an explicit capability precondition, so it drops into that selector unchanged when the
  selector is designed.

## Deferred but viable: multi-package trips for large holds

Because the footprint is a flat 100 rather than a per-contract unknown, the number of packages
a pilot can carry is computable before any server call: `floor(cargoFree / 100)`. A 100-unit
hold carries one; a 600-unit hauler carries six. Large-capacity pilots are therefore leaving
most of their earning capacity idle on a single-contract trip, and the fleet's biggest holds
are exactly the ones that would benefit most.

This is deferred out of v1 only to keep freight's first live validation simple — not because it
is hard. It is the natural Sub-project C, and v1 is shaped so it does not have to be unpicked:
`freightPackageFootprint` is already a named constant rather than an inlined `100`, the
capability precondition is a capacity comparison rather than a boolean, and `freightCand` is a
scored candidate that a set-selector can rank rather than a singleton the trip loop assumes.

Three things a stacking design must handle that v1 does not:
- **Aggregate liability caps.** Carrier tiers bound *total* concurrent exposure, not just single
  contracts (probationary: ≤5000 single, ≤10000 aggregate). Concurrency is capped by
  `min(floor(cargoFree/100), liability headroom)`, so the hold is not always the binding
  constraint.
- **Per-contract deadlines on a shared route.** Each package has its own `deadline_tick`, so
  feasibility becomes a route-ordering problem — the accept-then-verify check must hold for
  *every* contract given the visiting order, not just the next one.
- **Partial abort.** If one contract in a set becomes infeasible, `return` that one and continue
  the trip, rather than abandoning the set.

## Architecture

### Module boundary

New `pkg/worker/freight.go` and `freight_test.go`. Freight logic does **not** go into
`mission.go`, which is already 1115 lines. One narrow entrypoint:

```go
// freightCandidate evaluates freight at the current dock. Returns the best
// scored candidate, or a skip reason when freight is not viable this pass.
freightCandidate(ctx context.Context, deps MissionDeps) (cand *freightCand, skipReason string, ok bool)
```

`Missions()` compares `cand.Net` with the best mission net and pursues the higher; exploration
remains the fallback when neither clears its floor. That comparison is the entire coupling
between the two modules.

`freightCand` carries the contract, destination base, hops, net, and fuel cost — the same
shape `missionCandidate` has, so the eventual selector can rank them without adapters.

### Client-layer prerequisite (first task of B)

Sub-project A's **mutations are currently undecodable.** `accept`/`deliver`/`return`/`cancel`/
`post`/`pay_debt` are tick-deferred and reply in an `action_result` frame,
`{command:"shipping", tick:N, result:{action, contract, ...}}`, with **no top-level `action`**.
Two things miss it today:

- `Shipping()` (`pkg/game/client_commands.go:2662`) uses `WithAckOnly()`, so `await` resolves on
  the immediate pending ack, before the real body arrives.
- `storeRawJSON` (`pkg/game/client.go:4346`) keys on the top-level `action`, so the wrapped
  frame is never cached as `shipping_<action>` — it lands under `_last`.

Decoding the wrapper directly *succeeds* with every field absent, so this fails silently as an
empty contract rather than as a decode error. This is exactly the craft bug fixed 2026-07-12
(`reference_craft_action_result_wrapping`).

Fix, mirroring craft:
- Mutating shipping actions submit with `WithTerminator(terminateOnActionOrOK)` (already used
  throughout `client_commands.go`) instead of `WithAckOnly()`, so the await waits for the real
  frame. Read actions (`list`/`get`/`profile`/`track`) are synchronous, top-level-action, and
  decode correctly today — they keep their current path.
- `storeRawJSON` learns an `action_result` path: when the frame's `command` is `shipping`,
  cache it under `shipping_<result.action>` so callers find it where they expect.
- `pkg/worker` unwraps defensively via a helper mirroring `unwrapActionResult`
  (`craft_node.go:161`) — a no-op passthrough when `result` is absent.

### Per docked pass

1. **Capability precondition — before any server call.**
   `cargoFreeSpace(state) >= freightPackageFootprint` (const `100`). An agent whose current
   ship cannot hold a package never touches `/shipping`. This is the ship-capability gate the
   generalist selector will eventually own. Deliberately written as a capacity comparison, not
   a boolean: `floor(cargoFree / freightPackageFootprint)` is the concurrent-package count that
   Sub-project C will act on.
2. **Debt guard.** `ShippingProfile`. If `DebtBlocksAcceptance`, log `DebtBlockReason` once for
   the pass, surface it in the status line, and skip freight. The agent still runs missions and
   exploration.
3. **List.** `ShippingList`; consider only listings with `Eligible == true`. The server's flag
   already encodes carrier-tier, liability, and debt eligibility — we do not re-derive tier
   logic client-side. An empty board with `no_eligible_shipments` is normal, not an error.
4. **Score.** `net = base_reward − route_fuel_cost(hops)`, require `net >= freightMinNet`
   (reusing the mission/haul net floor). Drop candidates whose route will not resolve or whose
   path crosses a stronghold (reuse `missionRouteClear`). `max_speed_bonus` is treated as
   upside only — never as a reason to accept a contract that does not clear the floor on
   `base_reward` alone.
5. **Co-equal selection.** `best_freight_net` vs `best_mission_net`; the higher wins.

### The trip — accept-then-verify

Because the deadline is unknowable pre-accept, feasibility is checked *after* committing, using
the escape hatch the smoke proved safe:

1. `ShippingAccept` (carrier = player) → unwrap → the real `deadline_tick`, `target_tick`, and
   `route_hops` are now known. The sealed package is in personal storage at origin.
2. **Feasibility check:**
   `hops × freightTicksPerHop × freightDeadlineSlack ≤ deadline_tick − now_tick`
   with `freightTicksPerHop = 19` (measured: 56 ticks over 3 hops) and
   `freightDeadlineSlack = 1.5` — a named, tunable const whose ~50% buffer also absorbs
   GameClock forward-drift and reconnect stalls. Both are single-sample-derived constants and
   are expected to be re-tuned from `FreightResult` data after the canary.
   *The smoke's own contract clears this comfortably: it needed ~86 ticks of the 180 granted.*
3. **Infeasible → `ShippingReturn` immediately.** Debt-free, full `shipper_refund`, liability
   released. Record outcome `returned_infeasible`, release the candidate, and fall through to
   the mission path for the rest of the pass.
4. **Feasible →** withdraw the package from origin storage into cargo (the storage/cargo item
   id carries a `package:` prefix; the contract's `package_id` is the bare hash), route to
   `destination_base_id` with the existing navigation and refuel behavior, dock, and
   `ShippingDeliver`. Record `carrier_payout`.
5. **In-flight re-check, every pass while carrying.** If the remaining buffer collapses —
   `remaining_hops × freightTicksPerHop × freightDeadlineSlack > deadline_tick − now_tick`,
   typically after a long disconnect — `ShippingReturn` at the next station rather than ride
   into a breach, and record `returned_inflight`.

### Restart reconciliation

Workers restart often (watchdog), and there is no `captains_log` task-resume yet
(`project_captains_log_task_resume`), so an in-memory task does not survive. On connect, before
taking any new work, the carrier reconciles from **server** state: `ShippingProfile`'s active
contracts (plus `ShippingGet` for detail) reveal any contract the agent already holds. It then
resumes the deliver leg, or returns the contract if the deadline is no longer feasible. This is
v1, not a follow-up: an orphaned in-flight package is the expensive failure mode.

## Data flow

```
docked pass ─▶ cargoFree ≥ 100 ? ───────────────── no ─▶ skip freight (no server calls)
            ─▶ shipping profile (debt guard) ── blocked ─▶ skip freight, log, alert
            ─▶ shipping list → eligible only
            ─▶ score: net = base_reward − fuel ≥ floor ? route clear ?
            ─▶ best_freight_net vs best_mission_net ─▶ take higher (else exploration)

  accept ─▶ unwrap action_result ─▶ deadline_tick now known
         ─▶ hops×19×1.5 ≤ deadline−now ?
              no ─▶ return (debt-free) ─▶ record returned_infeasible ─▶ fall through
              yes ─▶ withdraw package: → cargo ─▶ route to destination_base_id
                  ─▶ dock ─▶ deliver ─▶ carrier_payout + standing ─▶ record delivered
  each pass while carrying ─▶ buffer collapsed ? ─▶ return at next station

  reconnect ─▶ shipping profile.active_contracts ─▶ resume deliver leg (or return)
```

## Error handling

| Situation | Response |
|---|---|
| Empty board / `empty_reason` | Not an error; fall through to mission/exploration |
| Debt blocks acceptance | Skip freight, log once per pass, surface in status line |
| Accept race (taken / no longer eligible) | Drop that candidate, re-evaluate the rest |
| Withdraw fails or package won't fit | `return` before transit; never carry what we can't hold |
| Route/market lookup unavailable | Skip freight for the pass (fail-open, as the mission availability gate does) |
| Disconnected | Existing pass-skip applies (`c71ffed`); reconcile on reconnect before new work |
| Deliver fails at destination | Retry next pass while the buffer holds; `return` once it collapses |

Every path terminates in "skip the pass" or "`return` the contract". No path ends in a breach
we chose.

## Debt policy

If a breach ever does happen, the carrier **skips freight while blocked and never auto-pays**.
Debt is settled by an operator. Rationale: auto-payment would let a systematic breach bug
repeatedly buy back the ability to keep breaching, and a debt-blocked agent still earns on
missions and exploration, so the cost of waiting is small.

## Telemetry

New `market.FreightResult` and its table, mirroring `market.MissionResult`
(`pkg/market/types.go:307`): agent id, contract id, package id, origin and destination base,
service level, route hops, `base_reward`, `max_speed_bonus`, fuel cost, `carrier_payout`,
outcome, reason, accepted/finished tick and timestamp.

Outcomes: `delivered`, `returned_infeasible`, `returned_inflight`, `accept_failed`, `breached`.

Two signals drive the rollout decision:
- **`breached` must be identically zero.** Any nonzero value stops the rollout.
- **`returned_infeasible` rate.** A high rate means the pre-accept `route_hops` filter is too
  loose and we are spending ticks and `returns++` to learn what we should have estimated. It is
  also the data that re-tunes `freightTicksPerHop` and `freightDeadlineSlack` away from their
  single-sample values.

`returns++` on the carrier profile is an accepted cost of accept-then-verify. Its effect on
tier progression (probationary → licensed at 5 deliveries + 250 delivered value) is unmeasured;
the canary's profile is watched for it.

## Testing

Unit tests with the fake `GameClient` already used across `pkg/worker`:

- Footprint precondition: a hold with <100 free makes **zero** server calls.
- Candidate scoring: net floor, fuel subtraction, stronghold-blocked route rejection,
  ineligible listings excluded.
- Accept-then-verify, **both branches**: feasible proceeds to withdraw; infeasible returns and
  falls through.
- In-flight abort fires when the buffer collapses.
- Debt block skips freight without skipping the pass.
- Reconciliation resumes a held contract on connect, and returns one that is no longer feasible.

In `pkg/game`, a decode test for the `action_result` unwrap built from the **captured live
accept/deliver payloads** from the smoke, used as fixtures. This is the failure most likely to
slip through — it fails silently as an empty contract — and a fixture taken from real wire
bytes is the only honest guard.

## Rollout

1. Land the `client_api_monitor` / `kind` fix first — it is its own standalone task
   (`project_kind_discriminator_drift`). Until it lands, every shipping call spams
   `[SERVER API CHANGE]` (bare-action keying is unaware of the `shipping_` namespace, and
   `list` collides with facility), which would bury the canary's real signal.
2. Land B with unit tests green (`go build ./...`, `go test ./...`, `golangci-lint`).
3. **Canary a single mission-runner.** Watch breach-rate (must be 0), net per trip, and the
   `returned_infeasible` rate.
4. Re-tune `freightTicksPerHop` / `freightDeadlineSlack` from canary data.
5. Roll to the mission-learn pool.

Haulers remain phase 2, and multi-package trips for large holds are Sub-project C.

## Open questions

None blocking. Two items are knowingly single-sample and are scheduled for re-tuning from
canary telemetry in step 4 rather than resolved up front: `freightTicksPerHop` (19) and the
server's deadline formula. Accept-then-verify is specifically chosen so that neither constant
being wrong can cause a breach — a bad estimate costs a `return`, not a deadline.
