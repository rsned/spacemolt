package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// RecipesResponse represents the server response from catalog_recipes
type RecipesResponse struct {
	Items []RecipeJSON `json:"items"`
}

// RecipeJSON represents a recipe from the game catalog JSON
type RecipeJSON struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Description     string                `json:"description"`
	Category        string                `json:"category"`
	CraftingTime    int                   `json:"crafting_time"`
	BaseQuality     int                   `json:"base_quality"`
	SkillQualityMod int                   `json:"skill_quality_mod"`
	RequiredSkills  map[string]int        `json:"required_skills"`
	Hidden          bool                  `json:"hidden"`
	FacilityOnly    bool                  `json:"facility_only"`
	NoRecycle       bool                  `json:"no_recycle"`
	FuelOutput      int                   `json:"fuel_output"`
	Inputs          []RecipeIngredientJSON `json:"inputs"`
	Outputs         []RecipeProductJSON    `json:"outputs"`
}

// RecipeIngredientJSON represents an input item from JSON
type RecipeIngredientJSON struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

// RecipeProductJSON represents an output item from JSON
type RecipeProductJSON struct {
	ItemID     string `json:"item_id"`
	Quantity   int    `json:"quantity"`
	QualityMod bool   `json:"quality_mod"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <catalog-recipes.json>", os.Args[0])
	}

	jsonFile := os.Args[1]

	// Read the JSON file
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var response RecipesResponse
	if err := json.Unmarshal(data, &response); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(response.Items) == 0 {
		log.Fatalf("No recipes found in JSON file")
	}

	// Create knowledge base
	config := knowledge.DefaultConfig()
	if dbPath := os.Getenv("SPACEMOLT_DB"); dbPath != "" {
		config.DBPath = dbPath
	} else {
		config.DBPath = "data/spacemolt-knowledge.db"
	}
	kb, err := knowledge.NewSQLiteKB(config)
	if err != nil {
		log.Fatalf("Failed to open knowledge base: %v", err)
	}
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Convert JSON recipes to knowledge base recipes
	recipes := make([]knowledge.RecipeDef, len(response.Items))
	for i, r := range response.Items {
		inputs := make([]knowledge.RecipeIngredient, len(r.Inputs))
		for j, inp := range r.Inputs {
			inputs[j] = knowledge.RecipeIngredient{
				ItemID:   inp.ItemID,
				Quantity: inp.Quantity,
			}
		}

		outputs := make([]knowledge.RecipeProduct, len(r.Outputs))
		for j, out := range r.Outputs {
			outputs[j] = knowledge.RecipeProduct{
				ItemID:     out.ItemID,
				Quantity:   out.Quantity,
				QualityMod: out.QualityMod,
			}
		}

		recipes[i] = knowledge.RecipeDef{
			ID:              r.ID,
			Name:            r.Name,
			Description:     r.Description,
			Category:        r.Category,
			CraftingTime:    r.CraftingTime,
			BaseQuality:     r.BaseQuality,
			SkillQualityMod: r.SkillQualityMod,
			RequiredSkills:  r.RequiredSkills,
			Hidden:          r.Hidden,
			FacilityOnly:    r.FacilityOnly,
			NoRecycle:       r.NoRecycle,
			FuelOutput:      r.FuelOutput,
			Inputs:          inputs,
			Outputs:         outputs,
			LastUpdatedTick: 0,
		}
	}

	// Store recipes in database
	if err := kb.StoreRecipes(ctx, recipes); err != nil {
		log.Fatalf("Failed to store recipes: %v", err)
	}

	log.Printf("✓ Successfully imported %d recipes\n", len(recipes))
}
