# Agent Asset Profile — Design

**Date:** 2026-08-01
**Status:** design approved, not implemented
**Sub-project of:** fleet role interchangeability (see "Why this is first" below)

## Problem

We cannot answer, from local data, what any of our agents is capable of or what
it owns.

Concretely, these questions currently require reading worker logs by hand or
running a live `play_as` session against one agent at a time:

- Which agents have left freight probation, and which are stalled in it?
- Which agents are at smuggling level 3, and which are next for a wave?
- Which hulls does an agent own, and at which base are they parked?
- What items do we hold across the fleet, and where — the "what can we source
  for free" question the crafting planner wants?

The tables that would answer these exist but are cold or empty:
`storage_snapshots` was last written **2026-07-02**, and `agent_ships` has
**never had a row**. Both were fed by `cmd/tools/daily-summary`, which cannot
run against a live fleet (see "Why the collector must live in the worker").

## Why this is first

The operator's stated end state is that pool membership becomes a **dynamic
per-agent capability flag** (`haul=true, freight=false, mission=true,
mission.smuggling=false`) held in the overmind's memory, rather than a roster
migration between fleet YAMLs. Flags are *derived from* what an agent can
actually do. So the asset/capability ledger is the substrate for that work, and
nothing above it can be built honestly without it.

Rotation policy and roster migration remain a medium-term need for skill-gating
agents upward; multi-role trip stacking (johnny_cab carrying passengers and
cargo on one route) comes after we can see who can do what, for how much, and
where.

## Scope

**In scope:** a new `assets.db` and `pkg/assets` package; capture of player
profile, skills, standings, carrier profile, owned hulls, per-base storage, and
faction assets; a derived eligibility layer.

**Out of scope, deliberately:**

- **Assignment.** Which agents are *actually* running which role stays with the
  overmind. This spec produces eligibility (*can*), not assignment (*should*).
- **Scheduler priority classes.** See "Scheduling" — the `spread: true` flag is
  a follow-on task so that this spec touches no shared code.
- **Faction ship garages.** See "Deferred: faction garages".
- **Snapshot history.** Current state only; progression is already derivable
  from the event tables in `market.db` (`mission_results`, `freight_results`).

## Coverage

Fleet agents only — the 129 agents named in overmind rosters, of which ~110 are
running workers at any time (`fleet.yaml` is a legacy roster). The ~39 off-fleet
agents (pirates, architects, retired marketbots, prophets, `databot`,
`random-clark`) are each blocked on a *capability* we have not built yet, and
roll into fleets as those land. The schema therefore treats the agent key as
open and makes no assumption about roster membership: adding a sweeper or a new
fleet later is additive, not a redesign.

## Identity

There are three identifiers and none is derivable from the others:

| Identifier | Example | Mutable | Used by |
|---|---|---|---|
| `player_id` | `a50924913cef881c5e4d14257589d9ba` | no | `Player.ID`, `StorageGift.SenderID`, action-log actors |
| `username` | `Arthur 'Artificer' Artis` | **yes** | `send_gift` target, chat, forums |
| `agent_id` | `engineer-3` | ours | `data/agents/`, fleet YAMLs, rescue queue, worker logs |

`agent_id` is not a game concept; it is our local label. **`player_id` is the
primary key everywhere.** The mapping is only learnable by logging in as that
agent, so the `agents` table is built up over time rather than seeded.

This table is independently useful today: gift `sender_id` values are hexes,
the rescue queue and fleet YAMLs speak `agent_id`, and `send_gift` takes a
username. Those are currently joined by hand.

## Data sources

All are free queries — no tick cost. Field names below are verified against the
structs in `pkg/game/serverapi`, not assumed.

| Command | Struct | Yields |
|---|---|---|
| `get_status` | `Player` | `ID`, `Username`, `Empire`, `Credits`, `CurrentSystem`, `CurrentPOI`, `CurrentShipID`, `HomeBase`, `DockedAtBase`, `FactionID`, `FactionRank`, `Experience`; `Skills map[string]Skill`; `Standings map[string]EmpireStanding` |
| `shipping action=profile` | `ShippingProfileResponse` | `CarrierProfile` + `CarrierCapacity` + `CarrierTierProgress` |
| `list_ships` | `ListShipsResponse.Ships []OwnedShip` | every owned hull **and its location** |
| `view_storage` / `view_storage station_id=` | `ViewStorageResponse` | `BaseID`, `Credits`, `Items []CargoItem`, `Ships []StorageShip`, `Hint` |
| `faction_info` | `FactionInfoResponse` | `Treasury`, `MemberCount`, `OwnedBases`, `FuelBunkers []FactionFuelBunker` |
| `view_faction_storage` / `..._at` | `ViewFactionStorageResponse` | `FactionID`, `BaseID`, `Credits`, `Items []CargoItem`, `FactionFuelReserve`, `FactionFuelCapacity` |

