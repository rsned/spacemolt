package knowledge

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestSQLiteKB_StoreItems(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	items := []CatalogItem{
		{ID: "iron_ore", Name: "Iron Ore", Description: "Raw iron ore", Category: "ore", Rarity: "common", Size: 1, BaseValue: 10, Stackable: true, Tradeable: true},
		{ID: "copper_ore", Name: "Copper Ore", Description: "Raw copper ore", Category: "ore", Rarity: "common", Size: 1, BaseValue: 12, Stackable: true, Tradeable: true},
		{ID: "laser_gun", Name: "Laser Gun", Description: "Basic weapon", Category: "weapon", Rarity: "uncommon", Size: 5, BaseValue: 500, Stackable: false, Tradeable: true},
	}

	if err := kb.StoreItems(ctx, items); err != nil {
		t.Fatalf("StoreItems failed: %v", err)
	}
}

func TestSQLiteKB_StoreItems_Empty(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Store empty list
	if err := kb.StoreItems(ctx, nil); err != nil {
		t.Fatalf("StoreItems with nil failed: %v", err)
	}

	items, err := kb.GetItems(ctx)
	if err != nil {
		t.Fatalf("GetItems after empty store failed: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("Expected 0 items, got %d", len(items))
	}
}

func TestSQLiteKB_StoreItems_Replaces(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Store initial items
	items1 := []CatalogItem{
		{ID: "item-1", Name: "Item 1", Category: "cat1"},
		{ID: "item-2", Name: "Item 2", Category: "cat1"},
	}
	if err := kb.StoreItems(ctx, items1); err != nil {
		t.Fatalf("StoreItems 1 failed: %v", err)
	}

	// Store replacement items (should clear old ones)
	items2 := []CatalogItem{
		{ID: "item-3", Name: "Item 3", Category: "cat2"},
	}
	if err := kb.StoreItems(ctx, items2); err != nil {
		t.Fatalf("StoreItems 2 failed: %v", err)
	}

	all, err := kb.GetItems(ctx)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("Expected 1 item after replacement, got %d", len(all))
	}
	if len(all) > 0 && all[0].ID != "item-3" {
		t.Errorf("Expected item-3, got %s", all[0].ID)
	}
}

func TestSQLiteKB_GetItem(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	items := []CatalogItem{
		{ID: "iron_ore", Name: "Iron Ore", Description: "Raw iron ore", Category: "ore", Rarity: "common", Size: 1, BaseValue: 10, Stackable: true, Tradeable: true},
	}
	if err := kb.StoreItems(ctx, items); err != nil {
		t.Fatalf("StoreItems failed: %v", err)
	}

	item, err := kb.GetItem(ctx, "iron_ore")
	if err != nil {
		t.Fatalf("GetItem failed: %v", err)
	}
	if item == nil {
		t.Fatal("Expected non-nil item")
	}
	if item.Name != "Iron Ore" {
		t.Errorf("Expected name 'Iron Ore', got '%s'", item.Name)
	}
	if !item.Stackable {
		t.Error("Expected stackable=true")
	}
	if !item.Tradeable {
		t.Error("Expected tradeable=true")
	}
}

func TestSQLiteKB_GetItem_NotFound(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	item, err := kb.GetItem(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetItem for nonexistent failed: %v", err)
	}
	if item != nil {
		t.Error("Expected nil for nonexistent item")
	}
}

func TestSQLiteKB_GetItemsByCategory(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	items := []CatalogItem{
		{ID: "iron_ore", Name: "Iron Ore", Category: "ore"},
		{ID: "copper_ore", Name: "Copper Ore", Category: "ore"},
		{ID: "laser_gun", Name: "Laser Gun", Category: "weapon"},
	}
	if err := kb.StoreItems(ctx, items); err != nil {
		t.Fatalf("StoreItems failed: %v", err)
	}

	ores, err := kb.GetItemsByCategory(ctx, "ore")
	if err != nil {
		t.Fatalf("GetItemsByCategory failed: %v", err)
	}
	if len(ores) != 2 {
		t.Errorf("Expected 2 ore items, got %d", len(ores))
	}

	weapons, err := kb.GetItemsByCategory(ctx, "weapon")
	if err != nil {
		t.Fatalf("GetItemsByCategory weapon failed: %v", err)
	}
	if len(weapons) != 1 {
		t.Errorf("Expected 1 weapon item, got %d", len(weapons))
	}

	// Nonexistent category
	none, err := kb.GetItemsByCategory(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetItemsByCategory nonexistent failed: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("Expected 0 items for nonexistent category, got %d", len(none))
	}
}

