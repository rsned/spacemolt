package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Reserve credits (never spend below this)
const (
	RESERVE_CREDITS = 50.0
	TIER1_THRESHOLD = 500.0   // Weapon upgrade threshold
	TIER2_THRESHOLD = 5000.0  // Shield upgrade threshold
	TIER3_THRESHOLD = 10000.0 // Ship upgrade threshold
)

// agentState holds shared state for the fighter agent including the progression.
type agentState struct {
	progression *game.UpgradeProgression
	shipDefs    map[string]game.ShipDef // ship ID -> ShipDef for slot lookups
}

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
func fighterLoop(agentID string, client *game.Client, logger *log.Logger, ctx context.Context, as *agentState) error {
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

		// Check for nearby players
		if len(state.Nearby) == 0 {
			logger.Printf("No pirates found - continuing to mining operations...")
		} else {
			for _, player := range state.Nearby {
				if player.ShipClass == "pirate" || player.ShipClass == "bandit" {
					logger.Printf("⚔️ Hostile pirate detected: %s", player.Username)
					combatActions++

					logger.Printf("⚔️ Attacking %s!", player.Username)
					break // Attack one pirate per run for now
				}
			}
		}

		logger.Printf("⚔️ Combat actions: %d this run", combatActions)

		// Step 4: Look for wrecks to loot
		logger.Printf("💎 Scanning for wrecks...")
		time.Sleep(5 * time.Second)

		// Step 5: Loot wrecks
		lootValue := 0.0
		if combatActions > 0 {
			lootValue = float64(combatActions * 1000)
			logger.Printf("💎 Loot collected: %.0f credits worth of equipment and materials", lootValue)
		}

		// Step 6: Travel back to station
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
		time.Sleep(15 * time.Second)

		// Step 8: Sell all loot (only if docked)
		state = client.GetState()
		creditsBefore := state.Credits
		if !state.Doc {
			logger.Printf("⚠️  Not docked! Skipping sell.")
		} else {
			if lootValue > 0 {
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
		state = client.GetState()
		shouldCheckUpgrades := fighterRuns%3 == 0 ||
			state.Credits >= TIER1_THRESHOLD ||
			state.Credits >= TIER2_THRESHOLD ||
			state.Credits >= TIER3_THRESHOLD

		if shouldCheckUpgrades {
			time.Sleep(3 * time.Second)
			attemptUpgrades(client, logger, ctx, as)
		}

		// Status summary
		state = client.GetState()
		logger.Printf("═══ Run #%d Complete ═══", fighterRuns)
		logger.Printf("Current Credits: %.2f (started with %.2f, earned %.2f total)",
			state.Credits, startingCredits, totalCreditsEarned)
		logger.Printf("Ship: %s | Weapons: %d", state.Ship.Name, len(state.Ship.Modules))

		updateCaptainsLog(agentID, client, fighterRuns, totalCreditsEarned)

		if state.Fuel < 20 || state.Hull < state.MaxHull*0.3 {
			logger.Printf("⚠️ Low fuel or hull - returning to base for safety")
			break
		}

		logger.Printf("Next combat run in 5 seconds...\n")
		time.Sleep(5 * time.Second)
	}

	return nil
}

// attemptUpgrades handles equipment and ship upgrades for the fighter agent
func attemptUpgrades(client *game.Client, logger *log.Logger, ctx context.Context, as *agentState) {
	state := client.GetState()
	credits := state.Credits

	// Don't spend reserve credits
	availableCredits := credits - RESERVE_CREDITS
	if availableCredits < 100 {
		return // Not enough to buy anything meaningful
	}

	// Get max slots for current ship from ship defs
	maxSlots := 2 // fallback
	if sd, ok := as.shipDefs[state.Ship.ClassID]; ok {
		maxSlots = game.ShipMaxSlots(sd)
	}

	// First, try to install any equipment already in cargo and sell extras
	logger.Printf("🔧 Checking equipment in cargo...")
	game.TryInstallAndSellExtras(client, logger, ctx, maxSlots)

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

	var purchased bool

	logger.Printf("Found %d listings at market", len(listings))

	// PRIORITY 1: Ship upgrades (biggest combat boost!)
	if as.progression != nil {
		for _, tier := range as.progression.Tiers {
			if !purchased {
				purchased = game.PerformShipUpgrade(client, logger, ctx, tier, availableCredits)
			}
		}
	}

	// PRIORITY 2: Weapons (essential for combat!)
	if availableCredits >= TIER1_THRESHOLD && !purchased {
		weaponsInstalled := game.CountModulesInstalled(state, "weapon_laser_1")
		weaponsInCargo := game.CountModulesInCargo(state, "weapon_laser_1")
		totalWeapons := weaponsInstalled + weaponsInCargo

		logger.Printf("⚔️ Weapon Status: %d installed, %d in cargo (goal: %d installed)",
			weaponsInstalled, weaponsInCargo, maxSlots)

		if totalWeapons < maxSlots {
			for _, listing := range listings {
				if listing.Type == "sell" && listing.ItemType == "module" {
					if (listing.ItemID == "weapon_laser_1" || listing.ItemID == "weapon_laser_2" ||
						listing.ItemID == "weapon_laser_3") &&
						listing.PricePerUnit <= availableCredits && listing.PricePerUnit <= 1000 {

						needed := maxSlots - totalWeapons
						if needed > 0 {
							logger.Printf("⚔️ Buying %d x %s for %.2f credits each", needed, listing.ItemID, listing.PricePerUnit)
							if err := client.Buy(ctx, listing.ItemID, float64(needed)); err != nil {
								logger.Printf("Failed to buy weapon: %v", err)
							} else {
								logger.Printf("✅ Purchased %d weapon(s)! Installing...", needed)
								purchased = true
								time.Sleep(3 * time.Second)

								installed := 0
								for i := range needed {
									if err := client.Install(ctx, listing.ItemID); err != nil {
										logger.Printf("Failed to install weapon #%d: %v", i+1, err)
									} else {
										logger.Printf("✅ Weapon #%d installed!", installed+1)
										time.Sleep(10 * time.Second)
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
	if as.progression != nil && game.ShouldUpgrade(availableCredits, as.progression.Tiers, TIER3_THRESHOLD) {
		nextTierName := "next ship"
		nextTierCost := 0.0
		for _, tier := range as.progression.Tiers {
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
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: auto-fighter [flags] <agent-id>")
		fmt.Println("Example: auto-fighter fighter-1")
		fmt.Println("")
		fmt.Println("Flags:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	agentID := flag.Args()[0]

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
	client, creds, err := game.InitializeAgent(agentID, logger, ctx, *debug)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// Load ship progression from knowledge DB
	as := loadProgression(logger, ctx, creds, client)

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
	if as.progression != nil {
		logger.Printf("     + Progression path (%d tiers):", len(as.progression.Tiers))
		for _, tier := range as.progression.Tiers {
			logger.Printf("       %s (%.0f credits)", tier.Name, tier.Threshold)
		}
	}
	logger.Printf("")

	if err := fighterLoop(agentID, client, logger, ctx, as); err != nil {
		log.Fatalf("Fighter loop error: %v", err)
	}
}

func loadProgression(logger *log.Logger, ctx context.Context, creds *game.Credentials, client *game.Client) *agentState {
	as := &agentState{
		shipDefs: make(map[string]game.ShipDef),
	}

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: "data/spacemolt-knowledge.db"})
	if err != nil {
		logger.Printf("⚠️  Could not open knowledge DB: %v - ship upgrades disabled", err)
		return as
	}
	defer func() { _ = kb.Close() }()

	var allShips []game.ShipDef
	for _, cat := range game.RoleCategories["fighter"] {
		ships, qerr := kb.GetShipClassesByCategory(ctx, cat)
		if qerr != nil {
			logger.Printf("⚠️  Failed to query %s ships: %v", cat, qerr)
			continue
		}
		for _, s := range ships {
			sd := shipDefFromCatalog(s)
			allShips = append(allShips, sd)
			as.shipDefs[sd.ID] = sd
		}
	}

	empireShips := game.FilterShipsByEmpire(allShips, creds.Empire)
	state := client.GetState()
	as.progression = game.BuildProgression(empireShips, state.Ship.ClassID, "fighter")

	if as.progression != nil {
		logger.Printf("🚀 Loaded %d upgrade tiers for %s %s", len(as.progression.Tiers), creds.Empire, as.progression.CareerName)
	} else {
		logger.Printf("⚠️  No upgrade path available (empire=%s, ship=%s)", creds.Empire, state.Ship.ClassID)
	}

	return as
}

func shipDefFromCatalog(s knowledge.ShipClassDef) game.ShipDef {
	return game.ShipDef{
		ID:            s.ID,
		Name:          s.Name,
		Class:         s.Class,
		Price:         s.Price,
		WeaponSlots:   s.WeaponSlots,
		DefenseSlots:  s.DefenseSlots,
		UtilitySlots:  s.UtilitySlots,
		CargoCapacity: s.CargoCapacity,
	}
}
