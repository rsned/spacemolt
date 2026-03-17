# Thought Engine Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a three-stage Tree-of-Thought decision pipeline ("Thought Engine") with real-time frontend visualization, replacing the single-call LLM decision path for opt-in agents.

**Architecture:** New `pkg/tot` package implements a staged pipeline (Assess → Evaluate → Select) that makes multiple LLM calls to produce a scored decision tree. The tree streams to the React frontend via the existing observer WebSocket for live visualization with React Flow tree graphs and radar charts. Agents opt in via a `decision_mode` field in their personality config.

**Tech Stack:** Go 1.24+, Ollama (local LLM), React 19, TypeScript, Vite, React Flow (new dependency), Tailwind CSS

**Design Doc:** `docs/plans/2026-03-16-thought-engine-design.md`

---

## File Structure

### New Go Files
| File | Responsibility |
|------|---------------|
| `pkg/tot/types.go` | Core data types: ThoughtTree, ThoughtNode, AxisScores, AxisWeights, NodeStatus, ActionOption |
| `pkg/tot/filter.go` | Code-based pre-filter: ValidActions(state) returns valid ActionOptions based on game state |
| `pkg/tot/weights.go` | DeriveWeights(personality) computes axis weights from personality traits/motivations |
| `pkg/tot/evaluator.go` | Pipeline orchestrator: Evaluate() runs the 3-stage pipeline, coordinates LLM calls |
| `pkg/tot/prompts.go` | Prompt builders for Stage 1 (assess) and Stage 2 (evaluate) |
| `pkg/tot/evaluator_test.go` | Tests for evaluator with mock LLM |
| `pkg/tot/filter_test.go` | Tests for action pre-filter |
| `pkg/tot/weights_test.go` | Tests for personality-to-weights derivation |

### New Prompt Templates
| File | Responsibility |
|------|---------------|
| `data/prompts/templates/tot/assess.v1.tmpl` | Stage 1 prompt: situation assessment, generate 3-5 options |
| `data/prompts/templates/tot/evaluate.v1.tmpl` | Stage 2 prompt: evaluate a single option on 5 axes |

### New Frontend Files
| File | Responsibility |
|------|---------------|
| `frontend/src/components/ThoughtTreeView.tsx` | Main tree visualization using React Flow |
| `frontend/src/components/RadarChart.tsx` | SVG radar chart component for axis scores |
| `frontend/src/components/DebugPanel.tsx` | Expandable panel showing prompts, responses, timing |

### Modified Files
| File | Change |
|------|--------|
| `pkg/agent/agent.go` | Add `DecisionMode` field to `Personality` struct |
| `pkg/agent/runner.go` | Add ToT toggle in `executeCycle()`, import `pkg/tot` |
| `pkg/observe/observer.go` | Add `thought_tree` event type handling |
| `frontend/src/lib/useObserver.ts` | Handle `thought_tree` WebSocket message type |
| `frontend/package.json` | Add `@xyflow/react` dependency |

---

## Chunk 1: Core Types and Action Filter

### Task 1: Core Data Types

**Files:**
- Create: `pkg/tot/types.go`

- [ ] **Step 1: Create the types file**

```go
// pkg/tot/types.go
package tot

import "time"

// NodeStatus represents the state of a thought node in the tree.
type NodeStatus string

const (
	StatusActive NodeStatus = "active"
	StatusPruned NodeStatus = "pruned"
	StatusWinner NodeStatus = "winner"
)

// AxisScores holds multi-criteria evaluation scores (0-100 scale).
type AxisScores struct {
	Survival     float64 `json:"survival"`
	Profit       float64 `json:"profit"`
	GoalProgress float64 `json:"goal_progress"`
	Risk         float64 `json:"risk"`
	Efficiency   float64 `json:"efficiency"`
}

// AxisWeights holds personality-derived weights for scoring (0-1 scale).
type AxisWeights struct {
	Survival     float64 `json:"survival"`
	Profit       float64 `json:"profit"`
	GoalProgress float64 `json:"goal_progress"`
	Risk         float64 `json:"risk"`
	Efficiency   float64 `json:"efficiency"`
}

// WeightedScore computes the combined score for an AxisScores using these weights.
func (w AxisWeights) WeightedScore(s AxisScores) float64 {
	total := w.Survival + w.Profit + w.GoalProgress + w.Risk + w.Efficiency
	if total == 0 {
		return 0
	}
	raw := s.Survival*w.Survival + s.Profit*w.Profit +
		s.GoalProgress*w.GoalProgress + s.Risk*w.Risk +
		s.Efficiency*w.Efficiency
	return raw / total
}

// ThoughtNode represents one option in the decision tree.
type ThoughtNode struct {
	ID        string         `json:"id"`
	Action    string         `json:"action"`
	Target    string         `json:"target"`
	Reasoning string         `json:"reasoning"`
	Scores    AxisScores     `json:"scores"`
	Combined  float64        `json:"combined"`
	Status    NodeStatus     `json:"status"`
	Children  []*ThoughtNode `json:"children,omitempty"`
	Depth     int            `json:"depth"`
	EvalTime  time.Duration  `json:"eval_time_ms"`
	Prompt    string         `json:"prompt,omitempty"`
	RawResponse string       `json:"raw_response,omitempty"`
}

// ThoughtTree holds the complete decision tree for one cycle.
type ThoughtTree struct {
	ID        string         `json:"id"`
	AgentID   string         `json:"agent_id"`
	Timestamp time.Time      `json:"timestamp"`
	Situation string         `json:"situation"`
	Root      []*ThoughtNode `json:"root"`
	WinnerID  string         `json:"winner_id"`
	Duration  time.Duration  `json:"duration_ms"`
	Model     string         `json:"model"`
	Weights   AxisWeights    `json:"weights"`
}

// ActionOption represents a valid action the agent can take right now.
type ActionOption struct {
	Action      string   `json:"action"`
	Description string   `json:"description"`
	Targets     []string `json:"targets,omitempty"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/tot/...`
Expected: Success (no errors)

- [ ] **Step 3: Commit**

```bash
git add pkg/tot/types.go
git commit -m "feat(tot): add core Thought Engine data types"
```

---

### Task 2: Action Pre-Filter

**Files:**
- Create: `pkg/tot/filter.go`
- Create: `pkg/tot/filter_test.go`

- [ ] **Step 1: Write the filter tests**

```go
// pkg/tot/filter_test.go
package tot

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestValidActions_Docked(t *testing.T) {
	state := &game.State{
		Doc: true,
		Ship: game.Ship{
			Hull:    80,
			MaxHull: 100,
			Fuel:    50,
			MaxFuel: 100,
		},
	}
	actions := ValidActions(state)
	actionNames := actionSet(actions)

	// Must include docked actions
	for _, want := range []string{"undock", "view_market", "buy", "sell", "repair", "refuel"} {
		if !actionNames[want] {
			t.Errorf("docked state should include %q", want)
		}
	}
	// Must not include space-only actions
	for _, unwant := range []string{"mine", "travel", "jump"} {
		if actionNames[unwant] {
			t.Errorf("docked state should not include %q", unwant)
		}
	}
}

func TestValidActions_InSpace(t *testing.T) {
	state := &game.State{
		Doc: false,
		System: game.SystemData{
			POIs: []game.POI{
				{ID: "asteroid_1", Type: "asteroid_belt", Name: "Belt Alpha"},
				{ID: "station_1", Type: "station", Name: "Station One"},
			},
			Connections: []game.ConnectionInfo{
				{SystemID: "sys_2", Name: "Jump Gate"},
			},
		},
	}
	actions := ValidActions(state)
	actionNames := actionSet(actions)

	for _, want := range []string{"travel", "mine", "dock", "jump", "scan"} {
		if !actionNames[want] {
			t.Errorf("in-space state should include %q", want)
		}
	}
	if actionNames["undock"] {
		t.Errorf("in-space state should not include undock")
	}
}

