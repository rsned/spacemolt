# Spacemolt - Codebase Context for Claude

When adding any sleep or pauses, always use one of the predefined Sleep constants in `pkg/game/constants.go`. If none are appropriate, prompt the user to add another value.

## Build & Test

After implementing changes, always run `go build ./...` and `go test ./...` before committing. For frontend changes, run the appropriate build command to verify no compilation errors.

## Project Context

Primary languages: Go (backend) and TypeScript (frontend). The codebase uses WebSocket connections for game client communication. Always check actual API/server response struct field names before coding against them — do NOT assume field names.

## Git Workflow

When asked to commit and push, use `git add -A && git commit -m '<message>' && git push` without excessive verification. Do not spend multiple tool calls checking if the push succeeded — trust the exit code.

Check .gitignore before committing new files in directories that may have broad ignore patterns (e.g., `docs/*.md`). If a file is being ignored unexpectedly, add a negation rule like `!docs/USAGE.md`.

---

## Directory Structure

```
spacemolt/
├── cmd/                              # Executable binaries
│   ├── spacemolt-server/             # Main unified server
│   ├── agent-server/                 # Legacy agent management server
│   ├── status-registry/              # Tool registration & status tracking
│   ├── auto-explorer/                # Automated explorer agent
│   ├── auto-miner/                   # Automated miner agent
│   ├── auto-trader/                  # Automated trader agent
│   ├── auto-fighter/                 # Automated fighter agent
│   ├── auto-pirate/                  # Automated pirate agent
│   ├── auto-craftsman/               # Automated craftsman agent
│   ├── auto-salvager/                # Automated salvager agent
│   ├── auto-prophet/                 # Market forecasting agent
│   ├── auto-random/                  # Random action agent
│   ├── auto-llm-miner/              # LLM-driven mining agent
│   ├── auto-recall/                  # Recall agent
│   ├── bridge/
│   │   ├── mcp-bridge-service/       # Main MCP bridge for Claude
│   │   ├── mcp-ws-bridge/            # WebSocket MCP bridge
│   │   └── mcp-sse-bridge/           # SSE MCP bridge
│   ├── tools/                        # CLI utilities (daily-summary, skill-tree, etc.)
│   ├── data/                         # Data import/conversion tools
│   └── debug/                        # Debug/test tools (benchmark, play-simple)
│
├── pkg/                              # Shared library packages
│   ├── game/                         # Game client, state, types, commands
│   ├── agent/                        # Agent interface, runner, manager
│   ├── strategy/                     # Pre-built behavior strategies
│   ├── knowledge/                    # SQLite/in-memory knowledge base
│   ├── llm/                          # Ollama LLM client
│   ├── prompts/                      # Prompt template management
│   ├── credentials/                  # Multi-backend credential storage
│   ├── observe/                      # Observer server & browser hub
│   ├── monitor/                      # Metrics collection
│   ├── registry/                     # Tool registration service
│   ├── team/                         # Multi-agent team coordination
│   ├── unified/                      # Unified server combining all components
│   ├── api/                          # REST API handlers
│   ├── config/                       # Configuration management
│   └── version/                      # Version checking
│
├── internal/
│   ├── protocol/                     # WebSocket message type constants
│   └── ws/                           # WebSocket utilities
│
├── frontend/                         # React 19 + TypeScript + Vite
│   └── src/
│       ├── components/               # React components
│       ├── lib/                      # Utilities (useObserver.ts = WebSocket hook)
│       └── types/                    # TypeScript type definitions
│
├── data/                             # Runtime data
│   ├── agents/                       # Agent personality YAML configs
│   ├── crafting/                     # Crafting recipe data
│   ├── prompts/templates/            # LLM prompt templates
│   └── game-api/                     # Cached API responses
│
├── scripts/sql/migrations/           # Database migration SQL files
├── server_docs/                      # API docs (openapi.json, api.md)
└── spacemolt-server.yaml             # Main server configuration
```

## Key Types and Interfaces

### pkg/game — Game Client & State

| Type | File | Purpose |
|------|------|---------|
| `GameClient` (interface) | `pkg/game/interface.go:7` | All game operations (150+ methods) |
| `MessageHandler` (interface) | `pkg/game/client.go:92` | OnConnected/OnMessage/OnDisconnected |
| `Client` | `pkg/game/client.go:22` | WebSocket client implementation |
| `State` | `pkg/game/types.go:296` | Complete game state (player, ship, system, combat) |
| `Player` | `pkg/game/types.go:19` | Player data (credits, skills, stats) |
| `Ship` | `pkg/game/types.go:110` | Ship with modules and cargo |
| `SystemData` | `pkg/game/types.go:208` | System info (POIs, connections, security) |
| `POI` | `pkg/game/types.go:168` | Point of Interest (station, asteroid, etc.) |
| `MarketListing` | `pkg/game/types.go:560` | Market listing with price |
| `CommandQueue` | `pkg/game/client_queue.go:24` | Sequential command execution |

