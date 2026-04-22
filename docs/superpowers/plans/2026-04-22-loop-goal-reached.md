# Loop Goal-Reached Exit Signal — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `*GoalReachedError` sentinel so commands like `mine` /
`refuel` / `repair` / `deposit_all` can signal "goal achieved, stop
cleanly"; the play_as loop executor treats that signal as a positive
exit (🎯 instead of ❌) from the innermost enclosing loop.

**Architecture:** A new typed error in `pkg/game/` classified by a
small `(command, code)` table. A helper `sendAndWaitGoalable` wraps
`Send + waitForActionResponse` for commands that have goal codes, so
they return `*GoalReachedError` when the server's error code matches
the table. The play_as loop executor and `simpleCommand` detect the
sentinel via `errors.As` and branch into positive UI. Waiter internals
change only at the single error-return site inside
`waitForActionResponse` (to emit a structured `*ServerError`), leaving
the rest of the response pipeline untouched.

**Tech Stack:** Go 1.24+, standard library only (no new deps).

---

## Spec reference

`docs/superpowers/specs/2026-04-22-loop-goal-reached-design.md`

## File structure

- **Create** `pkg/game/server_errors.go` — `ServerError`,
  `GoalReachedError`, `goalReachedCodes` table, `maybeGoalReached`
  classifier.
- **Create** `pkg/game/server_errors_test.go` — pure unit tests for
  `maybeGoalReached`.
- **Modify** `pkg/game/client.go` — at the error-return site inside
  `waitForActionResponse`, return a structured `*ServerError` instead
  of `fmt.Errorf("%s", msg)`. Drop the `tank_full` benign case (moves
  to the goal-reached path). Add `sendAndWaitGoalable`. Migrate
  `Mine`, `Refuel`, `Repair` to it. Add post-processing inside
  `DepositAllItems` to convert an `empty_cargo` server error into a
  goal sentinel.
- **Modify** `cmd/tools/play_as/loop_block.go` — in `executeLoop`,
  recognize `*game.GoalReachedError`, print the 🎯 line, return nil
  (innermost exit).
- **Modify** `cmd/tools/play_as/loop_block_test.go` — new tests for the
  goal-reached path, single and nested.
- **Modify** `cmd/tools/play_as/main.go` — `simpleCommand` recognizes
  the sentinel and prints a friendly ✓ instead of ❌.

Everything stays inside existing files aside from the new
`server_errors.go` / `_test.go` pair; file sizes don't grow enough to
warrant a split.

## Out of scope (explicitly)

- MCP client (`pkg/game/mcp_game_client*.go`). Goal-reached classification
  only ships for the WS `*Client`. MCP can adopt the same helper later;
  it will keep returning plain errors until then.
- Adding more (command, code) pairs beyond the initial four — easy
  follow-up; not this PR.
- Any Phase B conditional syntax (`break_if state.x eq y`).

---

## Task 1 — Types, table, classifier (pure unit)

**Files:**
- Create: `pkg/game/server_errors.go`
- Test: `pkg/game/server_errors_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/game/server_errors_test.go`:

