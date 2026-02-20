package knowledge

import (
	"encoding/json"
)

// Skill represents a skill in SpaceMolt with its XP requirements
type Skill struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Category       string         `json:"category"`
	Description    string         `json:"description"`
	MaxLevel       int            `json:"max_level"`
	TrainingSource string         `json:"training_source,omitempty"`
	XPPerLevel     []int          `json:"xp_per_level"`
	BonusPerLevel  map[string]int `json:"bonus_per_level,omitempty"`
	RequiredSkills map[string]int `json:"required_skills,omitempty"`
}

// GetSkill retrieves a single skill by ID, reading from the database first
// and falling back to static data if no skills are stored.
func (kb *SQLiteKB) GetSkill(id string) (*Skill, error) {
	var s Skill
	var xpJSON, bonusJSON, reqJSON string
	err := kb.db.QueryRow(`
		SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), max_level,
			COALESCE(training_source, ''), xp_per_level, bonus_per_level, required_skills
		FROM skills WHERE id = ?
	`, id).Scan(&s.ID, &s.Name, &s.Description, &s.Category, &s.MaxLevel,
		&s.TrainingSource, &xpJSON, &bonusJSON, &reqJSON)
	if err != nil {
		// Fallback to static data if DB query fails (table may not exist yet or row not found)
		return getStaticSkill(id), nil //nolint:nilerr // intentional fallback
	}
	_ = json.Unmarshal([]byte(xpJSON), &s.XPPerLevel)
	_ = json.Unmarshal([]byte(bonusJSON), &s.BonusPerLevel)
	_ = json.Unmarshal([]byte(reqJSON), &s.RequiredSkills)
	return &s, nil
}

// GetSkills retrieves all skills, reading from the database first
// and falling back to static data if no skills are stored.
func (kb *SQLiteKB) GetSkills() []Skill {
	rows, err := kb.db.Query(`
		SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), max_level,
			COALESCE(training_source, ''), xp_per_level, bonus_per_level, required_skills
		FROM skills ORDER BY category, name
	`)
	if err != nil {
		return getStaticSkills()
	}
	defer func() { _ = rows.Close() }()

	var skills []Skill
	for rows.Next() {
		var s Skill
		var xpJSON, bonusJSON, reqJSON string
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Category, &s.MaxLevel,
			&s.TrainingSource, &xpJSON, &bonusJSON, &reqJSON); err != nil {
			return getStaticSkills()
		}
		_ = json.Unmarshal([]byte(xpJSON), &s.XPPerLevel)
		_ = json.Unmarshal([]byte(bonusJSON), &s.BonusPerLevel)
		_ = json.Unmarshal([]byte(reqJSON), &s.RequiredSkills)
		skills = append(skills, s)
	}
	if err := rows.Err(); err != nil {
		return getStaticSkills()
	}

	// Fall back to static data if DB is empty
	if len(skills) == 0 {
		return getStaticSkills()
	}

	return skills
}

// getStaticSkills returns the hardcoded skill definitions as a fallback
// when the skills table is empty or unavailable.
func getStaticSkills() []Skill {
	return []Skill{
		{
			ID: "mining_basic", Name: "Mining", Category: "Mining",
			Description: "Basic ore extraction techniques", MaxLevel: 10,
			XPPerLevel: []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID: "mining_advanced", Name: "Advanced Mining", Category: "Mining",
			Description: "Advanced ore extraction and refining", MaxLevel: 10,
			XPPerLevel:     []int{500, 1500, 3000, 5000, 8000, 12000, 17000, 23000, 30000, 40000},
			RequiredSkills: map[string]int{"mining_basic": 5},
		},
		{
			ID: "exploration", Name: "Exploration", Category: "Exploration",
			Description: "Discovery and mapping of new systems", MaxLevel: 10,
			XPPerLevel: []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID: "trading", Name: "Trading", Category: "Trade",
			Description: "Buy low, sell high", MaxLevel: 10,
			XPPerLevel: []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID: "fuel_efficiency", Name: "Fuel Efficiency", Category: "Engineering",
			Description: "Reduce fuel consumption during travel", MaxLevel: 10,
			XPPerLevel: []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID: "navigation", Name: "Navigation", Category: "Piloting",
			Description: "Improved ship handling and jump gate accuracy", MaxLevel: 10,
			XPPerLevel: []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID: "scanning", Name: "Scanning", Category: "Exploration",
			Description: "Enhanced sensor and scanning capabilities", MaxLevel: 10,
			XPPerLevel: []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID: "small_ships", Name: "Small Ships", Category: "Piloting",
			Description: "Proficiency with small ship classes", MaxLevel: 10,
			XPPerLevel: []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
	}
}

// getStaticSkill returns a single skill by ID from static data.
func getStaticSkill(id string) *Skill {
	for _, s := range getStaticSkills() {
		if s.ID == id {
			return &s
		}
	}
	return nil
}

// GetXPForLevel returns the XP required to reach a specific level (1-indexed)
func (s *Skill) GetXPForLevel(level int) int {
	if level < 1 || level > s.MaxLevel {
		return 0
	}
	if level-1 < len(s.XPPerLevel) {
		return s.XPPerLevel[level-1]
	}
	return 0
}

// GetNextLevelXP returns the XP required to reach the next level from current level
func (s *Skill) GetNextLevelXP(currentLevel int) int {
	if currentLevel >= s.MaxLevel {
		return 0
	}
	return s.GetXPForLevel(currentLevel + 1)
}

