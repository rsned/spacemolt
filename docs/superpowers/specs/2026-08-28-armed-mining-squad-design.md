# Armed Mining Squads

**Date:** 2026-08-28
**Status:** Design — not implemented
**Supersedes nothing.** Related: `2026-08-23-mining-site-contention-design.md`

## Problem

The mining fleet is currently switched off. It was stopped because miners die:
24 ships lost, across 16 distinct agents, every one of them in `police_level: 0`
Lawless space.

```
cause      n
combat    17
wildlife   7
```

Nineteen of the twenty-four were tier-0 or legacy mining hulls -- shard 4,
cobble 3, prospect 3, prospector 3, excavator 2, theoria 2, drillship 1,
threshold 1. The remaining five were haulers and freight hulls lost on other
work.

The proximate cause is not bad luck or bad routing. It is that a mining worker
has **no combat behaviour at all**. It parks at a belt, mines, and if something
attacks it, it continues mining until it explodes. Nothing flees, nothing
returns fire, and — the fact that shapes this entire design — nothing helps.

## The constraint that shapes everything

> Without the `fleet` command, only the ship being attacked joins the battle.
> And even with it, the others have to independently see the battle notice and
> decide whether it is one they should join.

Two separate facts, and both matter.

Four miners working the same belt are not a group. They are four independent
ships that will be destroyed one at a time while the others continue mining.
Proximity confers nothing, so mutual defence is a game mechanic with an API,
not something we can build on top of ordinary worker behaviour.

But joining is **never automatic**. There is no "the fleet responds" event. Each
member's own client receives a broadcast, evaluates it, and chooses to enter the
fight. The squad's massed damage is therefore not a property of the squad — it
is the sum of four independent decisions that each have to be made correctly and
quickly. That decision logic, not the fleet formation, is the substance of this
design.

## What the catalogue permits

All five tier-0 mining hulls already carry a defense slot **and** a weapon
slot, and four of five ship with a weapon fitted from the shipyard.

| hull      | hull | shield | CPU | power | W / D / U | default modules                       |
|-----------|-----:|-------:|----:|------:|-----------|---------------------------------------|
| cobble    |   80 |     35 |  12 |    24 | 1 / 1 / 2 | mining_laser_i, autocannon_i          |
| prospect  |   95 |     50 |  13 |    26 | 1 / 1 / 2 | mining_laser_i, pulse_laser_i         |
| shard     |  110 |     40 |  12 |    28 | 2 / 1 / 1 | mining_laser_i, 2x autocannon_i       |
| theoria   |  100 |     55 |  16 |    28 | 1 / 1 / 3 | —                                     |
| threshold |   75 |     80 |  16 |    30 | 1 / 2 / 2 | —                                     |

Source: `data/game-api/latest/catalog_ships.json` (335 hulls). The legacy
mining hulls we own in bulk — drillship, excavator, deeprock_harvester — are
**absent from the catalogue** and their slot layout is unknown; see Open
Questions.

### Defensive modules

`shield_booster_*` adds flat shield pool in a defense slot:

| module             | shield | CPU | power | cost   |
|--------------------|-------:|----:|------:|-------:|
| shield_booster_i   |    +25 |   2 |     4 |  1,700 |
| shield_booster_ii  |    +50 |   3 |     6 |  7,200 |
| shield_booster_iii |   +100 |   5 |    10 | 13,000 |
| shield_booster_iv  |   +200 |   8 |    15 | 18,000 |

Because tier-0 base shields are small, a flat bonus is a large multiplier.

**Fitting a shield booster cannot lock a rig out of a deposit.** The mining
gate sums *mining* power against a resource's `supported_power`, and the
intersection of `item_mining` and `item_defenses` is **zero rows** — defensive
modules carry no mining power. The two systems are independent.

Shield recharge on these hulls is 1/tick, so a booster is a one-shot buffer,
not sustain. It buys time; it does not win fights.

### Weapons

| weapon         | dmg | type    | reach | cooldown | CPU/pow | cost  | ammo |
|----------------|----:|---------|------:|---------:|---------|------:|------|
| autocannon_i   |   8 | kinetic |     2 |        1 | 3 / 4   |   710 | yes  |
| autocannon_ii  |  14 | kinetic |     2 |        1 | 4 / 6   | 1,800 | yes  |
| pulse_laser_i  |  10 | energy  |     3 |        1 | 2 / 5   | 2,500 | no   |
| pulse_laser_ii |  18 | energy  |     3 |        1 | 3 / 8   | 6,900 | no   |

**Pulse lasers, not autocannons.** Autocannons draw from a 500-round magazine.
An unattended miner several systems from a station that runs dry has a weapon
slot and no weapon, and we have no ammo-resupply logic. Pulse lasers draw from
the powerplant and never need resupply.

This is a re-arm, not a top-up: cobble and shard ship with the ammo-dependent
weapon by default.

