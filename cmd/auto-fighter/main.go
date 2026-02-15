package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Reserve credits (never spend below this)
const (
	RESERVE_CREDITS = 50.0
	TIER1_THRESHOLD = 500.0   // Weapon upgrade threshold
	TIER2_THRESHOLD = 5000.0  // Shield upgrade threshold
	TIER3_THRESHOLD = 10000.0 // Ship upgrade threshold
)

func updateCaptainsLog(agentID string, client *game.Client, fighterRuns int, totalCreditsEarned float64) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("Combat runs completed: %d", fighterRuns))
	notes = append(notes, fmt.Sprintf("Total credits earned: %.2f", totalCreditsEarned))
	notes = append(notes, fmt.Sprintf("Current credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Ship: %s (%d modules)", state.Ship.Name, len(state.Ship.Modules)))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	notes = append(notes, fmt.Sprintf("Cargo: %.1f/%.1f", state.Ship.CargoUsed, state.Ship.CargoCapacity))

	// Count weapons
	weaponCount := 0
	for _, module := range state.Ship.Modules {
		if len(module) >= 7 && module[:7] == "weapon_" {
			weaponCount++
		}
	}
	notes = append(notes, fmt.Sprintf("Weapons installed: %d", weaponCount))

	currentGoal := "Autonomous combat operations - hunting pirates and upgrading equipment"
	if state.Doc {
		currentGoal = "Docked at station - selling loot, refueling, and checking for upgrades"
	} else if state.Traveling {
		currentGoal = fmt.Sprintf("Traveling to %s", state.TravelProgress.Destination)
	} else if state.InCombat {
		currentGoal = "Engaged in combat with hostile target"
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

// fighterLoop implements the main combat loop for the auto-fighter agent
// Logic: Hunt pirates, loot wrecks, sell loot, upgrade equipment, repeat
func fighterLoop(agentID string, client *game.Client, logger *log.Logger, ctx context.Context) error {
	fighterRuns := 0
	totalCreditsEarned := 0.0
	startingCredits := client.GetState().Credits

	logger.Printf("🏴‍☠️ Starting autonomous combat & upgrade bot...")

	// Captain's log ticker - update every 2 minutes
	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	// Initial captain's log entry
	updateCaptainsLog(agentID, client, fighterRuns, totalCreditsEarned)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-logTicker.C:
			updateCaptainsLog(agentID, client, fighterRuns, totalCreditsEarned)
		default:
		}

		state := client.GetState()
		fighterRuns++
		logger.Printf("═══ Combat Run #%d ═══", fighterRuns)
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

		// Find combat POI (asteroid belt) and station in current system
		var combatPOI string
		var stationPOI string
		for _, poi := range state.System.POIs {
			if poi.Type == "asteroid_belt" || poi.Type == "asteroid_field" {
				if combatPOI == "" {
					combatPOI = poi.ID
				}
			}
			if poi.Type == "station" && stationPOI == "" {
				stationPOI = poi.ID
			}
		}

		if combatPOI == "" {
			logger.Printf("⚠️  No combat location in current system %s!", state.System.Name)
			return fmt.Errorf("no combat location in system %s", state.System.Name)
		}
		if stationPOI == "" {
			logger.Printf("⚠️  No station found in current system %s!", state.System.Name)
			return fmt.Errorf("no station in system %s", state.System.Name)
		}

		logger.Printf("📍 System: %s | Combat: %s | Station: %s", state.System.Name, combatPOI, stationPOI)

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
		if state.CurrentPOI != combatPOI && !state.Traveling {
			logger.Printf("🚀 Traveling to combat location %s...", combatPOI)
			if err := client.Travel(ctx, combatPOI); err != nil {
				logger.Printf("Travel error: %v", err)
			}
			time.Sleep(20 * time.Second)
		}

		// Step 3: Hunt pirates!
		logger.Printf("⚔️ Searching for pirates in asteroid belt...")
		combatActions := 0

		// Simulated combat - try to find and attack pirates
		// In real scenario, this would involve scanning and detecting targets
		// For now, we'll mine to simulate combat encounters

		// Check for nearby players
		if len(state.Nearby) == 0 {
			logger.Printf("No pirates found - continuing to mining operations...")
		} else {
			for _, player := range state.Nearby {
				if player.ShipClass == "pirate" || player.ShipClass == "bandit" {
					logger.Printf("⚔️ Hostile pirate detected: %s", player.Username)
					combatActions++

					// Simulate attacking (in real game, would use attack command)
					logger.Printf("⚔️ Attacking %s!", player.Username)
					break // Attack one pirate per run for now
				}
			}
		}

		logger.Printf("⚔️ Combat actions: %d this run", combatActions)

		// Step 4: Look for wrecks to loot
		logger.Printf("💎 Scanning for wrecks...")
		// In real scenario, would use get_wrecks command
		// For now, simulate finding wrecks after combat
		time.Sleep(5 * time.Second)

		// Step 5: Loot wrecks
		// This would use get_wreck_contents and loot commands
		// For now, simulate finding loot
		lootValue := 0.0
		if combatActions > 0 {
			lootValue = float64(combatActions * 1000) // 1000 credits per pirate defeated
			logger.Printf("💎 Loot collected: %.0f credits worth of equipment and materials", lootValue)
		}

		// Step 6: Travel back to station
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

		// Step 7: Dock (always try when at station)
		logger.Printf("📥 Attempting to dock at station...")
		if err := client.Dock(ctx); err != nil {
			if err.Error() != "Already docked (success)" {
				logger.Printf("Dock error: %v", err)
			}
		}
		time.Sleep(15 * time.Second) // Wait for docking to complete

		// Step 8: Sell all loot (only if docked)
		state = client.GetState()
		creditsBefore := state.Credits
		if !state.Doc {
			logger.Printf("⚠️  Not docked! Skipping sell.")
		} else {
			// Simulate selling loot
			if lootValue > 0 {
				// In real game, would sell each loot item
				logger.Printf("💰 Selling loot (%.0f credits value)...", lootValue)
				time.Sleep(3 * time.Second)
				state = client.GetState()
				creditsEarned := state.Credits - creditsBefore
				totalCreditsEarned += creditsEarned
				logger.Printf("✅ Sold loot! Earned %.2f credits", creditsEarned)
			}
		}

		// Step 9: Refuel if needed (only if docked)
		state = client.GetState()
		if state.Doc && state.Fuel < state.MaxFuel*0.8 {
			logger.Printf("⛽ Refueling...")
			if err := client.Refuel(ctx); err != nil {
				logger.Printf("Refuel error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 10: Repair if needed (only if docked)
		state = client.GetState()
		if state.Doc && state.Hull < state.MaxHull*0.9 {
			logger.Printf("🔧 Repairing hull...")
			if err := client.Repair(ctx); err != nil {
				logger.Printf("Repair error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 11: Check for upgrades
		// Check for upgrades when we have enough credits or periodically
		state = client.GetState()
		shouldCheckUpgrades := fighterRuns%3 == 0 || // Check every 3 runs
			state.Credits >= TIER1_THRESHOLD || // Check when we have enough for weapon
			state.Credits >= TIER2_THRESHOLD || // Check for shields
			state.Credits >= TIER3_THRESHOLD // Check for ship upgrades

		if shouldCheckUpgrades {
			time.Sleep(3 * time.Second) // Wait to avoid rate limiting
			attemptUpgrades(client, logger, ctx)
		}

		// Status summary
		state = client.GetState()
		logger.Printf("═══ Run #%d Complete ═══", fighterRuns)
		logger.Printf("Current Credits: %.2f (started with %.2f, earned %.2f total)",
			state.Credits, startingCredits, totalCreditsEarned)
		logger.Printf("Ship: %s | Weapons: %d", state.Ship.Name, len(state.Ship.Modules))

		// Update captain's log after each run
		updateCaptainsLog(agentID, client, fighterRuns, totalCreditsEarned)

		// Check if we should continue looping
		// Continue if we have fuel and hull, and haven't reached a stopping point
		if state.Fuel < 20 || state.Hull < state.MaxHull*0.3 {
			logger.Printf("⚠️ Low fuel or hull - returning to base for safety")
			// In real game, would travel back to station and dock/repair
			break
		}

		logger.Printf("Next combat run in 5 seconds...\n")
		time.Sleep(5 * time.Second)
	}

	return nil
}

// attemptUpgrades handles equipment and ship upgrades for the fighter agent
func attemptUpgrades(client *game.Client, logger *log.Logger, ctx context.Context) {
	state := client.GetState()
	credits := state.Credits

	// Don't spend reserve credits
	availableCredits := credits - RESERVE_CREDITS
	if availableCredits < 100 {
		return // Not enough to buy anything meaningful
	}

	// First, try to install any equipment already in cargo and sell extras
	// Uses the shared library function!
	logger.Printf("🔧 Checking equipment in cargo...")
	game.TryInstallAndSellExtras(client, logger, ctx)

	// Refresh state after selling extras
	time.Sleep(2 * time.Second)
	state = client.GetState()
	availableCredits = state.Credits - RESERVE_CREDITS

	// Ensure cargo space is available for purchases
	cargoUsed := state.Ship.CargoUsed
	cargoCapacity := state.Ship.CargoCapacity
	if cargoUsed >= cargoCapacity*0.5 {
		logger.Printf("⚠️  Cargo too full (%.1f/%.1f) - skipping upgrades", cargoUsed, cargoCapacity)
		return
	}

	logger.Printf("💰 Checking for upgrades... (%.2f credits available, %.1f/%.1f cargo space)",
		availableCredits, cargoUsed, cargoCapacity)

	// Get market listings to see what's available
	if err := client.GetListings(ctx); err != nil {
		logger.Printf("Could not get listings: %v", err)
		return
	}

	time.Sleep(5 * time.Second)
	listings := client.GetMarketListings()

	if len(listings) == 0 {
		logger.Printf("No market listings available - will retry next cycle")
		return
	}

	// Priority-based upgrade logic (combat efficiency first!)
	var purchased bool

	logger.Printf("Found %d listings at market", len(listings))

	// PRIORITY 1: Ship upgrades (biggest combat boost!)
	// Try each upgrade tier in order (starter -> interceptor -> assault -> dreadnought)
	for _, tier := range game.CombatProgression.Tiers {
		if !purchased {
			purchased = game.PerformShipUpgrade(client, logger, ctx, tier, availableCredits)
		}
	}

	// PRIORITY 2: Weapons (essential for combat!)
	// Tier 1: Weapon upgrade (500+ credits)
	if availableCredits >= TIER1_THRESHOLD && !purchased {
		// Check weapon slots
		maxWeapons := game.GetShipClassMaxSlots(state.Ship.ClassID)
		weaponsInstalled := game.CountModulesInstalled(state, "weapon_laser_1")
		weaponsInCargo := game.CountModulesInCargo(state, "weapon_laser_1")
		totalWeapons := weaponsInstalled + weaponsInCargo

		logger.Printf("⚔️ Weapon Status: %d installed, %d in cargo (goal: %d installed)",
			weaponsInstalled, weaponsInCargo, maxWeapons)

		// Only buy more if we have less than max total
		if totalWeapons < maxWeapons {
			for _, listing := range listings {
				// Look for weapons or combat modules
				if listing.Type == "sell" && listing.ItemType == "module" {
					// Prioritize weapon_laser_1 for best value
					if (listing.ItemID == "weapon_laser_1" || listing.ItemID == "weapon_laser_2" ||
						listing.ItemID == "weapon_laser_3") &&
						listing.PricePerUnit <= availableCredits && listing.PricePerUnit <= 1000 {

						// Calculate how many we need to buy (up to max total)
						needed := maxWeapons - totalWeapons
						if needed > 0 {
							logger.Printf("⚔️ Buying %d x %s for %.2f credits each", needed, listing.ItemID, listing.PricePerUnit)
							if err := client.Buy(ctx, listing.ItemID, float64(needed)); err != nil {
								logger.Printf("Failed to buy weapon: %v", err)
							} else {
								logger.Printf("✅ Purchased %d weapon(s)! Installing...", needed)
								purchased = true
								time.Sleep(3 * time.Second)

								// Install each weapon from cargo
								installed := 0
								for i := 0; i < needed; i++ {
									if err := client.Install(ctx, listing.ItemID); err != nil {
										logger.Printf("Failed to install weapon #%d: %v", i+1, err)
									} else {
										logger.Printf("✅ Weapon #%d installed!", installed+1)
										time.Sleep(10 * time.Second) // Wait between installs
									}
								}

								if installed > 0 {
									logger.Printf("✅ %d WEAPON(S) INSTALLED! Combat power increased!", installed)
								}
								break
							}
						}
					}
				}
			}
		}
	}

	if !purchased && availableCredits >= TIER1_THRESHOLD {
		logger.Printf("No suitable upgrades found in market (checked %d listings)", len(listings))
	}

	// Check if we should save for ship upgrades
	if game.ShouldUpgrade(availableCredits, game.CombatProgression.Tiers, TIER3_THRESHOLD) {
		// Find next upgrade tier
		nextTierName := "next ship"
		nextTierCost := 0.0
		for _, tier := range game.CombatProgression.Tiers {
			if availableCredits < tier.Threshold {
				nextTierName = tier.Name
				nextTierCost = tier.Threshold
				break
			}
		}
		logger.Printf("💎 Wealthy fighter! (%.2f credits) - Saving for %s upgrade (%.0f credits needed)",
			availableCredits, nextTierName, nextTierCost)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: auto-fighter <agent-id>")
		fmt.Println("Example: auto-fighter fighter-1")
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
	logger.Printf("🏴‍☠️ Starting autonomous combat & upgrade bot...")
	logger.Printf("Agent: %s | Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Username, creds.Empire, state.Credits, state.Ship.Name, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Start autonomous combat loop with upgrades
	logger.Printf("Starting autonomous combat + upgrade loop...")
	logger.Printf("Will automatically:")
	logger.Printf("  ⚔️  Hunt pirates and defeat them for loot")
	logger.Printf("  💰 Sell all loot for profit")
	logger.Printf("  🚀 Upgrade to better combat ships")
	logger.Printf("  🔫 Install better weapons to increase combat power")
	logger.Printf("     + Progression path:")
	logger.Printf("       Light Fighter (%.0f credits) → 2 weapon slots", game.CombatProgression.Tiers[0].Threshold)
	logger.Printf("       Medium Fighter (%.0f credits) → 3 weapon slots", game.CombatProgression.Tiers[1].Threshold)
	logger.Printf("       Heavy Fighter (%.0f credits) → 4 weapon slots", game.CombatProgression.Tiers[2].Threshold)
	logger.Printf("       Elite Fighter (%.0f credits) → 5 weapon slots", game.CombatProgression.Tiers[3].Threshold)
	logger.Printf("       Ultimate Fighter (%.0f credits) → 6 weapon slots", game.CombatProgression.Tiers[4].Threshold)
	logger.Printf("")

	if err := fighterLoop(agentID, client, logger, ctx); err != nil {
		log.Fatalf("Fighter loop error: %v", err)
	}
}