`ShippingProfile()` already exists (`pkg/game/client_commands.go:2766`), as do
`ViewStorageAt` (`:732`) and `ViewFactionStorageAt` (`:2119`). No new client
commands are required.

### `list_ships` supersedes per-base ship capture

`ListShips` is documented as *"lists all ships owned by the player and their
locations"*, and `serverapi.OwnedShip` is a strict superset of
`serverapi.StorageShip`:

```
OwnedShip:   ShipID ClassID ClassName IsActive Hull Fuel CargoUsed
             Location LocationBaseID Modules ListingID ListingPrice ListingBaseID
StorageShip: ShipID ClassID ClassName            CargoUsed                Modules
```

One free call per agent therefore replaces an entire per-base ship sweep, and
catches hulls parked at bases we would not have thought to query. There is no
`agent_storage_ships` table; `ViewStorageResponse.Ships` is a consistency
cross-check only.

`ListingID` / `ListingPrice` / `ListingBaseID` come along free and distinguish a
hull that is **listed for sale** from one available for a refit.

### `Hull` and `Fuel` are `current/max` strings

Confirmed against a live payload:

```json
{"class_id":"survey_vessel","class_name":"Survey Vessel","fuel":"1020/1020",
 "hull":"340/340","is_active":false,"location":"stored at Grand Exchange Station",
 "location_base_id":"grand_exchange_station","modules":3,
 "ship_id":"74aeb79e64d9a12f682a2ee6daad79e4"}
```

Parsed into `hull_current`, `hull_max`, `fuel_current`, `fuel_max` integers,
**retaining the raw string** so a format change surfaces as a mismatch rather
than as silent zeros. The `"150/200"` case is the one that matters: a hull that
exists but is not ready to fly.

### `view_storage`'s `hint` enumerates every base with holdings

```
"hint":"2,720,379 items in storage at cargo_lanes_freight_depot, central_nexus,
 confederacy_central_command, ... voss_redoubt_station"
```

So base discovery needs no growing known-set and no brute-force sweep of all 64
stations: **one `view_storage` at the current dock, parse the hint, then N-1
targeted `ViewStorageAt` calls.** Twenty calls covers the heaviest agent in the
fleet (`craftsman-1`, ~2.7M units across 20+ stations).

Two properties are designed for rather than assumed away:

- It is a **prose string**. Parsing splits on `" in storage at "` then `", "`.
  A parse failure falls back to the previously-known base set with a loud log —
  **never to an empty sweep**, which is indistinguishable from "sold
  everything" and would trigger the deletion path below.
- It reports bases with **items**. A base holding only a parked ship or only
  credits may not appear. `agent_hulls.location_base_id` covers the ship case,
  so the two sources cover each other's blind spot. A credits-only base is a
  known, accepted gap.

## Why the collector must live in the worker

`daily-summary` needs control of an agent's session. The fleet holds those
sessions, and contending for one triggers a disconnect/reconnect thrash — the
same `session_replaced` failure that made `random-6`/`random-7` unrunnable
together. That is why it has been silent since 2026-07-02: it can only run with
the entire fleet offline.

Any centralized collector is therefore structurally dead on arrival while the
fleet runs. Workers must capture their own profile.

**Consequence:** once this lands, `daily-summary`'s storage capture is
superseded and must not be run against a live fleet.

**Corollary:** `play_as` already delegates to `worker.KBUpdateSystem`,
`worker.CaptureMarket` and `worker.Autopilot`, so the operator's manual
character feeds the same databases the fleet does. Capture must follow that
pattern, or the primary interactive account — by far the largest holder — is
the one agent that never reports.

## Architecture

`pkg/assets` owns **both** the store and the capture functions. Capture takes
`(ctx, game.GameClient, *Store)` and nothing else, so it is testable without a
worker harness.

`pkg/worker` and `cmd/tools/play_as` each get a thin dispatch case that calls
in. This mirrors the `worker.KBUpdate*` precedent but inverts where the logic
sits: `pkg/worker` already carries `mission.go` at 1,672 lines and `haul.go` at
1,470, and asset capture belongs to neither.

