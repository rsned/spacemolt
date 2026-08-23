# Fleet Drone Refit — Operator Runbook (Phases 1–3)

> **This is a runbook, not a code plan.** Every step here is an action in the live game,
> driven by hand through `play_as` or by dispatching fleet work. The code work this
> campaign needs lives in `2026-08-22-drone-refit-blockers.md`.

**Spec:** `docs/superpowers/specs/2026-08-22-fleet-drone-refit-design.md`

**Goal:** 175 `advanced_drone_bay` + 800 `mining_drone`, fitted across the fleet.

**Standing facts:** hub is `grand_exchange_station` (haven). Crafting escrows from the
**crafting agent's own station storage** — not cargo, not another agent's storage. That
single rule is why Phase 2 exists and why it is the bulk of the work.

---

## Phase 0 — DONE 2026-08-22

`polymer_extruder` `b659c3602da933e414c9fa91a072968a` built at grand_exchange_station,
`recipe_id=extrude_nanoplastic`, rent 123/cycle, 120 ticks to construct. The 4.42M
monopoly rent is gone; 491 runs now cost ~3,928 cr at `labor_cost` 8.

### 0a. Outstanding: the mistaken `polymer_refinery`

A `polymer_refinery` was built first. It makes `flex_polymer`, not
`nanoplastic_composite`, and we hold 10,968 flex_polymer against a need of 2,266 — so it
is surplus capacity accruing **123/cycle (~10,600 cr/day)**.

- [ ] `facility faction_list` — confirm both facilities and read the refinery's id
- [ ] Decide: keep (a second income/utility facility) or `facility faction_dismantle
      facility_id=<refinery id>`
- [ ] **If dismantling:** it costs **one `cargo_container` per package produced** and CRFT
      storage holds only **3**. Check the package count before starting; a stalled
      dismantle mid-way is worse than either end state.

### 0b. Verify the extruder is live and usable

- [ ] Wait out construction (120 ticks ≈ 20 min from build)
- [ ] `facility faction_list` — status should leave "Under construction"
- [ ] Confirm our capture sees it, using the **base id**, not the POI id:
```sql
sqlite3 data/spacemolt-knowledge.db "
  SELECT station_id, facility_id, recipe_id, rental_fee_per_run, last_seen_utc
  FROM public_facilities WHERE recipe_id='extrude_nanoplastic';"
```
      Expect a second row beside `confederacy_central_command`. If it does not appear,
      that is not necessarily a bug — a faction facility may not be listed as *public*
      unless access is set. It does not need to be public for us to use it.

---

## Phase 1 — Source 3,607 `energy_crystal`

The only material the fleet does not already own. Decision: **mine what we can, buy the
remainder.**

### 1a. Fit harvesters (nebula work needs a gas harvester, not a mining laser)

craftsman-1 holds, at `grand_exchange_station`: **203 `gas_harvester_i`, 100
`gas_harvester_iii`, 23 `gas_harvester_ii`**.

- [ ] Confirm current holdings before moving any:
```sql
sqlite3 data/assets.db "
  SELECT item_id, base_id, CAST(quantity AS INT) qty
  FROM agent_storage_items
  WHERE player_id='a50924913cef881c5e4d14257589d9ba' AND item_id LIKE 'gas_harvester%'
  ORDER BY qty DESC;"
```
- [ ] Move `gas_harvester_iii` (best tier, 100 available) to the 7 mining-fleet agents:
      `miner-1, miner-4, miner-9, miner-10, overmind, prophet-2, random-clark`
- [ ] Fit one per miner and confirm — **note this is a slot decision**: a miner already
      running a mining laser may have to give one up. See the blockers plan; we currently
      cannot see fitted modules.

### 1b. ⚠️ SURVEY BEFORE COMMITTING MINERS — the deposit data is months stale

**The "two rich nebulas" plan does not survive contact with the survey timestamps.**
`poi_resources.last_updated_tick` against a current tick of ~1,686,357:

| site | system | remaining | surveyed | age | jumps haven / sol |
|---|---|---|---|---|---|
| `forgotten_prism` | ivorygate | 5,000 | tick 611,806 | **~124 days** | 18 / 12 |
| `the_quiet_shimmer` | gsc_0002 | 5,000 | tick 974,972 | **~82 days** | 20 / 14 |
| `wanderers_veil` | struve_1321 | 289 | tick 1,687,267 | **current** | **8** / 11 |

Both 5,000 figures are stale, and both are exactly `5000` — a round number that reads more
like a survey-time cap than a measurement. **Every site surveyed within the last 30 days
reads between 25 and 289 units, and the seven fresh sites total 615 against a need of
3,607.**

There is also no trend data to fall back on: `resource_history` has **0 rows** — the
writer exists, nothing calls it, same shape as `ship_modules`.

