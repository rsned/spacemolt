# Cloverfield station: findings

Each entry: date · **measured** | **doc-quoted** | **player-reported** (unverified) · the fact · evidence
(an events.jsonl snapshot_id / tick, or a doc URL).

## Station command reference

**Source:** **doc-quoted** from the server's own `get_commands` entry for `station`
(cached at `data/game-api/20261018/get_commands.json`, category `base`). Rows
marked ✅ have been run live at Conjunction Junction. Every other row is
untested until it gets a ✅ and a date.

**Rules (server notes):**
- You must be **docked** at a station or outpost your faction owns.
- `info` is open to any member. Every other action needs **ManageBases** (leader/officer).
- Outposts support only `info`, `set_name`, and `set_description`.
  The rest are station-only.
- `set_contraband` / `reset_contraband` need a player-owned station or mobile base.

**play_as syntax:** `station <action> --param value` (since `04ab5c3b` the first word
is sent as `action`). Raw form: `{"type":"station","payload":{"action":"…", …}}`.

> play_as types the payload (since the station-payload commit): `public`,
> `allow_outsiders`, `auto_buy_fuel` go out as JSON booleans (bare `--public` = true);
> `fee_percent` and `price` as numbers; names and ids always as strings;
> `contraband_items` as an array (comma list, `["a","b"]`, or `@file`), and
> `--contraband_items "[]"` sends an **empty array** (allows all), never null.

| Action | Params | play_as example | Raw payload | What it does |
|---|---|---|---|---|
| `info` ✅ 2026-10-25 | — | `station info` | `{"action":"info"}` | Current config: name, description, public_access, allow_outsider_facilities, auto_buy_fuel, contraband_mode/items, service_access (once services exist). |
| `set_name` | `name` | `station set_name --name "Conjunction Junction"` | `{"action":"set_name","name":"Conjunction Junction"}` | Rename the station. |
| `set_description` ✅ 2026-10-25 | `description` (≤500 chars) | `station set_description --description "CRFT home. Crossroads of Haven and the Rim."` | `{"action":"set_description","description":"…"}` | Station blurb shown on dock. |
| `set_public` ✅ reply seen 2026-10-25 | `public` true/false | `station set_public true` | `{"action":"set_public","public":true}` | When false, only the owning faction, allowed factions and allowed players may dock. |
| `set_build_policy` | `allow_outsiders` true/false | `station set_build_policy --allow_outsiders true` | `{"action":"set_build_policy","allow_outsiders":true}` | Let non-members build their own facilities. Tenants pay rent each cycle to our treasury. |
| `set_service_access` | `service` = market\|refuel\|repair\|shipyard\|crafting\|salvage_yard\|missions, `access` = public\|allies\|faction | `station set_service_access --service refuel --access public` | `{"action":"set_service_access","service":"refuel","access":"public"}` | Gate one service to the faction, allies, or everyone. |
| `set_market_fee` | `fee_percent` 0–10 | `station set_market_fee --fee_percent 3` | `{"action":"set_market_fee","fee_percent":3}` | Listing fee outside traders pay; goes to our treasury. |
| `set_refuel_price` | `price` (per fuel unit) | `station set_refuel_price --price 12` | `{"action":"set_refuel_price","price":12}` | What outside pilots pay per unit of fuel; to treasury. |
| `set_repair_price` | `price` (per hull point) | `station set_repair_price --price 4` | `{"action":"set_repair_price","price":4}` | What outside pilots pay per hull point repaired; to treasury. |
| `set_auto_buy_fuel` | `auto_buy_fuel` true/false | `station set_auto_buy_fuel --auto_buy_fuel true` | `{"action":"set_auto_buy_fuel","auto_buy_fuel":true}` | Off by default. When on, any docked pilot can sell fuel from their tank into our shared tank at live scarcity prices, paid from the treasury (capped to what it can afford). |
| `set_contraband` | `contraband_items` (array of item ids) | `station set_contraband --contraband_items pirate_moonshine,void_spice` | `{"action":"set_contraband","contraband_items":["pirate_moonshine"]}` | Replaces this station market's contraband (banned) list. `[]` allows **all** items. Smuggling skill bypass still applies; empire customs unchanged. |
| `reset_contraband` | — | `station reset_contraband` | `{"action":"reset_contraband"}` | Back to the default contraband rules. |
| `allow_player` | `player` (id or username) | `station allow_player --player marketbot_home` | `{"action":"allow_player","player":"marketbot_home"}` | Let a non-member dock while the station is private. |
| `remove_player` | `player` | `station remove_player --player marketbot_home` | `{"action":"remove_player","player":"marketbot_home"}` | Take a player off the allow list. |
| `ban` | `player` | `station ban --player <username>` | `{"action":"ban","player":"<username>"}` | Blocks docking immediately and drops them from the allow list. |
| `unban` | `player` | `station unban --player <username>` | `{"action":"unban","player":"<username>"}` | Lift a ban. |
| `allow_faction` | `faction` (id) | `station allow_faction --faction 6686e24354619eadffc738b607425457` | `{"action":"allow_faction","faction":"6686e…"}` | Let a whole faction dock while private (example id: Explorers Guild, a CRFT ally). |
| `remove_faction` | `faction` (id) | `station remove_faction --faction 6686e24354619eadffc738b607425457` | `{"action":"remove_faction","faction":"6686e…"}` | Remove that faction's access. |