```go
package game

import (
	"errors"
	"testing"
)

func TestMaybeGoalReached_MatchesTable(t *testing.T) {
	se := &ServerError{Code: "no_cargo_space", Message: "Cargo hold is full"}
	got := maybeGoalReached("mine", se)
	var goal *GoalReachedError
	if !errors.As(got, &goal) {
		t.Fatalf("expected *GoalReachedError, got %T (%v)", got, got)
	}
	if goal.Command != "mine" || goal.Code != "no_cargo_space" || goal.Message != "Cargo hold is full" {
		t.Errorf("unexpected goal: %+v", goal)
	}
}

func TestMaybeGoalReached_PassesThroughUnmatched(t *testing.T) {
	// Code not in table for this command.
	se := &ServerError{Code: "no_fuel", Message: "Insufficient fuel"}
	got := maybeGoalReached("mine", se)
	var goal *GoalReachedError
	if errors.As(got, &goal) {
		t.Fatalf("did not expect *GoalReachedError, got %+v", goal)
	}
	if got != se {
		t.Errorf("expected original error to be returned, got %v", got)
	}
}

func TestMaybeGoalReached_CommandNotInTable(t *testing.T) {
	se := &ServerError{Code: "no_cargo_space", Message: "Cargo hold is full"}
	got := maybeGoalReached("buy", se) // buy has no goal codes
	var goal *GoalReachedError
	if errors.As(got, &goal) {
		t.Fatalf("buy should not goal-reach on no_cargo_space, got %+v", goal)
	}
}

func TestMaybeGoalReached_NilError(t *testing.T) {
	if got := maybeGoalReached("mine", nil); got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
}

func TestMaybeGoalReached_NonServerError(t *testing.T) {
	plain := errors.New("some transport failure")
	if got := maybeGoalReached("mine", plain); got != plain {
		t.Errorf("non-*ServerError should pass through, got %v", got)
	}
}

func TestServerError_Error(t *testing.T) {
	se := &ServerError{Code: "no_fuel", Message: "Insufficient fuel"}
	if se.Error() != "Insufficient fuel" {
		t.Errorf("Error() = %q, want %q", se.Error(), "Insufficient fuel")
	}
	// Fallback when message is empty: show the code.
	se2 := &ServerError{Code: "x"}
	if se2.Error() != "error: x" {
		t.Errorf("Error() = %q, want %q", se2.Error(), "error: x")
	}
}

func TestGoalReachedError_Error(t *testing.T) {
	g := &GoalReachedError{Command: "mine", Code: "no_cargo_space", Message: "Cargo hold is full"}
	want := "goal reached: no_cargo_space (Cargo hold is full)"
	if g.Error() != want {
		t.Errorf("Error() = %q, want %q", g.Error(), want)
	}
}
```

- [ ] **Step 2: Run tests and confirm they fail**

Run: `go test ./pkg/game/ -run "MaybeGoalReached|ServerError_Error|GoalReachedError_Error" -count=1`

Expected: FAIL — `undefined: ServerError`, `undefined: GoalReachedError`,
`undefined: maybeGoalReached`.

- [ ] **Step 3: Implement the types and classifier**

Create `pkg/game/server_errors.go`:

```go
// Package game — structured server-error types and goal-reached
// classification. See docs/superpowers/specs/2026-04-22-loop-goal-reached-design.md.
package game

import (
	"errors"
	"fmt"
)

// ServerError is a structured error returned by waitForActionResponse
// when the server replies with an `error` or `action_error` message.
// Holding Code separately (rather than only formatting into a string)
// lets callers classify without regexing the message text.
type ServerError struct {
	Code    string
	Message string
}

// Error returns the server's human-readable message, falling back to
// the code when no message was supplied.
func (e *ServerError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "error: " + e.Code
}

// GoalReachedError signals that a command's natural goal has been
// achieved (e.g. cargo hold is full after mine, fuel tank is full
// after refuel). It is distinct from:
//   - nil (success, may be repeated)
//   - a plain error (failure)
//
// The play_as loop executor treats *GoalReachedError as "exit the
// innermost enclosing loop cleanly with overall success". Standalone
// callers treat it as success with a friendly message.
type GoalReachedError struct {
	Command string // the command that reached its goal, e.g. "mine"
	Code    string // server error code, e.g. "no_cargo_space"
	Message string // server's human-readable message
}

func (e *GoalReachedError) Error() string {
	return fmt.Sprintf("goal reached: %s (%s)", e.Code, e.Message)
}

// goalReachedCodes maps command name -> set of server error codes that
// mean "this command's goal is already achieved". Initial set covers
// the most-looped workflows (mining fills cargo, refuel tops the tank,
// repair brings hull to max, deposit_all empties cargo). Extend as
// more commands gain loop-friendly goal semantics.
var goalReachedCodes = map[string]map[string]struct{}{
	"mine":        {"no_cargo_space": {}},
	"refuel":      {"tank_full": {}},
	"repair":      {"no_damage": {}},
	"deposit_all": {"empty_cargo": {}},
}

// maybeGoalReached inspects err and, when command+code matches
// goalReachedCodes, wraps it as *GoalReachedError. Non-matching errors
// and non-*ServerError values pass through unchanged. A nil input
// returns nil.
func maybeGoalReached(command string, err error) error {
	if err == nil {
		return nil
	}
	var se *ServerError
	if !errors.As(err, &se) {
		return err
	}
	codes, ok := goalReachedCodes[command]
	if !ok {
		return err
	}
	if _, match := codes[se.Code]; !match {
		return err
	}
	return &GoalReachedError{
		Command: command,
		Code:    se.Code,
		Message: se.Message,
	}
}
```

