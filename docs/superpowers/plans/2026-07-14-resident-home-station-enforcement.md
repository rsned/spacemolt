# Resident Home-Station Enforcement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give resident marketbots a standing behavior that returns them to their configured home station, so a drifted bot self-corrects instead of capturing the wrong station's market forever.

**Architecture:** Add a data-driven `ensure_home` command to the lean `WorkerDispatch` vocabulary. It reads the worker's home station POI from `WorkerDispatch.Station` (populated from the fleet YAML `station:` field, forwarded by the overmind as `--station`), resolves the home *system* live via `FindRoute` (last hop's system, or current system when the route is empty — the mechanism `assistResolveMobile` already uses), then autopilots home and docks. It runs as the first line of a new resident idle script, so it fires every idle pass and no-ops once docked at home.

**Tech Stack:** Go 1.24, `pkg/worker` (dispatch + standing behavior), `data/scripts/*.smolt`, `data/overmind/roles.yaml`, `data/overmind/mb-fleet.yaml`.

## Global Constraints

- Target Go 1.24+; use modern features (range-over-int, `b.Loop()` in benchmarks) where applicable.
- New code must pass `golangci-lint` with no new findings.
- Run `go build ./...` and `go test ./...` before every commit.
- Any sleep/pause must use a constant from `pkg/game/constants.go` (none are needed in this plan).
- Do NOT assume server response field names — the fields used here (`State.CurrentPOI`, `State.Doc`, `State.System.ID`, `RouteStep.SystemID`) are verified against `pkg/game/types.go`.

---

### Task 1: `ensure_home` dispatch command + navigation seam

**Files:**
- Modify: `pkg/worker/dispatch.go` (add `ensureHomeNav` field to `WorkerDispatch`; set its default in `NewWorkerDispatch`; add `"ensure_home": true` to `supported`; add a `case "ensure_home"`; add the `ensureHome` method; add `"strings"` to imports)
- Test: `pkg/worker/dispatch_test.go` (add `ensure_home` unit tests; ensure `slices` and `strings` are imported)

**Interfaces:**
- Consumes: `game.GameClient` (`GetState() *game.State`, `FindRoute(ctx, target) ([]game.RouteStep, error)`, `Dock(ctx) error`); `Autopilot(ctx, AutopilotDeps, system, poi) error`; `game.State{ CurrentPOI string; Doc bool; System game.SystemData }`; `game.SystemData{ ID string }`; `game.RouteStep{ SystemID string }`.
- Produces: `WorkerDispatch.ensureHome(ctx) error` (best-effort, always returns nil); a new dispatchable command token `ensure_home`; an unexported overridable field `ensureHomeNav func(ctx context.Context, system, poi string) error`.

- [ ] **Step 1: Write the failing tests**

Add to `pkg/worker/dispatch_test.go` (add `"slices"` and `"strings"` to the import block if not already present):