- 2026-10-25 · **measured** (tick 2234173) · `set_public` replies `{"command":"station","result":{…full
  station config…, "message":"Station is now private — only the owning faction, allowed
  factions, and allowed players may dock."}}`. ⚠ **A missing `public` is read as `false`,
  not refused.** `station set_public true` from an older play_as dropped the bare `true`,
  and the server made (kept) the station private. play_as now maps bare values to each
  action's parameter and refuses to send a setter with its value missing.

- 2026-10-25 · **measured** (tick 2234224) · `station set_public true` → `public_access: true`,
  *"Station is now public — any pilot may dock."* Description set to *"Our first tentative
  steps to the stars"*. `allow_outsider_facilities` stays false (only docking opened, not
  building). A public station means non-members such as marketbot_home can dock without
  joining CRFT.

Existing market orders stay active after a contraband or access change; new and
modified orders follow the current policy.

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
  facility still has to be built before anything else. **Settled 23:43Z: the
  built-in storage counts.** `faction_build locker_bay` succeeded with no separate
  storage facility.
- 2026-10-26 · **measured** · ⚠ After the Locker Bay finished, `get_base` shows the station
  **`critical`**: *"Critical infrastructure failure. Emergency lighting only. Station barely
  functional."* `total_service_infra` 1, `satisfied_count` 0, `satisfaction_pct` 0, and
  **`services: []`**, so the Locker Bay is built but delivers nothing. Its catalog upkeep is
  1 steel_plate + 1 flex_polymer per cycle. The faction storage had ~800 steel and **no flex
  polymer**. Lesson for the guide: **stock every upkeep input before the build finishes.**
  One unsatisfied facility takes the whole station from `thriving` to `critical`.
