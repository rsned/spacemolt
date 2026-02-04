package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/user/spacemolt/pkg/credentials"
	"github.com/user/spacemolt/pkg/knowledge"
	"github.com/user/spacemolt/pkg/llm"
)

func TestManager_SpawnAgentWithGame_Registration(t *testing.T) {
	// Create temp directory for test data
	tmpDir := t.TempDir()

	// Create minimal personality
	personality := Personality{
		ID:      "test-agent",
		Name:    "Test Agent",
		Role:    "test",
		Faction: "voidborn",
		Traits:  map[string]float64{"curiosity": 0.8},
		Motivations: Motivations{
			Primary: "test",
			Weights: map[string]float64{"test": 1.0},
		},
		Skills:    map[string]string{"test": "basic"},
		Biography: "Test agent",
	}

	// Setup
	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{BaseURL: "http://localhost:11434", Model: "llama3.2"})
	credsProvider := credentials.NewFileProvider(tmpDir)

	config := DefaultManagerConfig()
	config.AgentsDataDir = tmpDir
	config.MaxAgents = 5

	manager := NewManager(kb, llmClient, credsProvider, config)

	// Note: This test requires a running game server to fully test
	// For now, we'll test the structure and basic validation
	t.Skip("Skipping integration test - requires game server")

	ctx := context.Background()
	_, err := manager.SpawnAgentWithGame(ctx, personality)
	if err != nil {
		t.Logf("Expected error without game server: %v", err)
	}
}

func TestManager_SaveLoadCredentials(t *testing.T) {
	tmpDir := t.TempDir()

	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{})
	credsProvider := credentials.NewFileProvider(filepath.Join(tmpDir, "provider"))

	config := DefaultManagerConfig()
	config.AgentsDataDir = filepath.Join(tmpDir, "agents")
	manager := NewManager(kb, llmClient, credsProvider, config)

	ctx := context.Background()

	// Test saving credentials
	creds := &credentials.Credentials{
		Username: "test-user",
		Password:    "test-token-123",
		Empire:   "voidborn",
	}

	err := manager.saveCredentialsWithFallback(ctx, "test-agent", creds)
	if err != nil {
		t.Fatalf("Failed to save credentials: %v", err)
	}

	// Test loading credentials (should work from either provider or fallback)
	loadedCreds, err := manager.loadCredentialsWithFallback(ctx, "test-agent")
	if err != nil {
		t.Fatalf("Failed to load credentials: %v", err)
	}

	if loadedCreds.Username != creds.Username {
		t.Errorf("Username mismatch: expected %s, got %s", creds.Username, loadedCreds.Username)
	}
	if loadedCreds.Password != creds.Password {
		t.Errorf("Token mismatch: expected %s, got %s", creds.Password, loadedCreds.Password)
	}
	if loadedCreds.Empire != creds.Empire {
		t.Errorf("Empire mismatch: expected %s, got %s", creds.Empire, loadedCreds.Empire)
	}
}

func TestManager_LoadCredentials_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{})
	credsProvider := credentials.NewFileProvider(tmpDir)

	config := DefaultManagerConfig()
	config.AgentsDataDir = tmpDir
	manager := NewManager(kb, llmClient, credsProvider, config)

	ctx := context.Background()

	// Try to load non-existent credentials
	_, err := manager.loadCredentialsWithFallback(ctx, "nonexistent-agent")
	if err == nil {
		t.Fatal("Expected error for nonexistent credentials")
	}

	if !credentials.IsErrCredentialsNotFound(err) {
		t.Errorf("Expected ErrCredentialsNotFound, got: %v", err)
	}
}

func TestManager_SanitizeUsername(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with spaces", "with_spaces"},
		{"with-dashes", "with-dashes"},
		{"with_underscores", "with_underscores"},
		{"MixedCase123", "MixedCase123"},
		{"special!@#chars", "special___chars"},
		{"very-long-username-that-exceeds-the-limit-and-should-be-truncated", "very-long-username-that-exceeds-"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeUsername(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeUsername(%q) = %q, want %q", tt.input, result, tt.expected)
			}
			if len(result) > 32 {
				t.Errorf("Result length %d exceeds maximum 32: %q", len(result), result)
			}
		})
	}
}

func TestManager_ManagerConfig(t *testing.T) {
	config := DefaultManagerConfig()

	if config.MaxAgents != 10 {
		t.Errorf("Default MaxAgents should be 10, got %d", config.MaxAgents)
	}

	if config.GameServerURL == "" {
		t.Error("Default GameServerURL should not be empty")
	}

	if config.AgentsDataDir == "" {
		t.Error("Default AgentsDataDir should not be empty")
	}

	if config.RunnerConfig.DecisionInterval == 0 {
		t.Error("Default DecisionInterval should not be zero")
	}
}

