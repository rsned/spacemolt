package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/agent"
)

// AgentInfo represents basic agent information
type AgentInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// AgentDetails extends AgentInfo with additional runtime information
type AgentDetails struct {
	AgentInfo
	CurrentAction  string `json:"current_action,omitempty"`
	LastActionTick int64  `json:"last_action_tick"`
	IsRunning      bool   `json:"is_running"`
	HasCrashed     bool   `json:"has_crashed"`
	CrashCount     int    `json:"crash_count"`
	Faction        string `json:"faction"`
}

// handleListAgents returns list of all active agents
// GET /api/agents
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	runners := s.manager.ListRunners()
	agentInfos := make([]AgentInfo, 0, len(runners))

	for _, runner := range runners {
		agent := runner.GetAgent()
		status := agent.Status()

		agentInfos = append(agentInfos, AgentInfo{
			ID:     agent.ID(),
			Name:   agent.Name(),
			Role:   agent.Personality().Role,
			Status: status.State.String(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(agentInfos); err != nil {
		s.logger.Printf("Error encoding agents list: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleAgentRoute routes /api/agents/{id}/* requests
func (s *Server) handleAgentRoute(w http.ResponseWriter, r *http.Request) {
	// Parse agent ID and sub-path
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Agent ID required", http.StatusBadRequest)
		return
	}

	agentID := parts[0]
	subPath := ""
	if len(parts) > 1 {
		subPath = parts[1]
	}

	// Get the runner
	runner, ok := s.manager.GetRunner(agentID)
	if !ok {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	// Route to appropriate handler
	switch subPath {
	case "":
		s.handleGetAgent(w, r, runner)
	case "state":
		s.handleGetAgentState(w, r, runner)
	case "history":
		s.handleGetAgentHistory(w, r, runner)
	case "stream":
		s.handleAgentStream(w, r, runner)
	default:
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
	}
}

// handleGetAgent returns agent details
// GET /api/agents/{id}
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request, runner *agent.Runner) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agent := runner.GetAgent()
	status := agent.Status()
	personality := agent.Personality()

	details := AgentDetails{
		AgentInfo: AgentInfo{
			ID:     agent.ID(),
			Name:   agent.Name(),
			Role:   personality.Role,
			Status: status.State.String(),
		},
		CurrentAction:  status.CurrentAction,
		LastActionTick: runner.GetLastActionTick(),
		IsRunning:      runner.IsRunning(),
		HasCrashed:     runner.HasCrashed(),
		CrashCount:     runner.GetCrashCount(),
		Faction:        personality.Faction,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(details); err != nil {
		s.logger.Printf("Error encoding agent details: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleGetAgentState returns full game state
// GET /api/agents/{id}/state
func (s *Server) handleGetAgentState(w http.ResponseWriter, r *http.Request, runner *agent.Runner) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	gameClient := runner.GetGameClient()
	state := gameClient.GetState()

	if state == nil {
		http.Error(w, "Game state not available", http.StatusServiceUnavailable)
		return
	}

	// Clone state for safe serialization
	stateCopy := state.Clone()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stateCopy); err != nil {
		s.logger.Printf("Error encoding game state: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleGetAgentHistory returns action history
// GET /api/agents/{id}/history?limit=100
func (s *Server) handleGetAgentHistory(w http.ResponseWriter, r *http.Request, runner *agent.Runner) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse limit parameter
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
			if limit > 1000 {
				limit = 1000 // Cap at 1000
			}
		}
	}

	// Get history from runner
	history := runner.GetHistory(limit)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(history); err != nil {
		s.logger.Printf("Error encoding history: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleAgentStream provides Server-Sent Events stream
// GET /api/agents/{id}/stream
func (s *Server) handleAgentStream(w http.ResponseWriter, r *http.Request, runner *agent.Runner) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set headers for SSE (CORS handled by middleware)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	agentID := runner.GetAgent().ID()

	// Subscribe to events
	eventCh := s.streamManager.Subscribe(agentID)
	defer s.streamManager.Unsubscribe(agentID, eventCh)

	// Send initial connected event
	fmt.Fprintf(w, "event: connected\n")
	fmt.Fprintf(w, "data: {\"agent_id\":\"%s\",\"status\":\"connected\"}\n\n", agentID)
	flusher.Flush()

	// Stream events until client disconnects
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			return
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed
				return
			}

			// Marshal event to JSON
			data, err := json.Marshal(event)
			if err != nil {
				s.logger.Printf("Error marshaling event: %v", err)
				continue
			}

			// Send SSE event
			fmt.Fprintf(w, "event: %s\n", event.Type)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
