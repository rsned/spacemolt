# Auto-Explorer

Autonomous galaxy exploration bot for Spacemolt explorer agents.

## Overview

The auto-explorer bot enables agents to autonomously explore the galaxy using depth-first search (DFS), documenting all systems, stations, and POIs they encounter.

**Note:** This tool focuses solely on exploration. For mining and ship upgrades, use the `auto-miner` tool.

## Features

### Galaxy Exploration
- **Depth-First Search (DFS)** algorithm for systematic exploration
- Visits every connected system in the galaxy
- Collects and saves detailed system data to knowledge base
- Records market and ship listings from all stations (once per day)
- Automatic fuel management and refueling
- Intelligent backtracking when all neighbors are explored
- Direct jumping between connected systems (no jump gates required)

### Knowledge Base Integration
All discovered data is saved to the knowledge base:
- **Systems**: Name, ID, position, connections, faction, police level
- **POIs**: All points of interest with positions, types, and resources
- **Market Data**: Buy/sell listings captured once per station per day
- **Ship Listings**: Available ships at stations captured once per day

### Smart Navigation
- Uses `find_route` API for pathfinding to home base
- Direct jumps between connected systems
- Automatic state verification after reconnection
- Handles disconnections gracefully

## Usage

```bash
# Build the explorer
go build ./cmd/auto-explorer

# Run a specific agent
./auto-explorer explorer-1
./auto-explorer explorer-2
# ... etc
```

### Flags

```
-registry-url string
    Status registry URL (e.g., http://localhost:8081)
-db-backend string
    Knowledge base backend: sqlite or memory (default "sqlite")
-db-path string
    Path to SQLite database (default "data/spacemolt-knowledge.db")
```

## Data Collection

The explorer automatically saves data to the knowledge base (SQLite or in-memory):

### System Data
- All POIs in the system
- System metadata (name, empire, connections, position)
- Security status
- Discovered timestamp and agent

### Market Listings
- Current market buy/sell listings
- Station and system metadata
- Captured once per day per station

### Ship Listings
- Available ships at stations
- Ship class, price, and specifications
- Captured once per day per station

## Algorithm Details

### DFS Exploration Strategy

1. **Mark current system as visited**
2. **Collect system data** - GetSystem + Scan + Survey
3. **Explore all POIs** - Visit each POI, collect details
4. **Handle stations** - Dock, collect market/ship data, refuel
5. **Find unvisited neighbor systems**
6. **If unvisited neighbors exist**:
   - Push current system to stack
   - Jump to first unvisited neighbor
7. **If no unvisited neighbors**:
   - Pop from stack and backtrack
8. **If stack empty**:
   - Exploration complete! Return home and reset

### Navigation

- **Direct Jumping**: No need to travel to jump gates. Simply jump directly to any connected system.
- **State Verification**: After reconnection, always refresh system data to verify actual location.
- **Route Finding**: Uses `find_route` API for multi-hop navigation (e.g., returning to home base).

### Fuel Management

- Monitors fuel continuously
- Refuels when fuel drops below 30% of max
- Tracks last known fuel station
- Refuels automatically when docked at stations

### Station Handling

- Detects stations by checking POI type
- Visits only the first station per system (efficiency)
- Process:
  1. Travel to station POI
  2. Dock
  3. Get and save market listings (if not captured today)
  4. Get and save ship listings (if not captured today)
  5. Refuel if fuel < 30%
  6. Undock and continue

## Requirements

Each agent must have:
- `data/agents/{agent-name}/credentials.json` - Login credentials
- `data/agents/{agent-name}/personality.json` - Agent personality

## Knowledge Base

The explorer requires a knowledge base backend:

### SQLite Backend (Default)
Persists all data to a SQLite database:
```bash
./auto-explorer explorer-1 -db-backend sqlite -db-path data/spacemolt-knowledge.db
```

### In-Memory Backend
Stores data only during runtime (useful for testing):
```bash
./auto-explorer explorer-1 -db-backend memory
```

## Timing

The bot uses the following delays (based on game server timing):
- After dock/undock: 12-15 seconds
- After travel: 20 seconds
- After jump: 25 seconds
- After GetListings/GetShips: 2-3 seconds
- After scan/survey: 3 seconds

## Logs

The explorer provides detailed logs showing:
- Current system and location
- Systems explored count
- POI exploration progress
- Data collection status
- Navigation decisions (DFS stack depth, backtracking)
- Error messages and warnings

Example log output:
```
[EXPLORER-1] ═══════════════════════════════════════════════
[EXPLORER-1]      GALAXY EXPLORATION (DFS)
[EXPLORER-1] ═══════════════════════════════════════════════
[EXPLORER-1] 📍 Exploring new system: Alpha Centauri
[EXPLORER-1] 💾 Saved system to knowledge base: Alpha Centauri
[EXPLORER-1] 🔍 Exploring 5 POIs in system Alpha Centauri
[EXPLORER-1] 📍 Visiting POI: Alpha Station (poi_123) - Type: station
[EXPLORER-1] 🏪 Station detected! Docking to collect market and ship data...
[EXPLORER-1] 💾 Saved market snapshot to knowledge base
[EXPLORER-1] 💾 Saved ship listings to knowledge base
[EXPLORER-1] → Moving to unvisited system: Sirius (Stack depth: 1)
```

## Status Registry

When configured with a status registry URL, the explorer registers itself and sends heartbeats with current status:

```bash
./auto-explorer explorer-1 -registry-url http://localhost:8081
```

Registry information includes:
- Tool ID: `auto-explorer-{agent-name}`
- Tool Type: `auto-explorer`
- Agent ID and name
- Current status (exploring, connecting, etc.)
- Current location and credits

## Safety Features

- Verifies location after reconnection
- Checks fuel before every jump
- Automatic refueling when fuel is low
- Error handling for failed operations
- Automatic repair when ship is damaged
- Infinite exploration loop (returns home after completing galaxy)

## Architecture

The implementation follows a clean architecture:

1. **Knowledge Base Integration** - All data saved via knowledge.Base interface
2. **Direct Navigation** - No jump gates, direct system-to-system jumps
3. **State Management** - Proper location verification after reconnection
4. **DFS Algorithm** - Systematic exploration with backtracking

All game API interactions use the `pkg/game/client.go` client library.
