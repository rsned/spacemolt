package game

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"
)

// CraftingLoopConfig configures the behavior of the crafting loop
type CraftingLoopConfig struct {
	// AgentID for captain's log updates (optional)
	AgentID string

	// Strategy determines what to do with crafted items
	// "craft-deposit": Craft items and deposit to storage
	// "craft-sell": Craft items and sell for credits
	// "craft-profit": Craft items based on market profitability and deposit to storage
	Strategy string

	// StopCondition is called before each crafting run to check if we should stop
	// Return true to stop crafting, false to continue
	// If nil, crafting continues indefinitely
	StopCondition func(state *State) bool

	// OnRunComplete is called after each successful crafting run (optional)
	// Useful for tracking stats, updating logs, etc.
	OnRunComplete func(runNum int, itemsCrafted int, totalCredits float64)

	// RecipeSelector selects which recipes to craft based on available resources and skills
	// If nil, uses DefaultRecipeSelector
	RecipeSelector RecipeSelector

	// StorageManager handles withdrawing and depositing items from/to station storage
	// If nil, only uses cargo (no storage interaction)
	StorageManager StorageManager

	// CaptainsLogInterval controls how often to update captain's log
	// If 0, defaults to 2 minutes
	CaptainsLogInterval time.Duration
}

// CraftingLoopResult contains stats from a crafting loop execution
type CraftingLoopResult struct {
	RunsCompleted      int
	TotalItemsCrafted int
	StartingCredits    float64
	EndingCredits      float64
	StoppedReason      string // "context_cancelled", "stop_condition", "error", etc.
}

// RecipeSelector selects recipes to craft based on current state and storage
type RecipeSelector func(client GameClient, logger *log.Logger, ctx context.Context, storage StorageManager) ([]string, error)

// StorageManager handles storage operations
type StorageManager interface {
	// WithdrawItems withdraws items from storage to cargo
	WithdrawItems(ctx context.Context, itemID string, quantity float64) error
	// DepositItems deposits items from cargo to storage
	DepositItems(ctx context.Context, itemID string, quantity float64) error
	// ViewStorage returns the current storage contents
	ViewStorage(ctx context.Context) (map[string]float64, error)
}

// DefaultRecipeSelector selects recipes using the MCP crafting server
// It queries the server with current cargo + storage and skills, then returns
// the best recipe for each output item (most efficient first)
func DefaultRecipeSelector(client GameClient, logger *log.Logger, ctx context.Context, storage StorageManager) ([]string, error) {
	state := client.GetState()

	// Build components list from cargo + storage
	// The MCP server needs to know what we have available
	var components []Component

	// Add cargo items
	for _, item := range state.Ship.Cargo {
		components = append(components, Component{
			ID:       item.ItemID,
			Quantity: item.Quantity,
		})
	}

	// Add storage items if storage manager is available
	if storage != nil {
		storageItems, err := storage.ViewStorage(ctx)
		if err != nil {
			logger.Printf("Warning: Failed to view storage: %v", err)
		} else if len(storageItems) > 0 {
			logger.Printf("📦 Storage available: %d item types", len(storageItems))
			// Log first 10 items
			count := 0
			for itemID, qty := range storageItems {
				if count < 10 {
					logger.Printf("   • %s: %.0f units", itemID, qty)
					count++
				}
				// Add to components
				components = append(components, Component{
					ID:       itemID,
					Quantity: qty,
				})
			}
			if len(storageItems) > 10 {
				logger.Printf("   ... and %d more item types", len(storageItems)-10)
			}
		}
	}

	logger.Printf("📊 Resource Analysis (Cargo + Storage):")
	logger.Printf("   Total resources: %d item types", len(components))

	// Query the MCP crafting server for what we can craft
	// Use nil config to default to "crafting-server" from PATH
	// QueryCraftableFromComponents is only available on the WS client
	wsClient, ok := client.(*Client)
	if !ok {
		return nil, fmt.Errorf("DefaultRecipeSelector requires WS client for QueryCraftableFromComponents")
	}
	result, err := wsClient.QueryCraftableFromComponents(ctx, components, nil)
	if err != nil {
		logger.Printf("❌ Failed to query crafting server: %v", err)
		logger.Printf("   💡 Make sure the crafting-server is available in PATH")
		logger.Printf("   💡 Or specify custom path in CraftingConfig")
		return nil, fmt.Errorf("crafting server query failed: %w", err)
	}

	logger.Printf("🎯 Found %d fully craftable recipes", len(result.FullyCraftable))
	if len(result.PartialMatches) > 0 {
		logger.Printf("   (%d additional recipes have partial matches)", len(result.PartialMatches))
	}
	if len(result.SkillBlocked) > 0 {
		logger.Printf("   (%d recipes blocked by skill requirements)", len(result.SkillBlocked))
	}

	// Group recipes by output and select the most efficient for each
	type recipeWithEfficiency struct {
		recipeID   string
		name       string
		maxBatches int
		efficiency float64
	}

	// For now, we can't easily determine output items from the MCP response
	// The craft_query response doesn't include output information
	// So we'll just return all fully craftable recipes, sorted by quantity
	var craftable []recipeWithEfficiency
	for _, recipe := range result.FullyCraftable {
		if recipe.CanCraftQuantity <= 0 {
			continue
		}
		craftable = append(craftable, recipeWithEfficiency{
			recipeID:   recipe.RecipeID,
			name:       recipe.RecipeName,
			maxBatches: recipe.CanCraftQuantity,
			efficiency: 0, // Can't calculate without output info
		})
		logger.Printf("   ✓ %s: %s (can craft %d batches)",
			recipe.RecipeID, recipe.RecipeName, recipe.CanCraftQuantity)
	}

	if len(craftable) == 0 {
		logger.Printf("❌ No craftable recipes found with current cargo and skills")
		logger.Printf("   💡 To craft more items, you need to:")
		logger.Printf("      • Mine ores and deposit them to station storage")
		logger.Printf("      • Wait for ores to be withdrawn from storage")
		logger.Printf("      • Level up skills to unlock more recipes")
		return nil, nil
	}

	// Sort by max batches (most craftable first) - this is a simple heuristic
	// In the future, we could enhance the MCP server to return efficiency info
	sort.Slice(craftable, func(i, j int) bool {
		return craftable[i].maxBatches > craftable[j].maxBatches
	})

	// Return recipe IDs
	var recipeIDs []string
	for _, r := range craftable {
		recipeIDs = append(recipeIDs, r.recipeID)
	}

	logger.Printf("✅ Selected %d recipes to craft", len(recipeIDs))
	return recipeIDs, nil
}

