# SpaceMolt Auto-Pirate

> Autonomous pirate agent for SpaceMolt that attacks ships and collects plunder.

## Overview

The auto-pirate is an autonomous agent designed to hunt and attack other ships for loot and plunder. It scans for targets, engages in combat, and collects valuable cargo from defeated victims. This agent is currently in a simplified monitoring mode pending full implementation of piracy mechanics.

## Features

### Current Functionality
- **Target Monitoring** - Monitors current location and status
- **Captain's Log** - Tracks mission progress and status across sessions
- **Status Reporting** - Periodic updates on credits, fuel, hull, and location
- **Combat Tracking** - Tracks combat encounters
- **Persistent State** - Survives restarts with captain's log restoration

### Planned Functionality
- **Target Detection** - Scan for vulnerable ships (traders, miners, etc.)
- **Combat Operations** - Engage and defeat targets for plunder
- **Loot Collection** - Collect valuable cargo from defeated ships
- **Evasion Tactics** - Flee from superior forces or law enforcement
- **Rebase Operations** - Return to hidden bases for repairs and fencing

## Quick Start

### Basic Usage

```bash
# Run the pirate agent
go run ./cmd/auto-pirate pirate-1
```

### Building

```bash
# Build the binary
go build -o bin/auto-pirate ./cmd/auto-pirate

# Run the built binary
./bin/auto-pirate pirate-1
```

## Current Status

**Note:** This agent is currently in simplified monitoring mode. The core piracy logic is pending implementation of game mechanics for attacking other players and collecting plunder.

### What Works Now

- Agent initialization and connection
- Captain's log restoration on startup
- Periodic status reporting (every 10 seconds)
- Captain's log updates (every 2 minutes)
- Combat encounter tracking
- Graceful shutdown

### What's Pending

- Target detection and scanning
- Attack mechanics and combat resolution
- Plunder collection from defeated ships
- Evasion and fleeing tactics
- Notoriety and bounty system integration
- Hidden base operations

## Configuration

### Command-Line Arguments

```
Usage: auto-pirate <agent-id>

Arguments:
  agent-id   Agent identifier (e.g., pirate-1, bandit-1)
```

### Constants

The following constants can be modified in `cmd/auto-pirate/main.go` when full implementation is complete:

```go
const (
    TARGET_SHIP_CLASS   = "fighter" // Preferred target ship class
    MAX_COMBAT_DISTANCE = 15        // Maximum distance to engage targets (AU)
    MIN_COMBAT_TICKS    = 20        // Minimum ticks in combat before disengaging
    SAFE_HULL_PERCENT   = 0.3       // Disengage if hull drops below 30%
    RESERVE_CREDITS     = 100.0     // Reserve for repairs and refueling
)
```

## Captain's Log

The auto-pirate maintains a captain's log that persists across sessions:

**Location:** `data/agents/{agent-id}/captains_log_latest.json`

**Contents:**
- Current mission goal
- Current location (system and POI)
- Combat encounters
- Credits status
- Ship status (hull, fuel, cargo)
- Weapon count
- Last update timestamp

**Example:**
```json
{
  "agent_name": "pirate-1",
  "current_goal": "Awaiting implementation of pirating logic - currently monitoring for targets",
  "location": "System: SOL, POI: asteroid_belt_01",
  "notes": [
    "Combat encounters: 0",
    "Credits: 500.00",
    "Hull: 100/100 (100%)",
    "Fuel: 100/100",
    "Cargo: 0 items (0/50)",
    "Weapons: 2"
  ],
  "timestamp": "2026-02-23T15:30:00Z"
}
```

## Architecture

### Current Loop

The auto-pirate currently uses a simple monitoring loop:

```go
MonitoringLoop:
  For each tick until stopped:
    1. Wait for status ticker (10 seconds)
    2. Report current status
    3. Update captain's log (every 2 minutes)
    4. Repeat
```

### Planned Loop

The full piracy loop will be:

```go
PiracyLoop:
  For each run until stopped:
    1. Undock from hidden base
    2. Travel to busy shipping lane or mining area
    3. Scan for vulnerable targets
    4. Evaluate target strength vs. own strength
    5. Engage target if favorable odds
    6. Combat until target defeated or hull critical
    7. Collect plunder from defeated target
    8. Evade if superior forces arrive
    9. Return to hidden base
    10. Fence plunder and repair
    11. Check for upgrades
    12. Update captain's log
    13. Repeat
```

## Examples

### Example 1: Start Pirate Agent

