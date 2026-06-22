# Market Data Refresh Library

## Overview

The market refresh library provides automatic freshness checking and updating
of market data for trading and crafting agents. As of the market-data
consolidation, all volatile market data lives in the separate `pkg/market`
database (`data/market.db`) — these helpers take a `*market.Collector`, not a
`knowledge.Base`.

## Key Features

- **Automatic Freshness Checking**: Market data is fresh for 1 hour; LLM
  analysis is fresh for 2 hours.
- **Lazy Refresh**: Only captures new market data when the cached snapshot is stale.
- **Single Source**: Reads and writes the `pkg/market` collector (`data/market.db`).
- **Simple API**: One call to get fresh market data.

## Usage

### Basic Usage

```go
import (
    "context"

    "github.com/rsned/spacemolt/pkg/agent"
    "github.com/rsned/spacemolt/pkg/game"
    "github.com/rsned/spacemolt/pkg/market"
)

func makeTradingDecision(ctx context.Context, client *game.Client, mc *market.Collector, agentID string) error {
    // Get fresh market data (captures a new snapshot if older than 1 hour).
    snapshot, err := agent.RefreshMarketData(ctx, client, mc, agentID)
    if err != nil {
        return fmt.Errorf("failed to get market data: %w", err)
    }

    // Use the market data to make decisions.
    for _, order := range snapshot.Orders {
        if order.Side == "sell" && order.ItemID == "ore_iron" {
            fmt.Printf("Iron available at %s: %.0f units @ %.0f credits each\n",
                snapshot.StationName, order.Quantity, order.PriceEach)
        }
    }

    return nil
}
```

## API Reference

### RefreshMarketData

```go
func RefreshMarketData(ctx context.Context, client *game.Client, mc *market.Collector, agentID string) (*market.MarketSnapshot, error)
```

Ensures fresh market data for the current station. Returns the cached snapshot
if it is less than `MarketFreshnessThreshold` old, otherwise captures fresh data
via `CaptureMarketData` and reads it back. Internally uses
`mc.GetLatestSnapshot` for the freshness check.

### CaptureMarketData

```go
func CaptureMarketData(ctx context.Context, client *game.Client, mc *market.Collector, agentID string) error
```

Fetches the current station's order book from the game and writes it to the
collector via `mc.WriteSnapshot` (converting `game.MarketListing` →
`[]market.Order` with `market.OrdersFromListings`).

### RefreshMarketAnalysis

```go
func RefreshMarketAnalysis(ctx context.Context, client *game.Client, mc *market.Collector, agentID string) (*market.MarketAnalysis, error)
```

Ensures fresh LLM `analyze_market` output, refreshing when older than
`MarketAnalysisFreshnessThreshold`. Persists via `mc.StoreAnalysis` and reads
back via `mc.GetLatestAnalysis`.

## Freshness Thresholds

| Constant | Value | Meaning |
|---|---|---|
| `MarketFreshnessThreshold` | `360 * time.Second` (1 hour) | Snapshot freshness |
| `MarketAnalysisFreshnessThreshold` | `720 * time.Second` (2 hours) | LLM analysis freshness |

Freshness is based on the `CapturedAt` timestamp of the latest snapshot/analysis
returned by the collector.

## Market Snapshots

Each snapshot (`market.MarketSnapshot`) carries station/system identity plus the
captured orders:

```go
type MarketSnapshot struct {
    StationID   string
    StationName string
    SystemID    string
    SystemName  string
    Orders      []Order
    CapturedAt  time.Time
}

type Order struct {
    StationID  string
    ItemID     string
    ItemName   string
    Side       string  // "buy" or "sell"
    PriceEach  float64
    Quantity   float64
    MyQuantity float64
    Source     string  // provenance, e.g. agent id or "play_as"
    CapturedAt time.Time
    BucketUTC  string  // captured_at truncated to the hour
}
```

## Database Storage

Snapshots are reconstructed from the `market_orders` table in `data/market.db`
(the set of orders for a station sharing the newest `captured_at`). Station and
item names are joined from the `stations` and `items` tables. See
`pkg/market/schema.sql` for the full schema and `pkg/market/query.go` for the
read API (`GetLatestSnapshot`, `HasSnapshotToday`, `FindBestPrices`,
`GetLatestAnalysis`).

## Integration Example

```go
func (trader *AutoTrader) Run(ctx context.Context) error {
    for {
        snapshot, err := agent.RefreshMarketData(ctx, trader.client, trader.market, trader.agentID)
        if err != nil {
            return fmt.Errorf("market refresh failed: %w", err)
        }

        trades := trader.findProfitableTrades(snapshot)
        for _, trade := range trades {
            if err := trader.executeTrade(ctx, trade); err != nil {
                log.Printf("Trade failed: %v", err)
            }
        }

        time.Sleep(game.SleepLong)
    }
}
```