func TestValidActions_InCombat(t *testing.T) {
	state := &game.State{
		Doc:      false,
		InCombat: true,
	}
	actions := ValidActions(state)
	actionNames := actionSet(actions)

	for _, want := range []string{"battle_advance", "battle_retreat"} {
		if !actionNames[want] {
			t.Errorf("combat state should include %q", want)
		}
	}
}

func TestValidActions_AlwaysIncludesQueries(t *testing.T) {
	state := &game.State{}
	actions := ValidActions(state)
	actionNames := actionSet(actions)

	for _, want := range []string{"get_status", "get_system", "get_cargo"} {
		if !actionNames[want] {
			t.Errorf("should always include query %q", want)
		}
	}
}

func actionSet(actions []ActionOption) map[string]bool {
	m := make(map[string]bool, len(actions))
	for _, a := range actions {
		m[a.Action] = true
	}
	return m
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/tot/ -run TestValidActions -v`
Expected: FAIL — `ValidActions` not defined

- [ ] **Step 3: Implement the filter**

```go
// pkg/tot/filter.go
package tot

import "github.com/rsned/spacemolt/pkg/game"

// ValidActions returns the set of actions available given the current game state.
// This is a code-based pre-filter that removes physically impossible actions
// before the LLM sees them, reducing ~200 commands to ~5-15 relevant ones.
func ValidActions(state *game.State) []ActionOption {
	var actions []ActionOption

	if state.Doc {
		actions = dockedActions(state)
	} else {
		actions = spaceActions(state)
	}

	// Always available (no tick cost — queries)
	actions = append(actions, queryActions()...)

	return actions
}

func dockedActions(state *game.State) []ActionOption {
	actions := []ActionOption{
		{Action: "undock", Description: "Leave the station"},
		{Action: "view_market", Description: "Browse market listings"},
		{Action: "buy", Description: "Buy items from market"},
		{Action: "sell", Description: "Sell items from cargo"},
		{Action: "sell_all", Description: "Sell all cargo"},
		{Action: "view_storage", Description: "View station storage"},
		{Action: "deposit_credits", Description: "Deposit credits to storage"},
		{Action: "withdraw_items", Description: "Withdraw items from storage"},
		{Action: "list_ships", Description: "List owned ships"},
		{Action: "switch_ship", Description: "Switch to another ship"},
		{Action: "get_recipes", Description: "View crafting recipes"},
		{Action: "craft", Description: "Craft an item from recipe"},
		{Action: "get_missions", Description: "View available missions"},
		{Action: "complete_mission", Description: "Complete a mission"},
	}

	if state.Ship.Hull < state.Ship.MaxHull {
		actions = append(actions, ActionOption{Action: "repair", Description: "Repair hull damage"})
	}
	if state.Ship.Fuel < state.Ship.MaxFuel {
		actions = append(actions, ActionOption{Action: "refuel", Description: "Refuel the ship"})
	}

	return actions
}

func spaceActions(state *game.State) []ActionOption {
	var actions []ActionOption

	// Always available in space
	actions = append(actions, ActionOption{Action: "scan", Description: "Scan the area"})

	// Travel to POIs
	if len(state.System.POIs) > 0 {
		targets := make([]string, 0, len(state.System.POIs))
		for _, poi := range state.System.POIs {
			targets = append(targets, poi.ID)
		}
		actions = append(actions, ActionOption{
			Action:      "travel",
			Description: "Travel to a point of interest",
			Targets:     targets,
		})
	}

	// Dock at stations
	for _, poi := range state.System.POIs {
		if poi.Type == "station" || poi.Type == "outpost" {
			actions = append(actions, ActionOption{
				Action:      "dock",
				Description: "Dock at " + poi.Name,
				Targets:     []string{poi.ID},
			})
			break // one dock action is enough
		}
	}

	// Jump via connections
	if len(state.System.Connections) > 0 {
		targets := make([]string, 0, len(state.System.Connections))
		for _, conn := range state.System.Connections {
			targets = append(targets, conn.SystemID)
		}
		actions = append(actions, ActionOption{
			Action:      "jump",
			Description: "Jump to connected system",
			Targets:     targets,
		})
	}

	// Mine at asteroid belts
	for _, poi := range state.System.POIs {
		if poi.Type == "asteroid_belt" || poi.Type == "asteroid" {
			actions = append(actions, ActionOption{
				Action:      "mine",
				Description: "Mine resources at " + poi.Name,
			})
			break
		}
	}

	// Combat actions
	if state.InCombat {
		actions = append(actions,
			ActionOption{Action: "battle_advance", Description: "Close distance with enemy"},
			ActionOption{Action: "battle_retreat", Description: "Retreat from combat"},
			ActionOption{Action: "battle_stance", Description: "Change combat stance"},
			ActionOption{Action: "battle_target", Description: "Target an enemy"},
		)
	}

	return actions
}

func queryActions() []ActionOption {
	return []ActionOption{
		{Action: "get_status", Description: "Check current status"},
		{Action: "get_system", Description: "Get current system info"},
		{Action: "get_cargo", Description: "Check cargo hold"},
		{Action: "get_skills", Description: "View skill levels"},
		{Action: "get_map", Description: "View known map"},
		{Action: "get_nearby", Description: "Check nearby players"},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/tot/ -run TestValidActions -v`
Expected: PASS (all 4 tests)

- [ ] **Step 5: Run linter**

Run: `golangci-lint run ./pkg/tot/...`
Expected: No new findings

- [ ] **Step 6: Commit**

```bash
git add pkg/tot/filter.go pkg/tot/filter_test.go
git commit -m "feat(tot): add code-based action pre-filter"
```

---

### Task 3: Personality-to-Weights Derivation

**Files:**
- Create: `pkg/tot/weights.go`
- Create: `pkg/tot/weights_test.go`

- [ ] **Step 1: Write weight derivation tests**

```go
// pkg/tot/weights_test.go
package tot

import (
	"math"
	"testing"

	"github.com/rsned/spacemolt/pkg/agent"
)

func TestDeriveWeights_Miner(t *testing.T) {
	p := agent.Personality{
		Role: "Miner",
		Traits: map[string]float64{
			"caution":        0.5,
			"risk_tolerance": 0.5, // inverted in derivation
			"hard_work":      0.95,
			"perseverance":   0.9,
		},
		Motivations: agent.Motivations{
			Primary: "mine_resources",
			Weights: map[string]float64{
				"mine_resources": 0.9,
				"survival":       0.65,
			},
		},
	}
	w := DeriveWeights(p)

	// Survival should be moderate (0.65 survival weight + 0.5 caution + 0.5 inverted risk)
	if w.Survival < 0.4 || w.Survival > 0.8 {
		t.Errorf("miner survival weight %.2f out of expected range [0.4, 0.8]", w.Survival)
	}
	// Profit should be high (mine_resources 0.9)
	if w.Profit < 0.5 {
		t.Errorf("miner profit weight %.2f should be >= 0.5", w.Profit)
	}
	// All weights should be between 0 and 1
	for _, v := range []float64{w.Survival, w.Profit, w.GoalProgress, w.Risk, w.Efficiency} {
		if v < 0 || v > 1 {
			t.Errorf("weight %.2f out of [0, 1] range", v)
		}
	}
}

func TestDeriveWeights_Pirate(t *testing.T) {
	p := agent.Personality{
		Role: "Pirate",
		Traits: map[string]float64{
			"aggression":     0.9,
			"risk_tolerance": 0.8,
			"greed":          0.85,
			"cunning":        0.75,
		},
		Motivations: agent.Motivations{
			Primary: "plunder",
			Weights: map[string]float64{
				"plunder":  0.9,
				"survival": 0.6,
			},
		},
	}
	w := DeriveWeights(p)

	// Pirate should have lower survival than miner
	if w.Survival > 0.5 {
		t.Errorf("pirate survival weight %.2f should be < 0.5", w.Survival)
	}
	// Risk aversion should be low (high risk tolerance, high aggression)
	if w.Risk > 0.4 {
		t.Errorf("pirate risk weight %.2f should be < 0.4", w.Risk)
	}
}

func TestWeightedScore(t *testing.T) {
	w := AxisWeights{
		Survival: 0.5, Profit: 0.5, GoalProgress: 0.5, Risk: 0.5, Efficiency: 0.5,
	}
	s := AxisScores{
		Survival: 80, Profit: 60, GoalProgress: 40, Risk: 70, Efficiency: 50,
	}
	got := w.WeightedScore(s)
	want := 60.0 // (80+60+40+70+50)/5
	if math.Abs(got-want) > 0.01 {
		t.Errorf("WeightedScore = %.2f, want %.2f", got, want)
	}
}

func TestWeightedScore_ZeroWeights(t *testing.T) {
	w := AxisWeights{}
	s := AxisScores{Survival: 80}
	got := w.WeightedScore(s)
	if got != 0 {
		t.Errorf("WeightedScore with zero weights = %.2f, want 0", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/tot/ -run TestDeriveWeights -v`
Expected: FAIL — `DeriveWeights` not defined

- [ ] **Step 3: Implement weight derivation**

```go
// pkg/tot/weights.go
package tot

import "github.com/rsned/spacemolt/pkg/agent"

// DeriveWeights computes scoring axis weights from an agent's personality.
// Each axis is a blend of relevant traits and motivation weights.
func DeriveWeights(p agent.Personality) AxisWeights {
	return AxisWeights{
		Survival:     blend(p.Motivations.Weights["survival"], trait(p, "caution"), 1.0-trait(p, "risk_tolerance")),
		Profit:       blend(p.Motivations.Weights[profitMotivation(p)], trait(p, "greed"), trait(p, "ambition")),
		GoalProgress: blend(p.Motivations.Weights[p.Motivations.Primary], trait(p, "determination"), trait(p, "perseverance")),
		Risk:         blend(1.0-trait(p, "risk_tolerance"), trait(p, "caution"), 1.0-trait(p, "aggression")),
		Efficiency:   blend(1.0-trait(p, "patience"), trait(p, "discipline"), 0.5),
	}
}

// profitMotivation maps role-specific motivation keys to the Profit axis.
func profitMotivation(p agent.Personality) string {
	candidates := []string{
		"mine_resources", "maximize_profit", "plunder",
		"complete_contracts", "explore_unknown",
	}
	for _, c := range candidates {
		if _, ok := p.Motivations.Weights[c]; ok {
			return c
		}
	}
	return p.Motivations.Primary
}

// trait returns a personality trait value, defaulting to 0.5 if not set.
func trait(p agent.Personality, name string) float64 {
	if v, ok := p.Traits[name]; ok {
		return v
	}
	return 0.5
}

// blend averages all provided values that are non-zero, clamped to [0, 1].
func blend(values ...float64) float64 {
	var sum float64
	var count int
	for _, v := range values {
		if v != 0 {
			sum += v
			count++
		}
	}
	if count == 0 {
		return 0.5
	}
	result := sum / float64(count)
	if result > 1.0 {
		return 1.0
	}
	if result < 0.0 {
		return 0.0
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/tot/ -v`
Expected: PASS (all tests)

- [ ] **Step 5: Run linter**

Run: `golangci-lint run ./pkg/tot/...`
Expected: No new findings

- [ ] **Step 6: Commit**

```bash
git add pkg/tot/weights.go pkg/tot/weights_test.go
git commit -m "feat(tot): add personality-to-weights derivation"
```

---

## Chunk 2: Prompt Templates and Evaluator

### Task 4: Prompt Templates

**Files:**
- Create: `data/prompts/templates/tot/assess.v1.tmpl`
- Create: `data/prompts/templates/tot/evaluate.v1.tmpl`

- [ ] **Step 1: Create the assess template (Stage 1)**

```
{{/* data/prompts/templates/tot/assess.v1.tmpl */}}
You are {{.AgentName}}, a {{.Role}}.

CURRENT SITUATION:
- System: {{.State.SystemName}} ({{.State.SystemID}})
{{- if .State.IsDocked}}
- Docked at: {{.State.DockedAt}}
{{- end}}
- Fuel: {{printf "%.0f" .State.FuelPercent}}%
- Hull: {{printf "%.0f" .State.HullPercent}}%
- Cargo: {{.State.CargoCount}}/{{.State.MaxCargo}}
- Credits: {{printf "%.0f" .State.Credits}}
{{- if .State.InCombat}}
- ⚠ IN COMBAT
{{- end}}
{{- if gt .State.NearbyHostiles 0}}
- ⚠ {{.State.NearbyHostiles}} hostile(s) nearby
{{- end}}

{{- if .Knowledge.POIsInSystem}}
NEARBY LOCATIONS:
{{- range .Knowledge.POIsInSystem}}
  - {{.ID}}: {{.Name}} ({{.Type}})
{{- end}}
{{- end}}

{{- if .LastFeedback}}
LAST ACTION: {{.LastFeedback.Action}} → {{if .LastFeedback.Success}}success{{else}}failed: {{.LastFeedback.Error}}{{end}}
{{- end}}

{{- if .Goal}}
CURRENT GOAL: {{.Goal.Type}} — {{.Goal.Target}} ({{printf "%.0f" (mul .Goal.Progress 100)}}% complete)
{{- end}}

AVAILABLE ACTIONS:
{{- range .System.ValidActions}}
  - {{.Action}}: {{.Description}}{{if .Targets}} [targets: {{join ", " .Targets}}]{{end}}
{{- end}}

Analyze the situation and list 3-5 viable options you could take right now.
For each option, explain briefly why it's worth considering.

Respond in this exact JSON format:
{"situation":"one sentence summary","options":[{"action":"action_name","target":"target_id_or_empty","rationale":"why this option"}]}
```

- [ ] **Step 2: Create the evaluate template (Stage 2)**

```
{{/* data/prompts/templates/tot/evaluate.v1.tmpl */}}
You are {{.AgentName}}, a {{.Role}}.

SITUATION: {{.Situation}}

You are considering this action: {{.Option.Action}}{{if .Option.Target}} targeting {{.Option.Target}}{{end}}
Rationale: {{.Option.Rationale}}

CURRENT STATE:
- Fuel: {{printf "%.0f" .State.FuelPercent}}% | Hull: {{printf "%.0f" .State.HullPercent}}%
- Cargo: {{.State.CargoCount}}/{{.State.MaxCargo}} | Credits: {{printf "%.0f" .State.Credits}}
{{- if .State.InCombat}}
- ⚠ IN COMBAT
{{- end}}
{{- if gt .State.NearbyHostiles 0}}
- ⚠ {{.State.NearbyHostiles}} hostile(s) nearby
{{- end}}

Evaluate this action on these 5 criteria (score each 0-100):
1. survival: How well does this keep me alive? (hull, fuel, escape from threats)
2. profit: Does this earn credits or acquire valuable resources?
3. goal_progress: Does this advance my current goal?
4. risk: How safe is this choice? (100 = very safe, 0 = very dangerous)
5. efficiency: Am I spending my time wisely with this action?

Also suggest what logical next step would follow this action.

Respond in this exact JSON format:
{"action":"{{.Option.Action}}","target":"{{.Option.Target}}","analysis":"2-3 sentence evaluation","scores":{"survival":0,"profit":0,"goal_progress":0,"risk":0,"efficiency":0},"next_step":{"action":"next_action","target":"next_target"}}
```

- [ ] **Step 3: Verify templates parse**

Run: `go build ./pkg/tot/...`
Expected: Success (templates are loaded at runtime, not compile time, but this ensures the package still builds)

- [ ] **Step 4: Commit**

```bash
git add data/prompts/templates/tot/
git commit -m "feat(tot): add assess and evaluate prompt templates"
```

---

### Task 5: Prompt Builder

**Files:**
- Create: `pkg/tot/prompts.go`

- [ ] **Step 1: Create the prompt builder**

This builds prompts for each stage. It can use the template system if available, or construct prompts directly. The template context is extended to include ToT-specific fields.

```go
// pkg/tot/prompts.go
package tot

import (
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/prompts"
)

// AssessContext holds data for Stage 1 prompt rendering.
type AssessContext struct {
	*prompts.TemplateContext
	ValidActions []ActionOption
}

// EvaluateContext holds data for Stage 2 prompt rendering.
type EvaluateContext struct {
	AgentName string
	Role      string
	Situation string
	Option    AssessOption
	State     *prompts.StateContext
}

// AssessOption is one option returned by Stage 1.
type AssessOption struct {
	Action    string `json:"action"`
	Target    string `json:"target"`
	Rationale string `json:"rationale"`
}

// AssessResponse is the parsed Stage 1 LLM output.
type AssessResponse struct {
	Situation string         `json:"situation"`
	Options   []AssessOption `json:"options"`
}

// EvaluateResponse is the parsed Stage 2 LLM output.
type EvaluateResponse struct {
	Action   string     `json:"action"`
	Target   string     `json:"target"`
	Analysis string     `json:"analysis"`
	Scores   AxisScores `json:"scores"`
	NextStep struct {
		Action string `json:"action"`
		Target string `json:"target"`
	} `json:"next_step"`
}

// BuildAssessPrompt constructs the Stage 1 prompt.
func BuildAssessPrompt(ag agent.Agent, state *game.State, validActions []ActionOption) string {
	p := ag.Personality()

	var sb strings.Builder
	fmt.Fprintf(&sb, "You are %s, a %s.\n\n", p.Name, p.Role)

	sb.WriteString("CURRENT SITUATION:\n")
	fmt.Fprintf(&sb, "- System: %s (%s)\n", state.System.Name, state.System.ID)
	if state.Doc {
		fmt.Fprintf(&sb, "- Docked at: %s\n", state.CurrentPOI)
	}
	if state.MaxFuel > 0 {
		fmt.Fprintf(&sb, "- Fuel: %.0f%%\n", state.Fuel/state.MaxFuel*100)
	}
	if state.MaxHull > 0 {
		fmt.Fprintf(&sb, "- Hull: %.0f%%\n", state.Hull/state.MaxHull*100)
	}
	fmt.Fprintf(&sb, "- Cargo: %d/%d\n", len(state.Cargo), state.MaxCargo)
	fmt.Fprintf(&sb, "- Credits: %.0f\n", state.Credits)
	if state.InCombat {
		sb.WriteString("- ⚠ IN COMBAT\n")
	}
	hostiles := countHostiles(state)
	if hostiles > 0 {
		fmt.Fprintf(&sb, "- ⚠ %d hostile(s) nearby\n", hostiles)
	}

	if len(state.System.POIs) > 0 {
		sb.WriteString("\nNEARBY LOCATIONS:\n")
		for _, poi := range state.System.POIs {
			fmt.Fprintf(&sb, "  - %s: %s (%s)\n", poi.ID, poi.Name, poi.Type)
		}
	}

	if len(state.System.Connections) > 0 {
		sb.WriteString("\nCONNECTED SYSTEMS:\n")
		for _, conn := range state.System.Connections {
			fmt.Fprintf(&sb, "  - %s: %s\n", conn.SystemID, conn.Name)
		}
	}

	sb.WriteString("\nAVAILABLE ACTIONS:\n")
	for _, a := range validActions {
		fmt.Fprintf(&sb, "  - %s: %s", a.Action, a.Description)
		if len(a.Targets) > 0 {
			fmt.Fprintf(&sb, " [targets: %s]", strings.Join(a.Targets, ", "))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\nAnalyze the situation and list 3-5 viable options you could take right now.\n")
	sb.WriteString("For each option, explain briefly why it's worth considering.\n\n")
	sb.WriteString("Respond in this exact JSON format:\n")
	sb.WriteString(`{"situation":"one sentence summary","options":[{"action":"action_name","target":"target_id_or_empty","rationale":"why this option"}]}`)
	sb.WriteString("\n")

	return sb.String()
}

// BuildEvaluatePrompt constructs a Stage 2 prompt for one option.
func BuildEvaluatePrompt(ag agent.Agent, state *game.State, situation string, option AssessOption) string {
	p := ag.Personality()

	var sb strings.Builder
	fmt.Fprintf(&sb, "You are %s, a %s.\n\n", p.Name, p.Role)
	fmt.Fprintf(&sb, "SITUATION: %s\n\n", situation)
	fmt.Fprintf(&sb, "You are considering this action: %s", option.Action)
	if option.Target != "" {
		fmt.Fprintf(&sb, " targeting %s", option.Target)
	}
	fmt.Fprintf(&sb, "\nRationale: %s\n\n", option.Rationale)

	sb.WriteString("CURRENT STATE:\n")
	if state.MaxFuel > 0 {
		fmt.Fprintf(&sb, "- Fuel: %.0f%%", state.Fuel/state.MaxFuel*100)
	}
	if state.MaxHull > 0 {
		fmt.Fprintf(&sb, " | Hull: %.0f%%", state.Hull/state.MaxHull*100)
	}
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "- Cargo: %d/%d | Credits: %.0f\n", len(state.Cargo), state.MaxCargo, state.Credits)
	if state.InCombat {
		sb.WriteString("- ⚠ IN COMBAT\n")
	}
	hostiles := countHostiles(state)
	if hostiles > 0 {
		fmt.Fprintf(&sb, "- ⚠ %d hostile(s) nearby\n", hostiles)
	}

	sb.WriteString("\nEvaluate this action on these 5 criteria (score each 0-100):\n")
	sb.WriteString("1. survival: How well does this keep me alive? (hull, fuel, escape from threats)\n")
	sb.WriteString("2. profit: Does this earn credits or acquire valuable resources?\n")
	sb.WriteString("3. goal_progress: Does this advance my current goal?\n")
	sb.WriteString("4. risk: How safe is this choice? (100 = very safe, 0 = very dangerous)\n")
	sb.WriteString("5. efficiency: Am I spending my time wisely with this action?\n")
	sb.WriteString("\nAlso suggest what logical next step would follow this action.\n\n")
	sb.WriteString("Respond in this exact JSON format:\n")
	fmt.Fprintf(&sb, `{"action":"%s","target":"%s","analysis":"2-3 sentence evaluation","scores":{"survival":0,"profit":0,"goal_progress":0,"risk":0,"efficiency":0},"next_step":{"action":"next_action","target":"next_target"}}`, option.Action, option.Target)
	sb.WriteString("\n")

	return sb.String()
}

func countHostiles(state *game.State) int {
	count := 0
	for _, n := range state.Nearby {
		if n.Hostile {
			count++
		}
	}
	return count
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/tot/...`
Expected: Success

Note: Check the `NearbyPlayer` struct in `pkg/game/types.go` for the `Hostile` field name. If the field is named differently (e.g., `IsHostile` or doesn't exist), adjust `countHostiles()` accordingly. The `ConnectionInfo` struct's `Name` field should also be verified — check `pkg/game/types.go` for the exact field.

- [ ] **Step 3: Run linter**

Run: `golangci-lint run ./pkg/tot/...`
Expected: No new findings

- [ ] **Step 4: Commit**

```bash
git add pkg/tot/prompts.go
git commit -m "feat(tot): add prompt builders for assess and evaluate stages"
```

---

### Task 6: Pipeline Evaluator

**Files:**
- Create: `pkg/tot/evaluator.go`
- Create: `pkg/tot/evaluator_test.go`

- [ ] **Step 1: Write evaluator tests**

```go
// pkg/tot/evaluator_test.go
package tot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
)

// mockLLM implements a minimal LLM interface for testing.
type mockLLM struct {
	responses []string // queued responses, returned in order
	idx       int
}

func (m *mockLLM) Generate(ctx context.Context, prompt string) (string, error) {
	if m.idx >= len(m.responses) {
		return m.responses[len(m.responses)-1], nil
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func TestEvaluator_FullPipeline(t *testing.T) {
	assessResp := AssessResponse{
		Situation: "Mining in low-sec, pirates nearby",
		Options: []AssessOption{
			{Action: "travel", Target: "jump_gate_01", Rationale: "Flee to safety"},
			{Action: "mine", Target: "", Rationale: "Keep mining"},
		},
	}
	assessJSON, _ := json.Marshal(assessResp)

	evalResp1 := EvaluateResponse{
		Action:   "travel",
		Target:   "jump_gate_01",
		Analysis: "Fleeing preserves cargo",
		Scores:   AxisScores{Survival: 90, Profit: 40, GoalProgress: 30, Risk: 85, Efficiency: 35},
	}
	evalResp1.NextStep.Action = "jump"
	evalResp1.NextStep.Target = "safe_sys"
	evalJSON1, _ := json.Marshal(evalResp1)

	evalResp2 := EvaluateResponse{
		Action:   "mine",
		Target:   "",
		Analysis: "Risky but profitable",
		Scores:   AxisScores{Survival: 30, Profit: 80, GoalProgress: 70, Risk: 20, Efficiency: 60},
	}
	evalJSON2, _ := json.Marshal(evalResp2)

	llm := &mockLLM{
		responses: []string{
			string(assessJSON),
			string(evalJSON1),
			string(evalJSON2),
		},
	}

	personality := agent.Personality{
		Name: "Test Miner",
		Role: "Miner",
		Traits: map[string]float64{
			"caution":        0.7,
			"risk_tolerance": 0.3,
		},
		Motivations: agent.Motivations{
			Primary: "mine_resources",
			Weights: map[string]float64{
				"mine_resources": 0.9,
				"survival":       0.7,
			},
		},
	}

	weights := DeriveWeights(personality)

	eval := NewEvaluator(llm, "test-model")
	state := &game.State{
		Doc:      false,
		InCombat: false,
		System: game.SystemData{
			Name: "Test System",
			ID:   "test_sys",
		},
	}
	validActions := []ActionOption{
		{Action: "travel", Description: "Travel"},
		{Action: "mine", Description: "Mine"},
	}

	tree, err := eval.Evaluate(ctx(t), personality, state, validActions, weights)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if tree.WinnerID == "" {
		t.Error("expected a winner to be selected")
	}
	if len(tree.Root) != 2 {
		t.Errorf("expected 2 root branches, got %d", len(tree.Root))
	}

	// With high survival weights, travel should win
	var winner *ThoughtNode
	for _, n := range tree.Root {
		if n.Status == StatusWinner {
			winner = n
		}
	}
	if winner == nil {
		t.Fatal("no winner node found")
	}
	if winner.Action != "travel" {
		t.Errorf("expected travel to win for cautious miner, got %s", winner.Action)
	}
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/tot/ -run TestEvaluator -v`
Expected: FAIL — `NewEvaluator` not defined

- [ ] **Step 3: Implement the evaluator**

```go
// pkg/tot/evaluator.go
package tot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
)

// LLMGenerator is the interface for making LLM calls.
// This is satisfied by any client that can generate text from a prompt.
type LLMGenerator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// Evaluator orchestrates the three-stage Thought Engine pipeline.
type Evaluator struct {
	llm   LLMGenerator
	model string
}

// NewEvaluator creates a new Thought Engine evaluator.
func NewEvaluator(llm LLMGenerator, model string) *Evaluator {
	return &Evaluator{llm: llm, model: model}
}

// Evaluate runs the full pipeline: Assess → Evaluate (parallel) → Select.
func (e *Evaluator) Evaluate(
	ctx context.Context,
	personality agent.Personality,
	state *game.State,
	validActions []ActionOption,
	weights AxisWeights,
) (*ThoughtTree, error) {
	start := time.Now()

	tree := &ThoughtTree{
		ID:        fmt.Sprintf("tot_%d", start.UnixMilli()),
		AgentID:   personality.ID,
		Timestamp: start,
		Model:     e.model,
		Weights:   weights,
	}

	// Stage 1: Assess — generate options
	assessPrompt := BuildAssessPrompt(agentFromPersonality(personality), state, validActions)
	assessStart := time.Now()
	assessRaw, err := e.llm.Generate(ctx, assessPrompt)
	if err != nil {
		return nil, fmt.Errorf("stage 1 assess: %w", err)
	}
	assessDuration := time.Since(assessStart)

	assessed, err := parseAssessResponse(assessRaw)
	if err != nil {
		return nil, fmt.Errorf("stage 1 parse: %w", err)
	}
	tree.Situation = assessed.Situation

	if len(assessed.Options) == 0 {
		return nil, fmt.Errorf("stage 1 returned no options")
	}

	// Stage 2: Evaluate — score each option in parallel
	nodes := make([]*ThoughtNode, len(assessed.Options))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var evalErr error

	for i, opt := range assessed.Options {
		wg.Add(1)
		go func(idx int, option AssessOption) {
			defer wg.Done()

			evalPrompt := BuildEvaluatePrompt(agentFromPersonality(personality), state, assessed.Situation, option)
			evalStart := time.Now()
			evalRaw, err := e.llm.Generate(ctx, evalPrompt)
			evalDuration := time.Since(evalStart)

			if err != nil {
				mu.Lock()
				evalErr = fmt.Errorf("stage 2 evaluate %q: %w", option.Action, err)
				mu.Unlock()
				return
			}

			evalResp, err := parseEvaluateResponse(evalRaw)
			if err != nil {
				mu.Lock()
				evalErr = fmt.Errorf("stage 2 parse %q: %w", option.Action, err)
				mu.Unlock()
				return
			}

			node := &ThoughtNode{
				ID:          fmt.Sprintf("node_%d", idx),
				Action:      option.Action,
				Target:      option.Target,
				Reasoning:   evalResp.Analysis,
				Scores:      evalResp.Scores,
				Combined:    weights.WeightedScore(evalResp.Scores),
				Status:      StatusActive,
				Depth:       0,
				EvalTime:    evalDuration,
				Prompt:      evalPrompt,
				RawResponse: evalRaw,
			}

			// Add next_step as a child node if present
			if evalResp.NextStep.Action != "" {
				child := &ThoughtNode{
					ID:     fmt.Sprintf("node_%d_next", idx),
					Action: evalResp.NextStep.Action,
					Target: evalResp.NextStep.Target,
					Status: StatusActive,
					Depth:  1,
				}
				node.Children = append(node.Children, child)
			}

			mu.Lock()
			nodes[idx] = node
			mu.Unlock()
		}(i, opt)
	}
	wg.Wait()

	if evalErr != nil {
		return nil, evalErr
	}

	// Filter out nil nodes (shouldn't happen but defensive)
	tree.Root = make([]*ThoughtNode, 0, len(nodes))
	for _, n := range nodes {
		if n != nil {
			tree.Root = append(tree.Root, n)
		}
	}

	// Stage 3: Select — pick winner deterministically
	e.selectWinner(tree)

	tree.Duration = time.Since(start)
	return tree, nil
}

// selectWinner marks the highest-scoring branch as winner and prunes the rest.
func (e *Evaluator) selectWinner(tree *ThoughtTree) {
	if len(tree.Root) == 0 {
		return
	}

	var best *ThoughtNode
	for _, n := range tree.Root {
		if best == nil || n.Combined > best.Combined {
			best = n
		}
	}

	for _, n := range tree.Root {
		if n == best {
			n.Status = StatusWinner
			tree.WinnerID = n.ID
			for _, child := range n.Children {
				child.Status = StatusWinner
			}
		} else {
			n.Status = StatusPruned
			for _, child := range n.Children {
				child.Status = StatusPruned
			}
		}
	}
}

// ToDecision converts the winning branch to an agent.Decision for execution.
func (tree *ThoughtTree) ToDecision() agent.Decision {
	var winner *ThoughtNode
	for _, n := range tree.Root {
		if n.Status == StatusWinner {
			winner = n
			break
		}
	}
	if winner == nil {
		return agent.Decision{Action: "get_status", Reasoning: "no winner in thought tree"}
	}

	d := agent.Decision{
		Action:     winner.Action,
		Target:     winner.Target,
		Reasoning:  winner.Reasoning,
		Confidence: winner.Combined / 100.0, // normalize 0-100 to 0-1
	}

	// Convert next_step children to PlannedActions
	for i, child := range winner.Children {
		d.PlannedActions = append(d.PlannedActions, agent.PlannedAction{
			Sequence:  i + 1,
			Action:    child.Action,
			Target:    child.Target,
			Reasoning: "next step from thought engine",
		})
	}

	return d
}

// parseAssessResponse extracts the AssessResponse from LLM output.
func parseAssessResponse(raw string) (*AssessResponse, error) {
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in assess response")
	}
	var resp AssessResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("parse assess JSON: %w", err)
	}
	return &resp, nil
}

// parseEvaluateResponse extracts the EvaluateResponse from LLM output.
func parseEvaluateResponse(raw string) (*EvaluateResponse, error) {
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in evaluate response")
	}
	var resp EvaluateResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("parse evaluate JSON: %w", err)
	}
	return &resp, nil
}

// extractJSON finds the last JSON object in a string.
// LLMs often output explanation text before the actual JSON.
func extractJSON(s string) string {
	// Find all { } balanced blocks, return the last one
	var lastJSON string
	depth := 0
	start := -1

	for i, c := range s {
		switch c {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				lastJSON = s[start : i+1]
				start = -1
			}
		}
	}
	return lastJSON
}

// agentFromPersonality creates a minimal agent adapter for prompt building.
type personalityAgent struct {
	p agent.Personality
}

func agentFromPersonality(p agent.Personality) *personalityAgent {
	return &personalityAgent{p: p}
}

func (a *personalityAgent) Personality() agent.Personality { return a.p }
```

Note: The `BuildAssessPrompt` and `BuildEvaluatePrompt` functions in `prompts.go` take an `agent.Agent` interface, but the evaluator only has a `Personality`. We need a thin adapter. Check if `BuildAssessPrompt` actually needs the full Agent interface or just the Personality. If it only needs Personality, change the function signatures in `prompts.go` to accept `agent.Personality` directly instead of `agent.Agent`. That's simpler and avoids the adapter.

- [ ] **Step 4: Adjust prompts.go signatures to take Personality instead of Agent**

Update `BuildAssessPrompt` and `BuildEvaluatePrompt` in `pkg/tot/prompts.go`:
- Change parameter from `ag agent.Agent` to `p agent.Personality`
- Replace `ag.Personality()` calls with direct `p` usage
- Remove the `personalityAgent` adapter from `evaluator.go`

In `evaluator.go`, call them as:
```go
assessPrompt := BuildAssessPrompt(personality, state, validActions)
evalPrompt := BuildEvaluatePrompt(personality, state, assessed.Situation, option)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/tot/ -v`
Expected: PASS (all tests including the full pipeline test)

- [ ] **Step 6: Run linter**

Run: `golangci-lint run ./pkg/tot/...`
Expected: No new findings

- [ ] **Step 7: Commit**

```bash
git add pkg/tot/evaluator.go pkg/tot/evaluator_test.go pkg/tot/prompts.go
git commit -m "feat(tot): add pipeline evaluator with parallel branch evaluation"
```

---

## Chunk 3: Runner Integration

### Task 7: Add DecisionMode to Personality

**Files:**
- Modify: `pkg/agent/agent.go` (Personality struct, ~line 44)

- [ ] **Step 1: Add DecisionMode field**

In `pkg/agent/agent.go`, add to the `Personality` struct:

```go
DecisionMode string `yaml:"decision_mode,omitempty" json:"decision_mode,omitempty"` // "tot" or "" (default single-call)
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add pkg/agent/agent.go
git commit -m "feat(agent): add DecisionMode field to Personality"
```

---

### Task 8: Add LLMGenerator Interface Adapter

**Files:**
- Create: `pkg/tot/llm_adapter.go`

The existing `llm.Client` has a `Decide()` method but not a generic `Generate()`. We need a thin adapter or add a `Generate()` method. Check `pkg/llm/client.go` for existing methods.

- [ ] **Step 1: Check if llm.Client has a Generate method**

Read `pkg/llm/client.go` and look for a `Generate` or `Complete` method that takes a prompt and returns raw text. If it exists, use it. If not, create an adapter.

- [ ] **Step 2: Create adapter if needed**

```go
// pkg/tot/llm_adapter.go
package tot

import (
	"context"

	"github.com/rsned/spacemolt/pkg/llm"
)

// LLMClientAdapter wraps an llm.Client to implement LLMGenerator.
type LLMClientAdapter struct {
	Client *llm.Client
}

// Generate sends a prompt to Ollama and returns the raw text response.
func (a *LLMClientAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	return a.Client.Generate(ctx, prompt)
}
```

If `llm.Client` doesn't have `Generate()`, add it to `pkg/llm/client.go`:

```go
// Generate sends a raw prompt to the LLM and returns the response text.
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	payload := map[string]interface{}{
		"model":  c.model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]interface{}{
			"temperature": 0.7,
			"num_predict": 4096,
			"num_ctx":     16384,
		},
		"format": "json",
	}
	// ... same HTTP request logic as Decide, but return raw response text
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./pkg/tot/... ./pkg/llm/...`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add pkg/tot/llm_adapter.go pkg/llm/client.go
git commit -m "feat(tot): add LLMGenerator adapter for Ollama client"
```

---

### Task 9: Integrate ToT into Runner

**Files:**
- Modify: `pkg/agent/runner.go` (~line 193, executeCycle method)

- [ ] **Step 1: Add ToT fields to Runner struct**

In `pkg/agent/runner.go`, add fields to the `Runner` struct:

```go
totEvaluator *tot.Evaluator  // nil if ToT not enabled
```

And in `NewRunner`, after creating the runner, check if ToT should be enabled:

```go
// After creating the runner:
if agent.Personality().DecisionMode == "tot" {
    // ToT evaluator will be set via SetToTEvaluator()
}
```

Add a setter method:

```go
// SetToTEvaluator enables Tree-of-Thought decision making for this runner.
func (r *Runner) SetToTEvaluator(eval *tot.Evaluator) {
    r.totEvaluator = eval
}
```

- [ ] **Step 2: Add ToT path in executeCycle**

In `executeCycle()`, before the existing `r.agent.Decide()` call, add the ToT path:

```go
// Replace the existing decide block with:
var decision Decision
if r.totEvaluator != nil && r.agent.Personality().DecisionMode == "tot" {
    validActions := tot.ValidActions(stateCopy)
    weights := tot.DeriveWeights(r.agent.Personality())
    tree, totErr := r.totEvaluator.Evaluate(ctx, r.agent.Personality(), stateCopy, validActions, weights)
    if totErr != nil {
        r.logger.Printf("[%s] ToT failed, falling back to single-call: %v", r.agent.ID(), totErr)
        decision, err = r.agent.Decide(ctx, stateCopy)
    } else {
        decision = tree.ToDecision()
        // Emit thought tree event for visualization
        r.emitEvent("thought_tree", tree)
    }
} else {
    decision, err = r.agent.Decide(ctx, stateCopy)
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 4: Run existing tests**

Run: `go test ./pkg/agent/... -v`
Expected: PASS (existing tests should be unaffected since ToT is opt-in)

- [ ] **Step 5: Commit**

```bash
git add pkg/agent/runner.go
git commit -m "feat(agent): integrate Thought Engine into runner decision loop"
```

---

## Chunk 4: Observer Integration

### Task 10: Add thought_tree Event to Observer

**Files:**
- Modify: `pkg/observe/observer.go`

- [ ] **Step 1: Check how events currently flow from runner to observer**

Read `pkg/observe/observer.go` and `pkg/observe/session.go` to understand how events are broadcast. The runner emits events via `SetEventCallback()`. The observer needs to relay `thought_tree` events to browser clients.

The `thought_tree` event should be broadcast as a new `ServerMessage` type. In the observer's event handler (or wherever the runner callback is wired up), add handling for the `"thought_tree"` event type:

```go
case "thought_tree":
    msg := ServerMessage{
        Type:    "thought_tree",
        Agent:   agentID,
        Message: mustMarshal(data), // data is the *tot.ThoughtTree
    }
    s.BrowserHub.Broadcast(agentID, msg)
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/observe/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add pkg/observe/
git commit -m "feat(observe): relay thought_tree events to browser clients"
```

---

## Chunk 5: Frontend Visualization

### Task 11: Install React Flow

**Files:**
- Modify: `frontend/package.json`

- [ ] **Step 1: Install @xyflow/react**

Run: `cd frontend && npm install @xyflow/react`

- [ ] **Step 2: Verify it installs**

Run: `cd frontend && npm run build`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add frontend/package.json frontend/package-lock.json
git commit -m "chore: add @xyflow/react dependency for thought tree visualization"
```

---

### Task 12: Radar Chart Component

**Files:**
- Create: `frontend/src/components/RadarChart.tsx`

- [ ] **Step 1: Create the SVG radar chart**

```tsx
// frontend/src/components/RadarChart.tsx

interface RadarChartProps {
  scores: {
    survival: number
    profit: number
    goal_progress: number
    risk: number
    efficiency: number
  }
  size?: number
}

const AXES = ['survival', 'profit', 'goal_progress', 'risk', 'efficiency'] as const
const LABELS = ['SRV', 'PRF', 'GOL', 'RSK', 'EFF']

export function RadarChart({ scores, size = 80 }: RadarChartProps) {
  const cx = size / 2
  const cy = size / 2
  const r = size / 2 - 10

  const angleStep = (2 * Math.PI) / AXES.length
  const startAngle = -Math.PI / 2

  const points = AXES.map((axis, i) => {
    const value = (scores[axis] || 0) / 100
    const angle = startAngle + i * angleStep
    return {
      x: cx + r * value * Math.cos(angle),
      y: cy + r * value * Math.sin(angle),
      lx: cx + (r + 8) * Math.cos(angle),
      ly: cy + (r + 8) * Math.sin(angle),
    }
  })

  const polygon = points.map(p => `${p.x},${p.y}`).join(' ')

  // Grid lines at 25%, 50%, 75%, 100%
  const gridLevels = [0.25, 0.5, 0.75, 1.0]

  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
      {/* Grid */}
      {gridLevels.map(level => {
        const gridPoints = AXES.map((_, i) => {
          const angle = startAngle + i * angleStep
          return `${cx + r * level * Math.cos(angle)},${cy + r * level * Math.sin(angle)}`
        }).join(' ')
        return (
          <polygon
            key={level}
            points={gridPoints}
            fill="none"
            stroke="#374151"
            strokeWidth="0.5"
          />
        )
      })}

      {/* Axis lines */}
      {AXES.map((_, i) => {
        const angle = startAngle + i * angleStep
        return (
          <line
            key={i}
            x1={cx}
            y1={cy}
            x2={cx + r * Math.cos(angle)}
            y2={cy + r * Math.sin(angle)}
            stroke="#374151"
            strokeWidth="0.5"
          />
        )
      })}

      {/* Data polygon */}
      <polygon
        points={polygon}
        fill="rgba(59, 130, 246, 0.3)"
        stroke="#3b82f6"
        strokeWidth="1.5"
      />

      {/* Data points */}
      {points.map((p, i) => (
        <circle key={i} cx={p.x} cy={p.y} r="2" fill="#3b82f6" />
      ))}

      {/* Labels */}
      {points.map((p, i) => (
        <text
          key={i}
          x={p.lx}
          y={p.ly}
          textAnchor="middle"
          dominantBaseline="middle"
          fill="#9ca3af"
          fontSize="7"
        >
          {LABELS[i]}
        </text>
      ))}
    </svg>
  )
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/RadarChart.tsx
git commit -m "feat(frontend): add SVG radar chart component for axis scores"
```

---

### Task 13: Thought Tree View Component

**Files:**
- Create: `frontend/src/components/ThoughtTreeView.tsx`

- [ ] **Step 1: Create the ThoughtTreeView component**

```tsx
// frontend/src/components/ThoughtTreeView.tsx
import { useCallback, useEffect, useMemo } from 'react'
import {
  ReactFlow,
  type Node,
  type Edge,
  Background,
  useNodesState,
  useEdgesState,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { RadarChart } from './RadarChart'

interface AxisScores {
  survival: number
  profit: number
  goal_progress: number
  risk: number
  efficiency: number
}

interface ThoughtNodeData {
  id: string
  action: string
  target: string
  reasoning: string
  scores: AxisScores
  combined: number
  status: 'active' | 'pruned' | 'winner'
  children?: ThoughtNodeData[]
  depth: number
  eval_time_ms: number
}

interface ThoughtTree {
  id: string
  agent_id: string
  situation: string
  root: ThoughtNodeData[]
  winner_id: string
  duration_ms: number
  model: string
  weights: AxisScores
}

interface ThoughtTreeViewProps {
  tree: ThoughtTree | null
  onNodeClick?: (node: ThoughtNodeData) => void
}

function ThoughtNodeCard({ data }: { data: ThoughtNodeData }) {
  const statusColors = {
    active: 'border-blue-500 bg-gray-800',
    winner: 'border-green-400 bg-gray-800 shadow-lg shadow-green-900/50',
    pruned: 'border-gray-600 bg-gray-900 opacity-40',
  }

  return (
    <div className={`rounded-lg border-2 p-3 w-56 transition-opacity duration-500 ${statusColors[data.status]}`}>
      <div className="flex justify-between items-start mb-2">
        <div>
          <span className="text-sm font-bold text-white">{data.action}</span>
          {data.target && (
            <span className="text-xs text-gray-400 ml-1">→ {data.target}</span>
          )}
        </div>
        <span className={`text-xs px-1.5 py-0.5 rounded ${
          data.status === 'winner' ? 'bg-green-800 text-green-200' :
          data.status === 'pruned' ? 'bg-gray-700 text-gray-400' :
          'bg-blue-800 text-blue-200'
        }`}>
          {data.combined.toFixed(1)}
        </span>
      </div>

      {data.scores.survival > 0 && (
        <div className="flex justify-center mb-2">
          <RadarChart scores={data.scores} size={70} />
        </div>
      )}

      {data.reasoning && (
        <p className="text-xs text-gray-400 line-clamp-2">{data.reasoning}</p>
      )}

      {data.eval_time_ms > 0 && (
        <p className="text-xs text-gray-500 mt-1">{(data.eval_time_ms / 1_000_000).toFixed(1)}s</p>
      )}
    </div>
  )
}

export function ThoughtTreeView({ tree, onNodeClick }: ThoughtTreeViewProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])

  const buildGraph = useCallback((thoughtTree: ThoughtTree) => {
    const flowNodes: Node[] = []
    const flowEdges: Edge[] = []

    // Root situation node
    flowNodes.push({
      id: 'situation',
      type: 'default',
      position: { x: 300, y: 0 },
      data: { label: thoughtTree.situation },
      style: {
        background: '#1f2937',
        color: '#d1d5db',
        border: '1px solid #4b5563',
        borderRadius: '8px',
        padding: '8px 16px',
        fontSize: '12px',
        maxWidth: '400px',
      },
    })

    // Branch nodes
    const spacing = 220
    const startX = (thoughtTree.root.length - 1) * spacing / 2
    thoughtTree.root.forEach((node, i) => {
      const x = 300 - startX + i * spacing
      flowNodes.push({
        id: node.id,
        type: 'default',
        position: { x, y: 120 },
        data: { label: <ThoughtNodeCard data={node} /> },
        style: { background: 'transparent', border: 'none', padding: 0 },
      })

      flowEdges.push({
        id: `situation-${node.id}`,
        source: 'situation',
        target: node.id,
        style: {
          stroke: node.status === 'winner' ? '#4ade80' :
                  node.status === 'pruned' ? '#4b5563' : '#3b82f6',
          strokeWidth: node.status === 'winner' ? 2 : 1,
        },
        animated: node.status === 'winner',
      })

      // Child nodes (next_step)
      if (node.children) {
        node.children.forEach((child, j) => {
          const childId = child.id || `${node.id}_child_${j}`
          flowNodes.push({
            id: childId,
            type: 'default',
            position: { x, y: 380 },
            data: { label: <ThoughtNodeCard data={child} /> },
            style: { background: 'transparent', border: 'none', padding: 0 },
          })
          flowEdges.push({
            id: `${node.id}-${childId}`,
            source: node.id,
            target: childId,
            style: {
              stroke: child.status === 'winner' ? '#4ade80' :
                      child.status === 'pruned' ? '#4b5563' : '#3b82f6',
              strokeWidth: child.status === 'winner' ? 2 : 1,
            },
            animated: child.status === 'winner',
          })
        })
      }
    })

    setNodes(flowNodes)
    setEdges(flowEdges)
  }, [setNodes, setEdges])

  useEffect(() => {
    if (tree) {
      buildGraph(tree)
    }
  }, [tree, buildGraph])

  const handleNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    if (onNodeClick && tree) {
      const thoughtNode = tree.root.find(n => n.id === node.id)
      if (thoughtNode) onNodeClick(thoughtNode)
    }
  }, [onNodeClick, tree])

  if (!tree) {
    return (
      <div className="flex items-center justify-center h-64 text-gray-500">
        Waiting for thought tree...
      </div>
    )
  }

  return (
    <div className="h-96 w-full bg-gray-950 rounded-lg border border-gray-800">
      <div className="flex justify-between items-center px-3 py-2 border-b border-gray-800">
        <span className="text-sm font-semibold text-gray-300">Thought Engine</span>
        <div className="flex gap-3 text-xs text-gray-500">
          <span>Model: {tree.model}</span>
          <span>Time: {(tree.duration_ms / 1_000_000).toFixed(1)}s</span>
        </div>
      </div>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={handleNodeClick}
        fitView
        panOnDrag
        zoomOnScroll
        attributionPosition="bottom-left"
      >
        <Background color="#1f2937" gap={20} />
      </ReactFlow>
    </div>
  )
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: Success (or type adjustments needed for React Flow generics)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ThoughtTreeView.tsx
git commit -m "feat(frontend): add ThoughtTreeView with React Flow tree graph"
```

---

### Task 14: Debug Panel Component

**Files:**
- Create: `frontend/src/components/DebugPanel.tsx`

- [ ] **Step 1: Create the debug panel**

```tsx
// frontend/src/components/DebugPanel.tsx
import { useState } from 'react'

interface AxisScores {
  survival: number
  profit: number
  goal_progress: number
  risk: number
  efficiency: number
}

interface DebugNodeData {
  id: string
  action: string
  target: string
  reasoning: string
  scores: AxisScores
  combined: number
  status: string
  eval_time_ms: number
  prompt?: string
  raw_response?: string
}

interface DebugPanelProps {
  node: DebugNodeData | null
  weights: AxisScores | null
  model: string
}

export function DebugPanel({ node, weights, model }: DebugPanelProps) {
  const [expanded, setExpanded] = useState(false)
  const [activeTab, setActiveTab] = useState<'scores' | 'prompt' | 'response'>('scores')

  if (!node) {
    return (
      <div className="text-xs text-gray-500 p-3">
        Click a node to inspect
      </div>
    )
  }

  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg">
      <button
        className="w-full flex justify-between items-center px-3 py-2 text-sm text-gray-300 hover:bg-gray-800"
        onClick={() => setExpanded(!expanded)}
      >
        <span>Debug: {node.action}{node.target ? ` → ${node.target}` : ''}</span>
        <span>{expanded ? '▼' : '▶'}</span>
      </button>

      {expanded && (
        <div className="px-3 pb-3">
          {/* Tab buttons */}
          <div className="flex gap-1 mb-2">
            {(['scores', 'prompt', 'response'] as const).map(tab => (
              <button
                key={tab}
                className={`px-2 py-1 text-xs rounded ${
                  activeTab === tab
                    ? 'bg-blue-800 text-blue-200'
                    : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                }`}
                onClick={() => setActiveTab(tab)}
              >
                {tab}
              </button>
            ))}
          </div>

          {activeTab === 'scores' && (
            <div className="space-y-1 text-xs">
              <div className="text-gray-400 mb-1">Model: {model}</div>
              <div className="text-gray-400 mb-1">
                Eval time: {(node.eval_time_ms / 1_000_000).toFixed(1)}s
              </div>
              <table className="w-full">
                <thead>
                  <tr className="text-gray-500">
                    <th className="text-left">Axis</th>
                    <th className="text-right">Score</th>
                    <th className="text-right">Weight</th>
                    <th className="text-right">Weighted</th>
                  </tr>
                </thead>
                <tbody>
                  {Object.entries(node.scores).map(([axis, score]) => (
                    <tr key={axis} className="text-gray-300">
                      <td>{axis}</td>
                      <td className="text-right">{score}</td>
                      <td className="text-right">
                        {weights ? (weights[axis as keyof AxisScores] ?? 0).toFixed(2) : '-'}
                      </td>
                      <td className="text-right">
                        {weights
                          ? (score * (weights[axis as keyof AxisScores] ?? 0)).toFixed(1)
                          : '-'}
                      </td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr className="text-gray-200 border-t border-gray-700">
                    <td colSpan={3}>Combined</td>
                    <td className="text-right font-bold">{node.combined.toFixed(1)}</td>
                  </tr>
                </tfoot>
              </table>
            </div>
          )}

          {activeTab === 'prompt' && (
            <pre className="text-xs text-gray-400 bg-gray-950 p-2 rounded max-h-48 overflow-auto whitespace-pre-wrap">
              {node.prompt || 'Prompt not available'}
            </pre>
          )}

          {activeTab === 'response' && (
            <pre className="text-xs text-gray-400 bg-gray-950 p-2 rounded max-h-48 overflow-auto whitespace-pre-wrap">
              {node.raw_response || 'Response not available'}
            </pre>
          )}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/DebugPanel.tsx
git commit -m "feat(frontend): add debug panel for thought tree inspection"
```

---

### Task 15: Wire Frontend to Observer WebSocket

**Files:**
- Modify: `frontend/src/lib/useObserver.ts`

- [ ] **Step 1: Add thought_tree state and message handling**

In `useObserver.ts`, add to the state interface:

```typescript
thoughtTree: ThoughtTree | null
```

Initialize it as `null` in the default state.

In the message handler switch statement (where `game_message`, `agent_status`, etc. are handled), add:

```typescript
case 'thought_tree':
  setState(prev => ({
    ...prev,
    thoughtTree: msg.message as ThoughtTree,
  }))
  break
```

Export `thoughtTree` in the return value.

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/useObserver.ts
git commit -m "feat(frontend): handle thought_tree WebSocket messages"
```

---

## Chunk 6: Build Verification and Integration Test

### Task 16: Full Build and Test

- [ ] **Step 1: Build all Go code**

Run: `go build ./...`
Expected: Success

- [ ] **Step 2: Run all Go tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 3: Run linter**

Run: `golangci-lint run ./...`
Expected: No new findings from `pkg/tot/`

- [ ] **Step 4: Build frontend**

Run: `cd frontend && npm run build`
Expected: Success

- [ ] **Step 5: Commit any remaining fixes**

```bash
git add -A
git commit -m "chore: fix any build/lint issues from Thought Engine integration"
```

---

### Task 17: Enable ToT for a Test Agent

- [ ] **Step 1: Add decision_mode to a miner personality**

Edit `data/agents/miner-1/personality.json` and add the field:

```json
"decision_mode": "tot"
```

- [ ] **Step 2: Commit**

```bash
git add data/agents/miner-1/personality.json
git commit -m "feat: enable Thought Engine for miner-1 (Pickaxe)"
```

---

## Summary

| Chunk | Tasks | What it produces |
|-------|-------|-----------------|
| 1 | Tasks 1-3 | Core types, action filter, weight derivation — all tested |
| 2 | Tasks 4-6 | Prompt templates + pipeline evaluator — full pipeline tested with mock LLM |
| 3 | Tasks 7-9 | Runner integration — ToT wired into decision loop, opt-in per agent |
| 4 | Task 10 | Observer relay — thought trees stream to browser |
| 5 | Tasks 11-15 | Frontend — React Flow tree, radar charts, debug panel, WebSocket wiring |
| 6 | Tasks 16-17 | Full build verification + enable for test agent |

Total: 17 tasks across 6 chunks. Each chunk produces working, testable software that can be committed independently.