Reach matters as much as damage — reach gates firing entirely (a pulse_laser_iii
at reach 3 could not fire at distance 4). The pulse laser's reach 3 beats the
autocannon's 2.

## Design

Three components, in dependency order. Component 1 is a precondition for 2;
component 3 is independently useful but only reaches its full value inside 1.

### 1. Squad formation (the `fleet` API)

A mining squad is a game fleet: one leader plus 2–4 members, formed at a
station before departure and disbanded on return.

The API is `POST /fleet` with `action` in
`create | invite | accept | decline | leave | kick | disband | status`.
`Client.Fleet(ctx, action, playerID)` has existed since **v0.240**, is on the
`GameClient` interface, and **has never been called by any worker or strategy**.
The plumbing exists; the behaviour does not.

Two documented semantics govern the design:

- **"Fleet leader controls navigation and combat for all members."** The leader
  initiates travel and jumps for the whole squad. Members keep their own
  ships and continue to mine independently at the destination — leader control
  covers movement, not every action.
- **"Speed = slowest ship."** The squad moves at its slowest member.

`find_route` already returns `fleet_fuel[]`: one `FleetMemberFuel` per member
carrying `fuel_per_jump`, `fuel_on_arrival` and a **`can_complete`** flag. The
server answers "can this squad make this trip" in a single query before anyone
burns fuel. We do not build group fuel coordination; we ask for it.

`fleet_upkeep` is a boolean on a station-service schema, not a running cost.
Squads carry no ongoing credit drain.

**Lifecycle: per-run.** Form at the station, fly out, mine, fly back, disband.
Every run starts from a clean slate, so a death, a stranding, or a wedged
member cannot corrupt a long-lived group. Our worst outages this month were all
long-lived state going quietly wrong, and a standing squad is exactly that
shape.

**Leadership passes automatically on the leader's death**, to the surviving
ship with the highest Leadership skill. The squad is therefore not stranded by
losing its leader, and movement is not a single point of failure.

It does mean **any member may become the leader mid-run**, without warning and
without having planned the route. So the return plan cannot live only in the
leader: every member carries the destination and the way home, and a worker
that finds itself promoted continues the run rather than discovering it has
no idea where it was going. `fleet status` reports `is_leader`, which is how a
worker learns the promotion happened.

**We cannot currently see Leadership at all.** `player_skills` holds **zero
rows** — no agent's skills have ever been captured into the KB — so we cannot
rank a squad by the stat that decides its succession. This is the same shape of
gap as `ship_modules`, and it has to close before squad composition can be
chosen deliberately rather than by accident.

Worse, the ordering may be degenerate. Leadership trains through "lead faction
operations, manage faction members, participate in diplomacy" — none of which a
mining worker does. Our miners are therefore all plausibly at Leadership 0, in
which case "highest Leadership remaining" is a tie among equals and the promoted
leader is effectively arbitrary. That is survivable, given every member carries
the plan, but it removes any ability to *choose* the successor.

There is an upside worth noting: Leadership grants `fleetBonus` at 1 per level
against a max level of 100. A deliberately trained leader is a real force
multiplier for a squad, not just a tiebreak — though what `fleetBonus`
multiplies is undocumented and untested.

### 2. Reaction policy

This is the largest component, and it has two halves: what a ship does when it
is attacked, and what it does when a *squadmate* is attacked.

#### Joining a squadmate's fight

The server broadcasts `battle_alert` to every client in the system when a battle
begins. `BattleAlertResponse` carries `battle_id`, `system_id`, a `participants[]`
list of `{player_id, username, ship_class, side_id, stance, zone}`, and a
`sides[]` summary. Its doc comment calls it informational, "no action required
from the receiver" — true of the protocol, and precisely the assumption this
design overturns.

The join sequence is:

1. Receive `battle_alert`.
2. Scan `participants[]` for a squadmate. If none, ignore the alert.
3. Read that squadmate's `side_id`.
4. `battle engage` with that `side_id`.
5. `battle stance fire` and `battle target <attacker>`.

**Neither step 4 nor step 5 costs a tick.** Battle actions are queued and applied
together at the start of the next battle tick, so stance, target and engage can
all be set in the same tick. The cost of joining is latency, not ticks.

The decision at step 2 needs a squadmate roster. `fleet status` returns
`members[]` with `player_id` and `username`, which is the roster; it must be
cached at formation rather than queried on alert, because `fleet` is a mutation
command rate-limited to 1 per tick and an alert is exactly when we cannot
afford to spend that tick.

**Not every alert is ours to join.** The broadcast is system-wide, so an alert
will frequently name strangers. Joining a fight we have no stake in trades a
mining run for someone else's war. The rule is narrow: join only when a
squadmate is a participant.

#### Being attacked

The policy decides, per tick in combat, between three outcomes:

- **Fight** — hold position and fire. Correct when the squad's massed damage
  can win, or when fleeing is impossible.
- **Flee** — break off and run for a station. Correct when it cannot.
- **Continue** — the default only when not in combat.

