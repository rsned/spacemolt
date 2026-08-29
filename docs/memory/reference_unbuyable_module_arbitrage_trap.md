---
name: reference_unbuyable_module_arbitrage_trap
description: "Modules appear in market books but `buy` rejects their ids — the arbitrage scanner mints them forever because every scan rebuilds the whole pool"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T03:51:11.562Z
---

Modules (blank `category` in the items catalog — e.g. `reactive_armor_hardener`)
appear in market order books as ordinary listings: a player sell order at one
station, a station bid at another. The arbitrage scanner therefore prices them,
and because the spread is enormous (590 ask vs 17,996 bid) they rank at the TOP
of the board. But `buy` rejects the id outright:

    invalid_item: Unknown item 'reactive_armor_hardener'.

**Why it never self-corrected:** `ScanArbitrage` expires the ENTIRE available
pool and re-inserts from the books every cycle. Expiring the poison rows — or
releasing the claim, which is what the hauler did — puts the item straight back
on the board minutes later. One route survived **1,008 scan cycles**; 12,107
rows for that single item; 150 failed buys in one evening, 105 by trader-7
alone, and it wedged the explorer-1 canary in a 40-second retry loop.

**The fix (`443c0604`):** `unbuyable_items` table + `MarkUnbuyable`; blocked ids
filtered in `scanItemSet` (BOTH branches — an explicit `Items` allow-list is
filtered too); the hauler classifies `invalid_item` as permanent via
`isUnbuyableItemErr` and calls `abandonUnbuyable` instead of releasing.
`blocked_until` gives a 7-day TTL so a server-side fix costs one wasted buy to
discover rather than a permanent blind spot.

**Diagnostic:** `SELECT item_id, hits FROM unbuyable_items ORDER BY hits DESC`
in `data/market.db` names the items that cost the fleet the most.

**Generalise:** the local `tradeable` flag is useless as a discriminator (it
reads 1 for modules — see [[reference_catalog_items_tradeable_drift]]). The
server's own rejection is the only reliable signal. Blank `category` is a hint,
not proof.

Related: [[reference_haul_fleet_capacity_ceiling]] — a hauler stuck on a poison
row is a hauler not available when a fat opportunity appears, the same argument
that set `MinProfit`.
