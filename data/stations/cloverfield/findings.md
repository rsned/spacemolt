# Cloverfield station: findings

Each entry: date · **measured** | **doc-quoted** | **player-reported** (unverified) · the fact · evidence
(an events.jsonl snapshot_id / tick, or a doc URL).

## What comes with a station

- 2026-10-25 · **measured** (server hint, tick 2232761) · The `build_base` reply
  tells you to *"add a Faction Storage facility, then power, life support, and
  the services you need"*. **This contradicts the factions guide**, which says a
  station comes with top-tier storage, market, refuel, repair, shipyard,
  crafting, power and life support. To confirm with the first `station_snapshot`
  (facility list, power block, services).
- 2026-10-25 · **measured** (events 22:12–23:35Z) · Faction storage **works on a bare
  station**: `faction_deposit_items steel_plate 1200` succeeded twice, and
  `view_faction_storage` at the station lists the steel. Personal `deposit_items`
  still fails with `no_storage`. Yet `facility faction_list` says *"Faction Storage
  is required before any other faction facility"* and lists no facilities. So
  either the built-in storage counts and that hint is generic, or a Faction Storage
  facility still has to be built before anything else. **Open, settle on the first
  faction_build.**
- 2026-10-25 · **measured** · Docking reply `station_condition`: `thriving`,
  *"No service facilities to maintain."*, `satisfaction_pct` 100,
  `total_service_infra` 0. This is where the maintenance status will show up.
- 2026-10-25 · **measured** · An unfiltered `facility types` returns a discovery
  index (`kind: "discovery"`): 2,910 types, of which defense 12/14 buildable,
  faction 24/56, infrastructure (power + life support) 28/103, personal 4/13,
  production 816/2,625, service 23/96, and ranch 3. Add `facility_type=<id>` for
  build materials and maintenance.
- 2026-10-25 · **player-reported** (LT1428, 15:12) · A new station comes with
  **500k faction storage** (per item type, so top tier, matching the factions
  guide), but **0 power and 0 life support**. Verify with `view_faction_storage`
  and `facility list` (power block).
- 2026-10-25 · **player-reported** (LT1428) · Best early build order: **locker bay
  → faction workshop → atmospheric recycler**, then fuel production can start.
  (Our guess at what each does: locker bay = personal storage service, faction
  workshop = crafting, atmospheric recycler = life support. Confirm each with
  `facility types`.)
- 2026-10-25 · **measured** · Right after founding, `view_storage` at the new station
  returns `no_storage`: *"This station does not offer storage. Use 'get_base' to
  see available services."* A fresh station has **no personal storage service**.
  (*Corrected below:* faction storage DOES exist; `view_storage` is personal storage.) `get_base` is the
  command the server points to for listing a station's services.
- 2026-10-25 · **measured** · `station --action info` (15:12 PDT) replies with
  `action: "info"`, `public_access: false`, `allow_outsider_facilities: false`,
  `auto_buy_fuel: false`, `contraband_mode: "default"`, `contraband_items: null`,
  and description *"A faction station founded by Crafting Collective."* So the
  founding faction is **CRFT**, and there is **no `service_access` map** on a
  station with no facilities.
- 2026-10-25 · **measured** (server hint) · *"Keep the station's faction storage
  stocked with each facility's maintenance inputs or those facilities go
  offline."* So maintenance runs **per facility**, and an empty station has
  nothing to go offline. The shutdown risk starts with the first facility.

## Founding

- 2026-10-25 · **measured** · Founded **Conjunction Junction** in Cloverfield at tick
  2232761 (13:49 PDT) by craftsman-1: `build_base --name "Conjunction Junction"`.
  Reply type `action_result`, about 10 s round trip (1 tick).
  - `base_id` **c976fa7df53c87f9616165d7d411b41a** · `poi_id`
    **862e07fa76571d0a76cab3cbaf4046e7** (dual-named, like every station).
  - `fee_paid`: **5,000,000**. `public_access`: false by default.
  - The station **creates its own new POI** ("anchored at its own new point of
    interest nearby"). It does not take over an existing planet, and you choose
    no POI. The founding ship ends up **docked** there automatically.
  - Station Core consumed? Not stated in the reply. Check craftsman-1's cargo.

## Maintenance (per facility, per cycle)

1 cycle = 100 ticks ≈ 16.7 min ≈ 86.4 cycles/day.

## Power & life support

## Defense

## Costs we paid

- 2026-10-25 · **measured** · Hauled **2,400 steel_plate** from Grand Exchange into
  station faction storage (2 runs × 1,200, Enterprise hull). Also started crafting
  400 circuit boards at GE (`carbon_arc_circuit_etching`).
- 2026-10-25 · **measured** · CRFT treasury per `view_faction_storage`: 1,338,377
  at 22:12Z, then **1,290,929 at 22:41Z** (−47,448 in 29 min, while rent is still
  paid at other stations).

## Doc contradictions

- 2026-10-25 · **doc-quoted** · The founding command differs between guides:
  [base-builder](https://spacemolt.com/docs/guides/base-builder) says
  `facility action=found_station name=… public_access=false`;
  [factions](https://spacemolt.com/docs/guides/factions) says `build_base`.
  **Settled 2026-10-25 (measured):** `build_base` with `name` works; the factions
  guide is right and base-builder's `found_station` is not what we used.
- 2026-10-25 · **doc-quoted** · Faction storage capacity: base-builder lists
  lockbox → stronghold as 500 / 2,000 / 10,000 / 50,000 *items*; factions lists
  100k / 200k / 300k / 500k *per item type*. (Our station is said to include
  top-tier storage free.)