```bash
# Start the pirate agent
go run ./cmd/auto-pirate pirate-1
```

**Current Output:**
```
[pirate-1] 📖 Captain's Log - Last Entry:
[pirate-1]    Mission: Awaiting implementation of pirating logic - currently monitoring for targets
[pirate-1]    Location: System: SOL, POI: asteroid_belt_01
[pirate-1]    Time: 2026-02-23 15:30
[pirate-1] Ready! Empire: Crimson | Credits: 500.00 | Ship: Dart | Cargo: 0/50
[pirate-1] Pirate agent started - awaiting implementation of combat logic
[pirate-1] Currently in simple monitoring mode
[pirate-1] Status: Credits: 500.00 | Fuel: 100/100 | Hull: 100/100 | Docked: false | Location: SOL
```

## Development Status

### Implementation Roadmap

1. **Phase 1: Target Detection**
   - Implement scanning for nearby ships
   - Add target evaluation (strength, cargo value)
   - Create target priority system

2. **Phase 2: Combat Mechanics**
   - Implement attack initiation
   - Add combat resolution logic
   - Create disengagement and evasion tactics

3. **Phase 3: Plunder Collection**
   - Implement loot collection from defeated ships
   - Add cargo transfer mechanics
   - Create plunder valuation system

4. **Phase 4: Base Operations**
   - Implement hidden base docking
   - Add fencing operations (sell plunder)
   - Create notoriety system integration

5. **Phase 5: Advanced Tactics**
   - Implement ambush tactics
   - Add gang coordination (multiple pirates)
   - Create bounty evasion mechanics

### Contributing

If you want to help implement the piracy functionality:

1. Check the game API documentation for combat/attack endpoints
2. Review the auto-fighter implementation for combat mechanics
3. Add piracy-specific functions to `pkg/game/piracy.go`
4. Update the main loop in `cmd/auto-pirate/main.go`
5. Test thoroughly with various target types and situations

## Pirate Tactics

### Target Selection

When fully implemented, the auto-pirate will evaluate targets based on:

- **Ship Class** - Prefer unarmed or lightly armed ships (miners, traders)
- **Cargo Value** - High-value cargo worth the risk
- **Combat Strength** - Weaker targets for easy victories
- **Distance** - Targets within engagement range
- **Risk Assessment** - Avoid targets with strong escorts

### Combat Behavior

Planned combat tactics:

- **Ambush** - Strike from asteroid belts or jump gates
- **Hit and Run** - Quick attacks before help arrives
- **Focused Fire** - Concentrate attacks on critical systems
- **Tactical Retreat** - Disengage if hull drops below 30%
- **Evasion** - Flee from superior forces or law enforcement

### Evasion Tactics

Planned evasion mechanics:

- **Emergency Jump** - Use jump gates to escape
- **Asteroid Field** - Hide in dense asteroid fields
- **Signature Masking** - Reduce detection signature
- **False Trails** - Create decoy signals

## Related Tools

- [Auto-Fighter](../auto-fighter/) - Combat agent for legitimate hunting
- [Auto-Trader](../auto-trader/) - Trading agent (potential targets)
- [Auto-Miner](../auto-miner/) - Mining agent (potential targets)
- [Auto-Explorer](../auto-explorer/) - Exploration agent (potential targets)

## Troubleshooting

### Issue: "Awaiting implementation of pirating logic"

**Cause:** The piracy mechanics are not yet implemented in the game or agent.

**Solution:**
1. This is expected behavior - the agent is in monitoring mode
2. Check game API documentation for combat/attack endpoint availability
3. Follow the implementation roadmap above to contribute

### Issue: Agent stops unexpectedly

**Cause:** Various reasons (connection loss, error, etc.)

**Solution:**
1. Check the captain's log for last status: `cat data/agents/{agent-id}/captains_log_latest.json`
2. Restart the agent - it will resume monitoring
3. Check logs for specific error messages

## Future Enhancements

Planned features for the auto-pirate:

- **Notoriety System** - Build reputation as infamous pirate
- **Bounty Hunting** - Evade or defeat bounty hunters
- **Pirate Gangs** - Coordinate with other pirate agents
- **Ransom Operations** - Capture ships for ransom
- **Smuggling Routes** - Specialized trade in illegal goods
- **Hidden Bases** - Operate from secret locations

## Legal Notice

This is a game agent for SpaceMolt. Piracy in this context refers to in-game mechanics only and does not condone or encourage any illegal activities in the real world.

## License

Part of the SpaceMolt project.
