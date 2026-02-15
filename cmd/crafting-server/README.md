# SpaceMolt Crafting Query MCP Server

An MCP (Model Context Protocol) server that provides intelligent crafting queries for SpaceMolt AI agents. Enables agents to efficiently discover what they can craft, plan crafting paths, and optimize skill progression without complex prompt engineering.

## Features

### 5 Powerful MCP Tools

1. **`craft_query`** - "What can I craft with my inventory?"
   - Returns fully craftable recipes, partial matches, and skill-blocked recipes
   - Supports multiple optimization strategies (profit, volume, inventory usage)
   - Fast component matching with inverted index

2. **`craft_path_to`** - "How do I craft this specific item?"
   - Backward chaining from target recipe
   - Shows material requirements with acquisition methods
   - Single-level expansion (agents control planning depth)

3. **`recipe_lookup`** - "Tell me about this recipe"
   - Direct lookup by ID or search by name
   - Skill gap analysis
   - Shows what recipes use this output (crafting chains)

4. **`skill_craft_paths`** - "Which skills unlock new recipes?"
   - Identifies high-value skill progression paths
   - Shows recipes unlocked at next level
   - Sorted by number of recipes unlocked

5. **`component_uses`** - "What can I do with this component?"
   - Find all recipes using a specific component
   - Useful when acquiring new materials
   - Supports profit optimization

### Optimization Strategies

All query tools support strategic result sorting:
- `MAXIMIZE_PROFIT` - Sort by profit margin (requires market data)
- `MAXIMIZE_VOLUME` - Prefer recipes you can craft many times
- `OPTIMIZE_CRAFT_PATH` - Prefer simpler recipes
- `USE_INVENTORY_FIRST` - Minimize new acquisitions (default)
- `MINIMIZE_ACQUISITION` - Prefer recipes needing fewest missing components

## Installation

The crafting server is part of the spacemolt-agent-server project:

```bash
# Build the server
go build -o bin/crafting-server ./cmd/crafting-server

# Build the data converters
go build -o bin/convert-recipes ./cmd/convert-recipes
go build -o bin/convert-skills ./cmd/convert-skills
```

## Data Import

Before using the server, import recipe and skill data:

### 1. Convert SpaceMolt Data Format

```bash
# Convert recipes
./bin/convert-recipes server_docs/recipes.20260209.json data/crafting/recipes-import.json

# Convert skills
./bin/convert-skills server_docs/skills.20260209.json data/crafting/skills-import.json
```

### 2. Import into Database

```bash
# Import recipes (161 recipes)
./bin/crafting-server -import-recipes data/crafting/recipes-import.json

# Import skills (139 skills)
./bin/crafting-server -import-skills data/crafting/skills-import.json

# Optional: Import market data for profit calculations
./bin/crafting-server -import-market data/crafting/market.json
```

## Usage

### As an MCP Server (for Claude Desktop/API)

Run the server to communicate via stdin/stdout JSON-RPC:

```bash
./bin/crafting-server -db data/crafting/crafting.db
```

### MCP Client Configuration

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS):

```json
{
  "mcpServers": {
    "spacemolt-crafting": {
      "command": "/full/path/to/bin/crafting-server",
      "args": ["-db", "/full/path/to/data/crafting/crafting.db"]
    }
  }
}
```

### Command-Line Options

```
-db string
    Path to SQLite database (default "data/crafting/crafting.db")
-import-recipes string
    Import recipes from JSON file
-import-skills string
    Import skills from JSON file
-import-market string
    Import market data from JSON file
-verbose
    Enable verbose logging
```

## Example Queries

### Query Craftable Recipes

```json
{
  "method": "tools/call",
  "params": {
    "name": "craft_query",
    "arguments": {
      "components": [
        {"id": "ore_copper", "quantity": 50},
        {"id": "ore_iron", "quantity": 30}
      ],
      "skills": {
        "crafting_basic": 1,
        "mining_basic": 2
      },
      "limit": 10
    }
  }
}
```

**Response:** Returns craftable recipes, partial matches, and skill-blocked options.

### Plan Crafting Path

```json
{
  "method": "tools/call",
  "params": {
    "name": "craft_path_to",
    "arguments": {
      "target_recipe_id": "craft_sensor_array",
      "current_inventory": [
        {"id": "refined_circuits", "quantity": 5}
      ],
      "skills": {
        "crafting_basic": 4,
        "scanning": 2
      }
    }
  }
}
```

