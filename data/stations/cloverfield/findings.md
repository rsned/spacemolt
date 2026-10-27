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

- 2026-10-26 (tick 2237437) · **measured** · `facility faction_build stellar_collector_grid`
  → **Stellar Collector Grid**, facility_id `09960be12c023698746990244760a86d`,
  `under_construction: true`, `rent_per_cycle: 0`, again 750 corporation_management XP to
  each of 38 members. Catalog: 900,000 cr + 2,000 steel_plate + 1,200 circuit_board, build
  time 250, power supply 20, upkeep 2 circuit_board/cycle. Faction storage held only ~798
  steel_plate just before this — so where the other ~1,200 plate came from (builder's cargo?
  personal storage?) is **still to measure**.
- 2026-10-26 · **measured** · Stellar Collector Grid **finished**. `get_base` now carries a
  **`power` block** (absent on the bare station): `{"supply":20, "current_draw":0,
  "battery_capacity":0, "battery_stored":0, "efficiency":1}` — supply 20 matches the catalog.
  `condition: thriving`, `satisfied_count 2 / total_service_infra 2` (Locker Bay + Grid: infra
  facilities count toward station health too), `services: ["storage"]`. Base: hull 60,000,
  shield 12,000, armor 15, fuel 0/200, weapon_dps 0.
- 2026-10-26 (tick 2237416) · **measured** · `facility faction_build faction_fuel_bunker` is
  **refused** on your own station: `build_failed: a private Faction Fuel Bunker doesn't make
  sense on your own station — its fuel would just sit unused. Build a Fuel Tank Expansion
  (faction_build facility_type=fuel_tank_expansion) to grow the station's own shared tank
  instead`. See "Fuel" below.
- 2026-10-26T21:39Z · **measured** · `facility faction_list` shows **4 active** facilities:
  Stellar Collector Grid, Locker Bay, **Faction Workshop** (`bf83e1d9789f70078f65f75c16f7bee9`)
  and **Refueling Dock** (`90901d6f05756e5e0a588dbadc22c8e3`), all level 1, rent 0. Catalog
  cost of each: 500,000 cr + 800 steel_plate + 400 circuit_board (workshop) / 2,000 fuel_cell
  (dock). Faction storage: treasury 1,804,097 cr, 24 item types. `faction_rent`:
  total 0/cycle, **`grace_cycles: 260`**. The rent note says rent pauses while a facility is
  *"damaged, under construction, or dismantling"* — so **dismantling exists** as a state
  (the command itself is not yet tried). The dock is built but cannot work until a Fuel Tank
  Expansion exists (see Fuel).

- 2026-10-27 (tick 2245869) · **measured** · **First dismantle.** The user ran
  `facility action=faction_dismantle` on CRFT's **Iron Refinery** (`f9ef7a1940efe4484fefb2e393209a53`)
  at `grand_exchange_station`. Reply: 28 labeled packages, 231 ticks (complete_tick 2246100).
  `materials_to_package` = 250 control_node + 2,510 copper_piping, **100% of the catalog
  build_materials**. Credits (catalog build_cost 89,000) are not mentioned. The facility is
  offline immediately and vanishes when done. In-progress repair is cancelled without refund.
  Packages arrive in faction storage. *"Move the packages normally, unpack them at the
  destination, then rebuild from the recovered materials."* Items stay withdrawable after
  the storage facility itself is dismantled. This is the start of moving the rented NPC-station
  facilities (~500k cr/day rent) to Conjunction Junction.

- 2026-10-27 · **measured** · **Atmospheric Recycler live.** `get_base` gains a `life_support`
  block: `supply 12, demand 0, plants 1`, `maintenance` = 1 catalytic_scrubber_cartridge,
  2 oxygen_gas, 1 purified_water, 1 sunspindle, 3 water_ice per cycle (matches the catalog),
  and **`maintenance_cycle_ticks: 100`**.

