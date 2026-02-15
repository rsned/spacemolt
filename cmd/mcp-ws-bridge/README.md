# MCP WebSocket Bridge

Bridges between the MCP (Model Context Protocol) stdio interface and the SpaceMolt WebSocket game API, allowing AI agents like Claude to interact with the game using MCP tools.

## Overview

This bridge exposes **all SpaceMolt game server endpoints** (148+ tools) as MCP tools that can be called by AI agents through the MCP protocol. Tool definitions are automatically generated from the OpenAPI specification.

## Features

- **Complete API Coverage**: All game endpoints automatically exposed as MCP tools
- **Auto-Generated Tools**: Tool definitions generated from `server_docs/openapi.json`
- **Always Up-to-Date**: Run `make update-mcp` to sync with latest game API
- **Seamless Integration**: Works with Claude Code and other MCP clients

## Usage

### Running the Bridge

```bash
# Build the bridge
go build -o bin/mcp-ws-bridge ./cmd/mcp-ws-bridge

# Run with credentials file
./bin/mcp-ws-bridge -creds data/agents/pirate-4/credentials.json

# With verbose logging
./bin/mcp-ws-bridge -creds data/agents/pirate-4/credentials.json -verbose
```

### Credentials File Format

```json
{
  "username": "☠",
  "password": "your_password_hash",
  "empire": "crimson"
}
```

## Tool Generation

### How It Works

1. **OpenAPI Spec**: Game server publishes OpenAPI spec at `https://game.spacemolt.com/api/openapi.json`
2. **Fetch Latest**: `cmd/update-server-docs` downloads the spec to `server_docs/openapi.json`
3. **Generate Tools**: `cmd/generate-mcp-tools` reads the spec and generates `tools_generated.go`
4. **Bridge Uses Tools**: `main.go` calls `GeneratedTools()` to list all available tools

### Updating When API Changes

When the game server adds new endpoints or modifies existing ones:

```bash
# One-command update: fetch docs + regenerate tools
make update-mcp

# Or run steps individually:
make update-server-docs      # Download latest OpenAPI spec
make generate-mcp-tools       # Regenerate tool definitions
```

This will:
- Download the latest OpenAPI spec from spacemolt.com
- Regenerate `cmd/mcp-ws-bridge/tools_generated.go` with all endpoints
- Show you what changed

## Available Tools

All 148+ game endpoints are exposed. Run `make update-mcp` to sync with the latest API.

See [SpaceMolt API Documentation](https://www.spacemolt.com/api.md) for details.
