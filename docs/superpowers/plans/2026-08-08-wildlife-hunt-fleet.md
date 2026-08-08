# Wildlife Hunt Fleet Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a `hunt` overmind fleet whose agents complete wildlife combat missions, so the fleet accumulates combat skill XP and combat mechanics become observable.

**Architecture:** Mirrors the existing `haul` and `missions` standing behaviours exactly — `roles.yaml` sets `idle: hunt`, which runs `data/scripts/hunt.smolt` (one line: `hunt`), which `pkg/worker/dispatch.go` routes to `worker.Hunt(ctx, HuntDeps{...})` in a new `pkg/worker/hunt.go`. Two client gaps are filled first (`get_nearby` cannot currently see wildlife, and no `hunt` command is sent), then the mission gate, then the executor, and the fleet wiring last so nothing goes live before it can fight.

**Tech Stack:** Go 1.24, `modernc.org/sqlite`, existing `pkg/game` WebSocket client, overmind supervisor, YAML fleet configs.

**Design:** `docs/superpowers/specs/2026-08-08-wildlife-hunt-fleet-design.md`

## Global Constraints

- Go 1.24+. Use range-over-int (`for i := range n`) and `b.Loop()` in benchmarks.
- All new code must pass `golangci-lint` with **no new findings**. Run it after each task.
- `go build ./...` and `go test ./...` must pass before any commit.
- **Sleeps must use the constants in `pkg/game/constants.go`.** Do not introduce a literal duration. If none fits, stop and ask.
- **Never `git add -A`.** A live fleet constantly rewrites `data/agents/*/schedule.json`. Stage each file explicitly by path.
- Compiled binaries go in `bin/`, never the repo root.
- The pre-commit race gate can time out under fleet load. If it does, the approved substitute is `--no-verify` plus: `go build ./...`, `golangci-lint`, the full package test suite, and a scoped `-race` run on the touched package. Say so in the commit message.
- **Gate 1 (difficulty cap) starts at `1`.** Gate 2 (wildlife only) starts **on**.
- **Lasers only.** Never fit an ammo-fed weapon.
- Combat missions are `type: "combat"`. The wildlife ones are `first_hunt_belt_grazers`, `grazer_cull`, `ice_field_thinning`, `nebula_drift_hunt`. `pirate_bounty` and `convoy_defense` are combat but **not** wildlife.

---

## File Structure

| File | Responsibility |
|---|---|
| `pkg/game/serverapi/types.go` | `NearbyCreature` wire type (modify) |
| `pkg/game/serverapi/responses.go` | `GetNearbyResponse.Creatures` (modify) |
| `pkg/game/interface.go` | `Hunt` on `GameClient` (modify) |
| `pkg/game/client_commands.go` | `Hunt` on the WebSocket client (modify) |
| `pkg/game/mcp_game_client_commands.go` | `Hunt` on the MCP client (modify) |
| `pkg/worker/hunt_gate.go` | **new** — which combat missions are admissible |
| `pkg/worker/hunt.go` | **new** — the hunt executor |
| `pkg/worker/dispatch.go` | `case "hunt"` (modify) |
| `data/scripts/hunt.smolt` | **new** — one line: `hunt` |
| `data/overmind/roles.yaml` | `hunt` role (modify) |
| `data/overmind/hunt-fleet.yaml` | **new** — roster |

---

### Task 1: Decode wildlife from `get_nearby`

The client cannot see wildlife at all today. `GetNearbyResponse` models `Nearby`, `Pirates` and `EmpireNPCs` only, so `hunt` has no target id to send. Nothing else can be built until this lands.

**Files:**
- Modify: `pkg/game/serverapi/types.go` (add `NearbyCreature` near `NearbyPirate`, currently at :1042)
- Modify: `pkg/game/serverapi/responses.go:161-171` (`GetNearbyResponse`)
- Test: `pkg/game/serverapi/nearby_creature_test.go` (new)

**Interfaces:**
- Produces: `serverapi.NearbyCreature{CreatureID, Species, Name, Hull, MaxHull, Shield, MaxShield, IsAggressive, Status string/int}` and `GetNearbyResponse.Creatures []NearbyCreature`. Task 4 reads both.

- [ ] **Step 1: Write the failing test**

Create `pkg/game/serverapi/nearby_creature_test.go`:

