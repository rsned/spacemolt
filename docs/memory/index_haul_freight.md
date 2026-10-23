---
name: index-haul-freight
description: Haul & freight: revenue, book depth, fleet capacity, market.db index, scanner, freight contracts, probation, load-confirm, arbitrage
metadata:
  type: reference
---

# Haul & freight economics

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐🔴 [Haul revenue halved](project_haul_revenue_halved_v0547.md) — v0.547.1 moved the fat tier to `trade_authenticator`; real loss is ALLOCATION; `MinProfit: 1000` is the lever
- ⭐🔴 [Book depth is the real haul ceiling](reference_book_depth_is_the_real_haul_ceiling.md) — `bookCap=ceil(srcUnits/cargoCap)`: a BIGGER hull gets FEWER slots
- [Fleet capacity ceiling](reference_haul_fleet_capacity_ceiling.md) — DON'T add haulers; 21 saturate the fat tier, realized = 34.4% of predicted
- ⭐🔴 [market.db index CORRUPT — RECURRED 10-22 ~15:00](project_market_db_corrupt_index_prune_dead.md) — 09-15 `idx_orders_station_item`, 10-22 `idx_orders_item_time`; malformed → 254 failed prunes, 40GB file, scans 2min→22min, haul claims 651→111/day. Fix = REINDEX or market-rebuild, writers DOWN
- [Scanner outage + expiry fix](project_scanner_outage_expiry_fix.md) — scanner is UNSUPERVISED; its death mimics "no opportunities". Check it first
- [Earnings-per-jump dashboard](project_fleet_efficiency_dash.md) — SP1 LIVE `:8087`
- [Stranded-recovery POIs bug](project_haul_stranded_recovery_pois_bug.md) — FindNearest never populates NearestResult.POIs, so the recovery branch is dead
- [Book-coordination follow-ups](project_haul_book_coordination_followups.md) — two deferred items after the 07-18 merge
- [Haul idle-trap gate](project_haul_idle_trap_gate.md) — `52b2cfa`; constraints 1-3 unbuilt
- [Haul departs without enough fuel](project_haul_departs_without_enough_fuel.md) — autopilot said "Need N more" and left anyway, 11+ stranded; FIXED `4b84ac1b`
- [Haul rolling drain on completion](project_haul_rolling_drain_on_completion.md) — FUTURE: zero-abort rolling upgrades
- [Haul P&L adjustments](project_haul_pnl_adjustments.md) — deferred non-trading reconciliation
- [Arbitrage net-of-fuel](project_arbitrage_net_of_fuel.md) — A+B merged 07-15; C (fuel-arbitrage chains) open
- ⭐ [Unpriced freight is prime-carrier-only](reference_freight_unpriced_cargo_prime_gate.md) — accept lands the package in STORAGE AT ORIGIN, not your hold
- [Freight orphan salvage](reference_freight_orphan_salvage_unpack.md) — a DEFAULTED contract's sealed package becomes loot; playbook
- [No active-contracts listing](reference_shipping_no_active_contracts_listing.md) — no shipping equivalent of get_active_missions; track locally
- [Freight load-confirm regression](project_freight_load_confirm_regression.md) — v0.2.1 check is a false negative: 0 deliveries fleetwide from 07-24
- [Freight load-confirm + resume fixes](project_freight_load_confirm_and_resume.md) — MERGED `6e211cc` 07-24, not pushed/deployed
- [Freight withdraw silent failure](project_freight_withdraw_silent_failure.md) — 52 events/18 agents; the withdraw SUCCEEDS, read the request_id-correlated action_result
- [Freight returns only at origin](project_freight_wrong_origin_return.md) — wrong_origin retried forever, breached fighter-1; fixed `3f010dd`
- [Freight probation bootstrap](project_freight_probation_bootstrap.md) — take a loss on first deliveries to escape probationary → 4x tiers; SHIPPED
- [Freight probationary cargo fence](project_freight_probationary_cargo_fence.md) — senior carriers skip probationary-band cargo; DEPLOYED
- [Shipping carrier feature](project_shipping_carrier.md) — A+B+C all MERGED; C `fcc1fda` 07-22
- [Marketbot freight-demand scan](project_marketbot_freight_demand_scan.md) — ~40 parked marketbots as a demand sensor net; FEATURE
- [Empire treasury payout collapse](project_empire_treasury_payout_collapse.md) — mission credits ~37% of advertised from 07-23, dev-confirmed treasury dry; XP still full