func TestSQLiteKB_StoreShipClasses(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	classes := []ShipClassDef{
		{
			ID:               "shuttle",
			Name:             "Shuttle",
			Class:            "Civilian",
			Category:         "starter",
			Description:      "A basic shuttle",
			Tier:             1,
			BaseHull:         100,
			CargoCapacity:    20,
			WeaponSlots:      0,
			PilotingRequired: 1,
			DefaultModules:   []string{"basic_engine"},
			FlavorTags:       []string{"starter", "civilian"},
			BuildMaterials: []BuildMaterial{
				{ItemID: "iron_plates", Quantity: 5},
				{ItemID: "circuits", Quantity: 2},
			},
		},
		{
			ID:          "fighter",
			Name:        "Fighter",
			Class:       "Combat",
			Category:    "military",
			Description: "A combat fighter",
			Tier:        2,
			BaseHull:    200,
			WeaponSlots: 2,
		},
	}

	if err := kb.StoreShipClasses(ctx, classes); err != nil {
		t.Fatalf("StoreShipClasses failed: %v", err)
	}
}

func TestSQLiteKB_GetShipClass(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	classes := []ShipClassDef{
		{
			ID:               "shuttle",
			Name:             "Shuttle",
			Class:            "Civilian",
			PilotingRequired: 1,
			DefaultModules:   []string{"basic_engine"},
			FlavorTags:       []string{"starter"},
			BuildMaterials: []BuildMaterial{
				{ItemID: "iron_plates", Quantity: 5},
			},
			// Prestige/unlock cluster added in v0.495.1. Round-tripping these
			// is what proves migration 49's columns, the shipClassColumns
			// order, and the positional scanners all agree.
			RequiredReputation:         250,
			RequiredAchievement:        "first_blood",
			RequiredFactionAchievement: "war_chest",
			RequiredFactionLeader:      true,
			PrestigeLock:               "earn first_blood to unlock",
			DefaultLoadoutVersion:      3,
		},
	}
	if err := kb.StoreShipClasses(ctx, classes); err != nil {
		t.Fatalf("StoreShipClasses failed: %v", err)
	}

	sc, err := kb.GetShipClass(ctx, "shuttle")
	if err != nil {
		t.Fatalf("GetShipClass failed: %v", err)
	}
	if sc == nil {
		t.Fatal("Expected non-nil ship class")
	}
	if sc.Name != "Shuttle" {
		t.Errorf("Expected name 'Shuttle', got '%s'", sc.Name)
	}
	if sc.PilotingRequired != 1 {
		t.Errorf("Expected PilotingRequired=1, got %d", sc.PilotingRequired)
	}
	if len(sc.BuildMaterials) != 1 {
		t.Errorf("Expected 1 build material, got %d", len(sc.BuildMaterials))
	}
	if len(sc.BuildMaterials) > 0 && sc.BuildMaterials[0].ItemID != "iron_plates" {
		t.Errorf("Expected build material iron_plates, got %s", sc.BuildMaterials[0].ItemID)
	}
	if sc.RequiredReputation != 250 {
		t.Errorf("Expected RequiredReputation=250, got %d", sc.RequiredReputation)
	}
	if sc.RequiredAchievement != "first_blood" {
		t.Errorf("Expected RequiredAchievement='first_blood', got %q", sc.RequiredAchievement)
	}
	if sc.RequiredFactionAchievement != "war_chest" {
		t.Errorf("Expected RequiredFactionAchievement='war_chest', got %q", sc.RequiredFactionAchievement)
	}
	if !sc.RequiredFactionLeader {
		t.Error("Expected RequiredFactionLeader=true, got false")
	}
	if sc.PrestigeLock != "earn first_blood to unlock" {
		t.Errorf("Expected PrestigeLock round-trip, got %q", sc.PrestigeLock)
	}
	if sc.DefaultLoadoutVersion != 3 {
		t.Errorf("Expected DefaultLoadoutVersion=3, got %d", sc.DefaultLoadoutVersion)
	}
}

func TestSQLiteKB_GetShipClass_NotFound(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	sc, err := kb.GetShipClass(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetShipClass for nonexistent failed: %v", err)
	}
	if sc != nil {
		t.Error("Expected nil for nonexistent ship class")
	}
}

