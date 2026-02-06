# Spacemolt Multi-Agent System

An autonomous multi-agent system for the [SpaceMolt](https://spacemolt.com) game. AI agents explore, mine, trade, and interact with the game universe using LLM-powered decision making.

## Overview

The Spacemolt Multi-Agent System transforms the single-client game into a platform where multiple autonomous AI agents:

- **Explore** the universe independently using LLM-driven decision making
- **Learn** from their experiences and build knowledge over time
- **Collaborate** by sharing information with other agents through a persistent knowledge base
- **Play autonomously** while a human watcher observes via a TUI interface
- **Persist** all discoveries automatically to SQLite storage

Each agent has a unique personality that drives their behavior, and they make decisions about what to do next based on their current situation, past experiences, and cumulative knowledge of the universe.

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

- **Go** 1.24 or later
- **Ollama** running locally with the `llama3.2` model

### Installing Ollama

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

# Build all binaries
go build -o bin/agent-server ./cmd/agent-server
go build -o bin/watcher ./cmd/watcher
go build -o bin/agent ./cmd/agent
```

This creates three executables:
- `bin/agent-server` - The main server for running autonomous agents
- `bin/watcher` - TUI application for watching agents (optional)
- `bin/agent` - Test utility for verifying agent setup

### Running

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

```
┌─────────────────────────────────────────────────────────────┐
│                   Agent Server (Primary)                    │
│                                                             │
│  ┌────────────────────────────────────────────────────┐   │
│  │            Agent Manager                           │   │
│  │  • Agent discovery & selection                     │   │
│  │  • Spawning & lifecycle management                 │   │
│  │  • Credential management (register/login)          │   │
│  │  • Graceful shutdown                               │   │
│  └────────────────┬───────────────────────────────────┘   │
│                   │                                         │
│     ┌─────────────┼─────────────┐                          │
│     │             │             │                          │
│  ┌──▼──────┐  ┌──▼──────┐  ┌──▼──────┐                   │
│  │Runner 1 │  │Runner 2 │  │Runner 3 │                   │
│  │Explorer │  │Miner    │  │Fighter  │                   │
│  │         │  │         │  │         │                   │
│  │ Agent   │  │ Agent   │  │ Agent   │                   │
│  │ +Game   │  │ +Game   │  │ +Game   │                   │
│  │ Client  │  │ Client  │  │ Client  │                   │
│  └──┬──────┘  └──┬──────┘  └──┬──────┘                   │
│     │            │            │                            │
│     └────────────┼────────────┘                            │
│                  │                                         │
└──────────────────┼─────────────────────────────────────────┘
                   │
     ┌─────────────┼─────────────┐
     │             │             │
     │    ┌────────▼────────┐    │
     │    │ Knowledge Base  │    │
     │    │  (SQLite/Mem)   │    │
     │    │                 │    │
     │    │ • Systems       │    │
     │    │ • Connections   │    │
     │    │ • POIs          │    │
     │    │ • Experiences   │    │
     │    └─────────────────┘    │
     │                            │
     │    ┌─────────────────┐    │
     │    │  Ollama LLM     │    │
     │    │  (llama3.2)     │    │
     │    └─────────────────┘    │
     │                            │
     │    ┌─────────────────┐    │
     │    │  Credentials    │    │
     │    │  (file/sqlite/  │    │
     │    │   keyring)      │    │
     │    └─────────────────┘    │
     │                            │
     └────────────────────────────┘

┌─────────────────────────────────────────┐
│  Watcher TUI (Optional Observer)       │
│  ┌──────────┐ ┌──────────┐ ┌─────────┐ │
│  │Agents    │ │  Map     │ │ Status  │ │
│  │[Agent 1] │ │          │ │         │ │
│  │[Agent 2] │ │          │ │         │ │
│  │[Agent 3] │ │          │ │         │ │
│  └──────────┘ └──────────┘ └─────────┘ │
└─────────────────────────────────────────┘

           ┌──────────────────────┐
           │  Game Server         │
           │  (spacemolt.com)     │
           │                      │
           │  WebSocket per agent │
           └──────────────────────┘
```

**Key Points:**
- Agent server is the primary component (runs headless)
- Each agent has its own WebSocket connection (required by game)
- Each Runner manages an Agent + GameClient + play loop
- Watcher TUI is optional for observation
- All agents share Knowledge Base and LLM

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
        Path to SQLite database (default "data/spacemolt-kb.db")

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
# - Building knowledge in data/spacemolt-kb.db

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
sqlite3 data/spacemolt-kb.db "SELECT id, name, visit_count FROM systems;"
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
├── cmd/
│   ├── agent-server/     # Main agent server (primary entry point)
│   ├── watcher/          # Multi-agent TUI application (optional)
│   └── agent/            # Agent test utility
├── pkg/
│   ├── agent/            # Agent framework, manager, and runner
│   │   ├── base.go       # Base agent implementation
│   │   ├── manager.go    # Multi-agent manager
│   │   ├── runner.go     # Agent play loop with tick awareness
│   │   └── personality.go # Personality system
│   ├── game/             # Game client and state types
│   │   ├── client.go     # WebSocket game client
│   │   ├── interface.go  # GameClient interface
│   │   └── state.go      # Game state management
│   ├── credentials/      # Credential storage backends
│   │   ├── file.go       # File-based storage
│   │   ├── sqlite.go     # Encrypted SQLite storage
│   │   └── keyring.go    # OS keyring integration
│   ├── knowledge/        # Knowledge base (SQLite + memory)
│   │   ├── sqlite.go     # SQLite backend
│   │   └── memory.go     # In-memory backend
│   ├── llm/              # Ollama LLM client
│   └── tui/              # TUI components
├── data/
│   ├── agents/           # Agent personality definitions
│   │   ├── explorer-7/
│   │   │   ├── personality.json
│   │   │   └── credentials.json (auto-generated)
│   │   ├── miner-2/
│   │   └── ...
│   ├── spacemolt-kb.db   # SQLite knowledge base (auto-generated)
│   └── credentials/      # File-based credentials (auto-generated)
├── docs/
│   ├── agent_startup.md  # Complete architecture documentation
│   └── api_commands_reference.md # Game API documentation
└── agents_config.yaml.example # Example configuration file
```

## Troubleshooting

### Ollama Connection Issues

```bash
# Verify Ollama is running
curl http://localhost:11434/api/tags

# Check if model is available
ollama list

# Pull the required model
ollama pull llama3.2
```

### Agent Not Responding

- Check agent logs in the TUI for errors
- Verify Ollama is running: `curl http://localhost:11434/api/tags`
- Enable debug logging: `./watcher --debug --log-file=debug.log`
- Check agent personality file is valid JSON

### Database Issues

```bash
# Check database contents
sqlite3 spacemolt-knowledge.db ".schema"
sqlite3 spacemolt-knowledge.db "SELECT * FROM systems;"

# Reset database (delete and start fresh)
rm spacemolt-knowledge.db
./watcher
```

### Agents Not Spawning

- Verify agent directory exists: `ls data/agents/explorer-7/`
- Check personality.json is valid: `jq . data/agents/explorer-7/personality.json`
- Review debug logs for specific errors

## Contributing

Contributions are welcome! Areas of interest:

- New agent personalities
- Improved decision-making algorithms
- Enhanced UI/UX for the watcher
- Additional game actions and strategies
- Performance optimizations

## License

MIT License - See LICENSE file for details

## Acknowledgments

- [SpaceMolt](https://spacemolt.com) - The game that makes this possible
- [Ollama](https://ollama.ai) - Local LLM inference
- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) - Pure Go SQLite
