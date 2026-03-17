# Thought Engine — Tree-of-Thought Decision Framework

**Date**: 2026-03-16
**Status**: Design Complete

## Overview

The Thought Engine is a staged LLM decision pipeline that replaces the current single-call `llm.Decide()` with a three-stage Tree-of-Thought process. It produces a scored, visualizable decision tree that shows what the agent considered, how each option scored, and why the winning path was chosen.

The system serves dual purposes:
1. **Debugging/development** — understand prompt deficiencies, tune personality weights, compare model performance
2. **Live spectating** — watch agents think in real-time with animated tree graphs and radar charts

## Architecture

```
Agent Runner (pkg/agent/runner.go)
    │
    │  Toggle: useToT (per-agent, via personality config)
    │
    │  ToT path:
    ├── Pre-filter: ValidActions(state) → 5-15 valid actions (code-based)
    │
    ├── Stage 1: Assess (1 LLM call)
    │   "Given this situation, what are my 3-5 options?"
    │   → produces candidate branches
    │
    ├── Stage 2: Evaluate (N parallel LLM calls, one per branch)
    │   "If I choose X, score on 5 axes: survival, profit, goal, risk, efficiency"
    │   → each branch gets axis scores + reasoning + suggested next step
    │
    ├── Stage 3: Select (deterministic — no LLM call)
    │   Weighted sum using personality-derived axis weights
    │   → winning branch selected, losers marked pruned
    │
    └── Output: ThoughtTree
        ├── streamed to frontend via observer events
        ├── winning path → Decision + PlannedActions for execution
        └── full tree kept in history for replay/debugging
```

Fallback: if the ToT pipeline errors or times out, degrades to the existing single-call decision path.

## Core Data Structures

```go
// pkg/tot/types.go

type ThoughtTree struct {
    ID        string
    AgentID   string
    Timestamp time.Time
    Root      *ThoughtNode  // situation assessment node
    Winner    *ThoughtNode  // pointer to chosen branch
    Duration  time.Duration // total pipeline time
    Model     string        // LLM model used
}

type ThoughtNode struct {
    ID         string
    Action     string          // game action (travel, mine, dock, etc.)
    Target     string          // action target
    Reasoning  string          // LLM explanation
    Scores     AxisScores      // multi-axis evaluation
    Combined   float64         // weighted final score
    Status     NodeStatus      // active, pruned, winner
    Children   []*ThoughtNode  // next steps (depth 2+)
    Parent     *ThoughtNode
    Depth      int
    EvalTime   time.Duration   // LLM call duration for this node
}

type AxisScores struct {
    Survival     float64 `json:"survival"`
    Profit       float64 `json:"profit"`
    GoalProgress float64 `json:"goal_progress"`
    Risk         float64 `json:"risk"`
    Efficiency   float64 `json:"efficiency"`
}

type NodeStatus string
const (
    StatusActive  NodeStatus = "active"
    StatusPruned  NodeStatus = "pruned"
    StatusWinner  NodeStatus = "winner"
)

type AxisWeights struct {
    Survival     float64
    Profit       float64
    GoalProgress float64
    Risk         float64
    Efficiency   float64
}
```

## Stage Details

### Stage 1 — Situational Assessment

One LLM call using template `data/prompts/templates/tot/assess.v1.tmpl`.

**Input**: Current game state (location, fuel, hull, cargo, threats, nearby POIs) + pre-filtered valid actions list.

**Output**:
```json
{
  "situation": "Mining in low-security system, 60% cargo, pirates entered system",
  "options": [
    {"action": "travel", "target": "jump_gate_01", "rationale": "Flee to safety"},
    {"action": "mine", "target": "", "rationale": "Keep mining, hope pirates miss me"},
    {"action": "cloak", "target": "", "rationale": "Hide from pirates"},
    {"action": "dock", "target": "station_alpha", "rationale": "Dock for safety"}
  ]
}
```

### Stage 2 — Branch Evaluation

N parallel LLM calls (one per option) using template `data/prompts/templates/tot/evaluate.v1.tmpl`.

**Input**: Game state context + the specific option to evaluate.

**Output** (per branch):
```json
{
  "action": "travel",
  "target": "jump_gate_01",
  "analysis": "Fleeing preserves cargo and hull but costs mining ticks",
  "scores": {
    "survival": 90,
    "profit": 40,
    "goal_progress": 30,
    "risk": 85,
    "efficiency": 35
  },
  "next_step": {"action": "jump", "target": "safe_system_01"}
}
```

