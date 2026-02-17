package main

import (
	"net/http"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// skillJSON is the JSON representation of a skill for API responses
type skillJSON struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Category    string         `json:"category"`
	Description string         `json:"description"`
	MaxLevel    int            `json:"max_level"`
	XPPerLevel  []int          `json:"xp_per_level"`
}

// HandleGetSkills returns all available skills
// GET /api/skills
func (s *ObserverServer) HandleGetSkills(w http.ResponseWriter, _ *http.Request) {
	skills := s.kb.GetSkills()
	result := make([]skillJSON, 0, len(skills))
	for _, skill := range skills {
		result = append(result, skillToJSON(skill))
	}
	writeJSON(w, http.StatusOK, result)
}

// HandleGetSkill returns a single skill by ID
// GET /api/skills/{id}
func (s *ObserverServer) HandleGetSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"skill id is required"}`, http.StatusBadRequest)
		return
	}

	skill, err := s.kb.GetSkill(id)
	if err != nil {
		s.logger.Printf("error getting skill %s: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if skill == nil {
		http.Error(w, `{"error":"skill not found"}`, http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, skillToJSON(*skill))
}

// HandleGetPlayerSkills calculates XP progress for player's current skills
// GET /api/player/skills?agent={username}
func (s *ObserverServer) HandleGetPlayerSkills(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent == "" {
		http.Error(w, `{"error":"agent parameter is required"}`, http.StatusBadRequest)
		return
	}

	// Get the agent's current skill levels from their game state
	// This would need to be stored or fetched from the game server
	// For now, return a placeholder response
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Player skills endpoint not yet implemented",
	})
}

func skillToJSON(skill knowledge.Skill) skillJSON {
	return skillJSON{
		ID:          skill.ID,
		Name:        skill.Name,
		Category:    skill.Category,
		Description: skill.Description,
		MaxLevel:    skill.MaxLevel,
		XPPerLevel:  skill.XPPerLevel,
	}
}
