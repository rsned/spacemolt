# request_id WebSocket Pipeline — Design

**Date:** 2026-05-15
**Server target:** SpaceMolt Gameserver v0.296.1
**Status:** Spec approved, plan pending

## Background

Server v0.296.1 added an optional `request_id` field on any client frame; the server echoes it back on every response derived from that frame — the `pending:true` ack, the post-tick `action_result`, and any `error`/`action_error`. The patch notes also document the `close=1000` graceful-close lifecycle (max session age, inactivity timeout, rolling restart).

Today the client correlates responses via Classifier heuristics (`matchCommand`, `matchAction`, `matchPayloadKey`, `matchType`) and serializes mutations through a single `c.mutationMu` so only one mutation is in flight at a time. The `pkg/game/Client` also retains a legacy `waiters` map (type-keyed, single-slot), a deprecated `CommandQueue`, and several deprecated `waitFor*` helpers. With server-echoed `request_id` we can replace correlation guesswork with exact matching, allow truly concurrent mutations, and delete a large amount of code.

## Goals

1. Replace classifier-based response correlation with exact `request_id` lookup as the primary path.
2. Allow multiple mutations to be in flight concurrently, subject to per-action serialization and a soft global cap.
3. Expose a new low-level `Submit` primitive that returns a handle whose `Ack` and `Result` channels separate the `pending:true` ack from the terminal result.
4. Delete `CommandQueue`, the `waiters` map, `mutationMu`, `waitForResponse` / `waitForAuthResponse` / `waitForActionResponse`, `execMutation`, and `execQuery`.
5. Handle `close=1000` transparently by replaying outstanding mutations with fresh request_ids on the new connection; fail-fast on other close codes; flag unknown close codes.
6. Make every concurrency-sensitive component testable under `-race`.

## Non-goals

- Rewriting the 100+ blocking command methods to a new async API surface. They remain blocking wrappers around `Submit` + `<-handle.Result(ctx)`.
- Replacing `subscribePush` for unsolicited events (`chat_message`, `pirate_warning`, `tick`, `state_update`, ...). Those frames inherently have no `request_id` and continue to use classifier-based push fan-out.
- Backward compatibility with servers older than v0.296.1. `BuiltForAPIVersion` bumps; older servers are unsupported and fail at the welcome version check.
- Adding a structured `RequestID` on every existing call site. The handle exposes it for callers that want logging; otherwise the field is internal.

## Decisions (locked)

| Topic | Decision |
|---|---|
| Server compat | Bump `BuiltForAPIVersion` to `v0.296.1`; hard-require it on welcome. |
| Untagged frames | Pure push channel via `subscribePush`, with the existing classifier-based path retained as a fallback escape hatch. |
| Pending-ack semantics | `Submit` returns a `*RequestHandle` exposing separate `Ack()` and `Result()` channels. Blocking command wrappers consume only `Result()`. |
| Concurrency | `mutationMu` deleted. Replaced by per-action serialization (one of each `msg.Type` at a time) plus a soft global cap of 16 outstanding mutations. Queries bypass per-action serialization. |
| Close handling | `close=1000`: snapshot pending mutations, reconnect, replay with fresh UUIDs (server has no memory of prior IDs, so no double-execution). Other documented codes: fail-fast with `ConnectionClosed{Code, Reason}` error to all pending handles. Unknown codes: log WARN and fail-fast. |
| request_id format | UUIDv7 via `github.com/google/uuid`. Time-ordered, 36 chars, globally unique without per-connection state. |
| Migration shape | Two-layer API. `Submit` is the new primitive; existing blocking methods become wrappers. No mass call-site rewrite. |

## Architecture

### Wire protocol (`internal/protocol/messages.go`)

```go
type Message struct {
    Type      string         `json:"type"`
    Payload   map[string]any `json:"payload,omitempty"`
    Timestamp int64          `json:"timestamp,omitempty"`
    RequestID string         `json:"request_id,omitempty"`
}

type Response struct {
    Type      string         `json:"type"`
    Payload   map[string]any `json:"payload,omitempty"`
    RequestID string         `json:"request_id,omitempty"`
}
```

### Submit primitive (`pkg/game/submit.go`, new file)

