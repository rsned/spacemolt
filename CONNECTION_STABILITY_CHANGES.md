# WebSocket Connection Stability Fixes

## Changes Made

This document describes the fixes applied to improve WebSocket connection stability in the Spacemolt game client, including the critical ping/pong keep-alive solution.

## 1. Increased Health Monitor Timeout (Step 1)

**File**: `pkg/game/client.go`

**Change**: Increased `pongTimeout` from 10 seconds to 60 seconds.

```go
pongTimeout: 60 * time.Second, // Increased from 10s to reduce false reconnections during normal operation
```

**Rationale**:
- The health monitor was checking if messages were received every 30 seconds
- With a 10-second timeout, it would trigger reconnections too aggressively
- Game tick rate is 10 seconds, and during idle periods the server may not push messages
- 60 seconds gives the server ample time to send messages before considering the connection dead

**Expected Impact**: Should eliminate false-positive "connection timeout" reconnections during normal operation.

## 2. Added Comprehensive Diagnostic Logging (Step 2)

**File**: `pkg/game/client.go`

### New Diagnostic Fields
Added to the `Client` struct:
```go
// Diagnostic tracking
connectionID      string    // Unique ID for this connection instance
connectTime       time.Time // When this connection was established
messagesSent      int64     // Counter for messages sent
messagesReceived  int64     // Counter for messages received
lastSendTime      time.Time // Time of last send
lastReceiveTime   time.Time // Time of last receive
diagnosticMu      sync.RWMutex
goroutineID       int64     // Counter for tracking goroutine instances
```

### New Helper Functions

#### `logConnectionMetrics(event string)`
Logs comprehensive connection metrics including:
- Connection ID
- Uptime duration
- Messages sent/received counts
- Time since last server message
- Time since last client send/receive

#### `trackMessageSent()` / `trackMessageReceived()`
Atomic counters for tracking message throughput.

#### `generateConnectionID()`
Creates unique connection IDs like `20260225-142530-123`.

### Enhanced Logging

**Connection establishment**:
```
Connected to wss://game.spacemolt.com/ws (read limit: 10MB) | Connection ID: 20260225-142530-123 | Goroutine: 1
```

**Goroutine lifecycle**:
```
[listen-1] Goroutine started
[health-1] Health monitor started | Interval: 30s | Timeout: 60s
[listen-1] Goroutine exited
[health-1] Health monitor exited
```

**Server close frames**:
```
[listen-1] Server close frame | Status: StatusNormalClosure (1000) | Reason: ""
```

**Connection metrics on disconnect**:
```
=== Connection Metrics [disconnect] ===
  Connection ID: 20260225-142530-123
  Uptime: 1m45s
  Messages sent: 15 | received: 23 | total: 38
  Last server message: 2.3s ago
  Last client send: 5.1s ago
  Last client receive: 2.3s ago
```

**Health monitor timeout**:
```
[health-1] No messages received for 1m5s (timeout: 1m0s), connection may be dead
=== Connection Metrics [health_timeout] ===
  ...
```

**Rationale**: These logs will help identify:
- Whether the server is actively closing connections
- Message patterns before disconnects
- Goroutine lifecycle issues
- How long connections typically last
- Message throughput rates

## 3. Fixed Goroutine Management (Step 3)

**File**: `pkg/game/client.go`

### New Goroutine Management Fields
Added to the `Client` struct:
```go
// Goroutine lifecycle management
goroutineCtx    context.Context
goroutineCancel context.CancelFunc
goroutineWg     sync.WaitGroup
```

### Changes to `NewClient()`
Initialize goroutine context on client creation:
```go
goroutineCtx, goroutineCancel := context.WithCancel(context.Background())
```

### Changes to `Connect()`
Start goroutines with WaitGroup tracking:
```go
// Start message listener with managed lifecycle
c.goroutineWg.Add(1)
go func() {
    defer c.goroutineWg.Done()
    c.listen(c.goroutineCtx)
}()

// Start connection health monitoring with managed lifecycle
c.goroutineWg.Add(1)
go func() {
    defer c.goroutineWg.Done()
    c.monitorConnectionHealth(c.goroutineCtx)
}()
```

### Changes to `Disconnect()`
Properly signal goroutines to stop and wait for cleanup:
```go
// Signal all goroutines to stop
c.goroutineCancel()

// ... disconnect logic ...

// Wait for goroutines to exit (with timeout to prevent indefinite hang)
done := make(chan struct{})
go func() {
    c.goroutineWg.Wait()
    close(done)
}()

select {
case <-done:
    c.debugLogger.Printf("All goroutines exited cleanly")
case <-time.After(5 * time.Second):
    c.debugLogger.Printf("Warning: Timeout waiting for goroutines to exit")
}
```

### Changes to `Reconnect()`
Reset goroutine context after disconnect:
```go
// Reset goroutine context for the new connection
c.mu.Lock()
c.goroutineCancel()
c.goroutineCtx, c.goroutineCancel = context.WithCancel(context.Background())
c.mu.Unlock()
```

### Changes to `Close()`
Ensure goroutines are cleaned up on client close:
```go
// Signal all goroutines to stop
c.goroutineCancel()

// ... close logic ...

// Wait for goroutines to exit (with timeout)
```

### Changes to `listen()` and `monitorConnectionHealth()`
Use the managed context instead of the passed context:
```go
// Before: go c.listen(ctx)
// After:  go c.listen(c.goroutineCtx)
```

