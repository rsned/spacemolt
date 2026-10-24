# Contraband Pipeline — Worker & Fleet Code Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the fleet the verbs, gates and roles the contraband pipeline needs: a pinned pipeline miner that hands off its whole hold, a Lawless gate on mining, a shopkeeper that posts asks, a crafter step that pulls product out of faction storage, and the two supervisor fixes the builder check found.

**Architecture:** Everything is an idle-pass step or a dispatch verb inside the existing worker (`pkg/worker`), switched on per agent by fleet-yaml fields that the supervisor forwards as worker flags, exactly the way `consign` / `consign_offer` already work. No new processes. Supervisor fixes are two small predicate changes with tests.

**Tech Stack:** Go 1.24+, `pkg/worker`, `pkg/overmind/supervisor`, `pkg/game` client, `pkg/knowledge`, `pkg/assets`; tests use the existing `fakeClient` (`pkg/worker/dispatch_test.go`) and `fakeKB` (`pkg/worker/explore_test.go`).

**Spec:** `docs/superpowers/specs/2026-10-24-contraband-production-pipeline-design.md` (D1–D9, "Must-fix findings"). This plan is plan 1 of 3; the rollout runbook and the dashboard are separate plans.

## Global Constraints

- Go 1.24+: range-over-int, `b.Loop()` in benchmarks.
- Every new file passes `golangci-lint` with no new findings; run it after each task.
- Never `time.Sleep` in `pkg/worker`; use the injected `minePollSleep` / `settle(ctx, d)` seams.
- All sleeps use constants from `pkg/game/constants.go` (`SleepTick`, `SleepShort`, …).
- Gifting is irreversible: every new verb that gifts or sells takes an explicit allow-list; never a pattern. Never touch `energy_crystal`.
- New verbs go in `supported` (`pkg/worker/dispatch.go:204`); `roles_test.go` fails otherwise.
- Fleet yaml → supervisor flag → worker flag is the only per-worker config path (`idle_params` is dead).
- Commit after every task; `go build ./... && go test ./...` before each commit.

## Review Focus

1. A locked bot (baseline −30) given a pinned Lawless POI must refuse to depart and log why; it must not strand itself one jump out. Test in Task 1 and Task 2.
2. A KB system row with empty `SecurityStatus` must not block an unlocked bot (stale/missing data fails open with a warning). Test in Task 1.
3. A hold full of byproduct with zero target item must still be handed off, not carried back to the belt forever. Test in Task 3.
4. The shopkeeper must never post a second ask for an item while one is open, and never price below the floor. Tests in Task 7.
5. The faction pull must never withdraw more than the free hold and must leave non-allow-listed items in faction storage. Tests in Task 8.

---

### Task 1: Lawless gate

**Files:**
- Modify: `pkg/worker/mine_qty.go` (beside `strongholdBlocked`, line 159)
- Test: `pkg/worker/mine_qty_test.go`

**Interfaces:**
- Consumes: `knowledge.System{SecurityStatus string, PoliceLevel int}` (`pkg/knowledge/memory.go:397`); `(*assets.Store).HoldsPirateUnlock(ctx, agentID) (bool, error)`; `WorkerDispatch.Assets`, `WorkerDispatch.AgentID`.
- Produces: `func lawlessBlocked(sys *knowledge.System, unlocked bool) (blocked bool, reason string)` and `func (d *WorkerDispatch) pirateUnlocked(ctx context.Context) bool` (false when `d.Assets == nil`).

- [ ] **Step 1: Write the failing tests**