`Open(cfg) (*Store, error)` follows `pkg/market`: embedded `schema.sql` using
`CREATE TABLE IF NOT EXISTS`, plus an `ensureColumn` block for additive
changes. **Pragmas are set through the DSN, not `db.Exec`** — an `Exec` pragma
lands on a single pooled connection, so with many workers sharing a handle
every connection must inherit `busy_timeout` and WAL or contention surfaces as
immediate `SQLITE_BUSY` instead of a clean blocking wait. This is the same
reasoning recorded in `pkg/market/collector.go`.

### Why a separate database

`data/assets.db`, not `spacemolt-knowledge.db` and not `market.db`.

The reason is **blast radius, not row count**. Current volumes (227
`(agent, base)` pairs, 53,898 item rows) are unremarkable for SQLite, and even
at fleet scale this lands in the low millions. But `spacemolt-knowledge.db` is
1.4 GB and shared with the sibling `spacemolt-kb` repo, and `market.db`
required a full-day recovery from exactly this class of contention. A separate
file means an asset sweep can never stall the fleet, and the DB can be vacuumed
or rebuilt without touching anything that matters.

## Schema

Tables mirror the wire shapes: **maps become narrow tables, structs become wide
tables.** This is where extensibility comes from — a new skill or a new faction
is a new *row*, requiring no migration. Only a genuinely new source needs a new
table.

| Table | Key | Source |
|---|---|---|
| `agents` | `player_id` | `agent_id`, `username`, `first_seen`, `last_seen` |
| `agent_profile` | `player_id` | `Player` scalars |
| `agent_skills` | `(player_id, skill)` | `Player.Skills` → `level`, `xp` |
| `agent_standings` | `(player_id, faction)` | `Player.Standings` → `reputation`, `baseline`, `outstanding_bounty`, `jailed_until` |
| `agent_carrier` | `player_id` | `CarrierProfile` + `Capacity` + `TierProgress`, flattened |
| `agent_hulls` | `(player_id, ship_id)` | `OwnedShip` |
| `agent_storage` | `(player_id, base_id)` | `credits` |
| `agent_storage_items` | `(player_id, base_id, item_id)` | `name`, `quantity`, `size` |
| `faction_profile` | `faction_id` | `treasury`, `member_count`, `owned_bases`, `name`, `tag`, `leader_id` |
| `faction_storage` | `(faction_id, base_id)` | `credits`, `fuel_reserve`, `fuel_capacity` |
| `faction_storage_items` | `(faction_id, base_id, item_id)` | `name`, `quantity`, `size` |
| `faction_fuel_bunkers` | `(faction_id, base_id)` | `fuel_reserve`, `fuel_capacity` |

### No foreign keys to the catalogs

The existing `agent_ships` table declares `FOREIGN KEY (class_id) REFERENCES
ships(id)`, and **96% of recorded `class_id` values do not resolve** against
the catalog (`prospector` vs `prospect`, `excavator` vs `excavation`). That FK
is a plausible reason it has never had a row written.

New tables store `class_id` verbatim with no FK, and resolve against the
catalog at read time so a mismatch is *visible* rather than fatal. The existing
`agent_ships` table is left untouched.

### `captured_at` is per-table

The four sources refresh on different cadences — the carrier profile is two
free queries, a storage sweep is N calls across every base. A single
agent-level timestamp would make a 20-minute-old skill level and a 6-day-old
storage listing indistinguishable, and would mark carrier data as fresh when
only the profile had been re-read.

### `agent_standings.baseline` is the load-bearing column

Reputation floats above baseline from missions and decays back toward it when
idle; **baseline is the decay target**. Stronghold access is
`pirates.baseline >= 10` — the permanent unlock granted by `an_introduction`.
Gating on `reputation` would read as eligible during the float and flip back
later.

## Faction capture deduplicates

Faction assets are per-faction, not per-agent. Whichever faction an agent
belongs to (`Player.FactionID`), one member's capture covers every member — so
the faction sweep is claimed by a single member per cycle rather than run once
per member. The set of factions we control falls out of
`SELECT DISTINCT faction_id FROM agent_profile`.

## Capture commands and cadence

| Command | Calls | Cadence |
|---|---|---|
| `capture_profile` | `get_status`, `shipping profile`, `list_ships` | hourly |
| `capture_storage` | `view_storage` + `ViewStorageAt` × bases from hint | daily |
| `capture_faction` | `faction_info`, `view_faction_storage` × faction bases | daily, one member per faction |

Each is a dispatch case in `pkg/worker/dispatch.go` (plus its `supported` map
entry) and a `roles.yaml` schedule line, and a `play_as` command.