- [ ] **Step 4: Run tests and confirm they pass**

Run: `go test ./pkg/game/ -run "MaybeGoalReached|ServerError_Error|GoalReachedError_Error" -count=1`

Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/game/server_errors.go pkg/game/server_errors_test.go
git commit -m "feat(game): add ServerError + GoalReachedError sentinel types

ServerError carries server error code + message separately so callers
can classify without regexing text. GoalReachedError wraps it for
commands whose (command, code) pair means the command's goal is
already achieved — consumers (loop executor, simpleCommand) will
branch to a positive-exit path.

See docs/superpowers/specs/2026-04-22-loop-goal-reached-design.md"
```

---

## Task 2 — Return `*ServerError` from `waitForActionResponse`

**Files:**
- Modify: `pkg/game/client.go`

The plain `fmt.Errorf("%s", msg)` return at the end of the errorChan
case loses the code. Replace with `*ServerError` so later layers can
classify. This is a refactor — no new behavior surfaces yet.

- [ ] **Step 1: Write failing test**

Append to `pkg/game/server_errors_test.go`:

```go
func TestServerError_RoundTripsThroughErrorsAs(t *testing.T) {
	// Producers: callers return *ServerError from waitForActionResponse.
	// Consumers: unwrap via errors.As to read code/message.
	var produced error = &ServerError{Code: "no_fuel", Message: "Insufficient fuel"}

	var se *ServerError
	if !errors.As(produced, &se) {
		t.Fatalf("expected errors.As to unwrap *ServerError from %T", produced)
	}
	if se.Code != "no_fuel" || se.Message != "Insufficient fuel" {
		t.Errorf("unexpected fields: %+v", se)
	}
}
```

This is a sanity test; it also passes before Task 2 (it's pure
*ServerError behavior). It exists to lock in the contract the rest of
the plan depends on.

- [ ] **Step 2: Run the test**

Run: `go test ./pkg/game/ -run TestServerError_RoundTripsThroughErrorsAs -count=1`

Expected: PASS.

- [ ] **Step 3: Modify `waitForActionResponse` to return `*ServerError`**

In `pkg/game/client.go`, locate the error-return site (end of the
`case resp := <-errorChan:` block — the three lines that do
`return fmt.Errorf("%s", msg)` / `return fmt.Errorf("error: %s", code)`
/ `return fmt.Errorf("action failed")`). The current code:

```go
			// Extract error message from payload
			if msg, ok := resp.Payload["message"].(string); ok {
				return fmt.Errorf("%s", msg)
			}
			if code, ok := resp.Payload["code"].(string); ok {
				return fmt.Errorf("error: %s", code)
			}
			return fmt.Errorf("action failed")
```

Replace with:

```go
			// Extract error message and code from the payload and return
			// a structured *ServerError so callers can classify via
			// errors.As (see maybeGoalReached in server_errors.go).
			code, _ := resp.Payload["code"].(string)
			msg, _ := resp.Payload["message"].(string)
			if code == "" && msg == "" {
				return fmt.Errorf("action failed")
			}
			return &ServerError{Code: code, Message: msg}
```

- [ ] **Step 4: Run all game tests**

Run: `go test ./pkg/game/ -count=1`

Expected: PASS. The `*ServerError.Error()` method returns the same
message text that the old `fmt.Errorf("%s", msg)` produced, so any
existing test that string-matches the error continues to work.

- [ ] **Step 5: Drop the `tank_full` benign-case branch**

Still in `pkg/game/client.go`, inside the `case resp := <-errorChan:`
switch, remove the `case "tank_full":` block:

```go
				case "tank_full":
					// Refuel when fuel is already at 100% — nothing to do,
					// fuel is already at the goal state.
					c.debugLogger.Printf("Fuel tank already full (success)")
					return nil
```

Reason: under this design `tank_full` must surface as a goal-reached
signal from `Refuel`, not a silent nil (which would leave a loop
running 19 more pointless refuel iterations). By deleting the branch,
`tank_full` falls through to the general error path and becomes a
`*ServerError{Code:"tank_full"}`; `Refuel`'s wrapper (Task 4) converts
that to `*GoalReachedError`.

- [ ] **Step 6: Run all game tests**

Run: `go test ./pkg/game/ -count=1`

Expected: PASS. If a test specifically asserted that `tank_full`
returns nil, it will fail — update it in the same commit to expect
`errors.As(err, &se) && se.Code == "tank_full"`.

- [ ] **Step 7: Commit**

```bash
git add pkg/game/client.go
git commit -m "refactor(game): return *ServerError from waitForActionResponse

