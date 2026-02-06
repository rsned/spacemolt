# Watcher-Agent-Server Integration Plan

## Overview

Refactor the watcher to communicate with agent-server via HTTP API instead of spawning agents directly. The agent-server will cache command/response history and expose agent state for TUI rendering.

## Current Architecture Issues

1. **Watcher spawns agents directly** (`cmd/watcher/main.go`) - creates game clients, manages credentials
2. **Agent-server has no HTTP API** (`cmd/agent-server/main.go`) - headless CLI only
3. **No IPC between processes** - completely independent operation
4. **No command/response history** - only live state updates

## Goals

1. ✅ Watcher talks only to agent-server (no direct agent spawning in remote mode)
2. ✅ Agent-server exposes HTTP API for agent discovery, state, and history
3. ✅ Agent-server caches last N commands/responses per agent
4. ✅ Watcher fetches active agents from agent-server when no `--agents` flag
5. ✅ Real-time updates via Server-Sent Events (SSE)
6. ✅ Backward compatibility maintained

## Architecture Design

### Communication Protocol

**REST + Server-Sent Events (SSE)**
- REST for queries: list agents, get state, get history
- SSE for real-time: action logs, state updates, errors
- Go standard library only (`net/http`) - no new dependencies

### Data Flow

```
┌──────────┐    HTTP GET /api/agents     ┌──────────────┐
│ Watcher  │ ◄─────────────────────────► │ Agent-Server │
│  (TUI)   │    SSE /api/agents/{id}/stream  (HTTP API)   │
└──────────┘                              └──────────────┘
                                                │
                                                │ manages
                                                ▼
                                          ┌──────────────┐
                                          │   Runners    │
                                          │  + History   │
                                          └──────────────┘
```

## Implementation Plan

### Phase 1: HTTP API Foundation (agent-server)

#### 1.1 Create HTTP Server Package
**File:** `pkg/api/server.go`

```go
package api

type Server struct {
    manager *agent.Manager
    router  *http.ServeMux
    server  *http.Server
}

func NewServer(manager *agent.Manager, port int) *Server
func (s *Server) Start() error
func (s *Server) Shutdown(ctx context.Context) error
```

**File:** `pkg/api/handlers.go`

Implement REST endpoints:
- `GET /api/agents` - List all active agents
- `GET /api/agents/{id}` - Get agent details + status
- `GET /api/agents/{id}/state` - Get full game state
- `GET /api/agents/{id}/history?limit=100` - Get action history

#### 1.2 Add HTTP Flag to Agent-Server
**File:** `cmd/agent-server/main.go` (line ~46)

Add flag:
```go
httpPort = flag.Int("http-port", 0, "Enable HTTP API on port (0 = disabled)")
```

Add startup logic (after line 110):
```go
// Optionally start HTTP API
var apiServer *api.Server
if *httpPort > 0 {
    apiServer = api.NewServer(mgr, *httpPort)
    go func() {
        log.Printf("✓ HTTP API listening on :%d", *httpPort)
        if err := apiServer.Start(); err != nil {
            log.Printf("HTTP server error: %v", err)
        }
    }()
}
```

Add shutdown (after line 174):
```go
if apiServer != nil {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    apiServer.Shutdown(shutdownCtx)
}
```

### Phase 2: History Tracking (agent-server)

#### 2.1 Create History Data Structure
**File:** `pkg/agent/history.go` (NEW)

```go
package agent

type HistoryEntry struct {
    Tick       int64       `json:"tick"`
    Timestamp  time.Time   `json:"timestamp"`
    Action     string      `json:"action"`      // "mine", "travel", etc.
    Target     string      `json:"target"`      // POI/system name
    Confidence float64     `json:"confidence"`  // Decision confidence (0-1)
    Result     string      `json:"result"`      // "success", "error", "pending"
    Error      string      `json:"error,omitempty"`
    Reasoning  string      `json:"reasoning"`   // LLM reasoning
}

type History struct {
    mu      sync.RWMutex
    entries []HistoryEntry
    maxSize int
}

func NewHistory(maxSize int) *History
func (h *History) Add(entry HistoryEntry)
func (h *History) GetRecent(limit int) []HistoryEntry
func (h *History) GetSince(tick int64) []HistoryEntry
```

#### 2.2 Add History to Runner
**File:** `pkg/agent/runner.go` (modify)

Add field to Runner struct (line ~30):
```go
history *History
```

Initialize in NewRunner (line ~60):
```go
history: NewHistory(1000), // Last 1000 actions
```

Add method to record actions (line ~110 in executeCycle):
```go
func (r *Runner) recordAction(decision Decision, result string, err error) {
    entry := HistoryEntry{
        Tick:       r.gameClient.GetState().CurrentTick,
        Timestamp:  time.Now(),
        Action:     decision.Action,
        Target:     decision.Target,
        Confidence: decision.Confidence,
        Result:     result,
        Reasoning:  decision.Reasoning,
    }
    if err != nil {
        entry.Error = err.Error()
    }
    r.history.Add(entry)
}
```