```go
func TestLawlessBlockedRefusesLockedBot(t *testing.T) {
	sys := &knowledge.System{ID: "glenhaven", SecurityStatus: "Lawless (no police protection)", PoliceLevel: 0}
	blocked, reason := lawlessBlocked(sys, false)
	if !blocked || !strings.Contains(reason, "glenhaven") { t.Fatalf("want blocked with system in reason, got %v %q", blocked, reason) }
}
func TestLawlessBlockedAdmitsUnlockedBot(t *testing.T) {
	sys := &knowledge.System{ID: "glenhaven", SecurityStatus: "Lawless (no police protection)"}
	if blocked, _ := lawlessBlocked(sys, true); blocked { t.Fatal("unlocked bot must pass") }
}
func TestLawlessBlockedFailsOpenOnUnknownSecurity(t *testing.T) {
	sys := &knowledge.System{ID: "alniyat", SecurityStatus: "", PoliceLevel: 0}
	blocked, reason := lawlessBlocked(sys, false)
	if blocked { t.Fatal("unknown security must not block") }
	if !strings.Contains(reason, "unknown") { t.Fatalf("want a warning reason, got %q", reason) }
}
func TestLawlessBlockedIgnoresCase(t *testing.T) {
	if blocked, _ := lawlessBlocked(&knowledge.System{ID: "x", SecurityStatus: "LAWLESS"}, false); !blocked { t.Fatal("case-insensitive match") }
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./pkg/worker -run 'TestLawlessBlocked' -v`
Expected: FAIL, `undefined: lawlessBlocked`

- [ ] **Step 3: Implement in `pkg/worker/mine_qty.go`**

