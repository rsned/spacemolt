package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/registry"
)

// CLI flags
var (
	registryURL = flag.String("registry-url", "", "Status registry URL (e.g., http://localhost:8081)")
	dbBackend   = flag.String("db-backend", "sqlite", "Knowledge base backend: sqlite or memory")
	dbPath      = flag.String("db-path", "data/spacemolt-knowledge.db", "Path to SQLite database")
	debug       = flag.Bool("debug", false, "Enable debug logging")
)

// Exploration state for DFS algorithm
type ExplorationState struct {
	VisitedSystems  map[string]bool // Track explored systems
	VisitedPOIs     map[string]bool // Track explored POIs in current system
	DFSStack        []string        // Backtracking stack for systems
	HomeSystem      string          // Starting point
	LastFuelStation string          // Last known refuel point
	PreviousSystem  string          // Previous system for escape routes
	UnderAttack     bool            // Combat state flag
	LastAttackTime  time.Time       // Time of last attack
	AgentID         string          // Agent ID for knowledge base attribution
	kb              knowledge.Base  // Knowledge base for querying system data
}

type explorerSimpleHandler struct {
	client *game.Client
	logger *log.Logger
	kb     knowledge.Base
}

func (h *explorerSimpleHandler) OnConnected(state *game.State) {
	h.logger.Printf("✓ Connected! Credits: %.2f | System: %s (%s)",
		state.Credits, state.System.Name, state.CurrentSystem)

	// CRITICAL: After reconnection, always refresh system data to verify our actual location
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.logger.Printf("🔍 Refreshing system data after connection to verify location...")
	if err := h.client.GetSystem(ctx); err != nil {
		h.logger.Printf("⚠️  Failed to refresh system data after connection: %v", err)
	} else {
		time.Sleep(2 * time.Second)
		refreshedState := h.client.GetState()
		h.logger.Printf("✓ Verified location: %s (%s)", refreshedState.System.Name, refreshedState.CurrentSystem)
	}
}

func (h *explorerSimpleHandler) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.logger.Printf("✓ %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.logger.Printf("✗ %s", msg)
		}
	}
}

func (h *explorerSimpleHandler) OnDisconnected(err error) {
	h.logger.Printf("Disconnected: %v", err)
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func updateCaptainsLog(agentID string, client *game.Client, expState *ExplorationState) {
	state := client.GetState()

	var notes []string
	if expState != nil {
		notes = append(notes, fmt.Sprintf("Systems explored: %d", len(expState.VisitedSystems)))
		notes = append(notes, fmt.Sprintf("Current system: %s", state.CurrentSystem))
	}
	notes = append(notes, fmt.Sprintf("Credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Ship: %s", state.Ship.Name))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))

	currentGoal := "Autonomous galaxy exploration - mapping systems and discovering POIs"
	if expState != nil {
		if state.Doc {
			currentGoal = "Docked at station - refueling and collecting market data"
		} else if state.Traveling && state.TravelProgress != nil {
			currentGoal = fmt.Sprintf("Exploring: traveling to %s", state.TravelProgress.Destination)
		} else if state.InCombat {
			currentGoal = "Evading hostile contact during exploration"
		} else if len(expState.DFSStack) > 0 {
			currentGoal = fmt.Sprintf("Deep exploration (stack depth: %d)", len(expState.DFSStack))
		}
	}

	entry := &game.AgentLog{
		AgentName:   state.Player.Username,
		CurrentGoal: currentGoal,
		Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
		Notes:       notes,
	}

	if err := game.WriteCaptainsLog(agentID, entry); err != nil {
		_ = err // Log error but don't fail
	}
}

// extractConnections converts game ConnectionInfo to knowledge SystemConnection.
func extractConnections(conns []game.ConnectionInfo) []knowledge.SystemConnection {
	result := make([]knowledge.SystemConnection, len(conns))
	for i, conn := range conns {
		result[i] = knowledge.SystemConnection{
			SystemID: conn.SystemID,
			Distance: conn.Distance,
		}
	}
	return result
}

func needsRefuel(state *game.State) bool {
	return state.Fuel < (state.MaxFuel * 0.3)
}

// ============================================================================
// DATA COLLECTION (Uses Knowledge Base)
// ============================================================================