Add getter:
```go
func (r *Runner) GetHistory(limit int) []HistoryEntry {
    return r.history.GetRecent(limit)
}
```

Call recordAction in executeCycle (after action execution).

### Phase 3: Real-Time Streaming (agent-server)

#### 3.1 Create Stream Manager
**File:** `pkg/api/stream.go` (NEW)

```go
package api

type StreamManager struct {
    mu          sync.RWMutex
    subscribers map[string][]chan Event
}

type Event struct {
    AgentID   string      `json:"agent_id,omitempty"`
    Type      string      `json:"type"`
    Timestamp time.Time   `json:"timestamp"`
    Data      interface{} `json:"data"`
}

func NewStreamManager() *StreamManager
func (sm *StreamManager) Subscribe(agentID string) <-chan Event
func (sm *StreamManager) Unsubscribe(agentID string, ch <-chan Event)
func (sm *StreamManager) Publish(agentID string, event Event)
```

#### 3.2 Implement SSE Endpoints
**File:** `pkg/api/handlers.go` (add)

```go
// GET /api/agents/{id}/stream
func (s *Server) handleAgentStream(w http.ResponseWriter, r *http.Request)

// SSE format:
// event: action
// data: {"tick": 123, "action": "mine", "status": "executing"}
//
// event: state_update
// data: {"fuel": 45.0, "cargo": [...]}
```

#### 3.3 Integrate with Runner
**File:** `pkg/agent/runner.go` (modify)

Add observer callback to Runner:
```go
type EventCallback func(agentID string, eventType string, data interface{})

type Runner struct {
    // ...existing fields...
    eventCallback EventCallback
}

func (r *Runner) SetEventCallback(cb EventCallback)
```

Emit events in executeCycle:
```go
if r.eventCallback != nil {
    r.eventCallback(r.agent.ID(), "action", map[string]interface{}{
        "action": decision.Action,
        "target": decision.Target,
    })
}
```

Connect in api.Server initialization.

### Phase 4: Watcher HTTP Client (watcher)

#### 4.1 Create Agent-Server Client
**File:** `pkg/tui/agentserver_client.go` (NEW)

```go
package tui

type AgentServerClient struct {
    baseURL    string
    httpClient *http.Client
}

func NewAgentServerClient(baseURL string) *AgentServerClient

type AgentInfo struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Role        string `json:"role"`
    Status      string `json:"status"`
}

func (c *AgentServerClient) ListAgents() ([]AgentInfo, error)
func (c *AgentServerClient) GetState(agentID string) (*game.State, error)
func (c *AgentServerClient) GetHistory(agentID string, limit int) ([]HistoryEntry, error)
func (c *AgentServerClient) StreamEvents(agentID string) (<-chan Event, error)
```

#### 4.2 Add Flag to Watcher
**File:** `cmd/watcher/main.go` (line ~28)

Add flag:
```go
agentServerURL = flag.String("agent-server-url", "", "Agent-server HTTP API URL (e.g., http://localhost:8080)")
```

### Phase 5: Watcher Integration (watcher)

#### 5.1 Modify Main Logic
**File:** `cmd/watcher/main.go` (line ~108+)

Add mode detection:
```go
// Determine operating mode
var agentWrappers []AgentWrapper
var serverClient *tui.AgentServerClient

if *agentServerURL != "" {
    // Remote mode: connect to agent-server
    debugLogger.Printf("Remote mode: connecting to %s", *agentServerURL)
    serverClient = tui.NewAgentServerClient(*agentServerURL)

    // Fetch agent list
    agentInfos, err := serverClient.ListAgents()
    if err != nil {
        log.Fatalf("Failed to fetch agents from server: %v", err)
    }

    if len(agentInfos) == 0 {
        log.Fatal("No agents available on agent-server")
    }

    // Use server agents (no local spawning)
    debugLogger.Printf("Found %d agents on server", len(agentInfos))

} else if *agents != "" {
    // Local mode: spawn agents directly (existing behavior)
    debugLogger.Printf("Local mode: spawning agents %s", *agents)
    // ...existing agent spawning code...

} else {
    log.Fatal("Must provide either --agent-server-url or --agents")
}
```

#### 5.2 Modify TUI Model
**File:** `pkg/tui/model.go` (modify)

Add field to WatcherModel:
```go
type WatcherModel struct {
    // ...existing fields...
    serverClient *AgentServerClient
    remoteMode   bool
}
```

Update state fetching logic:
```go
func (m *WatcherModel) updateAgentState(agentID string) {
    if m.remoteMode {
        // Fetch from server
        state, err := m.serverClient.GetState(agentID)
        if err != nil {
            m.logError(agentID, err)
            return
        }
        m.agentStates[agentID] = state
    } else {
        // Use local state (existing behavior)
        // ...
    }
}
```

