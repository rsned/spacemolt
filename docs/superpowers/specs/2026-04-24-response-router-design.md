# Response Router — Client-Side Request/Response Correlation

**Status:** Design approved, awaiting implementation plan
**Date:** 2026-04-24

## Background

`pkg/game/Client` sends WebSocket messages fire-and-forget: `c.Send(ctx, msg)`
writes to the socket and returns immediately. The server's reply arrives on a
separate goroutine and updates `State` parsers or lands in a shared
`latestRawJSON["_last"]` slot. There is **no correlation ID on the wire** —
`protocol.Message` and `protocol.Response` both lack an `id`/`request_id`
field, and the server is not under our control, so adding one is off the
table.

Today's receive-side routing is a mix of:

- `CommandQueue` — serialized, per-command synthetic ID, only used by a
  subset of methods.
- `waiters map[type]chan` — single-slot per response type; multiple waiters
  for the same type collide.
- `latestRawJSON["_last"]` — shared mutable buffer that any non-push response
  clobbers.
- Type-specific parsers — run synchronously on receive, mutate `State`.
- Typed push callbacks — `SetOnChatMessage`, `SetOnTickReceived`, etc.

Recent bugs all trace to this racy layering:

- `mbox backfill` read `_last` before the server replied → reported `0
  messages` and aborted ingest.
- `chatPoller.fetchMessages` same race.
- `deposit_all` read `state.Ship.Cargo` before `get_cargo`'s reply landed →
  tried to deposit a stale quantity, server rejected.

Each has been patched with `time.Sleep(...)` before reading the shared slot.
That's a workaround, not a fix. The underlying flaw — no deterministic way to
match a response to the caller that sent the request — remains.

## Goal

Give every outbound message a deterministic path for receiving its
response(s), without relying on `time.Sleep`, without changing the wire
protocol, and without a big-bang rewrite.

## Non-goals

Explicit scope cuts so the spec stays focused:

- **No wire-protocol changes.** No `request_id` on `Message`/`Response`. All
  correlation is client-side heuristics.
- **No mutation queueing.** Only one mutation in flight at a time; a second
  `execMutation` call blocks on `mutationMu`. No priority queue,
  cancellation-of-in-flight, or dependency graphs.
- **No reconnect replay.** If the connection drops mid-mutation, the caller
  gets `ErrDisconnected` and decides what to do. The router doesn't re-send.
- **No changes to existing push callback signatures.** `SetOnChatMessage`,
  `SetOnTickReceived`, etc. stay on the `Client` surface — internally they
  become thin wrappers over `subscribePush`.
- **No new push fan-out performance work.** Synchronous dispatch into
  handlers, documented "don't block" rule.
- **No changes to `State` parsing.** Existing parsers keep running as
  side-effects in `dispatch`.
- **No agent runner / MCP client rewrites.** They consume the `GameClient`
  interface; the new primitives are internal to `*Client`, interface methods
  keep their signatures.
- **No catalog-wide refactor of return shapes.** Each migrated method
  preserves its current return type.
- **No metrics / tracing as a feature.** Existing debug logging stays.
- **No test harness rework.** `fakeGameClient` and the existing mocks keep
  their shapes.

## Architecture

A new `responseRouter` owns the WebSocket receive side. It replaces the
current soup of `waiters[type]`, `CommandQueue.handleResponse`, and shared
`_last` routing. Three caller-facing primitives live on top of it.

```
           ┌──────────────────────────────────────┐
           │              WebSocket rx            │
           └───────────────────┬──────────────────┘
                               │ protocol.Response
                               ▼
                     ┌──────────────────┐
                     │  responseRouter  │  ← only consumer of incoming frames
                     │  .dispatch(resp) │
                     └──────┬────┬──────┘
                  match     │    │    match
       ┌────────────────────┘    └───────────────┐
       ▼                                         ▼
┌─────────────┐    ┌─────────────┐    ┌──────────────────┐
│ mutation    │    │ query       │    │ push             │
│ subscriber  │    │ subscriber  │    │ subscribers      │
│ (exactly 1, │    │ (N, FIFO    │    │ (N per event     │
│  serialized │    │  within     │    │  type, fan-out)  │
│  on mu)     │    │  class)     │    │                  │
└─────────────┘    └─────────────┘    └──────────────────┘
```

Properties:

- One reader goroutine, one `dispatch` call per response. No parallel writes
  to `State` from receive.
- `execMutation`, `execQuery`, `subscribePush` are the only ways callers
  interact with send + receive. `Send` becomes internal (unexported) once the
  migration finishes.
- Dispatch order within `dispatch(resp)`:
  1. Fan out to all matching push subscribers.
  2. Deliver to the active mutation subscriber (if any) iff its classifier
     matches AND its terminator fires.
  3. Deliver to the earliest-registered matching query subscriber (FIFO
     within classification).
