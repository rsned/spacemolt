package monitor

import (
	"runtime"
	"sync"
	"time"
)

// ServerMetrics holds overall server health metrics.
type ServerMetrics struct {
	Uptime        time.Duration `json:"uptime_seconds"`
	GoRoutines    int           `json:"goroutines"`
	HeapAllocMB   float64       `json:"heap_alloc_mb"`
	HeapSysMB     float64       `json:"heap_sys_mb"`
	NumGC         uint32        `json:"num_gc"`
	TotalAgents   int           `json:"total_agents"`
	LLMAgents     int           `json:"llm_agents"`
	StrategyAgents int          `json:"strategy_agents"`
}

// Collector gathers server and agent metrics.
type Collector struct {
	startTime time.Time

	// Agent metric providers (set by the unified server)
	agentCountFn    func() (llm, strategy int)
	agentMetricsFn  func() []AgentMetric
	llmMetricsFn    func() *LLMMetrics
	kbStatsFn       func() *KBStats

	mu sync.RWMutex
}

// NewCollector creates a new metrics collector.
func NewCollector() *Collector {
	return &Collector{
		startTime: time.Now(),
	}
}

// SetAgentCountFunc sets the function used to retrieve agent counts.
func (c *Collector) SetAgentCountFunc(fn func() (llm, strategy int)) {
	c.mu.Lock()
	c.agentCountFn = fn
	c.mu.Unlock()
}

// SetAgentMetricsFunc sets the function used to retrieve per-agent metrics.
func (c *Collector) SetAgentMetricsFunc(fn func() []AgentMetric) {
	c.mu.Lock()
	c.agentMetricsFn = fn
	c.mu.Unlock()
}

// SetLLMMetricsFunc sets the function used to retrieve LLM metrics.
func (c *Collector) SetLLMMetricsFunc(fn func() *LLMMetrics) {
	c.mu.Lock()
	c.llmMetricsFn = fn
	c.mu.Unlock()
}

// SetKBStatsFunc sets the function used to retrieve knowledge base stats.
func (c *Collector) SetKBStatsFunc(fn func() *KBStats) {
	c.mu.Lock()
	c.kbStatsFn = fn
	c.mu.Unlock()
}

// ServerHealth returns current server health metrics.
func (c *Collector) ServerHealth() ServerMetrics {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	var llmCount, stratCount int
	c.mu.RLock()
	if c.agentCountFn != nil {
		llmCount, stratCount = c.agentCountFn()
	}
	c.mu.RUnlock()

	return ServerMetrics{
		Uptime:         time.Since(c.startTime),
		GoRoutines:     runtime.NumGoroutine(),
		HeapAllocMB:    float64(memStats.HeapAlloc) / 1024 / 1024,
		HeapSysMB:      float64(memStats.HeapSys) / 1024 / 1024,
		NumGC:          memStats.NumGC,
		TotalAgents:    llmCount + stratCount,
		LLMAgents:      llmCount,
		StrategyAgents: stratCount,
	}
}

// AgentMetrics returns per-agent metrics from the configured provider.
func (c *Collector) AgentMetrics() []AgentMetric {
	c.mu.RLock()
	fn := c.agentMetricsFn
	c.mu.RUnlock()

	if fn == nil {
		return nil
	}
	return fn()
}

// LLMHealth returns LLM provider metrics from the configured provider.
func (c *Collector) LLMHealth() *LLMMetrics {
	c.mu.RLock()
	fn := c.llmMetricsFn
	c.mu.RUnlock()

	if fn == nil {
		return &LLMMetrics{}
	}
	return fn()
}

// KnowledgeStats returns knowledge base statistics from the configured provider.
func (c *Collector) KnowledgeStats() *KBStats {
	c.mu.RLock()
	fn := c.kbStatsFn
	c.mu.RUnlock()

	if fn == nil {
		return &KBStats{}
	}
	return fn()
}
