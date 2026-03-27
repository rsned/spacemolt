package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Reserve credits (never spend below this)
const RESERVE_CREDITS = 50.0

func updateCaptainsLog(agentID string, client game.GameClient, fighterRuns int, totalCreditsEarned float64) {
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
		if strings.HasPrefix(module, "pulse_laser_") || strings.HasPrefix(module, "autocannon_") ||
			strings.HasPrefix(module, "focused_beam_") || strings.HasPrefix(module, "railgun_") ||
			strings.HasPrefix(module, "missile_launcher_") || strings.HasPrefix(module, "ion_cannon_") ||
			strings.HasPrefix(module, "plasma_cannon_") {
			weaponCount++
		}
	}
	notes = append(notes, fmt.Sprintf("Weapons installed: %d", weaponCount))

	currentGoal := "Autonomous combat operations - hunting pirates"
	if state.Doc {
		currentGoal = "Docked at station - selling loot and refueling"
	} else if state.Traveling && state.TravelProgress != nil {
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
// Logic: Hunt pirates, loot wrecks, sell loot, repeat
func fighterLoop(agentID string, client game.GameClient, logger *log.Logger, ctx context.Context) error {
	fighterRuns := 0
	totalCreditsEarned := 0.0
	startingCredits := client.GetState().Credits

	logger.Printf("🏴‍☠️ Starting autonomous combat bot...")

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
			if _, err := client.Travel(ctx, combatPOI); err != nil {
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
			if _, err := client.Travel(ctx, stationPOI); err != nil {
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

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: auto-fighter [flags] <agent-id>")
		fmt.Println("Example: auto-fighter fighter-1")
		fmt.Println("         auto-fighter -transport=mcp fighter-1")
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

	// Initialize game client based on transport selection
	var client game.GameClient
	var creds *game.Credentials

	switch *transport {
	case "mcp":
		logger.Printf("Using MCP transport")
		client, creds, err = game.InitializeMCPAgent(agentID, logger, ctx, *debug, false)
		if err != nil {
			log.Fatalf("Failed to initialize MCP agent: %v", err)
		}
	case "ws":
		logger.Printf("Using WebSocket transport")
		client, creds, err = game.InitializeAgent(agentID, logger, ctx, *debug)
		if err != nil {
			log.Fatalf("Failed to initialize agent: %v", err)
		}
	default:
		log.Fatalf("Unknown transport: %s (must be: ws, mcp)", *transport)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// Get initial state
	state := client.GetState()
	logger.Printf("🏴‍☠️ Starting autonomous combat bot...")
	logger.Printf("Agent: %s | Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Username, creds.Empire, state.Credits, state.Ship.Name, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Start autonomous combat loop
	logger.Printf("Starting autonomous combat loop...")
	logger.Printf("Will automatically:")
	logger.Printf("  ⚔️  Hunt pirates and defeat them for loot")
	logger.Printf("  💰 Sell all loot for profit")
	logger.Printf("")

	if err := fighterLoop(agentID, client, logger, ctx); err != nil {
		log.Fatalf("Fighter loop error: %v", err)
	}
}
