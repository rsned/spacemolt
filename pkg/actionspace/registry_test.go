package actionspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromOpenAPI(t *testing.T) {
	specPath := filepath.Join("..", "..", "server_docs", "openapi.json")
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		t.Skip("openapi.json not found at", specPath)
	}

	r, err := LoadFromOpenAPI(specPath)
	if err != nil {
		t.Fatalf("LoadFromOpenAPI failed: %v", err)
	}

	// Should have at least as many actions as the hardcoded list.
	if len(r.Actions) < len(AllActions) {
		t.Errorf("registry has %d actions, want at least %d (hardcoded)", len(r.Actions), len(AllActions))
	}

	// Log warnings for visibility.
	for _, w := range r.Warnings {
		t.Logf("WARNING: %s", w)
	}

	// Every action should have a name, category, and at least one precondition.
	for _, a := range r.Actions {
		if a.Name == "" {
			t.Error("action has empty name")
		}
		if a.Category == "" {
			t.Errorf("action %q has empty category", a.Name)
		}
		if len(a.Preconditions) == 0 {
			t.Errorf("action %q has no preconditions", a.Name)
		}
	}

	// No duplicate names.
	seen := make(map[string]bool)
	for _, a := range r.Actions {
		if seen[a.Name] {
			t.Errorf("duplicate action: %q", a.Name)
		}
		seen[a.Name] = true
	}
}

func TestLoadFromOpenAPIEvaluate(t *testing.T) {
	specPath := filepath.Join("..", "..", "server_docs", "openapi.json")
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		t.Skip("openapi.json not found at", specPath)
	}

	r, err := LoadFromOpenAPI(specPath)
	if err != nil {
		t.Fatalf("LoadFromOpenAPI failed: %v", err)
	}

	gc := GameContext{
		Docked: true, Credits: 1000,
		Hull: 80, MaxHull: 100, Fuel: 50, MaxFuel: 100,
		CargoUsed: 10, CargoCapacity: 50, CargoItemCount: 2,
	}

	as := r.Evaluate(gc)

	if as.Stats.TotalActions != len(r.Actions) {
		t.Errorf("TotalActions=%d, want %d", as.Stats.TotalActions, len(r.Actions))
	}
	if as.Stats.ValidActions == 0 {
		t.Error("expected some valid actions when docked")
	}
	if as.Stats.PrunedActions == 0 {
		t.Error("expected some pruned actions when docked")
	}
}

func TestLoadFromOpenAPIContainsAllHardcoded(t *testing.T) {
	specPath := filepath.Join("..", "..", "server_docs", "openapi.json")
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		t.Skip("openapi.json not found at", specPath)
	}

	r, err := LoadFromOpenAPI(specPath)
	if err != nil {
		t.Fatalf("LoadFromOpenAPI failed: %v", err)
	}

	registryNames := make(map[string]bool)
	for _, a := range r.Actions {
		registryNames[a.Name] = true
	}

	for _, a := range AllActions {
		if !registryNames[a.Name] {
			t.Errorf("hardcoded action %q missing from OpenAPI registry", a.Name)
		}
	}
}

func TestLoadFromOpenAPIInvalidPath(t *testing.T) {
	_, err := LoadFromOpenAPI("/nonexistent/file.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadFromOpenAPIInvalidJSON(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(tmp, []byte("not json"), 0644)

	_, err := LoadFromOpenAPI(tmp)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadFromOpenAPIMinimalSpec(t *testing.T) {
	// Minimal spec with one mutation and one query.
	spec := map[string]any{
		"paths": map[string]any{
			"/mine": map[string]any{
				"post": map[string]any{
					"operationId":  "mine",
					"summary":      "Mine resources",
					"tags":         []string{"mining"},
					"x-is-mutation": true,
				},
			},
			"/get_status": map[string]any{
				"post": map[string]any{
					"operationId": "get_status",
					"summary":     "Get status",
					"tags":        []string{"query"},
				},
			},
		},
	}

	tmp := filepath.Join(t.TempDir(), "spec.json")
	data, _ := json.Marshal(spec)
	_ = os.WriteFile(tmp, data, 0644)

	r, err := LoadFromOpenAPI(tmp)
	if err != nil {
		t.Fatalf("LoadFromOpenAPI failed: %v", err)
	}

	// Should include mine (mutation with annotation) but not get_status (query).
	found := false
	for _, a := range r.Actions {
		if a.Name == "mine" {
			found = true
			if a.Summary != "Mine resources" {
				t.Errorf("summary: got %q", a.Summary)
			}
		}
		if a.Name == "get_status" {
			t.Error("query get_status should not be in registry")
		}
	}
	if !found {
		t.Error("mine not found in registry")
	}
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	if len(r.Actions) != len(AllActions) {
		t.Errorf("DefaultRegistry has %d actions, want %d", len(r.Actions), len(AllActions))
	}
}
