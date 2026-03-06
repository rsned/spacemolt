package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// SkillsResponse represents the server response from catalog_skills
type SkillsResponse struct {
	Items []SkillJSON `json:"items"`
}

// SkillJSON represents a skill from the game catalog JSON
type SkillJSON struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Category       string         `json:"category"`
	MaxLevel       int            `json:"max_level"`
	TrainingSource string         `json:"training_source"`
	XPPerLevel     []int          `json:"xp_per_level"`
	BonusPerLevel     map[string]int `json:"bonus_per_level"`
	RequiredSkills    map[string]int `json:"required_skills"`
	EmpireRestriction string         `json:"empire_restriction"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <catalog-skills.json>", os.Args[0])
	}

	jsonFile := os.Args[1]

	// Read the JSON file
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var response SkillsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(response.Items) == 0 {
		log.Fatalf("No skills found in JSON file")
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

	// Convert JSON skills to knowledge base skills
	skills := make([]knowledge.Skill, len(response.Items))
	for i, s := range response.Items {
		skills[i] = knowledge.Skill{
			ID:                s.ID,
			Name:              s.Name,
			Description:       s.Description,
			Category:          s.Category,
			MaxLevel:          s.MaxLevel,
			TrainingSource:    s.TrainingSource,
			XPPerLevel:        s.XPPerLevel,
			BonusPerLevel:     s.BonusPerLevel,
			RequiredSkills:    s.RequiredSkills,
			EmpireRestriction: s.EmpireRestriction,
		}
	}

	// Store skills in database
	if err := kb.StoreSkills(ctx, skills); err != nil {
		log.Fatalf("Failed to store skills: %v", err)
	}

	log.Printf("✓ Successfully imported %d skills\n", len(skills))
}
