# SpaceMolt Status Registry

> HTTP-based status registry for tracking tool availability and health monitoring.

## Overview

The status-registry is a lightweight HTTP server that provides a centralized registry for tracking tool availability and health. It implements a REST API for tool registration, heartbeat monitoring, and status streaming via Server-Sent Events (SSE). Perfect for distributed systems where multiple tools need to advertise their availability.

## Features

### Core Functionality
- **📋 Tool Registration** - Tools can register themselves with metadata
- **💓 Heartbeat Monitoring** - Automatic cleanup of inactive tools
- **📡 SSE Streaming** - Real-time status updates via Server-Sent Events
- **🔍 Tool Discovery** - Query available tools and their status
- **🧹 Auto-Cleanup** - Removes inactive tools based on heartbeat timeout

### API Endpoints

- **POST /api/tools/register** - Register a new tool
- **GET /api/tools** - List all registered tools
- **GET /api/tools/{id}** - Get details for a specific tool
- **POST /api/tools/{id}/heartbeat** - Send heartbeat for a tool
- **GET /api/status/stream** - SSE stream of status updates

## Quick Start

### Basic Usage

```bash
# Start registry with default settings
go run ./cmd/status-registry

# Build and run
go build -o bin/status-registry ./cmd/status-registry
./bin/status-registry
```

### Building

```bash
# Build the binary
go build -o bin/status-registry ./cmd/status-registry

# Run with custom port
./bin/status-registry -port 8081
```

## Command-Line Flags

```
-port int
    Status registry HTTP port (default 8081)

-data-dir string
    Data directory for persistence (future feature) (default "data/registry")

-cleanup-interval duration
    Interval between cleanup tasks (default 30s)

-heartbeat-timeout duration
    Heartbeat timeout before removing inactive tools (default 15s)
```

## Examples

### Example 1: Start Registry

```bash
go run ./cmd/status-registry
```

**Output:**
```
=== SpaceMolt Status Registry ===
Port: 8081
Data directory: data/registry
✓ Registry server created
✓ Cleanup task started (interval: 30s, timeout: 15s)
=== Status Registry Running ===
Listening on http://localhost:8081
Endpoints:
  POST   /api/tools/register         - Register a tool
  GET    /api/tools                  - List all tools
  GET    /api/tools/{id}             - Get tool details
  POST   /api/tools/{id}/heartbeat  - Send heartbeat
  GET    /api/status/stream         - SSE status stream
```

### Example 2: Custom Configuration

```bash
# Use custom port and timeouts
go run ./cmd/status-registry -port 9000 -cleanup-interval 1m -heartbeat-timeout 30s
```

### Example 3: Register a Tool

```bash
curl -X POST http://localhost:8081/api/tools/register \
  -H "Content-Type: application/json" \
  -d '{
    "id": "mining-bot-1",
    "name": "Mining Bot 1",
    "type": "agent",
    "status": "running",
    "metadata": {
      "system": "sol",
      "poi": "asteroid_belt_1"
    }
  }'
```

**Response:**
```json
{
  "success": true,
  "tool": {
    "id": "mining-bot-1",
    "name": "Mining Bot 1",
    "type": "agent",
    "status": "running",
    "registered_at": "2026-02-23T10:15:30Z",
    "last_heartbeat": "2026-02-23T10:15:30Z",
    "metadata": {
      "system": "sol",
      "poi": "asteroid_belt_1"
    }
  }
}
```

### Example 4: List All Tools

```bash
curl http://localhost:8081/api/tools
```

**Response:**
```json
{
  "tools": [
    {
      "id": "mining-bot-1",
      "name": "Mining Bot 1",
      "type": "agent",
      "status": "running",
      "registered_at": "2026-02-23T10:15:30Z",
      "last_heartbeat": "2026-02-23T10:15:35Z"
    },
    {
      "id": "trading-bot-2",
      "name": "Trading Bot 2",
      "type": "agent",
      "status": "running",
      "registered_at": "2026-02-23T10:16:00Z",
      "last_heartbeat": "2026-02-23T10:16:05Z"
    }
  ],
  "count": 2
}
```

### Example 5: Send Heartbeat

```bash
curl -X POST http://localhost:8081/api/tools/mining-bot-1/heartbeat
```

**Response:**
```json
{
  "success": true,
  "last_heartbeat": "2026-02-23T10:15:40Z"
}
```

### Example 6: Status Streaming

```bash
curl -N http://localhost:8081/api/status/stream
```

**Output (SSE stream):**
```
data: {"type":"register","tool":{"id":"mining-bot-1","name":"Mining Bot 1",...}}

data: {"type":"heartbeat","tool_id":"mining-bot-1","timestamp":"2026-02-23T10:15:40Z"}

data: {"type":"timeout","tool_id":"mining-bot-1","timestamp":"2026-02-23T10:16:10Z"}
```

## API Reference

### POST /api/tools/register

Register a new tool.

**Request:**
```json
{
  "id": "tool-id",
  "name": "Tool Name",
  "type": "agent|service|utility",
  "status": "running|idle|error",
  "metadata": {
    "key": "value"
  }
}
```

**Response:**
```json
{
  "success": true,
  "tool": {...}
}
```

### GET /api/tools

List all registered tools.

**Response:**
```json
{
  "tools": [...],
  "count": 10
}
```

### GET /api/tools/{id}

Get details for a specific tool.

