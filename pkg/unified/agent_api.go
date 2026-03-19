package unified

import (
	"net/http"
	"strconv"

	"github.com/rsned/spacemolt/pkg/observe"
)

// registerAgentDetailRoutes adds per-agent detail endpoints to the mux.
func (s *Server) registerAgentDetailRoutes() {
	s.mux.HandleFunc("GET /api/agents/{id}/details", s.handleGetAgentDetails)
	s.mux.HandleFunc("GET /api/agents/{id}/state", s.handleGetAgentState)
	s.mux.HandleFunc("GET /api/agents/{id}/history", s.handleGetAgentHistory)
}

// handleGetAgentDetails returns detailed agent info including runtime status.
// GET /api/agents/{id}/details
func (s *Server) handleGetAgentDetails(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")

	runner, ok := s.manager.GetRunner(agentID)
	if !ok {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	a := runner.GetAgent()
	status := a.Status()
	personality := a.Personality()
	gameClient := runner.GetGameClient()
	state := gameClient.GetState()

	empire := ""
	if state != nil {
		empire = state.Player.Empire
	}

	observe.WriteJSON(w, http.StatusOK, map[string]any{
		"id":              a.ID(),
		"name":            a.Name(),
		"role":            personality.Role,
		"status":          status.State.String(),
		"empire":          empire,
		"faction":         personality.Faction,
		"current_action":  status.CurrentAction,
		"last_action_tick": runner.GetLastActionTick(),
		"is_running":      runner.IsRunning(),
		"has_crashed":     runner.HasCrashed(),
		"crash_count":     runner.GetCrashCount(),
	})
}

// handleGetAgentState returns the full game state for an agent.
// GET /api/agents/{id}/state
func (s *Server) handleGetAgentState(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")

	runner, ok := s.manager.GetRunner(agentID)
	if !ok {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	gameClient := runner.GetGameClient()
	state := gameClient.GetState()
	if state == nil {
		http.Error(w, "Game state not available", http.StatusServiceUnavailable)
		return
	}

	observe.WriteJSON(w, http.StatusOK, state.Clone())
}

// handleGetAgentHistory returns the action history for an agent.
// GET /api/agents/{id}/history?limit=100
func (s *Server) handleGetAgentHistory(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")

	runner, ok := s.manager.GetRunner(agentID)
	if !ok {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = min(parsed, 1000)
		}
	}

	observe.WriteJSON(w, http.StatusOK, runner.GetHistory(limit))
}
