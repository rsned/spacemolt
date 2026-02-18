package game

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// StationActionStrategy defines what to do with cargo when docked at a station
type StationActionStrategy func(client *Client, logger *log.Logger, ctx context.Context) error

// StationActionSellAll simply sells all cargo without any crafting
func StationActionSellAll(client *Client, logger *log.Logger, ctx context.Context) error {
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
// NOTE: Crafting is not yet implemented, currently just sells all cargo
// TODO: Implement actual crafting logic once Craft method is available
func StationActionCraftAndSell(client *Client, logger *log.Logger, ctx context.Context) error {
	state := client.GetState()
	if len(state.Ship.Cargo) == 0 {
		logger.Printf("📦 Cargo is empty, nothing to craft or sell")
		return nil
	}

	// TODO: Implement crafting logic
	// For now, just sell everything
	logger.Printf("🔨 Crafting not yet implemented, selling all cargo...")

	// Sell everything
	logger.Printf("💰 Selling all cargo (%d items)...", len(state.Ship.Cargo))
	if err := client.SellAllBulk(ctx, nil); err != nil {
		return fmt.Errorf("sell failed: %w", err)
	}

	time.Sleep(5 * time.Second)
	logger.Printf("✅ Sold all cargo!")
	return nil
}

// StationActionCraftAndDeposit crafts items from cargo resources, then deposits everything
// NOTE: Crafting is not yet implemented, currently just deposits all cargo
// TODO: Implement actual crafting logic once Craft method is available
func StationActionCraftAndDeposit(client *Client, logger *log.Logger, ctx context.Context) error {
	state := client.GetState()
	if len(state.Ship.Cargo) == 0 {
		logger.Printf("📦 Cargo is empty, nothing to deposit")
		return nil
	}

	// TODO: Implement crafting logic
	// For now, just deposit everything without crafting
	logger.Printf("🔨 Crafting not yet implemented, depositing all cargo as-is...")

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
func MiningLoop(client *Client, logger *log.Logger, ctx context.Context, config *MiningLoopConfig) (*MiningLoopResult, error) {
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

		// Get full system data to see POIs
		if len(state.System.POIs) == 0 {
			logger.Printf("Fetching system data...")
			if err := client.GetSystem(ctx); err != nil {
				logger.Printf("Failed to get system: %v", err)
			}
			time.Sleep(2 * time.Second)
			state = client.GetState()
		}

		// Find mining POI and station in current system
		var miningPOI string
		var stationPOI string
		for _, poi := range state.System.POIs {
			if (poi.Type == "asteroid_belt" || poi.Type == "asteroid_field") && miningPOI == "" {
				miningPOI = poi.ID
			}
			if poi.Type == "station" && stationPOI == "" {
				stationPOI = poi.ID
			}
		}

		if miningPOI == "" {
			result.EndingCredits = state.Credits
			result.StoppedReason = "no_mining_poi"
			return result, fmt.Errorf("no mining POI found in system %s", state.System.Name)
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
			if err := client.Undock(ctx); err != nil {
				logger.Printf("Undock error: %v", err)
			}
			time.Sleep(12 * time.Second)
		}

		// Step 2: Travel to mining location
		state = client.GetState()
		if state.CurrentPOI != miningPOI && !state.Traveling {
			logger.Printf("🚀 Traveling to mining location %s...", miningPOI)
			if err := client.Travel(ctx, miningPOI); err != nil {
				logger.Printf("Travel error: %v", err)
			}
			time.Sleep(20 * time.Second)
		}

		// Step 3: Mine until cargo full or fuel low
		state = client.GetState()
		maxMiningAttempts := config.MaxMiningAttempts
		if maxMiningAttempts == 0 {
			// Calculate based on cargo capacity and mining laser count
			numMiningLasers := countMiningLasers(state)
			if numMiningLasers == 0 {
				numMiningLasers = 1 // Default to 1 if no lasers found
			}
			maxMiningAttempts = max(int(state.Ship.CargoCapacity/(5.0*float64(numMiningLasers))), 5)
		}

		numMiningLasers := countMiningLasers(state)
		mineCount := 0
		logger.Printf("⛏️  Starting mining operations... (max %d attempts with %d laser(s))",
			maxMiningAttempts, numMiningLasers)

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
				if err.Error() == "must undock first - currently docked at station" {
					break
				}
				// Silently continue on other errors (might be rate limited)
			} else {
				mineCount++
				if mineCount%3 == 0 { // Log every 3rd mine to reduce spam
					logger.Printf("⛏️  Mining... [%d/%d] (%.1f/%.1f cargo)",
						mineCount, maxMiningAttempts, state.Ship.CargoUsed, state.Ship.CargoCapacity)
				}
			}

			time.Sleep(11 * time.Second)

			// Safety: max mining attempts per run
			if mineCount >= maxMiningAttempts {
				logger.Printf("✓ Reached max mining attempts (%d)", maxMiningAttempts)
				break
			}
		}

		logger.Printf("✓ Mined %d times this run", mineCount)

		// Step 4: Travel back to station
		// Refresh system data to ensure we have latest station info
		if err := client.GetSystem(ctx); err != nil {
			logger.Printf("Failed to get system: %v", err)
		}
		time.Sleep(2 * time.Second)

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
			if err := client.Travel(ctx, stationPOI); err != nil {
				logger.Printf("Travel error: %v", err)
			}
			time.Sleep(20 * time.Second)
		}

		// Step 5: Dock at station
		logger.Printf("📥 Attempting to dock at station...")
		if err := client.Dock(ctx); err != nil {
			if err.Error() != "Already docked (success)" {
				logger.Printf("Dock error: %v", err)
			}
		}
		time.Sleep(15 * time.Second)

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
					stationAction = func(c *Client, l *log.Logger, cx context.Context) error {
						if err := c.SellAll(cx); err != nil {
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

// countMiningLasers counts total mining lasers installed
func countMiningLasers(state *State) int {
	count := 0
	for _, module := range state.Ship.Modules {
		if strings.HasPrefix(module, "mining_laser_") || module == "advanced_mining_laser" {
			count++
		}
	}
	return count
}
