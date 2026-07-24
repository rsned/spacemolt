# Freight Load-Confirm + Restart-Resume Design

**Date:** 2026-07-23
**Status:** Approved (follow-up fixes surfaced by the freight-probation-bootstrap canary)

## Context

Two defects surfaced on the mission-learn freight canary (engineer-3, 2026-07-23),
both in `pkg/worker/freight.go`:

1. **Package strand** — a multi-package chain can navigate away with a just-accepted
   package still sitting in origin storage, then loop forever on `package_not_present`.
2. **Restart orphan** — a mid-chain restart wipes the in-memory held-freight set; because
   a carrier's own `in_transit` contracts never appear on the shipping board, reconcile
   cannot rediscover them and they default silently.

Both are scoped to the `missions` role with `--enable-freight`; the code is fully inert
otherwise.

## Fix #1 — Load-confirm poll

### Problem

`freightLoadPackage` issues `WithdrawItems(item, 1)` and returns `freightStepProceed`
immediately. `WithdrawItems` is **tick-deferred** (`client_commands.go:737` acks the
request, not the storage→cargo transfer). In `freightChainRefill`, a refill-accepted
package's withdraw is still pending when the chain navigates and undocks, so the package
is stranded in origin storage. Delivery and return then fail `package_not_present` on
every subsequent pass — a permanent stuck loop.

Contrast `freightSettleDock`, which already polls up to 3 ticks for a tick-deferred dock
to land before issuing a shipping mutation. `freightLoadPackage` has no equivalent guard.

### Design

After a **successful** `WithdrawItems`, poll `cargoCount(state, item) >= 1` for up to
`3 * game.SleepTick` at `game.SleepQuick` cadence, using `deps.sleep` (nil →
`craftPollSleepFunc`, the real ctx-aware sleep) so tests run instantly. This mirrors
`freightSettleDock` exactly.

- Package already aboard on entry → proceed without withdraw (unchanged).
- Withdraw errors → return the contract (unchanged).
- Withdraw succeeds, package confirmed in cargo within budget → `freightStepProceed`.
- Withdraw succeeds, package **not** in cargo after the full budget → return the contract
  (`returned_infeasible`, reason `"package did not load into cargo after withdraw"`). This
  is the same debt-free return path the withdraw-error case already uses.
- `deps.sleep` returns a ctx error mid-poll → `freightStepStuck`. The contract was already
  added to the held set by `freightAccept`, so it stays live; the pass parks rather than
  navigating on with an unconfirmed load, and the next session's reconcile settles it.

The new poll lives in a helper `freightPollLoaded(ctx, deps, item, out) (bool, error)`
(true = confirmed aboard; error = ctx cancelled) so it is unit-testable in isolation and
`freightLoadPackage` stays a thin dispatcher.

## Fix #2 — Held-freight persistence (restart resume)

### Problem

`missionRunState.heldFreight` is in-memory and dies with the worker process. A carrier's
own `in_transit` contracts never list on the board, so `freightReconcileSet` cannot
rediscover them after a restart — they orphan and default. Today this only produces a
loud "UNRECOVERABLE without operator rescue ... no captains_log resume yet" log line.

### Design

Persist the held-contract set to disk and reload it at worker start, so the existing
reconcile machinery can resume it.

- **Location:** `<AgentsDir>/<agent-id>/freight-held.json`, a JSON array of the full
  `serverapi.ShipmentContract` values (the same objects `heldFreightAll()` returns, sorted
  by ID for a stable file). Persisting the full contract — not just IDs — lets chain math
  work immediately on resume even if a `ShippingGet` fails (fail-open).
- **Write:** atomic `tmp + rename` (the `schedule.go:saveLocked` pattern), `MkdirAll` the
  agent dir first. Triggered after every `addHeldFreight` / `removeHeldFreight` mutation.
- **Concurrency:** one process per agent (overmind dedupes by `credentials.json`), so no
  cross-process locking is needed.
- **Load:** lazy, on the first `missions` pass, gated on `EnableFreight && AgentID != ""`
  (no file churn for non-freight fleets). Load the file and seed `heldFreight`, then wire
  the persist callback. `sync.Once` guards one-time setup.
- **Resume reuses reconcile:** once `heldFreight` is seeded, `freightReconcileSet` /
  `freightVerifyHeld` (which already run before every pass) re-read each contract by ID via
  `ShippingGet` and refresh (`in_transit`) or record-and-drop (`delivered`/`defaulted`/
  other). No new resume logic — persistence only restores the ID set reconcile needs.
- **Mismatch log:** update the `freightReconcileSet` line that says "no captains_log resume
  yet" to reflect that disk resume now exists (the mismatch detector stays — it still fires
  if the file was lost or corrupted).

### Wiring

`missionRunState` gains an optional `persistHeld func([]*serverapi.ShipmentContract)`
callback (nil = in-memory only, the current behavior — existing nil-receiver tests stay
green). `addHeldFreight` / `removeHeldFreight` invoke it with the post-mutation set.
`WorkerDispatch` builds the path from `d.agentsDir()` + `d.AgentID`, loads the file, seeds
`d.mission.heldFreight`, and installs the callback — once, lazily, in the `missions` case.

## Constraints

- `golangci-lint` clean, no new findings.
- Sleep values from `pkg/game/constants.go` only.
- Atomic file writes; JSON via `encoding/json`.
- `missionRunState` methods stay nil-safe; all existing `pkg/worker` tests stay green.
- Persistence is inert unless `EnableFreight && AgentID != ""`.

## Out of scope

- Marketbot freight-demand scan: the shipping board is tier-filtered (`shipping
  --action=list` returns only shipments the caller is eligible to accept), so a per-tier
  marketbot cannot build a universe demand map. That plan needs a rethink; tracked
  separately, not part of this work.
- Server-side active-contract listing: `shipping profile` returns `active_contracts` as a
  count only (schema-confirmed), so disk persistence — not a server query — is the resume
  source.
