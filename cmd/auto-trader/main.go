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

type TraderAgent struct {
	logger *log.Logger
}

func (t *TraderAgent) OnConnected(state *game.State) {
	t.logger.Printf("Connected! Credits: %.2f", state.Credits)
}

func (t *TraderAgent) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			t.logger.Printf("OK: %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			t.logger.Printf("ERROR: %s", msg)
		}
	}
}

func (t *TraderAgent) OnDisconnected(err error) {
	t.logger.Printf("Disconnected: %v", err)
}

func updateCaptainsLog(agentID string, client *game.Client, tradingRuns int, creditsEarned float64, strategy string) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("Trading runs completed: %d", tradingRuns))
	notes = append(notes, fmt.Sprintf("Credits earned this run: %.2f", creditsEarned))
	notes = append(notes, fmt.Sprintf("Current credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Ship: %s (%d modules)", state.Ship.Name, len(state.Ship.Modules)))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	if len(state.Ship.Cargo) > 0 {
		notes = append(notes, fmt.Sprintf("Cargo: %d items (%.0f/%.0f)", len(state.Ship.Cargo), state.Ship.CargoUsed, state.Ship.CargoCapacity))
	}

	currentGoal := "Autonomous trading operations - finding profitable trade routes"
	if state.Doc {
		switch strategy {
		case "sell":
			currentGoal = "Docked at station - selling cargo and monitoring markets"
		case "craft-sell":
			currentGoal = "Docked at station - crafting items, selling cargo, and monitoring markets"
		case "craft-deposit":
			currentGoal = "Docked at station - crafting items and depositing to storage"
		}
	} else if state.Traveling {
		currentGoal = fmt.Sprintf("Traveling to %s", state.TravelProgress.Destination)
	} else if !state.Doc && len(state.Ship.Cargo) > 0 {
		currentGoal = "In space - returning to station for trading operations"
	}

	entry := &game.AgentLog{
		AgentName:   state.Player.Username,
		CurrentGoal: currentGoal,
		Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
		Notes:       notes,
	}

	_ = game.WriteCaptainsLog(agentID, entry)
}

