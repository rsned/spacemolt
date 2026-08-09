# Wildlife Hunt Fleet — Design

**Date:** 2026-08-08
**Status:** Approved (design)

## Problem

The fleet cannot fight. craftsman-1's lifetime record is `damage_dealt: 20`
against `damage_taken: 822`, with `pirates_destroyed: 0` and three deaths by
pirate. Combat is the one major system no agent engages with, and it gates
several things the operator wants: the dormant pirate bands, salvage from kills,
and the `piracy` skill that improves customs scan evasion for the smuggling
runners.

Nothing in `pkg/worker` fights. `buildMissionCandidate` refuses every
non-deliver objective, and a test named *"kill mission must be rejected"* pins
that refusal against a `kill_creature` objective. The door is deliberately
closed.

The server has since added a taught curriculum for exactly this. This design
walks through it.

## Goal

Stand up a small, separate **hunt fleet** that completes wildlife combat
missions, accumulates combat skill XP, and makes combat mechanics observable —
without risking the 38 mission-learn workers currently earning on delivery and
smuggling.

## Non-goals

Explicitly out of scope for this iteration, each deferred for a stated reason:

- **PvP sparring.** `cmd/tools/spar` already exists (shipped 2026-05-25, still
  compiles) but predates wildlife, `get_battle_log`, and the tackle layer.
- **Electronic warfare.** Warp scramblers and webs are anti-ship; wildlife
  neither warps nor flees to hyperspace, so the EW role is *structurally
  untrainable* here. It needs sparring, later.
- **`leviathan_bounty`.** Difficulty 6, and the docs describe the Molt Leviathan
  as a predator that "hunts ships and fights to the death". See the gate below.
- **Wildlife ranching.** The server supports `faction_build
  facility_type=wildlife_corral` to domesticate a herd for diet secretions and a
  managed cull. A separate economy; noted so it is not rediscovered as novel.
- **Band 3 (pirate-11..15).** The crewed EW band waits for sparring.

## Fleet shape

A new overmind fleet, `hunt`, alongside the existing six. Separate socket,
status file, history file, and log — so it drains, restarts, and dies
independently, and a losing streak is visible without reading another fleet's
log.

**Roster: pirate-6..10** — the five band-2 brigands (Rocco 'Razor' Redgrave,
Vinnie 'Void' Vane, Scurvy 'Seadog' Sanders, Ransom 'Rogue' Reynolds, Blade
'Buccaneer' Blackwell). All five are registered with credentials, personality,
and play_as history; all five are dormant. Aggression traits run 0.6–0.95. They
were created for this and are otherwise idle.

Dynamic fleet membership is already built (membership engine, overrides sidecar,
SIGHUP diff, ovdash enter/leave buttons), so growing or shrinking the roster
needs no new machinery.

A new `hunt` role in `roles.yaml`, carrying the three asset captures like every
other role plus `idle: hunt`.

## Ships and the loss policy

**A destroyed ship is replaced free with a Tier-0 hull carrying 1 pulse laser
and 1 mining laser.** That single fact sets the floor: the fleet's worst case is
a free armed hull plus the travel time to get back to a belt. Insurance paying
credits rather than a ship does not matter when the replacement is free.

> 🔴 **The roster's hulls are LEGACY and cannot be re-bought. This invalidates
> the disposable-hull policy.**
>
> `get_ship` reports `"legacy": true` on the Prospector, and the operator
> confirms every mining hull the roster owns is legacy — Drillship, Deeprock
> Harvester, Excavator, Prospector. These classes no longer exist in game. A
> destroyed hull is not replaced at market price; it is replaced by an *inferior
> current-tier* ship, and for some there is no equivalent at any tier. The
> Deeprock Harvester carries 5–6 mining laser slots, which no current tier 1, 2
> or 3 mining hull offers.
>
> So a loss is a **permanent downgrade of an irreplaceable asset**, not a 2,500cr
> rebuy. "Cheap disposable hulls with a rebuy budget" was chosen on the
> assumption that losses were cheap and a free armed Tier-0 caught the fall.
> Neither holds.
>
> **This needs an explicit operator decision before the plan is written.** The
> options, and their consequences:
>
> - **Strict no-loss.** Flee early and abandon missions rather than risk a hull.
>   Slower, and some hunts will be aborted, but nothing irreplaceable is spent.
>   Difficulty-1 passive grazers should never threaten a hull anyway, so the cost
>   of this policy is near zero at the starting gate and rises only as gates lift.
> - **Sacrificial hull.** Buy a *current-tier* combat ship for hunting and leave
>   the legacy mining hulls docked at Frontier Station. Costs credits up front
>   against a roster holding ~77k, but makes losses genuinely affordable and
>   keeps the legacy fleet intact.
> - **Accept the risk** on the explicit understanding that a loss is permanent.
>
> **DECIDED: hunt in the Prospectors; losses are acceptable.**
>
> The legacy Drillships, Deeprock Harvester and Excavator stay docked at Frontier
> Station and are never risked — those are the genuinely irreplaceable hulls, the
> Deeprock especially with 5–6 mining laser slots that no current-tier ship
> offers.
>
> **The Prospector is different: it was the free starter, so losing one costs
> nothing.** Its replacement is nearly its equal and arrives armed:
>
> | | `prospector` (legacy, owned) | `prospect` (current, free on loss) |
> |---|---:|---:|
> | Hull | 100 | 95 |
> | Armor | 5 | 4 |
> | Speed | 2 | 1 |
> | Cargo | 50 | **100** |
> | Weapon slots | 1 | 1 |
> | Ships armed | **no** | **yes** (`pulse_laser_i`) |
>
> So the fit-out is: **buy one `pulse_laser_i` (2,500cr) for each of pirate-7..10**
> — pirate-6's is already fitted — and accept losses. A destroyed Prospector
> returns as an armed `prospect`, which hands the laser back for free and adds 50
> cargo, at the cost of 5 hull, 1 armor and 1 speed.
>
> Total outlay: **~10,000cr**, once. There is no rebuy budget to manage, because
> after the first loss the replacements arm themselves.
>
> A flee threshold is still worth having — an aborted hunt beats a pointless
> death, and a destroyed ship costs travel time back to a belt — but it is a
> convenience rather than a safeguard.