`lawlessBlocked`: blocked iff `strings.Contains(strings.ToLower(sys.SecurityStatus), "lawless") && !unlocked`; when `sys == nil || sys.SecurityStatus == ""` return `(false, "security of <id> unknown; proceeding")`; reason strings name the system id. `pirateUnlocked`: `d.Assets == nil` → false; else `HoldsPirateUnlock(ctx, d.AgentID)`, treating an error as false and writing it to `d.Out`.

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/worker -run 'TestLawlessBlocked' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/mine_qty.go pkg/worker/mine_qty_test.go
git commit -m "feat(worker): lawless gate for mining — locked bots refuse Lawless systems, unknown security fails open"
```

---

### Task 2: `mine_qty_at` — explicit POI with both gates

**Files:**
- Modify: `pkg/worker/mine_qty.go` (`MineQty` line 56; add `MineQtyAt`)
- Modify: `pkg/worker/dispatch.go` (`supported` line 204; `case "mine_qty"` line 449)
- Test: `pkg/worker/mine_qty_test.go`

**Interfaces:**
- Consumes: Task 1; `galaxy`/`d.KB.GetSystem(ctx, id) (*knowledge.System, error)`; `d.autopilotAndUndock`, `d.mineLoop`, `d.Deliver`.
- Produces: `func (d *WorkerDispatch) MineQtyAt(ctx context.Context, itemID string, qty int, systemID, poiID, to, recipient string) error`; dispatch verb `mine_qty_at ITEM QTY SYSTEM POI TO RECIPIENT`.

- [ ] **Step 1: Write the failing tests** (model on `TestMineQtyMinesUntilQtyThenDelivers`, line 96, using `fakeClient` + `fakeKB` with one `knowledge.System{ID:"glenhaven", SecurityStatus:"Lawless (no police protection)"}`)

```go
func TestMineQtyAtUsesThePinnedPOI(t *testing.T)            // calls contain "autopilot:glenhaven/glenhaven_ice_belt" (whatever fakeClient records), then mine, then deliver to recipient at TO
func TestMineQtyAtRefusesLawlessWhenLocked(t *testing.T)     // d.Assets == nil → error contains "lawless" and "glenhaven"; no autopilot call recorded
func TestMineQtyAtRefusesStrongholdWhenLocked(t *testing.T)  // system IsStronghold:true → error mentions stronghold; no travel
func TestMineQtyAtKeepsFuelFloor(t *testing.T)               // state.Ship.Fuel = MineQtyMinFuelReserve-1 → error mentions "reserve", no travel
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./pkg/worker -run 'TestMineQtyAt' -v`
Expected: FAIL, `undefined: MineQtyAt`

- [ ] **Step 3: Implement `MineQtyAt`** — same fuel floor as `MineQty`; look up `sys, err := d.KB.GetSystem(ctx, systemID)` (nil KB → error); `strongholdBlocked(d.mineStrongholdRefs(ctx, state), sys.ID, sys.Name)` → error; `lawlessBlocked(sys, d.pirateUnlocked(ctx))` → error when blocked, else write the reason to `d.Out` when non-empty; then `autopilotAndUndock(systemID, poiID)`, `mineLoop(itemID, qty, poiID)`, deliver as `MineQty` does. Refactor `MineQty`'s tail into a shared `mineThenDeliver` helper if that avoids copying; otherwise leave it. Add `"mine_qty_at": true` to `supported` and the dispatch case (6 args, qty parsed like `mine_qty`).

- [ ] **Step 4: Run tests + the roles test**

Run: `go test ./pkg/worker -run 'TestMineQtyAt|TestSeededCommands' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/mine_qty.go pkg/worker/dispatch.go pkg/worker/mine_qty_test.go
git commit -m "feat(worker): mine_qty_at — pinned POI mining behind the stronghold and lawless gates"
```

---

### Task 3: Hand off the whole hold

**Files:**
- Modify: `pkg/worker/mine_qty.go` (`MineQtyAt` tail)
- Test: `pkg/worker/mine_qty_test.go`

**Interfaces:**
- Consumes: `state.Ship.Cargo []game.CargoItem{ItemID string, Quantity float64}` (`pkg/game/types.go:182,225`); `d.Deliver(ctx, item, qty, "", to, recipient)`.
- Produces: `handOffHold` — AS BUILT (final-review amendment, commit dd10dec9): `MineQtyAt` snapshots cargo before departure and hands off, largest line first, the FULL current quantity of (a) the target item always and (b) every other line that grew on this trip; `fuel_cell` and `energy_crystal` are always kept (`handOffAlwaysKept`); unrelated lines that did not grow stay aboard. The original "everything but fuel_cell" wording below is superseded.

- [ ] **Step 1: Write the failing tests**

```go
func TestMineQtyAtHandsOffByproduct(t *testing.T)   // after mineLoop cargo = water_ice 110 + nitrogen_ice 60 → two deliver calls, both to recipient at TO
func TestMineQtyAtHandsOffEvenWithZeroTarget(t *testing.T) // dry belt, cargo = nitrogen_ice 40 only → one deliver call, returns nil (partial success is success)
func TestHandOffHoldSkipsKeepList(t *testing.T)     // keep{"fuel_cell":true} → fuel_cell line not delivered
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./pkg/worker -run 'HandsOff|HandOffHold' -v`
Expected: FAIL

- [ ] **Step 3: Implement** — `handOffHold` iterates a fresh `d.Client.GetState().Ship.Cargo`; `keep` always includes `fuel_cell` (the refuel fallback) in addition to the caller's set. `MineQtyAt` replaces its single `Deliver` with `handOffHold(ctx, to, recipient, nil)` and returns nil when at least one line was delivered or the hold was empty after a dry belt (log it).

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/worker -run 'TestMineQtyAt|HandOffHold' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/mine_qty.go pkg/worker/mine_qty_test.go
git commit -m "feat(worker): mine_qty_at hands off the whole hold, byproduct included"
```

---

### Task 4: `pipeline_miner` role — yaml → flag → verb

**Files:**
- Create: `pkg/worker/pipeline.go`, `pkg/worker/pipeline_test.go`
- Create: `data/scripts/pipeline_mine.smolt`
- Modify: `pkg/overmind/supervisor/config.go` (WorkerSpec, line ~12), `pkg/overmind/supervisor/supervisor.go` (args, line ~92), `pkg/overmind/supervisor/config_test.go`
- Modify: `cmd/worker/main.go` (flag beside `--consign-offer`, line 90; set `dispatch.Pipeline`)
- Modify: `pkg/worker/dispatch.go` (`Pipeline *Pipeline` field; `supported`; `case "pipeline_mine"`)
- Modify: `data/overmind/roles.yaml` (new role)

