# SpaceMolt Auto-Trader

> Autonomous trading bot for SpaceMolt with multiple strategies, inter-empire trade routes, and intelligent market operations.

## Overview

The auto-trader is a fully autonomous agent that performs trading operations across multiple strategies. It can execute simple sell-all operations, crafting-integrated trading, or complex inter-empire trade routes with return legs. The agent runs indefinitely until stopped, making it perfect for passive credit accumulation through trade.

## Features

### Core Functionality
- **Automatic Station Operations** - Travels to stations and executes cargo management
- **Market-Aware Trading** - Intelligent pricing based on market order books
- **Captain's Log** - Tracks mission progress and status across sessions
- **Continuous Operation** - Runs indefinitely, adapting to game state changes

### Trading Strategies

Choose from multiple trading strategies:

1. **Sell (Default)** - Sell all cargo immediately at station
2. **Craft + Sell** - Craft items from resources, then sell everything
3. **Craft + Deposit** - Craft items from resources, then deposit to storage
4. **Inter-Empire Trade** - Execute profitable trade routes between empires

### Inter-Empire Trade Routes

The `trade` strategy implements sophisticated inter-empire trading:

- **Route Discovery** - Auto-selects most profitable routes for your empire
- **Round-Trip Trading** - Executes outbound and return legs for maximum profit
- **State Persistence** - Survives crashes and resumes from last phase
- **Smart Ship Selection** - Switches to best cargo ship for maximum capacity
- **Market Order Trading** - Sells into buy orders at optimal prices
- **Cargo Insurance** - Automatically insures high-value cargo

## Quick Start

### Basic Usage

```bash
# Run with default strategy (sell all)
go run ./cmd/auto-trader trader-1

# Explicitly specify strategy
go run ./cmd/auto-trader trader-1 sell

# Craft items then sell
go run ./cmd/auto-trader trader-1 craft-sell

# Craft items then deposit to storage
go run ./cmd/auto-trader trader-1 craft-deposit

# Inter-empire trade (auto-select best route)
go run ./cmd/auto-trader trader-1 trade

# Inter-empire trade (explicit route)
go run ./cmd/auto-trader trader-1 trade ore_silicon crimson
```

### Building

```bash
# Build the binary
go build -o bin/auto-trader ./cmd/auto-trader

# Run the built binary
./bin/auto-trader trader-1 sell
```

## Strategies

### Strategy: `sell` (Default)

**Best for:** Quick station operations, monitoring markets

**Behavior:**
1. Travel to nearest station
2. Dock at station
3. Sell all cargo immediately
4. Refuel and repair
5. Repeat

**Example:**
```bash
go run ./cmd/auto-trader trader-1 sell
```

### Strategy: `craft-sell`

**Best for:** Adding value to raw resources through crafting

**Behavior:**
1. Travel to station with cargo
2. Query crafting server for craftable recipes
3. Craft items from available cargo resources
4. Sell all crafted items and remaining raw materials
5. Refuel and repair
6. Repeat

**Example:**
```bash
go run ./cmd/auto-trader trader-1 craft-sell
```

**Requirements:** Crafting MCP server must be configured

### Strategy: `craft-deposit`

**Best for:** Stockpiling crafted items for later use

**Behavior:**
1. Travel to station with cargo
2. Query crafting server for craftable recipes
3. Craft items from available cargo resources
4. Deposit all items to station storage
5. Refuel and repair
6. Repeat

**Example:**
```bash
go run ./cmd/auto-trader trader-1 craft-deposit
```

**Requirements:** Crafting MCP server must be configured

### Strategy: `trade`

**Best for:** Maximum profit through inter-empire arbitrage

**Behavior:**
1. Analyze available trade routes for your empire
2. Buy ore at home empire (cheap)
3. Travel to destination empire
4. Sell ore at destination (expensive)
5. Buy return cargo at destination
6. Travel back to home empire
7. Sell return cargo at home
8. Repeat

