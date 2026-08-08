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
> Strict no-loss is the recommendation for the first iteration: it costs almost
> nothing at difficulty 1, and it can be relaxed later once the flee threshold is
> known to work.

> ⚠️ **Unresolved: is the respawn Tier-0 armed?** The operator states the free
> replacement arrives with 1 pulse laser and 1 mining laser. But the Prospector
> class `default_modules` is `["mining_laser_i"]` alone, and of the five stored
> Prospectors only pirate-6's carries a pulse laser — fitted by hand. Both can be
> true if the *respawn grant* differs from the class default, which would be a
> sensible way to give a destroyed pilot a fighting chance. It matters: if the
> respawn hull is unarmed, every loss costs another 2,500cr to re-arm and the
> "free floor" is not free. Settle it by observing an actual destruction, or by
> asking; do not assume.

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

⚠️ **The chain's second mission is unknown.** `first_hunt_belt_grazers` chains to
`cracking_the_shell`, which has **never been seen on a board** and is absent from
the knowledge base — difficulty unknown. If it is difficulty 2, a cap of 1 stalls
the chain immediately. Capture it when it first appears and record its
difficulty; the plan should treat an accepted chain continuation as exempt from
gate 1, or the curriculum cannot proceed past its first step.

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

1. **Find a herd.** `get_nearby` at an asteroid belt returns a `creatures` list.
   Herds gather where ore is still *rich*, so a mined-thin belt is the wrong
   target — `poi_resources` depletion data already tells us which belts are rich.
   `survey_system` reports what lives in a system.
2. **Engage one creature.** `hunt <creature_id>`, or equivalently `attack` on a
   creature id. **Wildlife never dogpile** — attacking one grazer does not pull
   in the rest of the herd. This is the property that makes difficulty-1 hunting
   genuinely safe, and it must be relied on explicitly rather than assumed.
3. **Fight.** Zone/stance tactical combat via `battle`, polling
   `get_battle_status` (a free query, no tick cost). The flee threshold lives
   here.
4. **Repeat to the objective count**, e.g. "Hunt 3 Belt-Grazers".
5. **Loot the carcass.** Killing a creature drops a carcass wreck carrying
   carapace and biogas. Secondary value, and the existing salvage commands
   already handle wrecks.

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
