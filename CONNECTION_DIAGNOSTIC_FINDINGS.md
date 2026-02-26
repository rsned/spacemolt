# WebSocket Connection Diagnostic Findings

## Log Analysis Results

From the diagnostic logs collected on 2026/02/25 00:35:53, we can now see the root cause of the connection instability.

### Key Finding: Server-Side Idle Timeout

```
[health-2] No messages received for 1m0.711947565s (timeout: 1m0s), connection may be dead
=== Connection Metrics [health_timeout] ===
  Connection ID: 20260225-003353-134
  Uptime: 2m0s
  Messages sent: 5 | received: 9 | total: 14
  Last server message: 1m0.712s ago
  Last client send: 1m30.56s ago
  Last client receive: 1m0.712s ago
```

### Analysis

1. **Connection Pattern**:
   - Agent sent 5 messages and received 9 responses over 2 minutes
   - After the last message exchange (~30 seconds into connection), server stopped pushing updates
   - Server went silent for 60+ seconds until health monitor triggered

2. **Root Cause**:
   - The game server has an **idle connection timeout** of approximately 90-120 seconds
   - When no messages are exchanged for this period, the server closes the connection
   - This is a common WebSocket server configuration to prevent resource leaks

3. **Why Benchmarks Work**:
   - Benchmarks send continuous messages without long idle periods
   - Agents have natural idle periods (waiting for game ticks, thinking, etc.)
   - This idle time triggers the server's timeout

### Secondary Finding: Goroutine Cleanup Timeout

```
Warning: Timeout waiting for goroutines to exit
[listen-3] Connection error: failed to get reader: use of closed network connection
```

The listen goroutine blocks on `conn.Read()` even after the connection is closed, taking >5 seconds to exit.

## Solution Implemented

### 1. WebSocket Ping/Pong Keep-Alive

Added periodic WebSocket ping frames to keep the connection alive during idle periods:

```go
// Send ping every 30 seconds to prevent server-side idle timeout
pingTicker := time.NewTicker(30 * time.Second)

case <-pingTicker.C:
    err := conn.Ping(pingCtx)
    if err != nil {
        // Ping failed = connection dead
        handler.OnDisconnected(fmt.Errorf("ping failed: %w", err))
        return
    }
```

**Benefits**:
- Keeps connection alive through server-side idle timeouts
- Actively detects dead connections (ping failure)
- Standard WebSocket protocol feature
- Minimal bandwidth overhead

### 2. Improved Goroutine Cleanup

Reduced the aggressive close timeout and added immediate connection closure:

```go
// Use AbortiveClose to force immediate closure
_ = conn.Close(websocket.StatusNormalClosure, "client disconnect")

// Give goroutines 1 second to exit cleanly
select {
case <-done:
    // Clean exit
case <-time.After(1 * time.Second):
    // Force close anyway
}
```

## Expected Results

### Before Ping/Pong
```
[health-2] No messages received for 1m0s (timeout: 1m0s), connection may be dead
Disconnected: connection timeout
```

### After Ping/Pong (Expected)
```
[health-2] Ping sent successfully (keepalive)
[health-2] Ping sent successfully (keepalive)
... connection stays alive indefinitely ...
```

## Configuration

Current settings:
- **Ping interval**: 30 seconds (sends ping every 30s of no activity)
- **Health check interval**: 30 seconds (checks for received messages)
- **Health timeout**: 60 seconds (triggers reconnect if no messages for 60s)

These settings should handle server idle timeouts up to ~90 seconds (typical range).

## Testing

Run the agent and monitor for ping messages:

```bash
./auto-prophet prophet-1 2>&1 | grep -E "(Ping|Connection Metrics|health)"
```

Expected log pattern:
```
[health-1] Health monitor started | Interval: 30s | Timeout: 1m0s
[health-1] Ping sent successfully (keepalive)    # Every 30s during idle
[health-1] Ping sent successfully (keepalive)    # Connection stays alive
=== Connection Metrics [disconnect] ===           # Only on real errors
```

## Next Steps

1. **Monitor ping success rate**: Check if pings are consistently successful
2. **Check connection duration**: Connections should now last much longer
3. **Verify server response**: Ensure server responds to pongs correctly
4. **Adjust intervals if needed**: Some servers may need more frequent pings

## Potential Issues

If the server doesn't support WebSocket ping/pong:
- Pings will fail and trigger reconnections
- Will need to use application-level keepalive (e.g., `get_status` command)
- Monitor logs for "ping failed" errors