**Phases:**
- `BUY_AT_HOME` - Purchase outbound cargo at home station
- `TRAVEL_TO_SELL` - Navigate to destination empire
- `SELL_AT_DEST` - Sell outbound cargo at destination
- `BUY_RETURN` - Purchase return cargo at destination
- `TRAVEL_HOME` - Navigate back to home system
- `SELL_AT_HOME` - Sell return cargo and complete trip

**Auto-Select Route:**
```bash
go run ./cmd/auto-trader trader-1 trade
```

**Explicit Route:**
```bash
go run ./cmd/auto-trader trader-1 trade ore_silicon crimson
```

## Crafting Integration

The auto-trader integrates with the SpaceMolt Crafting MCP Server for `craft-sell` and `craft-deposit` strategies. See the Quick Crafting Setup section below for setup instructions.

### Quick Crafting Setup

```bash
# Build the crafting server
go build -o bin/crafting-server ./cmd/crafting-server

# Add to PATH
export PATH="$PATH:$(pwd)/bin"

# Verify installation
crafting-server -help
```

The auto-trader will automatically initialize crafting when using `craft-sell` or `craft-deposit` strategies.

## Inter-Empire Trade Routes

### Route Discovery

The agent automatically discovers profitable trade routes based on your empire:

- **Federation** - Trades with Crimson, Dynasty, Syndicate
- **Crimson** - Trades with Federation, Dynasty, Syndicate
- **Dynasty** - Trades with Federation, Crimson, Syndicate
- **Syndicate** - Trades with Federation, Crimson, Dynasty

### Route Selection

When using `trade` strategy without specifying ore/target:

1. Agent queries all available routes for your empire
2. Routes are ranked by priority (profitability × volume)
3. Highest priority route is selected
4. Return leg is automatically calculated if available

**Example output:**
```
[trader-1] Available routes for federation:
[trader-1]   1. Silicon: Federation→Crimson (priority: 1, margin: 60 cr/unit)
[trader-1]   2. Iron: Federation→Dynasty (priority: 2, margin: 45 cr/unit)
[trader-1]   3. Copper: Federation→Syndicate (priority: 3, margin: 30 cr/unit)
[trader-1] Selected: Silicon: Federation→Crimson
```

### State Persistence

Trade state is automatically persisted to `data/agents/{agent-id}/trade_state.json`:

```json
{
  "phase": "BUY_AT_HOME",
  "outbound_route": {
    "name": "Silicon: Federation→Crimson",
    "ore_id": "ore_silicon",
    "buy_empire": "federation",
    "sell_empire": "crimson"
  },
  "trip_count": 5,
  "total_profit": 15000.0,
  "last_updated": "2026-02-23T15:30:00Z"
}
```

If the agent crashes, it will:
1. Load previous trade state
2. Validate route matches current configuration
3. Resume from last phase
4. Continue trading seamlessly

### Smart Ship Switching

Before buying outbound cargo at home station, the agent:
1. Lists all owned ships at current station
2. Queries knowledge database for cargo capacities
3. Switches to ship with highest cargo capacity
4. Executes trade with maximum profit potential

**Example output:**
```
[trader-1] Checking 3 owned ships for better cargo capacity (current: 50.0)...
[trader-1]   Found stored ship: Dart (dart) - cargo capacity: 5
[trader-1]   Found stored ship: Hauler (hauler) - cargo capacity: 100
[trader-1] Switching to Hauler (hauler) - cargo capacity: 50.0 → 100.0
[trader-1] Now flying Hauler - cargo: 0/100
```

### Market-Aware Selling

The agent implements intelligent market operations:

1. **Fetch Market Order Book** - Get all buy/sell orders for each item
2. **Sell Into Buy Orders** - Match orders at >= 80% of base value
3. **Create Sell Orders** - List remaining units at 80% of base value
4. **Price Floor Protection** - Never sell below 80% of item value

**Example output:**
```
[trader-1] Selling 100.0 x Silicon Ore (base value: 50 cr, min price: 40 cr)...
[trader-1]   Selling 50.0 units into buy order at 45 cr/unit (total: 2250 cr)
[trader-1]   Selling 50.0 units into buy order at 42 cr/unit (total: 2100 cr)
[trader-1] Sold! Sale revenue: 4350.0 cr | Total credits: 15435.0
```