func collectSystemData(client *game.Client, ctx context.Context, logger *log.Logger, kb knowledge.Base, agentID string) error {
	// Request system data (includes jump connections)
	if err := client.GetSystem(ctx); err != nil {
		return fmt.Errorf("failed to get system: %w", err)
	}
	time.Sleep(2 * time.Second)

	state := client.GetState()

	// Debug: Log what we got
	logger.Printf("🔍 System data: ID=%s, Name=%s, Connections=%v",
		state.System.ID, state.System.Name, state.System.Connections)
	logger.Printf("   POIs count: %d", len(state.System.POIs))

	// Convert game state to knowledge.System
	kbSystem := knowledge.System{
		ID:              state.System.ID,
		Name:            state.System.Name,
		PoliceLevel:     state.System.PoliceLevel,
		Empire:          state.System.Empire,
		IsStronghold:    state.System.IsStronghold,
		Connections:     extractConnections(state.System.Connections),
		LastUpdatedTick: state.CurrentTick,
		Position: game.Position{
			X: state.System.Position.X,
			Y: state.System.Position.Y,
			Z: 0,
		},
	}

	// Remember the system in knowledge base
	if err := kb.RememberSystem(ctx, kbSystem); err != nil {
		logger.Printf("⚠️  Failed to save system to knowledge base: %v", err)
	} else {
		logger.Printf("💾 Saved system to knowledge base: %s", state.System.Name)
	}

	// Perform system survey to scan for hidden POIs
	logger.Printf("🔭 Surveying system for hidden POIs...")
	if err := client.SurveySystem(ctx); err != nil {
		logger.Printf("⚠️  Survey failed (may not have survey scanner): %v", err)
	} else {
		logger.Printf("✓ Survey complete")
		time.Sleep(3 * time.Second)
	}

	// Scan the system
	if err := client.Scan(ctx); err != nil {
		logger.Printf("Scan failed (may not have scanner): %v", err)
	}
	time.Sleep(3 * time.Second)

	return nil
}

func saveStationData(client *game.Client, ctx context.Context, logger *log.Logger, kb knowledge.Base, systemName, poiName, poiID string, agentID string) error {
	state := client.GetState()

	// Check if market listings were already captured today
	hasMarketToday, err := kb.HasMarketSnapshotToday(ctx, state.System.ID, poiID)
	if err != nil {
		logger.Printf("⚠️  Error checking for today's market snapshot: %v", err)
	}

	// Get market listings (only if not captured today)
	if !hasMarketToday {
		logger.Printf("📊 Getting market listings from %s...", poiName)
		if err := client.GetListings(ctx); err != nil {
			logger.Printf("Failed to get listings: %v", err)
		} else {
			time.Sleep(2 * time.Second)

			listings := client.GetMarketListings()
			marketSnapshot := convertMarketListingsToKnowledge(state.System.ID, systemName, poiID, poiName, state.CurrentTick, listings)
			if err := kb.StoreMarketSnapshot(ctx, marketSnapshot, agentID); err != nil {
				logger.Printf("⚠️  Failed to save market snapshot to knowledge base: %v", err)
			} else {
				logger.Printf("💾 Saved market snapshot to knowledge base")
			}
		}
	} else {
		logger.Printf("✓ Market listings already captured today for %s", poiName)
	}

	// Check if ship listings were already captured today
	hasShipsToday, err := kb.HasShipListingsToday(ctx, state.System.ID, poiID)
	if err != nil {
		logger.Printf("⚠️  Error checking for today's ship listings: %v", err)
	}

	// Get ship listings (only if not captured today)
	if !hasShipsToday {
		logger.Printf("🚢 Getting ship listings from %s...", poiName)
		if err := client.GetShips(ctx); err != nil {
			logger.Printf("Failed to get ship listings: %v", err)
		} else {
			time.Sleep(2 * time.Second)

			rawJSON := client.GetRawJSON("ships")
			if rawJSON != nil {
				var serverData map[string]any
				if err := json.Unmarshal(rawJSON, &serverData); err == nil {
					ships := extractShipListings(serverData)
					shipListings := convertShipListingsToKnowledge(state.System.ID, systemName, poiID, poiName, state.CurrentTick, ships)
					if err := kb.StoreShipListings(ctx, shipListings, agentID); err != nil {
						logger.Printf("⚠️  Failed to save ship listings to knowledge base: %v", err)
					} else {
						logger.Printf("💾 Saved ship listings to knowledge base")
					}
				}
			}
		}
	} else {
		logger.Printf("✓ Ship listings already captured today for %s", poiName)
	}

	return nil
}

func convertMarketListingsToKnowledge(systemID, systemName, stationID, stationName string, gameTick int64, gameListings []game.MarketListing) knowledge.MarketSnapshot {
	listings := make([]knowledge.MarketListing, len(gameListings))
	for i, l := range gameListings {
		listings[i] = knowledge.MarketListing{
			ItemID:       l.ItemID,
			ItemType:     l.ItemType,
			Quantity:     l.Quantity,
			PricePerUnit: l.PricePerUnit,
			TotalPrice:   l.TotalPrice,
			Type:         l.Type,
			ListedBy:     l.ListedBy,
		}
	}

	return knowledge.MarketSnapshot{
		SystemID:    systemID,
		SystemName:  systemName,
		StationID:   stationID,
		StationName: stationName,
		GameTick:    gameTick,
		Listings:    listings,
	}
}

