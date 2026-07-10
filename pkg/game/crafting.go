package game

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

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

// CraftWithQuantity queues a crafting job for the given recipe. quantity is the
// number of output items wanted; the server rounds it up to whole production
// runs. Inputs are escrowed from station storage and output is delivered there.
func (c *Client) CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error {
	return c.CraftWithOptions(ctx, recipeID, quantity, "")
}

// CraftWithOptions queues a crafting job. quantity is the number of output items
// wanted (server rounds up to whole runs). deliverTo may be "" (server default:
// station storage) or "faction" (faction storage; requires a Faction Workshop
// facility and manage-treasury permission, pulling inputs from faction storage).
// Crafting is async: the server replies with a single ok job frame and delivers
// output later via crafting_update; this method returns once the job is queued.
func (c *Client) CraftWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	if quantity < 1 {
		return fmt.Errorf("invalid quantity: %d (must be >= 1)", quantity)
	}

	payload := map[string]any{
		"recipe_id": recipeID,
		"quantity":  quantity,
	}
	if deliverTo != "" {
		payload["deliver_to"] = deliverTo
	}

	msg := protocol.Message{
		Type:      "craft",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// v0.389: craft is async-queued. The server replies with a single
	// non-pending ok carrying the job body; there is no action_result. Treat
	// that ok as terminal via terminateOnActionOrOK.
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// MaxCraftBulkJobs is the server-enforced ceiling on jobs per bulk craft request.
const MaxCraftBulkJobs = 50

// CraftBulk queues many craft (or recycle) jobs in a single request. Each entry
// is a job object of the same shape the server documents for craft's "jobs"
// param: {recipe_id, quantity, facility_id?, preset?, deliver_to?}. Top-level
// recipe_id/quantity are ignored in bulk mode; each job is queued independently
// (partial success), so a single bad entry does not fail the others. Bulk mode
// is not compatible with dry_run. The server caps a request at MaxCraftBulkJobs.
func (c *Client) CraftBulk(ctx context.Context, jobs []map[string]any) error {
	if len(jobs) == 0 {
		return fmt.Errorf("craft bulk requires at least one job")
	}
	if len(jobs) > MaxCraftBulkJobs {
		return fmt.Errorf("too many jobs: %d (max %d per request)", len(jobs), MaxCraftBulkJobs)
	}

	msg := protocol.Message{
		Type:      "craft",
		Payload:   map[string]any{"jobs": jobs},
		Timestamp: time.Now().UnixMilli(),
	}
	// Like single craft (v0.389+), the server replies with a single terminal ok
	// carrying the bulk result body; terminate on that.
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// CraftDryRun quotes a craft job without queuing it: the server's authoritative
// fee (credits_total), duration, and have_inputs/have_credits preflight for
// crafting quantity output units of recipeID. facilityID may be "" to let the
// server auto-route to the local facility/workshop (the hand-craft path — Task
// 0 findings 2026-07-10 confirmed hand/workshop dry-run IS supported live and
// its facility_id in the response is the auto-resolved workshop instance id),
// or an explicit facility instance id (crafting requires being docked at that
// facility's station — a remote facility_id errors no_facility). Bulk mode is
// not compatible with dry_run (CraftBulk's doc comment), so this always
// submits a single-job payload, mirroring CraftWithOptions/CraftBulk's
// Submit/terminator pattern.
func (c *Client) CraftDryRun(ctx context.Context, recipeID string, quantity int, facilityID string) (*serverapi.CraftDryRunResponse, error) {
	if quantity < 1 {
		return nil, fmt.Errorf("invalid quantity: %d (must be >= 1)", quantity)
	}

	payload := map[string]any{
		"recipe_id": recipeID,
		"quantity":  quantity,
		"dry_run":   true,
	}
	if facilityID != "" {
		payload["facility_id"] = facilityID
	}

	msg := protocol.Message{
		Type:      "craft",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepTick*3))
	if err != nil {
		return nil, err
	}
	resp, err := c.await(ctx, h)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("craft dry run: marshal payload: %w", err)
	}
	var out serverapi.CraftDryRunResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("craft dry run: decode payload: %w", err)
	}
	return &out, nil
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

// CraftItems queues crafting jobs for everything currently craftable from the
// station storage at the docked base. v0.389: crafting reads inputs from and
// delivers output to station storage, and runs asynchronously over ticks — so
// this deposits any cargo into storage, queries craftable recipes, and queues
// each one exactly once. It does NOT withdraw to cargo and does NOT re-issue a
// craft to "make progress" (that would double-spend). Output lands in storage
// over the following ticks; consumers can observe it via OnCraftingUpdate or
// `craft action=queue`. Returns the total output quantity queued.
func (c *Client) CraftItems(ctx context.Context, logger *log.Logger, config *CraftingConfig) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// Deposit any cargo so it counts toward craftable inputs in storage.
	state := c.GetState()
	if len(state.Ship.Cargo) > 0 {
		logger.Printf("📦 Depositing cargo into station storage...")
		if err := c.DepositAllItems(ctx); err != nil {
			logger.Printf("⚠️  Deposit failed: %v", err)
		} else if err := sleepCtx(ctx, SleepTick); err != nil {
			return 0, err
		}
	}

	// Read current storage contents.
	if err := c.ViewStorage(ctx); err != nil {
		return 0, fmt.Errorf("view storage failed: %w", err)
	}
	if err := sleepCtx(ctx, SleepQuick); err != nil {
		return 0, err
	}
	storageItems := c.getStorageItems()
	if len(storageItems) == 0 {
		logger.Printf("ℹ️  Storage is empty, nothing to craft")
		return 0, nil
	}

	// Determine what is craftable from storage.
	components := make([]Component, 0, len(storageItems))
	for itemID, qty := range storageItems {
		components = append(components, Component{ID: itemID, Quantity: qty})
	}
	result, err := c.QueryCraftableFromComponents(ctx, components, config)
	if err != nil {
		return 0, fmt.Errorf("craft query failed: %w", err)
	}
	if len(result.FullyCraftable) == 0 {
		logger.Printf("ℹ️  No craftable recipes from storage contents")
		return 0, nil
	}

	// Queue each craftable recipe exactly once for its full available quantity.
	totalQueued := 0
	for _, recipe := range result.FullyCraftable {
		if err := ctx.Err(); err != nil {
			return totalQueued, err
		}
		if recipe.CanCraftQuantity <= 0 {
			continue
		}
		logger.Printf("🔨 Queuing %d x %s...", recipe.CanCraftQuantity, recipe.RecipeName)
		if err := c.CraftWithQuantity(ctx, recipe.RecipeID, recipe.CanCraftQuantity); err != nil {
			logger.Printf("   ⚠️  Queue failed: %v", err)
			continue
		}
		totalQueued += recipe.CanCraftQuantity
		if err := sleepCtx(ctx, SleepShort); err != nil {
			return totalQueued, err
		}
	}

	logger.Printf("═══ Queued %d items across %d recipes (output lands in storage) ═══",
		totalQueued, len(result.FullyCraftable))
	return totalQueued, nil
}

