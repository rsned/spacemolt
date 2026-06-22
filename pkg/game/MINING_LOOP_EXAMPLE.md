# Shared Mining Loop

The `MiningLoop` function in `pkg/game/mining.go` provides a reusable mining cycle for all autonomous agents.

## Overview

The mining loop handles the complete workflow:
1. Find mining POI and station in current system
2. Undock if docked
3. Travel to mining location (asteroid belt/field)
4. Mine until cargo full or fuel low
5. Travel back to station
6. Dock at station
7. Sell all cargo (supports bulk selling)
8. Refuel if needed
9. Repair if needed
10. Check for upgrades (configurable)

## Basic Usage

### Continuous Mining (using MiningStrategy)

```go
config := &game.MiningLoopConfig{
    AgentID:              agentID,
    UpgradeCheckInterval: 5,           // Check upgrades every 5 runs
    Tier1Threshold:       300.0,       // Minimum credits for upgrades
    ReserveCredits:       50.0,        // Never spend below this
    UseBulkSell:          true,        // Use bulk sell API
    OnUpgradeCheck: func() bool {
        // Your upgrade logic here
        attemptUpgrades(client, logger, ctx)
        return false
    },
    OnRunComplete: func(runNum int, creditsEarned float64, totalCredits float64) {
        // Update captain's log, track stats, etc.
        updateCaptainsLog(agentID, client, runNum, creditsEarned)
    },
}

result, err := game.MiningLoop(client, logger, ctx, config)
if err != nil {
    log.Fatalf("Mining loop error: %v", err)
}
```

### Mining Until Goal Reached (like auto-explorer Phase 1)

```go
config := &game.MiningLoopConfig{
    AgentID:        agentID,
    UseBulkSell:    true,
    Tier1Threshold: 300.0,

    // Stop when we reach our goal
    StopCondition: func(state *game.State) bool {
        // Stop when we have the ship and equipment we need
        hasDrillship := state.Ship.ClassID == "mining_enhanced"
        hasEnoughLasers := countMiningLasers(state) >= 3
        return hasDrillship && hasEnoughLasers
    },

    OnUpgradeCheck: func() bool {
        upgraded := attemptExplorerUpgrades(client, logger, ctx)
        return upgraded
    },
}

result, err := game.MiningLoop(client, logger, ctx, config)
if err != nil {
    log.Fatalf("Mining phase error: %v", err)
}

// Check why we stopped
if result.StoppedReason == "stop_condition" {
    logger.Printf("✅ Mining phase complete!")
    logger.Printf("Runs: %d, Credits earned: %.2f",
        result.RunsCompleted, result.TotalCreditsEarned)
}
```

## Configuration Options

### Required
- None - all options have sensible defaults

### Optional

#### Agent Identification
- `AgentID` - Agent ID for captain's log updates (string)

#### Stop Conditions
- `StopCondition` - Function called before each run to check if we should stop
  - `func(state *State) bool`
  - Return `true` to stop mining, `false` to continue
  - If `nil`, mining continues indefinitely

#### Callbacks
- `OnRunComplete` - Called after each successful mining run
  - `func(runNum int, creditsEarned float64, totalCredits float64)`
  - Useful for tracking stats, updating logs, etc.

- `OnUpgradeCheck` - Called when it's time to check for upgrades
  - `func() bool`
  - Return `true` if an upgrade was performed
  - Return value currently not used but reserved for future features

#### Upgrade Timing
- `UpgradeCheckInterval` - How often to check for upgrades (int)
  - If `0`, checks every run when `credits >= Tier1Threshold`
  - If `> 0`, checks every N runs

- `Tier1Threshold` - Minimum credits to trigger upgrade checks (float64)
  - Default: `300.0`

#### Economic Settings
- `ReserveCredits` - Amount to always keep (never spend below this) (float64)
  - Default: `50.0`

#### Mining Behavior
- `MaxMiningAttempts` - Limits mining iterations per run (int)
  - If `0`, calculates based on cargo capacity and laser count
  - Useful for testing or rate-limiting

- `CargoFullThreshold` - Percentage of cargo to consider "full" (float64)
  - Default: `0.97` (97%)
  - Range: `0.0` to `1.0`

- `FuelLowThreshold` - Percentage of fuel to consider "low" (float64)
  - Default: `0.1` (10%)
  - Range: `0.0` to `1.0`

#### Performance
- `UseBulkSell` - Use `SellAllBulk` instead of `SellAll` (bool)
  - Default: `false`
  - Recommended: `true` for better performance

- `CaptainsLogInterval` - How often to update captain's log (time.Duration)
  - Default: `2 * time.Minute`

## Return Value

The `MiningLoop` returns a `*MiningLoopResult` containing:

```go
type MiningLoopResult struct {
    RunsCompleted      int     // Number of complete mining runs
    TotalCreditsEarned float64 // Total credits earned from all runs
    StartingCredits    float64 // Credits when loop started
    EndingCredits      float64 // Credits when loop ended
    StoppedReason      string  // Why the loop stopped
}
```

### Stop Reasons
- `"context_cancelled"` - Context was cancelled
- `"stop_condition"` - StopCondition returned true
- `"no_mining_poi"` - No mining POI found in system
- `"no_station"` - No station found in system
- `"error"` - An error occurred (check returned error)

## Integration Examples

### MiningLoop / MiningStrategy
See `pkg/strategy/mining.go` for a complete example of continuous mining with upgrades.

### Auto-Explorer
The mining loop can be used for Phase 1 (earning credits for exploration equipment):

```go
func miningPhase(client *game.Client, logger *log.Logger, ctx context.Context) error {
    config := &game.MiningLoopConfig{
        AgentID:     agentID,
        UseBulkSell: true,

        StopCondition: func(state *game.State) bool {
            // Stop when Phase 1 is complete
            hasDrillship := state.Ship.ClassID == "mining_enhanced"
            hasLasers := countMiningLasers(state) >= 3
            hasScanner := hasScanner(state)
            return (hasDrillship && hasLasers) || (hasScanner && hasMiningLaser(state))
        },

        OnUpgradeCheck: func() bool {
            return attemptExplorerUpgrades(client, logger, ctx)
        },
    }

    result, err := game.MiningLoop(client, logger, ctx, config)
    if err != nil {
        return err
    }

    if result.StoppedReason == "stop_condition" {
        logger.Printf("✅ Phase 1 COMPLETE!")
        logger.Printf("Credits: %.2f -> %.2f (earned %.2f)",
            result.StartingCredits, result.EndingCredits, result.TotalCreditsEarned)
    }

    return nil
}
```

## Benefits

1. **Code Reuse** - Single implementation shared across all agents
2. **Consistency** - All agents use the same proven mining logic
3. **Maintainability** - Fix bugs once, benefit everywhere
4. **Flexibility** - Configurable for different use cases
5. **Testing** - Easy to test mining behavior in isolation

## Migration Guide

If you have an existing agent with a custom mining loop:

1. Identify configuration needs (continuous vs goal-based)
2. Extract upgrade logic into `OnUpgradeCheck` callback
3. Extract logging/stats into `OnRunComplete` callback
4. Set appropriate thresholds and intervals
5. Replace custom loop with `game.MiningLoop` call
6. Test thoroughly!

## Future Enhancements

Potential future improvements:
- Support for multiple mining locations
- Smart POI selection (closest, richest, safest)
- Combat avoidance during mining
- Repair/refuel thresholds per agent type
- Mining efficiency tracking
- Automatic system switching when depleted
