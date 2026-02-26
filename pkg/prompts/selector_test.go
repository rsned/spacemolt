package prompts

import (
	"testing"
)

// mockRegistry creates a Registry with in-memory versions for testing.
func mockRegistry(versions map[string][]TemplateVersion) *Registry {
	return &Registry{
		templatesDir: "/tmp/test",
		versions:     versions,
	}
}

func TestSelector_SelectVersion_RequestedVersion(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{
		"decision": {
			{Name: "decision", Version: 1, Status: StatusActive},
			{Name: "decision", Version: 2, Status: StatusActive},
		},
	})
	cfg := &ConfigYAML{
		Prompts: map[string]PromptConfig{
			"decision": {ActiveVersion: 1},
		},
	}
	s := NewSelector(r, cfg)

	// Explicit version request should override config.
	ver, err := s.SelectVersion(SelectionContext{
		PromptType:       "decision",
		RequestedVersion: 2,
	})
	if err != nil {
		t.Fatalf("SelectVersion() error = %v", err)
	}
	if ver != 2 {
		t.Errorf("SelectVersion() = %d, want 2", ver)
	}
}

func TestSelector_SelectVersion_RequestedVersionNotFound(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{
		"decision": {
			{Name: "decision", Version: 1, Status: StatusActive},
		},
	})
	cfg := &ConfigYAML{
		Prompts: map[string]PromptConfig{
			"decision": {ActiveVersion: 1},
		},
	}
	s := NewSelector(r, cfg)

	_, err := s.SelectVersion(SelectionContext{
		PromptType:       "decision",
		RequestedVersion: 99,
	})
	if err == nil {
		t.Error("SelectVersion() expected error for nonexistent requested version")
	}
}

func TestSelector_SelectVersion_ConfiguredActive(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{
		"decision": {
			{Name: "decision", Version: 1, Status: StatusActive},
			{Name: "decision", Version: 2, Status: StatusActive},
		},
	})
	cfg := &ConfigYAML{
		Prompts: map[string]PromptConfig{
			"decision": {ActiveVersion: 2},
		},
	}
	s := NewSelector(r, cfg)

	ver, err := s.SelectVersion(SelectionContext{
		PromptType: "decision",
	})
	if err != nil {
		t.Fatalf("SelectVersion() error = %v", err)
	}
	if ver != 2 {
		t.Errorf("SelectVersion() = %d, want 2", ver)
	}
}

func TestSelector_SelectVersion_RoleOverride(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{
		"decision": {
			{Name: "decision", Version: 1, Status: StatusActive},
			{Name: "decision", Version: 3, Status: StatusActive},
		},
	})
	cfg := &ConfigYAML{
		Prompts: map[string]PromptConfig{
			"decision": {
				ActiveVersion: 1,
				RoleOverrides: map[string]int{
					"miner": 3,
				},
			},
		},
	}
	s := NewSelector(r, cfg)

	ver, err := s.SelectVersion(SelectionContext{
		PromptType: "decision",
		Role:       "miner",
	})
	if err != nil {
		t.Fatalf("SelectVersion() error = %v", err)
	}
	if ver != 3 {
		t.Errorf("SelectVersion() = %d, want 3 for miner role override", ver)
	}
}

func TestSelector_SelectVersion_FallbackToPrevious(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{
		"decision": {
			{Name: "decision", Version: 1, Status: StatusActive},
			// v5 does not exist in registry.
		},
	})
	cfg := &ConfigYAML{
		Global: GlobalConfig{
			FallbackToPrevious: true,
		},
		Prompts: map[string]PromptConfig{
			"decision": {
				ActiveVersion:   5, // Not in registry.
				FallbackVersion: 1,
			},
		},
	}
	s := NewSelector(r, cfg)

	ver, err := s.SelectVersion(SelectionContext{
		PromptType: "decision",
	})
	if err != nil {
		t.Fatalf("SelectVersion() error = %v", err)
	}
	if ver != 1 {
		t.Errorf("SelectVersion() = %d, want 1 (fallback)", ver)
	}
}

