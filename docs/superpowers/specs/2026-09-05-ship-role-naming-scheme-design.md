# Ship Role Naming Scheme — Design

**Date:** 2026-09-05
**Status:** Approved design, ready for implementation planning
**Supersedes scoping in:** `docs/memory/project_ship_role_naming_scheme.md` (requested 2026-07-19)

## Problem

Agents perform many task types, and each wants a differently fitted hull: an ore
miner needs mining lasers, an ice miner needs ice harvesters (the two are not
interchangeable), a hauler needs cargo, a tow mission needs a tow rig. Every
empire sells its own variety of each, so an agent's stable is a heterogeneous
set of hull classes with no common identifier for "the one fitted for mining".

We need a stable, machine-matchable way to ask "switch to your mining hull"
without knowing which class that agent happens to own.

## Constraints (verified against `server_docs/openapi.json` `/name_ship`)

These are server-enforced and drove most of the design:

- **3–32 characters.** Charset is **letters, digits, spaces, hyphens,
  apostrophes** — **no underscores.** The earlier memo's `ice_miner` /
  `gas_harvester` examples are invalid as written.
- **Globally unique across the galaxy, case-insensitive.** Confirmed with the
  dev team: names are public identity, appearing in battle logs and on wrecks,
  and uniqueness is deliberate.
- **Only the *active* ship can be named.** Naming a stored hull requires
  `switch_ship` first.
- **Mutations are rate-limited to 1 per tick (10 s).** So naming one hull costs
  ~20 s of tick budget (`switch_ship` + `name_ship`).
- Sending an empty name clears it.

## Scheme

```
<role> [<subtype>] <8-hex>
```

Examples:

```
miner ore a41e07a7
miner deepcore a41e07a7      (23 chars — the longest form, vs the 32 limit)
hauler c1eade83
tanker 65ab11bc
```

### Suffix derivation

    suffix = sha256(seed ‖ 0x00 ‖ agent_id)[:8 hex chars]

One suffix **per agent**, reused across that agent's roles. `miner ore a41e07a7`
and `hauler a41e07a7` are visibly the same pilot's stable.

**Why derived rather than random.** A derived name is *reconstructible* — any
agent's hull name can be computed from facts we already hold, with no lookup —
and *idempotent*, so the namer can run on every login and mint nothing. This is
what demotes persisting `agent_ships` from a blocker to a convenience.

**Why 8 hex.** Measured, not assumed. Against a projected roster of 1,371 ids
(13 agent types × 100, plus 71 non-numeric ids such as `assist-sol` and the
`marketbot_*` accounts), sampling 200 candidate seeds:

| suffix width | seeds with an internal collision |
|---|---|
| 4 | 200/200 — unusable |
| 6 | 9/200 — 4.5%, fragile |
| 8 | 0/200 |
| 10 | 0/200 |

Width 4 fails outright once roster growth is allowed for; width 6 leaves a
1-in-22 chance of picking a seed that breaks on some future agent. Under a
*derived* scheme a collision is not recoverable the way a random re-roll would
be — two agents sharing a digest means every same-role pair of their hulls wants
the identical name. Width 8 is the first with real headroom.

The shipped seed must satisfy the distinctness assertion below.

### Seed handling

The seed comes from config/env and **is not committed**. Agent ids are trivially
guessable (`miner-1`…`miner-10`), so seed plus function is enough for an outsider
to enumerate the fleet's ship names. The repo carries a documented test-only
default (`spacemolt-v1-0`) used by the unit tests.

### Collision ladder

Attempt 0 is the bare digest. Attempt *N* hashes `seed ‖ agent_id ‖ N`. One
mechanism covers both cases:

- a galaxy-wide name clash (another player holds the name), and
- an agent legitimately owning a second hull of the same role and subtype.

The ladder is reconstructible by probing attempts in order.

**A ladder step must log loudly and surface to the operator.** No human
independently chooses `miner ore a41e07a7`, so an external collision is
effectively impossible by accident. If one fires, the realistic causes are that
someone derived our seed and is squatting names, or that we are re-naming a hull
we already named and lost track of. Both are things an operator needs to see; a
silent retry would hide exactly the opsec breach it steps around, and would also
leave the ladder as untested code that fails the one time it matters.

### Matching

Prefix-based, and the hierarchical word order exists to make this a single rule:

- `"miner "` selects any mining hull (serves "assign to the mining fleet")
- `"miner ice "` selects only ice hulls (serves "we need ice specifically")

### Design rationale: what the scheme deliberately does not leak

