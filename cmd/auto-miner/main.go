package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/pkg/calllog"
	"github.com/rsned/spacemolt/pkg/game"
)

const (
	// Reserve credits (never spend below this)
	RESERVE_CREDITS = 50.0
)

func updateCaptainsLog(agentID string, client game.GameClient, miningRuns int, creditsEarned float64) {
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
	numLasers := game.CountModulesInstalled(state, "mining_laser_i") +
		game.CountModulesInstalled(state, "mining_laser_ii") +
		game.CountModulesInstalled(state, "mining_laser_iii") +
		game.CountModulesInstalled(state, "strip_mining_laser")
	notes = append(notes, fmt.Sprintf("Mining lasers: %d", numLasers))

	currentGoal := "Autonomous mining operations - collecting resources"
	if state.Doc {
		currentGoal = "Docked at station - selling cargo and refueling"
	} else if state.Traveling && state.TravelProgress != nil {
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

	_ = game.WriteCaptainsLog(agentID, entry)
}

func miningLoop(agentID string, client game.GameClient, logger *log.Logger, ctx context.Context, stationAction game.StationActionStrategy, miningType game.MiningType) error {
	// Configure the shared mining loop
	config := &game.MiningLoopConfig{
		AgentID:          agentID,
		MiningType:       miningType,
		ReserveCredits:   RESERVE_CREDITS,
		UseBulkSell:      true, // Use bulk sell for better performance
		OnStationActions: stationAction, // Use the selected strategy
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
	debug := flag.Bool("debug", false, "Enable debug logging")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	mineType := flag.String("type", "asteroid", "Mining type: asteroid, gas, or ice")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: auto-miner [flags] <agent-id> [strategy]")
		fmt.Println("")
		fmt.Println("Arguments:")
		fmt.Println("  agent-id   Agent identifier (e.g., miner-1, craftsman-1)")
		fmt.Println("  strategy   Station action strategy (optional, default: sell)")
		fmt.Println("")
		fmt.Println("Strategies:")
		fmt.Println("  sell       Sell all cargo immediately (default, fastest)")
		fmt.Println("  craft-sell Craft items from resources, then sell all")
		fmt.Println("  craft-deposit Craft items from resources, then deposit to storage")
		fmt.Println("")
		fmt.Println("Mining types (-type flag):")
		fmt.Println("  asteroid   Mine asteroid belts/fields with mining lasers (default)")
		fmt.Println("  gas        Harvest gas clouds with gas harvesters")
		fmt.Println("  ice        Harvest ice fields with ice harvesters")
		fmt.Println("")
		fmt.Println("Flags:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  auto-miner miner-1              # Sell everything")
		fmt.Println("  auto-miner miner-1 sell         # Sell everything (explicit)")
		fmt.Println("  auto-miner miner-1 craft-sell   # Craft then sell")
		fmt.Println("  auto-miner miner-1 craft-deposit # Craft then deposit")
		fmt.Println("  auto-miner -type=gas miner-1    # Harvest gas clouds")
		fmt.Println("  auto-miner -type=ice miner-1    # Harvest ice fields")
		fmt.Println("  auto-miner -debug miner-1       # With debug logging")
		fmt.Println("  auto-miner -transport=mcp miner-1 # Use MCP transport")
		os.Exit(1)
	}

	agentID := flag.Args()[0]

	// Validate mining type
	var miningType game.MiningType
	switch *mineType {
	case "asteroid":
		miningType = game.MiningTypeAsteroid
	case "gas":
		miningType = game.MiningTypeGas
	case "ice":
		miningType = game.MiningTypeIce
	default:
		log.Fatalf("Unknown mining type: %s (must be: asteroid, gas, ice)", *mineType)
	}

	// Parse station action strategy
	strategy := "sell"
	if len(flag.Args()) >= 2 {
		strategy = flag.Args()[1]
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
		var wsClient *game.Client
		wsClient, creds, err = game.InitializeAgent(agentID, logger, ctx, *debug)
		if err != nil {
			log.Fatalf("Failed to initialize agent: %v", err)
		}
		// Initialize crafting configuration if using a crafting strategy
		if strategy == "craft-sell" || strategy == "craft-deposit" {
			wsClient.CraftingConfig = &game.CraftingConfig{
				CraftingServerPath: "", // Empty string uses "crafting-server" from PATH
			}
			logger.Printf("🔧 Crafting configured: using MCP server from PATH")
		}
		client = wsClient
	default:
		log.Fatalf("Unknown transport: %s (must be: ws, mcp)", *transport)
	}

	// Set up structured call logger for request/response pairs
	logDir := filepath.Join("data", "agents", agentID, "logs")
	clog, err := calllog.New(logDir, "auto-miner", calllog.Mutations)
	if err != nil {
		log.Fatalf("Failed to create call logger: %v", err)
	}
	if wsClient, ok := client.(*game.Client); ok {
		wsClient.CallLogger = clog
	}

	defer func() {
		_ = clog.Close()
		if err := client.Close(); err != nil {
			logger.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// Get initial state
	state := client.GetState()
	logger.Printf("🏴‍☠️ Starting autonomous mining bot...")
	logger.Printf("Agent: %s | Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Username, creds.Empire, state.Credits, state.Ship.Name,
		state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Start autonomous mining loop
	logger.Printf("Starting autonomous mining loop...")
	logger.Printf("Mining type: %s | Station action strategy: %s", *mineType, strategy)
	logger.Printf("Will automatically:")
	logger.Printf("  ⛏️  Mine %s resources until cargo full", *mineType)
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
	logger.Printf("")

	if err := miningLoop(agentID, client, logger, ctx, stationAction, miningType); err != nil {
		log.Fatalf("Mining loop error: %v", err)
	}
}
