# Watcher-Agent-Server Integration Testing Guide

## Overview

The watcher and agent-server integration allows the watcher TUI to connect to a remote agent-server via HTTP API and Server-Sent Events (SSE). This enables:

- Multiple watchers observing the same agents
- Separation of agent execution from monitoring
- Historical action tracking
- Real-time event streaming

## What Was Implemented

### ✅ Phase 1: HTTP API Foundation (agent-server)
- **pkg/api/server.go** - HTTP server initialization and routing
- **pkg/api/handlers.go** - REST endpoint implementations
  - `GET /api/agents` - List all active agents
  - `GET /api/agents/{id}` - Get agent details + status
  - `GET /api/agents/{id}/state` - Get full game state
  - `GET /api/agents/{id}/history?limit=N` - Get action history

### ✅ Phase 2: History Tracking (agent-server)
- **pkg/agent/history.go** - Ring buffer for action history
  - Tracks last 1000 actions per agent
  - Includes tick, action, target, confidence, result, reasoning
  - Efficient GetRecent() and GetSince() queries

### ✅ Phase 3: Real-Time Streaming (agent-server)
- **pkg/api/stream.go** - SSE stream manager
  - `GET /api/agents/{id}/stream` - Subscribe to agent events
  - Event types: decision, action, error, state_update
  - Non-blocking publish with buffered channels

### ✅ Phase 4: HTTP Client (watcher)
- **pkg/tui/agentserver_client.go** - HTTP client for watcher
  - ListAgents() - Fetch agent list from server
  - GetState() - Fetch game state for specific agent
  - GetHistory() - Fetch action history
  - StreamEvents() - Subscribe to SSE stream

### ✅ Phase 5: Watcher Integration
- **cmd/watcher/main.go** - Remote mode support
  - `--agent-server-url` flag for remote mode
  - Mode detection (local vs remote)
  - SSE subscription and state polling
- **pkg/tui/model.go** - Remote mode in TUI
  - SetRemoteMode() method
  - Supports both local and remote agents

### ✅ Phase 6: Agent-Server HTTP API
- **cmd/agent-server/main.go** - HTTP server integration
  - `--http-port` flag to enable API (optional)
  - Event callback wiring to stream manager
  - Graceful shutdown support

## Testing Scenarios

### Scenario 1: Basic Remote Mode

#### Terminal 1: Start agent-server with HTTP API
```bash
go build -o agent-server ./cmd/agent-server
./agent-server \
  --agents=explorer-7 \
  --http-port=8080 \
  --db-backend=memory
```

Expected output:
```
✓ HTTP API listening on :8080
✓ Successfully started: 1/1 agents
```

#### Terminal 2: Start watcher in remote mode
```bash
go build -o watcher ./cmd/watcher
./watcher --agent-server-url=http://localhost:8080
```

Expected behavior:
- Watcher connects to agent-server
- Fetches list of agents (explorer-7)
- Displays agent in TUI
- Shows real-time action logs via SSE
- Displays game state (fuel, hull, cargo, POIs)

### Scenario 2: Multiple Watchers

Start agent-server in Terminal 1 (same as above), then:

#### Terminal 2: Watcher #1
```bash
./watcher --agent-server-url=http://localhost:8080
```

#### Terminal 3: Watcher #2
```bash
./watcher --agent-server-url=http://localhost:8080
```

Expected behavior:
- Both watchers display the same agent
- Both receive real-time updates
- Actions appear in both UIs simultaneously

### Scenario 3: Backward Compatibility

#### Local Mode (existing behavior)
```bash
./watcher --agents=explorer-7
```

Should work exactly as before (no remote connection).

#### Headless Agent-Server (existing behavior)
```bash
./agent-server --agents=explorer-7,miner-2
```

Should work without HTTP API (no --http-port flag).

### Scenario 4: HTTP API Verification

With agent-server running on port 8080:

```bash
# List agents
curl http://localhost:8080/api/agents | jq

# Get specific agent details
curl http://localhost:8080/api/agents/explorer-7 | jq

# Get game state
curl http://localhost:8080/api/agents/explorer-7/state | jq

# Get action history (last 10)
curl http://localhost:8080/api/agents/explorer-7/history?limit=10 | jq

# Subscribe to SSE stream (will stream events)
curl -N http://localhost:8080/api/agents/explorer-7/stream
```

### Scenario 5: Error Handling

#### Agent-server not running
```bash
./watcher --agent-server-url=http://localhost:9999
```

Expected:
```
Failed to connect to agent-server: server unreachable: ...
```

