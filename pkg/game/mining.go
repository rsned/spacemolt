package game

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// MiningType specifies the type of resource extraction to perform.
type MiningType string

const (
	// MiningTypeAsteroid mines asteroid belts/fields using mining lasers.
	MiningTypeAsteroid MiningType = "asteroid"
	// MiningTypeGas harvests gas clouds using gas harvesters.
	MiningTypeGas MiningType = "gas"
	// MiningTypeIce harvests ice fields using ice harvesters.
	MiningTypeIce MiningType = "ice"
)

// miningTypeConfig holds POI types and equipment prefixes for each mining type.
type miningTypeConfig struct {
	poiTypes       []string
	equipPrefixes  []string // module name prefixes (lowercase) to count
	equipTypePrefixes []string // module type prefixes (lowercase) to count
	label          string   // display label for logs
}

var miningTypeConfigs = map[MiningType]miningTypeConfig{
	MiningTypeAsteroid: {
		poiTypes:          []string{"asteroid_belt", "asteroid_field"},
		equipPrefixes:     []string{"mining laser", "strip mining laser"},
		equipTypePrefixes: []string{"mining"},
		label:             "asteroid",
	},
	MiningTypeGas: {
		poiTypes:          []string{"gas_cloud"},
		equipPrefixes:     []string{"gas harvester"},
		equipTypePrefixes: []string{"gas_harvester"},
		label:             "gas",
	},
	MiningTypeIce: {
		poiTypes:          []string{"ice_field"},
		equipPrefixes:     []string{"ice harvester"},
		equipTypePrefixes: []string{"ice_harvester"},
		label:             "ice",
	},
}

// StationActionStrategy defines what to do with cargo when docked at a station
type StationActionStrategy func(client GameClient, logger *log.Logger, ctx context.Context) error

// StationActionSellAll simply sells all cargo without any crafting
func StationActionSellAll(client GameClient, logger *log.Logger, ctx context.Context) error {
	state := client.GetState()
	if len(state.Ship.Cargo) == 0 {
		logger.Printf("📦 Cargo is empty, nothing to sell")
		return nil
	}

	// List what we're selling
	logger.Printf("💰 Selling all cargo (%d items)...", len(state.Ship.Cargo))
	for _, item := range state.Ship.Cargo {
		logger.Printf("   - %s x%.0f", item.ItemID, item.Quantity)
	}

	if err := client.SellAllBulk(ctx, nil); err != nil {
		return fmt.Errorf("sell failed: %w", err)
	}

	time.Sleep(5 * time.Second)
	logger.Printf("✅ Sold all cargo!")
	return nil
}

// StationActionCraftAndSell crafts items from cargo resources, then sells everything
func StationActionCraftAndSell(client GameClient, logger *log.Logger, ctx context.Context) error {
	state := client.GetState()
	if len(state.Ship.Cargo) == 0 {
		logger.Printf("📦 Cargo is empty, nothing to craft or sell")
		return nil
	}

	// Try to craft items from cargo (if crafting config is available).
	// CraftItems is only available on the WebSocket *Client, not the GameClient interface.
	if wsClient, ok := client.(*Client); ok && wsClient.CraftingConfig != nil {
		logger.Printf("🔨 Querying craftable recipes from cargo...")
		crafted, err := wsClient.CraftItems(ctx, logger, wsClient.CraftingConfig)
		if err != nil {
			logger.Printf("⚠️  Crafting query failed: %v, selling raw cargo", err)
		} else if crafted > 0 {
			logger.Printf("✅ Successfully crafted %d items!", crafted)
		} else {
			logger.Printf("ℹ️  No craftable recipes found with current cargo/skills")
		}
	} else {
		logger.Printf("ℹ️  Crafting not configured, skipping to sell")
	}

	// Sell everything (crafted items + remaining raw materials)
	logger.Printf("💰 Selling all cargo (%d items)...", len(state.Ship.Cargo))
	if err := client.SellAllBulk(ctx, nil); err != nil {
		return fmt.Errorf("sell failed: %w", err)
	}

	time.Sleep(5 * time.Second)
	logger.Printf("✅ Sold all cargo!")
	return nil
}

