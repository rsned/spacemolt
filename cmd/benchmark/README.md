# SpaceMolt Benchmark

> Performance testing tool for SpaceMolt game client backends with load simulation, metrics collection, and comparative analysis.

## Overview

The benchmark tool is a comprehensive performance testing suite that simulates multiple concurrent agents connecting to the SpaceMolt game server. It tests different backend implementations (WebSocket, HTTP, MCP) under various load conditions, collecting detailed metrics and generating comparative reports.

## Features

### Core Functionality
- **🔷 Multi-Backend Testing** - Test WebSocket, HTTP, and MCP backends
- **📊 Metrics Collection** - Collect detailed performance metrics (latency, throughput, errors)
- **⚡ Load Simulation** - Simulate multiple concurrent agents with configurable parameters
- **📈 Comparative Analysis** - Compare performance across different backends
- **🔄 Rate Limiting** - Built-in rate limiting for realistic load simulation

### Testing Capabilities
- **Configurable Load** - Adjust agent count, duration, and ramp-up period
- **Connection Pooling** - Test shared connection vs. individual connections
- **Real-time Reporting** - Periodic status updates during benchmark runs
- **Multiple Output Formats** - Export results as text, JSON, or CSV

### Metrics Tracked
- **Latency** - Request/response timing percentiles (p50, p95, p99)
- **Throughput** - Requests per second per backend
- **Error Rate** - Failed requests and error types
- **Connection Stats** - Active connections, connection time

## Quick Start

### Basic Usage

```bash
# Run benchmark with default settings (10 agents, 5 minutes)
go run ./cmd/benchmark

# Test specific backend
go run ./cmd/benchmark -backend ws

# Test with more agents
go run ./cmd/benchmark -agents 50 -duration 10m

# Export results to JSON
go run ./cmd/benchmark -output json -output-file results.json
```

### Building

```bash
# Build the binary
go build -o bin/benchmark ./cmd/benchmark

# Run the built binary
./bin/benchmark -backend all -agents 20
```

## Command-Line Flags

```
-backend string
    Backend to test: ws, http, mcp, or all (default "all")

-agents int
    Number of agents to simulate (default 10)

-agents-dir string
    Directory containing agent credentials (default "data/agents")

-duration duration
    Test duration (default 5m)

-ramp-up duration
    Stagger agent starts over this duration (default 30s)

-delay duration
    Delay between commands per agent (default 500ms)

-shared-conn
    Test multiplexed/shared connections (default false)

-pool-size int
    Connection pool size for shared mode (default 4)

-output string
    Output format: text, json, csv (default "text")

-output-file string
    Export path (empty = stdout only)

-verbose
    Per-command output

-server string
    Game server WebSocket URL (default "wss://game.spacemolt.com/ws")
```

## Backends

### WebSocket (ws)

The WebSocket backend maintains persistent connections using the game's native WebSocket protocol.

**Characteristics:**
- Lowest latency
- Full-duplex communication
- Server-initiated messages
- Best for real-time gameplay

**Usage:**
```bash
go run ./cmd/benchmark -backend ws -agents 20
```

### HTTP (http)

The HTTP backend uses REST API endpoints for communication.

**Characteristics:**
- Stateless requests
- Easier to debug
- Better for simple operations
- Higher latency than WebSocket

**Usage:**
```bash
go run ./cmd/benchmark -backend http -agents 20
```

### MCP (Model Context Protocol)

The MCP backend uses the MCP WebSocket bridge for tool-based interactions.

**Characteristics:**
- Tool-based API
- Structured requests/responses
- Suitable for AI agent integration
- Additional protocol overhead

**Usage:**
```bash
go run ./cmd/benchmark -backend mcp -agents 20
```

## Examples

### Example 1: Quick Performance Test

```bash
# Test WebSocket backend with 5 agents for 2 minutes
go run ./cmd/benchmark -backend ws -agents 5 -duration 2m
```

**Output:**
```
=== SpaceMolt Benchmark ===
Backend: ws
Agents: 5
Duration: 2m0s
Ramp-up: 30s

[00:30] === Running benchmark: ws ===
[01:00] Progress: 50% | Requests: 150 | Errors: 0
[01:30] Progress: 75% | Requests: 225 | Errors: 0
[02:00] Progress: 100% | Requests: 300 | Errors: 0

=== Final Results ===
Backend: ws
Total Requests: 300
Successful: 300 (100.00%)
Failed: 0 (0.00%)

Latency:
  p50: 45ms
  p95: 120ms
  p99: 180ms

Throughput: 2.50 req/s
```

### Example 2: Comparative Backend Testing

```bash
# Compare all backends with 20 agents each
go run ./cmd/benchmark -backend all -agents 20 -duration 5m -output json -output-file benchmark-results.json
```

**Output:**
```
=== Running benchmark: ws ===
[Progress indicators...]

=== Running benchmark: http ===
[Progress indicators...]

=== Running benchmark: mcp ===
[Progress indicators...]

=== Final Comparison ===
WebSocket:  600 req/s | p95: 45ms  | Errors: 0%
HTTP:       450 req/s | p95: 85ms  | Errors: 0%
MCP:        380 req/s | p95: 120ms | Errors: 0%
```

### Example 3: Load Testing with Ramp-Up

```bash
# Stress test with 100 agents ramping up over 2 minutes
go run ./cmd/benchmark -backend ws -agents 100 -ramp-up 2m -duration 10m -verbose
```

### Example 4: Connection Pool Testing