**Deposits regenerate — operator-confirmed 2026-08-22.** Observed live: `wanderers_veil`
read 255, then 289 forty minutes later. This changes the strategy more than the staleness
does:

- `remaining` is a **standing pool, not a lifetime budget**. A 289-unit site is not a
  289-unit site; it is a site that refills.
- Therefore **round-trip time, not deposit size, is the limiting variable.** A miner
  working an 8-jump site repeatedly out-produces one parked at a 20-jump site, because it
  gets more visits per day and the pool refills between them.
- The two distant "5,000" entries stop being the prize. Even if real, they are ~2.5x the
  round-trip distance of `wanderers_veil` for a pool we cannot verify.

**Preferred pattern: short-loop harvesting.** Work the nearest fresh sites on repeat
rather than mounting one long expedition. `wanderers_veil` (8 jumps from haven) is the
closest; `garnet` and `cedarhold` are 6 from sol, and `botein`/`pherkad`/`ruchbah` are 5-7
from sol — a sol-based miner has four sites inside 7 jumps.

Open question worth measuring on the first loop: **the regeneration rate**. Survey a site,
mine it out, and re-survey on the next visit. That number sets how many miners a site can
sustain and whether 3,607 is reachable by mining at all. Nothing records it today —
`resource_history` has 0 rows.

- [ ] **Survey first, mine second.** Send one agent (not the fleet) to `ivorygate` and
      `gsc_0002` and `survey_system` before committing miners to an 18–20 jump trip. If
      the 5,000 figures are real, proceed as planned. If they read like the fresh sites,
      **mining cannot supply 3,607** and the split shifts hard toward buying.
- [ ] Re-rank live before dispatching — this ordering will change:
```sql
sqlite3 data/spacemolt-knowledge.db "
  SELECT p.system_id, r.poi_id, CAST(r.remaining AS INT) rem, r.richness,
         r.last_updated_tick, r.detected_by
  FROM poi_resources r JOIN pois p ON p.id=r.poi_id
  WHERE r.resource_id='energy_crystal' ORDER BY r.last_updated_tick DESC;"
```
- [ ] **Start short-loop harvesting now** — it needs no survey expedition and no long
      trip. Put miners on the nearest fresh sites and let them cycle:
      `wanderers_veil` (8 from haven) · `garnet`, `cedarhold` (6 from sol) ·
      `botein`, `pherkad`, `ruchbah` (5-7 from sol).
- [ ] Record remaining-on-arrival each visit so we learn the regen rate. Until
      `resource_history` is wired, a note per visit is enough.

- [ ] ⚠️ **Do not use the `mine_qty` verb until its fuel bug is fixed** (blockers plan,
      Task 4). It has no fuel guard and no return-to-station; on 2026-07-12 it stranded
      two craftsmen fuel-dead into quarantine. Until then, drive mining by hand or via the
      miner idle script, which refuels first.
- [ ] Track yield against the target:
```sql
sqlite3 data/assets.db "
  SELECT CAST(SUM(quantity) AS INT) held FROM agent_storage_items
  WHERE item_id='energy_crystal';"
```
      Baseline at time of writing: **1,241**. Target: **4,848** total.

### 1c. Buy the remainder

- [ ] Deepest book by far is **The Obsidian Well** (arneb, POI
      `a356fc2c1744c0425cf6cf47f48def92`): 49,232 units at 10,001. Next best station lists
      **500**, so treat other stations as noise.
- [ ] arneb is **22 jumps from haven**. Batch this — one deep buy, not repeated trips.
- [ ] Budget: full shortfall at market ≈ **36M cr**. Every crystal mined saves 10,001.
- [ ] ⚠️ Given 1b, plan for buying the **majority**, not the remainder, until a fresh
      survey says otherwise. 615 units is the only mining supply we can currently evidence.

---

## Phase 2 — Consolidate to grand_exchange (the main event)

**This is the campaign's real work.** Materials sit across ~30 holders per base; crafting
escrows only from the crafting agent's own storage at the station it is docked at.

### 2a. Establish what must actually move

Most of the campaign is already at the hub. Run this to get the live gap — do not work
from the spec's figures, which age:

```sql
sqlite3 data/assets.db "
  SELECT item_id,
         CAST(SUM(CASE WHEN base_id='grand_exchange_station' THEN quantity END) AS INT) at_hub,
         CAST(SUM(quantity) AS INT) fleetwide,
         COUNT(DISTINCT player_id) holders
  FROM agent_storage_items
  WHERE item_id IN ('silicon_ore','iron_ore','titanium_ore','carbon_ore','energy_crystal',
                    'reinforced_glass','flex_polymer','circuit_board','steel_plate',
                    'titanium_alloy','nanoplastic_composite','micro_thruster_array',
                    'drone_chassis','laser_focus_array','optical_fiber_bundle','xenon_gas')
  GROUP BY item_id ORDER BY fleetwide DESC;"
```