// StationActionCraftAndDeposit crafts items from cargo resources, then deposits everything
func StationActionCraftAndDeposit(client GameClient, logger *log.Logger, ctx context.Context) error {
	state := client.GetState()
	if len(state.Ship.Cargo) == 0 {
		logger.Printf("📦 Cargo is empty, nothing to deposit")
		return nil
	}

	// Try to craft items from cargo (if crafting config is available).
	// CraftItems is only available on the WebSocket *Client, not the GameClient interface.
	if wsClient, ok := client.(*Client); ok && wsClient.CraftingConfig != nil {
		logger.Printf("🔨 Querying craftable recipes from cargo...")
		crafted, err := wsClient.CraftItems(ctx, logger, wsClient.CraftingConfig)
		if err != nil {
			logger.Printf("⚠️  Crafting query failed: %v, depositing raw cargo", err)
		} else if crafted > 0 {
			logger.Printf("✅ Successfully crafted %d items!", crafted)
		} else {
			logger.Printf("ℹ️  No craftable recipes found with current cargo/skills")
		}
	} else {
		logger.Printf("ℹ️  Crafting not configured, skipping to deposit")
	}

	// Wait a moment before depositing to ensure all crafting actions are complete
	time.Sleep(SleepShort)

	// Deposit all items to station storage
	logger.Printf("📥 Depositing all cargo to station storage (%d items)...", len(state.Ship.Cargo))
	for _, item := range state.Ship.Cargo {
		logger.Printf("   - %s x%.0f", item.ItemID, item.Quantity)
	}

	if err := client.DepositAllItems(ctx); err != nil {
		return fmt.Errorf("deposit failed: %w", err)
	}

	logger.Printf("✅ Deposited all cargo to station storage!")
	return nil
}

// MiningLoopConfig configures the behavior of the mining loop
type MiningLoopConfig struct {
	// AgentID for captain's log updates (optional)
	AgentID string

	// MiningType selects the resource type to mine: asteroid (default), gas, or ice.
	// This determines which POI types to target and which equipment to count.
	MiningType MiningType

	// StopCondition is called before each mining run to check if we should stop
	// Return true to stop mining, false to continue
	// If nil, mining continues indefinitely
	StopCondition func(state *State) bool

	// OnRunComplete is called after each successful mining run (optional)
	// Useful for tracking stats, updating logs, etc.
	OnRunComplete func(runNum int, creditsEarned float64, totalCredits float64)

	// OnUpgradeCheck is called when it's time to check for upgrades (optional)
	// Return true if an upgrade was performed
	OnUpgradeCheck func() bool

	// OnStationActions is called when docked at station with cargo
	// Determines what to do with cargo: sell, craft+sell, or craft+deposit
	// If nil, defaults to selling everything (StationActionSellAll)
	OnStationActions StationActionStrategy

	// UpgradeCheckInterval controls how often to check for upgrades
	// If 0, checks every run when credits > tier1Threshold
	// If > 0, checks every N runs
	UpgradeCheckInterval int

	// Tier1Threshold is the minimum credits to trigger upgrade checks
	// If 0, defaults to 300.0
	Tier1Threshold float64

	// ReserveCredits is the amount to always keep (never spend below this)
	// If 0, defaults to 50.0
	ReserveCredits float64

	// MaxMiningAttempts limits mining iterations per run
	// If 0, calculates based on cargo capacity and laser count
	MaxMiningAttempts int

	// CargoFullThreshold is the percentage of cargo capacity to consider "full"
	// If 0, defaults to 0.97 (97%)
	CargoFullThreshold float64

	// FuelLowThreshold is the percentage of fuel to consider "low"
	// If 0, defaults to 0.1 (10%)
	FuelLowThreshold float64

	// UseBulkSell uses SellAllBulk instead of SellAll
	// Recommended for better performance
	UseBulkSell bool

	// CaptainsLogInterval controls how often to update captain's log
	// If 0, defaults to 2 minutes
	CaptainsLogInterval time.Duration
}