Add SSE subscription in Init():
```go
if m.remoteMode {
    for _, agentID := range m.agentIDs {
        eventCh, _ := m.serverClient.StreamEvents(agentID)
        go m.handleEvents(agentID, eventCh)
    }
}

func (m *WatcherModel) handleEvents(agentID string, eventCh <-chan Event) {
    for event := range eventCh {
        m.program.Send(WsMsg{
            AgentID: agentID,
            Type:    event.Type,
            Data:    event.Data,
        })
    }
}
```

### Phase 6: Testing & Refinement

#### 6.1 Unit Tests
- `pkg/agent/history_test.go` - Ring buffer operations
- `pkg/api/handlers_test.go` - HTTP endpoints
- `pkg/tui/agentserver_client_test.go` - HTTP client

#### 6.2 Integration Testing

**Test Scenario 1: Basic Integration**
```bash
# Terminal 1: Start agent-server with HTTP
./agent-server --agents=explorer-7,miner-2 --http-port=8080

# Terminal 2: Start watcher in remote mode
./watcher --agent-server-url=http://localhost:8080
```

**Test Scenario 2: Backward Compatibility**
```bash
# Watcher in local mode (existing behavior)
./watcher --agents=explorer-7

# Agent-server without HTTP (existing behavior)
./agent-server --agents=miner-2,fighter-1
```

**Test Scenario 3: Error Handling**
```bash
# Watcher connects before agent-server starts
./watcher --agent-server-url=http://localhost:8080
# Should retry with backoff

# Kill agent-server while watcher running
# Watcher should detect disconnect and show error state
```

#### 6.3 Manual Verification Checklist

- [ ] Agent list displays correctly in watcher
- [ ] Action log shows real-time commands/responses
- [ ] Ship details panel updates (fuel, hull, cargo)
- [ ] System map panel shows POIs correctly
- [ ] Status panel shows credits, docked state
- [ ] Multiple watchers can connect to same agent-server
- [ ] Watcher reconnects after agent-server restart
- [ ] History API returns last N actions
- [ ] SSE stream delivers events in order

## Critical Files to Modify

### New Files
1. **pkg/api/server.go** - HTTP server initialization, routing
2. **pkg/api/handlers.go** - REST endpoint implementations
3. **pkg/api/stream.go** - SSE stream manager
4. **pkg/agent/history.go** - History tracking data structure
5. **pkg/tui/agentserver_client.go** - HTTP client for watcher

### Modified Files
1. **cmd/agent-server/main.go** - Add `--http-port` flag, start HTTP server
2. **cmd/watcher/main.go** - Add `--agent-server-url` flag, mode detection
3. **pkg/agent/manager.go** - No changes needed (methods already available)
4. **pkg/agent/runner.go** - Add history field, record actions, event callbacks
5. **pkg/tui/model.go** - Add remote mode support, SSE event handling

## Configuration Flags

### Agent-Server
```bash
--http-port=8080              # Enable HTTP API (0 = disabled)
--history-size=1000           # Max history entries per agent
```

### Watcher
```bash
--agent-server-url=http://localhost:8080  # Remote mode
--agents=explorer-7,miner-2               # Local mode (overrides remote)
```

## Backward Compatibility

### Agent-Server
- HTTP API is **optional** - only enabled with `--http-port` flag
- Without flag, operates as current headless manager
- No breaking changes to existing CLI

### Watcher
- `--agents` flag still works (local mode)
- Local mode behavior unchanged
- New `--agent-server-url` flag for remote mode
- Error if neither flag provided

## API Endpoint Summary

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/agents` | GET | List all active agents |
| `/api/agents/{id}` | GET | Get agent details + status |
| `/api/agents/{id}/state` | GET | Get full game state (player, ship, system) |
| `/api/agents/{id}/history` | GET | Get last N actions (query: ?limit=100) |
| `/api/agents/{id}/stream` | GET | SSE stream of real-time events |

## Success Criteria

1. ✅ Watcher can display agents from remote agent-server
2. ✅ Action log shows cached command/response history
3. ✅ Ship and system details render correctly from remote state
4. ✅ Real-time updates via SSE work without polling
5. ✅ Backward compatibility maintained (both tools work standalone)
6. ✅ Multiple watchers can connect to same agent-server
7. ✅ Graceful error handling for disconnections

## Risk Mitigation

- **Network failures**: Retry logic with exponential backoff
- **Agent crashes**: Continue showing other agents, display error state
- **Server restarts**: Watcher auto-reconnects, refreshes agent list
- **Performance**: Ring buffer limits memory, SSE more efficient than polling
- **Security**: Future: add authentication via API keys/JWT (not in MVP)
