# Contraband production pipeline — design

Date: 2026-10-24. Status: draft for builder review.

## Intent

Turn the five contraband recipes we own at Voss Redoubt (Alhena) into supervised
production lines fed by 31 new miningbots, with a web dashboard that shows each
line as a chain of nodes (mine, purchase, haul, convert, produce, store, sell),
colours each node by throughput, and names the agents attached to it. The first
line is pirate moonshine. Later lines are switched on one at a time once the
first runs cleanly. The long-term purpose of the dashboard is to make node
reassignment automatic; version one only detects and displays.

Why now: contraband demand has risen 17–188x since 2026-10-17, we own the
facilities (the Voss Backwater Still is CRFT's own, rental fee 0), craftsman-8
and craftsman-10 are docked at Voss, and 31 fresh miningbots are idle.

## Goals

- Moonshine line running end to end with named agents on every node, and a
  measured units/day figure, within the first rollout.
- Learn where the line actually caps (inputs, hauling, facility sharing, or
  sales) — the operator does not expect 100% facility utilisation to be
  reachable and wants the line to show why.
- All 31 miningbots assigned to a resource from day one, so inputs for the
  later lines are stockpiling before those lines are switched on.
- Every miningbot put through the pirate reputation unlock so it can mine in
  Lawless space without being a pirate target, and can dock at Voss itself.
- A dashboard a human can read at a glance: one row per line on the top board,
  click-through to the node view.

## Non-goals (version one)

- Starshine. It consumes the reserved energy crystals
  (`project_drone_project_crystal_reserve`). It stays off the board.
- Automatic node reassignment. Version one raises the alerts; a human moves
  agents.
- Building our own conversion facilities (the chlorine vat etc.). Use the
  public ones and surface pre-emption as an alert.
- Stylised vats and conveyor visuals. Version one is boxes, links and colours
  like the reference screenshot; metaphor visuals come later.
- The pharmaceutical-lab branch. Culture is bought, not crafted.

## Anti-goals

- Never gift or craft away energy crystals. Consignment is explicit allow-list
  only, exactly as `ConsignRule` already enforces.
- Never send a bot into Lawless space before its pirate baseline is 10.
- Never strand a bot: every mining verb keeps the existing fuel floor, and bots
  start with a credit floor (below).
- Never cancel craftsman-1's vacuum bids; the dashboard must exclude them from
  "demand".

## Facts the design rests on (recon 2026-10-24)

### The still and the recipe

- `distill_moonshine`: 10 water_ice + 2 pharmaceutical_culture -> 6
  pirate_moonshine. Facility rate at Voss is **2.7 ticks per run** (the 13.5
  in the recipe table is the hand-craft time). 100% utilisation = ~3,200
  runs/day = 32,000 ice + 6,400 culture in, 19,200 moonshine out.
- The Voss Backwater Still is owned by CRFT (`e727c0e9…`), `public=1`, fee 0.
  Other players can use it; that is the "pre-empted" alert. Backwater Stills
  also exist at dross_citadel_station and mera_sanctum_station (fee 225).
- Craft batch size = the crafter's Crafting skill level
  (`MaxCraftBatchSize`), so the production node's run size is per-agent.

### Demand

- Genuine station bids for pirate_moonshine right now: **168 units** across 14
  stations at 5,133–6,654 (best: war_citadel 6,654, thane_keep 6,308).
- The 50,368-unit bid at Voss (and the other 50k walls there) is
  **craftsman-1's own vacuum bid** (`source=worker`). Exclude `source=worker`
  rows from any demand figure.
- Memory: the visible book understates real sell-through ~100x because station
  bids refill (craftsman-1 cleared 881 moonshine in one hour at 1,500 on
  2026-10-22). The real ceiling is unmeasured. The line will measure it.

### Inputs for moonshine

- Water ice: `glenhaven_ice_belt` (1 jump from Alhena; richness water_ice 67 /
  nitrogen_ice 38 = 64% water ice; 50,000 pool; regen ≈ 0.64 unit/tick ≈
  5,500/day) and `enif_ice_belt` (2 jumps; 56/41/23/13 = 42% water ice).
  Both systems are **Lawless** and have **no station**.
- Pharmaceutical culture: buy. sirius_observatory_station has 58,933 on offer
  at 7 cr (Sirius, high security, 21 jumps from Alhena); sol_central has 400 at
  35.
- Voss already holds 428 water_ice and 250 liquid_nitrogen in **craftsman-1's**
  storage (not craftsman-8's).

### Inputs for the later lines (stockpile now)

- Chlorine gas: `glenhaven_gas_cloud` (1 jump from Alhena, richness 23, 10,000
  pool). Conversion to chlorine_compound (`chlorine_neutralization_vat`) exists
  only at **starfall_salvage_station** (Starfall, frontier, 15 jumps from
  Alhena). Feeds gutter flux (8 chlorine_compound + 12 chlorine_gas -> 8).
- Titanium ore: `ashmark_iron_ti_reef` (9 jumps, richness 40, Lawless),
  `alniyat_debris_ring` (8 jumps, richness 20), `gliese_667_rutile_shoal`
  (8 jumps, richness 25, Lawless). Feeds ghost rounds via titanium_ingot.
- Plasma gas (hot cells): source not yet located. Builder finds it or the hot
  cell group waits.
- Energy crystal: nearest `luminous_heart_nebula` (Farpoint, 7 jumps). Off the
  board (starshine).

### The miningbots

- 31 agents, `miningbot_0000`–`miningbot_0030`, directories and credentials
  (with `player_id`) exist, none in any fleet, none running. CRFT members,
  send_gift unlocked.
- Hull: **Cobble** starter, scale 1, speed 2, base fuel 120 (jump fuel 2,
  jump time 5 ticks), cargo **175** with cargo_expander_iii fitted,
  mining_laser_iii fitted. An ice_harvester and a gas_harvester module sit in
  each bot's storage at its home station.
- Home: docked at `mobile_capital` in Distant Light (Outer Rim, low security),
  **17 jumps** from Alhena. Note the mobile capital moves.
- ~1,064 credits each. They earn nothing on the line (they gift their loads).
- **Pirate baseline −30.** They cannot dock at Voss and are pirate targets in
  Lawless space until unlocked.

### Drop stations

Nearest non-stronghold stations to the ice belts: `treasure_cache_trading_post`
(Treasure Cache, frontier, 3 jumps from Enif, 4 from Glenhaven; sells fuel; it
is also the pirate-unlock pin station) and `factory_belt_manufacturing_hub`
(Factory Belt, low security, same distances). Trader's Rest and Grand Exchange
are 1–2 jumps further.

### Movement verbs that exist today

- `mine_qty ITEM QTY TO RECIPIENT` — mine ITEM at the nearest known POI until
  QTY or full (bounded dry passes, fuel floor), then carry it to TO and gift it
  to RECIPIENT (or deposit if RECIPIENT is self). A gift lands in the
  recipient's storage **at the sender's station**.
- `deliver ITEM QTY FROM TO RECIPIENT` — withdraw from own storage at FROM,
  carry to TO, gift/deposit there, in hold-sized batches.
- `ConsignRule` (`consign: ["item:recipient:min[:keep]"]` in a fleet yaml) —
  on each idle pass, gift allow-listed goods from storage to a named hauler.
- Consigned routes (`withdraw_then_sell`) + the consign generator
  (`consign_offer`) are committed (`1ec0d254`, `60bdcb32`). Whether the live
  `arbitrage_opportunities` table has been rebuilt to admit them is unknown;
  builder verifies before relying on it for the sell leg.
- `send_gift source=storage` moves goods between two agents' storage at the
  same station with no cargo limit.
- The "hauler re-gifts to a crafter at the destination" relay (path 2 in
  `project_consignment_haul_relay`) was never built; `deliver` covers it for
  version one.

### Pirate unlock

- `unlock` role (idle `missions`), pinned at `treasure_cache_trading_post`,
  `mission_categories: [smuggling, delivery, exploration]`. The chain's
  `an_introduction` raises the baseline −30 -> 10 on all nine pirate factions.
- Nine craftsmen took 2026-10-13 -> 2026-10-22. The campaign memory says the
  mission **board rotation** at the giver is the real rate limiter, not
  travel, so adding 31 agents at once will not go 3x faster than 10.
- Graduation is detected from `assets.db` standings by the secondment
  reconciler (`fleet-secondment`), which returns the agent to its home fleet.
  The daemon runs `--home haul`; a sweep for another home fleet is a separate
  invocation (`ReconcileSecondments` skips entries whose `HomeFleet` differs).

### Measurement plumbing

- Storage capture (`capture_storage`) is **daily** on every role. Tank levels
  need it hourly on line agents.
- Worker heartbeats carry system/poi/credits every 10 s into the overmind log
  and `*-status.json`; status-file position is frozen for quarantined agents.
- `market.db consign_stock` (from `consign_offer`) is the only existing
  "stock available at station X" feed.

## Decisions

### D1. Allocation of the 31 bots (operator approved)

| group | bots | resource | site | drop station | line |
|---|---|---|---|---|---|
| ice | 4 | water_ice (+ nitrogen_ice byproduct) | 2 Glenhaven, 2 Enif | Treasure Cache | moonshine (live) |
| chlorine | 8 | chlorine_gas | Glenhaven gas cloud | Treasure Cache | gutter flux (stockpile) |
| titanium | 8 | titanium_ore | Ashmark reef (fallback Alniyat) | builder picks nearest non-stronghold station | ghost rounds (stockpile) |
| hot cells | 6 | plasma_gas | not located | — | held back |
| spare | 5 | — | — | — | reserve / replacements |

Moonshine stays at 4 until it shows where it caps; the yaml makes re-weighting
a one-line change per bot. Byproduct (nitrogen ice etc.) is gifted with the
load, never dumped.

### D2. Unlock first, in waves (operator: "prioritise the unlock")

All 31 bots go through the pirate unlock before mining in Lawless space. They
enter the unlock pool in three waves of ~10, ordered by line priority: wave 1 =
the 4 ice + 6 chlorine bots; wave 2 = the 8 titanium + 2 hot-cell; wave 3 =
the rest. The next wave is nominated as the previous wave's graduates return.
**My call:** waves rather than all 31 at once, because the giver's board
rotation is the rate limiter and 31 agents queueing on one board starves all
of them; the operator can override to "all at once".

While waiting for its wave, a bot does nothing in Lawless space. **My call:**
un-unlocked bots mine water ice at the nearest ice field in police_level ≥ 30
space reachable from Treasure Cache, dropping at Treasure Cache, so the ice
tank fills from day one. Builder finds the site; if none exists within ~6
jumps, waiting bots stay docked at Treasure Cache.

Bots are enrolled through the secondment ledger (`fleet-secondment --nominate`
with the new fleet as `--home` and `unlock` as `--away`) so graduation returns
them automatically. The daemon only sweeps `--home haul`; run a second sweep
for the new fleet (watch mode or cron). Both fleet yamls must list every bot
(the removed-sets rule).

Once unlocked, a bot drops directly at Voss (Glenhaven is 1 jump) and the
Treasure Cache leg disappears for that bot. The yaml `to`/`recipient` for the
bot changes from `treasure_cache_trading_post`/hauler to `voss_redoubt`/
`craftsman-8`.

### D3. One new fleet, one new role

- New overmind fleet `crft-mining` (`data/overmind/crft-mining-fleet.yaml`,
  overrides, status/history files, launched like the others with
  `--assets-db-path data/assets.db` and `--handoff-queue`). Roster: all 31
  bots, each with `role: pipeline_miner` and per-worker params: resource
  item, mining POI (system + poi), drop station, recipient agent id.
- New role `pipeline_miner` in `roles.yaml`: idle script = refuel, `mine_qty
  <item> <hold> <drop> <recipient>`, plus the hourly capture_profile /
  capture_fuel and an **hourly** capture_storage. **Builder's choice** whether
  the per-worker params ride on existing flags (`--station` etc.) or a new
  `--pipeline` flag; the constraint is that a worker reads them at boot and
  the overmind forwards them from the yaml.
- `mine_qty` picks "the nearest known POI" for the item; the yaml must pin the
  intended POI. If `mine_qty` cannot take an explicit POI today, add that
  (small change) rather than trusting nearest.
- The harvester modules in each bot's home storage are fitted before the first
  trip (ice_harvester for the ice group, gas_harvester for chlorine/plasma);
  titanium bots keep the mining laser. Builder confirms fitting is a one-off
  play_as/script step at Distant Light before the fleet leaves.

### D4. Haul links are named agents running `deliver`

Two pirate-unlocked haulers from the haul fleet are seconded to the line
(same secondment mechanism, home = haul, away = crft-mining):

- **shuttle hauler**: loops `deliver water_ice <qty> treasure_cache_trading_post
  voss_redoubt craftsman-8` (and the same for culture when it lands at
  Treasure Cache). It is the RECIPIENT on the ice bots' `mine_qty`, so the
  loads accumulate in its own storage at Treasure Cache.
- **long hauler**: runs the Purchase Node at Sirius (buy pharmaceutical_culture
  up to a target stock at ≤ 10 cr, carry to Voss, gift to craftsman-8) and the
  sell leg (withdraw moonshine from craftsman-8 via consigned routes, or a
  scheduled `deliver`-then-sell if the consigned-route table is not yet
  rebuilt).

**My call:** dedicated named haulers rather than the shared pool, so each
dashboard link has an agent on it. Hull choice is the builder's; a 1,900-hold
congregation makes the Sirius run a monthly trip.

### D5. Production node

craftsman-8 at Voss runs `distill_moonshine` at its maximum batch on a short
schedule (every 10 min is fine; the craft is 2.7 ticks/run), consuming only
what is in **its own** storage. The 428 ice + 250 liquid nitrogen in
craftsman-1's storage at Voss are gifted to craftsman-8 once by hand to seed
the first runs. craftsman-10 is reserved for the second line. Outputs stay in
craftsman-8's storage until the sell link withdraws them.

### D6. Money

The banker floors every bot at 10,000 before launch (fuel is 2/jump; a tank is
~360 cr) and the dashboard alerts on any line agent under 500. Haulers already
hold hundreds of thousands.

### D7. Dashboard, version one

- Web page served by a Go binary alongside the existing dashboards (reuse
  their serving pattern), reading only from files and DBs that already exist
  or that D3 adds: `assets.db` storage snapshots (hourly), `market.db`
  (bids, excluding `source=worker`; `consign_stock`), `*-status.json` and
  heartbeats (agent position, credits), the handoff queue, and the fleet
  yamls (who is assigned where).
- **Top board:** one row per production line: name, state colour, moonshine
  (or line output) units/day over the last 24 h, and the active alert text
  ("output stalled waiting on inputs", "market saturated", "facility busy",
  "hauler starved", "source depleted", "agent broke/stranded").
- **Line view** (click a row): the chain as boxes and links like the
  reference screenshot. Node types: Mine, Purchase, Haul link, Convert,
  Produce, Store, Sell. Each node shows its fill level (units in the relevant
  storage), the agents attached, and its state. Links are labelled "haul
  consigned" with the hauler's id and the last delivery time.
- **States:** optimal (green), starved (yellow: input below one batch),
  choked (red: output store growing while the next link has not moved it in
  N hours, or the facility reported busy). Thresholds are the builder's first
  guess, tuned on the live line.
- The per-node control in the screenshot (sliders) is **display only** in
  version one, showing bot count per node. Writing it back to the fleet yaml
  is the automation phase.
- Read-only. No auth needed beyond what the existing dashboards have.

### D8. Rollout order

1. Fund bots; fit harvesters; create fleet + role; launch wave 1 into unlock;
   non-wave bots start the secure-space ice fallback (or wait).
2. Seed craftsman-8; start its craft schedule; second the two haulers; long
   hauler does the first Sirius culture run.
3. Dashboard v1 with the moonshine line only; measure units/day for a week.
4. Switch on gutter flux (needs the Starfall convert node and craftsman-10),
   then ghost rounds, then hot cells if plasma gas is found.
5. Automation phase: alerts drive yaml edits.

## Left to the builder

- Exact `pipeline_miner` flag/param plumbing and the yaml schema for it.
- Whether `mine_qty` needs an explicit-POI argument (see D3).
- The secure-space ice fallback site, and the titanium drop station.
- Hauler hull choice and whether the sell leg uses consigned routes (verify the
  `arbitrage_opportunities` rebuild) or scheduled deliver+sell.
- Dashboard state thresholds, units/day windowing, and layout details.
- Where plasma_gas is mined.

## Open items the operator should know

- The giver's board rotation may make the unlock take ~1–2 weeks per wave; the
  line can run on the Treasure Cache relay in the meantime.
- The mobile capital (the bots' home) drifts; fit the harvesters before it
  moves away from wherever the bots are.
- Demand is unmeasured; expect "market saturated" to be the normal alert and
  treat the first week's units/day as the number that sizes everything else.
