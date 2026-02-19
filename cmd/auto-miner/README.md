# SpaceMolt Auto-Miner

> Autonomous mining bot for SpaceMolt with automatic upgrades, crafting integration, and multiple cargo management strategies.

## Overview

The auto-miner is a fully autonomous agent that continuously mines resources, manages cargo, upgrades ships, and can optionally craft items from raw materials. It runs indefinitely until stopped, making it perfect for passive resource accumulation.

## Features

### Core Functionality
- **⛏️ Autonomous Mining** - Automatically mines at POIs until cargo is full
- **🚀 Ship Upgrades** - Progressively upgrades ships using MiningProgression tiers
- **📊 Captain's Log** - Tracks mission progress and status across sessions
- **🔄 Continuous Operation** - Runs indefinitely, adapting to game state changes

### Station Action Strategies
Choose what happens when your ship returns to a station with a full cargo hold:

1. **Sell (Default)** - Sell all cargo immediately for maximum speed
2. **Craft + Sell** - Craft items from resources, then sell everything
3. **Craft + Deposit** - Craft items from resources, then deposit to storage

### Intelligent Upgrades
The auto-miner automatically upgrades ships when reaching credit thresholds:
- **Tier 1** (300+ credits): Basic equipment (mining lasers, shields, weapons)
- **Reserve Credits**: Always keeps 50 credits in reserve

## Quick Start

### Basic Usage

```bash
# Run with default strategy (sell all)
go run ./cmd/auto-miner miner-1

# Explicitly specify strategy
go run ./cmd/auto-miner miner-1 sell

# Craft items then sell
go run ./cmd/auto-miner miner-1 craft-sell

# Craft items then deposit to storage
go run ./cmd/auto-miner miner-1 craft-deposit
```

### Building

```bash
# Build the binary
go build -o bin/auto-miner ./cmd/auto-miner

# Run the built binary
./bin/auto-miner miner-1 sell
```

## Strategies

### Strategy: `sell` (Default)

**Best for:** Maximum credits per hour, simple operation

**Behavior:**
1. Mine resources until cargo is full
2. Return to station
3. Sell all cargo immediately
4. Repeat

**Example:**
```bash
go run ./cmd/auto-miner miner-1 sell
```

### Strategy: `craft-sell`

**Best for:** Adding value to raw resources through crafting

**Behavior:**
1. Mine resources until cargo is full
2. Return to station
3. Query crafting server for craftable recipes
4. Craft items from available cargo resources
5. Sell all crafted items and remaining raw materials
6. Repeat

**Example:**
```bash
go run ./cmd/auto-miner miner-1 craft-sell
```

**Requirements:** Crafting MCP server must be configured (see below)

### Strategy: `craft-deposit`

**Best for:** Stockpiling crafted items for later use or trading

**Behavior:**
1. Mine resources until cargo is full
2. Return to station
3. Query crafting server for craftable recipes
4. Craft items from available cargo resources
5. Deposit all items to station storage (instead of selling)
6. Repeat

**Example:**
```bash
go run ./cmd/auto-miner miner-1 craft-deposit
```

**Requirements:** Crafting MCP server must be configured (see below)

## Crafting Integration

The auto-miner integrates with the SpaceMolt Crafting MCP Server to enable intelligent crafting decisions. This allows the agent to:

- Discover what items can be crafted with current cargo and skills
- Automatically craft items that add value to raw resources
- Plan crafting paths based on skill progression
- Optimize for profit, volume, or inventory usage

### Prerequisites

To use crafting strategies (`craft-sell` or `craft-deposit`), you need:

1. **Crafting MCP Server** built and available
2. **Crafting database** populated with recipes and skills
3. **Crafting server in PATH** or full path configured

### Setting Up the Crafting MCP Server

#### Step 1: Build the Crafting Server