**Interfaces:**
- Consumes: Task 2 `MineQtyAt`; Task 3.
- Produces:
  - `type Pipeline struct{ Item, SystemID, POI, To, Recipient string }`
  - `func ParsePipeline(s string) (*Pipeline, error)` — `"item,system,poi,to,recipient"`, 5 non-empty comma fields, else error; `""` → `nil, nil`.
  - `func (d *WorkerDispatch) PipelineMine(ctx context.Context) error` — qty = free hold (`CargoCapacity - CargoUsed`, min 1), calls `MineQtyAt`.
  - WorkerSpec field `Pipeline string \`yaml:"pipeline,omitempty"\`` forwarded as `--pipeline <value>`.
  - Role `pipeline_miner` in roles.yaml: `idle: pipeline_mine`, `idle_ticks: 2`, schedule: hourly `capture_profile`, `capture_fuel`, `capture_storage`; daily `capture_faction`, `capture_tax`, `capture_citizenship`.
  - `data/scripts/pipeline_mine.smolt` = `refuel` then `pipeline_mine` (two lines; comment header explaining refuel-first exactly as `idle_mine.smolt` does).

- [ ] **Step 1: Write the failing tests**

```go
// pkg/worker/pipeline_test.go
func TestParsePipeline(t *testing.T)             // table: valid 5 fields; 4 fields → error; empty item → error; "" → nil,nil
func TestPipelineMineUsesFreeHold(t *testing.T)  // CargoCapacity 175, CargoUsed 10 → MineQtyAt called with qty 165 (assert via recorded mine/deliver calls or a seam)
func TestPipelineMineWithoutConfigErrors(t *testing.T) // d.Pipeline nil → error "pipeline_mine: no --pipeline configured"
// pkg/overmind/supervisor/config_test.go
func TestWorkerSpecPipelineForwardsFlag(t *testing.T)  // spec.Pipeline "water_ice,glenhaven,glenhaven_ice_belt,treasure_cache_trading_post,trader-1" → args contain "--pipeline" followed by that string
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./pkg/worker -run 'Pipeline' -v && go test ./pkg/overmind/supervisor -run 'Pipeline' -v`
Expected: FAIL

- [ ] **Step 3: Implement** — parsing and verb in `pipeline.go`; flag `--pipeline` in `main.go` parsed with `ParsePipeline` (fatal on error, like `--consign`); supervisor forwards when non-empty (follow the `Consign` block at supervisor.go:92); add the role block and the smolt file.

- [ ] **Step 4: Run tests, roles test, build**

