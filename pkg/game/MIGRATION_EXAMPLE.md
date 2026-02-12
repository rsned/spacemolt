# Migration Example: Converting auto-miner to Use Bulk Sell

This document shows how to convert an auto-* tool from the old `SellAll()` method to the new `SellAllBulk()` method.

## Before: Old Code (Slow)

```go
// In auto-miner/main.go (or any auto-* tool)

// Check if cargo is full
state = client.GetState()
if state.Ship.CargoUsed >= state.Ship.CargoCapacity {
    logger.Printf("💰 Cargo full! Returning to dock and sell...")

    // Return to dock
    if !state.Doc {
        if err := returnToDock(client, ctx, state, logger); err != nil {
            logger.Printf("Failed to dock: %v", err)
            continue
        }
    }

    // Sell all cargo using OLD method
    creditsBefore := state.Credits
    logger.Printf("💰 Selling %d different items...", len(state.Ship.Cargo))

    // OLD WAY: Multiple API calls, slow
    if err := client.SellAll(ctx); err != nil {
        logger.Printf("Sell error: %v", err)
    } else {
        time.Sleep(5 * time.Second)
        state = client.GetState()
        creditsEarned := state.Credits - creditsBefore
        logger.Printf("✅ Sold cargo! Earned %.2f credits", creditsEarned)
    }

    // PROBLEM: If we had 10 items, this took 100+ seconds!
}
```

## After: New Code (Fast)

```go
// In auto-miner/main.go (or any auto-* tool)

// Check if cargo is full
state = client.GetState()
if state.Ship.CargoUsed >= state.Ship.CargoCapacity {
    logger.Printf("💰 Cargo full! Returning to dock and sell...")

    // Return to dock
    if !state.Doc {
        if err := returnToDock(client, ctx, state, logger); err != nil {
            logger.Printf("Failed to dock: %v", err)
            continue
        }
    }

    // Sell all cargo using NEW bulk method
    creditsBefore := state.Credits
    logger.Printf("💰 Selling cargo in bulk...")

    // NEW WAY: Single API call, fast!
    if err := client.SellAllBulk(ctx, nil); err != nil {
        logger.Printf("Sell error: %v", err)
    } else {
        time.Sleep(5 * time.Second)
        state = client.GetState()
        creditsEarned := state.Credits - creditsBefore
        logger.Printf("✅ Sold cargo in bulk! Earned %.2f credits", creditsEarned)
    }

    // IMPROVEMENT: Same 10 items now take only ~11 seconds!
}
```

## With Reserved Items (Advanced)

If your agent needs to keep certain items for crafting or other purposes:

```go
// Example: auto-craftsman keeping materials
state = client.GetState()
if state.Ship.CargoUsed >= state.Ship.CargoCapacity {
    logger.Printf("💰 Cargo full! Selling excess items...")

    // Return to dock
    if !state.Doc {
        if err := returnToDock(client, ctx, state, logger); err != nil {
            logger.Printf("Failed to dock: %v", err)
            continue
        }
    }

    // Define items to keep for crafting
    reservedItems := []string{
        "crystal_blue",
        "crystal_red",
        "gas_helium",
        "ore_titanium",  // Need for specific recipes
    }

    creditsBefore := state.Credits
    logger.Printf("💰 Selling non-reserved cargo...")

    if err := client.SellAllBulk(ctx, reservedItems); err != nil {
        logger.Printf("Sell error: %v", err)
    } else {
        time.Sleep(5 * time.Second)
        state = client.GetState()
        creditsEarned := state.Credits - creditsBefore
        logger.Printf("✅ Sold excess cargo! Earned %.2f credits (kept %d reserved items)",
            creditsEarned, len(reservedItems))
    }
}
```

## Complete Example: auto-miner Main Loop

Here's a complete before/after comparison in the main mining loop:

### BEFORE (Slow)
```go
for {
    select {
    case <-ctx.Done():
        return
    default:
    }

    state := client.GetState()

    // Mining logic...
    if !state.Doc && !state.Traveling {
        if err := client.Mine(ctx); err != nil {
            logger.Printf("Mining error: %v", err)
        }
        time.Sleep(10 * time.Second)
        continue
    }

    // Check cargo
    if state.Ship.CargoUsed >= state.Ship.CargoCapacity {
        if !state.Doc {
            returnToDock(client, ctx, state, logger)
        }

        // OLD: Slow selling
        logger.Printf("💰 Selling %d items...", len(state.Ship.Cargo))
        if err := client.SellAll(ctx); err != nil {  // SLOW!
            logger.Printf("Error: %v", err)
        }
        time.Sleep(5 * time.Second)

        // Try to buy upgrades
        tryUpgrades(client, ctx, state, logger)
    }
}
```

### AFTER (Fast)
```go
for {
    select {
    case <-ctx.Done():
        return
    default:
    }

    state := client.GetState()

    // Mining logic... (unchanged)
    if !state.Doc && !state.Traveling {
        if err := client.Mine(ctx); err != nil {
            logger.Printf("Mining error: %v", err)
        }
        time.Sleep(10 * time.Second)
        continue
    }

    // Check cargo
    if state.Ship.CargoUsed >= state.Ship.CargoCapacity {
        if !state.Doc {
            returnToDock(client, ctx, state, logger)
        }

        // NEW: Fast bulk selling
        logger.Printf("💰 Selling cargo in bulk...")
        if err := client.SellAllBulk(ctx, nil); err != nil {  // FAST!
            logger.Printf("Error: %v", err)
        }
        time.Sleep(5 * time.Second)

        // Try to buy upgrades
        tryUpgrades(client, ctx, state, logger)
    }
}
```

## Performance Impact

### Scenario: Mining ship with 10 ore types in cargo

**Before (SellAll):**
- `SellAll()` calls `Sell()` 10 times
- Each call waits 10 seconds
- Total time: 10 calls × 10 seconds = **100+ seconds**

**After (SellAllBulk):**
- `SellAllBulk()` makes 2 API calls:
  1. `get_listings` (~1 second)
  2. `create_sell_order` with bulk data (~10 seconds)
- Total time: **~11 seconds**

**Speedup: 9x faster!**

### Scenario: Mining ship with 30 ore types

**Before:** 30 × 10 seconds = **300+ seconds** (5 minutes!)
**After:** **~11 seconds** (same as above)

**Speedup: 27x faster!**

## Migration Checklist

For each auto-* tool that sells cargo:

- [ ] Find all calls to `client.SellAll(ctx)`
- [ ] Replace with `client.SellAllBulk(ctx, nil)`
- [ ] If tool needs to keep items, define `reservedItems` list
- [ ] Update log messages to reflect "bulk" operation
- [ ] Test with actual gameplay
- [ ] Remove any extra sleep/wait logic that was compensating for slow sells

## Files to Update

All auto-* tools that currently use `SellAll()`:

1. `cmd/auto-miner/main.go` - ✅ Primary candidate
2. `cmd/auto-explorer/main.go` - Check if it sells loot
3. `cmd/auto-salvager/main.go` - ✅ Primary candidate
4. `cmd/auto-trader/main.go` - May need custom pricing logic
5. `cmd/auto-pirate/main.go` - Check if it sells loot
6. `cmd/auto-fighter/main.go` - Check if it sells loot
7. `cmd/auto-craftsman/main.go` - Use reserved items for materials

## Important Notes

1. **API Change**: `create_sell_order` creates **sell orders** (not instant sells). Orders persist until filled by buyers.

2. **Pricing**: The bulk method fetches market prices automatically. Orders are priced competitively:
   - Match highest buy order (instant fill)
   - Or undercut lowest sell order by 1 credit

3. **Reserved Items**: Any items you want to keep must be in the `reservedItems` list, otherwise they'll be sold if they're ores/resources.

4. **Equipment Safety**: The function automatically skips equipment (mining lasers, weapons, shields, etc.), so you don't accidentally sell your upgrades.

5. **Rate Limiting**: The bulk API still respects the 1-call-per-tick (10 seconds) rate limit, but you only need 1 call instead of N calls.

## Testing

After migration, verify:

1. ✅ Cargo sells correctly (all ores/resources)
2. ✅ Equipment is not sold
3. ✅ Reserved items (if any) are kept
4. ✅ Credits are earned properly
5. ✅ Performance is significantly faster
6. ✅ No errors in logs