**Response:** Shows materials needed, what you have, what to acquire, and crafting time.

### Find Skill Progression Paths

```json
{
  "method": "tools/call",
  "params": {
    "name": "skill_craft_paths",
    "arguments": {
      "skills": {
        "crafting_basic": {"level": 1, "current_xp": 50},
        "mining_basic": {"level": 2, "current_xp": 100}
      },
      "limit": 5
    }
  }
}
```

**Response:** Lists skills sorted by recipes unlocked at next level, with XP needed.

## Architecture

```
cmd/crafting-server/
├── main.go                    # Entry point and CLI handling

pkg/crafting/
└── types.go                   # Public domain types (340 lines)

internal/crafting/
├── db/                        # Database layer
│   ├── db.go                  # Core DB wrapper
│   ├── schema.go              # Schema initialization
│   ├── recipes.go             # Recipe queries
│   ├── skills.go              # Skill queries
│   └── market.go              # Market data queries
├── engine/                    # Query business logic
│   ├── engine.go              # Main engine
│   ├── craft_query.go         # Component matching
│   ├── craft_path.go          # Path planning
│   ├── recipe_lookup.go       # Recipe search
│   ├── skill_paths.go         # Skill analysis
│   └── component_uses.go      # Reverse lookup
├── mcp/                       # MCP protocol
│   ├── server.go              # JSON-RPC server
│   └── tools.go               # Tool definitions
└── sync/                      # Data import
    └── sync.go                # Import from JSON
```

## Database Schema

SQLite database with the following tables:

- **recipes** - Recipe metadata
- **recipe_components** - Required inputs (inverted index on component_id)
- **recipe_skills** - Skill requirements
- **skills** - Skill definitions
- **skill_prerequisites** - Skill dependencies
- **skill_levels** - XP thresholds per level
- **market_prices** - Historical price data
- **market_price_summary** - Aggregated price stats
- **sync_metadata** - Import tracking

## Performance

- **Query Speed:** 1-5ms for typical craft_query (6-20 recipes checked)
- **Database Size:** ~500KB (161 recipes, 139 skills)
- **Binary Size:** 10MB (includes all dependencies)
- **Indexing:** Inverted component index for O(log n) lookups

## Data Sources

The server imports data from SpaceMolt game API snapshots:

- **Recipes:** `server_docs/recipes.20260209.json` (161 recipes)
- **Skills:** `server_docs/skills.20260209.json` (139 skills)
- **Market:** Agent-collected price data (optional)

## Testing

Test the server with sample queries:

```bash
# Initialize
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' | ./bin/crafting-server -db data/crafting/crafting.db 2>/dev/null

# List tools
echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./bin/crafting-server -db data/crafting/crafting.db 2>/dev/null

# Test craft_query
echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"craft_query","arguments":{"components":[{"id":"ore_copper","quantity":50}],"skills":{"crafting_basic":1},"limit":5}}}' | ./bin/crafting-server -db data/crafting/crafting.db 2>/dev/null
```

## Design Principles

1. **Stateless** - No per-agent state stored
2. **All recipes visible** - Agents see all game content
3. **Single-level expansion** - Agents control planning depth
4. **Fast lookups** - Inverted indexes for component matching
5. **Market-aware** - Optional profit optimization with real prices

## Scope

### In Scope
- Recipe querying and matching
- Component gap analysis
- Skill requirement checking
- Optimization strategies
- Market-based profit calculations

### Out of Scope (Agent Layer)
- Multi-agent coordination
- Inventory synchronization with game
- Crafting execution (calling game API)
- Goal prioritization
- Per-agent state persistence

## Related Documentation

- [Design Specification](../../docs/spacemolt-crafting-server-spec-final.md)
- [SpaceMolt Agent Guide](../../docs/SPACEMOLT_AGENT_GUIDE.md)
- [MCP Protocol](https://modelcontextprotocol.io)

## Status

✅ **Production Ready**
- All 5 tools implemented and tested
- 161 recipes imported
- 139 skills imported
- Query performance: 1-5ms
- Full MCP protocol compliance

## License

Part of the spacemolt-agent-server project.