**Response:**
```json
{
  "id": "tool-id",
  "name": "Tool Name",
  "type": "agent",
  "status": "running",
  "registered_at": "2026-02-23T10:15:30Z",
  "last_heartbeat": "2026-02-23T10:15:35Z",
  "metadata": {...}
}
```

### POST /api/tools/{id}/heartbeat

Send heartbeat for a tool.

**Response:**
```json
{
  "success": true,
  "last_heartbeat": "2026-02-23T10:15:40Z"
}
```

### GET /api/status/stream

SSE stream of status updates.

**Events:**
- `register` - New tool registered
- `heartbeat` - Tool heartbeat received
- `timeout` - Tool timed out (removed)

## How It Works

### Architecture

```
┌─────────────────┐
│   HTTP Server   │
└────────┬────────┘
         │
         ├──────────────────────────┐
         │                          │
    ┌────▼────┐              ┌─────▼─────┐
    │ Registry│              │  Cleanup  │
    │  Store  │              │   Task    │
    └────┬────┘              └───────────┘
         │
         ├──────────────────────────┐
         │                          │
    ┌────▼─────┐              ┌────▼────┐
    │   API    │              │   SSE   │
    │ Handlers │              │ Stream  │
    └──────────┘              └─────────┘
```

### Tool Lifecycle

```
1. Register Tool
   ↓
2. Send Heartbeats (periodically)
   ↓
3. Timeout if no heartbeat
   ↓
4. Tool removed from registry
```

### Cleanup Process

The cleanup task runs periodically to remove inactive tools:
1. Iterates through all registered tools
2. Checks if last heartbeat exceeds timeout threshold
3. Removes inactive tools
4. Sends timeout event on SSE stream

## Client Integration

### Go Example

```go
package main

import (
    "bytes"
    "encoding/json"
    "net/http"
)

func registerTool(id, name, toolType string) error {
    data := map[string]any{
        "id":     id,
        "name":   name,
        "type":   toolType,
        "status": "running",
    }
    body, _ := json.Marshal(data)

    resp, err := http.Post(
        "http://localhost:8081/api/tools/register",
        "application/json",
        bytes.NewReader(body),
    )
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    return nil
}

func sendHeartbeat(id string) error {
    _, err := http.Post(
        "http://localhost:8081/api/tools/"+id+"/heartbeat",
        "application/json",
        nil,
    )
    return err
}
```

### Python Example

```python
import requests
import time

registry_url = "http://localhost:8081"

def register_tool(tool_id, name, tool_type):
    data = {
        "id": tool_id,
        "name": name,
        "type": tool_type,
        "status": "running"
    }
    response = requests.post(f"{registry_url}/api/tools/register", json=data)
    return response.json()

def send_heartbeat(tool_id):
    response = requests.post(f"{registry_url}/api/tools/{tool_id}/heartbeat")
    return response.json()

# Register and send heartbeats
register_tool("bot-1", "Bot 1", "agent")
while True:
    send_heartbeat("bot-1")
    time.sleep(5)
```

## Configuration

### Timeouts

Adjust timeouts based on your needs:

- **Heartbeat Interval** - How often tools send heartbeats (e.g., 10s)
- **Heartbeat Timeout** - How long before tool is considered inactive (e.g., 30s)
- **Cleanup Interval** - How often cleanup runs (e.g., 60s)

**Rule of thumb:** Heartbeat timeout should be 2-3x heartbeat interval.

### Port Selection

Default port is 8081. Choose a different port if needed:

```bash
go run ./cmd/status-registry -port 9000
```

## Monitoring

### Health Checks

Check registry health:

```bash
curl http://localhost:8081/api/tools
```

### Tool Count Monitoring

Monitor number of registered tools:

```bash
# Count tools
curl -s http://localhost:8081/api/tools | jq '.count'
```

### SSE Monitoring

Monitor real-time events:

```bash
curl -N http://localhost:8081/api/status/stream
```

## Troubleshooting

### Issue: "Port already in use"

**Cause:** Another process is using the port.

**Solution:**
1. Find process using port: `lsof -i :8081`
2. Kill process or use different port: `-port 8082`

### Issue: "Tools timing out"

**Cause:** Tools not sending heartbeats frequently enough.

**Solution:**
1. Increase heartbeat timeout: `-heartbeat-timeout 30s`
2. Ensure tools send heartbeats more frequently
3. Check network connectivity

### Issue: "High memory usage"

**Cause:** Too many tools registered or not cleaning up properly.

**Solution:**
1. Reduce heartbeat timeout: `-heartbeat-timeout 10s`
2. Increase cleanup frequency: `-cleanup-interval 10s`
3. Monitor tool count regularly

## Best Practices

### For Tool Developers

1. **Register on startup** - Register when tool starts
2. **Send heartbeats regularly** - Use a timer/goroutine
3. **Handle errors gracefully** - Log but don't crash on registry errors
4. **Re-register on reconnect** - If registry restarts, re-register

### For Registry Operators

1. **Monitor tool count** - Alert on unusual counts
2. **Set appropriate timeouts** - Balance responsiveness and tolerance
3. **Log events** - Keep track of registrations and timeouts
4. **Backup data** - If persistence is added, backup regularly

## Related Documentation

- [Registry Package](../../pkg/registry/) - Registry implementation
- [HTTP Handlers](../../pkg/registry/server.go) - HTTP handler implementation
- [Agent Manager](../../pkg/agent/manager.go) - Agent management integration

## License

Part of the SpaceMolt project.
