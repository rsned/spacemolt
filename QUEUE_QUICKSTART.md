# Quick Start: Robust WebSocket Command Queue

## TL;DR

Use `*Queued` methods instead of regular methods for reliable sequential command execution.

## Basic Example

```go
package main

import (
    "context"
    "log"
    "github.com/rsned/spacemolt/pkg/game"
)

func main() {
    client := game.NewClient(
        "wss://game.spacemolt.com/ws",
        "username",
        "password",
        nil,
    )

    ctx := context.Background()
    client.Connect(ctx)
    client.Login(ctx)

    // Use queued methods for reliable execution
    client.DockQueued(ctx)
    client.TravelQueued(ctx, "asteroid_1")
    client.MineQueued(ctx)
    client.TravelQueued(ctx, "station_1")
    client.DockQueued(ctx)
    client.SellAllBulk(ctx, nil)
    client.RefuelQueued(ctx)
}
```

## Available Methods

All regular methods have `*Queued` equivalents:

- `DockQueued(ctx)` / `UndockQueued(ctx)`
- `TravelQueued(ctx, targetPOI)`
- `JumpQueued(ctx, targetSystem)`
- `MineQueued(ctx)`
- `RefuelQueued(ctx)` / `RepairQueued(ctx)`
- `SellQueued(ctx, itemID, quantity)`
- `BuyQueued(ctx, itemID, quantity)`
- `GetSystemQueued(ctx)` / `GetStatusQueued(ctx)` / `GetPOIQueued(ctx)`
- `GetListingsQueued(ctx)`

## With Automatic Retry

For even more reliability, use `SafeCommandBuilder`:

```go
builder := game.NewSafeCommandBuilder(client)

// These automatically retry on connection failures
builder.DockQueued(ctx)
builder.TravelQueued(ctx, "asteroid_1")
builder.MineQueued(ctx)
```

## Custom Commands

```go
resp, err := client.SendQueued(ctx, protocol.Message{
    Type:      "get_system",
    Timestamp: time.Now().UnixMilli(),
}, 15*time.Second)

if err != nil {
    log.Printf("Command failed: %v", err)
} else {
    log.Printf("Response: %v", resp.Payload)
}
```

## Monitor Queue

```go
// Check queue size
size := client.CmdQueue.QueueSize()

// Check active command
active := client.CmdQueue.GetActiveCommand()
if active != nil {
    log.Printf("Active command: %s", active.ID)
}
```

## Why Use Queued Methods?

❌ **Old way (flaky)**:
```go
client.Dock(ctx)      // Might fail
client.Travel(ctx, ...)    // Race condition
client.Mine(ctx)      // Lost response
```

✅ **New way (reliable)**:
```go
client.DockQueued(ctx)      // ✅ Executes
client.TravelQueued(ctx, ...)  // ✅ Waits for dock to complete
client.MineQueued(ctx)      // ✅ Waits for travel to complete
```

## Key Benefits

1. **Sequential**: Commands execute one at a time
2. **Reliable**: Each command waits for its response
3. **No Race Conditions**: Unique waiters per command
4. **Auto-Reconnect**: Handles connection failures
5. **Clear Errors**: Proper timeout and error handling

## Migration

Simply add `Queued` to method names:

```go
// Before
client.Dock(ctx)
client.Travel(ctx, "poi_1")
client.Mine(ctx)

// After
client.DockQueued(ctx)
client.TravelQueued(ctx, "poi_1")
client.MineQueued(ctx)
```

That's it! The queue system handles the rest.
