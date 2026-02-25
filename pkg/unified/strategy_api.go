package unified

import (
	"encoding/json"
	"net/http"

	"github.com/rsned/spacemolt/pkg/observe"
)

// registerStrategyRoutes adds strategy-related REST endpoints to the mux.
func (s *Server) registerStrategyRoutes() {
	s.mux.HandleFunc("GET /api/strategies", s.handleListStrategies)
	s.mux.HandleFunc("GET /api/agents/{id}/strategy", s.handleGetAgentStrategy)
	s.mux.HandleFunc("PUT /api/agents/{id}/strategy", s.handleSetAgentStrategy)
}

// handleListStrategies returns all available strategies.
// GET /api/strategies
func (s *Server) handleListStrategies(w http.ResponseWriter, _ *http.Request) {
	if s.strategies == nil {
		observe.WriteJSON(w, http.StatusOK, []any{})
		return
	}
	observe.WriteJSON(w, http.StatusOK, s.strategies.List())
}

// handleGetAgentStrategy returns the current strategy for an agent.
// GET /api/agents/{id}/strategy
func (s *Server) handleGetAgentStrategy(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	if agentID == "" {
		observe.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agent id is required"})
		return
	}

	sr, ok := s.manager.GetStrategyRunner(agentID)
	if !ok {
		observe.WriteJSON(w, http.StatusNotFound, map[string]string{
			"error":    "no strategy assigned",
			"agent_id": agentID,
		})
		return
	}

	observe.WriteJSON(w, http.StatusOK, map[string]any{
		"agent_id": agentID,
		"strategy": sr.Strategy().Name(),
		"status":   sr.Strategy().CurrentStatus(),
		"running":  sr.IsRunning(),
	})
}

// handleSetAgentStrategy assigns or swaps a strategy for an agent.
// PUT /api/agents/{id}/strategy
func (s *Server) handleSetAgentStrategy(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	if agentID == "" {
		observe.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agent id is required"})
		return
	}

	if s.strategies == nil {
		observe.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "strategy registry not configured"})
		return
	}

	var req struct {
		Strategy   string            `json:"strategy"`
		Parameters map[string]string `json:"parameters,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		observe.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	strat := s.strategies.Get(req.Strategy)
	if strat == nil {
		observe.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":    "unknown strategy",
			"strategy": req.Strategy,
		})
		return
	}

	// Check if agent already has a strategy - swap it
	if _, ok := s.manager.GetStrategyRunner(agentID); ok {
		if err := s.manager.SwapStrategy(r.Context(), agentID, strat, req.Parameters); err != nil {
			observe.WriteJSON(w, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
			return
		}
		observe.WriteJSON(w, http.StatusOK, map[string]any{
			"status":   "swapped",
			"agent_id": agentID,
			"strategy": strat.Name(),
		})
		return
	}

	// Spawn new strategy agent
	_, err := s.manager.SpawnStrategyAgent(r.Context(), agentID, strat, req.Parameters)
	if err != nil {
		observe.WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	observe.WriteJSON(w, http.StatusCreated, map[string]any{
		"status":   "assigned",
		"agent_id": agentID,
		"strategy": strat.Name(),
	})
}
