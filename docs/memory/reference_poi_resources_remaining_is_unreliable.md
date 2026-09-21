---
name: reference_poi_resources_remaining_is_unreliable
description: "poi_resources.remaining=0 does NOT mean a POI is mined out — ore regenerates slowly and capital-system belts sit near 0 from over-mining while still yielding; rank by richness"
metadata:
  type: reference
---

**`poi_resources.remaining = 0` is not a write-off.** Never route away from a
POI on the strength of it.

**Ore regenerates slowly** (user, 2026-09-20). A deposit at 0 is being drawn
down faster than it refills, not exhausted forever — the trickle still yields.
**Capital systems are the most over-mined**, so a 0 on a capital belt is the
expected steady state of a popular field, and says more about traffic than
about the deposit. Conversely a 0 is a real signal *about competition*: it is
the one place the table does mean something.

**Proof (2026-09-08).** `commerce_fields`, the Haven belt marketbot_haven's
drones have been working continuously, reads `remaining = 0.0` on all five of
its resources. Its storage at that moment held exactly those five — 694
`trade_crystal`, 235 `copper_ore`, 193 `iron_ore`, 96 `silicon_ore`, 72
`nickel_ore`. The belt is demonstrably producing while the table calls it empty.
A live `get_poi` on 2026-09-20 confirmed it server-side: `"remaining": 0` beside
`"max_remaining": 100000`.

Only **97 of 2,430** rows read 0, so a zero looks like a meaningful signal
rather than a gap, which is exactly what makes it dangerous. It nearly rerouted
two marketbots off `ironhearth_fields` (0 across all five ores) onto ice.

**Use `richness` to rank.** It is populated everywhere and behaves sensibly —
`the_old_seam` iron_ore 85 against `ironhearth_fields` 42/35/34/30/9 correctly
ranks the single rich vein above the diverse-but-thinner belt. Prefer belts
*outside* capital systems when richness ties.

**`max_remaining` was 0 on every row — FIXED 2026-09-20 (`847d47d2`).**
`worker.GetPOI` built `game.POIResource` without copying `max_remaining`, so
`mergePOIDetail`'s capacity map was always empty. Only `get_poi` reports
capacity; the KB upsert was already correct (it keeps an established capacity
when an incoming `get_location` row carries none), so rows refill on the next
`update_poi` at each POI with no backfill needed. Same family as
[[reference_capture_loss_taxonomy]]: a column that has never held real data
looks identical to one reporting a real zero.
