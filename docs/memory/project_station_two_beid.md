---
name: project-station-two-beid
description: "CRFT station #2 in beid (founded 11-16, base b7ecd9b1…, POI 4650c48e…); lead craftsman-2; record set data/stations/beid; pending setup steps"
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

**PENDING (operator said yes, 11-16):** when haul sessions end, put craftsman-2
back in a fleet pinned to beid with a scheduled faction_deposit_all, so
marketbot_home2 (DB) gifts drone output to craftsman-2 who deposits into beid
storage (mirrors marketbot_home -> miningbot_0000 at CJ). Show the yaml first.

Related: [[project_cloverfield_home_station]] [[reference_congregation_is_outerrim_exclusive]]
