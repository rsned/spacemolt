---
name: project_fleet_asset_snapshots
description: "FUTURE TASK — regular local snapshots of every agent's assets (ships + storage) so we can query the whole fleet's holdings; feeds crafting 'what can we source for free' and 'which hull does this agent own, and where'"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2cf71781-6ccf-44b8-b879-8971d8d06726
  modified: 2026-07-27T08:55:23.305Z
---

**Operator request 2026-07-27 (explicitly NOT right away — a future task):**

> *"every agent probably does own more than one ship, but they are likely at the agents `<home_base>` far from where they are. this feeds into a separate longer term feature I want to get to, locally storing snapshots of all agents game assets. all of these agents have thousands of items in storage, and more than one ship. i want to be able to start doing regular saving of this data to make it easier to find and lookup across them what we have access to. (this would also tie into the larger crafting planning request, what items can we source for 'free' from our asset pool.)"*

## Why it surfaced

Directly out of the smuggling-hull finding ([[project_smuggling_enablement]]): the fix for smuggling economics is to fly a **tier 0/1 hull at ~1 cr/jump** instead of a 355-cargo mission hauler at ~176 cr/jump. But we cannot answer *"what hulls does engineer-1 already own, and which base are they sitting at"* from local data — so every hull decision needs a live per-agent query with a supervisor-freeze window. Multiply that across ~40 agents and it is unworkable. The same lookup is what craftbrain's planner wants for free-sourcing.

## Verified state of the tables, 2026-07-27 (read-only against `data/spacemolt-knowledge.db`)

| table | rows | coverage | last captured |
|---|---|---|---|
| `storage_snapshots` | 227 | 150 agents, 227 (agent,base) pairs | **2026-07-02T17:16** — 25 days stale |
| `storage_snapshot_items` | 53,898 | 96,711,092 units total | same |
| `storage_snapshot_ships` | 6,863 | ships parked per snapshot (`ship_id, class_id, class_name, cargo_used`) | same |
| `agent_ships` | **0** | **never populated** | — |

So the schema for both halves already exists. `storage_snapshot_ships` even carries the ship-at-base data the hull question needs — it is just a month cold. `agent_ships` is the richer per-hull table (hull/shield/fuel/cargo/cpu/power/slots/`docked_at_base`/`last_updated_tick`, FK to `players` and `ships`) and has **never had a row written to it**; whatever was meant to fill it was never wired.

Every existing row came from `cmd/tools/daily-summary` runs, which stopped 2026-07-02. Nothing in the live worker fleet writes any of this.

## The blocker is already diagnosed

[[project_worker_storage_capture_gap]] (deferred 2026-07-09) has the three verified blockers and the cheapest correct route: call `agent.WireStorageCapture(client, kb, agentID, logger)` at worker startup in `cmd/worker/main.go` (agentID already in scope), then `view_storage` becomes a one-line dispatch case + `supported` entry + `roles.yaml` schedule line. Verify those file paths still hold before quoting them — that note is from 07-09.

That note solves **storage**. This project is broader and adds:
- **Ships** — `list_ships`/`get_ship` capture into `agent_ships`, which is the half that has literally zero data and is what the hull question needs.
- **Cross-agent query surface** — the point is *"easier to find and lookup ACROSS them what we have access to."* A per-agent table nobody can join over doesn't deliver that. Wants a rollup: item → which agents hold it, how much, at which base, how stale.
- **Regular cadence**, not a manual daily-summary run that can silently stop for 25 days without anyone noticing (same unsupervised-daemon failure mode as [[reference_market_db_prune]] and [[project_scanner_outage_expiry_fix]] — whatever schedules this needs a staleness alarm, not just a cron).

## 🔴 96% of the recorded ship rows do NOT join to the ships catalog

Verified 2026-07-27: `storage_snapshot_ships LEFT JOIN ships ON ships.id = class_id` leaves **6,579 of 6,863 rows unmatched**. The stored `class_id` values are legacy/renamed slugs the catalog no longer carries:

| stored `class_id` | catalog has |
|---|---|
| `prospector` | `prospect` (tier 0, cargo 100, fuel 130) |
| `excavator` | `excavation` (tier 2, cargo 200, fuel 420) |
| `drillship` | — (`siege_drill`? unconfirmed) |
| `deeprock_harvester`, `sparrow` | — |

Consequences for the build:
- **Any hull-stat lookup off this table silently yields nothing.** Asking "what tier/cargo/fuel is the ship engineer-1 owns" returns zero rows, not a wrong answer — easy to misread as "owns no ships."
- **`agent_ships` has a FK to `ships(id)`.** That is a plausible reason it has never had a row written: the natural insert would be rejected (or would need FKs off). Confirm before designing around it.
- The mapping is NOT a safe guess. `prospector`→`prospect` looks obvious but `prospectus` also exists (tier 2, cargo 80) — guessing wrong swaps a tier-0 hull for a tier-2 one, which inverts the fuel-economics conclusion the lookup exists to serve. Resolve class ids from a **live** `list_ships`/catalog refresh, don't fuzzy-match slugs.

## Fuel prices are NOT static across the galaxy (operator, 2026-07-27)

Relevant because "what can we source for free" and hull-economics questions both want a *cost*, and cost is location-dependent:
- The durable, hull-intrinsic quantity is **fuel UNITS per jump** (engineer-1's mission hauler measured at **8/jump** on the wire, 2026-07-27). That is comparable across the galaxy.
- Credits per jump is **not** — it is units × the local station's fuel price. Quoting a cr/jump figure without naming the station is meaningless.
- `pkg/worker/mission.go` (~:399) already reflects this only partially: `fuelCostFor(jumps) = jumps * fuelPerJump * priceOf(state.CurrentPOI)` — it prices an entire multi-jump route at the **origin POI's** rate. Not wrong so much as noisy in both directions, and worth knowing before trusting any gate's net figure on a long route.

## Gotchas already known

- `storage_snapshots` is **upserted**, `UNIQUE(agent_id, base_id)` — one current row per pair, no history. `quantity` is `REAL`. See [[reference_storage_snapshots_shape]]; don't re-add "pick the latest" logic.
- Writes land in `spacemolt-knowledge.db` (1.4GB), NOT `market.db` — so this does not add to the market.db write contention the operator wants to relieve.
- ~40 live workers all capturing on a schedule is real write load; stagger it.
- Free-sourcing math must treat stale holdings honestly — craftbrain already has `StatusStale` / `MaxStockAge = 24h`, so degrade rather than over-claim. Flooring quantities is the safe direction.

Related: [[project_crafting_brain]], [[project_smuggling_enablement]], [[reference_ship_role_naming_scheme]].
