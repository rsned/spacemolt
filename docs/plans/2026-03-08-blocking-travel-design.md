# Blocking Travel Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Convert `Travel()` from fire-and-hope to a fully blocking call that returns only when the ship has arrived (or failed), so callers never need to manage wait logic themselves.

**Architecture:** `Travel()` sends the travel message, waits for the initial server acknowledgment, then polls `state.Traveling` until the server marks arrival. A new `waitForStateChange()` helper encapsulates the polling pattern for reuse by future command conversions (jump, craft, mine, etc.). Return type changes from `error` to `(*TravelResult, error)`.

**Tech Stack:** Go, internal WebSocket client, existing state synchronization via `handleResponse()`

---

### Task 1: Add `TravelResult` type and `waitForStateChange` helper

**Files:**
- Modify: `pkg/game/types.go` (add `TravelResult` after `TravelProgress` struct, ~line 259)
- Modify: `pkg/game/client.go` (add `waitForStateChange` helper, after `waitForActionResponse` ~line 3090)

**Step 1: Add `TravelResult` to `types.go`**

Insert after the `TravelProgress` struct (line 259):

```go
// TravelResult contains the outcome of a completed Travel() call.
type TravelResult struct {
	POI      string // Final POI ID arrived at
	POIName  string // Human-readable POI name (if available)
	Canceled bool   // True if travel was interrupted (e.g., combat)
}
```

**Step 2: Add `waitForStateChange` to `client.go`**

Insert after `waitForActionResponse` (after ~line 3090):

```go
// waitForStateChange polls the client state until check returns true.
// It returns nil on success, or an error on timeout/context cancellation.
func (c *Client) waitForStateChange(ctx context.Context, check func(*State) bool, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for state change after %v", timeout)
		case <-ticker.C:
			if check(c.GetState()) {
				return nil
			}
		}
	}
}
```

**Step 3: Verify it compiles**

Run: `go build ./pkg/game/...`
Expected: success (new code is unused but compiles)

**Step 4: Commit**

```bash
git add pkg/game/types.go pkg/game/client.go
git commit -m "feat: add TravelResult type and waitForStateChange helper"
```

---

### Task 2: Write test for blocking Travel

**Files:**
- Modify: `pkg/game/client_test.go` (add test)

**Step 1: Write the test**

Append to `pkg/game/client_test.go`:

```go
// TestTravel_BlocksUntilArrived verifies that Travel() blocks until
// state.Traveling becomes false and returns the arrived POI.
func TestTravel_BlocksUntilArrived(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	// Override send to capture the message without a real WebSocket
	var sentMsg protocol.Message
	client.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		sentMsg = msg
		return nil
	}

	targetPOI := "poi_asteroid_belt_1"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simulate the server response flow in a goroutine:
	// 1. Initial OK with action:"travel" and arrival_tick (travel accepted)
	// 2. State update setting Traveling=false and CurrentPOI (arrived)
	go func() {
		// Wait for Travel() to send the message
		time.Sleep(100 * time.Millisecond)

		// Simulate initial OK response — sets Traveling=true
		client.mu.Lock()
		client.state.Traveling = true
		client.mu.Unlock()

		// Deliver the OK to the waiter
		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeOK]; ok {
			ch <- protocol.Response{
				Type: protocol.TypeOK,
				Payload: map[string]any{
					"action":       "travel",
					"arrival_tick": float64(5),
				},
			}
		}
		client.waiterMu.Unlock()

		// Simulate arrival after a short delay
		time.Sleep(300 * time.Millisecond)
		client.mu.Lock()
		client.state.Traveling = false
		client.state.CurrentPOI = targetPOI
		client.mu.Unlock()
	}()

	result, err := client.Travel(ctx, targetPOI)
	if err != nil {
		t.Fatalf("Travel() returned error: %v", err)
	}

	if sentMsg.Type != "travel" {
		t.Errorf("expected sent message type 'travel', got %q", sentMsg.Type)
	}

	if result.POI != targetPOI {
		t.Errorf("expected POI %q, got %q", targetPOI, result.POI)
	}
	if result.Canceled {
		t.Error("expected Canceled=false")
	}
}

// TestTravel_TimeoutReturnsError verifies Travel() returns an error
// if the state never transitions.
func TestTravel_TimeoutReturnsError(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)

	client.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Simulate server accepting travel but never arriving
	go func() {
		time.Sleep(100 * time.Millisecond)
		client.mu.Lock()
		client.state.Traveling = true
		client.mu.Unlock()

		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeOK]; ok {
			ch <- protocol.Response{
				Type: protocol.TypeOK,
				Payload: map[string]any{
					"action":       "travel",
					"arrival_tick": float64(1),
				},
			}
		}
		client.waiterMu.Unlock()
		// Never set Traveling=false — should timeout
	}()

	_, err := client.Travel(ctx, "poi_nowhere")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestTravel_AlreadyAtDestination verifies Travel() returns immediately
// when server says already_there.
func TestTravel_AlreadyAtDestination(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.state.CurrentPOI = "poi_station_1"

	client.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(100 * time.Millisecond)
		client.waiterMu.Lock()
		if ch, ok := client.waiters[protocol.TypeError]; ok {
			ch <- protocol.Response{
				Type: protocol.TypeError,
				Payload: map[string]any{
					"code": "already_there",
				},
			}
		}
		client.waiterMu.Unlock()
	}()

	result, err := client.Travel(ctx, "poi_station_1")
	if err != nil {
		t.Fatalf("Travel() returned error: %v", err)
	}
	if result.POI != "poi_station_1" {
		t.Errorf("expected POI 'poi_station_1', got %q", result.POI)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestTravel_ -v`
