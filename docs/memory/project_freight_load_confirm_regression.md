---
name: project_freight_load_confirm_regression
description: The v0.2.1 freight load-confirm check (a77df25) is a false negative that returns every healthy contract — 0 deliveries fleetwide since 2026-07-24T10:00Z
metadata: 
  node_type: memory
  type: project
  originSessionId: 2f3b8937-e63d-42aa-8015-c67d52bc5fd2
  modified: 2026-07-25T22:06:51.290Z
---

**2026-07-25 — Freight is 100% broken fleetwide. Root cause: the load-confirm check itself.**

## The cutover (authoritative, from `market.db` `freight_results`)

| date | delivered | returned_infeasible | return_failed |
|---|---|---|---|
| 07-20 | 2 | 0 | 0 |
| 07-21 | 11 | 0 | 0 |
| 07-22 | 20 | 0 | 0 |
| 07-23 | 34 | 0 | 0 |
| 07-24 | 37 | 37 | 52 |
| 07-25 | **0** | **51** | 0 |

Last delivery ever: `2026-07-24T09:59:06Z`. First `returned_infeasible` ever: `2026-07-24T10:03:36Z`, reason `package did not load into cargo after withdraw`. 67 clean deliveries in the 4 days before, zero since. 88 consecutive returns.

**Cause: `a77df25 fix(freight): confirm package loaded into cargo before proceeding` (v0.2.1), rolled out with the fleet restart at ~10:00Z on 07-24.** Before that check existed, withdraws loaded and delivered fine. The check is what broke it — it is a **false negative**, not a real load failure.

⚠️ Do NOT attribute this to the v0.2.3 tagging build (`bin/worker` built 2026-07-25T02:49:49Z). The `[worker:<id>]` tag boundary sits ~17h AFTER the real breakage, so splitting the log by tagged/untagged gives a misleading "it started with v0.2.3" reading. Use `market.db freight_results`, not the log, for freight timing.

## Mechanism (confirmed by reading the code)

1. `WithdrawItems` is tick-deferred — it only acks `Action 'withdraw_items' pending` and **never returns an error**. A fleetwide grep finds zero withdraw errors.
2. `freightPollLoaded` (pkg/worker/freight.go:697) polls `deps.Client.GetState().Ship.Cargo` for `3 * game.SleepTick` (30s) and **issues no refresh command** — unlike `freightSettleDock`, which nudges with an explicit `Dock`.
3. The worker heartbeat (cmd/worker/main.go:477) also only reads `client.GetState()`; it queries nothing.
4. So `Ship.Cargo` never refreshes inside the poll window → check times out → `freightLoadPackage` calls `freightReturn`, which does a real server-side `ShippingReturn` on a perfectly good contract.

**Likely fix:** issue an explicit `GetCargo`/`GetShip` refresh inside the `freightPollLoaded` loop (mirroring the `freightSettleDock` nudge pattern). Cheapest discriminator: if failures vanish with a refresh in the loop, the false-negative diagnosis is confirmed.

## Collateral damage

- Explorers: 16 contracts, **0 delivered, 16 returned, 0 payout** → zero probation/tier progress since being re-added.
- Each failure calls `ShippingReturn`, which increments the server-side carrier `Returns` counter. `CarrierProfile` exposes `Returns`/`Breaches`/`Defaults`/`OutstandingDebt` but the worker logs **none** of them (only `DebtBlocksAcceptance` and the tier line), so accumulating reputational damage is invisible from the logs.
- No agent is currently debt-blocked: zero `operator must settle` lines in the current era (all such lines, and all 52 `return_failed`, are from the older 07-24 02:22–02:31Z incident).
- Ships are clean — `cargo_used` 0–11 across explorers, no ~100-unit packages stranded aboard.

## Second, independent blocker

Most explorer freight polls log `No freight contracts are posted at this station right now` — the still-open reposition-onto-live-hubs problem. explorer-11 at `the_rampart_checkpoint` additionally sits on `licensed cargo requires licensed carrier standing` (fence behaving correctly, but leaves it nothing to take).

See [[project_freight_load_confirm_and_resume]], [[project_freight_probationary_cargo_fence]], [[reference_worker_storage_capture_gap]].

**Note:** `storage_snapshots` in the KB is ~3 weeks stale (last capture 2026-07-03), so it cannot answer "is a package left in station storage" — that needs a live `view_storage`.