func TestSQLiteKB_GetShipClasses(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	classes := []ShipClassDef{
		{ID: "shuttle", Name: "Shuttle", Class: "Civilian", BuildMaterials: []BuildMaterial{{ItemID: "iron", Quantity: 5}}},
		{ID: "fighter", Name: "Fighter", Class: "Combat"},
	}
	if err := kb.StoreShipClasses(ctx, classes); err != nil {
		t.Fatalf("StoreShipClasses failed: %v", err)
	}

	all, err := kb.GetShipClasses(ctx)
	if err != nil {
		t.Fatalf("GetShipClasses failed: %v", err)
	}
	// Every store also folds in the retired-but-still-flown classes (legacy_ships.go),
	// so the stored set is a floor, not the total. Assert on what was stored.
	byID := map[string]ShipClassDef{}
	for _, sc := range all {
		byID[sc.ID] = sc
	}
	for _, want := range []string{"shuttle", "fighter"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("stored class %s missing from GetShipClasses", want)
		}
	}
	legacy, err := LegacyShipClasses()
	if err != nil {
		t.Fatalf("LegacyShipClasses failed: %v", err)
	}
	if len(all) != len(classes)+len(legacy) {
		t.Errorf("Expected %d ship classes (2 stored + %d legacy), got %d", len(classes)+len(legacy), len(legacy), len(all))
	}

	// Verify build materials are loaded
	for _, sc := range all {
		if sc.ID == "shuttle" && len(sc.BuildMaterials) != 1 {
			t.Errorf("Expected 1 build material for shuttle, got %d", len(sc.BuildMaterials))
		}
	}
}

func TestSQLiteKB_GetShipClassesByCategory(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Transport is inserted first but is the higher tier, so a correct
	// tier-ascending sort has to reorder it behind Shuttle. (This used to sort
	// by price, which the server no longer sends.)
	classes := []ShipClassDef{
		{ID: "transport", Name: "Transport", Class: "Civilian", Tier: 3},
		{ID: "shuttle", Name: "Shuttle", Class: "Civilian", Tier: 1},
		{ID: "fighter", Name: "Fighter", Class: "Combat", Tier: 2},
	}
	if err := kb.StoreShipClasses(ctx, classes); err != nil {
		t.Fatalf("StoreShipClasses failed: %v", err)
	}

	civilian, err := kb.GetShipClassesByCategory(ctx, "Civilian")
	if err != nil {
		t.Fatalf("GetShipClassesByCategory failed: %v", err)
	}
	if len(civilian) != 2 {
		t.Errorf("Expected 2 civilian classes, got %d", len(civilian))
	}
	if len(civilian) == 2 && civilian[0].ID != "shuttle" {
		t.Errorf("Expected tier-ascending order (shuttle before transport), got %s first", civilian[0].ID)
	}

	combat, err := kb.GetShipClassesByCategory(ctx, "Combat")
	if err != nil {
		t.Fatalf("GetShipClassesByCategory combat failed: %v", err)
	}
	if len(combat) != 1 {
		t.Errorf("Expected 1 combat class, got %d", len(combat))
	}
}

func TestSQLiteKB_StoreRecipes(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	recipes := []RecipeDef{
		{
			ID:              "iron_plate",
			Name:            "Iron Plate",
			Description:     "Smelt iron ore into plates",
			Category:        "smelting",
			CraftingTime:    10,
			BaseQuality:     50,
			SkillQualityMod: 5,
			RequiredSkills:  map[string]int{"mining": 1},
			Inputs:          []RecipeIngredient{{ItemID: "iron_ore", Quantity: 3}},
			Outputs:         []RecipeProduct{{ItemID: "iron_plate", Quantity: 1, QualityMod: true}},
		},
	}

	if err := kb.StoreRecipes(ctx, recipes); err != nil {
		t.Fatalf("StoreRecipes failed: %v", err)
	}
}