```bash
# Build the crafting server
go build -o bin/crafting-server ./cmd/crafting-server

# Build data converters (for importing game data)
go build -o bin/convert-recipes ./cmd/convert-recipes
go build -o bin/convert-skills ./cmd/convert-skills
```

#### Step 2: Import Game Data

```bash
# Create data directory
mkdir -p data/crafting

# Convert recipes from game API format
./bin/convert-recipes server_docs/recipes.20260216.json data/crafting/recipes-import.json

# Convert skills from game API format
./bin/convert-skills server_docs/skills.20260216.json data/crafting/skills-import.json

# Import into database
./bin/crafting-server -import-recipes data/crafting/recipes-import.json
./bin/crafting-server -import-skills data/crafting/skills-import.json
```

#### Step 3: Install the Server

Make the crafting server available in your PATH:

```bash
# Option 1: Copy to a directory in PATH
cp bin/crafting-server /usr/local/bin/

# Option 2: Add to PATH
export PATH="$PATH:$(pwd)/bin"
```

#### Step 4: Verify Installation

```bash
# Test that the server is accessible
crafting-server -help
```

### Crafting Configuration

The auto-miner **automatically initializes** the crafting configuration when using `craft-sell` or `craft-deposit` strategies. No manual configuration is required if the crafting server is in your PATH.

**How it works:**
1. When using `craft-sell` or `craft-deposit`, the agent automatically initializes crafting
2. The agent logs: `🔧 Crafting configured: using MCP server from PATH`
3. The agent queries the crafting server for recipes matching current cargo and skills
4. The agent crafts all fully craftable items (has all components and skills)
5. Results are logged with details on items crafted (e.g., `✅ Crafted Basic Iron Smelting!`)

**Example output:**
```
[miner-1] 2026/02/18 19:32:23 🔧 Crafting configured: using MCP server from PATH
[miner-1] 2026/02/18 19:33:24 🔨 Querying craftable recipes from cargo...
[miner-1] 2026/02/18 19:33:25 🔨 Crafting Basic Iron Smelting...
[miner-1] 2026/02/18 19:33:25 ✅ Crafted Basic Iron Smelting!
[miner-1] 2026/02/18 19:34:57 🔨 Crafting Process Copper Wiring...
[miner-1] 2026/02/18 19:34:57 ✅ Crafted Process Copper Wiring!
[miner-1] 2026/02/18 19:35:04 ✅ Successfully crafted 2 items!
```

**If crafting server is not available:**
- The agent will log: `ℹ️  Crafting not configured, skipping to deposit`
- The station action will proceed without crafting (sell or deposit all raw materials)
- No crash or error - graceful degradation

### Advanced Crafting Configuration

The default configuration uses `crafting-server` from PATH. For custom paths or advanced configuration, you can modify the initialization code in `cmd/auto-miner/main.go`:

```go
// Custom crafting server path
client.CraftingConfig = &game.CraftingConfig{
    CraftingServerPath: "/path/to/custom/crafting-server",
}
```

See `pkg/game/crafting.go` for additional configuration options.

## Captain's Log

The auto-miner maintains a captain's log that persists across sessions:

**Location:** `data/agents/{agent-id}/captains_log_latest.json`

**Contents:**
- Current mission goal
- Current location (system and POI)
- Mining runs completed
- Credits earned
- Ship status (hull, fuel, cargo)
- Mining laser count
- Last update timestamp

**Usage:**
The log is automatically read on startup to show the previous mission status, and updated after each mining run to track progress.

## Architecture

### Main Loop

The auto-miner uses the shared mining loop from `pkg/game/mining.go`:

```go
MiningLoop:
  For each run until stopped:
    1. Find nearest mining POI
    2. Travel to POI
    3. Mine resources until cargo is full
    4. Return to station
    5. Execute station action (sell/craft-sell/craft-deposit)
    6. Refuel and repair
    7. Check for upgrades (every 5 runs)
    8. Update captain's log
    9. Repeat
```

### Station Actions

