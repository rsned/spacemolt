# Bulk Sell Order API

This library provides efficient bulk selling of cargo using the new `create_sell_order` API endpoint, which can process up to 50 items in a single API call instead of requiring N separate calls with 10-second waits between each.

## Quick Start

### Basic Usage (Replace SellAll)

The simplest way to use bulk selling is with the `SellAllBulk()` method:

```go
// Old way (slow - multiple API calls)
if err := client.SellAll(ctx); err != nil {
    log.Printf("Error selling: %v", err)
}

// New way (fast - single API call)
if err := client.SellAllBulk(ctx, nil); err != nil {
    log.Printf("Error selling: %v", err)
}
```

### With Reserved Items

If you want to keep certain items (e.g., crafting materials), pass them as reserved items:

```go
// Keep some crystals for crafting
reservedItems := []string{"crystal_blue", "crystal_red", "gas_helium"}
if err := client.SellAllBulk(ctx, reservedItems); err != nil {
    log.Printf("Error selling: %v", err)
}
```

## Advanced Usage

For more control over pricing and what gets sold, you can use the individual functions:

### Step 1: Get Market Prices

```go
// Fetch current market listings
if err := client.GetListings(ctx); err != nil {
    log.Printf("Warning: Failed to get listings: %v", err)
}
time.Sleep(1 * time.Second) // Wait for response

// Get the listings
listings := client.GetMarketListings()

// Create price map based on market data
state := client.GetState()
priceMap := game.GetMarketPricesForCargo(state.Ship.Cargo, listings)
```

### Step 2: Prepare Bulk Order

```go
// Prepare the bulk sell order
reservedItems := []string{"crystal_blue"} // Items to keep
orders, skipped := game.PrepareBulkSellOrder(state.Ship.Cargo, reservedItems, priceMap)

log.Printf("Prepared %d sell orders (%d items skipped)", len(orders), skipped)
```

### Step 3: Create the Sell Orders

```go
// Send the bulk order to the server
if len(orders) > 0 {
    if err := client.CreateBulkSellOrder(ctx, orders); err != nil {
        log.Printf("Error creating sell orders: %v", err)
    } else {
        log.Printf("Successfully created %d sell orders!", len(orders))
    }
}
```

## Custom Pricing Strategy

You can also provide your own pricing:

```go
// Custom pricing: sell everything at 10 credits each
customPrices := map[string]int{
    "ore_iron":   10,
    "ore_copper": 15,
    "ore_gold":   50,
}

orders, _ := game.PrepareBulkSellOrder(state.Ship.Cargo, nil, customPrices)
client.CreateBulkSellOrder(ctx, orders)
```

## What Gets Sold?

The bulk sell functions automatically:

- ✅ **Sell**: Ores (`ore_*`), gases (`gas_*`), crystals (`crystal_*`), salvage (`salvage_*`), scrap (`scrap_*`)
- ❌ **Skip**: Equipment (mining lasers, weapons, shields, cargo modules, engines, etc.)
- ❌ **Skip**: Reserved items (items you explicitly want to keep)
- ❌ **Skip**: Zero-quantity items

## Performance Benefits

### Old Method (SellAll)
```go
// For 10 items in cargo:
// - 10 API calls
// - 10 * 10 seconds = 100 seconds wait time
// - Total time: ~100+ seconds
```

### New Method (SellAllBulk)
```go
// For 10 items in cargo:
// - 2 API calls (get_listings + create_sell_order)
// - ~11 seconds wait time
// - Total time: ~11 seconds
```

**Result: ~9x faster for 10 items! Scales even better with more items (up to 50).**

## API Reference

### `PrepareBulkSellOrder(cargo, reservedItems, pricePerItem)`

Prepares cargo for bulk selling.

**Parameters:**
- `cargo []CargoItem` - Ship's cargo contents
- `reservedItems []string` - Items to keep (won't be sold)
- `pricePerItem map[string]int` - Price per item (uses default of 1 if nil or missing)

**Returns:**
- `orders []BulkSellOrder` - Ready for API call
- `skippedCount int` - Number of items skipped

### `GetMarketPricesForCargo(cargo, listings)`

Creates competitive pricing based on market data.

**Strategy:**
1. If buy orders exist → Use highest buy price (instant fill)
2. If only sell orders → Undercut lowest sell by 1 credit
3. If no market data → Not included in map (use default)

**Parameters:**
- `cargo []CargoItem` - Items to price
- `listings []MarketListing` - Market data from client.GetMarketListings()

**Returns:**
- `priceMap map[string]int` - Suggested prices

### `Client.CreateBulkSellOrder(ctx, orders)`

Sends bulk sell order to server (max 50 items).

**Parameters:**
- `ctx context.Context` - Context
- `orders []BulkSellOrder` - Orders from PrepareBulkSellOrder()

**Returns:**
- `error` - nil on success

### `Client.SellAllBulk(ctx, reservedItems)`

High-level method that does everything automatically.

**Parameters:**
- `ctx context.Context` - Context
- `reservedItems []string` - Items to keep (optional, can be nil)

**Returns:**
- `error` - nil on success

## Migration Guide

### Before (Old SellAll)
```go
func sellAndReturn(client *game.Client, ctx context.Context) {
    // Sell cargo
    if err := client.SellAll(ctx); err != nil {
        log.Printf("Error: %v", err)
        return
    }
    // This took ~N*10 seconds for N items
}
```

### After (New SellAllBulk)
```go
func sellAndReturn(client *game.Client, ctx context.Context) {
    // Sell cargo - much faster!
    if err := client.SellAllBulk(ctx, nil); err != nil {
        log.Printf("Error: %v", err)
        return
    }
    // This takes ~11 seconds regardless of item count (up to 50)
}
```

### With Reserved Items
```go
func sellAndReturn(client *game.Client, ctx context.Context) {
    // Keep crafting materials
    reserved := []string{"crystal_blue", "gas_helium"}

    if err := client.SellAllBulk(ctx, reserved); err != nil {
        log.Printf("Error: %v", err)
        return
    }
}
```

## Notes

- Maximum 50 items per bulk order (API limitation)
- Requires being docked at a station
- Creates sell orders (not instant sells) - orders persist until filled
- Uses market pricing when available for competitive rates
- Automatically filters out equipment to prevent accidental sales
