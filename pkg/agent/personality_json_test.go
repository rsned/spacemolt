package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPersonalityJSON(t *testing.T) {
	// Create a temp file with valid personality JSON
	p := Personality{
		Name: "TestBot",
		ID:   "test-1",
		Role: "miner",
		Traits: map[string]float64{
			"boldness":  0.7,
			"caution":   0.3,
			"curiosity": 0.8,
		},
		Skills: map[string]string{
			"mining": "advanced",
		},
		Motivations: Motivations{
			Primary:   "wealth",
			Secondary: "exploration",
			Tertiary:  "combat",
			Weights: map[string]float64{
				"wealth": 0.6,
			},
		},
		Biography: "A seasoned miner.",
		Faction:   "solarian",
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test_personality.json")

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write error: %v", err)
	}

	loaded, err := LoadPersonalityJSON(path)
	if err != nil {
		t.Fatalf("LoadPersonalityJSON error: %v", err)
	}

	if loaded.Name != "TestBot" {
		t.Errorf("Name = %q, want %q", loaded.Name, "TestBot")
	}
	if loaded.ID != "test-1" {
		t.Errorf("ID = %q, want %q", loaded.ID, "test-1")
	}
	if loaded.Role != "miner" {
		t.Errorf("Role = %q, want %q", loaded.Role, "miner")
	}
	if loaded.Biography != "A seasoned miner." {
		t.Errorf("Biography = %q, want %q", loaded.Biography, "A seasoned miner.")
	}
	if loaded.Faction != "solarian" {
		t.Errorf("Faction = %q, want %q", loaded.Faction, "solarian")
	}
	if loaded.Traits["boldness"] != 0.7 {
		t.Errorf("Traits[boldness] = %v, want 0.7", loaded.Traits["boldness"])
	}
	if loaded.Motivations.Primary != "wealth" {
		t.Errorf("Motivations.Primary = %q, want %q", loaded.Motivations.Primary, "wealth")
	}
	if loaded.Skills["mining"] != "advanced" {
		t.Errorf("Skills[mining] = %q, want %q", loaded.Skills["mining"], "advanced")
	}
}

func TestLoadPersonalityJSON_FileNotFound(t *testing.T) {
	_, err := LoadPersonalityJSON("/nonexistent/path/personality.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadPersonalityJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("write error: %v", err)
	}

	_, err := LoadPersonalityJSON(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadPersonalityJSON_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")

	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatalf("write error: %v", err)
	}

	p, err := LoadPersonalityJSON(path)
	if err != nil {
		t.Fatalf("LoadPersonalityJSON error: %v", err)
	}

	if p.Name != "" {
		t.Errorf("Name = %q, want empty", p.Name)
	}
	if p.Role != "" {
		t.Errorf("Role = %q, want empty", p.Role)
	}
}

func TestSavePersonalityJSON(t *testing.T) {
	p := Personality{
		Name: "SaveTest",
		ID:   "save-1",
		Role: "trader",
		Traits: map[string]float64{
			"patience": 0.9,
		},
		Motivations: Motivations{
			Primary: "wealth",
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "saved.json")

	if err := SavePersonalityJSON(p, path); err != nil {
		t.Fatalf("SavePersonalityJSON error: %v", err)
	}

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var loaded Personality
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if loaded.Name != "SaveTest" {
		t.Errorf("Name = %q, want %q", loaded.Name, "SaveTest")
	}
	if loaded.Traits["patience"] != 0.9 {
		t.Errorf("Traits[patience] = %v, want 0.9", loaded.Traits["patience"])
	}
}

func TestSavePersonalityJSON_InvalidPath(t *testing.T) {
	p := Personality{Name: "Test"}

	err := SavePersonalityJSON(p, "/nonexistent/dir/file.json")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	original := Personality{
		Name: "RoundTrip",
		ID:   "rt-1",
		Role: "explorer",
		Traits: map[string]float64{
			"curiosity": 0.95,
			"boldness":  0.4,
		},
		Skills: map[string]string{
			"navigation": "expert",
			"scanning":   "intermediate",
		},
		Motivations: Motivations{
			Primary:   "exploration",
			Secondary: "knowledge",
			Tertiary:  "wealth",
			Weights: map[string]float64{
				"exploration": 0.7,
				"knowledge":   0.2,
				"wealth":      0.1,
			},
		},
		Biography: "Loves discovering new systems.",
		Faction:   "voidborn",
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "roundtrip.json")

	if err := SavePersonalityJSON(original, path); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := LoadPersonalityJSON(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if loaded.Name != original.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, original.Name)
	}
	if loaded.ID != original.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, original.ID)
	}
	if loaded.Role != original.Role {
		t.Errorf("Role = %q, want %q", loaded.Role, original.Role)
	}
	if loaded.Faction != original.Faction {
		t.Errorf("Faction = %q, want %q", loaded.Faction, original.Faction)
	}
	if len(loaded.Traits) != len(original.Traits) {
		t.Errorf("Traits len = %d, want %d", len(loaded.Traits), len(original.Traits))
	}
	for k, v := range original.Traits {
		if loaded.Traits[k] != v {
			t.Errorf("Traits[%s] = %v, want %v", k, loaded.Traits[k], v)
		}
	}
	if loaded.Motivations.Primary != original.Motivations.Primary {
		t.Errorf("Motivations.Primary = %q, want %q", loaded.Motivations.Primary, original.Motivations.Primary)
	}
	if len(loaded.Motivations.Weights) != len(original.Motivations.Weights) {
		t.Errorf("Motivations.Weights len = %d, want %d", len(loaded.Motivations.Weights), len(original.Motivations.Weights))
	}
}

func TestLoadPersonalityJSON_NilTraits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no_traits.json")

	jsonData := `{"name":"NoTraits","id":"nt-1","role":"miner"}`
	if err := os.WriteFile(path, []byte(jsonData), 0644); err != nil {
		t.Fatalf("write error: %v", err)
	}

	p, err := LoadPersonalityJSON(path)
	if err != nil {
		t.Fatalf("LoadPersonalityJSON error: %v", err)
	}

	if p.Traits != nil {
		t.Errorf("expected nil Traits, got %v", p.Traits)
	}
}