```go
package serverapi

import (
	"encoding/json"
	"testing"
)

// A get_nearby body carrying a creatures list must decode into Creatures.
// Wildlife is invisible to the client without this, so hunting is impossible.
func TestGetNearbyDecodesCreatures(t *testing.T) {
	const body = `{
	  "action": "get_nearby",
	  "nearby": [],
	  "count": 0,
	  "poi_id": "sol_asteroid_belt",
	  "creatures": [
	    {"creature_id": "c1", "species": "belt_grazer", "name": "Belt-Grazer",
	     "hull": 40, "max_hull": 40, "is_aggressive": false, "status": "grazing"},
	    {"creature_id": "c2", "species": "slag_tortoise", "name": "Slag-Tortoise",
	     "hull": 90, "max_hull": 120, "shield": 10, "max_shield": 20,
	     "is_aggressive": false, "status": "idle"}
	  ]
	}`

	var got GetNearbyResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Creatures) != 2 {
		t.Fatalf("Creatures = %d, want 2 (a nil slice means wildlife is invisible)", len(got.Creatures))
	}
	c := got.Creatures[0]
	if c.CreatureID != "c1" {
		t.Errorf("CreatureID = %q, want c1 — this is the id hunt takes", c.CreatureID)
	}
	if c.Species != "belt_grazer" {
		t.Errorf("Species = %q, want belt_grazer", c.Species)
	}
	if c.Hull != 40 || c.MaxHull != 40 {
		t.Errorf("hull = %d/%d, want 40/40", c.Hull, c.MaxHull)
	}
	if c.IsAggressive {
		t.Error("belt-grazers are passive; IsAggressive must decode false")
	}
	if got.Creatures[1].MaxShield != 20 {
		t.Errorf("MaxShield = %d, want 20", got.Creatures[1].MaxShield)
	}
}

// An older body with no creatures key must still decode, leaving Creatures nil.
func TestGetNearbyWithoutCreatures(t *testing.T) {
	var got GetNearbyResponse
	if err := json.Unmarshal([]byte(`{"nearby": [], "count": 0}`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Creatures != nil {
		t.Errorf("Creatures = %v, want nil when the key is absent", got.Creatures)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./pkg/game/serverapi/ -run 'GetNearby.*Creatures|GetNearbyWithout' -v`
Expected: FAIL to compile — `got.Creatures undefined`.

- [ ] **Step 3: Add the type**

In `pkg/game/serverapi/types.go`, immediately after the `NearbyPirate` struct (ends :1052):

```go
// NearbyCreature is one wildlife creature from get_nearby's "creatures" list.
// CreatureID is what the hunt command takes as its target.
//
// Wildlife never dogpile: engaging one creature does not pull in the rest of
// the herd, which is what makes difficulty-1 hunting safe.
type NearbyCreature struct {
	CreatureID   string `json:"creature_id"`
	Species      string `json:"species,omitempty"`
	Name         string `json:"name,omitempty"`
	Hull         int    `json:"hull"`
	MaxHull      int    `json:"max_hull"`
	Shield       int    `json:"shield,omitempty"`
	MaxShield    int    `json:"max_shield,omitempty"`
	IsAggressive bool   `json:"is_aggressive,omitempty"`
	Status       string `json:"status,omitempty"`
}
```

- [ ] **Step 4: Add the field**

In `pkg/game/serverapi/responses.go`, in `GetNearbyResponse`, after the `Pirates` line:

```go
	Creatures        []NearbyCreature `json:"creatures,omitempty"`
```

- [ ] **Step 5: Run the tests**

Run: `go test ./pkg/game/serverapi/ -count=1`
Expected: PASS.

- [ ] **Step 6: Lint and commit**

```bash
golangci-lint run ./pkg/game/...
git add pkg/game/serverapi/types.go pkg/game/serverapi/responses.go pkg/game/serverapi/nearby_creature_test.go
git commit -m "feat(game): decode wildlife creatures from get_nearby

GetNearbyResponse modelled players, pirates and empire NPCs but not the
creatures list, so the client could not see wildlife at all and had no
creature id to pass to hunt."
```

---

### Task 2: `Hunt` on the game client

`HuntResponse` already exists in `serverapi` and in the API-monitor registry, but nothing sends the command. `attack` on a creature id is documented as equivalent; an explicit `Hunt` is clearer and the response type is already there.

