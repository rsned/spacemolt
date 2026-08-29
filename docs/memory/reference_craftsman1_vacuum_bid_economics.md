---
name: reference_craftsman1_vacuum_bid_economics
description: "craftsman-1's 8,868 lowball buy orders are an INTENTIONAL vacuum-bid strategy (never cancel); wallet is swept at login to fund order escrow (~85k floor) so gifts/large credits vanish — misread as a loss on 2026-07-22"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-23T06:01:20.290Z
---

**craftsman-1 (Arthur 'Artificer' Artis) runs an intentional vacuum-bid market strategy — do NOT cancel these orders or treat its vanishing wallet as a bug/theft.**

- ~8,868 open BUY orders across 17 stations (created 2026-06-28, mostly 2 cr × qty 101 lowballs), ~₡10M credits + ~26,674 items in escrow as of 2026-07-22. Other agents dump items into them for quick cash; the user collects below market and re-sells later.
- **Wallet sweep at login (unlogged):** the order book was never fully escrow-funded, so the server sweeps craftsman-1's liquid credits into escrow at session login, leaving a floor of ~₡85k. NO action_log event is emitted for the sweep.
- **CORRECTION (user, 2026-07-22): the ~₡1M credit drop observed on 2026-07-22 was a SERVER BUG — a genuine loss, NOT the escrow-sweep reconciliation I originally diagnosed.** Do not attribute that specific 1M swing to the login sweep. The escrow-sweep mechanic above is real and still explains the ~₡85k floor, but it was the wrong explanation for this particular 1M event.
- Facility rents are trivial (₡52/facility per ~17-min cycle, ~₡9k/day) — the ₡85k floor + mission income covers them indefinitely; big rent-buffer gifts are unnecessary.
- The heartbeat/client credits go STALE for escrow sweeps (client showed 1.31M while server had already swept) — server truth only via fresh login. Diagnosis path that worked: SIGSTOP worker → `bin/play_as craftsman-1` ≤60s (`get_action_log --page_size 100 --page N`, `view_orders`, `get_tax_estimate`) → SIGCONT; worker survives with one reconnect. NOTE: play_as spams "Error reading input: EOF" forever after stdin closes — always wrap in `timeout` + `head -c`.
- **Someday-project (user, 2026-07-22): consolidate craftsman-1's 2,649,736 stored items into one place — explicitly NOT now.** Exact holdings (user-verified 2026-07-22): 2,649,736 items in storage at 20 stations: cargo_lanes_freight_depot, central_nexus, confederacy_central_command, crix_stronghold_station, dross_citadel_station, frontier_station, gold_run_extraction_hub, grand_exchange_station, kael_arsenal_station, market_prime_exchange, mera_sanctum_station, nyx_nexus_station, sable_port_station, thane_keep_station, the_experiment_research_station, the_rampart_checkpoint, traders_rest_resort_station, treasure_cache_trading_post, unknown_edge_waystation, voss_redoubt_station. (Includes pirate strongholds — crix/kael/sable/voss — so consolidation routing will need the lawless-routing work.) Related passive idea offered: claim-filled-order-goods-when-docked worker hook (not queued).

Relates to [[project_crafting_brain]] [[reference_empire_tax_day]] (tax was exonerated in the incident — property tax due was only ₡6,700).