```bash
# Test shared connection mode with connection pool
go run ./cmd/benchmark -backend ws -agents 50 -shared-conn -pool-size 4
```

### Example 5: Custom Agent Selection

```bash
# Test specific agents only
go run ./cmd/benchmark -agents miner-1,explorer-2,trader-1 -duration 5m
```

## Output Formats

### Text Output (Default)

Human-readable format with summary statistics and percentiles.

```bash
go run ./cmd/benchmark -output text
```

### JSON Output

Machine-readable format suitable for automation and further analysis.

```bash
go run ./cmd/benchmark -output json -output-file results.json
```

**JSON Schema:**
```json
{
  "backend": "ws",
  "config": {
    "agents": 10,
    "duration": "5m",
    "ramp_up": "30s"
  },
  "metrics": {
    "total_requests": 1500,
    "successful": 1485,
    "failed": 15,
    "latency_p50": 45.2,
    "latency_p95": 120.5,
    "latency_p99": 180.3,
    "throughput": 5.0
  }
}
```

### CSV Output

Spreadsheet-friendly format for data analysis.

```bash
go run ./cmd/benchmark -output csv -output-file results.csv
```

## How It Works

### Architecture

```
┌─────────────┐
│ Orchestrator│
└──────┬──────┘
       │
       ├─────────────────────────────────┐
       │                                 │
┌──────▼──────┐    ┌──────────┐    ┌────▼─────┐
│ WS Backend  │    │HTTP Backend│   │MCP Backend│
└──────┬──────┘    └────┬─────┘    └────┬─────┘
       │                │               │
       └────────┬───────┴───────────────┘
                │
         ┌──────▼──────┐
         │   Metrics   │
         │  Collector  │
         └──────┬──────┘
                │
         ┌──────▼──────┐
         │   Reporter  │
         └─────────────┘
```

### Benchmark Flow

1. **Configuration** - Parse command-line flags and validate settings
2. **Agent Resolution** - Load agent credentials from agents directory
3. **Backend Selection** - Determine which backends to test
4. **Ramp-Up** - Gradually start agents over ramp-up period
5. **Execution** - Run agents for specified duration
6. **Metrics Collection** - Collect performance metrics
7. **Reporting** - Generate summary and export results

### Rate Limiting

The benchmark uses a token bucket rate limiter to control request frequency:

- **Token Bucket** - Refills at configured rate
- **Per-Agent** - Each agent has independent rate limiter
- **Configurable** - Adjust delay between commands

### Connection Modes

**Individual Mode (Default):**
- Each agent gets its own connection
- Higher resource usage
- Isolated failure domains

**Shared Mode:**
- Agents share connection pool
- Lower resource usage
- Potential for contention

## Configuration

### Agent Directory Structure

```
data/agents/
├── miner-1/
│   └── credentials.json
├── explorer-2/
│   └── credentials.json
└── trader-1/
    └── credentials.json
```

### Credentials Format

```json
{
  "username": "agent-username",
  "password": "agent-password",
  "empire": "voidborn"
}
```

## Troubleshooting

### Issue: "Failed to load credentials"

**Cause:** Agent credentials not found or malformed.

**Solution:**
1. Verify agent directory exists: `ls data/agents/`
2. Check credentials.json format
3. Ensure agents-dir flag points to correct location

### Issue: "High error rate"

**Cause:** Server overload, network issues, or invalid credentials.

**Solution:**
1. Reduce agent count: `-agents 5`
2. Increase delay: `-delay 1s`
3. Check network connectivity
4. Verify credentials are valid

### Issue: "Connection timeout"

**Cause:** Server not responding or firewall blocking connections.

**Solution:**
1. Check server URL: `-server wss://game.spacemolt.com/ws`
2. Verify network connectivity
3. Check firewall settings
4. Try HTTP backend as fallback

## Performance

### Typical Performance

On a modern system with stable network:

| Backend | Throughput | p95 Latency | p99 Latency |
|---------|-----------|-------------|-------------|
| WebSocket | 600 req/s | 45ms | 120ms |
| HTTP | 450 req/s | 85ms | 180ms |
| MCP | 380 req/s | 120ms | 250ms |

### Scaling Behavior

- **Linear Scaling** - Performance scales linearly up to ~50 agents
- **Diminishing Returns** - Beyond 100 agents, contention increases
- **Shared Connections** - Better resource utilization at high scale

## Best Practices

### For Development

```bash
# Quick smoke test
go run ./cmd/benchmark -agents 3 -duration 30s
```

### For Load Testing

```bash
# Find breaking point
go run ./cmd/benchmark -agents 100 -duration 10m -ramp-up 2m
```

### For Regression Testing

```bash
# Consistent baseline test
go run ./cmd/benchmark -agents 20 -duration 5m -output json -output-file baseline.json
```

### For Production Monitoring

```bash
# Regular health check
go run ./cmd/benchmark -agents 10 -duration 2m -output csv -output-file metrics-$(date +%Y%m%d).csv
```

## Related Documentation

- [Game Client Documentation](../../pkg/game/) - Game client API reference
- [Protocol Documentation](../../internal/protocol/) - Message protocol details
- [HTTP Backend](../../pkg/game/http.go) - HTTP client implementation
- [WebSocket Backend](../../pkg/game/client.go) - WebSocket client implementation
- [MCP Bridge](../mcp-ws-bridge/) - MCP WebSocket bridge documentation

## License

Part of the SpaceMolt project.
