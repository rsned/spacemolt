package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Upgrade thresholds and priorities
const (
	// Credit threshold for basic equipment (mining lasers, shields, weapons)
	TIER1_THRESHOLD = 300.0 // Mining laser (faster mining!)

	// Reserve credits (never spend below this)
	RESERVE_CREDITS = 50.0
)

func attemptUpgrades(client *game.Client, logger *log.Logger, ctx context.Context) {
	state := client.GetState()
	credits := state.Credits

	// Don't spend reserve credits
	availableCredits := credits - RESERVE_CREDITS
	// 150 credits is lowest cost of mining_laser_1
	if availableCredits < 150 {
		return // Not enough to buy anything meaningful
	}

	// First, try to install any equipment already in cargo and sell extras
	// Uses library method from pkg/game/upgrades.go
	game.TryInstallAndSellExtras(client, logger, ctx)

	// Refresh state after selling extras
	time.Sleep(2 * time.Second)
	state = client.GetState()
	availableCredits = state.Credits - RESERVE_CREDITS

	// Check if we can upgrade ship using library progression
	canUpgradeShip := game.CanUpgradeAnyShip(state.Ship.ClassID, availableCredits, game.MiningProgression.Tiers)

	// Ensure cargo space is available for purchases (except ship upgrades)
	cargoUsed := state.Ship.CargoUsed
	cargoCapacity := state.Ship.CargoCapacity
	if cargoUsed >= cargoCapacity*0.5 && !canUpgradeShip {
		logger.Printf("⚠️  Cargo too full (%.1f/%.1f) - skipping upgrades until cargo is sold", cargoUsed, cargoCapacity)
		return
	}

	logger.Printf("💰 Checking for upgrades... (%.2f credits available, %.1f/%.1f cargo space)", availableCredits, cargoUsed, cargoCapacity)

	// Attempt ship upgrades using library method
	// Check all mining progression tiers in order
	for _, tier := range game.MiningProgression.Tiers {
		if game.PerformShipUpgrade(client, logger, ctx, tier, availableCredits) {
			logger.Printf("✅ Ship upgrade complete!")
			return // Ship upgraded, done for this cycle
		}
	}

	// If no ship upgrade was performed, check for basic equipment upgrades
	// Refresh state and get market listings
	time.Sleep(2 * time.Second)
	state = client.GetState()
	availableCredits = state.Credits - RESERVE_CREDITS

	if err := client.GetListings(ctx); err != nil {
		logger.Printf("Could not get listings: %v", err)
		return
	}

	time.Sleep(2 * time.Second) // Wait for listings response
	listings := client.GetMarketListings()

	if len(listings) == 0 {
		logger.Printf("No market listings available")
		return
	}

	logger.Printf("Found %d listings at market", len(listings))

	// Buy additional mining lasers if we have room
	maxLasers := game.GetShipClassMaxSlots(state.Ship.ClassID)
	miningLasersInstalled := game.CountModulesInstalled(state, "mining_laser_1")
	miningLasersInCargo := game.CountModulesInCargo(state, "mining_laser_1")
	totalMiningLasers := miningLasersInstalled + miningLasersInCargo

	logger.Printf("⛏️  Mining Laser Status: %d installed, %d in cargo (max: %d)",
		miningLasersInstalled, miningLasersInCargo, maxLasers)

	if totalMiningLasers < maxLasers && availableCredits >= TIER1_THRESHOLD {
		for _, listing := range listings {
			if listing.Type == "sell" && listing.ItemID == "mining_laser_1" &&
				listing.PricePerUnit <= availableCredits && listing.PricePerUnit <= 1000 {

				needed := maxLasers - totalMiningLasers
				if needed > 0 {
					logger.Printf("⛏️  Buying %d x mining_laser_1 for %.2f credits each", needed, listing.PricePerUnit)
					if err := client.Buy(ctx, "mining_laser_1", float64(needed)); err != nil {
						logger.Printf("Failed to buy mining laser: %v", err)
					} else {
						logger.Printf("✅ Purchased %d mining laser(s)! Installing...", needed)
						time.Sleep(2 * time.Second)

						// Install each mining laser from cargo
						installed := 0
						for i := range needed {
							if err := client.Install(ctx, "mining_laser_1"); err != nil {
								logger.Printf("Failed to install mining laser #%d: %v", i+1, err)
							} else {
								installed++
								logger.Printf("✅ Mining laser #%d installed!", installed)
							}
							time.Sleep(10 * time.Second)
						}

						if installed > 0 {
							logger.Printf("✅ %d MINING LASER(S) INSTALLED! Mining speed increased!", installed)
						}
						return
					}
				}
			}
		}
	}

	// Buy shields for protection if available and affordable
	hasShield := false
	for _, module := range state.Ship.Modules {
		if len(module) >= 7 && module[:7] == "shield_" {
			hasShield = true
			break
		}
	}

	if !hasShield && availableCredits >= 1500 {
		for _, listing := range listings {
			if listing.Type == "sell" && listing.ItemType == "shield" &&
				listing.PricePerUnit <= availableCredits && listing.PricePerUnit <= 2000 {

				logger.Printf("🛡️  Buying shield: %s for %.2f credits", listing.ItemID, listing.PricePerUnit)
				if err := client.Buy(ctx, listing.ItemID, 1); err != nil {
					logger.Printf("Failed to buy shield: %v", err)
				} else {
					logger.Printf("✅ Purchased shield! Installing...")
					time.Sleep(2 * time.Second)
					if err := client.Install(ctx, listing.ItemID); err != nil {
						logger.Printf("Failed to install shield: %v (in cargo)", err)
					} else {
						logger.Printf("✅ SHIELD INSTALLED!")
					}
					return
				}
			}
		}
	}
}

