# Auto-Explorer

Autonomous galaxy exploration bot for Spacemolt explorer agents.

## Overview

The auto-explorer bot enables explorer agents (explorer-1 through explorer-10) to autonomously:
1. Mine resources and upgrade their ships with essential exploration equipment
2. Systematically explore the galaxy using depth-first search (DFS)
3. Document all systems and stations they encounter

## Features

### Phase 1: Mining & Upgrades
- Automatically mines resources to earn credits
- **Primary Goal: Reach 2,000 credits** for the Drillship upgrade
- Purchases and installs (in priority order):
  - **Drillship (mining_enhanced)** at 2,000 credits - 100 cargo capacity, 3 utility slots
  - **3x Mining Lasers** - Fill all utility slots for maximum mining speed
  - **Scanner** (~600 credits) - System scanning capability for exploration
- Manages fuel and cargo automatically
- Sells all mined resources at stations
- Completes Phase 1 when equipped with either:
  - Drillship with 3 mining lasers, OR
  - Scanner + mining laser (if Drillship unavailable)

### Phase 2: Galaxy Exploration
- **Depth-First Search (DFS)** algorithm for systematic exploration
- Visits every connected system in the galaxy
- Collects and saves detailed system data
- Records market listings from all stations
- Automatic fuel management and refueling
- Intelligent backtracking when all neighbors are explored

## Usage

```bash
# Build the explorer
go build ./cmd/auto-explorer

# Run a specific explorer (1-10)
./auto-explorer 1    # Runs explorer-1
./auto-explorer 2    # Runs explorer-2
# ... etc
```

## Data Collection

The explorer automatically saves two types of data:

### System Data
Location: `data/server/systems/{system_name}.json`

Contains:
- All POIs (points of interest) in the system
- System metadata (name, empire, connections, etc.)
- Position data
- Security status

Format matches the existing `sol.json` structure.

### Market Listings
Location: `data/server/listings/{system}_{station}_{timestamp}.json`

Contains:
- Current market buy/sell listings
- Station and system metadata
- Timestamp for historical tracking

Timestamp format: `YYYYMMDDHHMM` (e.g., `202602071904`)

## Algorithm Details

### DFS Exploration Strategy

1. **Mark current system as visited**
2. **Collect system data** - GetSystem + Scan + save JSON
3. **Check for stations** - Dock, get listings, refuel if needed
4. **Find unvisited neighbor systems**
5. **If unvisited neighbors exist**:
   - Push current system to stack
   - Jump to first unvisited neighbor
6. **If no unvisited neighbors**:
   - Pop from stack and backtrack
7. **If stack empty**:
   - Exploration complete! Reset and continue

### Fuel Management

- Monitors fuel continuously
- Refuels when fuel drops below 30% of max
- Before each jump, verifies fuel > 10 + safety margin
- Tracks last known fuel station for emergency backtracking
- If low on fuel, backtracks to nearest known station

### Station Handling

- Detects stations by checking POI type
- Visits only the first station per system (efficiency)
- Process:
  1. Travel to station POI
  2. Dock
  3. Get and save market listings
  4. Refuel if fuel < 80%
  5. Undock and continue

## Requirements

Each explorer agent must have:
- `data/agents/explorer-{N}/credentials.json` - Login credentials
- `data/agents/explorer-{N}/personality.json` - Agent personality

## Output Directories

The bot creates the following directories if they don't exist:
- `data/server/systems/` - System data files
- `data/server/listings/` - Market listing files

## Timing

The bot uses the following delays (based on game server timing):
- After dock/undock: 12-15 seconds
- After travel: 20 seconds
- After jump: 25 seconds (longer than travel)
- After buy/sell: 2-5 seconds
- After GetSystem/GetListings: 2-3 seconds
- After scan: 3 seconds

## Logs

Each explorer provides detailed logs showing:
- Current phase (Mining or Exploration)
- Credits, fuel, cargo status
- System discoveries
- Data collection progress
- Navigation decisions (DFS stack depth, backtracking)
- Error messages and warnings

Example log output:
```
[EXPLORER-1] ═══════════════════════════════════════════════
[EXPLORER-1]         PHASE 1: Mining & Upgrades
[EXPLORER-1] ═══════════════════════════════════════════════
[EXPLORER-1] ⛏️  Buying mining laser: mining_laser_1 for 150.00 credits
[EXPLORER-1] ✅ MINING LASER INSTALLED!
[EXPLORER-1] 📡 Buying scanner: scanner for 400.00 credits
[EXPLORER-1] ✅ SCANNER INSTALLED!
[EXPLORER-1] ═══════════════════════════════════════════════
[EXPLORER-1]       PHASE 2: Galaxy Exploration (DFS)
[EXPLORER-1] ═══════════════════════════════════════════════
[EXPLORER-1] 📍 Exploring new system: Alpha Centauri
[EXPLORER-1] 💾 Saved system data: data/server/systems/alpha_centauri.json
[EXPLORER-1] 🏪 Found station: Alpha Station
[EXPLORER-1] 💾 Saved market listings: data/server/listings/alpha_centauri_alpha_station_202602071915.json
[EXPLORER-1] → Moving to unvisited system: Sirius (Stack depth: 1)
```

## Safety Features

- Never spends below reserve credits (50 credits)
- Checks fuel before every jump
- Automatic refueling when fuel is low
- Error handling for failed operations
- Infinite exploration loop (resets after completing galaxy)

## Architecture

The implementation follows the `auto-miner` pattern with two distinct phases:

1. **Mining Phase** - Accumulate credits and purchase upgrades
2. **Exploration Phase** - DFS galaxy mapping

All game API interactions use the `pkg/game/client.go` client library.