- 2026-10-26 · **measured** · `get_base` station stats: hull 60,000/60,000, shield
  12,000/12,000, armor 15 (matches the docs' stock station), **fuel 0 / max 200**,
  weapon_dps 0, weapon_reach 0, `type: station`, owner_id `a50924913cef881c5e4d14257589d9ba`.
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

## Build log

- 2026-10-25 23:43Z (tick 2233808) · **measured** · `facility faction_build locker_bay`
  → **Locker Bay**, facility_id `ccebca67f80b05ac66e9fef340579706`,
  `under_construction: true`, **`rent_per_cycle: 0`** (no rent at our own station,
  as documented), `faction_service: ""`. It awarded **750 corporation_management XP
  to each of 38 members** (`members_awarded_xp: 38`). The reply gives **no cost,
  materials, or build time**. Measure them from the storage and treasury
  difference (before: 2,400 steel_plate, treasury 1,290,929 at 22:41Z) and from
  `faction_list` while it builds.

## Maintenance (per facility, per cycle)

**Where to read it (measured 2026-10-26):**
- `facility list` → each `faction_facilities[]` entry carries **`maintenance_satisfied`** (bool)
  and **`maintenance_level`** (0–1; the Locker Bay read **0.4** at 02:03Z after running with no
  flex_polymer), and its `description` switches to the facility's *degraded* text.
- `facility types --facility_type <id>` → `maintenance_per_cycle`, the catalog's input list.
- `get_base` / docking → only the station roll-up (`condition`, `satisfied_count`,
  `total_service_infra`), not which items are missing.
- Once life support exists, `get_base` and `facility` replies carry a `life_support` block
  with `maintenance` (per-cycle items) and **`starved`** (items it's short of) + `remediation`.
- `faction_rent.grace_cycles: 260` at our own station (rent is 0 here anyway).

**Field shapes from a full grid (Grand Exchange `facility list`, measured 2026-10-26):**
- `power`: `{"supply":200, "current_draw":1, "battery_capacity":180, "battery_stored":180,
  "efficiency":1}`. `efficiency` < 1 is presumably the "undersupplied grid throttles the station"
  signal.
- `life_support`: `{"supply":132, "demand":102, "plants":1, "maintenance_cycle_ticks":100,
  "maintenance":[{"item_id":"honeyglobe","quantity_per_cycle":3},{"item_id":"water_ice",
  "quantity_per_cycle":6}]}` (`starved` / `remediation` only appear when short).
- Per facility: `maintenance_level` (0–1, e.g. Haven Grand Bazaar 0.838 unsatisfied, Fleet
  Personnel Command 0), `maintenance_satisfied`, and on some infra `maintenance_per_cycle`
  (`[{item_id,name,quantity}]`) plus **`labor_per_cycle`** (Haven Fleet Operations 700, Haven
  Promenade 30). That's the treasury labor charge.
- `construction.pending[]`: `{definition_id, build_cost, status:"waiting_for_materials",
  materials:[{item_id, quantity_required, quantity_in_storage, quantity_missing}]}`. This is the
  **station owner's** build queue. At Grand Exchange it's the Nebula empire's Synthesize
  Pan-Galactic Matrix Factory, not ours (corrected by the user). At our own station it should
  show our faction's builds, including any waiting for materials.
- **Why own a station, in one number:** CRFT's 11 facilities at Grand Exchange show
  `faction_rent.est_rent_per_day` **443,932** (5,162/cycle).

1 cycle = 100 ticks ≈ 16.7 min ≈ 86.4 cycles/day. ⚠ **OPEN CONFLICT (2026-10-26):**
the user reports the **station maintenance cycle is ~3,000 ticks** (≈ 8.3 h, ≈ 2.9 cycles/day).
The 100-tick figure comes from the *rent* cycle and from Grand Exchange's
`life_support.maintenance_cycle_ticks: 100` (measured). If service and infrastructure upkeep runs
every 3,000 ticks, every per-day upkeep figure here and in the KB guide is **~30× too high**.
Settle it by depositing an exact count of flex_polymer, noting the tick, and re-reading
`view_faction_storage` after 3,000+ ticks.

Source: **catalog-quoted** from `data/game-api/20261018/catalog_facilities.json`
(`maintenance_inputs`, `maintenance_fuel`). Per day = per cycle × 86.4. To be
confirmed **measured** from storage differences once each facility is running.

| Facility | Build (credits + materials) | Build time | Maintenance / cycle | ≈ / day | Supplies |
|---|---|---|---|---|---|
| Locker Bay (service: storage) | 500,000 + 1,600 steel_plate | 80 ticks | 1 steel_plate, 1 flex_polymer | 87 + 87 | personal storage |
| Faction Workshop (service: crafting) | 500,000 + 800 steel_plate + 400 circuit_board | 100 | 1 circuit_board, 1 focused_crystal | 87 + 87 | crafting |
| Atmospheric Recycler (infra) | 600,000 + 1,200 steel_plate + 200 water_recycler | 150 | 1 sunspindle, 3 water_ice, 1 purified_water, 2 oxygen_gas, 1 catalytic_scrubber_cartridge | 87 / 260 / 87 / 173 / 87 | life support 12 |
| Life Support System (infra) | 600,000 + 1,200 steel_plate + 200 water_recycler | 150 | — (none listed) | — | (no supply listed) |
| Stellar Collector Grid (infra) | 900,000 + 2,000 steel_plate + 1,200 circuit_board | 250 | 2 circuit_board | 173 | power 20 |
| Fuel Cell Generator (infra) | 500,000 + 2,000 steel_plate + 400 power_cell | 200 | 2 fuel_cell, 1 liquid_hydrogen | 173 + 87 | power 12 |
| Bunker-Fed Reactor (infra) | 500,000 + 2,000 steel_plate + 400 power_cell | 200 | 55 fuel | 4,752 fuel | power 12 |
| Scrap Reactor (infra) | 80,000 + 40 steel_plate + 30 salvage_metal | 120 | 2 fuel_cell, 2 salvage_metal | 173 + 173 | power 10 |

- **catalog-quoted** · Locker Bay, Faction Workshop and Atmospheric Recycler list
  **no `power_draw` or `life_support_draw`**, so services may run on a station with
  0 power. Production facilities do list draws (e.g. power 6, life support 1).
  Confirm with `facility list` power block after the locker bay finishes.
- **catalog-quoted** · Faction Workshop maintenance eats **focused_crystal**
  (recipe: 4 energy crystal + 1 palladium each), so about **346 energy crystals a day**.
  Energy crystals are reserved, so this needs a decision before building it.
- Cloverfield has a star, and the Stellar Collector Grid's only upkeep is 2
  circuit_board per cycle, the simplest power source to keep supplied.

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
