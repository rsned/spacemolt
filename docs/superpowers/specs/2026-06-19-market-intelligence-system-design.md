# Market Intelligence System Design

**Date:** 2026-06-19
**Status:** Approved
**Author:** Design Brainstorming Session

---

## Overview

A separate SQLite database system that collects hourly market snapshots from ~40 station agents, stores individual orders for deep analysis, and supports both real-time arbitrage detection and historical trend analysis.

### Goals

1. **Data Collection** — Gather market data from all stations systematically
2. **Real-time Awareness** — Current market state matrix for quick arbitrage identification
3. **Historical Analysis** — Time-series data for trend detection, volatility analysis, and pattern discovery
4. **Arbitrage Detection** — Offline scanner that finds profitable opportunities for agents to execute

---

## Database Schema

### Location

- Path: `/home/robert/spacemolt/spacemolt/data/market.db`
- Separate from main knowledge base (`~/.local/state/spacemolt/knowledge.db`)
- Centralized — all 40 station agents write to the same DB

### Tables

#### Dimension Tables

```sql
-- Item catalog
CREATE TABLE items (
    item_id         TEXT PRIMARY KEY,
    item_name       TEXT NOT NULL,
    category        TEXT,
    first_seen_utc  TEXT NOT NULL,
    last_updated_utc TEXT NOT NULL
);

-- Stations (points of interest with markets)
CREATE TABLE stations (
    station_id      TEXT PRIMARY KEY,
    station_name    TEXT NOT NULL,
    system_id       TEXT NOT NULL,
    system_name     TEXT NOT NULL,
    first_seen_utc  TEXT NOT NULL,
    last_updated_utc TEXT NOT NULL
);
```

#### Fact Tables

```sql
-- Individual orders (main fact table)
CREATE TABLE market_orders (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    station_id      TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    side            TEXT NOT NULL,  -- 'buy' or 'sell'
    price_each      REAL NOT NULL,
    quantity        REAL NOT NULL,
    my_quantity     REAL DEFAULT 0,
    source          TEXT,
    captured_at     TEXT NOT NULL,  -- ISO timestamp
    bucket_utc      TEXT NOT NULL,  -- Truncated to hour for aggregation
    FOREIGN KEY (station_id) REFERENCES stations(station_id),
    FOREIGN KEY (item_id) REFERENCES items(item_id)
);
CREATE INDEX idx_orders_station_item ON market_orders(station_id, item_id, bucket_utc);
CREATE INDEX idx_orders_item_time ON market_orders(item_id, captured_at);
CREATE INDEX idx_orders_bucket ON market_orders(bucket_utc);

-- Hourly OHLCV aggregates (pre-computed for analysis)
CREATE TABLE market_ohlcv (
    station_id      TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    side            TEXT NOT NULL,
    bucket_utc      TEXT NOT NULL,
    open_price      REAL NOT NULL,
    high_price      REAL NOT NULL,
    low_price       REAL NOT NULL,
    close_price     REAL NOT NULL,
    volume          REAL NOT NULL,
    trade_count     INTEGER NOT NULL,
    vwap            REAL NOT NULL,  -- Volume-weighted average price
    FOREIGN KEY (station_id) REFERENCES stations(station_id),
    FOREIGN KEY (item_id) REFERENCES items(item_id),
    PRIMARY KEY (station_id, item_id, side, bucket_utc)
);
```

#### Arbitrage Opportunities

