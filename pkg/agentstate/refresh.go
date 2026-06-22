package agentstate

import (
	"context"

	"github.com/rsned/spacemolt/pkg/actionspace"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/tot"
)

// miningPOITypes are POI types where resource enrichment is relevant.
var miningPOITypes = map[string]bool{
	"asteroid_belt":  true,
	"asteroid_field": true,
	"asteroid":       true,
	"ice_field":      true,
	"gas_cloud":      true,
}

// Refresh rebuilds the enrichment and action layers from current game state + KB.
// Called once at the top of each agent decision cycle.
func (s *AgentState) Refresh(ctx context.Context) {
	state := s.game.Clone()

	// 1. Resolve current POI type.
	s.enriched.CurrentPOIType = resolvePOIType(state)

	// 2. System-level enrichment.
	s.enriched.SystemSecurity = securityLabel(state.System.PoliceLevel)
	s.enriched.SystemDanger, _ = s.kb.GetSystemDanger(ctx, state.CurrentSystem)
	s.enriched.SystemAnomalies, _ = s.kb.GetActiveAnomalies(ctx, state.CurrentSystem)

	// 3. Neighbor enrichment.
	s.enriched.Neighbors = s.enrichNeighbors(ctx, state)
	s.enriched.FuelToNeighbors = s.buildFuelMap(ctx, state)

	// 4. POI resource enrichment (only at minable locations).
	if miningPOITypes[s.enriched.CurrentPOIType] {
		s.enriched.ResourceDepletion, _ = s.kb.GetDepletingResources(ctx, 0)
		s.enriched.ResourceHistory, _ = s.kb.GetResourceHistory(ctx, state.CurrentPOI, "", 10)
	} else {
		s.enriched.ResourceDepletion = nil
		s.enriched.ResourceHistory = nil
	}

	// 5. Market enrichment (only when docked).
	if state.Doc {
		s.refreshMarket(ctx, state)
	} else {
		s.enriched.MarketSnapshot = nil
		s.enriched.MarketAnalysis = nil
		s.enriched.NearbyBestBuys = nil
		s.enriched.NearbyBestSells = nil
	}

	// 6. Rebuild action space from the cloned state (not s.game, which is
	// the live pointer written to concurrently by handleResponse).
	gc := actionspace.FromState(state)
	as := actionspace.Evaluate(gc)
	s.actions = ActionState{
		GameContext:  gc,
		AllActions:   as.Actions,
		ValidActions: filterValid(as.Actions),
	}

	// 7. Update agent weights if personality is attached.
	if s.agent != nil && s.agent.Personality != nil {
		w := tot.DeriveWeights(*s.agent.Personality)
		s.agent.Weights = &w
	}

	s.lastSystem = state.CurrentSystem
}

// resolvePOIType finds the type of the player's current POI from system data.
func resolvePOIType(state *game.State) string {
	for _, poi := range state.System.POIs {
		if poi.ID == state.CurrentPOI {
			return poi.Type
		}
	}
	return ""
}

// securityLabel converts a numeric police level to a human-readable label.
func securityLabel(level int) string {
	switch level {
	case 0:
		return "Lawless"
	case 1:
		return "Low Security"
	case 2:
		return "Medium Security"
	case 3:
		return "High Security"
	default:
		return "Unknown"
	}
}

// enrichNeighbors builds EnrichedNeighbor entries for each system connection.
func (s *AgentState) enrichNeighbors(ctx context.Context, state *game.State) []EnrichedNeighbor {
	conns := state.System.Connections
	neighbors := make([]EnrichedNeighbor, len(conns))

	for i, conn := range conns {
		en := EnrichedNeighbor{
			SystemID: conn.SystemID,
			Name:     conn.Name,
			Distance: conn.Distance,
		}

		// Enrich from KB system data.
		if sys, err := s.kb.GetSystem(ctx, conn.SystemID); err == nil && sys != nil {
			en.Security = securityLabel(sys.PoliceLevel)
			en.Empire = sys.Empire
			en.IsStronghold = sys.IsStronghold

			// Count station/resource POIs from KB.
			if pois, err := s.kb.GetPOIs(ctx, conn.SystemID); err == nil {
				for _, poi := range pois {
					switch poi.Type {
					case "station", "outpost":
						en.StationCount++
					case "asteroid_belt", "asteroid_field", "asteroid", "ice_field", "gas_cloud":
						en.ResourcePOIs++
					}
				}
			}
		}

		// Enrich with route metrics.
		if metrics, err := s.kb.GetOptimalRoute(ctx, state.CurrentSystem, conn.SystemID); err == nil && metrics != nil {
			en.AvgFuelCost = metrics.AvgFuelCost
			en.AvgTravelTime = metrics.AvgTravelTime
		}

		// Enrich with danger level.
		if danger, err := s.kb.GetSystemDanger(ctx, conn.SystemID); err == nil && danger != nil {
			en.DangerLevel = danger.DangerLevel
		}

		neighbors[i] = en
	}

	return neighbors
}

// buildFuelMap builds a quick-lookup map of systemID → avg fuel cost.
func (s *AgentState) buildFuelMap(ctx context.Context, state *game.State) map[string]float64 {
	fuelMap := make(map[string]float64, len(state.System.Connections))
	for _, conn := range state.System.Connections {
		if metrics, err := s.kb.GetOptimalRoute(ctx, state.CurrentSystem, conn.SystemID); err == nil && metrics != nil {
			fuelMap[conn.SystemID] = metrics.AvgFuelCost
		}
	}
	return fuelMap
}

// refreshMarket populates market enrichment data for the current station.
func (s *AgentState) refreshMarket(ctx context.Context, state *game.State) {
	if s.mc == nil {
		return
	}
	stationID := state.CurrentPOI

	s.enriched.MarketSnapshot, _ = s.mc.GetLatestSnapshot(ctx, stationID)
	s.enriched.MarketAnalysis, _ = s.mc.GetLatestAnalysis(ctx, stationID)

	// Find best sell prices for items currently in cargo.
	s.enriched.NearbyBestSells = nil
	for _, item := range state.Ship.Cargo {
		if prices, err := s.mc.FindBestPrices(ctx, item.ItemID, "buy", 3); err == nil {
			s.enriched.NearbyBestSells = append(s.enriched.NearbyBestSells, prices...)
		}
	}

	// Find best buy opportunities (items available cheaply).
	s.enriched.NearbyBestBuys = nil
	if s.enriched.MarketSnapshot != nil {
		for _, order := range s.enriched.MarketSnapshot.Orders {
			if order.Side == "sell" {
				if prices, err := s.mc.FindBestPrices(ctx, order.ItemID, "sell", 1); err == nil {
					s.enriched.NearbyBestBuys = append(s.enriched.NearbyBestBuys, prices...)
				}
			}
		}
	}
}

// filterValid returns only the actions that passed all preconditions.
func filterValid(actions []actionspace.ActionResult) []actionspace.ActionResult {
	var valid []actionspace.ActionResult
	for _, a := range actions {
		if a.Valid {
			valid = append(valid, a)
		}
	}
	return valid
}
