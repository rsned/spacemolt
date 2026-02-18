package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseAgentList(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", []string{}},
		{"agent-1", []string{"agent-1"}},
		{"agent-1,agent-2", []string{"agent-1", "agent-2"}},
		{"agent-1, agent-2, agent-3", []string{"agent-1", "agent-2", "agent-3"}},
		{" agent-1 , agent-2 ", []string{"agent-1", "agent-2"}},
		{"agent-1,,agent-2", []string{"agent-1", "agent-2"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseAgentList(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseAgentList(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDiscoverAgents(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create valid agent directories
	validAgents := []string{"agent-1", "agent-2", "agent-3"}
	for _, agentID := range validAgents {
		agentDir := filepath.Join(tmpDir, agentID)
		if err := os.MkdirAll(agentDir, 0755); err != nil {
			t.Fatalf("Failed to create agent dir: %v", err)
		}
		personalityPath := filepath.Join(agentDir, "personality.json")
		if err := os.WriteFile(personalityPath, []byte("{}"), 0644); err != nil {
			t.Fatalf("Failed to create personality file: %v", err)
		}
	}

	// Create invalid agent directory (no personality.json)
	invalidDir := filepath.Join(tmpDir, "invalid-agent")
	if err := os.MkdirAll(invalidDir, 0755); err != nil {
		t.Fatalf("Failed to create invalid agent dir: %v", err)
	}

	// Create a file (not a directory)
	filePath := filepath.Join(tmpDir, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test discovery
	discovered := discoverAgents(tmpDir)

	if len(discovered) != len(validAgents) {
		t.Errorf("Expected %d agents, got %d", len(validAgents), len(discovered))
	}

	// Check that all valid agents were discovered
	for _, agentID := range validAgents {
		found := false
		for _, id := range discovered {
			if id == agentID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find agent %s in discovered list", agentID)
		}
	}

	// Check that invalid agent was not discovered
	for _, id := range discovered {
		if id == "invalid-agent" {
			t.Errorf("Invalid agent should not be in discovered list")
		}
	}
}

func TestDiscoverAgents_NonexistentDir(t *testing.T) {
	// Test with nonexistent directory
	ids := discoverAgents("/nonexistent/path")
	if ids != nil {
		t.Errorf("Expected nil for nonexistent directory, got %v", ids)
	}
}

func TestLoadConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	// Create test config
	config := AgentsConfig{}
	config.Agents.Enabled = []string{"agent-1", "agent-2", "agent-3"}

	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test loading
	ids, err := loadConfigFile(configPath)
	if err != nil {
		t.Fatalf("Failed to load config file: %v", err)
	}

	expected := []string{"agent-1", "agent-2", "agent-3"}
	if !reflect.DeepEqual(ids, expected) {
		t.Errorf("Expected %v, got %v", expected, ids)
	}
}

func TestLoadConfigFile_Nonexistent(t *testing.T) {
	_, err := loadConfigFile("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestLoadConfigFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	// Write invalid YAML
	if err := os.WriteFile(configPath, []byte("not: valid: yaml: content:"), 0644); err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}

	_, err := loadConfigFile(configPath)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestGetAgentIDs_Priority(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup: Create test agents directory with valid agents
	validAgents := []string{"discover-1", "discover-2"}
	for _, agentID := range validAgents {
		agentDir := filepath.Join(tmpDir, agentID)
		_ = os.MkdirAll(agentDir, 0755)
		personalityPath := filepath.Join(agentDir, "personality.json")
		_ = os.WriteFile(personalityPath, []byte("{}"), 0644)
	}

	// Setup: Create test config file
	configPath := filepath.Join(tmpDir, "test_config.yaml")
	config := AgentsConfig{}
	config.Agents.Enabled = []string{"config-1", "config-2"}
	data, _ := yaml.Marshal(config)
	_ = os.WriteFile(configPath, data, 0644)

	// Test 1: CLI flag takes highest priority
	result := getAgentIDs("cli-1,cli-2", tmpDir, configPath)
	expected := []string{"cli-1", "cli-2"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("CLI flag priority: expected %v, got %v", expected, result)
	}

	// Test 2: Env var takes priority over config and discover
	_ = os.Setenv("SPACEMOLT_AGENTS", "env-1,env-2")
	defer func() { _ = os.Unsetenv("SPACEMOLT_AGENTS") }()

	result = getAgentIDs("", tmpDir, configPath)
	expected = []string{"env-1", "env-2"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Env var priority: expected %v, got %v", expected, result)
	}

	// Test 3: Config file takes priority over discover
	_ = os.Unsetenv("SPACEMOLT_AGENTS")
	result = getAgentIDs("", tmpDir, configPath)
	expected = []string{"config-1", "config-2"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Config file priority: expected %v, got %v", expected, result)
	}

	// Test 4: Auto-discover is lowest priority
	nonexistentConfig := filepath.Join(tmpDir, "nonexistent.yaml")
	result = getAgentIDs("", tmpDir, nonexistentConfig)
	// Should discover the two agents we created
	if len(result) != 2 {
		t.Errorf("Auto-discover: expected 2 agents, got %d", len(result))
	}
	for _, agentID := range result {
		if agentID != "discover-1" && agentID != "discover-2" {
			t.Errorf("Auto-discover: unexpected agent ID %s", agentID)
		}
	}
}

func TestInitKnowledgeBase(t *testing.T) {
	tmpDir := t.TempDir()

	// Test memory backend
	kb, err := initKnowledgeBase("memory", "")
	if err != nil {
		t.Fatalf("Failed to init memory KB: %v", err)
	}
	if kb == nil {
		t.Error("Memory KB should not be nil")
	}
	_ = kb.Close()

	// Test sqlite backend
	dbPath := filepath.Join(tmpDir, "test.db")
	kb, err = initKnowledgeBase("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to init sqlite KB: %v", err)
	}
	if kb == nil {
		t.Error("SQLite KB should not be nil")
	}
	_ = kb.Close()

	// Test invalid backend
	_, err = initKnowledgeBase("invalid", "")
	if err == nil {
		t.Error("Expected error for invalid backend")
	}
}

func TestInitLLMClient(t *testing.T) {
	client, err := initLLMClient("http://localhost:11434", "llama3.2")
	if err != nil {
		t.Errorf("Failed to initialize LLM client: %v", err)
	}
	if client == nil {
		t.Error("LLM client should not be nil")
	}
}

func TestInitCredentialsProvider(t *testing.T) {
	tmpDir := t.TempDir()

	// Test file backend
	provider, err := initCredentialsProvider("file", tmpDir, "data/agents")
	if err != nil {
		t.Fatalf("Failed to init file provider: %v", err)
	}
	if provider == nil {
		t.Error("File provider should not be nil")
	}

	// Test keyring backend
	provider, err = initCredentialsProvider("keyring", "", "")
	if err != nil {
		t.Fatalf("Failed to init keyring provider: %v", err)
	}
	if provider == nil {
		t.Error("Keyring provider should not be nil")
	}

	// Test sqlite backend (requires passphrase)
	_ = os.Setenv("SPACEMOLT_PASSPHRASE", "test-passphrase")
	defer func() { _ = os.Unsetenv("SPACEMOLT_PASSPHRASE") }()

	dbPath := filepath.Join(tmpDir, "creds.db")
	provider, err = initCredentialsProvider("sqlite", tmpDir, dbPath)
	if err != nil {
		t.Fatalf("Failed to init sqlite provider: %v", err)
	}
	if provider == nil {
		t.Error("SQLite provider should not be nil")
	}

	// Test invalid backend
	_, err = initCredentialsProvider("invalid", "", "")
	if err == nil {
		t.Error("Expected error for invalid backend")
	}
}

func TestMigrateCredentials(t *testing.T) {
	tmpDir := t.TempDir()

	// Create old credentials structure
	oldCredsDir := filepath.Join(tmpDir, "credentials")
	agentID := "test-agent"
	oldAgentDir := filepath.Join(oldCredsDir, agentID)
	if err := os.MkdirAll(oldAgentDir, 0755); err != nil {
		t.Fatalf("Failed to create old agent dir: %v", err)
	}

	// Write old credentials
	oldCredsPath := filepath.Join(oldAgentDir, "credentials.json")
	credData := `{
  "username": "test-user",
  "password": "test-pass",
  "empire": "voidborn"
}`
	if err := os.WriteFile(oldCredsPath, []byte(credData), 0600); err != nil {
		t.Fatalf("Failed to write old credentials: %v", err)
	}

	// Create new agents directory
	newAgentsDir := filepath.Join(tmpDir, "agents")

	// Run migration
	if err := migrateCredentials(oldCredsDir, newAgentsDir); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Verify credentials were migrated
	newCredsPath := filepath.Join(newAgentsDir, agentID, "credentials.json")
	if _, err := os.Stat(newCredsPath); os.IsNotExist(err) {
		t.Error("Credentials were not migrated to new location")
	}

	// Verify content matches
	newData, err := os.ReadFile(newCredsPath)
	if err != nil {
		t.Fatalf("Failed to read migrated credentials: %v", err)
	}
	if string(newData) != credData {
		t.Errorf("Migrated credentials don't match. Got: %s, Want: %s", newData, credData)
	}

	// Test that re-running migration doesn't fail or duplicate
	if err := migrateCredentials(oldCredsDir, newAgentsDir); err != nil {
		t.Errorf("Second migration should not fail: %v", err)
	}

	// Test skip migration when paths are same
	if err := migrateCredentials(newAgentsDir, newAgentsDir); err != nil {
		t.Errorf("Migration with same paths should not fail: %v", err)
	}

	// Test migration with non-existent old directory
	if err := migrateCredentials(filepath.Join(tmpDir, "nonexistent"), newAgentsDir); err != nil {
		t.Errorf("Migration with non-existent old dir should not fail: %v", err)
	}
}
