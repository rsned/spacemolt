# Hunt decision capture: training data for a local hunt model

Date: 2026-11-21
Status: draft for approval

## Intent

Mining and hauling run well as fixed loops. Hunting does not: wildlife moves,
grounds dry up, predators appear, weapons suit some creatures and not others,
and ammo runs out mid-trip. The hunt worker handles this with hand-written
rules (`pkg/worker/hunt*.go`), most of which pick "first admissible" or
"nearest". The fleet is currently stalled: hunters log `NO AMMO` and nothing in
the loop can fetch more.

The operator wants a small local model that makes the hunt's judgment calls
better than the rules do. This design covers the step before any training:
**make the hunt fleet produce detailed, trustworthy training data**, and make
the hunt able to take the actions (ammo, rearm, range) that are worth learning.

Background research (operator's web session) set the recipe this follows: log
exactly what the small model will see and produce, record legal options per
step, label whole episodes with outcomes, split by episode, keep game facts in
the prompt rather than in the weights.

## Goals

1. Every hunt judgment call passes through one seam and is recorded as
   (compact state, options, choice, who chose) in a form a model could be
   shown and asked to answer.
2. Hunters can provision ammo, track it, and return to rearm before they run
   dry. The fleet fights again.
3. Each sortie is an episode with an id; fights, searches, loot, payouts and
   costs are recorded against it so every decision can be scored afterwards.
4. Hunters sometimes take a random safe option, so the data contains
   outcomes for choices the rules would never make.
5. Fleet-wide variation in weapons (a fixed, different loadout per hunter)
   so weapon choice can be compared.
6. A tool turns the records into (a) fact tables and (b) training examples.

## Non-goals

- Training, converting or serving a model. No model answers any decision yet.
- A frontier model as teacher (may be added later behind the same seam).
- A learned per-tick fight controller. Fights stay on `huntPolicy`; only the
  range band is chosen per fight.
- Per-sortie weapon refits. Loadout is fixed per hunter and recorded as state.
  Refitting is done by the operator by hand, not by code.
- Working several missions in one sortie. One job per sortie for now; pairing
  missions that share goods or a destination is a later optimisation.
- Making route planning hazard-aware, or widening what hunters may hunt.

## Anti-goals

- **Losing ships to exploration.** Random picks happen only among options the
  existing safety gates already allow.
- **Records a model could not be given.** No free-text-only decisions, no
  state that exists only in the KB at the time and can't be rebuilt.
- **Secrets in records.** `game.State.Password` is untagged and would
  serialize; records are built from an explicit projection, never from
  `State` directly.
- **Another 9 GB log.** Records are compact, per agent per day, compressible.
- **One blended score.** Credits per hour mixes luck with judgment (three
  empty belts before a find is not a bad decision). Raw outcome pieces are
  stored; scoring happens offline and can change without re-collecting.

## Constraints

- No sleeps in `pkg/worker` except through the `settle(ctx, d)` seam and the
  constants in `pkg/game/constants.go`.
- Check real server response field names before coding against them (battle
  log, reload plan, market buy, storage withdraw).
- Never edit an applied KB migration; add new ones.
- `data/` is dirty in git by design; new record files must be ignored, not
  committed.
- Go 1.24+, `golangci-lint` clean, `go build ./...` and `go test ./...`.
- JSON Schema for the record format uses Draft 2020-12.
- The server changes often: every record carries the game version and the
  worker commit so stale data can be dropped.

## Approaches considered

**A. Recorder calls at each decision site.** Fastest to start, but each site
formats its own state and options, records drift apart, and adding a model
later means touching every site again.

**B. One decision seam with the rules behind it (chosen).** Every choice is
"given this kind of decision, this compact state and these options, pick
one". Today's rules are the default answerer; one wrapper substitutes a random
safe option some of the time; another records. The record is by construction
what a model would see and produce, and a model later is a third answerer
behind the same call. Costs more up front: existing choices must list their
options before picking.

**C. Rebuild from existing logs and KB tables.** No worker changes, but the
text log has no state and no alternatives, and the KB inputs to ground ranking
are mutable. Cannot support training. Dropped.

The operator picked B, moving decisions behind the seam one at a time in the
phase order below. For the source of better-than-rules signal the operator
picked **rules plus exploration** over a frontier-model driver or offline
relabelling; the record format serves all three.

## Design

### The seam

A new package (suggested `pkg/huntdecide`) defines:

- **Decision**: kind, sortie id, sequence number, compact state, options.
- **Option**: a stable id plus the features the rules use to rank it (for a
  ground: system, POI type, jumps, sighting class, police level; for a quarry:
  species, role, hull, max hull; and so on).
- **Decider**: `Decide(ctx, Decision) Choice`.

Three implementations:

1. **Rules**: today's logic, returning its pick and a short reason.
2. **Explorer** (wraps a Decider): with probability ε for that decision kind,
   picks uniformly among the options. The rule's pick is still computed and
   recorded, so every explored record also carries the rule's label and the
   probability the choice was made with.
3. **Recorder** (outermost): writes the decision record.

Options offered to the seam are already filtered by the hard gates. The gates
are not decisions and are never explored:

- leave a ground when a non-quarry role is present
- hull floor flee, worn-down flee, tick and no-progress give-ups
- policed-only routes and grounds
- difficulty cap, mission allowlist, species role limits
- default stance flee

### Decision kinds

| Kind | Options | Rule today | Phase |
|---|---|---|---|
| `provision` | rounds to carry per ammo-fed weapon, in magazine-count buckets | new | 1 |
| `return` | continue, return to rearm, return to repair | hull only today | 1 |
| `ground` | candidate grounds within the radius | nearest / sighting class | 3 |
| `target` | admissible creatures at the POI | lowest hull | 3 |
| `range` | close / mid / far band, relative to weapon reach, held for the fight | advance to reach | 4 |
| `job` | resumable mission, admissible board missions, qualifying Station Needs, roam | first admissible / reward per jump | 5 |

### Compact state

One JSON-tagged projection, the same for every kind, small enough for a
couple of thousand tokens. Start from `agentstate.StateSnapshot` and add what
the hunt needs:

- ship class, hull, shield, fuel, free hold, credits
- **loadout**: each weapon with damage type, reach, cooldown
- **ammo**: per ammo-fed weapon, rounds loaded, magazine size, reserve rounds
  aboard, ammo item id
- location, docked, police level, jumps to nearest station and to home
- current job: mission or need, required species, kills or items so far
- sortie so far: ticks out, grounds visited and what was found, fights and
  results, rounds spent
- game tick, game version, worker commit

Facts that come from tables (drop odds, damage by weapon and species, sighting
rates) enter as option features or state fields at decision time, copied into
the record. The record must be self-contained: the KB will have changed by the
time it is read.

### Records

One file per agent per day, JSON lines, under
`data/agents/<agent>/hunt-decisions/YYYY-MM-DD.jsonl`, git-ignored. Three
record types share a sortie id:

- **decision**: kind, seq, state, options, chosen option id, rule's option id,
  source (`rule` or `explore`), ε, rule's reason.
- **outcome**: what happened as a result of a phase, keyed by the decision seq
  where one applies:
  - *search*: POI visited, creatures seen by species and role, jumps and ticks
    spent getting there
  - *fight*: battle id, species, range band, ticks, damage dealt and taken by
    weapon, rounds spent by weapon, result (kill confirmed by wreck, kill
    unconfirmed, broke off and why, fled)
  - *loot*: items and quantities taken, salvage
  - *provision*: rounds obtained, from storage or market, credits paid, or why
    it failed
- **sortie**: start and end records. Start: where, loadout, ammo, fuel, hull,
  credits. End: a result enum (`completed`, `returned_dry`, `returned_damaged`,
  `no_quarry`, `blocked_unarmed`, `stranded`, `destroyed`, `interrupted`),
  payout, items delivered, repair, fuel and ammo cost, ticks.

**Sortie shape (operator decision: one mission at a time).** A sortie is one
job, dock to dock. The hunter stays out for that job across as many grounds
as it takes: when a ground is empty or hunted out it makes another `ground`
decision, and it heads for a station only when the job is done or the
`return` decision says so (ammo, hull, full hold, or giving up on the job).
This replaces today's pass shape, where one `Hunt` call fights at a single
ground and the next call goes back to a station. Sortie state (id, sequence,
grounds visited, rounds spent) is kept in a per-agent file so it survives
across `Hunt` calls and restarts. A restart that finds a sortie open resumes
it if the hunter is still undocked and closes it as `interrupted` otherwise.
A dry-board roam move between stations is not a sortie and is recorded as a
`job` decision with a roam outcome. A Station Need's delivery leg is part of
its sortie. *(Reading "one mission at a time" as also meaning "stay out
across grounds for that mission" is the author's interpretation of the
operator's answer and of their bell_jelly example.)*

**Pass outcomes (author's call).** `Hunt` keeps its contract of never
returning an error that kills the worker loop. The sortie result is written
to `task-outcomes.json` as the reason. `completed`, `no_quarry`,
`returned_dry`, `returned_damaged` and `interrupted` are status `ok`;
`blocked_unarmed`, `stranded` and `destroyed` are status `failed`. If
`pkg/outcomes` cannot carry a reason on `ok` today, extending it is in scope;
dashboard changes are not.

A JSON Schema (Draft 2020-12) for the three record types lives beside the
package and is checked in tests.

### Ammo and rearm (phase 1)

New hunt steps, each a rule the seam can later explore:

Supply model (operator decision): **a home depot, with market buying as the
fallback.**

- **Depot**: each hunter's own storage at its home station (the fleet yaml
  `station`, `frontier_station` today) holds its ammo. The first fill is a
  hand transfer by the operator from the stock in craftsman-1's storage; it
  is an ops step, not code. *(Author's call: no automated resupply worker in
  this design. The depot level per hunter is included in the coverage report
  and a hunter surfaces one actionable line when its depot falls below two
  sorties' worth.)*
- **At dock, before leaving**: for each ammo-fed weapon, bring rounds aboard
  up to the provision target. Sources in order: ammo already aboard, the
  hunter's own storage at this station, the station market under a per-sortie
  credit cap. Default target is a fixed number of magazines. *(Author's call:
  the cap is a fleet yaml field, default 5,000 credits per sortie, and a
  hunter never spends below a small credit floor kept for fuel and repair.)*
- **Rearm destination**: a hunter that needs ammo and cannot get it where it
  is docked travels to the nearer of (a) its home depot, if the depot holds
  its ammo type, and (b) the nearest policed station whose market is known to
  sell it. With neither reachable it goes home.
- **Unarmed gate**: a hunter never leaves a dock for a job with no weapon
  able to fire. If it is unarmed at the home depot and the market cannot arm
  it, the sortie ends `blocked_unarmed` and it surfaces one actionable line.
- **During the sortie**: after each fight, the `return` decision. The rule
  returns to rearm when no ammo-fed weapon has a full magazine in reserve and
  no ammo-free weapon is fitted; with an ammo-free weapon fitted it may
  continue.
- Reload between engagements stays as `huntReload` does it today.

### Fight capture (phase 2)

- Save the raw `get_battle_log` reply for every hunt fight, compressed, under
  `data/agents/<agent>/battles/`, named by battle id. This is what makes
  range and weapon comparisons possible, including retroactively.
- Fill `damage_dealt` and `damage_taken` on `wildlife_kills` (hard-coded 0
  today) and stop writing kill rows for fights that were not won.
- Add `agent_id` to `wildlife_attacks` (new migration).
- Write `wildlife_observations` for creatures the hunter engages.

### Exploration (phases 3 to 5)

- ε is set per decision kind in the hunt fleet yaml; default 0.15, and 0
  disables it. Each hunter draws independently. The explorer wrapper is
  built in phase 1 with every ε at 0; exploration is switched on from phase 3,
  including for `provision` and `return`.
- `return` explores at a lower default (0.05) and only in safe directions:
  going home early after at least one fight, or staying out when the rule
  says rearm but a weapon can still fire. Returns forced by the hull gate or
  by having nothing that can fire are not options.
- `job`: a held, resumable mission is always resumed. It is forced, not an
  option, and is never randomly skipped.
- `ground` exploration is bounded by a maximum extra jump count over the
  rule's pick, so a random ground cannot become a long trek.
- `provision` exploration only varies upward and downward within the buckets;
  the unarmed gate still applies.
- `range` bands are measured against the ship's longest weapon reach (the
  server's `max_weapon_reach`); bands a short-reach ship cannot tell apart
  are merged, so some hunters have two options or one.
- `range` holds one band for a whole fight so the battle log gives a clean
  comparison. How a band is held (advance and retreat commands against
  `zone_distance` and weapon reach) is for the builder to work out from the
  battle API.

### Loadouts (ops, alongside phase 1)

Hulls today: five Gauntlets (3 weapon slots, 22 CPU, 50 power: pirate-5, -6,
-9, -10, -13), two Prospectors (1 weapon slot: pirate-7, -8), and pirate-1
quarantined in a Sparrow. Suggested mix, drawn from stock already at
`frontier_station` (mostly in craftsman-1's storage) unless noted:

| Hunter | Weapons | What it tests |
|---|---|---|
| pirate-5 | 3× pulse_laser_ii | energy, no ammo, reach 3 |
| pirate-9 | 3× autocannon_ii | kinetic, big magazine, reach 2 |
| pirate-10 | 2× railgun_i + pulse_laser_i | kinetic, 5-round magazine, reach 5 |
| pirate-13 | 2× missile_launcher_ii + pulse_laser_i | explosive, reach 6; launchers are at nova_terra_central |
| pirate-6 | autocannon_ii + railgun_i + pulse_laser_i | mixed reach on one ship |
| pirate-7 | pulse_laser_i | single-weapon baseline, no ammo |
| pirate-8 | autocannon_ii | single-weapon baseline, ammo |

Ammo at `frontier_station`: standard rounds (tens of thousands), ferrous
slugs (675), guided missiles (101). This table is a proposal; the operator
approves the final mix after checking each hull's fitted modules and power
budget live. EM and thermal weapons are in stock elsewhere and can join once
the first tables show whether resistances matter. pirate-6 is at 3 credits
and 3 fuel and needs funding or gifted stock before it can provision.

### Dataset tool (phase 6)

`cmd/tools/hunt-dataset` reads the record files and battle logs and writes:

- **Fact tables**: damage per tick and rounds per kill by species, weapon and
  range band; sighting rate by ground and POI type; drop odds by species
  (extending `wildlife_kill_drops`). These are useful for manual hunts
  immediately and become prompt inputs for a model.
- **Scored decisions**: each decision joined to the outcome it controls:
  ground to creatures found per jump and tick; target and range to ticks to
  kill, damage taken, rounds spent; provision and return to ran dry, came home
  early, or carried unused rounds; job to payout and items delivered over
  sortie time.
- **Training examples**: chat-format JSON lines (system prompt, state plus
  options, chosen option), with the scores as side columns so filtering is a
  later choice. Train and evaluation sets are split by sortie, never by step.
- **Coverage report**: counts per decision kind, per species × loadout ×
  range cell, and per sortie result, so it is visible when there is enough
  data and where it is thin.

## Phases

1. Seam, recorder, explorer (off) and sortie ids; the main loop reshaped to
   one job per sortie across several grounds; ammo provisioning and
   return-to-rearm as the first two decision kinds. Operator refits the
   hunters and fills the depots. Fleet fights again.
2. Outcome records and fight capture tied to the sortie.
3. `ground` and `target` behind the seam, with exploration.
4. `range` per fight, with exploration.
5. `job` behind the seam, with exploration.
6. Dataset tool: tables, scored decisions, training examples, coverage.

Each phase is deployable on its own and leaves the fleet hunting.

## Left to the builder

- Package layout and names, and how the seam is threaded through `HuntDeps`.
- The exact magazine-count buckets and the default provision target.
- Which commands buy ammo and withdraw from storage, and their field names.
- How a range band is held during a fight.
- Compression and retention for battle logs and record files.
- Whether the dataset tool also loads records into SQLite for querying.
