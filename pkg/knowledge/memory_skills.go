package knowledge

// GetSkill retrieves a single skill by ID from the static skill definitions
func (kb *MemoryKB) GetSkill(id string) (*Skill, error) {
	return getStaticSkill(id), nil
}

// GetSkills retrieves all skills from the static skill definitions
func (kb *MemoryKB) GetSkills() []Skill {
	return getStaticSkills()
}
