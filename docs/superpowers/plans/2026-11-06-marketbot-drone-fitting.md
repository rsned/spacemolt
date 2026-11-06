# Marketbot Drone Fitting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fit 62 marketbots with one `advanced_drone_bay` and five `mining_drone` each, using only parts we already hold, so their storage fills with raw materials we can pull from.

**Architecture:** A hauler carries the parts on one tour and gifts them into each bot's storage at its station. Each bot then runs the existing `fit_drones` verb from its schedule. New code is limited to a read-only fit check, a multi-module strip, and a tour verb built on `Deliver`.

**Tech Stack:** Go worker verbs (`pkg/worker`), agent `schedule.json` tasks, DroneLang templates in `data/scripts/drones/`.

**Spec:** `docs/superpowers/plans/2026-10-31-drone-offtake-and-marketbot-refit.md` (Phase B, superseded in scope by this plan) and `docs/superpowers/specs/2026-08-22-fleet-drone-refit-design.md` (module and drone facts).

## Global Constraints

- Operator chose the marketbot-only scope on 2026-11-06, not the 150-agent option.
- Each bot gets exactly 1 `advanced_drone_bay` and 5 `mining_drone`. One bay holds 5 drones.
- The bay needs a utility slot, cpu 12 and power 15.
- No energy crystals are spent: the 62 bots need 62 bays, we hold 61, and the last bot waits.
- `craftsman-1` is the operator's own session. Nothing here logs into it; the operator sends its gifts.
- Gifts are irreversible, so every gift comes from a manifest reviewed before the tour starts.
- Only an agent holding the pirate unlock may stop at a stronghold.
- All pauses use Sleep constants from `pkg/game/constants.go`; `pkg/worker` never calls `time.Sleep`.
- Run `go build ./...`, `go test ./...` and `golangci-lint` before each commit.
- Never `git add -A`; `data/*.json` is dirty. Add specific paths.

## Review Focus

- A bot that moved after the manifest was built should stop the tour at that stop with a clear error, not gift parts into the wrong storage. Task 3 pins this.
- A tour restarted mid-way should skip finished stops, because a repeat gift cannot be taken back. Task 3 pins this.
- A hull with a harvester the bot's role needs should refuse the strip and change nothing. Task 2 pins this.
- A bay that installs while the drones fail should resume without withdrawing or stripping again. Task 2 pins this; `Resume` already exists.
- A stronghold stop on a courier without the unlock should fail before any jump. Task 3 pins this.

---

## Scope

Live data from 2026-11-06. Of 79 marketbots, 12 already have drones and 67 do not.

| Group | Bots | Bays | Drones | Note |
|---|---:|---:|---:|---|
| Wave 1, mb-fleet | 49 | 49 | 245 | 8 are in strongholds |
| Wave 2, `marketbot_021`–`033` | 13 | 13 | 65 | Still in the unlock fleet, not parked at stations |
| Excluded | 5 | 0 | 0 | See below |

Excluded bots:
- `marketbot_deep_range` (`huffnpuff`, cpu 9) and `marketbot_last_light` (`prayer`, cpu 4) cannot take a bay.
- `marketbot_synchrony`, `marketbot_the_rampart` and `marketbot_traders_rest` have no belt, ice field or gas cloud in their systems.

**Parts held:**

| Part | Where | Holder | Units |
|---|---|---|---:|
| Bays | Grand Exchange (Haven) | CRFT faction storage | 25 |
| Bays | Frontier | `craftsman-1` | 20 |
| Bays | Central Nexus | `craftsman-1` | 9 |
| Bays | Grand Exchange | `craftsman-1` 1, `marketbot_014` 1 | 2 |
| Bays | Sirius, Sol | `marketbot_sirius` 2, `marketbot_sol` 1, `fighter-4` 2 | 5 |
| Drones | Sol | `craftsman-1` | 500 |
| Drones | Frontier | `craftsman-1` | 90 |

**Tour:** Wave 1 fits one Congregation trip: 49 × 10 + 245 × 5 = 1,715 of 1,900 cargo. The greedy route below is 207 jumps from Haven across 46 systems, not counting the pickup trip to Sol, Frontier and Central Nexus. The fleet's measured pace (about 330 jumps a day) makes that about 15 hours plus stops; Task 5's pilot replaces this estimate.

**Yield:** A new bot starts near 15,200 units a day and climbs toward 30,400 as drone control rises. Wave 1 adds about 745,000 units a day at the start. The bots fill their own storage; hauling that output is out of scope.

