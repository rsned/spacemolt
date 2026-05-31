package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/registry"
)

// CLI flags
var (
	registryURL = flag.String("registry-url", "", "Status registry URL (e.g., http://localhost:8081)")
	dbBackend   = flag.String("db-backend", "sqlite", "Knowledge base backend: sqlite or memory")
	dbPath      = flag.String("db-path", "data/spacemolt-knowledge.db", "Path to SQLite database")
	debug        = flag.Bool("debug", false, "Enable debug logging")
	forceRescan  = flag.Bool("force", false, "Force rescan of all POIs and systems, ignoring freshness checks")
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

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func updateCaptainsLog(agentID string, client game.GameClient, expState *ExplorationState) {
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

// waitForTransitComplete polls the game state until the agent is no longer
// traveling (jump or intra-system travel). It refreshes status periodically
// so the state picks up the server's arrival event.
func waitForTransitComplete(client game.GameClient, ctx context.Context, logger *log.Logger) error {
	const maxWait = 2 * time.Minute
	deadline := time.Now().Add(maxWait)

	for {
		if err := client.GetStatus(ctx); err != nil {
			return fmt.Errorf("failed to refresh status during transit wait: %w", err)
		}
		time.Sleep(game.SleepQuick)
		state := client.GetState()

		if !state.Traveling && state.System.ID != "" {
			logger.Printf("✓ Transit complete — arrived in %s", state.System.Name)
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for transit to complete after %v", maxWait)
		}

		logger.Printf("⏳ Still in transit (Traveling=%v, System.ID=%q), waiting...",
			state.Traveling, state.System.ID)
		time.Sleep(game.SleepTick)
	}
}

// ============================================================================
// DATA COLLECTION (Uses Knowledge Base)
// ============================================================================

func collectSystemData(client game.GameClient, ctx context.Context, logger *log.Logger, kb knowledge.Base, agentID string) error {
	// If system data isn't loaded yet (empty ID), fetch full status first
	state := client.GetState()
	if state.System.ID == "" {
		logger.Printf("⚠️  No system data loaded, fetching full status...")
		if err := client.GetStatus(ctx); err != nil {
			return fmt.Errorf("failed to get status: %w", err)
		}
		time.Sleep(game.SleepQuick)
		state = client.GetState()

		// If still no system data after GetStatus, we're likely in transit
		if state.System.ID == "" || state.Traveling {
			logger.Printf("⏳ Agent is in transit, waiting for arrival...")
			if err := waitForTransitComplete(client, ctx, logger); err != nil {
				return fmt.Errorf("transit wait failed: %w", err)
			}
			state = client.GetState()
		}

		// If still no system data, we can't proceed
		if state.System.ID == "" {
			return fmt.Errorf("no system data available after retry (agent may be in transit)")
		}

		logger.Printf("✓ System data loaded: %s", state.System.Name)
	}

	// Now request system data (includes jump connections)
	if err := client.GetSystem(ctx); err != nil {
		return fmt.Errorf("failed to get system: %w", err)
	}
	time.Sleep(2 * time.Second)

	state = client.GetState()

	// Debug: Log what we got
	logger.Printf("🔍 System data: ID=%s, Name=%s, Connections=%v, Tick=%d",
		state.System.ID, state.System.Name, state.System.Connections, state.GetTick())
	logger.Printf("   POIs count: %d", len(state.System.POIs))

	// Validate we have system data before trying to save
	if state.System.ID == "" {
		return fmt.Errorf("cannot save system data: system ID is empty (agent may be in transit)")
	}

	// Convert game state to knowledge.System
	kbSystem := knowledge.System{
		ID:              state.System.ID,
		Name:            state.System.Name,
		PoliceLevel:     state.System.PoliceLevel,
		SecurityStatus:  state.System.SecurityStatus,
		Empire:          state.System.Empire,
		IsStronghold:    state.System.IsStronghold,
		Connections:     extractConnections(state.System.Connections),
		LastUpdatedTick: state.GetTick(),
		LastVisitedTick: state.GetTick(),
		Position: game.Position{
			X: state.System.Position.X,
			Y: state.System.Position.Y,
		},
	}

	// Remember the system in knowledge base
	if err := kb.RememberSystem(ctx, kbSystem); err != nil {
		logger.Printf("⚠️  Failed to save system to knowledge base: %v", err)
	} else {
		logger.Printf("💾 Saved system to knowledge base: %s (tick: %d)", state.System.Name, kbSystem.LastUpdatedTick)
	}

	// Save basic POI data from the system response (full details come from exploreAllPOIs)
	for _, poi := range state.System.POIs {
		kbPOI := knowledge.POI{
			ID:       poi.ID,
			SystemID: state.System.ID,
			Name:     poi.Name,
			Type:     poi.Type,
			Class:    poi.Class,
			Position: game.Position{
				X: poi.Position.X,
				Y: poi.Position.Y,
			},
			Hidden:           poi.Hidden,
			RevealDifficulty: poi.RevealDifficulty,
			LastUpdatedTick:  state.GetTick(),
		}
		if err := kb.RememberPOI(ctx, kbPOI); err != nil {
			logger.Printf("⚠️  Failed to save POI %s: %v", poi.Name, err)
		}
	}
	logger.Printf("💾 Saved %d POIs from system data", len(state.System.POIs))

	// Perform system survey to scan for hidden POIs (requires a survey scanner module)
	if hasSurveyScanner(client) {
		logger.Printf("🔭 Surveying system for hidden POIs...")
		if err := client.SurveySystem(ctx); err != nil {
			logger.Printf("⚠️  Survey failed: %v", err)
		} else {
			time.Sleep(game.SleepQuick)
			processSurveyResults(client, ctx, logger, kb, state.System.ID, state.GetTick())
		}
	}

	// Scan the system
	if err := client.Scan(ctx); err != nil {
		logger.Printf("Scan failed (may not have scanner): %v", err)
	}
	time.Sleep(3 * time.Second)

	return nil
}

// hasSurveyScanner checks if the ship has any survey scanner module installed.
func hasSurveyScanner(client game.GameClient) bool {
	state := client.GetState()
	for _, mod := range state.Ship.Modules {
		switch mod {
		case "survey_scanner_i", "survey_scanner_ii",
			"survey_scanner_1", "survey_scanner_2",
			"deep_core_survey_scanner", "deep_core_scanner":
			return true
		}
	}
	return false
}

// processSurveyResults parses the survey_system response and stores discovered
// POIs and faint signatures in the knowledge base.
func processSurveyResults(client game.GameClient, ctx context.Context, logger *log.Logger, kb knowledge.Base, systemID string, tick int64) {
	rawJSON := client.GetRawJSON("survey")
	if rawJSON == nil {
		logger.Printf("⚠️  No survey response data available")
		return
	}

	var resp serverapi.SurveySystemResponse
	if err := json.Unmarshal(rawJSON, &resp); err != nil {
		logger.Printf("⚠️  Failed to parse survey response: %v", err)
		return
	}

	// Store newly revealed POIs as full POI records
	for _, revealed := range resp.NewlyRevealed {
		kbPOI := knowledge.POI{
			ID:              revealed.ID,
			SystemID:        systemID,
			Name:            revealed.Name,
			Type:            revealed.Type,
			Description:     revealed.Description,
			LastUpdatedTick: tick,
		}
		// Copy survey resources to game POI resources. Server now returns
		// numeric richness/remaining directly (not string tier labels).
		for _, sr := range revealed.Resources {
			kbPOI.Resources = append(kbPOI.Resources, game.POIResource{
				ResourceID: sr.ResourceID,
				Richness:   sr.Richness,
				Remaining:  sr.Remaining,
			})
		}
		if err := kb.RememberPOI(ctx, kbPOI); err != nil {
			logger.Printf("⚠️  Failed to save revealed POI %s: %v", revealed.Name, err)
		} else {
			logger.Printf("🆕 Revealed POI: %s (%s)", revealed.Name, revealed.Type)
		}
	}

	// Store faint signatures as placeholder POI records for later investigation
	for i, sig := range resp.FaintSignatures {
		// Generate a deterministic placeholder ID from system + signature index + type
		placeholderID := fmt.Sprintf("faint_%s_%s_%d", systemID, sig.Type, i)
		name := "Faint Signature"
		if sig.Hint != "" {
			name = fmt.Sprintf("Faint Signature: %s", sig.Hint)
		}
		desc := fmt.Sprintf("Unresolved survey signature (type: %s, difficulty: %s). Requires better scanner or higher skills to identify.", sig.Type, sig.Difficulty)

		kbPOI := knowledge.POI{
			ID:              placeholderID,
			SystemID:        systemID,
			Name:            name,
			Type:            "faint_signature",
			Description:     desc,
			LastUpdatedTick: tick,
		}
		if err := kb.RememberPOI(ctx, kbPOI); err != nil {
			logger.Printf("⚠️  Failed to save faint signature: %v", err)
		} else {
			logger.Printf("🔮 Faint signature recorded: %s (difficulty: %s)", sig.Type, sig.Difficulty)
		}
	}

	total := len(resp.NewlyRevealed) + len(resp.FaintSignatures)
	if total > 0 {
		logger.Printf("📡 Survey results: %d revealed, %d faint signatures (power: %d)",
			len(resp.NewlyRevealed), len(resp.FaintSignatures), resp.SurveyPower)
	} else if len(resp.AlreadyRevealed) > 0 {
		logger.Printf("📡 Survey: %d already known POIs, nothing new (power: %d)",
			len(resp.AlreadyRevealed), resp.SurveyPower)
	} else {
		logger.Printf("📡 Survey complete: no signatures detected (power: %d)", resp.SurveyPower)
	}
}

func saveStationData(client game.GameClient, ctx context.Context, logger *log.Logger, kb knowledge.Base, systemName, poiName, poiID string, agentID string) error {
	state := client.GetState()

	// Fetch base details from the server and save to knowledge base
	logger.Printf("🏗️  Getting base details for %s...", poiName)
	if err := client.GetBase(ctx); err != nil {
		logger.Printf("⚠️  Failed to get base details: %v", err)
	} else {
		time.Sleep(game.SleepQuick)

		rawJSON := client.GetRawJSON("base")
		if rawJSON != nil {
			kbBase, err := knowledge.BaseDataFromRawJSON(rawJSON, agentID, state.CurrentTick)
			if err != nil {
				logger.Printf("⚠️  Failed to parse base response: %v", err)
			} else {
				// Preserve dock story if available
				if story := state.LastDockStory; story != "" {
					kbBase.Story = story
				}
				if err := kb.RememberBase(ctx, *kbBase); err != nil {
					logger.Printf("⚠️  Failed to save base to knowledge base: %v", err)
				} else {
					logger.Printf("💾 Saved base details for %s (%d facilities)", poiName, len(kbBase.Facilities))
				}
			}
		}
	}

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

	// Get ship listings (only if not captured today). The server replaced
	// shipyard_showroom with browse_ships; pass nil payload to fetch the
	// full listing without category/scale filters.
	if !hasShipsToday {
		if wsClient, ok := client.(*game.Client); ok {
			logger.Printf("🚢 Getting ship listings from %s...", poiName)
			if err := wsClient.BrowseShips(ctx, nil); err != nil {
				logger.Printf("Failed to get ship listings: %v", err)
			} else {
				rawJSON := client.GetRawJSON("listings")
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
			logger.Printf("⊙ Ship listings not available via MCP transport")
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

	// browse_ships response has a "listings" array (the old shipyard_showroom
	// returned "ships"); per-listing fields are also slimmer — there's no
	// description/cargo_space/utility_slots/weapon_slots at this level. Map
	// what's available; downstream consumers tolerate zero defaults.
	listingsData, ok := serverData["listings"]
	if !ok {
		return ships
	}

	listingsArray, ok := listingsData.([]any)
	if !ok {
		return ships
	}

	for _, listingData := range listingsArray {
		listingMap, ok := listingData.(map[string]any)
		if !ok {
			continue
		}

		ship := knowledge.ShipListing{}

		if classID, ok := listingMap["class_id"].(string); ok {
			ship.ShipClass = classID
		}
		if name, ok := listingMap["ship_name"].(string); ok {
			ship.ShipName = name
		}
		if price, ok := listingMap["price"].(float64); ok {
			ship.BasePrice = price
		}
		if modules, ok := listingMap["modules_count"].(float64); ok {
			ship.ModuleSlots = int(modules)
		}

		ships = append(ships, ship)
	}

	return ships
}

// ============================================================================
// CHANGE DETECTION
// ============================================================================

// systemChangeResult holds the outcome of comparing server data against the DB.
type systemChangeResult struct {
	SystemChanged bool     // system-level fields changed
	POIsChanged   bool     // POI count, types, or positions changed
	ChangedPOIIDs []string // IDs of POIs that differ from DB
	NewPOIIDs     []string // IDs of POIs not in DB
	RemovedPOIIDs []string // IDs of POIs in DB but not on server
}

// detectAndRecordChanges compares the current server state for a system against
// what is stored in the knowledge base. If differences are found, it snapshots
// the old data into change_snapshots and returns which entities changed.
func detectAndRecordChanges(ctx context.Context, logger *log.Logger, kb knowledge.Base, state *game.State, agentID string) (*systemChangeResult, error) {
	result := &systemChangeResult{}
	systemID := state.System.ID
	currentTick := state.GetTick()

	// --- System-level comparison ---
	kbSys, err := kb.GetSystem(ctx, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get system from KB: %w", err)
	}
	if kbSys == nil {
		// System not in DB at all — everything is new, no snapshot needed
		logger.Printf("🆕 System %s not in KB — first visit", systemID)
		result.SystemChanged = true
		result.POIsChanged = true
		for _, poi := range state.System.POIs {
			result.NewPOIIDs = append(result.NewPOIIDs, poi.ID)
		}
		return result, nil
	}

	// Compare system fields
	var sysChanges []string
	if kbSys.Name != state.System.Name {
		sysChanges = append(sysChanges, fmt.Sprintf("name: %q→%q", kbSys.Name, state.System.Name))
	}
	if kbSys.Empire != state.System.Empire {
		sysChanges = append(sysChanges, fmt.Sprintf("empire: %q→%q", kbSys.Empire, state.System.Empire))
	}
	if kbSys.PoliceLevel != state.System.PoliceLevel {
		sysChanges = append(sysChanges, fmt.Sprintf("police_level: %d→%d", kbSys.PoliceLevel, state.System.PoliceLevel))
	}
	if kbSys.SecurityStatus != state.System.SecurityStatus {
		sysChanges = append(sysChanges, fmt.Sprintf("security: %q→%q", kbSys.SecurityStatus, state.System.SecurityStatus))
	}
	if kbSys.IsStronghold != state.System.IsStronghold {
		sysChanges = append(sysChanges, fmt.Sprintf("stronghold: %v→%v", kbSys.IsStronghold, state.System.IsStronghold))
	}
	if len(kbSys.Connections) != len(state.System.Connections) {
		sysChanges = append(sysChanges, fmt.Sprintf("connections: %d→%d", len(kbSys.Connections), len(state.System.Connections)))
	}

	if len(sysChanges) > 0 {
		result.SystemChanged = true
		logger.Printf("⚡ System %s changed: %s", systemID, strings.Join(sysChanges, ", "))

		// Snapshot old system data
		oldData, _ := json.Marshal(map[string]any{
			"name":            kbSys.Name,
			"empire":          kbSys.Empire,
			"police_level":    kbSys.PoliceLevel,
			"security_status": kbSys.SecurityStatus,
			"is_stronghold":   kbSys.IsStronghold,
			"connections":     len(kbSys.Connections),
			"position":        map[string]float64{"x": kbSys.Position.X, "y": kbSys.Position.Y},
		})
		if err := kb.RecordChangeSnapshot(ctx, knowledge.ChangeSnapshot{
			EntityType:     "system",
			EntityID:       systemID,
			SystemID:       systemID,
			ChangeSummary:  strings.Join(sysChanges, "; "),
			OldData:        string(oldData),
			DetectedBy:     agentID,
			DetectedAtTick: currentTick,
		}); err != nil {
			logger.Printf("⚠️  Failed to record system change snapshot: %v", err)
		}
	}

	// --- POI-level comparison ---
	dbPOIs, err := kb.GetPOIs(ctx, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get POIs from KB: %w", err)
	}

	// Build lookup maps
	dbPOIMap := make(map[string]knowledge.POI, len(dbPOIs))
	for _, p := range dbPOIs {
		dbPOIMap[p.ID] = p
	}
	serverPOIMap := make(map[string]game.POI, len(state.System.POIs))
	for _, p := range state.System.POIs {
		serverPOIMap[p.ID] = p
	}

	// Check for count difference
	if len(dbPOIs) != len(state.System.POIs) {
		result.POIsChanged = true
		logger.Printf("⚡ POI count changed in %s: %d→%d", systemID, len(dbPOIs), len(state.System.POIs))
	}

	// Detect new and changed POIs
	for _, serverPOI := range state.System.POIs {
		dbPOI, exists := dbPOIMap[serverPOI.ID]
		if !exists {
			result.NewPOIIDs = append(result.NewPOIIDs, serverPOI.ID)
			result.POIsChanged = true
			logger.Printf("⚡ New POI detected: %s (%s) type=%s", serverPOI.Name, serverPOI.ID, serverPOI.Type)
			continue
		}

		// Compare POI fields
		var poiChanges []string
		if dbPOI.Type != serverPOI.Type {
			poiChanges = append(poiChanges, fmt.Sprintf("type: %q→%q", dbPOI.Type, serverPOI.Type))
		}
		if dbPOI.Name != serverPOI.Name {
			poiChanges = append(poiChanges, fmt.Sprintf("name: %q→%q", dbPOI.Name, serverPOI.Name))
		}
		if dbPOI.Position.X != serverPOI.Position.X || dbPOI.Position.Y != serverPOI.Position.Y {
			poiChanges = append(poiChanges, fmt.Sprintf("position: (%.1f,%.1f)→(%.1f,%.1f)",
				dbPOI.Position.X, dbPOI.Position.Y, serverPOI.Position.X, serverPOI.Position.Y))
		}
		if dbPOI.Class != serverPOI.Class {
			poiChanges = append(poiChanges, fmt.Sprintf("class: %q→%q", dbPOI.Class, serverPOI.Class))
		}

		// Compare resource composition (which resource types are present),
		// ignoring quantity changes since those fluctuate naturally.
		if len(dbPOI.Resources) > 0 || len(serverPOI.Resources) > 0 {
			dbResIDs := make(map[string]bool, len(dbPOI.Resources))
			for _, r := range dbPOI.Resources {
				dbResIDs[r.ResourceID] = true
			}
			serverResIDs := make(map[string]bool, len(serverPOI.Resources))
			for _, r := range serverPOI.Resources {
				serverResIDs[r.ResourceID] = true
			}

			var added, removed []string
			for id := range serverResIDs {
				if !dbResIDs[id] {
					added = append(added, id)
				}
			}
			for id := range dbResIDs {
				if !serverResIDs[id] {
					removed = append(removed, id)
				}
			}
			if len(added) > 0 || len(removed) > 0 {
				var parts []string
				if len(added) > 0 {
					parts = append(parts, fmt.Sprintf("+[%s]", strings.Join(added, ",")))
				}
				if len(removed) > 0 {
					parts = append(parts, fmt.Sprintf("-[%s]", strings.Join(removed, ",")))
				}
				poiChanges = append(poiChanges, fmt.Sprintf("resources: %s", strings.Join(parts, " ")))
			}
		}

		if len(poiChanges) > 0 {
			result.ChangedPOIIDs = append(result.ChangedPOIIDs, serverPOI.ID)
			result.POIsChanged = true
			logger.Printf("⚡ POI %s (%s) changed: %s", serverPOI.Name, serverPOI.ID, strings.Join(poiChanges, ", "))

			// Snapshot old POI data
			oldResources := make([]map[string]any, len(dbPOI.Resources))
			for i, r := range dbPOI.Resources {
				oldResources[i] = map[string]any{
					"resource_id": r.ResourceID,
					"richness":    r.Richness,
					"remaining":   r.Remaining,
				}
			}
			oldData, _ := json.Marshal(map[string]any{
				"name":      dbPOI.Name,
				"type":      dbPOI.Type,
				"class":     dbPOI.Class,
				"position":  map[string]float64{"x": dbPOI.Position.X, "y": dbPOI.Position.Y},
				"resources": oldResources,
			})
			if err := kb.RecordChangeSnapshot(ctx, knowledge.ChangeSnapshot{
				EntityType:     "poi",
				EntityID:       serverPOI.ID,
				SystemID:       systemID,
				ChangeSummary:  strings.Join(poiChanges, "; "),
				OldData:        string(oldData),
				DetectedBy:     agentID,
				DetectedAtTick: currentTick,
			}); err != nil {
				logger.Printf("⚠️  Failed to record POI change snapshot: %v", err)
			}
		}
	}

	// Detect removed POIs (in DB but not on server)
	for _, dbPOI := range dbPOIs {
		if _, exists := serverPOIMap[dbPOI.ID]; !exists {
			result.RemovedPOIIDs = append(result.RemovedPOIIDs, dbPOI.ID)
			result.POIsChanged = true
			logger.Printf("⚡ POI removed: %s (%s) type=%s", dbPOI.Name, dbPOI.ID, dbPOI.Type)

			// Snapshot the removed POI
			oldData, _ := json.Marshal(map[string]any{
				"name":     dbPOI.Name,
				"type":     dbPOI.Type,
				"class":    dbPOI.Class,
				"position": map[string]float64{"x": dbPOI.Position.X, "y": dbPOI.Position.Y},
			})
			if err := kb.RecordChangeSnapshot(ctx, knowledge.ChangeSnapshot{
				EntityType:     "poi",
				EntityID:       dbPOI.ID,
				SystemID:       systemID,
				ChangeSummary:  "removed from system",
				OldData:        string(oldData),
				DetectedBy:     agentID,
				DetectedAtTick: currentTick,
			}); err != nil {
				logger.Printf("⚠️  Failed to record removed POI snapshot: %v", err)
			}
		}
	}

	// --- Base-level comparison (for station POIs that have bases) ---
	for _, serverPOI := range state.System.POIs {
		if serverPOI.BaseID == "" {
			continue
		}
		dbBase, err := kb.GetBase(ctx, serverPOI.BaseID)
		if err != nil || dbBase == nil {
			continue // base not in DB yet, will be collected during POI exploration
		}

		var baseChanges []string
		if dbBase.Name != serverPOI.BaseName && serverPOI.BaseName != "" {
			baseChanges = append(baseChanges, fmt.Sprintf("name: %q→%q", dbBase.Name, serverPOI.BaseName))
		}

		if len(baseChanges) > 0 {
			logger.Printf("⚡ Base %s changed: %s", serverPOI.BaseID, strings.Join(baseChanges, ", "))

			oldData, _ := json.Marshal(map[string]any{
				"name":          dbBase.Name,
				"empire":        dbBase.Empire,
				"defense_level": dbBase.DefenseLevel,
				"has_drones":    dbBase.HasDrones,
				"public_access": dbBase.PublicAccess,
			})
			if err := kb.RecordChangeSnapshot(ctx, knowledge.ChangeSnapshot{
				EntityType:     "base",
				EntityID:       serverPOI.BaseID,
				SystemID:       systemID,
				ChangeSummary:  strings.Join(baseChanges, "; "),
				OldData:        string(oldData),
				DetectedBy:     agentID,
				DetectedAtTick: currentTick,
			}); err != nil {
				logger.Printf("⚠️  Failed to record base change snapshot: %v", err)
			}
		}
	}

	return result, nil
}

// ============================================================================
// NAVIGATION (Direct jumping, no jump gates)
// ============================================================================

func findRouteToSystem(client game.GameClient, ctx context.Context, targetSystem string) error {
	// Use FindRoute API to get the path
	_, err := client.FindRoute(ctx, targetSystem)
	return err
}

func jumpToSystem(client game.GameClient, ctx context.Context, targetSystem string) error {
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

	// Undock if docked (can't jump while docked)
	if state.Doc {
		logger.Printf("📤 Undocking before jump...")
		if err := client.Undock(ctx); err != nil && err.Error() != "Already undocked (success)" {
			return fmt.Errorf("failed to undock: %w", err)
		}
		time.Sleep(game.SleepUndock)
	}

	// Attempt jump with retry for action_pending errors
	for attempt := range 3 {
		if attempt > 0 {
			logger.Printf("⏳ Waiting for pending action to complete...")
			time.Sleep(game.SleepTick)
		}

		logger.Printf("🌟 Jumping to %s...", targetSystem)
		_, err := client.Jump(ctx, targetSystem)
		if err == nil {
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

func navigateToHomeBase(client game.GameClient, ctx context.Context, homeSystem string) error {
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

func exploreAllPOIs(client game.GameClient, ctx context.Context, logger *log.Logger, expState *ExplorationState, kb knowledge.Base, forceFullScan bool) error {
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

		// Skip POIs with fresh data unless a full scan was triggered by change detection
		if !forceFullScan && !*forceRescan {
			if lastTick, ok := knownPOIs[poi.ID]; ok && lastTick > 0 {
				threshold := game.POIFreshnessThreshold(poi.Type)
				age := currentTick - lastTick
				if age >= 0 && age < threshold {
					logger.Printf("⊙ Skipping POI %s (%s) - data still fresh (age: %d ticks, threshold: %d)",
						poi.Name, poi.Type, age, threshold)
					expState.VisitedPOIs[poi.ID] = true
					continue
				}
				logger.Printf("🔄 POI %s (%s) data stale (age: %d ticks, threshold: %d) - rescanning",
					poi.Name, poi.Type, age, threshold)
			} else if lastTick, ok := knownPOIs[poi.ID]; ok && lastTick <= 0 {
				logger.Printf("🆕 POI %s (%s) has last_updated_tick=%d (unset/unviewed) - scanning",
					poi.Name, poi.Type, lastTick)
			}
		} else if forceFullScan {
			logger.Printf("🔄 Force-scanning POI %s (%s) due to detected system changes", poi.Name, poi.Type)
		}

		logger.Printf("📍 Visiting POI: %s (%s) - Type: %s", poi.Name, poi.ID, poi.Type)

		// Travel to POI if not already there
		if state.CurrentPOI != poi.ID {
			if _, err := client.Travel(ctx, poi.ID); err != nil {
				logger.Printf("Failed to travel to POI %s: %v", poi.ID, err)
				continue
			}
			logger.Printf("→ Arrived at %s", poi.Name)
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
			Class:           poi.Class,
			Description:     poi.Description,
			Position:        poi.Position,
			Services:        []string{},
			Resources:       poi.Resources,
			LastUpdatedTick: state.GetTick(),
		}
		if err := kb.RememberPOI(ctx, kbPOI); err != nil {
			logger.Printf("Failed to save POI to knowledge base: %v", err)
		} else {
			logger.Printf("💾 Saved POI: %s (tick: %d)", poi.Name, kbPOI.LastUpdatedTick)
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

func repairShip(client game.GameClient, ctx context.Context, logger *log.Logger, expState *ExplorationState) error {
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
		if _, err := client.Travel(ctx, stationPOI.ID); err != nil {
			return fmt.Errorf("failed to travel to station: %w", err)
		}
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

func explorationPhase(client game.GameClient, logger *log.Logger, ctx context.Context, kb knowledge.Base, agentID string) error {
	state := client.GetState()

	logger.Printf("🔍 Initial state check: System.ID=%q, CurrentSystem=%q, Player.CurrentSystem=%q",
		state.System.ID, state.CurrentSystem, state.Player.CurrentSystem)

	// Ensure we have system data before starting exploration
	if state.System.ID == "" || state.CurrentSystem == "" {
		logger.Printf("⚠️  No system data available after login, fetching status...")
		if err := client.GetStatus(ctx); err != nil {
			return fmt.Errorf("failed to get initial status: %w", err)
		}
		time.Sleep(game.SleepQuick)
		state = client.GetState()

		// If still no system data, agent is likely mid-jump or mid-travel
		if state.System.ID == "" || state.CurrentSystem == "" || state.Traveling {
			logger.Printf("⏳ Agent appears to be in transit, waiting for arrival...")
			if err := waitForTransitComplete(client, ctx, logger); err != nil {
				return fmt.Errorf("transit wait failed: %w", err)
			}
			state = client.GetState()
		}

		if state.System.ID == "" {
			return fmt.Errorf("cannot start exploration: no system data available (System.ID=%q, CurrentSystem=%q)",
				state.System.ID, state.CurrentSystem)
		}

		logger.Printf("✓ System data loaded: %s (%s)", state.System.Name, state.System.ID)
	}

	// Undock to start exploration if docked
	state = client.GetState()
	if state.Doc {
		logger.Printf("📤 Undocking to start exploration...")
		if err := client.Undock(ctx); err != nil {
			logger.Printf("Failed to undock: %v", err)
		} else {
			logger.Printf("✅ Undocked successfully")
			time.Sleep(12 * time.Second)
		}
	}

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

		// If we lost system state (e.g., during reconnect), fetch it
		if currentSystem == "" || state.System.ID == "" {
			logger.Printf("⚠️  Lost system state, fetching status...")
			if err := client.GetStatus(ctx); err != nil {
				logger.Printf("Failed to get status: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}
			time.Sleep(1 * time.Second)
			state = client.GetState()
			currentSystem = state.CurrentSystem

			if currentSystem == "" {
				logger.Printf("⏳ Still in transit, waiting...")
				time.Sleep(5 * time.Second)
				continue
			}
			logger.Printf("✓ System state recovered: %s", state.System.Name)
		}

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

			// Always fetch system data from the server so we can compare
			if err := collectSystemData(client, ctx, logger, kb, expState.AgentID); err != nil {
				logger.Printf("Failed to collect system data: %v", err)
			}

			// Refresh state after collection
			state = client.GetState()

			// Detect changes between server data and what's in the DB
			forceFullScan := *forceRescan
			changes, err := detectAndRecordChanges(ctx, logger, kb, state, expState.AgentID)
			if err != nil {
				logger.Printf("⚠️  Change detection failed: %v", err)
				forceFullScan = true // scan on error to be safe
			} else if changes.SystemChanged || changes.POIsChanged {
				totalChanges := len(changes.ChangedPOIIDs) + len(changes.NewPOIIDs) + len(changes.RemovedPOIIDs)
				logger.Printf("⚡ Changes detected in %s: system=%v, poi_changes=%d (new=%d, modified=%d, removed=%d)",
					currentSystem, changes.SystemChanged, totalChanges,
					len(changes.NewPOIIDs), len(changes.ChangedPOIIDs), len(changes.RemovedPOIIDs))
				forceFullScan = true
			} else {
				logger.Printf("✓ No changes detected in %s — data matches DB", currentSystem)
			}

			// Explore POIs: full scan if changes detected, otherwise per-POI freshness
			if forceFullScan {
				logger.Printf("🔍 Full POI scan triggered by detected changes...")
			} else {
				logger.Printf("🔍 Beginning POI exploration (per-POI freshness)...")
			}
			if err := exploreAllPOIs(client, ctx, logger, expState, kb, forceFullScan); err != nil {
				logger.Printf("POI exploration failed: %v", err)
			}
		}

		// CRITICAL: Refresh state after collecting data to get current system's connections
		state = client.GetState()
		currentSystem = state.CurrentSystem

		// If neither System.ID nor System.Name matches CurrentSystem, data may be stale.
		// CurrentSystem can be either the ID (e.g., "tau_ceti") from parsePlayerData
		// or the display name (e.g., "Tau Ceti") from mergeSystemDataLocked.
		systemMatchesCurrent := strings.EqualFold(state.System.ID, currentSystem) ||
			strings.EqualFold(state.System.Name, currentSystem)
		if state.System.ID == "" || !systemMatchesCurrent {
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
				// Stack empty — try to find an unvisited system in the KB
				nextTarget := findUnvisitedSystemInKB(expState, state.System.Connections, state.System.Position)
				if nextTarget != "" {
					logger.Printf("🧭 All neighbors visited, navigating to %s to find unvisited systems", nextTarget)
					navLogger := log.New(os.Stdout, "[NAV] ", log.LstdFlags)
					if err := game.NavigateToSystem(client, ctx, nextTarget, navLogger); err != nil {
						logger.Printf("Navigation error to %s: %v", nextTarget, err)
						time.Sleep(game.SleepTick)
					}
					time.Sleep(game.SleepShort)
					continue
				}

				// Truly no unvisited systems remain
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
				time.Sleep(game.SleepMedium)
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

	// Safety check: if neither System.ID nor System.Name matches CurrentSystem, data may be stale.
	// CurrentSystem can be either the ID (e.g., "tau_ceti") from parsePlayerData
	// or the display name (e.g., "Tau Ceti") from mergeSystemDataLocked.
	if state.System.ID == "" {
		logger.Printf("⚠️  System.ID is empty, CurrentSystem=%s - system data not yet loaded", state.CurrentSystem)
		logger.Printf("     Returning empty unvisited list to force data refresh")
		return []string{}
	}
	systemMatchesCurrent := strings.EqualFold(state.System.ID, state.CurrentSystem) ||
		strings.EqualFold(state.System.Name, state.CurrentSystem)
	if !systemMatchesCurrent {
		logger.Printf("⚠️  STALE DATA: System.ID=%s, System.Name=%s doesn't match CurrentSystem=%s", state.System.ID, state.System.Name, state.CurrentSystem)
		logger.Printf("     This indicates system data wasn't refreshed after a jump!")
		logger.Printf("     Returning empty unvisited list to force data refresh")
		return []string{}
	}

	logger.Printf("Current system: %s, Connections: %v", state.System.ID, state.System.Connections)
	logger.Printf("Visited systems: %d", len(expState.VisitedSystems))

	// Query KB visit status only for connected systems (not all 500+)
	systemTickMap := make(map[string]int64)
	if expState.kb != nil {
		for _, conn := range state.System.Connections {
			sys, err := expState.kb.GetSystem(context.Background(), conn.SystemID)
			if err != nil || sys == nil {
				continue
			}
			systemTickMap[sys.ID] = sys.LastUpdatedTick
			logger.Printf("   KB neighbor: %s (tick: %d)", sys.ID, sys.LastUpdatedTick)
			// Restore VisitedSystems from KB for persistence across restarts
			if sys.LastUpdatedTick > 0 && !expState.VisitedSystems[sys.ID] {
				expState.VisitedSystems[sys.ID] = true
				logger.Printf("   ✓ Restored %s as visited from KB", sys.ID)
			}
		}
	}

	logger.Printf("After KB restore, Visited systems: %d", len(expState.VisitedSystems))

	// Collect unvisited neighbors with their LastUpdatedTick from KB
	type neighborInfo struct {
		SystemID         string
		LastUpdatedTick  int64
	}

	var neighbors []neighborInfo
	for _, conn := range state.System.Connections {
		// Skip if already visited in current session OR in KB
		if expState.VisitedSystems[conn.SystemID] {
			logger.Printf("   ⊙ Skipping %s (already in VisitedSystems)", conn.SystemID)
			continue
		}
		// Also skip if it exists in KB with a LastUpdatedTick
		if tick, ok := systemTickMap[conn.SystemID]; ok {
			if tick > 0 {
				// System exists in KB and has been visited - mark as visited
				expState.VisitedSystems[conn.SystemID] = true
				logger.Printf("   ⊙ Skipping %s (in KB with tick %d)", conn.SystemID, tick)
				continue
			}
			logger.Printf("   ? %s is in KB but tick=0, treating as unvisited", conn.SystemID)
		}
		// Truly unvisited system
		neighbors = append(neighbors, neighborInfo{
			SystemID:        conn.SystemID,
			LastUpdatedTick: systemTickMap[conn.SystemID], // Will be 0 if not in KB
		})
	}

	// If we have unvisited neighbors, sort them by LastUpdatedTick
	if len(neighbors) > 0 {
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

// findUnvisitedSystemInKB scans the full KB for the closest unvisited system
// (LastUpdatedTick == 0 and not in VisitedSystems), sorted by Euclidean
// distance from the current position.
// Returns the system ID or "" if all known systems have been visited.
func findUnvisitedSystemInKB(expState *ExplorationState, currentConnections []game.ConnectionInfo, currentPos game.Position) string {
	if expState.kb == nil {
		return ""
	}
	logger := log.New(os.Stdout, "[DFS] ", log.LstdFlags)

	allSystems, err := expState.kb.GetSystems(context.Background())
	if err != nil {
		logger.Printf("Failed to get systems: %v", err)
		return ""
	}

	// Best case: an unvisited system is directly connected to us
	for _, conn := range currentConnections {
		if !expState.VisitedSystems[conn.SystemID] {
			// Verify it's actually unvisited in KB too
			sys, err := expState.kb.GetSystem(context.Background(), conn.SystemID)
			if err != nil || sys == nil || sys.LastUpdatedTick == 0 {
				logger.Printf("Direct: unvisited %s is a neighbor", conn.SystemID)
				return conn.SystemID
			}
		}
	}

	// Collect unvisited systems with their distance from current position
	type candidate struct {
		id   string
		dist float64
	}
	var candidates []candidate
	for _, sys := range allSystems {
		if sys.LastUpdatedTick > 0 || expState.VisitedSystems[sys.ID] {
			continue
		}
		dx := sys.Position.X - currentPos.X
		dy := sys.Position.Y - currentPos.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		candidates = append(candidates, candidate{id: sys.ID, dist: dist})
	}

	if len(candidates) == 0 {
		logger.Printf("All %d known systems have been visited", len(allSystems))
		return ""
	}

	// Sort by distance (closest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	logger.Printf("Found %d unvisited systems, closest: %s (%.0f units away)",
		len(candidates), candidates[0].id, candidates[0].dist)

	return candidates[0].id
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: auto-explorer [flags] <explorer-number>")
		fmt.Println("Example: auto-explorer 1")
		fmt.Println("  auto-explorer -transport=mcp 1")
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

	// Initialize game client based on transport selection
	var client game.GameClient
	var creds *game.Credentials

	switch *transport {
	case "mcp":
		logger.Printf("Using MCP transport")
		client, creds, err = game.InitializeMCPAgent(explorer, logger, ctx, *debug, false)
		if err != nil {
			log.Fatalf("Failed to initialize MCP agent: %v", err)
		}
	case "ws":
		logger.Printf("Using WebSocket transport")
		var wsClient *game.Client
		wsClient, creds, err = game.InitializeAgent(explorer, logger, ctx, *debug)
		if err != nil {
			log.Fatalf("Failed to initialize agent: %v", err)
		}
		// Wire XP observation tracking into the client
		knowledge.NewXPTracker(wsClient, kb, explorer, logger)
		// Persist every encountered player to the shared KB. See spec
		// docs/superpowers/specs/2026-05-17-player-sightings-design.md.
		agent.WirePlayerObserver(wsClient, kb, nil)
		client = wsClient
	default:
		log.Fatalf("Unknown transport: %s (must be: ws, mcp)", *transport)
	}

	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	logger.Printf("🔭 Starting autonomous explorer bot...")
	logger.Printf("Explorer: %s | Empire: %s", creds.Username, creds.Empire)

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

		defer func() {
			if regClient != nil {
				if err := regClient.Deregister(); err != nil {
					logger.Printf("Warning: Failed to deregister: %v", err)
				}
			}
		}()
	}

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

	time.Sleep(1 * time.Second)

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
