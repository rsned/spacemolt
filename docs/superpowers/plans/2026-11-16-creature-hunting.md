# Creature Hunting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Gauntlet hunters take the hunt chain, then Station Need creature-part deliveries, fight
only creatures their hull can handle, record everything they meet, and ship surplus parts to CJ.

**Architecture:** Extend the existing hunt role (`pkg/worker/hunt*.go`). New, focused files hold
each piece: target scan, hull eligibility, Station Need parsing/admission, the delivery job, the
trend break-off, dry-board roaming, surplus banking, and a hauler-side gift collector. The Hunt
pass keeps its shape: dock → board → job → ground → fights → complete.

**Tech Stack:** Go 1.24+, SQLite knowledge base (`pkg/knowledge`), worker dispatch (`pkg/worker`).

**Spec:** `docs/superpowers/specs/2026-11-16-creature-hunting-design.md`

## Global Constraints

- Go 1.24+: range over ints, `b.Loop()` in benchmarks. golangci-lint must stay at 0 issues.
- Never `time.Sleep` in pkg/worker; use `deps.sleep` / `settle()` and Sleep constants in
  `pkg/game/constants.go` (ask the operator if none fits).
- `go build ./...` and `go test ./...` pass before every commit; commit messages end with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never `git add -A`.
- Check real API field names (`server_docs/openapi.json`) before coding against them.
- Gauntlets never engage predators: quarry roles stay `grazer` / `scavenger`
  (`huntQuarryRoles`); unknown role = refuse.
- Creature-fight break-off: when the hunter's hull is being worn down and the target's is not,
  retreat at **50%** own hull; hard floor `huntDefaultFleeHull` = **0.35** stays.
- Bounty / other combat missions keep today's behaviour exactly.
- Station Need difficulty cap = **2**; bounty cap stays `huntDefaultMaxDifficulty` = 1.
- Dry boards: go home if CJ is within **12** jumps, else move to another empire capital.
- Hunting grounds: empire-policed systems only; existing stronghold / Lawless / hazard gates apply.
- Home = Conjunction Junction, POI `862e07fa76571d0a76cab3cbaf4046e7`, system `cloverfield`.
- Surplus hand-off is `send_gift` (item gifts need a dock; they land in the recipient's storage
  at that station).

## Review Focus

1. A Station Need whose "Dropped by:" names a predator (e.g. a leviathan) — must be refused, not hunted.
2. A Station Need whose description has no "Dropped by:" line and no KB drop record — refuse, don't guess.
3. Hold fills with carcass loot before the delivery quantity is reached — mission part must win the space.
4. Hunter flying a non-combat or T1 hull (pirate-7/-8) — must keep today's bounty-only behaviour.
5. The delivery's target base differs from the station the mission was accepted at — deliver to `target_base_id`.

---

### Task 1: Targeted creature scan

**Files:**
- Modify: `pkg/game/client.go` (beside `Scan`), `pkg/game/interface.go`, `pkg/game/mcp_game_client_commands.go`, every `GameClient` fake (find with `grep -rln 'Scan(ctx context.Context) error'`)
- Create: `pkg/worker/hunt_scan.go`, `pkg/worker/hunt_scan_test.go`

**Interfaces:**
- Produces: `func (c *Client) ScanTarget(ctx context.Context, targetID string) error` (payload
  `{"target_id": targetID}`, reply stored under raw key `"scan"`); added to `GameClient`.
- Produces: `func huntScanUnknown(ctx context.Context, deps HuntDeps, out io.Writer, creatures []serverapi.NearbyCreature) int`
  — scans one creature per species that `knowledge.UnscannedSpecies` reports, parses with
  `knowledge.ParseCreatureScan`, stores with `knowledge.CaptureWildlifeScan`; returns scans made.

- [ ] **Step 1:** Test `TestHuntScanUnknownScansOnlyUnscannedSpecies`: fake client + KB where species
  `belt_grazer` is scanned and `frost_moth` is not; creatures list has two belt_grazers and one
  frost_moth → exactly one `ScanTarget` call, with the frost_moth's `CreatureID`; return value 1.
- [ ] **Step 2:** Run `go test ./pkg/worker -run HuntScanUnknown` → FAIL (undefined).
- [ ] **Step 3:** Implement `ScanTarget` and `huntScanUnknown`. Copy the reply parsing from
  `cmd/tools/play_as/explore.go` (the `CaptureWildlifeScan` call site) rather than re-deriving it.
