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
	h.logger.Printf("✓ Connected! Credits: %.2f", state.Credits)
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

func isItemOwned(state *game.State, itemID string) bool {
	// Check if installed on ship
	for _, module := range state.Ship.Modules {
		if module == itemID {
			return true
		}
	}

	// Check if in cargo
	for _, item := range state.Ship.Cargo {
		if item.ItemID == itemID && item.Quantity > 0 {
			return true
		}
	}

	return false
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

func needsRefuel(state *game.State) bool {
	return state.Fuel < (state.MaxFuel * 0.3)
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

	miningRuns := 0
	state := client.GetState()
	startingCredits := state.Credits

	// Continue mining until we have mining_enhanced ship with 3 lasers OR scanner (not all stations sell scanners)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		state = client.GetState()
		miningRuns++

		// Check if we've completed Phase 1 goals
		hasDrillship := state.Ship.ClassID == "mining_enhanced" || state.Ship.ClassID == "mining_barge"
		hasTripleLasers := false
		miningLaserCount := 0
		for _, module := range state.Ship.Modules {
			if strings.HasPrefix(module, "mining_laser") {
				miningLaserCount++
			}
		}
		hasTripleLasers = miningLaserCount >= 3
		hasScanner := hasScanner(state)

		// Phase 1 complete if we have Drillship with 3 lasers, OR we have scanner + mining laser
		phase1Complete := (hasDrillship && hasTripleLasers) || (hasScanner && hasMiningLaser(state))

		if phase1Complete {
			logger.Printf("✅ Phase 1 COMPLETE! Exploration equipment ready!")
			logger.Printf("Ship: %s | Mining Lasers: %d | Scanner: %v", state.Ship.Name, miningLaserCount, hasScanner)
			logger.Printf("Credits: %.2f (started with %.2f)", state.Credits, startingCredits)
			return nil
		}

		logger.Printf("═══ Mining Run #%d ═══", miningRuns)
		logger.Printf("Credits: %.2f | Fuel: %.0f/%.0f | Cargo: %.1f/%.1f",
			state.Credits, state.Fuel, state.MaxFuel,
			state.Ship.CargoUsed, state.Ship.CargoCapacity)

		// Get full system data to see POIs
		if len(state.System.POIs) == 0 {
			logger.Printf("Fetching system data...")
			if err := client.GetSystem(ctx); err != nil {
				logger.Printf("Failed to get system: %v", err)
			}
			time.Sleep(2 * time.Second)
			state = client.GetState()
		}

		// Find a mining POI in the current system
		var miningPOI string
		for _, poi := range state.System.POIs {
			if poi.Type == "asteroid_belt" || poi.Type == "asteroid_field" {
				miningPOI = poi.ID
				break
			}
		}

		if miningPOI == "" {
			logger.Printf("⚠️  No mining POI found in current system!")
			return fmt.Errorf("no mining location available")
		}

		// Step 1: Undock if docked
		if state.Doc {
			logger.Printf("📤 Undocking from station...")
			if err := client.Undock(ctx); err != nil {
				logger.Printf("Undock error: %v", err)
			}
			time.Sleep(12 * time.Second)
		}

		// Step 2: Travel to mining POI
		state = client.GetState()
		if state.CurrentPOI != miningPOI && !state.Traveling {
			logger.Printf("🚀 Traveling to mining location...")
			if err := client.Travel(ctx, miningPOI); err != nil {
				logger.Printf("Travel error: %v", err)
			}
			time.Sleep(20 * time.Second)
		}

		// Step 3: Mine until cargo full
		mineCount := 0
		logger.Printf("⛏️  Starting mining operations...")
		for {
			state = client.GetState()

			if state.Ship.CargoUsed >= state.Ship.CargoCapacity*0.9 {
				logger.Printf("✓ Cargo nearly full (%.1f/%.1f), heading back",
					state.Ship.CargoUsed, state.Ship.CargoCapacity)
				break
			}

			if state.Fuel < 30 {
				logger.Printf("⚠️  Low fuel (%.0f), heading back", state.Fuel)
				break
			}

			if err := client.Mine(ctx); err == nil {
				mineCount++
				if mineCount%3 == 0 {
					logger.Printf("⛏️  Mining... [%d] (%.1f/%.1f cargo)",
						mineCount, state.Ship.CargoUsed, state.Ship.CargoCapacity)
				}
			}

			time.Sleep(11 * time.Second)

			if mineCount >= 15 {
				break
			}
		}

		logger.Printf("✓ Mined %d times this run", mineCount)

		// Step 4: Find station and return
		var stationPOI string
		for _, poi := range state.System.POIs {
			if poi.Type == "station" {
				stationPOI = poi.ID
				break
			}
		}

		if stationPOI == "" {
			logger.Printf("⚠️  No station found in current system!")
			return fmt.Errorf("no station available")
		}

		// Travel to station
		state = client.GetState()
		if state.CurrentPOI != stationPOI && !state.Traveling {
			logger.Printf("🚀 Returning to station...")
			if err := client.Travel(ctx, stationPOI); err != nil {
				logger.Printf("Travel error: %v", err)
			}
			time.Sleep(20 * time.Second)
		}

		// Step 5: Dock
		logger.Printf("📥 Docking at station...")
		if err := client.Dock(ctx); err != nil {
			if err.Error() != "Already docked (success)" {
				logger.Printf("Dock error: %v", err)
			}
		}
		time.Sleep(15 * time.Second)

		// Step 6: Sell all cargo
		state = client.GetState()
		if state.Doc && len(state.Ship.Cargo) > 0 {
			logger.Printf("💰 Selling cargo...")
			if err := client.SellAll(ctx); err != nil {
				logger.Printf("Sell error: %v", err)
			}
			time.Sleep(5 * time.Second)
		}

		// Step 7: Refuel if needed
		state = client.GetState()
		if state.Doc && state.Fuel < state.MaxFuel*0.8 {
			logger.Printf("⛽ Refueling...")
			if err := client.Refuel(ctx); err != nil {
				logger.Printf("Refuel error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 8: Attempt upgrades
		state = client.GetState()
		logger.Printf("Current credits: %.2f", state.Credits)
		attemptExplorerUpgrades(client, logger, ctx)

		time.Sleep(5 * time.Second)
	}
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
	kbSystem := convertGameSystemToKnowledge(state.System, agentID)

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
func convertGameSystemToKnowledge(sys game.SystemData, discoveredBy string) knowledge.System {
	kbSystem := knowledge.System{
		ID:           sys.ID,
		Name:         sys.Name,
		PoliceLevel:  sys.PoliceLevel,
		Faction:      sys.Empire,
		Connections:  sys.Connections,
		DiscoveredBy: discoveredBy,
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

	timestamp := time.Now().Format("20060102") // YYYYMMDD

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

	// Save to file
	filename := fmt.Sprintf("%s.%s.%s.json",
		sanitizeFilename(systemName),
		sanitizeFilename(baseName),
		timestamp)
	filePath := filepath.Join("data", "server", "listings", filename)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write listings: %w", err)
	}

	logger.Printf("💾 Saved market listings: %s", filePath)
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
			ID:           poi.ID,
			SystemID:     state.System.ID,
			Name:         poi.Name,
			Type:         poi.Type,
			Description:  poi.Description,
			Position:     poi.Position,
			Services:     []string{}, // Not available in game.POI
			Resources:    poi.Resources,
			DiscoveredBy: expState.AgentID,
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

	// Marshal to JSON
	data, err := json.MarshalIndent(poiData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal POI data: %w", err)
	}

	// Create filename: {SYSTEM_NAME}.{POI_NAME}.{TIMESTAMP}json
	systemName := sanitizeFilename(state.System.Name)
	poiName := sanitizeFilename(targetPOI.Name)
	timestamp := time.Now().Format("20060102") // YYYYMMDD
	filename := fmt.Sprintf("%s.%s.%s.json", systemName, poiName, timestamp)
	filePath := filepath.Join("data", "server", "systems", filename)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write POI data: %w", err)
	}

	logger.Printf("💾 Saved POI data: %s", filePath)
	return nil
}

// saveCombatData saves attacker information when under attack
func saveCombatData(client *game.Client, logger *log.Logger, attackers []game.NearbyPlayer, escapeRoute string) error {
	state := client.GetState()
	timestamp := time.Now().Format("20060102150405") // YYYYMMDDHHMMSS

	combatData := map[string]any{
		"timestamp":       time.Now().Format(time.RFC3339),
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

	// Marshal to JSON
	data, err := json.MarshalIndent(combatData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal combat data: %w", err)
	}

	// Create filename: combat_{SYSTEM_NAME}_{TIMESTAMP}.json
	systemName := sanitizeFilename(state.System.Name)
	filename := fmt.Sprintf("combat.%s.%s.json", systemName, timestamp)
	filePath := filepath.Join("data", "server", "combat_logs", filename)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write combat data: %w", err)
	}

	logger.Printf("⚠️  Saved combat data: %s", filePath)
	return nil
}

// checkAndEvadeCombat checks if under attack and attempts escape
func checkAndEvadeCombat(client *game.Client, ctx context.Context, logger *log.Logger, expState *ExplorationState) (underAttack bool) {
	state := client.GetState()

	// Check combat state
	// Only consider it combat if actually in combat OR if there are hostile nearby players
	if !state.InCombat && len(state.Nearby) == 0 {
		if expState.UnderAttack {
			// Just escaped combat
			logger.Printf("✅ Successfully escaped combat!")
			expState.UnderAttack = false
		}
		return false
	}

	// Additional check: only treat nearby players as combat if we're at a dangerous location
	// or if they're actually hostile (for now, we'll only trigger on explicit InCombat=true)
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

	// Attempt escape if we have a previous system to return to
	if expState.PreviousSystem != "" && expState.PreviousSystem != state.CurrentSystem {
		logger.Printf("🚀 Attempting escape to previous system: %s", expState.PreviousSystem)

		// Check if previous system is directly connected
		directlyConnected := false
		for _, conn := range state.System.Connections {
			if conn == expState.PreviousSystem {
				directlyConnected = true
				break
			}
		}

		var targetSystem string
		if directlyConnected {
			// Can jump directly to previous system
			targetSystem = expState.PreviousSystem
			logger.Printf("🚀 Direct escape to %s", targetSystem)
		} else {
			// Previous system not connected, find any connected system for escape
			logger.Printf("⚠️  Previous system not connected, finding alternate escape route...")
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
			return err
		}
		time.Sleep(25 * time.Second)

		// Update state after jump
		state = client.GetState()

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

	for {
		select {
		case <-ctx.Done():
			return nil
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
				logger.Printf("Navigation error: %v", err)
				time.Sleep(10 * time.Second)
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
				logger.Printf("Backtrack error: %v", err)
				time.Sleep(10 * time.Second)
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
	defer kb.Close()
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
			regClient.UpdateStatus("starting", "Initializing")
		}
	}

	// Create game client using shared library function
	// This handles: credential loading, client creation, connection, and login
	client, creds, err := game.InitializeAgent(explorer, logger, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer client.Close()

	// Set up explorer-specific handler with knowledge base integration
	explorerHandler := &explorerSimpleHandler{
		client:  client,
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
		client.Close()
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
		regClient.UpdateStatus("connecting", "Logging in")
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