func convertShipListingsToKnowledge(systemID, systemName, stationID, stationName string, gameTick int64, ships []knowledge.ShipListing) knowledge.ShipListings {
	return knowledge.ShipListings{
		SystemID:    systemID,
		SystemName:  systemName,
		StationID:   stationID,
		StationName: stationName,
		GameTick:    gameTick,
		Listings:    ships,
	}
}

func extractShipListings(serverData map[string]any) []knowledge.ShipListing {
	var ships []knowledge.ShipListing

	shipsData, ok := serverData["ships"]
	if !ok {
		return ships
	}

	shipsArray, ok := shipsData.([]any)
	if !ok {
		return ships
	}

	for _, shipData := range shipsArray {
		shipMap, ok := shipData.(map[string]any)
		if !ok {
			continue
		}

		ship := knowledge.ShipListing{}

		if id, ok := shipMap["id"].(string); ok {
			ship.ShipClass = id
		}
		if name, ok := shipMap["name"].(string); ok {
			ship.ShipName = name
		}
		if price, ok := shipMap["price"].(float64); ok {
			ship.BasePrice = price
		}
		if desc, ok := shipMap["description"].(string); ok {
			ship.Description = desc
		}
		if cargo, ok := shipMap["cargo_space"].(float64); ok {
			ship.CargoSpace = int(cargo)
		}
		if modules, ok := shipMap["module_slots"].(float64); ok {
			ship.ModuleSlots = int(modules)
		}
		if utility, ok := shipMap["utility_slots"].(float64); ok {
			ship.UtilitySlots = int(utility)
		}
		if weapons, ok := shipMap["weapon_slots"].(float64); ok {
			ship.WeaponSlots = int(weapons)
		}

		ships = append(ships, ship)
	}

	return ships
}

// ============================================================================
// NAVIGATION (Direct jumping, no jump gates)
// ============================================================================

func findRouteToSystem(client *game.Client, ctx context.Context, targetSystem string) error {
	// Use find_route API to get the path
	msg := protocol.Message{
		Type: "find_route",
		Payload: map[string]any{
			"target_system": targetSystem,
		},
	}
	return client.Send(ctx, msg)
}

func jumpToSystem(client *game.Client, ctx context.Context, targetSystem string) error {
	logger := log.New(os.Stdout, "[JUMP] ", log.LstdFlags)

	state := client.GetState()

	// Check if we're already there
	if state.CurrentSystem == targetSystem {
		logger.Printf("✓ Already at target system %s", targetSystem)
		return nil
	}

	// Verify target is in connections
	isConnected := false
	for _, conn := range state.System.Connections {
		if conn.SystemID == targetSystem {
			isConnected = true
			break
		}
	}

	if !isConnected {
		return fmt.Errorf("system %s is not connected to current system %s", targetSystem, state.CurrentSystem)
	}

	// Find jump gate in current system
	jumpGate := game.FindJumpGate(state)
	if jumpGate == nil {
		return fmt.Errorf("no jump gate found in current system %s", state.CurrentSystem)
	}

	// Undock if docked (can't travel while docked)
	if state.Doc {
		logger.Printf("📤 Undocking before traveling to jump gate...")
		if err := client.Undock(ctx); err != nil && err.Error() != "Already undocked (success)" {
			return fmt.Errorf("failed to undock: %w", err)
		}
		time.Sleep(game.SleepUndock)
		state = client.GetState()
	}

	// Travel to jump gate if not already there
	if state.CurrentPOI != jumpGate.ID {
		logger.Printf("🚶 Traveling to jump gate: %s", jumpGate.Name)
		if err := client.Travel(ctx, jumpGate.ID); err != nil {
			return fmt.Errorf("failed to travel to jump gate: %w", err)
		}
		time.Sleep(game.SleepTravel)
	}

	// Attempt jump with retry for action_pending errors
	for attempt := range 3 {
		if attempt > 0 {
			logger.Printf("⏳ Waiting for pending action to complete...")
			time.Sleep(game.SleepTick)
		}

		logger.Printf("🌟 Jumping to %s...", targetSystem)
		err := client.Jump(ctx, targetSystem)
		if err == nil {
			// Jump initiated successfully, wait for travel
			time.Sleep(game.SleepJump)
			return nil
		}

		// Check if this is an action_pending error
		if strings.Contains(err.Error(), "action_pending") || strings.Contains(err.Error(), "already pending") {
			logger.Printf("⚠️  Action pending (attempt %d/3), will retry...", attempt+1)
			continue
		}

		// Other error - return immediately
		return fmt.Errorf("failed to jump: %w", err)
	}

	return fmt.Errorf("failed to jump after 3 attempts (action_pending)")
}

