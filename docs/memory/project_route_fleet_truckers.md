---
name: project_route_fleet_truckers
description: "trucker-100..109 route fleet (11-20): new outerrim Cobbles, overmind route.sock / route-fleet.yaml; phase 1 gift unlock done (9/10 first pass), phase 2 = explicit station routes moving our stored stock to CJ"
metadata:
  type: project
---

**2026-11-20 operator:** trucker-100..109 are new outerrim accounts. Each starts docked at
Deep Range mobile_capital with 100 cr, no faction, and a starter Cobble (75 cargo).
They run in a separate overmind: `bin/overmind --socket data/overmind/route.sock --fleet
data/overmind/route-fleet.yaml ...`, logging to data/overmind/route-overmind.log.
Added to docs/COLD_START.md §3g on 2026-11-21 (operator OK); fleet-watch restarted to watch it. Role `idle`, plus an hourly `unlock_gifting` in each
schedule.json.

- Gift unlock: 9 of 10 completed at 20:55 (10 rare_salvage, about 20 cr). trucker-103's buy lost the
  race; its hourly retry finishes the active mission. After that, remove the unlock_gifting tasks.
- Plan (operator): 1–2 truckers on CJ <-> beid. The rest move OUR stored stock to CJ, carbon_ore first
  (CJ faction storage had 2). They stay in starter hulls until hauling hulls are found or built.
- The stock is in agents' PERSONAL storage across about 31 stations (1.23M carbon_ore; biggest piles:
  Treasure Cache 300k, Gold Run 161k, Ironhearth 156k). Only the OWNER can move it, and only while
  docked there: `send_gift source=storage` to the trucker, or `deposit_items source=storage
  target=faction`. A trucker alone can't pick it up. The proposed design: release step (owner) +
  haul step (trucker -> CJ faction storage).

Related: [[project_station_two_beid]], [[project_cloverfield_home_station]], [[reference_gift_unlock_and_officer_storage_split]]

**11-21 live:**
- All ten trucker accounts are in CRFT. They joined via the new `faction_accept_invite` verb.
- New verbs:
  - `release ITEM STATION RECIPIENT [KEEP]` (owner side, ten_minutely; no-op unless docked there).
  - `switch_hull CLASS`: own stored hull first, then this station's garage, via the list_ships `faction_garage`.
- Routes (hourly `deliver ... <station> <CJ base> faction`):
  - 101+102: Treasure Cache carbon.
  - 103: Gold Run carbon.
  - 104: Ironhearth carbon.
  - 105: Unknown Edge aluminum_ore.
  - 106: Nova Terra aluminum_ore.
  - 100: daily LH2 to beid (taken over from craftsman-2).
- Owners releasing:
  - prophet-1, miner-1 (NOT RUNNING), miner-4, and marketbot_treasure_cache keep 50k.
  - miner-6.
  - marketbot_015 keeps 50k.
  - prophet-2.
  - marketbot_the_telescope, marketbot_unknown_edge, marketbot_nova_terra (aluminum).
- Excluded owners: craftsman-1 (operator account) and marketbot_gold_run (consigns carbon to trader-9).
- Hulls: craft_boss commissioned 10 Congregations at CJ (1,900 cargo); 105, 102, 106, 104, 101, 103 boarded by 11-21 01:55; 6 more deposited 11-21 07:05; boarded 07:14–07:17: 100 4d4bdfee, 107 5c07d348, 109 7f711e29, 108 bb41d806. 2 spare in CJ garage. Finished hulls go into the CJ
  faction garage. trucker-105 and trucker-102 boarded the two garage hulls on 11-21.
- **Gotcha:** a running `deliver` blocks the scheduler. To switch a hull, prepend weekly one-offs
  `autopilot cloverfield <CJ poi>`, `dock`, `switch_hull congregation` and restart. A TERMed worker's
  dying scheduler can fire and stamp them, so if it did, re-add them under NEW ids (max over ALL tasks incl. the old one-offs + 1; reusing ids keeps the stamp). The running
  worker then picks them up with no restart.
- Aluminum: smelt_aluminum_sheet is facility-only. On operator approval, an Aluminum Smelter was placed at CJ 11-21
  (facility 231a4c88…). craftsman-3 runs hourly `craft_stock smelt_aluminum_sheet aluminum_sheet 1000`
  and `craft_stock basic_silicon_refinement flex_polymer 500`.

