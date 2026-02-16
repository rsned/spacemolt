package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
)

// Phase 1 constants - Mining & upgrades
const (
	TIER1_THRESHOLD      = 300.0  // Mining laser (faster mining!)
	TIER2_THRESHOLD      = 800.0  // Better mining laser or cargo
	TIER2_SHIP_THRESHOLD = 2000.0 // Upgrade to mining_enhanced ship + 3 mining lasers
	SCANNER_THRESHOLD    = 600.0  // Scanner for exploration
	RESERVE_CREDITS      = 50.0   // Never spend below this
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
	// This prevents the "cannot jump, system is not connected" problem where we think
	// we're in one system but are actually in another after reconnection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.logger.Printf("🔍 Refreshing system data after connection to verify location...")
	if err := h.client.GetSystem(ctx); err != nil {
		h.logger.Printf("⚠️  Failed to refresh system data after connection: %v", err)
		// Continue anyway - state might still be useful
	} else {
		// Wait for state update
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

func sanitizeFilename(name string) string {
	// Replace spaces and special chars with underscores
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	// Remove other special characters
	result := ""
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			result += string(c)
		}
	}
	return result
}

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

	// Count scanners
	scannerCount := 0
	for _, module := range state.Ship.Modules {
		if strings.HasPrefix(module, "scanner") {
			scannerCount++
		}
	}
	if scannerCount > 0 {
		notes = append(notes, fmt.Sprintf("Scanners: %d", scannerCount))
	}

	currentGoal := "Autonomous galaxy exploration - mapping systems and discovering POIs"
	if expState != nil {
		if state.Doc {
			currentGoal = "Docked at station - refueling and collecting market data"
		} else if state.Traveling {
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
		// Log error but don't fail - captain's log is not critical
		_ = err
	}
}

func hasMiningLaser(state *game.State) bool {
	for _, module := range state.Ship.Modules {
		if strings.HasPrefix(module, "mining_laser_") {
			return true
		}
	}
	return false
}

func hasScanner(state *game.State) bool {
	for _, module := range state.Ship.Modules {
		if strings.HasPrefix(module, "scanner") {
			return true
		}
	}
	return false
}

func hasWeapons(state *game.State) bool {
	for _, module := range state.Ship.Modules {
		// Check for common weapon module prefixes
		if strings.Contains(module, "laser") && !strings.Contains(module, "mining") {
			return true
		}
		if strings.Contains(module, "cannon") || strings.Contains(module, "turret") ||
			strings.Contains(module, "missile") || strings.Contains(module, "torpedo") {
			return true
		}
	}
	return false
}

func getHullPercentage(state *game.State) float64 {
	if state.Ship.MaxHull == 0 {
		return 100.0
	}
	return (state.Ship.Hull / state.Ship.MaxHull) * 100
}

func getShieldPercentage(state *game.State) float64 {
	if state.Ship.MaxShield == 0 {
		return 100.0
	}
	return (state.Ship.Shield / state.Ship.MaxShield) * 100
}

func needsRefuel(state *game.State) bool {
	return state.Fuel < (state.MaxFuel * 0.3)
}

// checkForHiddenFindings scans POIs for hidden findings and prints details
func checkForHiddenFindings(logger *log.Logger, state *game.State) {
	hiddenCount := 0
	for _, poi := range state.System.POIs {
		// Check if this POI is hidden (newly discovered by survey)
		// Hidden POIs have a Hidden field or were just revealed
		if poi.Type == "deep_core_deposit" {
			hiddenCount++
			logger.Printf("🔍 ═══════════════════════════════════════════════")
			logger.Printf("🔍 HIDDEN FINDING DISCOVERED!")
			logger.Printf("🔍 ═══════════════════════════════════════════════")
			logger.Printf("🔍 Type: %s", poi.Type)
			logger.Printf("🔍 Name: %s", poi.Name)
			logger.Printf("🔍 ID: %s", poi.ID)
			logger.Printf("🔍 System: %s (%s)", state.System.Name, state.System.ID)
			logger.Printf("🔍 Description: %s", poi.Description)
			if len(poi.Resources) > 0 {
				logger.Printf("🔍 Resources: %v", poi.Resources)
			}
			logger.Printf("🔍 Position: X=%.2f, Y=%.2f", poi.Position.X, poi.Position.Y)
			logger.Printf("🔍 ═══════════════════════════════════════════════")

			// Save hidden finding to a special file
			saveSurveyFinding(logger, state, poi)
		}
	}

	if hiddenCount > 0 {
		logger.Printf("✨ Found %d hidden POI(s) in this survey!", hiddenCount)
	}
}

// saveSurveyFinding saves a hidden POI finding to a special file for later reference
func saveSurveyFinding(logger *log.Logger, state *game.State, poi game.POI) {
	timestamp := time.Now().Format(game.TimestampPrecise)

	findingData := map[string]any{
		"timestamp":    time.Now().Format(game.TimestampISO8601),
		"game_tick":    state.CurrentTick,
		"system_id":    state.System.ID,
		"system_name":  state.System.Name,
		"finding_type": "deep_core_deposit",
		"poi": map[string]any{
			"id":          poi.ID,
			"name":        poi.Name,
			"type":        poi.Type,
			"description": poi.Description,
			"position":    poi.Position,
			"resources":   poi.Resources,
			"base_id":     poi.BaseID,
		},
		"discovered_by": state.Username,
	}

	// Save to file using library helper
	filename := game.GenerateDataFilename(state.System.Name, timestamp, "survey_finding")
	dir := filepath.Join("data", "server", "survey_findings")

	if err := game.SaveStructToFile(findingData, dir, filename); err != nil {
		logger.Printf("⚠️  Failed to write survey finding: %v", err)
		return
	}

	logger.Printf("💾 Saved survey finding to: %s", filepath.Join(dir, filename))
}

// ============================================================================
// PHASE 1: MINING & UPGRADES
// ============================================================================

