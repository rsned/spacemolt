---
name: project_mining_fleet
description: "The mining fleet: created 2026-08-21 as the pirate-unlock campaign's graduation destination; roster, two-system loop design, and the ore-selection refinement still open"
metadata:
  type: project
---

**Created 2026-08-21. Fleet set is now NINE.** Built to receive the seven agents
that had banked the pirate unlock and were still churning the finished unlock
board — the same waste as johnny_cab's 47-hour livelock, found the same day.
[[project_pirate_reputation_unlock_campaign]] · [[reference_livelock_invisible_to_health_checks]]

**There was no mining fleet before this.** `data/overmind/fleet.yaml` is the dead
Plan-A monolith — it still lists miner-1..10 beside explorers and haulers that
have since moved to live fleets, so STARTING IT WOULD DOUBLE-RUN ~30 AGENTS
(`session_replaced` thrash). It stays unrun. The roster is
`data/overmind/mining-fleet.yaml`; launch line mirrors shuttle's, see
[[reference_overmind_launch_commands]].

**The `miner` role did not mine.** Until this change it had no `idle:` key at
all — captures only. Now `idle: idle_mine` plus an hourly `capture_fuel`.

## Roster (7) and the two-system split

Four mine in place (single rich belt in their home system, station present):
**miner-1, miner-4** (Treasure Cache), **miner-9, random-clark** (Dheneb).
random-clark has mining skill 0 and mines fine — **beginner agents ship with a
mining laser** (operator), so skill 0 is not a gate.

Three ran out of rock and got the **operator's two-system loop** via per-agent
script overrides (`data/agents/<id>/scripts/idle_mine.smolt`):

```
jump <stationless_system> ; get_system ; travel <belt> ; loop -f 100 mine
jump <home_system>        ; get_system ; travel $STATION$ ; dock ; deposit_all ; refuel
```

| agent | mines in | home (fuel base) | why |
|---|---|---|---|
| miner-10 | frostfeld | ironhearth | 402,006 remaining, richest in reach |
| prophet-2 | 82_eridani | ironhearth | 110,300 @ rich 52, SINGLE belt |
| overmind | hd_20794 (`hd_20794_forge_vein`) | the_anvil | 62,283 copper + 12,875 iron |

**Why stationless systems:** nobody mines where they cannot dock, so the rock is
untouched — frostfeld holds 24x what Ironhearth has left.

**Why home stays put:** ironhearth and the_anvil have MEASURED fuel desks
(`station_fuel_prices`: 49,900 @ 6 and 186,871 @ 6). The nearer low-security
systems that have their own station — **alzirr, gsc_0034, tiaki, silvermark —
have NEVER had a fuel reading**, and an unverified desk in lawless space is how
an agent strands. Lawless idling is safe for THESE agents specifically because
they hold the pirate unlock ([[reference_lawless_transit_vs_idle]]).
**zaniah was excluded deliberately: 6 combat losses, it is a kill zone.**

Result after the fixes: **7/7 collecting, 6 at a 100% yield rate.**

## Open / next

- **⭐ ORE SELECTION (operator, deferred): "for the time being it doesn't really
  matter what ore they mine as long as they are collecting ore."**
  **`mine` TAKES NO ARGUMENTS AND THE ORE IS RANDOM PER CYCLE** (operator,
  confirmed in server_docs/openapi.json — the documented payload is literally
  `{"type": "mine"}`). So this is NOT a client dispatch change: the server has no
  ore parameter to pass. **The only lever that exists today is WHICH DEPOSIT you
  stand on** — the random draw is over that deposit's resource list, so ore
  selection is really POI selection, which the per-agent scripts already do
  (`travel hd_20794_forge_vein` picks a copper/iron/darksteel pool over the
  9-resource `hd_20794_belt`). Narrow the pool by choosing a deposit with few
  resource rows in `poi_resources`.
- **⭐ BEAM POWER vs DEPLETION (server docs, not yet acted on):** deposits have a
  `supported_power` (visible in `get_poi`); power above it is capped, and an array
  **more than 4x over a heavily depleted deposit's supported power cannot extract
  at all** — error `deposit_too_sparse`, remedy is "relocate or fit smaller/finer
  modules". So a BIG mining rig on a thin belt can be strictly worse than a small
  one. Worth checking if a miner reports `deposit_too_sparse` rather than the
  ordinary `Resources depleted`.
- KB resource rows for these belts are stale (hd_20794 last surveyed at tick
  1172416 vs a live tick ~1676499), so `remaining` is a ranking hint, not truth.

Script authoring traps that cost three restarts here: [[reference_idle_script_authoring_traps]]