func updateCaptainsLog(agentID string, client *game.Client, miningRuns int, totalCreditsEarned float64) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("Mining runs completed: %d", miningRuns))
	notes = append(notes, fmt.Sprintf("Total credits earned: %.2f", totalCreditsEarned))
	notes = append(notes, fmt.Sprintf("Current credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Ship: %s (%d modules)", state.Ship.Name, len(state.Ship.Modules)))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	notes = append(notes, fmt.Sprintf("Cargo: %.1f/%.1f", state.Ship.CargoUsed, state.Ship.CargoCapacity))

	// Count mining lasers
	numLasers := game.CountModulesInstalled(state, "mining_laser_1") +
		game.CountModulesInstalled(state, "mining_laser_2") +
		game.CountModulesInstalled(state, "mining_laser_3") +
		game.CountModulesInstalled(state, "advanced_mining_laser")
	notes = append(notes, fmt.Sprintf("Mining lasers: %d", numLasers))

	currentGoal := fmt.Sprintf("Autonomous mining operations - collecting resources and upgrading ship")
	if state.Doc {
		currentGoal = "Docked at station - selling cargo, refueling, and checking for upgrades"
	} else if state.Traveling {
		currentGoal = fmt.Sprintf("Traveling to %s", state.TravelProgress.Destination)
	} else if !state.Doc && state.Ship.CargoUsed > state.Ship.CargoCapacity*0.5 {
		currentGoal = "Mining operations in progress - cargo filling up"
	}

	entry := &game.AgentLog{
		AgentName:   state.Player.Username,
		CurrentGoal: currentGoal,
		Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
		Notes:       notes,
	}

	game.WriteCaptainsLog(agentID, entry)
}