- [ ] **Step 4:** `go build ./... && go test ./pkg/worker ./pkg/game` → PASS.
- [ ] **Step 5:** Commit `hunt: scan unknown creatures by id (scan target_id)`.

### Task 2: Hull-class eligibility for creature work

**Files:** Create `pkg/worker/hunt_hull.go`, `pkg/worker/hunt_hull_test.go`

**Interfaces:**
- Produces: `func huntCreatureWorkHull(ctx context.Context, kb knowledge.Base, classID string) bool`
  — true when `kb.GetShipClass(classID)` has `Tier >= 2` and `Category == "Combat"`; false on
  any lookup error or unknown class (the safe failure).

- [ ] **Step 1:** Table test `TestHuntCreatureWorkHull`: `gauntlet` (T2 Combat) → true;
  `prospector`-like legacy class missing from the catalog → false; a T2 `Industrial` class → false;
  a T1 Combat class → false.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `hunt: creature work requires a T2+ combat hull`.

### Task 3: Station Need parsing and admission

**Files:** Create `pkg/worker/hunt_need.go`, `pkg/worker/hunt_need_test.go`; Modify
`pkg/knowledge/wildlife.go` (+ `Base` interface and memory KB) for the reverse drop lookup.

**Interfaces:**
- Produces: `type huntNeed struct { ItemID string; Quantity int; TargetBaseID, SystemID string; Species []string; Reward int }`
- Produces: `func huntParseNeed(e serverapi.MissionBoardEntry, speciesByName map[string]string, dropsOf func(itemID string) []string) (huntNeed, bool)`
  — accepts type `delivery` with a single `deliver_item` objective; species from the
  description's `Dropped by: <Display Name>` (map through `speciesByName`, from
  `kb.SpeciesByDisplayName`), else from `dropsOf(itemID)`; `ok=false` when no species is found.
- Produces: `func (kb *SQLiteKB) SpeciesDroppingItem(ctx context.Context, itemID string) ([]string, error)` (species from `wildlife_kill_drops` ⋈ `wildlife_kills`).
- Produces: `const huntNeedMaxDifficulty = 2`
- Produces: `func huntAdmissibleNeed(n huntNeed, difficulty int, roleOf func(species string) string) (ok bool, reason string)`
  — refuses difficulty > 2, and any species whose role is not in `huntQuarryRoles` (unknown role = refuse).

- [ ] **Step 1:** Tests, using the real board text captured in
  `data/game-api/latest/get_missions.json` for Superfluid Vial:
  `TestHuntParseNeedReadsDroppedBy` (description "Dropped by: Frost-Moth (only where…)" →
  Species `["frost_moth"]`, ItemID `superfluid_vial`, Quantity 10, TargetBaseID
  `confederacy_central_command`, Reward 7118);
  `TestHuntParseNeedFallsBackToDropRecords` (no "Dropped by" → species from `dropsOf`);
  `TestHuntParseNeedRefusesUnknownSource` (neither → ok false);
  `TestHuntAdmissibleNeedRefusesPredator` (role `predator` → false, reason names the species);
  `TestHuntAdmissibleNeedRefusesUnknownRole`; `TestHuntAdmissibleNeedCapsDifficultyAt2`;
  `TestSpeciesDroppingItem` in pkg/knowledge (crusher_mantis → mantis_claw).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `hunt: parse and admit Station Need creature deliveries`.

### Task 4: Trend break-off for creature fights

**Files:** Modify `pkg/worker/hunt.go` (`huntPolicy`, `huntFight`); Test `pkg/worker/hunt_breakoff_test.go`

**Interfaces:**
- Produces: `huntPolicy.creature bool` and `huntPolicy.startSelf, startEnemy int` (hull % at the
  first tick); `func huntWornDown(startSelf, self, startEnemy, enemy int) bool` — true when
  `self <= 50` and the enemy has lost a smaller share of its hull than the hunter has
  (`startEnemy-enemy < startSelf-self`) and `enemy > 50`.
- Consumes: `huntFight(ctx, deps, out, fleeAtHull, creatureID)` gains a `creature bool` parameter;
  wildlife jobs pass true, bounty/combat jobs false.