---

### Task 1: Read-only fit check

Module data is not captured anywhere: `ship_modules` is empty and `agent_hulls.modules` is only a count. Thirteen wave 1 hulls have 2 or 3 modules, and those need real names before anything is stripped.

**Files:**
- Modify: `pkg/worker/fit_drones.go`, `pkg/worker/dispatch.go`
- Test: `pkg/worker/fit_drones_test.go`

**Interfaces:**
- Produces: `func (d *WorkerDispatch) FitCheck(ctx context.Context, bayItem string) (FitReport, error)` and the `fit_check` verb.
- Produces: `type FitReport struct { Modules []installedModule; CPUFree, PowerFree, BaysFitted int; MustStrip []installedModule; Fits bool }`

- [ ] **Step 1: Write failing tests**

`TestFitCheckReportsModulesWithoutMutating` asserts a `prospect` reply with one laser gives `MustStrip` of length 1, `Fits` true, and no mutating call in `f.calls`. `TestFitCheckNoUtilitySlot` asserts `Fits` false with a zero-slot hull. `TestFitCheckAlreadyFitted` asserts `BaysFitted` 1 and an empty `MustStrip`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/worker/ -run TestFitCheck -v`
Expected: FAIL, `FitCheck` undefined.

- [ ] **Step 3: Implement `FitCheck`**

Read `get_ship` only. `MustStrip` is the fewest non-bay modules whose removal frees cpu 12 and power 15, largest cpu first. Log one line per module. Register `fit_check` in `dispatch.go` beside `fit_drones`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestFitCheck -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/fit_drones.go pkg/worker/dispatch.go pkg/worker/fit_drones_test.go
git commit -m "worker: fit_check reports what a bay install would need"
```

- [ ] **Step 6: Survey the fleet**

Roll mb-fleet onto the new binary, which needs operator approval. Add one `fit_check advanced_drone_bay` task to each of the 49 wave 1 schedules, then read `task-outcomes.json` and the worker logs. Expected: a list of bots with `Fits` false or a `MustStrip` over 1. Move any `Fits` false bot to Excluded in this plan.

### Task 2: Strip more than one module

`Strip` removes one module today. `freeModuleSlot` refuses when two or more are fitted, because a wrong strip is silent and costly. Hulls like `iron_reach` (maul, 3 modules) need more than one out, and only with an explicit list.

**Files:**
- Modify: `pkg/worker/fit_drones.go:178-190` (`FitDronesOpts`), `pkg/worker/fit_drones.go` (`freeModuleSlot`), `pkg/worker/dispatch.go:516-570`
- Test: `pkg/worker/fit_drones_test.go`

**Interfaces:**
- Consumes: `FitCheck` from Task 1.
- Produces: `FitDronesOpts.StripTypes []string`, and a ninth `fit_drones` argument `STRIP_TYPES`, comma separated, `-` for none.

- [ ] **Step 1: Write failing tests**

`TestFitDronesStripsNamedTypes` asserts `mining_laser_i,pulse_laser_i` uninstalls both and installs the bay. `TestFitDronesRefusesHarvesterOnGasRole` asserts `gas_harvester_i` listed on a `resident_gas` bot errors with no uninstall, and the same for `ice_harvester_i` on `resident_ice`. `TestFitDronesResumeDoesNotStripAgain` asserts a re-run with one bay already fitted makes no uninstall call.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/worker/ -run 'TestFitDrones(Strips|Refuses|Resume)' -v`
Expected: FAIL

- [ ] **Step 3: Implement the named strip**

Only modules whose `TypeID` is in `StripTypes` may be removed, and the existing one-module `Strip` path stays as it is. Check the role guard before the first uninstall so a refused fit changes nothing.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestFitDrones' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/fit_drones.go pkg/worker/dispatch.go pkg/worker/fit_drones_test.go
git commit -m "worker: fit_drones strips only the module types it is given"
```

### Task 3: Tour verb

`deliver` handles one item to one recipient per task, and schedule order is not guaranteed. The courier needs one ordered, resumable run.

**Files:**
- Create: `pkg/worker/deliver_tour.go`, `pkg/worker/deliver_tour_test.go`
- Modify: `pkg/worker/dispatch.go`

