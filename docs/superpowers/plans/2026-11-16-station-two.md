# Station #2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Found CRFT's second station, led by craftsman-2, in the markeb-adjacent
pocket, and get lead_shielding and plasma_injector lines earning from empire and
stronghold bids.

**Architecture:** This plan needs **no new code**. Everything it needs already
exists:
- **Recording:** play_as `--record-dir`, `station_snapshot`, `ledger`. These are
  the CJ record-set tools from plan 2026-10-25.
- **Moving stock:** the worker verbs `deliver`, `faction_deposit_all` and
  `faction_restock`.
- **Selling:** the fleet-yaml fields `faction_pull` and `consign_offer`. A
  resident pulls goods out of faction storage and offers them on consignment.
  The haul fleet claims each consignment and sells it at the best bid; haulers
  with the pirate unlock also sell at strongholds.

The tasks are ops, run by hand with the operator. Each one ends at a gate, and
the next starts only after the operator's go-ahead. Any task that turns up a
missing capability stops and gets its own small plan.

**Tech Stack:** play_as (`go run ./cmd/tools/play_as`), worker and overmind
fleet yaml, the assets.db and knowledge-DB ledgers.

**Spec:** `docs/superpowers/specs/2026-11-15-station-two-and-radioactive-outpost-design.md`.
The outpost sections are deferred.

## Global Constraints

- The CRFT treasury never goes below **1M**. There is one floor for the whole
  faction.
- The_banker adds **10M** to the CRFT treasury, earmarked for station #2's setup
  and its founding fee.
- **Every facility build** is planned (credits plus materials) and approved by
  the operator before it is started.
- **Stock every upkeep input before a build finishes.** CJ's Locker Bay and
  recycler both starved.
- **Lead agent: craftsman-2.** It founds the station and is its officer in
  charge. It needs ManageBases permission and Officer rank.
- **Site:** lawless, with no station already there. Recommended: **ironpeak**,
  with **beid** as backup. Eligibility is confirmed in game with `get_base_cost`.
- **Never route through strongholds** without the pirate unlock. Stronghold
  sales go only through unlocked haulers.
- **Live fleet actions need the operator's yes.** This covers restarts, yaml
  changes, gifts and builds.
- **Fleet yaml rules:**
  - Removing an agent from a fleet works by override (removed-SETS). List an
    agent that rotates between fleets in **both** yamls.
  - A **new** yaml field needs the overmind restarted, not SIGHUPed.
  - Never restart a disconnected worker.
- **Commits:** never `git add -A` while `data/*.json` is dirty; commit
  `data/stations/<system>/` explicitly.
- **Sleeps:** use the Sleep constants or `settle()`; never `time.Sleep` in
  pkg/worker.

## Review Focus

Failure modes the spec implies that no step can unit-test. Each one is a check
inside the task that owns it.

1. **Faction storage is per station.**
   - If CJ's faction storage cannot be reached from station #2, inputs held at
     CJ need hauling.
   - Task 1 measures this. Task 5 hauls if needed.
2. **The founding fee drains the treasury below the floor.**
   - Check the treasury the moment before `build_base`. The fee comes out of
     the treasury first.
   - Covered in Task 3 (top-up) and Task 4 (re-check right before founding).
3. **craftsman-2 gets relaunched or displaced while play_as holds it.**
   - Both the craft fleet and the unlock fleet list craftsman-2.
   - Task 2 removes it from both before any play_as session.
4. **A consigned item that is also upkeep gets sold.**
   - The 11-12 trade_authenticator mistake.
   - Task 8 offers only finished goods that no facility at either station
     consumes.
5. **Stronghold sale routes.**
   - Only haulers that hold the pirate unlock may claim a stronghold sale.
   - Task 8 verifies the first stronghold sale was flown by an unlocked
     hauler.

---

### Task 1: Pre-flight (read-only)

**Files:**
- Create: `data/stations/station2-preflight.md` (scratch notes, committed).
  Task 3 moves it into the record set.

**Read-only checks, in order:**

- [ ] **Step 1: craftsman-2's standing.**
  - Run play_as for craftsman-2 (not while its worker runs; see Task 2).
  - `faction_info`: is it CRFT, and does its role carry `manage_bases`?
  - Officer rank: the role list on `faction_info`.
  - Record the answers in the notes.
  - Read-only alternative: `data/assets.db` faction_members plus CJ's
    findings.md rank notes.
- [ ] **Step 2: where the cores are.**
  - `data/assets.db agent_storage_items` (read 2026-11-16) shows craftsman-1
    holding one `station_core` at CJ (base `c976fa7d…`) and one at
    `voss_redoubt_station`.
  - Plan for the operator to gift the CJ core to craftsman-2 at CJ, by hand
    from craftsman-1, the operator's manual account.
- [ ] **Step 3: fresh belt reads.**
  - Use faction intel (`faction_intel_resources`, once craftsman-3 has run
    `capture_intel`) or a scout pass with `get_poi`.
  - Read ironpeak, beid, castor and markeb.
  - Record richness and remaining for each.