> ✅ **Resolved: the respawn Tier-0 IS armed.** The current starter is
> **`prospect`** — not the legacy `prospector` the roster owns — and its
> `default_modules` is `["mining_laser_i", "pulse_laser_i"]`. Confirmed against
> `data/game-api/latest/catalog_ships.json`. So a destroyed hull is replaced by a
> functional armed ship at no cost.

**The Tier-0 is a respawn hull, not a stored asset — do not plan around
switching into one.** Checked against the asset ledger on 2026-08-08, across all
87 captured agents:

- **39 of 87 hold no spare hull at all**, only the one they fly.
- The 48 that do hold spares hold **mining rigs**: drillship (35), prospector
  (33), excavator (26), deeprock_harvester (16). These carry mining lasers, not
  weapons — a legacy of the agents that once ran `auto-miner` to sell ore.
- No agent is sitting on a spare combat fit.

### The roster as captured — verified 2026-08-08

Step 0 ran: all five of pirate-6..10 were captured via `play_as
--assets-db-path`. The result changed the outfitting plan.

| Agent | Credits | Active hull | Stored | Weapons skill |
|---|---:|---|---|---:|
| pirate-6 | 13,227 | Drillship (fuel **0/130**) | Prospector | 0 |
| pirate-7 | 17,013 | Deeprock Harvester | Drillship, Excavator, Prospector | 0 |
| pirate-8 | 15,329 | Drillship | Prospector | 0 |
| pirate-9 | 24,657 | Drillship | Prospector | 0 |
| pirate-10 | 6,661 | Drillship | Prospector | 0 |

Three facts follow.

**They are miners, not pirates.** Top skills are piloting, deep_core_mining and
mining; **weapons is 0 across the whole roster**. The brigand personalities are
aspirational — the `auto-miner` era is what actually shaped them. Weapons 0 is
not a problem: First Hunt exists precisely for pilots at weapons 0, so the
roster is the chain's intended audience.

**Every spare sits at Frontier Station.** All five Prospectors, plus pirate-7's
Drillship and Excavator, are stored there. That makes Frontier Station the
natural home base: fitting, rebuys and post-loss recovery all happen in one
place, and hunting belts should be chosen for proximity to it.

**The stored Prospector is the free Tier-0, and it has exactly one weapon slot.**
Verified via `get_ship` on pirate-6's: `tier: 0`, `starter_ship: true`,
`price: 0`, `weapon_slots: 1`, `utility_slots: 2`, `defense_slots: 1`, hull 100,
shield 50, armor 5, speed 2. CPU 4/12 and power 10/25 used, so there is headroom.

**Only pirate-6 is armed, and the operator armed it.** The class
`default_modules` is `["mining_laser_i"]` — a mining laser and nothing else. The
Pulse Laser I on pirate-6's Prospector was fitted by hand. **The other four
Prospectors carry a mining laser only and cannot fight.**

So the fleet needs four weapons bought, not zero:

| Item | Cost | Requires | Note |
|---|---:|---|---|
| `pulse_laser_i` | 2,500 | **nothing** | damage 10, energy, reach 3, cooldown 1 |
| `pulse_laser_ii` | 6,900 | weapons 2 | the first upgrade the chain unlocks |
| `focused_beam_i` | 8,700 | weapons 3 | bypasses shields |
| `pulse_laser_iii` | 13,000 | weapons 4 | |

**`pulse_laser_i` is the only laser in the catalog with no skill requirement**, so
at weapons 0 it is the roster's sole option — the decision makes itself. Arming
four agents is **~10,000cr** at base value against ~77k held across the roster,
which is affordable but not trivial: pirate-10 holds 6,661 and can cover one fit
with room to spare.

The upgrade ladder is then driven by weapons XP, which is what the fleet exists
to earn — `pulse_laser_ii` at weapons 2 is the first rung.

**Open question for the plan: does the Drillship have a weapon slot?** All five
agents are flying Drillships (hull 140) and would be switching *down* to a
Prospector (hull 100). If the Drillship has a weapon slot, arming it in place is
strictly better — more hull, and no ship-switch step. The local `ship_classes`
table is empty, so this needs a live `get_ship` or a catalog refresh to answer.

**Weapon choice: lasers only.** Energy weapons need no ammunition and no
reloading, so a hunt loop never has to break off to resupply. The catalog does
carry ammo-fed weapons — `neural_disruptor` pairs with
`neural_disruption_charge_pack` — and picking one would add a consumable
dependency, a resupply trip between hunts, and a new failure mode (out of ammo
mid-fight) for no benefit at this tier. The Prospector's single weapon slot
holds a Pulse Laser I; keep it that way, and upgrade along the laser line.

### Buying a warship: checked, and the free Prospect still wins

Pirate strongholds sell real combat hulls, so the obvious question is whether
the fleet should buy out of the starter tier. It should not — the numbers say
so twice over.

