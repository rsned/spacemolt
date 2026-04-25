# Response Router Migration Tracking

**Spec:** [2026-04-24-response-router-design.md](../superpowers/specs/2026-04-24-response-router-design.md)
**Phase 0 plan:** [2026-04-24-response-router-phase0.md](../superpowers/plans/2026-04-24-response-router-phase0.md)
**CI gate:** [`scripts/check_legacy_response_api.sh`](../../scripts/check_legacy_response_api.sh)

The router replaces fire-and-forget `c.Send` + racy `_last`/`waiters[type]` lookups
with three primitives — `execQuery`, `execMutation`, `subscribePush` — sharing a
single dispatch loop. Methods migrate in batches; the CI gate's allowlist
shrinks as each batch lands.

Legend: ✅ migrated · 🚧 in progress · ⬜ not started

## Batch 0 — Validation

| Method            | Status | Notes                          |
| ----------------- | ------ | ------------------------------ |
| `GetCargo`        | ✅     | Phase 0 plan, Task 9 (b96e6b7) |
| `GetChatHistory`  | ✅     | Phase 0 plan, Task 10 (a48be07) |

## Batch 1 — Queries

One-shot `ok` reply. Use `execQuery` with a `matchAll` of `matchType(OK)` plus
either `matchAction(name)` (when the response includes an `action` field) or
`matchPayloadKey(key)` (when it doesn't).

| Method              | Status |
| ------------------- | ------ |
| `GetStatus`         | ⬜     |
| `GetShip`           | ⬜     |
| `GetSystem`         | ⬜     |
| `GetPOI`            | ⬜     |
| `GetMap`            | ⬜     |
| `GetSkills`         | ⬜     |
| `ViewStorage`       | ⬜     |
| `ViewMarket`        | ⬜     |
| `GetNearby`         | ⬜     |
| `GetNotifications`  | ⬜     |

## Batch 2 — Simple mutations

`pending ok` → `action_result` / `action_error`. Use `execMutation` with
`matchCommand(name)` and the default `terminateOnAction`.

| Method              | Status |
| ------------------- | ------ |
| `DepositItems`      | ⬜     |
| `WithdrawItems`     | ⬜     |
| `Buy`               | ⬜     |
| `Sell`              | ⬜     |
| `Jettison`          | ⬜     |
| `Refuel`            | ⬜     |
| `Repair`            | ⬜     |
| `SellAll`           | ⬜     |
| `Craft`             | ⬜     |
| `CreateBuyOrder`    | ⬜     |
| `CreateSellOrder`   | ⬜     |

## Batch 3 — Complex mutations

Multi-stage: `pending ok` → progress events (`tick`, `traveling`,
`mining_yield`, `combat_update`) → terminal (`arrived`/`docked`/`action_result`/
`player_died`/`pirate_destroyed`). Use `execMutation` with a custom
`terminateOnTypes(...)` for the terminal types; intermediate events flow to
`subscribePush` listeners.

| Method              | Status | Terminator                                      |
| ------------------- | ------ | ----------------------------------------------- |
| `Travel`            | ⬜     | `terminateOnTypes(POIArrival, ActionError)`     |
| `Jump`              | ⬜     | `terminateOnTypes(POIArrival, ActionError)`     |
| `Dock`              | ⬜     | `terminateOnTypes(Docked, ActionError)`         |
| `Undock`            | ⬜     | `terminateOnTypes(Undocked, ActionError)`       |
| `Mine`              | ⬜     | `terminateOnAction` + `MiningYield` push        |
| `Salvage`           | ⬜     | `terminateOnAction`                             |
| `Battle`            | ⬜     | `terminateOnAction` + `CombatUpdate`/`PlayerDied` push |
| `Scan`              | ⬜     | `terminateOnAction`                             |

## Batch 4 — Long tail

Factions (30+), missions, forum, chat send (`Chat`, `TradeOffer`, `SendGift`),
drones, bases, insurance, commission, captain's log, and the external tool
call sites currently allowlisted in `scripts/check_legacy_response_api.sh`:

- `cmd/debug/play-simple/main.go`
- `cmd/tools/facility-check/main.go`
- `cmd/tools/faction-join/main.go`
- `cmd/tools/server-cmd/main.go`

Enumerate per-method when batch 3 lands. Each external tool likely needs only
a handful of method swaps; group them by tool in the follow-up plan.

## Completion criteria

When every row above is ✅ and `scripts/check_legacy_response_api.sh` reports
no allowlist entries:

1. Delete `scripts/check_legacy_response_api.sh` and its pre-commit
   integration in `scripts/setup-pre-commit.sh` (and `.git/hooks/pre-commit`).
2. Rename `Client.Send` → `client.send` (unexported); only the response router
   should call it.
3. Delete `pkg/game/client_queue.go`, `pkg/game/client_queue_test.go`, and the
   `CmdQueue` field on `Client`.
4. Remove `waitForResponse`, `waitForActionResponse`, and the `waiters` map on
   `Client`. Remove the legacy notify block in the read loop.
5. Delete this tracking doc.

## Follow-ups not in Phase 0

Items deferred from review feedback during Phase 0:

- **Integration test for the read-loop wiring.** A regression that deletes the
  `c.router.dispatch(resp)` call in `client.go` would currently pass CI. Add a
  test that exercises a full `WebSocket → handleResponse → dispatch` flow once
  Batch 1 lands — by then the router is on the critical path of multiple
  methods and silent breakage is more likely to surface as test failures
  anyway, but an explicit regression test is the belt-and-braces fix.
- **Terminator panic semantics.** A terminator that panics on every input pins
  its mutation's mutex for the full timeout duration. Phase 1 may want either
  a panic-once circuit-breaker that unregisters after N panics, or to surface
  the recovered panic as an error on `respCh`. Document choice when it
  matters; for Phase 0 the wait-for-timeout behavior is acceptable.
- **`_last`-clobber window for downstream readers.** The router unblocks
  `execQuery` synchronously, but the read goroutine continues to the next
  frame and may overwrite `_last` before the caller reads it. Push events are
  filtered out of `_last`; concurrent `ok` responses from another goroutine
  are not. Today's callers (`mbox` sequential backfill, `play_as` single
  poller) don't hit this. The Phase 1 fix is to have callers consume
  `resp.Payload` directly from `execQuery`'s return value rather than
  round-tripping through `_last`.