func miningLoop(agentID string, client *game.Client, logger *log.Logger, ctx context.Context) error {
	miningRuns := 0
	totalCreditsEarned := 0.0
	startingCredits := client.GetState().Credits

	// Captain's log ticker - update every 2 minutes
	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	// Initial captain's log entry
	updateCaptainsLog(agentID, client, miningRuns, totalCreditsEarned)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-logTicker.C:
			updateCaptainsLog(agentID, client, miningRuns, totalCreditsEarned)
		default:
		}

		state := client.GetState()
		miningRuns++
		logger.Printf("═══ Mining Run #%d ═══", miningRuns)
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

		// Find a mining POI and station in the current system
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
			logger.Printf("⚠️  No mining POI found in current system %s!", state.System.Name)
			return fmt.Errorf("no mining location in system %s", state.System.Name)
		}
		if stationPOI == "" {
			logger.Printf("⚠️  No station found in current system %s!", state.System.Name)
			return fmt.Errorf("no station in system %s", state.System.Name)
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

		// Step 2: Travel to asteroid belt
		state = client.GetState()
		if state.CurrentPOI != miningPOI && !state.Traveling {
			logger.Printf("🚀 Traveling to mining location %s...", miningPOI)
			if err := client.Travel(ctx, miningPOI); err != nil {
				logger.Printf("Travel error: %v", err)
			}
			time.Sleep(20 * time.Second)
		}

		// Step 3: Mine repeatedly until cargo full or fuel low
		// Calculate max mining attempts based on cargo capacity and number of mining lasers
		numMiningLasers := game.CountModulesInstalled(state, "mining_laser_1") +
			game.CountModulesInstalled(state, "mining_laser_2") +
			game.CountModulesInstalled(state, "mining_laser_3") +
			game.CountModulesInstalled(state, "advanced_mining_laser")
		if numMiningLasers == 0 {
			numMiningLasers = 1 // Default to 1 if no lasers found (shouldn't happen)
		}
		maxMiningAttempts := max(int(state.Ship.CargoCapacity/(5.0*float64(numMiningLasers))), 5)

		mineCount := 0
		logger.Printf("⛏️  Starting mining operations... (max %d attempts with %d laser(s))", maxMiningAttempts, numMiningLasers)
		for {
			state = client.GetState()

			// Check if cargo is nearly full
			if state.Ship.CargoUsed >= state.Ship.CargoCapacity*0.97 {
				logger.Printf("✓ Cargo nearly full (%.1f/%.1f), heading back",
					state.Ship.CargoUsed, state.Ship.CargoCapacity)
				break
			}

			// Check fuel (less than 10% remaining)
			if state.Fuel < state.MaxFuel*0.1 {
				logger.Printf("⚠️  Low fuel (%.0f/%.0f = %.0f%%), heading back", state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100)
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

			// Safety: max mining attempts per run (based on cargo capacity and laser count)
			if mineCount >= maxMiningAttempts {
				logger.Printf("✓ Reached max mining attempts (%d)", maxMiningAttempts)
				break
			}
		}

		logger.Printf("✓ Mined %d times this run", mineCount)

		// Step 4: Travel back to station
		// Get fresh system data to find station
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
			logger.Printf("⚠️  No station found in current system!")
			return fmt.Errorf("no station in system %s", state.System.Name)
		}

		if state.CurrentPOI != stationPOI && !state.Traveling {
			logger.Printf("🚀 Returning to station %s...", stationPOI)
			if err := client.Travel(ctx, stationPOI); err != nil {
				logger.Printf("Travel error: %v", err)
			}
			time.Sleep(20 * time.Second)
		}

		// Step 5: Dock (always try when at station)
		logger.Printf("📥 Attempting to dock at station...")
		if err := client.Dock(ctx); err != nil {
			// Might already be docked, that's okay
			if err.Error() != "Already docked (success)" {
				logger.Printf("Dock error: %v", err)
			}
		}
		time.Sleep(15 * time.Second) // Wait for docking to complete

		// Verify we're docked by checking the actual state after the wait
		state = client.GetState()
		logger.Printf("Dock status: docked=%v, POI=%s", state.Doc, state.CurrentPOI)

		// Step 6: Sell all cargo (only if docked)
		state = client.GetState()
		creditsBefore := state.Credits
		if !state.Doc {
			logger.Printf("⚠️  Not docked! Skipping sell. Current POI: %s", state.CurrentPOI)
		} else if len(state.Ship.Cargo) > 0 {
			logger.Printf("💰 Selling %d different items in bulk...", len(state.Ship.Cargo))

			// List what we're selling
			for _, item := range state.Ship.Cargo {
				logger.Printf("   - %s x%.0f", item.ItemID, item.Quantity)
			}

			if err := client.SellAllBulk(ctx, nil); err != nil {
				logger.Printf("Sell error: %v", err)
			} else {
				// Wait longer for state update
				time.Sleep(5 * time.Second)
				state = client.GetState()
				creditsEarned := state.Credits - creditsBefore
				totalCreditsEarned += creditsEarned
				if creditsEarned > 0 {
					logger.Printf("✅ Sold cargo in bulk! Earned %.2f credits (Total: %.2f)",
						creditsEarned, totalCreditsEarned)
				} else {
					// State might not have updated yet, but sell likely succeeded
					logger.Printf("✓ Bulk sell command completed (check next state update for credits)")
				}
			}
		}

		// Step 7: Refuel if needed (only if docked)
		state = client.GetState()
		if state.Doc && state.Fuel < state.MaxFuel*0.8 {
			logger.Printf("⛽ Refueling...")
			if err := client.Refuel(ctx); err != nil {
				logger.Printf("Refuel error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 8: Repair if needed (only if docked)
		state = client.GetState()
		if state.Doc && state.Hull < state.MaxHull*0.9 {
			logger.Printf("🔧 Repairing hull...")
			if err := client.Repair(ctx); err != nil {
				logger.Printf("Repair error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 9: Check for upgrades every 5 runs or when wealthy
		state = client.GetState()
		if miningRuns%5 == 0 || state.Credits >= TIER1_THRESHOLD {
			time.Sleep(3 * time.Second) // Wait to avoid rate limiting
			attemptUpgrades(client, logger, ctx)
		}

		// Status summary and captain's log update
		state = client.GetState()
		logger.Printf("═══ Run #%d Complete ═══", miningRuns)
		logger.Printf("Current Credits: %.2f (started with %.2f, earned %.2f total)",
			state.Credits, startingCredits, totalCreditsEarned)
		logger.Printf("Ship: %s | Modules: %d", state.Ship.Name, len(state.Ship.Modules))
		logger.Printf("Next run in 5 seconds...\n")

		// Update captain's log after each run
		updateCaptainsLog(agentID, client, miningRuns, totalCreditsEarned)

		time.Sleep(5 * time.Second)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: auto-miner <agent-id>")
		fmt.Println("Example: auto-miner pirate-1")
		fmt.Println("Example: auto-miner miner-1")
		fmt.Println("Example: auto-miner craftsman-1")
		os.Exit(1)
	}

	agentID := os.Args[1]

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	// Check captain's log for previous mission
	previousLog, err := game.ReadLatestCaptainsLog(agentID)
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

	// Create context for lifecycle management
	ctx := context.Background()

	// Initialize game client using shared library function
	// This handles: credential loading, client creation, connection, and login
	client, creds, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// Get initial state
	state := client.GetState()
	logger.Printf("🏴‍☠️ Starting autonomous mining & upgrade bot...")
	logger.Printf("Agent: %s | Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Username, creds.Empire, state.Credits, state.Ship.Name,
		state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Start autonomous mining loop with upgrades
	logger.Printf("Starting autonomous mining + upgrade loop...")
	logger.Printf("Will automatically:")
	logger.Printf("  ⛏️  Mine resources until cargo full")
	logger.Printf("  💰 Sell all cargo for credits")
	logger.Printf("  🚀 Upgrade ships progressively using MiningProgression tiers")
	logger.Printf("")

	if err := miningLoop(agentID, client, logger, ctx); err != nil {
		log.Fatalf("Mining loop error: %v", err)
	}
}
