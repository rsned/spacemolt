// convert-recipes transforms SpaceMolt recipe JSON to the format expected by the crafting server
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// SpaceMoltRecipeFile is the top-level structure
type SpaceMoltRecipeFile struct {
	Recipes map[string]SpaceMoltRecipe `json:"recipes"`
}

// SpaceMoltRecipe is the format from server_docs/recipes.20260209.json
type SpaceMoltRecipe struct {
	ID              string                   `json:"id"`
	Name            string                   `json:"name"`
	Description     string                   `json:"description"`
	Category        string                   `json:"category"`
	Inputs          []SpaceMoltComponent     `json:"inputs"`
	Outputs         []SpaceMoltOutput        `json:"outputs"`
	RequiredSkills  map[string]int           `json:"required_skills"`
	CraftingTime    int                      `json:"crafting_time"`
	BaseQuality     int                      `json:"base_quality,omitempty"`
	SkillQualityMod int                      `json:"skill_quality_mod,omitempty"`
}

type SpaceMoltComponent struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type SpaceMoltOutput struct {
	ItemID     string `json:"item_id"`
	Quantity   int    `json:"quantity"`
	QualityMod bool   `json:"quality_mod,omitempty"`
}

// ImportRecipe is the format expected by the crafting server sync
type ImportRecipe struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	CraftTimeSec int                 `json:"craft_time_sec,omitempty"`
	Components   []ImportComponent   `json:"components,omitempty"`
	Skills       []ImportSkill       `json:"skills,omitempty"`
	Output       ImportOutput        `json:"output,omitempty"`
}

type ImportComponent struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type ImportSkill struct {
	SkillID       string `json:"skill_id"`
	LevelRequired int    `json:"level_required"`
}

type ImportOutput struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <input.json> <output.json>\n", os.Args[0])
		os.Exit(1)
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]

	// Read input file
	data, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input file: %v\n", err)
		os.Exit(1)
	}

	// Parse SpaceMolt format
	var smRecipes SpaceMoltRecipeFile
	if err := json.Unmarshal(data, &smRecipes); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing input JSON: %v\n", err)
		os.Exit(1)
	}

	// Convert to import format
	var importRecipes []ImportRecipe
	for _, smRecipe := range smRecipes.Recipes {
		// Build components
		var components []ImportComponent
		for _, input := range smRecipe.Inputs {
			components = append(components, ImportComponent{
				ItemID:   input.ItemID,
				Quantity: input.Quantity,
			})
		}

		// Build skills
		var skills []ImportSkill
		for skillID, level := range smRecipe.RequiredSkills {
			skills = append(skills, ImportSkill{
				SkillID:       skillID,
				LevelRequired: level,
			})
		}

		// Get first output (primary output)
		var output ImportOutput
		if len(smRecipe.Outputs) > 0 {
			output = ImportOutput{
				ItemID:   smRecipe.Outputs[0].ItemID,
				Quantity: smRecipe.Outputs[0].Quantity,
			}
		}

		importRecipe := ImportRecipe{
			ID:           smRecipe.ID,
			Name:         smRecipe.Name,
			Description:  smRecipe.Description,
			Category:     smRecipe.Category,
			CraftTimeSec: smRecipe.CraftingTime,
			Components:   components,
			Skills:       skills,
			Output:       output,
		}

		importRecipes = append(importRecipes, importRecipe)
	}

	// Write output file
	outputData, err := json.MarshalIndent(importRecipes, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling output JSON: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, outputData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Converted %d recipes from %s to %s\n", len(importRecipes), inputPath, outputPath)
}