Run: `go test ./pkg/worker ./pkg/overmind/supervisor -v -run 'Pipeline|TestSeededCommands|TestLoadRoles' && go build ./...`
Expected: PASS (roles test proves `pipeline_mine` is in `supported` and the script resolves)

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/pipeline.go pkg/worker/pipeline_test.go pkg/worker/dispatch.go cmd/worker/main.go pkg/overmind/supervisor/config.go pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/config_test.go data/scripts/pipeline_mine.smolt data/overmind/roles.yaml
git commit -m "feat(fleet): pipeline_miner role — yaml pipeline spec forwarded to a pinned mine-and-hand-off idle"
```

---

### Task 5: Stall watchdog matches any `*miner` role

**Files:**
- Modify: `pkg/overmind/supervisor/fleet.go:235-243`
- Test: `pkg/overmind/supervisor/fleet_test.go`

**Interfaces:**
- Produces: unchanged signature `StallTimeoutFor(info WorkerInfo, base time.Duration) time.Duration`; now applies `MinerStallFactor` when `info.Role == "miner"` or `strings.HasSuffix(info.Role, "_miner")`.

- [ ] **Step 1: Write the failing test**

```go
func TestStallTimeoutForPipelineMiner(t *testing.T) {
	base := 15 * time.Minute
	if got := StallTimeoutFor(WorkerInfo{Role: "pipeline_miner"}, base); got != base*MinerStallFactor { t.Fatalf("got %v", got) }
	if got := StallTimeoutFor(WorkerInfo{Role: "resident"}, base); got != base { t.Fatalf("resident must keep base, got %v", got) }
}
```

- [ ] **Step 2: Run to verify it fails** — `go test ./pkg/overmind/supervisor -run TestStallTimeoutForPipelineMiner -v` → FAIL
- [ ] **Step 3: Implement** the suffix match (keep the `== "miner"` case).
- [ ] **Step 4: Run** the same test plus `TestStalled` → PASS
- [ ] **Step 5: Commit** — `git commit -m "fix(supervisor): stall factor applies to every *_miner role"`

---

### Task 6: Reconciler away-fleet filter

**Files:**
- Modify: `pkg/overmind/supervisor/reconcile.go:124-150`
- Test: `pkg/overmind/supervisor/reconcile_test.go` (use the existing `fakeFleet` fixture, line 20)

**Interfaces:**
- Produces: `ReconcileSecondments` additionally skips an entry when `e.AwayFleet != "" && e.AwayFleet != awayFleet.Name`.

- [ ] **Step 1: Write the failing test**

```go
func TestReconcileSkipsEntriesForAnotherAwayFleet(t *testing.T) {
	// ledger: one nominated entry HomeFleet "haul", AwayFleet "crft-mining"; sweep run with home=haul, away=unlock
	// expect: no moves in trace, entry phase still "nominated"
}
```

- [ ] **Step 2: Run** → FAIL (entry gets moved into unlock)
- [ ] **Step 3: Implement** the filter next to the `HomeFleet` check.
- [ ] **Step 4: Run** `go test ./pkg/overmind/supervisor -run 'Reconcile|Secondment|Graduation' -v` → PASS
- [ ] **Step 5: Commit** — `git commit -m "fix(secondment): sweep only entries bound for its own away fleet"`

---

### Task 7: Shopkeeper — post and manage asks

**Files:**
- Create: `pkg/worker/shopkeep.go`, `pkg/worker/shopkeep_test.go`
- Modify: `pkg/worker/standing.go` (StandingDeps: `Shopkeep func(context.Context)`, run after `ConsignOffer` at line ~290)
- Modify: `cmd/worker/main.go` (flag `--shopkeep`, deps wiring beside `deps.ConsignOffer`), `pkg/overmind/supervisor/config.go` + `supervisor.go` (`Shopkeep []string \`yaml:"shopkeep,omitempty"\`` → `--shopkeep a,b`), `config_test.go`

**Interfaces:**
- Consumes: `client.ViewStorage(ctx)` + `consignHeld(client.GetRawJSON("storage"))` (`consign.go:107`); `client.ViewMarket(ctx, map[string]any{"item_id": item})` + `GetRawJSON("market")` decoded as `serverapi.ViewMarketResponse{Orders []ExchangeOrder{OrderID, Side, ItemID, Remaining, PriceEach, CreatedBy, CreatedAt}}`; `client.CreateSellOrder(ctx, map[string]any{"item_id","quantity","price_each"})`; `client.ModifyOrder(ctx, map[string]any{"order_id","price_each"})`; `state.Player.Username`.
- Produces:
  - `type ShopRule struct{ Item string; Start, Floor int }`; `func ParseShopRules(s string) ([]ShopRule, error)` — `"item:start[:floor]"`, comma-separated; start ≥ 1; floor defaults to start/2; floor > start → error.
  - `type ShopState struct{ Items map[string]ShopItemState }` with `ShopItemState{Price int; OrderID string; PostedAt time.Time; LastFillAt time.Time}`; `LoadShopState(path) (*ShopState, error)`, `(*ShopState).Save(path) error` — JSON at `data/agents/<id>/shopkeep.json`.
  - `type ShopOptions struct{ Lot int; StepPct int; StaleAfter time.Duration; Now func() time.Time }` defaults `Lot 50, StepPct 5, StaleAfter 6*time.Hour`.
  - `func Shopkeep(ctx context.Context, client game.GameClient, rules []ShopRule, st *ShopState, opts ShopOptions, out io.Writer) (posted, repriced int, err error)`.
  - Policy per rule: own open ask = order in `Orders` with `Side=="sell"`, `ItemID==rule.Item`, `CreatedBy==username`. If one exists and `now - PostedAt > StaleAfter` → `ModifyOrder` to `max(Floor, Price*(100-StepPct)/100)`, update state. If none exists: if the previous order filled (`OrderID != ""` and now absent) → `Price = Price*(100+StepPct)/100`, `LastFillAt = now`; if held ≥ 1 → `CreateSellOrder(min(held, Lot), Price)`, record `OrderID` from the response raw JSON (`GetRawJSON("create_sell_order")` → `order_id`), `PostedAt = now`. First ever post uses `Start`.

