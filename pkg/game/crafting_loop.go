package game

import (
	"context"
	"fmt"
	"log"
	"time"
)

// CraftingLoopConfig configures the behavior of the crafting loop
type CraftingLoopConfig struct {
	// AgentID for captain's log updates (optional)
	AgentID string

	// Strategy determines what to do with crafted items
	// "craft-deposit": Craft items and deposit to storage
	// "craft-sell": Craft items and sell for credits
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

// RecipeSelector selects recipes to craft based on current state
type RecipeSelector func(client *Client, logger *log.Logger, ctx context.Context) ([]string, error)

// StorageManager handles storage operations
type StorageManager interface {
	// WithdrawItems withdraws items from storage to cargo
	WithdrawItems(ctx context.Context, itemID string, quantity float64) error
	// DepositItems deposits items from cargo to storage
	DepositItems(ctx context.Context, itemID string, quantity float64) error
	// ViewStorage returns the current storage contents
	ViewStorage(ctx context.Context) (map[string]float64, error)
}

// DefaultRecipeSelector selects basic recipes based on available cargo and skills
// For now, it prioritizes:
// 1. basic_smelt_iron (iron_ore -> iron_ingot)
// 2. basic_copper_processing (copper_ore -> copper_plate)
// When agents reach higher skill levels (crafting > 5, refining > 5, crafting_advanced > 1),
// more advanced recipes will be unlocked
func DefaultRecipeSelector(client *Client, logger *log.Logger, ctx context.Context) ([]string, error) {
	state := client.GetState()

	// Build a map of available cargo items
	cargoMap := make(map[string]float64)
	for _, item := range state.Ship.Cargo {
		cargoMap[item.ItemID] = item.Quantity
	}

	// Get skill levels
	skills := getPlayerSkillLevels(state)

	// Define available recipes with their requirements
	type Recipe struct {
		ID               string
		Name             string
		RequiredInputs   map[string]float64
		RequiredSkills   map[string]int
		RequiredCrafting int
		RequiredRefining int
		RequiredAdvanced int
	}

	recipes := []Recipe{
		{
			ID:     "basic_smelt_iron",
			Name:   "Basic Iron Smelting",
			RequiredInputs: map[string]float64{
				"iron_ore": 10,
			},
		},
		{
			ID:     "basic_copper_processing",
			Name:   "Basic Copper Processing",
			RequiredInputs: map[string]float64{
				"copper_ore": 10,
			},
		},
		{
			ID:     "refine_copper_wire",
			Name:   "Process Copper Wiring",
			RequiredInputs: map[string]float64{
				"copper_plate": 5,
			},
			RequiredRefining: 1,
		},
		{
			ID:     "smelt_aluminum_sheet",
			Name:   "Smelt Aluminum Sheet",
			RequiredInputs: map[string]float64{
				"aluminum_ore": 10,
			},
			RequiredRefining: 1,
		},
	}

	// Check which recipes can be crafted with current cargo and skills
	var craftableRecipes []string

	for _, recipe := range recipes {
		// Check skill requirements
		if recipe.RequiredCrafting > 0 {
			if skillLevel, ok := skills["crafting"]; !ok || skillLevel < recipe.RequiredCrafting {
				logger.Printf("Recipe %s requires crafting level %d", recipe.Name, recipe.RequiredCrafting)
				continue
			}
		}
		if recipe.RequiredRefining > 0 {
			if skillLevel, ok := skills["refining"]; !ok || skillLevel < recipe.RequiredRefining {
				logger.Printf("Recipe %s requires refining level %d", recipe.Name, recipe.RequiredRefining)
				continue
			}
		}
		if recipe.RequiredAdvanced > 0 {
			if skillLevel, ok := skills["crafting_advanced"]; !ok || skillLevel < recipe.RequiredAdvanced {
				logger.Printf("Recipe %s requires crafting_advanced level %d", recipe.Name, recipe.RequiredAdvanced)
				continue
			}
		}

		// Check if we have the required inputs
		hasAllInputs := true
		for inputID, requiredQty := range recipe.RequiredInputs {
			if availableQty, ok := cargoMap[inputID]; !ok || availableQty < requiredQty {
				hasAllInputs = false
				logger.Printf("Recipe %s needs %f of %s, have %f", recipe.Name, requiredQty, inputID, availableQty)
				break
			}
		}

		if hasAllInputs {
			craftableRecipes = append(craftableRecipes, recipe.ID)
			logger.Printf("✓ Can craft %s (%s)", recipe.Name, recipe.ID)
		}
	}

	if len(craftableRecipes) == 0 {
		logger.Printf("No craftable recipes found with current cargo and skills")
		return nil, nil
	}

	return craftableRecipes, nil
}

// getPlayerSkillLevels extracts skill levels from state
func getPlayerSkillLevels(state *State) map[string]int {
	skills := make(map[string]int)

	// First, try to use actual skill levels from Player.Skills if available
	if len(state.Player.Skills) > 0 {
		for skillID, playerSkill := range state.Player.Skills {
			skills[skillID] = playerSkill.Level
		}
	} else {
		// Fallback: use XP to estimate level
		for skillID, skillXP := range state.SkillXP {
			level := xpToLevel(int(skillXP))
			skills[skillID] = level
		}
	}

	return skills
}

// xpToLevel converts skill XP to an approximate level
func xpToLevel(xp int) int {
	if xp < 100 {
		return 1
	} else if xp < 300 {
		return 2
	} else if xp < 600 {
		return 3
	} else if xp < 1000 {
		return 4
	} else {
		return 5
	}
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
func CraftingLoop(client *Client, logger *log.Logger, ctx context.Context, config *CraftingLoopConfig) (*CraftingLoopResult, error) {
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
	if config.Strategy != "craft-deposit" && config.Strategy != "craft-sell" {
		return nil, fmt.Errorf("invalid strategy: %s (must be craft-deposit or craft-sell)", config.Strategy)
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
		// Even if state.Doc is true, we might need to re-dock to access storage
		atStation := state.Doc && state.CurrentPOI == stationPOI
		needsDocking := !atStation && !state.Traveling

		// Always try to dock if at the station POI to ensure proper docked state
		if state.CurrentPOI == stationPOI && !state.Traveling {
			logger.Printf("📥 Ensuring proper docked state at station...")
			if err := client.Dock(ctx); err != nil && err.Error() != "Already docked (success)" {
				logger.Printf("Dock refresh error: %v", err)
			} else {
				logger.Printf("✅ Docked successfully")
			}
			time.Sleep(3 * time.Second)
			state = client.GetState()
		} else if needsDocking {
			// Need to travel to station first
			if state.CurrentPOI != stationPOI && !state.Traveling {
				logger.Printf("🚀 Traveling to station %s...", stationPOI)
				if err := client.Travel(ctx, stationPOI); err != nil {
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
		}

		// Step 4: Withdraw resources from storage (if configured)
		atStation = state.Doc && state.CurrentPOI == stationPOI
		if config.StorageManager != nil && atStation {
			if err := withdrawOresForCrafting(client, logger, ctx, config.StorageManager); err != nil {
				logger.Printf("Failed to withdraw ores: %v", err)
			}
		}

		// Step 5: Select recipes and craft
		state = client.GetState()
		if !state.Doc {
			logger.Printf("⚠️  Not docked, skipping crafting")
			time.Sleep(5 * time.Second)
			continue
		}

		recipes, err := config.RecipeSelector(client, logger, ctx)
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
			}
		}

		result.TotalItemsCrafted += itemsCrafted

		// Step 6: Handle crafted items based on strategy
		state = client.GetState()
		creditsBefore := state.Credits

		switch config.Strategy {
		case "craft-deposit":
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

		// Step 7: Refuel if needed
		state = client.GetState()
		if state.Doc && state.Fuel < state.MaxFuel*0.8 {
			logger.Printf("⛽ Refueling...")
			if err := client.Refuel(ctx); err != nil {
				logger.Printf("Refuel error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 8: Repair if needed
		state = client.GetState()
		if state.Doc && state.Hull < state.MaxHull*0.9 {
			logger.Printf("🔧 Repairing hull...")
			if err := client.Repair(ctx); err != nil {
				logger.Printf("Repair error: %v", err)
			}
			time.Sleep(3 * time.Second)
		}

		// Step 9: Run completion callback
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

// withdrawOresForCrafting withdraws common ores from storage for crafting
func withdrawOresForCrafting(client *Client, logger *log.Logger, ctx context.Context, storage StorageManager) error {
	state := client.GetState()
	cargoCapacity := state.Ship.CargoCapacity - state.Ship.CargoUsed

	if cargoCapacity < 10 {
		logger.Printf("⚠️  Not enough cargo capacity (%.1f)", cargoCapacity)
		return nil
	}

	// View storage to see what's available
	storageItems, err := storage.ViewStorage(ctx)
	if err != nil {
		return fmt.Errorf("failed to view storage: %w", err)
	}

	// Ores we want to withdraw for crafting
	ores := []string{"iron_ore", "copper_ore", "aluminum_ore"}

	for _, ore := range ores {
		if qty, ok := storageItems[ore]; ok && qty > 0 {
			// Withdraw up to cargo capacity (minimum of available and capacity)
			withdrawQty := qty
			if withdrawQty > cargoCapacity {
				withdrawQty = cargoCapacity
			}

			if withdrawQty > 0 {
				logger.Printf("📤 Withdrawing %.0f %s from storage...", withdrawQty, ore)
				if err := storage.WithdrawItems(ctx, ore, withdrawQty); err != nil {
					logger.Printf("Failed to withdraw %s: %v", ore, err)
				} else {
					logger.Printf("✅ Withdrew %.0f %s", withdrawQty, ore)
					time.Sleep(2 * time.Second)
				}
			}
		}
	}

	return nil
}

// craftRecipe crafts a specific recipe, returns number of items crafted
func craftRecipe(client *Client, logger *log.Logger, ctx context.Context, recipeID string) (int, error) {
	state := client.GetState()

	// For now, craft in batches of 10 (max allowed)
	// TODO: Calculate optimal batch size based on available cargo
	batchSize := 10
	remainingCargo := state.Ship.CargoCapacity - state.Ship.CargoUsed
	if remainingCargo < 5 {
		return 0, fmt.Errorf("not enough cargo space")
	}

	logger.Printf("🔨 Crafting %s (batch size: %d)...", recipeID, batchSize)

	if err := client.CraftWithQuantity(ctx, recipeID, batchSize); err != nil {
		return 0, fmt.Errorf("craft command failed: %w", err)
	}

	logger.Printf("✅ Crafted %d x %s", batchSize, recipeID)
	return batchSize, nil
}