- 2026-10-27 · **measured** · **Recycler starved → recovered.** `get_base` 08:20Z (tick 2245484) and
  08:47Z (2245647): `operational`, 4/5, `life_support.starved` + remediation *"A life support plant is
  short of upkeep. Deposit these into this station's faction storage — the plants need this much on
  hand, and draw it down slowly."* Faction storage then had sunspindle 100, cartridges 45, purified
  water 100, water ice 19k+, oxygen_gas **0** (4 arrived by tick 2245642). At 08:58Z (2245702):
  **thriving 5/5**, no `starved`, and oxygen 4 → **3** with nothing else consumed. So: (a) a restocked
  plant only recovers at its next maintenance check (here within ≤60 ticks of the deposit read, ≥5 after
  the starved read); (b) that check took **1** oxygen alone — looks like paying the shortfall of the
  unaffordable cycle (user's catch-up reading), not a full cycle; (c) the `starved` list names **every**
  upkeep input, not just the missing one — read storage to find the gap.

## Maintenance cycle: ~3,000 ticks measured, despite `maintenance_cycle_ticks: 100`

Faction storage at Conjunction Junction (events.jsonl `view_faction_storage`), one unit = one
cycle of that facility's upkeep. Windows are (last old reading, first new reading]:

| facility (item) | drop 1 | drop 2 | drop 3 | gap |
|---|---|---|---|---|
| locker_bay (flex_polymer 100→97) | (2237826, 2240207] | (2241748, 2242301] | (2243446, 2245235] | 1,541–4,475; 1,145–3,487 |
| faction_workshop (focused_crystal 100→98) | (2241574, 2241635] | (2243446, 2245235] | | 1,811–3,661 |
| refueling_dock (liquid_hydrogen 400→398) | (2241635, 2241748] | (2243446, 2245235] | | 1,698–3,600 |
| refueling_dock (fuel_cell 1000→998) | (2242301, 2243446] | | | one cycle = 2 |

Every gap fits ~3,000 ticks; a 100-tick cycle would have consumed ~30× more. Each facility
runs its own phase (from its build). So the life_support block's `maintenance_cycle_ticks: 100`
is **not** the consumption cycle — most likely the 100-tick rent-billing clock players report.
Keep 3,000 as the working figure; tighten it with closer-spaced storage reads.

## Fuel (measured + catalog, 2026-10-26)

- **Faction Fuel Bunker cannot be built at your own station** (server refusal above). Its
  whole family (`faction_fuel_bunker` / `faction_large_fuel_bunker` /
  `faction_capital_fuel_bunker`) is for NPC stations.
- **Fuel Tank Expansion** (`fuel_tank_expansion`, infrastructure, `player_station_buildable`):
  500,000 cr + 8,000 steel_plate + 1,500 ceramite_plating + 800 fuel_tank; build 100; **+50,000
  fuel capacity** each ("build more for more capacity"); upkeep 5 steel_plate/cycle. Fill it
  from fuel-production facilities or by depositing a docked ship's fuel (storage deposit,
  `item_id=fuel`).
- **Refueling Dock** (`refueling_dock`, service `refuel`, `player_station_buildable`):
  500,000 cr + 800 steel_plate + 2,000 fuel_cell; build 80; own 5,000-fuel tank; upkeep
  2 fuel_cell + 1 liquid_hydrogen/cycle. *Faction members draw from the shared tank for free;
  everyone else pays station price* — this is the facility that **sells** fuel, the crossroads
  refuel business. First report (user, 2026-10-26): the dock "doesn't work without a tank
  expansion". **Revised** (LT1428's hint, confirmed by `get_base` ~22:15Z): the dock is
  **unmaintained** — faction storage held 0 fuel_cell (liquid_hydrogen 400), `condition:
  operational` "some facilities show signs of deferred maintenance", `satisfied_count 3 /
  total_service_infra 4`, `services: ["crafting","storage"]` (no `refuel`), `fuel_price 20`.
  **Resolved 2026-10-27:** 1,000 fuel_cell faction-deposited at tick 2242294 (23:26Z); by
  tick 2242363 (00:42Z) `get_base` read `thriving`, 100%, 4/4, `services: crafting, refuel,
  storage`. **No expansion needed**: the dock brings its own tank (`max_fuel` 200 → 5,000 since
  the dock was built). Refuel at 00:42Z → `station_fuel_empty: This station's fuel reserves are
  depleted. Buy fuel cells from the market and use them directly.` — the tank must be filled
  (tanker storage deposit `item_id=fuel`).
- **Fuel production checks** (h2_fuel_combustor): at 21:37Z (dock unpaid) → `no_fuel_bunker`;
  at 00:43Z, tick 2242754 (dock paid) → `no_life_support: This station has no life support yet.
  Build a life support facility (faction_build) before adding facilities that draw life
  support.` Fuel production draws life support → atmospheric_recycler before any production
  line. **Correction (SpaceMolt Bug Bot, 2026-10-27):** the `no_fuel_bunker` refusal was NOT
  about the unpaid dock — it is a server timing bug: for up to ~17 min after a refueling dock
  finishes, the station doesn't count the dock's 5,000 tank, so fuel producers *and fuel
  deposits* are told to build an expansion. Being fixed; retry after the window. The dock's
  tank is enough — no expansion needed. Also: `fuel_depot` is the empire-station version;
  `refueling_dock` is the player-station one.