// CraftingLoop runs the core crafting cycle:
//  1. Find nearest station
//  2. Dock at station if not docked
//  3. Withdraw resources from storage (if configured)
//  4. Craft items based on strategy
//  5. Handle crafted items (deposit/sell)
//  6. Refuel and repair as needed
//
// The loop continues until:
//   - Context is cancelled
//   - StopCondition returns true
//   - An error occurs (returns error)
func CraftingLoop(client GameClient, logger *log.Logger, ctx context.Context, config *CraftingLoopConfig) (*CraftingLoopResult, error) {
	// Apply defaults
	if config == nil {
		config = &CraftingLoopConfig{}
	}
	if config.Strategy == "" {
		config.Strategy = "craft-deposit" // Default strategy
	}
	if config.CaptainsLogInterval == 0 {
		config.CaptainsLogInterval = 2 * time.Minute
	}
	if config.RecipeSelector == nil {
		config.RecipeSelector = DefaultRecipeSelector
	}

	// Validate strategy
	if config.Strategy != "craft-deposit" && config.Strategy != "craft-sell" && config.Strategy != "craft-profit" {
		return nil, fmt.Errorf("invalid strategy: %s (must be craft-deposit, craft-sell, or craft-profit)", config.Strategy)
	}

	// Initialize result
	state := client.GetState()
	result := &CraftingLoopResult{
		StartingCredits: state.Credits,
	}

	// Main crafting loop
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			result.EndingCredits = client.GetState().Credits
			result.StoppedReason = "context_cancelled"
			return result, nil
		default:
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

		logger.Printf("═══ Crafting Run #%d ═══", result.RunsCompleted)
		logger.Printf("Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Cargo: %.1f/%.1f",
			state.Credits, state.Fuel, state.MaxFuel, state.Hull, state.MaxHull,
			state.Ship.CargoUsed, state.Ship.CargoCapacity)

		// Step 1: Get system data if needed
		if len(state.System.POIs) == 0 {
			logger.Printf("Fetching system data...")
			if err := client.GetSystem(ctx); err != nil {
				logger.Printf("Failed to get system: %v", err)
			}
			time.Sleep(2 * time.Second)
			state = client.GetState()
		}

		// Step 2: Find nearest station
		var stationPOI string
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

		logger.Printf("📍 Station found: %s | Current: %s | Docked: %v | Traveling: %v",
			stationPOI, state.CurrentPOI, state.Doc, state.Traveling)

		// Step 3: Ensure we're properly docked at the station
		atStation := state.Doc && state.CurrentPOI == stationPOI
		needsDocking := !atStation && !state.Traveling

		// Only dock if we're not already docked at the station
		if needsDocking {
			// Need to travel to station first
			if state.CurrentPOI != stationPOI && !state.Traveling {
				logger.Printf("🚀 Traveling to station %s...", stationPOI)
				if _, err := client.Travel(ctx, stationPOI); err != nil {
					logger.Printf("Travel error: %v", err)
				}
				time.Sleep(20 * time.Second)
			}

			// Dock at station
			state = client.GetState()
			atStation = state.Doc && state.CurrentPOI == stationPOI
			if !atStation && !state.Traveling {
				logger.Printf("📥 Docking at station...")
				if err := client.Dock(ctx); err != nil {
					if err.Error() != "Already docked (success)" {
						logger.Printf("Dock error: %v", err)
					}
				}
				time.Sleep(15 * time.Second)
			}
		} else {
			logger.Printf("✅ Already docked at station")
		}

		// Step 4: Deposit any existing cargo to free up space for crafted items
		// Note: Crafting uses items directly from storage, output goes to cargo
		atStation = state.Doc && state.CurrentPOI == stationPOI
		if atStation && len(state.Ship.Cargo) > 0 {
			logger.Printf("📦 Depositing existing cargo to free up space for crafted items...")
			if err := client.DepositAllItems(ctx); err != nil {
				logger.Printf("Deposit error: %v", err)
			} else {
				logger.Printf("✅ Deposited all items")
			}
			// Wait for server tick
			time.Sleep(SleepTick)
		}

		// Step 6: Select recipes and craft
		state = client.GetState()
		if !state.Doc {
			logger.Printf("⚠️  Not docked, skipping crafting")
			time.Sleep(5 * time.Second)
			continue
		}

		recipes, err := config.RecipeSelector(client, logger, ctx, config.StorageManager)
		if err != nil {
			logger.Printf("Recipe selector error: %v", err)
			time.Sleep(30 * time.Second)
			continue
		}

		if len(recipes) == 0 {
			logger.Printf("ℹ️  No craftable recipes, waiting for resources...")
			time.Sleep(30 * time.Second)
			continue
		}

		// Craft the selected recipes
		itemsCrafted := 0
		for _, recipeID := range recipes {
			crafted, err := craftRecipe(client, logger, ctx, recipeID)
			if err != nil {
				logger.Printf("Failed to craft %s: %v", recipeID, err)
			} else {
				itemsCrafted += crafted
				// Wait for server tick before next craft action
				time.Sleep(SleepTick)
			}
		}

		result.TotalItemsCrafted += itemsCrafted

		// Step 7: Handle crafted items based on strategy
		state = client.GetState()
		creditsBefore := state.Credits

		switch config.Strategy {
		case "craft-deposit", "craft-profit":
			// Deposit all items to storage
			if len(state.Ship.Cargo) > 0 {
				logger.Printf("📥 Depositing %d items to storage...", len(state.Ship.Cargo))
				if err := client.DepositAllItems(ctx); err != nil {
					logger.Printf("Deposit error: %v", err)
				} else {
					logger.Printf("✅ Deposited all items to storage")
				}
			}
		case "craft-sell":
			// Sell all items
			if len(state.Ship.Cargo) > 0 {
				logger.Printf("💰 Selling all cargo...")
				if err := client.SellAllBulk(ctx, nil); err != nil {
					logger.Printf("Sell error: %v", err)
				} else {
					logger.Printf("✅ Sold all cargo")
				}
			}
		}

		// Step 8: Refuel if needed
		state = client.GetState()
		if state.Doc && state.Fuel < state.MaxFuel*0.8 {
			logger.Printf("⛽ Refueling...")
			if err := client.Refuel(ctx); err != nil {
				logger.Printf("Refuel error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 9: Repair if needed
		state = client.GetState()
		if state.Doc && state.Hull < state.MaxHull*0.9 {
			logger.Printf("🔧 Repairing hull...")
			if err := client.Repair(ctx); err != nil {
				logger.Printf("Repair error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 10: Run completion callback
		state = client.GetState()
		_ = creditsBefore // Used for tracking credit changes (available for future use)

		logger.Printf("═══ Run #%d Complete ═══", result.RunsCompleted)
		logger.Printf("Items crafted: %d | Credits: %.2f", itemsCrafted, state.Credits)

		if config.OnRunComplete != nil {
			config.OnRunComplete(result.RunsCompleted, itemsCrafted, state.Credits)
		}

		logger.Printf("Next run in 5 seconds...\n")
		time.Sleep(5 * time.Second)
	}
}

// craftRecipe crafts a specific recipe, returns number of items crafted
func craftRecipe(client GameClient, logger *log.Logger, ctx context.Context, recipeID string) (int, error) {
	state := client.GetState()

	// Batch size is determined by the player's Crafting skill level.
	// Perform enough batches to craft ~200 items total per recipe per loop.
	batchSize := MaxCraftBatchSize(state)
	batchesPerLoop := max(1, 200/batchSize)
	totalItems := 0

	remainingCargo := state.Ship.CargoCapacity - state.Ship.CargoUsed
	if remainingCargo < float64(batchSize) {
		return 0, fmt.Errorf("not enough cargo space")
	}

	logger.Printf("🔨 Crafting %s (%d batches of %d each)...", recipeID, batchesPerLoop, batchSize)

	for i := 0; i < batchesPerLoop; i++ {
		// Check cargo space before each batch
		state = client.GetState()
		remainingCargo = state.Ship.CargoCapacity - state.Ship.CargoUsed
		if remainingCargo < float64(batchSize) {
			logger.Printf("⚠️  Cargo full after %d batches", i)
			break
		}

		if err := client.CraftWithQuantity(ctx, recipeID, batchSize); err != nil {
			logger.Printf("Failed batch %d: %v", i+1, err)
			return totalItems, fmt.Errorf("craft command failed on batch %d: %w", i+1, err)
		}

		totalItems += batchSize
		time.Sleep(SleepTick) // Wait for server tick between batches
	}

	logger.Printf("✅ Crafted %d total x %s", totalItems, recipeID)
	return totalItems, nil
}
