package knowledge

import "context"

// GetSkill retrieves a single skill by ID.
func (kb *MemoryKB) GetSkill(id string) (*Skill, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if s, ok := kb.skills[id]; ok {
		return &s, nil
	}
	return getStaticSkill(id), nil
}

// GetSkills retrieves all skills.
func (kb *MemoryKB) GetSkills() []Skill {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if len(kb.skills) > 0 {
		skills := make([]Skill, 0, len(kb.skills))
		for _, s := range kb.skills {
			skills = append(skills, s)
		}
		return skills
	}
	return getStaticSkills()
}

// StoreSkills stores skills in memory.
func (kb *MemoryKB) StoreSkills(ctx context.Context, skills []Skill) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.skills = make(map[string]Skill, len(skills))
	for _, s := range skills {
		kb.skills[s.ID] = s
	}
	return nil
}
