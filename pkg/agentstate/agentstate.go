// Package agentstate provides a unified state object that combines live game
// state, knowledge-base enrichment, action space evaluation, and optional
// agent strategic context into a single object attachable to any tool or call.
package agentstate

import (
	"time"

	"github.com/rsned/spacemolt/pkg/actionspace"
	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/tot"
)

// AgentState is the unified state object that can be attached to any tool,
// strategy, or agent decision cycle. It combines live game state, KB-enriched
// data, and optional agent strategic context.
type AgentState struct {
	game *game.State        // live state, updated by handleResponse()
	kb   knowledge.Base     // for enrichment queries
	mc   *market.Collector  // for market data reads/writes

	// Enriched data — rebuilt each Refresh() call.
	enriched EnrichedState

	// Action space — valid actions given current state.
	actions ActionState

	// Agent context — nil for non-agent consumers (tools, visualizer).
	agent *AgentContext

	// Tracks when enrichment was last rebuilt.
	lastRefresh time.Time
	lastSystem  string
}

// EnrichedState holds KB-derived data layered on top of live game state.
// Rebuilt on each Refresh() call via local SQLite queries.
type EnrichedState struct {
	// Current system enrichment.
	SystemSecurity  string
	SystemDanger    *knowledge.DangerZone
	SystemAnomalies []knowledge.Anomaly

	// Neighbor systems with full KB intelligence.
	Neighbors []EnrichedNeighbor

	// Current POI enrichment.
	CurrentPOIType    string
	ResourceDepletion []knowledge.DepletingResource
	ResourceHistory   []knowledge.ResourceHistory

	// Market intel (populated only when docked).
	MarketSnapshot  *market.MarketSnapshot
	MarketAnalysis  *market.MarketAnalysis
	NearbyBestBuys  []market.BestPrice
	NearbyBestSells []market.BestPrice

	// Route intelligence: systemID → avg fuel cost.
	FuelToNeighbors map[string]float64
}

// EnrichedNeighbor combines connection data with KB system intelligence.
type EnrichedNeighbor struct {
	SystemID      string  `json:"system_id"`
	Name          string  `json:"name"`
	Distance      int     `json:"distance"`
	Security      string  `json:"security"`
	Empire        string  `json:"empire"`
	IsStronghold  bool    `json:"is_stronghold"`
	DangerLevel   int     `json:"danger_level"`
	StationCount  int     `json:"station_count"`
	ResourcePOIs  int     `json:"resource_pois"` //nolint:revive // POIs is an acronym used as a name
	AvgFuelCost   float64 `json:"avg_fuel_cost"`
	AvgTravelTime float64 `json:"avg_travel_time"`
}

// ActionState holds the evaluated action space for the current state.
type ActionState struct {
	ValidActions []actionspace.ActionResult
	AllActions   []actionspace.ActionResult
	GameContext  actionspace.GameContext
}

// AgentContext carries agent-specific strategic data. Nil for non-agent consumers.
type AgentContext struct {
	Personality   *agent.Personality
	Goal          *agent.Goal
	Priority      *agent.Priority
	RecentActions []agent.HistoryEntry
	QueuedPlan    []agent.PlannedAction
	Weights       *tot.AxisWeights
}

// New creates an AgentState for non-agent consumers (tools, visualizer).
func New(state *game.State, kb knowledge.Base, mc *market.Collector) *AgentState {
	return &AgentState{
		game: state,
		kb:   kb,
		mc:   mc,
	}
}

// NewWithAgent creates an AgentState with agent strategic context attached.
func NewWithAgent(state *game.State, kb knowledge.Base, mc *market.Collector, agentCtx *AgentContext) *AgentState {
	return &AgentState{
		game:  state,
		kb:    kb,
		mc:    mc,
		agent: agentCtx,
	}
}
