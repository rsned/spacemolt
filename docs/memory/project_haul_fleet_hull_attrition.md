---
name: project_haul_fleet_hull_attrition
description: "15 of 22 haulers lost operator-purchased freight hulls + cargo expanders and are flying free starter replacements; kill zones are zaniah (pirate stronghold) and goldcrest (wildlife)"
metadata:
  type: project
---

**Found 2026-08-22, only because `ship_class` reached the dashboard.** It was
invisible before: nothing displayed the hull an agent was actually flying.

**The operator bought every hauler a freight hull WITH cargo expanders to
bootstrap the fleet. 15 of 22 have lost that investment** and are flying free
starter replacements. The hulls AND the fitted expanders are gone — the 15 carry
2-3 modules, against 4-8 on the intact ships.

| tier | classes | cargo | count |
|---|---|---|---|
| starter (lost their hull) | shard / threshold / theoria / cobble / prospect | 60-100 | **15** |
| freight (intact) | dividend / bedrock / floor_price / empirica / chop_shop | 180-480 | 5 |
| heavy (intact) | enterprise / junk_convoy | 1100-1350 | 2 |

**They are sitting on ~296M credits** (11M-27M each) while hauling 60-100 units a
trip. Insurance pays CREDITS, not a hull ([[reference_ship_replacement_workflow]]),
which is exactly why the wallets are fat and the holds are small.

## It is an ONGOING loop, not historical attrition

**salvager-7 has died twice in goldcrest, both times in a `shard`** — killed,
free-replaced, killed again. salvager-9 lost a cobble at zaniah and flies a cobble
now. **trader-2 lost a `floor_price` (420 cargo) at zaniah** — that is a real
freight hull dying, not a starter.

Ten of the twelve recorded destructions are in two systems:
- **zaniah — 6, cause `combat`. It is a PIRATE STRONGHOLD** (operator).
- **goldcrest — 4, cause `wildlife`.**

Only 9 of the 15 have a recorded destruction; capture backfilled to 2026-08-07,
so the other 6 (salvager-1/5/6, trader-3/7/8) lost theirs before the ledger began.

## ⭐🔴 ROOT CAUSE PROVEN: the mid-route re-route bypasses the stronghold filter

**`haulSellLeg` (pkg/worker/haul.go:~1190-1214) re-routes to a fresh sell market
without ANY stronghold check.** The claimed opportunity was filtered; its
replacement is not.

```
haul: opp 659254 demand thinned mid-route; re-routing to zaniah @mera_sanctum
```

Flow: a sell-leg watchdog notices demand at the claimed destination has thinned
below break-even -> autopilot stops early -> `haulFindReroute` picks the
best-priced alternative -> `sellSys, opp.ToStationID = newSys, newStn` -> the
agent flies there. `haulFindReroute` (~:1414) ranks purely on
`FindBestPrices` + `absorbableProceeds` over a plain jump graph; it never
consults the stronghold set. **A pirate stronghold market looks like the best
price on the board precisely because nobody safely trades there.**

**salvager-4 and salvager-9 were both re-routed to zaniah @mera_sanctum within
10 SECONDS of each other** (2026-08-19 10:55:16 and 10:55:26 local) and died 100
seconds apart. Same opportunity item, same "best" alternative, two dead hulls.

**Everything upstream is innocent, and I wrongly suspected it first:**
- `filterStrongholdRoutes` IS working: across 60MB+ of log, only the three agents
  that hold the pirate unlock (craftsman-1, trader-10, explorer-1) were ever told
  "pirate unlock held". Zero galaxy-graph build failures.
- Only trader-10 (unlocked) ever logged travel to a stronghold via normal routing.
- **Strongholds are DEAD-END systems with no transit through them** (operator), so
  the endpoint guard is sufficient in principle and "transit hops" was a red
  herring. The only way in is to be sent there deliberately -- which is exactly
  what the re-route does.

**Fix:** thread the stronghold set into `haulSellLeg`/`haulFindReroute` and drop
candidate markets whose system is a stronghold the agent may not enter, mirroring
`filterStrongholdRoutes`. NOT YET IMPLEMENTED.

**goldcrest is separate and still unaddressed.** Those 4 are `wildlife`, not
combat, and there is no wildlife avoidance anywhere; `danger_zones` exists in the
KB with 0 rows and nothing writes to it.

## ⭐ DO NOT RE-EQUIP FIRST

Buying 15 replacement freight hulls before the routing is fixed feeds fresh
capital into the same grinder — trader-2 already proves a 420-cargo hull dies
there just as readily as a starter. Sequence: populate `danger_zones` from
`combat.ship_destroyed`, gate haul routing on it, verify no further losses, THEN
re-equip from the 296M.

Note the counter-argument on sizing: [[reference_haul_fleet_capacity_ceiling]] and
[[reference_book_depth_is_the_real_haul_ceiling]] say a BIGGER hull gets FEWER
book slots (`bookCap=ceil(srcUnits/cargoCap)`), so the gain from re-equipping is
not linear in cargo. Rank fitted, not by base stats
([[reference_prayer_class_freight_hulls.md]]).

**2026-08-30 operator sighting:** Bulk Terms (T2 Commercial hauler,
`bulk_terms`) listed at 130,278 cr by the Station Manager at Cargo Lanes
Freight Depot, listing `1615a7455302eb39b63c987cb3961cf1`. A candidate
replacement hull — but the standing rule above holds: do NOT re-equip
before the routing fix. Station-manager listings expire/sell; re-check
before acting.