Two rules the policy must encode:

**Arming a hull invalidates `brace`.** Brace reduces incoming damage to 0.25x
but zeroes offense. That is correct for a defenceless hull and wrong the moment
the ship can shoot — and catastrophic for a squad whose entire value is massed
fire, because a braced member contributes nothing to the volley that is meant
to end the fight early.

**The worker has no combat code today.** `advance` comes from the server
autopilot. The policy therefore issues stance and target decisions and lets the
server fly; it does not attempt manoeuvre.

### 3. Fit standard

One fit per hull, applied at a station. Module wear, damage and repair were
removed from the game in v0.568, so refitting is now consequence-free and
modules may be moved between hulls freely.

Fits are CPU/power-constrained, and prospect cannot have both the big shield
and the big gun:

| hull      | fit                                                          | CPU/pow  | shield | dmg |
|-----------|--------------------------------------------------------------|----------|-------:|----:|
| prospect  | mining_laser_i + pulse_laser_i + booster_iv                   | 12 / 25  |    250 |  10 |
| prospect  | mining_laser_i + pulse_laser_ii + booster_iii  *(alt)*        | 10 / 23  |    150 |  18 |
| cobble    | mining_laser_i + pulse_laser_i + booster_iii                  | 9 / 20   |    135 |  10 |
| threshold | mining_laser_i + pulse_laser_ii + booster_iv + armor_plate_ii | 10 / 15  |    280 |  18 |

**Threshold is the standout**: two defense slots, the best base shield of the
five, and enough CPU and power to carry the big booster, armour and the better
gun at once.

The prospect trade — +100 shield against +8 damage/tick — argues for mixed
squads rather than one uniform fit: shield-heavy members absorb, gun-heavy
members resolve.

Cobble misses `booster_iv` by a single CPU point. Engineering reduces module
CPU/power by 1%/level, so the gap closes around Engineering 13; the fit table
must be computed against each agent's actual skill level, not the base cost.

**Cost is not a constraint.** Roughly 540,000cr to fit ~30 miners, against
~296M idle credits — two tenths of one percent.

## Non-goals

- **A dedicated fighter hull in the squad.** Possibly correct later, but it is
  a strictly larger change than arming the miners already present, and its
  value cannot be measured until armed miners have been observed losing.
- **Ammo logistics.** Avoided by choosing pulse lasers, not solved.
- **Standing squads.** Explicitly rejected above.
- **Manoeuvre control.** The server autopilot flies.

## Testing

- Fit computation is pure: hull slot/CPU/power budget plus module costs plus an
  Engineering level in, a fit or a refusal out. Table-driven unit tests, no
  game connection.
- Reaction policy is pure: combat state in, decision out. Table-driven, with
  cases for armed vs unarmed (the brace rule), fleeable vs cornered, and squad
  vs solo.
- Squad formation needs a live two-agent test in `play_as`; it cannot be
  meaningfully mocked, because the questions that matter are all about server
  behaviour.

## Open questions

These block implementation and are answered by a probe, not by design:

1. **Does `battle engage` succeed for a fleet member who is not yet a
   combatant, and is fleet membership what permits it?** `battle_alert` is
   broadcast system-wide to everyone, so detection plainly does not require a
   fleet. What the fleet grants — the right to take a side, or merely the
   coordination — is untested, and the whole design rests on it.
2. **What is `max_size`?** `FleetCreateResponse` carries it; the value is
   unknown. It caps squad size.
3. **What does `fleetBonus` actually do?** Leadership grants 1 per level to it
   and the skill description calls it "fleet coordination bonuses", which is not
   a mechanic. If it affects damage, speed or fuel, it changes who should lead.
   *(Resolved separately: leadership passes to the highest-Leadership survivor
   when the leader dies, so succession itself is not in question.)*
4. **Can a member `leave` mid-combat to flee independently?** Determines
   whether "flee" is even available to a squad member.
5. **How quickly must a join land to matter?** A tier-0 miner with a booster
   survives roughly 2-3 ticks against heavy damage. If alert-to-engage latency
   exceeds that, the squad arrives to a wreck and the whole model fails.
6. **Slot layout of the legacy hulls** (drillship, excavator,
   deeprock_harvester) — absent from the catalogue, and we own 144 of them.
   `ship_modules` has never captured a row, so we cannot read fitted loadouts
   from the KB; `list_ships` now returns `module_type_ids` (v0.568) and is the
   way in.

## Rollout

1. Probe the six open questions in `play_as` with two agents.
2. Capture agent skills into `player_skills`, which has never held a row --
   without it Leadership is invisible and squad composition is guesswork.
3. Fit computation + tests; report what each idle hull *could* carry.
4. Reaction policy + tests, deployed to solo miners first — it is independently
   valuable and does not depend on squads.
5. Squad formation, one squad, one system, observed.
6. Compare loss rate against the 24-ship baseline before widening.

Mining stays off until step 4 lands.
