---
name: reference_market_orders_duplicate_books
description: "market_orders stored the same book 2-9x under one captured_at, inflating arbitrage book depth; fixed 2026-08-12 but EVERY fleet writing market.db must be rolled"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-12T18:25:52.495Z
---

`market_orders` had no uniqueness guard and `captured_at` is RFC3339 (**second**
resolution), so when several marketbots docked at one station and captured it in
the same second, each wrote a **complete copy of the book** under one timestamp.
Live on 2026-08-12: Frontier Station (`mobile_capital`) at 8-9x, Central Nexus
2x, plus grand_exchange and unknown_edge_waystation.

**Not cosmetic.** `GetItemStationPrices` does `p.AskQty += qty` over every row at
the station's latest capture (`pkg/market/arbitrage.go:59`), so copies became
phantom depth → `source_units` → `bookCap()` → the number of haulers a book is
believed to supply. An 8x-duplicated book issues up to 8x the hauler slots it
can fill. It also inflated raw station-manager sell value from 8.37B to 21.72B.

**Fix (`08fb75be`)**: `WriteSnapshot` DELETEs rows for `(station_id,
captured_at)` before inserting — idempotent per station-capture. Deliberately
NOT a UNIQUE index: two real orders can tie on price and quantity within one
book, and an index would silently eat one and under-report depth. An order-less
snapshot never replaces anything.

**⭐ The trap: a partial fleet roll leaves duplication running.** The collector
lives in the worker, so only rolled fleets stop duplicating. Frontier Station was
verified clean at 18:15Z, 8x-dirty at 18:23Z, and still 7x at 18:32Z after five
fleets had been rolled.

**EVERY fleet must be rolled — no exceptions.** `--market-db-path` defaults to
`data/market.db` **in the worker itself** (`cmd/worker/main.go:81`), so every
worker captures regardless of what its overmind passes. Reading the overmind
cmdline to decide which fleets matter is WRONG and cost two wasted verification
rounds here: `haul` and `assist` show no `--market-db-path` and were duplicating
anyway. haul workers frequent `mobile_capital`, the worst offender.

**Verify a roll by worker PROCESS START TIME vs `bin/worker` mtime**, per
[[reference_deploy_verification]] — a fleet can also be a MIX, since watchdog
restarts silently pick up the new binary (3 of haul's 21 had).

**Detection** — whole-book multiplicity, safe against genuine ties:
```sql
SELECT COUNT(*), COUNT(DISTINCT item_id||side||price_each||quantity||source)
FROM market_orders WHERE station_id=? AND captured_at=(SELECT MAX(captured_at) ...);
```
Equal = clean. A GCD>1 across per-row multiplicities means an N-fold copy.

Historical dups age out with the ~3.5h retention. Do NOT bulk-DELETE them:
market.db is heavily contended and even read queries time out under fleet load
([[reference_market_db_prune]], [[feedback_bulk_delete_scale]]).

Related: [[reference_market_ohlcv_orderbook]] (OHLCV is upserted, so it was never
affected) · [[reference_haul_fleet_capacity_ceiling]] · [[reference_book_depth_is_the_real_haul_ceiling]]
