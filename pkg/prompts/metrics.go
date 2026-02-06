package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Metrics tracks prompt performance metrics
type Metrics struct {
	PromptType    string                 `yaml:"prompt_type"`
	Version       int                    `yaml:"version"`
	TotalUses     int                    `yaml:"total_uses"`
	SuccessCount  int                    `yaml:"success_count"`
	ErrorCount    int                    `yaml:"error_count"`
	AvgConfidence float64                `yaml:"avg_confidence"`
	LastUsed      time.Time              `yaml:"last_used"`
	Errors        []ErrorRecord          `yaml:"errors,omitempty"`
	ByRole        map[string]RoleMetrics `yaml:"by_role,omitempty"`
}

// RoleMetrics tracks metrics per role
type RoleMetrics struct {
	Uses          int     `yaml:"uses"`
	SuccessCount  int     `yaml:"success_count"`
	AvgConfidence float64 `yaml:"avg_confidence"`
}

// ErrorRecord records an error occurrence
type ErrorRecord struct {
	Timestamp time.Time `yaml:"timestamp"`
	Error     string    `yaml:"error"`
	Context   string    `yaml:"context,omitempty"`
}

// MetricsCollector collects and stores prompt metrics
type MetricsCollector struct {
	storagePath string
	metrics     map[string]*Metrics // key: "{promptType}.v{version}"
	mu          sync.RWMutex
	enabled     bool
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(storagePath string, enabled bool) *MetricsCollector {
	return &MetricsCollector{
		storagePath: storagePath,
		metrics:     make(map[string]*Metrics),
		enabled:     enabled,
	}
}

// RecordUse records a prompt usage
func (mc *MetricsCollector) RecordUse(promptType string, version int, role string, success bool, confidence float64, err error) {
	if !mc.enabled {
		return
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	key := fmt.Sprintf("%s.v%d", promptType, version)
	m, ok := mc.metrics[key]
	if !ok {
		m = &Metrics{
			PromptType: promptType,
			Version:    version,
			ByRole:     make(map[string]RoleMetrics),
		}
		mc.metrics[key] = m
	}

	m.TotalUses++
	m.LastUsed = time.Now()

	if success {
		m.SuccessCount++
	} else {
		m.ErrorCount++
		if err != nil {
			m.Errors = append(m.Errors, ErrorRecord{
				Timestamp: time.Now(),
				Error:     err.Error(),
			})
		}
	}

	// Update average confidence
	if confidence > 0 {
		m.AvgConfidence = (m.AvgConfidence*float64(m.TotalUses-1) + confidence) / float64(m.TotalUses)
	}

	// Update role metrics
	if role != "" {
		rm := m.ByRole[role]
		rm.Uses++
		if success {
			rm.SuccessCount++
		}
		if confidence > 0 {
			rm.AvgConfidence = (rm.AvgConfidence*float64(rm.Uses-1) + confidence) / float64(rm.Uses)
		}
		m.ByRole[role] = rm
	}
}

// Save saves metrics to disk
func (mc *MetricsCollector) Save() error {
	if !mc.enabled {
		return nil
	}

	mc.mu.RLock()
	defer mc.mu.RUnlock()

	// Ensure storage directory exists
	if err := os.MkdirAll(mc.storagePath, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Save each metric to a separate file
	for key, m := range mc.metrics {
		filename := filepath.Join(mc.storagePath, fmt.Sprintf("%s.yaml", key))
		data, err := yaml.Marshal(m)
		if err != nil {
			return fmt.Errorf("failed to marshal metrics for %s: %w", key, err)
		}

		if err := os.WriteFile(filename, data, 0644); err != nil {
			return fmt.Errorf("failed to write metrics file %s: %w", filename, err)
		}
	}

	return nil
}

// Load loads metrics from disk
func (mc *MetricsCollector) Load() error {
	if !mc.enabled {
		return nil
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Check if storage directory exists
	if _, err := os.Stat(mc.storagePath); os.IsNotExist(err) {
		return nil // No metrics to load yet
	}

	// Read all metric files
	files, err := os.ReadDir(mc.storagePath)
	if err != nil {
		return fmt.Errorf("failed to read storage directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".yaml" {
			continue
		}

		path := filepath.Join(mc.storagePath, file.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("Warning: Failed to read metrics file %s: %v\n", path, err)
			continue
		}

		var m Metrics
		if err := yaml.Unmarshal(data, &m); err != nil {
			fmt.Printf("Warning: Failed to parse metrics file %s: %v\n", path, err)
			continue
		}

		key := fmt.Sprintf("%s.v%d", m.PromptType, m.Version)
		mc.metrics[key] = &m
	}

	return nil
}

// GetMetrics returns metrics for a specific prompt version
func (mc *MetricsCollector) GetMetrics(promptType string, version int) *Metrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	key := fmt.Sprintf("%s.v%d", promptType, version)
	return mc.metrics[key]
}

// GetAllMetrics returns all collected metrics
func (mc *MetricsCollector) GetAllMetrics() map[string]*Metrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[string]*Metrics, len(mc.metrics))
	for k, v := range mc.metrics {
		result[k] = v
	}
	return result
}

// SuccessRate calculates success rate for a prompt
func (m *Metrics) SuccessRate() float64 {
	if m.TotalUses == 0 {
		return 0
	}
	return float64(m.SuccessCount) / float64(m.TotalUses)
}

// ErrorRate calculates error rate for a prompt
func (m *Metrics) ErrorRate() float64 {
	if m.TotalUses == 0 {
		return 0
	}
	return float64(m.ErrorCount) / float64(m.TotalUses)
}