- [ ] **Step 4: is faction storage per station?**
  - Check CJ findings.md for an answer.
  - If none is recorded, ask the operator to confirm from the game docs.
  - The result decides whether Task 5 hauls inputs from CJ.
- [ ] **Step 5: treasury now.**
  - Read the CRFT treasury from the `faction_info` treasury field.
  - The founding fee is not known until Task 4's `get_base_cost`. CJ's was 5M.
- [ ] **Step 6: catalog refresh.**
  - Rerun the recon chain script
    (`/tmp/.../scratchpad/recon/chain2.py`, or re-derive from
    `data/game-api/latest/catalog_*.json`).
  - Record the current L1 costs and build materials for:
    - lead_blast_furnace, lead_rolling_mill, lead_shielding_assembly_line;
    - precious_metal_refinery, plasma_containment_cell,
      ceramite_sintering_kiln, plasma_injector_assembly;
    - the base sequence: locker bay, faction workshop, atmospheric recycler,
      fuel production;
    - the defense kit: shield, armor module, one level-3 gun, one level-2 gun
      and one level-1 gun.
- [ ] **Step 7: Commit.**
  `git add data/stations/station2-preflight.md && git commit -m "station2: pre-flight notes"`
- [ ] **Gate:** present the site choice, the founding route and the budget
  table (fee + base sequence + defenses + both lines, against 10M minus the
  floor) to the operator. Stop if anything is ineligible or unknown.

### Task 2: Free craftsman-2 from its fleets

**Files:**
- Modify: `data/overmind/craft-fleet.yaml`: craftsman-2's line becomes a comment
  with the date and reason, "2026-11-16 station #2 lead".
- Modify: `data/overmind/unlock-fleet.yaml`: the same for its line there.

- [ ] **Step 1:** Check that craftsman-2 is not mid-task:
  - its heartbeat in `craft-overmind.log`;
  - no claimed handoffs in `data/overmind/handoff-queue.json`.
- [ ] **Step 2:** Comment out both lines, then SIGHUP both overminds. Run
  `pgrep -af '[b]in/overmind.*craft'`, then `kill -HUP <pid>`, and the same for
  unlock.
- [ ] **Step 3:** Verify nothing will relaunch it:
  - each log shows `membership: "craftsman-2" removed`;
  - `pgrep -af '[b]in/worker --agent craftsman-2 '` prints nothing.
- [ ] **Step 4: Commit** only the two yaml files.
- [ ] **Gate:** operator yes before the fleet yaml change.

### Task 3: Money and record set

**Files:**
- Create: `data/stations/<system>/README.md`. Copy
  `data/stations/cloverfield/README.md`, changing the system, the agent
  (craftsman-2) and the record dir.
- Create: `data/stations/<system>/findings.md`. Use CJ's headings plus a
  `Production lines` heading, and seed it from Task 1's notes. Move
  `station2-preflight.md` into it.

- [ ] **Step 1:** The operator has the_banker donate 10M to the CRFT treasury.
  Re-read the treasury and record it with
  `ledger funding credits +10000000` under `--record-dir data/stations/<system>`.
- [ ] **Step 2:** Run `git check-ignore data/stations/<system>/findings.md`; it
  must print nothing.
- [ ] **Step 3: Commit**
  `git add data/stations/<system> && git commit -m "docs(station): station #2 record set"`

### Task 4: Convoy, found, inventory

All play_as work uses
`go run ./cmd/tools/play_as --record-dir data/stations/<system> craftsman-2`.

- [ ] **Step 1:** The operator gifts the station_core from craftsman-1 to
  craftsman-2 at CJ. craftsman-2 withdraws it to cargo.
- [ ] **Step 2: shopping list.** Draft it from Task 1's catalog:
  - upkeep inputs for the base sequence and the defense kit, covering 3+ days
    at CJ's measured rates;
  - fuel.

  Give quantities and costs. Spending must keep the treasury at or above the
  floor. **Operator approves.** Buy, and record each purchase with
  `ledger purchase`.
- [ ] **Step 3:** Fly to the site, avoiding strongholds. Stay undocked at a
  planet POI. Run `get_base_cost`, and record the fee, the requirements and
  the eligibility flag.
- [ ] **Step 4:** Re-read the treasury. If treasury minus the fee is under 1M,
  **stop** and ask for a top-up.
- [ ] **Step 5:** Run `build_base --name "<operator's name>"`. Record the
  founding with `ledger founding credits -<fee>` and
  `ledger founding station_core -1`. Note the **base id and the POI id**;
  they differ.
- [ ] **Step 6:** Dock, take a `station_snapshot`, and deposit everything into
  faction storage (each deposit gets `ledger haul-in`). Record in findings what
  came with the station.
- [ ] **Step 7: Commit** `data/stations/<system>/`.
- [ ] **Gate:** operator review of the founding results.

### Task 5: Defenses, then the base sequence

- [ ] **Step 1: defense kit.**
  - The kit: 1× shield, 1× armor module, one level-3 gun, one level-2 gun and
    one level-1 gun.
  - Build plan: facility ids from the Task 1 catalog, cost, materials, upkeep
    and ammo.
  - **Operator approves.** Stock the upkeep and ammo **first**, then build one
    piece at a time, recording each in findings.