Expected: FAIL — `Travel()` still returns `error`, not `(*TravelResult, error)`

**Step 3: Commit the failing test**

```bash
git add pkg/game/client_test.go
git commit -m "test: add failing tests for blocking Travel()"
```

---

### Task 3: Implement blocking `Travel()`

**Files:**
- Modify: `pkg/game/client.go:640-649` (rewrite `Travel` method)

**Step 1: Replace the `Travel` method**

Replace lines 640-649 with:

```go
// Travel travels to a POI within the current system.
// It blocks until the ship arrives at the destination or an error occurs.
// The returned TravelResult contains the final POI the ship ended up at.
func (c *Client) Travel(ctx context.Context, targetPOI string) (*TravelResult, error) {
	if err := c.Send(ctx, protocol.Message{
		Type:      "travel",
		Payload:   map[string]any{"target_poi": targetPOI},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return nil, err
	}

	// Wait for initial server acknowledgment (OK or error).
	resp, err := c.waitForInitialResponse(ctx, SleepTick)
	if err != nil {
		// Check for "already_there" which is a benign success.
		return nil, err
	}

	// Handle "already_there" — returned as an error response with code.
	if resp.Type == protocol.TypeError {
		if code, _ := resp.Payload["code"].(string); code == "already_there" {
			state := c.GetState()
			return &TravelResult{POI: state.CurrentPOI}, nil
		}
	}

	// Compute timeout from arrival_tick if available, else use generous default.
	timeout := 90 * time.Second
	if arrivalTick, ok := resp.Payload["arrival_tick"].(float64); ok {
		currentTick := c.GetState().CurrentTick
		ticksRemaining := int64(arrivalTick) - currentTick
		if ticksRemaining < 1 {
			ticksRemaining = 1
		}
		// Each tick ~10s, plus 30s buffer for safety.
		timeout = time.Duration(ticksRemaining)*SleepTick + 30*time.Second
	}

	c.debugLogger.Printf("Travel to %s: waiting up to %v for arrival", targetPOI, timeout)

	// Block until state.Traveling becomes false (arrival or interruption).
	if err := c.waitForStateChange(ctx, func(s *State) bool {
		return !s.Traveling
	}, timeout); err != nil {
		return &TravelResult{Canceled: true}, fmt.Errorf("travel to %s: %w", targetPOI, err)
	}

	state := c.GetState()
	return &TravelResult{
		POI:      state.CurrentPOI,
		Canceled: false,
	}, nil
}
```

**Step 2: Add `waitForInitialResponse` helper**

This is a simplified version of `waitForActionResponse` that only waits for the first OK or error — no multi-step looping. Add it near `waitForActionResponse` (~line 3090):