func attemptExplorerUpgrades(client *game.Client, logger *log.Logger, ctx context.Context) bool {
	state := client.GetState()
	credits := state.Credits
	availableCredits := credits - RESERVE_CREDITS

	if availableCredits < 100 {
		return false // Not enough to buy anything
	}

	// Ensure cargo space is available for purchases
	cargoUsed := state.Ship.CargoUsed
	cargoCapacity := state.Ship.CargoCapacity
	if cargoUsed >= cargoCapacity*0.5 {
		logger.Printf("⚠️  Cargo too full (%.1f/%.1f) - skipping upgrades until cargo is sold", cargoUsed, cargoCapacity)
		return false
	}

	logger.Printf("💰 Checking for explorer upgrades... (%.2f credits available, %.1f/%.1f cargo space)", availableCredits, cargoUsed, cargoCapacity)

	// Get market listings
	if err := client.GetListings(ctx); err != nil {
		logger.Printf("Could not get listings: %v", err)
		return false
	}

	time.Sleep(2 * time.Second)
	listings := client.GetMarketListings()

	if len(listings) == 0 {
		logger.Printf("No market listings available")
		return false
	}

	var purchased bool

	// Priority 1: Ship upgrade to mining_enhanced + 3 mining lasers (2000+ credits) - MAJOR UPGRADE!
	if availableCredits >= TIER2_SHIP_THRESHOLD && !purchased {
		// Check if we're still on the starter ship
		if state.Ship.ClassID == "starter_mining" {
			logger.Printf("🚀 SHIP UPGRADE TIME! You have %.2f credits - upgrading to Drillship!", availableCredits)

			// CRITICAL: Sell all cargo first (it will be lost when switching ships!)
			if len(state.Ship.Cargo) > 0 {
				logger.Printf("📦 Selling all cargo before ship upgrade...")
				if err := client.SellAll(ctx); err != nil {
					logger.Printf("Failed to sell cargo: %v", err)
				} else {
					logger.Printf("✅ Cargo sold!")
					time.Sleep(3 * time.Second)
				}
			}

			// CRITICAL: Uninstall all utility slot modules (mining lasers) first!
			logger.Printf("🔧 Uninstalling mining lasers before ship upgrade...")
			for _, moduleID := range state.Ship.Modules {
				uninstallMsg := protocol.Message{
					Type: "uninstall_mod",
					Payload: map[string]any{
						"module_id": moduleID,
					},
				}
				if err := client.Send(ctx, uninstallMsg); err != nil {
					logger.Printf("Failed to uninstall module %s: %v", moduleID, err)
				} else {
					logger.Printf("✅ Uninstalled module: %s", moduleID)
				}
				time.Sleep(2 * time.Second)
			}

			logger.Printf("🚀 Purchasing mining_enhanced ship (Drillship)...")

			// Buy the mining_enhanced ship using direct protocol message
			buyShipMsg := protocol.Message{
				Type: "buy_ship",
				Payload: map[string]any{
					"ship_class": "mining_enhanced",
				},
			}
			if err := client.Send(ctx, buyShipMsg); err != nil {
				logger.Printf("Failed to buy ship: %v", err)
			} else {
				logger.Printf("✅ SHIP UPGRADED TO DRILLSHIP!")
				logger.Printf("✅ New capacity: 100 cargo, 3 utility slots for mining lasers!")
				purchased = true
				time.Sleep(3 * time.Second)

				// Refresh state after ship purchase
				state = client.GetState()

				// Now buy 3 mining lasers to fill all utility slots
				logger.Printf("⛏️  Now purchasing 3 mining lasers...")
				if err := client.Buy(ctx, "mining_laser_1", 3); err != nil {
					logger.Printf("Failed to buy mining lasers: %v", err)
				} else {
					logger.Printf("✅ Purchased 3 mining lasers!")
					time.Sleep(2 * time.Second)

					// Install each mining laser
					for i := 1; i <= 3; i++ {
						if err := client.Install(ctx, "mining_laser_1"); err != nil {
							logger.Printf("Failed to install mining laser #%d: %v", i, err)
						} else {
							logger.Printf("✅ Mining laser #%d installed!", i)
						}
						time.Sleep(2 * time.Second)
					}

					logger.Printf("✅✅✅ TRIPLE MINING LASER SETUP COMPLETE! Mining power TRIPLED!")
				}
			}
		}
	}

	// Priority 2: Scanner (essential for exploration)
	if availableCredits >= SCANNER_THRESHOLD && !hasScanner(state) && !purchased {
		for _, listing := range listings {
			if strings.HasPrefix(listing.ItemID, "scanner") {
				if listing.PricePerUnit <= availableCredits {
					logger.Printf("📡 Buying scanner: %s for %.2f credits", listing.ItemID, listing.PricePerUnit)
					if err := client.Buy(ctx, listing.ItemID, 1); err != nil {
						logger.Printf("Failed to buy scanner: %v", err)
					} else {
						logger.Printf("✅ Purchased scanner! Installing...")
						purchased = true
						time.Sleep(2 * time.Second)
						if err := client.Install(ctx, listing.ItemID); err != nil {
							logger.Printf("Failed to install scanner: %v", err)
						} else {
							logger.Printf("✅ SCANNER INSTALLED!")
						}
						time.Sleep(2 * time.Second)
					}
				}
			}
		}
	}

	// Priority 3: Mining Laser (faster mining) - install up to ship capacity
	if availableCredits >= TIER1_THRESHOLD && !purchased {
		// Count how many mining lasers we have
		miningLasersInstalled := 0
		miningLasersInCargo := 0
		for _, module := range state.Ship.Modules {
			if strings.HasPrefix(module, "mining_laser") {
				miningLasersInstalled++
			}
		}
		for _, item := range state.Ship.Cargo {
			if strings.HasPrefix(item.ItemID, "mining_laser") && item.Quantity > 0 {
				miningLasersInCargo++
			}
		}
		totalMiningLasers := miningLasersInstalled + miningLasersInCargo

		// Determine max lasers based on ship class
		maxLasers := 2 // starter_mining has 2 utility slots
		switch state.Ship.ClassID {
		case "mining_enhanced":
			maxLasers = 3 // Drillship has 3 utility slots
		case "mining_barge":
			maxLasers = 4 // Excavator has 4 utility slots
		}

		logger.Printf("⛏️  Mining Laser Status: %d installed, %d in cargo (goal: %d installed)",
			miningLasersInstalled, miningLasersInCargo, maxLasers)

		// Only buy more if we have less than max total
		if totalMiningLasers < maxLasers {
			for _, listing := range listings {
				if (listing.ItemID == "mining_laser_1" || listing.ItemID == "mining_laser_2" ||
					listing.ItemID == "mining_laser_3" || listing.ItemID == "advanced_mining_laser") &&
					listing.PricePerUnit <= availableCredits && listing.PricePerUnit <= 1000 {

					// Calculate how many we need to buy (up to max total)
					needed := maxLasers - totalMiningLasers
					if needed > 0 {
						logger.Printf("⛏️  Buying %d x %s for %.2f credits each", needed, listing.ItemID, listing.PricePerUnit)
						if err := client.Buy(ctx, listing.ItemID, float64(needed)); err != nil {
							logger.Printf("Failed to buy mining laser: %v", err)
						} else {
							logger.Printf("✅ Purchased %d mining laser(s)! Installing...", needed)
							purchased = true
							time.Sleep(2 * time.Second)

							// Install each mining laser from cargo
							installed := 0
							for i := 0; i < needed; i++ {
								if err := client.Install(ctx, listing.ItemID); err != nil {
									logger.Printf("Failed to install mining laser #%d: %v", i+1, err)
								} else {
									installed++
									logger.Printf("✅ Mining laser #%d installed!", installed)
								}
								time.Sleep(2 * time.Second)
							}

							if installed > 0 {
								logger.Printf("✅ %d MINING LASER(S) INSTALLED! Mining speed increased!", installed)
							}
							break
						}
					}
				}
			}
		}
	}

	return purchased
}

func miningPhase(client *game.Client, logger *log.Logger, ctx context.Context) error {
	logger.Printf("Starting mining phase to earn credits for exploration upgrades...")
	logger.Printf("Target: %.0f credits for Drillship (mining_enhanced) + 3 mining lasers + scanner", TIER2_SHIP_THRESHOLD)

	startingCredits := client.GetState().Credits

	// Helper function to count mining lasers
	countMiningLasers := func(state *game.State) int {
		count := 0
		for _, module := range state.Ship.Modules {
			if strings.HasPrefix(module, "mining_laser") {
				count++
			}
		}
		return count
	}

	// Configure the shared mining loop for Phase 1
	config := &game.MiningLoopConfig{
		Tier1Threshold:     TIER1_THRESHOLD,
		ReserveCredits:     RESERVE_CREDITS,
		MaxMiningAttempts:  15, // Explorer uses fixed limit instead of calculated
		CargoFullThreshold: 0.9, // Explorer is less aggressive (90% vs 97%)
		UseBulkSell:        true, // Use bulk sell for better performance

		// Stop when Phase 1 goals are achieved
		StopCondition: func(state *game.State) bool {
			hasDrillship := state.Ship.ClassID == "mining_enhanced" || state.Ship.ClassID == "mining_barge"
			hasTripleLasers := countMiningLasers(state) >= 3
			scannerPresent := hasScanner(state)

			// Phase 1 complete if we have Drillship with 3 lasers, OR we have scanner + mining laser
			return (hasDrillship && hasTripleLasers) || (scannerPresent && hasMiningLaser(state))
		},

		OnUpgradeCheck: func() bool {
			attemptExplorerUpgrades(client, logger, ctx)
			return false
		},
	}

	// Run the shared mining loop
	result, err := game.MiningLoop(client, logger, ctx, config)
	if err != nil {
		return err
	}

	// Check if we completed Phase 1
	if result.StoppedReason == "stop_condition" {
		state := client.GetState()
		miningLaserCount := countMiningLasers(state)
		scannerPresent := hasScanner(state)

		logger.Printf("✅ Phase 1 COMPLETE! Exploration equipment ready!")
		logger.Printf("Ship: %s | Mining Lasers: %d | Scanner: %v", state.Ship.Name, miningLaserCount, scannerPresent)
		logger.Printf("Credits: %.2f (started with %.2f, earned %.2f over %d runs)",
			state.Credits, startingCredits, result.TotalCreditsEarned, result.RunsCompleted)
		return nil
	}

	// Mining stopped for another reason
	logger.Printf("Mining phase stopped: %s", result.StoppedReason)
	return nil
}

// ============================================================================
// PHASE 2: GALAXY EXPLORATION
// ============================================================================

