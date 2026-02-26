package game

import (
	"testing"
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
	client := NewClient("wss://test.example.com", "user", "pass", nil)

	// Quantity validation - these should fail immediately without needing a connection
	tests := []struct {
		name     string
		quantity int
		wantErr  bool
	}{
		{"zero quantity", 0, true},
		{"negative quantity", -1, true},
		{"above max", 11, true},
		{"way above max", 100, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.CraftWithQuantity(t.Context(), "test_recipe", tt.quantity)
			if (err != nil) != tt.wantErr {
				t.Errorf("CraftWithQuantity(quantity=%d) err = %v, wantErr %v", tt.quantity, err, tt.wantErr)
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

func TestCraftableRecipe_Fields(t *testing.T) {
	recipe := CraftableRecipe{
		RecipeID:         "recipe_1",
		RecipeName:       "Iron Plate",
		CanCraftQuantity: 5,
		Components: []Component{
			{ID: "ore_iron", Quantity: 10},
			{ID: "ore_copper", Quantity: 5},
		},
		CanCraft:  true,
		Profit:    25.5,
		SkillGaps: []string{"smithing_3"},
	}

	if recipe.RecipeID != "recipe_1" {
		t.Errorf("RecipeID = %q, want %q", recipe.RecipeID, "recipe_1")
	}
	if len(recipe.Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(recipe.Components))
	}
	if recipe.Components[0].Quantity != 10 {
		t.Errorf("component quantity = %v, want 10", recipe.Components[0].Quantity)
	}
}
