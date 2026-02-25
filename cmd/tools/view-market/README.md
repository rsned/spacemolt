# SpaceMolt View Market

> Interactive CLI tool for querying and viewing market data, price history, and arbitrage opportunities from the knowledge base.

## Overview

view-market provides command-line access to SpaceMolt market data stored in the knowledge base. It allows you to query current market listings, historical prices, discovered items, and identify arbitrage opportunities across different systems and stations.

## Features

### Core Functionality
- **📊 Market Listings** - View current buy/sell orders
- **📈 Price History** - Track price changes over time
- **🔍 Item Discovery** - List all items seen across markets
- **💰 Arbitrage** - Find buy-low-sell-high opportunities
- **🎨 Beautiful Output** - Styled terminal output with colors

### Commands

- **latest** - Show most recent market snapshot
- **history** - Show historical market snapshots
- **items** - List all unique items seen
- **prices** - Show price history for an item
- **arbitrage** - Show arbitrage opportunities

## Quick Start

### Basic Usage

```bash
# View latest market for a system
go run ./cmd/view-market latest sol

# View market history
go run ./cmd/view-market history sol --limit 50

# List all items
go run ./cmd/view-market items

# Show price history for an item
go run ./cmd/view-market prices iron_ore

# Find arbitrage opportunities
go run ./cmd/view-market arbitrage
```

### Building

```bash
# Build the binary
go build -o bin/view-market ./cmd/view-market

# Run the built binary
./bin/view-market latest sol
```

## Command-Line Flags

### Global Flags

```
-db-path string
    Path to SQLite database file (default "spacemolt-knowledge.db")

-limit int
    Limit number of records to show (default 20)

-format string
    Output format: table, json (default "table")
```

## Commands

### latest

Show the most recent market snapshot for a system/station.

**Usage:**
```bash
view-market latest <system-id> [station-id] [flags]
```

**Example:**
```bash
# Latest market for Sol system (auto-detects station)
go run ./cmd/view-market latest sol

# Latest market for specific station
go run ./cmd/view-market latest sol station-1
```

**Output:**
```
Market: Sol / Station-1
────────────────────────────────────────────────────────────────────────────────

  Captured: 2 min ago
  Game Tick: 12345
  Agent: explorer-7

Sell Orders (Available)
────────────────────────────────────────────────────────────────────────────────

iron_ore
  iron_ore x 100.00 @ 15.50 credits
  iron_ore x 50.00 @ 16.00 credits

copper_ore
  copper_ore x 75.00 @ 12.00 credits

Buy Orders (Wanted)
────────────────────────────────────────────────────────────────────────────────

refined_iron
  refined_iron x 20.00 @ 25.00 credits

────────────────────────────────────────────────────────────────────────────────

Total: 4 listing(s)
```

### history

Show historical market snapshots for a system/station.

**Usage:**
```bash
view-market history <system-id> [station-id] [flags]
```

**Example:**
```bash
go run ./cmd/view-market history sol --limit 10
```

**Output:**
```
Market History
────────────────────────────────────────────────────────────────────────────────

[2 min ago] Sol / Station-1
  Tick: 12345 | Agent: explorer-7
  → Snapshot ID: 42 (run 'view-market latest sol station-1' for details)

────────────────────────────────────────────────────────────────────────────────

[1 hr ago] Sol / Station-1
  Tick: 12300 | Agent: explorer-7
  → Snapshot ID: 41 (run 'view-market latest sol station-1' for details)

────────────────────────────────────────────────────────────────────────────────

[2 hr ago] Sol / Station-1
  Tick: 12250 | Agent: explorer-7
  → Snapshot ID: 40 (run 'view-market latest sol station-1' for details)

────────────────────────────────────────────────────────────────────────────────

Total: 3 snapshot(s)
```

### items

List all unique items seen across markets.

**Usage:**
```bash
view-market items [flags]
```

**Example:**
```bash
go run ./cmd/view-market items --limit 100
```

**Output:**
```
Market Items
────────────────────────────────────────────────────────────────────────────────

Ore
  • copper_ore
  • iron_ore
  • titanium_ore

Refined Materials
  • copper_ingot
  • iron_ingot
  • titanium_ingot

Components
  • basic_circuit
  • advanced_circuit
  • power_cell

────────────────────────────────────────────────────────────────────────────────

Total: 9 unique item(s)
```