### pkg/agent — Agent System

| Type | File | Purpose |
|------|------|---------|
| `Agent` (interface) | `pkg/agent/agent.go:10` | Autonomous agent (Decide, Learn, Memory) |
| `BaseAgent` | `pkg/agent/base.go:59` | Default agent implementation |
| `Runner` | `pkg/agent/runner.go:17` | Decision-making execution loop |
| `Manager` | `pkg/agent/manager.go:29` | Multi-agent lifecycle management |
| `Decision` | `pkg/agent/agent.go:74` | Action choice (action, target, reasoning, confidence) |
| `Personality` | `pkg/agent/agent.go:44` | Agent traits and motivations |
| `Memory` (interface) | `pkg/agent/agent.go:150` | Agent knowledge storage |

### pkg/knowledge — Knowledge Base

| Type | File | Purpose |
|------|------|---------|
| `Base` (interface) | `pkg/knowledge/base.go:9` | KB operations (systems, markets, analytics) |
| `SQLiteKB` | `pkg/knowledge/sqlite.go:36` | SQLite implementation |
| `MemoryKB` | `pkg/knowledge/memory.go:13` | In-memory implementation |

### pkg/strategy — Behavior Strategies

| Type | File | Purpose |
|------|------|---------|
| `Strategy` (interface) | `pkg/strategy/strategy.go:15` | Reusable behavior loop (Name, Run, CurrentStatus) |
| `Registry` | `pkg/strategy/strategy.go:41` | Strategy registration |
| Implementations | `mining.go`, `explorer.go`, `fighter.go`, `trader.go`, `idle.go` | Per-role strategies |

### pkg/credentials — Credential Providers

| Type | File | Purpose |
|------|------|---------|
| `Provider` (interface) | `pkg/credentials/provider.go:36` | Get/Store/Remove credentials |
| Implementations | `file.go`, `sqlite.go`, `keyring.go`, `env.go` | Multiple backends |

## Data Flow

```
┌──────────────────────────────────────────────────────┐
│ GAME SERVER (wss://game.spacemolt.com/ws)            │
└──────────────┬───────────────────────────────────────┘
               │ WebSocket: protocol.Message
               │
      ┌────────▼──────────────────────────────┐
      │ Game Client (pkg/game/Client)         │
      │ - 150+ command methods                │
      │ - State synchronization               │
      │ - handleResponse() switch @ :1226     │
      └────┬──────────┬──────────┬────────────┘
           │          │          │
   ┌───────▼───┐ ┌────▼────┐ ┌──▼──────────────┐
   │ Strategy  │ │ Agent   │ │ Frontend        │
   │ Runner    │ │ Runner  │ │ (React/TS)      │
   │ (pre-     │ │ (LLM    │ │ via Observer    │
   │  built    │ │  driven │ │ WebSocket       │
   │  bots)    │ │  loop)  │ │ /ws endpoint    │
   └───────────┘ └────┬────┘ └─────────────────┘
                      │
              ┌───────▼───────┐
              │ LLM (Ollama)  │
              │ via prompts/  │
              └───────────────┘
           │          │
      ┌────▼──────────▼────────────────┐
      │ Knowledge Base (SQLite)        │
      │ - Systems, POIs, resources     │
      │ - Market snapshots & trends    │
      │ - Experiences & anomalies      │
      └────────────────────────────────┘
```

### Message Flow

1. **Client → Server**: `protocol.Message{Type: "mine", Payload: {...}}`
2. **Server → Client**: Response with `type: "ok"`, `"error"`, `"action_error"`, or event types
3. **Client updates**: Internal `State` struct via `handleResponse()` in `client.go:1226`
4. **Frontend**: Observer server (`pkg/observe/`) bridges browser WebSocket ↔ agent game state

## WebSocket Protocol

### Protocol Message Constants — `internal/protocol/messages.go`

**Server → Client event types:**
- `welcome`, `registered`, `logged_in` — Connection lifecycle
- `ok` — Successful command response (contains updated state data)
- `error`, `action_error` — Error responses
- `state_update`, `tick` — Periodic state sync
- `docked`, `undocked` — Location events
- `pirate_warning`, `pirate_combat`, `pirate_destroyed` — Pirate events
- `police_warning`, `police_combat` — Police events
- `combat_update`, `player_died` — Combat events
- `mining`, `mining_yield` — Mining events
- `skill_level_up` — Progression events
- `chat_message`, `trade_offer_received` — Social events

### Key Client Commands (grouped)

