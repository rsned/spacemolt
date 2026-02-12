package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

// Upgrade thresholds and priorities
const (
	// Credit thresholds for different upgrade tiers
	TIER1_THRESHOLD      = 300.0   // Mining laser (faster mining!)
	TIER2_THRESHOLD      = 800.0   // Better mining laser or cargo
	TIER2_SHIP_THRESHOLD = 2000.0  // Upgrade to mining_enhanced ship + 3 mining lasers
	TIER3_THRESHOLD      = 1500.0  // Shield upgrade
	TIER4_THRESHOLD      = 3000.0  // Weapon for combat
	TIER5_THRESHOLD      = 5000.0  // Better ship or advanced weapons
	TIER6_THRESHOLD      = 10000.0 // Top tier equipment

	// Reserve credits (never spend below this)
	RESERVE_CREDITS = 50.0
)

// Common upgrade item IDs (these may need to be discovered via get_listings)
var upgradeItems = map[string][]string{
	"weapons": {
		"laser_cannon_1",
		"missile_launcher_1",
		"plasma_gun_1",
	},
	"shields": {
		"shield_generator_1",
		"shield_booster_1",
	},
	"cargo": {
		"cargo_expansion_1",
		"cargo_expansion_2",
	},
	"engines": {
		"engine_upgrade_1",
		"thruster_boost_1",
	},
}

// countModulesInstalled counts how many of a specific module type are installed
func countModulesInstalled(state *game.State, itemID string) int {
	count := 0
	for _, module := range state.Ship.Modules {
		if module == itemID {
			count++
		}
	}
	return count
}

// countModulesInCargo counts how many of a specific module type are in cargo
func countModulesInCargo(state *game.State, itemID string) int {
	count := 0
	for _, item := range state.Ship.Cargo {
		if item.ItemID == itemID && item.Quantity > 0 {
			count += int(item.Quantity)
		}
	}
	return count
}

// isItemOwned checks if an item is already installed on the ship or in cargo
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

