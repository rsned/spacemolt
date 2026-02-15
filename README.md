# Spacemolt Multi-Agent System

An autonomous multi-agent system and development toolkit for the [SpaceMolt](https://spacemolt.com) game. Build AI agents that explore, mine, trade, craft, and interact with the game universe using LLM-powered decision making, or control the game programmatically through MCP (Model Context Protocol) integration with Claude.

## Table of Contents

- [Overview](#overview)
- [Key Features](#key-features)
- [Recent Additions](#recent-additions)
- [Quick Start](#quick-start)
- [Running Autonomous LLM Agents](#running-autonomous-llm-agents)
- [Database Backends](#database-backends)
- [Architecture](#architecture)
- [How It Works](#how-it-works)
- [Agent Personalities](#agent-personalities)
- [Example Sessions](#example-sessions)
- [Claude Integration via MCP](#claude-integration-via-mcp)
- [Automated Specialized Agents](#automated-specialized-agents)
- [Web Interface](#web-interface)
- [REST API](#rest-api)
- [Development](#development)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [License](#license)

## Overview

The Spacemolt Multi-Agent System is a comprehensive development toolkit and automation platform that provides:

### Autonomous Agent Framework
- **Explore** the universe independently using LLM-driven decision making
- **Learn** from experiences and build knowledge over time
- **Collaborate** by sharing information through a persistent knowledge base
- **Play autonomously** with specialized agent types (explorers, miners, traders, fighters, pirates, craftsmen, salvagers)
- **Persist** all discoveries automatically to SQLite storage
- **Customize** agent personalities and behaviors through JSON configuration

### Claude AI Integration (MCP)
- **Direct Control** - Play SpaceMolt through Claude using Model Context Protocol
- **148 Game Commands** - Auto-generated tools from OpenAPI spec covering the entire game API
- **Crafting Intelligence** - Query recipes and plan crafting based on inventory
- **Real-time Updates** - WebSocket and SSE bridges for live game state
- **Multiple Bridges** - Choose between WebSocket, SSE, or service-based architectures

### Development Tools
- **Web UI** - React/TypeScript frontend for monitoring and controlling agents
- **REST API** - Programmatic control and status monitoring
- **TUI Watcher** - Terminal interface for observing agent activity
- **Knowledge Analytics** - Query system for mining discoveries, market data, and routes
- **Automated Agents** - Pre-built specialized bots for common tasks

Each agent has a unique personality that drives their behavior, making decisions based on current situation, past experiences, and cumulative knowledge.

## Key Features

### 🤖 Multiple Integration Modes
- **LLM-Powered Agents** - Autonomous agents with personality-driven decision making
- **Claude Integration** - Direct control via MCP (Model Context Protocol)
- **Automated Bots** - Pre-built specialized agents for specific tasks
- **Programmatic API** - REST API for custom integrations

### 🎮 Comprehensive Game Coverage
- **148 Game Commands** - Auto-generated from OpenAPI spec, covering entire game API
- **All Game Systems** - Navigation, combat, mining, trading, crafting, factions, bases
- **Real-time Updates** - WebSocket and SSE support for live game state
- **Version Tracking** - Automatic API version compatibility checking

### 🧠 Knowledge & Learning
- **Shared Knowledge Base** - SQLite database with discoveries from all agents
- **Advanced Analytics** - Rich deposits, depleting resources, danger zones, market trends
- **Experience Tracking** - Complete action history for learning and improvement
- **Route Optimization** - Fuel cost and travel time learning

### 🛠️ Crafting System
- **Recipe Database** - Complete recipe and skill tree data
- **Material Planning** - Calculate requirements for any craftable item
- **Profit Analysis** - Find profitable crafting opportunities
- **MCP Integration** - Query crafting data through Claude

### 📊 Monitoring & Control
- **React Web UI** - Modern frontend for monitoring and control
- **TUI Watcher** - Terminal interface for observing agent activity
- **REST API** - Programmatic access to agent status and control
- **Agent Registry** - Centralized status and coordination

### 🔐 Flexible Storage
- **Multiple Backends** - File, encrypted SQLite, OS keyring for credentials
- **Knowledge Persistence** - Long-term accumulation across sessions
- **Market Data Capture** - Historical price and listing tracking
- **Captain's Log** - In-game logging and note-taking

## Recent Additions

### Claude AI Integration (MCP)
Play SpaceMolt directly through Claude with the Model Context Protocol. Three bridge implementations (WebSocket, SSE, Service) provide 148 auto-generated tools covering the entire game API. The crafting server adds recipe intelligence for material planning and profit optimization.

### Automated Specialized Agents
Seven pre-built automated agents (explorer, miner, trader, fighter, pirate, salvager, craftsman) provide task-specific automation without requiring LLM setup. Perfect for focused farming, exploration, or testing.

### Web-Based Monitoring
React/TypeScript frontend provides modern UI for monitoring agent activity, viewing knowledge base, and controlling agents. Includes real-time dashboards, map views, and analytics.

### Advanced Crafting System
Complete crafting database with recipe queries, material requirements calculation, and profit analysis. Integrated with both MCP and agent decision-making.

### Enhanced Knowledge Base
Upgraded analytics for market trends, danger zones, route optimization, and resource depletion tracking. Agents share discoveries and learn from collective experience.

### Playbook System
Strategic playbooks guide agents in specialized roles (explorer, miner, trader, fighter, etc.) with proven tactics and decision patterns.

### MCP Integration - Play Through Claude

Use Claude AI to play SpaceMolt directly through the Model Context Protocol:

**Available Bridges:**
- **`mcp-ws-bridge`** - WebSocket bridge with 148 auto-generated game commands
- **`mcp-sse-bridge`** - Server-Sent Events bridge for streaming updates
- **`mcp-bridge-service`** - Service-based architecture for persistent connections
- **`crafting-server`** - MCP server for recipe queries and crafting intelligence

**Claude Integration Features:**
- Full game API access (navigation, combat, mining, trading, crafting, missions, factions)
- Recipe and skill tree queries for crafting decisions
- Real-time game state updates
- Market data and price analysis
- Faction management and warfare
- Base building and management

See [cmd/mcp-ws-bridge/README.md](cmd/mcp-ws-bridge/README.md) and [docs/MCP_TOOLS_IMPLEMENTATION.md](docs/MCP_TOOLS_IMPLEMENTATION.md) for details.

### Automatic Knowledge Persistence & Analytics

All agent discoveries are automatically saved to a SQLite knowledge base with **advanced analytics**:
- **Systems**: Names, positions, security levels, factions, connections
- **POIs**: Stations, asteroid belts, planets with resources
- **Resources**: Mining locations with richness, depletion tracking, and trend analysis
- **Experiences**: Complete action history for learning
- **Anomalies**: Automatic detection of rich deposits, depleting resources, and opportunities
- **Routes**: Optimization through learning fuel costs and travel times
- **Markets**: Price analytics and best buy/sell identification
- **Danger Zones**: Hostile activity tracking and risk assessment

**Documentation:**
- [Knowledge Base Overview](docs/KNOWLEDGE_BASE.md)
- [Enhanced Analytics Guide](docs/ENHANCED_ANALYTICS.md)
- [Implementation Summary](docs/ENHANCEMENTS_SUMMARY.md)

**Query Tools:**
```bash
./scripts/query-knowledge.sh systems          # View discovered systems
./scripts/query-analytics.sh rich-deposits    # Find best mining spots
./scripts/query-analytics.sh anomalies        # View detected opportunities
```

## Quick Start

### Prerequisites

**Core Requirements:**
- **Go** 1.24 or later
- **SpaceMolt Account** - Register at [spacemolt.com](https://spacemolt.com)

**For LLM-Powered Agents:**
- **Ollama** running locally with the `llama3.2` model (or other LLM provider)

**For Claude Integration:**
- **Claude Desktop** or **Claude API access** for MCP functionality

**For Web UI:**
- **Node.js** 18+ and **npm** (for frontend development)

### Installing Ollama (Optional - for LLM agents)

```bash
# Install Ollama
curl -fsSL https://ollama.com/install.sh | sh

# Start Ollama service
ollama serve

# Pull the required model (in another terminal)
ollama pull llama3.2
```

### Building

```bash
# Clone the repository
git clone https://github.com/rsned/spacemolt.git
cd spacemolt-agent-server

# Build core binaries
make build

# Or build specific tools
go build -o bin/agent-server ./cmd/agent-server
go build -o bin/watcher ./cmd/watcher
go build -o bin/mcp-ws-bridge ./cmd/mcp-ws-bridge
go build -o bin/crafting-server ./cmd/crafting-server
```

**Core Executables:**
- `bin/agent-server` - Main server for running autonomous LLM agents
- `bin/watcher` - TUI application for watching agents (optional)
- `bin/mcp-ws-bridge` - MCP bridge for Claude integration
- `bin/crafting-server` - MCP server for crafting queries

**Specialized Automated Agents:**
- `bin/auto-explorer` - Automated exploration bot
- `bin/auto-miner` - Automated mining bot
- `bin/auto-trader` - Automated trading bot
- `bin/auto-fighter` - Automated combat bot
- `bin/auto-pirate` - Automated pirate/raider bot
- `bin/auto-salvager` - Automated salvage bot
- `bin/auto-craftsman` - Automated crafting bot

### Quick Start Guides

Choose your path:

1. **LLM-Powered Autonomous Agents** - Continue reading below for full agent setup
2. **Claude Integration (MCP)** - See [cmd/mcp-ws-bridge/README.md](cmd/mcp-ws-bridge/README.md)
3. **Automated Bots** - See [docs/QUICKSTART.md](docs/QUICKSTART.md) for pre-built automation
4. **Crafting System** - See [docs/spacemolt-crafting-server-spec-final.md](docs/spacemolt-crafting-server-spec-final.md)
5. **Web UI Development** - See [frontend/README.md](frontend/README.md)

### Running Autonomous LLM Agents

The agent-server is the primary way to run autonomous agents. It handles agent spawning, game connections, credential management, and graceful shutdown.

#### Basic Usage

```bash
# Start all agents found in data/agents/
bin/agent-server

# Start specific agents (highest priority method)
bin/agent-server --agents=explorer-7,miner-2

# Use environment variable
export SPACEMOLT_AGENTS=explorer-7,miner-2
bin/agent-server

# Use configuration file (see agents_config.yaml.example)
bin/agent-server --config=agents_config.yaml
```

The server will:
1. Discover agents based on your selection method
2. Load each agent's personality from `data/agents/<id>/personality.json`
3. Check for existing credentials (login) or register new agents
4. Connect to the game server with retry logic
5. Start the autonomous decision loop for each agent
6. Run until you press Ctrl+C for graceful shutdown

#### Watch Agents with TUI (Optional)

After starting the agent-server, you can optionally watch agents in a TUI:

```bash
# In another terminal
bin/watcher --agents=explorer-7,miner-2
```

**Watcher TUI Controls:**
- `q` or `Ctrl+C` - Quit (agents keep running in server)
- `Tab` / `Shift+Tab` - Cycle between agents
- `↑`/`↓` or `j`/`k` - Scroll the action log

**The TUI displays:**
- **Agents Panel** - List of active agents with status indicators
- **Map Panel** - Current system view with discovered POIs
- **Status Panel** - Selected agent's ship stats (fuel, hull, cargo, credits)
- **Action Log** - Real-time log of all agent activities and decisions

#### Test Agent Setup

Test an agent's personality and LLM connection without connecting to the game:

```bash
# Test the explorer-7 personality
bin/agent data/agents/explorer-7/personality.json

# Test with SQLite storage (default)
bin/agent data/agents/miner-2/personality.json --db-backend=sqlite --db-path=test-knowledge.db

# Test with in-memory storage
bin/agent data/agents/pirate-1/personality.json --db-backend=memory
```

## Database Backends

The system supports two knowledge base backends:

### SQLite (Default)

Persists all agent knowledge to disk, allowing discoveries to accumulate across sessions:

```bash
# Use SQLite (default)
./watcher --db-backend=sqlite --db-path=spacemolt-knowledge.db
```

**Benefits:**
- Knowledge persists across restarts
- Agents remember systems, POIs, and connections from previous sessions
- Experience history is maintained
- Enables long-term learning and accumulation

**Database location:**
- Default: `spacemolt-knowledge.db` in current directory
- Custom path via `--db-path` flag

**Query the database:**
```bash
# View all discovered systems
sqlite3 spacemolt-knowledge.db "SELECT id, name, visit_count, last_visited FROM systems;"

# View agent experiences
sqlite3 spacemolt-knowledge.db "SELECT agent_id, type, description, time FROM experiences ORDER BY time DESC LIMIT 20;"

# View registered agents
sqlite3 spacemolt-knowledge.db "SELECT id, name, role, faction FROM agents;"
```

### In-Memory

All knowledge is lost when the program exits:

```bash
# Use in-memory backend
./watcher --db-backend=memory
```

**Use cases:**
- Testing and development
- Fresh start each session
- No database file management

## Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          SpaceMolt Game Server                          │
│                         (game.spacemolt.com)                            │
│                                                                         │
│  WebSocket API │ HTTP API │ OpenAPI Spec                               │
└────────┬────────────┬─────────────┬──────────────────────────────────┘
         │            │             │
         │            │             │ (sync spec)
         │            │             ▼
         │            │        ┌──────────────────┐
         │            │        │ generate-mcp-    │
         │            │        │ tools            │
         │            │        └────────┬─────────┘
         │            │                 │
         │            │                 ▼ (generates 148 tools)
    ┌────┴────────────┴──────────────────────────────────────┐
    │                                                         │
┌───▼────────────┐  ┌──────────────┐  ┌────────────────────┐│
│ Agent Server   │  │ MCP Bridges  │  │  Web UI / API      ││
│                │  │              │  │                    ││
│ ┌────────────┐ │  │• ws-bridge   │  │• React Frontend    ││
│ │Agent Mgr   │ │  │• sse-bridge  │  │• REST API Server   ││
│ │            │ │  │• service     │  │• Agent Registry    ││
│ │┌──────────┐│ │  │              │  │                    ││
│ ││Runner 1  ││ │  └──────┬───────┘  └─────────┬──────────┘│
│ ││Explorer  ││ │         │                    │            │
│ │└──────────┘│ │         │                    │            │
│ │┌──────────┐│ │         ▼                    ▼            │
│ ││Runner 2  ││ │  ┌──────────────┐   ┌────────────────┐   │
│ ││Miner     ││ │  │ Claude AI    │   │   Monitoring   │   │
│ │└──────────┘│ │  │              │   │   Dashboard    │   │
│ │┌──────────┐│ │  │• Desktop     │   │                │   │
│ ││Runner 3  ││ │  │• API         │   │                │   │
│ ││Fighter   ││ │  │              │   │                │   │
│ │└──────────┘│ │  └──────────────┘   └────────────────┘   │
│ └────────────┘ │                                           │
└────────┬───────┘                                           │
         │                                                   │
         │                                                   │
    ┌────┴─────────────────────┐    ┌──────────────────────┐│
    │                          │    │                      ││
┌───▼────────────┐  ┌─────────▼────▼──┐  ┌───────────────┐││
│ Crafting       │  │  Knowledge Base  │  │  Credentials  │││
│ Server (MCP)   │  │  (SQLite)        │  │  Storage      │││
│                │  │                  │  │               │││
│• Recipe DB     │  │• Systems/POIs    │  │• file         │││
│• Skill Tree    │  │• Experiences     │  │• sqlite       │││
│• Material      │  │• Market Data     │  │• keyring      │││
│  Requirements  │  │• Routes          │  │               │││
│• Profit Calc   │  │• Danger Zones    │  │               │││
└────────────────┘  └──────────────────┘  └───────────────┘││
                                                            ││
    ┌───────────────────────────────────────────────────────┘│
    │                                                         │
┌───▼────────────┐  ┌──────────────┐  ┌────────────────────┐│
│ Automated      │  │ Ollama LLM   │  │  Watcher TUI       ││
│ Agents         │  │              │  │                    ││
│                │  │• llama3.2    │  │• Multi-agent view  ││
│• auto-explorer │  │• mistral     │  │• Map display       ││
│• auto-miner    │  │• other       │  │• Status panels     ││
│• auto-trader   │  │  models      │  │• Action logs       ││
│• auto-fighter  │  │              │  │                    ││
│• auto-pirate   │  └──────────────┘  └────────────────────┘│
│• auto-salvager │                                           │
│• auto-craftsman│                                           │
└────────────────┘                                           │
                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Key Components:**

1. **Agent Server** - Core autonomous agent framework with LLM integration
2. **MCP Bridges** - Connect Claude AI to the game (WebSocket, SSE, Service)
3. **Crafting Server** - MCP server for recipe/skill queries and crafting planning
4. **Knowledge Base** - Shared SQLite database for discoveries and analytics
5. **Web UI** - React frontend for monitoring and control
6. **API Server** - REST API for programmatic agent control
7. **Automated Agents** - Specialized pre-built bots for specific tasks
8. **Watcher TUI** - Terminal UI for observing agent activity
9. **Credentials** - Flexible storage (file, encrypted SQLite, OS keyring)

## Command-Line Options

### Agent Server

```bash
bin/agent-server [options]

Agent Selection:
  --agents string
        Comma-separated list of agent IDs (highest priority)
  --agents-dir string
        Directory containing agent personalities (default "data/agents")
  --config string
        Path to configuration file (default "agents_config.yaml")

Server Configuration:
  --server-url string
        Game server WebSocket URL (default "wss://game.spacemolt.com/ws")
  --max-agents int
        Maximum concurrent agents (default 10)
  --decision-interval duration
        Decision interval for agents (default 5s)

Knowledge Base:
  --db-backend string
        Backend: "sqlite" or "memory" (default "sqlite")
  --db-path string
        Path to SQLite database (default "data/spacemolt-knowledge.db")

LLM Configuration:
  --llm-url string
        LLM server URL (default "http://localhost:11434")
  --llm-model string
        LLM model name (default "llama3.2")

Credentials:
  --creds-backend string
        Backend: "file", "sqlite", or "keyring" (default "file")
  --creds-path string
        Path for credentials storage (default "data/credentials")
```

Environment Variables:
- `SPACEMOLT_AGENTS` - Comma-separated agent IDs (medium priority)
- `SPACEMOLT_PASSPHRASE` - Passphrase for SQLite credential encryption

See [cmd/agent-server/README.md](cmd/agent-server/README.md) for detailed documentation.

### Watcher (Optional)

```bash
bin/watcher [options]

Options:
  --agents string
        Comma-separated list of agent IDs to watch
  --db-backend string
        Database backend: "sqlite" or "memory" (default "sqlite")
  --db-path string
        Path to SQLite database file (default "spacemolt-knowledge.db")
  --debug
        Enable debug logging to stderr
  --log-file string
        Write debug logs to file instead of stderr
```

### Agent Test Utility

```bash
bin/agent [options] personality.json

Options:
  --db-backend string
        Database backend: "sqlite" or "memory" (default "sqlite")
  --db-path string
        Path to SQLite database file (default "spacemolt-knowledge.db")
```

## How It Works

### System Architecture

The agent server manages multiple autonomous agents, each with their own:
- **WebSocket connection** to the game server (required by game architecture)
- **Personality** defining behavior, traits, and motivations
- **Memory** for tracking experiences and discoveries
- **Decision loop** making autonomous choices every 5-10 seconds

### Agent Lifecycle

1. **Discovery** - Server finds agents using priority order:
   - CLI flags (`--agents=id1,id2`) → Environment (`SPACEMOLT_AGENTS`) → Config file → Auto-discover

2. **Initialization** - For each agent:
   - Load personality from `data/agents/<agent-id>/personality.json`
   - Check for existing credentials in provider or `data/agents/<agent-id>/credentials.json`

3. **Connection & Authentication** - Each agent independently:
   - **First time (no credentials)**:
     - Connects to game server
     - Registers new account with sanitized username
     - Receives authentication token
     - Saves credentials to provider (with fallback to file)
   - **Returning agent (has credentials)**:
     - Connects to game server
     - Logs in with username and token
     - Resumes previous session
   - Both include retry logic: up to 3 attempts with exponential backoff (2s, 4s, 8s)

4. **Autonomous Play Loop** - Each agent continuously:
   - Receives game state updates every 10 seconds (tick)
   - Examines current situation (location, fuel, cargo, nearby objects)
   - Queries past experiences from knowledge base
   - Consults personality traits and motivations
   - Uses LLM to decide next action
   - Executes action through game client
   - Learns from the result (success/failure)
   - Updates knowledge base with discoveries

5. **Graceful Shutdown** - On Ctrl+C:
   - Server stops all agent decision loops
   - Closes all WebSocket connections
   - Commits knowledge to database
   - Exits cleanly

### Tick-Based Action System

The game server operates on a **10-second tick** cycle:
- **Action commands** (mine, travel, attack, etc.): Limited to 1 per tick
- **Query commands** (get_status, get_system, etc.): Unlimited

Agents respect this by:
- Tracking the last tick when an action was executed
- Only sending action commands on new ticks
- Using queries freely between ticks for information gathering
- Retrying immediately if an action fails (failures don't consume tick)

### Credential Management

The system supports flexible credential storage:

**Backends:**
- `file` - Store in `<creds-path>/<agent-id>.json` (default)
- `sqlite` - Encrypted SQLite database (requires `SPACEMOLT_PASSPHRASE`)
- `keyring` - OS keyring/keychain integration

**Fallback mechanism:**
1. Try primary provider (configured backend)
2. If provider fails, fall back to `data/agents/<agent-id>/credentials.json`
3. Load also checks fallback location if provider has no credentials

This ensures agents can always save/load credentials even if the primary backend fails.

### Supported Actions

**Navigation:**
- `undock` - Leave the current station
- `dock` - Dock at a station in the current system
- `travel` - Travel to a POI within the system
- `jump` - Jump to another star system

**Resource Gathering:**
- `mine` - Mine resources at the current location

**Queries (no tick cost):**
- `get_status` - Current player/ship state
- `get_system` - Current system details
- `get_ship` - Detailed vessel specifications
- `get_skills` - Full skill tree

See [docs/api_commands_reference.md](docs/api_commands_reference.md) for the complete list of 61 action commands and 18 query commands.

### Knowledge Persistence

With SQLite backend (default), agents accumulate knowledge over time:

- **Systems** - Discovered star systems with visit counts and timestamps
- **Connections** - Known jump routes between systems
- **POIs** - Points of interest (stations, asteroids, anomalies)
- **Experiences** - Agent action history and outcomes (last 100 per agent)
- **Agents** - Registered agent metadata
- **Shared knowledge** - All agents see each other's discoveries

## Agent Personalities

The system includes diverse agent personalities, each specialized for different playstyles:

- **Explorers** - Curiosity-driven agents who document discoveries
- **Miners** - Resource-focused industrial miners
- **Traders** - Commerce-focused merchants maximizing profits
- **Fighters** - Combat specialists and bounty hunters
- **Pirates** - Aggressive raiders plundering trade routes
- **Salvagers** - Scavengers finding value in debris
- **Craftsmen** - Builders and manufacturers
- **Engineers** - Technical specialists and researchers

See [data/agents/README.md](data/agents/README.md) for detailed documentation on creating and customizing agent personalities.

### Creating New Agents

1. Create a new directory: `mkdir -p data/agents/my-agent`
2. Add a `personality.json` file with the agent's configuration
3. Run the watcher: `./watcher --agents=my-agent`

Example personality structure:

```json
{
  "name": "My Agent",
  "id": "my-agent",
  "role": "Explorer",
  "faction": "Independent",
  "traits": {
    "curiosity": 0.8,
    "risk_tolerance": 0.5,
    "altruism": 0.7,
    "patience": 0.6,
    "aggression": 0.2
  },
  "motivations": {
    "primary": "explore_unknown",
    "secondary": "help_others",
    "tertiary": "document_findings"
  },
  "skills": {
    "navigation": "intermediate",
    "scanning": "basic",
    "combat": "novice"
  },
  "biography": "A newly independent explorer seeking to make their mark."
}
```

## Example Sessions

### Scenario 1: Starting from Scratch (First Agent)

Create and register your first autonomous agent:

```bash
# 1. Make sure you have an agent personality
ls data/agents/explorer-7/personality.json

# 2. Start the agent server (will auto-register)
bin/agent-server --agents=explorer-7

# Output:
# === Spacemolt Agent Server ===
# Found 1 agent(s) to start: [explorer-7]
# ✓ Knowledge base initialized (sqlite)
# ✓ LLM client initialized (llama3.2 @ http://localhost:11434)
# ✓ Credentials provider initialized (file)
# ✓ Agent manager created (max agents: 10)
#
# === Spawning Agents ===
# [explorer-7] Spawning agent: Deep Space Seven (Explorer)
# [explorer-7] Registering new agent
# [explorer-7] ✓ Connected successfully
# [explorer-7] ✓ Registered as explorer-7-Deep_Space_Seven
# [explorer-7] ✓ Saved credentials to data/agents/explorer-7/credentials.json
# ✓ [explorer-7] Started successfully (faction: Terran)
#
# === Agent Server Started ===
# ✓ Successfully started: 1/1 agents
# Press Ctrl+C to stop all agents and exit

# The agent is now:
# - Registered in the game with a new account
# - Credentials saved to data/agents/explorer-7/credentials.json
# - Autonomously exploring and making decisions
# - Building knowledge in data/spacemolt-knowledge.db

# Let it run for 5-10 minutes, then stop with Ctrl+C
# Credentials are saved - next time it will log in automatically!
```

### Scenario 2: Running with Existing Agents

Restart agents that have already been registered (they log in automatically):

```bash
# Start the same agent - it will login instead of register
bin/agent-server --agents=explorer-7

# Output:
# [explorer-7] Spawning agent: Deep Space Seven (Explorer)
# [explorer-7] Logging in with existing credentials
# [explorer-7] ✓ Connected successfully
# [explorer-7] ✓ Logged in successfully
# ✓ [explorer-7] Started successfully (faction: Terran)

# The agent:
# - Found existing credentials
# - Logged in with saved username/token
# - Resumed from its last position in the game
# - Remembers all previous discoveries from knowledge base
```

### Scenario 3: Multi-Agent Fleet

Run multiple agents simultaneously with diverse specializations:

```bash
# Start a diverse fleet
bin/agent-server --agents=explorer-7,miner-2,trader-1,fighter-1

# Output shows each agent starting:
# [explorer-7] ✓ Logged in successfully (has credentials)
# [miner-2] ✓ Registered as miner-2-Ore_Crusher (first time - no credentials)
# [trader-1] ✓ Logged in successfully (has credentials)
# [fighter-1] ✓ Registered as fighter-1-Steel_Fang (first time - no credentials)
#
# ✓ Successfully started: 4/4 agents

# Watch as they collaborate:
# - Explorer-7: Maps new systems and reports discoveries
# - Miner-2: Focuses on resource extraction
# - Trader-1: Moves goods between stations
# - Fighter-1: Patrols for threats
#
# All agents share the same knowledge base!
# Explorer's discoveries help Miner find resources
# Trader benefits from both Explorer and Miner's intel
```

### Scenario 4: Persistent Knowledge Accumulation

Demonstrate how knowledge persists across sessions:

```bash
# Session 1: Initial exploration
bin/agent-server --agents=explorer-7
# Let run for 5 minutes, then Ctrl+C

# Check what was discovered
sqlite3 data/spacemolt-knowledge.db "SELECT id, name, visit_count FROM systems;"
# Output:
# Sol|Sol|3
# Alpha-Centauri|Alpha Centauri|2
# Sirius|Sirius|1

# Session 2: Add more explorers
bin/agent-server --agents=explorer-7,explorer-8,explorer-9

# All three explorers:
# - Share knowledge of the 3 systems already discovered
# - Don't waste time re-exploring known territory
# - Can branch out in different directions
# - Build on each other's discoveries

# Session 3: Bring in specialists after exploration
bin/agent-server --agents=explorer-7,miner-2,miner-3,trader-1

# Specialists benefit from exploration:
# - Miners know which systems have rich asteroids
# - Traders know which stations buy/sell what
# - No wasted exploration time for specialists
```

### Scenario 5: Different Configuration Methods

Use different methods to select agents:

```bash
# Method 1: CLI flags (highest priority)
bin/agent-server --agents=miner-2,miner-3

# Method 2: Environment variable
export SPACEMOLT_AGENTS=explorer-7,trader-1
bin/agent-server

# Method 3: Configuration file
cat > agents_config.yaml <<EOF
agents:
  enabled:
    - explorer-7
    - explorer-8
    - miner-2
    - miner-3
    - trader-1
EOF
bin/agent-server --config=agents_config.yaml

# Method 4: Auto-discover (all agents with personality.json)
bin/agent-server --agents-dir=data/agents
# Starts ALL agents found in data/agents/
```

### Scenario 6: Custom Configuration

Run with custom backends and settings:

```bash
# Use SQLite for everything with encryption
export SPACEMOLT_PASSPHRASE=my-secure-password
bin/agent-server \
  --agents=explorer-7,miner-2 \
  --db-backend=sqlite \
  --db-path=data/game-knowledge.db \
  --creds-backend=sqlite \
  --creds-path=data/secure-creds.db \
  --llm-model=mistral \
  --decision-interval=3s

# Use memory mode for testing (no persistence)
bin/agent-server \
  --agents=test-agent \
  --db-backend=memory \
  --creds-backend=file

# Connect to custom game server
bin/agent-server \
  --agents=explorer-7 \
  --server-url=ws://localhost:8080/ws
```

### Scenario 7: Handling Failures

The server continues if some agents fail:

```bash
bin/agent-server --agents=valid-agent,missing-agent,explorer-7

# Output:
# ❌ [missing-agent] Failed to load personality: file not found
# ✓ [valid-agent] Started successfully
# ✓ [explorer-7] Started successfully
#
# ⚠ Failed to start: 1 agents: [missing-agent]
# ✓ Successfully started: 2/3 agents
# Server continues running with 2 agents

# If ALL agents fail, server exits with error:
bin/agent-server --agents=nonexistent1,nonexistent2
# ❌ FATAL: All agents failed to start
# Check your configuration and credentials
# (exits with code 1)
```

## Claude Integration via MCP

Play SpaceMolt through Claude AI using the Model Context Protocol.

### Quick Start with MCP

```bash
# Build the MCP bridge
go build -o bin/mcp-ws-bridge ./cmd/mcp-ws-bridge

# Configure Claude Desktop
# Add to ~/Library/Application Support/Claude/claude_desktop_config.json:
{
  "mcpServers": {
    "spacemolt": {
      "command": "/path/to/bin/mcp-ws-bridge",
      "args": ["--username", "your-username", "--token", "your-token"]
    }
  }
}

# Restart Claude Desktop
# Now Claude can control your SpaceMolt character!
```

### Available MCP Servers

1. **mcp-ws-bridge** - Full game control (148 commands)
   - Navigation, combat, mining, trading
   - Faction management, base building
   - Market orders, crafting, missions
   - See [cmd/mcp-ws-bridge/README.md](cmd/mcp-ws-bridge/README.md)

2. **mcp-sse-bridge** - Server-Sent Events variant
   - Alternative architecture for streaming updates
   - Same functionality as WebSocket bridge

3. **crafting-server** - Crafting intelligence
   - Query recipes by material or skill
   - Calculate crafting requirements
   - Find profitable crafting opportunities
   - See [docs/spacemolt-crafting-server-spec-final.md](docs/spacemolt-crafting-server-spec-final.md)

### Example Claude Interactions

```
You: "Check my ship status in SpaceMolt"
Claude: *uses get_status tool*

You: "Mine some asteroids"
Claude: *uses mine tool repeatedly*

You: "What can I craft with iron ore?"
Claude: *uses crafting-server to query recipes*

You: "Travel to the nearest station and sell my cargo"
Claude: *uses travel, dock, and view_market tools*
```

### Auto-Generated Tools

The MCP bridge tools are automatically generated from the game's OpenAPI spec:

```bash
# Update tools when game API changes
make update-mcp

# This will:
# 1. Download latest openapi.json from game server
# 2. Regenerate all 148 tool definitions
# 3. Update cmd/mcp-ws-bridge/tools_generated.go
```

See [docs/MCP_TOOLS_IMPLEMENTATION.md](docs/MCP_TOOLS_IMPLEMENTATION.md) for details.

## Automated Specialized Agents

Pre-built bots for specific tasks (no LLM required):

### Available Automated Agents

- **auto-explorer** - Systematically explores and maps systems
- **auto-miner** - Mines resources and sells for profit
- **auto-trader** - Trades goods between stations
- **auto-fighter** - Engages in combat and bounty hunting
- **auto-pirate** - Raids and plunders (use responsibly!)
- **auto-salvager** - Salvages wrecks and debris
- **auto-craftsman** - Crafts items based on available materials

### Running Automated Agents

```bash
# Build the agent
go build -o bin/auto-miner ./cmd/auto-miner

# Run with credentials
bin/auto-miner --username your-username --token your-token

# Or use credential file
bin/auto-miner --creds-file data/agents/miner-1/credentials.json

# Run in background
nohup bin/auto-miner --username your-username --token your-token > logs/miner.log 2>&1 &
```

See [docs/QUICKSTART.md](docs/QUICKSTART.md) for detailed automated agent documentation.

## Web Interface

### React Frontend

Modern web UI for monitoring and controlling agents:

```bash
# Setup frontend
cd frontend
npm install

# Development mode
npm run dev

# Production build
npm run build
```

See [frontend/README.md](frontend/README.md) for details.

### Simple HTML Dashboard

Lightweight monitoring interface:

```bash
# Serve the web directory
cd web
python3 -m http.server 8080

# Visit http://localhost:8080
```

## REST API

Programmatic control and monitoring:

```bash
# Start API server (usually part of agent-server)
bin/agent-server --enable-api --api-port 8080

# Query agent status
curl http://localhost:8080/api/agents

# Get specific agent
curl http://localhost:8080/api/agents/explorer-7

# Stream agent events
curl http://localhost:8080/api/stream
```

See [pkg/api/](pkg/api/) for API documentation.

## Development

### Running Linters

```bash
golangci-lint run ./...
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./pkg/knowledge/...

# Run tests with verbose output
go test ./pkg/knowledge/... -v

# Run benchmarks
go test ./pkg/knowledge/... -bench=.
```

### Project Structure

```
spacemolt-agent-server/
├── cmd/                        # Command-line executables
│   ├── agent-server/           # Main LLM agent server (primary)
│   ├── watcher/                # Multi-agent TUI observer
│   ├── mcp-ws-bridge/          # MCP WebSocket bridge for Claude
│   ├── mcp-sse-bridge/         # MCP SSE bridge (alternative)
│   ├── mcp-bridge-service/     # MCP service-based architecture
│   ├── crafting-server/        # MCP crafting query server
│   ├── generate-mcp-tools/     # Auto-generate MCP tools from OpenAPI
│   ├── update-server-docs/     # Sync API docs from game server
│   ├── auto-explorer/          # Automated exploration bot
│   ├── auto-miner/             # Automated mining bot
│   ├── auto-trader/            # Automated trading bot
│   ├── auto-fighter/           # Automated combat bot
│   ├── auto-pirate/            # Automated pirate/raider bot
│   ├── auto-salvager/          # Automated salvage bot
│   ├── auto-craftsman/         # Automated crafting bot
│   ├── skill-tree/             # Skill tree visualization
│   └── ...                     # 20+ additional utilities
├── pkg/                        # Reusable Go packages
│   ├── agent/                  # Agent framework & personality system
│   ├── game/                   # Game client & WebSocket communication
│   ├── credentials/            # Multi-backend credential storage
│   ├── knowledge/              # SQLite knowledge base & analytics
│   ├── llm/                    # LLM client (Ollama/OpenAI compatible)
│   ├── tui/                    # Terminal UI components
│   ├── api/                    # REST API server
│   ├── registry/               # Agent status registry
│   ├── prompts/                # Prompt management system
│   ├── crafting/               # Crafting system data structures
│   └── version/                # Version compatibility checking
├── internal/                   # Internal packages
│   └── protocol/               # Internal protocol definitions
├── frontend/                   # React/TypeScript web UI
│   ├── src/                    # React components & pages
│   ├── public/                 # Static assets
│   ├── package.json            # Node dependencies
│   └── vite.config.ts          # Vite configuration
├── web/                        # Simple HTML/JS monitoring interface
│   ├── index.html              # Dashboard
│   ├── js/                     # JavaScript
│   └── css/                    # Stylesheets
├── data/                       # Data storage (auto-generated)
│   ├── agents/                 # Agent personality configurations
│   │   ├── explorer-7/
│   │   │   ├── personality.json
│   │   │   ├── biography.md
│   │   │   └── credentials.json (auto-generated)
│   │   └── ...
│   ├── spacemolt-knowledge.db  # Shared knowledge base (SQLite)
│   └── credentials/            # File-based credentials
├── docs/                       # Documentation (40+ documents)
│   ├── QUICKSTART.md           # Quick start guide
│   ├── MCP_TOOLS_IMPLEMENTATION.md
│   ├── spacemolt-crafting-server-spec-final.md
│   ├── api_commands_reference.md
│   ├── KNOWLEDGE_BASE.md
│   └── ...
├── playbook/                   # Strategic playbooks for agents
│   ├── explorer.md
│   ├── miner.md
│   ├── trader.md
│   ├── fighter.md
│   └── ...
├── server_docs/                # Auto-synced API documentation
│   ├── openapi.json            # OpenAPI specification
│   ├── api.md                  # API reference
│   └── skill.md                # Skill tree documentation
├── scripts/                    # Utility scripts
│   ├── query-knowledge.sh      # Query knowledge base
│   ├── query-analytics.sh      # Query analytics
│   └── ...
├── Makefile                    # Build automation
├── agents_config.yaml.example  # Example agent configuration
└── go.mod                      # Go module definition
```

## Troubleshooting

### Game Connection Issues

```bash
# Test connection to game server
curl https://game.spacemolt.com/api/version

# Verify WebSocket connectivity
# Check firewall rules allow outbound WSS connections

# Test with simple client
go run ./cmd/play-simple --username your-user --token your-token
```

### Ollama Connection Issues (for LLM agents)

```bash
# Verify Ollama is running
curl http://localhost:11434/api/tags

# Check if model is available
ollama list

# Pull the required model
ollama pull llama3.2

# Test Ollama directly
curl http://localhost:11434/api/generate -d '{"model":"llama3.2","prompt":"test"}'
```

### MCP Bridge Issues

```bash
# Test MCP bridge manually
bin/mcp-ws-bridge --username your-user --token your-token

# Check Claude Desktop logs
# macOS: ~/Library/Logs/Claude/
# Check for MCP server startup errors

# Verify configuration
cat ~/Library/Application\ Support/Claude/claude_desktop_config.json

# Common issues:
# - Incorrect path to mcp-ws-bridge binary
# - Missing or invalid credentials
# - Firewall blocking game server connection
```

### Agent Not Responding

- Check agent logs in the TUI for errors
- Verify Ollama is running: `curl http://localhost:11434/api/tags`
- Enable debug logging: `./watcher --debug --log-file=debug.log`
- Check agent personality file is valid JSON
- Ensure agent has valid credentials in credentials.json

### Database Issues

```bash
# Check database contents
sqlite3 data/spacemolt-knowledge.db ".schema"
sqlite3 data/spacemolt-knowledge.db "SELECT * FROM systems;"

# Check for corruption
sqlite3 data/spacemolt-knowledge.db "PRAGMA integrity_check;"

# Reset database (delete and start fresh)
rm data/spacemolt-knowledge.db
bin/agent-server
```

### Agents Not Spawning

- Verify agent directory exists: `ls data/agents/explorer-7/`
- Check personality.json is valid: `jq . data/agents/explorer-7/personality.json`
- Verify credentials if reusing existing agent
- Review debug logs for specific errors
- Check agent-server logs for detailed error messages

### Frontend Development Issues

```bash
# Clear frontend cache
cd frontend
rm -rf node_modules package-lock.json
npm install

# Check for TypeScript errors
npm run type-check

# Verify Vite dev server
npm run dev
# Should start on http://localhost:5173
```

### Build Issues

```bash
# Clean build cache
go clean -cache -testcache -modcache

# Update dependencies
go mod tidy
go mod download

# Rebuild everything
make clean
make build

# Check for lint errors
make lint
```

## Contributing

Contributions are welcome! Areas of interest:

**Agent Development:**
- New agent personalities and behaviors
- Improved decision-making algorithms and strategies
- Enhanced learning and knowledge sharing
- Better prompt engineering for LLM agents

**MCP Integration:**
- Additional MCP bridges and servers
- Better Claude integration patterns
- Tool improvements and new capabilities
- Enhanced crafting intelligence

**Automation:**
- New specialized automated agents
- Improved efficiency algorithms
- Better resource management
- Advanced trading strategies

**UI/UX:**
- React frontend enhancements
- New monitoring dashboards
- Better visualization of agent activity
- Mobile-responsive designs

**Infrastructure:**
- Performance optimizations
- Better error handling and recovery
- Enhanced testing coverage
- Documentation improvements

**Analytics:**
- Advanced knowledge base queries
- Better market analysis
- Route optimization
- Profit calculation improvements

See existing code for patterns and conventions. All contributions should:
- Pass `make lint` and `make test`
- Include tests for new functionality
- Update documentation as needed
- Follow Go best practices

## License

MIT License - See LICENSE file for details

## Acknowledgments

**Game & Platform:**
- [SpaceMolt](https://spacemolt.com) - The game that makes this possible
- SpaceMolt community for testing and feedback

**AI & LLM:**
- [Ollama](https://ollama.ai) - Local LLM inference
- [Anthropic Claude](https://anthropic.com) - MCP integration and AI assistance
- [Model Context Protocol](https://modelcontextprotocol.io) - Standardized AI integration

**Core Libraries:**
- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) - Pure Go SQLite
- [coder/websocket](https://github.com/coder/websocket) - WebSocket client

**Frontend:**
- [React](https://react.dev) - UI framework
- [TypeScript](https://www.typescriptlang.org) - Type safety
- [Vite](https://vite.dev) - Build tool
- [Tailwind CSS](https://tailwindcss.com) - Styling

**Development:**
- [golangci-lint](https://golangci-lint.run) - Go linting
- [Go 1.24](https://go.dev) - Programming language
- OpenAPI specification - API documentation and code generation