### Cargo Insurance

High-value cargo is automatically insured before travel:

- **Threshold:** 10,000 credits
- **Coverage:** 100 ticks
- **Cost:** Variable based on cargo value

**Example output:**
```
[trader-1] Cargo value 12000 cr exceeds threshold, purchasing insurance (100 ticks)...
[trader-1] Insurance purchased for 100 ticks
```

## Captain's Log

The auto-trader maintains a captain's log that persists across sessions:

**Location:** `data/agents/{agent-id}/captains_log_latest.json`

**Contents:**
- Current mission goal
- Current location (system and POI)
- Trading runs completed
- Credits earned
- Ship status (hull, fuel, cargo)
- Trade phase and route (for `trade` strategy)
- Last update timestamp

## Architecture

### Trading Loop

The auto-trader uses a simple monitoring loop for non-trade strategies:

```go
TradingLoop:
  For each run until stopped:
    1. Find nearest station
    2. Travel to station if not docked
    3. Dock at station
    4. Execute station action (sell/craft-sell/craft-deposit)
    5. Refuel and repair
    6. Update captain's log
    7. Repeat
```

### Trade Loop

The `trade` strategy uses a sophisticated phase-based loop:

```go
TradeLoop:
  Restore persisted state (if available)
  For each tick:
    Execute current phase:
      BUY_AT_HOME → TRAVEL_TO_SELL → SELL_AT_DEST →
      BUY_RETURN → TRAVEL_HOME → SELL_AT_HOME → BUY_AT_HOME
    Save state to disk
    Update captain's log
```

### Station Actions

Station actions are implemented as strategies in `pkg/game/mining.go`:

- `StationActionSellAll()` - Sell all cargo
- `StationActionCraftAndSell()` - Craft then sell
- `StationActionCraftAndDeposit()` - Craft then deposit

## Configuration

### Command-Line Arguments

```
Usage: auto-trader <agent-id> [strategy] [ore] [target-empire]

Arguments:
  agent-id       Agent identifier (e.g., trader-1, merchant-1)
  strategy       Station action strategy (optional, default: sell)
  ore            Ore ID for trade strategy (e.g., ore_silicon)
  target-empire  Target empire for trade strategy (e.g., crimson)

Strategies:
  sell           Sell all cargo immediately (default)
  craft-sell     Craft items from resources, then sell all
  craft-deposit  Craft items from resources, then deposit to storage
  trade          Inter-empire trade routes (auto-select or explicit)
```

### Examples

```bash
# Sell everything (simplest)
go run ./cmd/auto-trader trader-1

# Craft then sell (add value)
go run ./cmd/auto-trader trader-1 craft-sell

# Auto-select best trade route
go run ./cmd/auto-trader trader-1 trade

# Explicit trade route
go run ./cmd/auto-trader trader-1 trade ore_silicon crimson
```

## Examples

### Example 1: Simple Sell Bot

```bash
# Start a basic trading bot that sells everything
go run ./cmd/auto-trader trader-1
```

**Output:**
```
[trader-1] Starting autonomous trading agent...
[trader-1] Agent: trader-1 | Empire: Federation | Credits: 1000.00 | Ship: Dart
[trader-1] Starting autonomous trading loop...
[trader-1] Station action strategy: sell
[trader-1] ═══ Trading Run #1 ═══
[trader-1] Credits: 1000.00 | Fuel: 100/100 | Hull: 100/100 | Cargo: 10/50
[trader-1] Docking at station...
[trader-1] Executing station action strategy: sell
[trader-1] 💰 Selling all cargo (10 items)...
[trader-1] ✅ Sold all cargo!
```

### Example 2: Inter-Empire Trade Bot

```bash
# Start a bot that trades between empires
go run ./cmd/auto-trader trader-1 trade
```

