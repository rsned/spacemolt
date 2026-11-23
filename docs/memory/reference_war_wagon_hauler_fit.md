---
name: reference_war_wagon_hauler_fit
description: "war_wagon (crimson T2, piloting 10) hauler fit: strip defaults, 7x cargo_expander_iii + expanded_fuel_tank = 1500 cargo / 340 fuel / CPU 22 of 22; catalog 1200 cargo already includes 8 CX II"
metadata:
  type: reference
---

2026-11-22. war_wagon catalog stats include its default modules (8 cargo_expander_ii, 2 armor_plate_ii): bare hull = 800 cargo, 240 fuel, 8 utility slots, 22 CPU, 50 power. Operator confirmed ALL modules can be removed.

Hauler fit: 7x cargo_expander_iii (+100 each, cpu 3/power 3) + expanded_fuel_tank (+100 fuel, -20 hull, cpu 1/power 2) = **1,500 cargo, 340 fuel, CPU 22/22**, power 23/50. Dropping the armor plates also removes -1 speed each.
Alternative: 7x CX III + cargo_expander_i = 1,520 cargo / 240 fuel.
vs congregation (T1, 1,900 cargo, 110 fuel, 0 utility slots).

Watched big-hold hulls (operator list): bonanza_king, enterprise, shell_company, syndicate, war_wagon, conglomerate, frankenhauler, junk_convoy, bulk_discount, dimensional_fold (operator calls it "dimensional_hold"). 11-22 listings: war_wagon 319,236 from station managers at Ironhearth, The Rampart, The Crucible; junk_convoy 175k/186k at Ironlight Crossroads (lhs_1140, player ILC Fettle). KB table ship_listings has the capture.

Related: [[reference_catalog_ship_stats_include_default_modules]], [[reference_customs_mechanics]]

**Two trucker tiers (operator 11-22):** congregation for routes under ~18 hops; "galaxy truckers" for full range. junk_convoy (outerrim T2, piloting 10) is the galaxy hull: comes WITH 5x CX III + CX II + armor_plate_ii + autocannon_i, 850 cargo (900 with CX II -> CX III), 720 fuel, speed 2 (about 3 without the armor). Cheaper and longer-range than war_wagon (319k, 340 fuel after refit).
**Bought 11-22** by engineer-1 (its own 4.16M credits; no treasury) at Ironlight Crossroads (lhs_1140, 17 jumps from CJ), seller player ILC Fettle: f53c347a3d0f4c84daf583ba06484095 (186k, engineer-1 now flying it) and 213380fbfea4d4167ce35bae32031dd7 (175k, stored at Ironlight with engineer-1's old ship 335d5ad7). The stored one needs a pilot sent to collect it.
**Operator 11-22:** when the trucker-110..129 pirate unlocks finish, send the first one to Ironlight Crossroads (lhs_1140) to `switch_ship` into the stored junk_convoy 213380fb; it becomes the first dedicated galaxy trucker. The other finishers board the 10 congregations held in the CJ garage.
**Fuel measured 11-22:** engineer-1 junk_convoy (1,540 cargo, seller refit) burns **4 fuel per jump** (route planner: "4 per jump, ~68 total, 720 available"; measured 720 -> 676 over 11 jumps) = ~180 jumps per tank. Its previous hull burned 1/jump on 94 fuel.
