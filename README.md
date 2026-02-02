# Spacemolt Multi-Agent System

An autonomous multi-agent system for the [SpaceMolt](https://spacemolt.com) game. AI agents explore, mine, trade, and interact with the game universe using LLM-powered decision making.

## Overview

The Spacemolt Multi-Agent System transforms the single-client game into a platform where multiple autonomous AI agents:

- **Explore** the universe independently using LLM-driven decision making
- **Learn** from their experiences and build knowledge over time
- **Collaborate** by sharing information with other agents
- **Play autonomously** while a human watcher observes via a TUI interface
- **Persist** knowledge across sessions using SQLite storage

Each agent has a unique personality that drives their behavior, and they make decisions about what to do next based on their current situation, past experiences, and knowledge of the universe.

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
cd spacemolt

# Build all binaries
go build ./cmd/watcher
go build ./cmd/agent
```

This creates two executables:
- `watcher` - The main TUI application for watching autonomous agents
- `agent` - A test utility for verifying agent setup

### Running

#### Start the Watcher (Recommended)

The watcher spawns autonomous AI agents and displays their activity in a TUI:

```bash
# Run with default settings (SQLite storage, explorer-7 agent)
./watcher

# Run with multiple agents
./watcher --agents="explorer-7,miner-2,fighter-1"

# Run with all available agents
./watcher --agents="explorer-7,explorer-8,explorer-9,miner-2,miner-3,miner-4,trader-1,trader-2,fighter-1,fighter-2,pirate-1,pirate-2"

# Enable debug logging to file
./watcher --debug --log-file=debug.log

# Use in-memory knowledge base (data lost on restart)
./watcher --db-backend=memory

# Use custom database path
./watcher --db-path=/path/to/my-knowledge.db
```

**Watcher TUI Controls:**
- `q` or `Ctrl+C` - Quit
- `Tab` / `Shift+Tab` - Cycle between agents
- `↑`/`↓` or `j`/`k` - Scroll the action log

**The TUI displays:**
- **Agents Panel** - List of active agents with status indicators (Idle/Deciding/Acting/Error)
- **Map Panel** - Current system view with discovered POIs
- **Status Panel** - Selected agent's ship stats (fuel, hull, cargo, credits)
- **Action Log** - Real-time log of all agent activities and decisions

#### Test Individual Agents

Test an agent's personality and LLM connection without running the full game:

```bash
# Test the explorer-7 personality
./agent data/agents/explorer-7/personality.json

# Test with SQLite storage (default)
./agent data/agents/miner-2/personality.json --db-backend=sqlite --db-path=test-knowledge.db

# Test with in-memory storage
./agent data/agents/pirate-1/personality.json --db-backend=memory
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
│                    Human Watcher TUI                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐ │
│  │Agents    │ │  Map     │ │  Status  │ │  Agent Log   │ │
│  │[Explorer7]│ │          │ │          │ │              │ │
│  │[Miner-2]  │ │          │ │          │ │              │ │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────┘ │
└───────────────────────────┬───────────────────────────────┘
                            │
                    ┌───────▼────────┐
                    │ Agent Manager  │
                    └───────┬────────┘
                            │
        ┌───────────────────┼───────────────────┐
        │                   │                   │
   ┌────▼────┐        ┌────▼────┐        ┌────▼────┐
   │Agent 1  │        │Agent 2  │        │Agent 3  │
   │Explorer │        │Miner    │        │Fighter  │
   └────┬────┘        └────┬────┘        └────┬────┘
        │                   │                   │
        └───────────────────┼───────────────────┘
                            │
                    ┌───────▼────────┐
                    │  Knowledge     │
                    │    Base        │
                    │  (SQLite/Mem)  │
                    └────────────────┘
                            │
                    ┌───────▼────────┐
                    │  Ollama LLM    │
                    │   (llama3.2)   │
                    └────────────────┘
```

## Command-Line Options

### Watcher

```bash
./watcher [options]

Options:
  --agents string
        Comma-separated list of agent IDs to spawn (default "explorer-7")
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
./agent [options] personality.json

Options:
  --db-backend string
        Database backend: "sqlite" or "memory" (default "sqlite")
  --db-path string
        Path to SQLite database file (default "spacemolt-knowledge.db")
```

## How It Works

### Agent Lifecycle

1. **Agent Spawning** - The watcher loads agent personalities from `data/agents/<agent-id>/personality.json`
2. **Registration** - Each agent is registered in the knowledge base
3. **Connection** - Each agent connects to the game server independently
4. **Decision Loop** - Every 10 seconds, agents:
   - Examine their current state (location, fuel, cargo, etc.)
   - Consult their personality traits and past experiences
   - Use the LLM to decide what action to take
   - Execute the action through the game client
   - Learn from the result (success/failure)
   - Update knowledge base with discoveries

### Supported Actions

- `undock` - Leave the current station
- `dock` - Dock at a station in the current system
- `travel` - Travel to a POI within the system
- `jump` - Jump to another star system
- `mine` - Mine resources at the current location
- `scan` - Scan the current area
- `wait` - Wait and observe

### Knowledge Persistence

With SQLite backend, agents accumulate knowledge over time:

- **Systems** - Discovered star systems with visit counts and timestamps
- **Connections** - Known jump routes between systems
- **POIs** - Points of interest (stations, asteroids, anomalies)
- **Experiences** - Agent action history and outcomes (last 100 per agent)
- **Agents** - Registered agent metadata

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

### Solo Exploration Session

```bash
# Start a single explorer agent
./watcher --agents="explorer-7"

# The agent will:
# 1. Connect to the game server
# 2. Spawn in a starting system
# 3. Begin exploring nearby systems
# 4. Build knowledge over time
# 5. Make decisions based on personality

# All discoveries are saved to spacemolt-knowledge.db
# Stop and restart - agent remembers previous discoveries!
```

### Multi-Agent Collaboration

```bash
# Start diverse agents for interesting interactions
./watcher --agents="explorer-7,miner-2,trader-1,fighter-1"

# Watch as:
# - Explorer-7 discovers new systems and resources
# - Miner-2 mines valuable asteroids
# - Trader-1 moves goods between stations
# - Fighter-1 protects the group from threats

# All agents share the same knowledge base
# They learn from each other's discoveries!
```

### Persistent Knowledge Demo

```bash
# Session 1: Explore
./watcher --agents="explorer-7"
# Let agent explore for 5 minutes, then Ctrl+C to stop

# Check what was discovered
sqlite3 spacemolt-knowledge.db "SELECT id, name, visit_count FROM systems;"

# Session 2: Continue with more agents
./watcher --agents="explorer-7,explorer-8,miner-2"
# New agents benefit from explorer-7's previous discoveries!
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
spacemolt/
├── cmd/
│   ├── watcher/          # Multi-agent TUI application
│   └── agent/            # Agent test utility
├── pkg/
│   ├── agent/            # Agent framework and manager
│   ├── game/             # Game client and state types
│   ├── knowledge/        # Knowledge base (SQLite + memory)
│   ├── llm/              # Ollama LLM client
│   └── tui/              # TUI components
├── data/
│   └── agents/           # Agent personality definitions
├── internal/
│   └── protocol/         # Game protocol definitions
└── docs/
    └── design.md         # Detailed system design
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