```go
// waitForInitialResponse waits for the first OK or error response from the server.
// Unlike waitForActionResponse, it does NOT loop on pending/in-progress — it returns
// the first response and lets the caller decide what to do.
func (c *Client) waitForInitialResponse(ctx context.Context, timeout time.Duration) (protocol.Response, error) {
	okChan := make(chan protocol.Response, 1)
	errorChan := make(chan protocol.Response, 1)
	actionErrorChan := make(chan protocol.Response, 1)

	c.waiterMu.Lock()
	c.waiters[protocol.TypeOK] = okChan
	c.waiters[protocol.TypeError] = errorChan
	c.waiters[protocol.TypeActionError] = actionErrorChan
	c.waiterMu.Unlock()

	defer func() {
		c.waiterMu.Lock()
		delete(c.waiters, protocol.TypeOK)
		delete(c.waiters, protocol.TypeError)
		delete(c.waiters, protocol.TypeActionError)
		c.waiterMu.Unlock()
	}()

	deadline := time.After(timeout)

	for {
		select {
		case resp := <-okChan:
			// If pending, keep waiting for the real initial response.
			if pending, ok := resp.Payload["pending"].(bool); ok && pending {
				c.debugLogger.Printf("Action pending — waiting for server to start")
				deadline = time.After(timeout)
				continue
			}
			return resp, nil

		case resp := <-errorChan:
			if code, ok := resp.Payload["code"].(string); ok {
				switch code {
				case "already_there", "already_docked", "not_docked":
					return resp, nil // Benign — caller handles these
				case "action_pending":
					deadline = time.After(timeout)
					continue
				}
			}
			msg, _ := resp.Payload["message"].(string)
			if msg == "" {
				msg = "server error"
			}
			return resp, fmt.Errorf("%s", msg)

		case resp := <-actionErrorChan:
			msg, _ := resp.Payload["message"].(string)
			if msg == "" {
				msg = "action error"
			}
			return resp, fmt.Errorf("%s", msg)

		case <-deadline:
			return protocol.Response{}, fmt.Errorf("timeout waiting for initial response")

		case <-ctx.Done():
			return protocol.Response{}, ctx.Err()
		}
	}
}
```

**Step 3: Add `sendOverride` field to Client for testing**

In `client.go`, add a field to the `Client` struct (~line 22-50) and wire it into `Send`:

Add to the Client struct fields:
```go
sendOverride func(ctx context.Context, msg protocol.Message) error // Test hook
```

In the existing `Send` method, add at the top:
```go
if c.sendOverride != nil {
    return c.sendOverride(ctx, msg)
}
```

**Step 4: Run the tests**

Run: `go test ./pkg/game/ -run TestTravel_ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/game/client.go pkg/game/types.go
git commit -m "feat: Travel() now blocks until arrival using state polling"
```

---

### Task 4: Update `GameClient` interface

**Files:**
- Modify: `pkg/game/interface.go:21`

**Step 1: Change the Travel signature**

Change line 21 from:
```go
Travel(ctx context.Context, targetPOI string) error
```
to:
```go
Travel(ctx context.Context, targetPOI string) (*TravelResult, error)
```

**Step 2: Verify it compiles (will fail — callers not updated yet)**

Run: `go build ./pkg/game/...`
Expected: success (Client satisfies interface now)

Run: `go build ./...`
Expected: FAIL — callers still use single-return

**Step 3: Commit**

```bash
git add pkg/game/interface.go
git commit -m "feat: update GameClient interface with Travel return type"
```

---

### Task 5: Update all callers

**Files:**
- Modify: `pkg/skills/client_dispatcher.go:95-103`
- Modify: `pkg/game/client_helpers.go:210-213`
- Modify: `pkg/game/navigation.go:123`
- Modify: `pkg/game/crafting_loop.go:282`
- Modify: `pkg/game/mining.go:332,421`
- Modify: `cmd/bridge/mcp-bridge-service/tools.go:453`
- Modify: `cmd/auto-fighter/main.go:156,216`
- Modify: `cmd/auto-random/main.go:216`
- Modify: `cmd/auto-recall/main.go:400,435`
- Modify: `cmd/auto-explorer/main.go:545,689`
- Modify: `cmd/auto-llm-miner/main.go:316`
- Modify: `cmd/auto-trader/main.go:163`

