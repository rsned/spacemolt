---
name: index-market-economy
description: Market & economy: fuel prices/reserves, arbitrage traps, market_orders/ohlcv, GetReferenceAsk perf, storage snapshots, customs, tax, find_item, demand ledger
metadata:
  type: reference
---

# Market & economy

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐🟢 [Station fuel-reserve capture](project_station_fuel_reserve_capture.md) — SHIPPED 08-15; 6 of 9 strongholds run DRY desks; faction bunkers member-only
- [Fuel price spread = 13×](reference_station_fuel_price_spread.md) — sol_central is the DEAREST; name the station or cr/jump is meaningless
- [Buy fuel at the cheaper end](project_refuel_timing_endpoint_choice.md) — `bc2adfe`, HAUL ONLY
- ⭐🔴 [Unbuyable modules poison the arbitrage board](reference_unbuyable_module_arbitrage_trap.md) — block at GENERATION; FIXED `443c0604`
- [market_orders stored duplicate books](reference_market_orders_duplicate_books.md) — phantom depth; FIXED `08fb75be`, a PARTIAL roll leaves it running
- [market_ohlcv = order-book data](reference_market_ohlcv_orderbook.md) — not trades; 999999 sentinel fix
- [GetReferenceAsk perf](reference_getreferenceask_perf.md) — 3.3s/call on 190M rows; needs the (item,side,station,captured_at) index
- [storage_snapshots is upserted](reference_storage_snapshots_shape.md) — UNIQUE agent+base; quantities REAL
- [empire field semantics](reference_empire_field_semantics.md) — get_system=REGION vs get_map=OWNERSHIP
- [craftsman-1 vacuum bids](reference_craftsman1_vacuum_bid_economics.md) — INTENTIONAL, never cancel
- [TRADING missions NOT market-validated](reference_trading_missions_not_market_validated.md) — reward and qty ignore the market
- [bill_of_materials is LOSSY](reference_bom_table_lossy.md) — ceils fractions; drops has_alternatives
- [Item category = sourcing rule](reference_item_category_sourcing.md) — ONLY `category='ore'` is mineable
- [Customs mechanics](reference_customs_mechanics.md) — scans only pilots who STOP 10 ticks at a border; keep moving
- [Empire tax day](reference_empire_tax_day.md) — weekly; get_tax_estimate shows amount + reason
- [Pirate base registry](reference_pirate_base_registry.md) — ids + systems; stale, in knowledge.db only
- [Market intelligence pkg](project_market_intelligence.md) — pkg/market, single source of volatile data; merged 06-21/22
- [Demand ledger](project_demand_ledger.md) — Station Manager buy orders, fulfill-now + craftable matching
- [find_item command](project_find_item_command.md) — BUILT `e1c73ff`; ranks stations by hops then price
- [price command depth-walk](project_price_command_depthwalk.md) — single-best-ask today; revisit
- [Arbitrage opportunity detail view](project_arbitrage_opportunity_detail_view.md) — FUTURE dashboard drill-down
- [Citizen demand brainstorm](project_citizen_demand_brainstorm.md) — PARKED 08-14: ~25M population, ~30B infrastructure
- [Passenger demand intel](project_passenger_demand_intel.md) — marketbots survey list_station_passengers into a KB table
- [Idle mining marketbots](project_idle_mining_marketbots.md) — short idle-mining loop between scans; canary viable
