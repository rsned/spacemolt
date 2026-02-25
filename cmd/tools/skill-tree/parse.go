// Package main parses skills.json for the skill-tree diagram generator.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Skill represents a single skill from the skills catalog.
type Skill struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Category       string         `json:"category"`
	RequiredSkills map[string]int `json:"required_skills"`
}

// SkillsPayload is the top-level structure of skills.json.
type SkillsPayload struct {
	Skills map[string]Skill `json:"skills"`
}

// UnmarshalJSON implements custom unmarshaling for required_skills.
// JSON numbers decode as float64; we convert to int.
func (s *Skill) UnmarshalJSON(data []byte) error {
	type skillAlias Skill
	aux := &struct {
		*skillAlias
		RequiredSkills map[string]interface{} `json:"required_skills"`
	}{
		skillAlias: (*skillAlias)(s),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	s.RequiredSkills = make(map[string]int)
	for k, v := range aux.RequiredSkills {
		if v == nil {
			continue
		}
		switch val := v.(type) {
		case float64:
			s.RequiredSkills[k] = int(val)
		case int:
			s.RequiredSkills[k] = val
		}
	}
	return nil
}

// LoadSkills reads and parses skills.json from path.
func LoadSkills(path string) (*SkillsPayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var payload SkillsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if payload.Skills == nil {
		return nil, fmt.Errorf("missing 'skills' object")
	}
	return &payload, nil
}