**Output:**
```
[trader-1] Available routes for federation:
[trader-1]   1. Silicon: Federation→Crimson (priority: 1, margin: 60 cr/unit)
[trader-1] Selected: Silicon: Federation→Crimson
[trader-1] ═══ Trade Route: Silicon: Federation→Crimson ═══
[trader-1]   Outbound: Buy ore_silicon at federation, sell at crimson (est. margin: 60 cr/unit)
[trader-1]   Home: SOL | Dest: CRIMSON_01 | Starting phase: BUY_AT_HOME
[trader-1] Buying 100.0 x Silicon Ore at 40.0 cr/unit (total: 4000.0)...
[trader-1] Navigating to CRIMSON_01...
[trader-1] Selling 100.0 x Silicon Ore (base value: 50 cr, min price: 40 cr)...
[trader-1] Sold! Sale revenue: 6000.0 cr | Total credits: 7000.0
[trader-1] ═══ Trip 1 complete! Profit: 2000.0 cr (5000.0 → 7000.0) | Lifetime profit: 2000.0 cr ═══
```

### Example 3: Explicit Trade Route

```bash
# Trade specific ore between specific empires
go run ./cmd/auto-trader trader-1 trade ore_iron dynasty
```

**Output:**
```
[trader-1] ═══ Trade Route: Iron: Federation→Dynasty ═══
[trader-1]   Outbound: Buy ore_iron at federation, sell at dynasty (est. margin: 45 cr/unit)
[trader-1]   Return:   Buy ore_copper at dynasty, sell at federation (est. margin: 35 cr/unit)
[trader-1]   Home: SOL | Dest: DYNASTY_01 | Starting phase: BUY_AT_HOME
```

## Troubleshooting

### Issue: "No trade routes available for empire"

**Cause:** No routes defined for your empire in the game configuration.

**Solution:**
1. Check that your empire is supported (Federation, Crimson, Dynasty, Syndicate)
2. Verify trade route data is loaded in `pkg/game/trade.go`
3. Use `sell`, `craft-sell`, or `craft-deposit` strategy instead

### Issue: "Crafting not configured, skipping to deposit"

**Cause:** The crafting MCP server is not available in PATH.

**Solution:**
1. Build the crafting server: `go build -o bin/crafting-server ./cmd/crafting-server`
2. Add to PATH: `export PATH="$PATH:$(pwd)/bin"`
3. Verify: `crafting-server -help`

**Note:** This is not an error - the agent will gracefully continue without crafting.

### Issue: "Failed to load trade state"

**Cause:** Corrupted or incompatible trade state file.

**Solution:**
1. Remove the trade state file: `rm data/agents/{agent-id}/trade_state.json`
2. Restart the agent - it will start fresh
3. Trade state will be recalculated on next run

### Issue: Agent stops unexpectedly

**Cause:** Various reasons (connection loss, error, etc.)

**Solution:**
1. Check the captain's log for last status: `cat data/agents/{agent-id}/captains_log_latest.json`
2. Restart the agent - it will resume from where it left off (for `trade` strategy)
3. Check logs for specific error messages

## Performance

### Typical Performance

- **Trading Cycle Time:** 1-3 minutes (depends on distance to station)
- **Trade Route Time:** 10-20 minutes (depends on route length)
- **Market Query Time:** 1-2 seconds
- **Credits per Hour:** Varies based on strategy and market conditions

### Strategy Comparison

| Strategy | Speed | Profit | Complexity | Best For |
|----------|-------|---------|------------|----------|
| `sell` | ⭐⭐⭐ | ⭐⭐ | Simple | Quick operations |
| `craft-sell` | ⭐⭐ | ⭐⭐⭐ | Moderate | Adding value |
| `craft-deposit` | ⭐⭐ | ⭐⭐⭐⭐ | Moderate | Stockpiling |
| `trade` | ⭐ | ⭐⭐⭐⭐⭐ | Complex | Maximum profit |

## Related Documentation

- [Crafting Server README](../crafting-server/README.md) - Full crafting server documentation
- [Game Client Documentation](../../pkg/game/) - Game client API reference
- [Trade Routes](../../pkg/game/trade.go) - Trade route implementation

## License

Part of the SpaceMolt project.
