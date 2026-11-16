# Station #2 and Radioactive Outpost — Design

Date: 2026-11-15. Status: draft for operator review.

**Scope update (operator, 2026-11-15):** the radioactive outpost and its mining
squad are **deferred to a later task**. This design now builds station #2 only.
The outpost sections stay as recorded context for that later task. Until then,
station #2's radioactive inputs come from stock already held and from market
buys.

## Intent

CRFT has two station cores and one outpost kit left. This design covers one
core and the kit. The second core is parked for a separate idea the operator has.

The aim is steady credits from **empire station-manager demand**: NPC empire
stations post standing bids that other players fill within hours. Those credits
pay for more upgrades and growth.

Conjunction Junction (CJ, in cloverfield) already runs the first common
production lines. Station #2 adds the next tier up, and a radioactive mining
outpost feeds it the ores that the high-value items need.

**Why now:** the drone fleet already brings in plenty of common and uncommon ore.
CJ's base lines are running. The gap is mid-tier lines and access to
radioactive ores.

## Goals

1. **Found station #2**, a real station run the CJ way, in a site with:
   - a good ore mix in the system and within 1–2 jumps;
   - a short haul to CJ (no 30-jump runs);
   - pirate safety: other stations closer to the nearest stronghold than us.
2. **Run mid-tier lines at station #2** that sell into empire demand.
   - First: lead_shielding, plasma_injector.
   - Then: the radioactive chain, uranium/thorium → neutronium_ingot →
     station_reactor_core.
3. **Deploy the outpost kit** as the base for a radioactive mining squad.
   - The squad is miningbot ships with rad_harvester (I–IV) and
     lead_lined_cargo (I/II).
   - They mine belts 1–2 hops from the outpost and flee when hostiles appear.
4. **Haul the ore** from the outpost to station #2, and move intermediates
   between station #2 and CJ.

## Non-goals

- The second remaining station core; it is a separate idea.
- Trade_authenticator and trade_cipher. They are the most persistent bids, but
  they are only craftable in Nebula empire foundries or a pirate facility, and
  need trade_crystal, which exists only in Nebula space.
- Making our own power cores, which needs tritium and energy crystals. Buy them
  instead: about 6.5k each, 123 listed on 2026-11-15.
- Defenses at the outpost; outposts cannot have any.

## Anti-goals (failure even if it "works")

- **Breaking the treasury floor.** The station's treasury must never drop below
  1M, the same rule as CJ.
- **A facility going offline** for lack of maintenance inputs. This is the
  operator's top fear from CJ.
- **Losing miners to pirates.** Radioactive sites near us are exposed; flee
  first, never fight.
- **Routing through strongholds** without the pirate unlock (standing rule).

## Constraints and facts

### Founding rules

From get_commands.json, 2026-11-13:
- **build_base:** needs a lawless system (no empire, police 0). Only one station
  per system, whoever owns it. Check eligibility in game with `get_base_cost`.
- **build_outpost:** needs a lawless system and a POI with no base already;
  stars and wormholes are excluded.
  - Requires ManageBases permission and an undocked ship carrying the outpost
    kit (size 30).
  - The fee comes from the treasury first; it is not refunded if the outpost is
    dismantled.
  - The outpost is members-only. It comes with faction storage and a faction fuel
    bunker, and there is no maintenance and no rent.
- **Ruled out:** markeb, starfall and the_telescope have the best radioactives
  near us, but they are Outer Rim empire space. No station and no outpost.

### Money

- The_banker donates **10M** to the agent in charge of station #2 to start it.
- Station #2's treasury floor is **1M**.
- **Every facility build** is planned (credits + materials) and approved by the
  operator before it runs, as at CJ.

### Lessons from CJ

Sources: `data/stations/cloverfield/findings.md` and memory
project_cloverfield_home_station.
- A fresh station has 500k faction storage but 0 power and 0 life support, and
  no personal storage.
- Reported build order: locker bay → faction workshop → atmospheric recycler →
  fuel production.
- **Stock every upkeep input before a build finishes.** Locker Bay went short of
  polymer, and the recycler starved.
- Fuel production needs tank capacity first.
- The maintenance cycle is about 3,000 ticks measured, not the documented 100.
- Facility speed: L1 is 5× hand speed, +3× per upgrade. Rent is 3% a day.
- Faction-storage crafting needs Officer rank.
- The founding agent auto-docks, and the station gets its own new POI.

### Mining

- Mining drones extract radioactive ore with no special gear (operator,
  2026-11-15). Without a station nearby, though, the operator prefers ships: see
  memory reference_drones_mine_radioactive.