```sql
CREATE TABLE arbitrage_opportunities (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    from_station_id     TEXT NOT NULL,
    to_station_id       TEXT NOT NULL,
    item_id             TEXT NOT NULL,
    action_type         TEXT NOT NULL,  -- 'buy_then_sell' or 'sell_then_buy'

    -- Opportunity details
    buy_price           REAL NOT NULL,
    sell_price          REAL NOT NULL,
    quantity            REAL NOT NULL,
    gross_profit        REAL NOT NULL,

    -- Logistics estimates
    fuel_cost           REAL NOT NULL,
    travel_ticks        INTEGER NOT NULL,
    cargo_required      REAL NOT NULL,
    risk_score          REAL DEFAULT 0,

    -- Agent coordination
    claimed_by          TEXT,  -- agent_id who claimed this
    claimed_at          TEXT,
    status              TEXT DEFAULT 'available',  -- 'available', 'claimed', 'completed', 'expired'
    expires_at          TEXT NOT NULL,

    -- Metadata
    discovered_at       TEXT NOT NULL,
    discovered_by       TEXT NOT NULL,  -- 'arbitrage_scanner' or agent_id
    notes               TEXT,

    FOREIGN KEY (from_station_id) REFERENCES stations(station_id),
    FOREIGN KEY (to_station_id) REFERENCES stations(station_id),
    FOREIGN KEY (item_id) REFERENCES items(item_id)
);
CREATE INDEX idx_arbitrage_status ON arbitrage_opportunities(status, expires_at);
CREATE INDEX idx_arbitrage_item ON arbitrage_opportunities(item_id, status);
```

---

## Data Collection Layer

### Package: `pkg/market/`

#### MarketCollector

```go
// Collects market data from view_market responses and writes to market DB
type MarketCollector struct {
    db        *sql.DB
    stationID string
    systemID  string
    log       *log.Logger
}

// CollectSnapshot processes a ViewMarketResponse and stores orders
func (mc *MarketCollector) CollectSnapshot(ctx context.Context, resp *serverapi.ViewMarketResponse) error
```

**Process:**
1. Begin transaction
2. Ensure station exists in `stations` table
3. For each `ViewMarketItem`:
   - Ensure item exists in `items` table
   - Insert all `buy_orders` rows
   - Insert all `sell_orders` rows
4. Compute OHLCV for this station/item/side/bucket
5. Commit transaction

#### Write Contention Handling

- **Batched writes** — Single transaction per snapshot (all orders for one station)
- **Retry logic** — 5 attempts with exponential backoff (50ms → 800ms)
- **Configurable timeout** — Via context
- **Optional staggering** — Agent wake times can be offset in scheduler config

### Scheduler Integration

**Option 1:** Extend `play_as` scheduled system to support `action: update_market`

**Option 2:** Add to agent `Decide()` loop with last-capture tracking

---

## Webapp: Market Matrix

### Package: `cmd/market-dashboard/`

Lightweight HTTP server with vanilla JS frontend (no build step).

### API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /matrix` | Latest items × stations snapshot |
| `GET /item/{id}/history` | Price history for an item |
| `GET /station/{id}/orders` | Current order book for a station |
| `GET /opportunities` | Active arbitrage opportunities |
| `WebSocket /ws` | Real-time updates (optional) |

### Matrix Response Format

```json
{
  "timestamp": "2026-06-19T12:00:00Z",
  "items": [
    {
      "item_id": "iron",
      "item_name": "Iron Ore",
      "category": "raw_material"
    }
  ],
  "stations": [
    {
      "station_id": "station_alpha",
      "station_name": "Alpha Station",
      "system_id": "sol",
      "system_name": "Sol"
    }
  ],
  "cells": [
    {
      "item_id": "iron",
      "station_id": "station_alpha",
      "buy": {
        "best_price": 5000,
        "vwap": 4800,
        "volume": 15000
      },
      "sell": {
        "best_price": 5200,
        "vwap": 5250,
        "volume": 8000
      }
    }
  ]
}
```

### UI Features

- **Pagination:** 50 items per page (~10 pages for 500 items)
- **Filtering:** By category (All, Raw Materials, Manufactured, Ships, Modules)
- **Search:** By item name
- **Cell details:** Click to show full order book modal

---

## Arbitrage Scanner

### Package: `cmd/arbitrage-scanner/`

On-demand binary that analyzes the market DB and writes opportunities.

### Usage

```bash
# Scan for opportunities across all stations
./arbitrage-scanner --db /path/to/market.db --min-profit 10000

# Scan specific items or routes
./arbitrage-scanner --db /path/to/market.db --items iron,copper --systems sol,sirius

# Set opportunity expiration
./arbitrage-scanner --db /path/to/market.db --expires 6h
```