func navigateToHomeBase(client *game.Client, ctx context.Context, homeSystem string) error {
	logger := log.New(os.Stdout, "[NAV] ", log.LstdFlags)

	state := client.GetState()

	if state.CurrentSystem == homeSystem {
		logger.Printf("✓ Already at home system %s", homeSystem)
		return nil
	}

	logger.Printf("🏠 Finding route to home base: %s", homeSystem)

	// Use find_route to get the path
	if err := findRouteToSystem(client, ctx, homeSystem); err != nil {
		logger.Printf("Failed to find route: %v", err)
		// Fall back to direct jump if connected
		return jumpToSystem(client, ctx, homeSystem)
	}

	time.Sleep(2 * time.Second)

	// TODO: Parse the route response and follow it step by step
	// For now, just try direct jump
	return jumpToSystem(client, ctx, homeSystem)
}

// ============================================================================
// POI EXPLORATION
// ============================================================================

func exploreAllPOIs(client *game.Client, ctx context.Context, logger *log.Logger, expState *ExplorationState, kb knowledge.Base) error {
	state := client.GetState()

	if expState.VisitedPOIs == nil {
		expState.VisitedPOIs = make(map[string]bool)
	}

	logger.Printf("🔍 Exploring %d POIs in system %s", len(state.System.POIs), state.System.Name)

	// Load known POIs from knowledge base to check freshness
	knownPOIs := make(map[string]int64) // poiID → lastUpdatedTick
	if dbPOIs, err := kb.GetPOIs(ctx, state.System.ID); err == nil {
		for _, p := range dbPOIs {
			knownPOIs[p.ID] = p.LastUpdatedTick
		}
	}
	currentTick := state.GetTick()

	for _, poi := range state.System.POIs {
		if expState.VisitedPOIs[poi.ID] {
			logger.Printf("⊙ Already visited POI: %s (%s)", poi.Name, poi.ID)
			continue
		}

		// Check if POI data is still fresh in the knowledge base
		if lastTick, ok := knownPOIs[poi.ID]; ok {
			threshold := game.POIFreshnessThreshold(poi.Type)
			if currentTick-lastTick < threshold {
				logger.Printf("⊙ Skipping POI %s (%s) - data still fresh (age: %d ticks, threshold: %d)",
					poi.Name, poi.Type, currentTick-lastTick, threshold)
				expState.VisitedPOIs[poi.ID] = true
				continue
			}
		}

		logger.Printf("📍 Visiting POI: %s (%s) - Type: %s", poi.Name, poi.ID, poi.Type)

		// Travel to POI if not already there
		if state.CurrentPOI != poi.ID {
			if err := client.Travel(ctx, poi.ID); err != nil {
				logger.Printf("Failed to travel to POI %s: %v", poi.ID, err)
				continue
			}
			logger.Printf("→ Arrived at %s", poi.Name)
			time.Sleep(20 * time.Second)
		}

		// Get POI details — this updates the POI in state with full data (resources, etc.)
		logger.Printf("🔍 Getting POI details at %s...", poi.Name)
		if err := client.GetPOI(ctx); err != nil {
			logger.Printf("Get POI failed: %v", err)
		} else {
			logger.Printf("✅ POI details retrieved at %s", poi.Name)
		}
		time.Sleep(3 * time.Second)

		// Refresh state to pick up the detailed POI data from get_poi response
		state = client.GetState()
		// Find the updated POI in the refreshed state
		for _, updated := range state.System.POIs {
			if updated.ID == poi.ID {
				poi = updated
				break
			}
		}

		// Handle station-specific actions
		if poi.Type == "station" {
			logger.Printf("🏪 Station detected! Docking to collect market and ship data...")

			if err := client.Dock(ctx); err != nil {
				if err.Error() != "Already docked (success)" {
					logger.Printf("Failed to dock: %v", err)
				} else {
					logger.Printf("✅ Already docked at %s", poi.Name)
				}
			} else {
				logger.Printf("✅ Docked at %s", poi.Name)
			}
			time.Sleep(15 * time.Second)

			// Collect station data
			if err := saveStationData(client, ctx, logger, kb, state.System.Name, poi.Name, poi.ID, expState.AgentID); err != nil {
				logger.Printf("Failed to save station data: %v", err)
			}

			// Update last fuel station
			expState.LastFuelStation = state.CurrentSystem

			// Refuel if needed
			state = client.GetState()
			if needsRefuel(state) {
				logger.Printf("⛽ Refueling...")
				if err := client.Refuel(ctx); err != nil {
					logger.Printf("Refuel error: %v", err)
				}
				time.Sleep(3 * time.Second)
			}

			// Undock
			logger.Printf("📤 Undocking from %s...", poi.Name)
			if err := client.Undock(ctx); err != nil {
				logger.Printf("Failed to undock: %v", err)
			} else {
				logger.Printf("✅ Undocked from %s", poi.Name)
			}
			time.Sleep(12 * time.Second)
		}

		// Save POI to knowledge base
		kbPOI := knowledge.POI{
			ID:              poi.ID,
			SystemID:        state.System.ID,
			Name:            poi.Name,
			Type:            poi.Type,
			Description:     poi.Description,
			Position:        poi.Position,
			Services:        []string{},
			Resources:       poi.Resources,
			LastUpdatedTick: state.GetTick(),
		}
		if err := kb.RememberPOI(ctx, kbPOI); err != nil {
			logger.Printf("Failed to save POI to knowledge base: %v", err)
		}

		// Mark as visited
		expState.VisitedPOIs[poi.ID] = true

		time.Sleep(2 * time.Second)
	}

	logger.Printf("✅ Completed POI exploration in %s", state.System.Name)
	return nil
}

