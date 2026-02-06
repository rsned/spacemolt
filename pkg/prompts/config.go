package prompts

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ConfigYAML represents the prompts configuration file
type ConfigYAML struct {
	Global  GlobalConfig            `yaml:"global"`
	Prompts map[string]PromptConfig `yaml:"prompts"`
	Metrics MetricsConfig           `yaml:"metrics"`
}

// GlobalConfig holds global configuration
type GlobalConfig struct {
	SelectionStrategy  string `yaml:"selection_strategy"` // "stable", "latest", "role_based"
	FallbackToPrevious bool   `yaml:"fallback_to_previous"`
	CollectMetrics     bool   `yaml:"collect_metrics"`
}

// PromptConfig holds configuration for a specific prompt type
type PromptConfig struct {
	ActiveVersion   int            `yaml:"active_version"`
	RoleOverrides   map[string]int `yaml:"role_overrides"`
	FallbackVersion int            `yaml:"fallback_version"`
}

// MetricsConfig holds metrics collection configuration
type MetricsConfig struct {
	Enabled       bool   `yaml:"enabled"`
	RetentionDays int    `yaml:"retention_days"`
	StoragePath   string `yaml:"storage_path"`
}

// LoadConfig loads the configuration from a YAML file
func LoadConfig(path string) (*ConfigYAML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg ConfigYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	// Set defaults
	if cfg.Global.SelectionStrategy == "" {
		cfg.Global.SelectionStrategy = "stable"
	}
	if cfg.Metrics.RetentionDays == 0 {
		cfg.Metrics.RetentionDays = 90
	}
	if cfg.Metrics.StoragePath == "" {
		cfg.Metrics.StoragePath = "data/prompts/metadata"
	}

	return &cfg, nil
}

// GetActiveVersion returns the active version for a prompt type and role
func (c *ConfigYAML) GetActiveVersion(promptType, role string) (int, bool) {
	promptCfg, ok := c.Prompts[promptType]
	if !ok {
		return 0, false
	}

	// Check for role-specific override
	if role != "" {
		if version, ok := promptCfg.RoleOverrides[role]; ok {
			return version, true
		}
	}

	// Return active version
	if promptCfg.ActiveVersion > 0 {
		return promptCfg.ActiveVersion, true
	}

	return 0, false
}

// GetFallbackVersion returns the fallback version for a prompt type
func (c *ConfigYAML) GetFallbackVersion(promptType string) (int, bool) {
	promptCfg, ok := c.Prompts[promptType]
	if !ok {
		return 0, false
	}

	if promptCfg.FallbackVersion > 0 {
		return promptCfg.FallbackVersion, true
	}

	return 0, false
}