### Detection Algorithm

1. Fetch latest snapshot (grouped by station/item)
2. For each item, find best buy and best sell across stations
3. Calculate route metrics:
   - `gross_profit = (sell_price - buy_price) * quantity`
   - `fuel_cost = estimate from distance`
   - `net_profit = gross_profit - fuel_cost - taxes`
4. Filter by `minProfit` and `maxJumps`
5. Write to `arbitrage_opportunities` table

### Opportunity Structure

```go
type Opportunity struct {
    FromStation    string
    ToStation      string
    ItemID         string
    BuyPrice       float64
    SellPrice      float64
    Quantity       float64
    GrossProfit    float64
    NetProfit      float64
    ProfitPerTick  float64  // For comparison
    RiskScore      float64  // 0-1 based on route danger
}
```

### Claiming API

```sql
-- Agent claims an opportunity
UPDATE arbitrage_opportunities
SET status = 'claimed', claimed_by = ?, claimed_at = ?
WHERE id = ? AND status = 'available';

-- Agent completes (or fails) an opportunity
UPDATE arbitrage_opportunities
SET status = 'completed'  -- or 'failed'
WHERE id = ? AND claimed_by = ?;
```

---

## Implementation Phases

### Phase 1: MVP — Data Collection
- [ ] Create `pkg/market/` with schema and `MarketCollector`
- [ ] Extend `play_as` scheduled system to call `update_market`
- [ ] 40 station agents begin hourly collection
- [ ] Verify data accumulation via SQLite queries

### Phase 2: Visualization
- [ ] Build `cmd/market-dashboard/` with matrix view
- [ ] API: `/matrix` endpoint
- [ ] Basic pagination and filtering
- [ ] Verify real-time updates

### Phase 3: Analysis
- [ ] Implement hourly OHLCV aggregation in collector
- [ ] Build basic time-series queries
- [ ] Manual SQL exploration for patterns

### Phase 4: Arbitrage Scanner
- [ ] `cmd/arbitrage-scanner/` binary
- [ ] Basic cross-station arbitrage detection
- [ ] Opportunity table + claiming mechanism
- [ ] Manual testing with real agents

### Phase 5: Agent Integration (future)
- [ ] Agents query `arbitrage_opportunities` table
- [ ] Evaluate and claim opportunities
- [ ] Execute trades and report results

### Phase 6: Advanced Analysis (future)
- [ ] Correlation analysis (cross-station price movement)
- [ ] Volatility/clustering (player manipulation detection)
- [ ] Seasonal pattern detection (empire/station market cycles)

---

## Key Decisions Summary

| Aspect | Choice |
|--------|--------|
| DB location | `/data/market.db` (centralized) |
| Schema | Normalized (items, stations, market_orders, OHLCV) |
| Data granularity | Individual orders + hourly OHLCV |
| Collection frequency | Hourly (configurable) |
| Write contention | Batched single-transaction + retry |
| Webapp | Lightweight Go + vanilla JS |
| Matrix display | Best price + VWAP, 50 items/page |
| Arbitrage scanner | Separate binary, on-demand |
| Opportunity lifespan | 6 hours (configurable) |
| Agent participation | Passive collectors first (Phase 1) |

---

## Analysis Patterns Supported

The schema supports the following analysis types:

1. **Price Trend Detection** — Linear regression on OHLCV close prices over time
2. **Volatility Analysis** — Standard deviation, z-scores, outlier detection on price changes
3. **Cross-Station Correlation** — Correlation matrices for price co-movement
4. **Seasonal Patterns** — Cyclical analysis by time of day, day of week
5. **Manipulation Detection** — Identify abnormal price/volume spikes
6. **Arbitrage Discovery** — Cross-station profit opportunity detection

---

## Notes

- Basement orders (e.g., 250,000 @ 1cr) are preserved in raw order data but filtered from VWAP calculations for meaningful market prices
- Best price + VWAP comparison helps identify genuine market opportunities vs distorted averages
- Write contention handled via batching and retry; optional staggering available in scheduler
- Future: Can move from hourly to 10-minute intervals if needed for higher resolution