// ============================================================================
// COMBAT AND DAMAGE
// ============================================================================

func isDamaged(state *game.State) bool {
	if state.Ship.MaxHull == 0 {
		return false
	}
	hullPercent := (state.Ship.Hull / state.Ship.MaxHull) * 100
	return hullPercent < 50
}

func repairShip(client *game.Client, ctx context.Context, logger *log.Logger, expState *ExplorationState) error {
	state := client.GetState()

	logger.Printf("⚠️  SHIP DAMAGED! Hull: %.0f/%.0f (%.1f%%)",
		state.Ship.Hull, state.Ship.MaxHull, (state.Ship.Hull/state.Ship.MaxHull)*100)

	// Find station in current system
	var stationPOI *game.POI
	for i := range state.System.POIs {
		if state.System.POIs[i].Type == "station" {
			stationPOI = &state.System.POIs[i]
			break
		}
	}

	if stationPOI == nil {
		logger.Printf("❌ No station in current system, returning to home: %s", expState.HomeSystem)
		if err := navigateToHomeBase(client, ctx, expState.HomeSystem); err != nil {
			return err
		}
		// Refresh state and find station
		state = client.GetState()
		for i := range state.System.POIs {
			if state.System.POIs[i].Type == "station" {
				stationPOI = &state.System.POIs[i]
				break
			}
		}
		if stationPOI == nil {
			return fmt.Errorf("no station found in home system")
		}
	}

	// Travel to station
	if state.CurrentPOI != stationPOI.ID {
		logger.Printf("🚀 Traveling to station: %s", stationPOI.Name)
		if err := client.Travel(ctx, stationPOI.ID); err != nil {
			return fmt.Errorf("failed to travel to station: %w", err)
		}
		time.Sleep(20 * time.Second)
	}

	// Dock
	logger.Printf("📥 Docking at %s...", stationPOI.Name)
	if err := client.Dock(ctx); err != nil && err.Error() != "Already docked (success)" {
		return fmt.Errorf("failed to dock: %w", err)
	}
	time.Sleep(15 * time.Second)

	// Repair
	logger.Printf("🔧 Repairing ship...")
	if err := client.Repair(ctx); err != nil {
		return fmt.Errorf("failed to repair: %w", err)
	}
	time.Sleep(3 * time.Second)

	state = client.GetState()
	logger.Printf("✅ Repaired! Hull: %.0f/%.0f (%.1f%%)",
		state.Ship.Hull, state.Ship.MaxHull, (state.Ship.Hull/state.Ship.MaxHull)*100)

	// Undock
	logger.Printf("📤 Undocking from %s...", stationPOI.Name)
	if err := client.Undock(ctx); err != nil {
		logger.Printf("Failed to undock: %v", err)
	}
	time.Sleep(12 * time.Second)

	return nil
}

// ============================================================================
// EXPLORATION LOOP
// ============================================================================

