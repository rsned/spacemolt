# Market Intelligence Package

`pkg/market` is a standalone SQLite store for game market data, separate from the
main knowledge base. It preserves individual orders (not just aggregates) so the
data can support deep analysis, cross-station arbitrage detection, and
time-series pattern discovery.

- **Database:** `/home/robert/spacemolt/spacemolt/data/market.db` (overridable via `market.Config`)
- **Schema:** normalized — `items`, `stations`, `market_orders` (raw orders),
  `market_ohlcv` (hourly OHLC+VWAP aggregates), `arbitrage_opportunities`
- **Collection:** station agents run `update_market` (manually or via
  `schedule_add hourly update_market` in `play_as`); each call upserts the
  station + items and inserts every buy/sell order in one atomic transaction,
  then computes the hourly OHLCV bucket.

## Verifying data collection

After deploying to station agents:

1. **Stats tool:**
   ```bash
   go run ./cmd/tools/market-stats
   ```

2. **Query directly:**
   ```bash
   sqlite3 data/market.db "SELECT COUNT(*) FROM market_orders"
   sqlite3 data/market.db "SELECT station_id, COUNT(*) FROM market_orders GROUP BY station_id"
   ```

3. **Check hourly buckets:**
   ```bash
   sqlite3 data/market.db "SELECT bucket_utc, COUNT(*) FROM market_orders GROUP BY bucket_utc ORDER BY bucket_utc DESC LIMIT 10"
   ```

4. **Item time series (OHLCV):**
   ```bash
   sqlite3 data/market.db "SELECT bucket_utc, close_price, vwap, volume FROM market_ohlcv WHERE item_id='iron_ore' AND side='buy' ORDER BY bucket_utc DESC LIMIT 24"
   ```

## Package layout

| File | Responsibility |
|------|----------------|
| `types.go` | Core types: `Item`, `Station`, `Order`, `OHLCV`, `MarketSnapshot`, `ArbitrageOpportunity` |
| `schema.sql` / `migrations.go` | Embedded normalized schema + idempotent `runMigrations` |
| `collector.go` | `Collector` (Open/Close), atomic `WriteSnapshot`, OHLCV aggregation, write-retry for SQLITE_BUSY |
| `capture.go` | `parseViewMarket` + `CaptureFromClient` bridging the game client to the collector |
| `query.go` | Read helpers: `GetStats`, `GetLatestOrders` |
| `integration_test.go` | Build-tagged manual verification notes |
