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
type RecipeSelector func(client *Client, logger *log.Logger, ctx context.Context, storage StorageManager) ([]string, error)

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
// 1. basic_smelt_iron (ore_iron -> iron_ingot)
// 2. basic_copper_processing (ore_copper -> copper_plate)
// When agents reach higher skill levels (crafting > 5, refinement > 5, crafting_advanced > 1),
// more advanced recipes will be unlocked
func DefaultRecipeSelector(client *Client, logger *log.Logger, ctx context.Context, storage StorageManager) ([]string, error) {
	state := client.GetState()

	// Build a map of available resources (cargo + storage)
	// Crafting uses items directly from storage, so we need to check both
	resourceMap := make(map[string]float64)

	// Add cargo items
	for _, item := range state.Ship.Cargo {
		resourceMap[item.ItemID] += item.Quantity
	}

	// Add storage items if storage manager is available
	if storage != nil {
		storageItems, err := storage.ViewStorage(ctx)
		if err != nil {
			logger.Printf("Warning: Failed to view storage: %v", err)
		} else {
			for itemID, qty := range storageItems {
				resourceMap[itemID] += qty
			}

			if len(storageItems) > 0 {
				logger.Printf("📦 Storage available: %d item types", len(storageItems))
				// Log first 10 items
				count := 0
				for itemID, qty := range storageItems {
					if count < 10 {
						logger.Printf("   • %s: %.0f units", itemID, qty)
						count++
					}
				}
				if len(storageItems) > 10 {
					logger.Printf("   ... and %d more item types", len(storageItems)-10)
				}
			}
		}
	}

	logger.Printf("📊 Resource Analysis (Cargo + Storage):")
	logger.Printf("   Total resources: %d item types", len(resourceMap))
	if len(resourceMap) > 0 {
		// Log first 15 items to avoid spam
		count := 0
		for itemID, qty := range resourceMap {
			if count < 15 {
				logger.Printf("   • %s: %.0f units", itemID, qty)
				count++
			}
		}
		if len(resourceMap) > 15 {
			logger.Printf("   ... and %d more item types", len(resourceMap)-15)
		}
	} else {
		logger.Printf("   (no resources available)")
	}

	// Get skill levels
	skills := getPlayerSkillLevels(state)

	logger.Printf("🎓 Skill Levels:")
	if len(skills) > 0 {
		for skill, level := range skills {
			logger.Printf("   • %s: level %d", skill, level)
		}
	} else {
		logger.Printf("   (no skills detected)")
	}

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
			Name:   "Basic Iron Smelting (no skill, 10 ore -> 1 steel)",
			RequiredInputs: map[string]float64{
				"ore_iron": 10,
			},
		},
		{
			ID:     "refine_steel",
			Name:   "Refine Steel (refinement 1+, 5 ore -> 2 steel) - 4x better!",
			RequiredInputs: map[string]float64{
				"ore_iron": 5,
			},
			RequiredRefining: 1,
		},
		{
			ID:     "basic_copper_processing",
			Name:   "Basic Copper Processing (no skill, 10 ore -> 1 copper)",
			RequiredInputs: map[string]float64{
				"ore_copper": 10,
			},
		},
		{
			ID:     "refine_copper_wire",
			Name:   "Process Copper Wiring (refinement 1+, 5 copper -> 1 wire)",
			RequiredInputs: map[string]float64{
				"refined_copper": 5,
			},
			RequiredRefining: 1,
		},
		{
			ID:     "smelt_aluminum_sheet",
			Name:   "Smelt Aluminum Sheet (refinement 1+, 10 ore -> 1 aluminum)",
			RequiredInputs: map[string]float64{
				"ore_aluminum": 10,
			},
			RequiredRefining: 1,
		},
	}

	// Check which recipes can be crafted with current cargo and skills
	var craftableRecipes []string

	logger.Printf("🔍 Checking %d recipes against current cargo and skills...", len(recipes))

	for _, recipe := range recipes {
		// Check skill requirements
		if recipe.RequiredCrafting > 0 {
			if skillLevel, ok := skills["crafting"]; !ok || skillLevel < recipe.RequiredCrafting {
				logger.Printf("   ✗ %s: needs crafting level %d (have %d)",
					recipe.Name, recipe.RequiredCrafting, skillLevel)
				continue
			}
		}
		if recipe.RequiredRefining > 0 {
			if skillLevel, ok := skills["refinement"]; !ok || skillLevel < recipe.RequiredRefining {
				logger.Printf("   ✗ %s: needs refinement level %d (have %d)",
					recipe.Name, recipe.RequiredRefining, skillLevel)
				continue
			}
		}
		if recipe.RequiredAdvanced > 0 {
			if skillLevel, ok := skills["crafting_advanced"]; !ok || skillLevel < recipe.RequiredAdvanced {
				logger.Printf("   ✗ %s: needs crafting_advanced level %d (have %d)",
					recipe.Name, recipe.RequiredAdvanced, skillLevel)
				continue
			}
		}

		// Check if we have the required inputs
		hasAllInputs := true
		maxBatches := 0
		missingInputs := []string{}

		for inputID, requiredQty := range recipe.RequiredInputs {
			availableQty, ok := resourceMap[inputID]
			if !ok {
				hasAllInputs = false
				missingInputs = append(missingInputs, fmt.Sprintf("%s (missing)", inputID))
				break
			} else if availableQty < requiredQty {
				hasAllInputs = false
				missingInputs = append(missingInputs, fmt.Sprintf("%s (%.0f/%.0f)", inputID, availableQty, requiredQty))
				break
			} else {
				possibleBatches := int(availableQty / requiredQty)
				if maxBatches == 0 || possibleBatches < maxBatches {
					maxBatches = possibleBatches
				}
			}
		}

		if !hasAllInputs {
			logger.Printf("   ✗ %s: missing inputs - %s",
				recipe.Name, missingInputs[0])
			continue
		}

		if hasAllInputs {
			craftableRecipes = append(craftableRecipes, recipe.ID)
			logger.Printf("   ✓ %s: %s - can craft %d batches (%.0f items)",
				recipe.Name, recipe.ID, maxBatches, float64(maxBatches*10))
		}
	}

	if len(craftableRecipes) == 0 {
		logger.Printf("❌ No craftable recipes found with current cargo and skills")
		logger.Printf("   💡 To craft more items, you need to:")
		logger.Printf("      • Mine ores and deposit them to station storage")
		logger.Printf("      • Wait for ores to be withdrawn from storage")
		logger.Printf("      • Level up skills to unlock more recipes")
		return nil, nil
	}

	logger.Printf("✅ Found %d craftable recipes!", len(craftableRecipes))
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

