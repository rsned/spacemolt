package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// handleRegister processes tool registration requests
// POST /api/tools/register
func (s *RegistryServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reg ToolRegistration
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if reg.ToolID == "" || reg.ToolType == "" {
		http.Error(w, "tool_id and tool_type are required", http.StatusBadRequest)
		return
	}

	// Set timestamps
	now := time.Now()
	reg.RegisteredAt = now
	reg.LastHeartbeat = now

	// Store registration
	s.mu.Lock()
	s.tools[reg.ToolID] = &reg
	s.mu.Unlock()

	s.logger.Printf("Tool registered: %s (type: %s)", reg.ToolID, reg.ToolType)

	// Broadcast registration event
	s.streamMgr.Publish(Event{
		Type: "tool_registered",
		Data: reg,
	})

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"registration_id":    fmt.Sprintf("reg-%d", now.Unix()),
		"heartbeat_interval": "5s",
	})
}

// handleToolRoute routes /api/tools/{id}/* requests
func (s *RegistryServer) handleToolRoute(w http.ResponseWriter, r *http.Request) {
	// Parse tool ID and sub-path
	path := strings.TrimPrefix(r.URL.Path, "/api/tools/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Tool ID required", http.StatusBadRequest)
		return
	}

	toolID := parts[0]
	subPath := ""
	if len(parts) > 1 {
		subPath = parts[1]
	}

	// Route to appropriate handler
	switch subPath {
	case "":
		s.handleGetTool(w, r, toolID)
	case "heartbeat":
		s.handleHeartbeat(w, r, toolID)
	default:
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
	}
}

// handleGetTool returns details for a specific tool
// GET /api/tools/{id}
func (s *RegistryServer) handleGetTool(w http.ResponseWriter, r *http.Request, toolID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	tool, exists := s.tools[toolID]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Tool not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tool)
}

// handleHeartbeat processes tool heartbeat requests
// POST /api/tools/{id}/heartbeat
func (s *RegistryServer) handleHeartbeat(w http.ResponseWriter, r *http.Request, toolID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	tool, exists := s.tools[toolID]
	if exists {
		oldStatus := tool.Status
		tool.Status = req.Status
		if req.CurrentAction != "" {
			tool.CurrentAction = req.CurrentAction
		}
		tool.LastHeartbeat = time.Now()

		// Broadcast status change if status changed
		if oldStatus != req.Status {
			s.streamMgr.Publish(Event{
				Type: "tool_status_changed",
				Data: map[string]interface{}{
					"tool_id":        toolID,
					"status":         req.Status,
					"current_action": req.CurrentAction,
				},
			})
		}

		// Broadcast heartbeat event
		s.streamMgr.Publish(Event{
			Type: "tool_heartbeat",
			Data: map[string]interface{}{
				"tool_id": toolID,
				"health":  req.Health,
			},
		})
	}
	s.mu.Unlock()

	if !exists {
		http.Error(w, "Tool not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleListTools returns all registered tools
// GET /api/tools
func (s *RegistryServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	tools := make([]*ToolRegistration, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, tool)
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tools)
}

// handleStatusStream provides Server-Sent Events for status updates
// GET /api/status/stream
func (s *RegistryServer) handleStatusStream(w http.ResponseWriter, r *http.Request) {
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

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Subscribe to events
	eventCh := s.streamMgr.Subscribe()
	defer s.streamMgr.Unsubscribe(eventCh)

	// Send initial connected event
	fmt.Fprintf(w, "event: connected\n")
	fmt.Fprintf(w, "data: {\"status\":\"connected\",\"timestamp\":\"%s\"}\n\n", time.Now().Format(time.RFC3339))
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
			data, err := json.Marshal(event.Data)
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

// corsMiddleware adds CORS headers to all responses
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
