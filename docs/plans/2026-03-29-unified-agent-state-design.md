# Unified Agent State Design

**Date:** 2026-03-29
**Status:** Draft
**Package:** `pkg/agentstate/`

## Problem

State data for agents is fragmented across four sources:

1. **`game.State`** — Live game client state (80+ fields), auto-updated by `handleResponse()`
2. **`actionspace.GameContext`** — Flattened precondition context with 24 checks
3. **`tot.PromptContext`** — Agent strategic context assembled for LLM prompts
4. **`knowledge.Base`** — SQLite-backed intelligence (systems, markets, routes, danger, depletion)

Agents and tools each assemble their own view by reaching into multiple packages. The game server's `get_system` returns neighbor connections with only name + distance — no security, resources, or fuel costs. Agents call `get_status`, `get_ship`, `get_system` repeatedly to stay current.

## Solution

A unified `AgentState` struct in `pkg/agentstate/` that:

- Wraps the live `*game.State` (no copy, same pointer the client maintains)
- Adds a KB-enriched layer rebuilt per decision cycle (~11s)
- Includes evaluated action space (valid/invalid with reasons)
- Carries optional agent strategic context (goals, plan, history)
- Provides convenience accessor methods so consumers don't care which layer data lives in
- Produces a flat `StateSnapshot` for serialization (prompts, logging, observer)

## Architecture

### Layered Design

```
┌─────────────────────────────────────────┐
│          AgentState (unified)           │
├─────────────────────────────────────────┤
│  Accessors: Fuel(), SystemSecurity(),   │
│  NeighborInfo(), CanDo(), Goal(), ...   │
├──────────┬──────────┬──────────┬────────┤
│  Live    │ Enriched │ Actions  │ Agent  │
│  Game    │ State    │ State    │ Context│
│  State   │ (KB)     │ (action  │ (opt)  │
│  (*game. │          │  space)  │        │
│  State)  │          │          │        │
└──────────┴──────────┴──────────┴────────┘
```

### Dependencies

```
pkg/agentstate/
  ├── pkg/game        (live state)
  ├── pkg/knowledge   (KB enrichment)
  ├── pkg/actionspace (action evaluation)
  └── pkg/agent       (strategic context types)
```

## Core Struct

```go
package agentstate

// AgentState is the unified state object that can be attached to any tool,
// strategy, or agent decision cycle. It combines live game state, KB-enriched
// data, and optional agent strategic context.
type AgentState struct {
    game    *game.State
    kb      knowledge.Base

    // Enriched data — rebuilt each Refresh() call
    enriched EnrichedState

    // Action space — valid actions given current state
    actions *ActionState

    // Agent context — nil for non-agent consumers (tools, visualizer)
    agent *AgentContext

    // Tracks when enrichment was last rebuilt
    lastRefresh time.Time
    lastSystem  string
}
```

### Constructors

```go
func New(state *game.State, kb knowledge.Base) *AgentState
func NewWithAgent(state *game.State, kb knowledge.Base, ctx *AgentContext) *AgentState
```

## Enriched State

KB-derived data layered on top of live game state. Replaces repeated `get_system` calls and provides intelligence the game API doesn't return.

```go
type EnrichedState struct {
    // Current system enrichment
    SystemSecurity  string
    SystemDanger    *knowledge.DangerZone
    SystemAnomalies []knowledge.Anomaly

    // Neighbor systems — the biggest value-add
    Neighbors []EnrichedNeighbor

    // Current POI enrichment
    CurrentPOIType    string
    ResourceDepletion []knowledge.DepletingResource
    ResourceHistory   []knowledge.ResourceHistory

    // Market intel (populated only when docked)
    MarketSnapshot  *knowledge.MarketSnapshot
    MarketAnalysis  *knowledge.MarketAnalysis
    NearbyBestBuys  []knowledge.BestPrice
    NearbyBestSells []knowledge.BestPrice

    // Route intelligence
    FuelToNeighbors map[string]float64
}

// EnrichedNeighbor combines connection data with KB system intelligence.
type EnrichedNeighbor struct {
    SystemID      string
    Name          string
    Distance      int
    Security      string
    Empire        string
    IsStronghold  bool
    DangerLevel   int
    StationCount  int
    ResourcePOIs  int
    AvgFuelCost   float64
    AvgTravelTime float64
}
```