```go
func TestEnsureHomeNoStation(t *testing.T) {
	c := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "" // no home configured
	navigated := false
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { navigated = true; return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home: %v", err)
	}
	if navigated {
		t.Error("navigated despite no home configured")
	}
	for _, call := range c.calls {
		if strings.HasPrefix(call, "find_route") {
			t.Errorf("called %q with no home", call)
		}
	}
}

func TestEnsureHomeAlreadyDocked(t *testing.T) {
	c := &fakeClient{state: &game.State{CurrentPOI: "grand_exchange", Doc: true}}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "grand_exchange"
	navigated := false
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { navigated = true; return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home: %v", err)
	}
	if navigated {
		t.Error("navigated while already docked at home")
	}
	for _, call := range c.calls {
		if strings.HasPrefix(call, "find_route") || call == "dock" {
			t.Errorf("unexpected call %q when already home", call)
		}
	}
}

func TestEnsureHomeDisplacedTravelsAndDocks(t *testing.T) {
	c := &fakeClient{
		state: &game.State{CurrentPOI: "unknown_edge_waystation", Doc: true},
		route: []game.RouteStep{{SystemID: "market_prime"}},
	}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "market_prime_exchange"
	var gotSystem, gotPOI string
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error {
		gotSystem, gotPOI = system, poi
		return nil
	}
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home: %v", err)
	}
	if gotSystem != "market_prime" || gotPOI != "market_prime_exchange" {
		t.Errorf("navigated to %s/%s, want market_prime/market_prime_exchange", gotSystem, gotPOI)
	}
	if !slices.Contains(c.calls, "find_route:market_prime_exchange") {
		t.Errorf("find_route not called; calls=%v", c.calls)
	}
	if !slices.Contains(c.calls, "dock") {
		t.Errorf("dock not called; calls=%v", c.calls)
	}
}

func TestEnsureHomeEmptyRouteUsesCurrentSystem(t *testing.T) {
	// In the home system already (route empty) but parked at a belt, not the station.
	c := &fakeClient{
		state: &game.State{CurrentPOI: "market_prime_belt", Doc: false, System: game.SystemData{ID: "market_prime"}},
		route: nil,
	}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "market_prime_exchange"
	var gotSystem string
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { gotSystem = system; return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home: %v", err)
	}
	if gotSystem != "market_prime" {
		t.Errorf("system=%q, want market_prime from current state", gotSystem)
	}
}

func TestEnsureHomeFindRouteErrorIsBestEffort(t *testing.T) {
	c := &fakeClient{
		state:    &game.State{CurrentPOI: "somewhere", Doc: false},
		routeErr: errors.New("You are not in a system"),
	}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "market_prime_exchange"
	navigated := false
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { navigated = true; return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home must be best-effort nil, got %v", err)
	}
	if navigated {
		t.Error("navigated despite find_route error")
	}
}

func TestEnsureHomeToleratesAlreadyDockedError(t *testing.T) {
	c := &fakeClient{
		state:   &game.State{CurrentPOI: "market_prime_belt", Doc: false, System: game.SystemData{ID: "market_prime"}},
		dockErr: errors.New("Already docked at this station"),
	}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "market_prime_exchange"
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home must swallow 'Already docked', got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestEnsureHome -v`
Expected: FAIL — compile error `d.ensureHomeNav undefined` (and `ensure_home` unsupported).

- [ ] **Step 3: Add the `ensureHomeNav` field to `WorkerDispatch`**

In `pkg/worker/dispatch.go`, inside the `WorkerDispatch` struct (next to `handoffPersist`), add:

```go
	// ensureHomeNav navigates the ship to (system, poi) for the ensure_home
	// command. Defaults to a real Autopilot round-trip; tests override it to
	// avoid driving the routing loop against a stub client.
	ensureHomeNav func(ctx context.Context, system, poi string) error
```

- [ ] **Step 4: Default `ensureHomeNav` in `NewWorkerDispatch`**

Replace the `return &WorkerDispatch{...}` at the end of `NewWorkerDispatch` with a named value so the closure can reference the dispatch:

```go
	d := &WorkerDispatch{
		Client: client, KB: kb, Market: mc, Out: out,
		treasury:       &treasuryRescue{},
		shuttle:        &shuttleState{},
		craftPollSleep: craftPollSleepFunc,
		minePollSleep:  craftPollSleepFunc,
		handoffPersist: defaultHandoffPersist,
	}
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error {
		return Autopilot(ctx, AutopilotDeps{Client: d.Client, Out: d.Out}, system, poi)
	}
	return d
```

- [ ] **Step 5: Register `ensure_home` in the `supported` set**

In `pkg/worker/dispatch.go`, add `ensure_home` to the `supported` map (put it on the movement line):

```go
	"undock": true, "dock": true, "travel": true, "jump": true, "autopilot": true, "ensure_home": true,
```

- [ ] **Step 6: Add the dispatch case and the `ensureHome` method**

