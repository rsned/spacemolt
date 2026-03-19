package unified

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/rsned/spacemolt/pkg/observe"
)

// registerModelRoutes adds LLM model management endpoints to the mux.
func (s *Server) registerModelRoutes() {
	s.mux.HandleFunc("GET /api/model", s.handleGetModel)
	s.mux.HandleFunc("PUT /api/model", s.handleSetModel)
}

// handleGetModel returns the current LLM model and available models from Ollama.
// GET /api/model
func (s *Server) handleGetModel(w http.ResponseWriter, _ *http.Request) {
	if s.llm == nil {
		http.Error(w, "LLM client not configured", http.StatusServiceUnavailable)
		return
	}

	available := s.fetchOllamaModels()
	observe.WriteJSON(w, http.StatusOK, map[string]any{
		"current":   s.llm.Model(),
		"available": available,
	})
}

// handleSetModel switches the LLM model for subsequent calls.
// PUT /api/model  {"model": "name"}
func (s *Server) handleSetModel(w http.ResponseWriter, r *http.Request) {
	if s.llm == nil {
		http.Error(w, "LLM client not configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		http.Error(w, `invalid request: need {"model": "name"}`, http.StatusBadRequest)
		return
	}

	old := s.llm.Model()
	s.llm.SetModel(req.Model)
	s.logger.Printf("LLM model switched: %s → %s", old, req.Model)

	observe.WriteJSON(w, http.StatusOK, map[string]string{
		"previous": old,
		"current":  req.Model,
	})
}

// fetchOllamaModels queries Ollama for installed models.
func (s *Server) fetchOllamaModels() []string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(s.llm.BaseURL() + "/api/tags")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	names := make([]string, len(result.Models))
	for i, m := range result.Models {
		names[i] = m.Name
	}
	return names
}
