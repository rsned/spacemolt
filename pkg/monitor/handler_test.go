package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	c := NewCollector()
	RegisterRoutes(mux, c)

	endpoints := []struct {
		path string
	}{
		{"/api/admin/health"},
		{"/api/admin/metrics"},
		{"/api/admin/agents"},
		{"/api/admin/llm"},
		{"/api/admin/knowledge"},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
			}

			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("content-type: got %q, want %q", ct, "application/json")
			}
		})
	}
}

func TestHandleHealthResponse(t *testing.T) {
	mux := http.NewServeMux()
	c := NewCollector()
	c.SetAgentCountFunc(func() (int, int) { return 2, 3 })
	RegisterRoutes(mux, c)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result ServerMetrics
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if result.LLMAgents != 2 {
		t.Errorf("LLMAgents: got %d, want 2", result.LLMAgents)
	}
	if result.StrategyAgents != 3 {
		t.Errorf("StrategyAgents: got %d, want 3", result.StrategyAgents)
	}
	if result.TotalAgents != 5 {
		t.Errorf("TotalAgents: got %d, want 5", result.TotalAgents)
	}
	if result.GoRoutines <= 0 {
		t.Error("GoRoutines should be positive")
	}
}

func TestHandleMetricsResponse(t *testing.T) {
	mux := http.NewServeMux()
	c := NewCollector()
	RegisterRoutes(mux, c)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	for _, key := range []string{"server", "agents", "llm", "kb"} {
		if _, ok := result[key]; !ok {
			t.Errorf("missing key %q in metrics response", key)
		}
	}
}

func TestHandleAgentsEmptyList(t *testing.T) {
	mux := http.NewServeMux()
	c := NewCollector()
	// agentMetricsFn is nil, so AgentMetrics() returns nil
	// handler should return empty array, not null
	RegisterRoutes(mux, c)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/agents", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result []AgentMetric
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if result == nil {
		t.Error("agents response should be empty array, not nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 agents, got %d", len(result))
	}
}

func TestHandleAgentsWithData(t *testing.T) {
	mux := http.NewServeMux()
	c := NewCollector()
	c.SetAgentMetricsFunc(func() []AgentMetric {
		return []AgentMetric{
			{AgentID: "a1", Type: "llm", Status: "active"},
			{AgentID: "a2", Type: "strategy", Status: "idle"},
		}
	})
	RegisterRoutes(mux, c)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/agents", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result []AgentMetric
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(result))
	}
	if result[0].AgentID != "a1" {
		t.Errorf("first agent: got %q, want %q", result[0].AgentID, "a1")
	}
}

func TestHandleLLMResponse(t *testing.T) {
	mux := http.NewServeMux()
	c := NewCollector()
	c.SetLLMMetricsFunc(func() *LLMMetrics {
		return &LLMMetrics{TotalCalls: 42, ErrorRate: 0.1}
	})
	RegisterRoutes(mux, c)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if result["total_calls"].(float64) != 42 {
		t.Errorf("total_calls: got %v, want 42", result["total_calls"])
	}
}

func TestHandleKnowledgeResponse(t *testing.T) {
	mux := http.NewServeMux()
	c := NewCollector()
	c.SetKBStatsFunc(func() *KBStats {
		return &KBStats{Systems: 10, POIs: 25}
	})
	RegisterRoutes(mux, c)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/knowledge", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result KBStats
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if result.Systems != 10 {
		t.Errorf("systems: got %d, want 10", result.Systems)
	}
	if result.POIs != 25 {
		t.Errorf("POIs: got %d, want 25", result.POIs)
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	c := NewCollector()
	RegisterRoutes(mux, c)

	// POST to a GET-only endpoint
	req := httptest.NewRequest(http.MethodPost, "/api/admin/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Go 1.22+ method routing returns 405 for wrong method
	if w.Code == http.StatusOK {
		t.Error("POST to GET endpoint should not return 200")
	}
}