Known-local at time of writing (no movement needed): silicon 63,767 · iron 37,489 ·
flex_polymer 8,886 · reinforced_glass 8,711 · xenon 297.

Known-elsewhere (must move): titanium_alloy 3,100 · titanium_ore 22,967 · steel_plate
(19,548 fleetwide, only 1,289 at hub) · circuit_board (2,314 fleetwide, 196 at hub) ·
carbon_ore (173,288 fleetwide, 296 at hub — 119,659 of it at `gold_run_extraction_hub`,
**2 jumps away**).

### 2b. Choose a movement mechanism

Three exist. They are not equivalent:

| mechanism | how | when to use |
|---|---|---|
| **`dispatch` (Executor B)** | `play_as … dispatch <plan.json> --budget=N`; the plans Runner assigns haul nodes across the craft fleet | bulk, many items, many sources — the intended path |
| **Freight contracts** | `shipping` — post a contract, a carrier moves it | long legs where a hauler's round trip is not worth it |
| **Hand `send_gift`** | sender must be **DOCKED**; recipient is the **game username**, not the agent id; **cargo-only** | small, targeted top-ups |

- [ ] ⚠️ **Before dispatching anything through Executor B**, fix its stale-inventory read
      (blockers plan). Its planner reads `storage_snapshots`, last captured **2026-07-02**;
      it will subtract stock we no longer have and stock we have gained since.
- [ ] The `Deliver` worker verb (`pkg/worker/deliver.go`) moves item×qty from a worker's
      own storage at FROM to a recipient at TO in cargo-sized batches. It short-delivers
      rather than looping when the source is exhausted — check its note in `d.Out`.
- [ ] **The carbon leg is cheap: gold_run → haven is 2 jumps.** If the diamond optics path
      is ever revisited, this is why it is not the blocker.

### 2c. Move it

- [ ] Consolidate onto **craftsman-2** (stationed at grand_exchange) as the primary
      crafting agent; the other 8 craftsmen shard wave work from their own stations
- [ ] Re-run the 2a query after each batch and stop when the gap closes — do not move
      material speculatively; hold space at the hub is finite
- [ ] ⚠️ Deposit accounting was wrong until `115690c9` (today) — the client reported
      182/100 for an empty hold and `cargoFree` could go negative, silently refusing to
      load. Confirm the fleet binary postdates that commit before trusting a hauler that
      claims to be full.

---

## Phase 3 — Craft waves

5,679 runs. All hand-craftable except 491 `extrude_nanoplastic` at our new facility.

### 3a. Wave 1 by hand — this is a measurement, not just work

The single most valuable unknown: **is `craft jobs=[...]` one mutation, or N ticks?**
Everything downstream is sized by the answer.

- [ ] Pick the smallest independent wave: `extract_xenon 36` (hand, no dependencies)
- [ ] Submit it as a bulk job and **time it**:
```
craft --file=/tmp/wave.json
```
      with `[{"recipe_id":"extract_xenon","quantity":36}]`
- [ ] `craft queue` immediately after — record how many jobs appear and how fast they drain
- [ ] Record: wall-clock to completion, whether one mutation covered all 36 runs, and
      whether the queue accepted them all. **Write the answer into the spec** — every
      later wave is planned from it.

### 3b. Dependency order

Waves must respect the recipe DAG. Independent within a wave; finish a wave before the
next.

1. `spin_optical_fiber` 1,299 · `onboard_alloy_synthesis` 880 · `carbon_arc_circuit_etching` 54 · `extract_xenon` 36
2. `extrude_nanoplastic` 491 **(our facility — needs `--facility_id=b659c3602da933e414c9fa91a072968a`)** · `build_micro_thruster` 302
3. `assemble_laser_focus_array` 750 · `build_drone_chassis` 892
4. `build_mining_drone` 800 · `build_advanced_drone_bay` 175

- [ ] ⚠️ `circuit_board` has **zero margin** — 2,314 needed against 2,314 held, before
      Phase 0's control_node crafting took ~46. Expect to run
      `carbon_arc_circuit_etching` beyond the 54 planned.
- [ ] Shard each wave across the 9 craftsmen. Crafting is 1 mutation per tick per agent,
      so 9 agents is a 9× ceiling on throughput.

---

## Phase 4 — blocked

Fitting and distribution cannot be sized until fitted-module capture exists. 153 of 158
hulls *could* take a bay on hull capacity, but we cannot see what already occupies those
utility slots. See the blockers plan.