**Files:**
- Modify: `pkg/game/interface.go` (near `Attack`, :34)
- Modify: `pkg/game/client_commands.go`
- Modify: `pkg/game/mcp_game_client_commands.go`
- Modify: `pkg/agent/runner_test.go`, `pkg/skills/client_dispatcher_test.go` (mocks)
- Test: `pkg/game/hunt_command_test.go` (new)

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `Hunt(ctx context.Context, creatureID string) error` on `game.GameClient`. Task 4 calls it.

⚠️ **Adding a method to `GameClient` breaks every mock.** `go build ./...` will NOT catch it — the mocks live in test files. Run `go test ./...` before believing this task is done.

- [ ] **Step 1: Write the failing test**

`pkg/game` has **no generic send-capturing harness** — command sends are
asserted through the WebSocket test server in
`pkg/game/client_integration_test.go`, which inspects `msg.Payload[...]` on the
server side. Read that file and follow its pattern.

`Hunt` is a three-line wrapper over `c.Send`, so if wiring the integration
server for it costs more than it proves, it is acceptable to skip the send test
and rely on Task 4's executor tests plus the compile-time interface check. State
which you chose in the commit message. **Do not invent a harness that does not
exist.**

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./pkg/game/ -run TestHuntSendsCreatureID -v`
Expected: FAIL — `c.Hunt undefined`.

- [ ] **Step 3: Add to the interface**

In `pkg/game/interface.go`, directly under `Attack`:

```go
	// Hunt engages a wildlife creature by its creature_id from get_nearby.
	// Equivalent to Attack on a creature id. Wildlife never dogpile.
	Hunt(ctx context.Context, creatureID string) error
```

- [ ] **Step 4: Implement on both clients**

`pkg/game/client_commands.go`:

```go
// Hunt engages the wildlife creature with the given creature_id (from
// get_nearby's creatures list). Reply: hunt.
func (c *Client) Hunt(ctx context.Context, creatureID string) error {
	return c.Send(ctx, protocol.Message{
		Type:    "hunt",
		Payload: map[string]any{"target_id": creatureID},
	})
}
```

`pkg/game/mcp_game_client_commands.go` — follow the surrounding MCP style exactly (look at the neighbouring `Attack`/`Cloak` implementations and mirror them; do not invent a new shape).

- [ ] **Step 5: Fix every mock**

Run `go test ./... 2>&1 | grep -i 'does not implement'` and add a `Hunt` stub to each mock it names. Minimum expected: `pkg/agent/runner_test.go`, `pkg/skills/client_dispatcher_test.go`.

```go
func (m *mockGameClient) Hunt(_ context.Context, _ string) error { return nil }
```

- [ ] **Step 6: Verify**

Run: `go build ./... && go test ./... -count=1`
Expected: both pass. A `does not implement` failure means a mock was missed.

- [ ] **Step 7: Lint and commit**

```bash
golangci-lint run ./pkg/game/... ./pkg/agent/... ./pkg/skills/...
git add pkg/game/interface.go pkg/game/client_commands.go pkg/game/mcp_game_client_commands.go pkg/game/hunt_command_test.go pkg/agent/runner_test.go pkg/skills/client_dispatcher_test.go
git commit -m "feat(game): add the hunt command

HuntResponse and its api-monitor registration already existed; nothing
sent the command."
```

---

### Task 3: The combat mission gate

Two gates, because one number cannot express the requirement: difficulty 2 holds both the safe repeatable wildlife culls and the two missions that shoot back.

**Files:**
- Create: `pkg/worker/hunt_gate.go`
- Test: `pkg/worker/hunt_gate_test.go`

**Interfaces:**
- Produces: `huntAdmissible(e serverapi.MissionBoardEntry, maxDifficulty int, wildlifeOnly bool) (ok bool, reason string)`, plus `huntDefaultMaxDifficulty = 1` and `huntWildlifeOnlyDefault = true`. Task 4 calls `huntAdmissible`.

- [ ] **Step 1: Write the failing test**

Create `pkg/worker/hunt_gate_test.go`:

```go
package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func combatEntry(id string, difficulty int) serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: id, Type: "combat", Title: id, Difficulty: difficulty,
		Objectives: []serverapi.MissionObjective{{Type: "kill_creature", Quantity: 3}},
	}
}

