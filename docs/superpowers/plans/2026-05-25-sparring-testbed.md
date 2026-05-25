# Sparring Testbed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a controlled PvP sparring harness (`cmd/tools/spar` + `pkg/spar`) that logs in two or more of our own agents, equips and moves them to a non-empire arena, and runs scripted (or human-partnered) battles so the combat mechanics become observable and testable.

**Architecture:** A standalone orchestrator binary drives a `pkg/spar` package whose pure units — `policy` (battle decision presets), `arena` (system selection), `combatant` setup helpers, `match` end-detection — are unit-testable against fakes. A small foundation fix in `pkg/game` parses `get_battle_status` into `state.BattleState` (currently declared but never populated), giving the policy loop structured battle state and laying groundwork the deferred smart-handler reuses.

**Tech Stack:** Go 1.24+, existing `pkg/game` client (`InitializeAgent`, `NavigateToSystem`, `Battle`, `Attack`, `GetBattleStatus`, `Buy`, `InstallMod`), stdlib `sync.WaitGroup` for concurrent setup (no new deps), standard `flag`/`testing`.

**Spec:** `docs/superpowers/specs/2026-05-25-sparring-testbed-design.md`

---

## File Structure

| File | Responsibility |
|------|----------------|
| `pkg/game/types.go` (modify) | Add `HullPct`/`ShieldPct`/`TargetID`/`AutoPilot` to `BattleParticipant`. |
| `pkg/game/client.go` (modify) | `parseBattleStatusData` + wire into the `TypeOK` case. |
| `pkg/game/client_test.go` (modify) | Test that `get_battle_status` populates `state.BattleState`. |
| `pkg/spar/policy.go` (create) | `View`, `Action`, `Policy` interface, `BuildView`, zone helpers, presets, `PolicyByName`. |
| `pkg/spar/policy_test.go` (create) | Table tests for each preset + `BuildView`. |
| `pkg/spar/arena.go` (create) | `IsNonEmpireArena`, `hasSafeAdjacency`, `SelectArena` (pure), `ValidateArenaSystem`. |
| `pkg/spar/arena_test.go` (create) | Table tests for predicate + selection. |
| `pkg/spar/combatant.go` (create) | `Combatant`, `neededModules`/`isWeaponModule` (pure), `Setup` (glue). |
| `pkg/spar/combatant_test.go` (create) | Table tests for `neededModules`. |
| `pkg/spar/match.go` (create) | `battleClient` interface, `battleOver`, `runPolicyLoop`, `Match`, `Run`. |
| `pkg/spar/match_test.go` (create) | Fake `battleClient`: loop dispatches actions, stops on side eliminated. |
| `pkg/spar/telemetry.go` (create) | `formatTickRow`, `formatSummary`. |
| `pkg/spar/telemetry_test.go` (create) | Tests for the formatters. |
| `cmd/tools/spar/main.go` (create) | Flags, login, build & run `Match`. |
| `cmd/tools/spar/README.md` (create) | Usage docs. |

---

## Task 1: Foundation — extend `BattleParticipant`

**Files:**
- Modify: `pkg/game/types.go` (the `BattleParticipant` struct at ~line 293)

- [ ] **Step 1: Add tactical/percentage fields to `BattleParticipant`**

Find the struct:

```go
// BattleParticipant represents a participant in a tactical battle.
type BattleParticipant struct {
	PlayerID  string `json:"player_id"`
	Username  string `json:"username"`
	ShipClass string `json:"ship_class"`
	SideID    string `json:"side_id"`
	Zone      string `json:"zone"`
	Stance    string `json:"stance"`
	Hull      float64 `json:"hull"`
	MaxHull   float64 `json:"max_hull"`
	Shield    float64 `json:"shield"`
	MaxShield float64 `json:"max_shield"`
}
```

Replace it with (adds four fields; keeps existing ones for compatibility):

```go
// BattleParticipant represents a participant in a tactical battle.
type BattleParticipant struct {
	PlayerID  string `json:"player_id"`
	Username  string `json:"username"`
	ShipClass string `json:"ship_class"`
	SideID    string `json:"side_id"`
	Zone      string `json:"zone"`
	Stance    string `json:"stance"`
	Hull      float64 `json:"hull"`
	MaxHull   float64 `json:"max_hull"`
	Shield    float64 `json:"shield"`
	MaxShield float64 `json:"max_shield"`
	// Percentage + tactical fields as reported by get_battle_status / battle_update.
	HullPct   int    `json:"hull_pct"`
	ShieldPct int    `json:"shield_pct"`
	TargetID  string `json:"target_id,omitempty"`
	AutoPilot bool   `json:"auto_pilot,omitempty"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/game/...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add pkg/game/types.go
