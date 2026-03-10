# MCP Migration Design

**Date:** 2026-03-09
**Goal:** Replace WebSocket game client with direct MCP HTTP client for all agents and strategies.

---

## Context

The WebSocket connection to `wss://game.spacemolt.com/ws` is fragile — reconnection logic, listen loops, waiter channels, health monitoring, and state synchronization add significant complexity and failure modes. The game server now exposes a preferred MCP endpoint at `https://game.spacemolt.com/mcp` using Streamable HTTP transport.

MCP is fundamentally simpler: synchronous request-response over HTTP. The server handles tick-waiting server-side, so actions like `mine`, `travel`, `dock` block at the HTTP level until complete. This eliminates the need for waiter channels, listen loops, command queues, and most of the connection management code.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Transport | Direct HTTP to `game.spacemolt.com/mcp` | No Node.js dependency, Go handles HTTP+SSE natively |
| Architecture | New `MCPGameClient` struct implementing `GameClient` | Clean separation, test in parallel, delete WS later |
| Event handling | Background polling goroutine (~10s) | Keeps state fresh, same mental model for strategies |
| MessageHandler | Lifecycle only (OnConnected/OnDisconnected) | No synthesized OnMessage events, callers read state |
| Observer/frontend | Deferred — out of scope | Separate concern, migrate after core is proven |
| Session recovery | Auto-retry with re-login on `session_invalid` | Transparent to callers, poller prevents most expiry |

## Architecture

### MCPGameClient Struct

New file: `pkg/game/mcp_game_client.go`

```go
type MCPGameClient struct {
    serverURL   string          // https://game.spacemolt.com/mcp
    sessionID   string          // game session from login response
    mcpSession  string          // MCP protocol session from Mcp-Session-Id header
    username    string          // stored for auto-re-login
    password    string          // stored for auto-re-login
    state       *State          // internal state, updated by poller + commands
    mu          sync.RWMutex    // protects state
    httpClient  *http.Client    // 90s timeout for blocking actions
    logger      *log.Logger
    handler     MessageHandler  // lifecycle callbacks only
    pollCtx     context.Context
    pollCancel  context.CancelFunc
    connected   bool
    requestID   atomic.Int64    // incrementing JSON-RPC request IDs
}
```

### Streamable HTTP Transport

The core `callTool` method:

1. Builds JSON-RPC request: `{"jsonrpc": "2.0", "id": N, "method": "tools/call", "params": {"name": "<tool>", "arguments": {"session_id": "...", ...}}}`
2. HTTP POST to `/mcp` with `Content-Type: application/json`
3. Parses SSE response (`text/event-stream`): reads `data:` lines, extracts JSON-RPC result
4. On `session_invalid` error: calls `login()`, updates `sessionID`, retries once
5. Returns parsed result or error

The `session_id` is passed as a tool argument (MCP convention), not as an HTTP header.

MCP `initialize` handshake is performed once on `Connect()`, also via HTTP POST.

HTTP timeout is 90s+ to accommodate multi-tick blocking actions (e.g., travel across systems).

### Command Implementation Pattern

All 111 `GameClient` methods follow the same mechanical pattern:

```go
func (m *MCPGameClient) Mine(ctx context.Context) error {
    result, err := m.callTool(ctx, "mine", nil)
    if err != nil {
        return err
    }
    m.updateStateFromResult(result)
    return nil
}

func (m *MCPGameClient) Travel(ctx context.Context, targetPOI string) (*TravelResult, error) {
    result, err := m.callTool(ctx, "travel", map[string]any{"target_poi": targetPOI})
    if err != nil {
        return nil, err
    }
    m.updateStateFromResult(result)
    return &TravelResult{POI: m.GetState().CurrentPOI}, nil
}
```

Key simplifications over WebSocket:
- No `waitForActionResponse` / `waitForInitialResponse` / `waitForStateChange`
- No `CommandQueue` — HTTP is inherently sequential, server enforces one-action-per-tick
- No waiter channels or response routing
- `updateStateFromResult` reuses existing `parsePlayerData`, `parseShipData`, etc.

Tool names match WebSocket command types 1:1. The OpenAPI spec (`server_docs/openapi.20260309.json`) is the source of truth for exact names and parameter schemas.

### Background Poller

A goroutine started by `Connect()`:

- Runs every 10 seconds (`SleepTick`)
- Calls `get_status` via `callTool`
- Parses response, updates `m.state` under write lock
- On HTTP failure: logs error, continues (next poll retries)
- On `session_invalid`: triggers auto-re-login before next poll
- Stopped via `pollCancel` when `Close()` is called

This keeps `state.InCombat`, `state.Traveling`, `state.Docked`, `state.CurrentTick` fresh. Strategies that check state between actions work without changes. Also naturally prevents session expiry (30 min timeout).

Chat messages, trade offers, etc. are NOT polled — agents that need them call `get_chat_history` or `get_trades` explicitly.

### Lifecycle

1. `NewMCPGameClient(serverURL, username, password, logger)` — creates client
2. `Connect()` — sends MCP `initialize` handshake, starts background poller
3. `Login()` — calls `login` tool, stores `sessionID`, poller begins fetching
4. Commands update state inline via `updateStateFromResult` (supplements polling)
5. `Close()` — cancels poller, waits for goroutine exit, fires `OnDisconnected`

### MessageHandler

`SetHandler(h)` stores the handler. Only lifecycle events fire:
- `OnConnected(state)` — after successful `Login()`
- `OnDisconnected(err)` — on `Close()` or unrecoverable error
- `OnMessage` — not called (no event stream)

## File Changes

### New Files

| File | Purpose |
|------|---------|
| `pkg/game/mcp_game_client.go` | Struct, `callTool`, `Connect`, `Close`, `Login`, `Register`, poller, session management |
| `pkg/game/mcp_game_client_commands.go` | All `GameClient` method implementations |
| `pkg/game/mcp_sse.go` | SSE response parser |
| `pkg/game/mcp_game_client_test.go` | Tests with mock HTTP server |

### Modified Files

| File | Change |
|------|--------|
| `pkg/strategy/strategy.go` | Change `Strategy.Run` signature: `*game.Client` → `game.GameClient` |
| `pkg/strategy/mining.go` | Accept `GameClient` interface |
| `pkg/strategy/explorer.go` | Accept `GameClient` interface |
| `pkg/strategy/trader.go` | Accept `GameClient` interface |
| `pkg/strategy/fighter.go` | Accept `GameClient` interface |
| `pkg/strategy/idle.go` | Accept `GameClient` interface |
| `cmd/auto-miner/main.go` | Add `--transport=mcp` flag, construct `MCPGameClient` when selected |
| `cmd/auto-explorer/main.go` | Same flag |
| `cmd/auto-trader/main.go` | Same flag |
| `cmd/auto-fighter/main.go` | Same flag |
| (all other `cmd/auto-*`) | Same flag |

### Unchanged

| File/Package | Why |
|------|-----|
| `pkg/game/interface.go` | MCPGameClient implements existing interface |
| `pkg/agent/runner.go` | Already uses `GameClient` interface |
| `pkg/knowledge/` | No impact |
| `pkg/observe/` | Deferred — stays on WebSocket |
| `pkg/game/mcp_client.go` | Unrelated — local stdio client for crafting server |
| `cmd/bridge/` | Unrelated — bridges for external AI clients |

## Phased Rollout

### Phase 1: Core MCP Client
- Build `MCPGameClient` with `callTool`, SSE parsing, session management
- Implement `Connect`, `Close`, `Login`, `Register`, `GetState`
- Background poller
- Tests with mock HTTP server

### Phase 2: Command Methods
- Implement all 111 `GameClient` methods
- Map each to corresponding MCP tool name + arguments
- Verify against OpenAPI spec for parameter names

### Phase 3: Strategy Interface Refactor
- Change `Strategy.Run` to accept `game.GameClient` interface
- Update all strategy implementations
- Verify builds

### Phase 4: Agent Binary Integration
- Add `--transport=mcp` flag to `auto-miner` first
- Test with live gameplay
- Roll out to remaining binaries

### Phase 5: Cleanup (later, after validation)
- Remove `--transport` flag, make MCP the default
- Remove WebSocket client code
- Remove connection health monitoring, waiter channels, CommandQueue, ReconnectingHandler

## Out of Scope

- Observer/frontend migration (separate follow-up)
- MCP bridge services (`cmd/bridge/`) — unrelated
- Local MCP client (`mcp_client.go`) — unrelated
- WebSocket code deletion — kept until MCP proven stable
