package monitor

import (
	"testing"
	"time"
)

func TestAgentMetricFields(t *testing.T) {
	now := time.Now()
	m := AgentMetric{
		AgentID:      "agent-1",
		Type:         "llm",
		Strategy:     "mining",
		Status:       "active",
		ActionsTotal: 42,
		ErrorsTotal:  3,
		LastAction:   now,
		LLMLatency:   150 * time.Millisecond,
		Team:         "alpha",
		System:       "Sol",
		Docked:       true,
	}

	if m.AgentID != "agent-1" {
		t.Errorf("AgentID: got %q, want %q", m.AgentID, "agent-1")
	}
	if m.Type != "llm" {
		t.Errorf("Type: got %q, want %q", m.Type, "llm")
	}
	if m.Strategy != "mining" {
		t.Errorf("Strategy: got %q, want %q", m.Strategy, "mining")
	}
	if m.Status != "active" {
		t.Errorf("Status: got %q, want %q", m.Status, "active")
	}
	if m.ActionsTotal != 42 {
		t.Errorf("ActionsTotal: got %d, want 42", m.ActionsTotal)
	}
	if m.ErrorsTotal != 3 {
		t.Errorf("ErrorsTotal: got %d, want 3", m.ErrorsTotal)
	}
	if !m.LastAction.Equal(now) {
		t.Errorf("LastAction mismatch")
	}
	if m.LLMLatency != 150*time.Millisecond {
		t.Errorf("LLMLatency: got %v, want 150ms", m.LLMLatency)
	}
	if m.Team != "alpha" {
		t.Errorf("Team: got %q, want %q", m.Team, "alpha")
	}
	if m.System != "Sol" {
		t.Errorf("System: got %q, want %q", m.System, "Sol")
	}
	if !m.Docked {
		t.Error("Docked should be true")
	}
}

func TestAgentMetricZeroValue(t *testing.T) {
	var m AgentMetric
	if m.AgentID != "" {
		t.Errorf("zero AgentID should be empty, got %q", m.AgentID)
	}
	if m.ActionsTotal != 0 {
		t.Errorf("zero ActionsTotal should be 0, got %d", m.ActionsTotal)
	}
	if m.Docked {
		t.Error("zero Docked should be false")
	}
	if !m.LastAction.IsZero() {
		t.Error("zero LastAction should be zero time")
	}
}

func TestLLMMetricsFields(t *testing.T) {
	m := LLMMetrics{
		AvgLatency:  200 * time.Millisecond,
		P99Latency:  1 * time.Second,
		ErrorRate:   0.01,
		RequestsMin: 60.0,
		TotalCalls:  10000,
		TotalErrors: 100,
	}

	if m.AvgLatency != 200*time.Millisecond {
		t.Errorf("AvgLatency: got %v, want 200ms", m.AvgLatency)
	}
	if m.P99Latency != time.Second {
		t.Errorf("P99Latency: got %v, want 1s", m.P99Latency)
	}
	if m.ErrorRate != 0.01 {
		t.Errorf("ErrorRate: got %f, want 0.01", m.ErrorRate)
	}
	if m.RequestsMin != 60.0 {
		t.Errorf("RequestsMin: got %f, want 60.0", m.RequestsMin)
	}
	if m.TotalCalls != 10000 {
		t.Errorf("TotalCalls: got %d, want 10000", m.TotalCalls)
	}
	if m.TotalErrors != 100 {
		t.Errorf("TotalErrors: got %d, want 100", m.TotalErrors)
	}
}

func TestKBStatsFields(t *testing.T) {
	s := KBStats{
		Systems:     50,
		POIs:        200,
		Bases:       10,
		Experiences: 500,
		Items:       75,
	}

	if s.Systems != 50 {
		t.Errorf("Systems: got %d, want 50", s.Systems)
	}
	if s.POIs != 200 {
		t.Errorf("POIs: got %d, want 200", s.POIs)
	}
	if s.Bases != 10 {
		t.Errorf("Bases: got %d, want 10", s.Bases)
	}
	if s.Experiences != 500 {
		t.Errorf("Experiences: got %d, want 500", s.Experiences)
	}
	if s.Items != 75 {
		t.Errorf("Items: got %d, want 75", s.Items)
	}
}

func TestKBStatsZeroValue(t *testing.T) {
	var s KBStats
	if s.Systems != 0 || s.POIs != 0 || s.Bases != 0 || s.Experiences != 0 || s.Items != 0 {
		t.Error("zero value KBStats should have all zero fields")
	}
}