func collectSystemData(client *game.Client, ctx context.Context, logger *log.Logger, kb knowledge.Base, agentID string) error {
	// Request system data
	if err := client.GetSystem(ctx); err != nil {
		return fmt.Errorf("failed to get system: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get current state
	state := client.GetState()

	// Check if we already have this system in the knowledge base
	existingSystem, err := kb.GetSystem(ctx, state.System.ID)
	if err != nil {
		logger.Printf("⚠️  Error checking knowledge base for system: %v", err)
	}

	// Convert game state to knowledge.System
	kbSystem := convertGameSystemToKnowledge(state.System, agentID, state.GetTick())

	// Compare with existing data (if any)
	if existingSystem != nil {
		diff := compareSystems(existingSystem, &kbSystem)
		if len(diff) == 0 {
			logger.Printf("✓ System %s (%s) unchanged in knowledge base", state.System.Name, state.System.ID)
			// Still save to file for compatibility with existing workflows
			return saveSystemToFile(client, logger, state)
		}
		logger.Printf("🆕 System changes detected: %s", strings.Join(diff, ", "))
	}

	// Remember the system in knowledge base
	if err := kb.RememberSystem(ctx, kbSystem); err != nil {
		logger.Printf("⚠️  Failed to save system to knowledge base: %v", err)
	} else {
		logger.Printf("💾 Saved system to knowledge base: %s", state.System.Name)
	}

	// Perform system survey to scan for hidden POIs (requires survey scanner module)
	logger.Printf("🔭 Surveying system for hidden POIs...")
	if err := client.SurveySystem(ctx); err != nil {
		logger.Printf("⚠️  Survey failed (may not have survey scanner): %v", err)
	} else {
		logger.Printf("✓ Survey complete")
		time.Sleep(3 * time.Second)

		// Check for and report any hidden findings
		state = client.GetState()
		checkForHiddenFindings(logger, state)
	}

	// Save to file for compatibility with existing workflows
	return saveSystemToFile(client, logger, state)
}

// saveSystemToFile saves system data to file (legacy compatibility)
func saveSystemToFile(client *game.Client, logger *log.Logger, state *game.State) error {
	ctx := context.Background()

	// Scan the system
	if err := client.Scan(ctx); err != nil {
		logger.Printf("Scan failed (may not have scanner): %v", err)
	}
	time.Sleep(3 * time.Second)

	// Try to get raw JSON from server response
	rawJSON := client.GetRawJSON("system")
	var data []byte
	var err error

	if rawJSON != nil {
		// Use raw JSON from server - add metadata wrapper
		wrapper := map[string]any{
			"captured_at": time.Now().Format(time.RFC3339),
			"game_tick":   state.CurrentTick,
		}

		// Unmarshal server response
		var serverData map[string]any
		if err := json.Unmarshal(rawJSON, &serverData); err == nil {
			for k, v := range serverData {
				wrapper[k] = v
			}
		}

		data, err = json.MarshalIndent(wrapper, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal system data: %w", err)
		}
	} else {
		// Fall back to state-based approach if raw JSON not available
		logger.Printf("⚠️  No raw system JSON available, using state data")
		systemData := map[string]any{
			"pois": state.System.POIs,
			"system": map[string]any{
				"id":           state.System.ID,
				"name":         state.System.Name,
				"description":  state.System.Description,
				"empire":       state.System.Empire,
				"police_level": state.System.PoliceLevel,
				"connections":  state.System.Connections,
				"pois": func() []string {
					poiIDs := make([]string, len(state.System.POIs))
					for i, poi := range state.System.POIs {
						poiIDs[i] = poi.ID
					}
					return poiIDs
				}(),
				"position": state.System.Position,
			},
		}

		data, err = json.MarshalIndent(systemData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal system data: %w", err)
		}
	}

	// Save to file
	timestamp := time.Now().Format("20060102") // YYYYMMDD
	filename := fmt.Sprintf("%s.%s.json",
		sanitizeFilename(state.System.Name),
		timestamp)
	filePath := filepath.Join("data", "server", "systems", filename)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write system data: %w", err)
	}

	logger.Printf("💾 Saved system data to file: %s", filePath)
	return nil
}

// convertGameSystemToKnowledge converts a game system to knowledge.System
func convertGameSystemToKnowledge(sys game.SystemData, discoveredBy string, tick int64) knowledge.System {
	kbSystem := knowledge.System{
		ID:              sys.ID,
		Name:            sys.Name,
		PoliceLevel:     sys.PoliceLevel,
		Faction:         sys.Empire,
		Connections:     sys.Connections,
		DiscoveredBy:    discoveredBy,
		LastUpdatedTick: tick,
		Position: game.Position{
			X: sys.Position.X,
			Y: sys.Position.Y,
			Z: 0, // Game Position only has X, Y - set Z to 0
		},
	}

	return kbSystem
}

// compareSystems compares two systems and returns a list of differences (excluding resource counts)
func compareSystems(existing, new *knowledge.System) []string {
	var diffs []string

	if existing.Name != new.Name {
		diffs = append(diffs, fmt.Sprintf("name: %s -> %s", existing.Name, new.Name))
	}
	if existing.Faction != new.Faction {
		diffs = append(diffs, fmt.Sprintf("faction: %s -> %s", existing.Faction, new.Faction))
	}
	if existing.PoliceLevel != new.PoliceLevel {
		diffs = append(diffs, fmt.Sprintf("police: %d -> %d", existing.PoliceLevel, new.PoliceLevel))
	}

	// Compare connections
	existingConnMap := make(map[string]bool)
	for _, c := range existing.Connections {
		existingConnMap[c] = true
	}
	newConnMap := make(map[string]bool)
	for _, c := range new.Connections {
		newConnMap[c] = true
	}

	for _, c := range new.Connections {
		if !existingConnMap[c] {
			diffs = append(diffs, fmt.Sprintf("new connection: %s", c))
		}
	}
	for _, c := range existing.Connections {
		if !newConnMap[c] {
			diffs = append(diffs, fmt.Sprintf("removed connection: %s", c))
		}
	}

	return diffs
}

func saveMarketListings(client *game.Client, ctx context.Context, logger *log.Logger, systemName, systemID, baseID, baseName string) error {
	// Get listings
	if err := client.GetListings(ctx); err != nil {
		return fmt.Errorf("failed to get listings: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Try to get raw JSON from server response
	rawJSON := client.GetRawJSON("listings")
	var data []byte
	var err error

	timestamp := time.Now().Format(game.TimestampDaily)

	if rawJSON != nil {
		// Use raw JSON from server - add metadata wrapper
		wrapper := map[string]any{
			"system":              systemID,
			"system_name":         systemName,
			"base":                baseID,
			"base_name":           baseName,
			"timestamp":           time.Now().Format(time.RFC3339),
			"buy_price_modifier":  0,
			"sell_price_modifier": 0,
		}

		// Unmarshal server response
		var serverData map[string]any
		if err := json.Unmarshal(rawJSON, &serverData); err == nil {
			for k, v := range serverData {
				wrapper[k] = v
			}
		}

		data, err = json.MarshalIndent(wrapper, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal listings: %w", err)
		}
	} else {
		// Fall back to parsed data if raw JSON not available
		logger.Printf("⚠️  No raw listings JSON available, using parsed data")
		listings := client.GetMarketListings()

		wrapper := map[string]any{
			"system":              systemID,
			"system_name":         systemName,
			"base":                baseID,
			"base_name":           baseName,
			"timestamp":           time.Now().Format(time.RFC3339),
			"buy_price_modifier":  0,
			"sell_price_modifier": 0,
			"listings":            listings,
		}

		data, err = json.MarshalIndent(wrapper, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal listings: %w", err)
		}
	}

	// Save to file using library helper
	timestamp = time.Now().Format(game.TimestampDaily)
	filename := game.GenerateDataFilename(systemName+"."+baseName, timestamp, "market.listing")
	dir := filepath.Join("data", "server", "listings")

	if err := game.SaveJSONToFile(data, dir, filename); err != nil {
		return fmt.Errorf("failed to write listings: %w", err)
	}

	logger.Printf("💾 Saved market listings: %s", filepath.Join(dir, filename))
	return nil
}

// saveStationMarketData saves market listings and ship data from a station
// Saves raw JSON from server responses to avoid missing new fields
func saveStationMarketData(client *game.Client, ctx context.Context, logger *log.Logger, kb knowledge.Base, systemName, poiName, poiID string, agentID string) error {
	state := client.GetState()

	// Create listings directory
	listingsDir := filepath.Join("data", "server", "listings")
	if err := os.MkdirAll(listingsDir, 0755); err != nil {
		return fmt.Errorf("failed to create listings directory: %w", err)
	}

	// Generate daily timestamp: YYYYMMDD
	timestamp := time.Now().Format("20060102") // YYYYMMDD

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

			// Get raw JSON from server response
			rawJSON := client.GetRawJSON("listings")
			if rawJSON != nil {
				// Create wrapper with metadata
				wrapper := map[string]any{
					"system_id":   state.System.ID,
					"system_name": systemName,
					"poi_id":      poiID,
					"poi_name":    poiName,
					"timestamp":   time.Now().Format(time.RFC3339),
					"game_tick":   state.CurrentTick,
				}

				// Unmarshal server response to merge with wrapper
				var serverData map[string]any
				if err := json.Unmarshal(rawJSON, &serverData); err == nil {
					for k, v := range serverData {
						wrapper[k] = v
					}
				}

				// Save market listings with period separators: {SYSTEM}.{POI}.{TIMESTAMP}.market.listing.json
				marketFilename := fmt.Sprintf("%s.%s.%s.market.listing.json", sanitizeFilename(systemName), sanitizeFilename(poiName), timestamp)
				marketPath := filepath.Join(listingsDir, marketFilename)

				data, err := json.MarshalIndent(wrapper, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal market data: %w", err)
				}

				if err := os.WriteFile(marketPath, data, 0644); err != nil {
					return fmt.Errorf("failed to write market data: %w", err)
				}
				logger.Printf("💾 Saved market data to file: %s", marketPath)

				// Also save to knowledge base
				listings := client.GetMarketListings()
				marketSnapshot := convertMarketListingsToKnowledge(state.System.ID, systemName, poiID, poiName, state.CurrentTick, listings)
				if err := kb.StoreMarketSnapshot(ctx, marketSnapshot, agentID); err != nil {
					logger.Printf("⚠️  Failed to save market snapshot to knowledge base: %v", err)
				} else {
					logger.Printf("💾 Saved market snapshot to knowledge base")
				}
			} else {
				logger.Printf("⚠️  No raw market data available from server")
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

			// Get raw JSON from server response
			rawJSON := client.GetRawJSON("ships")
			if rawJSON != nil {
				// Create wrapper with metadata
				wrapper := map[string]any{
					"system_id":   state.System.ID,
					"system_name": systemName,
					"poi_id":      poiID,
					"poi_name":    poiName,
					"timestamp":   time.Now().Format(time.RFC3339),
					"game_tick":   state.CurrentTick,
				}

				// Unmarshal server response to merge with wrapper
				var serverData map[string]any
				if err := json.Unmarshal(rawJSON, &serverData); err == nil {
					for k, v := range serverData {
						wrapper[k] = v
					}
				}

				// Save ship listings with period separators: {SYSTEM}.{POI}.{TIMESTAMP}.ships.listing.json
				shipFilename := fmt.Sprintf("%s.%s.%s.ships.listing.json", sanitizeFilename(systemName), sanitizeFilename(poiName), timestamp)
				shipPath := filepath.Join(listingsDir, shipFilename)

				data, err := json.MarshalIndent(wrapper, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal ship data: %w", err)
				}

				if err := os.WriteFile(shipPath, data, 0644); err != nil {
					return fmt.Errorf("failed to write ship data: %w", err)
				}
				logger.Printf("💾 Saved ship data to file: %s", shipPath)

				// Also save to knowledge base
				// Extract ship listings from the response
				ships := extractShipListings(serverData)
				shipListings := convertShipListingsToKnowledge(state.System.ID, systemName, poiID, poiName, state.CurrentTick, ships)
				if err := kb.StoreShipListings(ctx, shipListings, agentID); err != nil {
					logger.Printf("⚠️  Failed to save ship listings to knowledge base: %v", err)
				} else {
					logger.Printf("💾 Saved ship listings to knowledge base")
				}
			} else {
				logger.Printf("⚠️  No raw ship data available from server")
			}
		}
	} else {
		logger.Printf("✓ Ship listings already captured today for %s", poiName)
	}

	return nil
}

// convertMarketListingsToKnowledge converts game market listings to knowledge.MarketSnapshot
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

// convertShipListingsToKnowledge converts ship listing data to knowledge.ShipListings
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

// extractShipListings extracts ship listings from the server response data
func extractShipListings(serverData map[string]any) []knowledge.ShipListing {
	var ships []knowledge.ShipListing

	// The server response format may vary - try to extract ships
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

// exploreAllPOIs visits each POI in the current system, scans, and saves data
func exploreAllPOIs(client *game.Client, ctx context.Context, logger *log.Logger, expState *ExplorationState, kb knowledge.Base) error {
	state := client.GetState()

	// Initialize visited POIs for this system if needed
	if expState.VisitedPOIs == nil {
		expState.VisitedPOIs = make(map[string]bool)
	}

	logger.Printf("🔍 Exploring %d POIs in system %s", len(state.System.POIs), state.System.Name)

	// Visit each POI
	for _, poi := range state.System.POIs {
		// Skip if already visited
		if expState.VisitedPOIs[poi.ID] {
			logger.Printf("⊙ Already visited POI: %s (%s)", poi.Name, poi.ID)
			continue
		}

		logger.Printf("📍 Visiting POI: %s (%s) - Type: %s", poi.Name, poi.ID, poi.Type)

		// Travel to POI if not already there
		if state.CurrentPOI != poi.ID {
			if err := client.Travel(ctx, poi.ID); err != nil {
				logger.Printf("Failed to travel to POI %s: %v", poi.ID, err)
				continue
			}
			logger.Printf("→ Arrived at %s", poi.Name)
			time.Sleep(3 * time.Second)
		}

		// Get POI details
		logger.Printf("🔍 Getting POI details at %s...", poi.Name)
		if err := client.GetPOI(ctx); err != nil {
			logger.Printf("Get POI failed: %v", err)
		} else {
			logger.Printf("✅ POI details retrieved at %s", poi.Name)
		}
		time.Sleep(3 * time.Second)

		// Check for nearby players/ships
		state = client.GetState()
		if len(state.Nearby) > 0 {
			logger.Printf("⚠️  %d nearby players/ships detected at %s", len(state.Nearby), poi.Name)
		}

		// Handle station-specific actions
		if poi.Type == "station" {
			logger.Printf("🏪 Station detected! Docking to collect market and ship data...")

			// Dock at the station
			if err := client.Dock(ctx); err != nil {
				if err.Error() != "Already docked (success)" {
					logger.Printf("Failed to dock: %v", err)
				} else {
					logger.Printf("✅ Already docked at %s", poi.Name)
				}
			} else {
				logger.Printf("✅ Docked at %s", poi.Name)
			}
			time.Sleep(3 * time.Second)

			// Collect and save market/ship data
			if err := saveStationMarketData(client, ctx, logger, kb, state.System.Name, poi.Name, poi.ID, expState.AgentID); err != nil {
				logger.Printf("Failed to save station data: %v", err)
			}

			// Undock before continuing exploration
			logger.Printf("📤 Undocking from %s...", poi.Name)
			if err := client.Undock(ctx); err != nil {
				logger.Printf("Failed to undock: %v", err)
			} else {
				logger.Printf("✅ Undocked from %s", poi.Name)
			}
			time.Sleep(3 * time.Second)
		}

		// Save POI-specific data to knowledge base
		kbPOI := knowledge.POI{
			ID:              poi.ID,
			SystemID:        state.System.ID,
			Name:            poi.Name,
			Type:            poi.Type,
			Description:     poi.Description,
			Position:        poi.Position,
			Services:        []string{}, // Not available in game.POI
			Resources:       poi.Resources,
			DiscoveredBy:    expState.AgentID,
			LastUpdatedTick: state.GetTick(),
		}
		if err := kb.RememberPOI(ctx, kbPOI); err != nil {
			logger.Printf("Failed to save POI to knowledge base: %v", err)
		}

		// Save POI-specific data to file
		if err := savePOIData(client, logger, poi.ID); err != nil {
			logger.Printf("Failed to save POI data: %v", err)
		}

		// Mark as visited
		expState.VisitedPOIs[poi.ID] = true

		// Small delay between POIs
		time.Sleep(2 * time.Second)
	}

	logger.Printf("✅ Completed POI exploration in %s", state.System.Name)
	return nil
}

// savePOIData saves detailed information about a specific POI
func savePOIData(client *game.Client, logger *log.Logger, poiID string) error {
	state := client.GetState()

	// Find the POI in current system
	var targetPOI *game.POI
	for i := range state.System.POIs {
		if state.System.POIs[i].ID == poiID {
			targetPOI = &state.System.POIs[i]
			break
		}
	}

	if targetPOI == nil {
		return fmt.Errorf("POI %s not found in current system", poiID)
	}

	// Build POI data structure
	poiData := map[string]any{
		"system_id":   state.System.ID,
		"system_name": state.System.Name,
		"poi": map[string]any{
			"id":          targetPOI.ID,
			"name":        targetPOI.Name,
			"type":        targetPOI.Type,
			"description": targetPOI.Description,
			"position":    targetPOI.Position,
			"resources":   targetPOI.Resources,
			"base_id":     targetPOI.BaseID,
		},
		"nearby_players": state.Nearby,
		"timestamp":      time.Now().Format(time.RFC3339),
		"game_tick":      state.CurrentTick,
	}

	// Add nearby players if combat could be a concern
	if state.InCombat {
		poiData["in_combat"] = true
		logger.Printf("⚔️  Combat detected at POI %s!", targetPOI.Name)
	}

	// Save to file using library helper
	timestamp := time.Now().Format(game.TimestampDaily)
	filename := game.GenerateDataFilename(state.System.Name+"."+targetPOI.Name, timestamp, "json")
	dir := filepath.Join("data", "server", "systems")

	if err := game.SaveStructToFile(poiData, dir, filename); err != nil {
		return fmt.Errorf("failed to write POI data: %w", err)
	}

	logger.Printf("💾 Saved POI data: %s", filepath.Join(dir, filename))
	return nil
}

// saveCombatData saves attacker information when under attack
func saveCombatData(client *game.Client, logger *log.Logger, attackers []game.NearbyPlayer, escapeRoute string) error {
	state := client.GetState()
	timestamp := time.Now().Format(game.TimestampPrecise)

	combatData := map[string]any{
		"timestamp":       time.Now().Format(game.TimestampISO8601),
		"game_tick":       state.CurrentTick,
		"system_id":       state.System.ID,
		"system_name":     state.System.Name,
		"poi_id":          state.CurrentPOI,
		"player_id":       state.Player.ID,
		"player_username": state.Username,
		"ship_id":         state.Ship.ID,
		"ship_class":      state.Ship.ClassID,
		"ship_name":       state.Ship.Name,
		"hull":            state.Ship.Hull,
		"shield":          state.Ship.Shield,
		"attackers":       attackers,
		"escape_route":    escapeRoute,
	}

	// Save to file using library helper
	filename := game.GenerateDataFilename(state.System.Name, timestamp, "combat")
	dir := filepath.Join("data", "server", "combat_logs")

	if err := game.SaveStructToFile(combatData, dir, filename); err != nil {
		return fmt.Errorf("failed to write combat data: %w", err)
	}

	logger.Printf("⚠️  Saved combat data: %s", filepath.Join(dir, filename))
	return nil
}

// checkAndEvadeCombat checks if under attack and decides whether to fight or flee
func checkAndEvadeCombat(client *game.Client, ctx context.Context, logger *log.Logger, expState *ExplorationState) (underAttack bool) {
	state := client.GetState()

	// Check combat state
	if !state.InCombat && len(state.Nearby) == 0 {
		if expState.UnderAttack {
			// Just escaped combat
			logger.Printf("✅ Successfully escaped combat!")
			expState.UnderAttack = false
		}
		return false
	}

	// Only trigger on explicit InCombat=true
	if !state.InCombat {
		if expState.UnderAttack {
			logger.Printf("✅ Combat resolved - clearing attack state")
			expState.UnderAttack = false
		}
		return false
	}

	// We're under attack!
	if !expState.UnderAttack {
		logger.Printf("⚠️  COMBAT DETECTED! Initiating emergency protocols!")
		expState.UnderAttack = true
		expState.LastAttackTime = time.Now()

		// Save combat data
		if len(state.Nearby) > 0 {
			if err := saveCombatData(client, logger, state.Nearby, expState.PreviousSystem); err != nil {
				logger.Printf("Failed to save combat data: %v", err)
			}
		}
	}

	// Assess our situation
	hullPct := getHullPercentage(state)
	shieldPct := getShieldPercentage(state)
	hasWeapon := hasWeapons(state)

	logger.Printf("⚔️  Combat Status: Hull: %.1f%%, Shield: %.1f%%, Weapons: %v", hullPct, shieldPct, hasWeapon)

	// Decision: Fight or Flee
	// Flee if:
	// - Hull below 40% (critical damage)
	// - No weapons installed
	// - Hull below 70% AND shields depleted
	shouldFlee := hullPct < 40.0 || !hasWeapon || (hullPct < 70.0 && shieldPct < 10.0)

	if shouldFlee {
		logger.Printf("🏃 FLEEING: Hull too low or no weapons - seeking safety!")
		return attemptFleeToStation(client, ctx, logger, expState, state)
	}

	// We have weapons and reasonable health - FIGHT BACK!
	logger.Printf("⚔️  FIGHTING BACK: Engaging attacker!")

	// Find the attacker from nearby players
	var attackerID string
	for _, nearby := range state.Nearby {
		if nearby.InCombat {
			attackerID = nearby.PlayerID
			logger.Printf("🎯 Targeting attacker: %s (Ship: %s)", nearby.Username, nearby.ShipClass)
			break
		}
	}

	if attackerID == "" && len(state.Nearby) > 0 {
		// If we can't identify who's in combat, attack the first nearby player
		attackerID = state.Nearby[0].PlayerID
		logger.Printf("🎯 Targeting nearby player: %s", state.Nearby[0].Username)
	}

	if attackerID != "" {
		// Attack!
		if err := client.Attack(ctx, attackerID); err != nil {
			logger.Printf("Failed to attack: %v", err)
		} else {
			logger.Printf("💥 Firing weapons at %s!", attackerID)
		}
		time.Sleep(3 * time.Second)
	}

	// Check if we should switch to fleeing after attacking
	state = client.GetState()
	hullPct = getHullPercentage(state)
	if hullPct < 30.0 {
		logger.Printf("⚠️  Hull critical (%.1f%%) - switching to flee mode!", hullPct)
		return attemptFleeToStation(client, ctx, logger, expState, state)
	}

	return true // Still in combat
}

// attemptFleeToStation tries to escape combat and find a station to dock for repairs
func attemptFleeToStation(client *game.Client, ctx context.Context, logger *log.Logger, expState *ExplorationState, state *game.State) bool {
	// Priority 1: Try to find a station in current system
	var stationPOI string
	for _, poi := range state.System.POIs {
		if poi.Type == "station" {
			stationPOI = poi.ID
			logger.Printf("🏥 Station found in current system: %s", poi.Name)
			break
		}
	}

	if stationPOI != "" {
		// Travel to station in current system
		if state.CurrentPOI != stationPOI && !state.Traveling {
			logger.Printf("→ Fleeing to station %s in current system...", stationPOI)
			if err := client.Travel(ctx, stationPOI); err != nil {
				logger.Printf("Failed to travel to station: %v", err)
			} else {
				time.Sleep(3 * time.Second)
			}
		}
		return true // Still fleeing
	}

	// Priority 2: Jump to a system with a known station (LastFuelStation or previous system)
	var targetSystem string
	if expState.LastFuelStation != "" && expState.LastFuelStation != state.CurrentSystem {
		targetSystem = expState.LastFuelStation
		logger.Printf("🏥 Fleeing to known station system: %s", targetSystem)
	} else if expState.PreviousSystem != "" && expState.PreviousSystem != state.CurrentSystem {
		targetSystem = expState.PreviousSystem
		logger.Printf("🚀 Fleeing to previous system: %s", targetSystem)
	}

	if targetSystem != "" {
		logger.Printf("🚀 Attempting escape to: %s", targetSystem)

		// Check if target system is directly connected
		directlyConnected := false
		for _, conn := range state.System.Connections {
			if conn == targetSystem {
				directlyConnected = true
				break
			}
		}

		if !directlyConnected {
			// Target not connected, find any connected system for escape
			logger.Printf("⚠️  Target system not connected, finding alternate escape route...")
			targetSystem = ""
			for _, conn := range state.System.Connections {
				if expState.VisitedSystems[conn] {
					// Jump to a visited system we know has fuel/stations
					targetSystem = conn
					logger.Printf("🚀 Escaping to connected system: %s", targetSystem)
					break
				}
			}

			if targetSystem == "" {
				// Last resort - any connected system
				for _, conn := range state.System.Connections {
					targetSystem = conn
					logger.Printf("🚀 Escaping to any connected system: %s", targetSystem)
					break
				}
			}
		}

		if targetSystem != "" {
			// Find jump gate
			var targetPOI string
			for _, poi := range state.System.POIs {
				if poi.Type == "jump_gate" {
					targetPOI = poi.ID
					break
				}
			}

			if targetPOI != "" {
				// Travel to jump gate
				if state.CurrentPOI != targetPOI {
					logger.Printf("→ Heading to jump gate: %s", targetPOI)
					if err := client.Travel(ctx, targetPOI); err != nil {
						logger.Printf("Failed to travel to jump gate: %v", err)
						return true
					}
					time.Sleep(3 * time.Second)
				}

				// Jump to safety
				logger.Printf("🌟 Jumping to safety: %s", targetSystem)
				if err := client.Jump(ctx, targetSystem); err != nil {
					logger.Printf("Failed to jump to safety: %v", err)

					// Check if we escaped despite the error (e.g., after reconnection)
					state = client.GetState()
					if state.CurrentSystem == targetSystem {
						logger.Printf("✅ Despite error, escaped to %s!", targetSystem)
						time.Sleep(5 * time.Second)
						return false // Successfully escaped
					}
					return true
				}

				logger.Printf("✅ Escaped to %s!", targetSystem)
				time.Sleep(5 * time.Second)
				return false // Successfully escaped
			} else {
				logger.Printf("❌ No jump gate found! Unable to escape!")
			}
		} else {
			logger.Printf("❌ No connected systems found! Trapped!")
		}
	}

	// No escape route available or already in escape system
	logger.Printf("⚔️  Trapped in combat! Hull: %.0f/%.0f, Shield: %.0f/%.0f",
		state.Ship.Hull, state.Ship.MaxHull,
		state.Ship.Shield, state.Ship.MaxShield)

	return true
}

func handleStations(client *game.Client, ctx context.Context, logger *log.Logger, expState *ExplorationState) error {
	state := client.GetState()

	// Find first station in system
	var stationPOI *game.POI
	for i := range state.System.POIs {
		if state.System.POIs[i].Type == "station" {
			stationPOI = &state.System.POIs[i]
			break
		}
	}

	if stationPOI == nil {
		logger.Printf("No station in this system")
		return nil
	}

	logger.Printf("🏪 Found station: %s", stationPOI.Name)

	// Travel to station if not already there
	state = client.GetState()
	if state.CurrentPOI != stationPOI.ID && !state.Traveling {
		logger.Printf("🚀 Traveling to station...")
		if err := client.Travel(ctx, stationPOI.ID); err != nil {
			return fmt.Errorf("failed to travel to station: %w", err)
		}
		time.Sleep(20 * time.Second)
	}

	// Dock
	logger.Printf("📥 Docking...")
	if err := client.Dock(ctx); err != nil {
		if err.Error() != "Already docked (success)" {
			logger.Printf("Dock error: %v", err)
		}
	}
	time.Sleep(15 * time.Second)

	// Save market listings
	state = client.GetState()
	if state.Doc {
		baseID := stationPOI.BaseID
		if baseID == "" {
			baseID = stationPOI.ID + "_base"
		}
		if err := saveMarketListings(client, ctx, logger, state.System.Name, state.System.ID, baseID, stationPOI.Name); err != nil {
			logger.Printf("Failed to save listings: %v", err)
		}

		// Update last fuel station
		expState.LastFuelStation = state.CurrentSystem

		// Refuel if needed
		if needsRefuel(state) {
			logger.Printf("⛽ Refueling...")
			if err := client.Refuel(ctx); err != nil {
				logger.Printf("Refuel error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Undock
		logger.Printf("📤 Undocking...")
		if err := client.Undock(ctx); err != nil {
			logger.Printf("Undock error: %v", err)
		}
		time.Sleep(12 * time.Second)
	}

	return nil
}

func getUnvisitedNeighbors(state *game.State, expState *ExplorationState) []string {
	unvisited := []string{}
	for _, conn := range state.System.Connections {
		if !expState.VisitedSystems[conn] {
			unvisited = append(unvisited, conn)
		}
	}
	return unvisited
}

func findAndRefuel(client *game.Client, ctx context.Context, logger *log.Logger, expState *ExplorationState) error {
	state := client.GetState()

	// Check current system for station
	var stationPOI *game.POI
	for i := range state.System.POIs {
		if state.System.POIs[i].Type == "station" {
			stationPOI = &state.System.POIs[i]
			break
		}
	}

	if stationPOI != nil {
		// Station in current system
		logger.Printf("🏪 Refueling at local station...")
		if state.CurrentPOI != stationPOI.ID {
			if err := client.Travel(ctx, stationPOI.ID); err != nil {
				return err
			}
			time.Sleep(20 * time.Second)
		}

		if err := client.Dock(ctx); err != nil {
			if err.Error() != "Already docked (success)" {
				return err
			}
		}
		time.Sleep(15 * time.Second)

		if err := client.Refuel(ctx); err != nil {
			return err
		}
		time.Sleep(3 * time.Second)

		if err := client.Undock(ctx); err != nil {
			return err
		}
		time.Sleep(12 * time.Second)

		return nil
	}

	// Backtrack to last known fuel station through visited systems
	if expState.LastFuelStation != "" && expState.LastFuelStation != state.CurrentSystem {
		logger.Printf("⚠️  Low fuel! Navigating back to %s for refuel", expState.LastFuelStation)

		// First, check if current system has a jump gate
		hasJumpGate := false
		for _, poi := range state.System.POIs {
			if poi.Type == "jump_gate" {
				hasJumpGate = true
				break
			}
		}

		if !hasJumpGate {
			logger.Printf("❌ No jump gate in current system %s! Cannot escape.", state.CurrentSystem)
			logger.Printf("💡 Tip: If trapped without fuel, consider self-destructing to respawn at home")
			return fmt.Errorf("no jump gate in current system %s", state.CurrentSystem)
		}

		// Try to find a path through connected systems
		// Check if LastFuelStation is directly connected
		directlyConnected := false
		for _, conn := range state.System.Connections {
			if conn == expState.LastFuelStation {
				directlyConnected = true
				break
			}
		}

		if directlyConnected {
			// Direct jump available
			logger.Printf("🌟 Jumping directly to %s...", expState.LastFuelStation)
			if err := client.Jump(ctx, expState.LastFuelStation); err != nil {
				// Check if we arrived despite the error (e.g., after reconnection)
				state = client.GetState()
				if state.CurrentSystem == expState.LastFuelStation {
					logger.Printf("✓ Despite error, arrived at fuel station %s", expState.LastFuelStation)
					return findAndRefuel(client, ctx, logger, expState)
				}
				return fmt.Errorf("failed to jump to fuel station: %w", err)
			}
			time.Sleep(25 * time.Second)
			return findAndRefuel(client, ctx, logger, expState)
		}

		// Not directly connected - need to find a path
		logger.Printf("🔍 Fuel station not directly connected, finding route...")

		// Check connected systems for ones with known stations
		for _, conn := range state.System.Connections {
			if expState.VisitedSystems[conn] {
				// We've been here before, check if it has a station
				logger.Printf("🌟 Jumping to %s (visited system toward fuel)...", conn)
				if err := client.Jump(ctx, conn); err != nil {
					if strings.Contains(err.Error(), "rate_limited") {
						logger.Printf("Rate limited, waiting...")
						time.Sleep(10 * time.Second)
					}
					// Check if we arrived despite the error (e.g., after reconnection)
					state = client.GetState()
					if state.CurrentSystem == conn {
						logger.Printf("✓ Despite error, arrived at %s", conn)
						return findAndRefuel(client, ctx, logger, expState)
					}
					// Try next connection
					continue
				}
				time.Sleep(25 * time.Second)

				// Recursively try to get fuel from this new system
				return findAndRefuel(client, ctx, logger, expState)
			}
		}

		// No visited connected systems - try any connected system
		for _, conn := range state.System.Connections {
			logger.Printf("🌟 Jumping to %s (exploring for fuel)...", conn)
			if err := client.Jump(ctx, conn); err != nil {
				if strings.Contains(err.Error(), "rate_limited") {
					logger.Printf("Rate limited, waiting...")
					time.Sleep(10 * time.Second)
				}
				// Check if we arrived despite the error (e.g., after reconnection)
				state = client.GetState()
				if state.CurrentSystem == conn {
					logger.Printf("✓ Despite error, arrived at %s", conn)
					return findAndRefuel(client, ctx, logger, expState)
				}
				continue
			}
			time.Sleep(25 * time.Second)
			return findAndRefuel(client, ctx, logger, expState)
		}

		return fmt.Errorf("no accessible connected systems from %s", state.CurrentSystem)
	}

	logger.Printf("⚠️  WARNING: No fuel station available!")
	return fmt.Errorf("no fuel station available")
}

func navigateToSystem(client *game.Client, ctx context.Context, logger *log.Logger, targetSystem string, expState *ExplorationState) error {
	state := client.GetState()

	// Check if we're already at the target system (can happen after reconnection)
	if state.CurrentSystem == targetSystem {
		logger.Printf("✓ Already at target system %s (skipping jump)", targetSystem)
		return nil
	}

	// Store current system as previous before jumping (for escape routes)
	if state.CurrentSystem != expState.HomeSystem || expState.PreviousSystem == "" {
		expState.PreviousSystem = state.CurrentSystem
		logger.Printf("📍 Setting escape route: %s", expState.PreviousSystem)
	}

	// Check fuel before jump
	fuelNeeded := 10.0 // Base fuel cost
	if state.Fuel < fuelNeeded+20 {
		logger.Printf("⚠️  Low fuel (%.0f), refueling before jump...", state.Fuel)
		if err := findAndRefuel(client, ctx, logger, expState); err != nil {
			return err
		}
	}

	// Undock if needed
	if state.Doc {
		logger.Printf("📤 Undocking...")
		if err := client.Undock(ctx); err != nil {
			logger.Printf("Undock error: %v", err)
		}
		time.Sleep(12 * time.Second)
	}

	// Jump to target system
	logger.Printf("🌟 Jumping to %s...", targetSystem)
	if err := client.Jump(ctx, targetSystem); err != nil {
		return fmt.Errorf("failed to jump: %w", err)
	}
	time.Sleep(25 * time.Second)

	return nil
}

// isDamaged checks if the ship is damaged below 50% hull
func isDamaged(state *game.State) bool {
	if state.Ship.MaxHull == 0 {
		return false // Avoid division by zero
	}
	hullPercent := (state.Ship.Hull / state.Ship.MaxHull) * 100
	return hullPercent < 50
}

// findNearestStation looks for a station in the current system or returns the last known fuel station
func findNearestStation(state *game.State, lastFuelStation string) (string, string, string) {
	// First check if current system has a station
	for _, poi := range state.System.POIs {
		if poi.Type == "station" {
			return state.CurrentSystem, poi.ID, poi.Name
		}
	}

	// If no station in current system, return the last known fuel station
	if lastFuelStation != "" && lastFuelStation != state.CurrentSystem {
		return lastFuelStation, "", "" // Will need to find station POI after jumping
	}

	// No station found
	return "", "", ""
}

// repairShip travels to the nearest station and repairs the ship
func repairShip(client *game.Client, ctx context.Context, logger *log.Logger, expState *ExplorationState) error {
	state := client.GetState()

	logger.Printf("⚠️  SHIP DAMAGED! Hull: %.0f/%.0f (%.1f%%), Armor: %.0f",
		state.Ship.Hull,
		state.Ship.MaxHull,
		(state.Ship.Hull/state.Ship.MaxHull)*100,
		state.Ship.Armor)

	// Find nearest station
	systemID, poiID, poiName := findNearestStation(state, expState.LastFuelStation)

	if systemID == "" {
		logger.Printf("❌ No station found! Attempting to return to home system: %s", expState.HomeSystem)
		systemID = expState.HomeSystem
	}

	// If station is in different system, jump to it
	if systemID != state.CurrentSystem {
		logger.Printf("🚀 Traveling to system with station: %s", systemID)

		// Find jump gate
		var targetPOI string
		for _, poi := range state.System.POIs {
			if poi.Type == "jump_gate" {
				targetPOI = poi.ID
				break
			}
		}

		if targetPOI == "" {
			logger.Printf("⚠️  No jump gate in current system, cannot reach station")
			return fmt.Errorf("no jump gate found in current system")
		}

		// Travel to jump gate if not there
		if state.CurrentPOI != targetPOI && !state.Traveling {
			if err := client.Travel(ctx, targetPOI); err != nil {
				logger.Printf("Failed to travel to jump gate: %v", err)
				time.Sleep(10 * time.Second) // Wait before retry
				return err
			}
			time.Sleep(20 * time.Second)
		}

		// Wait for any ongoing travel to complete
		if state.Traveling {
			logger.Printf("⏳ Waiting for travel to complete...")
			time.Sleep(20 * time.Second)
		}

		// Jump to station system
		if err := client.Jump(ctx, systemID); err != nil {
			logger.Printf("Failed to jump to %s: %v", systemID, err)
			time.Sleep(10 * time.Second) // Wait before retry

			// Check if we arrived despite the error (e.g., after reconnection)
			state = client.GetState()
			if state.CurrentSystem != systemID {
				return err
			}
			logger.Printf("✓ Despite error, arrived at %s", systemID)
		} else {
			time.Sleep(25 * time.Second)
			// Update state after jump
			state = client.GetState()
		}

		// Find station in the new system
		for _, poi := range state.System.POIs {
			if poi.Type == "station" {
				poiID = poi.ID
				poiName = poi.Name
				break
			}
		}

		if poiID == "" {
			logger.Printf("⚠️  No station found in system %s", systemID)
			return fmt.Errorf("no station found in system %s", systemID)
		}
	} else {
		// Already in the system with station, find it if poiID is empty
		if poiID == "" {
			for _, poi := range state.System.POIs {
				if poi.Type == "station" {
					poiID = poi.ID
					poiName = poi.Name
					break
				}
			}
		}
	}

	// Travel to station if not already there
	if state.CurrentPOI != poiID && !state.Traveling {
		logger.Printf("🚀 Traveling to station: %s", poiName)
		if err := client.Travel(ctx, poiID); err != nil {
			logger.Printf("Failed to travel to station: %v", err)
			time.Sleep(10 * time.Second)
			return err
		}
		time.Sleep(20 * time.Second)
	}

	// Dock
	logger.Printf("📥 Docking at %s...", poiName)
	if err := client.Dock(ctx); err != nil && err.Error() != "Already docked (success)" {
		logger.Printf("Failed to dock: %v", err)
		time.Sleep(10 * time.Second)
		return err
	}
	time.Sleep(15 * time.Second)

	// Repair
	logger.Printf("🔧 Repairing ship...")
	if err := client.Repair(ctx); err != nil {
		logger.Printf("Failed to repair: %v", err)
		time.Sleep(10 * time.Second)
		return err
	}
	time.Sleep(3 * time.Second)

	// Check repair success
	state = client.GetState()
	logger.Printf("✅ Repaired! Hull: %.0f/%.0f (%.1f%%), Armor: %.0f",
		state.Ship.Hull,
		state.Ship.MaxHull,
		(state.Ship.Hull/state.Ship.MaxHull)*100,
		state.Ship.Armor)

	// Update last fuel station
	expState.LastFuelStation = systemID

	// Undock before continuing
	logger.Printf("📤 Undocking from %s...", poiName)
	if err := client.Undock(ctx); err != nil {
		logger.Printf("Failed to undock: %v", err)
	}
	time.Sleep(12 * time.Second)

	return nil
}

func explorationPhase(client *game.Client, logger *log.Logger, ctx context.Context, kb knowledge.Base, agentID string) error {
	state := client.GetState()

	// Initialize exploration state
	expState := &ExplorationState{
		VisitedSystems:  make(map[string]bool),
		VisitedPOIs:     make(map[string]bool),
		DFSStack:        []string{},
		HomeSystem:      state.CurrentSystem,
		LastFuelStation: state.CurrentSystem,
		PreviousSystem:  "", // Will be set when we first jump
		AgentID:         agentID,
	}

	logger.Printf("Starting DFS exploration from home system: %s", expState.HomeSystem)

	// Captain's log ticker - update every 2 minutes
	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	// Initial captain's log entry
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

		// Check for combat and evade if necessary
		if checkAndEvadeCombat(client, ctx, logger, expState) {
			// We're in combat, wait before trying again
			time.Sleep(10 * time.Second)
			continue
		}

		// Check for damage and repair if necessary
		if isDamaged(state) {
			logger.Printf("💔 Ship damaged, seeking repairs...")
			if err := repairShip(client, ctx, logger, expState); err != nil {
				logger.Printf("Failed to repair: %v", err)
				// Wait before retrying to avoid rate limiting
				time.Sleep(10 * time.Second)
				continue
			}
			// After repair, restart the loop to check state again
			continue
		}

		// Mark current system as visited
		if !expState.VisitedSystems[currentSystem] {
			logger.Printf("📍 Exploring new system: %s", currentSystem)
			expState.VisitedSystems[currentSystem] = true
			expState.VisitedPOIs = make(map[string]bool) // Reset POI visits for new system

			// Update captain's log for new system discovery
			updateCaptainsLog(agentID, client, expState)

			// Collect system data
			if err := collectSystemData(client, ctx, logger, kb, expState.AgentID); err != nil {
				logger.Printf("Failed to collect system data: %v", err)
			}

			// Explore all POIs in this system
			logger.Printf("🔍 Beginning comprehensive POI exploration...")
			if err := exploreAllPOIs(client, ctx, logger, expState, kb); err != nil {
				logger.Printf("POI exploration failed: %v", err)
			}

			// Handle stations (listings, refuel) after POI exploration
			if err := handleStations(client, ctx, logger, expState); err != nil {
				logger.Printf("Failed to handle stations: %v", err)
			}
		}

		// Get unvisited neighbors
		unvisited := getUnvisitedNeighbors(state, expState)

		if len(unvisited) > 0 {
			// Push current system to stack and explore first unvisited neighbor
			expState.DFSStack = append(expState.DFSStack, currentSystem)
			nextSystem := unvisited[0]
			logger.Printf("→ Moving to unvisited system: %s (Stack depth: %d)", nextSystem, len(expState.DFSStack))

			if err := navigateToSystem(client, ctx, logger, nextSystem, expState); err != nil {
				errMsg := err.Error()

				// Handle "not connected" errors specially - need reconnection time
				if strings.Contains(errMsg, "not connected") || strings.Contains(errMsg, "rate_limit") {
					logger.Printf("⚠️  Connection error detected, waiting for reconnection...")
					time.Sleep(20 * time.Second) // Longer wait for reconnection

					// Refresh system data to verify actual location
					logger.Printf("🔍 Refreshing system data after reconnection...")
					if refreshErr := client.GetSystem(ctx); refreshErr != nil {
						logger.Printf("⚠️  Failed to refresh system: %v", refreshErr)
					} else {
						time.Sleep(2 * time.Second)
					}

					// Check if we're actually connected now
					state = client.GetState()
					if state.CurrentSystem == nextSystem {
						logger.Printf("✓ Successfully arrived at %s after reconnection", nextSystem)
						// Don't retry - we're already there
						continue
					}

					logger.Printf("⚠️  Still in %s, failed to reach %s", state.CurrentSystem, nextSystem)
					// Pop system from stack and try different route
					if len(expState.DFSStack) > 0 {
						expState.DFSStack = expState.DFSStack[:len(expState.DFSStack)-1]
					}
					// Extra delay before trying again to avoid rate limiting
					time.Sleep(10 * time.Second)
				} else {
					logger.Printf("Navigation error: %v", err)
					time.Sleep(10 * time.Second)

					// After reconnection, check if we actually arrived at destination despite the error
					state = client.GetState()
					if state.CurrentSystem == nextSystem {
						logger.Printf("✓ Despite error, we successfully arrived at %s", nextSystem)
						// Don't retry - we're already there
					} else {
						logger.Printf("⚠️  Still in %s, failed to reach %s", state.CurrentSystem, nextSystem)
						// Pop the system we just added to the stack since navigation failed
						if len(expState.DFSStack) > 0 {
							expState.DFSStack = expState.DFSStack[:len(expState.DFSStack)-1]
						}
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
					if err := navigateToSystem(client, ctx, logger, expState.HomeSystem, expState); err != nil {
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

			if err := navigateToSystem(client, ctx, logger, backtrackSystem, expState); err != nil {
				errMsg := err.Error()

				// Handle "not connected" errors specially - need reconnection time
				if strings.Contains(errMsg, "not connected") || strings.Contains(errMsg, "rate_limit") {
					logger.Printf("Connection error detected, waiting for reconnection...")
					time.Sleep(20 * time.Second) // Longer wait for reconnection

					// Refresh system data to verify actual location
					logger.Printf("Refreshing system data after reconnection...")
					if refreshErr := client.GetSystem(ctx); refreshErr != nil {
						logger.Printf("Failed to refresh system: %v", refreshErr)
					} else {
						time.Sleep(2 * time.Second)
					}

					// Check if we're actually connected now
					state = client.GetState()
					if state.CurrentSystem == backtrackSystem {
						logger.Printf("Successfully backtracked to %s after reconnection", backtrackSystem)
						// Don't retry - we're already there
						continue
					}

					logger.Printf("Still in %s, failed to reach %s", state.CurrentSystem, backtrackSystem)
					// Don't put system back on stack - try different route
					time.Sleep(10 * time.Second)
				} else {
					logger.Printf("Backtrack error: %v", err)
					time.Sleep(10 * time.Second)

					// After reconnection, check if we actually arrived at destination despite error
					state = client.GetState()
					if state.CurrentSystem == backtrackSystem {
						logger.Printf("Despite error, we successfully backtracked to %s", backtrackSystem)
						// Don't retry - we're already there
					} else {
						logger.Printf("Still in %s, failed to reach %s", state.CurrentSystem, backtrackSystem)
						// Put system back on stack since we failed to reach it
						expState.DFSStack = append(expState.DFSStack, backtrackSystem)
					}
				}
			}

		}

		time.Sleep(5 * time.Second)
	}
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	flag.Parse()

	// Get remaining args after flag parsing
	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: auto-explorer <explorer-number>")
		fmt.Println("Example: auto-explorer 1")
		fmt.Println("\nFlags:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// auto-explorer isnt limited to just explorer-%, anyone can do it.
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

	// Load credentials using shared library function
	creds, err := game.LoadCredentials(fmt.Sprintf("data/agents/%s", explorer))
	if err != nil {
		log.Fatalf("Failed to load credentials: %v", err)
	}

	logger.Printf("🔭 Starting autonomous explorer bot...")
	logger.Printf("Explorer: %s | Empire: %s", creds.Username, creds.Empire)

	// Create context for lifecycle management
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
			// Update status
			if err := regClient.UpdateStatus("starting", "Initializing"); err != nil {
				logger.Printf("Warning: Failed to update status: %v", err)
			}
		}
	}

	// Create game client using shared library function
	// This handles: credential loading, client creation, connection, and login
	client, _, err := game.InitializeAgent(explorer, logger, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	// Set up explorer-specific handler with knowledge base integration
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
		// Deregister from registry
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

	// Get initial state
	state := client.GetState()
	logger.Printf("✓ Ready! Credits: %.2f | Ship: %s", state.Credits, state.Ship.Name)

	// Check if we need Phase 1 (mining & upgrades)
	// Skip if already have a better ship than starter_mining (Prospector)
	skipMiningPhase := state.Ship.ClassID != "starter_mining"

	if skipMiningPhase {
		logger.Printf("")
		logger.Printf("═══════════════════════════════════════════════")
		logger.Printf("      Already have %s - skipping mining phase!", state.Ship.ClassID)
		logger.Printf("═══════════════════════════════════════════════")
		logger.Printf("")
	} else {
		// PHASE 1: Mining & Upgrades
		logger.Printf("")
		logger.Printf("═══════════════════════════════════════════════")
		logger.Printf("        PHASE 1: Mining & Upgrades")
		logger.Printf("═══════════════════════════════════════════════")
		logger.Printf("")

		if err := miningPhase(client, logger, ctx); err != nil {
			log.Fatalf("Mining phase error: %v", err)
		}
	}

	// PHASE 2: Galaxy Exploration
	logger.Printf("")
	logger.Printf("═══════════════════════════════════════════════")
	logger.Printf("      PHASE 2: Galaxy Exploration (DFS)")
	logger.Printf("═══════════════════════════════════════════════")
	logger.Printf("")

	if err := explorationPhase(client, logger, ctx, kb, explorer); err != nil {
		log.Fatalf("Exploration phase error: %v", err)
	}
}

// initKnowledgeBase creates the knowledge base based on backend type
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