**The good ship is out of reach.** `eviction_notice` (Tier-3 pirate cruiser,
hull 480, shield 200, recharge 4, **five** weapon mounts, two defense slots) is
sold at Mera Sanctum Station in zaniah for **145,476cr**. The entire hunt roster
holds ~76,900cr across all five agents, so the fleet cannot afford *one* between
them. It is a superb hunting platform — at `shield_recharge: 4` against a
belt-grazer's 4/tick its net shield drain is exactly zero, meaning it cannot be
worn down by the quarry at all — and it is simply not purchasable at this
bankroll. Revisit only if the fleet ever holds ~150k spare.

**Tier-1 Combat hulls are plentiful and buyable in empire space** — thirteen in
the catalog, none with a `piloting_required`. Ranked by what this fleet actually
runs on (shield pool and recharge, since shields are renewable and hull is a
debt that needs a station):

| | `prospect` free | **`paradox`** 96,399 | `underwriter` 70,151 | `axiom` 77,476 | `levy` 23,400 |
|---|---:|---:|---:|---:|---:|
| Faction | — | voidborn | nebula | solarian | crimson |
| Hull | 95 | 60 | 130 | 120 | 103 |
| **Shield** | 50 | **130** | 70 | 75 | **7** |
| **Shield recharge** | 1 | **4** | 2 | 2 | 1 |
| Armor | 4 | 2 | 6 | 7 | 6 |
| Weapon slots | 1 | 2 | 2 | 2 | 2 |
| **Defense slots** | 1 | **2** | 1 | 1 | 1 |