### Key enrichment: neighbor systems

Today an agent gets `[{SystemID: "sol-3", Name: "Sol", Distance: 2}]` from the game. With enrichment, each neighbor includes security level, empire, danger rating, station count, resource POI count, and fuel cost — all from the local KB, no additional API calls.

## Action State

```go
type ActionState struct {
    ValidActions []actionspace.EvalResult
    AllActions   []actionspace.EvalResult
    GameContext  actionspace.GameContext
}
```

## Agent Context

Optional layer for agent-specific strategic data. Nil for non-agent consumers.

```go
type AgentContext struct {
    Personality   *agent.Personality
    Goal          *agent.Goal
    Priority      *agent.Priority
    RecentActions []agent.HistoryEntry
    QueuedPlan    []agent.PlannedAction
    Weights       *weights.AxisWeights
}
```

## Accessor Methods

Convenience methods that reach into the correct layer transparently:

```go
// Live game state
func (s *AgentState) Fuel() (current, max float64)
func (s *AgentState) Hull() (current, max float64)
func (s *AgentState) Credits() float64
func (s *AgentState) Docked() bool
func (s *AgentState) InCombat() bool
func (s *AgentState) InBattle() bool
func (s *AgentState) Traveling() bool
func (s *AgentState) CurrentSystem() string
func (s *AgentState) CurrentPOI() string
func (s *AgentState) Ship() game.Ship
func (s *AgentState) Player() game.Player
func (s *AgentState) Nearby() []game.NearbyPlayer
func (s *AgentState) Tick() int64
func (s *AgentState) System() game.SystemData
func (s *AgentState) Cargo() []game.CargoItem
func (s *AgentState) CargoSpace() (used, capacity float64)
func (s *AgentState) Skills() map[string]game.Skill

// Enriched
func (s *AgentState) SystemSecurity() string
func (s *AgentState) SystemDanger() *knowledge.DangerZone
func (s *AgentState) NeighborInfo(systemID string) *EnrichedNeighbor
func (s *AgentState) Neighbors() []EnrichedNeighbor
func (s *AgentState) FuelCostTo(systemID string) (float64, bool)
func (s *AgentState) CurrentPOIType() string
func (s *AgentState) ResourceDepletion() []knowledge.DepletingResource
func (s *AgentState) MarketSnapshot() *knowledge.MarketSnapshot
func (s *AgentState) BestSellsForCargo() []knowledge.BestPrice

// Actions
func (s *AgentState) ValidActions() []actionspace.EvalResult
func (s *AgentState) CanDo(action string) bool

// Agent context (zero values if agent is nil)
func (s *AgentState) Goal() *agent.Goal
func (s *AgentState) Focus() string
func (s *AgentState) Constraints() []string
func (s *AgentState) HasQueuedPlan() bool
func (s *AgentState) QueuedPlan() []agent.PlannedAction

// Serialization
func (s *AgentState) Snapshot() StateSnapshot
```

## Refresh Logic

Called once per agent decision cycle (~11s). Uses `Clone()` to snapshot game state under lock, then works unlocked against the KB.

```go
func (s *AgentState) Refresh(ctx context.Context) error
```

Refresh steps:

1. `state := s.game.Clone()` — snapshot under lock
2. Resolve current POI type from system POIs
3. System enrichment: security label, danger zone, anomalies
4. Neighbor enrichment: KB system data + connection metrics for each connection
5. Fuel cost map for all neighbors
6. Resource depletion (only if at mining POI)
7. Market intel (only if docked)
8. Rebuild action space via `actionspace.FromState()` + `EvaluateAll()`
9. Recompute agent personality weights (if agent context attached)

