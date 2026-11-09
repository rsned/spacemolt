---
name: project_cloverfield_home_station
description: "CRFT's own player station Conjunction Junction in Cloverfield (founded 2026-10-25) — record set, recorder flags, budget rules, and what the docs got wrong"
metadata:
  node_type: memory
  type: project
  originSessionId: 90ad50cf-9c6a-4f6f-a1d4-f3dc05c8ebcb
  modified: 2026-11-03T03:16:36.032Z
---

**Conjunction Junction** — CRFT player station in `cloverfield` (lawless crossroads
Haven↔Outer Rim). Founded 2026-10-25 13:49 PDT, tick 2232761, by craftsman-1 (the
user's MANUAL account, never in a fleet) with `build_base --name "..."`, fee 5,000,000.
base_id `c976fa7df53c87f9616165d7d411b41a`, poi_id `862e07fa76571d0a76cab3cbaf4046e7`
(station got its OWN new POI; founder auto-docked).

**Why:** a home of our own first (stop paying rent across NPC stations), growth from
crossroads traffic second, defenses required. Also a charting expedition: findings
feed a KB station guide (~/spacemolt/kb) and later a SCADA/HMI station console.

**How to apply:**
- Spec `docs/superpowers/specs/2026-10-25-cloverfield-home-station-design.md`; plan
  `docs/superpowers/plans/2026-10-25-cloverfield-home-station.md` (Tasks 1-4 code done;
  5-9 are by-hand ops with the user).
- Record set `data/stations/cloverfield/` — run play_as with
  `--record-dir data/stations/cloverfield`; `station_snapshot`; `ledger <cat> <item|credits> <qty>`.
  `findings.md` is the guide's source — tag every fact measured/doc-quoted + date.
- Budget (user): CRFT treasury floor 1M; consumable purchases ≤1M total; EVERY facility
  build planned + user-approved first (credits + materials); the_banker tops up.
- User's top fear: facilities going offline from missing maintenance inputs — stock
  each facility's inputs BEFORE building it.
- Fresh station: 500k FACTION storage included (player-reported, LT1428), but 0 power /
  0 life support and NO personal storage (`view_storage` → no_storage). Reported best
  build order: locker bay → faction workshop → atmospheric recycler → then fuel production.
- Don't `set_home_base` until checked against fleet routing.
- KB guide (2026-10-25): `~/spacemolt/kb` has `cmd/generate-guides` + `pkg/stationplan` +
  `guides/player-stations.md` → `kb/guides/` (catalog-driven build steps, sticky upkeep sidebar),
  merged to KB LOCAL main 74cdd577, NOT pushed. KB local main has DIVERGED from origin (24 local
  vs 10 remote, incl. origin 167f8fd4 "Did You Know in the top nav" which touches kbnav.Items) —
  user chose to leave it; resolve before pushing. Guides is linked from kb/index.html only; adding
  it to kbnav.Items needs a full ~14k-page regen (TestGeneratedPagesMatchNav).
- Station went CRITICAL after the Locker Bay finished with no flex_polymer stocked; fix =
  faction_deposit_items flex_polymer (CRFT has ~1,940 at GE, craftsman-1 1,164 personal).

**Next phase (user, 2026-10-27):** once the maintenance cycle size and upkeep needs are validated, dismantle CRFT's rented facilities at Haven and haul them to rebuild at Conjunction Junction. **Why:** Haven rent costs ~500k cr/day. **How to apply:** dismantling is untested (only seen as a facility state in the rent note); run the first dismantle as an experiment on the cheapest spare Haven facility and record what it returns, where it lands and its size before planning hauls.

**2026-10-29 state (Conjunction Junction):**
- Station fuel tank holds only **5,000**. Combustor jobs consume all LH2 up front, so a 20,000-fuel job overfills and probably wastes the excess; miningbot_0000 runs `craft manufacture_fuel_basic 2000` hourly. Nobody ran the combustor automatically before 10-29 and the tank went empty (`station_fuel_empty`).
- Next build in progress: `fuel_tank_expansion` (+50k tank) = 500k + 8,000 steel + 1,500 ceramite_plating + 800 fuel_tank. Ceramite/alloy made at Sol's public kiln/foundry by craftsman-3, carried by trader-2's daily deliver; tanks crafted at CJ (build_fuel_tank, hourly on miningbot_0000 — remove once 800 reached).
- Build policy: `allow_outsiders=true` (set by operator 10-29). YSMT (hauler-0/smt_boss) built a faction_lockbox here, rent 10/cycle to CRFT.
- Control-node line: explorer-3 hauls Ironhearth platinum (marketbot_015 consign) to CJ; miningbot_0000 crafts platinum wire, circuit boards (carbon_arc) and platinum control nodes hourly; trader-2 hauls carbon (Gold Run) and silicon (Grand Exchange).

**Faction storage buckets at CJ (2026-11-02):** `view_faction_storage` now returns `buckets` (named storage extensions, each with `cap_per_item` 100000 and `package_cap` 1000). There is probably just one: bucket id `a6bc64fb3ce3ba7973fa08fe3fbaf3a3`, first seen under its default name "Storage Extension" and then named **"Maintainence"** by the operator. Check with `view_faction_storage`. Use that exact spelling in `bucket=<name>`. Deposit with `storage deposit target=faction bucket=<name>`, per the server hint. As of 11-02 our client does not pass a `bucket` param, and the 11-01 command docs don't mention buckets. The serverapi struct decodes `buckets` (commit 351e3cde).
Server notes (operator-quoted 11-02):
- A bucket is a facility; rename it with `facility action=set_name`.
- Faction crafting can pull from and deliver into a bucket with `deliver_to="faction:<name>"`.
- play_as `craft ... --deliver_to=faction:<name>` passes the string through unchanged (pkg/game/crafting.go craftJob), so it should already work. Untested.

**Why (operator 11-02):** Maintainence is a reserve for station-maintenance inputs, so that construction (commission_ship materials, facility builds), sales (faction_restock sell orders) and crafting can't use them up by accident.
**How to apply:** UNVERIFIED as of 11-02: whether facility maintenance can draw from a bucket at all. If maintenance only consumes the main faction pool, moving inputs into the bucket would starve the facilities, which is the operator's top fear. Test with a small amount first, and only bucket the surplus above about one cycle's needs until that's confirmed.

**Contraband shelf at CJ (2026-11-01):** miningbot_0000 runs hourly `faction_restock <item> <price>` (lists whatever faction storage holds as a faction sell order; same-price orders consolidate): pirate_moonshine **1500** (operator floor — 200 was sold out to resellers in 12 fills), void_dust **5000**, nerve_burn **6500** (repriced 2026-11-09 from 15,000: 581 sat unfilled since 11-01; top NPC bid 6,154, other asks 6,017–6,290; 6,500 stays above NPC bids so resellers cannot flip it). Moonshine now ships Voss → CJ (trader-9 task 11, recipient miningbot_0000) instead of Treasure Cache. Raising an order's price costs 1% of the value increase (nerve burn 8.5k→15k = 21,580); miningbot_0000 got a 50k float from the_banker for fees.

**2026-11-05 foundry + steel:** 500k moved the_banker → miningbot_0000 → treasury (1,424,238 after spend). miningbot_0000 (Officer) built alloy_foundry, facility 789997108b3cbd91fe4d0d0f3345983b, because craftsman-1 is the operator's open play_as session; do not log into craftsman-1 from a tool. CJ had NO scheduled steel craft before this. miningbot_0000 schedule id 47 = hourly `craft refine_steel 500 prefer_own` (labor 5 cr/steel billed to the treasury, ~60k/day at full rate): cut or remove it once the steel backlog is met. Iron refinery L1 already makes ~6,600 steel/hr, so the L2 Industrial Forge is not the bottleneck fix; L2 is 3x speed, output per run stays 2, labor per run doubles.

**2026-11-05 battery + coolant:** another 3M moved to the treasury; point_defense_battery built (f5aa79582a5fa81aab9826b1044565dd). miningbot_0000 ids 48 (daily `process_nitrogen_ice 300`) and 49 (hourly `synthesize_coolant_fluid 6`, ~144/day) feed coolant upkeep; raise id 49 before building shield_projector, which adds 2 coolant/cycle. Consign fixes written (faction_pull item:keep; deposit sweeps skip consign offers) but `deliver <item> ... faction` tasks are NOT guarded, so remove any such task before offering that item (miningbot_0000 id 12 for control_node).