func explorationPhase(client *game.Client, logger *log.Logger, ctx context.Context, kb knowledge.Base, agentID string) error {
	state := client.GetState()

	expState := &ExplorationState{
		VisitedSystems:  make(map[string]bool),
		VisitedPOIs:     make(map[string]bool),
		DFSStack:        []string{},
		HomeSystem:      state.CurrentSystem,
		LastFuelStation: state.CurrentSystem,
		PreviousSystem:  "",
		AgentID:         agentID,
		kb:              kb,
	}

	logger.Printf("Starting DFS exploration from home system: %s", expState.HomeSystem)

	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	updateCaptainsLog(agentID, client, expState)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-logTicker.C:
			updateCaptainsLog(agentID, client, expState)
		default:
		}

		state = client.GetState()
		currentSystem := state.CurrentSystem

		// Check for damage and repair if necessary
		if isDamaged(state) {
			logger.Printf("💔 Ship damaged, seeking repairs...")
			if err := repairShip(client, ctx, logger, expState); err != nil {
				logger.Printf("Failed to repair: %v", err)
				time.Sleep(10 * time.Second)
				continue
			}
			continue
		}

		// Mark current system as visited
		if !expState.VisitedSystems[currentSystem] {
			logger.Printf("📍 Exploring system: %s", currentSystem)
			expState.VisitedSystems[currentSystem] = true
			expState.VisitedPOIs = make(map[string]bool)

			updateCaptainsLog(agentID, client, expState)

			// Check if system data is still fresh in the knowledge base
			systemFresh := false
			if kbSys, err := kb.GetSystem(ctx, currentSystem); err == nil && kbSys != nil {
				if state.GetTick()-kbSys.LastUpdatedTick < game.FreshnessSystem {
					logger.Printf("⊙ System %s data still fresh (age: %d ticks), skipping system collection",
						currentSystem, state.GetTick()-kbSys.LastUpdatedTick)
					systemFresh = true
				}
			}

			// Collect system data only if stale or unknown
			if !systemFresh {
				if err := collectSystemData(client, ctx, logger, kb, expState.AgentID); err != nil {
					logger.Printf("Failed to collect system data: %v", err)
				}
			} else {
				// Even when KB data is fresh, we still need to populate the in-memory
				// state with system data (ID, connections, POIs) so that
				// getUnvisitedNeighbors can find connections to jump to.
				logger.Printf("🔄 Refreshing in-memory system state...")
				if err := client.GetSystem(ctx); err != nil {
					logger.Printf("⚠️  Failed to refresh system state: %v", err)
				}
				time.Sleep(2 * time.Second)
			}

			// Always explore POIs (freshness is checked per-POI inside)
			logger.Printf("🔍 Beginning comprehensive POI exploration...")
			if err := exploreAllPOIs(client, ctx, logger, expState, kb); err != nil {
				logger.Printf("POI exploration failed: %v", err)
			}
		}

		// CRITICAL: Refresh state after collecting data to get current system's connections
		state = client.GetState()
		currentSystem = state.CurrentSystem

		// If System.Name doesn't match CurrentSystem, data may be stale.
		// CurrentSystem is set from System.Name in mergeSystemDataLocked, not from System.ID.
		// System.ID uses server's internal format (e.g., "nexus_prime") while
		// System.Name and CurrentSystem use the display name (e.g., "Nexus Prime").
		if state.System.ID == "" || !strings.EqualFold(state.System.Name, currentSystem) {
			logger.Printf("⚠️  System data not loaded (ID=%q, Name=%q, CurrentSystem=%s), forcing refresh...",
				state.System.ID, state.System.Name, currentSystem)
			if err := client.GetSystem(ctx); err != nil {
				logger.Printf("Failed to get system: %v", err)
			}
			time.Sleep(3 * time.Second)
			state = client.GetState()
			currentSystem = state.CurrentSystem
		}

		// Get unvisited neighbors from the CURRENT system (not the previous one)
		unvisited := getUnvisitedNeighbors(state, expState)

		if len(unvisited) > 0 {
			// Push current system to stack and explore first unvisited neighbor
			expState.DFSStack = append(expState.DFSStack, currentSystem)
			nextSystem := unvisited[0]
			logger.Printf("→ Moving to unvisited system: %s (Stack depth: %d)", nextSystem, len(expState.DFSStack))

			// Check fuel before jump
			if state.Fuel < 30 {
				logger.Printf("⚠️  Low fuel, refueling before jump...")
				// FindAndRefuel would go here - for now just try to jump
			}

			if err := jumpToSystem(client, ctx, nextSystem); err != nil {
				logger.Printf("Navigation error: %v", err)
				time.Sleep(10 * time.Second)

				// Check if we actually arrived
				state = client.GetState()
				if state.CurrentSystem == nextSystem {
					logger.Printf("✓ Despite error, we successfully arrived at %s", nextSystem)
				} else {
					// Pop the system from stack
					if len(expState.DFSStack) > 0 {
						expState.DFSStack = expState.DFSStack[:len(expState.DFSStack)-1]
					}
				}
			}
		} else {
			// All neighbors visited - backtrack
			if len(expState.DFSStack) == 0 {
				// Exploration complete! Reset and continue
				logger.Printf("🎉 Galaxy exploration complete! %d systems explored", len(expState.VisitedSystems))
				logger.Printf("Resetting and continuing exploration...")

				// Return home
				if state.CurrentSystem != expState.HomeSystem {
					logger.Printf("🏠 Returning to home system...")
					if err := navigateToHomeBase(client, ctx, expState.HomeSystem); err != nil {
						logger.Printf("Failed to return home: %v", err)
					}
				}

				// Reset exploration state
				expState.VisitedSystems = make(map[string]bool)
				expState.VisitedPOIs = make(map[string]bool)
				expState.DFSStack = []string{}
				expState.PreviousSystem = ""
				time.Sleep(30 * time.Second)
				continue
			}

			// Pop from stack and backtrack
			backtrackSystem := expState.DFSStack[len(expState.DFSStack)-1]
			expState.DFSStack = expState.DFSStack[:len(expState.DFSStack)-1]
			logger.Printf("← Backtracking to: %s (Stack depth: %d)", backtrackSystem, len(expState.DFSStack))

			if err := jumpToSystem(client, ctx, backtrackSystem); err != nil {
				logger.Printf("Backtrack error: %v", err)
				time.Sleep(10 * time.Second)

				state = client.GetState()
				if state.CurrentSystem == backtrackSystem {
					logger.Printf("Despite error, we successfully backtracked to %s", backtrackSystem)
				} else {
					// Put system back on stack
					expState.DFSStack = append(expState.DFSStack, backtrackSystem)
				}
			}
		}

		time.Sleep(5 * time.Second)
	}
}