**Interfaces:**
- Consumes: the existing `Deliver` with `from == ""`, which gifts from cargo at the destination.
- Produces: `func (d *WorkerDispatch) DeliverTour(ctx context.Context, manifestPath string) error` and the `deliver_tour` verb.
- Produces: `type TourStop struct { StationID, System, Recipient string; Items map[string]int; Stronghold bool }`
- Progress file: `<manifest>.progress`, one finished `Recipient` per line, appended after that stop's last gift.

- [ ] **Step 1: Write failing tests**

- `TestDeliverTourGiftsInOrder`: two stops give gifts in manifest order.
- `TestDeliverTourResumeSkipsFinishedStops`: a progress line for stop 1 means no gift to that recipient.
- `TestDeliverTourStopsWhenRecipientMoved`: when the fresh station lookup for the recipient differs from `StationID`, it errors naming the bot, with no gift at that stop.
- `TestDeliverTourStrongholdNeedsUnlock`: a locked courier with any `Stronghold` stop errors before the first jump.
- `TestDeliverTourShortCargoStops`: fewer units aboard than the stop needs errors with no partial gift.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/worker/ -run TestDeliverTour -v`
Expected: FAIL, `DeliverTour` undefined.

- [ ] **Step 3: Implement `DeliverTour`**

Validate the whole manifest first, then walk the stops. Read the recipient's station from the newest `data/overmind/mb-status.json` row just before each stop, and refuel at each dock through the existing autopilot path.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestDeliverTour -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/deliver_tour.go pkg/worker/deliver_tour_test.go pkg/worker/dispatch.go
git commit -m "worker: deliver_tour carries a reviewed manifest stop by stop"
```

### Task 4: Manifest and pickup

**Files:**
- Create: `data/stations/refit/wave1.json`

- [ ] **Step 1: Build `wave1.json` from the table in Task 5**

One `TourStop` per bot, each with `{"advanced_drone_bay": 1, "mining_drone": 5}`. Resolve `StationID` from each bot's live base, never from the POI id; the two are different names. Expected: 49 stops, 49 bays, 245 drones.

- [ ] **Step 2: Choose the courier**

Use one Congregation holding the unlock, running a worker binary built after Task 3. Second it out of the haul fleet in both yamls; fleet overrides are removed-sets. Expected: it appears as `removed` in `haul-status.json`.

- [ ] **Step 3: Operator gifts from `craftsman-1`**

Operator action, all to the courier: 245 `mining_drone` at Sol, 20 `advanced_drone_bay` at Frontier, 3 at Central Nexus and 1 at Grand Exchange. Gifts land in the courier's storage at that station.

- [ ] **Step 4: Faction withdraw at Grand Exchange**

The courier withdraws the 25 bays from CRFT storage at Grand Exchange. This needs Officer rank; check with a 1-unit withdraw first. Expected: 49 bays and 245 drones in cargo, verified with `get_cargo`.

- [ ] **Step 5: Commit the manifest**

```bash
git add data/stations/refit/wave1.json
git commit -m "refit: wave 1 manifest for 49 marketbots"
```

### Task 5: Pilot, tour and fit

Scripts use the generic templates. `mine_asteroid`, `mine_ice` and `mine_gas` resolve their POIs at fit time, so no per-site file is needed. Pointing a bot at a specific ore is a later retarget.

- [ ] **Step 1: Pilot on stops 1–3**

Cut a copy of the manifest to the first three stops and run `deliver_tour`. Then add to each of the three bots: `fit_drones <script> 1 5 advanced_drone_bay mining_drone true <strip> false <types>`, using Task 1's survey for the strip fields. Expected: `fit_drones: 1 bay(s), 5 drone(s) deployed` in each log, and ore in storage within 2 hours.

- [ ] **Step 2: Check before the full tour**

Record jumps per hour and fuel per jump against the estimate above. Stop and report if any bot's ore has not appeared, or if a resident stayed undocked after `launch_drones`.

- [ ] **Step 3: Run the full tour**

Add `deliver_tour data/stations/refit/wave1.json` to the courier. The progress file lets it skip the pilot's stops if those lines are carried over. Expected: 49 lines in the progress file.

- [ ] **Step 4: Fit each bot as its stop completes**

Add the `fit_drones` task from Step 1 to each schedule. Expected: 49 `deployed` lines, and `drone_control` above 0 for all 49 in `agent_skills` within 24 hours.

- [ ] **Step 5: Wave 2**

After `marketbot_021`–`033` leave the unlock fleet and park, repeat Tasks 4 and 5 with 12 bots. The 13th waits for one more bay.

