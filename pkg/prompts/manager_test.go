package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManager_LoadTemplates(t *testing.T) {
	// Use the actual templates directory
	manager, err := NewManager(Config{
		TemplatesDir: "../../data/prompts/templates",
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Check that templates were loaded
	templates := manager.GetAvailableTemplates()
	if len(templates) == 0 {
		t.Error("No templates loaded")
	}

	// Check for decision.v1
	if !manager.HasTemplate("decision", 1) {
		t.Error("decision.v1 template not found")
	}

	t.Logf("Loaded %d templates: %v", len(templates), templates)
}

func TestManager_RenderPrompt(t *testing.T) {
	// Create test context
	ctx := &TemplateContext{
		AgentID:   "test-agent",
		AgentName: "Test Agent",
		Role:      "miner",
		Personality: &PersonalityContext{
			Name:   "Test",
			Role:   "miner",
			Traits: []string{"curious", "brave"},
			Skills: []string{"mining", "combat"},
		},
		State: &StateContext{
			SystemName:  "Sol",
			SystemID:    "sol-1",
			Location:    "Sol-Station-Alpha",
			IsDocked:    true,
			DockedAt:    "Sol-Station-Alpha",
			Fuel:        100,
			MaxFuel:     100,
			FuelPercent: 100,
			Hull:        100,
			MaxHull:     100,
			HullPercent: 100,
			Credits:     1000,
		},
		Knowledge: &KnowledgeContext{
			POIsInSystem: []POIInfo{
				{ID: "poi-1", Name: "Asteroid Field", Type: "mining"},
			},
			Connections: []string{"Alpha Centauri"},
		},
	}

	manager, err := NewManager(Config{
		TemplatesDir: "../../data/prompts/templates",
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Render template
	prompt, err := manager.RenderPrompt("decision", 1, ctx)
	if err != nil {
		t.Fatalf("Failed to render prompt: %v", err)
	}

	// Check that prompt contains expected content
	if len(prompt) == 0 {
		t.Error("Rendered prompt is empty")
	}

	// Should contain agent name
	if !contains(prompt, "Test Agent") {
		t.Error("Prompt does not contain agent name")
	}

	// Should contain role
	if !contains(prompt, "miner") {
		t.Error("Prompt does not contain role")
	}

	t.Logf("Rendered prompt length: %d bytes", len(prompt))
}

func TestRegistry_Scan(t *testing.T) {
	registry, err := NewRegistry("../../data/prompts/templates")
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	// Check that decision templates were found
	versions := registry.GetVersions("decision")
	if len(versions) == 0 {
		t.Error("No decision template versions found")
	}

	// Get latest version
	latest, ok := registry.GetLatestVersion("decision")
	if !ok {
		t.Error("No latest version found")
	}

	t.Logf("Found %d versions, latest: %d", len(versions), latest)
}

func TestSelector_SelectVersion(t *testing.T) {
	registry, err := NewRegistry("../../data/prompts/templates")
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	// Create test config
	cfg := &ConfigYAML{
		Prompts: map[string]PromptConfig{
			"decision": {
				ActiveVersion:   1,
				FallbackVersion: 1,
			},
		},
	}

	selector := NewSelector(registry, cfg)

	// Test selection
	ctx := SelectionContext{
		PromptType: "decision",
		Role:       "miner",
	}

	version, err := selector.SelectVersion(ctx)
	if err != nil {
		t.Fatalf("Failed to select version: %v", err)
	}

	if version != 1 {
		t.Errorf("Expected version 1, got %d", version)
	}

	t.Logf("Selected version: %d", version)
}

func TestMetricsCollector(t *testing.T) {
	tmpDir := t.TempDir()

	mc := NewMetricsCollector(tmpDir, true)

	// Record some uses
	mc.RecordUse("decision", 1, "miner", true, 0.9, nil)
	mc.RecordUse("decision", 1, "miner", true, 0.85, nil)
	mc.RecordUse("decision", 1, "explorer", false, 0.7, nil)

	// Get metrics
	m := mc.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("Metrics not found")
	}

	if m.TotalUses != 3 {
		t.Errorf("Expected 3 uses, got %d", m.TotalUses)
	}

	if m.SuccessCount != 2 {
		t.Errorf("Expected 2 successes, got %d", m.SuccessCount)
	}

	// Save metrics
	if err := mc.Save(); err != nil {
		t.Fatalf("Failed to save metrics: %v", err)
	}

	// Check file exists
	metricFile := filepath.Join(tmpDir, "decision.v1.yaml")
	if _, err := os.Stat(metricFile); os.IsNotExist(err) {
		t.Error("Metrics file not created")
	}

	// Load metrics
	mc2 := NewMetricsCollector(tmpDir, true)
	if err := mc2.Load(); err != nil {
		t.Fatalf("Failed to load metrics: %v", err)
	}

	m2 := mc2.GetMetrics("decision", 1)
	if m2 == nil {
		t.Fatal("Loaded metrics not found")
	}

	if m2.TotalUses != m.TotalUses {
		t.Errorf("Loaded metrics differ: expected %d uses, got %d", m.TotalUses, m2.TotalUses)
	}

	t.Logf("Metrics: %d uses, %.2f%% success, %.2f avg confidence",
		m.TotalUses, m.SuccessRate()*100, m.AvgConfidence)
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
