# SpaceMolt Auto-Fighter

> Autonomous combat bot for SpaceMolt that hunts pirates, collects loot, and progressively upgrades ships and weapons.

## Overview

The auto-fighter is a fully autonomous combat agent that hunts pirates in asteroid belts, collects loot from defeated enemies, and continuously upgrades ships and weapons. It runs indefinitely until stopped, making it perfect for automated combat operations and credit accumulation through bounties and loot sales.

## Features

### Core Functionality
- **Autonomous Combat** - Automatically hunts and engages pirates in asteroid belts
- **Loot Collection** - Collects valuable equipment and materials from defeated enemies
- **Ship Progression** - Upgrades to better combat ships using FighterProgression tiers
- **Weapon Upgrades** - Installs and upgrades weapons for maximum combat power
- **Captain's Log** - Tracks mission progress and status across sessions
- **Continuous Operation** - Runs indefinitely, adapting to game state changes

### Intelligent Combat

The auto-fighter implements smart combat behavior:

- **Pirate Detection** - Scans for pirate and bandit ships in asteroid belts
- **Target Selection** - Prioritizes hostile ships (pirate, bandit class)
- **Combat Actions** - Engages enemies until defeated or disengages for safety
- **Loot Scanning** - Scans for wrecks after combat to collect loot
- **Safe Retreat** - Returns to station when hull or fuel is critically low

### Upgrade System

The auto-fighter uses a sophisticated upgrade system:

- **Knowledge Database** - Loads ship definitions from knowledge DB
- **Empire-Specific Progression** - Builds upgrade paths for your empire's ships
- **Credit Thresholds** - Progressive upgrades based on earned credits
- **Smart Installation** - Installs equipment and sells extras
- **Ship Switching** - Upgrades to better ships when thresholds are met

## Quick Start

### Basic Usage

```bash
# Run the fighter agent
go run ./cmd/auto-fighter fighter-1
```

### Building

```bash
# Build the binary
go build -o bin/auto-fighter ./cmd/auto-fighter

# Run the built binary
./bin/auto-fighter fighter-1
```

## How It Works

### Main Combat Loop

The auto-fighter uses a dedicated combat loop for autonomous operations:

```go
CombatLoop:
  For each run until stopped:
    1. Find combat POI (asteroid belt) and station in current system
    2. Undock from station
    3. Travel to asteroid belt
    4. Scan for pirates and hostiles
    5. Engage combat if targets found
    6. Scan for wrecks and collect loot
    7. Return to station
    8. Dock at station
    9. Sell all loot
    10. Refuel and repair
    11. Check for upgrades (every 3 runs or at credit thresholds)
    12. Update captain's log
    13. Repeat
```

### Upgrade System

The auto-fighter implements progressive upgrades:

**Tier 1 (500+ credits):**
- Buy and install weapons (weapon_laser_1/2/3)
- Maximize weapon slots based on ship capacity

**Tier 2 (5000+ credits):**
- Upgrade shields and defenses
- Install advanced combat modules

**Tier 3 (10000+ credits):**
- Upgrade to better combat ships
- Install superior weapons and equipment

### Ship Progression

The agent loads empire-specific ship progression from the knowledge database:

1. Queries knowledge DB for fighter-class ships
2. Filters ships by empire affiliation
3. Builds upgrade tiers based on cargo capacity and combat capability
4. Selects next upgrade when credit threshold is met

**Example progression:**
```
Dart (5 cargo) → Viper (15 cargo) → Mamba (30 cargo) → Cobra (50 cargo)
```

## Captain's Log

The auto-fighter maintains a captain's log that persists across sessions:

**Location:** `data/agents/{agent-id}/captains_log_latest.json`

**Contents:**
- Current mission goal
- Current location (system and POI)
- Combat runs completed
- Total credits earned
- Current credits
- Ship name and module count
- Hull, fuel, and cargo status
- Weapons installed
- Last update timestamp

## Configuration

### Command-Line Arguments

```
Usage: auto-fighter <agent-id>

Arguments:
  agent-id   Agent identifier (e.g., fighter-1, combat-1)
```

### Constants

