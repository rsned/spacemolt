# Spacemolt Multi-Agent System

An autonomous multi-agent system for the [SpaceMolt](https://spacemolt.com) game. AI agents explore, mine, trade, and interact with the game universe using LLM-powered decision making.

## Overview

The Spacemolt Multi-Agent System transforms the single-client game into a platform where multiple autonomous AI agents:

- **Explore** the universe independently using LLM-driven decision making
- **Learn** from their experiences and build knowledge over time
- **Collaborate** by sharing information with other agents
- **Play autonomously** while a human watcher observes via a TUI interface

Each agent has a unique personality that drives their behavior, and they make decisions about what to do next based on their current situation, past experiences, and knowledge of the universe.

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
                    │   (Ollama)      │
                    └────────────────┘
```

## Requirements

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

## Building

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
- `spacemolt-agent` - A test utility for verifying agent setup

## Running

### Watcher (Multi-Agent TUI)

The watcher spawns autonomous AI agents and displays their activity in a TUI:

```bash
# Run with default explorer-7 agent
./watcher

# Run with multiple agents
./watcher --agents="explorer-7,miner-2"

# Run with debug logging
./watcher --debug --log-file=debug.log

# Run with specific agent list
./watcher --agents="explorer-7,fighter-1,trader-3"
```

**Key Controls:**
- `q` or `Ctrl+C` - Quit
- `Tab` / `Shift+Tab` - Cycle between agents
- `↑`/`↓` or `j`/`k` - Scroll the action log

The TUI displays:
- **Agents Panel** - List of active agents with status indicators
- **Map Panel** - Current system view with POIs
- **Status Panel** - Selected agent's ship and player stats
- **Action Log** - Real-time log of all agent activities

### Agent Test Utility

Test an agent's personality and LLM connection without running the full game:

```bash
# Test the explorer-7 personality
./spacemolt-agent data/agents/explorer-7/personality.json
```

## Configuration

### Agent Personalities

Agents are defined by personality files in `data/agents/<agent-id>/personality.json`:

```json
{
  "name": "Explorer-7",
  "id": "explorer-7",
  "role": "Explorer",
  "faction": "Explorers Guild",
  "traits": {
    "curiosity": 0.95,
    "risk_tolerance": 0.65,
    "altruism": 0.40,
    "patience": 0.55,
    "aggression": 0.20
  },
  "motivations": {
    "primary": "explore_unknown",
    "secondary": "document_discoveries",
    "tertiary": "share_knowledge"
  },
  "skills": {
    "navigation": "intermediate",
    "scanning": "intermediate",
    "combat": "basic",
    "mining": "basic"
  },
  "biography": "Born in the asteroid belt colonies..."
}
```

### Creating New Agents

1. Create a new directory under `data/agents/<agent-id>/`
2. Add a `personality.json` file with the agent's configuration
3. Run the watcher with `--agents=<agent-id>`

## How It Works

1. **Agent Spawning** - The watcher loads agent personalities and spawns them
2. **Connection** - Each agent connects to the game server independently
3. **Decision Loop** - Every 10 seconds, agents:
   - Examine their current state (location, fuel, cargo, etc.)
   - Consult their personality and past experiences
   - Use the LLM to decide what action to take
   - Execute the action through the game client
   - Learn from the result

### Supported Actions

- `undock` - Leave the current station
- `dock` - Dock at a station in the current system
- `travel` - Travel to a POI within the system
- `jump` - Jump to another star system
- `mine` - Mine resources at the current location
- `scan` - Scan the current area
- `wait` - Wait and observe

## Development

### Running Linters

```bash
golangci-lint run ./...
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
│   ├── knowledge/        # In-memory knowledge base
│   ├── llm/              # Ollama LLM client
│   └── tui/              # TUI components
├── data/
│   └── agents/           # Agent personality definitions
├── internal/
│   └── protocol/         # Game protocol definitions
└── docs/
    └── design.md         # Detailed system design
```

## License

MIT License - See LICENSE file for details