```go
type RequestHandle struct {
    ID     string                  // current UUIDv7; mutated on reconnect-replay
    ack    chan protocol.Response  // buffered 1; closed if no ack will arrive
    result chan Result             // buffered 1
}

type Result struct {
    Response protocol.Response
    Err      error // *ServerError, *ConnectionClosed, *ConnectionLost, ctx.Err(), or timeout
}

func (h *RequestHandle) Ack(ctx context.Context) (protocol.Response, error)
func (h *RequestHandle) Result(ctx context.Context) (protocol.Response, error)

type SubmitOption func(*submitConfig)

func WithTerminator(t Terminator) SubmitOption  // default: terminateOnAction
func WithTimeout(d time.Duration) SubmitOption  // default: SleepTick*3
func WithAckOnly() SubmitOption                 // ack IS the answer (queries, handshakes); skips per-action lock; ignores any WithTerminator

func (c *Client) Submit(ctx context.Context, msg protocol.Message, opts ...SubmitOption) (*RequestHandle, error)
```

`Submit` is the only public path that stamps `RequestID`. It generates a UUIDv7, mutates `msg.RequestID`, registers a subscription under that ID, takes the per-action lock (unless `WithAckOnly()`), takes a global-cap slot, writes the frame, and returns the handle. A single per-Submit cleanup goroutine owns the slot/lock release and the eventual `Result` delivery, ensuring no leaks regardless of caller behavior.

### Router (`pkg/game/response_router.go`)

```go
type responseRouter struct {
    mu      sync.Mutex
    byReqID map[string]*subscription  // primary: exact request_id correlation
    subs    []*subscription           // fallback: push handlers + untagged classifier subs
    orphan  orphanStats               // counters for unmatched request_ids
}
```

`dispatch(resp)` order:

1. If `resp.RequestID != ""`: look up `byReqID[resp.RequestID]`.
   - **Hit:** deliver to the subscription. If the response satisfies `terminate()`, unregister and close out the handle. Otherwise (pending ack), forward to `ack` channel and leave subscription live. **Done — do not fall through.**
   - **Miss:** orphan. Increment counter, record metadata, log WARN at ≤1/sec, drop. **Done — do not fall through.**
2. Otherwise (`resp.RequestID == ""`): existing logic — push fan-out, then classifier-based query/mutation match.

Snapshot-under-lock pattern is preserved for safe iteration during dispatch.

### Concurrency layer

**Soft global cap (`pkg/game/inflight.go`).** `chan struct{}` of capacity 16 (configurable via `Client.SetMaxInFlight`). `Submit` blocks on the channel before sending, respecting `ctx`. Slot is released by the per-Submit cleanup goroutine on terminal/error/cancel.

**Per-action serialization (`pkg/game/actionlock.go`).** `map[string]*ctxMutex` keyed on `msg.Type`, protected by `sync.RWMutex` (rare key creation, frequent lookups). `ctxMutex` is a 1-capacity `chan struct{}` wrapping `LockCtx(ctx) error` and `Unlock()`. `Submit` acquires the action lock after the global cap. Submits configured with `WithAckOnly()` (queries) skip this layer entirely.

**Lock/cap release ordering on cleanup:** action lock first, then global cap. Always invoked from a `defer` chain so panic safety is preserved.

### Reconnect & replay (`pkg/game/client.go` listen loop)

On read-loop close detection:

1. Snapshot live request_id subscriptions under router lock. For each, retain the original `protocol.Message` (sans request_id) and the cleanup goroutine's channels.
2. Consult `closeCodePolicy` registry.
3. Run policy:
   - **`closeReplay`** (currently: `code=1000` + documented reason): tear down conn, run existing reconnect+login machinery. On success, for each snapshotted subscription: generate fresh UUIDv7, update `handle.ID`, re-register in `byReqID` under the new ID, re-write the frame. Caller sees no error.
   - **`closeFailFast`** (`1001`, `1006`, `1011`, any unknown): all pending handles receive `Result{Err: &ConnectionClosed{Code, Reason}}`. Cleanup goroutines release slots and locks normally.
4. If reconnect itself fails (network down, auth fails): all pending handles receive `&ConnectionLost{}`.
5. If caller's `ctx` expires during reconnect: that handle alone gets `ctx.Err()`; other pending handles continue waiting for the reconnect outcome.

### Close-code registry (`pkg/game/close_codes.go`, new file)

```go
type closeAction int
const (
    closeReplay closeAction = iota
    closeFailFast
    closeAbort // reserved
)

type closeCodePolicy struct {
    code         websocket.StatusCode
    knownReasons []string // best-effort reason-prefix match; empty = any reason
    action       closeAction
    note         string
}

var knownCloseCodes = []closeCodePolicy{
    {code: 1000, knownReasons: []string{"max session age", "inactivity", "rolling restart"},
     action: closeReplay, note: "documented graceful close (v0.296.1)"},
    {code: 1001, action: closeFailFast, note: "going away"},
    {code: 1006, action: closeFailFast, note: "abnormal close (no close frame)"},
    {code: 1011, action: closeFailFast, note: "server internal error"},
}
```

