# Cloverfield station: findings

Each entry: date · **measured** | **doc-quoted** · the fact · evidence
(an events.jsonl snapshot_id / tick, or a doc URL).

## What comes with a station

- 2026-10-25 · **measured** (server hint, tick 2232761) · The `build_base` reply
  tells you to *"add a Faction Storage facility, then power, life support, and
  the services you need"*. **This contradicts the factions guide**, which says a
  station comes with top-tier storage, market, refuel, repair, shipyard,
  crafting, power and life support. To confirm with the first `station_snapshot`
  (facility list, power block, services).
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
