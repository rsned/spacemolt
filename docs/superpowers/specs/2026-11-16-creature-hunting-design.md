# Creature Hunting for the Hunt Fleet — Design

Date: 2026-11-16 · Status: draft for operator review · Area: `pkg/worker` hunt role

## Why

The hunt fleet (pirate-5, -6, -9, -10, -13) has just moved from legacy mining hulls into
T2 **gauntlets** (3 weapons, 2 defense: shield booster + armor plate). Until now every hunter
sat idle: the only combat missions on offer were Pirate Bounties at difficulty 2, over the
fleet's cap of 1, and the hunt gate skips every creature-part delivery as "not a combat
mission (type "delivery")" (~2,500 such skips in recent logs).

There is easy work available that fits a gauntlet:

- **The hunt chain.** `first_hunt_belt_grazers` (combat, difficulty 1, 1,000 cr, weapons +50,
  xenobiology +15) → `cracking_the_shell` → … with `nebula_drift_hunt` (d2, 1,300 cr) and
  `the_long_hunt` (d3, 3,500 cr, weapons +100) further on. Offered at 12+ empire stations.
- **Station Need: <part>** deliveries. 155 templates, average ~7,000 cr, top 60,526
  (Ember Gland at The Levy); Caustic Sac is asked for at 17 stations, 23–32k at several.
  The job is: go to the belt / field / ice shelf where the creature lives, hunt it, loot the
  parts from the carcass, deliver.
- **Surplus parts** are inputs to Dining, Leisure and Tourism recipes, needed at CJ
  (tourism revenue), later at beid, and by the shuttle fleets for in-flight dining.

The operator's words: these should be "easy XP, credits, and goods to start."

## Success (operator's measures, after a week)

1. Hunter skill levels up (weapons, gunnery, tactics, xenobiology).
2. Hunter credits up.
3. **No ship losses.**
4. A fuller pantry at CJ (creature parts in faction storage).

## Anti-goals

- **Dying to a leviathan or other predator.** This is the one outcome that makes the whole
  feature a failure even if it earns. Gauntlets never engage predators.
- Breaking or slowing the existing hunt behaviour for bounty/combat missions.

## The hunter's day (operator-specified)

1. **Take First Hunt** at the station it is at (or the nearest one offering it), then go to a
   belt with belt-grazers and complete it.
2. **Follow the chain**: each next step as it unlocks, wherever it is offered.
3. **Then the best nearby Station Need**: a creature-part delivery whose creature we know and
   whose hunting ground is reachable within a reasonable number of ticks.
4. **If none are nearby**, check other nearby empire stations' boards.
5. **If all of those are dry** (operator rule):
   a. **if home (CJ) is within 12 jumps**, go home and hunt whatever lives around there,
      banking the parts;
   b. **otherwise pick a new empire capital, jump there, and restart the loop** from step 1.

## Rules

### Fight selection — the predator rule

- A hunter only fights creatures its hull class can handle.
- **Predators are never quarry for a T2 hull.** Juvenile/young leviathans and other young
  predators need at least **T3 with stronger weapons and defense**; full leviathans need a
  **T4 battlecruiser or a squad of T3s**. In this design, gauntlets never engage any of them.
- If a predator is present when the hunter arrives at a ground, it leaves that ground.
- Wildlife does **not** swarm: only the creature under attack fights back. Size each fight
  against that one creature's hull/damage, not the population at the POI.
- Default stance `flee` (shipped 11-16) and the battle flee gate cover fights the hunter did
  not start; the hunt role is exempt from default-flee and fights on purpose.

### Break-off

- **Operator rule: if the hunter's hull is being worn down and the target's is not, the hunter
  retreats when its own hull reaches 50%.** If the creature is losing hull too (being worn
  down at least as fast), the hunter may keep fighting past 50% to finish it. This is a trend
  judgement over the fight's ticks, not a single snapshot.
- *Designer's call:* the existing hard floor (`FleeAtHull`, 0.35) stays as a last resort, so a
  close trade that keeps going still breaks off before the ship is at risk ("no ship losses").
- *Designer's call:* this rule applies to **creature fights only**; bounty and other combat
  missions keep today's behaviour (anti-goal: don't change bounty behaviour).
- After breaking off, it repairs at the nearest station with repair service before taking
  more work.

### Data capture after every fight

After each fight (win, loss or break-off) the hunter runs the DB update steps so our creature
records fill in:

- `capture_wildlife_attacks`
- `get_wrecks`, then `loot_all` on the carcass (all items, not just the mission part)
- the existing kill/drop recording (`wildlife_kills`, `wildlife_kill_drops`)
- **`scan <creature_id>`** on any creature that is not already in our records
  (`wildlife_species`), whether or not it was fought.

This is how unknown part sources get found: Ember Gland (13 stations asking),
Krypton Bladder (12), Radiant Bile (8) and Sol Nacre (5) have no recorded source yet.

### Parts logistics

- Each hunter **centralises on one base station** to work out of, and banks parts there in
  its own station storage (empire stations have no faction storage for us).