func tradingLoop(agentID string, client *game.Client, logger *log.Logger, ctx context.Context, stationAction game.StationActionStrategy, strategy string) error {
	// For now, the auto-trader will operate in a simple loop:
	// 1. If docked with cargo, execute station action (craft/sell/deposit)
	// 2. If not docked, travel to nearest station
	// 3. Refuel and repair as needed
	// TODO: Add market analysis and profitable trade route finding

	runNum := 0
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runNum++
			state := client.GetState()

			logger.Printf("═══ Trading Run #%d ═══", runNum)
			logger.Printf("Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Cargo: %.1f/%.1f",
				state.Credits, state.Fuel, state.MaxFuel, state.Hull, state.MaxHull,
				state.Ship.CargoUsed, state.Ship.CargoCapacity)

			// Get system data if needed
			if len(state.System.POIs) == 0 {
				logger.Printf("Fetching system data...")
				if err := client.GetSystem(ctx); err != nil {
					logger.Printf("Failed to get system: %v", err)
				}
				time.Sleep(2 * time.Second)
				state = client.GetState()
			}

			// Find nearest station
			var stationPOI string
			for _, poi := range state.System.POIs {
				if poi.Type == "station" {
					stationPOI = poi.ID
					break
				}
			}

			if stationPOI == "" {
				logger.Printf("⚠️  No station found in system %s", state.System.Name)
				time.Sleep(30 * time.Second)
				continue
			}

			// Travel to station if not docked
			if !state.Doc {
				if state.CurrentPOI != stationPOI && !state.Traveling {
					logger.Printf("🚀 Traveling to station %s...", stationPOI)
					if err := client.Travel(ctx, stationPOI); err != nil {
						logger.Printf("Travel error: %v", err)
					}
					time.Sleep(20 * time.Second)
				}

				// Dock at station
				state = client.GetState()
				if !state.Doc && !state.Traveling {
					logger.Printf("📥 Docking at station...")
					if err := client.Dock(ctx); err != nil {
						if err.Error() != "Already docked (success)" {
							logger.Printf("Dock error: %v", err)
						}
					}
					time.Sleep(15 * time.Second)
				}
			}

			// Execute station action if docked with cargo
			state = client.GetState()
			creditsBefore := state.Credits
			if state.Doc && len(state.Ship.Cargo) > 0 {
				logger.Printf("📦 Executing station action strategy: %s", strategy)
				if err := stationAction(client, logger, ctx); err != nil {
					logger.Printf("Station action error: %v", err)
				} else {
					state = client.GetState()
					creditsEarned := state.Credits - creditsBefore
					if creditsEarned > 0 {
						logger.Printf("💰 Credits earned: %.2f", creditsEarned)
					}
				}
			} else if !state.Doc {
				logger.Printf("⚠️  Not docked, waiting...")
			} else if len(state.Ship.Cargo) == 0 {
				logger.Printf("📦 Cargo is empty, monitoring market conditions...")
				// TODO: Implement market analysis and buying logic
			}

			// Refuel if needed
			state = client.GetState()
			if state.Doc && state.Fuel < state.MaxFuel*0.8 {
				logger.Printf("⛽ Refueling...")
				if err := client.Refuel(ctx); err != nil {
					logger.Printf("Refuel error: %v", err)
				}
				time.Sleep(3 * time.Second)
			}

			// Repair if needed
			state = client.GetState()
			if state.Doc && state.Hull < state.MaxHull*0.9 {
				logger.Printf("🔧 Repairing hull...")
				if err := client.Repair(ctx); err != nil {
					logger.Printf("Repair error: %v", err)
				}
				time.Sleep(3 * time.Second)
			}

			// Update captain's log
			state = client.GetState()
			runCreditsEarned := state.Credits - creditsBefore
			updateCaptainsLog(agentID, client, runNum, runCreditsEarned, strategy)

			logger.Printf("═══ Run #%d Complete ═══", runNum)
			logger.Printf("Current Credits: %.2f\n", state.Credits)

		case <-logTicker.C:
			state := client.GetState()
			logger.Printf("Status: Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Docked: %v | Location: %s",
				state.Credits, state.Fuel, state.MaxFuel, state.Hull,
				state.MaxHull, state.Doc, state.System.Name)
			updateCaptainsLog(agentID, client, runNum, 0, strategy)
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: auto-trader <agent-id> [strategy]")
		fmt.Println("")
		fmt.Println("Arguments:")
		fmt.Println("  agent-id   Agent identifier (e.g., trader-1, trader-2)")
		fmt.Println("  strategy   Station action strategy (optional, default: sell)")
		fmt.Println("")
		fmt.Println("Strategies:")
		fmt.Println("  sell       Sell all cargo immediately (default)")
		fmt.Println("  craft-sell Craft items from resources, then sell all")
		fmt.Println("  craft-deposit Craft items from resources, then deposit to storage")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  auto-trader trader-1            # Sell everything (default)")
		fmt.Println("  auto-trader trader-1 sell       # Sell everything (explicit)")
		fmt.Println("  auto-trader trader-1 craft-sell # Craft then sell")
		fmt.Println("  auto-trader trader-1 craft-deposit # Craft then deposit")
		fmt.Println("")
		fmt.Println("NOTE: Market analysis and trade route finding logic is still being developed")
		os.Exit(1)
	}

	agentID := os.Args[1]

	// Parse station action strategy
	strategy := "sell"
	if len(os.Args) >= 3 {
		strategy = os.Args[2]
	}

	// Validate strategy
	var stationAction game.StationActionStrategy
	switch strategy {
	case "sell":
		stationAction = game.StationActionSellAll
	case "craft-sell":
		stationAction = game.StationActionCraftAndSell
	case "craft-deposit":
		stationAction = game.StationActionCraftAndDeposit
	default:
		log.Fatalf("Unknown strategy: %s (must be: sell, craft-sell, craft-deposit)", strategy)
	}

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
	}

	ctx := context.Background()

	client, creds, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	// Initialize crafting configuration if using a crafting strategy
	if strategy == "craft-sell" || strategy == "craft-deposit" {
		client.CraftingConfig = &game.CraftingConfig{
			CraftingServerPath: "", // Empty string uses "crafting-server" from PATH
		}
		logger.Printf("🔧 Crafting configured: using MCP server from PATH")
	}

	time.Sleep(1 * time.Second)

	// Get initial state
	state := client.GetState()
	logger.Printf("🏴‍☠️ Starting autonomous trading agent...")
	logger.Printf("Agent: %s | Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Username, creds.Empire, state.Credits, state.Ship.Name,
		state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Start autonomous trading loop
	logger.Printf("Starting autonomous trading loop...")
	logger.Printf("Station action strategy: %s", strategy)
	logger.Printf("Will automatically:")
	switch strategy {
	case "sell":
		logger.Printf("  💰 Sell all cargo for credits")
	case "craft-sell":
		logger.Printf("  🔨 Craft items from resources")
		logger.Printf("  💰 Sell all cargo for credits")
	case "craft-deposit":
		logger.Printf("  🔨 Craft items from resources")
		logger.Printf("  📥 Deposit all cargo to station storage")
	}
	logger.Printf("  🛠️  Refuel and repair as needed")
	logger.Printf("  📊 Monitor market conditions (future: profitable trade routes)")
	logger.Printf("")

	if err := tradingLoop(agentID, client, logger, ctx, stationAction, strategy); err != nil {
		log.Fatalf("Trading loop error: %v", err)
	}
}