Design principles:
- KB query errors are swallowed — enrichment is best-effort, live state is authoritative
- Market enrichment gated on `Doc` — no wasted queries in space
- Resource enrichment gated on mining POI type

## StateSnapshot

Flat, JSON-serializable copy for prompts, observer broadcasts, logging:

```go
type StateSnapshot struct {
    // Timing
    Tick      int64     `json:"tick"`
    Refreshed time.Time `json:"refreshed"`

    // Player
    Username string  `json:"username"`
    Empire   string  `json:"empire"`
    Credits  float64 `json:"credits"`

    // Location
    CurrentSystem  string `json:"current_system"`
    CurrentPOI     string `json:"current_poi"`
    CurrentPOIType string `json:"current_poi_type"`
    Docked         bool   `json:"docked"`
    Traveling      bool   `json:"traveling"`
    SystemSecurity string `json:"system_security"`

    // Ship
    ShipClass     string           `json:"ship_class"`
    ShipName      string           `json:"ship_name"`
    Fuel          float64          `json:"fuel"`
    MaxFuel       float64          `json:"max_fuel"`
    Hull          float64          `json:"hull"`
    MaxHull       float64          `json:"max_hull"`
    CargoUsed     float64          `json:"cargo_used"`
    CargoCapacity float64          `json:"cargo_capacity"`
    Cargo         []game.CargoItem `json:"cargo"`

    // Combat
    InCombat   bool   `json:"in_combat"`
    InBattle   bool   `json:"in_battle"`
    PirateName string `json:"pirate_name,omitempty"`
    PirateTier string `json:"pirate_tier,omitempty"`

    // Enrichment
    Neighbors         []EnrichedNeighbor            `json:"neighbors"`
    SystemDangerLevel int                            `json:"system_danger_level"`
    ResourceDepletion []knowledge.DepletingResource  `json:"resource_depletion,omitempty"`
    FuelToNeighbors   map[string]float64             `json:"fuel_to_neighbors"`

    // Actions
    ValidActionCount int      `json:"valid_action_count"`
    ValidActionNames []string `json:"valid_action_names"`

    // Agent context (omitted if nil)
    Goal          *agent.Goal           `json:"goal,omitempty"`
    Focus         string                `json:"focus,omitempty"`
    Constraints   []string              `json:"constraints,omitempty"`
    QueuedPlan    []agent.PlannedAction `json:"queued_plan,omitempty"`
    RecentActions []agent.HistoryEntry  `json:"recent_actions,omitempty"`
}
```

## Integration Points

### Agent Runner (`pkg/agent/runner.go`)

Create `AgentState` at agent startup, call `Refresh()` at the top of each decision cycle. Pass to `Agent.Decide()` and `executeDecision()` instead of raw `*game.State`.

### ToT Prompt Building (`pkg/tot/prompts.go`)

Replace manual state assembly in `BuildAssessPrompt` with `agentState.Snapshot()`. Eliminates reaching into three packages to construct context.

### Action Visualizer (`cmd/tools/action-visualizer/`)

Use `New(state, kb)` (no agent context) to get enriched state + action evaluation for visualization.

### Strategies (`pkg/strategy/`)

Strategies can accept `*AgentState` for richer decision-making (e.g., mining strategy checks resource depletion, trading strategy checks best prices).

## Migration Path

1. Create `pkg/agentstate/` with types, constructors, refresh, accessors
2. Wire into agent runner — create + refresh per cycle
3. Refactor ToT to use `Snapshot()` instead of manual assembly
4. Gradually migrate strategies and tools to accept `*AgentState`
5. Existing `game.State` and `actionspace.GameContext` remain unchanged — no breaking changes
