package main

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// ProfitBasedRecipeSelector selects recipes based on market profitability
// It analyzes current market buy listings to determine which crafted items
// will generate the most profit when sold, prioritizing high-margin items.
func ProfitBasedRecipeSelector(kb knowledge.Base) game.RecipeSelector {
	return func(client *game.Client, logger *log.Logger, ctx context.Context, storage game.StorageManager) ([]string, error) {
		state := client.GetState()

		// Refresh market data to ensure we have current prices
		snapshot, err := agent.RefreshMarketData(ctx, client, kb, state.Player.Username)
		if err != nil {
			logger.Printf("Warning: Failed to refresh market data: %v (using cached data if available)", err)
			// Try to get cached data anyway
			snapshot, err = kb.GetLatestMarketSnapshot(ctx, state.System.ID, state.CurrentPOI)
			if err != nil {
				return nil, fmt.Errorf("failed to get market data: %w", err)
			}
		}

		if snapshot == nil {
			return nil, fmt.Errorf("no market data available")
		}

		// Get current market listings
		listings := snapshot.Listings
		if len(listings) == 0 {
			return nil, fmt.Errorf("no market listings available")
		}

		// Build a map of available resources (cargo + storage)
		resourceMap := make(map[string]float64)
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
			}
		}

		// Get available skills
		skills := state.Player.Skills

		// Score each recipe based on profitability
		type recipeScore struct {
			recipeID    string
			profit      float64
			marginPct   float64
			materialCost float64
			sellPrice   float64
		}

		var scoredRecipes []recipeScore

		// Define recipes with their required materials and output
		recipes := getProfitableRecipes()

		for _, recipe := range recipes {
			// Check if agent has required skills
			requiredSkill := recipe.requiredSkill
			if requiredSkill != "" {
				skillLevel, hasSkill := skills[requiredSkill]
				if !hasSkill || skillLevel.Level < recipe.requiredSkillLevel {
					continue // Skip if don't have required skill level
				}
			}

			// Check if we have required materials
			hasMaterials := true
			materialCost := 0.0

			for _, material := range recipe.materials {
				availableQty, hasResource := resourceMap[material.itemID]
				if !hasResource || availableQty < material.quantity {
					hasMaterials = false
					break
				}
				// We'll buy materials at the lowest buy price
				materialCost += getBuyPrice(listings, material.itemID, material.quantity)
			}

			if !hasMaterials {
				continue
			}

			// Get sell price for crafted item
			sellPrice := getSellPrice(listings, recipe.outputItemID, recipe.outputQuantity)
			if sellPrice == 0 {
				continue // No market data for this item
			}

			profit := sellPrice - materialCost
			marginPct := (profit / materialCost) * 100

			// Only consider profitable recipes with decent margin (> 5%)
			if profit > 0 && marginPct > 5 {
				scoredRecipes = append(scoredRecipes, recipeScore{
					recipeID:    recipe.recipeID,
					profit:      profit,
					marginPct:   marginPct,
					materialCost: materialCost,
					sellPrice:   sellPrice,
				})
			}
		}

		if len(scoredRecipes) == 0 {
			logger.Printf("💰 No profitable recipes found with current market conditions")
			return []string{}, nil
		}

		// Sort by profit (descending)
		sort.Slice(scoredRecipes, func(i, j int) bool {
			// Primary sort: profit
			if scoredRecipes[i].profit != scoredRecipes[j].profit {
				return scoredRecipes[i].profit > scoredRecipes[j].profit
			}
			// Secondary sort: margin percentage
			return scoredRecipes[i].marginPct > scoredRecipes[j].marginPct
		})

		// Log top opportunities
		logger.Printf("💰 Found %d profitable recipes:", len(scoredRecipes))
		for i, sr := range scoredRecipes {
			if i >= 5 {
				break // Log top 5
			}
			logger.Printf("   %d. %s: profit %.0f (margin %.1f%%, cost %.0f, sell %.0f)",
				i+1, sr.recipeID, sr.profit, sr.marginPct, sr.materialCost, sr.sellPrice)
		}

		// Return top recipe IDs (can craft multiple if profitable)
		var recipeIDs []string
		for _, sr := range scoredRecipes {
			recipeIDs = append(recipeIDs, sr.recipeID)
		}

		return recipeIDs, nil
	}
}

// recipeMaterials defines the materials needed for a recipe
type recipeMaterials struct {
	itemID   string
	quantity float64
}

// profitableRecipe defines a recipe that can be crafted for profit
type profitableRecipe struct {
	recipeID           string
	outputItemID       string
	outputQuantity     float64
	materials          []recipeMaterials
	requiredSkill      string
	requiredSkillLevel int
}

// getProfitableRecipes returns all known profitable crafting recipes
func getProfitableRecipes() []profitableRecipe {
	return []profitableRecipe{
		// Basic recipes (no skills required)
		{
			recipeID:       "basic_smelt_iron",
			outputItemID:   "iron_ingot",
			outputQuantity: 1,
			materials:      []recipeMaterials{{"ore_iron", 1}},
		},
		{
			recipeID:       "basic_copper_processing",
			outputItemID:   "copper_plate",
			outputQuantity: 1,
			materials:      []recipeMaterials{{"ore_copper", 1}},
		},

		// Refinement level 1 recipes
		{
			recipeID:           "refine_copper_wire",
			outputItemID:       "copper_wire",
			outputQuantity:     1,
			materials:          []recipeMaterials{{"copper_plate", 1}},
			requiredSkill:      "refinement",
			requiredSkillLevel: 1,
		},
		{
			recipeID:           "smelt_aluminum_sheet",
			outputItemID:       "aluminum_sheet",
			outputQuantity:     1,
			materials:          []recipeMaterials{{"ore_aluminum", 1}},
			requiredSkill:      "refinement",
			requiredSkillLevel: 1,
		},

		// Crafting basic level 5 recipes
		{
			recipeID:           "craft_steel_plate",
			outputItemID:       "steel_plate",
			outputQuantity:     1,
			materials:          []recipeMaterials{{"iron_ingot", 2}},
			requiredSkill:      "crafting_basic",
			requiredSkillLevel: 5,
		},
	}
}

// getBuyPrice finds the lowest buy price for an item (what we pay to buy materials)
func getBuyPrice(listings []knowledge.MarketListing, itemID string, quantity float64) float64 {
	var bestPrice float64
	found := false

	for _, listing := range listings {
		// We buy from sell listings (others selling to us)
		if listing.Type == "sell" && listing.ItemID == itemID {
			if !found || listing.PricePerUnit < bestPrice {
				bestPrice = listing.PricePerUnit
				found = true
			}
		}
	}

	if !found {
		return 0
	}

	return bestPrice * quantity
}

// getSellPrice finds the highest buy price for an item (what we earn when selling)
func getSellPrice(listings []knowledge.MarketListing, itemID string, quantity float64) float64 {
	var bestPrice float64
	found := false

	for _, listing := range listings {
		// We sell to buy listings (others buying from us)
		if listing.Type == "buy" && listing.ItemID == itemID {
			if !found || listing.PricePerUnit > bestPrice {
				bestPrice = listing.PricePerUnit
				found = true
			}
		}
	}

	if !found {
		return 0
	}

	return bestPrice * quantity
}