// MiningLoopResult contains stats from a mining loop execution
type MiningLoopResult struct {
	RunsCompleted      int
	TotalCreditsEarned float64
	StartingCredits    float64
	EndingCredits      float64
	StoppedReason      string // "context_cancelled", "stop_condition", "error", etc.
}

// MiningLoop runs the core mining cycle: undock, mine, dock, sell, refuel, repair, upgrade
//
// This is the shared mining loop used by auto-miner, auto-explorer, and other autonomous agents.
// It handles the complete mining workflow:
//  1. Find mining POI and station in current system
//  2. Undock if docked
//  3. Travel to mining location
//  4. Mine until cargo full or fuel low
//  5. Travel back to station
//  6. Dock at station
//  7. Sell all cargo
//  8. Refuel if needed
//  9. Repair if needed
//
// 10. Check for upgrades (if configured)
//
// The loop continues until:
//   - Context is cancelled
//   - StopCondition returns true
//   - An error occurs (returns error)
//
// Example usage:
//
//	config := &game.MiningLoopConfig{
//	    AgentID: "miner-1",
//	    StopCondition: func(state *game.State) bool {
//	        return state.Credits >= 5000.0 // Stop when we have 5000 credits
//	    },
//	    OnUpgradeCheck: func() bool {
//	        return attemptUpgrades(client, logger, ctx)
//	    },
//	    UseBulkSell: true,
//	}
//	result, err := game.MiningLoop(client, logger, ctx, config)
func MiningLoop(client GameClient, logger *log.Logger, ctx context.Context, config *MiningLoopConfig) (*MiningLoopResult, error) {
	// Apply defaults
	if config == nil {
		config = &MiningLoopConfig{}
	}
	if config.Tier1Threshold == 0 {
		config.Tier1Threshold = 300.0
	}
	if config.ReserveCredits == 0 {
		config.ReserveCredits = 50.0
	}
	if config.CargoFullThreshold == 0 {
		config.CargoFullThreshold = 0.97
	}
	if config.FuelLowThreshold == 0 {
		config.FuelLowThreshold = 0.1
	}
	if config.CaptainsLogInterval == 0 {
		config.CaptainsLogInterval = 2 * time.Minute
	}
	if config.MiningType == "" {
		config.MiningType = MiningTypeAsteroid
	}
	mtConfig := miningTypeConfigs[config.MiningType]

	// Initialize result
	state := client.GetState()
	result := &MiningLoopResult{
		StartingCredits: state.Credits,
	}

	// Captain's log ticker (if enabled)
	var logTicker *time.Ticker
	if config.AgentID != "" {
		logTicker = time.NewTicker(config.CaptainsLogInterval)
		defer logTicker.Stop()
	}

	// Main mining loop
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			result.EndingCredits = client.GetState().Credits
			result.StoppedReason = "context_cancelled"
			return result, nil
		default:
		}

		// Check captain's log ticker
		if logTicker != nil {
			select {
			case <-logTicker.C:
				// Optionally update captain's log here
				// For now, caller should handle this via OnRunComplete
			default:
			}
		}

		// Get current state
		state = client.GetState()

		// Check stop condition
		if config.StopCondition != nil && config.StopCondition(state) {
			result.EndingCredits = state.Credits
			result.StoppedReason = "stop_condition"
			return result, nil
		}

		result.RunsCompleted++
		runStartCredits := state.Credits

		logger.Printf("═══ Mining Run #%d ═══", result.RunsCompleted)
		logger.Printf("Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Cargo: %.1f/%.1f",
			state.Credits, state.Fuel, state.MaxFuel, state.Hull, state.MaxHull,
			state.Ship.CargoUsed, state.Ship.CargoCapacity)

		// IMPORTANT: Check fuel level BEFORE starting the run
		// If docked and fuel is low, refuel first to prevent getting stuck
		if state.Doc && state.Fuel < state.MaxFuel*0.2 {
			logger.Printf("⚠️  Low fuel before run (%.0f/%.0f = %.0f%%), refueling first...",
				state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100)
			if err := client.Refuel(ctx); err != nil {
				logger.Printf("Refuel error: %v", err)
			} else {
				time.Sleep(3 * time.Second)
				state = client.GetState()
				logger.Printf("✅ Refueled to %.0f/%.0f", state.Fuel, state.MaxFuel)
			}
		}

		// Get full system data to see POIs
		if len(state.System.POIs) == 0 {
			logger.Printf("Fetching system data...")
			if err := client.GetSystem(ctx); err != nil {
				logger.Printf("Failed to get system: %v", err)
			}
			state = client.GetState()
		}

		// Find mining POI and station in current system
		var miningPOI string
		var stationPOI string
		for _, poi := range state.System.POIs {
			if miningPOI == "" && isMiningPOI(poi.Type, mtConfig) {
				miningPOI = poi.ID
			}
			if poi.Type == "station" && stationPOI == "" {
				stationPOI = poi.ID
			}
		}

		if miningPOI == "" {
			result.EndingCredits = state.Credits
			result.StoppedReason = "no_mining_poi"
			return result, fmt.Errorf("no %s mining POI found in system %s", mtConfig.label, state.System.Name)
		}
		if stationPOI == "" {
			result.EndingCredits = state.Credits
			result.StoppedReason = "no_station"
			return result, fmt.Errorf("no station found in system %s", state.System.Name)
		}

		logger.Printf("📍 System: %s | Mining: %s | Station: %s", state.System.Name, miningPOI, stationPOI)

		// Step 1: Undock if docked
		if state.Doc {
			logger.Printf("📤 Undocking from station...")
			undockSuccess := false
			for retries := range 5 {
				if err := client.Undock(ctx); err != nil {
					if strings.Contains(err.Error(), "action pending") {
						logger.Printf("⏳ Waiting for pending action to complete before undock... (attempt %d/5)", retries+1)
						time.Sleep(SleepTick)
						continue
					}
					logger.Printf("Undock error: %v", err)
				}
				undockSuccess = true
				break
			}
			if !undockSuccess {
				logger.Printf("❌ Failed to undock after 5 attempts, skipping this run")
				time.Sleep(5 * time.Second)
				continue
			}
		}

		// Refresh state after undock to verify we're actually undocked
		time.Sleep(SleepTick)
		state = client.GetState()
		if state.Doc {
			logger.Printf("⚠️  Still docked after undock attempt, skipping this run")
			time.Sleep(5 * time.Second)
			continue
		}

		// Step 2: Travel to mining location (blocks until arrival)
		state = client.GetState()
		if state.CurrentPOI != miningPOI && !state.Traveling {
			logger.Printf("🚀 Traveling to mining location %s...", miningPOI)
			if _, err := client.Travel(ctx, miningPOI); err != nil {
				logger.Printf("Travel error: %v", err)
			}
		}

		// Step 3: Mine until cargo full or fuel low
		state = client.GetState()
		maxMiningAttempts := config.MaxMiningAttempts
		if maxMiningAttempts == 0 {
			// Calculate based on cargo capacity and equipment count
			numEquipment := countMiningEquipment(state, mtConfig)
			if numEquipment == 0 {
				numEquipment = 1 // Default to 1 if no equipment found
			}
			maxMiningAttempts = max(int(state.Ship.CargoCapacity/(5.0*float64(numEquipment))), 5)
		}

		numEquipment := countMiningEquipment(state, mtConfig)
		mineCount := 0
		logger.Printf("⛏️  Starting %s mining... (max %d attempts with %d equipment)",
			mtConfig.label, maxMiningAttempts, numEquipment)

		beltDepleted := false
		for {
			state = client.GetState()

			// Check if cargo is nearly full
			if state.Ship.CargoUsed >= state.Ship.CargoCapacity*config.CargoFullThreshold {
				logger.Printf("✓ Cargo nearly full (%.1f/%.1f), heading back",
					state.Ship.CargoUsed, state.Ship.CargoCapacity)
				break
			}

			// Check fuel
			if state.Fuel < state.MaxFuel*config.FuelLowThreshold {
				logger.Printf("⚠️  Low fuel (%.0f/%.0f = %.0f%%), heading back",
					state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100)
				break
			}

			// Mine
			if err := client.Mine(ctx); err != nil {
				errMsg := err.Error()
				switch {
				case errMsg == "must undock first - currently docked at station":
					break
				case strings.Contains(strings.ToLower(errMsg), "nothing to mine"):
					logger.Printf("⚠️  Belt depleted: %s", errMsg)
					beltDepleted = true
				default:
					// Log other errors but continue (might be rate limited)
					logger.Printf("⚠️  Mine error: %s", errMsg)
				}
				if beltDepleted || errMsg == "must undock first - currently docked at station" {
					break
				}
			} else {
				mineCount++
				if mineCount%3 == 0 { // Log every 3rd mine to reduce spam
					logger.Printf("⛏️  Mining... [%d/%d] (%.1f/%.1f cargo)",
						mineCount, maxMiningAttempts, state.Ship.CargoUsed, state.Ship.CargoCapacity)
				}
			}

			// Safety: max mining attempts per run
			if mineCount >= maxMiningAttempts {
				logger.Printf("✓ Reached max mining attempts (%d)", maxMiningAttempts)
				break
			}
		}

		// If belt is depleted, try to find another mining POI in the system
		if beltDepleted {
			logger.Printf("🔍 Resource depleted at %s, looking for another %s location...", miningPOI, mtConfig.label)
			altMiningPOI := ""
			for _, poi := range state.System.POIs {
				if isMiningPOI(poi.Type, mtConfig) && poi.ID != miningPOI {
					altMiningPOI = poi.ID
					break
				}
			}
			if altMiningPOI != "" {
				logger.Printf("🔄 Found alternative mining location: %s", altMiningPOI)
				miningPOI = altMiningPOI
				// Travel directly to the new belt instead of going back to station
				logger.Printf("🚀 Traveling to %s...", altMiningPOI)
				for retries := range 5 {
					if _, err := client.Travel(ctx, altMiningPOI); err != nil {
						if strings.Contains(err.Error(), "action pending") {
							logger.Printf("⏳ Waiting for pending action to complete before travel... (attempt %d/5)", retries+1)
							time.Sleep(SleepTick)
							continue
						}
						logger.Printf("Travel error: %v", err)
					}
					break
				}
				continue // Start mining at the new location
			}
			logger.Printf("⚠️  No alternative mining locations in system %s, returning to station", state.System.Name)
		}

		logger.Printf("✓ Mined %d times this run", mineCount)

		// Step 4: Travel back to station
		// Refresh system data to ensure we have latest station info
		if err := client.GetSystem(ctx); err != nil {
			logger.Printf("Failed to get system: %v", err)
		}
		state = client.GetState()
		stationPOI = ""
		for _, poi := range state.System.POIs {
			if poi.Type == "station" {
				stationPOI = poi.ID
				break
			}
		}

		if stationPOI == "" {
			result.EndingCredits = state.Credits
			result.StoppedReason = "no_station"
			return result, fmt.Errorf("no station found in system %s", state.System.Name)
		}

		if state.CurrentPOI != stationPOI && !state.Traveling {
			logger.Printf("🚀 Returning to station %s...", stationPOI)
			for retries := range 5 {
				if _, err := client.Travel(ctx, stationPOI); err != nil {
					if strings.Contains(err.Error(), "action pending") {
						logger.Printf("⏳ Waiting for pending action to complete before travel... (attempt %d/5)", retries+1)
						time.Sleep(SleepTick)
						continue
					}
					logger.Printf("Travel error: %v", err)
				}
				break
			}
		}

		// Step 5: Dock at station
		logger.Printf("📥 Attempting to dock at station...")
		for retries := range 5 {
			if err := client.Dock(ctx); err != nil {
				if strings.Contains(err.Error(), "action pending") {
					logger.Printf("⏳ Waiting for pending action to complete before docking... (attempt %d/5)", retries+1)
					time.Sleep(SleepTick)
					continue
				}
				if err.Error() != "Already docked (success)" {
					logger.Printf("Dock error: %v", err)
				}
			}
			break
		}

		// Step 6: Handle cargo with station actions
		state = client.GetState()
		creditsBefore := state.Credits
		if !state.Doc {
			logger.Printf("⚠️  Not docked! Skipping station actions. Current POI: %s", state.CurrentPOI)
		} else if len(state.Ship.Cargo) > 0 {
			// Determine which station action strategy to use
			stationAction := config.OnStationActions
			if stationAction == nil {
				// Default to selling all if no strategy specified
				if config.UseBulkSell {
					stationAction = StationActionSellAll
				} else {
					// Legacy behavior
					stationAction = func(c GameClient, l *log.Logger, cx context.Context) error {
						if err := c.SellAllBulk(cx, nil); err != nil {
							return err
						}
						time.Sleep(5 * time.Second)
						return nil
					}
				}
			}

			// Execute the station action strategy
			if err := stationAction(client, logger, ctx); err != nil {
				logger.Printf("Station action error: %v", err)
			} else {
				// Calculate credits earned (if we sold items)
				state = client.GetState()
				creditsEarned := state.Credits - creditsBefore
				result.TotalCreditsEarned += creditsEarned
				if creditsEarned > 0 {
					logger.Printf("💰 Credits earned: %.2f (Total: %.2f)",
						creditsEarned, result.TotalCreditsEarned)
				}
			}
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

		// Step 8: Repair if needed
		state = client.GetState()
		if state.Doc && state.Hull < state.MaxHull*0.9 {
			logger.Printf("🔧 Repairing hull...")
			if err := client.Repair(ctx); err != nil {
				logger.Printf("Repair error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 9: Check for upgrades
		state = client.GetState()
		shouldCheckUpgrades := false

		if config.UpgradeCheckInterval > 0 {
			// Check every N runs
			if result.RunsCompleted%config.UpgradeCheckInterval == 0 {
				shouldCheckUpgrades = true
			}
		} else {
			// Check when we have enough credits
			if state.Credits >= config.Tier1Threshold {
				shouldCheckUpgrades = true
			}
		}

		if shouldCheckUpgrades && config.OnUpgradeCheck != nil {
			time.Sleep(3 * time.Second) // Wait to avoid rate limiting
			logger.Printf("💰 Checking for upgrades...")
			config.OnUpgradeCheck()
		}

		// Step 10: Run completion callback
		state = client.GetState()
		runCreditsEarned := state.Credits - runStartCredits

		logger.Printf("═══ Run #%d Complete ═══", result.RunsCompleted)
		logger.Printf("Current Credits: %.2f (started with %.2f, earned %.2f total)",
			state.Credits, result.StartingCredits, result.TotalCreditsEarned)

		if config.OnRunComplete != nil {
			config.OnRunComplete(result.RunsCompleted, runCreditsEarned, state.Credits)
		}

		logger.Printf("Next run in 5 seconds...\n")
		time.Sleep(5 * time.Second)
	}
}

// isMiningPOI returns true if the POI type matches the given mining type config.
func isMiningPOI(poiType string, cfg miningTypeConfig) bool {
	for _, t := range cfg.poiTypes {
		if poiType == t {
			return true
		}
	}
	return false
}

// countMiningEquipment counts equipment modules matching the mining type config.
func countMiningEquipment(state *State, cfg miningTypeConfig) int {
	count := 0
	for _, moduleID := range state.Ship.Modules {
		moduleDef, ok := state.ModuleDefinitions[moduleID]
		if !ok {
			continue
		}
		nameLower := strings.ToLower(moduleDef.Name)
		typeLower := strings.ToLower(moduleDef.Type)
		for _, prefix := range cfg.equipPrefixes {
			if strings.HasPrefix(nameLower, prefix) {
				count++
				goto next
			}
		}
		for _, prefix := range cfg.equipTypePrefixes {
			if strings.HasPrefix(typeLower, prefix) {
				count++
				goto next
			}
		}
	next:
	}
	return count
}