In `pkg/worker/dispatch.go`, add the case (near `case "dock":`):

```go
	case "ensure_home":
		return d.ensureHome(ctx)
```

Add `"strings"` to the import block, then add the method (place it after `Run`):

```go
// ensureHome parks a resident worker docked at its configured home station
// (d.Station). It no-ops when no home is configured or the ship is already
// docked there. The home *system* is resolved live via FindRoute — the last
// hop's system, or the current system when the route is empty — mirroring the
// mobile-home resolution in assist. Best-effort: every failure logs and returns
// nil so the standing loop simply retries on the next idle pass.
func (d *WorkerDispatch) ensureHome(ctx context.Context) error {
	home := d.Station
	if home == "" {
		return nil
	}
	st := d.Client.GetState()
	if st != nil && st.CurrentPOI == home && st.Doc {
		return nil // already parked and docked at home
	}
	route, err := d.Client.FindRoute(ctx, home)
	if err != nil {
		fmt.Fprintf(d.Out, "ensure_home: find_route %s: %v\n", home, err) //nolint:errcheck
		return nil
	}
	system := ""
	if len(route) > 0 {
		system = route[len(route)-1].SystemID
	} else if st != nil {
		system = st.System.ID
	}
	if system == "" {
		fmt.Fprintf(d.Out, "ensure_home: cannot resolve home system for %s\n", home) //nolint:errcheck
		return nil
	}
	// Travel only when we are not already sitting at the home POI. Re-traveling
	// to a POI we already occupy auto-undocks us every pass (the assist thrash),
	// so the dock never sticks.
	if st == nil || st.CurrentPOI != home {
		if nerr := d.ensureHomeNav(ctx, system, home); nerr != nil {
			fmt.Fprintf(d.Out, "ensure_home: navigate to %s/%s: %v\n", system, home, nerr) //nolint:errcheck
			return nil
		}
	}
	if derr := d.Client.Dock(ctx); derr != nil && !strings.Contains(derr.Error(), "Already docked") {
		fmt.Fprintf(d.Out, "ensure_home: dock %s: %v\n", home, derr) //nolint:errcheck
	}
	return nil
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestEnsureHome -v`
Expected: PASS (all six).

- [ ] **Step 8: Build, full test, lint**

Run: `go build ./... && go test ./pkg/worker/ && golangci-lint run ./pkg/worker/...`
Expected: build ok; tests pass; no new lint findings.

- [ ] **Step 9: Commit**

```bash
git add pkg/worker/dispatch.go pkg/worker/dispatch_test.go
git commit -m "feat(worker): ensure_home command returns a resident to its home station"
```

---

### Task 2: Resident idle script + role wiring

**Files:**
- Create: `data/scripts/resident_market.smolt`
- Modify: `data/overmind/roles.yaml` (repoint `resident`, `resident_gas`, `resident_ice` `idle:` to `resident_market`)
- Modify: `pkg/worker/roles_test.go` (update `TestLoadRolesParsesResident` to expect `resident_market`)

**Interfaces:**
- Consumes: the `ensure_home` command from Task 1; the existing `dock` / `refuel` commands.
- Produces: a `resident_market` idle script; `roles["resident"].Idle == "resident_market"`.

- [ ] **Step 1: Create the resident idle script**

Create `data/scripts/resident_market.smolt`:

```
# Resident market idle: return-home guard + free market capture. NO mining, so
# NO fuel/credit need. ensure_home nudges a drifted resident back to its
# configured home station (data/overmind/*-fleet.yaml `station:`); it no-ops
# once docked at home. Then the same park-and-capture as idle_market: dock
# re-docks a bot left undocked at a station POI (free); refuel tops the tank if
# the bot has credits (no-op when broke). The scheduled kb_update/view_market/
# update_market do the actual market capture.
ensure_home
dock
refuel
```

- [ ] **Step 2: Repoint the resident roles**

