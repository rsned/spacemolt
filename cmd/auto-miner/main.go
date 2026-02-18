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

func updateCaptainsLog(agentID string, client *game.Client, miningRuns int, creditsEarned float64) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("Mining runs completed: %d", miningRuns))
	notes = append(notes, fmt.Sprintf("Credits earned this run: %.2f", creditsEarned))
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
	// Configure the shared mining loop
	config := &game.MiningLoopConfig{
		AgentID:              agentID,
		UpgradeCheckInterval: 500, // Check every 5 runs
		Tier1Threshold:       TIER1_THRESHOLD,
		ReserveCredits:       RESERVE_CREDITS,
		UseBulkSell:          true, // Use bulk sell for better performance
		OnUpgradeCheck: func() bool {
			//attemptUpgrades(client, logger, ctx)
			return false // Return value not used for continuous mining
		},
		OnRunComplete: func(runNum int, creditsEarned float64, totalCredits float64) {
			state := client.GetState()
			logger.Printf("Ship: %s | Modules: %d", state.Ship.Name, len(state.Ship.Modules))

			// Update captain's log after each run
			updateCaptainsLog(agentID, client, runNum, creditsEarned)
		},
	}

	// Run the shared mining loop
	result, err := game.MiningLoop(client, logger, ctx, config)
	if err != nil {
		return err
	}

	// Log final results
	logger.Printf("Mining loop stopped: %s", result.StoppedReason)
	logger.Printf("Total runs: %d, Total credits earned: %.2f",
		result.RunsCompleted, result.TotalCreditsEarned)

	return nil
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
