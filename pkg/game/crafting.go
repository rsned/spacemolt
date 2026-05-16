package game

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// MaxCraftBatchSize returns the maximum number of items that can be crafted
// in a single craft command based on the player's Crafting skill level.
// The server allows batch size equal to the player's crafting skill level,
// with a minimum of 1 (for skill level 0 or 1).
func MaxCraftBatchSize(state *State) int {
	if state == nil {
		return 1
	}
	if skill, ok := state.Player.Skills["crafting"]; ok && skill.Level > 1 {
		return skill.Level
	}
	return 1
}

// CraftingConfig configures the crafting integration
type CraftingConfig struct {
	// Path to the crafting MCP server executable
	CraftingServerPath string
	// If empty, assumes "crafting-server" in PATH
}

// Global MCP manager (shared across all clients)
var globalMCPManager *MCPManager
var mcpManagerOnce sync.Once

// getMCPManager gets or creates the global MCP manager
func getMCPManager(logger *log.Logger) *MCPManager {
	mcpManagerOnce.Do(func() {
		globalMCPManager = NewMCPManager(logger)
	})
	return globalMCPManager
}

// CraftableRecipe represents a recipe that can be crafted
type CraftableRecipe struct {
	RecipeID          string     `json:"id"`
	RecipeName        string     `json:"name"`
	CanCraftQuantity  int        `json:"can_craft_quantity"` // How many can be crafted
	Components        []Component `json:"components"`        // Required components
	CanCraft          bool        `json:"-"`
	SkillGaps         []string    `json:"-"`
	Profit            float64     `json:"-"`
}

// Component represents a crafting component
type Component struct {
	ID       string  `json:"id"`
	Quantity float64 `json:"quantity"`
}

