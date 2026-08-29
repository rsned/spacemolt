---
name: project_arbitrage_opportunity_detail_view
description: "FUTURE — make an arbitrage opportunity clickable in the dashboard and show buy/sell pricing, depth, opportunity age; plus fix the misleading \"buying up to X of Y\" label."
metadata: 
  node_type: memory
  type: project
  originSessionId: 8c9098dc-7c7c-4768-bb10-b65a2ce84115
  modified: 2026-07-20T15:19:03.221Z
---

Requested 2026-07-20 while the shipping Sub-project B branch was in flight. NOT started.

**Ask:** make the arbitrage instance clickable in the dashboard, opening a detail view for that
opportunity: buy price / sell price, depth on both sides, opportunity age, and similar.

**Bundled bug found the same day — the activity label misreads supply as demand.**
`haulActivityLabel` (`pkg/worker/haul.go:562`) formats
`"Opportunity #%d · buying up to %.0f of %.0f %s · %s → %s"` where the second number is
`opp.SourceUnits` — documented at `pkg/market/types.go:76` as *"book's source best-ask depth
(src.AskQty)"*, i.e. units FOR SALE at the source. Placed right after "buying up to N of", it
reads as if someone is buying 1,032,117 Frontier Rotgut. Minimal fix: `"... of %.0f available"`.

**Six-figure depth is normal, not a glitch** — verified live in market.db: `iron_ore@war_citadel
sell 630,338`; `platinum_ore@grand_exchange sell 375,939`; `liquid_hydrogen@war_citadel buy
216,032`. NPC hub stations carry effectively unlimited stock, and items tend to show their
largest depth at their namesake production hub (frontier_rotgut @ frontier_station). So a huge
`SourceUnits` is never by itself evidence of a data problem.

**How to apply:** the detail view is a natural home for the depth-vs-demand distinction that the
one-line label cannot carry — show both sides of the book rather than one number. Related:
[[project_fleet_efficiency_dash]] (the :8087 panel this would extend),
[[reference_market_ohlcv_orderbook]] (order-book, not trades — use best-ask, not vwap),
[[reference_haul_fleet_capacity_ceiling]].