- `storeRawJSON("_last")` continues as a final side-effect catchall so
  interactive REPL debug dumps still work, but nobody *routes* via `_last`.
- Existing `State` parsers (`parseShipData`, `parseCargoData`,
  `parseChatHistoryData`, …) keep running as side-effects in `dispatch` so
  background state stays fresh.

## Classifier library

Used by queries, pushes, and as the "match mine" part of mutations.

```go
type Classifier func(resp protocol.Response) bool

func matchType(t string) Classifier          // resp.Type == t
func matchAction(name string) Classifier     // Payload["action"] == name
func matchCommand(name string) Classifier    // Payload["command"] == name
func matchChannel(channel string) Classifier // Payload["channel"] == channel
func matchPayloadKey(key string) Classifier  // Payload[key] exists (shape)
func matchAll(cs ...Classifier) Classifier   // AND combinator
```

Coverage of observed response shapes:

| Response                    | Classifier                                                              |
| --------------------------- | ----------------------------------------------------------------------- |
| `get_cargo`                 | `matchAll(matchType(OK), matchPayloadKey("cargo"))`                     |
| `get_system`                | `matchAll(matchType(OK), matchAction("get_system"))`                    |
| `get_chat_history("system")`| `matchAll(matchType(OK), matchChannel("system"), matchPayloadKey("messages"))` |
| `deposit_items` terminal    | `matchCommand("deposit_items")`                                         |
| `chat_message` push         | `matchType(TypeChatMessage)`                                            |

## Terminator library

Used only by mutations. The classifier gates "does this response concern my
command"; the terminator runs only against responses the classifier already
accepted and decides whether to resolve the mutation.

```go
type Terminator func(resp protocol.Response) (done bool, err error)

// Most common: done on TypeActionResult, error on TypeActionError / TypeError
var terminateOnAction Terminator // shared default

// For travel/jump — terminal is "arrived"
var terminateOnArrived = terminateOnTypes(TypeArrived, TypeActionError, TypeError)
var terminateOnDocked  = terminateOnTypes(TypeDocked,  TypeActionError, TypeError)
```

Intermediate events (`tick`, `traveling`, `mining_yield`, `combat_update`)
fail the mutation's classifier — they never touch the mutation wait and flow
to push subscribers instead.

Edge case: a generic `TypeError` with no `command` field is delivered to the
active mutation if one is holding the lock (the only plausible owner);
otherwise it drops to `_last` for REPL visibility.

## Primitives

```go
// Query: single classified reply. Can run concurrently with other queries
// and (non-exclusively) alongside a mutation.
func (c *Client) execQuery(
    ctx context.Context,
    msg protocol.Message,
    match Classifier,
    timeout time.Duration,
) (protocol.Response, error)

// Mutation: holds mutationMu for the whole lifetime; waits for a response
// that both matches AND satisfies the terminator.
func (c *Client) execMutation(
    ctx context.Context,
    msg protocol.Message,
    match Classifier,
    terminate Terminator,
    timeout time.Duration,
) (protocol.Response, error)

// Push: long-lived fan-out. Returns cancel — caller owns its lifetime.
func (c *Client) subscribePush(
    match Classifier,
    handler func(protocol.Response),
) (cancel func())
```

Internal state:

```go
type subscription struct {
    match      Classifier
    terminate  Terminator              // nil for queries and pushes
    respCh     chan protocol.Response  // one-shot for query/mutation
    handler    func(protocol.Response) // push only
    registered time.Time               // for FIFO ordering within class
}
```

Locking:

- `mutationMu sync.Mutex` — held by `execMutation` for its full duration.
  Queries and pushes do **not** block on this.
- `subsMu sync.Mutex` — protects the subscriber list. Held briefly in
  `dispatch` and in register/unregister.

Timeout & cancel semantics:

- Query/mutation timeout: subscription removed, caller gets `timeout` error.
  If the response arrives later, `dispatch` finds no matching subscriber and
  falls through to `_last`.
- `ctx.Done()` behaves the same.
- Connection loss: router cancels all pending query + mutation subscriptions
  with `ErrDisconnected`. Push subscribers survive reconnect.

Concurrent same-classification queries resolve FIFO by `registered`
timestamp. WebSocket is single-reader and server responses to identical
queries arrive in send order. Caveat for same-classification queries that
could legitimately return different payloads per call — not present in the
current code; if one appears, the cure is a more specific classifier
(e.g., including a parameter value in the match).

## Migration

Five batches, each shippable independently:

| Batch       | Methods                                                                                                                                             | Why first/later                                                                 |
| ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| 0 — validation | `GetCargo`, `GetChatHistory`                                                                                                                    | Both have active bug reports; exercises `matchPayloadKey` and `matchChannel`.   |
| 1 — queries | `GetStatus`, `GetShip`, `GetSystem`, `GetPOI`, `GetMap`, `GetSkills`, `ViewStorage`, `ViewMarket`, `GetNearby`, `GetNotifications`                  | One-shot, no mutation-lock complexity.                                          |
| 2 — simple mutations | `DepositItems`, `WithdrawItems`, `Buy`, `Sell`, `Jettison`, `Refuel`, `Repair`, `SellAll`, `Craft`, `CreateBuyOrder`, `CreateSellOrder`      | All use `terminateOnAction` — validates the default terminator.                 |
| 3 — complex mutations | `Travel`, `Jump`, `Dock`, `Undock`, `Mine`, `Salvage`, `Battle`, `Scan`                                                                    | Custom terminators + intermediate-event push streams.                           |
| 4 — long tail | Factions (30+), missions, forum, chat send, social, drones, bases, insurance, commission, captain's log                                          | Mechanical pass once the pattern is proven.                                     |

After batch 0, `CommandQueue`, `waitForResponse`, `waitForActionResponse`
are **not** yet removed — marked `// Deprecated: use execQuery/execMutation`
so new call sites surface as IDE/vet warnings. Removal happens after the
last method migrates.

Tracking lives in `docs/migration/response-router.md` — methods with ✅ when
migrated, linking to the PR.

### "No new legacy" enforcement

Two layers:

1. **Compile-time deprecation** — legacy entry points get
   `// Deprecated: use execQuery/execMutation` godoc. IDEs highlight.
   Advisory.

2. **CI gate** — `scripts/check_legacy_response_api.sh`: greps `pkg/game`
   for new references to `c.Send(` / `client.Send(`,
   `waitForResponse`, `waitForActionResponse`, `CommandQueue.Enqueue`
   outside a shrinking allowlist of files not yet migrated. PR adding a
   new legacy call → CI fails. PR finishing a migration → edit the
   allowlist. Once the allowlist is empty, `Send` is renamed `send`,
   `client_queue.go` is deleted, and the check script is deleted.

### Per-method validation

- **Unit test** against a `fakeRouter`: the method builds the right `msg`,
  registers the right classifier, surfaces the response correctly, and
  cleans up on timeout / ctx-cancel.
- **Integration test** in `pkg/game` using a scripted fake WebSocket
  (existing `fakeGameClient` pattern): server sends a synthetic payload,
  client returns parsed result.
- **Smoke in `play_as`** — rerun `mbox backfill` and `deposit_all` as
  live regressions of the exact bugs that motivated this work.

## Example — `GetCargo` migrated

Before (current, racy):

```go
func (c *Client) GetCargo(ctx context.Context) error {
    return c.Send(ctx, protocol.Message{Type: "get_cargo", ...})
}
// Callers then read c.GetState().Ship.Cargo, which may still be stale.
```

After:

```go
func (c *Client) GetCargo(ctx context.Context) error {
    msg := protocol.Message{Type: "get_cargo", Timestamp: time.Now().UnixMilli()}
    match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("cargo"))
    resp, err := c.execQuery(ctx, msg, match, SleepMedium)
    if err != nil {
        return err
    }
    // parseCargoData already runs in dispatch — State is now fresh.
    // Return successful; caller reads State.Ship.Cargo, guaranteed current.
    _ = resp
    return nil
}
```

Callers (`DepositAllItems`, any other flow that reads cargo) no longer need
`time.Sleep` after `GetCargo` — on return, `State.Ship.Cargo` is
guaranteed to reflect the server's reply.

## Example — `DepositItems` migrated

```go
func (c *Client) DepositItems(ctx context.Context, itemID string, qty float64) error {
    msg := protocol.Message{
        Type:    "deposit_items",
        Payload: map[string]any{"item_id": itemID, "quantity": qty},
        ...
    }
    _, err := c.execMutation(
        ctx, msg,
        matchCommand("deposit_items"),
        terminateOnAction,
        SleepTick*3,
    )
    return err
}
```

`execMutation` holds `mutationMu` for the whole call. A second
`DepositItems` (or any other mutation) issued concurrently blocks until
the first resolves — which is the current de-facto behavior, made
explicit and correct.

## Open questions (to be resolved in the implementation plan)

- Exact error type hierarchy (`ErrTimeout`, `ErrDisconnected`, `ServerError`
  wrapping `action_error` details).
- Whether the router goroutine reads directly from the WebSocket or from a
  channel fed by an existing reader — pick during implementation based on
  which is less invasive to `client.go`'s current structure.
- Whether the allowlist file should be shell-grepped or `.golangci.yml`
  (custom `forbidigo` rule). Decision during batch 0.
