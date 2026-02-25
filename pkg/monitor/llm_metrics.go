package monitor

import "time"

// LLMMetrics tracks LLM provider health.
type LLMMetrics struct {
	AvgLatency  time.Duration `json:"avg_latency_ms"`
	P99Latency  time.Duration `json:"p99_latency_ms"`
	ErrorRate   float64       `json:"error_rate"`
	RequestsMin float64      `json:"requests_per_min"`
	TotalCalls  int64         `json:"total_calls"`
	TotalErrors int64         `json:"total_errors"`
}

// KBStats holds knowledge base statistics.
type KBStats struct {
	Systems     int `json:"systems"`
	POIs        int `json:"pois"`
	Bases       int `json:"bases"`
	Experiences int `json:"experiences"`
	Items       int `json:"items"`
}
