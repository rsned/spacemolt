// Package market provides a separate SQLite database for collecting and analyzing
// game market data across all stations. Unlike the main knowledge base's
// market_demand_history (which stores aggregated hourly samples), this system
// preserves individual orders for deep analysis, cross-station arbitrage
// detection, and time-series pattern discovery.
//
// Database location: /home/robert/spacemolt/spacemolt/data/market.db
//
// The schema is normalized with dimension tables (items, stations) and fact
// tables (market_orders, market_ohlcv, arbitrage_opportunities).
package market
