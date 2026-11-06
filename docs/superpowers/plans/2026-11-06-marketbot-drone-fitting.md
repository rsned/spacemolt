# Marketbot Drone Fitting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fit 62 marketbots with one `advanced_drone_bay` and five `mining_drone` each, using only parts we already hold, so their storage fills with raw materials we can pull from.

**Architecture:** Parts sit at the five nearest capitals. Each bot collects its own, flies home and runs `fit_drones` there; a courier serves the 8 strongholds. New code is a read-only fit check, a named strip, `deliver_tour` and `refit_trip`.

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
- A tour restarted mid-way should skip finished gifts, because a repeat gift cannot be taken back. Task 3 pins this.
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

**Delivery:** Operator chose hub-and-spoke on 2026-11-06. Each bot flies to its nearest capital, collects its parts, flies home and fits there, because drone scripts bind to the system they are fitted in. The 8 stronghold bots stay put and a courier delivers to them.

| Route | Wave 1 bots | Bays | Drones | Longest trip, one way | Hub bot |
|---|---:|---:|---:|---:|---|
| Krynn (`crimson_war_citadel`) | 16 | 16 | 80 | 9 jumps | `marketbot_krynn` |
| Starfall (`frontier_station`) | 8 | 8 | 40 | 4 jumps | `marketbot_frontier` |
| Haven (`grand_exchange_station`) | 6 | 6 | 30 | 3 jumps | `marketbot_haven` |
| Sol (`confederacy_central_command`) | 6 | 6 | 30 | 4 jumps | `marketbot_sol` |
| Nexus Prime (`central_nexus`) | 5 | 5 | 25 | 3 jumps | `marketbot_nexus_prime` |
| Stronghold courier | 8 | 8 | 40 | 131 jumps in all from Sol | Unlocked Congregation |

Altais has no station in the knowledge base, so Starfall is the Outerrim hub. `craftsman-1` already holds 20 bays and 90 drones at `frontier_station`.

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

### Task 4: Hub delivery

Built: `refit_trip HUB HOME SCRIPT [STRIP_TYPES]` and `TourStop.FromStation`. Manifests and assignments are in `data/stations/refit/`: `hub-*.json`, `strongholds.json` and `assignments.json`.

- [ ] **Step 1: Stock each hub**

Operator action: `craftsman-1` gifts each hub's parts to its hub bot at the hub station. A gift lands in the sender's station, so the parts must be carried there first. Krynn has no bays today; 2 are for sale at `war_citadel` at 9,240.

- [ ] **Step 2: Hand parts to visitors**

Add `deliver_tour data/stations/refit/hub-<hub>.json` to the hub bot. Expected: every recipient has 1 bay and 5 drones in its storage at the hub, and the progress file has a `done` line per item.

- [ ] **Step 3: Pilot on Nexus Prime**

Add `refit_trip central_nexus <home> <script> <strip>` to the 5 Nexus Prime bots from `assignments.json`, one at a time, with strip types from the Task 1 survey. Expected: `fit_drones: 1 bay(s), 5 drone(s) deployed`, the bot docked at home, and ore in storage within 2 hours. Stop and report otherwise.

- [ ] **Step 4: Remaining hubs**

Repeat one bot at a time per hub, with hubs running side by side. Expected: 41 fitted bots.

### Task 5: Stronghold delivery and wave 2

- [ ] **Step 1: Choose the courier**

One unlocked Congregation, seconded out of the haul fleet in both yamls; fleet overrides are removed-sets.

- [ ] **Step 2: Run `deliver_tour data/stations/refit/strongholds.json`**

Load 8 bays and 40 drones first. Expected: 8 stops done, about 131 jumps.

- [ ] **Step 3: Fit the 8 bots**

Add `fit_drones <script> 1 5 advanced_drone_bay mining_drone true false true <types>` to each. Expected: 8 `deployed` lines.

- [ ] **Step 4: Wave 2**

After `marketbot_021`–`033` leave the unlock fleet and park, assign each to its nearest hub and repeat Task 4. One bot waits for a bay; the cheapest for sale is 6,935 at Nova Terra.

- [ ] **Step 5: Record the result**

Update `project_fleet_drone_refit.md` with counts and pace, then run `make memory-sync` and commit `docs/memory`.

## Follow-up: second bay on `eigenstate` hulls

Researched 2026-11-06. Nothing bought. Decide after Task 5 shows real yields.

`eigenstate` is the only T1 hull that holds 2 bays at engineering 0 (cpu 24, power 36, 3 utility slots). Both default modules must come off. A third bay fits at engineering 34.

- **License:** `commission_quote eigenstate` at Conjunction Junction replied that it is Voidborn-exclusive and needs `buy_ship_license ship_class=eigenstate`. The T1 price is 1,000,000 from the treasury, taken from the 2026-11-02 tier list; the game gave no price for this hull.
- **Build cost:** about 10,100 a hull: 8,282 materials at today's cheapest asks, about 830 royalty at 10%, and 1,000 labor. Labor and royalty are read across from the Congregation quote until a real quote exists.
- **Buy cost:** 40,236 and 40,536 from Station Managers at Node Gamma and Central Nexus, one hull each.
- **Break-even:** about 33 hulls. At 62 hulls the license saves about 870,000.
- **Parts:** after both waves no bays are left and about 301 drones remain, so 62 hulls need 62 more bays and 9 more drones. Crafting bays spends reserved energy crystals and needs operator approval.
- **Delivery:** hulls appear at Conjunction Junction, where the garage holds 20. Each bot must fly there for `switch_ship`, and many are 25 or more jumps away. Strip and refit run again on the new hull.
