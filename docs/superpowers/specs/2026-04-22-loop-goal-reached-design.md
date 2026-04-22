# Loop Resilience — Goal-Reached Exit Signal

**Date:** 2026-04-22
**Status:** Design approved; implementation pending.
**Scope:** Phase A of the broader loop-resilience work. Phase B
(conditional control flow — `break_if state.x eq y`) is deferred to a
separate design once A ships.

## Motivation

Loops that repeat variable-yield actions have no natural stop condition
today. Mining is the clearest case: a single `mine` yields anywhere
from a few to a few dozen units depending on richness, skill level, and
resource unit size (1/2/3 cargo units each). The user can't predict how
many iterations will fill the hold, so they pick:

- **A count that's too low** → stops with empty space remaining.
- **`loop -f 100 mine`** → fills the hold, then burns ticks force-mining
  into a full cargo hold until the count runs out.

Neither matches intent. The intent is "mine until cargo is full, then
stop cleanly." The server already tells us when the goal is reached —
it returns `no_cargo_space` once the hold fills — but we treat that as
an error (❌) and abort the loop as a failure.

Similar patterns apply to `refuel` (done when `tank_full`), `repair`
(done at full hull), `deposit_all` (done when cargo is empty), and
salvage/loot (cargo full).

## Non-Goals

- General conditional control flow (`break_if`, `while`, `until`) — that
  is Phase B, out of scope here.
- Wiring every command with a goal-code mapping. Start with four; extend
  as people hit new cases.
- Changing `-f` semantics except the narrow interaction spelled out
  below.

## Design

### The signal — `GoalReachedError`

A new typed error lives in `pkg/game/`:

```go
// GoalReachedError signals that a command's natural goal has been
// achieved (e.g. cargo hold is full after mine, fuel tank is full
// after refuel). It is distinct from:
//   - nil (success, may be repeated)
//   - a plain error (failure)
// Callers — notably the play_as loop executor — treat it as
// "exit the innermost loop cleanly with overall success."
type GoalReachedError struct {
    Command string // the command that reached its goal, e.g. "mine"
    Code    string // server error code, e.g. "no_cargo_space"
    Message string // server's human-readable message
}

func (e *GoalReachedError) Error() string {
    return fmt.Sprintf("goal reached: %s (%s)", e.Code, e.Message)
}
```

Callers distinguish via `errors.As(err, &goal)`.

### The (command, code) classification table

Single source of truth in `pkg/game/`:

```go
var goalReachedCodes = map[string]map[string]struct{}{
    "mine":        {"no_cargo_space": {}},
    "refuel":      {"tank_full": {}},
    "repair":      {"no_damage": {}},
    "deposit_all": {"empty_cargo": {}},
}
```

(Salvage, loot_wreck, and others can be added in follow-ups.)

Anything not in the table stays a regular error and propagates exactly
as today. `tank_full` is currently in the benign-codes list that returns
plain `nil`; it moves to this table instead, so `refuel` now exits a
loop on fuel-full rather than silently succeeding.

### Where the classification happens — `sendAndWaitGoalable`

The classification requires knowing both the command and the error code.
`waitForActionResponse` today only sees the response, not the request,
so it cannot classify on its own. Adding a command-aware helper keeps
the change minimal:

```go
// sendAndWaitGoalable sends a mutation command and waits for its
// completion. If the server returns an error whose code is in
// goalReachedCodes[command], the returned error is *GoalReachedError
// instead of a plain error. Use this from command methods whose
// "already at goal" failure codes are meaningful as loop exit signals.
func (c *Client) sendAndWaitGoalable(ctx context.Context, msg protocol.Message, timeout time.Duration) error {
    if err := c.Send(ctx, msg); err != nil {
        return err
    }
    err := c.waitForActionResponse(ctx, timeout)
    if err == nil {
        return nil
    }
    return maybeGoalReached(msg.Type, err)
}
```