The following constants can be modified in `cmd/auto-fighter/main.go`:

```go
const (
    RESERVE_CREDITS = 50.0   // Never spend below this amount
    TIER1_THRESHOLD = 500.0  // Weapon upgrade threshold
    TIER2_THRESHOLD = 5000.0 // Shield upgrade threshold
    TIER3_THRESHOLD = 10000.0 // Ship upgrade threshold
)
```

## Examples

### Example 1: Start Combat Bot

```bash
# Start a combat bot
go run ./cmd/auto-fighter fighter-1
```

**Output:**
```
[fighter-1] 📖 Captain's Log - Last Entry:
[fighter-1]    Mission: Autonomous combat operations - hunting pirates and upgrading equipment
[fighter-1]    Location: System: SOL, POI: asteroid_belt_01
[fighter-1]    Time: 2026-02-23 15:30
[fighter-1] 🏴‍☠️ Starting autonomous combat & upgrade bot...
[fighter-1] Agent: fighter-1 | Empire: Federation | Credits: 500.00 | Ship: Dart
[fighter-1] Starting autonomous combat + upgrade loop...
[fighter-1] Will automatically:
[fighter-1]   ⚔️  Hunt pirates and defeat them for loot
[fighter-1]   💰 Sell all loot for profit
[fighter-1]   🚀 Upgrade to better combat ships
[fighter-1]   🔫 Install better weapons to increase combat power
[fighter-1]     + Progression path (4 tiers):
[fighter-1]       Dart (0 credits)
[fighter-1]       Viper (5000 credits)
[fighter-1]       Mamba (15000 credits)
[fighter-1]       Cobra (50000 credits)
```

### Example 2: Combat Run

```
[fighter-1] ═══ Combat Run #1 ═══
[fighter-1] Credits: 500.00 | Fuel: 100/100 | Hull: 100/100 | Cargo: 0/5
[fighter-1] 📍 System: SOL | Combat: asteroid_belt_01 | Station: station_01
[fighter-1] 📤 Undocking from station...
[fighter-1] 🚀 Traveling to combat location asteroid_belt_01...
[fighter-1] ⚔️ Searching for pirates in asteroid belt...
[fighter-1] ⚔️ Hostile pirate detected: pirate_bandit_42
[fighter-1] ⚔️ Attacking pirate_bandit_42!
[fighter-1] ⚔️ Combat actions: 1 this run
[fighter-1] 💎 Scanning for wrecks...
[fighter-1] 💎 Loot collected: 1000 credits worth of equipment and materials
[fighter-1] 🚀 Returning to station station_01...
[fighter-1] 📥 Attempting to dock at station...
[fighter-1] 💰 Selling loot (1000 credits value)...
[fighter-1] ✅ Sold loot! Earned 1000.00 credits
[fighter-1] ═══ Run #1 Complete ═══
[fighter-1] Current Credits: 1500.00 (started with 500.00, earned 1000.00 total)
[fighter-1] Ship: Dart | Weapons: 1
```

### Example 3: Weapon Upgrades

```
[fighter-1] 💰 Checking for upgrades... (1450.0 credits available, 0/5 cargo space)
[fighter-1] Found 25 listings at market
[fighter-1] 🔧 Checking equipment in cargo...
[fighter-1] ⚔️ Weapon Status: 1 installed, 0 in cargo (goal: 2 installed)
[fighter-1] ⚔️ Buying 1 x weapon_laser_1 for 500.00 credits each
[fighter-1] ✅ Purchased 1 weapon(s)! Installing...
[fighter-1] ✅ Weapon #1 installed!
[fighter-1] ✅ 1 WEAPON(S) INSTALLED! Combat power increased!
```

### Example 4: Ship Upgrades

```
[fighter-1] 💰 Checking for upgrades... (9500.0 credits available, 0/15 cargo space)
[fighter-1] 🚀 Attempting ship upgrade to Viper (cost: 8000.0 credits)
[fighter-1] Buying ship Viper...
[fighter-1] ✅ Purchased Viper!
[fighter-1] Switching to ship Viper...
[fighter-1] ✅ Now flying Viper! Cargo: 0/15 (increased from 5)
```

## Architecture

