// convert-skills transforms SpaceMolt skill JSON to the format expected by the crafting server
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// SpaceMoltSkillFile is the top-level structure
type SpaceMoltSkillFile struct {
	Skills map[string]SpaceMoltSkill `json:"skills"`
}

// SpaceMoltSkill is the format from server_docs/skills.20260209.json
type SpaceMoltSkill struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Category        string         `json:"category"`
	Description     string         `json:"description"`
	MaxLevel        int            `json:"max_level"`
	RequiredSkills  map[string]int `json:"required_skills,omitempty"`
	XPPerLevel      []int          `json:"xp_per_level,omitempty"`
	BonusPerLevel   map[string]any `json:"bonus_per_level,omitempty"`
}

// ImportSkill is the format expected by the crafting server sync
type ImportSkill struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Category      string               `json:"category,omitempty"`
	Description   string               `json:"description,omitempty"`
	MaxLevel      int                  `json:"max_level,omitempty"`
	Prerequisites []ImportPrerequisite `json:"prerequisites,omitempty"`
	Levels        []ImportLevel        `json:"levels,omitempty"`
}

type ImportPrerequisite struct {
	SkillID string `json:"skill_id"`
	Level   int    `json:"level"`
}

type ImportLevel struct {
	Level      int `json:"level"`
	XPRequired int `json:"xp_required"`
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
	var smSkills SpaceMoltSkillFile
	if err := json.Unmarshal(data, &smSkills); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing input JSON: %v\n", err)
		os.Exit(1)
	}

	// Convert to import format
	var importSkills []ImportSkill
	for _, smSkill := range smSkills.Skills {
		// Build prerequisites
		var prerequisites []ImportPrerequisite
		for skillID, level := range smSkill.RequiredSkills {
			prerequisites = append(prerequisites, ImportPrerequisite{
				SkillID: skillID,
				Level:   level,
			})
		}

		// Build level XP requirements (cumulative)
		var levels []ImportLevel
		cumulativeXP := 0
		for i, xp := range smSkill.XPPerLevel {
			cumulativeXP += xp
			levels = append(levels, ImportLevel{
				Level:      i + 1,
				XPRequired: cumulativeXP,
			})
		}

		importSkill := ImportSkill{
			ID:            smSkill.ID,
			Name:          smSkill.Name,
			Category:      smSkill.Category,
			Description:   smSkill.Description,
			MaxLevel:      smSkill.MaxLevel,
			Prerequisites: prerequisites,
			Levels:        levels,
		}

		importSkills = append(importSkills, importSkill)
	}

	// Write output file
	outputData, err := json.MarshalIndent(importSkills, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling output JSON: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, outputData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Converted %d skills from %s to %s\n", len(importSkills), inputPath, outputPath)
}