Sequential naming (`miner 1`, `miner 2`, …) would leak fleet **headcount** and
imply **rank** — `miner 1` reads as the oldest or most-invested hull, and a
hunter picking targets from a battle log would start there. A derived suffix
designates no one as number one and reveals nothing about scale.

**Residual, accepted:** the role word itself advertises cargo value.
`hauler 91cc2d0a` on a wreck tells a pirate that pilot moves freight; `tanker`
marks a support ship. This is inherent to a greppable prefix, and operator
legibility was chosen over opacity. If it ever bites, the lever is renaming the
two or three sensitive roles to neutral words — not redesigning the scheme.

## Vocabulary

Eight top-level roles, five of which are `miner` subtypes — twelve distinct
name prefixes in total. Each is defined where possible by a **required module
family**, so membership is *verifiable* from `list_ships` rather than merely
asserted by the label.

| ship role | defining fitting | serves worker roles |
|---|---|---|
| `miner ore` | `mining_laser_*` | miner |
| `miner ice` | `ice_harvester_*` | miner, resident_ice |
| `miner gas` | `gas_harvester_*` | miner, resident_gas |
| `miner rad` | `rad_harvester_*` | (none yet) |
| `miner deepcore` | `deep_core_survey_scanner` | (none yet) |
| `hauler` | cargo capacity + `cargo_expander_*` | hauler, craftsman |
| `tanker` | built-in pump hulls (capability-defined) | assist |
| `hunter` | weapons + `shield_booster_*` | hunt |
| `explorer` | `survey_scanner_*`, fuel efficiency | explorer agents |
| `shuttle` | `passenger_*_berths` | shuttle |
| `smuggler` | `scan_resistance` / `integrated_cloak` | smuggling project |
| `tow` | `basic_tow_rig` / `advanced_tow_rig` | missionrunner (tow missions) |

**Ship roles are a smaller vocabulary than worker roles.** Of 213 worker-role
assignments across 10 fleets, 138 are `unlock` (47), `resident` (46) and
`missionrunner` (45) — all behavioral, implying nothing about fitting. Only
roles with a distinctive module requirement need a named hull.

Two decisions taken as defaults, open to revision before names are minted:

- **`hauler` is one role**, not `hauler bulk` / `hauler freight`. Both variants
  want maximum cargo. Splitting later is cheap (the `hauler ` prefix still
  matches); merging already-minted names is not.
- **`miner rad` and `miner deepcore` ship in v1** despite no worker role using
  them today. The modules demonstrably exist in the catalogue, and defining a
  prefix is far cheaper than changing one after names are minted galaxy-wide.

Extraction subtypes are first-class in both hulls and modules, and line up 1:1:

| subtype | hull capability | module family |
|---|---|---|
| ore | `ore_yield_bonus`, `ore_cargo_efficiency` | `mining_laser_i…v` |
| ice | `ice_yield_bonus`, `ice_cargo_efficiency` | `ice_harvester_i…iv` |
| gas | `gas_yield_bonus`, `gas_cargo_efficiency` | `gas_harvester_i…iv` |
| rad | — | `rad_harvester_i…iv` |
| deep core | — | `deep_core_survey_scanner` |

150 of 341 hull classes carry `inherent_capabilities`. `resident_gas` and
`resident_ice` already encode the subtype axis in worker roles, so this
partitioning matches how the fleet is independently organised.

## Components

### `data/overmind/ship-roles.yaml` (new)

The canonical vocabulary, fitting-defined:

```yaml
ship_roles:
  miner:
    subtypes:
      ore:      { requires: [mining_laser_i, mining_laser_ii, mining_laser_iii,
                             mining_laser_iv, mining_laser_v] }
      ice:      { requires: [ice_harvester_i, ice_harvester_ii,
                             ice_harvester_iii, ice_harvester_iv] }
      gas:      { requires: [gas_harvester_i, gas_harvester_ii,
                             gas_harvester_iii, gas_harvester_iv] }
      rad:      { requires: [rad_harvester_i, rad_harvester_ii,
                             rad_harvester_iii, rad_harvester_iv] }
      deepcore: { requires: [deep_core_survey_scanner] }
  hauler:   { prefers_capability: [ore_cargo_efficiency] }
  tanker:   { prefers_capability: [] }
  hunter:   { requires: [] }
  explorer: { requires: [survey_scanner_i, survey_scanner_ii] }
  shuttle:  { prefers_capability: [passenger_economy_berths,
                                   passenger_business_berths,
                                   passenger_first_berths] }
  smuggler: { prefers_capability: [scan_resistance, integrated_cloak] }
  tow:      { requires: [basic_tow_rig, advanced_tow_rig] }
```