- [ ] **Step 1:** Tests: `TestHuntWornDownRetreatsWhenOnlyHunterLosing` (self 100→50, enemy
  100→80 → true); `TestHuntWornDownFightsOnWhenTradeIsEven` (self 100→50, enemy 100→45 → false);
  `TestHuntPolicyCreatureKeepsHardFloor` (creature fight, enemy 10%, self 34% → breaks off on the
  0.35 floor); `TestHuntPolicyBountyUnchanged` (creature=false, self 45%, enemy 90% → no
  break-off at 50%).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement in `Decide` after the existing floor check.
- [ ] **Step 4:** Run the whole hunt suite `go test ./pkg/worker -run Hunt` → PASS.
- [ ] **Step 5:** Commit `hunt: creature fights retreat at 50% when only the hunter is losing`.

### Task 5: Per-fight data capture

**Files:** Modify `pkg/worker/hunt.go` (fight loop, `huntFindQuarry`); Test in `pkg/worker/hunt_capture_test.go`

**Interfaces:**
- Consumes: Task 1 `huntScanUnknown`; `HuntDeps` gains `CaptureAttacks func(ctx context.Context, battleID string) error`
  (wired in dispatch to `d.CaptureWildlifeAttacks(ctx, []string{battleID})`; nil in tests = skip).
- Behaviour: after every engagement (any outcome) call `CaptureAttacks` with the fight's battle id;
  after every get_nearby read call `huntScanUnknown`. Looting stays `huntLootCarcass` (it already
  takes every item that fits).

- [ ] **Step 1:** Tests: `TestHuntCapturesAttacksAfterEveryFight` (fled, resolved, gave-up each
  produce one `CaptureAttacks` call with the battle id); `TestHuntScansUnknownOnArrival`.
- [ ] **Step 2–4:** FAIL → implement → PASS.
- [ ] **Step 5:** Commit `hunt: capture wildlife attacks and scan unknowns every fight`.

### Task 6: The delivery job

