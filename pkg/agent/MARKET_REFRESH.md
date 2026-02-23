# Market Data Refresh Library

## Overview

The market refresh library provides automatic freshness checking and updating of market data for trading and crafting agents.

## Key Features

- **Automatic Freshness Checking**: Market data is considered fresh for 1 hour (360 game ticks)
- **Lazy Refresh**: Only fetches new market data when the cached data is stale
- **Knowledge Base Integration**: Stores market snapshots in SQLite for persistence
- **Simple API**: One function call to get fresh market data

## Usage

### Basic Usage

```go
import (
    "context"
    "github.com/rsned/spacemolt/pkg/agent"
    "github.com/rsned/spacemolt/pkg/game"
    "github.com/rsned/spacemolt/pkg/knowledge"
)

func makeTradingDecision(ctx context.Context, client *game.Client, kb knowledge.Base, agentID string) error {
    // Get fresh market data (automatically refreshes if older than 1 hour)
    snapshot, err := agent.RefreshMarketData(ctx, client, kb, agentID)
    if err != nil {
        return fmt.Errorf("failed to get market data: %w", err)
    }

    // Use the market data to make decisions
    for _, listing := range snapshot.Listings {
        if listing.Type == "buy" && listing.ItemID == "ore_iron" {
            fmt.Printf("Iron available at %s: %.0f units @ %.0f credits each\n",
                snapshot.StationName, listing.Quantity, listing.PricePerUnit)
        }
    }

    return nil
}
```

### Check if Refresh is Needed

```go
// Check if market data needs refreshing without actually refreshing
shouldRefresh, err := agent.ShouldRefreshMarket(ctx, kb, "haven", "haven_exchange")
if err != nil {
    return fmt.Errorf("failed to check market freshness: %w", err)
}

if shouldRefresh {
    fmt.Println("Market data is stale, consider refreshing")
}
```

### Get Market Age

```go
// Get the age of the market data
age, exists, err := agent.GetMarketAge(ctx, kb, "haven", "haven_exchange")
if err != nil {
    return fmt.Errorf("failed to get market age: %w", err)
}

if exists {
    fmt.Printf("Market data is %s old\n", age)
} else {
    fmt.Println("No market data available")
}
```

## API Reference

### RefreshMarketData

```go
func RefreshMarketData(ctx context.Context, client *game.Client, kb knowledge.Base, agentID string) (*knowledge.MarketSnapshot, error)
```

Ensures fresh market data for the current station. Returns cached data if less than 1 hour old, otherwise captures fresh data.

**Parameters:**
- `ctx`: Context for the operation
- `client`: Game client for fetching market data
- `kb`: Knowledge base for storing/retrieving snapshots
- `agentID`: Agent identifier for tracking

**Returns:**
- `*knowledge.MarketSnapshot`: Fresh market snapshot
- `error`: Any error encountered

### ShouldRefreshMarket

```go
func ShouldRefreshMarket(ctx context.Context, kb knowledge.Base, systemID, stationID string) (bool, error)
```

Checks if market data should be refreshed (doesn't actually refresh).

**Returns:**
- `bool`: True if data doesn't exist or is stale
- `error`: Any error encountered

### GetMarketAge

```go
func GetMarketAge(ctx context.Context, kb knowledge.Base, systemID, stationID string) (time.Duration, bool, error)
```

Returns the age of the most recent market snapshot.

**Returns:**
- `time.Duration`: Age of the snapshot
- `bool`: True if snapshot exists
- `error`: Any error encountered

## Market Data Freshness

Market data is considered fresh for **1 hour** (360 game ticks = 3600 seconds). This threshold is defined in `MarketFreshnessThreshold`.

The freshness check is based on the `CapturedAt` timestamp of the market snapshot stored in the knowledge base.

## Market Snapshots

Each market snapshot contains:

```go
type MarketSnapshot struct {
    SystemID    string           // System identifier (e.g., "haven")
    SystemName  string           // Human-readable system name
    StationID   string           // Station identifier (e.g., "haven_exchange")
    StationName string           // Human-readable station name
    GameTick    int64            // Game tick when captured
    Listings    []MarketListing  // Market listings
    CapturedAt  time.Time        // When the snapshot was captured
}
```

Each listing contains:

```go
type MarketListing struct {
    ItemID       string  // Item identifier (e.g., "ore_iron")
    ItemType     string  // Item type
    Quantity     float64 // Available quantity
    PricePerUnit float64 // Price per unit
    TotalPrice   float64 // Total price
    Type         string  // "buy" or "sell"
    ListedBy     string  // Who listed it
}
```

## Database Storage

Market snapshots are stored in the `market_snapshots` table with the following schema:

```sql
CREATE TABLE market_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    system_id TEXT NOT NULL,
    system_name TEXT NOT NULL,
    station_id TEXT NOT NULL,
    station_name TEXT NOT NULL,
    game_tick INTEGER NOT NULL,
    captured_at TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    last_updated_tick INTEGER NOT NULL
);
```

Listings are stored in the `market_listings` table linked to their snapshot.

## Performance Considerations

- **Caching**: Fresh data is returned immediately without server calls
- **Batch Queries**: Market data captures all listings at once
- **Indexed Queries**: Database is indexed on system_id and station_id for fast lookups

## Integration Example

Here's how to integrate market refresh into an auto-trader:

```go
func (trader *AutoTrader) Run(ctx context.Context) error {
    for {
        // Get fresh market data
        snapshot, err := agent.RefreshMarketData(ctx, trader.client, trader.kb, trader.agentID)
        if err != nil {
            return fmt.Errorf("market refresh failed: %w", err)
        }

        // Analyze market and make trading decisions
        trades := trader.findProfitableTrades(snapshot)

        // Execute trades
        for _, trade := range trades {
            if err := trader.executeTrade(ctx, trade); err != nil {
                log.Printf("Trade failed: %v", err)
            }
        }

        // Wait before next iteration
        time.Sleep(5 * time.Minute)
    }
}
```
