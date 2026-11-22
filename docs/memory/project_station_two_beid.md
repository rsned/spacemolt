---
name: project-station-two-beid
description: "CRFT station #2 in beid (founded 11-16, base b7ecd9b1…, POI 4650c48e…); lead craftsman-2; daily LH2 delivery feeds the combustor; marketbot_home2 drones live 11-20, spare bay+5 drones reserved for a 2nd host"
metadata:
  node_type: memory
  type: project
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-16T10:20:59.413Z
---

Station #2 founded 2026-11-16 in **beid** (base `b7ecd9b1ce7c2689d73d4f15f30dd134`,
own new POI `4650c48e3b6787e6cbeb69ebb3c61933`, fee 5M). Record set
data/stations/beid (README, findings.md, ledger.jsonl, events.jsonl);
play_as `--record-dir data/stations/beid`. Lead: **craftsman-2** (CRFT officer).

- Faction storage is FREE at your own station: `faction_build faction_lockbox`
  is refused ("already included free"). Deposits work at once.
- Power = stellar_collector_grid (900k + 2,000 steel + 1,200 circuit_board);
  operator brings the 1,200 boards. Each build needs the operator's yes.
- Haul sessions CJ->beid run by craftsman-2 (~4.5 min, 44 fuel per round trip,
  CJ fuel free from the bunker). Session 1: 2,982 steel + 50 boards.
- New accounts (11-16): miningbot_0100-0109 (travel miners, starter Cobble, CRFT),
  trucker-10..15 (congregation collectors, CRFT), marketbot_home2 (**Data Bots**
  faction, like all marketbots; station-bound drone host at beid). No pirate
  unlock: operator keeps them to empire/home systems.
- Drones are for station-bound bots only; miningbots travel to resources.
  marketbot_home2's bay + 5 drones come from craftsman-1's stock at frontier_station
  (operator gifts them).

**DONE 11-16:** craftsman-2 is a crft-mining resident pinned to beid with a
half_hourly faction_deposit_all (marketbot_home2 gifts drone output to it).
stellar_collector_grid placed 11-16 (900k; operator brought the boards).
300 flex_polymer hauled from CJ for locker_bay upkeep (it fell to 40% maint).
beid has NO fuel market yet: refuel there fails; craftsman-2 stays docked.
trucker-10..15 (congregations) joined the haul fleet; no pirate unlock, and
haul gates strongholds per agent.

**11-16 evening:** refueling_dock and h2_fuel_combustor ACTIVE; **faction_workshop placed**
(under construction 16:53 local). Without the workshop the station refuses ALL crafting with
`no_service`, even on a named production facility: build it right after the locker bay at any new
station. Upkeep supplies stocked (sunspindle, purified water, scrubbers, focused crystal).

Related: [[project_cloverfield_home_station]] [[reference_congregation_is_outerrim_exclusive]]

**11-20:** Beid bunker hit 0/5000 (no liquid_hydrogen). craftsman-2 now runs a daily
`deliver liquid_hydrogen 1000 faction:<CJ base> <beid base> faction` (new `faction:` source on
deliver); its hourly `craft manufacture_fuel_basic 2000` burns it. marketbot_home2 already had
bay+5 drones fitted but never deployed; fit_drones (resume) launched them on mine_asteroid_beid
17:53. A SPARE advanced_drone_bay + 5 mining_drone (gifted from CJ faction storage) sit in
marketbot_home2's personal storage at beid, reserved by the operator for a SECOND beid drone host on
another beid mining site. Don't return them to CJ.