func TestSQLiteKB_GetRecipe(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	recipes := []RecipeDef{
		{
			ID:             "iron_plate",
			Name:           "Iron Plate",
			Category:       "smelting",
			CraftingTime:   10,
			RequiredSkills: map[string]int{"mining": 1},
			Inputs:         []RecipeIngredient{{ItemID: "iron_ore", Quantity: 3}},
			Outputs:        []RecipeProduct{{ItemID: "iron_plate", Quantity: 1, QualityMod: true}},
		},
	}
	if err := kb.StoreRecipes(ctx, recipes); err != nil {
		t.Fatalf("StoreRecipes failed: %v", err)
	}

	r, err := kb.GetRecipe(ctx, "iron_plate")
	if err != nil {
		t.Fatalf("GetRecipe failed: %v", err)
	}
	if r == nil {
		t.Fatal("Expected non-nil recipe")
	}
	if r.Name != "Iron Plate" {
		t.Errorf("Expected name 'Iron Plate', got '%s'", r.Name)
	}
	if len(r.Inputs) != 1 {
		t.Errorf("Expected 1 input, got %d", len(r.Inputs))
	}
	if len(r.Outputs) != 1 {
		t.Errorf("Expected 1 output, got %d", len(r.Outputs))
	}
	if len(r.Outputs) > 0 && !r.Outputs[0].QualityMod {
		t.Error("Expected QualityMod=true")
	}
}

func TestSQLiteKB_GetRecipe_NotFound(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	r, err := kb.GetRecipe(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetRecipe for nonexistent failed: %v", err)
	}
	if r != nil {
		t.Error("Expected nil for nonexistent recipe")
	}
}

func TestSQLiteKB_GetRecipes(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	recipes := []RecipeDef{
		{
			ID:       "iron_plate",
			Name:     "Iron Plate",
			Category: "smelting",
			Inputs:   []RecipeIngredient{{ItemID: "iron_ore", Quantity: 3}},
			Outputs:  []RecipeProduct{{ItemID: "iron_plate", Quantity: 1}},
		},
		{
			ID:       "copper_wire",
			Name:     "Copper Wire",
			Category: "fabrication",
			Inputs:   []RecipeIngredient{{ItemID: "copper_ore", Quantity: 2}},
			Outputs:  []RecipeProduct{{ItemID: "copper_wire", Quantity: 1}},
		},
	}
	if err := kb.StoreRecipes(ctx, recipes); err != nil {
		t.Fatalf("StoreRecipes failed: %v", err)
	}

	all, err := kb.GetRecipes(ctx)
	if err != nil {
		t.Fatalf("GetRecipes failed: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("Expected 2 recipes, got %d", len(all))
	}

	// Check inputs/outputs are loaded
	for _, r := range all {
		if len(r.Inputs) != 1 {
			t.Errorf("Recipe %s: expected 1 input, got %d", r.ID, len(r.Inputs))
		}
		if len(r.Outputs) != 1 {
			t.Errorf("Recipe %s: expected 1 output, got %d", r.ID, len(r.Outputs))
		}
	}
}

func TestSQLiteKB_GetRecipesByCategory(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	recipes := []RecipeDef{
		{ID: "iron_plate", Name: "Iron Plate", Category: "smelting"},
		{ID: "steel_plate", Name: "Steel Plate", Category: "smelting"},
		{ID: "copper_wire", Name: "Copper Wire", Category: "fabrication"},
	}
	if err := kb.StoreRecipes(ctx, recipes); err != nil {
		t.Fatalf("StoreRecipes failed: %v", err)
	}

	smelting, err := kb.GetRecipesByCategory(ctx, "smelting")
	if err != nil {
		t.Fatalf("GetRecipesByCategory failed: %v", err)
	}
	if len(smelting) != 2 {
		t.Errorf("Expected 2 smelting recipes, got %d", len(smelting))
	}

	none, err := kb.GetRecipesByCategory(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetRecipesByCategory nonexistent failed: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("Expected 0 recipes, got %d", len(none))
	}
}

func TestSQLiteKB_Catalog_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Store initial data
	items := []CatalogItem{{ID: "item-1", Name: "Item 1", Category: "cat1"}}
	if err := kb.StoreItems(ctx, items); err != nil {
		t.Fatalf("StoreItems failed: %v", err)
	}

	// Concurrent reads
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := kb.GetItem(ctx, "item-1")
			if err != nil {
				t.Errorf("Concurrent GetItem %d failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent reads and writes
	for i := range 5 {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			newItems := []CatalogItem{{ID: fmt.Sprintf("item-%d", idx), Name: fmt.Sprintf("Item %d", idx)}}
			_ = kb.StoreItems(ctx, newItems)
		}(i)
		go func(idx int) {
			defer wg.Done()
			_, _ = kb.GetItems(ctx)
		}(i)
	}
	wg.Wait()
}
