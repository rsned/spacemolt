package unified

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/rsned/spacemolt/pkg/observe"
	"github.com/rsned/spacemolt/pkg/team"
)

// registerTeamRoutes adds team-related REST endpoints to the mux.
func (s *Server) registerTeamRoutes() {
	s.mux.HandleFunc("GET /api/teams", s.handleListTeams)
	s.mux.HandleFunc("POST /api/teams", s.handleCreateTeam)
	s.mux.HandleFunc("GET /api/teams/{id}", s.handleGetTeam)
	s.mux.HandleFunc("PUT /api/teams/{id}", s.handleUpdateTeam)
	s.mux.HandleFunc("DELETE /api/teams/{id}", s.handleDeleteTeam)
	s.mux.HandleFunc("POST /api/teams/{id}/orders", s.handleIssueOrder)
	s.mux.HandleFunc("GET /api/teams/{id}/log", s.handleGetTeamLog)
}

func (s *Server) handleListTeams(w http.ResponseWriter, _ *http.Request) {
	if s.teamCoordinator == nil {
		observe.WriteJSON(w, http.StatusOK, []any{})
		return
	}
	observe.WriteJSON(w, http.StatusOK, s.teamCoordinator.ListTeams())
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	if s.teamCoordinator == nil {
		observe.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team system not initialized"})
		return
	}

	var t team.Team
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		observe.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if t.ID == "" || t.Name == "" {
		observe.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id and name are required"})
		return
	}

	s.teamCoordinator.AddTeam(t)
	observe.WriteJSON(w, http.StatusCreated, t)
}

func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	if s.teamCoordinator == nil {
		observe.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team system not initialized"})
		return
	}

	teamID := r.PathValue("id")
	t := s.teamCoordinator.GetTeam(teamID)
	if t == nil {
		observe.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}

	// Collect fresh status.
	s.teamCoordinator.CollectStatusReports(r.Context())
	status := s.teamCoordinator.GetTeamStatus(teamID)

	observe.WriteJSON(w, http.StatusOK, map[string]any{
		"team":    t,
		"members": status,
	})
}

func (s *Server) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	if s.teamCoordinator == nil {
		observe.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team system not initialized"})
		return
	}

	teamID := r.PathValue("id")
	existing := s.teamCoordinator.GetTeam(teamID)
	if existing == nil {
		observe.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}

	var updated team.Team
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		observe.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	updated.ID = teamID
	s.teamCoordinator.AddTeam(updated)
	observe.WriteJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	if s.teamCoordinator == nil {
		observe.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team system not initialized"})
		return
	}

	teamID := r.PathValue("id")
	if err := s.teamCoordinator.RemoveTeam(teamID); err != nil {
		observe.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	observe.WriteJSON(w, http.StatusOK, map[string]string{"status": "disbanded"})
}

func (s *Server) handleIssueOrder(w http.ResponseWriter, r *http.Request) {
	if s.teamCoordinator == nil {
		observe.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team system not initialized"})
		return
	}

	teamID := r.PathValue("id")

	var order team.Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		observe.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if order.ExpiresAt.IsZero() {
		order.ExpiresAt = time.Now().Add(5 * time.Minute)
	}

	if err := s.teamCoordinator.IssueOrder(teamID, order); err != nil {
		observe.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	observe.WriteJSON(w, http.StatusOK, map[string]string{"status": "dispatched"})
}

func (s *Server) handleGetTeamLog(w http.ResponseWriter, r *http.Request) {
	if s.teamCoordinator == nil {
		observe.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team system not initialized"})
		return
	}

	teamID := r.PathValue("id")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	entries := s.teamCoordinator.GetActionLog(teamID, limit)
	if entries == nil {
		entries = []team.ActionLogEntry{}
	}
	observe.WriteJSON(w, http.StatusOK, entries)
}