`maybeGoalReached` inspects the error's embedded code (already carried
in error messages produced by `waitForActionResponse`; add a structured
`ServerError` type if string-sniffing becomes fragile) and returns a
`*GoalReachedError` when `(msg.Type, code)` matches the table.

To preserve structure cleanly, `waitForActionResponse` returns a
lightweight `*ServerError{Code, Message}` for server-driven errors
(replacing the current `fmt.Errorf("%s", msg)` shape at a single site).
`maybeGoalReached` wraps that.

### Command methods that opt in

Four methods are retargeted to use `sendAndWaitGoalable`:

- `(*Client).Mine`
- `(*Client).Refuel`
- `(*Client).Repair`
- `(*Client).DepositAllItems` — already returns early on empty cargo;
  wire the remaining deposit-items error path so an explicit
  `empty_cargo` response also becomes `GoalReachedError`.

All other commands keep the existing `c.Send(...); return c.waitForActionResponse(...)` shape.

### Loop executor — `executeLoop`

In `cmd/tools/play_as/loop_block.go`:

```go
if err != nil {
    var goal *game.GoalReachedError
    if errors.As(err, &goal) {
        fmt.Fprintf(out, "%s🎯 goal reached: %s → exiting loop\n", indent, goal.Message)
        return nil // exit innermost loop cleanly
    }
    // existing error path...
}
```

Key semantics:

- **Innermost only.** Goal-reached exits the currently executing loop.
  Outer loops continue with the next statement. This matches the common
  case `loop 20 { ... loop 40 mine ... }` where inner mining should stop
  and the outer workflow proceeds.
- **`-f` interaction.** `-f` tolerates errors; it does not force past
  successes. Goal-reached always exits the innermost loop, including
  under `-f`. Users who want literal N-iterations-regardless can't do
  that today with this design; YAGNI — add if ever asked for.

### `simpleCommand` — standalone UX

In `cmd/tools/play_as/main.go`, `simpleCommand` checks for the sentinel:

```go
if err := fn(ctx); err != nil {
    var goal *game.GoalReachedError
    if errors.As(err, &goal) {
        fmt.Printf("✓ goal reached: %s\n", goal.Message)
        return nil
    }
    // existing error path...
}
```

A bare `mine` when cargo is already full now prints `✓ goal reached:
Cargo hold is full` instead of a scary ❌.

## Tests

- **`loop_block_test.go`**: mock `runStatement` returns
  `*GoalReachedError` on iteration N. Assert the loop prints the 🎯
  line and returns without error, with N iterations of output and the
  remaining `count - N` iterations skipped.
- **`loop_block_test.go`**: nested case — outer loop's current
  iteration runs an inner loop that goal-exits on iter 3 of 40. Assert
  the outer loop proceeds to the next statement of its current
  iteration (not the next outer iteration), and subsequent outer
  iterations still run the inner loop from scratch.
- **`pkg/game/client_errors_test.go`** (new or folded in):
  `maybeGoalReached` returns `*GoalReachedError` for matched pairs and
  passes through unrelated errors unchanged.
- **play_as `simpleCommand` regression**: calling a fn that returns
  `*GoalReachedError` prints ✓ and returns nil.

## Risks / follow-ups

- **Exact server codes.** `no_damage` for repair-at-full-hull is a
  placeholder — confirm against live server before shipping. Same for
  `empty_cargo` on `deposit_all`. Wrong codes just means the sentinel
  never fires, which degrades to current behavior (regular error).
- **Migrating the existing `tank_full` benign path.** Today `tank_full`
  returns `nil` from `waitForActionResponse`. Under this design it
  needs to bubble up as `*GoalReachedError` from `Refuel`. Keep the
  benign-codes list intact but special-case `tank_full` to fall
  through to the goal-check path instead of returning nil.
- **Phase B dependencies.** A state-predicate break (`break_if
  state.cargo.remaining lt 50`) is orthogonal to this design and does
  not depend on it; they compose — a user could write `loop 40 { mine;
  break_if state.cargo.remaining lt 10 }` on top of whatever goal-code
  handling this phase ships.