// The whole point of the gate: difficulty decides, never reward.
func TestHuntAdmissibleDifficultyCap(t *testing.T) {
	for _, tc := range []struct {
		id    string
		diff  int
		cap   int
		admit bool
	}{
		{"first_hunt_belt_grazers", 1, 1, true},
		{"grazer_cull", 2, 1, false},
		{"grazer_cull", 2, 2, true},
		{"ice_field_thinning", 2, 2, true},
		{"nebula_drift_hunt", 2, 2, true},
		{"starfall_prospector_defense", 4, 2, false},
		{"leviathan_bounty", 6, 2, false},
		{"smugglers_route", 7, 2, false},
	} {
		ok, reason := huntAdmissible(combatEntry(tc.id, tc.diff), tc.cap, true)
		if ok != tc.admit {
			t.Errorf("%s (diff %d, cap %d): admitted = %v, want %v (reason %q)",
				tc.id, tc.diff, tc.cap, ok, tc.admit, reason)
		}
		if !ok && reason == "" {
			t.Errorf("%s: a refusal must carry a reason", tc.id)
		}
	}
}

// leviathan_bounty is the mission a reward-maximising selector picks first: the
// best XP in the table, 8,000cr, AND repeatable, so it would be chosen forever.
// The Molt Leviathan hunts ships and fights to the death. It must stay refused
// however good its numbers look.
func TestHuntAdmissibleRefusesLeviathanOnAnyReward(t *testing.T) {
	e := combatEntry("leviathan_bounty", 6)
	e.Rewards = &serverapi.MissionRewards{Credits: 1_000_000}
	if ok, _ := huntAdmissible(e, 2, true); ok {
		t.Fatal("a difficulty-6 mission must be refused no matter how large the reward")
	}
	// And at every cap this iteration will ever use.
	for cap := range 3 {
		if ok, _ := huntAdmissible(e, cap, true); ok {
			t.Errorf("cap %d admitted the leviathan", cap)
		}
	}
}

// Gate 2 is what lets gate 1 rise to 2 without admitting the two combat
// missions that shoot back.
func TestHuntAdmissibleWildlifeOnly(t *testing.T) {
	for _, id := range []string{"pirate_bounty", "convoy_defense"} {
		if ok, reason := huntAdmissible(combatEntry(id, 2), 2, true); ok {
			t.Errorf("%s must be refused while wildlifeOnly is set", id)
		} else if reason == "" {
			t.Errorf("%s: refusal needs a reason", id)
		}
		if ok, _ := huntAdmissible(combatEntry(id, 2), 2, false); !ok {
			t.Errorf("%s must be admitted once wildlifeOnly is lifted", id)
		}
	}
}

// Non-combat types are none of this gate's business.
func TestHuntAdmissibleRejectsNonCombat(t *testing.T) {
	e := combatEntry("some_delivery", 1)
	e.Type = "delivery"
	if ok, _ := huntAdmissible(e, 1, true); ok {
		t.Error("a delivery mission must not be admitted by the hunt gate")
	}
}

// A board entry with no kill objective is not huntable even if it is combat.
func TestHuntAdmissibleNeedsAKillObjective(t *testing.T) {
	e := combatEntry("odd", 1)
	e.Objectives = []serverapi.MissionObjective{{Type: "dock_at_base"}}
	if ok, _ := huntAdmissible(e, 1, true); ok {
		t.Error("combat mission with no kill objective must be refused")
	}
}

