# WebSocket Client Robustness Improvements

## Overview

The WebSocket client in `pkg/game/client.go` has been significantly improved to handle connection issues more robustly and prevent the "flaky" behavior where connections would drop after only one or two commands.

## Problems Fixed

### 1. **Race Condition in Reconnection Handler**
**Issue**: The `ReconnectingHandler.OnDisconnected` method had a race condition where it checked `reconnecting.Load()` twice, which could cause multiple concurrent reconnection attempts.

**Fix**: Simplified the logic to use a single `CompareAndSwap` operation, ensuring only one reconnection attempt occurs at a time.

### 2. **No Connection Health Monitoring**
**Issue**: Dead connections would go undetected until the next operation failed, causing unnecessary delays.

**Fix**: Added connection health monitoring that:
- Tracks the last message received time
- Checks if messages are received within a configurable timeout (default 40s)
- Triggers reconnection if no messages are received for too long

### 3. **Inadequate Reconnection Logic**
**Issue**: Reconnection would fail on transient network issues without retry.

**Fix**: Improved the `Reconnect` method to:
- Use exponential backoff between attempts
- Retry up to 3 times before giving up
- Wait for the connection to be ready after reconnecting
- Properly handle context cancellation

### 4. **No Write Timeout**
**Issue**: WebSocket writes could hang indefinitely if the connection was slow.

**Fix**: Added a 10-second write timeout to all WebSocket writes.

### 5. **Lack of Connection Verification**
**Issue**: Commands could be sent before the connection was fully ready.

**Fix**: Added:
- `WaitForReady()` method to wait for the first message
- `EnsureConnected()` method to verify and reconnect if needed

## New Features

### 1. **Connection Health Monitoring**

```go
// Automatic monitoring in background goroutine
func (c *Client) monitorConnectionHealth(ctx context.Context)
```

- Checks connection health every 30 seconds
- Triggers reconnection if no messages received for 40 seconds
- Can be configured via `pingInterval` and `pongTimeout` fields

### 2. **Robust Command Execution**

New helper file: `pkg/game/client_helpers.go`

```go
executor := NewRobustCommandExecutor(client)

// Automatic retry with connection recovery
err := executor.ExecuteCommand(ctx, func(ctx context.Context) error {
    return client.Travel(ctx, "station")
})
```

Features:
- Automatic retry (up to 3 attempts by default)
- Exponential backoff between retries
- Automatic reconnection on connection errors
- Distinguishes between retryable and non-retryable errors

### 3. **Safe Command Builder**

```go
builder := NewSafeCommandBuilder(client)

// All commands with automatic error handling
err := builder.Travel(ctx, "station")
err := builder.Dock(ctx)
err := builder.Mine(ctx)
```

Provides type-safe wrappers for all common game commands with built-in retry logic.

## Usage Examples

### For Agent Authors

#### Option 1: Use SafeCommandBuilder (Recommended)

```go
import "github.com/rsned/spacemolt/pkg/game"

func MyAgentLogic(client *game.Client) {
    ctx := context.Background()
    builder := game.NewSafeCommandBuilder(client)

    // All commands are automatically retried and connection is managed
    if err := builder.Undock(ctx); err != nil {
        log.Printf("Failed to undock: %v", err)
        return
    }

    if err := builder.Travel(ctx, "mining_asteroid_1"); err != nil {
        log.Printf("Failed to travel: %v", err)
        return
    }

    if err := builder.Mine(ctx); err != nil {
        log.Printf("Failed to mine: %v", err)
        return
    }
}
```

#### Option 2: Use RobustCommandExecutor for Custom Commands

```go
executor := game.NewRobustCommandExecutor(client)

err := executor.ExecuteCommand(ctx, func(ctx context.Context) error {
    return client.CustomCommand(ctx, args)
})
```

#### Option 3: Manual Connection Management

```go
// Ensure connected before operations
if err := client.EnsureConnected(ctx); err != nil {
    return fmt.Errorf("connection failed: %w", err)
}

// Execute command
if err := client.Travel(ctx, "station"); err != nil {
    return fmt.Errorf("travel failed: %w", err)
}
```

