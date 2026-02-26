package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
global:
  selection_strategy: "latest"
  fallback_to_previous: true
  collect_metrics: true
prompts:
  decision:
    active_version: 2
    fallback_version: 1
    role_overrides:
      miner: 3
      explorer: 1
  feedback:
    active_version: 1
metrics:
  enabled: true
  retention_days: 30
  storage_path: "/tmp/metrics"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Global.SelectionStrategy != "latest" {
		t.Errorf("SelectionStrategy = %q, want %q", cfg.Global.SelectionStrategy, "latest")
	}
	if !cfg.Global.FallbackToPrevious {
		t.Error("FallbackToPrevious = false, want true")
	}
	if !cfg.Global.CollectMetrics {
		t.Error("CollectMetrics = false, want true")
	}
	if cfg.Prompts["decision"].ActiveVersion != 2 {
		t.Errorf("decision.ActiveVersion = %d, want 2", cfg.Prompts["decision"].ActiveVersion)
	}
	if cfg.Prompts["decision"].RoleOverrides["miner"] != 3 {
		t.Errorf("decision.RoleOverrides[miner] = %d, want 3", cfg.Prompts["decision"].RoleOverrides["miner"])
	}
	if cfg.Metrics.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d, want 30", cfg.Metrics.RetentionDays)
	}
	if cfg.Metrics.StoragePath != "/tmp/metrics" {
		t.Errorf("StoragePath = %q, want %q", cfg.Metrics.StoragePath, "/tmp/metrics")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Minimal config with no defaults specified.
	content := `
global: {}
prompts: {}
metrics: {}
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Global.SelectionStrategy != "stable" {
		t.Errorf("default SelectionStrategy = %q, want %q", cfg.Global.SelectionStrategy, "stable")
	}
	if cfg.Metrics.RetentionDays != 90 {
		t.Errorf("default RetentionDays = %d, want 90", cfg.Metrics.RetentionDays)
	}
	if cfg.Metrics.StoragePath != "data/prompts/metadata" {
		t.Errorf("default StoragePath = %q, want %q", cfg.Metrics.StoragePath, "data/prompts/metadata")
	}
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(cfgPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// Defaults should be applied.
	if cfg.Global.SelectionStrategy != "stable" {
		t.Errorf("default SelectionStrategy = %q, want %q", cfg.Global.SelectionStrategy, "stable")
	}
}

func TestLoadConfig_NonexistentFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("LoadConfig() expected error for nonexistent file, got nil")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(cfgPath, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Error("LoadConfig() expected error for invalid YAML, got nil")
	}
}

func TestConfigYAML_GetActiveVersion(t *testing.T) {
	cfg := &ConfigYAML{
		Prompts: map[string]PromptConfig{
			"decision": {
				ActiveVersion: 2,
				RoleOverrides: map[string]int{
					"miner":    3,
					"explorer": 1,
				},
			},
			"feedback": {
				ActiveVersion: 1,
			},
			"empty": {
				ActiveVersion: 0,
			},
		},
	}

	tests := []struct {
		name       string
		promptType string
		role       string
		wantVer    int
		wantOK     bool
	}{
		{
			name:       "active version without role",
			promptType: "decision",
			role:       "",
			wantVer:    2,
			wantOK:     true,
		},
		{
			name:       "role override exists",
			promptType: "decision",
			role:       "miner",
			wantVer:    3,
			wantOK:     true,
		},
		{
			name:       "role override for explorer",
			promptType: "decision",
			role:       "explorer",
			wantVer:    1,
			wantOK:     true,
		},
		{
			name:       "role not in overrides falls back to active",
			promptType: "decision",
			role:       "trader",
			wantVer:    2,
			wantOK:     true,
		},
		{
			name:       "prompt without role overrides",
			promptType: "feedback",
			role:       "miner",
			wantVer:    1,
			wantOK:     true,
		},
		{
			name:       "unknown prompt type",
			promptType: "nonexistent",
			role:       "",
			wantVer:    0,
			wantOK:     false,
		},
		{
			name:       "zero active version returns false",
			promptType: "empty",
			role:       "",
			wantVer:    0,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, ok := cfg.GetActiveVersion(tt.promptType, tt.role)
			if ver != tt.wantVer || ok != tt.wantOK {
				t.Errorf("GetActiveVersion(%q, %q) = (%d, %v), want (%d, %v)",
					tt.promptType, tt.role, ver, ok, tt.wantVer, tt.wantOK)
			}
		})
	}
}

func TestConfigYAML_GetFallbackVersion(t *testing.T) {
	cfg := &ConfigYAML{
		Prompts: map[string]PromptConfig{
			"decision": {
				FallbackVersion: 1,
			},
			"feedback": {
				FallbackVersion: 0,
			},
		},
	}

	tests := []struct {
		name       string
		promptType string
		wantVer    int
		wantOK     bool
	}{
		{
			name:       "fallback version exists",
			promptType: "decision",
			wantVer:    1,
			wantOK:     true,
		},
		{
			name:       "zero fallback version",
			promptType: "feedback",
			wantVer:    0,
			wantOK:     false,
		},
		{
			name:       "unknown prompt type",
			promptType: "nonexistent",
			wantVer:    0,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, ok := cfg.GetFallbackVersion(tt.promptType)
			if ver != tt.wantVer || ok != tt.wantOK {
				t.Errorf("GetFallbackVersion(%q) = (%d, %v), want (%d, %v)",
					tt.promptType, ver, ok, tt.wantVer, tt.wantOK)
			}
		})
	}
}

func TestConfigYAML_NilPrompts(t *testing.T) {
	cfg := &ConfigYAML{}

	ver, ok := cfg.GetActiveVersion("decision", "miner")
	if ver != 0 || ok {
		t.Errorf("GetActiveVersion on nil Prompts = (%d, %v), want (0, false)", ver, ok)
	}

	ver, ok = cfg.GetFallbackVersion("decision")
	if ver != 0 || ok {
		t.Errorf("GetFallbackVersion on nil Prompts = (%d, %v), want (0, false)", ver, ok)
	}
}
