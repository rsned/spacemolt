package agentstate

import (
	"maps"

	"github.com/rsned/spacemolt/pkg/actionspace"
	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// --- Live game state accessors ---

// Fuel returns current and maximum fuel.
func (s *AgentState) Fuel() (current, max float64) {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.Ship.Fuel, s.game.Ship.MaxFuel
}

// Hull returns current and maximum hull.
func (s *AgentState) Hull() (current, max float64) {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.Ship.Hull, s.game.Ship.MaxHull
}

// Credits returns the player's credit balance.
func (s *AgentState) Credits() float64 {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.Credits
}

// Docked returns whether the ship is docked at a station.
func (s *AgentState) Docked() bool {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.Doc
}

// InCombat returns whether the player is in NPC/pirate combat.
func (s *AgentState) InCombat() bool {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.InCombat
}

// InBattle returns whether the player is in a tactical battle.
func (s *AgentState) InBattle() bool {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.InBattle
}

// Traveling returns whether the ship is in transit.
func (s *AgentState) Traveling() bool {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.Traveling
}

// CurrentSystem returns the ID of the current system.
func (s *AgentState) CurrentSystem() string {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.CurrentSystem
}

// CurrentPOI returns the ID of the current point of interest.
func (s *AgentState) CurrentPOI() string {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.CurrentPOI
}

// Ship returns a copy of the current ship data.
func (s *AgentState) Ship() game.Ship {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.Ship
}

// Player returns a copy of the current player data.
func (s *AgentState) Player() game.Player {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.Player
}

// Nearby returns a copy of the nearby players list.
func (s *AgentState) Nearby() []game.NearbyPlayer {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	out := make([]game.NearbyPlayer, len(s.game.Nearby))
	copy(out, s.game.Nearby)
	return out
}

// Tick returns the current game tick.
func (s *AgentState) Tick() int64 {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.CurrentTick
}

// System returns a copy of the current system data.
func (s *AgentState) System() game.SystemData {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.System
}

// Cargo returns a copy of the current cargo items.
func (s *AgentState) Cargo() []game.CargoItem {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	out := make([]game.CargoItem, len(s.game.Ship.Cargo))
	copy(out, s.game.Ship.Cargo)
	return out
}

// CargoSpace returns cargo used and total capacity.
func (s *AgentState) CargoSpace() (used, capacity float64) {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	return s.game.Ship.CargoUsed, s.game.Ship.CargoCapacity
}

// Skills returns a copy of the player's skill map.
func (s *AgentState) Skills() map[string]game.Skill {
	s.game.Mu.Lock()
	defer s.game.Mu.Unlock()
	if s.game.Player.Skills == nil {
		return nil
	}
	out := make(map[string]game.Skill, len(s.game.Player.Skills))
	maps.Copy(out, s.game.Player.Skills)
	return out
}

// --- Enriched data accessors ---

// SystemSecurity returns the human-readable security label for the current system.
func (s *AgentState) SystemSecurity() string {
	return s.enriched.SystemSecurity
}

// SystemDanger returns the danger zone data for the current system, or nil.
func (s *AgentState) SystemDanger() *knowledge.DangerZone {
	return s.enriched.SystemDanger
}

// NeighborInfo returns enriched data for a specific neighbor system, or nil.
func (s *AgentState) NeighborInfo(systemID string) *EnrichedNeighbor {
	for i := range s.enriched.Neighbors {
		if s.enriched.Neighbors[i].SystemID == systemID {
			return &s.enriched.Neighbors[i]
		}
	}
	return nil
}

// Neighbors returns all enriched neighbor systems.
func (s *AgentState) Neighbors() []EnrichedNeighbor {
	return s.enriched.Neighbors
}

// FuelCostTo returns the average fuel cost to reach a neighbor system.
func (s *AgentState) FuelCostTo(systemID string) (float64, bool) {
	cost, ok := s.enriched.FuelToNeighbors[systemID]
	return cost, ok
}

// CurrentPOIType returns the type of the current POI (e.g., "station", "asteroid_belt").
func (s *AgentState) CurrentPOIType() string {
	return s.enriched.CurrentPOIType
}

// ResourceDepletion returns depleting resource data at the current POI.
func (s *AgentState) ResourceDepletion() []knowledge.DepletingResource {
	return s.enriched.ResourceDepletion
}

// MarketSnapshot returns the latest market snapshot at the current station.
func (s *AgentState) MarketSnapshot() *knowledge.MarketSnapshot {
	return s.enriched.MarketSnapshot
}

// BestSellsForCargo returns the best sell opportunities for items in cargo.
func (s *AgentState) BestSellsForCargo() []knowledge.BestPrice {
	return s.enriched.NearbyBestSells
}

// --- Action space accessors ---

// ValidActions returns the actions that passed all preconditions.
func (s *AgentState) ValidActions() []actionspace.ActionResult {
	return s.actions.ValidActions
}

// CanDo returns whether a specific action is currently valid.
func (s *AgentState) CanDo(action string) bool {
	for _, a := range s.actions.ValidActions {
		if a.Action.Name == action {
			return true
		}
	}
	return false
}

// --- Agent context accessors (return zero values if agent is nil) ---

// Goal returns the agent's current strategic goal, or nil.
func (s *AgentState) Goal() *agent.Goal {
	if s.agent == nil {
		return nil
	}
	return s.agent.Goal
}

// Focus returns the agent's current strategic focus, or empty string.
func (s *AgentState) Focus() string {
	if s.agent == nil || s.agent.Priority == nil {
		return ""
	}
	return s.agent.Priority.Focus
}

// Constraints returns the agent's active constraints, or nil.
func (s *AgentState) Constraints() []string {
	if s.agent == nil || s.agent.Priority == nil {
		return nil
	}
	return s.agent.Priority.Constraints
}

// HasQueuedPlan returns whether the agent has a queued multi-step plan.
func (s *AgentState) HasQueuedPlan() bool {
	return s.agent != nil && len(s.agent.QueuedPlan) > 0
}

// QueuedPlan returns the agent's queued planned actions, or nil.
func (s *AgentState) QueuedPlan() []agent.PlannedAction {
	if s.agent == nil {
		return nil
	}
	return s.agent.QueuedPlan
}

// GameState returns the underlying live game state pointer.
// Use sparingly — prefer the typed accessors.
func (s *AgentState) GameState() *game.State {
	return s.game
}

// SetAgentContext replaces the agent context (e.g., after goal/priority changes).
func (s *AgentState) SetAgentContext(ctx *AgentContext) {
	s.agent = ctx
}
