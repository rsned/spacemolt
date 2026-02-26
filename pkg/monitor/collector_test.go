package monitor

import (
	"sync"
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	if c.startTime.IsZero() {
		t.Error("startTime should be set")
	}
}

func TestServerHealthDefaults(t *testing.T) {
	c := NewCollector()
	health := c.ServerHealth()

	if health.Uptime <= 0 {
		t.Error("uptime should be positive")
	}
	if health.GoRoutines <= 0 {
		t.Error("goroutines should be positive")
	}
	if health.HeapAllocMB <= 0 {
		t.Error("heap alloc should be positive")
	}
	if health.HeapSysMB <= 0 {
		t.Error("heap sys should be positive")
	}
	if health.TotalAgents != 0 {
		t.Errorf("total agents should be 0, got %d", health.TotalAgents)
	}
	if health.LLMAgents != 0 {
		t.Errorf("LLM agents should be 0, got %d", health.LLMAgents)
	}
	if health.StrategyAgents != 0 {
		t.Errorf("strategy agents should be 0, got %d", health.StrategyAgents)
	}
}

func TestSetAgentCountFunc(t *testing.T) {
	c := NewCollector()
	c.SetAgentCountFunc(func() (int, int) {
		return 3, 5
	})

	health := c.ServerHealth()
	if health.LLMAgents != 3 {
		t.Errorf("LLM agents: got %d, want 3", health.LLMAgents)
	}
	if health.StrategyAgents != 5 {
		t.Errorf("strategy agents: got %d, want 5", health.StrategyAgents)
	}
	if health.TotalAgents != 8 {
		t.Errorf("total agents: got %d, want 8", health.TotalAgents)
	}
}

func TestAgentMetricsNilFunc(t *testing.T) {
	c := NewCollector()
	metrics := c.AgentMetrics()
	if metrics != nil {
		t.Errorf("expected nil, got %v", metrics)
	}
}

func TestSetAgentMetricsFunc(t *testing.T) {
	c := NewCollector()
	expected := []AgentMetric{
		{AgentID: "a1", Type: "llm", Status: "active", ActionsTotal: 10},
		{AgentID: "a2", Type: "strategy", Strategy: "mining", Status: "idle"},
	}
	c.SetAgentMetricsFunc(func() []AgentMetric {
		return expected
	})

	metrics := c.AgentMetrics()
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if metrics[0].AgentID != "a1" {
		t.Errorf("first agent ID: got %q, want %q", metrics[0].AgentID, "a1")
	}
	if metrics[1].Strategy != "mining" {
		t.Errorf("second agent strategy: got %q, want %q", metrics[1].Strategy, "mining")
	}
}

func TestLLMHealthNilFunc(t *testing.T) {
	c := NewCollector()
	llm := c.LLMHealth()
	if llm == nil {
		t.Fatal("LLMHealth should return empty struct, not nil")
	}
	if llm.TotalCalls != 0 {
		t.Errorf("expected 0 total calls, got %d", llm.TotalCalls)
	}
}

func TestSetLLMMetricsFunc(t *testing.T) {
	c := NewCollector()
	expected := &LLMMetrics{
		AvgLatency:  100 * time.Millisecond,
		P99Latency:  500 * time.Millisecond,
		ErrorRate:   0.05,
		RequestsMin: 12.5,
		TotalCalls:  1000,
		TotalErrors: 50,
	}
	c.SetLLMMetricsFunc(func() *LLMMetrics {
		return expected
	})

	llm := c.LLMHealth()
	if llm.TotalCalls != 1000 {
		t.Errorf("total calls: got %d, want 1000", llm.TotalCalls)
	}
	if llm.ErrorRate != 0.05 {
		t.Errorf("error rate: got %f, want 0.05", llm.ErrorRate)
	}
}

func TestKnowledgeStatsNilFunc(t *testing.T) {
	c := NewCollector()
	kb := c.KnowledgeStats()
	if kb == nil {
		t.Fatal("KnowledgeStats should return empty struct, not nil")
	}
	if kb.Systems != 0 {
		t.Errorf("expected 0 systems, got %d", kb.Systems)
	}
}

func TestSetKBStatsFunc(t *testing.T) {
	c := NewCollector()
	expected := &KBStats{
		Systems:     42,
		POIs:        100,
		Bases:       5,
		Experiences: 200,
		Items:       50,
	}
	c.SetKBStatsFunc(func() *KBStats {
		return expected
	})

	kb := c.KnowledgeStats()
	if kb.Systems != 42 {
		t.Errorf("systems: got %d, want 42", kb.Systems)
	}
	if kb.POIs != 100 {
		t.Errorf("POIs: got %d, want 100", kb.POIs)
	}
}

func TestCollectorConcurrentAccess(t *testing.T) {
	c := NewCollector()
	c.SetAgentCountFunc(func() (int, int) { return 1, 2 })
	c.SetAgentMetricsFunc(func() []AgentMetric {
		return []AgentMetric{{AgentID: "test"}}
	})
	c.SetLLMMetricsFunc(func() *LLMMetrics {
		return &LLMMetrics{TotalCalls: 5}
	})
	c.SetKBStatsFunc(func() *KBStats {
		return &KBStats{Systems: 10}
	})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(4)
		go func() {
			defer wg.Done()
			_ = c.ServerHealth()
		}()
		go func() {
			defer wg.Done()
			_ = c.AgentMetrics()
		}()
		go func() {
			defer wg.Done()
			_ = c.LLMHealth()
		}()
		go func() {
			defer wg.Done()
			_ = c.KnowledgeStats()
		}()
	}
	wg.Wait()
}

func TestCollectorSetFuncsConcurrent(t *testing.T) {
	c := NewCollector()

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.SetAgentCountFunc(func() (int, int) { return 1, 1 })
		}()
		go func() {
			defer wg.Done()
			_ = c.ServerHealth()
		}()
	}
	wg.Wait()
}
