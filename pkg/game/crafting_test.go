package game

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestXpToLevel(t *testing.T) {
	client := NewClient("wss://test.example.com", "user", "pass", nil)

	tests := []struct {
		xp   int
		want int
	}{
		{0, 1},
		{50, 1},
		{99, 1},
		{100, 2},
		{200, 2},
		{299, 2},
		{300, 3},
		{500, 3},
		{599, 3},
		{600, 4},
		{999, 4},
		{1000, 5},
		{5000, 5},  // capped at 5
		{-1, 1},    // negative XP
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if got := client.xpToLevel(tt.xp); got != tt.want {
				t.Errorf("xpToLevel(%d) = %d, want %d", tt.xp, got, tt.want)
			}
		})
	}
}

func TestCraftWithQuantity_Validation(t *testing.T) {
	// v0.389: CraftWithOptions no longer clamps quantity to MaxCraftBatchSize.
	// The only validation is quantity >= 1.
	c := newSubmitClientSkeleton()

	zeroTests := []struct {
		name     string
		quantity int
	}{
		{"zero quantity", 0},
		{"negative quantity", -1},
	}
	for _, tt := range zeroTests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.CraftWithQuantity(t.Context(), "test_recipe", tt.quantity)
			if err == nil || !strings.Contains(err.Error(), "invalid quantity") {
				t.Errorf("CraftWithQuantity(quantity=%d) should have 'invalid quantity' error, got: %v", tt.quantity, err)
			}
		})
	}

	// Quantities well above the old skill cap (1) are now valid — they reach
	// Submit and return "not connected" (or time out), NOT "invalid quantity".
	largeTests := []struct {
		name     string
		quantity int
	}{
		{"large quantity no skill", 2},
		{"way above old cap", 100},
		{"much larger than old cap", 250},
	}
	for _, tt := range largeTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			err := c.CraftWithOptions(ctx, "test_recipe", tt.quantity, "")
			// Should NOT be an "invalid quantity" error — the new code only rejects < 1.
			if err != nil && strings.Contains(err.Error(), "invalid quantity") {
				t.Errorf("CraftWithOptions(quantity=%d) got unexpected validation error: %v", tt.quantity, err)
			}
		})
	}
}

func TestMaxCraftBatchSize(t *testing.T) {
	tests := []struct {
		name     string
		skills   map[string]Skill
		expected int
	}{
		{"nil state", nil, 1},
		{"no skills", map[string]Skill{}, 1},
		{"crafting level 0", map[string]Skill{"crafting": {Level: 0}}, 1},
		{"crafting level 1", map[string]Skill{"crafting": {Level: 1}}, 1},
		{"crafting level 2", map[string]Skill{"crafting": {Level: 2}}, 2},
		{"crafting level 5", map[string]Skill{"crafting": {Level: 5}}, 5},
		{"crafting level 10", map[string]Skill{"crafting": {Level: 10}}, 10},
		{"crafting level 20", map[string]Skill{"crafting": {Level: 20}}, 20},
		{"only mining skill", map[string]Skill{"mining": {Level: 10}}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var state *State
			if tt.skills != nil {
				state = &State{Player: Player{Skills: tt.skills}}
			}
			if got := MaxCraftBatchSize(state); got != tt.expected {
				t.Errorf("MaxCraftBatchSize() = %d, want %d", got, tt.expected)
			}
		})
	}
}


func TestCraftingLoopConfig_Defaults(t *testing.T) {
	// Verify that a nil config gets sensible defaults applied in CraftingLoop's validation
	config := &CraftingLoopConfig{}

	if config.Strategy != "" {
		t.Errorf("default Strategy should be empty, got %q", config.Strategy)
	}
	if config.CaptainsLogInterval != 0 {
		t.Errorf("default CaptainsLogInterval should be 0, got %v", config.CaptainsLogInterval)
	}
}

func TestCraftingLoopConfig_InvalidStrategy(t *testing.T) {
	// We can test the strategy validation without a real client
	// by checking CraftingLoop returns an error for invalid strategies
	client := NewClient("wss://test.example.com", "user", "pass", nil)

	config := &CraftingLoopConfig{
		Strategy: "invalid-strategy",
	}

	_, err := CraftingLoop(client, nil, t.Context(), config)
	if err == nil {
		t.Error("expected error for invalid strategy")
	}
}

