# Sleep Synchronization and WebSocket State Management Fix Plan

## Problem Summary

The current implementation in `cmd/watcher/main.go` uses arbitrary `time.Sleep()` calls to work around synchronization issues. This approach is brittle and can lead to:
- Race conditions when delays are too short
- Unnecessary latency when delays are too long
- Difficult-to-debug timing-dependent failures
- Poor user experience with slow startup

## Issues Identified

### 1. TUI Initialization Wait (Line 204-206)
**Current:** 2-second sleep before starting agent connections
**Problem:** No guarantee TUI is actually ready; may be too long or too short
**Location:** `startAgentConnections()`

### 2. WebSocket Connection Ready (Lines 236-237, 374-375, 390-391)
**Current:** 500ms sleep after connection before authentication
**Problem:** WebSocket handshake may not be complete; no confirmation of ready state
**Locations:**
- `startAgentConnections()` after `client.Connect()`
- `startWatcherClient()` after `websocket.Dial()`

### 3. Authentication Completion (Lines 250-251, 417-418)
**Current:** 1-3 second sleep after sending login/register before next action
**Problem:** No confirmation that authentication succeeded; arbitrary delays
**Locations:**
- `startAgentConnections()` before `client.GetSystem()`
- `startWatcherClient()` before sending `get_system`

## Proposed Solutions

### Phase 1: TUI Ready Signal

**Goal:** Replace sleep with explicit ready signal from TUI

**Changes:**
1. Add a `Ready` channel to `WatcherModel` or pass as parameter
2. Close the channel in `Init()` or first `Update()` call
3. In `main()`, wait for ready signal before launching agent connections

**Implementation:**
```go
// In main()
readyChan := make(chan struct{})
model := tui.NewWatcherModel(state, readyChan)

// After tea.NewProgram()
go func() {
    <-readyChan
    startAgentConnections(ctx, agentMgr, username, token, p)
}()
```

**Benefits:**
- Deterministic startup order
- No arbitrary delays
- TUI controls its own initialization

### Phase 2: WebSocket Ready State

**Goal:** Wait for actual WebSocket ready state instead of assuming after delay

**Changes:**
1. Modify `game.Client` to expose a `Ready()` channel
2. Client closes ready channel upon receiving first message (welcome)
3. Wait for ready signal before sending authentication

**Implementation in `pkg/game/client.go`:**
```go
type Client struct {
    // ... existing fields
    readyChan chan struct{}
    readyOnce sync.Once
}

func (c *Client) Ready() <-chan struct{} {
    return c.readyChan
}

// In message receive loop:
func (c *Client) handleMessage(resp protocol.Response) {
    c.readyOnce.Do(func() {
        close(c.readyChan)
    })
    // ... rest of message handling
}
```

**Usage in `startAgentConnections()`:**
```go
if err := client.Connect(ctx); err != nil { ... }

// Wait for connection ready
select {
case <-client.Ready():
    // Connection is ready, proceed with auth
case <-time.After(10 * time.Second):
    log.Printf("[%s] Timeout waiting for connection ready", a.ID())
    return
}
```

**Benefits:**
- Responds immediately when server is ready
- Timeout as fallback for hung connections
- More reliable than fixed delays

### Phase 3: Synchronous Authentication

**Goal:** Make login/register operations synchronous with proper response handling

**Changes:**
1. Add response waiting mechanism to `game.Client`
2. Implement request-response pattern for auth operations
3. Return error if authentication fails

**Implementation:**

Add response waiter to Client:
```go
type responseWaiter struct {
    mu       sync.Mutex
    waiters  map[string]chan protocol.Response
    nextID   int
}

func (c *Client) waitForResponse(ctx context.Context, messageType string, timeout time.Duration) (protocol.Response, error) {
    respChan := make(chan protocol.Response, 1)

    c.waiter.mu.Lock()
    c.waiter.waiters[messageType] = respChan
    c.waiter.mu.Unlock()

    defer func() {
        c.waiter.mu.Lock()
        delete(c.waiter.waiters, messageType)
        c.waiter.mu.Unlock()
    }()

    select {
    case resp := <-respChan:
        return resp, nil
    case <-time.After(timeout):
        return protocol.Response{}, fmt.Errorf("timeout waiting for %s response", messageType)
    case <-ctx.Done():
        return protocol.Response{}, ctx.Err()
    }
}
```