func TestManager_NewManagerLegacy(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{})
	credsProvider := credentials.NewFileProvider(t.TempDir())

	manager := NewManagerLegacy(kb, llmClient, credsProvider, 5)

	if manager.maxAgents != 5 {
		t.Errorf("Expected maxAgents=5, got %d", manager.maxAgents)
	}

	if manager.gameServerURL == "" {
		t.Error("gameServerURL should be set by default")
	}

	if manager.agentsDataDir == "" {
		t.Error("agentsDataDir should be set by default")
	}
}

func TestManager_GetRunner(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{})
	credsProvider := credentials.NewFileProvider(t.TempDir())

	config := DefaultManagerConfig()
	manager := NewManager(kb, llmClient, credsProvider, config)

	// Test getting non-existent runner
	_, ok := manager.GetRunner("nonexistent")
	if ok {
		t.Error("Expected GetRunner to return false for nonexistent runner")
	}
}

func TestManager_ListRunners(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{})
	credsProvider := credentials.NewFileProvider(t.TempDir())

	config := DefaultManagerConfig()
	manager := NewManager(kb, llmClient, credsProvider, config)

	// Test empty list
	runners := manager.ListRunners()
	if len(runners) != 0 {
		t.Errorf("Expected empty runners list, got %d", len(runners))
	}
}

func TestManager_AgentCount(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{})
	credsProvider := credentials.NewFileProvider(t.TempDir())

	config := DefaultManagerConfig()
	manager := NewManager(kb, llmClient, credsProvider, config)

	if manager.AgentCount() != 0 {
		t.Errorf("Expected AgentCount()=0, got %d", manager.AgentCount())
	}
}

func TestManager_CredentialsFallback(t *testing.T) {
	tmpDir := t.TempDir()

	// Create credentials file manually in fallback location
	agentDir := filepath.Join(tmpDir, "agents", "manual-agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create agent dir: %v", err)
	}

	creds := credentials.Credentials{
		Username: "manual-user",
		Password:    "manual-token",
		Empire:   "voidborn",
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal credentials: %v", err)
	}

	credsPath := filepath.Join(agentDir, "credentials.json")
	if err := os.WriteFile(credsPath, data, 0600); err != nil {
		t.Fatalf("Failed to write credentials: %v", err)
	}

	// Now try to load with manager
	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{})
	credsProvider := credentials.NewFileProvider(filepath.Join(tmpDir, "provider"))

	config := DefaultManagerConfig()
	config.AgentsDataDir = filepath.Join(tmpDir, "agents")
	manager := NewManager(kb, llmClient, credsProvider, config)

	ctx := context.Background()
	loadedCreds, err := manager.loadCredentialsWithFallback(ctx, "manual-agent")
	if err != nil {
		t.Fatalf("Failed to load credentials from fallback: %v", err)
	}

	if loadedCreds.Username != creds.Username {
		t.Errorf("Username mismatch: expected %s, got %s", creds.Username, loadedCreds.Username)
	}
}

func TestManager_ConnectionRetry_Timeout(t *testing.T) {
	// This test verifies the retry mechanism structure
	// Actual retry testing requires a mock server

	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{})
	credsProvider := credentials.NewFileProvider(t.TempDir())

	config := DefaultManagerConfig()
	manager := NewManager(kb, llmClient, credsProvider, config)

	if manager == nil {
		t.Fatal("Manager should not be nil")
	}

	if MaxConnectionRetries != 3 {
		t.Errorf("Expected MaxConnectionRetries=3, got %d", MaxConnectionRetries)
	}

	if MaxLoginRetries != 3 {
		t.Errorf("Expected MaxLoginRetries=3, got %d", MaxLoginRetries)
	}

	// Verify manager has the retry constants available
	t.Logf("Connection retries: %d", MaxConnectionRetries)
	t.Logf("Login retries: %d", MaxLoginRetries)
}

func TestManager_StopAll(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	llmClient := llm.New(llm.Config{})
	credsProvider := credentials.NewFileProvider(t.TempDir())

	config := DefaultManagerConfig()
	manager := NewManager(kb, llmClient, credsProvider, config)

	// Test stopping empty manager
	err := manager.StopAll()
	if err != nil {
		t.Errorf("StopAll on empty manager should not error: %v", err)
	}

	if manager.AgentCount() != 0 {
		t.Errorf("AgentCount should be 0 after StopAll, got %d", manager.AgentCount())
	}
}