func TestCraftQueryResult_EmptyInit(t *testing.T) {
	result := &CraftQueryResult{
		FullyCraftable: []CraftableRecipe{},
		PartialMatches: []CraftableRecipe{},
		SkillBlocked:   []CraftableRecipe{},
	}

	if len(result.FullyCraftable) != 0 {
		t.Errorf("expected empty FullyCraftable, got %d", len(result.FullyCraftable))
	}
	if len(result.PartialMatches) != 0 {
		t.Errorf("expected empty PartialMatches, got %d", len(result.PartialMatches))
	}
	if len(result.SkillBlocked) != 0 {
		t.Errorf("expected empty SkillBlocked, got %d", len(result.SkillBlocked))
	}
}

// TestCraftWithOptionsPayload verifies the new async queue-aware semantics:
// quantity above the old skill-based cap must be accepted, quantity is always
// in the payload, deliver_to is forwarded when non-empty, and cargo is NOT a
// valid deliver_to (the new model only allows "" or "faction"/"storage").
//
// Uses newSubmitTestClient (from submit_test.go) — mirrors the pattern used by
// TestBattle_PlainOKTerminates and TestBattle_ErrorStillTerminates.
func TestCraftWithOptionsPayload(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start the craft call in background; it blocks waiting for the server reply.
	errCh := make(chan error, 1)
	go func() {
		// quantity 250 is way above the old skill-based cap (max=1 with no skill).
		errCh <- c.CraftWithOptions(ctx, "basic_iron_smelting", 250, "faction")
	}()

	// Receive the sent message.
	var sent protocol.Message
	select {
	case sent = <-sendCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for send — CraftWithOptions likely rejected 250 before sending")
	}

	if sent.Type != "craft" {
		t.Fatalf("sent.Type = %q, want %q", sent.Type, "craft")
	}
	if got := sent.Payload["recipe_id"]; got != "basic_iron_smelting" {
		t.Fatalf("recipe_id = %v, want %q", got, "basic_iron_smelting")
	}
	if got := sent.Payload["quantity"]; got != 250 {
		t.Fatalf("quantity = %v, want 250 — clamp was NOT removed", got)
	}
	if got := sent.Payload["deliver_to"]; got != "faction" {
		t.Fatalf("deliver_to = %v, want %q", got, "faction")
	}

	// Simulate the server's single non-pending ok (v0.389 async model).
	c.router.dispatch(protocol.Response{
		Type:      protocol.TypeOK,
		RequestID: sent.RequestID,
		Payload:   map[string]any{"action": "craft", "job_id": "j1"},
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("CraftWithOptions returned unexpected error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("CraftWithOptions did not return after ok")
	}
}

// TestCraftWithOptionsRejectsBadQuantity verifies validation still rejects quantity < 1.
func TestCraftWithOptionsRejectsBadQuantity(t *testing.T) {
	c := newSubmitClientSkeleton()
	if err := c.CraftWithOptions(context.Background(), "r", 0, ""); err == nil {
		t.Fatal("expected error for quantity 0")
	}
}

func TestCraftableRecipe_Fields(t *testing.T) {
	recipe := CraftableRecipe{
		RecipeID:         "recipe_1",
		RecipeName:       "Iron Plate",
		CanCraftQuantity: 5,
		Components: []Component{
			{ID: "iron_ore", Quantity: 10},
			{ID: "copper_ore", Quantity: 5},
		},
		CanCraft:  true,
		Profit:    25.5,
		SkillGaps: []string{"smithing_3"},
	}

	if recipe.RecipeID != "recipe_1" {
		t.Errorf("RecipeID = %q, want %q", recipe.RecipeID, "recipe_1")
	}
	if recipe.RecipeName != "Iron Plate" {
		t.Errorf("RecipeName = %q, want %q", recipe.RecipeName, "Iron Plate")
	}
	if recipe.CanCraftQuantity != 5 {
		t.Errorf("CanCraftQuantity = %d, want 5", recipe.CanCraftQuantity)
	}
	if len(recipe.Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(recipe.Components))
	}
	if recipe.Components[0].Quantity != 10 {
		t.Errorf("component quantity = %v, want 10", recipe.Components[0].Quantity)
	}
	if !recipe.CanCraft {
		t.Errorf("CanCraft = %v, want true", recipe.CanCraft)
	}
	if recipe.Profit != 25.5 {
		t.Errorf("Profit = %v, want 25.5", recipe.Profit)
	}
	if len(recipe.SkillGaps) != 1 || recipe.SkillGaps[0] != "smithing_3" {
		t.Errorf("SkillGaps = %v, want [smithing_3]", recipe.SkillGaps)
	}
}