### For Testing

The improvements make testing easier:

```go
// Connection won't drop mid-test
builder := NewSafeCommandBuilder(testClient)

// Multiple operations without fear of connection loss
builder.Dock(ctx)
builder.GetListings(ctx)
builder.SellAllBulk(ctx, nil)
builder.Refuel(ctx)
```

## Error Handling Improvements

### Connection Error Detection

The system now automatically detects and handles:

- `"not connected"` - Connection lost, will reconnect
- `"connection reset"` - Connection closed by peer
- `"broken pipe"` - Network failure
- `"connection timeout"` - No response
- `"websocket closed"` - WebSocket closed

### Retryable Errors

The following errors trigger automatic retry:

- `"timeout"` - Operation timed out
- `"deadline exceeded"` - Context deadline
- `"rate limited"` - Server rate limiting
- `"already in transit"` - Already traveling/jumping

### Non-Retryable Errors

These errors return immediately without retry:

- `"insufficient fuel"` - User needs to refuel
- `"insufficient credits"` - User needs money
- `"cargo hold full"` - User needs to sell items
- `"must undock first"` - Wrong state
- Most game logic errors

## Configuration

You can customize the behavior:

```go
client := game.NewClient(url, username, password, logger)

// Adjust health monitoring (if you need direct access)
// Note: These are set in NewClient, but you can modify if needed
// client.pingInterval = 60 * time.Second  // Check every minute
// client.pongTimeout = 20 * time.Second   // Timeout after 20s of silence

// Customize executor behavior
executor := game.NewRobustCommandExecutor(client)
// Can't directly modify private fields, but ExecuteWithRetry allows custom retry count
executor.ExecuteWithRetry(ctx, cmdFunc, 5) // 5 retries instead of 3
```

## Migration Guide

### Before (Flaky)

```go
// This could fail after a couple of commands
client.Travel(ctx, "station")
client.Dock(ctx)
client.GetListings(ctx)
```

### After (Robust)

```go
// Option 1: Use SafeCommandBuilder
builder := game.NewSafeCommandBuilder(client)
builder.Travel(ctx, "station")
builder.Dock(ctx)
builder.GetListings(ctx)

// Option 2: Use RobustCommandExecutor
executor := game.NewRobustCommandExecutor(client)
executor.ExecuteCommand(ctx, func(ctx context.Context) error {
    return client.Travel(ctx, "station")
})
```

## Testing the Improvements

To test the robustness improvements:

1. **Simulate Connection Loss**:
   ```bash
   # Temporarily block WebSocket port
   sudo iptables -A OUTPUT -p tcp --dport 443 -j DROP
   # Wait a few seconds
   sudo iptables -D OUTPUT -p tcp --dport 443 -j DROP
   ```

2. **Monitor Logs**:
   - Look for "No messages received for X, connection may be dead"
   - Verify automatic reconnection occurs
   - Check that commands continue to work after reconnection

3. **Load Testing**:
   ```go
   // Execute many commands in sequence
   builder := NewSafeCommandBuilder(client)
   for i := 0; i < 100; i++ {
       builder.GetStatus(ctx)
       time.Sleep(1 * time.Second)
   }
   ```

## Performance Considerations

- **Health Monitoring**: Adds minimal overhead (one goroutine checking every 30s)
- **Connection Tracking**: Minimal memory overhead (timestamps and mutexes)
- **Retry Logic**: Only adds overhead when failures occur

## Future Improvements

Potential areas for future enhancement:

1. **Metrics Collection**: Track connection failures, retry counts, etc.
2. **Adaptive Timeouts**: Adjust timeouts based on observed latency
3. **Connection Pooling**: For agents that need multiple concurrent connections
4. **Circuit Breaker**: Temporarily stop retrying after many consecutive failures

## Files Modified

- `pkg/game/client.go`: Core WebSocket client improvements
- `pkg/game/client_helpers.go`: New utility functions for robust command execution

## Backward Compatibility

All changes are backward compatible. Existing code using `client.Method()` directly will continue to work, but won't benefit from automatic retry unless migrated to use the new helper functions.