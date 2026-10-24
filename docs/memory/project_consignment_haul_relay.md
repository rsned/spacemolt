---
name: project_consignment_haul_relay
description: "Gift-sourced hauling: ConsignRule + consigned routes + (2026-10-22, UNCOMMITTED/UNDEPLOYED) the generator. BLOCKER: live arbitrage_opportunities CHECK rejects withdraw_then_sell — needs a table rebuild"
metadata:
  type: project
---

Gift-sourced hauling, started 2026-10-17. **Two halves shipped, the generator
is the remaining piece.**

## The mechanic that makes it work

`send_gift` lands in the RECIPIENT's storage at the **sending** station and
waits there for them. Only the sender must be docked where the goods are, so a
pinned collector can consign to a hauler days away — no rendezvous.
See [[reference_faction_capture_remote_vs_docked]].

## SHIPPED: send side — `ConsignRule` (`1b655b42`)

`pkg/worker/consign.go`, run from the standing **idle pass** (beside PayDebts
and Handoffs), not the schedule — schedule seeding is ADDITIVE ONLY so a
seeded command fires forever, while an idle-pass step is switched by the fleet
yaml and is reversible. Fleet yaml: `consign: ["item:recipient:min[:keep]"]`,
forwarded as `--consign`.

Explicit ALLOW-LIST, never patterns: gifting is irreversible and the drone
project's reserved energy crystals and a ship's fuel cells share that storage.
`keep` holds a reserve; `min` batches (min<1 rejected). Recipient is an AGENT
ID resolved via `UsernameFor` — ids are stable, usernames rot. Reads personal
storage at the docked station only, never cargo.

**Still useful and kept** even though claim-driven is better for path 1,
because path 2's destination agent IS fixed.

## SHIPPED: engine side — consigned routes (`08439d32`), INERT

`market.ActionWithdrawThenSell` + `ArbitrageOpportunity.IsConsigned()`.
haulGate routes past the ask gate; `sizeConsigned` caps by the hold alone, so
a BROKE hauler can run one. Load branches to `WithdrawItems` instead of `Buy`.
Everything downstream unchanged.

**Why not just buy_price = 0:** `sizeBuy` returns 0 for any ask <= 0 then
divides credits by it, and haulGate reports "no live ask at buy station" — a
reason the run treats as proof the source book is gone, so it calls
`abandonCollapsed` and expires the route AND its sibling destinations. A
synthetic zero-price row poisons the pool rather than failing.

A failed withdraw is `releaseBookAnd`, not `abandonCollapsed`, so the arrival
race costs a wasted pass and not the route.

## BUILT 2026-10-22 (uncommitted, undeployed): the generator

- Collector: fleet yaml `consign_offer: ["item[:keep]"]` -> `--consign-offer`
  (needs `--handoff-queue`). Idle pass, after Handoffs, upserts market.db
  `consign_stock` = held - keep - pending handoffs owed (`ReportConsignStock`).
  No storage reading -> reports NOTHING (never a false zero).
- Scanner: `ScanArbitrage` turns fresh (<2h) stock into withdraw_then_sell rows,
  appended AFTER the Limit cap; nets out consigned claims made after the last
  report. Holder+base ride in `notes` ("consign holder=X base=Y"), parsed by
  `market.ConsignSource` — the opp reads do not select discovered_by.
- Hauler: on claim, enqueue handoff `consign/<opp>/<agent>` for
  min(opp qty, hold/size); at the source, wait (15 x SleepLong) for it, then
  withdraw min(gate qty, moved, free/size). Failed gift -> abandonCollapsed.
- Stronghold guard already covers the SOURCE (`haul.go` FromSystemName), so
  only pirate-unlocked haulers claim Voss routes.

⭐🔴 **BLOCKER: the live `arbitrage_opportunities` has
`CHECK (action_type IN ('buy_then_sell','sell_then_buy'))`** (1.66M rows,
9.8 GB market.db). Any withdraw_then_sell insert fails and rolls back the
WHOLE scan. The code checks sqlite_master first and skips consignment on an
old table, so deploying before migrating is safe — but nothing flows until the
table is rebuilt (or its CHECK widened). schema.sql is fixed for fresh DBs.

## Path 2, not started: haul + re-gift relay

