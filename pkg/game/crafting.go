package game

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// CraftingConfig configures the crafting integration
type CraftingConfig struct {
	// Path to the crafting MCP server executable
	CraftingServerPath string
	// If empty, assumes "crafting-server" in PATH
}

// CraftableRecipe represents a recipe that can be crafted
type CraftableRecipe struct {
	RecipeID    string   `json:"recipe_id"`
	RecipeName  string   `json:"recipe_name"`
	CanCraft    bool     `json:"can_craft"`
	Components  []Component `json:"components"`
	SkillGaps   []string `json:"skill_gaps,omitempty"`
	Profit      float64  `json:"estimated_profit,omitempty"`
}

// Component represents a crafting component
type Component struct {
	ID       string  `json:"id"`
	Quantity float64 `json:"quantity"`
}

// CraftQueryResult is the response from craft_query
type CraftQueryResult struct {
	FullyCraftable []CraftableRecipe `json:"fully_craftable"`
	PartialMatches  []CraftableRecipe `json:"partial_matches"`
	SkillBlocked    []CraftableRecipe `json:"skill_blocked"`
}

// Craft executes a crafting command for a specific recipe
// The recipe_id should match the format expected by the game server
func (c *Client) Craft(ctx context.Context, recipeCommand string) error {
	// recipeCommand should be something like "craft basic_smelt_iron"
	// The game server expects this as a message type
	parts := strings.SplitN(recipeCommand, " ", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid craft command format: %s (expected 'craft <recipe_id>')", recipeCommand)
	}

	recipeID := parts[1]

	if err := c.Send(ctx, protocol.Message{
		Type:      "craft",
		Payload:   map[string]any{"recipe_id": recipeID},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		return err
	}

	return c.waitForActionResponse(ctx, 10*time.Second) // Crafting can take longer
}

// QueryCraftableRecipes queries the crafting MCP server to find what can be crafted
// with the current cargo and skills
func (c *Client) QueryCraftableRecipes(ctx context.Context, config *CraftingConfig) (*CraftQueryResult, error) {
	state := c.GetState()

	// Build components list from cargo
	components := make([]Component, 0, len(state.Ship.Cargo))
	for _, item := range state.Ship.Cargo {
		components = append(components, Component{
			ID:       item.ItemID,
			Quantity: item.Quantity,
		})
	}

	// Build skills map from state
	skills := make(map[string]int)
	for skillID, skillXP := range state.SkillXP {
		// Convert XP to level (simplified - you may want to use actual level data)
		level := c.xpToLevel(int(skillXP))
		skills[skillID] = level
	}

	// Call crafting MCP server
	result, err := c.callCraftingServer(ctx, config, components, skills)
	if err != nil {
		return nil, fmt.Errorf("crafting server query failed: %w", err)
	}

	return result, nil
}

// callCraftingServer invokes the crafting MCP server via stdio
func (c *Client) callCraftingServer(ctx context.Context, config *CraftingConfig, components []Component, skills map[string]int) (*CraftQueryResult, error) {
	// Determine server path
	serverPath := config.CraftingServerPath
	if serverPath == "" {
		// Use default path
		serverPath = "crafting-server"
		_ = serverPath // Will be used when MCP client is implemented
	}

	// TODO: Implement stdio MCP client communication
	// For now, return empty result
	return &CraftQueryResult{
		FullyCraftable: []CraftableRecipe{},
		PartialMatches:  []CraftableRecipe{},
		SkillBlocked:    []CraftableRecipe{},
	}, nil
}

// xpToLevel converts skill XP to an approximate level
// This is a simplified calculation - the real formula may be different
func (c *Client) xpToLevel(xp int) int {
	// Simplified XP to level conversion
	// Level 1: 0 XP, Level 2: 100 XP, Level 3: 300 XP, etc.
	// This is a rough approximation
	if xp < 100 {
		return 1
	} else if xp < 300 {
		return 2
	} else if xp < 600 {
		return 3
	} else if xp < 1000 {
		return 4
	} else {
		return 5 // Cap at level 5 for now
	}
}

// CraftFromCargo automatically crafts items from available cargo resources
// Returns the number of items successfully crafted
func (c *Client) CraftFromCargo(ctx context.Context, logger *log.Logger, config *CraftingConfig) (int, error) {
	// Query what can be crafted
	result, err := c.QueryCraftableRecipes(ctx, config)
	if err != nil {
		return 0, err
	}

	// Craft all fully craftable items
	crafted := 0
	for _, recipe := range result.FullyCraftable {
		logger.Printf("🔨 Crafting %s...", recipe.RecipeName)

		// Execute craft command
		craftCmd := fmt.Sprintf("craft %s", recipe.RecipeID)
		if err := c.Craft(ctx, craftCmd); err != nil {
			logger.Printf("⚠️  Failed to craft %s: %v", recipe.RecipeName, err)
		} else {
			crafted++
			logger.Printf("✅ Crafted %s!", recipe.RecipeName)
			time.Sleep(3 * time.Second) // Wait for crafting to complete
		}
	}

	if len(result.PartialMatches) > 0 {
		logger.Printf("ℹ️  Found %d partial matches (not enough components)", len(result.PartialMatches))
	}

	if len(result.SkillBlocked) > 0 {
		logger.Printf("ℹ️  Found %d skill-blocked recipes (need higher skills)", len(result.SkillBlocked))
	}

	return crafted, nil
}