All are free queries, so cadence is bounded by write contention and politeness
rather than game economics. Hourly profile capture across ~110 workers is ~330
calls/hour — noise beside the existing market captures.

## Scheduling

`pkg/worker/schedule.go` offers six frequencies (`ten_minutely`,
`quarter_hourly`, `half_hourly`, `hourly`, `daily`, `weekly`) with **hard
boundary alignment and no jitter**: `CurrentBoundary("hourly")` is the top of
the hour for every worker, and a task is due when `LastRun < CurrentBoundary`.
There is no priority class and no spread.

The operator's scheduling taxonomy maps onto that gap:

| Tier | Example | Needs |
|---|---|---|
| 1 — barrier | marketbot `update_market` → arbitrage scanner | exact boundary, no jitter, plus a downstream completion barrier |
| 1b — fresh-ish | shipping listings, `list_station_passengers` | ~2×/hour, "when calm" |
| 2 — eventual | `browse_ships`, storage snapshots, **all three captures here** | just needs to happen; spread freely |

All three asset captures are **tier 2**.

**Deferred to a follow-on task:** an opt-in `spread: true` on a `roles.yaml`
schedule entry, offsetting the due time by a deterministic hash of `player_id`
into the period. Default `false` would preserve today's behavior exactly, which
matters because jittering marketbot `update_market` is precisely what would
break the scanner's coherent snapshot.

**Accepted consequence of deferring it:** all workers fire `capture_storage` at
the same `daily` boundary. This is judged tolerable for v1 — free queries and
small write transactions are a different animal from the whole-table scans that
made `market.db` contend — but it is a real burst, and `spread` is the correct
fix rather than a workaround inside the capture.

Tier 1 (scanner barrier) and tier 1b ("when calm", which needs a fleet-load
signal that does not exist) are real and unbuilt, and belong to a scheduler
spec rather than this one.

## Staleness must be visible

The precedent is bad and consistent: `daily-summary` stopped writing on
2026-07-02 and was unnoticed for 25 days; `market-prune` died and grew the DB
to 62 GB; the arbitrage scanner's death mimicked "no opportunities". Every one
was an **unsupervised daemon**.

So: **no new daemon.** Capture rides the worker schedule, which the overmind
already supervises and restarts. Coverage becomes a query — max age per source
per agent — surfaced on **ovdash**, which is already running and already
watched. The alarm is "N agents have no profile newer than X", visible where
the operator already looks, rather than a cron whose silence means nothing.

## Eligibility layer

`agent_capability(player_id, capability, eligible, blocking_reason, as_of)` —
**a table materialized on every capture, never hand-written.**

A table rather than a SQL view, because the rules live in Go: a registry,
`map[string]func(Profile) (bool, string)`, in `pkg/assets`. Expressing them as
SQL would fork the definitions across two languages and let them drift. A new
capability is a new function and a new key: no schema change, no migration.
This is the layer that grows as needs change, wrapping tables that stay pinned
to the wire format.

| Capability | Derived from | Example `blocking_reason` |
|---|---|---|
| `haul` | active hull cargo capacity, credits (arbitrage buying power), fuel | `cargo 45 < 100` |
| `freight` | `agent_carrier.tier`, liability headroom, `outstanding_debt`, cargo ≥ one package (`freightPackageFootprint` = 100) | `outstanding_debt 4200` |
| `mission_delivery` | active hull cargo capacity | — |
| `smuggling` | `agent_skills['smuggling'].level` | `level 1, needs 3` |
| `stronghold_access` | `agent_standings['pirates'].baseline >= 10` | `baseline -30, needs 10` |

`blocking_reason` is what turns the ledger into a worklist: "who is next for the
smuggling wave" becomes a query
(`WHERE capability='smuggling' AND NOT eligible ORDER BY level DESC`) instead
of a manual log read.

### Screening filter, not a promise

These predicates **approximate** the workers' own gates; they do not duplicate
them. The real gates (`buildMissionCandidate`, `freightCandidate`, `haulGate`)
are per-pass, live-priced, and depend on the board in front of the agent.
Making them literally identical would require refactoring three gate paths.

**`agent_capability` answers "could this agent plausibly do X". The worker's own
gate remains authoritative at accept time.** Stated here so the divergence is a
known property rather than a later bug report.

## Failure handling

The governing rule is the one `pkg/worker/mission.go` already states for
freight: *any failure degrades to "no capture this pass" — it must never be a
new way for the pass to fail.* Asset capture is strictly less important than
freight, so it holds harder. A nil store disables capture entirely.

