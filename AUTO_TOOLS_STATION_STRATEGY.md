# Station Strategy Integration for Auto-Crafter and Auto-Trader

## Summary

Updated `auto-crafter` and `auto-trader` tools to use the same station strategy skills as `auto-miner`, allowing them to craft items, sell cargo, and deposit items to storage when docked at stations.

## Changes Made

### 1. Auto-Crafter (`cmd/auto-craftsman/main.go`)

**Before:**
- Simple monitoring mode only
- No crafting logic implemented
- Just logged status every 10 seconds

**After:**
- Full station strategy support with three modes:
  - `sell` - Sell all cargo immediately
  - `craft-sell` (default) - Craft items from resources, then sell all
  - `craft-deposit` - Craft items from resources, then deposit to storage
- Autonomous crafting loop that:
  - Travels to nearest station
  - Docks at station
  - Executes station action strategy (craft/sell/deposit)
  - Refuels and repairs as needed
  - Updates captain's log with progress

**Key Features:**
- Command-line argument parsing for strategy selection
- Crafting configuration initialization (MCP server integration)
- Proper error handling and logging
- Captain's log updates with detailed status

### 2. Auto-Trader (`cmd/auto-trader/main.go`)

**Before:**
- Simple monitoring mode only
- No trading logic implemented
- Just logged status every 10 seconds

**After:**
- Full station strategy support with three modes:
  - `sell` (default) - Sell all cargo immediately
  - `craft-sell` - Craft items from resources, then sell all
  - `craft-deposit` - Craft items from resources, then deposit to storage
- Autonomous trading loop that:
  - Travels to nearest station
  - Docks at station
  - Executes station action strategy (craft/sell/deposit)
  - Refuels and repairs as needed
  - Monitors market conditions (future: profitable trade routes)
  - Updates captain's log with progress

**Key Features:**
- Command-line argument parsing for strategy selection
- Crafting configuration initialization when using craft strategies
- Proper error handling and logging
- Captain's log updates with detailed status
- Foundation for future market analysis and trade route finding

## Station Strategy Functions

Both tools now use the same station strategy functions from `pkg/game/mining.go`:

- **`StationActionSellAll()`** - Sells all cargo without crafting
- **`StationActionCraftAndSell()`** - Crafts items from cargo, then sells everything
- **`StationActionCraftAndDeposit()`** - Crafts items from cargo, then deposits everything

These functions:
- Query craftable recipes using the MCP crafting server
- Execute batch crafting operations (up to 10 items per action)
- Handle skill requirements and blocking
- Sell or deposit all items using bulk operations
- Provide comprehensive logging

## Usage Examples

### Auto-Crafter
```bash
# Craft then sell (default)
auto-craftsman craftsman-1

# Sell everything
auto-craftsman craftsman-1 sell

# Craft then deposit
auto-craftsman craftsman-1 craft-deposit
```

### Auto-Trader
```bash
# Sell everything (default)
auto-trader trader-1

# Craft then sell
auto-trader trader-1 craft-sell

# Craft then deposit
auto-trader trader-1 craft-deposit
```

## Benefits

1. **Consistency** - All auto tools now use the same station strategy pattern
2. **Flexibility** - Each tool can choose appropriate cargo handling strategy
3. **Crafting Integration** - Both tools can leverage crafting skills via MCP server
4. **Future-Ready** - Foundation for advanced features like market analysis and trade routes
5. **Proper Error Handling** - Graceful degradation when crafting server unavailable
6. **Comprehensive Logging** - Clear status updates and captain's log entries

## Testing

All changes pass `golangci-lint` with 0 issues.

## Future Enhancements

### Auto-Crafter
- Material buying logic from station markets
- Recipe selection based on profit margins
- Skill progression tracking

### Auto-Trader
- Market analysis across multiple stations
- Profitable trade route calculation
- Buy low/sell high strategies
- Cargo capacity optimization

## Technical Details

### Crafting Configuration
When using `craft-sell` or `craft-deposit` strategies, both tools initialize:
```go
client.CraftingConfig = &game.CraftingConfig{
    CraftingServerPath: "", // Uses "crafting-server" from PATH
}
```

This enables:
- Recipe querying based on current cargo and skills
- Batch crafting operations
- Skill level validation
- Graceful fallback when MCP server unavailable

### Loop Structure
Both tools use a similar loop pattern:
1. 15-second ticker for main operations
2. 2-minute ticker for captain's log updates
3. Context cancellation for graceful shutdown
4. State tracking with credits earned per run
5. Automatic refuel and repair at stations

### Captain's Log Updates
Both tools update captain's log with:
- Run/completion count
- Credits earned this run
- Current credits
- Ship status (hull, fuel, cargo)
- Current goal and location
- Strategy being used