Agent K (System W) -> hauler H -> Agent D (System Z), for crafting logistics.
Needs a PERSISTED record (a hauler mid-relay must survive restart), must claim
the hull against arbitrage, and delivery is not confirmable from our side —
there is no shipping-style active-contracts listing and the post-gift state
read is unreliable. Likely home is the existing handoff queue
(`deps.Handoffs` / `HandoffPass`), which already has queue semantics for
"fulfill pending stock-handoff records" — but read what it carries before
overloading it.

## First real use case

marketbot_alhena is pinned at `voss_redoubt` with drones mining
adamantite_ore. Factory Belt is **5 jumps** from Alhena and bids **1,600**
(2x base 800) — the same price every buyer offers, so proximity is the only
discriminator. A 1,900-cargo congregation carries 950 units (size 2) =
~1.52M/run.

Related: [[project_haul_fleet_rehull_2026_10]] ·
[[reference_book_depth_is_the_real_haul_ceiling]]

**⭐ 2026-10-24 LIVE: the generator works end to end up to the route.** craftsman-10 (`consign_offer:
[pirate_moonshine]` in craft-fleet.yaml, SIGHUP relaunched the worker with `--consign-offer`) reported
`consign_stock` = 72 within two idle passes of the deposit. **But the live scanner produced nothing for 40 min: the
`arbitrage-scanner watch` process had been running since 2026-09-21 — a binary rebuilt on 10-22 does not replace a
running process.** One `bin/arbitrage-scanner scan --items pirate_moonshine` with the rebuilt binary wrote 6+
`withdraw_then_sell` rows (voss_redoubt → war_citadel 14 @6,654, the_core 14, the_anvil_arsenal 5, …; notes
`consign holder=craftsman-10 base=voss_redoubt_station`). Restarted the watcher with the COLD_START command
(docs/COLD_START.md:127). The `arbitrage_opportunities` CHECK already admits `withdraw_then_sell` (the 60bdcb32
rebuild ran). Hauler-claim half: see the status board for the result.

**Hauler-claim half, live 2026-10-24 03:15–03:52 PDT (route 1704948, 72 pirate_moonshine):** trader-8
(pirate-unlocked, smuggling 16) claimed voss_redoubt → grand_exchange @6,308; craftsman-10's handoff gifted the
72 to trader-8's storage at Voss in 26 s; trader-8 flew 5 jumps, withdrew, reached Grand Exchange — and stuck:
1. Grand Exchange's live book could not absorb 72, so the demand watchdog chose the COST ORDER, and a consigned
   route's cost basis is 0 → `invalid_payload: Price must be greater than 0` every pass. **FIXED** (haul.go, commit
   "consigned-route cost order lists at the planned sell price"): `unitBuy <= 0` falls back to `opp.SellPrice`.
2. Then the real gate: `contraband_restricted: Pirate Moonshine is contraband. This market (level 5) requires
   smuggling level 25` — the Empire rule is **smuggling ≥ 5 × market level**, and the error text carries the
   level, so the capture gap ("we store tier nowhere") can be closed from this reply. The haul loop does NOT treat
   `contraband_restricted` as goods-unsellable: it re-posted every ~18 s until the opp was completed by hand
   (`bin/arbitrage-scanner complete --id 1704948 --agent trader-8`). TODO (plan 1, sell-destination task): scanner
   must skip Empire destinations for contraband unless the holder's smuggling ≥ 5 × level; haul must treat
   `contraband_restricted` like `haulGoodsGone` (re-route/abandon, never loop).
3. The scanner priced a grand_exchange destination with NO grand_exchange bid in market_orders (6,308 matches
   thane_keep's bid) — source of that price unknown; investigate `consignCandidates` destination list.
Resolution: trader-8 removed from the haul roster (overrides), flown by play_as to treasure_cache_trading_post
(the one ungated Empire market), **ask posted 04:01 PDT: 72 pirate_moonshine @5,500, order `3667a747…`, listing fee
5,940 = 1.5% of face value** — the first fleet-posted contraband ask. Treasure Cache's own moonshine bids are 1-cr
junk walls only, so this ask is the shopkeeper experiment: watch `market_orders.my_quantity` for trader-8 at
treasure_cache to see fills. trader-8 returned to the haul roster afterwards.