**Files:** Create `pkg/worker/hunt_delivery.go`, `pkg/worker/hunt_delivery_test.go`; Modify
`pkg/worker/hunt.go` (`huntJob`, `huntSelectJob`, the kill loop's stop condition, step 8),
`pkg/worker/dispatch.go`.

**Interfaces:**
- Consumes: Tasks 2–5.
- Produces: `huntJob.need *huntNeed` (nil for kill missions). For a need: quarry is restricted to
  `need.Species`; the loop stops when cargo holds `need.Quantity` of `need.ItemID` (read from
  state cargo after each loot); then autopilot to `need.SystemID`, `travel need.TargetBaseID`,
  `dock`, `CompleteMission(activeID)`.
- Produces: `func huntPickNeed(needs []huntNeed, jumpsTo func(systemID string) int) (huntNeed, bool)`
  — best `Reward / (jumpsTo(ground)+jumpsTo(target)+1)`, only grounds within `huntNearbyJumps = 6`.
- Order in `huntSelectJob`: resume → chain/combat board job (today) → Station Need (only when
  `huntCreatureWorkHull` is true).
- Ground for a need: existing `huntFindGround` filtered to POIs where `GetWildlifeSightings`
  saw one of `need.Species`, empire-policed systems only (`requirePoliced=true`).

- [ ] **Step 1:** Tests: `TestHuntPickNeedPrefersRewardPerJump`; `TestHuntPickNeedSkipsBeyondSixJumps`;
  `TestHuntDeliveryStopsAtQuantityAndDelivers` (fake: 3 kills × 4 caustic_sac → stops at 10+,
  calls travel/dock/complete at `target_base_id`, not the accepting station);
  `TestHuntDeliveryMissionPartWinsHoldSpace` (Review Focus 3: loot of other items never pushes
  the need item out); `TestHuntNoNeedsOnNonCombatHull` (Review Focus 4).
- [ ] **Step 2–4:** FAIL → implement → `go test ./pkg/worker` PASS.
- [ ] **Step 5:** Commit `hunt: take Station Need deliveries and deliver to the target base`.

### Task 7: Dry-board roaming

**Files:** Create `pkg/worker/hunt_roam.go`, `pkg/worker/hunt_roam_test.go`; Modify `hunt.go` (dry branch).

**Interfaces:**
- Consumes: `capitalSystems`, `nearestCapital` (`pkg/worker/haul_desert.go`); `navigation.JumpGraph`.
- Produces: `type huntRoam struct { Visited []string; UpdatedUTC string }` persisted at
  `<AgentsDir>/<AgentID>/hunt_roam.json`.
- Produces: `func huntNextMove(graph navigation.JumpGraph, current string, nearbyStations []string, roam huntRoam, systems []knowledge.System) (kind, target string)`
  where kind is `"station"` (an unvisited empire station within `huntNearbyJumps`), `"home"`
  (CJ within `huntHomeJumps = 12`), or `"capital"` (nearest capital not in `Visited`).
- Behaviour: `"home"` runs home hunting — grazer/scavenger quarry near cloverfield with no
  mission, parts banked at CJ (Task 8). `Visited` resets when a job is found.

- [ ] **Step 1:** Tests: `TestHuntNextMoveTriesNearbyStationFirst`; `TestHuntNextMoveGoesHomeWithin12`
  (11 jumps → home; 13 → capital); `TestHuntNextMoveSkipsVisitedCapitals`.
- [ ] **Step 2–4:** FAIL → implement → PASS.
- [ ] **Step 5:** Commit `hunt: roam to nearby stations, home within 12 jumps, else a new capital`.

### Task 8: Surplus banking and the collector

**Files:** Create `pkg/worker/hunt_surplus.go`, `pkg/worker/collect_gifts.go` and tests; Modify
`pkg/worker/dispatch.go` (new schedulable command `collect_gifts`), `pkg/worker/schedule.go` allow-list.

**Interfaces:**
- Produces: `HuntDeps.GiftTo string` (collector username; empty = bank only) and
  `HuntDeps.HomeStation string` (from yaml `station`, the hunter's base).
- Produces: `func huntBankSurplus(ctx context.Context, deps HuntDeps, out io.Writer, keep map[string]int) error`
  — docked at base: deposit cargo except `keep` (the active need) to storage; when stored
  creature parts total ≥ `huntGiftBatch = 50`, `send_gift <GiftTo> <item> <qty> --source=storage`
  per item. At CJ (home hunting) the recipient is `haulDumpRecipient` (miningbot_0000) instead.
- Produces: collector command `collect_gifts`: `view_storage` across stations → for the station
  with the most units (≥ `collectMinUnits = 50`), autopilot there, `withdraw_items` all,
  autopilot to CJ, `send_gift` to `haulDumpRecipient` (its faction_deposit_all sweeps them in).

- [ ] **Step 1:** Tests: `TestHuntBankSurplusKeepsNeedItem`; `TestHuntBankSurplusGiftsAtBatch`;
  `TestHuntBankSurplusAtHomeGiftsToMiningbot0000`; `TestCollectGiftsPicksFullestStation`;
  `TestCollectGiftsSkipsBelowMinimum`.
- [ ] **Step 2–4:** FAIL → implement → PASS.
- [ ] **Step 5:** Commit `hunt: bank surplus parts and gift them to a collector hauler`.

### Task 9: Fleet wiring and rollout (operator approves each live step)

**Files:** Modify `data/overmind/hunt-fleet.yaml` (each gauntlet hunter's `station` = its base),
`data/overmind/roles.yaml` (`hunt` schedule unchanged; collector `collect_gifts` hourly on the
chosen hauler), `pkg/worker/dispatch.go` (`GiftTo`, `HomeStation`, `CaptureAttacks` into `HuntDeps`).

- [ ] **Step 1:** `go build ./... && go test ./... && golangci-lint run` → all clean.
- [ ] **Step 2:** Operator picks each hunter's base station and the collector hauler; set them.
- [ ] **Step 3:** Gift unlock per hunter that lacks it (play_as: accept `market_participation_buying`,
  buy 10 of the cheapest item, `complete_mission <mission_id>`).
- [ ] **Step 4:** Rebuild `bin/worker`, roll the hunt fleet (and the collector's fleet) with the
  SIGTERM → relaunch procedure; verify worker start time > binary mtime.
- [ ] **Step 5:** Watch for `First Hunt` acceptance, a Station Need delivery, `scan` captures and
  no `SHIP DESTROYED`; commit config `hunt: gauntlet hunters on creature work`.