**`paradox` is the fleet's eventual ship, and the reason is `shield_recharge:
4`.** Against a belt-grazer's 4 kinetic per tick that is a net shield drain of
*exactly zero* — the same immunity the 145k cruiser buys, on a 96k hull. Its
130-point pool and two defense slots are secondary to that. Most striking, its
`default_modules` are `["pulse_laser_i", "pulse_laser_i", "shield_booster_i",
"shield_recharger_i"]` — two ammo-free lasers plus **both** shield modules,
which is precisely the fit derived independently above. It arrives configured
for this job.

`underwriter` is the cheapest credible alternative at 70,151 and stays in empire
space (nebula), trading half the recharge for double the hull. `axiom` ships a
`focused_beam_i`, which **bypasses shields** — of specific interest against
`cracking_the_shell`'s armoured slag-tortoises, though armour and shields are
different mechanics and this needs checking rather than assuming.

Avoid the cheap end for a reason the model makes obvious. `levy` (23,400) has a
**shield of 7**, so essentially every point of incoming damage goes to hull and
every pass ends in a repair trip; `lawn_dart` (15,804) has **no defense slot at
all** and cannot fit a recharger. Both cost real credits *and* forfeit the
free-replacement property, converting a loss from "free armed hull plus travel
time" into a rebuy.

**Conclusion for now: stay in Prospectors and spend on modules.** Not because
the tier-1 hulls are bad — `paradox` is clearly better — but because the roster
holds ~76,900cr across all five agents and the cheapest credible hull is 70,151
for *one*. ~10,000cr arms all five with `pulse_laser_i` and 6,500cr more fits
five `shield_recharger_i`: 16,500cr to equip the entire fleet, against 70k for a
single ship four of them still could not fly.

**Revisit at ~100k per agent.** `paradox` is the target, and the trigger is
bankroll, not doctrine. Note the fleet's own earnings are the path there:
`cracking_the_shell` pays 1,500cr and the repeatable difficulty-2 wildlife
missions pay 1,200–1,300 each.

### The defense slot is worth more than the weapon upgrade

The Prospect's single defense slot is the highest-leverage fitting decision the
fleet makes, because of the asymmetry live play established: **shields
regenerate, hull does not.** Hull is repaired only by docking at a station or by
a Combat Support ship with a repair kit, so every point of hull lost is a debt
carried until the next dock. Shields come back for free — a live ship observed
at 35/50 mid-session was back at 50/50 shortly after disengaging.

So the defense slot's job is to make the *renewable* buffer outlast the fight.
Captured numbers: a Prospect reports `max_shield 50`, `shield_recharge 1`,
`max_hull 95`; a Belt-Grazer deals **4 kinetic per tick**. Shields therefore
drain at a net 3/tick and the 50-point pool covers roughly 17 ticks of fire.

The four defense modules with no skill requirement, against that 4/tick attacker:

| Module | Cost | Effect | CPU/Pwr | Ticks of fire absorbed |
|---|---:|---|---|---:|
| *(empty)* | — | shield 50, recharge 1 | — | ~17 |
| **`shield_recharger_i`** | **1,300** | **`shield_recharge_bonus: 2` → recharge 3** | 2/5 | **~50** |
| `shield_booster_i` | 1,700 | `shield_bonus: 25` → shield 75 | 2/4 | ~25 |
| `hull_reinforcement_i` | 880 | `hull_bonus: 25` → hull 120, cargo −5 | 1/0 | ~17 |
| `armor_plate_i` | 490 | armor +5, **`speed_penalty: 1`** | 1/0 | ~17 |

**`shield_recharger_i` wins, and it is not close.** Tripling recharge cuts the
net drain from 3/tick to 1/tick, which roughly triples sustain — twice what the
larger shield pool buys, for 400cr less. It compounds twice over: the
between-engagement shield-recovery wait also shrinks threefold, so passes finish
faster, and against any quarry dealing 3/tick or less the ship stops losing
shield at all. Both cost and fit are trivial: 1,300cr each (6,500 for five)
against a Prospect's 8 spare CPU and 15 spare power.

`armor_plate_i` should be avoided outright despite being cheapest: `speed_penalty
1` on a speed-2 hull raises `jumpTicks = max(1, 7−speed)` from 5 to 6, taxing
every jump the fleet makes for +5 armor. `hull_reinforcement_i` is the fallback
if shield recharge turns out not to apply during combat — it raises the
non-renewable pool by 25 for 880cr — but it treats the symptom rather than the
mechanism.

> ⚠️ **Unverified: whether `shield_recharge` ticks during combat or only out of
> it.** The whole table above assumes in-combat regeneration. A live check
> — watch `shield` across consecutive `battle_damage` ticks — settles it, and
> flips the recommendation to `shield_booster_i` if regeneration pauses in
> battle. Worth doing before buying five.

**Damage-typed defenses exist, are cheap, and still lose to the recharger.**
Belt-grazers deal `kinetic`, so the typed counters are the obvious specialist
answer. They are far more affordable than the `adaptive_shield` line
(`adaptive_shield_i` is 21,000cr at `shields: 4`):

| Module | Cost | Effect | Requires | Ticks of grazer fire absorbed |
|---|---:|---|---|---:|
| **`shield_recharger_i`** | **1,300** | recharge 1 → 3 | **nothing** | **~50** |
| `kinetic_shield_hardener` | 3,800 | `kinetic: 25` resist → 3/tick | `shields: 2` | ~25 |
| `kinetic_hull_hardener` | 740 | `kinetic: 30` resist, hull only | `armor: 2` | ~17, then hull lasts ~43% longer |

The recharger still wins on the arithmetic — 25% off incoming damage takes the
net drain from 3/tick to 2/tick, while tripling recharge takes it to 1/tick, so
the hardener buys half as much sustain for nearly three times the price.

> 🔴 **Retracted: `required_skills` on modules is NOT enforced at install.**
> An earlier revision argued the skill gate "decides it outright", since both
> hardeners need `armor: 2`/`shields: 2` while `shield_recharger_i` needs
> nothing. That argument is dead. The operator installed **Shield Booster IV**
> — `required_skills: {shields: 4}` — and the server accepted it:
> `{"command":"install_mod","result":{"message":"Installed Shield Booster IV in
> defense slot.","cpu_used":27,"power_used":45}}`.
>
> My error was treating a catalog field as an enforced rule without testing it.
> That is the third time on this branch that a documented property turned out
> not to describe the server — after the fabricated `NearbyCreature` fields and
> the `is_npc` retraction. **CPU and power ARE enforced** (the response reports
> both as running totals), so the real constraints on a fit are slots, CPU,
> power and credits — not skills.

**So the whole module ladder is open, and the tier-II parts change the answer.**
Re-ranked with the skill gate removed, for a Prospect's single defense slot with
8 spare CPU and 15 spare power:

| Module | Cost | Effect | CPU/Pwr | Net shield drain vs a grazer's 4/tick |
|---|---:|---|---|---:|
| `shield_recharger_i` | 1,300 | recharge → 3 | 2/5 | −1/tick |
| **`shield_recharger_ii`** | **7,500** | **recharge → 6** | 3/8 | **+2/tick — shields GROW mid-fight** |
| `shield_booster_ii` | 7,200 | shield → 100 | 3/6 | −3/tick, on double the pool |
| `kinetic_shield_hardener` | 3,800 | 25% resist → 3/tick | 3/6 | −2/tick |

**`shield_recharger_ii` is the fleet's module.** At recharge 6 against 4/tick
incoming, shields do not merely hold — they *regenerate faster than the quarry
can strip them*, so a belt-grazer can never reach hull no matter how long the
fight runs. That converts the shield-recovery wait between engagements into a
formality and makes hull damage a non-event for the whole difficulty-1 tier. It
fits the Prospect's spare CPU and power with room left.

At 7,500cr each, five is **37,500cr** against the roster's ~76,900 — affordable,
though no longer trivial. `shield_recharger_i` at 1,300 remains the budget
option and still yields a net −1/tick.

> ⚠️ **Verify before buying five.** Install being unenforced does not prove the
> *effect* is ungated — a module might install and then underperform, or the
> lenience might be a server bug that gets fixed. Fit one `shield_recharger_ii`
> on one agent and confirm `get_ship` reports the raised `shield_recharge`
> before committing credits.

### ✅ Nothing needs buying — craftsman-1 already holds the modules

craftsman-1's storage at grand_exchange_station carries the defense modules
outright, so the fit-out cost is a transfer, not a purchase:

| Module | Held | Effect |
|---|---:|---|
| `shield_booster_iv` | **150** | +200 shield |
| `shield_booster_iii` | 43 | +100 shield |
| `shield_booster_ii` | 66 | +50 shield |
| `shield_booster_i` | 202 | +25 shield |
| `shield_recharger_ii` | **1** | +5 recharge |
| `shield_recharger_i` | 1 | +2 recharge |
| `kinetic_shield_hardener` | 2 | 25% kinetic resist |
| `adaptive_shield_i` / `iii` | 26 / 2 | adaptive resist |

Both candidate fits clear the Prospect's 13 CPU / 26 power:

| Fit (weapon + defense + utility) | CPU | Power | Shield | Recharge | vs 4/tick |
|---|---:|---:|---:|---:|---|
| `pulse_laser_i` + `shield_booster_iv` + `mining_laser_i` | 12/13 | 25/26 | **250** | 1 | −3/tick → ~83 ticks |
| `pulse_laser_i` + `shield_recharger_ii` + `mining_laser_i` | 7/13 | 18/26 | 50 | **6** | **+2/tick → never falls** |

**Recommended allocation, costing nothing:** the single `shield_recharger_ii`
goes to one agent, where it makes belt-grazers literally unable to reach hull;
the other four take `shield_booster_iv` and its 250-point pool, which absorbs
~83 ticks of grazer fire — far more than the ~10-tick engagements observed. The
`shield_booster_iv` fit is tight at 12/13 CPU and 25/26 power, so **dropping
`mining_laser_i` frees 2 CPU and 5 power** on a fleet that never mines, and is
worth doing for the headroom alone.

`adaptive_shield_iii` (+200 shield *and* 35% resist) is strictly better than the
booster but needs 12 CPU against the Prospect's 13, leaving nothing for a
weapon. It is the right module for a tier-1 hull later, not for a Prospect.

> The two `kinetic_shield_hardener` are now installable (see the retraction
> above) and kinetic is exactly what belt-grazers deal — but with 250 shield or
> a recharge of 6 already making hull unreachable, resistance has nothing left
> to protect. Hold them for a quarry that out-damages the shield.

`kinetic_hull_hardener` is the one to revisit first anyway once `armor: 2`
exists — at 740cr it is the cheapest defense module in the catalog, and 30% off
kinetic damage protects the *non-renewable* pool, which is the resource that
actually costs a station visit. It is complementary to the recharger rather than
competing: the recharger keeps shields up, the hull hardener limits the damage
of the fights where shields fall anyway. But the Prospect has **one** defense
slot, so it is one or the other until a bigger hull.

Record each quarry's damage type as observed. A fleet that later meets thermal
or explosive wildlife wants `thermal_hull_hardener` (580cr) or
`explosive_hull_hardener` (640cr) rather than a generic fit — the typed modules
are cheap precisely because they are narrow.

> ⚠️ **`agent_hulls.modules` cannot tell you whether a hull is armed.**
> `OwnedShip.Modules` is an opaque `int` from `list_ships`, and it reported **1**
> for a Prospector carrying **2** modules — most likely counting non-default
> fittings only, since `default_modules` for the class is `["mining_laser_i"]`.
> Judging armament from that field says "unarmed" for a ship with a laser in its
> weapon slot. Use `get_ship` for loadout truth. Capturing real module detail is
> a worthwhile ledger extension and a prerequisite for any targeting logic.

Two safety rules regardless:

- **Flee threshold.** Below a hull fraction, switch to `flee` stance and abandon
  the mission. Losing a hull is affordable, not desirable.
- **Rebuy budget per agent.** A cap on replacement spend, so a systematically
  losing configuration stops rather than grinding credits away. Exceeding it
  removes the agent from the fleet rather than continuing.

## Mission scope and the difficulty gate

Missions are `type: combat`. Eleven templates exist. **The gate is
difficulty-first, and this is the inverse of how the delivery gate works.**

The delivery gate selects on net credits. Applied to combat, that logic marches
straight into `leviathan_bounty`: difficulty 6, 8,000cr, the best XP in the
table (weapons 40 + xenobiology 60 + tactics 20) — *and repeatable*, so it would
be chosen not once but forever. Every reward signal points at the boss that
kills you.

**Two gates, not one.** A single difficulty number cannot express what is
actually wanted, because difficulty 2 holds two different kinds of mission: the
three repeatable wildlife culls (passive quarry, safe) *and* `pirate_bounty` /
`convoy_defense`, which shoot back. Separating them takes a second gate.

**Gate 1 — difficulty cap, starts at 1.** A constant, raised deliberately as
weapons level climbs. It starts at **1** so the fleet proves itself on passive
grazers before anything harder, and moves to 2 once hunts complete reliably and
weapons XP is confirmed to move. It is never derived from a reward score.

**Gate 2 — wildlife only.** `pirate_bounty` and `convoy_defense` are excluded
regardless of difficulty, until the operator lifts it explicitly. This is what
lets gate 1 rise to 2 and admit the three repeatable culls *without* also
admitting the two missions that fight back.

⚠️ **A difficulty-1 cap leaves exactly one mission, and it is not repeatable.**
`first_hunt_belt_grazers` is the only difficulty-1 combat mission known. Once
each agent completes it, the fleet has no work until gate 1 rises to 2. That is
acceptable for a canary but not a steady state, so gate 1 rising is part of the
rollout, not an afterthought.

✅ **The chain's second mission is now captured.** `cracking_the_shell` is
**difficulty 2** — the case this section warned about, so the difficulty-1 cap
would have stalled the chain at step one. **Operator decision: the cap stays at
1 and a chain continuation is exempt from it.** A mission is admissible if
`difficulty <= cap` OR it is the `chain_next` of one this agent has already
*completed*. So the curriculum advances at its own pace while the open board
stays conservative — `grazer_cull` and the other difficulty-2 wildlife missions
remain refused. The exemption waives gate 1 only; gate 2 (wildlife-only) still
applies in full.

| | |
|---|---|
| Mission | `cracking_the_shell` |
| Objective | Hunt 3 Slag-Tortoises at an asteroid belt |
| Species | `slag_tortoise` — 90 hull, role `grazer`, shares the iron belts |
| Difficulty | 2 |
| Rewards | 1,500cr, weapons 55, xenobiology 20 |
| Chains to | `ghosts_in_the_cloud` — unseen, difficulty unknown |

**Armor is a mechanic our damage model does not have.** The dialog: slag-tortoises
"armor away about half of every shot you land", and "won't flee and won't hurt
you much". So the fight is long and safe rather than short and risky — roughly
10 ticks per kill against a belt-grazer's 4, on 90 hull at ~50% absorption
against 18/tick. Two consequences: the per-engagement tick budget must survive a
legitimate grind, and the no-progress give-up must treat "hull decreased at all"
as progress rather than "decreased by enough", or a slow grind reads as a stall
and the fleet abandons fights it is winning. No armor value is exposed by any
captured payload — only the dialog's "about half" — so the executor cannot
predict fight length and must rely on the tick budget.

⚠️ **The chain exemption has an accepted cost.** `ghosts_in_the_cloud` will be
auto-admitted at whatever difficulty it turns out to be, once
`cracking_the_shell` completes. That is the trade for a curriculum that
progresses without a gate change at every step, but it means an unseen mission
of unknown difficulty is admitted without review. The admitted-continuation log
line — naming the predecessor and the difficulty being waived — is what keeps
that visible rather than silent.

The full admissible set once gate 1 reaches 2 and gate 2 still holds:

| Mission | Diff | Credits | Skill XP | Repeatable |
|---|---:|---:|---|---|
| `first_hunt_belt_grazers` | 1 | 1,000 | weapons 50, xenobiology 15 | no (chain → `cracking_the_shell`) |
| `grazer_cull` | 2 | 1,200 | weapons 10, xenobiology 20 | **yes** |
| `ice_field_thinning` | 2 | 1,300 | weapons 10, xenobiology 20 | **yes** |
| `nebula_drift_hunt` | 2 | 1,300 | weapons 10, xenobiology 20 | **yes** |
| `pirate_bounty` | 2 | 2,000 | weapons 20 | no |
| `convoy_defense` | 2 | 2,000 | nebula_attunement 25, weapons 25 | no |

Everything at difficulty 4 and above is excluded by the cap, including the
leviathan. The cap is a constant, raised deliberately as weapons level climbs —
never derived from a reward score.

`pirate_bounty` and `convoy_defense` are held out by gate 2 for now — the
operator's call, "till things are working and skilled". They stay excluded until
wildlife hunting is reliable and the roster has real weapons levels, at which
point lifting gate 2 is a one-line change.

## Why XP is the payload, not credits

Mission credit payouts are running at roughly 37% of face value because the
empire treasury is broke — dev-confirmed, not a bug. **XP is paid in full; only
credits are short.** That was the finding that cracked the payout mystery.

So a 1,000cr First Hunt may not clear the realized-ratio gate on credits alone,
and the gate would reject the very mission this fleet exists to run. The gate
already carries an exemption for smuggling below L3; combat missions under the
difficulty cap need the equivalent — **judged on XP, not realized credits.**

The XP compounds in ways worth stating, because they justify the grind:

| Skill | Effect per level |
|---|---|
| Weapons | +1% all weapon damage, +0.2% crit chance |
| Gunnery | +1% damage with all weapon types |
| Tactics | +1% accuracy, +1% evasion, +1% combat speed |
| Armor | +1% armor effectiveness, +1% hull HP |
| Shields | +1% shield capacity, recharge, and damage resistance |
| Bounty Hunting | +1% bounty reward payouts |
| Piracy | +1% raid loot, **+1% scan evasion vs customs and NPC inspections** |
| Leadership | fleet coordination bonuses |

Two consequences. **Weapons and Gunnery stack** — both grant +1% damage, so
levelling both is +2%/level compounding. And **Piracy feeds the smuggling
fleet**: scan evasion against customs is exactly what the smuggling couriers
need, so combat XP is not confined to combat. Leadership is the mechanical basis
for band 3's crewed design, later.

## The hunt executor

The one genuinely new behaviour. Per the server docs, the loop is:

1. **Find a herd.** `get_nearby` at a wildlife-bearing POI returns a
   `creatures` list. Not only asteroid belts: captures put creatures at a gas
   plume (`market_prime_gas_plume`), a cryobelt (`gold_run_cryobelt`) and an
   asteroid belt (`commerce_fields`), in different systems — so POI selection
   reads the KB `pois.type` values rather than hardcoding one type. Herds
   gather where ore is still *rich*, so a mined-thin belt is the wrong target —
   `poi_resources` depletion data already tells us which belts are rich.
   `survey_system` reports what lives in a system.

   **The board and the quarry are in different places.** A mission board
   exists only at a station; no creature does. A pass therefore has two legs —
   dock and take the mission, then undock and travel to a wildlife POI — and
   an executor that reads the board and calls `get_nearby` at one POI fails
   whichever place it happens to be standing.
2. **Engage one creature — the right species, and the least dangerous one.**
   `hunt <creature_id>`, or equivalently `attack` on a creature id. **Wildlife
   never dogpile** — attacking one grazer does not pull in the rest of the
   herd. This is the property that makes difficulty-1 hunting genuinely safe,
   and it must be relied on explicitly rather than assumed.

   > **Retracted: grazers are not harmless.** Both the mission text ("slow,
   > iron-plated filter-feeders that won't fight back") and this document's
   > own earlier rationale ("the perfect quarry to learn your guns on") are
   > flavour, not mechanics. **Belt-grazers shoot back** — captured
   > `battle_damage`: `Belt-Grazer -> us, kinetic, 4 damage, weapons_fired
   > ["Belt-Grazer (natural)"]`. The `role` filter still earns its place by
   > keeping the fleet off predators, but "grazer therefore safe" is gone. The
   > hull-abort gate is safety-critical, not defensive polish.

   **Species, not role, decides mission credit.** One captured belt held three
   grazer species at once — `slag_tortoise` (90 hull), `patina_grazer` (60)
   and `belt_grazer` (60) — and only the last counts for a Belt-Grazer
   mission. Two of the three decoys share the target's role *and* its hull, so
   nothing but the species id distinguishes them. Every wildlife objective is
   scoped this way:

   | Mission | Objective text | Species |
   |---|---|---|
   | `first_hunt_belt_grazers` | Hunt 3 Belt-Grazers at an asteroid belt | `belt_grazer` |
   | `grazer_cull` | Hunt 8 Belt-Grazers | `belt_grazer` |
   | `ice_field_thinning` | Hunt 6 Rime-Grazers | `rime_grazer` |
   | `nebula_drift_hunt` | Hunt 6 Sift-Rays | `sift_ray` |

   The objective carries **no machine-readable target** — no `target_id`, no
   `item_id`, only prose and a quantity. So the executor holds a small curated
   `mission_id → species` table rather than parsing the description, and a
   server-supplied `TargetID` outranks the table whenever one appears. A POI
   holding none of the required species is the "wrong place" case: log it
   naming what was needed against what was present, and end the pass rather
   than burning engagements on decoys.

   **`role` decides what is safe to engage, and it is not optional.** A single
   live `get_nearby` list held eight creatures: seven `grazer`s from 45 to 220
   hull, and a `Tempest-Eel` — `role: predator`, 280 hull — standing among
   them. A quarry picker that sorts on hull and ignores `role` will engage the
   predator with a starter Prospect and one pulse laser, which voids the
   entire safety case for a difficulty-1 fleet. Filter on `role`: engage
   grazers and scavengers, never a predator while `wildlifeOnly` is in force,
   and log the refusal naming the role. `role` is also visible mid-fight — the
   quarry's `ship_name` in the battle payload is its role.

   Filter on `role`, never on a species allowlist. Eight species appeared in
   that one list (`pilot_whale`, `bell_jelly`, `hollow_pilgrim`, `rime_grazer`,
   `pressblister`, `tempest_eel`, `drift_ray`, `sift_ray`) and they vary by POI
   and system; `role` is the stable classifier.

   Prefer the *weakest* eligible quarry, not the healthiest. A 45-hull
   Bell-Jelly and a 220-hull Pilot-Whale pay the same objective credit for
   very different fights.
3. **Fight, and keep closing the range.** Zone/stance tactical combat via
   `battle`, polling `get_battle_status` (a free query, no tick cost). The flee
   threshold lives here.

   A battle opens with both sides in zone `outer`, and firing from there is
   refused outright — `{"code":"out_of_range","message":"Your weapons can't
   reach the enemy at this range — 'advance' to close the distance."}`. Since
   the fleet's whole armament is short-range lasers, `battle advance` is not
   an optimisation; nothing lands without it.

   **Advance is a per-tick obligation, not an opening move.** Low-level
   wildlife flees and reopens the gap mid-fight, so the loop has to re-read
   the zone every tick and re-close as the quarry runs. That makes the
   engagement a control loop rather than a passive wait, and it needs a
   genuine no-progress give-up — quarry hull not dropping, or zone not
   improving across N ticks — or a worker will spend a whole pass chasing one
   grazer it cannot catch. A fleeing quarry should be abandoned for the next
   creature, disengaging first rather than calling `hunt` again mid-battle.

   `pkg/spar` already implements this loop (`runPolicyLoop`, `BuildView`,
   `battleOver`, `NewAggressor`, the `outer/mid/inner/engaged` zone ladder)
   and is unit-tested; the hunt executor drives combat through it rather than
   hand-rolling a second one.

   **Zone appears to be shared, not per-ship.** Captures of `battle_started`
   and `battle_update` show *both* participants reporting the same zone —
   `outer`/`outer`, then `inner`/`inner` after an advance. Read that way,
   zone is the distance *between* the sides, which is the mechanical reason
   advance has to repeat: a fleeing quarry drags the shared distance back out
   from under you. The loop should be written to hold under either reading —
   compare our own participant's zone against the target rung and re-advance
   when it regresses.

   Of spar's four rungs, `outer` and `inner` are wire-attested; `mid` and
   `engaged` are still repo-internal assumptions, and one observed advance
   went `outer` → `inner` without `mid` ever appearing. So the rungs are not
   known to be contiguous: never count advances to infer position, always
   re-read the zone, and keep spar's "unrecognized zone maps to 0" default.
4. **Repeat to the objective count**, e.g. "Hunt 3 Belt-Grazers".
5. **Loot the carcass.** Killing a creature drops a carcass wreck. The
   executor matches its own kill by `victim_id` against the creature id it
   engaged — `killer_id` cannot distinguish this pass's second kill from its
   first, and another hunter's wreck at the same belt matches a `type`-only
   filter. The wreck expires 30 minutes after the kill, so loot per-kill
   rather than batching at the end of a twelve-engagement pass.

   The wreck doubles as the **kill receipt**: no wreck bearing our
   `victim_id` means the creature is not confirmed dead, and the objective
   count must not advance on an assumption.

### Client gaps this exposes

Two, both small and both required before any of the above runs:

- **`GetNearbyResponse` has no `Creatures` field.** It models `Nearby`,
  `Pirates`, and `EmpireNPCs` only, so the client currently cannot see wildlife
  at all. Hunting is impossible until this is decoded.
- **No `Hunt` method on `GameClient`.** `HuntResponse` exists in `serverapi` and
  the API monitor registry, but nothing sends the command. `Attack(creatureID)`
  is documented as equivalent, so this is optional — but an explicit `Hunt` is
  clearer and the response type already exists.

`get_battle_status` lists every combatant with `kind`
(player/pirate/police/drone/creature/station) and `is_npc`, which is how the
executor distinguishes its quarry from anything else that wanders in.

> **That sentence is correct about the wire. The Go structs are what fall
> short.** A `get_battle_status` reply captured 2026-08-08 carries
> `"is_npc": true` and `"kind": "creature"` on the quarry, exactly as claimed.
>
> An earlier revision of this note asserted `is_npc` was "not on this
> response" and lived only on `get_system`'s `ActiveBattleParticipant`. That
> was wrong. It was reasoning from the Go struct's missing field back to the
> wire, which is the same mistake in the opposite direction as the one that
> put fabricated field names into `NearbyCreature` — inferring the server from
> our code instead of from a capture. Left visible rather than quietly edited,
> because being burned twice in opposite directions is the useful lesson: only
> a capture settles what the server sends.
>
> What is genuinely missing is on our side:
>
> - `serverapi.BattleParticipant.Kind` exists but is **silently dropped by the
>   parser** — `parseBattleStatusData` copies the other ten fields and omits
>   it, so `game.BattleParticipant` has no `Kind`.
> - `serverapi.BattleParticipant.IsNPC` does not exist, though the wire sends
>   it.
> - The whole `combat_state` block is **unmodelled** (see below).
>
> Separately and confirmed: the reply carries **no `action` key at all**, and
> the client writes the raw `battle_status` key only from inside an `action`
> switch. So that key is never populated for this command and
> `GetRawJSON("battle_status")` returns empty in production — the same
> key-drift class already recorded for `browse_ships`/`owned_ships`, but here
> confirmed rather than suspected. A shape-based fallback keyed on
> `battle_id` + `participants` is required, not optional.

### Range is numeric, and `combat_state` is where it lives

The captured reply carries a `combat_state` block that no struct models:

```json
{"can_escape": true, "effective_speed": 1, "em_disrupted": false,
 "flee_counter": 0, "flee_required": 3, "max_weapon_reach": 3,
 "warp_disrupted": false, "webbed": false}
```

with a `zone_distance` on each participant (6, against a `max_weapon_reach`
of 3). **In range means `zone_distance <= max_weapon_reach`** — a number, not
a label. The `outer`/`inner` zone string is a coarse name over that distance,
so the fight loop measures progress on `zone_distance` and treats the zone
label as cosmetic. This also disposes of the "are the rungs contiguous"
worry: a number needs no ladder. `max_weapon_reach` is read from the wire
every poll and never hardcoded, since it must vary with the fitted weapon.

`flee_counter` / `flee_required` quantify the escape: **fleeing takes three
counts.** An executor that issues a flee stance and returns immediately
abandons its ship three ticks into an escape it never confirms, while still a
participant and still under fire. The abort path polls until the escape
completes or the battle ends, with advancing disabled. `can_escape` reports
whether escape is possible at all.

`auto_pilot` is per-participant — our own ship's state, not a battle-wide
flag. Whether it can be overridden by an explicit stance is still unproven,
so the flee gate must not be assumed to win against it.

## Objective tracking

`kill_creature` objectives are counted (quantity 3 for First Hunt). The runner
today understands only deliver-shaped completion, and `buildMissionCandidate`
rejects non-deliver objectives outright.

Opening that gate is **the last step, not the first**. Allowing `combat` into
`mission_categories` before the executor exists would have workers accept hunts
they cannot perform and strand on them — the same failure mode as the TRADING
missions that stranded fighter-4.

## Testing

- **Difficulty gate** — table test over all 11 combat templates: everything ≤2
  admitted, everything ≥4 refused. `leviathan_bounty` gets its own named case,
  because it is the mission most likely to be admitted by a future reward-based
  regression.
- **Reward-blind selection** — a synthetic difficulty-6 mission with enormous
  XP and credits must still be refused. This is the test that would have caught
  the failure mode before it cost a hull.
- **Hunt executor** — against a fake client: creatures list → engage → battle
  ticks → count reaches quantity → complete. Includes the flee-threshold path.
- **Herd selection** — a rich belt is preferred over a depleted one.
- **XP-based gate exemption** — a 1,000cr combat mission under the realized
  ratio is still accepted on XP grounds.
- **No live-fire test in CI.** Combat costs hulls; the executor is tested
  against a fake, and validated live by canary.

## Rollout

Deliberately incremental, because the failure mode is losing ships:

0. ~~Capture pirate-6..10.~~ **DONE 2026-08-08** — all five captured with
   profile, storage and faction. It did change the plan: the roster is at
   weapons 0 in mining hulls, every spare is a Tier-0 Prospector at Frontier
   Station, and those Prospectors already carry a Pulse Laser I. Refuel
   pirate-6 first (0/130).
1. Decode `creatures`; land the client gaps. Inert.
2. Build the hunt executor and its tests. Inert — nothing dispatches it.
3. **Canary: one agent, one `first_hunt_belt_grazers`, run by hand via
   `play_as`.** Passive quarry, difficulty 1, one agent, observed live. This is
   where the real mechanics get learned and the design meets the server.
4. Stand up the `hunt` fleet with **two** of pirate-6..10, gate 1 at difficulty
   1, gate 2 (wildlife only) on. Note this admits exactly one non-repeatable
   mission per agent — enough to prove the loop, not to sustain it.
5. **Raise gate 1 to 2** once hunts complete reliably and weapons XP is
   confirmed to move. This is what unlocks the three repeatable culls and gives
   the fleet steady work. Grow to all five agents.
6. Record `cracking_the_shell` when it first appears; if the chain needs a
   difficulty above gate 1, exempt accepted chain continuations.
7. Lift gate 2 for `pirate_bounty` / `convoy_defense` last, and only on an
   explicit decision — they shoot back, and the roster starts at weapons 0.

Success signal: weapons XP rising across the roster, hull losses at or near
zero, and a growing body of `get_battle_log` data to learn from.

## What this unlocks

`get_battle_log` returns per-tick entries for **any** battle, including other
players' — full weapon pipeline with hit/crit rolls, resist percentages, and
damage breakdown. The hunt fleet generates a steady supply of its own battles to
mine, which is the substrate for the deferred battle-visualisation and
smart-battle-handler work, and eventually for the pirate bands.