#### No agents on server
Start agent-server with no agents, then start watcher:

Expected:
```
No agents available on agent-server
```

## API Endpoint Reference

### GET /api/agents
Returns list of all active agents.

**Response:**
```json
[
  {
    "id": "explorer-7",
    "name": "Navigator Zephyr",
    "role": "Exploration Specialist",
    "status": "Idle"
  }
]
```

### GET /api/agents/{id}
Returns detailed agent information.

**Response:**
```json
{
  "id": "explorer-7",
  "name": "Navigator Zephyr",
  "role": "Exploration Specialist",
  "status": "Idle",
  "current_action": "scan",
  "last_action_tick": 12345,
  "is_running": true,
  "has_crashed": false,
  "crash_count": 0,
  "faction": "solarian"
}
```

### GET /api/agents/{id}/state
Returns full game state for the agent.

**Response:** (game.State JSON)
```json
{
  "current_tick": 12345,
  "fuel": 45.5,
  "max_fuel": 100.0,
  "hull": 98.0,
  "max_hull": 100.0,
  "cargo": [...],
  "system": {...},
  ...
}
```

### GET /api/agents/{id}/history?limit=N
Returns last N action history entries (max 1000).

**Response:**
```json
[
  {
    "tick": 12340,
    "timestamp": "2024-01-15T10:30:45Z",
    "action": "travel",
    "target": "Asteroid Belt Alpha",
    "confidence": 0.85,
    "result": "success",
    "reasoning": "Moving to mining location"
  },
  {
    "tick": 12350,
    "timestamp": "2024-01-15T10:31:05Z",
    "action": "mine",
    "target": "",
    "confidence": 0.92,
    "result": "success",
    "reasoning": "Rich mineral deposits detected"
  }
]
```

### GET /api/agents/{id}/stream
Server-Sent Events stream of real-time agent events.

**Event Types:**
- `connected` - Initial connection established
- `decision` - Agent made a decision
- `action` - Action executed (success)
- `error` - Action failed
- `state_update` - Game state changed

**Example SSE Output:**
```
event: connected
data: {"agent_id":"explorer-7","status":"connected"}

event: decision
data: {"agent_id":"explorer-7","type":"decision","timestamp":"...","data":{"action":"scan","confidence":0.75,"reasoning":"Checking for resources"}}

event: action
data: {"agent_id":"explorer-7","type":"action","timestamp":"...","data":{"action":"scan","status":"success","tick":12355}}
```

## Configuration Flags

### Agent-Server
- `--http-port=PORT` - Enable HTTP API (0 = disabled, default)
- All existing flags remain unchanged

### Watcher
- `--agent-server-url=URL` - Connect to remote agent-server (remote mode)
- `--agents=LIST` - Spawn agents locally (local mode)
- Cannot use both simultaneously

## Architecture Notes

### Event Flow (Remote Mode)
```
Agent Runner → Event Callback → Stream Manager → SSE → Watcher TUI
     ↓
  History Buffer
     ↓
  HTTP API (/history)
```

### Data Flow
1. **Agent-Server** spawns and manages agent runners
2. **Runners** emit events via callbacks to stream manager
3. **Stream Manager** publishes events to SSE subscribers
4. **Watcher** subscribes to SSE stream and polls state
5. **TUI** updates in real-time from SSE events

### Backward Compatibility
- HTTP API is **optional** (only when --http-port provided)
- Watcher works in **local mode** (without --agent-server-url)
- No breaking changes to existing CLIs

## Known Limitations

1. **No authentication** - HTTP API is open (add JWT/API keys in future)
2. **Single server** - Cannot load balance across multiple agent-servers
3. **No reconnection** - Watcher doesn't auto-reconnect on disconnect (planned)
4. **State polling** - Game state fetched every 5 seconds (could be optimized)

## Success Criteria

✅ Watcher can display agents from remote agent-server
✅ Action log shows cached command/response history
✅ Ship and system details render correctly from remote state
✅ Real-time updates via SSE work without polling
✅ Backward compatibility maintained (both tools work standalone)
✅ Multiple watchers can connect to same agent-server
✅ HTTP API endpoints return correct JSON data
✅ Graceful error handling for disconnections

## Next Steps (Future Enhancements)

- Add authentication (API keys or JWT)
- Implement watcher auto-reconnection on disconnect
- Add WebSocket support as alternative to SSE
- Create agent control endpoints (POST /api/agents/{id}/stop, etc.)
- Add metrics endpoint (/api/metrics)
- Support filtering history by action type
- Add pagination for history endpoint