- The flee path for non-hunt workers already exists: `battle stance flee`
  (memory reference_battle_flee_gate).

## Recon findings (2026-11-15, read-only)

### Demand

Station-manager bids at empire stations, 7-day picture from
`market_buy_orders` / `market_demand_history`.

| item | bid | notes |
|---|---|---|
| station_reactor_core | 668–678k | persistent; ~214 units, 143M drained in 7d |
| neutronium_ingot | ~100k (crimson) | persistent; ~47M drained |
| lead_shielding | 1.5–1.6k | bursty, filled within hours; inputs ~452 (common lead + iron) |
| plasma_injector | 2.2–2.9k | bursty; inputs ~680 |
| reinforced_frame | 2.1–2.8k | thin margin over ~1,960 inputs |
| control_node | 640–950 | steady; CJ makes these and holds 7,066 |
| silicon_ore (raw) | 360 | steady; sell raw |

"Drained" counts every drop in bid size, so it is a high estimate: it can't tell
fills from expiries.

Ore we already hold (agent storage, 2026-11-15):

| ore | units |
|---|---|
| uranium | 4,598 |
| thorium | 12,042 |
| polonium | 4,456 |
| radium | 4,326 |
| lead | 16,007 |

Rechecked 2026-11-16 00:26Z: thorium 13,476, uranium 4,598, polonium 4,456,
lead 17,442. All of it is in agents' **personal** storage, spread across 3–5
agents. None is in CJ faction storage (older memory notes said "thorium gone";
that is stale). It has to be hauled to station #2.

Uranium caps us at about 47 neutronium, or about 9 reactor cores, before new
supply is needed. Radium feeds none of these chains. marketbot_alhena also holds
37,158 adamantite_ore at voss_redoubt_station.

### Chains and facilities

L1 build costs:

- **lead_shielding:**
  - lead_blast_furnace 91k, lead_rolling_mill 92k, lead_shielding_assembly_line
    110k.
  - Steel plate is already made at CJ.
- **plasma_injector:**
  - precious_metal_refinery 132k, plasma_containment_cell 133k,
    ceramite_sintering_kiln 108k, plasma_injector_assembly 105k.
- **neutronium_ingot, about 2.5M:**
  - Uranium path: leach pad → concentrate → fluorination (UF6) → centrifuge →
    fuel_rod_press.
  - Thorium path: thorium crusher, roaster and rod foundry.
  - Then breeder, polonium_doping_cell, neutronium chamber, and an alloy_forge
    for durasteel.
  - Shortcut while ramping: buy reactor-grade plutonium at ~57.5k, about 71k per
    ingot.
- **station_reactor_core:** reactor_core_production_line, 806k. Each core takes
  5 neutronium, 5 superconductor, 5 durasteel and 2 power cells.

Full trees: scratchpad `recon/chain2.py` output. Rerun before building; the
catalog is dated 2026-11-13.

### Station #2 site shortlist

All lawless, with no station or outpost. The shield is the number of player
stations closer to that stronghold than the site.

| system | jumps from CJ | in-system | rare/radioactive within 1–2 jumps | nearest stronghold | shield |
|---|---|---|---|---|---|
| ironpeak | 2 | quantum, exotic_matter, gold, iron, copper | U, Ra (markeb), deuterium, helium, phase; fluorine, iridium | algol, 12 jumps | 12 |
| beid | 2 | deuterium r30, plasma, xenon, iron | U, Ra, exotic, quantum, phase, helium | algol, 12 | 12 |
| castor | 2 | plasma, ion, manganese, iron | deuterium, exotic, helium, phase, quantum; U, Ra at 2 | algol, 13 | 17 |
| kochab | 4 | quantum, iron, copper | U, Ra, Po (starfall, void_gate), iridium | algol, 11 | 7 |
| driftwood | 1 | phase, helium, iron | deuterium, exotic, fluorine, quantum; U, Ra at 2 | alhena, 12 | 8 |

### Outpost site shortlist

All 8–10 jumps from CJ and exposed: 6–8 jumps from algol, shield 0.

| system | radioactives |
|---|---|
| rukbat | U, Th, tritium within 1 jump; dark_matter in-system |
| ain | thorium in-system (r8, 31/5000 remaining at last read); tritium 1 jump |
| mirfak | uranium in-system (r9) |
| tarazed | tritium in-system; Th at 1 jump, U at 2 |

### Data caveats

