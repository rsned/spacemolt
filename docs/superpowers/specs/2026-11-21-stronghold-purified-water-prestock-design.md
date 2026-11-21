# Pre-stock purified_water at pirate strongholds (short design)

**Approved by operator 2026-11-21** ("yes, this looks right and prices sound ok").

## Intent
Station-Manager (SM) bids for purified_water at the pirate strongholds are bursty. Over 11-07..11-21 they
were open 11–51% of hours, and the station restocked 1.4k–21k units a day at 89–156 cr. The bids refill
and close before a 14–27 jump haul from CJ arrives, which is why the CJ ask experiment
(project_purified_water_sm_arbitrage) sold nothing. Stock already resting at the stronghold reaches the
bid first.

## Approaches
1. **Resting asks at the stronghold (chosen).** The stronghold's resident marketbot holds 1,000 units
   and shopkeeps one ask priced just under the SM's typical bid. Evidence: only 23 of 636k
   station/item/snapshot books were ever crossed, so new crossing orders appear to fill at once.
   This is inferred, not proven, so one stronghold gets tested first.
2. Faster haulers from CJ. Rejected: the bids close in hours and the trip is 14–27 jumps.
3. A buy-on-open watcher at each stronghold that sells from storage the moment a bid appears.
   Rejected for now: it needs new code, and approach 1 needs none if matching works.

## Design (config only, no new code)
- **Carrier:** craftsman-4, a CRFT officer with all 9 pirate unlocks, docked at CJ. It moves into a
  spare Congregation (`switch_hull congregation`) and runs one-off
  `deliver purified_water 1000 faction:<CJ base> <stronghold base> <stronghold marketbot>`.
  The water comes from CJ faction storage (53.8k), and the CJ 9,950 ask stays as it is.
- **Sellers:** each stronghold marketbot (mb-fleet.yaml) gets the shopkeep rule
  `purified_water:P:P:0:1000`, with start = floor so the price never steps down.

  | Stronghold | Marketbot | Price P |
  |---|---|---|
  | dross_citadel_station | marketbot_algol | 100 |
  | mera_sanctum_station | marketbot_zaniah | 120 |
  | nyx_nexus_station | marketbot_gsc_0008 | 120 |
  | korr_fortress_station | marketbot_gliese_581 | 100 |
  | crix_stronghold_station | marketbot_bellatrix | 100 |
  | thane_keep_station | marketbot_sheratan | 85 |
  | voss_redoubt_station | marketbot_alhena | 140 |

- **Order:** Dross first (14 jumps, the closest). Roll out to the other six only after a Dross fill
  against an SM bid is seen.

## Competition check (11-21 15:20Z)
- The SM's own asks sit at every stronghold (117k–235k units, 102–228 cr). Those belong to the
  station, not to players.
- The only player asks below our prices are at Dross: 9@100, 145@105, 380@106 (about 530 units).
  Mera has 2,159@228, which is above our price. Nothing else.
- So nobody else is visibly pre-stocking at scale.

## Left to the builder
Trip batching (a Congregation can carry 1,900, so one trip can cover two strongholds), and the per-station
price later, from fill data.
