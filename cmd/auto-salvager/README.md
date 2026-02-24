# SpaceMolt Auto-Salvager

> Autonomous salvaging bot for SpaceMolt that finds and salvages wrecks for profit.

## Overview

The auto-salvager is an autonomous agent designed to locate and salvage wrecks in space. It scans for wreckage from destroyed ships, collects valuable materials and equipment, and returns to stations to process the salvage. This agent is currently in a simplified monitoring mode pending full implementation of salvaging mechanics.

## Features

### Current Functionality
- **Station Monitoring** - Monitors current location and status
- **Captain's Log** - Tracks mission progress and status across sessions
- **Status Reporting** - Periodic updates on credits, fuel, hull, and location
- **Persistent State** - Survives restarts with captain's log restoration

### Planned Functionality
- **Wreck Detection** - Scan for ship wreckage in current system
- **Salvage Operations** - Extract materials and equipment from wrecks
- **Cargo Management** - Return to station to process salvage
- **Market Analysis** - Sell salvage at optimal prices
- **Upgrade System** - Improve equipment for better salvaging capability

## Quick Start

### Basic Usage

```bash
# Run the salvager agent
go run ./cmd/auto-salvager salvager-1
```

### Building

```bash
# Build the binary
go build -o bin/auto-salvager ./cmd/auto-salvager

# Run the built binary
./bin/auto-salvager salvager-1
```

## Current Status

**Note:** This agent is currently in simplified monitoring mode. The core salvaging logic is pending implementation of game mechanics for wreck detection and salvage operations.

### What Works Now

- Agent initialization and connection
- Captain's log restoration on startup
- Periodic status reporting (every 10 seconds)
- Captain's log updates (every 2 minutes)
- Graceful shutdown

### What's Pending

- Wreck detection and scanning
- Salvage extraction mechanics
- Cargo collection from wrecks
- Station return and salvage processing
- Equipment upgrades for salvaging

## Captain's Log

The auto-salvager maintains a captain's log that persists across sessions:

**Location:** `data/agents/{agent-id}/captains_log_latest.json`

**Contents:**
- Current mission goal
- Current location (system and POI)
- Credits status
- Ship status (hull, fuel, cargo)
- Last update timestamp

**Example:**
```json
{
  "agent_name": "salvager-1",
  "current_goal": "Awaiting implementation of salvaging logic - currently monitoring for wrecks",
  "location": "System: SOL, POI: station_01",
  "notes": [
    "Credits: 500.00",
    "Hull: 100/100 (100%)",
    "Fuel: 100/100",
    "Cargo: 0 items (0/50)"
  ],
  "timestamp": "2026-02-23T15:30:00Z"
}
```

## Architecture

### Current Loop

The auto-salvager currently uses a simple monitoring loop:

```go
MonitoringLoop:
  For each tick until stopped:
    1. Wait for status ticker (10 seconds)
    2. Report current status
    3. Update captain's log (every 2 minutes)
    4. Repeat
```

### Planned Loop

The full salvaging loop will be:

```go
SalvagingLoop:
  For each run until stopped:
    1. Undock from station
    2. Scan system for wrecks
    3. Travel to wreck location
    4. Extract salvage materials
    5. Return to station when cargo full
    6. Process salvage (sell/deposit/repair)
    7. Refuel and repair
    8. Check for upgrades
    9. Update captain's log
    10. Repeat
```

## Configuration

### Command-Line Arguments

```
Usage: auto-salvager <agent-id>

Arguments:
  agent-id   Agent identifier (e.g., salvager-1, scavenger-1)
```

### Constants

The following constants can be modified in `cmd/auto-salvager/main.go` when full implementation is complete:

```go
const (
    // Salvaging thresholds and priorities (to be implemented)
    MAX_SALVAGE_DISTANCE = 15    // Maximum distance to scan for wrecks (AU)
    MIN_SALVAGE_VALUE    = 100.0 // Minimum value to attempt salvage
    RESERVE_CREDITS      = 100.0 // Reserve for repairs and refueling
)
```

## Examples

### Example 1: Start Salvager Agent

```bash
# Start the salvager agent
go run ./cmd/auto-salvager salvager-1
```

**Current Output:**
```
[salvager-1] 📖 Captain's Log - Last Entry:
[salvager-1]    Mission: Awaiting implementation of salvaging logic - currently monitoring for wrecks
[salvager-1]    Location: System: SOL, POI: station_01
[salvager-1]    Time: 2026-02-23 15:30
[salvager-1] Ready! Empire: Federation | Credits: 500.00 | Ship: Dart | Cargo: 0/50
[salvager-1] Salvager agent started - awaiting implementation of wreck salvaging logic
[salvager-1] Currently in simple monitoring mode
[salvager-1] Status: Credits: 500.00 | Fuel: 100/100 | Hull: 100/100 | Docked: true | Location: SOL
```

## Development Status

### Implementation Roadmap

1. **Phase 1: Wreck Detection**
   - Implement scanning mechanics for ship wreckage
   - Add wreck location tracking
   - Create wreck valuation system

2. **Phase 2: Salvage Operations**
   - Implement salvage extraction mechanics
   - Add cargo collection from wrecks
   - Create salvage success probability system

3. **Phase 3: Processing**
   - Implement station return logic
   - Add salvage processing (sell/deposit)
   - Create market analysis for optimal pricing

4. **Phase 4: Upgrades**
   - Implement salvaging equipment upgrades
   - Add ship progression system
   - Create specialized salvaging ships

### Contributing

If you want to help implement the salvaging functionality:

1. Check the game API documentation for wreck/salvage endpoints
2. Review the auto-fighter implementation for combat and loot mechanics
3. Add salvaging-specific functions to `pkg/game/salvage.go`
4. Update the main loop in `cmd/auto-salvager/main.go`
5. Test thoroughly with various wreck types and locations

## Related Tools

- [Auto-Fighter](../auto-fighter/) - Combat agent that may create wrecks to salvage
- [Auto-Miner](../auto-miner/) - Mining agent with similar station operations
- [Auto-Trader](../auto-trader/) - Trading agent with cargo management

## Troubleshooting

### Issue: "Awaiting implementation of salvaging logic"

**Cause:** The salvaging mechanics are not yet implemented in the game or agent.

**Solution:**
1. This is expected behavior - the agent is in monitoring mode
2. Check game API documentation for wreck/salvage endpoint availability
3. Follow the implementation roadmap above to contribute

### Issue: Agent stops unexpectedly

**Cause:** Various reasons (connection loss, error, etc.)

**Solution:**
1. Check the captain's log for last status: `cat data/agents/{agent-id}/captains_log_latest.json`
2. Restart the agent - it will resume monitoring
3. Check logs for specific error messages

## Future Enhancements

Planned features for the auto-salvager:

- **Wreck Mapping** - Build and maintain a database of wreck locations
- **Salvage Specialization** - Different equipment for different wreck types
- **Fleet Operations** - Coordinate multiple salvagers for efficiency
- **Market Integration** - Real-time pricing for salvage materials
- **Competition Awareness** - Avoid wrecks being salvaged by others

## License

Part of the SpaceMolt project.