- The remaining amounts are days old, and many radioactive belts read near zero
  because they are mined; they regenerate.
- The faction_intel_* tables are empty until craftsman-3 restarts and runs
  `capture_intel`.
- Founding eligibility is inferred from the KB, not yet checked with
  `get_base_cost`.

## Decided (operator)

- Station #2 focuses on mid-tier lines and radioactives. CJ keeps its current
  lines.
- It is a real station built the CJ way: 1M treasury floor, every build approved.
- Starting capital is a 10M donation from the_banker to the station's lead agent.
- Safety is measured by stronghold distance and shield count. All non-empire
  space is the same security 0.
- The station should be "somewhat close" to CJ, with no 30-jump hauls.
- The outpost kit hosts a radioactive mining squad. The squad is existing
  miningbots refitted with rad_harvester I–IV and lead_lined_cargo I/II, or
  newly spawned agents.
  - Miners travel 1–2 hops to belts and flee from hostiles.
  - They are ships, not drones, because there is no station there.

### Operator answers to the builder check (2026-11-15)

1. **Ownership and treasury.**
   - Station #2 is a CRFT station. It shares CJ's faction storage and officer
     ranks, and moves goods with faction_deposit and faction_withdraw.
   - The_banker adds 10M to the **CRFT treasury** before founding. That money is
     earmarked for station #2's setup, and the founding and outpost fees come
     out of it.
   - There is one 1M floor on the CRFT treasury, not one per station.
2. **Budget scope.**
   - The 10M is enough to get production lines running and earning. It is not
     the whole budget.
   - Phase 1: found the station, build the base sequence and defenses, then
     lead_shielding and plasma_injector. Deploy the outpost and start mining.
   - **Stop and review** once revenue is flowing. Climbing the radioactive chain
     (neutronium, reactor cores) is a later decision, made on measured income.
   - Bought reactor-grade plutonium is an acceptable bridge while that decision
     is pending.
3. **Buyers.** Sell to anyone who pays, strongholds included. Our haulers that
   hold the pirate unlock deliver there. Stronghold bids for reactor cores were
   up to 1.24M (11-08). The stronghold routing rule still applies to every other
   agent.
4. **Squad size.** As many miners as the ore supports. The only limit is how long
   it takes to log the fleet back in after a restart.
   - The host-wide login gate allows 60 logins a minute. A full cold start takes
     about 5 minutes.
   - Each extra 60 agents adds about a minute to a full restart.
   - Size the squad from belt yield and haul capacity, and report the
     added restart time.
5. **Station #2 defenses: yes**, built early so the station is never at zero.
   The starting kit is meant to scare off opportunists:
   - 1× shield
   - 1× armor module
   - one level-3 gun, one level-2 gun and one level-1 gun

   It is part of the phase-1 approval list. The CJ defense design
   (2026-11-07-cj-station-defense-design.md) is the reference.

## Left to the builder (and the operator where marked)

- **Final site for station #2 (operator).** Recommendation: ironpeak first,
  beid as backup.
  - Confirm both with `get_base_cost` while in the system.
  - Re-scan their belts and markeb's before choosing.
- **Final outpost site (operator).** Choose after a fresh read of the radioactive
  belts within 2 hops, from faction intel or a scout pass. Favor the hub with the
  most remaining radioactive ore within 1–2 hops.
- **Station #2 lead agent: craftsman-2 (operator, 2026-11-15).** craftsman-2
  founds the station and is the officer in charge of it. It needs ManageBases
  permission and officer rank, so confirm both before founding.
- **Facility build order and per-build approvals.**
  - Start with the CJ base sequence.
  - Then the lead_shielding lines, which use common ore already held and have
    the fastest payback.
  - Then plasma_injector.
  - Then the radioactive chain, one facility at a time, each with its upkeep
    inputs stocked first.
- **Worker support.** A mining mode for the radioactive squad:
  - home is the outpost's faction storage;
  - the target belt is chosen by remaining ore within 1–2 hops;
  - a hostile check on arrival, plus the existing flee gate;
  - unload the whole hold at the outpost (hold-full check first, per the
    mining-loop rule);
  - refuel from the outpost bunker.
- **Hauling.** An outpost → station #2 haul task, and an intermediates exchange
  between station #2 and CJ.
- **Selling.** Route finished goods to empire station-manager bids; reuse the
  existing sell paths where they fit.
- **Quick wins**, no build needed. Operator decision on each:
  - Sell surplus silicon_ore raw.
  - Sell spare control_nodes, after checking they are not reserved for
    facility builds.