### prices

Show price history for an item across systems.

**Usage:**
```bash
view-market prices <item-id> [flags]
```

**Example:**
```bash
go run ./cmd/view-market prices iron_ore --limit 20
```

**Output:**
```
Price History: iron_ore
────────────────────────────────────────────────────────────────────────────────

[2 min ago] Sol / Station-1
  sell x 100.00 @ 15.50 credits

[1 hr ago] Sol / Station-1
  sell x 75.00 @ 15.00 credits

[2 hr ago] Sol-2 / Station-2
  sell x 50.00 @ 14.50 credits

────────────────────────────────────────────────────────────────────────────────

Total: 3 price record(s)
```

### arbitrage

Show arbitrage opportunities (buy low, sell high).

**Usage:**
```bash
view-market arbitrage [flags]
```

**Example:**
```bash
go run ./cmd/view-market arbitrage --limit 10
```

**Output:**
```
Arbitrage Opportunities
────────────────────────────────────────────────────────────────────────────────

Item                       Buy At        Sell At       Profit        Margin
────────────────────────────────────────────────────────────────────────────────
iron_ore                   14.50        16.00        1.50          10.3%
copper_ore                 11.50        13.00        1.50          13.0%
titanium_ore               45.00        52.00        7.00          15.6%

────────────────────────────────────────────────────────────────────────────────

Total: 3 opportunity(ies)
```

## Output Formats

### Table Format (Default)

Human-readable with colors and formatting:
```bash
go run ./cmd/view-market latest sol
```

### JSON Format

Machine-readable for automation:
```bash
go run ./cmd/view-market latest sol --format json
```

**Output:**
```json
{
  "system_name": "Sol",
  "station_name": "Station-1",
  "captured_at": "2026-02-23T10:15:30Z",
  "game_tick": 12345,
  "agent_id": "explorer-7",
  "listings": [
    {
      "item_id": "iron_ore",
      "item_type": "ore",
      "quantity": 100.0,
      "price_per_unit": 15.50,
      "total_price": 1550.0,
      "type": "sell",
      "listed_by": "station"
    }
  ]
}
```

## Examples

### Example 1: Quick Market Check

```bash
# Check current market
go run ./cmd/view-market latest sol

# Check specific station
go run ./cmd/view-market latest sol station-1
```

### Example 2: Price Analysis

```bash
# Track price changes
go run ./cmd/view-market prices iron_ore --limit 50

# View historical data
go run ./cmd/view-market history sol --limit 20
```

### Example 3: Find Trading Opportunities

```bash
# Find arbitrage
go run ./cmd/view-market arbitrage

# Show top opportunities
go run ./cmd/view-market arbitrage --limit 5
```

### Example 4: Market Research

```bash
# List all available items
go run ./cmd/view-market items

# Check prices for specific item
go run ./cmd/view-market prices refined_iron
```

### Example 5: Data Export

```bash
# Export market data
go run ./cmd/view-market latest sol --format json > sol-market.json

# Export arbitrage data
go run ./cmd/view-market arbitrage --format json > arbitrage.json
```

## Database Schema

The tool queries these tables:

### market_snapshots
```sql
CREATE TABLE market_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    system_id TEXT NOT NULL,
    system_name TEXT NOT NULL,
    station_id TEXT NOT NULL,
    station_name TEXT NOT NULL,
    game_tick INTEGER NOT NULL,
    captured_at TEXT NOT NULL,
    agent_id TEXT
);
```

### market_listings
```sql
CREATE TABLE market_listings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    item_id TEXT NOT NULL,
    item_type TEXT NOT NULL,
    quantity REAL NOT NULL,
    price_per_unit REAL NOT NULL,
    total_price REAL NOT NULL,
    listing_type TEXT NOT NULL,
    listed_by TEXT,
    FOREIGN KEY (snapshot_id) REFERENCES market_snapshots(id)
);
```

## Integration

### With Trading Agents