**Rationale**:
- Prevents goroutine leaks during reconnections
- Ensures old goroutines exit before new ones start
- Uses context cancellation for clean shutdown
- WaitGroup ensures we can wait for goroutines to finish
- Timeout prevents indefinite hangs if goroutines are stuck

## Expected Results

### Before These Changes
- Frequent disconnections every 1-2 actions
- Multiple goroutine instances running concurrently
- No visibility into why connections close
- Aggressive 10-second health timeout causing false reconnections
- **Server idle timeout after 90-120 seconds of inactivity**

### After These Changes
- Longer-lived connections (60-second timeout reduces false positives)
- Clean goroutine lifecycle management
- Comprehensive logging to identify root causes
- Better handling of reconnections without resource leaks
- **WebSocket ping/pong keep-alive prevents server idle timeouts**

## 4. Added WebSocket Ping/Pong Keep-Alive (Critical Fix)

**File**: `pkg/game/client.go`

**Problem Identified** (from diagnostic logs):
```
[health-2] No messages received for 1m0.711947565s (timeout: 1m0s)
  Uptime: 2m0s
  Messages sent: 5 | received: 9 | total: 14
  Last server message: 1m0.712s ago
  Last client send: 1m30.56s ago
```

The server has an **idle connection timeout** (~90-120 seconds). When no messages are exchanged, the server closes the connection.

**Solution**: Send WebSocket ping frames every 30 seconds during idle periods.

```go
// Added to monitorConnectionHealth()
pingTicker := time.NewTicker(30 * time.Second)

case <-pingTicker.C:
    // Send WebSocket ping to keep connection alive
    c.mu.RLock()
    conn := c.conn
    c.mu.RUnlock()

    if conn != nil {
        pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
        err := conn.Ping(pingCtx)
        cancel()

        if err != nil {
            // Ping failure = connection dead
            handler.OnDisconnected(fmt.Errorf("ping failed: %w", err))
            return
        }
        c.debugLogger.Printf("[health-%d] Ping sent successfully (keepalive)", goroutineID)
    }
```

**Rationale**:
- WebSocket ping/pong is a standard protocol feature for keep-alive
- Pings are lightweight (few bytes) and don't count as application messages
- Server must respond with pong, proving connection is alive
- Prevents server-side idle timeouts
- Actively detects dead connections (ping failure = immediate reconnect)

## 5. Improved Goroutine Cleanup

**File**: `pkg/game/client.go`

**Problem**: Listen goroutine blocked on `conn.Read()` for >5 seconds during disconnect.

**Solution**: Close connection aggressively and give goroutines 1 second to exit:

```go
if c.conn != nil {
    c.connected = false
    conn := c.conn
    c.conn = nil

    // Close the connection (unblocks Read())
    _ = conn.Close(websocket.StatusNormalClosure, "client disconnect")

    // Give goroutines 1 second to exit cleanly
    done := make(chan struct{})
    go func() {
        c.goroutineWg.Wait()
        close(done)
    }()

    select {
    case <-done:
        c.debugLogger.Printf("All goroutines exited cleanly")
    case <-time.After(1 * time.Second):
        c.debugLogger.Printf("Goroutines slow to exit, continuing anyway")
    }
}
```

## Configuration Summary

| Setting | Value | Purpose |
|---------|-------|---------|
| Ping interval | 30 seconds | Send keep-alive ping every 30s of idle |
| Health check interval | 30 seconds | Check for received messages |
| Health timeout | 60 seconds | Reconnect if no messages for 60s |
| Goroutine cleanup timeout | 1 second (fast), 10s (extended) | Prevent indefinite waits |

## Expected Results

### Before All Fixes
```
Connection alive: 2 minutes
Messages: 5 sent, 9 received
[health-2] No messages received for 1m0s
Disconnected: connection timeout
Reconnection attempt 1/5...
```

### After All Fixes (Expected)
```
Connection alive: 30+ minutes (or indefinitely)
Messages: 50 sent, 90 received
[health-2] Ping sent successfully (keepalive)
[health-2] Ping sent successfully (keepalive)
... connection stays alive ...
```

## Testing

```bash
# Build the agent
go build -o /tmp/auto-prophet ./cmd/auto-prophet/

# Run with verbose logging, filter for health events
./auto-prophet prophet-1 2>&1 | grep -E "(Ping|Connection Metrics|health|Uptime)"
```

### Success Indicators
- ✅ "Ping sent successfully (keepalive)" every ~30 seconds during idle
- ✅ Connections last >30 minutes without disconnect
- ✅ No "connection timeout" messages
- ✅ Connection metrics show high uptime

### Failure Indicators
- ❌ "Ping failed" errors (server doesn't support ping/pong)
- ❌ Still seeing "No messages received for 1m0s"
- ❌ Connections still dropping every 2 minutes

## Fallback if Ping Fails

If the server doesn't support WebSocket ping/pong:
1. Logs will show "Ping failed" errors
2. Need to implement application-level keepalive (e.g., send `get_status` command periodically)
3. Monitor for these errors in production

## Next Steps for Investigation

Run an agent (e.g., `auto-prophet`) and monitor logs for:

1. **Ping success**: Look for "Ping sent successfully (keepalive)" messages
2. **Connection duration**: Should be much longer than 2 minutes
3. **Disconnect patterns**: Should see real errors, not idle timeouts
4. **Goroutine lifecycle**: Confirm goroutines exit within 1-2 seconds

See also: `CONNECTION_DIAGNOSTIC_FINDINGS.md` for detailed analysis of the root cause.
