# WebSocket Connection Stability Analysis

## Problem Statement
WebSocket connections show high reliability in benchmarks but frequently disconnect during normal agent operation (auto-prophet, auto-trader, etc.), barely staying connected for 1-2 actions before requiring reconnection.

## Root Cause Analysis

### 1. **Server-Initiated NormalClosure** ⚠️ PRIMARY SUSPECT

The log shows:
```
Connection error: failed to get reader: received close frame: status = StatusNormalClosure and reason = ""
```

**Key Finding**: This is a **server-side** close with no reason provided. The server is actively closing the connection, not a network error.

**Possible Server-Side Triggers**:
- Rate limiting exceeded (too many messages too quickly)
- Concurrent connection limits (multiple agents running)
- Session timeout (idle connections)
- Protocol violation (malformed messages or wrong sequence)
- Server-side resource limits

### 2. **Aggressive Health Monitor Timeout**

```go
pingInterval: 30 * time.Second,  // Check every 30s
pongTimeout:  10 * time.Second,  // Connection dead after 10s of no messages
```

**Problem**: The health monitor expects messages at least every 10 seconds, but:
- Game tick rate is 10 seconds
- Agent operations use `SleepQuick` (2s) waits between commands
- Server may not push updates during idle periods
- This causes false-positive "connection dead" triggers

### 3. **Goroutine Management Issues**

**Current Flow**:
```
Connect() → starts listen() and monitorConnectionHealth()
Disconnect() → sends stopPing signal, closes connection
Reconnect() → Disconnect() → wait 2s → Connect()
```

**Problems**:
1. **No goroutine lifecycle tracking**: `Connect()` starts new goroutines but doesn't wait for old ones to exit
2. **Race condition**: During reconnect, old `listen()` may still be running when new one starts
3. **Channel reuse**: `stopCh` is closed in `Close()` but never reset for reconnections
4. **Multiple health monitors**: Each `Connect()` call starts a new `monitorConnectionHealth()` without stopping the previous one

### 4. **Reconnection Logic Flaws**

In `ReconnectingHandler.OnDisconnected()`:
```go
if r.reconnecting.CompareAndSwap(false, true) {
    go r.attemptReconnection()
}
```

**Problems**:
- Exponential backoff: 2s, 4s, 8s, 16s, 32s = **62 seconds total** for 5 attempts
- During backoff, if connection fails again, no new reconnection starts
- The "already reconnecting" flag may get stuck if goroutine panics

### 5. **Message Storm During Reconnection**

Looking at agent code patterns:
```go
// prophetLoop runs every SleepTick (10s)
case <-ticker.C:
    // May call: GetMap(), NavigateToSystem(), Chat(), etc.
```

When connection drops and reconnects:
1. Agent may be mid-operation with pending API calls
2. Reconnection happens and agent tries to continue
3. Multiple rapid messages may trigger server rate limiting
4. Server closes connection with StatusNormalClosure
5. Cycle repeats

### 6. **Context Cancellation Issues**

The `Connect()` method receives a context:
```go
func (c *Client) Connect(ctx context.Context) error {
    // ...
    go c.listen(ctx)           // Goroutine uses context
    go c.monitorConnectionHealth(ctx)
}
```

**Problem**: If context is cancelled during reconnection, goroutines may not shut down cleanly, leading to:
- Dangling goroutines
- Channel operations on closed channels
- Memory leaks

### 7. **Lock Contention**

`Send()` holds `c.mu.RLock()` while writing:
```go
func (c *Client) Send(ctx context.Context, msg protocol.Message) error {
    c.mu.RLock()
    defer c.mu.RUnlock()
    // ...
    c.conn.Write(writeCtx, websocket.MessageText, data)
```

`Disconnect()` and `Connect()` both acquire `c.mu.Lock()` (write lock).

**Problem**: If a send is in progress when disconnect happens, it can block:
- Disconnect waits for Send to complete
- Send tries to write to closing connection
- Race condition causes errors

## Why Benchmarks Work But Agents Don't

| Benchmarks | Agents |
|------------|--------|
| Single connection | Multiple agents may run concurrently |
| Controlled message timing | Agents send messages based on game ticks (10s) |
| No reconnection logic | Complex reconnection with exponential backoff |
| Short-lived (seconds/minutes) | Long-running (hours) |
| Sequential operations | Concurrent ticker + goroutine operations |
| No health monitor | Health monitor with 10s timeout |

## Recommended Fixes (Priority Order)

### Priority 1: Increase Health Monitor Timeout
```go
// Change from 10s to 60s
pongTimeout: 60 * time.Second,
```
This reduces false-positive reconnections during normal operation.

### Priority 2: Fix Goroutine Management
Add a `sync.WaitGroup` to track goroutines:
```go
type Client struct {
    // ...
    wg      sync.WaitGroup
    connMu  sync.Mutex  // Separate mutex for connection operations
}
```

### Priority 3: Add Message Rate Limiting
Prevent message storms by adding a rate limiter:
```go
type Client struct {
    rateLimiter *rate.Limiter
}
```

### Priority 4: Better Reconnection Strategy
- Reduce exponential backoff (use 1s, 2s, 4s instead of powers of 2)
- Add jitter to prevent thundering herd
- Don't treat StatusNormalClosure as error (it's expected)

### Priority 5: Add Connection Metrics
Track:
- Messages sent/received per second
- Average time between server messages
- Reconnection frequency
- Server close frame status codes

## Fixes Applied

The following fixes have been implemented (see `CONNECTION_STABILITY_CHANGES.md` for details):

1. ✅ **Increased health monitor timeout** from 10s to 60s
2. ✅ **Added comprehensive diagnostic logging**
3. ✅ **Fixed goroutine management** with proper lifecycle tracking

## Next Steps for Investigation

With the new diagnostic logging in place, run an agent and monitor the output for:

1. **Connection metrics on disconnect** - Check uptime, message counts
2. **Server close frame details** - Status codes and reasons
3. **Goroutine lifecycle** - Confirm clean start/stop
4. **Message timing patterns** - Identify gaps or storms

Example command:
```bash
./auto-prophet prophet-1 2>&1 | tee prophet-debug.log
```

Look for log patterns:
```
=== Connection Metrics [disconnect] ===
[listen-X] Server close frame | Status: ...
[health-X] No messages received for ...
```