`requires` is satisfied by **at least one** listed module being fitted.

`hunter` and `tanker` carry no `requires` list: a hunter is defined by weapon and
defense slot usage rather than by a single identifying module, and tanker hulls
carry a built-in pump that is a hull property, not a fitted module. **The drift
audit cannot verify these two roles** and must report them as `unverifiable`
rather than as passing — otherwise an empty `requires` reads as a clean audit.
Closing that gap means checking slot usage and hull capability respectively, and
is deferred.

### `data/overmind/roles.yaml` (extended)

Gains one optional key per worker role — `ship_role: miner ice` — joining the
two vocabularies. This is what lets "move agents to the mining fleet" resolve to
a concrete hull. Absent key means the worker role has no hull preference.

### `pkg/shiprole` (new)

Small and dependency-free, so it is testable without a game client:

```go
func Name(seed, agentID, role, subtype string, attempt int) string
func Parse(name string) (role, subtype, suffix string, ok bool)
func Match(name, prefix string) bool
```

Mirrors the shape of `pkg/worker/roles.go` (~50-line YAML loader).

### Namer

Reads `list_ships` once, computes the expected name for each owned hull, and
acts only on mismatches.

- **Opportunistic by default.** An agent names a hull when it is *already*
  switching to it for work, so `switch_ship` is free and only `name_ship` is
  marginal cost. A dedicated backfill pass is the fallback for hulls that never
  come up naturally.
- **Idempotent and skip-cheap.** A correctly-named fleet costs one query and
  zero mutations, which is what makes it safe to run on every login.
- **Never fights the worker.** Naming steals the active-ship slot, so it runs
  between jobs via the existing quiesce/idle-pass seam, never mid-task.

### Drift audit

Reads live `list_ships`; for each hull whose name parses as ours, checks
`ModuleTypeIDs` contains at least one module of the required family. A hull
named `miner ice …` carrying only `mining_laser_ii` is reported, not silently
trusted.

`OwnedShip.ModuleTypeIDs` (added v0.568.0) is the only view we have into what is
fitted — `ship_modules` has never captured a row — which is why roles are
defined by fitting rather than by hull class.

## Error handling

- **`name_ship` rejected (name taken):** advance the collision ladder, log
  loudly, surface to the operator (see above).
- **`name_ship` rejected (invalid):** a bug in `Name()`. Fail the pass, do not
  retry — retrying cannot fix a malformed name.
- **Roster digest collision at startup:** refuse to run. The seed is no longer
  valid for the current roster and a human must choose a new one.
- **Interrupted mid-rename:** the backfill pass records the intended active ship
  on disk *before* switching, and restores it on completion. Without this, an
  agent that dies between `switch_ship` and restore is stranded on the wrong
  hull. This is the failure mode that bit the 2026-08-14 tanker migration, where
  `switch_ship` being a separate step was missed.

## Testing

1. **Roster distinctness** — the projected 1,371-id roster hashes to 1,371
   distinct suffixes under the shipped seed. Pins the seed and fails loudly if
   the roster grows into a clash. Uses the *projected* roster (100 per agent
   type), not the live one, so growth is pre-cleared and never re-verified.
2. **Cross-reference** — every module id in `ship-roles.yaml` resolves in
   `pkg/fitting.Catalog`, and every `ship_role:` in `roles.yaml` exists in
   `ship-roles.yaml`. Same shape as the existing role check in
   `pkg/worker/hunt_dispatch_test.go`.
3. **Round-trip** — `Parse(Name(...))` recovers role and subtype for all 12
   prefixes, including the subtype-less ones.
6. **Unverifiable roles** — the audit reports `hunter` and `tanker` as
   `unverifiable`, never as passing.
4. **Charset and length** — every name generated for the projected roster × all
   roles is 3–32 chars and uses only the permitted charset.
5. **Ladder** — attempt *N* differs from attempt 0 and is deterministic.

## Out of scope

- **Buying, building or fitting hulls.** The scheme names and switches among
  hulls that already exist. (The memo's longer-term "fleet provisioning" horizon
  — acquire a role-appropriate hull for every role for every agent — is a
  separate project that builds on this vocabulary.)
- **Persisting `agent_ships`.** The audit works off live `list_ships`, and
  derived names mean no lookup table is needed to *find* a hull. Worth doing
  later for fleet-wide reporting without 180 logins, but not a dependency.
- **Overmind re-allocation policy** — deciding *which* agents to move to the
  mining fleet. This design provides the mechanism (name, match, switch); the
  policy is separate.