- [ ] **Step 6: Record the result**

Update `project_fleet_drone_refit.md` with final counts and pace, then run `make memory-sync` and commit `docs/memory`.

## Wave 1 stops

| Stop | Bot | System | Hull | Fitted now | Script |
|---|---|---|---|---:|---|
| 1 | market_prime | market_prime | prospect | 1 | mine_gas |
| 2 | cargo_lanes | cargo_lanes | appraisal | 2 | mine_gas |
| 3 | gold_run | gold_run | prospect | 1 | mine_asteroid |
| 4 | the_levy | the_levy | prospect | 1 | mine_ice |
| 5 | factory_belt | factory_belt | prospect | 2 | mine_gas |
| 6 | treasure_cache | treasure_cache | prospect | 1 | mine_asteroid |
| 7 | home | cloverfield | cobble | 2 | mine_asteroid |
| 8 | sulafat | sulafat | cobble | 2 | mine_asteroid |
| 9 | frontier | starfall | cobble | 1 | mine_asteroid |
| 10 | starfall | starfall | cobble | 1 | mine_asteroid |
| 11 | void_gate | void_gate | cobble | 1 | mine_asteroid |
| 12 | first_step | first_step | cobble | 1 | mine_asteroid |
| 13 | the_telescope | unknown_edge | cobble | 1 | mine_asteroid |
| 14 | unknown_edge | unknown_edge | cobble | 1 | mine_asteroid |
| 15 | algol | algol (stronghold) | cobble | 2 | mine_gas |
| 16 | nova_terra | nova_terra | theoria | 1 | mine_asteroid |
| 17 | procyon | procyon | theoria | 1 | mine_gas |
| 18 | 001 | dheneb | theoria | 2 | mine_asteroid |
| 19 | sirius | sirius | theoria | 2 | mine_gas |
| 20 | sol | sol | theoria | 1 | mine_asteroid |
| 21 | alpha_centauri | alpha_centauri | archimedes | 3 | mine_ice |
| 22 | the_experiment | the_experiment | threshold | 1 | mine_asteroid |
| 23 | node_gamma | node_gamma | threshold | 1 | mine_asteroid |
| 24 | node_alpha | node_alpha | threshold | 1 | mine_asteroid |
| 25 | nexus_prime | nexus_prime | threshold | 1 | mine_asteroid |
| 26 | node_beta | node_beta | threshold | 1 | mine_asteroid |
| 27 | gliese_581 | gliese_581 (stronghold) | threshold | 2 | mine_gas |
| 28 | 007 | bd20_2457 | threshold | 1 | mine_asteroid |
| 29 | 000 | gsc_0019 | prospect | 1 | mine_asteroid |
| 30 | 002 | arneb | theoria | 1 | mine_asteroid |
| 31 | 008 | kornephoros | shard | 1 | mine_asteroid |
| 32 | ramens_rest | kornephoros | cobble | 1 | mine_asteroid |
| 33 | 009 | thuban | shard | 1 | mine_asteroid |
| 34 | 003 | crosshaven | prospect | 1 | mine_asteroid |
| 35 | 004 | alpheratz | cobble | 1 | mine_gas |
| 36 | 005 | saiph | cobble | 1 | mine_ice |
| 37 | 006 | 40_eridani | threshold | 1 | mine_asteroid |
| 38 | the_crucible | the_crucible | shard | 1 | mine_gas |
| 39 | iron_reach | iron_reach | maul | 3 | mine_asteroid |
| 40 | the_anvil | the_anvil | shard | 1 | mine_asteroid |
| 41 | krynn | krynn | shard | 1 | mine_asteroid |
| 42 | blood_forge | blood_forge | shard | 1 | mine_asteroid |
| 43 | 020 | ironhearth | prospect | 1 | mine_asteroid |
| 44 | gsc_0008 | gsc_0008 (stronghold) | maul | 1 | mine_ice |
| 45 | zaniah | zaniah (stronghold) | theoria | 1 | mine_asteroid |
| 46 | barnard_44 | barnard_44 (stronghold) | archimedes | 3 | mine_asteroid |
| 47 | bellatrix | bellatrix (stronghold) | theoria | 3 | mine_ice |
| 48 | sheratan | sheratan (stronghold) | prospect | 2 | mine_ice |
| 49 | xamidimura | xamidimura (stronghold) | cobble | 1 | mine_asteroid |