Scores are 0-100 per axis. The `next_step` field provides depth-2 in the tree without additional LLM calls.

### Stage 3 — Selection

Deterministic. No LLM call.

```
combined = (survival * w_survival) + (profit * w_profit) +
           (goal_progress * w_goal) + (risk * w_risk) +
           (efficiency * w_efficiency)
```

Weights derived from agent personality (see below). Highest combined score wins. All non-winning branches marked `StatusPruned`. Winning branch action becomes the `Decision`; `next_step` becomes a `PlannedAction` in the queue.

## Action Pre-Filter

Code-based filter in `pkg/tot/filter.go` that removes physically impossible actions before the LLM sees them. Rules based on game state:

- **Docked**: undock, buy, sell, repair, refuel, view_market, view_storage, list_ships, switch_ship, craft, get_missions, complete_mission, etc.
- **In space**: travel, scan, get_nearby + conditionals:
  - Jump gate nearby → jump
  - Mineable POI nearby → mine
  - Station nearby → dock
  - Wrecks nearby → loot_wreck, salvage_wreck, tow_wreck
  - Has cloak module → cloak
  - In combat → battle_advance, battle_retreat, battle_stance, battle_target
- **Always available** (no tick cost): get_status, get_system, get_cargo, get_skills, get_map

Each action carries a name, one-line description, and valid targets (POI IDs, item names, etc.). Reduces ~200 commands to ~5-15 relevant ones.

Filter is intentionally permissive — if it includes something borderline, the LLM's Stage 1 simply won't pick it. Better to over-include than to accidentally hide a valid option.

## Personality-to-Weights Mapping

Axis weights derived from existing personality JSON fields, not hardcoded per role.

```go
// pkg/tot/weights.go

func DeriveWeights(p agent.Personality) AxisWeights {
    return AxisWeights{
        Survival:     blend(p.Motivations.Weights["survival"], p.Traits["caution"], 1.0-p.Traits["risk_tolerance"]),
        Profit:       blend(p.Motivations.Weights[profitMotivation(p)], p.Traits["greed"], p.Traits["ambition"]),
        GoalProgress: blend(p.Motivations.Weights[p.Motivations.Primary], p.Traits["determination"], p.Traits["perseverance"]),
        Risk:         blend(1.0-p.Traits["risk_tolerance"], p.Traits["caution"], 1.0-p.Traits["aggression"]),
        Efficiency:   blend(1.0-p.Traits["patience"], p.Traits["discipline"], 0.5),
    }
}
```

`blend()` averages non-zero values, normalized to 0-1. `profitMotivation()` maps role-specific motivation keys (mine_resources, maximize_profit, plunder, etc.) to the Profit axis.

**Example derived weights:**

| Agent | Survival | Profit | Goal | Risk | Efficiency |
|-------|----------|--------|------|------|------------|
| Miner (Pickaxe) | 0.55 | 0.90 | 0.90 | 0.53 | 0.50 |
| Pirate (Redbeard) | 0.27 | 0.88 | 0.90 | 0.17 | 0.50 |
| Trader (Mercury) | 0.62 | 0.83 | 0.90 | 0.72 | 0.65 |

Same pirate threat, different personality → different winning branch. Weights visible in debug panel for tuning.

## Runner Integration

Minimal change to `pkg/agent/runner.go`:

```go
// In executeCycle():
if r.useToT {
    validActions := tot.ValidActions(state)
    weights := tot.DeriveWeights(r.agent.GetPersonality())
    tree, err := r.totEvaluator.Evaluate(ctx, state, r.agent, validActions, weights)
    if err != nil {
        decision = r.agent.Decide(state)  // fallback
    } else {
        decision = tree.ToDecision()
        r.emitEvent("thought_tree", tree)
    }
} else {
    decision = r.agent.Decide(state)  // existing path
}
```

Toggle via personality JSON field `"decision_mode": "tot"` or runner config. Existing single-call path stays available for comparison and for agents that don't need ToT overhead.

## Frontend Visualization

### Event Streaming

New event type `thought_tree` emitted through existing `SetEventCallback()` mechanism. Incremental updates:

1. Stage 1 completes → root node + candidate branches appear (all `active`)
2. Each Stage 2 branch completes → node scores populate, radar chart fills in
3. Stage 3 completes → winner highlighted, losers fade to grey

### ThoughtTreeView Component