func TestSelector_SelectVersion_FallbackDisabled(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{
		"decision": {
			{Name: "decision", Version: 1, Status: StatusActive},
		},
	})
	cfg := &ConfigYAML{
		Global: GlobalConfig{
			FallbackToPrevious: false,
		},
		Prompts: map[string]PromptConfig{
			"decision": {
				ActiveVersion:   5, // Not in registry.
				FallbackVersion: 1,
			},
		},
	}
	s := NewSelector(r, cfg)

	// With fallback disabled, should fall through to latest.
	ver, err := s.SelectVersion(SelectionContext{
		PromptType: "decision",
	})
	if err != nil {
		t.Fatalf("SelectVersion() error = %v", err)
	}
	if ver != 1 {
		t.Errorf("SelectVersion() = %d, want 1 (latest)", ver)
	}
}

func TestSelector_SelectVersion_FallsBackToLatest(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{
		"decision": {
			{Name: "decision", Version: 1, Status: StatusActive},
			{Name: "decision", Version: 2, Status: StatusActive},
		},
	})
	cfg := &ConfigYAML{
		Prompts: map[string]PromptConfig{
			// No decision config, so no active version is set.
		},
	}
	s := NewSelector(r, cfg)

	ver, err := s.SelectVersion(SelectionContext{
		PromptType: "decision",
	})
	if err != nil {
		t.Fatalf("SelectVersion() error = %v", err)
	}
	if ver != 2 {
		t.Errorf("SelectVersion() = %d, want 2 (latest)", ver)
	}
}

func TestSelector_SelectVersion_NoVersionsFound(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{})
	cfg := &ConfigYAML{}
	s := NewSelector(r, cfg)

	_, err := s.SelectVersion(SelectionContext{
		PromptType: "decision",
	})
	if err == nil {
		t.Error("SelectVersion() expected error for no versions found")
	}
}

func TestSelector_SelectVersion_FallbackVersionAlsoMissing(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{
		"decision": {
			{Name: "decision", Version: 1, Status: StatusActive},
		},
	})
	cfg := &ConfigYAML{
		Global: GlobalConfig{
			FallbackToPrevious: true,
		},
		Prompts: map[string]PromptConfig{
			"decision": {
				ActiveVersion:   5,  // Not in registry.
				FallbackVersion: 10, // Also not in registry.
			},
		},
	}
	s := NewSelector(r, cfg)

	// Both configured and fallback versions are missing, should fall to latest.
	ver, err := s.SelectVersion(SelectionContext{
		PromptType: "decision",
	})
	if err != nil {
		t.Fatalf("SelectVersion() error = %v", err)
	}
	if ver != 1 {
		t.Errorf("SelectVersion() = %d, want 1 (latest fallback)", ver)
	}
}

func TestSelector_SelectVersion_DraftNotSelectable(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{
		"decision": {
			{Name: "decision", Version: 1, IsDraft: true, Status: StatusDraft},
		},
	})
	cfg := &ConfigYAML{}
	s := NewSelector(r, cfg)

	// HasVersion and GetLatestVersion exclude drafts, so this should fail.
	_, err := s.SelectVersion(SelectionContext{
		PromptType: "decision",
	})
	if err == nil {
		t.Error("SelectVersion() expected error when only drafts exist")
	}
}

func TestNewSelector(t *testing.T) {
	r := mockRegistry(map[string][]TemplateVersion{})
	cfg := &ConfigYAML{}
	s := NewSelector(r, cfg)
	if s == nil {
		t.Fatal("NewSelector() returned nil")
	}
	if s.registry != r {
		t.Error("NewSelector() registry not set correctly")
	}
	if s.config != cfg {
		t.Error("NewSelector() config not set correctly")
	}
}