**Navigation**: `travel`, `jump`, `dock`, `undock`
**Combat**: `battle` (advance/retreat/stance/target/engage), `cloak`, `scan`, `self_destruct`
**Mining/Resources**: `mine`, `refuel`, `repair`
**Trading**: `buy`, `sell`, `sell_all`, `create_buy_order`, `create_sell_order`, `view_market`, `view_orders`
**Crafting**: `craft` (with recipe and quantity)
**Ship Management**: `list_ships`, `buy_ship`, `sell_ship`, `switch_ship`, `commission_ship`
**Storage**: `view_storage`, `withdraw_items`, `deposit_credits`, `jettison`
**Salvage**: `get_wrecks`, `loot_wreck`, `salvage_wreck`, `tow_wreck`
**Info Queries** (no tick cost): `get_status`, `get_system`, `get_poi`, `get_ship`, `get_cargo`, `get_map`, `get_skills`, `get_recipes`, `get_nearby`
**Factions**: 30+ commands (`create_faction`, `faction_info`, `faction_declare_war`, `faction_submit_intel`, etc.)
**Missions**: `get_missions`, `accept_mission`, `complete_mission`, `abandon_mission`
**Social**: `chat`, `forum_list`, `forum_create_thread`, `trade_offer`, `send_gift`

### Response Struct Definitions — `pkg/game/serverapi/responses.go`

All follow pattern: `type {Command}Response struct { Action string; ... }`

### Main Handler — `pkg/game/client.go:1226`

`handleResponse()` switch dispatches all incoming messages to parsers: `parsePlayerData()`, `parseShipData()`, `parseSystemData()`, etc.

### Frontend WebSocket — `frontend/src/lib/useObserver.ts`

Messages: `list_agents`, `subscribe`, `command`, `game_message`, `agent_list`, `agent_status`, `error`

## Database Schema (SQLite Knowledge Base)

**14 migrations** in `pkg/knowledge/sqlite_migrations.go`:

### Core Tables
- **systems** — System metadata (position, empire, security, stronghold)
- **connections** — System-to-system routes with distance
- **pois** — Points of Interest (type, system, coordinates, docked status)
- **poi_resources** — Resource richness and depletion tracking
- **bases** — Player bases with services
- **agents** — Agent metadata
- **experiences** — Learning history with outcomes

### Market Intelligence
- **market_snapshots** — Market state at timestamp
- **market_listings** — Item listings per snapshot
- **ship_listings** — Ships available at stations
- **market_analyses** — AI-generated trading insights

### Analytics
- **resource_history** — Resource depletion trends
- **connection_metrics** — Route fuel cost and travel time
- **anomalies** — Unusual discoveries
- **price_trends** — Price analysis windows
- **danger_zones** — Hostile encounter tracking

### Catalogs (Read-Only Game Data)
- **items** — Item definitions
- **ship_classes** — Ship models with stats
- **skills** — Skill definitions with XP curves
- **recipes**, **recipe_inputs**, **recipe_outputs** — Crafting recipes

### Player State
- **players**, **player_stats**, **player_skills** — Player progression
- **ships**, **ship_cargo**, **ship_modules** — Fleet inventory
- **mission_templates**, **mission_objectives** — Missions

## Common Patterns

### Adding a New Game Command

1. **Add method** to `pkg/game/client_commands.go`:
   ```go
   func (c *Client) MyAction(ctx context.Context, target string) error {
       return c.Send(ctx, protocol.Message{Type: "my_action", Payload: map[string]any{"target": target}})
   }
   ```
2. **Add to interface** in `pkg/game/interface.go`
3. **Add dispatch case** in `pkg/agent/runner.go` → `executeDecision()` (50+ existing cases)
4. **Classify** in `isActionCommand()` — queries return false, actions return true
5. **Add response struct** in `pkg/game/serverapi/responses.go` if needed

### Adding a New Strategy

1. Create `pkg/strategy/mystrategy.go` implementing `Strategy` interface (Name, Description, Run, CurrentStatus)
2. Register in strategy `Registry`
3. Create `cmd/auto-mystrategy/main.go` binary

### Agent Decision Loop (pkg/agent/runner.go)

Every ~11 seconds: check tick → dequeue planned action OR call `Agent.Decide()` → `executeDecision()` dispatches to `GameClient` method → record result → `Agent.Learn()`

### Sleep Constants (pkg/game/constants.go)

`SleepTick=10s`, `SleepQuick=2s`, `SleepShort=5s`, `SleepMedium=30s`, `SleepLong=60s`, `SleepDock=15s`, `SleepTravel=10s`, `SleepJump=20s`, `SleepReconnect=30s`

### Freshness Thresholds

Resource POIs: 6 hours, Default POIs: 1 week, Stations: 1 day, Systems: 1 day

### State Access

`State` is the central game state struct. Thread-safe access via `state.Clone()`. Updated by `handleResponse()` on every server message.