Unknown code-or-reason combinations log:

```
WARN: unknown WebSocket close: code=%d reason=%q — please document; defaulting to fail-fast
```

A documented `close=1000` with an undocumented reason ALSO logs the WARN, then proceeds with replay (we assume the server's intent matches the other `1000` cases). This makes new server-side close vocabulary loudly visible without breaking the client.

### Orphan handling

When a response carries a `request_id` that misses `byReqID`:

- Increment `orphanStats.count` (atomic).
- Record last ID / type / timestamp.
- Log `WARN: orphan response request_id=%s type=%s` rate-limited to ≤1 line per second.
- Drop the frame. Do NOT fall through to the classifier path — a tagged-but-unknown ID is unambiguous; falling through risks mis-delivery.

Cases this covers:

- Late response from a pre-`close=1000` request_id arriving after replay has already re-registered the work under a new UUID.
- Process restart where the server somehow delivers stragglers from a prior session on the new connection.
- Server bug.

Exposed via `Client.DiagnosticStats()` so the count is visible from outside.

## Deletions

| Target | Location | Rationale |
|---|---|---|
| `CommandQueue`, `QueuedCommand`, `generateCommandID` | `pkg/game/client_queue.go` | Superseded by request_id correlation. |
| `client_queue_test.go` | | Test of deleted code. |
| `QUEUE.md`, `MIGRATION_EXAMPLE.md` | `pkg/game/` | Documentation of deleted code. |
| `Client.CmdQueue` field, init, dispatch, deprecated wrapper | `client.go:74, 331-335, 1796-1797, 4745-4753` | Field of deleted type. |
| `Client.waiters`, `waiterMu` | `client.go:70-71` | Replaced by `responseRouter.byReqID`. |
| `waitForResponse`, `waitForAuthResponse`, `waitForActionResponse` | `client.go` (~lines 4087-4500; exact range to be re-checked at implementation time) | Replaced by `Submit` + `Result`. |
| `Client.mutationMu` | `client.go:107` | Replaced by per-action lock + soft cap. |
| `Client.execMutation`, `Client.execQuery` | `pkg/game/response_exec.go` | Replaced by `Submit`. |
| `waiters[...]` dispatch leg in `handleResponse` | `client.go:1802` onward | Field deleted. |

## Retained

- `subscribePush` — push fan-out for `OnChatMessage`, `OnStorageUpdate`, pirate/police events.
- `Classifier`, `matchType`, `matchCommand`, `matchAction`, `matchPayloadKey`, `matchChannel`, `matchAll`, `matchAny` — used by push subscribers and the untagged-frame fallback.
- `Terminator`, `terminateOnAction`, `terminateOnTypes` — passed to `Submit` via `WithTerminator`.
- `responseRouter.subs` slice for push handlers.

## Rewrites

- All ~100 blocking command methods (`Mine`, `Travel`, `Dock`, `Buy`, `Sell`, ...) become 3-line wrappers: build msg → `Submit` → `<-handle.Result(ctx)` → return error.
- `Client.Send` becomes private `send`; `Submit` is the only public send path.
- `Login`, `Register`, `Claim` use `Submit` with `WithAckOnly()` and a handshake-specific terminator. Special-cased today via `waitForAuthResponse`.
- Bump `BuiltForAPIVersion` to `v0.296.1` in `pkg/version/checker.go`.
- Update `BuiltForAPIVersion` doc string per existing memory note.

## Additions

| File | Purpose |
|---|---|
| `pkg/game/submit.go` | `Submit`, `RequestHandle`, `Result`, `SubmitOption`, cleanup goroutine. |
| `pkg/game/close_codes.go` | `closeCodePolicy` registry, lookup, default-action logic. |
| `pkg/game/actionlock.go` | Context-aware per-action mutex map. |
| `pkg/game/inflight.go` | Soft-cap semaphore wrapper. |
| `pkg/game/orphan.go` | `orphanStats`, rate-limited WARN logger. |

## Error types (new, in `pkg/game/errors.go` or `submit.go`)

```go
type ConnectionClosed struct {
    Code   websocket.StatusCode
    Reason string
}
func (e *ConnectionClosed) Error() string

type ConnectionLost struct {
    Cause error // last reconnect-attempt error
}
func (e *ConnectionLost) Error() string
func (e *ConnectionLost) Unwrap() error
```

`ServerError` (existing) continues to wrap `code`/`message` from the server error payload.

## Testing

All concurrency-touching tests run with `-race` in CI.

### Router (`byReqID`)

- 1000 concurrent Submits of distinct actions all complete with correct correlation.
- Register/unregister storm: 100 goroutines registering and cancelling while one goroutine dispatches frames; no deadlock; `subCount` model still works.
- Pre-registration: response arrives in the gap between `Submit` registering and `Send` returning — must deliver, not orphan. (Existing router code already addresses this; verify still true.)

### Per-action lock map

- 50 concurrent first-time access of distinct actions — lazy map creation safe under load.
- 100 concurrent `mine` Submits serialize FIFO, no interleaving, all complete; lock acquired/released exactly 100 times.
- Ctx-cancellation while waiting on per-action lock: caller returns `ctx.Err()` and releases its global-cap slot.

### Soft cap

- 64 concurrent Submits with cap=16: invariant — at any moment ≤16 Submits are past the gate (verified via in-test counter).
- Cap leak test: 10,000 Submits with random outcomes (success / error / timeout / cancel / forced close) — `inflight` channel drains to 0 at the end.
- Cancelled `Submit` (ctx fires before acquiring slot) does not consume a slot.

### Handle lifecycle

- `Result()` called twice returns the same value (documented behavior).
- Handle abandoned (caller never calls `Result()`): no goroutine leak, slot released, lock released. Cleanup goroutine owns release.
- Caller's `ctx` in `Result()` expires before terminal arrives: `Result` returns `ctx.Err()`, subscription stays live until terminal/close so the real response is not orphaned.

### Reconnect + replay

- 5 pending mutations of different actions, simulated `close=1000`, all replay with new UUIDs, all resolve. Original handles see no error. Replayed IDs visible in diagnostic logs.
- 5 pending mutations, simulated `close=1011`, all get `ConnectionClosed`, all locks/cap released.
- Reconnect succeeds but auth fails — all pending get `ConnectionLost`.
- `close=1000` arrives mid-`Send` (slot taken, lock taken, frame not yet written): clean release, sensible error to caller.

### Orphans

- Response with unknown request_id is counted, logged WARN, dropped.
- Counter visible via `DiagnosticStats()`.
- Rate-limit: 1000 orphans in 1 sec produce ≤2 log lines.
- After replay, a late response for the pre-replay UUID is orphaned (not delivered to the re-registered handle under the new UUID).

### Close-code registry

- Each known code triggers its declared action.
- Unknown code logs WARN and defaults to fail-fast.
- Documented `code=1000` with undocumented reason logs WARN and still replays.

### Stress / soak

- 60-second mixed workload: 4 goroutines firing random mutations + queries, 1 goroutine forcing simulated close every 5 sec. Verify no deadlock, no leaked slots, no leaked subscriptions; orphan count grows predictably and proportional to forced closes.

### Updates to existing tests

- `client_queue_test.go` → delete.
- `response_router_test.go` → add request_id correlation cases.
- Any test relying on `CmdQueue` / `waiters` / `mutationMu` internals → update or delete.

## Migration order

The implementation plan will sequence this, but rough order:

1. Add `RequestID` to protocol structs.
2. Add `byReqID` to router; dispatch routes by ID first, falls through to existing logic. (Backward compatible — nobody stamps IDs yet.)
3. Add `Submit` + `RequestHandle` + cleanup goroutine. Existing `execMutation` / `execQuery` still work in parallel.
4. Add concurrency primitives (action lock, soft cap, orphan stats).
5. Add close-code registry + replay path.
6. Add error types.
7. Bump `BuiltForAPIVersion`.
8. Rewrite blocking command methods one batch at a time to use `Submit`, removing their `execMutation`/`execQuery` calls. Tests stay green throughout.
9. Once all blocking methods are migrated: delete `execMutation`, `execQuery`, `waitFor*`, `CommandQueue`, `mutationMu`, `waiters`, related docs.
10. `Send` → `send`.

Each step is reviewable independently. Steps 1–6 add new code without changing behavior; step 8 is the long mechanical pass; step 9 is the cleanup.

## Open issues

None at spec time. Plan-time questions are likely to surface around: which agent strategies (`pkg/strategy/`) will benefit from concurrent Submits enough to be rewritten as async vs. left blocking; that decision is per-strategy and out of scope for this spec.