// withdrawOresForCrafting withdraws common ores from storage for crafting
func withdrawOresForCrafting(client *Client, logger *log.Logger, ctx context.Context, storage StorageManager) error {
	state := client.GetState()
	cargoCapacity := state.Ship.CargoCapacity - state.Ship.CargoUsed

	// Reserve 50 units of space for crafting output (each batch needs ~10 units for output)
	reserveSpace := 50.0
	availableForWithdrawal := cargoCapacity - reserveSpace

	logger.Printf("📦 Storage Withdrawal Check:")
	logger.Printf("   Cargo capacity: %.1f/%.1f (%.1f free, %.1f reserved for output)",
		state.Ship.CargoUsed, state.Ship.CargoCapacity, cargoCapacity, reserveSpace)

	if availableForWithdrawal < 10 {
		logger.Printf("⚠️  Not enough cargo capacity after reserve (%.1f), skipping withdrawal", availableForWithdrawal)
		return nil
	}

	// View storage to see what's available
	logger.Printf("🔍 Checking station storage for resources...")
	storageItems, err := storage.ViewStorage(ctx)
	if err != nil {
		return fmt.Errorf("failed to view storage: %w", err)
	}

	// Log storage contents
	if len(storageItems) == 0 {
		logger.Printf("📭 Storage is EMPTY - no items available for withdrawal")
		logger.Printf("   This means:")
		logger.Printf("   • No items have been deposited to this station's storage")
		logger.Printf("   • Or the storage data hasn't been updated yet")
		logger.Printf("   • Or you're at a different station than where resources were deposited")
	} else {
		logger.Printf("📦 Storage contains %d item types:", len(storageItems))
		// Log first 10 items to avoid spam
		count := 0
		for itemID, qty := range storageItems {
			if count < 10 {
				logger.Printf("   • %s: %.0f units", itemID, qty)
				count++
			}
		}
		if len(storageItems) > 10 {
			logger.Printf("   ... and %d more item types", len(storageItems)-10)
		}
	}

	// Ores we want to withdraw for crafting
	ores := []string{"ore_iron", "ore_copper", "ore_aluminum"}

	foundOres := 0
	for _, ore := range ores {
		if qty, ok := storageItems[ore]; ok && qty > 0 {
			foundOres++
			// Withdraw up to available capacity (minimum of available and capacity with reserve)
			withdrawQty := qty
			if withdrawQty > availableForWithdrawal {
				withdrawQty = availableForWithdrawal
			}

			if withdrawQty > 0 {
				logger.Printf("📤 Withdrawing %.0f %s from storage...", withdrawQty, ore)
				if err := storage.WithdrawItems(ctx, ore, withdrawQty); err != nil {
					logger.Printf("❌ Failed to withdraw %s: %v", ore, err)
				} else {
					logger.Printf("✅ Withdrew %.0f %s", withdrawQty, ore)
					time.Sleep(2 * time.Second)
				}
			}
		}
	}

	if foundOres == 0 {
		logger.Printf("⚠️  No crafting ores (ore_iron, ore_copper, ore_aluminum) found in storage")
		logger.Printf("   Will craft with current cargo instead")
	}

	return nil
}

// craftRecipe crafts a specific recipe, returns number of items crafted
func craftRecipe(client *Client, logger *log.Logger, ctx context.Context, recipeID string) (int, error) {
	state := client.GetState()

	// Craft in batches of 10 (max allowed per command)
	// Perform 20 batches per recipe per loop as requested
	batchSize := 10
	batchesPerLoop := 20
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
