# CJ station console (HMI) — design

Date: 2026-11-08. Status: approved in conversation, ready for a plan.
This is project #3 from `2026-10-25-cloverfield-home-station-design.md` ("SCADA/HMI-style
station console"), now that CJ has enough facilities that its state is worth watching.

## Intent

A browser tab the operator leaves open in the background and glances at. The first thing
it says is CJ's **overall state** (THRIVING / STRAINED / CRITICAL) and the health bars for
the main stats. Then the operator can "zoom in" on any area of the station and get a
detailed popup. It should feel like a sci-fi command console: a slowly rotating holographic
station whose parts change colour as the systems they stand for wear down.

Two text companions in play_as get the same grouping, for when the operator is at the terminal.

## Goals

1. **Glance view.** Status word, station name/system, time of last reading, and health
   bars: hull, shield, fuel, power, life support, satisfaction.
2. **Hologram.** The station's 3D mesh rendered as a cyan wireframe hologram that rotates
   slowly; drag to orbit, scroll to zoom.
   - **Shield bubble(s):** one shell per shield module. Colour goes cyan → yellow → red as
     shield % drops; fades as it wears; flickers below ~25%.
   - **Hull damage:** scorched patches that flicker orange/red, proportional to hull damage;
     the rest dims.
   - **Zones:** each system owns a region of the model and takes its state colour
     (cyan ok, yellow strained, red critical). Examples the operator gave: life support
     strained → its section yellow; battery 0 → its part red; all production lines
     backlogged → production area yellow; power draw near critical → core bright red.
   - **Labels:** every zone has a floating label with a leader line and a one-line reading.
     Dim + dashed when fine, bright + solid in its state colour when not. Hover a label →
     that zone brightens and the rest of the station dims. Click → detail popup.
3. **Popups**, one per zone: the facilities in it (name, level, status, maintenance
   satisfied), the zone's own figures, and **trend lines over the last ~48 hours** of
   hourly readings for the zone's key numbers.
4. **Model switcher** over the operator's nine picks, **default `player__wheel__capital`**.
5. **Concept-art badge:** the chosen model's keyed concept art as a small static
   thumbnail (corner badge) — the operator liked the static view as a summary.
6. **Hourly data capture** feeding all of the above (below).
7. **play_as formatters** for `get_base` and `list_station_passengers`.

## Non-goals (for now)

- Other stations (CJ only). Live/raid-time updates — hourly is enough for now.
- Driving the game from the console (it is read-only).
- Per-gun stats (the server only reports aggregate `weapon_dps` / `weapon_reach`).

## Anti-goals

- A pretty picture that hides trouble: any zone that is strained must be obviously
  coloured **and** named (the operator needs to know which ring is fuel vs life support).
- Showing stale data as current: if the latest reading is more than ~2 hours old, the
  header says so.
- Breaking the fleet: capture runs inside miningbot_0000's existing schedule; it must not
  add logins or long play_as sessions.

## Zones (default mapping, shown on the wheel)

| Zone | Part of the model | Driven by |
|---|---|---|
| CORE · POWER | centre hub | power `current_draw` vs `supply`, battery stored/capacity, reactor facilities |
| LIFE SUPPORT | inner ring | life_support `supply` vs `demand`, recycler/plants maintenance |
| FUEL | spokes | base `fuel` / `max_fuel`, fuel tank, refueling dock, combustor |
| PRODUCTION | outer-rim arc | production facilities: `queued_runs`, `backlog_ticks`, `items_per_hour` |
| TOURISM | outer-rim arc | tourism revenue rate, lounge occupancy/capacity, demand level, venues |
| SHIPYARD | outer-rim arc | fabrication yard, ship garage, construction (`under_construction`) |
| DEFENSE | outer-rim arc | hull/shield/armor, `weapon_dps`, defense facilities |
| COMMERCE · ADMIN | outer-rim arc | everything else: `faction_office`, `storage_extension`, `hiring_board`, `locker_bay`, `trade_concourse`, `faction_workshop`, and any facility type not mapped to another zone (operator, 2026-11-08) |

Zones are assigned **by facility type** (a type→zone table), not by `category`.
`h2_fuel_combustor` is category production but belongs to FUEL; its queue counts toward
FUEL strain, not PRODUCTION's backlog rule (assistant's call). Unknown future types fall
into COMMERCE · ADMIN so no facility's trouble is ever uncoloured.