// MCPCraftQueryResponse is the raw response from the crafting MCP server
type MCPCraftQueryResponse struct {
	Craftable []struct {
		Recipe struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
			CraftTimeSec int   `json:"craft_time_sec"`
			Components []struct {
				ComponentID string  `json:"component_id"`
				Quantity    float64 `json:"quantity"`
			} `json:"components"`
		} `json:"recipe"`
		CanCraftQuantity int `json:"can_craft_quantity"`
	} `json:"craftable"`
	PartialComponents []struct {
		Recipe struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
		} `json:"recipe"`
	} `json:"partial_components"`
	BlockedBySkills []struct {
		Recipe struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"recipe"`
	} `json:"blocked_by_skills"`
}

// CraftQueryResult is the simplified response from craft_query
type CraftQueryResult struct {
	FullyCraftable []CraftableRecipe `json:"fully_craftable"`
	PartialMatches  []CraftableRecipe `json:"partial_matches"`
	SkillBlocked    []CraftableRecipe `json:"skill_blocked"`
}

// Craft executes a crafting command for a specific recipe
// The recipe_id should match the format expected by the game server
func (c *Client) Craft(ctx context.Context, recipeCommand string) error {
	return c.CraftWithQuantity(ctx, recipeCommand, 1)
}

// CraftWithQuantity executes a crafting command with a specific quantity.
// The maximum batch size is determined by the player's Crafting skill level
// (minimum 1 for skill level 0 or 1).
func (c *Client) CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error {
	maxBatch := MaxCraftBatchSize(c.GetState())
	if quantity < 1 || quantity > maxBatch {
		return fmt.Errorf("invalid quantity: %d (must be 1-%d based on crafting skill)", quantity, maxBatch)
	}

	payload := map[string]any{"recipe_id": recipeID}
	if quantity > 1 {
		payload["quantity"] = quantity
	}

	msg := protocol.Message{
		Type:      "craft",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Server sends: ok (pending) → action_result with command="craft".
	// Submit's exact request_id correlation means intermediate events
	// (e.g. skill_level_up) carrying different request_ids pass through
	// to push subscribers without disturbing the mutation wait.
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// QueryCraftableRecipes queries the crafting MCP server to find what can be crafted
// with the current cargo and skills.
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

	return c.QueryCraftableFromComponents(ctx, components, config)
}

// QueryCraftableFromComponents queries the crafting MCP server to find what can
// be crafted from an explicit list of components and the player's current skills.
func (c *Client) QueryCraftableFromComponents(ctx context.Context, components []Component, config *CraftingConfig) (*CraftQueryResult, error) {
	state := c.GetState()

	// Build skills map from state
	skills := make(map[string]int)

	// First, try to use actual skill levels from Player.Skills if available
	if len(state.Player.Skills) > 0 {
		for skillID, playerSkill := range state.Player.Skills {
			skills[skillID] = playerSkill.Level
			c.debugLogger.Printf("Skill: %s = level %d (from player skills)", skillID, playerSkill.Level)
		}
	} else {
		// Fallback: use XP to estimate level (simplified)
		for skillID, skillXP := range state.SkillXP {
			level := c.xpToLevel(int(skillXP))
			skills[skillID] = level
			c.debugLogger.Printf("Skill: %s = level %d (estimated from %d XP)", skillID, level, int(skillXP))
		}
	}

	// Call crafting MCP server
	result, err := c.callCraftingServer(ctx, config, components, skills)
	if err != nil {
		return nil, fmt.Errorf("crafting server query failed: %w", err)
	}

	// Log what we got back
	c.debugLogger.Printf("Crafting query result: %d fully craftable, %d partial matches, %d skill blocked",
		len(result.FullyCraftable), len(result.PartialMatches), len(result.SkillBlocked))

	return result, nil
}

// callCraftingServer invokes the crafting MCP server via stdio
func (c *Client) callCraftingServer(ctx context.Context, config *CraftingConfig, components []Component, skills map[string]int) (*CraftQueryResult, error) {
	// Determine server path
	serverPath := ""
	if config != nil {
		serverPath = config.CraftingServerPath
	}
	if serverPath == "" {
		serverPath = "crafting-server"
	}

	// Get MCP client
	mgr := getMCPManager(c.debugLogger)
	client, err := mgr.GetClient(ctx, serverPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCP client: %w", err)
	}

	// Build craft_query request
	params := map[string]interface{}{
		"components":           components,
		"skills":               skills,
		"include_partial":      true,
		"min_match_ratio":      0.25,
		"optimization_strategy": "USE_INVENTORY_FIRST",
	}

	// Call the craft_query tool
	result, err := client.CallTool(ctx, "craft_query", params)
	if err != nil {
		return nil, fmt.Errorf("craft_query tool call failed: %w", err)
	}

	// Parse the result - it comes as text in the content field
	contentBytes, ok := result["content"].([]interface{})
	if !ok || len(contentBytes) == 0 {
		return &CraftQueryResult{
			FullyCraftable: []CraftableRecipe{},
			PartialMatches:  []CraftableRecipe{},
			SkillBlocked:    []CraftableRecipe{},
		}, nil
	}

	// The first content block should be a map with type and text
	contentBlock, ok := contentBytes[0].(map[string]interface{})
	if !ok {
		// Try old format (direct string)
		contentText, ok := contentBytes[0].(string)
		if !ok {
			return nil, fmt.Errorf("unexpected content format: expected map or string")
		}
		// Parse old format
		var craftResult CraftQueryResult
		if err := json.Unmarshal([]byte(contentText), &craftResult); err != nil {
			c.debugLogger.Printf("Failed to parse craft query result: %v", err)
			return &CraftQueryResult{}, nil
		}
		return &craftResult, nil
	}

	// New format: extract text from content block
	contentText, ok := contentBlock["text"].(string)
	if !ok {
		return nil, fmt.Errorf("unexpected content format: missing text field")
	}

	// Parse the JSON text into MCPCraftQueryResponse
	var mcpResponse MCPCraftQueryResponse
	if err := json.Unmarshal([]byte(contentText), &mcpResponse); err != nil {
		// If parsing fails, return empty result
		c.debugLogger.Printf("Failed to parse craft query result: %v", err)
		return &CraftQueryResult{
			FullyCraftable: []CraftableRecipe{},
			PartialMatches:  []CraftableRecipe{},
			SkillBlocked:    []CraftableRecipe{},
		}, nil
	}

	// Convert MCP response to simplified format
	craftResult := &CraftQueryResult{
		FullyCraftable: make([]CraftableRecipe, 0, len(mcpResponse.Craftable)),
		PartialMatches:  make([]CraftableRecipe, 0),
		SkillBlocked:    make([]CraftableRecipe, 0, len(mcpResponse.BlockedBySkills)),
	}

	for _, craftable := range mcpResponse.Craftable {
		if craftable.CanCraftQuantity <= 0 {
			continue
		}
		// Convert components from MCP format to our format
		components := make([]Component, 0, len(craftable.Recipe.Components))
		for _, comp := range craftable.Recipe.Components {
			components = append(components, Component{
				ID:       comp.ComponentID,
				Quantity: comp.Quantity,
			})
		}

		craftResult.FullyCraftable = append(craftResult.FullyCraftable, CraftableRecipe{
			RecipeID:         craftable.Recipe.ID,
			RecipeName:       craftable.Recipe.Name,
			CanCraftQuantity: craftable.CanCraftQuantity,
			Components:       components,
		})
	}

	for _, partial := range mcpResponse.PartialComponents {
		craftResult.PartialMatches = append(craftResult.PartialMatches, CraftableRecipe{
			RecipeID:   partial.Recipe.ID,
			RecipeName: partial.Recipe.Name,
		})
	}

	for _, blocked := range mcpResponse.BlockedBySkills {
		craftResult.SkillBlocked = append(craftResult.SkillBlocked, CraftableRecipe{
			RecipeID:   blocked.Recipe.ID,
			RecipeName: blocked.Recipe.Name,
		})
	}

	return craftResult, nil
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

// getStorageItems parses the raw JSON stored by the view_storage response
// into a map of item ID to quantity.
func (c *Client) getStorageItems() map[string]float64 {
	raw := c.GetRawJSON("storage")
	if raw == nil {
		return nil
	}

	var resp struct {
		Items []CargoItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		c.debugLogger.Printf("Failed to parse storage JSON: %v", err)
		return nil
	}

	items := make(map[string]float64, len(resp.Items))
	for _, item := range resp.Items {
		items[item.ItemID] = item.Quantity
	}
	return items
}

// sleepCtx sleeps for the given duration but returns early if the context
// is cancelled. Returns ctx.Err() if cancelled, nil otherwise.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// CraftItems crafts items using materials from station storage and ship cargo.
// It deposits current cargo to storage, queries craftable recipes from the
// combined pool, withdraws raw materials, crafts, and deposits outputs.
// Repeats until nothing more can be crafted or the safety cap (10 iterations)
// is reached. Returns the total number of items crafted.
func (c *Client) CraftItems(ctx context.Context, logger *log.Logger, config *CraftingConfig) (int, error) {
	const maxIterations = 10
	totalCrafted := 0

	for iteration := range maxIterations {
		if err := ctx.Err(); err != nil {
			return totalCrafted, err
		}

		logger.Printf("═══ Storage craft iteration %d ═══", iteration+1)

		// Step 1: Deposit all cargo to free workspace
		state := c.GetState()
		if len(state.Ship.Cargo) > 0 {
			logger.Printf("📦 Depositing cargo to free space...")
			if err := c.DepositAllItems(ctx); err != nil {
				logger.Printf("⚠️  Deposit failed: %v", err)
			} else if err := sleepCtx(ctx, SleepTick); err != nil {
				return totalCrafted, err
			}
		}

		// Step 2: View storage to get current contents
		if err := c.ViewStorage(ctx); err != nil {
			return totalCrafted, fmt.Errorf("view storage failed: %w", err)
		}
		if err := sleepCtx(ctx, SleepQuick); err != nil {
			return totalCrafted, err
		}

		storageItems := c.getStorageItems()
		if len(storageItems) == 0 {
			logger.Printf("ℹ️  Storage is empty, nothing to craft")
			break
		}

		logger.Printf("📦 Storage contains %d item types", len(storageItems))

		// Step 3: Build components from storage and query craftable recipes
		components := make([]Component, 0, len(storageItems))
		for itemID, qty := range storageItems {
			components = append(components, Component{ID: itemID, Quantity: qty})
		}

		result, err := c.QueryCraftableFromComponents(ctx, components, config)
		if err != nil {
			return totalCrafted, fmt.Errorf("craft query failed: %w", err)
		}

		if len(result.FullyCraftable) == 0 {
			logger.Printf("ℹ️  No craftable recipes from storage contents")
			break
		}

		logger.Printf("🎯 Found %d craftable recipes from storage", len(result.FullyCraftable))

		// Step 4: For each recipe, withdraw materials, craft, deposit outputs
		iterationCrafted := 0
		for _, recipe := range result.FullyCraftable {
			if recipe.CanCraftQuantity <= 0 {
				continue
			}
			if err := ctx.Err(); err != nil {
				return totalCrafted + iterationCrafted, err
			}

			logger.Printf("🔨 Crafting %s (can craft %d)...", recipe.RecipeName, recipe.CanCraftQuantity)

			// Withdraw needed components from storage
			for _, comp := range recipe.Components {
				if err := ctx.Err(); err != nil {
					return totalCrafted + iterationCrafted, err
				}

				needed := comp.Quantity * float64(recipe.CanCraftQuantity)
				// Cap by what's actually in storage
				if avail, ok := storageItems[comp.ID]; ok && needed > avail {
					needed = avail
				}
				if needed <= 0 {
					continue
				}

				logger.Printf("   📥 Withdrawing %.0f x %s", needed, comp.ID)
				if err := c.WithdrawItems(ctx, comp.ID, needed); err != nil {
					logger.Printf("   ⚠️  Withdraw %s failed: %v", comp.ID, err)
					continue
				}
				if err := sleepCtx(ctx, SleepTick); err != nil {
					return totalCrafted + iterationCrafted, err
				}
			}

			// Craft in batches sized by crafting skill level.
			maxBatch := MaxCraftBatchSize(state)
			quantity := recipe.CanCraftQuantity
			for quantity > 0 {
				if err := ctx.Err(); err != nil {
					return totalCrafted + iterationCrafted, err
				}

				batchSize := min(quantity, maxBatch)

				// Check cargo space
				state = c.GetState()
				remaining := state.Ship.CargoCapacity - state.Ship.CargoUsed
				if remaining < float64(batchSize) {
					// Cargo getting full — deposit and continue
					logger.Printf("   📦 Cargo full, depositing...")
					if err := c.DepositAllItems(ctx); err != nil {
						logger.Printf("   ⚠️  Deposit failed: %v", err)
						break
					}
					if err := sleepCtx(ctx, SleepTick); err != nil {
						return totalCrafted + iterationCrafted, err
					}
				}

				if err := sleepCtx(ctx, SleepShort); err != nil {
					return totalCrafted + iterationCrafted, err
				}
				if err := c.CraftWithQuantity(ctx, recipe.RecipeID, batchSize); err != nil {
					logger.Printf("   ⚠️  Craft failed: %v", err)
					if strings.Contains(err.Error(), "action_pending") || strings.Contains(err.Error(), "already pending") {
						_ = sleepCtx(ctx, SleepShort)
					}
					break
				}

				iterationCrafted += batchSize
				quantity -= batchSize
				logger.Printf("   ✅ Crafted %d x %s", batchSize, recipe.RecipeName)
				if err := sleepCtx(ctx, SleepMedium); err != nil {
					return totalCrafted + iterationCrafted, err
				}
			}
		}

		// Step 5: Deposit remaining crafted items
		state = c.GetState()
		if len(state.Ship.Cargo) > 0 {
			logger.Printf("📦 Depositing crafted items to storage...")
			if err := c.DepositAllItems(ctx); err != nil {
				logger.Printf("⚠️  Final deposit failed: %v", err)
			}
			_ = sleepCtx(ctx, SleepTick)
		}

		totalCrafted += iterationCrafted

		if iterationCrafted == 0 {
			logger.Printf("ℹ️  No items crafted this iteration, stopping")
			break
		}

		logger.Printf("✅ Iteration %d: crafted %d items (total: %d)", iteration+1, iterationCrafted, totalCrafted)
	}

	logger.Printf("═══ Storage crafting complete: %d items crafted ═══", totalCrafted)
	return totalCrafted, nil
}

