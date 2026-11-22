---
name: project_purified_water_sm_arbitrage
description: "2026-11-20 experiment — 10k purified_water listed @60 at CJ via miningbot_0000 faction_pull+shopkeep so haulers arbitrage it to stronghold Station-Manager bids; how to read results, shopkeep gotchas"
metadata:
  node_type: memory
  type: project
  originSessionId: 8c15f693-8825-4572-8f22-51141db1e328
  modified: 2026-11-21T00:59:53.287Z
---

**Started 2026-11-20 ~16:52 PT (operator: "start with 10k @ 60").** The idea is the operator's: put a cheap ask at Conjunction Junction and let the existing haul/arbitrage fleet match it to Station-Manager purified_water bids at pirate strongholds (Mera Sanctum, Korr, Nyx Nexus, Thane Keep, Dross Citadel; 89–156 cr). Haulers gate strongholds per agent on live pirate standing, and 166 agents hold all nine unlocks.

Setup (crft-mining-fleet.yaml, miningbot_0000, a CRFT Officer at CJ):
- `faction_pull: "purified_water:53927"`: keep 53,927 of 63,927, so it pulls 10k at 175 per pass.
- `shopkeep: "purified_water:60:60:0:9950"`: one 9,950-unit ask, floor 60. A 50-unit ask posted first at the default lot.
- If faction purified_water grows (gifts or crafts), the pull takes more than 10k, because keep is absolute.

**Expect bursty results.** From `bin/sm-demand` (branch sm-demand-report): stronghold SM bids are open only 12–38% of hours and get filled within hours, so a 14–27 jump haul often arrives late and the reroute dumps the cargo at Frontier Station (37) or holds it. Judge the experiment by how much was sold at a stronghold versus dumped, using the haul_results table in market.db.

Gotchas found:
- Shopkeep keeps ONE ask per item; each is one lot (default 50). After a FULL fill the next ask is priced +5%. Use a big lot.
- faction_deposit and faction_deposit_all skipped only consign_offer items, so they swept pulled shopkeep stock straight back to faction storage. Fixed with `worker.DepositHeld` (consign offers ∪ shopkeep items), uncommitted in the main tree as of 11-20.
- CJ is `public_access=1`, so outside players can buy our asks. That's why the price is 60, not the 1 cr first floated.

Related: [[project_cloverfield_home_station]], [[feedback_stronghold_routing_requires_pirate_unlock]], [[reference_shopkeep_consign_overlap_is_fatal]]

**11-21 07:15 check: 0 sold.**
- Only the first 50-unit ask @60 is on the CJ book. It never filled, so shopkeep never posted the 9,950 lot (one ask per item).
- 9,910 units sit in miningbot_0000's personal storage at CJ.
- No arbitrage_opportunities from CJ and no haul_results.
- Live stronghold bids were thin: Korr 48 @123, plus Iron Reach (not a stronghold) 35 @90.
- Fix if wanted: cancel order 5600a790… so shopkeep reposts the big lot.

**11-21 stronghold pre-stock (operator approved):** spec docs/superpowers/specs/2026-11-21-stronghold-purified-water-prestock-design.md.
- Stronghold marketbots shopkeep 1,000 at start = floor. Prices: Dross 100, Mera 120, Nyx 120, Korr 100, Crix 100, Thane 85, Voss 140.
- craftsman-4 (in a Congregation) carries the water: `deliver purified_water 1000 faction:<CJ> <stronghold>_station <marketbot>`.
- Dross (marketbot_algol) goes first, as a test of whether a resting ask fills when the SM bid opens. The other six wait for a Dross fill.
- The huge 117k–235k asks at every stronghold are the SM's own. The only player competition is ~530 units at Dross @100–106.
- **Gotcha:** mb-fleet yaml edits need `kill -HUP <mb overmind pid>`. A worker TERM alone reuses the old args.
- **11-21 07:40 rollout to all six demand strongholds** (operator: "stock the other 8").
  - Skipped sable_port and kael_arsenal: no SM water demand (Kael's max bid is 2, and it has 5.2M asks @78; Sable had one bid @41).
  - craftsman-4: Dross, then Mera, Thane, Korr.
  - craftsman-5 (in the last spare Congregation): Voss, Crix, Nyx.
  - The delivers are WEEKLY. Remove or adjust them if a stronghold's stock goes unsold, or the stock accumulates.
- **CRFT members can't faction-withdraw** ("manage treasury" permission is needed). trucker-100's `deliver faction:` LH2 run fails for that reason. Officers (craftsman-*, miningbot_0000) can withdraw.
- **11-21 08:09: the partial loads were a stale cargo capacity, not gift failures** (the operator noted that marketbots gift from strongholds fine).
  - `switch_ship` leaves the old hull's capacity cached. `deliver` sized its first withdraw from that cache: Voss got 100 and Dross a partial load.
  - Fix, uncommitted:
    - deliverWithdraw now calls GetCargo before sizing the withdraw.
    - switch_hull now calls GetCargo after boarding.
    - Tests: TestDeliverSizesWithdrawFromFreshCapacity, TestSwitchHullRefreshesCargoAfterBoarding.
  - Both carriers were restarted at 08:09 and all 7 delivers re-queued.
- Also: a pending action blocks every other action until the next tick. A play_as command on a moving worker breaks its in-flight steps. Don't test on a worker that is mid-route.
- **11-21 10:10 Nyx Nexus had its blast doors shut for a battle**, so craftsman-5 could not dock.
  - Operator: put that water at Korr for now.
  - Operator: after the battle, Nyx will need to rebuild, which means buy orders for components. That is a supply opportunity: watch Nyx's buy book once the battle ends.
  - marketbot_gsc_0008 keeps its purified_water rule but has no stock delivered.
- **Beid refueling dock** (11-21): its upkeep per cycle is 2 flex_polymer, 2 steel_plate, 1 copper_wiring.
  - Beid had no copper_wiring, so the dock went offline ("owning faction has not supplied its upkeep").
  - craftsman-2 now runs daily deliveries of 100 copper_wiring and 200 steel_plate from CJ.
  - Watch flex_polymer (280 at Beid). The facility catalog lists maintenance_inputs per facility.
- **11-22 08:00 check:** in about 24 h only Thane sold anything (32 units at 85). The other six asks are untouched.
  - The weekly carriers re-fired at Sunday 00:00 UTC ([[reference_schedule_periods_are_utc_calendar]]), so each stronghold bot also holds about 1,000 more in storage behind its ask. Korr has 2,000, because the one-off fired twice.
  - Removed craftsman-5 id 31 (the Korr one-off).
  - Weekly carriers craftsman-4 25–28 and craftsman-5's are still scheduled. Decide before next Sunday 00:00 UTC.