Modify Login/Register:
```go
func (c *Client) Login(ctx context.Context) error {
    msg := protocol.Message{
        Type: protocol.TypeLoggedIn,
        // ... payload
    }

    if err := c.send(msg); err != nil {
        return err
    }

    resp, err := c.waitForResponse(ctx, protocol.TypeLoggedIn, 5*time.Second)
    if err != nil {
        return err
    }

    if resp.Type == protocol.TypeError {
        return fmt.Errorf("login failed: %v", resp.Payload)
    }

    return nil
}
```

Update message handler to notify waiters:
```go
func (c *Client) handleMessage(resp protocol.Response) {
    c.readyOnce.Do(func() { close(c.readyChan) })

    // Check if anyone is waiting for this response
    c.waiter.mu.Lock()
    if ch, ok := c.waiter.waiters[resp.Type]; ok {
        select {
        case ch <- resp:
        default:
        }
    }
    c.waiter.mu.Unlock()

    // Continue with normal handler
    if c.handler != nil {
        c.handler.OnMessage(resp)
    }
}
```

**Benefits:**
- Errors propagate properly
- No arbitrary waits
- Clear success/failure feedback
- Can retry on failure

### Phase 4: Watcher Client Refactoring

**Goal:** Apply same patterns to watcher client WebSocket connection

**Changes:**
1. Wrap watcher WebSocket in similar abstraction to `game.Client`
2. Use same ready channel pattern
3. Use same synchronous auth pattern

**Alternative:** Reuse `game.Client` for watcher instead of separate WebSocket handling

## Implementation Order

1. **Phase 1 (Low Risk):** TUI ready signal - isolated change
2. **Phase 2 (Medium Risk):** WebSocket ready state - requires Client changes but backward compatible
3. **Phase 3 (Higher Risk):** Synchronous auth - changes Client API and behavior
4. **Phase 4 (Optional):** Watcher client refactoring - can reuse code from phases 2-3

## Testing Strategy

1. **Unit Tests:**
   - Test ready channel closes on first message
   - Test response waiter timeout behavior
   - Test response waiter with successful response

2. **Integration Tests:**
   - Test full connection flow with mock server
   - Test authentication failure handling
   - Test timeout scenarios

3. **Manual Testing:**
   - Test with slow network (add artificial latency)
   - Test with server unavailable
   - Test with invalid credentials
   - Verify no delays in happy path

## Risks and Mitigations

**Risk:** Breaking existing behavior
**Mitigation:**
- Keep old behavior behind feature flag initially
- Add timeout fallbacks to all new wait operations
- Extensive testing before removing sleeps

**Risk:** Deadlocks in new synchronization code
**Mitigation:**
- Always use `select` with timeout or context
- Add deadlock detection in tests
- Use `go test -race` to catch issues

**Risk:** Server sends unexpected message order
**Mitigation:**
- Log unexpected messages
- Handle out-of-order responses gracefully
- Add state machine validation

## Success Criteria

- [ ] No `time.Sleep()` calls for synchronization
- [ ] All state transitions use explicit signals
- [ ] Authentication errors are properly detected and reported
- [ ] Startup time is minimized (responds as fast as server allows)
- [ ] No race conditions detected by `-race` flag
- [ ] Integration tests pass reliably (100 consecutive runs)

## Future Enhancements

1. **Connection Pooling:** Reuse WebSocket connections for multiple operations
2. **Retry Logic:** Automatic retry with exponential backoff for transient failures
3. **Health Checks:** Periodic ping/pong to detect dead connections
4. **Graceful Degradation:** Continue operation if some agents fail to connect
