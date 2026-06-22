package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
)

func updateCaptainsLog(agentID string, client game.GameClient, craftingRuns int, itemsCrafted int, credits float64, strategy string) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("Crafting runs completed: %d", craftingRuns))
	notes = append(notes, fmt.Sprintf("Items crafted this run: %d", itemsCrafted))
	notes = append(notes, fmt.Sprintf("Total items crafted: %d", craftingRuns))
	notes = append(notes, fmt.Sprintf("Current credits: %.2f", credits))
	notes = append(notes, fmt.Sprintf("Ship: %s (%d modules)", state.Ship.Name, len(state.Ship.Modules)))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	if len(state.Ship.Cargo) > 0 {
		notes = append(notes, fmt.Sprintf("Cargo: %d items (%.0f/%.0f)", len(state.Ship.Cargo), state.Ship.CargoUsed, state.Ship.CargoCapacity))
	}

	currentGoal := "Autonomous crafting operations"
	if state.Doc {
		switch strategy {
		case "craft-sell":
			currentGoal = "Docked at station - crafting items and selling for credits"
		case "craft-deposit":
			currentGoal = "Docked at station - crafting items and depositing to storage"
		case "craft-profit":
			currentGoal = "Docked at station - crafting profitable items based on market analysis"
		}
	} else if state.Traveling && state.TravelProgress != nil {
		currentGoal = fmt.Sprintf("Traveling to %s", state.TravelProgress.Destination)
	} else if !state.Doc {
		currentGoal = "In space - returning to station for crafting operations"
	}

	entry := &game.AgentLog{
		AgentName:   state.Player.Username,
		CurrentGoal: currentGoal,
		Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
		Notes:       notes,
	}

	_ = game.WriteCaptainsLog(agentID, entry)
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	marketDBPath := flag.String("market-db-path", "data/market.db", "Path to market database")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: auto-craftsman [flags] <agent-id> [strategy]")
		fmt.Println("")
		fmt.Println("Arguments:")
		fmt.Println("  agent-id   Agent identifier (e.g., craftsman-1, craftsman-2)")
		fmt.Println("  strategy   Crafting strategy (optional, default: craft-deposit)")
		fmt.Println("")
		fmt.Println("Flags:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Strategies:")
		fmt.Println("  craft-deposit  Craft items from resources, then deposit to storage (default)")
		fmt.Println("  craft-sell     Craft items from resources, then sell for credits")
		fmt.Println("  craft-profit   Craft items based on market profitability analysis")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  auto-craftsman craftsman-1               # Craft then deposit (default)")
		fmt.Println("  auto-craftsman craftsman-1 craft-deposit  # Craft then deposit (explicit)")
		fmt.Println("  auto-craftsman craftsman-1 craft-sell     # Craft then sell")
		fmt.Println("  auto-craftsman craftsman-1 craft-profit   # Craft based on profit analysis")
		fmt.Println("  auto-craftsman -transport=mcp craftsman-1 # Use MCP transport")
		os.Exit(1)
	}

	agentID := flag.Args()[0]

	// Parse strategy
	strategy := "craft-deposit"
	if len(flag.Args()) >= 2 {
		strategy = flag.Args()[1]
	}

	// Validate strategy
	if strategy != "craft-deposit" && strategy != "craft-sell" && strategy != "craft-profit" {
		log.Fatalf("Unknown strategy: %s (must be: craft-deposit, craft-sell, craft-profit)", strategy)
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
			log.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// Get initial state
	state := client.GetState()
	logger.Printf("🏴‍☠️ Starting autonomous crafting agent...")
	logger.Printf("Agent: %s | Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Username, creds.Empire, state.Credits, state.Ship.Name,
		state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Start autonomous crafting loop
	logger.Printf("Starting autonomous crafting loop...")
	logger.Printf("Crafting strategy: %s", strategy)
	logger.Printf("Will automatically:")
	logger.Printf("  🔨 Craft items from available resources (iron, copper, aluminum)")
	switch strategy {
	case "craft-deposit":
		logger.Printf("  📥 Deposit crafted items to station storage")
	case "craft-sell":
		logger.Printf("  💰 Sell crafted items for credits")
	case "craft-profit":
		logger.Printf("  💰💰 Craft items based on market profitability (uses market data)")
		logger.Printf("  📊 Analyzes local market buy/sell prices to maximize profit")
		logger.Printf("  📦 Deposits crafted items to station storage for later sale")
	}
	logger.Printf("  🛠️  Refuel and repair as needed")
	logger.Printf("  📦 Withdraw ores from storage when available")
	logger.Printf("  📈 Craft batch size scales with crafting skill level")
	logger.Printf("")

	// Create storage manager (uses client's storage methods)
	storageManager := &clientStorageManager{client: client, logger: logger}

	// Configure the crafting loop
	config := &game.CraftingLoopConfig{
		AgentID:        agentID,
		Strategy:       strategy,
		StorageManager: storageManager,
		OnRunComplete: func(runNum int, itemsCrafted int, totalCredits float64) {
			// Update captain's log after each run
			updateCaptainsLog(agentID, client, runNum, itemsCrafted, totalCredits, strategy)
		},
	}

	// Use profit-based recipe selector for craft-profit strategy
	if strategy == "craft-profit" {
		// Initialize market collector
		mc, err := market.Open(market.Config{DBPath: *marketDBPath})
		if err != nil {
			log.Fatalf("Failed to initialize market collector: %v", err)
		}
		defer func() {
			if err := mc.Close(); err != nil {
				logger.Printf("Warning: Failed to close market collector: %v", err)
			}
		}()

		config.RecipeSelector = ProfitBasedRecipeSelector(mc)
		logger.Printf("📊 Initialized market collector for market analysis")
		logger.Printf("   Market data will be cached for 1 hour")
		logger.Printf("   Market analysis will be cached for 2 hours")
	}

	// Run the crafting loop
	result, err := game.CraftingLoop(client, logger, ctx, config)
	if err != nil {
		log.Fatalf("Crafting loop error: %v", err)
	}

	// Log final results
	logger.Printf("Crafting loop stopped: %s", result.StoppedReason)
	logger.Printf("Total runs: %d, Total items crafted: %d",
		result.RunsCompleted, result.TotalItemsCrafted)
	logger.Printf("Credits: %.2f -> %.2f (%.2f change)",
		result.StartingCredits, result.EndingCredits,
		result.EndingCredits-result.StartingCredits)
}

// clientStorageManager implements game.StorageManager using the game client
type clientStorageManager struct {
	client game.GameClient
	logger *log.Logger
}

func (m *clientStorageManager) WithdrawItems(ctx context.Context, itemID string, quantity float64) error {
	return m.client.WithdrawItems(ctx, itemID, quantity)
}

func (m *clientStorageManager) DepositItems(ctx context.Context, itemID string, quantity float64) error {
	return m.client.DepositItems(ctx, itemID, quantity)
}

func (m *clientStorageManager) ViewStorage(ctx context.Context) (map[string]float64, error) {
	// For WS clients, use Submit (ack-only since view_storage is a query)
	// to get the response directly from the game server.
	if wsClient, ok := m.client.(*game.Client); ok {
		h, err := wsClient.Submit(ctx, protocol.Message{
			Type:      "view_storage",
			Timestamp: time.Now().UnixMilli(),
		}, game.WithAckOnly(), game.WithTimeout(10*time.Second))
		if err != nil {
			return nil, fmt.Errorf("failed to submit view_storage: %w", err)
		}
		resp, err := h.Result(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to view storage: %w", err)
		}

		storageItems := make(map[string]float64)

		// Parse the items from the response payload
		items, ok := resp.Payload["items"].([]any)
		if !ok {
			// No items in storage, return empty map
			return storageItems, nil
		}

		// Convert items to map[itemID]quantity
		for _, itemAny := range items {
			item, ok := itemAny.(map[string]any)
			if !ok {
				continue
			}
			itemID, ok := item["item_id"].(string)
			if !ok {
				continue
			}
			quantity, ok := item["quantity"].(float64)
			if !ok {
				// Try int as fallback
				if quantityInt, ok := item["quantity"].(int); ok {
					quantity = float64(quantityInt)
				} else {
					continue
				}
			}
			storageItems[itemID] = quantity
		}

		return storageItems, nil
	}

	// For MCP clients, call ViewStorage (sends command) but storage items
	// are not captured in state. Return empty map for now.
	// TODO: Parse MCP ViewStorage response to extract storage items.
	if err := m.client.ViewStorage(ctx); err != nil {
		return nil, fmt.Errorf("failed to view storage: %w", err)
	}
	m.logger.Printf("Warning: MCP transport storage viewing returns empty results; storage item parsing not yet implemented for MCP")
	return make(map[string]float64), nil
}
