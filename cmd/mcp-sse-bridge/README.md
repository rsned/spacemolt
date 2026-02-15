# MCP stdio-to-SSE Bridge

Bridges between stdio MCP protocol (used by Claude Code) and SSE MCP protocol (used by Spacemolt game server).

## Overview

This bridge allows Claude Code to seamlessly communicate with the Spacemolt game server by:
- Accepting stdio MCP protocol requests (JSON-RPC over stdin/stdout)
- Translating them to HTTP POST requests with SSE responses
- Automatically handling authentication and session management
- Forwarding tool calls with the required `session_id` parameter

## Usage

### Build

```bash
go build -o bin/mcp-sse-bridge ./cmd/mcp-sse-bridge
```

### Configuration

Add to your Claude Code settings (`~/.claude/settings.json`):

```json
{
  "mcpServers": {
    "spacemolt": {
      "command": "/home/robert/spacemolt/spacemolt-agent-server/bin/mcp-sse-bridge",
      "args": [
        "-creds",
        "/home/robert/spacemolt/spacemolt-agent-server/data/agents/random-2/credentials.json"
      ]
    }
  }
}
```

### Credentials File

Create a credentials JSON file:

```json
{
  "username": "your-username",
  "password": "your-password",
  "empire": "solarian"
}
```

### Command-Line Options

```
-creds string
    Path to credentials JSON file (required)
-server string
    Game server URL (default "https://game.spacemolt.com/mcp")
-verbose
    Enable verbose logging
```

## How It Works

1. **Initialization**: When Claude Code calls `initialize`, the bridge:
   - Connects to the game server via SSE
   - Logs in with the provided credentials
   - Stores the `session_id` for future calls

2. **Tools List**: Returns available game tools (get_status, get_ship, etc.)

3. **Tool Calls**: Forwards tool calls to the game server:
   - Adds `session_id` to the parameters automatically
   - Translates SSE responses to stdio MCP format
   - Returns results as MCP tool results

## Available Tools

The bridge exposes these game tools:

- **get_status** - Current player and ship status
- **get_ship** - Detailed ship information
- **get_cargo** - Cargo contents
- **get_system** - Current system details
- **get_map** - All discovered star systems
- **get_skills** - Skills and progress
- **get_recipes** - Available crafting recipes
- **get_listings** - Market listings at current base
- **help** - Game command help

## Testing

Test the bridge manually:

```bash
# Initialize
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' | ./bin/mcp-sse-bridge -creds data/agents/random-2/credentials.json

# List tools
echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./bin/mcp-sse-bridge -creds data/agents/random-2/credentials.json

# Call a tool
echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_status","arguments":{}}}' | ./bin/mcp-sse-bridge -creds data/agents/random-2/credentials.json
```

## Architecture

```
Claude Code
    ↓ (stdio MCP)
mcp-sse-bridge
    ↓ (HTTP POST with SSE)
game.spacemolt.com/mcp
```

### Bridge Components

- **SSEClient**: HTTP client that handles SSE transport
- **Bridge**: Main server that:
  - Reads JSON-RPC from stdin
  - Writes JSON-RPC to stdout
  - Manages game session and authentication
  - Forwards tool calls with session_id

## Logging

The bridge logs to stderr:
- Info: Initialization, login, session management
- Debug: Request/response details (use `-verbose`)
- Error: Connection failures, authentication errors

## Error Handling

- Authentication errors are logged and returned as tool errors
- Session expiration triggers re-initialization
- Network errors are propagated to Claude Code

## Related

- **cmd/mcp-scraper** - Tool for exploring all game API endpoints
- **cmd/crafting-server** - Local MCP server for crafting queries
- **internal/crafting/mcp** - Reference stdio MCP server implementation

## License

Part of the spacemolt-agent-server project.