- [ ] **Step 1: Write the failing tests** (extend `fakeClient`: it already has `CreateSellOrder` and `ViewMarket`; add a `raw["market"]`/`raw["storage"]` fixture and record `modify_order`)

```go
func TestParseShopRules(t *testing.T)                 // table incl. floor default and floor>start error
func TestShopkeepPostsFirstAskAtStart(t *testing.T)   // held 72, no orders → one create_sell_order qty 50 price 5000; state OrderID set
func TestShopkeepNeverDoublePosts(t *testing.T)       // own open ask present, fresh → no create, no modify
func TestShopkeepRepricesStaleAskDownToFloor(t *testing.T) // open ask PostedAt 7h ago, price 5000 floor 4800 step 5 → modify to 4800 (not 4750)
func TestShopkeepStepsUpAfterFill(t *testing.T)       // state OrderID "o1", market has no o1, held 22 → create at 5250 qty 22
func TestShopkeepSkipsWithoutStorageReading(t *testing.T) // raw storage missing → posted 0, no calls (mirror ReportConsignStock)
func TestShopkeepIgnoresOtherSellersOrders(t *testing.T) // order CreatedBy "someone else" → treated as none
```

- [ ] **Step 2: Run** `go test ./pkg/worker -run 'Shop' -v` → FAIL
- [ ] **Step 3: Implement** `shopkeep.go`; wire `StandingDeps.Shopkeep` (nil-safe, runs after `ConsignOffer`); `main.go` parses `--shopkeep` with `ParseShopRules`, loads state from `filepath.Join(agentsDir, agentID, "shopkeep.json")`, and sets `deps.Shopkeep` to call `Shopkeep` then `Save`; supervisor forwards `shopkeep` yaml list.
- [ ] **Step 4: Run** `go test ./pkg/worker ./pkg/overmind/supervisor -run 'Shop|Standing|Pipeline' -v && go build ./...` → PASS
- [ ] **Step 5: Commit**

```bash
git add pkg/worker/shopkeep.go pkg/worker/shopkeep_test.go pkg/worker/standing.go pkg/worker/dispatch_test.go cmd/worker/main.go pkg/overmind/supervisor/config.go pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/config_test.go
git commit -m "feat(worker): shopkeep — post and reprice asks from personal storage on the idle pass"
```

---

### Task 8: Crafter faction pull — product out of faction storage

**Files:**
- Create: `pkg/worker/faction_pull.go`, `pkg/worker/faction_pull_test.go`
- Modify: `pkg/worker/standing.go` (StandingDeps `FactionPull func(context.Context)`, run before `Consign`/`ConsignOffer`/`Shopkeep`), `cmd/worker/main.go` (`--faction-pull`), supervisor config/args (`FactionPull []string \`yaml:"faction_pull,omitempty"\``), `config_test.go`

