---
name: reference_book_depth_is_the_real_haul_ceiling
description: "N 'opportunities' at one station are usually N destinations off ONE source book with shared depth, and bookCap=ceil(srcUnits/cargoCap) means a BIGGER hull gets FEWER claim slots on a thin book"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2cf71781-6ccf-44b8-b879-8971d8d06726
  modified: 2026-07-27T10:12:23.303Z
---

Verified live 2026-07-27 while diagnosing the voidborn idle trap ([[project_haul_idle_trap_gate]]).

## N opportunities != N runs

`arbitrage_opportunities` fans one source book out across many destinations. `source_units`
is *"the book's source best-ask depth (src.AskQty); **shared across a book's dest rows**"*
(`pkg/market/types.go:76`) — so a station showing 21 six-figure rows may hold enough supply
for exactly one hauler.

Live snapshot:

| station / item | rows | src_units | Σ cargo_required |
|---|---|---|---|
| nova_terra_central / fuel_cell | **21 available** | **56** | 827 |
| treasure_cache / circuit_board | 7 available | 10 | 70 |
| treasure_cache / trade_authenticator | 2 available | 4 | 8 |
| war_citadel / deuterium | 2 available | **13,454** | 24 |

Those 21 fuel_cell rows advertised 114k–220k gross each. They are 21 *destinations* off a
**56-unit** book. One worker absorbs the whole thing. **Never size fleet capacity by
counting available rows** — group by `(item_id, from_station_id)` and read `source_units`
once. This is the mechanism behind [[reference_haul_fleet_capacity_ceiling]]'s "21 haulers
saturate the fat tier."

Note the inversion in that table: the genuinely deep unallocated book is `war_citadel`
deuterium (13,454 units) — and deuterium is the low-value tier that
[[project_haul_revenue_halved_v0547]] records as having eaten 141 runs at ~15k. Deep supply
and high value are anti-correlated.

## 🔴 A bigger hull gets FEWER slots on a thin book

`bookCap(sourceUnits, cargoCap) = max(1, ceil(sourceUnits / cargoCap))` (`pkg/worker/haul.go:497`),
and that is the number of concurrent claims `AdmitBookClaim` will admit on the book.
Dividing by cargo capacity means capacity works *against* you when supply is thin:

| hull cargoCap | bookCap on a 56-unit book |
|---|---|
| 355 (mission hauler) | **1** |
| 90 | 1 |
| 25 (tier-1, e.g. `absence`) | **3** |
| 15 (tier-1, e.g. `cleaver`) | 4 |

So on 2026-07-27 nine idle haulers could not share the nova_terra fuel_cell book *because
their hulls were too big*: cap 1, already claimed by trader-8. Small hulls would have let
three of them work it concurrently.

This is the **second** independent argument for small hulls, converging with the fuel one
([[project_smuggling_enablement]], [[reference_station_fuel_price_spread]]): big hulls burn
8 fuel/jump instead of ~1 **and** monopolize thin books. The intuition that a hauler wants
maximum cargo only holds when books are deep, which the table above shows is the exception,
not the rule.

Caveat before acting: a small hull also caps the load size, so on a DEEP book it earns
proportionally less per run. The right move is probably a mixed fleet (a few large hulls
for deep books, many small ones for the fat-but-thin tier), not a wholesale migration.
Nothing has been changed on this basis yet.