**Step 1: Update `ClientDispatcher` (biggest win — remove `waitForArrival`)**

In `pkg/skills/client_dispatcher.go`, replace the travel case (lines 95-103):

```go
	case "travel":
		if target == "" {
			return fmt.Errorf("travel requires a target POI")
		}
		_, err := d.Client.Travel(ctx, target)
		if err != nil {
			return err
		}
		// Refresh system data for condition evaluation.
		d.fetchSystemData(ctx)
		return nil
```

Delete the `waitForArrival` method (lines 474-493) — it is no longer needed.

Also delete the `arrivalTimeout` constant (line 468) if no longer referenced by `waitForSystemChange`. If `waitForSystemChange` still uses it, keep it.

**Step 2: Update `SafeCommandBuilder.Travel` in `client_helpers.go`**

Replace lines 210-213:
```go
func (s *SafeCommandBuilder) Travel(ctx context.Context, targetPOI string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		_, err := s.client.Travel(ctx, targetPOI)
		return err
	})
}
```

**Step 3: Update all remaining callers**

Each caller follows the same pattern — add `_,` before `err`:

```go
// Before:
if err := client.Travel(ctx, target); err != nil {

// After:
if _, err := client.Travel(ctx, target); err != nil {
```

Apply this to all files listed above.

**Step 4: Build and test**

Run: `go build ./...`
Expected: success

Run: `go test ./...`
Expected: all tests pass

**Step 5: Lint**

Run: `golangci-lint run ./...`
Expected: no new findings

**Step 6: Commit**

```bash
git add -A
git commit -m "refactor: update all Travel() callers for new return type"
```

---

### Task 6: Remove `TickDelay` sleep for travel in `ClientDispatcher`

**Files:**
- Modify: `pkg/skills/client_dispatcher.go:66-86`

**Step 1: Skip tick delay for travel**

The `Dispatch` method currently sleeps `TickDelay` after every tick action. Since `Travel()` now blocks until complete, this sleep is redundant for travel. Update the `Dispatch` method:

```go
func (d *ClientDispatcher) Dispatch(ctx context.Context, action, target string) error {
	err := d.dispatch(ctx, action, target)
	if err != nil {
		return err
	}

	// Travel and jump manage their own waiting internally.
	// Only sleep for other tick-consuming actions.
	if isTickAction(action) && action != "travel" {
		delay := d.TickDelay
		if delay == 0 {
			delay = game.SleepTick + time.Second
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
```

**Step 2: Build and test**

Run: `go build ./... && go test ./...`
Expected: all pass

**Step 3: Commit**

```bash
git add pkg/skills/client_dispatcher.go
git commit -m "fix: skip redundant tick delay for travel in skill dispatcher"
```

---

## Summary of Changes

| File | Change |
|------|--------|
| `pkg/game/types.go` | Add `TravelResult` struct |
| `pkg/game/client.go` | Rewrite `Travel()`, add `waitForStateChange()`, add `waitForInitialResponse()`, add `sendOverride` test hook |
| `pkg/game/interface.go` | Update `Travel` signature |
| `pkg/game/client_test.go` | Add 3 tests for blocking Travel |
| `pkg/skills/client_dispatcher.go` | Simplify travel dispatch, remove `waitForArrival`, skip tick delay for travel |
| `pkg/game/client_helpers.go` | Update `SafeCommandBuilder.Travel` |
| 10 cmd/* and pkg/* files | Mechanical `_, err :=` updates |

## Future Work (not in this plan)

Once travel is validated, apply the same pattern to:
- `Jump()` → `(*JumpResult, error)` — wait for `state.Traveling == false && state.System.ID changed`
- `Mine()` → `(*MineResult, error)` — wait for mining_yield or cargo change
- `CraftWithQuantity()` → `(*CraftResult, error)` — wait for craft_complete
- `Dock()`/`Undock()` → already fast, but could return `(*DockResult, error)` for consistency
- Eventually remove `waitForActionResponse` entirely
