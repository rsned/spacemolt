---
name: reference_market_ohlcv_orderbook
description: market_ohlcv is order-book snapshot data (not trades); 999999 sentinel contamination fix + GetReferenceAsk + recompute tool (2026-07-08)
metadata: 
  node_type: memory
  type: reference
  originSessionId: 9d76ccbd-ca44-4de8-904c-982c4f6dc972
---

**`market_ohlcv` is order-book aggregate data, NOT executed trades.** `view_market` exposes only resting orders — there is no trade feed. `computeOHLCV` (pkg/market/collector.go) aggregates one snapshot's resting ladder per (station,item,side,hour), so `volume`=resting qty, `trade_count`=order count, `vwap`=qty-weighted avg of the resting ladder. The names are aspirational. To infer real fills you'd have to diff snapshots over time (vanished sell order ≈ probable trade) — not built.

**The 999999 "not for sale" sentinel bug (fixed 2026-07-08):** the game stamps 999999 on placeholder rungs. Only `price<=0` was filtered, so the sentinel folded into vwap/volume/high — dragged iron_ore sell VWAP to 382 vs a ~2cr live floor. Fix (pkg/market/prices.go): `NotForSalePrice=999999.0` + `tradeablePrice()` gate in computeOHLCV + sentinel filters on GetMatrix/FindItemSellers/GetItemPriceHistory. Highest real price ever seen ~600k, so the cutoff is safe.

**New price signal — `GetReferenceAsk(ctx, itemID) (ReferenceAsk, bool, error)`:** the honest live floor = cheapest sentinel-filtered ask across stations' latest snapshots, plus Depth + Stations + AtAsk (so a lone-troll floor is visible — low-side is the mirror problem: 1-2cr troll asks on expensive items). Surfaced in `view-market prices` as a "Live floor ask" headline. Use this, not vwap, for "what does X cost". See [[project_market_intelligence]], [[project_price_command_depthwalk]].

**History was NOT fully recoverable:** raw `market_orders` retention is ~4h (prune daemon), but `market_ohlcv` history spans weeks — so `cmd/tools/market-recompute-ohlcv` (Collector.RecomputeContaminatedOHLCV) could only recompute 16 of 1867 contaminated rows from surviving raw orders; the rest were deleted (unrecoverable, and already hidden from readers by the query filters). Ran once on live market.db 2026-07-08: iron_ore 382.7→50.8, 0 contaminated rows left.

kb consumer `kb/cmd/generate-build-costs/load.go` already had a read-time `high_price < 999999` workaround; the producer fix now cleans the source so it's belt-and-suspenders.
