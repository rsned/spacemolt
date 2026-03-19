package unified

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rsned/spacemolt/pkg/api"
)

// registerStreamRoutes adds SSE streaming endpoints to the mux.
func (s *Server) registerStreamRoutes() {
	s.mux.HandleFunc("GET /api/agents/{id}/stream", s.handleAgentStream)
}

// handleAgentStream provides a Server-Sent Events stream of agent events.
// GET /api/agents/{id}/stream
func (s *Server) handleAgentStream(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")

	// Verify agent exists (check both LLM runners and strategy runners).
	_, hasRunner := s.manager.GetRunner(agentID)
	_, hasStrategy := s.manager.GetStrategyRunner(agentID)
	if !hasRunner && !hasStrategy {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	eventCh := s.streams.Subscribe(agentID)
	defer s.streams.Unsubscribe(agentID, eventCh)

	// Send initial connected event.
	_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"agent_id\":%q,\"status\":\"connected\"}\n\n", agentID)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}

// makeEventPublisher returns an EventCallback that publishes to the stream manager.
func (s *Server) makeEventPublisher() func(agentID string, eventType string, data any) {
	return func(agentID string, eventType string, data any) {
		s.streams.Publish(agentID, api.Event{
			AgentID: agentID,
			Type:    eventType,
			Data:    data,
		})
	}
}