// isOreOrResource returns true if the item is ore or a resource (should be sold, not installed)
func isOreOrResource(itemID string) bool {
	// Ores and resources to sell
	oreAndResourcePrefixes := []string{
		"ore_",     // All ores (ore_iron, ore_copper, etc.)
		"gas_",     // Gases
		"crystal_", // Crystals
		"salvage_", // Salvage materials
		"scrap_",   // Scrap materials
	}

	for _, prefix := range oreAndResourcePrefixes {
		if len(itemID) >= len(prefix) && itemID[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}

// tryInstallAndSellExtras attempts to install modules from cargo and sells any that can't be installed
func tryInstallAndSellExtras(client *game.Client, logger *log.Logger, ctx context.Context) {
	state := client.GetState()

	// Find all equipment items in cargo (not ores/resources)
	for _, item := range state.Ship.Cargo {
		// Skip ores and resources
		if isOreOrResource(item.ItemID) {
			continue
		}

		// Special handling for mining lasers - keep only what we can use
		if item.ItemID == "mining_laser_1" || item.ItemID == "mining_laser_2" ||
			item.ItemID == "mining_laser_3" || item.ItemID == "advanced_mining_laser" {
			miningLasersInstalled := countModulesInstalled(state, item.ItemID)
			miningLasersInCargo := countModulesInCargo(state, item.ItemID)
			totalMiningLasers := miningLasersInstalled + miningLasersInCargo

			// Determine max lasers based on ship
			maxLasers := 2
			switch state.Ship.ClassID {
			case "mining_enhanced":
				maxLasers = 3
			case "mining_barge":
				maxLasers = 4
			}

			// If we have less than max, try to install
			if totalMiningLasers < maxLasers && miningLasersInCargo > 0 {
				for i := 0; i < int(item.Quantity) && (miningLasersInstalled+i) < maxLasers; i++ {
					logger.Printf("🔧 Installing mining laser %s from cargo...", item.ItemID)
					if err := client.Install(ctx, item.ItemID); err != nil {
						logger.Printf("⚠️  Cannot install %s: %v", item.ItemID, err)
						break
					} else {
						logger.Printf("✅ Installed mining laser!")
						miningLasersInstalled++
						time.Sleep(2 * time.Second)
					}
				}
			}

			// If we have MORE than max, sell the excess to free up cargo space
			if totalMiningLasers > maxLasers && miningLasersInCargo > 0 {
				excess := totalMiningLasers - maxLasers
				logger.Printf("⚠️  Too many mining lasers! Have %d, can only use %d - selling %d excess",
					totalMiningLasers, maxLasers, excess)
				if err := client.Sell(ctx, item.ItemID, float64(excess)); err != nil {
					logger.Printf("Failed to sell excess mining lasers: %v", err)
				} else {
					logger.Printf("✅ Sold %d excess mining laser(s) - freed up cargo space!", excess)
					time.Sleep(1 * time.Second)
				}
			}
			continue
		}

		// This is other equipment - try to install it
		if item.Quantity > 0 {
			logger.Printf("🔧 Attempting to install %s from cargo...", item.ItemID)
			if err := client.Install(ctx, item.ItemID); err != nil {
				// Installation failed - probably no slots, CPU, or power available
				logger.Printf("⚠️  Cannot install %s: %v - selling it", item.ItemID, err)
				time.Sleep(2 * time.Second)

				// Sell the item since we can't use it
				if err := client.Sell(ctx, item.ItemID, item.Quantity); err != nil {
					logger.Printf("Failed to sell %s: %v", item.ItemID, err)
				} else {
					logger.Printf("✅ Sold extra %s (%.0f units)", item.ItemID, item.Quantity)
				}
				time.Sleep(1 * time.Second)
			} else {
				logger.Printf("✅ Installed %s successfully!", item.ItemID)
				time.Sleep(2 * time.Second)
			}
		}
	}
}

func attemptUpgrades(client *game.Client, logger *log.Logger, ctx context.Context) {
	state := client.GetState()
	credits := state.Credits

	// Don't spend reserve credits
	availableCredits := credits - RESERVE_CREDITS
	if availableCredits < 100 {
		return // Not enough to buy anything meaningful
	}

	// First, try to install any equipment already in cargo and sell extras
	// This is CRITICAL - it sells excess mining lasers to free up cargo space
	tryInstallAndSellExtras(client, logger, ctx)

	// Refresh state after selling extras
	time.Sleep(2 * time.Second)
	state = client.GetState()
	availableCredits = state.Credits - RESERVE_CREDITS

	// Check if we can upgrade ship (ship upgrades sell cargo first, so allow even with full cargo)
	canUpgradeShip := (state.Ship.ClassID == "starter_mining" && availableCredits >= TIER2_SHIP_THRESHOLD) ||
		(state.Ship.ClassID == "mining_enhanced" && availableCredits >= TIER5_THRESHOLD)

	// Ensure cargo space is available for purchases (except ship upgrades)
	cargoUsed := state.Ship.CargoUsed
	cargoCapacity := state.Ship.CargoCapacity
	if cargoUsed >= cargoCapacity*0.5 && !canUpgradeShip {
		logger.Printf("⚠️  Cargo too full (%.1f/%.1f) - skipping upgrades until cargo is sold", cargoUsed, cargoCapacity)
		return
	}

	logger.Printf("💰 Checking for upgrades... (%.2f credits available, %.1f/%.1f cargo space)", availableCredits, cargoUsed, cargoCapacity)

	// Refresh state after installation attempts
	time.Sleep(2 * time.Second)
	state = client.GetState()
	availableCredits = state.Credits - RESERVE_CREDITS

	// Get market listings to see what's available
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

	// Priority-based upgrade logic (highest tier first)
	var purchased bool

	logger.Printf("Found %d listings at market", len(listings))

	// Tier 4: Weapon (3000+ credits) - HIGHEST PRIORITY for best equipment!
	if availableCredits >= TIER4_THRESHOLD && !purchased {
		for _, listing := range listings {
			if listing.Type == "sell" && listing.ItemType == "weapon" {
				if listing.PricePerUnit <= availableCredits && listing.PricePerUnit <= 3000 {
					// Check if we already own this item
					if isItemOwned(state, listing.ItemID) {
						logger.Printf("🔫 Already own %s - skipping purchase", listing.ItemID)
						continue
					}

					logger.Printf("🔫 Buying weapon: %s for %.2f credits each", listing.ItemID, listing.PricePerUnit)
					if err := client.Buy(ctx, listing.ItemID, 1); err != nil {
						logger.Printf("Failed to buy weapon: %v", err)
					} else {
						logger.Printf("✅ Purchased weapon! Installing...")
						purchased = true
						time.Sleep(2 * time.Second)
						// Try to install it
						if err := client.Install(ctx, listing.ItemID); err != nil {
							logger.Printf("Failed to install weapon: %v (module in cargo)", err)
						} else {
							logger.Printf("✅ WEAPON INSTALLED!")
						}
						time.Sleep(2 * time.Second)
						break
					}
				}
			}
		}
	}

	// Tier 3: Shield upgrade (1500+ credits) - Protection for combat
	if availableCredits >= TIER3_THRESHOLD && !purchased {
		for _, listing := range listings {
			if listing.Type == "sell" && listing.ItemType == "shield" {
				if listing.PricePerUnit <= availableCredits && listing.PricePerUnit <= 2000 {
					// Check if we already own this item
					if isItemOwned(state, listing.ItemID) {
						logger.Printf("🛡️  Already own %s - skipping purchase", listing.ItemID)
						continue
					}

					logger.Printf("🛡️  Buying shield: %s for %.2f credits each", listing.ItemID, listing.PricePerUnit)
					if err := client.Buy(ctx, listing.ItemID, 1); err != nil {
						logger.Printf("Failed to buy shield: %v", err)
					} else {
						logger.Printf("✅ Purchased shield! Installing...")
						purchased = true
						time.Sleep(2 * time.Second)
						// Try to install it
						if err := client.Install(ctx, listing.ItemID); err != nil {
							logger.Printf("Failed to install shield: %v (module in cargo)", err)
						} else {
							logger.Printf("✅ SHIELD INSTALLED!")
						}
						time.Sleep(2 * time.Second)
						break
					}
				}
			}
		}
	}

	// Tier 5: Ship upgrade to mining_barge + 4 mining lasers (5000+ credits) - ULTIMATE MINING SETUP!
	if availableCredits >= TIER5_THRESHOLD && !purchased {
		// Only upgrade to mining_barge if we have mining_enhanced (proper progression)
		// Don't upgrade if already have mining_barge or better
		if state.Ship.ClassID == "mining_enhanced" {
			logger.Printf("🚀 MEGA UPGRADE TIME! You have %.2f credits - upgrading to Excavator!", availableCredits)

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
				// Uninstall mining lasers and other utility modules
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

			logger.Printf("🚀 Purchasing mining_barge ship (Excavator)...")

			// Buy the mining_barge ship using direct protocol message
			buyShipMsg := protocol.Message{
				Type: "buy_ship",
				Payload: map[string]any{
					"ship_class": "mining_barge",
				},
			}
			if err := client.Send(ctx, buyShipMsg); err != nil {
				logger.Printf("Failed to buy ship: %v", err)
			} else {
				logger.Printf("✅ SHIP UPGRADED TO EXCAVATOR!")
				logger.Printf("✅ New capacity: 150 cargo, 4 utility slots for mining lasers!")
				purchased = true
				time.Sleep(3 * time.Second)

				// Refresh state after ship purchase
				state = client.GetState()

				// Now buy 4 mining lasers to fill all utility slots
				logger.Printf("⛏️  Now purchasing 4 mining lasers...")
				if err := client.Buy(ctx, "mining_laser_1", 4); err != nil {
					logger.Printf("Failed to buy mining lasers: %v", err)
				} else {
					logger.Printf("✅ Purchased 4 mining lasers!")
					time.Sleep(2 * time.Second)

					// Install each mining laser
					for i := 1; i <= 4; i++ {
						if err := client.Install(ctx, "mining_laser_1"); err != nil {
							logger.Printf("Failed to install mining laser #%d: %v", i, err)
						} else {
							logger.Printf("✅ Mining laser #%d installed!", i)
						}
						time.Sleep(2 * time.Second)
					}

					logger.Printf("✅✅✅✅ QUAD MINING LASER SETUP COMPLETE! Mining power QUADRUPLED!")
				}
			}
		}
	}

	// Tier 2.5: Ship upgrade to mining_enhanced + 3 mining lasers (2000+ credits) - MAJOR UPGRADE!
	if availableCredits >= TIER2_SHIP_THRESHOLD && !purchased {
		// Check if we're still on the starter mining ship
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
				// Uninstall mining lasers and other utility modules
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

	// Tier 2: Cargo expansion (800+ credits) - More cargo = more credits per run
	if availableCredits >= TIER2_THRESHOLD && !purchased {
		for _, listing := range listings {
			// Only buy actual cargo expansions, not mining lasers
			if listing.Type == "sell" && listing.ItemType == "cargo" {
				if listing.PricePerUnit <= availableCredits && listing.PricePerUnit <= 1500 {
					// Check if we already own this item
					if isItemOwned(state, listing.ItemID) {
						logger.Printf("📦 Already own %s - skipping purchase", listing.ItemID)
						continue
					}

					logger.Printf("📦 Buying cargo expansion: %s for %.2f credits each", listing.ItemID, listing.PricePerUnit)
					if err := client.Buy(ctx, listing.ItemID, 1); err != nil {
						logger.Printf("Failed to buy cargo: %v", err)
					} else {
						logger.Printf("✅ Purchased cargo expansion! Installing...")
						purchased = true
						time.Sleep(2 * time.Second)
						// Try to install it
						if err := client.Install(ctx, listing.ItemID); err != nil {
							logger.Printf("Failed to install cargo expansion: %v (module in cargo)", err)
						} else {
							logger.Printf("✅ CARGO CAPACITY INCREASED!")
						}
						time.Sleep(2 * time.Second)
						break
					}
				}
			}
		}
	}

	// Tier 1: Mining laser upgrade (300+ credits) - Basic upgrade for faster mining
	// GOAL: Install up to 3 mining_laser_1 modules for maximum mining performance!
	if availableCredits >= TIER1_THRESHOLD && !purchased {
		// Count how many mining lasers we have
		miningLasersInstalled := countModulesInstalled(state, "mining_laser_1")
		miningLasersInCargo := countModulesInCargo(state, "mining_laser_1")
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
				// Look for mining laser or mining modules
				if listing.Type == "sell" && (listing.ItemType == "module" || listing.ItemType == "mining") {
					// Prioritize mining_laser_1 for best value, but accept upgrades
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
		} else {
			logger.Printf("✅ Already have %d mining lasers (max needed for optimal mining)", totalMiningLasers)
		}
	}

	if !purchased && availableCredits >= TIER1_THRESHOLD {
		logger.Printf("No suitable upgrades found in market (checked %d listings)", len(listings))
	}
	if !purchased && availableCredits >= TIER5_THRESHOLD {
		logger.Printf("💎 Wealthy pirate! (%.2f credits) - Saving for advanced upgrades", credits)
	}
}

func miningLoop(client *game.Client, logger *log.Logger, ctx context.Context) error {
	miningRuns := 0
	totalCreditsEarned := 0.0
	startingCredits := client.GetState().Credits

	for {
		select {
		case <-ctx.Done():
			return nil
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
		mineCount := 0
		logger.Printf("⛏️  Starting mining operations...")
		for {
			state = client.GetState()

			// Check if cargo is nearly full
			if state.Ship.CargoUsed >= state.Ship.CargoCapacity*0.9 {
				logger.Printf("✓ Cargo nearly full (%.1f/%.1f), heading back",
					state.Ship.CargoUsed, state.Ship.CargoCapacity)
				break
			}

			// Check fuel
			if state.Fuel < 30 {
				logger.Printf("⚠️  Low fuel (%.0f), heading back", state.Fuel)
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
					logger.Printf("⛏️  Mining... [%d] (%.1f/%.1f cargo)",
						mineCount, state.Ship.CargoUsed, state.Ship.CargoCapacity)
				}
			}

			time.Sleep(11 * time.Second)

			// Safety: max mining attempts per run
			if mineCount >= 15 {
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
			logger.Printf("💰 Selling %d different items...", len(state.Ship.Cargo))

			// List what we're selling
			for _, item := range state.Ship.Cargo {
				logger.Printf("   - %s x%.0f", item.ItemID, item.Quantity)
			}

			if err := client.SellAll(ctx); err != nil {
				logger.Printf("Sell error: %v", err)
			} else {
				// Wait longer for state update
				time.Sleep(5 * time.Second)
				state = client.GetState()
				creditsEarned := state.Credits - creditsBefore
				totalCreditsEarned += creditsEarned
				if creditsEarned > 0 {
					logger.Printf("✅ Sold cargo! Earned %.2f credits (Total: %.2f)",
						creditsEarned, totalCreditsEarned)
				} else {
					// State might not have updated yet, but sell likely succeeded
					logger.Printf("✓ Sell command completed (check next state update for credits)")
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

		// Status summary
		state = client.GetState()
		logger.Printf("═══ Run #%d Complete ═══", miningRuns)
		logger.Printf("Current Credits: %.2f (started with %.2f, earned %.2f total)",
			state.Credits, startingCredits, totalCreditsEarned)
		logger.Printf("Ship: %s | Modules: %d", state.Ship.Name, len(state.Ship.Modules))
		logger.Printf("Next run in 5 seconds...\n")

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

	// Create context for lifecycle management
	ctx := context.Background()

	// Initialize game client using shared library function
	// This handles: credential loading, client creation, connection, and login
	client, creds, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer client.Close()

	time.Sleep(1 * time.Second)

	// Get initial state
	state := client.GetState()
	logger.Printf("🏴‍☠️ Starting autonomous mining & upgrade bot...")
	logger.Printf("Agent: %s | Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Username, creds.Empire, state.Credits, state.Ship.Name, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Start autonomous mining loop with upgrades
	logger.Printf("Starting autonomous mining + upgrade loop...")
	logger.Printf("Will automatically:")
	logger.Printf("  ⛏️  Mine resources until cargo full")
	logger.Printf("  💰 Sell all cargo for credits")
	logger.Printf("  🚀 PRIORITY 1: Upgrade to Excavator (mining_barge) + 4 lasers >= %.0f credits", TIER5_THRESHOLD)
	logger.Printf("  🔫 PRIORITY 2: Buy weapons >= %.0f credits (best equipment first!)", TIER4_THRESHOLD)
	logger.Printf("  🛡️  PRIORITY 3: Buy shields >= %.0f credits (prepare for combat)", TIER3_THRESHOLD)
	logger.Printf("  🚀 PRIORITY 4: Upgrade to Drillship (mining_enhanced) + 3 lasers >= %.0f credits", TIER2_SHIP_THRESHOLD)
	logger.Printf("  📦 PRIORITY 5: Buy cargo expansion >= %.0f credits (more ore per run)", TIER2_THRESHOLD)
	logger.Printf("  ⛏️  PRIORITY 6: Buy mining laser >= %.0f credits (mine faster)", TIER1_THRESHOLD)
	logger.Printf("")

	if err := miningLoop(client, logger, ctx); err != nil {
		log.Fatalf("Mining loop error: %v", err)
	}
}