In `data/overmind/roles.yaml`, change the `idle:` for the three resident roles from `idle_market` to `resident_market`. For `resident`:

```yaml
  resident:
    schedule:
      - { every: hourly, command: "kb_update" }
      - { every: hourly, command: "view_market" }
      - { every: hourly, command: "update_market" }
      - { every: hourly, command: "facilities" }
    idle: resident_market
    idle_params:
      N: "20"
```

For `resident_gas`:

```yaml
    idle: resident_market  # PAUSED 2026-06-30 (was idle_gas); see resident note
```

For `resident_ice`:

```yaml
    idle: resident_market  # PAUSED 2026-06-30 (was idle_ice); see resident note
```

Leave the `craftsman` role on `idle_market` — craftsmen move between stations for craft nodes and must NOT be pulled to a fixed home.

- [ ] **Step 3: Update the role assertion test**

In `pkg/worker/roles_test.go`, `TestLoadRolesParsesResident`, change the idle assertion:

```go
	// resident switched idle_mine→idle_market (570d148), then idle_market→
	// resident_market (home-return guard added; see resident_home spec).
	if r.Idle != "resident_market" {
		t.Fatalf("idle=%q", r.Idle)
	}
```

- [ ] **Step 4: Run the role/dispatch coverage tests**

Run: `go test ./pkg/worker/ -run 'TestLoadRolesParsesResident|TestSeededCommandsAreDispatchable' -v`
Expected: PASS. `TestSeededCommandsAreDispatchable` reads `resident_market.smolt`, sees `ensure_home`, and finds it in `supported` (Task 1). `TestLoadRolesParsesResident` sees `resident_market`.

- [ ] **Step 5: Full test + lint**

Run: `go build ./... && go test ./... && golangci-lint run ./...`
Expected: build ok; all tests pass; no new lint findings.

- [ ] **Step 6: Commit**

```bash
git add data/scripts/resident_market.smolt data/overmind/roles.yaml pkg/worker/roles_test.go
git commit -m "feat(worker): resident roles run ensure_home before market capture"
```

---

### Task 3: Populate marketbot home stations + redeploy (operator-gated)

**Files:**
- Modify: `data/overmind/mb-fleet.yaml` (fill each worker's `station:` with its home POI)

**Interfaces:**
- Consumes: the overmind's existing forwarding of the fleet-YAML `station:` field to each worker as `--station`, surfaced as `WorkerDispatch.Station` (Task 1's input).
- Produces: a fleet roster where every resident has a valid home POI.

This task is config + a live redeploy. It has no unit test; it is verified by observing the fleet return home. Do the reachability check (Step 2) before deploying.

- [ ] **Step 1: Fill `station:` per worker in `mb-fleet.yaml`**

Set each worker's `station:` to its home station POI id. All values below are verified against `data/spacemolt-knowledge.db` `pois` (type='station'). 31 workers get their namesake station; the four exceptions are called out:

| agent_id | station: |
|---|---|
| marketbot_001 | `98eba8b1a7ad0520d6a7c8ea44b2d6aa` (Hex Star, Dheneb — player station) |
| marketbot_alpha_centauri | `alpha_centauri_colonial_station` |
| marketbot_blood_forge | `blood_forge_smelting_works` |
| marketbot_cargo_lanes | `cargo_lanes_freight_depot` |
| marketbot_deep_range | `deep_range_outpost` |
| marketbot_factory_belt | `factory_belt_manufacturing_hub` |
| marketbot_first_step | `first_step_memorial_station` |
| marketbot_frontier | `expedition_launch` *(Frontier; avoids assist-frontier's `mobile_capital`)* |
| marketbot_gold_run | `gold_run_extraction_hub` |
| marketbot_haven | `grand_exchange` |
| marketbot_iron_reach | `iron_reach_mining_colony` |
| marketbot_krynn | `war_citadel` |
| marketbot_last_light | `ramens_rest` |
| marketbot_market_prime | `market_prime_exchange` |
| marketbot_nexus_prime | `the_core` |
| marketbot_node_alpha | `node_alpha_processing_station` |
| marketbot_node_beta | `node_beta_industrial_station` |
| marketbot_node_gamma | `node_gamma_relay_station` |
| marketbot_nova_terra | `nova_terra_central` |
| marketbot_procyon | `procyon_colonial_station` |
| marketbot_ramens_rest | `scout_docks` *(reassigned; 2nd Frontier station, Max-Sec)* |
| marketbot_sirius | `sirius_observatory_station` |
| marketbot_sol | `sol_central` |
| marketbot_starfall | `starfall_salvage_station` |
| marketbot_synchrony | `synchrony_hub` |
| marketbot_the_anvil | `the_anvil_arsenal` |
| marketbot_the_crucible | `the_crucible_garrison` |
| marketbot_the_experiment | `the_experiment_research_station` |
| marketbot_the_levy | `the_levy_customs_station` |
| marketbot_the_rampart | `the_rampart_checkpoint` |
| marketbot_the_telescope | `ironhearth_station` *(reassigned; Ironhearth, Low-Sec — no station in The Telescope)* |
| marketbot_traders_rest | `traders_rest_resort_station` |
| marketbot_treasure_cache | `treasure_cache_trading_post` |
| marketbot_unknown_edge | `unknown_edge_waystation` |
| marketbot_void_gate | `void_gate_outpost` |

Example line (keep the existing role/comment; only fill the quotes):

```yaml
  - { agent_id: marketbot_market_prime, role: resident, station: "market_prime_exchange" }
```

- [ ] **Step 2: Verify the two reassignment targets are reachable**

For `ironhearth_station` (Ironhearth) and `scout_docks` (Frontier), confirm a route exists from the fleet's operating area before deploying. With one marketbot connected via `play_as`, run `plan_route ironhearth` and `plan_route frontier` (or `find_route`) and confirm a finite jump path. If either is unreachable, pick the next safe uncovered station from the 2026-07-14 survey (candidates: any empire station with `police_level > 0` that no other marketbot homes to) and update that row.

- [ ] **Step 3: Commit the config**

```bash
git add data/overmind/mb-fleet.yaml
git commit -m "config(mb-fleet): set resident home stations; reassign the_telescope + ramens_rest"
```

- [ ] **Step 4: Redeploy the marketbot fleet (operator-gated)**

The running `ensure_home` will move each displaced bot itself once the fleet restarts with the new config. Follow the standard drain/stop/staggered-relaunch (see the overmind launch/drain notes):

```bash
# graceful drain, then stop the mb overmind
kill -USR1 <mb-overmind-pid>   # optional drain
kill -TERM <mb-overmind-pid>
rm -f data/overmind/mb.sock
nohup bin/overmind --fleet data/overmind/mb-fleet.yaml --socket data/overmind/mb.sock \
  --status-file data/overmind/mb-status.json --history-file data/overmind/mb-history.jsonl \
  --stagger 10s >> data/overmind/mb-overmind.log 2>&1 &
```

- [ ] **Step 5: Verify the fleet converges home**

After the fleet has had time to route (residents idle every `SleepShort`; travel is multi-tick), confirm from `data/overmind/mb-status.json` that each bot's `poi`/`system` matches its configured `station:`, that no station is double-occupied, and that `market_prime_exchange` and `node_beta_industrial_station` begin receiving fresh `market_orders` rows in `data/market.db`. Reconcile against the design spec's coverage table.

---

## Notes

- Rebuilding `bin/worker` (and `bin/overmind` if touched) is required before the Task 3 redeploy so the new `ensure_home` command is present in the deployed binary: `go build -o bin/worker ./cmd/worker`.
- The design spec is `docs/superpowers/specs/2026-07-14-resident-home-station-enforcement-design.md`.