### Combat Loop Implementation

The combat loop is implemented in `cmd/auto-fighter/main.go`:

```go
func fighterLoop(agentID, client, logger, ctx, agentState) error {
    for {
        // Find combat location
        combatPOI, stationPOI := findCombatAndStation()

        // Undock and travel
        undock()
        travelTo(combatPOI)

        // Hunt pirates
        combatActions := scanAndEngagePirates()

        // Collect loot
        lootValue := scanAndCollectWrecks()

        // Return and sell
        travelTo(stationPOI)
        dock()
        sellAllLoot()

        // Maintenance
        refuelIfNeeded()
        repairIfNeeded()

        // Upgrades
        if shouldCheckUpgrades() {
            attemptUpgrades()
        }

        // Logging
        updateCaptainsLog()
    }
}
```

### Upgrade System Implementation

Upgrades are implemented with multiple priority levels:

```go
func attemptUpgrades(client, logger, ctx, agentState) {
    // PRIORITY 1: Install existing equipment
    installEquipmentFromCargo()

    // PRIORITY 2: Ship upgrades (biggest boost)
    if canAffordShipUpgrade() {
        performShipUpgrade()
    }

    // PRIORITY 3: Weapons (essential for combat)
    if canAffordWeapons() {
        buyAndInstallWeapons()
    }
}
```

### Knowledge Database Integration

The auto-fighter uses the knowledge database for ship progression:

```go
func loadProgression(logger, ctx, creds, client) *agentState {
    // Open knowledge DB
    kb := knowledge.NewSQLiteKB("data/spacemolt-knowledge.db")

    // Query fighter-class ships for empire
    ships := kb.GetShipClassesByCategory(ctx, "fighter")

    // Filter by empire and build progression
    empireShips := FilterShipsByEmpire(ships, creds.Empire)
    progression := BuildProgression(empireShips, currentShip, "fighter")

    return &agentState{progression: progression}
}
```

## Performance

### Typical Performance

- **Combat Run Time:** 3-5 minutes (depends on combat duration)
- **Loot Value:** 500-2000 credits per pirate (varies)
- **Upgrade Frequency:** Every 3-10 runs (depends on loot value)
- **Credits per Hour:** Varies based on pirate density and loot quality

### Upgrade Timeline

| Runs | Credits | Ship | Weapons |
|------|---------|------|---------|
| 1-5 | 500-2500 | Dart | 1-2 weapons |
| 6-15 | 2500-7500 | Dart | 2 weapons max |
| 16-25 | 7500-15000 | Viper | 2-3 weapons |
| 26-50 | 15000-50000 | Mamba | 3-4 weapons |
| 50+ | 50000+ | Cobra | 4+ weapons |

## Troubleshooting

### Issue: "No combat location in current system"

**Cause:** Current system has no asteroid belts or fields.

**Solution:**
1. Move to a different system with asteroid belts
2. Travel back to home system manually
3. Check game map for systems with combat POIs

### Issue: "No station found in current system"

**Cause:** Current system has no stations.

**Solution:**
1. Move to a different system with stations
2. Travel back to home system manually
3. Check game map for systems with stations

### Issue: "Could not open knowledge DB"

**Cause:** Knowledge database not found or inaccessible.

**Solution:**
1. Run `make import-knowledge` to create the database
2. Check that `data/spacemolt-knowledge.db` exists
3. Verify file permissions on the database

### Issue: "No upgrade path available"

**Cause:** No ships defined for your empire's fighter class.

**Solution:**
1. Verify your empire is supported (Federation, Crimson, Dynasty, Syndicate)
2. Import ship data: `go run ./cmd/import-catalog-ships`
3. Check that ships have "fighter" category in knowledge DB

### Issue: Agent stops unexpectedly

**Cause:** Various reasons (connection loss, error, etc.)

**Solution:**
1. Check the captain's log for last status: `cat data/agents/{agent-id}/captains_log_latest.json`
2. Restart the agent - it will resume from where it left off
3. Check logs for specific error messages

## Related Tools

- [Auto-Trader](../auto-trader/) - Trading agent with cargo management

## License

Part of the SpaceMolt project.