New React component using React Flow library. Renders in the agent observation view.

**Node rendering**: Each node is a card showing:
- Action name and target
- Short reasoning text (truncated, expandable on click)
- Mini radar chart (5 axes) — filled when scores arrive
- Combined score badge
- Color/opacity by status:
  - Active: vivid blue
  - Winner: green glow
  - Pruned: grey, 40% opacity (with ~500ms fade transition)

**Layout**: Top-down tree. Root (situation summary) at top, branches fan downward. `next_step` children render at depth 2. Edges animate in as nodes appear. Pruned branches and their ancestry fade together.

**History**: Previous trees kept in scrollable history (last 10-20 cycles). Auto-clears current tree when next decision cycle starts.

### Debug Panel

Expandable sidebar showing (per selected node):
- Full LLM prompt text for that stage
- Raw LLM response text
- Timing breakdown (per-stage and per-node durations)
- Personality weight vector used for scoring
- Model name and token counts (if available from Ollama)

## Local LLM Considerations

### Performance Estimates (qwen3:15b)

| Stage | Calls | Time (sequential) | Time (batched) |
|-------|-------|--------------------|----------------|
| Pre-filter | 0 (code) | <1ms | <1ms |
| Stage 1 | 1 | 3-4s | 3-4s |
| Stage 2 | 3-4 | 12-16s | 4-5s |
| Stage 3 | 0 (code) | <1ms | <1ms |
| **Total** | **4-5** | **~15-20s** | **~8-9s** |

1-2 game ticks per decision. Acceptable for turn-based game. Measure before optimizing.

### Prompt Design for Small Models

- Keep prompts 300-500 tokens max per stage (not the full context dump)
- Stage 1 gets situation summary; Stage 2 gets only the specific branch
- Use Ollama's `format: "json"` parameter for structured output
- Keep expected JSON schema simple and flat
- Robust parsing with fallbacks (existing `llm.Client` pattern)

### Model-Agnostic Config

Model name passed through config to existing `llm.Client`. Swap models by changing one config value. Test matrix:

- `qwen3:15b` — baseline, good reasoning quality
- `llama3:8b` — faster, test quality tradeoff
- `qwen3:32b` — better quality if GPU supports it

### GPU Test Matrix

| GPU | VRAM | Expected Performance |
|-----|------|---------------------|
| ADA 2000 | 16GB | Comfortable for 15B, baseline testing |
| RTX 2060 | 6GB | Stress test, likely needs 8B model |
| RTX 3080 | 10GB | 15B should fit, moderate speed |
| RTX 4080 | 16GB | Fast 15B, may support batching |

## New Files

| File | Purpose |
|------|---------|
| `pkg/tot/types.go` | ThoughtTree, ThoughtNode, AxisScores, NodeStatus |
| `pkg/tot/evaluator.go` | Three-stage pipeline orchestration |
| `pkg/tot/filter.go` | Code-based action pre-filter |
| `pkg/tot/weights.go` | Personality-to-weights derivation |
| `pkg/tot/prompts.go` | Prompt building for each stage |
| `data/prompts/templates/tot/assess.v1.tmpl` | Stage 1 prompt template |
| `data/prompts/templates/tot/evaluate.v1.tmpl` | Stage 2 prompt template |
| `frontend/src/components/ThoughtTreeView.tsx` | Tree graph visualization |
| `frontend/src/components/RadarChart.tsx` | Mini radar chart for node scores |
| `frontend/src/components/DebugPanel.tsx` | LLM prompt/response inspector |

## Modified Files

| File | Change |
|------|--------|
| `pkg/agent/runner.go` | Add ToT toggle in `executeCycle()` |
| `pkg/agent/agent.go` | Add `DecisionMode` field to Personality |
| `pkg/observe/observer.go` | Handle `thought_tree` event type |
| `frontend/src/lib/useObserver.ts` | Handle `thought_tree` WebSocket message |

## Future Enhancements (Not in v1)

- **Async forecast planning**: Start thinking about next decision while current action executes
- **Deeper trees**: Stage 2 evaluates 2-3 steps ahead instead of just next_step
- **Cross-agent reasoning**: Shared knowledge base informs branch evaluation
- **Learning from history**: Past decision trees inform future scoring calibration
- **Additional scoring axes**: Faction reputation, skill XP gain, social/team value
- **Pruning during evaluation**: If a branch scores below threshold on survival, skip remaining axes