- **Fuel production needs tank capacity first** (measured, tick 2241639): `faction_build
  h2_fuel_combustor` → `no_fuel_bunker: This station needs fuel tank capacity before building
  fuel production facilities. Use 'faction_build' with facility_type='fuel_tank_expansion'
  first.` (Before the dock was paid; see the resolution above.)
- Our miners at the station currently get `no_fuel_source … insufficient credits for station
  refueling` (misleading: there is simply no refuel service yet).

## What can be built on a player station

The facility catalog carries **`player_station_buildable: true`** on **63 of 2,907**
facilities (catalog 2026-10-26). That flag, not the official guide, is the list to plan
from: it includes locker_bay, faction_workshop, atmospheric_recycler, the power plants
(stellar_collector_grid, fuel_cell_generator, bunker_fed_reactor, scrap_reactor,
dark_matter_reactor), fuel_tank_expansion, refueling_dock, trade_concourse, repair_slip,
outfitting_bay, the defense line (point_defense_battery … siege_lance), and food/leisure
venues. `faction_*` facilities (e.g. the fuel bunkers) are NOT flagged.

## Target service set (user, 2026-10-26)

A full station lists 11 `services`: crafting, marine_training, market, medical, missions,
personnel, refuel, repair, salvage_yard, shipyard, storage. Every one has a level-1
`player_station_buildable` facility at **500,000 cr** (catalog 2026-10-26):

| Service | Level 1 | Upgrades (L2 → L4) | Upkeep / cycle |
|---|---|---|---|
| storage | locker_bay | — | 1 steel_plate, 1 flex_polymer |
| crafting | faction_workshop | — | 1 circuit_board, 1 focused_crystal |
| refuel | refueling_dock | — | 2 fuel_cell, 1 liquid_hydrogen |
| market | trade_concourse | — | 1 trade_authenticator |
| repair | repair_slip | — | 1 repair_kit, 1 steel_plate |
| missions | contract_terminal | — | 1 circuit_board, 1 copper_wiring |
| salvage_yard | reclamation_dock | — | 1 steel_plate, 1 fuel_cell |
| shipyard | outfitting_bay | fabrication_yard → assembly_complex → grand_shipworks | 2 steel_plate, 1 titanium_alloy |
| medical | first_aid_ward | trauma_clinic → surgical_center → regenerative_medical_complex | none listed |
| personnel | crew_registry | personnel_bureau → sector_labor_exchange → fleet_personnel_command | none listed |
| marine_training | security_training_post | marine_training_barracks → fleet_marine_academy → expeditionary_warfare_command | none listed |

All 11 at level 1 = 5.5M cr. Founding + power + life support + fuel storage + all 11 = 12.5M cr
(KB guide sidebar). Upkeep inputs to source: steel_plate, flex_polymer, focused_crystal,
fuel_cell, liquid_hydrogen, trade_authenticator, repair_kit, circuit_board, copper_wiring,
titanium_alloy.

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

**Two different cycles (user, 2026-10-26):**
- **Facility rent cycle = 100 ticks** (≈ 16.7 min, 86.4/day). This is what `rent_per_cycle` bills on.
- **Station maintenance cycle ≈ 3,000 ticks** (≈ 8.3 h, **≈ 2.9 cycles/day**). This is how often
  facilities consume their `maintenance_inputs`.
- Still open: Grand Exchange reports `life_support.maintenance_cycle_ticks: 100` (*measured*), so
  life support may run on its own 100-tick cycle. Confirm at our own station once life support exists.
- **First consumption seen (measured 2026-10-26):** faction steel_plate at Conjunction Junction read
  **799** in the CRFT capture at 08:33Z and **798** in craftsman-1's `view_faction_storage` at ~09:25Z,
  which is one Locker Bay cycle (1 steel_plate). No flex_polymer was in storage before the user stocked
  100, so no flex reading to compare yet. Stock seen at ~09:25Z: circuit_board 2000, flex_polymer 100,
  focused_crystal 100, iron_ore 46 (first pipeline deposit by miningbot_0019 at 02:20:50 PDT = 09:20:50Z),
  steel_plate 798. To bracket the cycle length: re-read and note the tick each time steel_plate and
  flex_polymer drop together.
- ⚠ The catalog upkeep table below and the KB guide still multiply by 86.4/day. At ~2.9/day the
  real daily upkeep is **~30× lower** (Locker Bay ≈ 3 steel + 3 flex polymer a day; Workshop ≈ 3
  focused crystals ≈ 12 energy crystals a day). To measure it: deposit an exact count, note the
  tick, and re-read `view_faction_storage` after 3,000+ ticks.

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