- **Operator decision: hand-off is `send_gift`.** Periodically the hunter gifts its surplus
  parts to a hauler, which carries them to CJ (beid later); at CJ they go into faction storage
  (gifts land in the recipient's storage; the existing faction_deposit sweep moves them).
  Shuttles draw from that stock for in-flight dining.
- Each hunter needs the **gift unlock** (1,000 earned credits): run the
  market_participation_buying mission (buy 10 of the cheapest item, `complete_mission
  <mission_id>`) once per hunter if it does not already have it.
- *Designer's call:* reuse the haul-leftovers path (haulers already gift failed-arbitrage cargo
  into CJ, see memory `reference_haul_leftovers_to_cloverfield`) rather than invent a new
  consign rule; `ConsignRule`'s exact-item allow-list does not fit ~155 part types.
- It keeps the mission quantity for an active delivery; everything else is surplus.

## What exists today (build on it)

`pkg/worker/hunt.go` (~1,800 lines) and friends already provide:

- board read + mission accept (`huntBoardJob`, `huntAcceptMission`), chain waivers above the
  difficulty cap (`hunt_chain.go`), admission (`hunt_gate.go`, `hunt_board_gate.go`);
- ground finding (`huntFindGround`, `huntTravelToWildlifePOI`, `huntLocalWildlifePOI`);
- quarry filtering with a **no-predator rule on two lines** (`huntAdmissibleQuarry` via
  get_nearby role; the `huntPolicy.Decide` guard on battle participants);
- the fight loop (`huntFight`, `huntPolicy`) with a hull-fraction break-off (`FleeAtHull`);
- carcass looting and kill/drop recording (`huntLootCarcass`, `huntCaptureKill`);
- wildlife sighting capture (`huntCaptureWildlife`), repair at dock (`huntRepairAtDock`),
  recovery to a station (`huntRecoverToStation`);
- consignment to haulers exists elsewhere (`consign.go`, `haul_consign_*`).

## What is new

1. **Delivery admission.** The hunt gate admits `Station Need` creature deliveries
   (`creature_<station>_<part>` templates, type `delivery`) whose part has a known source
   creature that is not a predator, with its own difficulty cap separate from the bounty cap
   of 1 (these are difficulty 2).
2. **Part → creature → ground.** Look up which species dropped the part
   (`wildlife_kill_drops` ⋈ `wildlife_kills`) and where that species is seen
   (`wildlife_sightings`, `wildlife_surveys`); hunt there until the hold has the quantity,
   then deliver.
3. **Job choice in the operator's order**: chain step → best nearby Station Need (reward vs
   travel + hunt ticks) → nearby empire stations → another capital's area, or home hunting.
4. **Break-off rule** changes from a single hull fraction to the two-sided 50%/50% rule.
5. **Per-fight capture**: `capture_wildlife_attacks`, `loot_all`, and `scan <creature_id>` for
   unknown species (the game client's `Scan` today only scans the area; a targeted scan is
   needed).
6. **Base station + consignments** of surplus parts to a hauler bound for CJ.

## Data to draw on

- `mission_templates` (28,010 rows: rewards, `chain_next`, difficulty),
  `mission_template_locations` (which station offers what), `mission_completions`
  (1,229 rows, 101 with `chain_next`: actual payouts and XP).
- `wildlife_kills`, `wildlife_kill_drops` (193 kills, 29 species, 166 carcasses read),
  `wildlife_sightings`, `wildlife_surveys`, `wildlife_species`, `wildlife_attacks`.
- Memory: belt-grazer grounds (kochab / ironpeak), combat roadmap (enter combat via
  wildlife / First Hunt), wildlife never swarms, hunt fleet rehull.

## Settled by the designer (from the builder check)

- **Chain steps** get the same predator / hull-class check as deliveries; if a step's target
  is beyond a T2 hull, the hunter stops the chain there (the existing chain waiver only lifts
  the difficulty cap, never the predator rule). **Delivery difficulty cap = 2.**
- **Eligibility is by hull class**, not a fixed agent list: a hunter flying a T2+ combat hull
  takes creature work; pirate-7 and -8 keep today's behaviour and qualify automatically when
  rehulled.
- **Grounds and routes:** empire-policed space only for now, plus the existing stronghold,
  Lawless and hazard (11-10) gates. Strictest option for a zero-losses target.
- **Targeted scan is supported** by the server: `scan` with `target_id` = a wildlife creature
  ID (from get_nearby) always succeeds and returns species, role, danger, hull and lore. The
  game client needs a `ScanTarget(ctx, id)` beside today's area-only `Scan`.

## Fleet scope

- pirate-5, -6, -9, -10, -13 (gauntlets). pirate-7 and -8 stay on Prospectors and sit this
  out until they have gauntlets. pirate-1 is stranded (quarantined) and out of scope.

## Afterwards (not in this build)

- **If the gauntlets perform well for a week**, the operator will look at buying the gauntlet
  **ship license** and building them at home (CJ shipyard) instead of buying from station
  managers. Out of scope here; the week's success measures above are the trigger.
- Upgrading to T3 (warmaul etc.) to take on juvenile predators, and T4 or T3 squads for full
  leviathans.

## Left to the builder

- How "best nearby" is scored, and the default for "a reasonable number of ticks".
- How the base station is chosen per hunter, and the consignment cadence and batch size.
- How a predator is recognised beyond today's role field (e.g. species lists, max hull).
- Whether chain progress is tracked per hunter in KB or in the agent's state file.