Error-return site now produces structured *ServerError{Code,Message}
instead of fmt.Errorf(message-only). Error() returns the same text so
no caller breaks on stringified comparison.

Drops the tank_full benign-case nil return — callers that want
'fuel-full is success' semantics now get it via the GoalReachedError
path (Refuel migration in the next commit)."
```

---

## Task 3 — Add `sendAndWaitGoalable`, migrate `Mine`

**Files:**
- Modify: `pkg/game/client.go`

- [ ] **Step 1: Write failing test**

Append to `pkg/game/server_errors_test.go`:

```go
func TestMaybeGoalReached_WrapsMineCargoFull(t *testing.T) {
	// Mirrors the exact error shape waitForActionResponse produces,
	// then the shape sendAndWaitGoalable would post-process.
	raw := error(&ServerError{Code: "no_cargo_space", Message: "Cargo hold is full"})

	out := maybeGoalReached("mine", raw)

	var goal *GoalReachedError
	if !errors.As(out, &goal) {
		t.Fatalf("mine + no_cargo_space should become *GoalReachedError, got %T (%v)", out, out)
	}
	if goal.Command != "mine" {
		t.Errorf("goal.Command = %q, want %q", goal.Command, "mine")
	}
	if goal.Code != "no_cargo_space" {
		t.Errorf("goal.Code = %q, want %q", goal.Code, "no_cargo_space")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./pkg/game/ -run TestMaybeGoalReached_WrapsMineCargoFull -count=1`

Expected: PASS (built on Task 1's machinery; no new code needed).

- [ ] **Step 3: Add the `sendAndWaitGoalable` helper**

In `pkg/game/client.go`, add a new method immediately after
`waitForActionResponse` (search for `func (c *Client) waitForActionResponse`
and place the new method below it):

```go
// sendAndWaitGoalable sends msg and waits for its completion. When
// the server replies with an error whose (msg.Type, code) pair is in
// goalReachedCodes, the returned error is *GoalReachedError instead
// of the plain *ServerError — signalling to the caller (typically
// the play_as loop executor) that the command's goal is already
// achieved and the enclosing loop should exit cleanly.
//
// Use this in place of the Send+waitForActionResponse pair for any
// command method that has an entry in goalReachedCodes (e.g. Mine,
// Refuel, Repair).
func (c *Client) sendAndWaitGoalable(ctx context.Context, msg protocol.Message, timeout time.Duration) error {
	if err := c.Send(ctx, msg); err != nil {
		return err
	}
	return maybeGoalReached(msg.Type, c.waitForActionResponse(ctx, timeout))
}
```

- [ ] **Step 4: Migrate `Mine` to use it**

Locate `func (c *Client) Mine` in `pkg/game/client.go` and replace its
body:

```go
func (c *Client) Mine(ctx context.Context) error {
	return c.sendAndWaitGoalable(ctx, protocol.Message{
		Type:      "mine",
		Timestamp: time.Now().UnixMilli(),
	}, SleepActionStartTimeout)
}
```

- [ ] **Step 5: Run all game tests**

Run: `go test ./pkg/game/ -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/client.go
git commit -m "feat(game): add sendAndWaitGoalable + migrate Mine

Mine now returns *GoalReachedError when the server replies with
no_cargo_space (cargo full). Other error codes still return
*ServerError as before."
```

---

## Task 4 — Migrate `Refuel`

**Files:**
- Modify: `pkg/game/client.go`

- [ ] **Step 1: Replace `Refuel` body**

```go
func (c *Client) Refuel(ctx context.Context) error {
	return c.sendAndWaitGoalable(ctx, protocol.Message{
		Type:      "refuel",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
}
```

- [ ] **Step 2: Run all game tests**

Run: `go test ./pkg/game/ -count=1`

Expected: PASS. If any test previously relied on `Refuel` returning
nil when the tank was already full, update it to assert
`errors.As(err, &goal) && goal.Code == "tank_full"` — both conditions
(`nil` old behavior vs `*GoalReachedError` new behavior) indicate
"goal achieved," so the fix is only in the test's expected shape.

- [ ] **Step 3: Commit**

```bash
git add pkg/game/client.go
git commit -m "feat(game): migrate Refuel to sendAndWaitGoalable

Refuel now returns *GoalReachedError when the server replies with
tank_full. Replaces the old behavior where tank_full was silently
collapsed to nil inside waitForActionResponse — that shape was
indistinguishable from a successful refuel and gave loops no signal
to stop."
```

---

## Task 5 — Migrate `Repair`

**Files:**
- Modify: `pkg/game/client.go`

- [ ] **Step 1: Replace `Repair` body**

```go
func (c *Client) Repair(ctx context.Context) error {
	return c.sendAndWaitGoalable(ctx, protocol.Message{
		Type:      "repair",
		Timestamp: time.Now().UnixMilli(),
	}, SleepTick)
}
```

- [ ] **Step 2: Run all game tests**

Run: `go test ./pkg/game/ -count=1`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add pkg/game/client.go
git commit -m "feat(game): migrate Repair to sendAndWaitGoalable

Repair now returns *GoalReachedError when the server replies with
no_damage. If the server uses a different code for 'hull already at
max,' update goalReachedCodes[\"repair\"] in server_errors.go."
```

---

## Task 6 — Wire `DepositAllItems` to the sentinel

**Files:**
- Modify: `pkg/game/client.go`

`DepositAllItems` is not a single `c.Send(...); return
c.waitForActionResponse(...)` pair — it iterates cargo and calls
`DepositItems` per item. Two paths hit the goal:

1. Starting state: cargo already empty. Today we print
   `"📦 Cargo is empty, nothing to deposit"` and return nil, which makes
   the loop continue pointlessly.
2. Mid-flight: the server starts returning `empty_cargo` on further
   deposit calls. Today that would surface as a regular error.

Both should become `*GoalReachedError`.

- [ ] **Step 1: Handle path 1 — start empty**

Inside `DepositAllItems`, replace:

```go
	if len(state.Ship.Cargo) == 0 {
		fmt.Printf("📦 Cargo is empty, nothing to deposit\n")
		return nil // Nothing to deposit
	}
```

with:

```go
	if len(state.Ship.Cargo) == 0 {
		fmt.Printf("📦 Cargo is empty, nothing to deposit\n")
		return &GoalReachedError{
			Command: "deposit_all",
			Code:    "empty_cargo",
			Message: "Cargo is already empty",
		}
	}
```

- [ ] **Step 2: Handle path 2 — empty mid-iteration**

Still inside `DepositAllItems`, locate the loop that calls
`c.DepositItems` and its per-item error branch. After the existing
`action_pending` retry check, detect an `empty_cargo` server error and
break with the sentinel:

Find:

```go
		if err := c.DepositItems(ctx, item.ItemID, currentQty); err != nil {
			fmt.Printf("   [%d/%d] ✗ Failed to deposit %.0f x %s: %v\n", i+1, len(state.Ship.Cargo), currentQty, item.ItemID, err)
			c.debugLogger.Printf("Failed to deposit %s: %v", item.ItemID, err)
			depositErrors++
			// If action is pending, wait longer before next item
			if strings.Contains(err.Error(), "action_pending") || strings.Contains(err.Error(), "already pending") {
				...
			}
			// Continue depositing other items even if one fails
		} else {
```

and add the goal check at the top of the error branch (before
`depositErrors++`):

```go
		if err := c.DepositItems(ctx, item.ItemID, currentQty); err != nil {
			var se *ServerError
			if errors.As(err, &se) && se.Code == "empty_cargo" {
				fmt.Printf("📦 Cargo now empty after %d deposit(s)\n", successfulDeposits)
				return &GoalReachedError{
					Command: "deposit_all",
					Code:    "empty_cargo",
					Message: "Cargo is empty",
				}
			}
			fmt.Printf("   [%d/%d] ✗ Failed to deposit %.0f x %s: %v\n", ...)
			...
```

Add `"errors"` to the import block of `pkg/game/client.go` — it is
not currently imported, and both `server_errors.go` (Task 1) and the
new `errors.As` call above need it. Place it alphabetically among
the stdlib imports.

- [ ] **Step 3: Run all game tests**

Run: `go test ./pkg/game/ -count=1`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/game/client.go
git commit -m "feat(game): DepositAllItems returns GoalReachedError when cargo empty

Both the start-state empty-cargo case and mid-iteration empty_cargo
server error now return *GoalReachedError{Command: deposit_all,
Code: empty_cargo}. Replaces the previous silent nil return that
hid the goal from an enclosing loop."
```

---

## Task 7 — Loop executor recognizes the sentinel

**Files:**
- Modify: `cmd/tools/play_as/loop_block.go`
- Modify: `cmd/tools/play_as/loop_block_test.go`

- [ ] **Step 1: Write failing tests**

Append to `cmd/tools/play_as/loop_block_test.go`:

```go
func TestExecuteLoop_GoalReachedExitsInnermost(t *testing.T) {
	// Body is a single "mine" statement. The dispatcher returns nil
	// for the first 4 calls, then *GoalReachedError on the 5th —
	// simulating cargo filling on iteration 5 of 20.
	body := mustParseStmts(t, "mine")
	script := []error{nil, nil, nil, nil, &game.GoalReachedError{
		Command: "mine",
		Code:    "no_cargo_space",
		Message: "Cargo hold is full",
	}}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 20, false, body, 0, dispatch)
	if err != nil {
		t.Fatalf("goal-reached should exit cleanly (nil), got %v", err)
	}
	// 5 calls total: 4 nil successes + the 1 goal-reached signal.
	if len(*calls) != 5 {
		t.Errorf("expected 5 calls (loop exits on goal-reached), got %d", len(*calls))
	}
}

func TestExecuteLoop_GoalReachedExitsInnerLoopOuterContinues(t *testing.T) {
	// Outer loop has 2 iterations; each runs: travel, inner loop of
	// up to 40 mine, dock. Inner mine goal-reaches on iter 3 of the
	// FIRST outer iteration AND iter 3 of the SECOND — so each outer
	// iteration produces 1 travel + 3 mine + 1 dock = 5 calls. Two
	// outer iterations = 10 calls. If the sentinel bled out to the
	// outer loop we'd see fewer than 10.
	body := mustParseStmts(t, "travel sol_belt; loop 40 mine; dock")
	goal := &game.GoalReachedError{Command: "mine", Code: "no_cargo_space", Message: "Cargo hold is full"}
	script := []error{
		// outer iter 1: travel ok; mine nil, nil, goal; dock ok
		nil, nil, nil, goal, nil,
		// outer iter 2: travel ok; mine nil, nil, goal; dock ok
		nil, nil, nil, goal, nil,
	}
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 2, false, body, 0, dispatch)
	if err != nil {
		t.Fatalf("outer should succeed after inner goal-exits, got %v", err)
	}
	if len(*calls) != 10 {
		t.Errorf("expected 10 calls (2×(travel+3mine+dock)), got %d", len(*calls))
	}
	want := []string{"travel", "mine", "mine", "mine", "dock", "travel", "mine", "mine", "mine", "dock"}
	for i, w := range want {
		if (*calls)[i][0] != w {
			t.Errorf("call %d: got %q, want %q", i, (*calls)[i][0], w)
		}
	}
}

func TestExecuteLoop_GoalReachedIgnoresForceFlag(t *testing.T) {
	// -f only tolerates errors; goal-reached is a success and still
	// exits the innermost loop. `loop -f 20 mine` should stop on the
	// first goal-reached, not power through.
	body := mustParseStmts(t, "mine")
	goal := &game.GoalReachedError{Command: "mine", Code: "no_cargo_space", Message: "Cargo hold is full"}
	script := []error{nil, goal} // goal-reaches on iter 2
	dispatch, calls := recordingDispatcher(script)
	err := executeLoop(context.Background(), io.Discard, 20, true /* force */, body, 0, dispatch)
	if err != nil {
		t.Fatalf("goal-reached under -f should exit cleanly, got %v", err)
	}
	if len(*calls) != 2 {
		t.Errorf("expected 2 calls (loop exits on goal-reached even with -f), got %d", len(*calls))
	}
}
```

Make sure `loop_block_test.go` imports `github.com/rsned/spacemolt/pkg/game`.

- [ ] **Step 2: Run tests and confirm they fail**

Run: `go test ./cmd/tools/play_as/ -run "TestExecuteLoop_GoalReached" -count=1`

Expected: FAIL — each new test fails either at compile (import) or
runtime (`goal-reached should exit cleanly, got goal reached: ...`),
because `executeLoop` currently treats `*GoalReachedError` as a
regular error.

- [ ] **Step 3: Update `executeLoop` to branch on the sentinel**

In `cmd/tools/play_as/loop_block.go`, find the error branch inside
`executeLoop`:

```go
			if err != nil {
				errCount++
				fmt.Fprintf(out, "%s❌ %v\n", indent, err)               //nolint:errcheck
				if !force {
					fmt.Fprintf(out, "%sStopping loop after %d/%d iterations\n", indent, i+1, count) //nolint:errcheck
					return err
				}
				if firstErr == nil {
					firstErr = err
				}
				// Inner loop failures abort the remaining statements in this
				// outer iteration; plain statement failures continue to the
				// next statement within the same iteration.
				if isLoop {
					iterFailed = true
					break
				}
			}
```

Insert a goal-reached check at the top of the branch (before
`errCount++`):

```go
			if err != nil {
				// A *game.GoalReachedError signals "this command's goal is
				// already achieved." Treat it as a positive exit from the
				// innermost enclosing loop: print a 🎯 line and return nil.
				// -f is intentionally ignored — -f tolerates errors, not
				// successes, and re-running a satisfied command is pointless.
				var goal *game.GoalReachedError
				if errors.As(err, &goal) {
					fmt.Fprintf(out, "%s🎯 goal reached: %s → exiting loop\n", indent, goal.Message) //nolint:errcheck
					return nil
				}
				errCount++
				fmt.Fprintf(out, "%s❌ %v\n", indent, err)               //nolint:errcheck
				...
```

Update the import block at the top of
`cmd/tools/play_as/loop_block.go` to add both `"errors"` and
`github.com/rsned/spacemolt/pkg/game` — neither is currently imported:

```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/peterh/liner"
	"github.com/rsned/spacemolt/pkg/game"
)
```

- [ ] **Step 4: Run tests and confirm they pass**

Run: `go test ./cmd/tools/play_as/ -run "TestExecuteLoop" -count=1`

Expected: PASS for both the new tests and the pre-existing
`TestExecuteLoop_*` cases (which still exercise the non-goal error
paths unchanged).

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/loop_block.go cmd/tools/play_as/loop_block_test.go
git commit -m "feat(play_as): loop executor exits cleanly on GoalReachedError

*game.GoalReachedError now triggers a 🎯 positive exit from the
innermost enclosing loop. Outer loops continue with the next
statement of their current iteration. -f intentionally does not
override goal-reached — -f tolerates errors, not successes."
```

---

## Task 8 — `simpleCommand` handles the sentinel

**Files:**
- Modify: `cmd/tools/play_as/main.go`
- Create: `cmd/tools/play_as/simple_command_test.go`

- [ ] **Step 1: Write failing test**

Create `cmd/tools/play_as/simple_command_test.go`:

```go
package main

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// stubGameClientForSimple satisfies the tiny slice of game.GameClient
// that simpleCommand actually touches: GetRawJSON (for the raw-payload
// lookup paths) only. Everything else panics if invoked — those paths
// are out of scope for this test.
type stubGameClientForSimple struct {
	game.GameClient
}

func (stubGameClientForSimple) GetRawJSON(string) []byte { return nil }

func TestSimpleCommand_GoalReachedReturnsNil(t *testing.T) {
	client := stubGameClientForSimple{}
	fn := func(context.Context) error {
		return &game.GoalReachedError{
			Command: "mine",
			Code:    "no_cargo_space",
			Message: "Cargo hold is full",
		}
	}
	err := simpleCommand(client, fn, context.Background(), 0, "mine", formatRaw)
	if err != nil {
		t.Fatalf("simpleCommand should return nil on GoalReachedError, got %v", err)
	}
}

func TestSimpleCommand_PassesThroughRegularErrors(t *testing.T) {
	client := stubGameClientForSimple{}
	want := errors.New("boom")
	fn := func(context.Context) error { return want }
	err := simpleCommand(client, fn, context.Background(), 0, "mine", formatRaw)
	if err != want {
		t.Errorf("simpleCommand should pass regular errors through, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests and confirm they fail**

Run: `go test ./cmd/tools/play_as/ -run TestSimpleCommand -count=1`

Expected: FAIL — the goal-reached case currently returns the error
instead of nil.

- [ ] **Step 3: Update `simpleCommand`**

Find `simpleCommand` in `cmd/tools/play_as/main.go`:

```go
func simpleCommand(client game.GameClient, fn func(context.Context) error, ctx context.Context, wait time.Duration, command string, format outputFormat) error {
	if err := fn(ctx); err != nil {
		// Even on error, show the server's response for debugging/JSON mode
		// The response contains: action, code, message, command, tick
		if raw := lookupRawJSON(client, command); len(raw) > 0 {
			printResponse(raw, format, command)
		}
		return err
	}
	showLastResponse(client, format, command)
	if wait > 0 {
		time.Sleep(wait)
	}
	return nil
}
```

Insert a goal-reached branch:

```go
func simpleCommand(client game.GameClient, fn func(context.Context) error, ctx context.Context, wait time.Duration, command string, format outputFormat) error {
	if err := fn(ctx); err != nil {
		// A *game.GoalReachedError means the command's goal is already
		// satisfied (e.g. mine while cargo is full, refuel at 100%).
		// In the standalone REPL case, print it as a ✓ rather than ❌.
		var goal *game.GoalReachedError
		if errors.As(err, &goal) {
			fmt.Printf("✓ goal reached: %s\n", goal.Message)
			return nil
		}
		if raw := lookupRawJSON(client, command); len(raw) > 0 {
			printResponse(raw, format, command)
		}
		return err
	}
	showLastResponse(client, format, command)
	if wait > 0 {
		time.Sleep(wait)
	}
	return nil
}
```

Add `"errors"` to the import block of `cmd/tools/play_as/main.go` —
not currently imported. Place alphabetically among the stdlib
imports.

- [ ] **Step 4: Run tests and confirm they pass**

Run: `go test ./cmd/tools/play_as/ -run TestSimpleCommand -count=1`

Expected: PASS.

- [ ] **Step 5: Build**

Run: `go build ./...`

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/main.go cmd/tools/play_as/simple_command_test.go
git commit -m "feat(play_as): standalone commands print ✓ on GoalReachedError

A bare 'mine' when the cargo hold is already full, or 'refuel' at a
full tank, now prints ✓ goal reached: <message> and returns nil
instead of ❌ error. Two unit tests pin the return-nil contract and
pass-through for regular errors."
```

---

## Task 9 — Final verification

- [ ] **Step 1: Build everything**

Run: `go build ./...`

Expected: clean.

- [ ] **Step 2: Full test pass**

Run: `go test ./... -count=1`

Expected: all PASS. No skipped, no failures.

- [ ] **Step 3: Lint**

Run: `golangci-lint run ./...`

Expected: `0 issues.`

- [ ] **Step 4: Manual REPL smoke (human loop, optional)**

Start play_as against a live connection with a ship whose fuel tank is
full:

```bash
go run ./cmd/tools/play_as <agent-id>
```

Then in the REPL:

```
refuel
```

Expected terminal output:
```
▶ Executing: refuel 
✓ goal reached: Fuel tank is already full (200/200). No refueling needed.
```

Exit cleanly. Next, with full fuel but cargo space, try:

```
loop 20 mine
```

Expected (abridged): several ✓ iterations showing mined quantities,
then:

```
🎯 goal reached: Cargo hold is full → exiting loop
```

and control returns to the prompt — no ❌, no "Stopping loop after
N/20 iterations" suffix.

- [ ] **Step 5: Push**

```bash
git push
```

Expected: one push containing commits from Tasks 1–8.

---

## Appendix — troubleshooting

- **A server code doesn't match the table.** `goalReachedCodes` uses
  guessed codes for `repair` (`no_damage`) and `deposit_all`
  (`empty_cargo`). If the real server code differs, the sentinel
  never fires and behavior degrades to today's (regular error →
  loop stops with ❌). Fix: update the map entry in `pkg/game/server_errors.go`.

- **`Refuel` test fails because it expected `nil` for tank_full.**
  The old `tank_full` benign case returned nil; the new shape returns
  `*GoalReachedError`. Update the test to:
  ```go
  var goal *game.GoalReachedError
  if !errors.As(err, &goal) || goal.Code != "tank_full" {
      t.Fatalf("want tank_full GoalReachedError, got %v", err)
  }
  ```

- **`action_pending` retry path still uses string matching.** The
  existing `strings.Contains(err.Error(), "action_pending")` in
  `DepositAllItems` continues to work because `*ServerError.Error()`
  includes the code in its fallback. Leave it alone; string matching
  is fine for that path and not worth touching in this PR.
