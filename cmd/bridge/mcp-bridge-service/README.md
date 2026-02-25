# MCP Bridge Service - Multi-Agent Connection Pool

A continuously running MCP server that manages persistent game connections for multiple agents.

## Features

- **Connection Pooling**: Single service manages connections for all agents
- **Persistent Sessions**: Connections survive Claude Code restarts
- **Auto-Reconnection**: Automatic reconnection with exponential backoff
- **Lazy Initialization**: Agents connect on first use
- **Idle Cleanup**: Automatically closes unused connections after timeout
- **Agent ID Parameter**: All tools accept `agent_id` to route to correct connection

## Architecture

```
Claude Code
    ↓ (stdio MCP)
mcp-bridge-service
    ├─ Connection Pool
    │  ├─ pirate-4 → WebSocket connection
    │  ├─ explorer-1 → WebSocket connection
    │  └─ miner-3 → WebSocket connection
    ↓
game.spacemolt.com/ws
```

### Key Components

- **ConnectionPool**: Manages all agent connections
- **AgentConnection**: Per-agent state (client, credentials, session)
- **Keep-Alive**: Background goroutine maintains each connection
- **Auto-Reconnect**: Handles disconnections with backoff retry

## Configuration

### Service Configuration

`bridge-service.json`:
```json
{
  "agents_dir": "data/agents",
  "game_server_url": "wss://game.spacemolt.com/ws",
  "idle_timeout": "15m",
  "max_connections": 100,
  "keep_alive_interval": "30s",
  "reconnect_attempts": 5
}
```

### Agent Credentials

Each agent needs a credentials file at:
```
data/agents/{agent-id}/credentials.json
```

Format:
```json
{
  "username": "☣",
  "password": "hex_password_hash",
  "empire": "crimson"
}
```

### Claude Code Configuration

`~/.claude/settings.json`:
```json
{
  "mcpServers": {
    "spacemolt": {
      "command": "/path/to/bin/mcp-bridge-service",
      "args": [
        "-config",
        "/path/to/bridge-service.json",
        "-verbose"
      ]
    }
  }
}
```

## Usage

### Building

```bash
go build -o bin/mcp-bridge-service ./cmd/mcp-bridge-service
```

### Running Standalone

```bash
# With default config (bridge-service.json in current directory)
./bin/mcp-bridge-service

# With custom config
./bin/mcp-bridge-service -config /path/to/config.json

# With verbose logging
./bin/mcp-bridge-service -verbose
```

### Using from Claude Code

All tools now accept `agent_id` as the first parameter:

```javascript
// Get status for pirate-4
{
  "name": "get_status",
  "arguments": {
    "agent_id": "pirate-4"
  }
}

// Travel with explorer-1
{
  "name": "travel",
  "arguments": {
    "agent_id": "explorer-1",
    "poi_id": "asteroid_belt_1"
  }
}

// Accept mission for miner-3
{
  "name": "accept_mission",
  "arguments": {
    "agent_id": "miner-3",
    "mission_id": "mining_contract_42"
  }
}
```

## Available Tools

All tools require `agent_id` parameter:

### Status & Information
- `get_status` - Current agent status (location, credits, fuel, etc.)
- `get_ships` - List owned ships
- `get_system` - Current system information and POIs
- `get_active_missions` - Active missions
- `get_missions` - Available missions at station

### Navigation
- `travel` - Travel to POI in current system
- `jump` - Jump to another star system
- `find_route` - Calculate route between systems
- `dock` - Dock at station
- `undock` - Undock from station

### Ship Management
- `switch_ship` - Switch to different ship
- `refuel` - Refuel at station
- `repair` - Repair hull at station

### Missions
- `accept_mission` - Accept a mission
- (More mission tools coming...)

### Captain's Log
- `captains_log_add` - Add log entry
- `captains_log_get` - Get recent log entries

## Connection Lifecycle

1. **First Request**: Agent connection created on demand
2. **Login**: Automatic connection and authentication
3. **Active**: Connection maintained with keep-alive pings
4. **Idle**: After 15 minutes of inactivity (configurable)
5. **Cleanup**: Idle connections automatically closed
6. **Reconnect**: Failed connections auto-reconnect with backoff

## Benefits vs. Per-Agent Bridge

### Old Architecture (mcp-ws-bridge)
- ❌ One process per Claude Code session
- ❌ No session persistence
- ❌ Manual reconnection required
- ❌ 90 agents = potential 90 processes

### New Architecture (mcp-bridge-service)
- ✅ Single long-running process
- ✅ Persistent sessions across restarts
- ✅ Automatic reconnection
- ✅ 90 agents = 1 process with connection pool
- ✅ Lazy connection (only connect when needed)
- ✅ Resource efficient with idle cleanup

## Monitoring

### Connection Status

The service logs connection events to stderr:

```
[bridge] Created connection for agent pirate-4 (☣)
[pirate-4] Connected successfully as ☣
[pirate-4] Keep-alive check passed
[pirate-4] Reconnecting (attempt 1/5)...
[bridge] Closing idle connection for explorer-1 (idle: 16m)
```

### Troubleshooting

**Connection fails:**
- Check credentials file exists at `data/agents/{agent-id}/credentials.json`
- Verify game server is reachable
- Check network connectivity

**Tool call errors:**
- Ensure `agent_id` parameter is provided
- Verify agent credentials are valid
- Check connection pool hasn't hit max_connections limit

**Performance issues:**
- Adjust `idle_timeout` to reduce memory usage
- Lower `max_connections` if hitting resource limits
- Increase `keep_alive_interval` to reduce network overhead

## Development

### Adding New Tools

1. Add tool definition to `toolDefinitions` in `tools.go`
2. Add case to `executeTool()` switch statement
3. Implement game command using `conn.client`
4. Rebuild service

Example:
```go
{
    "name": "scan_nearby",
    "description": "Scan nearby ships",
    "inputSchema": map[string]any{
        "type": "object",
        "properties": map[string]any{
            "agent_id": map[string]any{
                "type": "string",
                "description": "Agent ID",
            },
        },
        "required": []string{"agent_id"},
    },
}

// In executeTool()
case "scan_nearby":
    if err := conn.client.Send(ctx, protocol.Message{
        Type: "get_nearby",
    }); err != nil {
        return "", err
    }
    time.Sleep(1 * time.Second)
    state := conn.client.GetState()
    data, _ := json.MarshalIndent(state.Nearby, "", "  ")
    return string(data), nil
```

## License

Part of the spacemolt-agent-server project.
