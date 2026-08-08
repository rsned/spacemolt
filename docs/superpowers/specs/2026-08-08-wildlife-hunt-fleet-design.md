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

So the policy is **cheap disposable hulls with a rebuy budget**, with the
understanding that the floor beneath the budget is free and already armed. A
Tier-0 with one pulse laser is sufficient for difficulty-1 passive grazers.

**The Tier-0 is a respawn hull, not a stored asset — do not plan around
switching into one.** Checked against the asset ledger on 2026-08-08, across all
87 captured agents:

- **39 of 87 hold no spare hull at all**, only the one they fly.
- The 48 that do hold spares hold **mining rigs**: drillship (35), prospector
  (33), excavator (26), deeprock_harvester (16). These carry mining lasers, not
  weapons — a legacy of the agents that once ran `auto-miner` to sell ore.
- No agent is sitting on a spare combat fit.

### The roster is already armed — verified 2026-08-08

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

**The stored Prospector is the free Tier-0, and it arrives armed.** Verified via
`get_ship` on pirate-6's: `tier: 0`, `starter_ship: true`, `price: 0`,
`weapon_slots: 1`, `utility_slots: 2`, `defense_slots: 1`, hull 100, shield 50,
armor 5, speed 2 — carrying **Mining Laser I** (utility) *and* **Pulse Laser I**
(weapon: damage 10, energy, reach 3, cooldown 1). CPU 4/12 and power 10/25 used,
so there is headroom to fit more.

So the fleet may need **no outfitting spend at all** to attempt difficulty-1
grazers: a Tier-0 Prospector with a Pulse Laser I is already a weapon-bearing
hull. Confirm the other four match pirate-6 before relying on it — only its
Prospector has been inspected directly.

Credits are thin (6,661–24,657, ~77k across the roster), which is another reason
to start with what is already fitted rather than buying.

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

**Hard cap: difficulty ≤ 2.** That admits exactly the useful ramp:

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

`pirate_bounty` and `convoy_defense` fight back, unlike the grazers. They are in
scope per the operator's decision, but the flee threshold is what makes that
safe, and the rollout below reaches them only after wildlife is proven.

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
4. Stand up the `hunt` fleet with **two** of pirate-6..10, difficulty capped at
   1, chain missions only.
5. Raise the cap to 2 for the repeatable culls once weapons XP is confirmed to
   move. Grow to all five.
6. `pirate_bounty` and `convoy_defense` last — they shoot back.

Success signal: weapons XP rising across the roster, hull losses at or near
zero, and a growing body of `get_battle_log` data to learn from.

## What this unlocks

`get_battle_log` returns per-tick entries for **any** battle, including other
players' — full weapon pipeline with hit/crit rolls, resist percentages, and
damage breakdown. The hunt fleet generates a steady supply of its own battles to
mine, which is the substrate for the deferred battle-visualisation and
smart-battle-handler work, and eventually for the pirate bands.
