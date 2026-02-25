package monitor

import (
	"net/http"

	"github.com/rsned/spacemolt/pkg/observe"
)

// RegisterRoutes registers the admin monitoring REST endpoints on the given mux.
func RegisterRoutes(mux *http.ServeMux, c *Collector) {
	mux.HandleFunc("GET /api/admin/health", handleHealth(c))
	mux.HandleFunc("GET /api/admin/metrics", handleMetrics(c))
	mux.HandleFunc("GET /api/admin/agents", handleAgents(c))
	mux.HandleFunc("GET /api/admin/llm", handleLLM(c))
	mux.HandleFunc("GET /api/admin/knowledge", handleKnowledge(c))
}

func handleHealth(c *Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		observe.WriteJSON(w, http.StatusOK, c.ServerHealth())
	}
}

func handleMetrics(c *Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		observe.WriteJSON(w, http.StatusOK, map[string]any{
			"server": c.ServerHealth(),
			"agents": c.AgentMetrics(),
			"llm":    c.LLMHealth(),
			"kb":     c.KnowledgeStats(),
		})
	}
}

func handleAgents(c *Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		agents := c.AgentMetrics()
		if agents == nil {
			agents = []AgentMetric{}
		}
		observe.WriteJSON(w, http.StatusOK, agents)
	}
}

func handleLLM(c *Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		observe.WriteJSON(w, http.StatusOK, c.LLMHealth())
	}
}

func handleKnowledge(c *Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		observe.WriteJSON(w, http.StatusOK, c.KnowledgeStats())
	}
}