git commit -m "feat(game): add pct/tactical fields to BattleParticipant"
```

---

## Task 2: Foundation — parse `get_battle_status` into `state.BattleState`

**Files:**
- Modify: `pkg/game/client.go` (add `parseBattleStatusData`; wire into `TypeOK` case at ~line 2152)
- Test: `pkg/game/client_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/game/client_test.go`:

```go
func TestGetBattleStatus_PopulatesBattleState(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.state.Player.ID = "me-123"

	client.recvFrame(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action":         "get_battle_status",
			"battle_id":      "b1",
			"system_id":      "ross_128",
			"is_participant": true,
			"participants": []any{
				map[string]any{"player_id": "me-123", "username": "Me", "ship_class": "axiom", "side_id": "1", "zone": "mid", "stance": "fire", "hull_pct": float64(80), "shield_pct": float64(50), "target_id": "p2"},
				map[string]any{"player_id": "p2", "username": "Foe", "ship_class": "axiom", "side_id": "2", "zone": "mid", "stance": "brace", "hull_pct": float64(100), "shield_pct": float64(100)},
			},
		},
	})

	st := client.GetState()
	if st.BattleState == nil {
		t.Fatal("BattleState nil; want populated")
	}
	if st.BattleState.BattleID != "b1" || len(st.BattleState.Participants) != 2 {
		t.Fatalf("unexpected BattleState: %+v", st.BattleState)
	}
	if st.BattleState.Participants[0].HullPct != 80 {
		t.Errorf("want HullPct 80, got %d", st.BattleState.Participants[0].HullPct)
	}
	if st.BattleState.Participants[0].TargetID != "p2" {
		t.Errorf("want TargetID p2, got %q", st.BattleState.Participants[0].TargetID)
	}
	if !st.InBattle {
		t.Error("want InBattle true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestGetBattleStatus_PopulatesBattleState -v`
Expected: FAIL — `BattleState nil; want populated` (parse not wired yet).

- [ ] **Step 3: Add the parse function**

Add this method near the other `parse*` helpers in `pkg/game/client.go` (e.g. just after `parseChatHistoryData`; `json` and `serverapi` are already imported):

```go
// parseBattleStatusData populates state.BattleState from a get_battle_status
// response. The server reports hull/shield as percentages plus zone/stance and
// per-participant target. This is the structured read the spar harness and the
// future smart battle handler consume; the raw payload also goes to the monitor
// store, but that is unstructured.
func (c *Client) parseBattleStatusData(payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	var resp serverapi.GetBattleStatusResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}

	parts := make([]BattleParticipant, 0, len(resp.Participants))
	for _, p := range resp.Participants {
		parts = append(parts, BattleParticipant{
			PlayerID:  p.PlayerID,
			Username:  p.Username,
			ShipClass: p.ShipClass,
			SideID:    p.SideID,
			Zone:      p.Zone,
			Stance:    p.Stance,
			TargetID:  p.TargetID,
			HullPct:   p.HullPct,
			ShieldPct: p.ShieldPct,
			AutoPilot: p.AutoPilot,
		})
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if resp.BattleID == "" && len(parts) == 0 {
		return
	}
	c.state.BattleState = &BattleState{
		BattleID:      resp.BattleID,
		SystemID:      resp.SystemID,
		IsParticipant: resp.IsParticipant,
		Participants:  parts,
		TickDuration:  resp.TickDuration,
	}
	if resp.IsParticipant {
		c.state.InBattle = true
	}
}
```

- [ ] **Step 4: Wire it into the `TypeOK` case**

In the `case protocol.TypeOK:` block, immediately after the `get_chat_history` parse block (`pkg/game/client.go` ~line 2154), add:

```go
		// get_battle_status returns type "ok" with action "get_battle_status"
		// and participants/sides/battle_id in payload.
		if action, ok := resp.Payload["action"].(string); ok && action == "get_battle_status" {
			c.parseBattleStatusData(resp.Payload)
		}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestGetBattleStatus_PopulatesBattleState -v`
Expected: PASS.

- [ ] **Step 6: Run the full game package tests (no regressions)**

Run: `go test ./pkg/game/`
Expected: PASS (existing `TestBattleEventsHandled` etc. still pass).

- [ ] **Step 7: Commit**

```bash
git add pkg/game/client.go pkg/game/client_test.go
git commit -m "feat(game): parse get_battle_status into state.BattleState"
```

---

## Task 3: `pkg/spar` policy presets

**Files:**
- Create: `pkg/spar/policy.go`
- Test: `pkg/spar/policy_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/spar/policy_test.go`:

```go
package spar

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func bs(parts ...game.BattleParticipant) *game.BattleState {
	return &game.BattleState{BattleID: "b1", IsParticipant: true, Participants: parts}
}

func TestBuildView_SplitsAlliesAndEnemies(t *testing.T) {
	state := bs(
		game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", HullPct: 90},
		game.BattleParticipant{PlayerID: "ally", SideID: "1"},
		game.BattleParticipant{PlayerID: "foe", SideID: "2"},
	)
	v, ok := BuildView(state, "me")
	if !ok {
		t.Fatal("BuildView returned ok=false; want self found")
	}
	if v.Self.PlayerID != "me" || len(v.Allies) != 1 || len(v.Enemies) != 1 {
		t.Fatalf("unexpected view: self=%s allies=%d enemies=%d", v.Self.PlayerID, len(v.Allies), len(v.Enemies))
	}
}

func TestPolicies_Decide(t *testing.T) {
	enemy := game.BattleParticipant{PlayerID: "foe", SideID: "2", HullPct: 100}

	tests := []struct {
		name       string
		policy     Policy
		self       game.BattleParticipant
		wantKind   string
		wantAction string
		wantStance string // "" if not checked
	}{
		{"aggressor advances from outer", NewAggressor(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "outer", HullPct: 100}, "battle", "advance", ""},
		{"aggressor targets when engaged untargeted", NewAggressor(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "engaged", HullPct: 100}, "battle", "target", ""},
		{"aggressor fires when engaged+targeted, not firing", NewAggressor(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "engaged", TargetID: "foe", Stance: "brace", HullPct: 100}, "battle", "stance", "fire"},
		{"aggressor noop when engaged+targeted+firing", NewAggressor(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "engaged", TargetID: "foe", Stance: "fire", HullPct: 100}, "noop", "", ""},
		{"skirmisher advances toward mid from outer", NewSkirmisher(40), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "outer", HullPct: 100}, "battle", "advance", ""},
		{"skirmisher retreats toward mid from inner", NewSkirmisher(40), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "inner", HullPct: 100}, "battle", "retreat", ""},
		{"skirmisher retreats when hull low", NewSkirmisher(40), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", HullPct: 30}, "battle", "retreat", ""},
		{"retreater flees", NewRetreater(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", Stance: "fire", HullPct: 100}, "battle", "stance", "flee"},
		{"dummy braces", NewDummy(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", Stance: "fire", HullPct: 100}, "battle", "stance", "brace"},
		{"dummy noop when already bracing", NewDummy(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", Stance: "brace", HullPct: 100}, "noop", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := View{Self: tt.self, Enemies: []game.BattleParticipant{enemy}}
			got := tt.policy.Decide(v)
			if got.Kind != tt.wantKind {
				t.Fatalf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if tt.wantKind == "battle" && got.BattleAction != tt.wantAction {
				t.Fatalf("BattleAction = %q, want %q", got.BattleAction, tt.wantAction)
			}
			if tt.wantStance != "" {
				if got.Payload["stance"] != tt.wantStance {
					t.Fatalf("stance = %v, want %q", got.Payload["stance"], tt.wantStance)
				}
			}
		})
	}
}

func TestPolicyByName(t *testing.T) {
	for _, n := range []string{"aggressor", "skirmisher", "retreater", "dummy"} {
		if p, err := PolicyByName(n); err != nil || p == nil {
			t.Fatalf("PolicyByName(%q) = %v, %v", n, p, err)
		}
	}
	if _, err := PolicyByName("bogus"); err == nil {
		t.Fatal("PolicyByName(bogus) should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/spar/ -run TestPolicies -v`
Expected: FAIL — package/types do not exist yet (compile error).

- [ ] **Step 3: Implement `policy.go`**

Create `pkg/spar/policy.go`:

```go
// Package spar provides a controlled PvP sparring harness: it logs in two or
// more agents, equips and moves them to a non-empire arena, and runs scripted
// (or human-partnered) battles so combat mechanics are observable and testable.
package spar

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// View is the read-only battle picture handed to a Policy each tick.
type View struct {
	Self     game.BattleParticipant
	Enemies  []game.BattleParticipant
	Allies   []game.BattleParticipant
	Tick     int64
	BattleID string
}

// Action is a Policy's chosen move. Kind is "battle" (dispatch via
// client.Battle with BattleAction+Payload) or "noop" (do nothing this tick).
type Action struct {
	Kind         string         // "battle" | "noop"
	BattleAction string         // advance | retreat | stance | target
	Payload      map[string]any // stance, target_id
}

// Policy decides one battle action from a View. Implementations are pure
// functions of the View, which keeps them unit-testable. This interface is the
// seam where custom .smolt-style battle scripts plug in later.
type Policy interface {
	Name() string
	Decide(View) Action
}

func noop() Action { return Action{Kind: "noop"} }

// zoneIndex orders the tactical zones from far (0) to close (3).
var zoneIndex = map[string]int{"outer": 0, "mid": 1, "inner": 2, "engaged": 3}

// BuildView assembles a View for the participant whose PlayerID is selfID.
// Returns ok=false if selfID is not among the participants.
func BuildView(b *game.BattleState, selfID string) (View, bool) {
	if b == nil {
		return View{}, false
	}
	var self game.BattleParticipant
	found := false
	var allies, enemies []game.BattleParticipant
	for _, p := range b.Participants {
		if p.PlayerID == selfID {
			self = p
			found = true
		}
	}
	if !found {
		return View{}, false
	}
	for _, p := range b.Participants {
		if p.PlayerID == selfID {
			continue
		}
		if p.SideID == self.SideID {
			allies = append(allies, p)
		} else {
			enemies = append(enemies, p)
		}
	}
	return View{Self: self, Allies: allies, Enemies: enemies, BattleID: b.BattleID}, true
}

// --- presets ---

type aggressor struct{}

// NewAggressor advances to the engaged zone, then targets the nearest enemy and
// holds the fire stance.
func NewAggressor() Policy { return aggressor{} }
func (aggressor) Name() string { return "aggressor" }
func (aggressor) Decide(v View) Action {
	if zoneIndex[v.Self.Zone] < zoneIndex["engaged"] {
		return Action{Kind: "battle", BattleAction: "advance"}
	}
	if v.Self.TargetID == "" && len(v.Enemies) > 0 {
		return Action{Kind: "battle", BattleAction: "target", Payload: map[string]any{"target_id": v.Enemies[0].PlayerID}}
	}
	if v.Self.Stance != "fire" {
		return Action{Kind: "battle", BattleAction: "stance", Payload: map[string]any{"stance": "fire"}}
	}
	return noop()
}

type skirmisher struct{ hullFloor int }

// NewSkirmisher holds the mid zone and fires, but retreats one zone when its
// own hull percentage drops below hullFloor.
func NewSkirmisher(hullFloor int) Policy { return skirmisher{hullFloor: hullFloor} }
func (skirmisher) Name() string { return "skirmisher" }
func (s skirmisher) Decide(v View) Action {
	if v.Self.HullPct < s.hullFloor && zoneIndex[v.Self.Zone] > zoneIndex["outer"] {
		return Action{Kind: "battle", BattleAction: "retreat"}
	}
	switch {
	case zoneIndex[v.Self.Zone] < zoneIndex["mid"]:
		return Action{Kind: "battle", BattleAction: "advance"}
	case zoneIndex[v.Self.Zone] > zoneIndex["mid"]:
		return Action{Kind: "battle", BattleAction: "retreat"}
	}
	if v.Self.TargetID == "" && len(v.Enemies) > 0 {
		return Action{Kind: "battle", BattleAction: "target", Payload: map[string]any{"target_id": v.Enemies[0].PlayerID}}
	}
	if v.Self.Stance != "fire" {
		return Action{Kind: "battle", BattleAction: "stance", Payload: map[string]any{"stance": "fire"}}
	}
	return noop()
}

type retreater struct{}

// NewRetreater immediately adopts the flee stance (which auto-retreats over
// several ticks), exercising the escape mechanic.
func NewRetreater() Policy { return retreater{} }
func (retreater) Name() string { return "retreater" }
func (retreater) Decide(v View) Action {
	if v.Self.Stance != "flee" {
		return Action{Kind: "battle", BattleAction: "stance", Payload: map[string]any{"stance": "flee"}}
	}
	return noop()
}

type dummy struct{}

// NewDummy braces and never advances or fires — a low-risk practice partner.
func NewDummy() Policy { return dummy{} }
func (dummy) Name() string { return "dummy" }
func (dummy) Decide(v View) Action {
	if v.Self.Stance != "brace" {
		return Action{Kind: "battle", BattleAction: "stance", Payload: map[string]any{"stance": "brace"}}
	}
	return noop()
}

// PolicyByName resolves a preset name to a Policy. The skirmisher uses a 40%
// hull floor by default.
func PolicyByName(name string) (Policy, error) {
	switch name {
	case "aggressor":
		return NewAggressor(), nil
	case "skirmisher":
		return NewSkirmisher(40), nil
	case "retreater":
		return NewRetreater(), nil
	case "dummy":
		return NewDummy(), nil
	default:
		return nil, fmt.Errorf("unknown policy %q (want: aggressor, skirmisher, retreater, dummy)", name)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/spar/ -run 'TestPolicies|TestBuildView|TestPolicyByName' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/spar/policy.go pkg/spar/policy_test.go
git commit -m "feat(spar): battle policy presets and View"
```

---

## Task 4: `pkg/spar` arena selection

**Files:**
- Create: `pkg/spar/arena.go`
- Test: `pkg/spar/arena_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/spar/arena_test.go`:

```go
package spar

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestIsNonEmpireArena(t *testing.T) {
	tests := []struct {
		name string
		sys  game.SystemData
		want bool
	}{
		{"empire policed", game.SystemData{ID: "sol", Empire: "terran", SecurityStatus: "High Security", PoliceLevel: 5}, false},
		{"lawless", game.SystemData{ID: "ross_128", Empire: "", SecurityStatus: "Lawless", PoliceLevel: 0}, true},
		{"stronghold", game.SystemData{ID: "den", IsStronghold: true, PoliceLevel: 3}, true},
		{"unowned no police", game.SystemData{ID: "void", Empire: "", PoliceLevel: 0}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNonEmpireArena(tt.sys); got != tt.want {
				t.Fatalf("IsNonEmpireArena(%s) = %v, want %v", tt.sys.ID, got, tt.want)
			}
		})
	}
}

func TestSelectArena(t *testing.T) {
	treasure := game.SystemData{ID: "treasure_cache", Empire: "terran", SecurityStatus: "High Security", PoliceLevel: 5}
	ross := game.SystemData{
		ID: "ross_128", Empire: "", SecurityStatus: "Lawless", PoliceLevel: 0,
		POIs:        []game.POI{{ID: "ross_128-belt", Type: "asteroid_belt"}},
		Connections: []game.ConnectionInfo{{SystemID: "treasure_cache"}},
	}
	// Lawless but no safe-adjacent neighbor and no POI: should be rejected.
	deepVoid := game.SystemData{ID: "deep_void", Empire: "", PoliceLevel: 0}

	byID := map[string]game.SystemData{"treasure_cache": treasure, "ross_128": ross, "deep_void": deepVoid}
	all := []game.SystemData{treasure, ross, deepVoid}
	reachable := func(string) bool { return true }

	got, err := SelectArena(all, byID, reachable)
	if err != nil {
		t.Fatalf("SelectArena error: %v", err)
	}
	if got != "ross_128" {
		t.Fatalf("SelectArena = %q, want ross_128", got)
	}
}

func TestSelectArena_NoneFound(t *testing.T) {
	sol := game.SystemData{ID: "sol", Empire: "terran", PoliceLevel: 5}
	byID := map[string]game.SystemData{"sol": sol}
	if _, err := SelectArena([]game.SystemData{sol}, byID, func(string) bool { return true }); err == nil {
		t.Fatal("SelectArena should error when no non-empire arena exists")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/spar/ -run TestSelectArena -v`
Expected: FAIL — functions undefined (compile error).

- [ ] **Step 3: Implement `arena.go`**

Create `pkg/spar/arena.go`:

```go
package spar

import (
	"context"
	"fmt"
	"log"

	"github.com/rsned/spacemolt/pkg/game"
)

// IsNonEmpireArena reports whether a system permits anything-goes PvP: it is
// not owned by an empire, or is lawless / unpoliced / a pirate stronghold.
func IsNonEmpireArena(s game.SystemData) bool {
	return s.Empire == "" || s.SecurityStatus == "Lawless" || s.PoliceLevel == 0 || s.IsStronghold
}

// hasSafeAdjacency reports whether s connects to at least one policed/empire
// system — somewhere to retreat to, equip at, or rebuild from.
func hasSafeAdjacency(s game.SystemData, byID map[string]game.SystemData) bool {
	for _, c := range s.Connections {
		if n, ok := byID[c.SystemID]; ok && !IsNonEmpireArena(n) {
			return true
		}
	}
	return false
}

// hasRendezvousPOI reports whether s has at least one POI to gather combatants at.
func hasRendezvousPOI(s game.SystemData) bool { return len(s.POIs) > 0 }

// SelectArena picks the best non-empire arena from systems: it must be a
// non-empire system, be reachable by all combatants (reachable(id) true), have
// a rendezvous POI, and have safe-space adjacency. Among candidates the first
// match (by input order) is returned. Pure — no network.
func SelectArena(systems []game.SystemData, byID map[string]game.SystemData, reachable func(systemID string) bool) (string, error) {
	for _, s := range systems {
		if !IsNonEmpireArena(s) {
			continue
		}
		if !hasRendezvousPOI(s) {
			continue
		}
		if !hasSafeAdjacency(s, byID) {
			continue
		}
		if !reachable(s.ID) {
			continue
		}
		return s.ID, nil
	}
	return "", fmt.Errorf("no reachable non-empire arena with a rendezvous POI and safe-space adjacency found; pass --arena explicitly")
}

// ValidateArenaSystem checks that an already-loaded system is a legal arena.
// Used at arrival to confirm a --arena value really is outside empire space.
func ValidateArenaSystem(s game.SystemData) error {
	if !IsNonEmpireArena(s) {
		return fmt.Errorf("system %s (%s) is empire space, not a valid arena", s.ID, s.SecurityStatus)
	}
	return nil
}
```

> **Why no auto-discovery glue here:** the client does not cache the full
> systems list — `parseMapData` (pkg/game/client.go) only refreshes the current
> system's connections, so there is no `[]SystemData` to scan. Auto-discovery is
> therefore deferred (see the plan's "Deferred" note); the pure `IsNonEmpireArena`
> / `SelectArena` functions are kept as the tested foundation a future KB-backed
> finder will use. For this iteration the arena is supplied via `--arena` and
> validated on arrival with `ValidateArenaSystem`.

Note: the `context` and `log` imports are no longer needed in `arena.go` after
dropping `ArenaFinder`; the file only needs `fmt` and `pkg/game`. Adjust the
import block accordingly:

```go
import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/spar/ -run TestSelectArena -v && go test ./pkg/spar/ -run TestIsNonEmpireArena -v`
Expected: PASS.

- [ ] **Step 5: Verify the package builds**

Run: `go build ./pkg/spar/...`
Expected: builds.

- [ ] **Step 6: Commit**

```bash
git add pkg/spar/arena.go pkg/spar/arena_test.go
git commit -m "feat(spar): non-empire arena selection"
```

---

## Task 5: `pkg/spar` combatant setup helpers

**Files:**
- Create: `pkg/spar/combatant.go`
- Test: `pkg/spar/combatant_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/spar/combatant_test.go`:

```go
package spar

import (
	"reflect"
	"testing"
)

func TestNeededModules(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		weapon    string
		shield    string
		want      []string
	}{
		{"none installed", nil, "pulse_laser_i", "shield_booster_i", []string{"pulse_laser_i", "shield_booster_i"}},
		{"has weapon only", []string{"pulse_laser_i"}, "pulse_laser_i", "shield_booster_i", []string{"shield_booster_i"}},
		{"has shield only", []string{"shield_booster_ii"}, "pulse_laser_i", "shield_booster_i", []string{"pulse_laser_i"}},
		{"fully equipped", []string{"autocannon_i", "shield_booster_i"}, "pulse_laser_i", "shield_booster_i", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := neededModules(tt.installed, tt.weapon, tt.shield)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("neededModules = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/spar/ -run TestNeededModules -v`
Expected: FAIL — `neededModules` undefined.

- [ ] **Step 3: Implement `combatant.go`**

Create `pkg/spar/combatant.go`:

```go
package spar

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// weaponPrefixes are the module-id prefixes that count as a fitted weapon.
var weaponPrefixes = []string{
	"pulse_laser_", "autocannon_", "focused_beam_", "railgun_",
	"missile_launcher_", "ion_cannon_", "plasma_cannon_",
}

func isWeaponModule(id string) bool {
	for _, p := range weaponPrefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// neededModules returns the module ids to buy+install so the ship has at least
// one weapon and one shield. Pure — order is weapon then shield.
func neededModules(installed []string, weaponID, shieldID string) []string {
	haveWeapon, haveShield := false, false
	for _, m := range installed {
		if isWeaponModule(m) {
			haveWeapon = true
		}
		if strings.HasPrefix(m, "shield_") {
			haveShield = true
		}
	}
	var need []string
	if !haveWeapon && weaponID != "" {
		need = append(need, weaponID)
	}
	if !haveShield && shieldID != "" {
		need = append(need, shieldID)
	}
	return need
}

// Combatant is one logged-in agent plus its assigned policy and setup config.
type Combatant struct {
	AgentID  string
	Username string
	Client   game.GameClient
	Policy   Policy // nil for the human partner slot
	WeaponID string
	ShieldID string
	NoEquip  bool
	Logger   *log.Logger // must be non-nil: game.NavigateToSystem dereferences it
}

// PlayerID returns the combatant's player id from live state.
func (c *Combatant) PlayerID() string { return c.Client.GetState().Player.ID }

// Setup equips the combatant (if needed), navigates it to the arena system, and
// travels to the rendezvous POI type. Equip runs first because stations are
// typically one or more jumps from lawless arenas.
func (c *Combatant) Setup(ctx context.Context, arenaSystem, rendezvousType string) error {
	if !c.NoEquip {
		if err := c.equip(ctx); err != nil {
			return fmt.Errorf("%s equip: %w", c.AgentID, err)
		}
	}
	if err := game.NavigateToSystem(c.Client, ctx, arenaSystem, c.Logger); err != nil {
		return fmt.Errorf("%s navigate to %s: %w", c.AgentID, arenaSystem, err)
	}
	if err := c.Client.GetSystem(ctx); err != nil {
		return fmt.Errorf("%s get_system: %w", c.AgentID, err)
	}
	poi := firstPOIOfType(c.Client.GetState().System, rendezvousType)
	if poi == "" {
		return fmt.Errorf("%s: arena %s has no rendezvous POI", c.AgentID, arenaSystem)
	}
	if _, err := c.Client.Travel(ctx, poi); err != nil {
		return fmt.Errorf("%s travel to rendezvous %s: %w", c.AgentID, poi, err)
	}
	return nil
}

// equip docks at a station, buys+installs any missing weapon/shield, refuels and
// repairs, then undocks. If the ship is already armed and shielded it is a no-op
// beyond a refuel/repair.
func (c *Combatant) equip(ctx context.Context) error {
	st := c.Client.GetState()
	need := neededModules(st.Ship.Modules, c.WeaponID, c.ShieldID)
	if len(need) == 0 {
		return nil
	}
	if err := game.NavigateAndDock(c.Client, ctx, c.Logger); err != nil {
		return fmt.Errorf("dock for equip: %w", err)
	}
	for _, mod := range need {
		if err := c.Client.Buy(ctx, mod, 1); err != nil {
			return fmt.Errorf("buy %s: %w", mod, err)
		}
		time.Sleep(game.SleepQuick)
		if err := c.Client.InstallMod(ctx, mod); err != nil {
			return fmt.Errorf("install %s: %w", mod, err)
		}
		time.Sleep(game.SleepQuick)
	}
	_ = c.Client.Refuel(ctx)
	_ = c.Client.Repair(ctx)
	if err := c.Client.Undock(ctx); err != nil {
		return fmt.Errorf("undock after equip: %w", err)
	}
	return nil
}

// firstPOIOfType returns the first POI id of the given type, or the first POI of
// any type if none match (fallback), or "" if the system has no POIs.
func firstPOIOfType(s game.SystemData, poiType string) string {
	for _, p := range s.POIs {
		if p.Type == poiType {
			return p.ID
		}
	}
	if len(s.POIs) > 0 {
		return s.POIs[0].ID
	}
	return ""
}
```

> **Note for the implementer:** `Ship.Modules` is `[]string` (`pkg/game/types.go:143`) and `game.NavigateAndDock` exists (`pkg/game/navigation.go:91`), docking at the current system's station — correct here, since equip precedes arena travel. `game.NavigateToSystem` calls `logger.Printf` **unguarded** (`navigation.go:18`), so `Combatant.Logger` MUST be non-nil; Task 8 sets it.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/spar/ -run TestNeededModules -v`
Expected: PASS.

- [ ] **Step 5: Verify the package builds**

Run: `go build ./pkg/spar/...`
Expected: builds.

- [ ] **Step 6: Commit**

```bash
git add pkg/spar/combatant.go pkg/spar/combatant_test.go
git commit -m "feat(spar): combatant setup (equip + travel + rendezvous)"
```

---

## Task 6: `pkg/spar` match loop & end-detection

**Files:**
- Create: `pkg/spar/match.go`
- Test: `pkg/spar/match_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/spar/match_test.go`:

```go
package spar

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// fakeBattleClient feeds a scripted sequence of BattleStates and records the
// battle actions dispatched against it.
type fakeBattleClient struct {
	states  []*game.BattleState
	idx     int
	actions []string // "<action>:<stance-or-target>"
}

func (f *fakeBattleClient) GetBattleStatus(_ context.Context) error { return nil }

func (f *fakeBattleClient) Battle(_ context.Context, action string, payload map[string]any) error {
	tag := action
	if s, ok := payload["stance"].(string); ok {
		tag += ":" + s
	} else if t, ok := payload["target_id"].(string); ok {
		tag += ":" + t
	}
	f.actions = append(f.actions, tag)
	return nil
}

func (f *fakeBattleClient) GetState() *game.State {
	var cur *game.BattleState
	if f.idx < len(f.states) {
		cur = f.states[f.idx]
	}
	f.idx++
	return &game.State{BattleState: cur}
}

func TestBattleOver(t *testing.T) {
	twoSides := &game.BattleState{IsParticipant: true, Participants: []game.BattleParticipant{
		{PlayerID: "me", SideID: "1", HullPct: 50},
		{PlayerID: "foe", SideID: "2", HullPct: 30},
	}}
	if battleOver(twoSides) {
		t.Fatal("two living sides: battle should NOT be over")
	}
	oneSide := &game.BattleState{IsParticipant: true, Participants: []game.BattleParticipant{
		{PlayerID: "me", SideID: "1", HullPct: 50},
		{PlayerID: "foe", SideID: "2", HullPct: 0},
	}}
	if !battleOver(oneSide) {
		t.Fatal("one living side: battle should be over")
	}
	if !battleOver(nil) {
		t.Fatal("nil battle state: should be over")
	}
}

func TestRunPolicyLoop_DispatchesThenStops(t *testing.T) {
	inBattle := &game.BattleState{BattleID: "b1", IsParticipant: true, Participants: []game.BattleParticipant{
		{PlayerID: "me", SideID: "1", Zone: "mid", Stance: "fire", HullPct: 100},
		{PlayerID: "foe", SideID: "2", Zone: "mid", HullPct: 100},
	}}
	over := &game.BattleState{BattleID: "b1", IsParticipant: true, Participants: []game.BattleParticipant{
		{PlayerID: "me", SideID: "1", HullPct: 100},
		{PlayerID: "foe", SideID: "2", HullPct: 0},
	}}
	f := &fakeBattleClient{states: []*game.BattleState{inBattle, over}}

	err := runPolicyLoop(context.Background(), f, "me", NewRetreater(), time.Millisecond)
	if err != nil {
		t.Fatalf("runPolicyLoop error: %v", err)
	}
	// First tick: retreater is firing, so it issues stance flee. Second tick:
	// battle is over, loop returns without acting.
	if len(f.actions) != 1 || f.actions[0] != "stance:flee" {
		t.Fatalf("actions = %v, want [stance:flee]", f.actions)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/spar/ -run 'TestBattleOver|TestRunPolicyLoop' -v`
Expected: FAIL — `battleClient`/`battleOver`/`runPolicyLoop` undefined.

- [ ] **Step 3: Implement `match.go`**

Create `pkg/spar/match.go`:

```go
package spar

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// battleClient is the minimal client surface the per-combatant loop needs. Both
// *game.Client and the test fake satisfy it.
type battleClient interface {
	GetBattleStatus(ctx context.Context) error
	Battle(ctx context.Context, action string, payload map[string]any) error
	GetState() *game.State
}

// battleOver reports whether a battle has ended: nil/absent state, we are no
// longer a participant, or one or fewer sides still have a living (HullPct>0)
// participant.
func battleOver(b *game.BattleState) bool {
	if b == nil || !b.IsParticipant {
		return true
	}
	living := map[string]bool{}
	for _, p := range b.Participants {
		if p.HullPct > 0 {
			living[p.SideID] = true
		}
	}
	return len(living) <= 1
}

// runPolicyLoop drives one combatant: each tick it refreshes battle status,
// stops when the battle is over, otherwise applies its policy. tickSleep is the
// pause between iterations (game.SleepTick in production; tiny in tests).
func runPolicyLoop(ctx context.Context, bc battleClient, selfID string, p Policy, tickSleep time.Duration) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := bc.GetBattleStatus(ctx); err != nil {
			return fmt.Errorf("get_battle_status: %w", err)
		}
		b := bc.GetState().BattleState
		if battleOver(b) {
			return nil
		}
		if v, ok := BuildView(b, selfID); ok {
			act := p.Decide(v)
			if act.Kind == "battle" {
				if err := bc.Battle(ctx, act.BattleAction, act.Payload); err != nil {
					return fmt.Errorf("battle %s: %w", act.BattleAction, err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tickSleep):
		}
	}
}

// Match orchestrates a sparring session.
type Match struct {
	Combatants     []*Combatant
	Arena          string // arena system id ("" => auto-discover)
	Rendezvous     string // POI type, e.g. "asteroid_belt"
	AggressorIdx   int    // index of the bot that initiates (botvbot mode)
	Partner        bool   // partner mode: don't initiate; wait for a human to attack
	MaxTicks       int
	Logger         *log.Logger
}

// Run sets up all combatants, initiates (or waits for) the fight, drives the
// bots, and prints telemetry plus a final summary.
func (m *Match) Run(ctx context.Context) error {
	// botvbot needs >=2 bots; partner mode needs >=1 bot (the human is an
	// external play_as session, not a combatant the harness logs in).
	minC := 2
	if m.Partner {
		minC = 1
	}
	if len(m.Combatants) < minC {
		return fmt.Errorf("need at least %d combatant(s), got %d", minC, len(m.Combatants))
	}

	// Resolve arena. Auto-discovery is deferred (the client does not cache the
	// full systems list), so require an explicit --arena for now.
	arena := m.Arena
	if arena == "" {
		return fmt.Errorf("no arena specified: pass --arena <system> (e.g. ross_128); auto-discovery is not yet supported")
	}

	// Concurrent setup (WaitGroup + first-error; no errgroup dependency).
	if err := m.setupAll(ctx, arena); err != nil {
		return fmt.Errorf("combatant setup: %w", err)
	}
	m.Logger.Printf("All %d combatants staged in %s", len(m.Combatants), arena)

	// Validate the arena really is outside empire space (checked on arrival,
	// since we can only load the current system's data).
	if err := m.Combatants[0].Client.GetSystem(ctx); err != nil {
		return fmt.Errorf("validate arena: get_system: %w", err)
	}
	if err := ValidateArenaSystem(m.Combatants[0].Client.GetState().System); err != nil {
		return err
	}

	// Initiate.
	if m.Partner {
		opp := m.Combatants[0]
		m.Logger.Printf("PARTNER MODE: arena=%s rendezvous-type=%s", arena, m.Rendezvous)
		m.Logger.Printf("  Run: play_as <your-agent>, travel to the rendezvous POI, then: attack %s", opp.Username)
		m.Logger.Printf("  Waiting for a battle to start...")
		if err := m.waitForBattle(ctx, opp); err != nil {
			return err
		}
	} else {
		agg := m.Combatants[m.AggressorIdx]
		target := m.Combatants[(m.AggressorIdx+1)%len(m.Combatants)]
		m.Logger.Printf("%s attacking %s", agg.Username, target.Username)
		if err := agg.Client.Attack(ctx, target.Username); err != nil {
			return fmt.Errorf("initiate attack: %w", err)
		}
	}

	// Run bot policy loops + telemetry concurrently until the battle ends.
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if m.MaxTicks > 0 {
		go func() {
			select {
			case <-loopCtx.Done():
			case <-time.After(time.Duration(m.MaxTicks) * game.SleepTick):
				m.Logger.Printf("max-ticks reached; ending match")
				cancel()
			}
		}()
	}

	// Telemetry off the first combatant's client.
	go m.telemetryLoop(loopCtx, m.Combatants[0])

	var wg sync.WaitGroup
	for _, c := range m.Combatants {
		c := c
		if c.Policy == nil {
			continue // human partner slot: not bot-driven
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runPolicyLoop(loopCtx, c.Client, c.PlayerID(), c.Policy, game.SleepTick); err != nil && err != context.Canceled {
				m.Logger.Printf("%s policy loop ended: %v", c.Username, err)
			}
		}()
	}
	wg.Wait()
	cancel()

	m.printSummary()
	return nil
}

// setupAll runs Setup for every combatant concurrently, returning the first
// error encountered.
func (m *Match) setupAll(ctx context.Context, arena string) error {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for _, c := range m.Combatants {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Setup(ctx, arena, m.Rendezvous); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

// waitForBattle polls until the given combatant is in a battle (partner mode).
func (m *Match) waitForBattle(ctx context.Context, c *Combatant) error {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_ = c.Client.GetBattleStatus(ctx)
		if !battleOver(c.Client.GetState().BattleState) {
			return nil
		}
		time.Sleep(game.SleepTick)
	}
	return fmt.Errorf("no battle started within timeout; did the human attack %s?", c.Username)
}

// telemetryLoop prints a per-tick row for each participant.
func (m *Match) telemetryLoop(ctx context.Context, c *Combatant) {
	var lastTick int64 = -1
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_ = c.Client.GetBattleStatus(ctx)
		st := c.Client.GetState()
		if st.BattleState != nil && st.CurrentTick != lastTick {
			lastTick = st.CurrentTick
			for _, p := range st.BattleState.Participants {
				m.Logger.Print(formatTickRow(st.CurrentTick, p))
			}
		}
		time.Sleep(game.SleepTick)
	}
}

// printSummary prints final hull/shield per combatant.
func (m *Match) printSummary() {
	m.Logger.Printf("──── match summary ────")
	for _, c := range m.Combatants {
		st := c.Client.GetState()
		m.Logger.Print(formatSummary(c.Username, st))
	}
}
```

> **Note for the implementer:** this task deliberately uses the stdlib
> `sync.WaitGroup` (not `golang.org/x/sync/errgroup`, which is not a dependency)
> for both `setupAll` and the policy-loop fan-out, so no `go.mod` change is
> needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/spar/ -run 'TestBattleOver|TestRunPolicyLoop' -v`
Expected: PASS.

- [ ] **Step 5: Build the package**

Run: `go build ./pkg/spar/...`
Expected: builds. (`formatTickRow`/`formatSummary` come from Task 7; if building this task alone fails on those, do Task 7 before building. They are referenced here and defined there.)

- [ ] **Step 6: Commit**

```bash
git add pkg/spar/match.go pkg/spar/match_test.go
git commit -m "feat(spar): match orchestration, policy loop, end-detection"
```

---

## Task 7: `pkg/spar` telemetry formatters

**Files:**
- Create: `pkg/spar/telemetry.go`
- Test: `pkg/spar/telemetry_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/spar/telemetry_test.go`:

```go
package spar

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestFormatTickRow(t *testing.T) {
	p := game.BattleParticipant{Username: "Me", Zone: "mid", Stance: "fire", HullPct: 80, ShieldPct: 50}
	row := formatTickRow(917153, p)
	for _, want := range []string{"917153", "Me", "mid", "fire", "80", "50"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row %q missing %q", row, want)
		}
	}
}

func TestFormatSummary(t *testing.T) {
	st := &game.State{}
	st.Hull = 40
	st.MaxHull = 100
	s := formatSummary("Me", st)
	if !strings.Contains(s, "Me") || !strings.Contains(s, "40") {
		t.Fatalf("summary %q missing name or hull", s)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/spar/ -run 'TestFormatTickRow|TestFormatSummary' -v`
Expected: FAIL — formatters undefined.

- [ ] **Step 3: Implement `telemetry.go`**

Create `pkg/spar/telemetry.go`:

```go
package spar

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// formatTickRow renders one participant's per-tick battle state:
// "tick 917153 | Me        | zone=mid     | stance=fire  | hull 80% | shield 50%".
func formatTickRow(tick int64, p game.BattleParticipant) string {
	return fmt.Sprintf("tick %d | %-12s | zone=%-7s | stance=%-6s | hull %3d%% | shield %3d%%",
		tick, p.Username, p.Zone, p.Stance, p.HullPct, p.ShieldPct)
}

// formatSummary renders a combatant's end-of-match ship state.
func formatSummary(username string, st *game.State) string {
	hullPct := 0
	if st.MaxHull > 0 {
		hullPct = int(st.Hull / st.MaxHull * 100)
	}
	status := "alive"
	if st.Hull <= 0 {
		status = "DESTROYED"
	}
	return fmt.Sprintf("  %-12s | hull %.0f/%.0f (%d%%) | %s", username, st.Hull, st.MaxHull, hullPct, status)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/spar/ -run 'TestFormatTickRow|TestFormatSummary' -v`
Expected: PASS.

- [ ] **Step 5: Build & test the whole package**

Run: `go build ./pkg/spar/... && go test ./pkg/spar/`
Expected: builds and all `pkg/spar` tests pass.

- [ ] **Step 6: Commit**

```bash
git add pkg/spar/telemetry.go pkg/spar/telemetry_test.go
git commit -m "feat(spar): battle telemetry formatters"
```

---

## Task 8: `cmd/tools/spar` binary

**Files:**
- Create: `cmd/tools/spar/main.go`
- Create: `cmd/tools/spar/README.md`

- [ ] **Step 1: Implement `main.go`**

Create `cmd/tools/spar/main.go`:

```go
// Command spar runs a controlled PvP sparring match between two or more of our
// own agents in a non-empire arena, so combat mechanics are observable and
// testable. See docs/superpowers/specs/2026-05-25-sparring-testbed-design.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/spar"
)

func main() {
	var (
		mode       = flag.String("mode", "botvbot", "botvbot | partner")
		arena      = flag.String("arena", "", "arena system id, REQUIRED (e.g. ross_128); auto-discovery not yet supported")
		policyFlag = flag.String("policy", "", "per-agent policies, e.g. fighter-1=aggressor,fighter-2=dummy")
		aggressor  = flag.String("aggressor", "", "agent id that initiates (botvbot; default: first)")
		rendezvous = flag.String("rendezvous", "asteroid_belt", "POI type to gather at")
		maxTicks   = flag.Int("max-ticks", 60, "safety cap on match length in ticks")
		noEquip    = flag.Bool("no-equip", false, "skip auto-equip (verify only)")
		weaponID   = flag.String("weapon", "pulse_laser_i", "cheap weapon module id to fit if missing")
		shieldID   = flag.String("shield", "shield_booster_i", "cheap shield module id to fit if missing")
		debug      = flag.Bool("debug", false, "enable debug logging")
	)
	flag.Parse()

	agentIDs := flag.Args()
	minAgents := 2 // botvbot needs 2 bots; partner mode needs 1 (human is external)
	if *mode == "partner" {
		minAgents = 1
	}
	if len(agentIDs) < minAgents {
		fmt.Println("Usage: spar [flags] <agent-1> <agent-2> [agent-3 ...]")
		fmt.Println("Example: spar --arena ross_128 fighter-1 fighter-2")
		fmt.Println("         spar --mode partner --arena ross_128 --policy fighter-2=dummy fighter-2")
		flag.PrintDefaults()
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "[spar] ", log.LstdFlags)
	ctx := context.Background()

	policies := parsePolicyFlag(*policyFlag)

	var combatants []*spar.Combatant
	aggIdx := 0
	for i, id := range agentIDs {
		client, creds, err := game.InitializeAgent(id, logger, ctx, *debug)
		if err != nil {
			log.Fatalf("login %s: %v", id, err)
		}
		defer func(c *game.Client, id string) {
			if err := c.Close(); err != nil {
				logger.Printf("close %s: %v", id, err)
			}
		}(client, id)

		// Every agent passed to spar is a bot the harness drives (in partner
		// mode the human is a separate play_as session, not an arg). Default
		// policy: first=aggressor, rest=skirmisher; override with --policy.
		name, ok := policies[id]
		if !ok {
			if i == 0 {
				name = "aggressor"
			} else {
				name = "skirmisher"
			}
		}
		pol, err := spar.PolicyByName(name)
		if err != nil {
			log.Fatalf("policy for %s: %v", id, err)
		}

		combatants = append(combatants, &spar.Combatant{
			AgentID:  id,
			Username: creds.Username,
			Client:   client,
			Policy:   pol,
			WeaponID: *weaponID,
			ShieldID: *shieldID,
			NoEquip:  *noEquip,
			Logger:   logger, // required: NavigateToSystem dereferences it
		})
		if *aggressor != "" && id == *aggressor {
			aggIdx = i
		}
	}

	m := &spar.Match{
		Combatants:   combatants,
		Arena:        *arena,
		Rendezvous:   *rendezvous,
		AggressorIdx: aggIdx,
		Partner:      *mode == "partner",
		MaxTicks:     *maxTicks,
		Logger:       logger,
	}
	if err := m.Run(ctx); err != nil {
		log.Fatalf("match: %v", err)
	}
}

// parsePolicyFlag parses "a=aggressor,b=dummy" into a map.
func parsePolicyFlag(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}
```

> **Note for the implementer:** the struct literal sets `Logger: logger` because `game.NavigateToSystem` dereferences the logger unguarded (`navigation.go:18`) — a nil logger would panic during setup. Every agent passed to `spar` is a bot the harness logs in and drives. In **partner mode** the human is NOT passed as an arg — they run their own `play_as <their-agent>` session, travel to the arena, and `attack <bot-username>`; the harness only stages/drives the bot(s) and polls a bot client to detect when the battle starts (`Match.waitForBattle`). This avoids a double-login of the same agent. The `Combatant.Policy == nil` branch in `match.go` is therefore unused in the shipped CLI but kept as the seam for that future "human owns a harness-tracked slot" refinement.

- [ ] **Step 2: Build the binary into `bin/`**

Run: `go build -o bin/spar ./cmd/tools/spar`
Expected: builds, produces `bin/spar` (project rule: binaries live in `bin/`).

- [ ] **Step 3: Verify usage output**

Run: `./bin/spar` (no args)
Expected: prints usage and exits non-zero.

- [ ] **Step 4: Write the README**

Create `cmd/tools/spar/README.md`:

```markdown
# spar

Controlled PvP sparring harness. Logs in two or more of our own agents, equips
and moves them to a non-empire arena (PvP is anything-goes outside empire
space), and runs scripted or human-partnered battles so the combat mechanics
are observable and testable.

## Usage

```
spar [flags] <agent-1> <agent-2> [agent-3 ...]
```

### Examples

# ross_128 is lawless, one jump from the station system treasure_cache.

```bash
# Two bots fight; fighter-1 (aggressor) attacks fighter-2 (skirmisher).
spar --arena ross_128 fighter-1 fighter-2

# Practice yourself against a passive dummy. The harness stages & drives
# fighter-2 only; you separately run `play_as <your-agent>`, travel to the
# arena rendezvous POI, and `attack <fighter-2-username>`.
spar --mode partner --arena ross_128 --policy fighter-2=dummy fighter-2
```

`--arena` is required for now (auto-discovery is deferred).

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `botvbot` | `botvbot` (all scripted) or `partner` (first agent is a human slot). |
| `--arena` | — | Arena system id, **required** (e.g. `ross_128`); auto-discovery deferred. |
| `--policy` | — | Per-agent policies, e.g. `fighter-1=aggressor,fighter-2=dummy`. |
| `--aggressor` | first | Which bot initiates (botvbot). |
| `--rendezvous` | `asteroid_belt` | POI type to gather combatants at. |
| `--max-ticks` | `60` | Safety cap on match length. |
| `--no-equip` | `false` | Skip auto-equip (verify only). |
| `--weapon` / `--shield` | `pulse_laser_i` / `shield_booster_i` | Cheap gear fitted if missing. |
| `--debug` | `false` | Debug logging. |

## Policies

- **aggressor** — advance to `engaged`, target nearest, hold `fire`.
- **skirmisher** — hold `mid` and fire; retreat a zone when hull < 40%.
- **retreater** — adopt `flee` immediately (exercises the multi-tick escape).
- **dummy** — `brace` only; never advance or fire (low-risk practice partner).

Stakes: fights can run to completion (ships can die) — use cheap/throwaway
loadouts. Arena must be outside empire space.
```

- [ ] **Step 5: Verify README is not gitignored**

Run: `git check-ignore cmd/tools/spar/README.md && echo IGNORED || echo OK`
Expected: `OK`. If `IGNORED`, add a negation rule to `.gitignore` (e.g. `!cmd/tools/spar/README.md`).

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/spar/main.go cmd/tools/spar/README.md
git commit -m "feat(spar): spar orchestrator binary + README"
```

---

## Task 9: Full build, test, lint, and final commit

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 2: Run all tests**

Run: `go test ./...`
Expected: PASS (focus: `pkg/game`, `pkg/spar`).

- [ ] **Step 3: Lint**

Run: `golangci-lint run ./pkg/spar/... ./cmd/tools/spar/... ./pkg/game/...`
Expected: no new findings. Fix any reported by the new code (common: unchecked errors — wrap with `_ =` only where intentional, as in `equip`'s refuel/repair).

- [ ] **Step 4: Confirm `bin/spar` is built and gitignored appropriately**

Run: `git status --porcelain bin/ | head`
Expected: `bin/` artifacts are ignored (per project rule binaries live in `bin/`). If `bin/` is tracked unexpectedly, do not commit the binary.

- [ ] **Step 5: Final commit (if lint fixes were needed)**

```bash
git add -A
git commit -m "chore(spar): lint fixes and final cleanup"
```

---

## Self-Review notes (addressed)

- **Spec coverage:** arena predicate + arrival validation (Task 4; auto-discovery deferred per spec — `--arena` required), policy presets (Task 3), both drive modes (Task 6/8 — `Partner` flag; human is an external `play_as` session), full-auto travel+equip (Task 5), accept-losses/cheap ships (defaults + summary `DESTROYED` status), telemetry + summary (Task 6/7), `BattleState` foundation (Tasks 1–2), tests (each task), CLI (Task 8). Auto-discovery, faction-war, `.smolt` scripts, 3+ sides, and battle visualization are explicitly out of scope per spec.
- **Type consistency:** `Action.Kind`/`BattleAction`/`Payload`, `View`, `Policy`, `battleClient`, `battleOver`, `runPolicyLoop`, `Combatant` (with exported `Logger`), `Match` (with `setupAll`), `formatTickRow`/`formatSummary`, `IsNonEmpireArena`/`SelectArena`/`ValidateArenaSystem`, `neededModules`/`isWeaponModule` are referenced identically across tasks.
- **Verified against the codebase while planning:** no client-side map cache exists (auto-discovery deferred, `--arena` required); `golang.org/x/sync/errgroup` is not a dependency (used `sync.WaitGroup`); `game.NavigateToSystem` dereferences its logger unguarded (`Combatant.Logger` required, set in Task 8). One remaining local note: `firstPOIOfType` fallback behavior in Task 5.
