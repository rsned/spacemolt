package knowledge

// Skill represents a skill in SpaceMolt with its XP requirements
type Skill struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Category         string   `json:"category"`
	Description      string   `json:"description"`
	MaxLevel         int      `json:"max_level"`
	XPPerLevel       []int    `json:"xp_per_level"`
	BonusPerLevel    map[string]int `json:"bonus_per_level,omitempty"`
	RequiredSkills   map[string]int `json:"required_skills,omitempty"`
}

// GetSkill retrieves a single skill by ID
func (kb *SQLiteKB) GetSkill(id string) (*Skill, error) {
	// For now, return hardcoded skill data
	// TODO: Store skills in database and fetch from there
	return getStaticSkill(id), nil
}

// GetSkills retrieves all skills
func (kb *SQLiteKB) GetSkills() []Skill {
	// For now, return hardcoded skill data
	// TODO: Store skills in database and fetch from there
	return getStaticSkills()
}

// getStaticSkills returns the hardcoded skill definitions
// TODO: Load from database once skills table is created
func getStaticSkills() []Skill {
	return []Skill{
		{
			ID:          "mining_basic",
			Name:        "Mining",
			Category:    "Mining",
			Description: "Basic ore extraction techniques",
			MaxLevel:    10,
			XPPerLevel:  []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID:          "mining_advanced",
			Name:        "Advanced Mining",
			Category:    "Mining",
			Description: "Advanced ore extraction and refining",
			MaxLevel:    10,
			XPPerLevel:  []int{500, 1500, 3000, 5000, 8000, 12000, 17000, 23000, 30000, 40000},
			RequiredSkills: map[string]int{"mining_basic": 5},
		},
		{
			ID:          "exploration",
			Name:        "Exploration",
			Category:    "Exploration",
			Description: "Discovery and mapping of new systems",
			MaxLevel:    10,
			XPPerLevel:  []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID:          "trading",
			Name:        "Trading",
			Category:    "Trade",
			Description: "Buy low, sell high",
			MaxLevel:    10,
			XPPerLevel:  []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID:          "fuel_efficiency",
			Name:        "Fuel Efficiency",
			Category:    "Engineering",
			Description: "Reduce fuel consumption during travel",
			MaxLevel:    10,
			XPPerLevel:  []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID:          "navigation",
			Name:        "Navigation",
			Category:    "Piloting",
			Description: "Improved ship handling and jump gate accuracy",
			MaxLevel:    10,
			XPPerLevel:  []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID:          "scanning",
			Name:        "Scanning",
			Category:    "Exploration",
			Description: "Enhanced sensor and scanning capabilities",
			MaxLevel:    10,
			XPPerLevel:  []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
		{
			ID:          "small_ships",
			Name:        "Small Ships",
			Category:    "Piloting",
			Description: "Proficiency with small ship classes",
			MaxLevel:    10,
			XPPerLevel:  []int{100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500, 5500},
		},
	}
}

// getStaticSkill returns a single skill by ID
func getStaticSkill(id string) *Skill {
	skills := getStaticSkills()
	for _, s := range skills {
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
	// level 1 = index 0, level 2 = index 1, etc.
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