**11-21 07:05 second wave:**
- 107: Grand Exchange nickel_ore (marketbot_haven).
- 108: Central Nexus silicon_ore (marketbot_nexus_prime, salvager-5, trader-5, pirate-12).
- 109: Confederacy Central Command nickel_ore (salvager-3, random-3, craftsman-4, explorer-3).
- Why: flex_polymer inputs, since CJ had nickel at 11.7k and silicon at 20k. Carbon, aluminum, iron and copper at CJ were already healthy.
- trucker-100's LH2 deliver had reused id 7, which carried a stamped faction_accept_invite, so it never fired. It was re-added as id 11.
- Trucker in-game usernames are `u_mother_trucker_1NN`, not the agent id. Use these for play_as send_gift.
- the_banker sent 50k to 107–109.
- Unrouted piles worth a future route: trader-9 holds 78k nickel at Stellar Leap (tiaki) and 72k silicon at The Hush Chamber (diphda).
- **11-21:** the nine second-wave owners' `release` tasks all failed with "unsupported command". Their workers predated bin/worker 11-21 00:21. All were TERMed at 07:32 to pick up the new binary. Check worker start time before relying on a new verb.
- **Beid LH2 since 11-21 07:43** (operator's idea; members can't withdraw faction storage):
  1. miningbot_0000 (officer) runs daily `deliver liquid_hydrogen 1000 faction:<CJ> <CJ> trucker-100`. It withdraws and gifts the LH2 into trucker-100's storage at CJ.
  2. trucker-100 runs hourly `deliver liquid_hydrogen 1000 <CJ> <beid> faction` from its own storage. Members CAN faction-deposit.
  3. An empty-source run errors at CJ without flying.
- **trucker-110..129 (operator 11-21):** 20 more outerrim accounts in route-fleet.yaml.
  - By 08:00 all had the gift unlock (`unlock_gifting`) and had joined CRFT (`faction_accept_invite`).
  - They were then switched to `role: unlock, station: treasure_cache_trading_post` for the pirate chain (`an_introduction` takes baseline to 10).
  - Goal: haul deeper into lawless space and stronghold routes. Give them routes and hulls after unlock.
  - Check unlock: the `agent_standings` pirate_* baseline >= 10 on all 9 keys.
- **11-21 third Congregation wave (8 hulls).**
  - Pirate-unlocked truckers 113, 125, 126, 127 boarded 4 of them at CJ (13:53–14:09). Each went role idle in route-fleet.yaml, then a SIGHUP, then one-off `autopilot cloverfield`, `dock`, `switch_hull congregation`.
  - 4 hulls are still in the CJ garage for the next truckers to unlock.
  - **The agent_standings table (assets.db, keyed by player_id) lags.** Count `missions: completed an_introduction` in route-overmind.log instead.
  - The remaining 16 truckers had all done across_the_line by 11:31 and were waiting for an_introduction on the board.
  - Lawless candidate piles (personal storage):
    - The Veil Anchor (bd20_2457): 978k iron, 924k copper, 14 owners.
    - Purified_argon / ionized_neon / LH2, ~300k each, at Plasma Echo (gsc_0010), Echo Protocol Hub (lilii_borea), Carnegie Hall (winterhold).
    - Hex Star (dheneb) iron.
    - Zero Point Nexus (kornephoros) copper and iron.

**11-21 17:52 Veil Anchor route (operator: "energy crystal ... and the quantum_fragments also"):**
- Source: The Veil Anchor (base 59b10227…, bd20_2457). It's 23 jumps to CJ, all lawless. A round trip is about 1 h at 70 s/jump.
- Stock: about 110k energy_crystal (size 1) and 112k quantum_fragments (size 3). That's about 444k cargo, or about 235 Congregation loads.
- Haulers: trucker-113, 125, 126 and 127 each run hourly `deliver energy_crystal 200000 <veil> <CJ> faction` and the same for quantum_fragments.
- Owners (Squad A crystal miners) run ten_minutely `release` for both items to:
  - trucker-113: fighter-5, random-1, random-4
  - trucker-125: random-9, random-5, random-8
  - trucker-126: fighter-6, random-2, random-6
  - trucker-127: fighter-7, random-3, fighter-9
- Excluded: craftsman-1 (2.5k) and fighter-10 (Squad B, homes at cargo_lanes).
- 11 of these owners were still running workers started 11-19, which lack `release`, so they need a TERM. That TERM was blocked by the permission classifier and passed to the operator.
- Squad B's pile at Cargo Lanes (7 jumps): fighter-2 77.7k crystals (last_seen 11-09), fighter-8 8.6k, random-npc 4.5k, fighter-10 4.2k. Not routed yet.
- Also seen: trucker-103 (Gold Run carbon) and trucker-109 (CCC nickel) routes are exhausted. Both could be reassigned.

## Cargo Lanes crystal route (11-22)
fighter-2 (no faction, Bonanza) was quarantined at cargo_lanes_freight_depot since 11-09: 0 credits, fuel 3, Nebula bounty 362. Operator gifted 10k; play_as paid bounty + refueled, then send_gift --source=storage 38,841 + 38,840 energy_crystal to trucker-103/109 (the depot's base_id is the slug `cargo_lanes_freight_depot`). Their exhausted carbon/nickel delivers (103 id 7, 109 id 18) were removed; new hourly `deliver energy_crystal 200000 cargo_lanes_freight_depot <CJ> faction` (103 id 8, 109 id 19). fighter-2 still holds 132k steel_plate, 48k copper_piping, 17k copper_wiring there; Operator 11-22: HOLD the steel/copper at the depot until we know whether CJ or beid needs it. Rescue record cleared 11-22 so fighter-2 rejoins mining.