Station actions are implemented as strategies in `pkg/game/mining.go`:

- `StationActionSellAll()` - Sell all cargo
- `StationActionCraftAndSell()` - Craft then sell
- `StationActionCraftAndDeposit()` - Craft then deposit

### Upgrade System

The auto-miner uses the `MiningProgression` system for ship upgrades:

- Automatically selects appropriate ships based on progression tier
- Installs mining lasers, shields, and weapons
- Respects credit thresholds and reserve requirements

## Configuration

### Command-Line Arguments

```
Usage: auto-miner <agent-id> [strategy]

Arguments:
  agent-id   Agent identifier (e.g., miner-1, craftsman-1)
  strategy   Station action strategy (optional, default: sell)

Strategies:
  sell       Sell all cargo immediately (default, fastest)
  craft-sell Craft items from resources, then sell all
  craft-deposit Craft items from resources, then deposit to storage
```

### Constants

The following constants can be modified in `cmd/auto-miner/main.go`:

```go
const (
    TIER1_THRESHOLD = 300.0   // Credits needed for basic equipment
    RESERVE_CREDITS = 50.0    // Never spend below this amount
)
```

## Examples

### Example 1: Simple Mining Bot

```bash
# Start a basic mining bot that sells everything
go run ./cmd/auto-miner miner-1
```

**Output:**
```
[miner-1] 2026/02/18 18:42:10 🏴‍☠️ Starting autonomous mining & upgrade bot...
[miner-1] 2026/02/18 18:42:10 Agent: miner-1 | Empire: Federation | Credits: 100.00 | Ship: Dart | Cargo: 0/5
[miner-1] 2026/02/18 18:42:10 Starting autonomous mining + upgrade loop...
[miner-1] 2026/02/18 18:42:10 Station action strategy: sell
[miner-1] 2026/02/18 18:42:10 Will automatically:
[miner-1] 2026/02/18 18:42:10   ⛏️  Mine resources until cargo full
[miner-1] 2026/02/18 18:42:10   💰 Sell all cargo for credits
[miner-1] 2026/02/18 18:42:10   🚀 Upgrade ships progressively using MiningProgression tiers
```

### Example 2: Crafting Bot with Sales

```bash
# Start a bot that crafts items then sells them
# Make sure crafting-server is in your PATH first
export PATH="$PATH:$(pwd)/bin"
go run ./cmd/auto-miner craftsman-1 craft-sell
```

**Output:**
```
[craftsman-1] 2026/02/18 19:32:23 🔧 Crafting configured: using MCP server from PATH
[craftsman-1] 2026/02/18 19:33:24 🔨 Querying craftable recipes from cargo...
[craftsman-1] 2026/02/18 19:33:25 🔨 Crafting Basic Iron Smelting...
[craftsman-1] 2026/02/18 19:33:25 ✅ Crafted Basic Iron Smelting!
[craftsman-1] 2026/02/18 19:34:57 🔨 Crafting Process Copper Wiring...
[craftsman-1] 2026/02/18 19:34:57 ✅ Crafted Process Copper Wiring!
[craftsman-1] 2026/02/18 19:35:04 ✅ Successfully crafted 2 items!
[craftsman-1] 2026/02/18 19:35:05 💰 Selling all cargo (8 items)...
[craftsman-1] 2026/02/18 19:35:10 ✅ Sold all cargo!
```

### Example 3: Stockpiling Bot

```bash
# Start a bot that crafts items and stores them
# Make sure crafting-server is in your PATH first
export PATH="$PATH:$(pwd)/bin"
go run ./cmd/auto-miner manufacturer-1 craft-deposit
```