Whole hull = hull damage; bubble = shields. **Shield shells (assistant's call):** one
baseline shell for the station's built-in shield (CJ shows 12,000 with no shield facility),
plus one shell per `shield_projector` facility (defense round 1 adds two → three shells).
All shells share one colour from aggregate `shield/max_shield`; outer shells drop out first
as shield % falls. The meshes are single unsegmented blobs (one
mesh, ~40k triangles, no named parts), so zones are **geometric regions per model** (radial
bands, angular sectors, height bands) in a small per-model zone map, plus a label anchor
and label offset per zone. The wheel gets a hand-tuned map; the other eight get rough maps
first. A spike proved the approach: `.superpowers/brainstorm/149828-1794193505/content/hologram-zones-v2.html`
(three.js 0.160, GLTFLoader, EdgesGeometry at 20°, per-vertex colours, additive blending).

Thresholds (strained/critical) are the builder's call to start; the spike used: power
draw ≥70% strained, ≥90% critical; battery 0 critical (only when a battery exists); life
support supply/demand <110% strained, <100% critical; fuel <40% strained, <15% critical;
production strained when any line has a backlog, critical when all do; shield <60%
strained, <25% critical; hull <75% strained, <40% critical; any facility with
`maintenance_satisfied:false` makes its zone at least strained; tourism strained when
`demand_level` is not normal or `market_conditions` says the economy is struggling.
The status word is the worst zone.

## Models

From `~/spacemolt/kb/data/mesh_bakeoff/out-stations/<name>/` (each has `mesh.glb`,
`keyed.png` 1024² RGBA, `view_side/td/tq.png`). Read from there, not copied into this repo.

- `player__wheel__capital_s5521` (default)
- `nebula__drum__moderate_s5230`
- `solarian__spindle__moderate_s5060`
- `crimson__ring__moderate_s5310`, `crimson__wheel__moderate_s5320` (operator said "ring or wheel"; offer both)
- `voidborn__pylon_toroid__moderate_s5440`
- `voidborn__spindle__capital_s5461`
- `player__drum__capital_s5531`, `player__terraces__capital_s5551`

## Data sources (field names from recorded replies)

All three are read hourly by **miningbot_0000** (CRFT Officer, docked at CJ) via its
schedule, and stored in `data/assets.db`.

1. **`get_base`** — already captured by `capture_base` (`pkg/assets/capture_base.go`,
   table `base_snapshots`), currently daily (schedule id 51). Change to hourly. Fields:
   `base.{hull,max_hull,shield,max_shield,weapon_dps,weapon_reach,armor,fuel,max_fuel}`,
   `condition.{condition,satisfaction_pct}`, `tourism_revenue` (**lifetime counter** —
   show the hourly rate from deltas, e.g. 3,956 → 7,638 in ~90 min), `dining_points`,
   `leisure_points`, `life_support.{supply,demand}`,
   `power.{supply,current_draw,battery_capacity,battery_stored}`.
2. **`facility list --show_station_facilities`** — new capture. Top level: `base_id`,
   `faction_facilities[]`, `station_facilities[]`, `player_facilities[]`,
   `power{battery_capacity,battery_stored,current_draw,efficiency,supply}`,
   `life_support{supply,demand,plants,maintenance_cycle_ticks,maintenance[{item_id,name,quantity_per_cycle}]}`,
   `faction_rent{...}`. Each faction facility: `facility_id`, `type`, `name`,
   `custom_name`, `category` (production / faction / service / infrastructure / defense),
   `level`, `service`, `capacity`, `maintenance_level`, `maintenance_satisfied`,
   `maintenance_per_cycle`, `labor_per_cycle`, `leisure_points`, `tourism_upkeep`,
   `recipe_id`, and for production lines
   `production{recipe,queued_runs,queued_items,backlog_ticks,items_per_hour,output_per_run,ticks_per_run,public,rental_fee_per_run}`.
   Facilities under construction come from `faction_list` (`status`, `under_construction`);
   the builder checks whether `facility list` carries a status too.
3. **`list_station_passengers`** — new capture. `count`, `demand_level` (e.g.
   "discounted"), `fare_surge` (0.6), `market_conditions` (free text, quoted verbatim in
   the tourism popup), `station`, `transit_lounge{capacity,lounge,occupancy,passengers[]}`,
   `waiting[]`.

Storage layout is the builder's call (typed columns where the console needs history,
raw JSON alongside is fine). Captures follow the `CaptureBase` rule: a failed read never
fails the capture pass.

## Where it lives (assistant's call, not objected to)

- A new Go command, `cmd/tools/station-console`, serving one page on **:8094** (free;
  :8093 is the line dashboard). Reads `data/assets.db`; serves meshes/images from the KB
  path (flag, default `~/spacemolt/kb/data/mesh_bakeoff/out-stations`). The page polls
  for new readings; three.js loaded from a pinned CDN version like the spike.
- Added to `docs/COLD_START.md` so it comes back after reboots.

## play_as formatters

- `get_base` (`cmd/tools/play_as/base_format.go`, today only a Tourism line + JSON dump):
  sectioned readout — status/condition, hull/shield/armor, defense, power, life support,
  fuel, tourism — each with a bar and ok/strained/critical colouring using the same
  thresholds as the console. When a cached `facility list` reply exists, append the
  production queues and any unmet maintenance.
- `list_station_passengers` (today "No passengers waiting at …"): lounge
  occupancy/capacity, demand level and fare multiplier, the market-conditions sentence,
  and the waiting list.

## Left to the builder

Thresholds, storage schema, polling interval, popup layout, per-model zone maps for the
eight non-default models, page styling within the sci-fi console look of the spike, and
how thresholds are shared between Go (formatter) and JS (console).