func getUnvisitedNeighbors(state *game.State, expState *ExplorationState) []string {
	unvisited := []string{}
	logger := log.New(os.Stdout, "[DEBUG] ", log.LstdFlags)

	// Safety check: if System.Name doesn't match CurrentSystem, data may be stale.
	// CurrentSystem is set from System.Name in mergeSystemDataLocked, not from System.ID.
	// System.ID uses server's internal format (e.g., "nexus_prime") while
	// System.Name and CurrentSystem use the display name (e.g., "Nexus Prime").
	if state.System.ID == "" {
		logger.Printf("⚠️  System.ID is empty, CurrentSystem=%s - system data not yet loaded", state.CurrentSystem)
		logger.Printf("     Returning empty unvisited list to force data refresh")
		return []string{}
	}
	if !strings.EqualFold(state.System.Name, state.CurrentSystem) {
		logger.Printf("⚠️  STALE DATA: System.Name=%s doesn't match CurrentSystem=%s", state.System.Name, state.CurrentSystem)
		logger.Printf("     This indicates system data wasn't refreshed after a jump!")
		logger.Printf("     Returning empty unvisited list to force data refresh")
		return []string{}
	}

	logger.Printf("Current system: %s, Connections: %v", state.System.ID, state.System.Connections)
	logger.Printf("Visited systems: %d", len(expState.VisitedSystems))

	// Check if current system has a jump gate - can't jump without one
	jumpGate := game.FindJumpGate(state)
	if jumpGate == nil {
		logger.Printf("⚠️  No jump gate in current system %s - dead end, cannot jump", state.System.Name)
		logger.Printf("     Returning empty unvisited list")
		return []string{}
	}
	logger.Printf("✓ Found jump gate: %s", jumpGate.Name)

	// Collect unvisited neighbors with their LastUpdatedTick from KB
	type neighborInfo struct {
		SystemID         string
		LastUpdatedTick  int64
	}

	var neighbors []neighborInfo
	for _, conn := range state.System.Connections {
		if !expState.VisitedSystems[conn.SystemID] {
			neighbors = append(neighbors, neighborInfo{
				SystemID:        conn.SystemID,
				LastUpdatedTick: 0, // 0 means never visited/unknown
			})
		}
	}

	// If we have unvisited neighbors, query KB for their LastUpdatedTick
	if len(neighbors) > 0 && expState.kb != nil {
		allSystems := expState.kb.GetSystems()
		systemTickMap := make(map[string]int64)
		for _, sys := range allSystems {
			systemTickMap[sys.ID] = sys.LastUpdatedTick
		}

		// Update LastUpdatedTick for each neighbor
		for i := range neighbors {
			if tick, ok := systemTickMap[neighbors[i].SystemID]; ok {
				neighbors[i].LastUpdatedTick = tick
			}
		}

		// Sort by LastUpdatedTick (oldest first, then 0/unknown systems)
		// This prioritizes systems that haven't been visited in a long time
		for i := 0; i < len(neighbors); i++ {
			for j := i + 1; j < len(neighbors); j++ {
				// 0 (unknown) should come before positive ticks
				// Lower ticks come before higher ticks
				if neighbors[i].LastUpdatedTick > neighbors[j].LastUpdatedTick {
					neighbors[i], neighbors[j] = neighbors[j], neighbors[i]
				}
			}
		}

		logger.Printf("Unvisited neighbors (sorted by LastUpdatedTick):")
		for _, n := range neighbors {
			age := "unknown"
			if n.LastUpdatedTick > 0 {
				age = fmt.Sprintf("tick %d", n.LastUpdatedTick)
			}
			logger.Printf("  - %s (last: %s)", n.SystemID, age)
		}
	}

	// Extract just the system IDs in order
	for _, n := range neighbors {
		unvisited = append(unvisited, n.SystemID)
	}

	logger.Printf("Unvisited neighbors: %v", unvisited)
	return unvisited
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: auto-explorer <explorer-number>")
		fmt.Println("Example: auto-explorer 1")
		fmt.Println("\nFlags:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	explorer := args[0]
	logger := log.New(os.Stdout, fmt.Sprintf("[EXPLORER-%s] ", explorer), log.LstdFlags)

	// Check captain's log for previous mission
	previousLog, err := game.ReadLatestCaptainsLog(explorer)
	if err != nil {
		logger.Printf("Failed to read captain's log: %v", err)
	} else if previousLog != nil {
		logger.Printf("📖 Captain's Log - Last Entry:")
		logger.Printf("   Mission: %s", previousLog.CurrentGoal)
		logger.Printf("   Location: %s", previousLog.Location)
		logger.Printf("   Time: %s", previousLog.Timestamp.Format("2006-01-02 15:04"))
		if len(previousLog.Notes) > 0 {
			logger.Printf("   Last Status:")
			for _, note := range previousLog.Notes {
				logger.Printf("      - %s", note)
			}
		}
	}

	// Load credentials
	creds, err := game.LoadCredentials(fmt.Sprintf("data/agents/%s", explorer))
	if err != nil {
		log.Fatalf("Failed to load credentials: %v", err)
	}

	logger.Printf("🔭 Starting autonomous explorer bot...")
	logger.Printf("Explorer: %s | Empire: %s", creds.Username, creds.Empire)

	ctx := context.Background()

	// Initialize knowledge base
	kb, err := initKnowledgeBase(*dbBackend, *dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize knowledge base: %v", err)
	}
	defer func() {
		if err := kb.Close(); err != nil {
			logger.Printf("Warning: Failed to close knowledge base: %v", err)
		}
	}()
	logger.Printf("✓ Knowledge base initialized (%s)", *dbBackend)

	// Register with status registry if configured
	var regClient *registry.Client
	if *registryURL != "" {
		toolID := fmt.Sprintf("auto-explorer-%s", explorer)
		regClient = registry.NewClient(*registryURL, toolID)

		reg := registry.ToolRegistration{
			ToolID:    toolID,
			ToolType:  registry.ToolTypeAutoExplorer,
			PID:       os.Getpid(),
			AgentID:   explorer,
			AgentName: creds.Username,
			AgentRole: "Explorer",
			Status:    "starting",
			Capabilities: map[string]any{
				"state_file": fmt.Sprintf("data/agents/%s/state.json", explorer),
			},
			Metadata: map[string]any{
				"empire": creds.Empire,
			},
		}

		if err := regClient.Register(reg); err != nil {
			logger.Printf("⚠ Warning: Failed to register with status registry: %v", err)
			regClient = nil
		} else {
			logger.Printf("✓ Registered with status registry")
			if err := regClient.UpdateStatus("starting", "Initializing"); err != nil {
				logger.Printf("Warning: Failed to update status: %v", err)
			}
		}
	}

	// Create game client (don't use InitializeAgent since we need custom handler)
	gameLogger := log.New(os.Stdout, fmt.Sprintf("[%s-GAME] ", explorer), log.LstdFlags)
	client := game.NewClient(game.DefaultGameServerURL, creds.Username, creds.Password, gameLogger)
	client.SetDebugLogging(*debug)
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	// Set up explorer-specific handler BEFORE connecting
	explorerHandler := &explorerSimpleHandler{
		client: client,
		logger: logger,
		kb:     kb,
	}
	reconnectingHandler := game.NewReconnectingHandler(client, explorerHandler, ctx, logger)
	client.SetHandler(reconnectingHandler)

	// Start heartbeat for registry if registered
	if regClient != nil {
		regClient.StartHeartbeat(ctx, 5*time.Second, func() (status, action string) {
			state := client.GetState()
			if state == nil {
				return "starting", "Connecting to game"
			}
			return "exploring", fmt.Sprintf("In %s (%.0f credits)", state.System.Name, state.Credits)
		})
	}

	// Connect to game
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
		if regClient != nil {
			if err := regClient.Deregister(); err != nil {
				logger.Printf("Warning: Failed to deregister: %v", err)
			}
		}
	}()

	// Wait for connection
	<-client.Ready()
	time.Sleep(1 * time.Second)

	// Login
	logger.Printf("Logging in...")
	if regClient != nil {
		if err := regClient.UpdateStatus("connecting", "Logging in"); err != nil {
			logger.Printf("Warning: Failed to update status: %v", err)
		}
	}
	if err := client.Login(ctx); err != nil {
		log.Fatalf("Failed to login: %v", err)
	}

	time.Sleep(2 * time.Second)

	state := client.GetState()
	logger.Printf("✓ Ready! Credits: %.2f | Ship: %s", state.Credits, state.Ship.Name)

	// GALAXY EXPLORATION
	logger.Printf("")
	logger.Printf("═══════════════════════════════════════════════")
	logger.Printf("      GALAXY EXPLORATION (DFS)")
	logger.Printf("═══════════════════════════════════════════════")
	logger.Printf("")

	if err := explorationPhase(client, logger, ctx, kb, explorer); err != nil {
		log.Fatalf("Exploration phase error: %v", err)
	}
}

func initKnowledgeBase(backend, dbPath string) (knowledge.Base, error) {
	switch backend {
	case "sqlite":
		return knowledge.NewSQLiteKB(knowledge.Config{
			DBPath:       dbPath,
			WAL:          true,
			MaxOpenConns: 25,
			MaxIdleConns: 5,
			BusyTimeout:  5 * time.Second,
		})
	case "memory":
		return knowledge.NewMemoryKB(), nil
	default:
		return nil, fmt.Errorf("unknown db-backend: %s (use 'sqlite' or 'memory')", backend)
	}
}