**Partial capture is normal and recorded honestly.** If `get_status` succeeds
and `shipping profile` errors, the profile is written and
`agent_carrier.captured_at` is left untouched. Per-table timestamps make this
the natural outcome rather than a special case.

**Two deletion invariants, at both grains.** Whole-set replacement inside one
transaction:

- an item that vanishes from a base has its row **deleted**, not left behind
- a **base** that vanishes from the hint means holdings there went to zero, and
  its `(player_id, base_id)` row is deleted too

Both guard against phantom stock, which is exactly what would poison the
"what can we source for free" query this ledger exists to serve. Hull sets are
replaced per-agent on the same rule.

**The hint parser never fails open.** An unparseable hint falls back to the
previously-known base set with a loud log, never to an empty sweep — an empty
sweep would trigger the base-deletion invariant above and erase real holdings.

**Identity conflicts are loud.** A `username` change updates in place against
the stable `player_id`. Two `agent_id`s resolving to one `player_id` is a
config error (the `random-6`/`random-7` shape) and is logged rather than
silently overwriting.

## Testing

TDD, proven red first. Fake `GameClient` following the existing `pkg/worker`
test fakes; no live-server tests.

- **Golden payloads** — real captured JSON to expected rows, covering the two
  parsers most likely to be wrong: the prose `hint` string, and `"150/200"` to
  `(150, 200)`. The `list_ships` and `view_storage` payloads quoted in this
  document are the first fixtures.
- **Replacement invariants, both grains** — capture `{x, y}` then `{x}` leaves
  no `y`; capture bases `{A, B}` then `{A}` leaves no `B`. These are named
  invariants and get explicit tests.
- **Partial failure** — shipping profile errors, profile row still written,
  `agent_carrier.captured_at` unchanged.
- **Eligibility boundaries**, table-driven: smuggling level 2 vs 3, pirate
  baseline 9 vs 10 — mirroring the existing
  `TestSmugglingBuyingXPBelowLevel3` style.
- **Hint parse failure** falls back to known bases and deletes nothing.

## Deferred: faction ship garages

`faction_ship_garage` (`faction_build`; 20 ships, upgrading to
`faction_ship_hangar` = 50 and `faction_fleet_yard` = 100) gives a faction a
shared fleet pool: gift a ship to the faction to store it, `switch_ship` while
docked to claim it. A `/faction_garages` command exists.

`FactionGaragesResponse` is already declared in
`pkg/game/serverapi/responses_passthrough.go`, but that file's header states
its shapes are *"spec-derived and unverified"*, its `FactionGarage.Ships` is a
bare `[]string` of IDs with no class or cargo detail, and **no client method
exists** for the command.

No faction has built a garage yet, so there is nothing to verify against.
Building a table from an unverified spec is the bet that has cost us before.
This is therefore the **first planned extension**, and a good test of the
wire-shaped-schema claim: when a garage is built, it should be one
`faction_garage_ships` table plus one capture call, touching nothing else.

## Open items

- Whether `view_storage`'s `hint` truncates for very large base lists. Twenty
  bases render in full; the parser treats a short list as authoritative, so a
  silent truncation would under-sweep. Verify against the largest holder.
- The exact format of `OwnedShip.Modules` (a count, per the payload) versus
  whatever `get_ship` returns for fitted modules. Fitting detail is not
  captured in v1; "which modules are fitted, and which are in storage" is the
  natural follow-on for the refit question.

## Build order

The pieces are separable and should land in this order, each independently
useful:

1. **`pkg/assets` store + `agents` + `agent_profile` + `agent_skills` +
   `agent_standings`**, fed by `capture_profile`'s `get_status` call alone.
   Smallest slice that produces the identity map and answers the smuggling-wave
   question.
2. **`agent_carrier`** — adds the `shipping profile` call. Answers the freight
   probation question that currently requires a log sweep.
3. **`agent_hulls`** — adds the `list_ships` call, including the `current/max`
   parser.
4. **`agent_capability`** — the eligibility registry over 1–3. Nothing above
   depends on storage.
5. **`agent_storage` + `agent_storage_items`** — the hint parser and the sweep.
   The largest and least urgent piece.
6. **Faction tables** — `capture_faction` with the one-member-per-faction claim.

## Follow-on work, in order

1. `spread: true` scheduler flag (tier-2 jitter).
2. Module/fitting capture, to answer "can this agent be refitted for the role".
3. Faction garages, once one is built.
4. Assignment (the C layer): overmind-held capability flags reading this view.
5. Scheduler tiers 1 and 1b: the scanner barrier and a fleet-load signal.
