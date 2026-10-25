# Cloverfield Home Station — Design

Date: 2026-10-25 · Status: draft for approval

## Intent

Our faction gets **a home of its own**: a player station founded with the
Station Core craftsman-1 is carrying, placed in **Cloverfield**, a lawless system
roughly halfway between Haven and Outer Rim space. Right now production lines run
in rented facilities spread across several NPC stations, and moving goods between
them costs a lot. We currently pay hundreds of thousands of credits a day in rent
and haul between stations. A station we own has no rent for us. It does have
maintenance costs, and we don't know them yet.

Founding the station is also a **charting expedition**. Player stations are only
partly documented. The three official guides
([base-builder](https://spacemolt.com/docs/guides/base-builder),
[factions](https://spacemolt.com/docs/guides/factions),
[crafting](https://spacemolt.com/docs/guides/crafting)) leave out maintenance
materials and rates, what a new station really ships with, production facility
costs, and power behaviour, and in places they contradict each other. Every step
we take is recorded with real numbers so that:

1. we can stand up **station #2** later from a playbook instead of guesswork;
2. **any player** can use what we learned, published on our Knowledge Base site
   (`~/spacemolt/kb`, live at rsned.github.io/spacemolt-kb);
3. a **SCADA/HMI-style station console** can show reserves, status, and the
   station's finances. Its requirements will come out of running the station.

### Purpose, in priority order (user's words, condensed)

1. **Home of our own.** This comes first.
2. **Growth second.** Cloverfield is a crossroads with player traffic, so later we
   sell refueling, market access, production, and similar services to it.
3. **Defenses should exist.** Distance from pirate outposts gives some natural
   protection, but defenses are still required.

## Projects and order

This is three projects. Only the first is designed in detail here.

| # | Project | When |
|---|---|---|
| 1 | **Found and run the station** (this doc) | now, by hand |
| 2 | **Station guide** on the KB site | written up from #1's records; its own design once we have data |
| 3 | **Station HMI/SCADA console** | its own design once #1 has run long enough to know what matters |

Project #1 must **record everything in a form #2 and #3 can use**, so we never
have to reconstruct history from logs after the fact.

## Anti-goals (what would make this a failure even if it "works")

- ⭐ **The station slows down or shuts down early because it ran out of required
  resources.** We don't know what those are until it's running. This is the
  user's top concern and drives the cautious, stock-before-you-build approach below.
- Guide content that presents guesses as facts. Every number is dated and
  labelled *measured* or *doc-quoted*.
- Losing the Station Core or the 5M founding fee to a failed or wrong founding
  attempt.
- Automating steps we don't understand yet.

## Non-goals (for now)

- Public services for outsiders (refuel pricing, market fee, public build policy).
  Growth comes later.
- Moving production lines off the rented stations. That comes after the station
  has run stably on its own maintenance for a while.
- The full ~3.9M defense build-out, mobile bases, and selling the station.
- Worker automation. A step becomes a worker verb only after we've done it by hand
  and understood it.

## What we already know (recon, 2026-10-25)

**Cloverfield** (`cloverfield`): police 0, no empire, "Lawless (no police
protection)", **no existing station**. So it meets the stated founding rules.

| POI | type | resources (remaining) |
|---|---|---|
| Cloverfield Belt | asteroid_belt | iron ~100k, copper ~100k, zinc ~12k |
| Cloverfield Frozen Drift | ice_field | nitrogen 50k, water ~11k, ammonia 7k |
| Cloverfield Gas Plume | gas_cloud | nebula 3.4k, fluorine 2.2k, traces of others |
| Phantom Glint, Shy Spark Nebula | nebula | iron 100k, copper 100k, phase_crystal 448 each |
| Cloverfield I–IV | planet | — (candidate founding POIs) |
| Cloverfield Star | sun | — (not allowed) |

Neighbours (all lawless): **Zosma** (412; has the only nearby station, owner
unchecked; water/nitrogen ice), **Wazn** (546; iron/copper ~200k, neon/hydrogen/
argon 50k each, plasma 10k, lithium 3.5k), **Cocibolca** (554; iron/copper),
**Driftwood** (686; iron/copper, nitrogen, a little helium/phase crystal).

**We can mine iron and copper on-site, so steel can be made locally. Silicon,
energy crystal, and titanium are not nearby,** so circuits and alloy have to be
hauled in.

**Doc-quoted facts (unverified until we see them happen):**

- Founding needs: Station Core in cargo · `manage_bases` · 5,000,000 cr (treasury
  first, then the founder's wallet) · undocked at an empty eligible POI · no
  stars/wormholes · max 5 stations/faction, 1 per system.
- **The founding command conflicts between guides:** base-builder says
  `facility action=found_station name=… public_access=false`; factions says
  `build_base`. `get_base_cost` is the documented pre-check.
- A station ships with: top-tier faction storage (a lockbox there is refused as
  redundant), market, refuel, repair, shipyard, crafting, power, and life
  support. Stock defense: 60k hull, 12k shield, 15 armor.
- No rent for us. Instead, **service and infrastructure facilities draw
  maintenance from faction storage at the station and labor credits from the
  treasury every cycle (100 ticks ≈ 16.7 min, 86.4 cycles/day). An undersupplied
  power plant throttles the whole station.** Which items maintenance uses, and how
  many, is not documented.
- Guns draw ammo from faction storage, and nothing restocks automatically. Point
  Defense Battery: 180k, ammo Ferrous Slug Case. Shield Projector: 900k, +18k
  shield, upkeep 4 power cells + 2 coolant per cycle.
- Production facility costs are only available via `facility action=types`.
- Admin: `station action=info | set_public | set_service_access | set_refuel_price
  | set_market_fee | set_build_policy | allow_* | ban …`.

**Client state:** response structs exist for `build_base`, `build_outpost`,
`get_base_cost`, and `station` (`pkg/game/serverapi`), but there are no command
wrappers or worker verbs for them. We'll drive them by hand through play_as
(`go run ./cmd/tools/play_as`).

## The approach: by hand, record everything, stock before you build

Craftsman-1 does the founding and first builds, driven through play_as by the
user and Claude together. Each step has three parts: **pre-check → act → record**.

### Phase 0: Pre-flight (read-only)

craftsman-1 is the user's manual account and never runs as a worker, so play_as
is safe to use. Capture:

- `get_base_cost` → real requirements and the correct founding command;
- `faction_info` → which faction founds it, whether craftsman-1's role has
  `manage_bases` and `manage_treasury`, and the treasury balance (≥6M: 5M fee +
  1M floor; the_banker tops up any shortfall);
- craftsman-1 location, ship, cargo (confirm `station_core`), fuel;
- `facility action=types` (all categories) → the full catalogue with costs, saved
  verbatim. This is the guide's raw material and fills the production-cost gap;
- the route to Cloverfield, and whether to bring an escort.

**Gate:** no travel until the founding command, permissions, and funds are confirmed.
*(Claude's call)* If the pre-check fails (requirements we can't meet, Cloverfield
turns out ineligible, or someone founds there first), stop, keep the core, and
bring it back to the user. No backup system is scouted in advance.

### Phase 1: Haul the first stockpile *before* founding

Because of the top anti-goal, the first convoy (core ship plus haulers) arrives
**already carrying likely maintenance and startup consumables.** We guess these
from the guides and the facility catalogue: fuel, power cells, coolant, steel,
circuits, repair kits / armor plate, and Point Defense ammo. Quantities are a
generous guess for several days, within the 1M purchase cap. Everything is deposited into faction storage
as soon as it exists. If we overbuy, we keep the stock; if we underbuy, the
station slows down. Overbuying is the cheaper mistake.

### Phase 2: Found

Pick a planet POI, go undocked, run the confirmed founding command. Record the
full request/response, the fee actually charged, whether the core was consumed,
and the station/base/POI ids (stations are dual-named; record both).

### Phase 3: Inventory "what actually comes with it"

Dock and capture `station action=info`, `facility action=list` (including the
power block), `facility action=faction_list`, faction storage view, and services.
This is the ground truth the guide's "what you get" section is built from.

### Phase 4: Arm, then watch the maintenance meter

*(Claude's call)* Don't leave the station undefended while we measure. Right after
the inventory, build **one Point Defense Battery** and load the ammo that came
with the convoy. Its own maintenance shows up as a separate line in the facility
list, so measuring with it built doesn't spoil the baseline. Then run the station
with nothing else added and **record what leaves faction storage and the treasury**.
*(Claude's call)* Cadence: a snapshot at the start and end of every by-hand
session, and at least hourly while one is running. Hourly diffs over a few days
give the rate. Per-cycle precision isn't needed. If one-hour readings turn out to
be too coarse, a read-only snapshot tool is the first thing to automate. Watch power, life support, and
condition too. This gives us the baseline maintenance bill and its rate. Restock
anything trending to zero long before it runs out.

### Phase 5: "We live here"

Add one facility at a time, and after each:
record build cost, materials, build time, and **the change in the maintenance
bill**. *(Claude's call)* Don't run `set_home_base` / change the faction home in
this phase. First check how home and respawn affect worker and overmind routing,
because it could pull the fleet into lawless space.

**Milestone ("we live here"):** founded, docked, storage working, refuel and
repair running, maintenance supplied with days of runway, PD battery armed, and
everything recorded. Production lines and public services are later steps,
each with its own record.

### Keeping it supplied after the milestone

*(Claude's call)* Until a worker verb exists, maintenance supply is checked by
hand **daily**. As soon as Phase 4 shows which items maintenance consumes, add a
"days of runway" figure per item and alert below **3 days**. The alert lives on
the existing line dashboard (:8093) until the HMI replaces it. Automating restock
is the first verb to build once the consumables are known.

## Recording (feeds the guide and the HMI)

One durable record set per station, kept in the repo under
`data/stations/cloverfield/` (exact layout is the builder's call):

- **Event log** (append-only, JSONL): every command we ran, the raw response,
  timestamp and game tick, and a one-line note of what we learned. Includes
  failures and their error codes.
- **Snapshots:** periodic captures of `station info`, facility list (power and
  life support), faction storage, treasury, and condition. These are the raw data
  for maintenance rates and the HMI's history.
- **Ledger:** every credit and item in or out with a category (founding,
  build, maintenance-items, maintenance-labor, ammo, haul-in, revenue). This is
  the HMI's finance books.
- **Findings:** a running human-readable notes file, one entry per fact
  learned, tagged *measured* or *doc-quoted* with date. The guide is written from
  this.

Snapshot capture can start as a by-hand play_as step. Making it automatic is the
first thing to automate once we know which reads matter.

## Budget and people (user's decisions, 2026-10-25)

- **Goal: self-sufficient.** The faction collectively has hundreds of millions to
  fall back on, but the station should aim to pay its own way.
- **Treasury floor: 1,000,000 cr.** The faction treasury never drops below 1M,
  because it still pays the rental bills until production lines move to the
  station. Founding (5M) therefore needs ≥6M in the treasury first; any shortfall
  is sent by **the_banker**, who holds the reserve funds.
- **Purchases capped at 1,000,000 cr total** for consumables and stockpile
  (fuel, ammo, power cells, coolant, steel, etc.). Going over needs the user's OK.
- **Facility builds are planned one at a time.** Each costs credits plus
  materials. Before each build, write up the cost, the materials and where they
  come from, and the expected maintenance, and get the user's approval.
- **craftsman-1 is the user's own manual account,** always outside the fleets.
  Driving it for days has no effect on the fleet. Its standing bids stay untouched.

## Constraints

- play_as only via `go run ./cmd/tools/play_as` (never bin/). It is a login, so
  don't run it against a live worker session.
- Lawless space: follow the stronghold-routing and escort rules. Never route the
  core ship through a pirate stronghold.
- Energy crystals are reserved. Don't spend them on station materials without
  asking.
- Any pauses in client code use the Sleep constants in `pkg/game/constants.go`.
- Field names in recorded responses are taken from the actual server reply, not
  from the docs.

## Left to the builder

- Exact file layout and schema of the record set.
- Which planet POI to found at (prefer the one closest to the belt and ice field).
- Escort and haul composition for the founding convoy.
- Initial stockpile quantities (a generous guess, written down with the reasoning).