// Defaults are the safe end: cap 1, wildlife only.
func TestHuntGateDefaults(t *testing.T) {
	if huntDefaultMaxDifficulty != 1 {
		t.Errorf("default cap = %d, want 1", huntDefaultMaxDifficulty)
	}
	if !huntWildlifeOnlyDefault {
		t.Error("wildlife-only must default on")
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./pkg/worker/ -run TestHunt -v`
Expected: FAIL to compile — `huntAdmissible undefined`.

- [ ] **Step 3: Implement the gate**

Create `pkg/worker/hunt_gate.go`:

```go
package worker

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

const (
	// huntDefaultMaxDifficulty is gate 1: the highest mission difficulty the
	// hunt fleet will accept. It starts at 1 — first_hunt_belt_grazers, passive
	// quarry — and is raised deliberately as weapons level climbs. It is NEVER
	// derived from a reward score.
	//
	// Reward-based selection is the failure mode this exists to prevent.
	// leviathan_bounty is difficulty 6, pays 8,000cr, carries the best XP in the
	// table, and is REPEATABLE — so a selector optimising reward would choose it
	// not once but forever, and the docs describe the Molt Leviathan as a
	// predator that hunts ships and fights to the death.
	huntDefaultMaxDifficulty = 1

	// huntWildlifeOnlyDefault is gate 2. Difficulty 2 holds both the safe
	// repeatable wildlife culls and pirate_bounty / convoy_defense, which shoot
	// back. A single numeric cap admits both or neither; this second gate is what
	// lets gate 1 rise to 2 for the culls alone.
	huntWildlifeOnlyDefault = true

	// missionTypeDelivery already exists at mission_select.go:175; these are new.
	missionTypeCombat     = "combat"
	objectiveKillCreature = "kill_creature"
)

// huntWildlifeMissions are the combat missions whose quarry is wildlife.
// Everything else of type combat fights back.
var huntWildlifeMissions = map[string]bool{
	"first_hunt_belt_grazers": true,
	"grazer_cull":             true,
	"ice_field_thinning":      true,
	"nebula_drift_hunt":       true,
}

// huntAdmissible reports whether the hunt fleet may accept this board entry.
// A non-empty reason explains every refusal, so a skipped mission is never
// silent.
func huntAdmissible(e serverapi.MissionBoardEntry, maxDifficulty int, wildlifeOnly bool) (bool, string) {
	if e.Type != missionTypeCombat {
		return false, fmt.Sprintf("not a combat mission (type %q)", e.Type)
	}
	if e.Difficulty > maxDifficulty {
		return false, fmt.Sprintf("difficulty %d over cap %d", e.Difficulty, maxDifficulty)
	}
	if wildlifeOnly && !huntWildlifeMissions[e.MissionID] {
		return false, fmt.Sprintf("%s is not a wildlife mission and wildlife-only is set", e.MissionID)
	}
	if huntKillQuantity(e) == 0 {
		return false, "no kill_creature objective"
	}

	return true, ""
}

// huntKillQuantity totals the creatures this mission asks to be killed.
// Objectives are summed rather than taking the first, so a multi-objective hunt
// reports the real target count.
func huntKillQuantity(e serverapi.MissionBoardEntry) int {
	total := 0
	for _, o := range e.Objectives {
		if o.Type != objectiveKillCreature {
			continue
		}
		q := o.Quantity
		if q <= 0 {
			q = 1 // an objective with no quantity still means one kill
		}
		total += q
	}

	return total
}
```

> Note `huntWildlifeMissions` is an allowlist keyed on mission id, not a
> heuristic. New wildlife missions must be added deliberately — the failure
> direction is refusing a safe mission, never admitting a dangerous one.

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/worker/ -run TestHunt -count=1 -v`
Expected: all PASS.

- [ ] **Step 5: Prove the difficulty check is load-bearing**

Temporarily delete the `e.Difficulty > maxDifficulty` branch, re-run, and confirm `TestHuntAdmissibleRefusesLeviathanOnAnyReward` FAILS. Restore it. Record the observed failure in the commit message.

- [ ] **Step 6: Lint and commit**

```bash
golangci-lint run ./pkg/worker/...
git add pkg/worker/hunt_gate.go pkg/worker/hunt_gate_test.go
git commit -m "feat(worker): difficulty-first gate for combat missions"
```

---

### Task 4: The hunt executor

The one genuinely new behaviour. Everything before this was plumbing.

**Files:**
- Create: `pkg/worker/hunt.go`
- Test: `pkg/worker/hunt_test.go`

**Interfaces:**
- Consumes: `serverapi.GetNearbyResponse.Creatures` (Task 1), `GameClient.Hunt` (Task 2), `huntAdmissible` / `huntKillQuantity` (Task 3).
- Produces: `Hunt(ctx context.Context, d HuntDeps) error` and `HuntDeps`. Task 6 calls it from dispatch.

```go
type HuntDeps struct {
	Client        game.GameClient
	KB            knowledge.Base
	Out           io.Writer
	AgentID       string
	NowFn         func() time.Time
	MaxDifficulty int  // 0 => huntDefaultMaxDifficulty
	WildlifeOnly  bool
	FleeAtHull    float64 // 0 => huntDefaultFleeHull
}
```

- [ ] **Step 1: Read the neighbours first**

Before writing anything, read `pkg/worker/haul.go` and the `Missions` entry point in `pkg/worker/mission.go`. Match their structure: a `*Deps` struct, a single exported entry point returning `error`, progress written to `d.Out`, and no sleeps outside `pkg/game/constants.go`.

- [ ] **Step 2: Write the failing tests**

Create `pkg/worker/hunt_test.go`. The package's fake is **`fakeClient` in
`pkg/worker/dispatch_test.go:19`** — it embeds `game.GameClient` (so
unimplemented methods panic if called), records `calls []string`, and serves
`GetRawJSON` from a `raw map[string][]byte`. **Extend that fake; do not write a
new one.**

The raw store keys the executor reads are **`"nearby"`** (get_nearby, which
carries the creatures list) and **`"battle_status"`**. Both are classified by
payload shape rather than by an `action` switch, so they are reachable — unlike
the `owned_ships` key that was silently dead.

```go
package worker

import (
	"context"
	"io"
	"strings"
	"testing"
)

// A gated pass must not issue a hunt at all.
func TestHuntRefusesOverCapMission(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "leviathan_bounty", difficulty: 6, quantity: 1}},
		creatures: []string{"c1"},
	})
	var log strings.Builder
	if err := Hunt(context.Background(), HuntDeps{
		Client: c, Out: &log, AgentID: "pirate-6", MaxDifficulty: 1, WildlifeOnly: true,
	}); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 0 {
		t.Errorf("hunt issued %d times against an over-cap mission, want 0", c.huntCalls)
	}
	if !strings.Contains(log.String(), "difficulty 6 over cap 1") {
		t.Errorf("refusal must be logged with its reason, got:\n%s", log.String())
	}
}

// The counted objective drives the loop: 3 grazers means 3 hunts.
func TestHuntKillsToObjectiveCount(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures: []string{"c1", "c2", "c3", "c4"},
	})
	if err := Hunt(context.Background(), HuntDeps{
		Client: c, Out: io.Discard, AgentID: "pirate-6", MaxDifficulty: 1, WildlifeOnly: true,
	}); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 3 {
		t.Errorf("hunt calls = %d, want 3 (the objective quantity)", c.huntCalls)
	}
}

// No creatures at this POI is a normal outcome, not an error: the herd moved or
// the belt is mined thin. It must not fail the pass.
func TestHuntNoCreaturesIsNotAnError(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures: nil,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), HuntDeps{
		Client: c, Out: &log, AgentID: "pirate-6", MaxDifficulty: 1, WildlifeOnly: true,
	}); err != nil {
		t.Fatalf("an empty belt must not fail the pass: %v", err)
	}
	if c.huntCalls != 0 {
		t.Error("no creatures means no hunts")
	}
}

// The flee threshold aborts the hunt rather than trading the hull for a kill.
func TestHuntFleesBelowHullThreshold(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:      []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures:  []string{"c1", "c2", "c3"},
		hullAfter1: 0.1, // 10% hull after the first fight
	})
	var log strings.Builder
	if err := Hunt(context.Background(), HuntDeps{
		Client: c, Out: &log, AgentID: "pirate-6",
		MaxDifficulty: 1, WildlifeOnly: true, FleeAtHull: 0.3,
	}); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 1 {
		t.Errorf("hunt calls = %d, want 1 — the pass must stop at the flee threshold", c.huntCalls)
	}
	if !c.fled {
		t.Error("dropping below the flee threshold must issue a flee stance")
	}
}
```

> **Implementer:** `newHuntFake`, `huntFakeOpts` and `boardMission` are yours to
> write in `hunt_test.go`, wrapping `fakeClient`. They must record `huntCalls`
> and `fled`, serve the board, serve the creatures list through
> `raw["nearby"]`, and drop reported hull to `hullAfter1` after the first fight.
> Add the `Hunt` and `Battle` recording to `fakeClient` itself if that is
> cleaner — it is the shared fake and other tests benefit.

- [ ] **Step 3: Run to confirm they fail**

Run: `go test ./pkg/worker/ -run TestHunt -v`
Expected: FAIL — `Hunt undefined`.

- [ ] **Step 4: Implement the executor**

Create `pkg/worker/hunt.go` implementing this sequence:

1. `GetStatus`; if not at a POI that can hold wildlife, reposition — reuse the mission path's repositioning rather than writing new travel code.
2. Read the mission board. For each entry, `huntAdmissible(e, maxDifficulty, wildlifeOnly)`; log every refusal with its reason. Accept the first admissible one.
3. `huntKillQuantity(e)` is the target count.
4. `GetNearby`; if `Creatures` is empty, log and return nil — an empty belt is normal.
5. Loop until the count is met: pick a creature (prefer `IsAggressive == false`), `Hunt(creatureID)`, then poll `GetBattleStatus` until the fight resolves. **`get_battle_status` is a free query with no tick cost**, so polling it is cheap.
6. Before each new engagement, check hull fraction against `FleeAtHull`; if below, issue `Battle(ctx, "stance", {"stance": "flee"})`, log, and return nil.
7. When the count is met, complete the mission through the existing completion path.
8. Loot the carcass wreck if the salvage helpers make it a one-liner; otherwise leave a TODO and do not block on it.

Constants:

```go
const (
	// huntDefaultFleeHull is the hull fraction below which the pass gives up.
	// A cheap loss is not a free one: the Prospector is legacy and its
	// replacement, while armed, is slightly worse.
	huntDefaultFleeHull = 0.35
	// huntMaxEngagements caps one pass so a pathological board or an
	// unkillable creature cannot loop forever.
	huntMaxEngagements = 12
)
```

- [ ] **Step 5: Run the tests**

Run: `go test ./pkg/worker/ -run TestHunt -count=1 -v`
Expected: all PASS.

- [ ] **Step 6: Lint, race, commit**

```bash
golangci-lint run ./pkg/worker/...
go test ./pkg/worker/ -run TestHunt -race -count=1
git add pkg/worker/hunt.go pkg/worker/hunt_test.go
git commit -m "feat(worker): wildlife hunt executor"
```

---

### Task 5: XP-based exemption to the payout gate

`first_hunt_belt_grazers` pays 1,000cr. Mission payouts run at roughly 37% of face because the empire treasury is broke, so the realized-ratio gate would refuse the very mission this fleet exists to run. **XP is paid in full** — that was the finding that cracked the payout mystery — so combat missions must be judged on XP.

**Files:**
- Modify: `pkg/worker/mission_select.go` (the `payoutRatio` handling in `buildMissionCandidate`, :460)
- Test: `pkg/worker/hunt_gate_test.go` (extend)

**Interfaces:**
- Consumes: `huntAdmissible` (Task 3).

- [ ] **Step 1: Read the existing exemption**

Read how smuggling-below-L3 is exempted (`pkg/worker/mission_standing.go:46-60`, `smugglingBuyingXP`). Mirror its shape — do not invent a second mechanism.

- [ ] **Step 2: Write the failing test**

```go
// A First Hunt pays 1,000cr, which the realized-ratio gate would refuse while
// the treasury is broke. XP is paid in FULL, so combat missions under the
// difficulty cap are judged on XP instead.
func TestCombatMissionExemptFromPayoutRatio(t *testing.T) {
	e := combatEntry("first_hunt_belt_grazers", 1)
	e.Rewards = &serverapi.MissionRewards{Credits: 1000}
	if !huntExemptFromPayoutRatio(e) {
		t.Error("a capped wildlife combat mission must be exempt from the realized-ratio gate")
	}
	if huntExemptFromPayoutRatio(combatEntry("leviathan_bounty", 6)) {
		t.Error("the exemption must not extend past the difficulty cap")
	}
}
```

- [ ] **Step 3: Implement**

```go
// huntExemptFromPayoutRatio reports whether a board entry should skip the
// realized-payout gate. Mission credits currently land at ~37% of face because
// the empire treasury is broke, but skill XP is paid in FULL — and XP is the
// entire point of the hunt fleet. Without this the gate refuses the curriculum
// it was never meant to judge.
//
// Scoped to what the hunt gate already admits, so it can never widen the fleet's
// reach: an over-cap or non-wildlife mission is not exempt.
func huntExemptFromPayoutRatio(e serverapi.MissionBoardEntry) bool {
	ok, _ := huntAdmissible(e, huntDefaultMaxDifficulty, huntWildlifeOnlyDefault)

	return ok
}
```

- [ ] **Step 4: Wire it into the gate, run, lint, commit**

Run: `go test ./pkg/worker/ -count=1`, then `golangci-lint run ./pkg/worker/...`.

```bash
git add pkg/worker/hunt_gate.go pkg/worker/hunt_gate_test.go pkg/worker/mission_select.go
git commit -m "feat(worker): judge combat missions on XP, not realized credits"
```

---

### Task 6: Fleet wiring — LAST

Nothing above changes behaviour for any running worker. This task does, so it goes last: a fleet that can accept hunts before it can perform them would strand agents on missions they cannot complete, exactly as the TRADING missions stranded fighter-4.

**Files:**
- Modify: `pkg/worker/dispatch.go` (near `case "haul"`, :247)
- Create: `data/scripts/hunt.smolt`
- Modify: `data/overmind/roles.yaml`
- Create: `data/overmind/hunt-fleet.yaml`

- [ ] **Step 1: Dispatch case**

In `pkg/worker/dispatch.go`, alongside `case "haul"`:

```go
	case "hunt":
		return Hunt(ctx, HuntDeps{
			Client: d.Client, KB: d.KB, Out: d.Out, AgentID: d.AgentID,
			NowFn:         time.Now,
			MaxDifficulty: huntDefaultMaxDifficulty,
			WildlifeOnly:  huntWildlifeOnlyDefault,
		})
```

Also add `"hunt": true` to the action-command map at `dispatch.go:178` — hunting costs ticks.

- [ ] **Step 2: The idle script**

`data/scripts/hunt.smolt`, one line:

```
hunt
```

- [ ] **Step 3: The role**

In `data/overmind/roles.yaml`, alongside the others (all ten roles already carry the three captures):

```yaml
  hunt:
    schedule:
      - { every: hourly, command: "capture_profile" }
      - { every: daily, command: "capture_storage" }
      - { every: daily, command: "capture_faction" }
    idle: hunt
```

- [ ] **Step 4: The fleet**

`data/overmind/hunt-fleet.yaml`, starting with **two** agents per the rollout:

```yaml
workers:
  - agent_id: pirate-6
    role: hunt
    station: frontier_station
  - agent_id: pirate-9
    role: hunt
    station: frontier_station
```

> pirate-6's Prospector is already armed. pirate-9 holds the most credits
> (24,657) so it can buy its own `pulse_laser_i`. Copy the exact key names from
> an existing fleet yaml — do not guess the schema.

- [ ] **Step 5: Verify inert, then commit**

```bash
go build ./... && go test ./... -count=1 && golangci-lint run ./...
git add pkg/worker/dispatch.go data/scripts/hunt.smolt data/overmind/roles.yaml data/overmind/hunt-fleet.yaml
git commit -m "feat(overmind): wire the hunt fleet"
```

**Do not launch the fleet as part of this task.** Launching is an operator decision. The manual pre-flight, in order:

1. Refuel pirate-6 — its active Drillship is at **0/130** and cannot move.
2. Buy `pulse_laser_i` (2,500cr) for pirate-9 and fit it to its Prospector.
3. Switch both agents into their Prospectors; leave the Drillships, Deeprock Harvester and Excavator docked at Frontier Station.
4. Canary `first_hunt_belt_grazers` by hand through `play_as` before the fleet runs unattended.
5. Launch with `--assets-db-path data/assets.db` and `--stagger 10s`.

---

## Deferred

Recorded so they are not rediscovered as novel:

- **`cracking_the_shell`** — the chain's second mission has never been seen on a board and is absent from the knowledge base. Handle it when it unlocks (operator's decision); if it exceeds gate 1, chain continuations will need an exemption.
- **Graduating to bigger hulls.** Drillship has 1 weapon / 2 defense / 3 utility slots; Deeprock Harvester has 2 / 3 / 6. Both out-slot the Prospector's 1 / 1 / 2, so they are the upgrade path once weapons skill justifies risking a legacy hull.
- **Module detail in the ledger.** `agent_hulls.modules` is an opaque count that under-reports; it cannot tell you whether a hull is armed. Real loadout capture is a prerequisite for any targeting logic.
- **`get_battle_log` mining** — per-tick hit/crit rolls, resist percentages and damage breakdown, for any battle including other players'. The richest combat signal available, and untouched here.
- **Lifting gate 2** for `pirate_bounty` / `convoy_defense`, and raising gate 1 past 2.
