---
name: project_haul_revenue_halved_v0547
description: "2026-07-26 haul revenue halved (1.4M/hr -> 797k/hr): server patch v0.547.1 reshaped the economy and MinProfit=1000 lets scraps soak up hauler capacity"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2f3b8937-e63d-42aa-8015-c67d52bc5fd2
  modified: 2026-07-26T23:17:43.577Z
---

**Symptom (2026-07-26):** haul/hr fell ~1.4M → 796,771 over ~a day. Run COUNT held (10–25/hr); **profit per run collapsed** 60–90k → 20–27k. Opportunity pool: avg gross 52k → 6–8k, fat (>200k) 91–102/hr → **0–3/hr**. Sharp onset **2026-07-25T12:00Z**.

**Eliminated with evidence (do not re-litigate):**
- Scanner down — NO (2,556 scans, inserting continuously; process up since 07-23, i.e. predates the cliff, so our code did not change).
- Marketbots not capturing — NO (millions of `market_orders` rows/hr across 33–34 stations, current).
- Prices moved — NO. A fixed 4-ore basket sat flat at 591 across the cliff. **A global `AVG(close_price)` appeared to rise 35% — that was an ITEM-MIX artifact. Always control the basket before believing an aggregate price move.**
- Our own orders shadowing top-of-book — NO (0% of top bids carry `my_quantity`).

**Root cause = server patch v0.547.1 (7/24/26 2:17 PM), operator-supplied:** Haven foundries no longer run dry → bearer-bond minting ~4×; Grand Exchange now buys `trade_crystal`/`trade_ciphers`/`copper_wiring` **much deeper**; long-stale `trade_authenticator` buy orders started clearing. The old fat routes drained and the pool refilled with scraps.

**The fat tier did NOT disappear — it MOVED.** Live 6h sample: `trade_authenticator` → alpha_centauri_colonial **max gross 2,417,402**, → sirius_observatory 1,286,532, → nova_terra_central 601,464.

**The real loss is ALLOCATION, not opportunity.** 24h by item:
| item | runs | avg/run |
|---|---|---|
| deuterium | **141** | **15,126** |
| fuel_cell | 152 | 110,182 |
| power_cell | 74 | 178,597 |
| trade_authenticator | 55 | 145,317 |
| circuit_board | 8 | 189,454 |

Deuterium consumed the largest run budget of any item at ~1/10th the value. **`MinProfit: 1000` (cmd/arbitrage-scanner/main.go:107) lets 1k-gross scraps into the pool, and a hauler near a scrap claims it instead of flying to a fat route** — against [[reference_haul_fleet_capacity_ceiling]]'s 220 cr/jump vs 3,057 finding. Raising the floor is the lever.

**Forensics limit:** `market_orders` retains only ~4h, so the 07-25 order book was already pruned — the direct evidence of the step was unrecoverable. Only `market_ohlcv` (~5 days) survives. If a market regression needs history, capture it fast. [[reference_market_db_prune]] [[reference_market_ohlcv_orderbook]]
