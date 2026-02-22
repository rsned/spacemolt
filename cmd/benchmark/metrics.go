package main

import (
	"slices"
	"sync"
	"time"
)

// Metrics collects benchmark results in a thread-safe manner.
type Metrics struct {
	mu sync.Mutex

	results []CommandResult

	// Aggregate counters
	totalCommands  int64
	totalErrors    int64
	rateLimits     int64
	gameErrors     int64
	transportErrs  int64
	reconnects     int64
	disconnects    int64

	startTime time.Time
}

// NewMetrics creates a new Metrics collector.
func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

// Record adds a command result to the metrics.
func (m *Metrics) Record(r CommandResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.results = append(m.results, r)
	m.totalCommands++

	if r.Err != nil {
		m.totalErrors++
		switch r.ErrorKind {
		case RateLimitError:
			m.rateLimits++
		case GameLogicError:
			m.gameErrors++
		case TransportError:
			m.transportErrs++
		}
	}
}

// RecordReconnect increments the reconnect counter.
func (m *Metrics) RecordReconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reconnects++
}

// RecordDisconnect increments the disconnect counter.
func (m *Metrics) RecordDisconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnects++
}

// Snapshot returns a point-in-time summary of the metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	elapsed := time.Since(m.startTime)

	snap := MetricsSnapshot{
		TotalCommands:   m.totalCommands,
		TotalErrors:     m.totalErrors,
		RateLimits:      m.rateLimits,
		GameErrors:      m.gameErrors,
		TransportErrs:   m.transportErrs,
		Reconnects:      m.reconnects,
		Disconnects:     m.disconnects,
		Elapsed:         elapsed,
		PerCommand:      make(map[string]LatencyStats),
		ErrorsByCode:    make(map[string]int64),
		ErrorsByCommand: make(map[string]CommandErrorStats),
	}

	if elapsed.Seconds() > 0 {
		snap.CommandsPerSec = float64(m.totalCommands) / elapsed.Seconds()
	}

	// Compute per-command latency stats and error breakdowns
	byCommand := make(map[string][]time.Duration)
	for _, r := range m.results {
		if r.Err == nil {
			byCommand[r.Command] = append(byCommand[r.Command], r.Latency)
		} else {
			// Error code breakdown
			code := r.ErrorCode
			if code == "" {
				code = "(transport)"
			}
			snap.ErrorsByCode[code]++

			// Per-command error breakdown
			ces := snap.ErrorsByCommand[r.Command]
			ces.Total++
			if ces.ByCodes == nil {
				ces.ByCodes = make(map[string]int64)
			}
			ces.ByCodes[code]++
			switch r.ErrorKind {
			case TransportError:
				ces.Transport++
			case RateLimitError:
				ces.RateLimit++
			case GameLogicError:
				ces.GameLogic++
			}
			snap.ErrorsByCommand[r.Command] = ces
		}
	}

	for cmd, latencies := range byCommand {
		snap.PerCommand[cmd] = computeLatencyStats(latencies)
	}

	// Overall latency stats (successful commands only)
	var allLatencies []time.Duration
	for _, r := range m.results {
		if r.Err == nil {
			allLatencies = append(allLatencies, r.Latency)
		}
	}
	snap.Overall = computeLatencyStats(allLatencies)

	return snap
}

// MetricsSnapshot is a point-in-time summary of collected metrics.
type MetricsSnapshot struct {
	TotalCommands  int64
	TotalErrors    int64
	RateLimits     int64
	GameErrors     int64
	TransportErrs  int64
	Reconnects     int64
	Disconnects    int64
	Elapsed        time.Duration
	CommandsPerSec float64
	Overall        LatencyStats
	PerCommand     map[string]LatencyStats
	// Error breakdowns
	ErrorsByCode    map[string]int64            // error_code → count
	ErrorsByCommand map[string]CommandErrorStats // command → error breakdown
}

// CommandErrorStats holds error counts for a single command.
type CommandErrorStats struct {
	Total     int64
	Transport int64
	RateLimit int64
	GameLogic int64
	ByCodes   map[string]int64 // error_code → count
}

// LatencyStats holds percentile and average latency data.
type LatencyStats struct {
	Count int
	Avg   time.Duration
	P50   time.Duration
	P90   time.Duration
	P99   time.Duration
	Min   time.Duration
	Max   time.Duration
}

func computeLatencyStats(latencies []time.Duration) LatencyStats {
	n := len(latencies)
	if n == 0 {
		return LatencyStats{}
	}

	slices.Sort(latencies)

	var total time.Duration
	for _, l := range latencies {
		total += l
	}

	return LatencyStats{
		Count: n,
		Avg:   total / time.Duration(n),
		P50:   latencies[n*50/100],
		P90:   latencies[n*90/100],
		P99:   latencies[n*99/100],
		Min:   latencies[0],
		Max:   latencies[n-1],
	}
}
