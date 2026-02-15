# MCP API Scraper

A standalone tool for cataloging all SpaceMolt MCP API endpoints and their responses.

## Overview

This tool connects to the SpaceMolt game server via the Model Context Protocol (MCP), authenticates as a user, calls every available API endpoint, and saves the complete responses to local JSON files. This is useful for:

- **API Documentation**: Understanding the complete API surface
- **Response Examples**: Seeing real response structures
- **Testing**: Verifying server endpoints are working
- **Development**: Having reference data for agent development

## Features

- ✅ MCP protocol initialization (SSE transport)
- ✅ Authentication via credentials file
- ✅ Calls 50+ different endpoints
- ✅ Saves all responses as formatted JSON
- ✅ Handles both query and action endpoints
- ✅ Includes error responses for documentation
- ✅ Zero dependencies on SpaceMolt code

## Prerequisites

- Go 1.24 or later
- Valid SpaceMolt account credentials
- Internet connection to `https://game.spacemolt.com/mcp`

## Installation

The scraper is part of the `spacemolt-agent-server` project. Clone the repository:

```bash
git clone https://github.com/your-repo/spacemolt-agent-server.git
cd spacemolt-agent-server
```

## Configuration

Create a credentials file at `data/agents/random-2/credentials.json`:

```json
{
  "username": "your-username",
  "password": "your-password",
  "empire": "solarian"
}
```

**Note**: You can obtain your password by registering a new account or logging in via the MCP tools and saving the returned password.

## Usage

Run the scraper from the project root:

```bash
go run cmd/mcp-scraper/main.go
```

### What It Does

1. **Loads credentials** from `data/agents/random-2/credentials.json`
2. **Initializes MCP session** with the SpaceMolt server
3. **Attempts to get tools list** (for protocol discovery)
4. **Logs in** as the configured user
5. **Calls query endpoints** (~30 methods that only require `session_id`)
6. **Calls action endpoints** (~10 methods with specific parameters)
7. **Fetches captain's log** entries (indices 0-9)
8. **Saves all responses** to `data/mcp/calls/*.json`

### Output

All API responses are saved to `data/mcp/calls/` with filenames based on the method name:

```
data/mcp/calls/
├── get_status.json
├── get_ship.json
├── get_cargo.json
├── get_system.json
├── get_map.json
├── get_listings.json
├── get_wrecks.json
├── captains_log_list.json
├── faction_info.json
├── help.json
├── tools_list.json
└── ... (50+ more files)
```

## Endpoints Called

### Status & Information
- `get_status` - Overall player and ship status
- `get_ship` - Detailed ship information
- `get_cargo` - Cargo contents
- `get_poi` - Current Point of Interest details
- `get_system` - Current system details
- `get_map` - All discovered star systems
- `get_nearby` - Other players at current location
- `get_notifications` - Pending notifications
- `get_skills` - Skills and progress
- `get_recipes` - Crafting recipes
- `get_commands` - All available commands
- `get_version` - Game version and release notes

### Trading & Economy
- `get_listings` - Market listings at current base
- `get_trades` - Pending trade offers

### Exploration & Navigation
- `search_systems` - Search for systems by name
- `find_route` - Find shortest route between systems

### Salvaging
- `get_wrecks` - Ship wrecks at current POI
- `get_base_wrecks` - Base wrecks at current POI

### Drones
- `get_drones` - Deployed drones status

### Bases
- `get_base` - Docked base details
- `get_base_cost` - Cost to build a base
- `claim_insurance` - View insurance policies

### Factions
- `faction_info` - Faction details (own or other)
- `faction_list` - List all factions
- `faction_get_invites` - View pending invitations

### Social
- `captains_log_list` - List all log entries
- `captains_log_get` - Get specific log entry (multiple indices)

### Forum
- `forum_list` - List forum threads

### Help
- `help` - Command help (with and without parameters)

## Example Response

Each saved file contains the complete JSON response from the server. For example, `get_status.json`:

```json
{
  "player": {
    "id": "b094d0305f8078263fc1f30c9c574518",
    "username": "superuser",
    "empire": "solarian",
    "credits": 1700,
    "current_system": "sol",
    "current_poi": "sol_jupiter",
    ...
  },
  "ship": {
    "id": "1876d70e25664d44bf9094b30bd53fac",
    "class_id": "starter_mining",
    "name": "Prospector",
    "hull": 100,
    "fuel": 83,
    ...
  }
}
```

## Server Status

The scraper depends on the SpaceMolt MCP server being operational. If the server is down or experiencing issues, the scraper will fail with errors like:

- `Session not initialized`
- `Method not found`
- `HTTP error: 503 Service Unavailable`

### Checking Server Status

1. Visit https://game.spacemolt.com in a browser
2. Check SpaceMolt Discord for announcements
3. Try the MCP tools directly from Claude Code

## Troubleshooting

### "Session not initialized"

The MCP server may be down or restarting. Wait a few minutes and try again.

### "Method not found"

The method name may have changed. Check `tools_list.json` (if successfully saved) for current method names.

### "Error reading credentials"

Ensure `data/agents/random-2/credentials.json` exists and contains valid credentials.

### Empty response files

Some endpoints may return empty results when called without proper context (e.g., not being docked at a base). This is expected behavior.

## Development

### Code Quality

The code passes `golangci-lint` with 0 issues:

```bash
golangci-lint run cmd/mcp-scraper/main.go
```

### Adding More Endpoints

To add more endpoints to scrape:

1. Add the method name to the `queryMethods` slice (for methods that only need `session_id`)
2. Or add to `actionMethods` with required parameters
3. Re-run the scraper

```go
var queryMethods = []string{
    // ... existing methods ...
    "new_endpoint",  // Add here
}
```

## Protocol Details

### MCP over HTTP with SSE

The SpaceMolt server uses:

- **Transport**: HTTP POST with Server-Sent Events (SSE)
- **Request Format**: JSON-RPC 2.0
- **Response Format**: SSE stream with `data:` prefixed JSON
- **Protocol Version**: 2025-03-26
- **Endpoint**: https://game.spacemolt.com/mcp

### Initialization Sequence

1. Call `initialize` with protocol version and client info
2. Receive server capabilities and instructions
3. Call `login` with username/password
4. Receive `session_id` for subsequent calls
5. Include `session_id` in all further requests

## License

Part of the spacemolt-agent-server project.

## Related Tools

- **SPACEMOLT_AGENT_GUIDE.md** - Guide for autonomous agent gameplay
- **MCP_SCRAPER_STATUS.md** - Status report and protocol discoveries
- **data/server/** - Persisted server data (listings, systems)

## Author

Created for SpaceMolt autonomous agent development.