**Interfaces:**
- Consumes: `client.ViewFactionStorage(ctx)` + `GetRawJSON("faction_storage")` (items `{item_id, quantity}` — decode with the same shape helper as `consignHeld`, adjusted to the faction response's `items` key; verify the key against a captured `view_faction_storage` reply before writing the decoder); `client.FactionWithdrawItems(ctx, item, qty)` (lands in cargo); `client.DepositItems(ctx, item, float64(qty))`; `state.Ship.CargoCapacity - CargoUsed`.
- Produces: `func FactionPull(ctx context.Context, client game.GameClient, items []string, out io.Writer) (moved int, err error)` — for each allow-listed item with faction quantity > 0: `qty = min(quantity, freeHold)`; withdraw then deposit; stop when free hold is 0.

- [ ] **Step 1: Write the failing tests**

```go
func TestFactionPullMovesAllowListedOnly(t *testing.T)  // faction has pirate_moonshine 72 + water_ice 1335, list ["pirate_moonshine"], hold 150 → withdraw 72, deposit 72, water_ice untouched
func TestFactionPullBoundedByFreeHold(t *testing.T)     // hold free 50, faction 72 → withdraw 50, deposit 50, moved 50
func TestFactionPullSkipsWithoutAReading(t *testing.T)  // raw faction_storage missing → moved 0, no withdraw
```

- [ ] **Step 2: Run** `go test ./pkg/worker -run 'FactionPull' -v` → FAIL
- [ ] **Step 3: Implement** and wire exactly like Task 7 (flag, deps step, yaml field, supervisor arg).
- [ ] **Step 4: Run** `go test ./pkg/worker ./pkg/overmind/supervisor -v && go build ./... && golangci-lint run ./pkg/worker/... ./pkg/overmind/... ./cmd/worker/...` → PASS, no new findings
- [ ] **Step 5: Commit** — `git commit -m "feat(worker): faction_pull — move allow-listed product from faction storage to personal storage"`

---

### Task 9: Rebuild and smoke the verbs live (no fleet changes yet)

**Files:** none (operations)

- [ ] **Step 1:** `go build -o bin/worker ./cmd/worker && go build -o bin/overmind ./cmd/overmind` (memory: a restart deploys nothing without a rebuild).
- [ ] **Step 2:** Shopkeeper smoke on the 72 test units: add `shopkeep: ["pirate_moonshine:5200:4000"]` to craftsman-10's line in `data/overmind/craft-fleet.yaml`, SIGHUP the craft overmind (it relaunches the worker with the new flag), then within two idle passes confirm in `data/overmind/craft-overmind.log` a `shopkeep: posted 50 pirate_moonshine @5200` line and `data/agents/craftsman-10/shopkeep.json` holds the order id. Expected: one ask visible in `view_market` at Voss under craftsman-10's username.
- [ ] **Step 3:** Faction pull smoke: add `faction_pull: ["pirate_moonshine"]` to the same line after the next craft job; confirm the log shows `faction_pull: moved N pirate_moonshine`.
- [ ] **Step 4:** Pipeline smoke with ONE bot, in secure space: fund `miningbot_0000` via the banker, add a `crft-mining-fleet.yaml` with that single worker, `role: pipeline_miner`, `pipeline: "water_ice,<secure ice system>,<poi>,treasure_cache_trading_post,trader-1"` (the builder picks the secure site from `poi_resources` joined to `systems.security_status` not containing Lawless), launch the overmind with `--assets-db-path data/assets.db --handoff-queue data/overmind/handoff-queue.json`, and confirm one full cycle: autopilot, mine until full, deliver, refuel. Then stop it; the rollout plan takes over.
- [ ] **Step 5:** Commit the yaml changes and record the smoke results in memory (`make memory-sync`).

---

## Self-review notes

- Spec coverage: D3 (role, explicit POI, harvesters fitting is an ops step → rollout plan), D1 byproduct (Task 3), D2 Lawless gate (Tasks 1–2), D5 faction pull (Task 8), D9 shopkeeper (Task 7), must-fix items (Tasks 3, 5, 6, 4). D4 haulers, D6 money, D2 waves, D7 dashboard, D8 rollout → plans 2 and 3.
- Names used consistently: `MineQtyAt`, `handOffHold`, `Pipeline`/`ParsePipeline`/`PipelineMine`, `ShopRule`/`ShopState`/`Shopkeep`, `FactionPull`, `lawlessBlocked`, `pirateUnlocked`.
- Left open on purpose: the exact raw-JSON key shapes for `faction_storage` and `create_sell_order` responses (verify against a capture before decoding), and the secure-space smoke site.