- [ ] **Step 2: base sequence.**
  - Order: locker bay → faction workshop → atmospheric recycler → fuel
    production. Fuel production needs tank capacity first.
  - Each build: plan, operator approval, upkeep stocked, build,
    `station_snapshot`, then a findings entry (cost, build time, change in the
    maintenance bill).
- [ ] **Step 3:** If Task 1 found faction storage is per station, haul the
  inputs CJ holds (steel plate, etc.):
  - a resident at CJ runs `faction_pull` to move them into its personal storage;
  - `deliver ITEM QTY <CJ base> <station2 base> craftsman-2` carries them over;
  - craftsman-2 runs `faction_deposit_all`.

  Each move gets operator approval.
- [ ] **Step 4: Commit** `data/stations/<system>/` after each build.

### Task 6: Lead shielding line

- [ ] **Step 1: lead ore to the station.**
  - Haul lead_ore from its holders to station #2: 17,442 held across 5 agents
    (assets.db, 2026-11-16).
  - Use one `deliver` task per holder:
    `deliver lead_ore <qty> <holder base> <station2 base> craftsman-2`,
    scheduled once on a hauler. Remember schedule due batches can't be
    cancelled.
  - craftsman-2 then runs `faction_deposit_all`.
- [ ] **Step 2: build the line.** Plan, approve, stock, build, one at a time:
  lead_blast_furnace, lead_rolling_mill, lead_shielding_assembly_line.
- [ ] **Step 3: first craft.**
  - Run a lead_shielding craft by hand at the station. Faction-storage crafting
    needs Officer rank.
  - Record the output and time in findings.
- [ ] **Step 4: Commit** `data/stations/<system>/`.

### Task 7: Plasma injector line

- [ ] **Step 1: build the line.** Plan, approve, stock, build, one at a time:
  precious_metal_refinery, plasma_containment_cell, ceramite_sintering_kiln,
  plasma_injector_assembly.
- [ ] **Step 2: inputs.**
  - CJ holds about 67k plasma_gas, 5.4k palladium and 7.7k titanium; titanium
    alloy, steel and copper wire are made at CJ.
  - Move what the line needs to station #2, as in Task 5 Step 3.
- [ ] **Step 3: first craft.** Make a plasma_injector by hand and record it in
  findings.
- [ ] **Step 4: Commit** `data/stations/<system>/`.

### Task 8: Selling through consignment

**Files:**
- Modify: the fleet yaml that will run craftsman-2 as a resident at station #2.
  Recommended: `data/overmind/craft-fleet.yaml`, with its line re-enabled.
  - `role: resident`
  - `station: <station2 POI id>`
  - `faction_pull: [lead_shielding, plasma_injector]`
  - `consign_offer: ["lead_shielding:0", "plasma_injector:0"]`

- [ ] **Step 1: check nothing burns these goods.**
  - Confirm that no facility at CJ or station #2 lists lead_shielding or
    plasma_injector as upkeep, using `facility faction_list` maintenance
    inputs. If one does, set a keep amount (`item:keep`) that covers 3 days of
    its upkeep.
- [ ] **Step 2: check the overmind supports the fields.**
  - Confirm the craft overmind binary supports `faction_pull` and
    `consign_offer` (built after 2026-10-24).
  - If not, restart it with its exact `ps` args. A SIGHUP drops new fields
    silently.
- [ ] **Step 3:** Re-enable craftsman-2's line with these fields. **Operator
  yes**, then SIGHUP (or restart).
- [ ] **Step 4: verify the first sale.**
  - `consign_stock` in `data/market.db` shows craftsman-2's offer.
  - A haul worker logs a claim on it.
  - The gift goes through the handoff queue.
  - The sale is logged with its price. If the buyer was a stronghold, check the
    hauler that flew it holds the pirate unlock.
  - Record it in findings.
- [ ] **Step 5: Commit** the yaml and `data/stations/<system>/`.

### Task 9: Quick wins (operator decisions)

- [ ] **silicon_ore:** sell surplus silicon raw at about 360. Offer it on
  consignment from its holders. List the holders and quantities and get
  operator approval first.
- [ ] **control_node:** check whether CJ's 7,066 are set aside for facility
  builds. Offer any surplus only on operator approval.
- [ ] **station_reactor_core:** craftsman-1 holds 2 at
  `cargo_lanes_freight_depot`. Stronghold bids were up to 1.24M on 2026-11-08,
  empire bids about 670k. Ask the operator whether to sell them.

### Task 10: Revenue review gate

- [ ] **Step 1:** After 7 days of both lines running, total these from the
  ledger, findings and haul results:
  - revenue per line;
  - upkeep cost;
  - margin.
- [ ] **Step 2:** Report to the operator with a recommendation on the next
  step up the radioactive chain:
  - the neutronium facilities (~2.5M);
  - or bought reactor-grade plutonium as a bridge, about 71k per ingot.

  That decision gets its own spec and plan, together with the deferred outpost
  and mining squad.
- [ ] **Step 3:** Update the memory resume point and commit `docs/memory`.