```bash
#!/bin/bash
# Find best trade routes

# Get arbitrage opportunities
OPPORTUNITIES=$(go run ./cmd/view-market arbitrage --format json)

# Parse and act on opportunities
echo "$OPPORTUNITIES" | jq -r '.[] | select(.profit_margin > 20) | "\(.item_id): buy at \(.min_sell_price), sell at \(.max_buy_price)"'
```

### With Monitoring

```bash
#!/bin/bash
# Monitor market prices

ITEM="iron_ore"
SYSTEM="sol"

while true; do
  echo "=== $(date) ==="
  go run ./cmd/view-market latest $SYSTEM
  sleep 300
done
```

### With Analysis

```bash
#!/bin/bash
# Compare prices across systems

for system in sol sol-2 corsair; do
  echo "=== $system ==="
  go run ./cmd/view-market latest $system --format json | \
    jq -r '.listings[] | select(.item_id == "iron_ore") | "\(.price_per_unit)"'
done
```

## Arbitrage Logic

The arbitrage command uses this logic:

```sql
WITH LatestSnapshots AS (
    SELECT DISTINCT system_id, station_id, MAX(captured_at) as latest_captured
    FROM market_snapshots
    GROUP BY system_id, station_id
),
LatestListings AS (
    SELECT ml.item_id, ml.item_type, ml.listing_type, ml.price_per_unit,
           ms.system_id, ms.system_name, ms.station_id, ms.station_name
    FROM market_listings ml
    JOIN market_snapshots ms ON ml.snapshot_id = ms.id
    JOIN LatestSnapshots ls ON ms.system_id = ls.system_id
        AND ms.station_id = ls.station_id
        AND ms.captured_at = ls.latest_captured
)
SELECT item_id, item_type,
       MIN(CASE WHEN listing_type = 'sell' THEN price_per_unit END) as min_sell_price,
       MAX(CASE WHEN listing_type = 'buy' THEN price_per_unit END) as max_buy_price
FROM LatestListings
WHERE listing_type IN ('sell', 'buy')
GROUP BY item_id, item_type
HAVING min_sell_price IS NOT NULL
  AND max_buy_price IS NOT NULL
  AND max_buy_price > min_sell_price
ORDER BY (max_buy_price - min_sell_price) DESC
```

## Troubleshooting

### Issue: "No market data found"

**Cause:** No market snapshots captured yet.

**Solution:**
1. Run agents to capture market data
2. Verify agents are visiting stations
3. Check agent is recording market data

### Issue: "Failed to open database"

**Cause:** Database file not found or corrupted.

**Solution:**
1. Verify database path: `-db-path spacemolt-knowledge.db`
2. Check file exists
3. Check file permissions

### Issue: "No price data found for this item"

**Cause:** Item not seen in any market.

**Solution:**
1. Check item ID is correct
2. List all items: `go run ./cmd/view-market items`
3. Wait for agents to discover more markets

### Issue: "No arbitrage opportunities found"

**Cause:** No profitable trades available or insufficient data.

**Solution:**
1. Verify multiple systems have been visited
2. Check recent market data exists
3. Consider lowering profit margin threshold

## Best Practices

### Regular Market Monitoring

```bash
# Daily market check
go run ./cmd/view-market latest sol

# Weekly arbitrage scan
go run ./cmd/view-market arbitrage --limit 50
```

### Data Export

```bash
# Regular backups
go run ./cmd/view-market latest sol --format json > backup/sol-$(date +%Y%m%d).json

# Export all markets
for system in sol sol-2 corsair; do
  go run ./cmd/view-market latest $system --format json > backup/$system-$(date +%Y%m%d).json
done
```

### Price Tracking

```bash
# Track specific item
go run ./cmd/view-market prices iron_ore --limit 100 --format json > iron-ore-prices.json

# Analyze with jq
jq -r '.[] | "\(.captured_at): \(.price_per_unit)"' iron-ore-prices.json
```

## Related Tools

- [view-learning](../view-learning/) - View agent learning data
- [Knowledge Base](../../pkg/knowledge/) - Knowledge base implementation
- [Market Data Collection](../../pkg/knowledge/market.go) - Market capture logic

## License

Part of the SpaceMolt project.