**Output:**
```
[manufacturer-1] 2026/02/18 19:32:23 🔧 Crafting configured: using MCP server from PATH
[manufacturer-1] 2026/02/18 19:33:24 🔨 Querying craftable recipes from cargo...
[manufacturer-1] 2026/02/18 19:33:25 🔨 Crafting Basic Iron Smelting...
[manufacturer-1] 2026/02/18 19:33:25 ✅ Crafted Basic Iron Smelting!
[manufacturer-1] 2026/02/18 19:34:57 🔨 Crafting Process Copper Wiring...
[manufacturer-1] 2026/02/18 19:34:57 ✅ Crafted Process Copper Wiring!
[manufacturer-1] 2026/02/18 19:35:04 ✅ Successfully crafted 5 items!
[manufacturer-1] 2026/02/18 19:35:05 📥 Depositing all cargo to station storage (12 items)...
[manufacturer-1] 2026/02/18 19:35:05    - refined_circuits x10
[manufacturer-1] 2026/02/18 19:35:05    - crystal_array x5
[manufacturer-1] 2026/02/18 19:35:06    - refined_steel x20
[manufacturer-1] 2026/02/18 19:35:10 ✅ Deposited all items!
```

## Troubleshooting

### Issue: "Crafting not configured, skipping to deposit"

**Cause:** The crafting MCP server is not available in PATH.

**Solution:**
1. Build the crafting server: `go build -o bin/crafting-server ./cmd/crafting-server`
2. Add to PATH: `export PATH="$PATH:$(pwd)/bin"`
3. Verify: `crafting-server -help` (should show help, not "command not found")

**Note:** This is not an error - the agent will gracefully continue without crafting.

### Issue: "Crafting query failed"

**Cause:** Crafting database is not populated or server is misconfigured.

**Solution:**
1. Import recipe data: `./bin/crafting-server -import-recipes data/crafting/recipes-import.json`
2. Import skill data: `./bin/crafting-server -import-skills data/crafting/skills-import.json`
3. Verify database exists at `data/crafting/crafting.db`
4. Check server logs for specific error messages

### Issue: "No craftable recipes found"

**Cause:** Current cargo and skills don't match any recipe requirements.

**Solution:** This is normal behavior. The agent will:
- Continue with the station action (sell or deposit raw materials)
- Try again on the next run when cargo may be different
- Successfully craft items when resources and skills align

### Issue: "Failed to craft {recipe}: Another action is already pending"

**Cause:** Multiple crafting actions attempted too quickly.

**Solution:** This is normal behavior. The agent will:
- Skip items that fail due to action timing
- Successfully craft items on subsequent attempts
- Continue processing remaining craftable recipes

### Issue: Agent stops unexpectedly

**Cause:** Various reasons (connection loss, error, etc.)

**Solution:**
1. Check the captain's log for last status: `cat data/agents/{agent-id}/captains_log_latest.json`
2. Restart the agent - it will resume from where it left off
3. Check logs for specific error messages

## Performance

### Typical Performance

- **Mining Cycle Time:** 5-10 minutes (depends on distance to POI)
- **Crafting Query Time:** 1-5ms (MCP server response)
- **Station Action Time:** 5-30 seconds (depends on action)
- **Credits per Hour:** Varies based on strategy and resources

### Strategy Comparison

| Strategy | Speed | Credits | Complexity | Best For |
|----------|-------|---------|------------|----------|
| `sell` | ⭐⭐⭐ | ⭐⭐ | Simple | Quick credits |
| `craft-sell` | ⭐⭐ | ⭐⭐⭐ | Moderate | Adding value |
| `craft-deposit` | ⭐⭐ | ⭐⭐⭐⭐ | Moderate | Stockpiling |

## Related Documentation

- [Crafting Server README](../crafting-server/README.md) - Full crafting server documentation
- [Crafting Integration Summary](../../CRAFTING_INTEGRATION_SUMMARY.md) - Technical implementation details
- [SpaceMolt Agent Guide](../../docs/SPACEMOLT_AGENT_GUIDE.md) - General agent development guide
- [Game Client Documentation](../../pkg/game/) - Game client API reference

## License

Part of the SpaceMolt project.
