package credentials

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestStaticProvider tests the static provider
func TestStaticProvider(t *testing.T) {
	provider := NewStaticProvider("testuser", "testtoken", "voidborn")

	ctx := context.Background()

	// Test GetCredentials
	creds, err := provider.GetCredentials(ctx, "any-agent-id")
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}

	if creds.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", creds.Username)
	}

	if creds.Token != "testtoken" {
		t.Errorf("Expected token 'testtoken', got '%s'", creds.Token)
	}

	if creds.Empire != "voidborn" {
		t.Errorf("Expected empire 'voidborn', got '%s'", creds.Empire)
	}

	// Test StoreCredentials (should be no-op)
	err = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "newuser",
		Token:     "newtoken",
		Empire:    "test",
	})
	if err != nil {
		t.Errorf("StoreCredentials failed: %v", err)
	}

	// Verify credentials didn't change
	creds2, _ := provider.GetCredentials(ctx, "agent-1")
	if creds2.Username != "testuser" {
		t.Error("Static credentials should not change")
	}

	// Test RemoveCredentials (should be no-op)
	err = provider.RemoveCredentials(ctx, "agent-1")
	if err != nil {
		t.Errorf("RemoveCredentials failed: %v", err)
	}

	// Test ListAgents
	agents, err := provider.ListAgents(ctx)
	if err != nil {
		t.Errorf("ListAgents failed: %v", err)
	}
	if agents != nil {
		t.Error("Static provider should return nil agent list")
	}
}

// TestFileProvider tests the file-based provider
func TestFileProvider(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")

	provider := NewFileProvider(agentsDir)
	ctx := context.Background()

	t.Run("Store and Retrieve Credentials", func(t *testing.T) {
		creds := &Credentials{
			Username: "test-agent",
			Token:     "test-token-123",
			Empire:    "voidborn",
		}

		// Store credentials
		err := provider.StoreCredentials(ctx, "explorer-7", creds)
		if err != nil {
			t.Fatalf("StoreCredentials failed: %v", err)
		}

		// Verify file exists with correct permissions
		credPath := provider.credentialPath("explorer-7")
		info, err := os.Stat(credPath)
		if err != nil {
			t.Fatalf("Credential file not created: %v", err)
		}

		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Errorf("Expected file permissions 0600, got %v", perm)
		}

		// Retrieve credentials
		retrieved, err := provider.GetCredentials(ctx, "explorer-7")
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}

		if retrieved.Username != "test-agent" {
			t.Errorf("Expected username 'test-agent', got '%s'", retrieved.Username)
		}

		if retrieved.Token != "test-token-123" {
			t.Errorf("Expected token 'test-token-123', got '%s'", retrieved.Token)
		}

		if retrieved.Empire != "voidborn" {
			t.Errorf("Expected empire 'voidborn', got '%s'", retrieved.Empire)
		}
	})

	t.Run("Credentials Not Found", func(t *testing.T) {
		_, err := provider.GetCredentials(ctx, "nonexistent")
		if err == nil {
			t.Error("Expected error for nonexistent agent")
		}

		if !IsErrCredentialsNotFound(err) {
			t.Errorf("Expected ErrCredentialsNotFound, got: %v", err)
		}
	})

	t.Run("Default Empire", func(t *testing.T) {
		creds := &Credentials{
			Username: "test-agent-2",
			Token:     "test-token-456",
			Empire:    "", // Empty empire should default to "voidborn"
		}

		err := provider.StoreCredentials(ctx, "miner-2", creds)
		if err != nil {
			t.Fatalf("StoreCredentials failed: %v", err)
		}

		retrieved, err := provider.GetCredentials(ctx, "miner-2")
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}

		if retrieved.Empire != "voidborn" {
			t.Errorf("Expected default empire 'voidborn', got '%s'", retrieved.Empire)
		}
	})

	t.Run("ListAgents", func(t *testing.T) {
		// Add another agent
		err := provider.StoreCredentials(ctx, "trader-1", &Credentials{
			Username: "trader-1",
			Token:     "token-789",
			Empire:    "voidborn",
		})
		if err != nil {
			t.Fatalf("StoreCredentials failed: %v", err)
		}

		// List agents
		agents, err := provider.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents failed: %v", err)
		}

		if len(agents) != 3 { // explorer-7, miner-2, trader-1
			t.Errorf("Expected 3 agents, got %d", len(agents))
		}

		// Verify agent IDs
		agentMap := make(map[string]bool)
		for _, agent := range agents {
			agentMap[agent] = true
		}

		if !agentMap["explorer-7"] || !agentMap["miner-2"] || !agentMap["trader-1"] {
			t.Error("Missing expected agents")
		}
	})

	t.Run("RemoveCredentials", func(t *testing.T) {
		err := provider.RemoveCredentials(ctx, "explorer-7")
		if err != nil {
			t.Fatalf("RemoveCredentials failed: %v", err)
		}

		// Verify credentials are gone
		_, err = provider.GetCredentials(ctx, "explorer-7")
		if err == nil {
			t.Error("Expected error after removing credentials")
		}

		if !IsErrCredentialsNotFound(err) {
			t.Errorf("Expected ErrCredentialsNotFound, got: %v", err)
		}

		// Verify file is deleted
		credPath := provider.credentialPath("explorer-7")
		if _, err := os.Stat(credPath); !os.IsNotExist(err) {
			t.Error("Credential file should be deleted")
		}
	})

	t.Run("Remove Nonexistent Credentials", func(t *testing.T) {
		err := provider.RemoveCredentials(ctx, "nonexistent")
		if err == nil {
			t.Error("Expected error when removing nonexistent credentials")
		}

		if !IsErrCredentialsNotFound(err) {
			t.Errorf("Expected ErrCredentialsNotFound, got: %v", err)
		}
	})
}

// TestLegacyProvider tests the legacy single-credential provider
func TestLegacyProvider(t *testing.T) {
	tmpDir := t.TempDir()
	credsFile := filepath.Join(tmpDir, "credentials.json")

	provider := NewLegacyProvider(credsFile)
	ctx := context.Background()

	t.Run("Store and Retrieve", func(t *testing.T) {
		creds := &Credentials{
			Username: "legacyuser",
			Token:     "legacytoken",
			Empire:    "voidborn",
		}

		err := provider.StoreCredentials(ctx, "any-agent", creds)
		if err != nil {
			t.Fatalf("StoreCredentials failed: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(credsFile); err != nil {
			t.Errorf("Credentials file not created: %v", err)
		}

		// Retrieve credentials for different agents
		agent1Creds, err := provider.GetCredentials(ctx, "agent-1")
		if err != nil {
			t.Fatalf("GetCredentials for agent-1 failed: %v", err)
		}

		agent2Creds, err := provider.GetCredentials(ctx, "agent-2")
		if err != nil {
			t.Fatalf("GetCredentials for agent-2 failed: %v", err)
		}

		// Usernames should be agent-specific (agentID appended)
		if agent1Creds.Username != "legacyuser_agent-1" {
			t.Errorf("Expected username 'legacyuser_agent-1', got '%s'", agent1Creds.Username)
		}

		if agent2Creds.Username != "legacyuser_agent-2" {
			t.Errorf("Expected username 'legacyuser_agent-2', got '%s'", agent2Creds.Username)
		}

		// Tokens should be the same
		if agent1Creds.Token != agent2Creds.Token {
			t.Error("Tokens should be the same for all agents")
		}
	})

	t.Run("Credentials Not Found", func(t *testing.T) {
		// Remove credentials file
		_ = os.Remove(credsFile)

		_, err := provider.GetCredentials(ctx, "any-agent")
		if err == nil {
			t.Error("Expected error when credentials file doesn't exist")
		}

		if !IsErrCredentialsNotFound(err) {
			t.Errorf("Expected ErrCredentialsNotFound, got: %v", err)
		}
	})

	t.Run("ListAgents", func(t *testing.T) {
		// Store credentials
		_ = provider.StoreCredentials(ctx, "agent-1", &Credentials{
			Username: "test",
			Token:     "test",
		})

		agents, err := provider.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents failed: %v", err)
		}

		if len(agents) != 1 || agents[0] != "legacy" {
			t.Errorf("Expected ['legacy'], got %v", agents)
		}
	})
}

// TestFallbackProvider tests the fallback provider
func TestFallbackProvider(t *testing.T) {
	ctx := context.Background()

	t.Run("First Provider Succeeds", func(t *testing.T) {
		provider1 := NewStaticProvider("user1", "token1", "voidborn")
		provider2 := NewStaticProvider("user2", "token2", "voidborn")

		fallback := NewFallbackProvider(provider1, provider2)

		creds, err := fallback.GetCredentials(ctx, "agent-1")
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}

		// Should get credentials from first provider
		if creds.Username != "user1" {
			t.Errorf("Expected username from first provider, got '%s'", creds.Username)
		}
	})

	t.Run("Second Provider Succeeds", func(t *testing.T) {
		// Create a custom provider that always returns "not found"
		notFoundProvider := &mockProvider{
			getFunc: func(ctx context.Context, agentID string) (*Credentials, error) {
				return nil, ErrCredentialsNotFound
			},
		}

		provider2 := NewStaticProvider("user2", "token2", "voidborn")

		fallback := NewFallbackProvider(notFoundProvider, provider2)

		creds, err := fallback.GetCredentials(ctx, "agent-1")
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}

		// Should get credentials from second provider
		if creds.Username != "user2" {
			t.Errorf("Expected username from second provider, got '%s'", creds.Username)
		}
	})

	t.Run("All Providers Fail", func(t *testing.T) {
		provider1 := &mockProvider{
			getFunc: func(ctx context.Context, agentID string) (*Credentials, error) {
				return nil, ErrCredentialsNotFound
			},
		}
		provider2 := &mockProvider{
			getFunc: func(ctx context.Context, agentID string) (*Credentials, error) {
				return nil, ErrCredentialsNotFound
			},
		}

		fallback := NewFallbackProvider(provider1, provider2)

		_, err := fallback.GetCredentials(ctx, "agent-1")
		if err == nil {
			t.Error("Expected error when all providers fail")
		}

		if !IsErrCredentialsNotFound(err) {
			t.Errorf("Expected ErrCredentialsNotFound, got: %v", err)
		}
	})

	t.Run("StoreCredentials in First Available", func(t *testing.T) {
		// Create providers that track if Store was called
		provider1 := &mockProvider{
			storeFunc: func(ctx context.Context, agentID string, creds *Credentials) error {
				if agentID == "fail-agent" {
					return ErrProviderUnavailable
				}
				return nil
			},
		}
		provider2 := &mockProvider{
			storeFunc: func(ctx context.Context, agentID string, creds *Credentials) error {
				return nil
			},
		}

		fallback := NewFallbackProvider(provider1, provider2)

		// Should succeed with first provider
		err := fallback.StoreCredentials(ctx, "agent-1", &Credentials{})
		if err != nil {
			t.Errorf("StoreCredentials failed: %v", err)
		}
	})

	t.Run("ListAgents Aggregates All Providers", func(t *testing.T) {
		provider1 := &mockProvider{
			listFunc: func(ctx context.Context) ([]string, error) {
				return []string{"agent-1", "agent-2"}, nil
			},
		}
		provider2 := &mockProvider{
			listFunc: func(ctx context.Context) ([]string, error) {
				return []string{"agent-2", "agent-3"}, nil
			},
		}

		fallback := NewFallbackProvider(provider1, provider2)

		agents, err := fallback.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents failed: %v", err)
		}

		// Should have unique agents from both providers
		if len(agents) != 3 {
			t.Errorf("Expected 3 unique agents, got %d", len(agents))
		}

		agentMap := make(map[string]bool)
		for _, agent := range agents {
			agentMap[agent] = true
		}

		for _, expected := range []string{"agent-1", "agent-2", "agent-3"} {
			if !agentMap[expected] {
				t.Errorf("Missing expected agent: %s", expected)
			}
		}
	})
}

// TestEnvProvider tests the environment variable provider
func TestEnvProvider(t *testing.T) {
	// Save original environment
	origEnvs := map[string]string{
		"SPACEMOLT_AGENT_EXPLORER_7_USERNAME": os.Getenv("SPACEMOLT_AGENT_EXPLORER_7_USERNAME"),
		"SPACEMOLT_AGENT_EXPLORER_7_TOKEN":     os.Getenv("SPACEMOLT_AGENT_EXPLORER_7_TOKEN"),
		"SPACEMOLT_AGENT_MINER_2_USERNAME":     os.Getenv("SPACEMOLT_AGENT_MINER_2_USERNAME"),
		"SPACEMOLT_AGENT_MINER_2_TOKEN":         os.Getenv("SPACEMOLT_AGENT_MINER_2_TOKEN"),
	}
	defer func() {
		// Restore environment
		for key, val := range origEnvs {
			if val == "" {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, val)
			}
		}
	}()

	provider := NewEnvProvider("")
	ctx := context.Background()

	t.Run("GetCredentials Success", func(t *testing.T) {
		_ = os.Setenv("SPACEMOLT_AGENT_EXPLORER_7_USERNAME", "explorer-7")
		_ = os.Setenv("SPACEMOLT_AGENT_EXPLORER_7_TOKEN", "explorer-token")

		creds, err := provider.GetCredentials(ctx, "explorer-7")
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}

		if creds.Username != "explorer-7" {
			t.Errorf("Expected username 'explorer-7', got '%s'", creds.Username)
		}

		if creds.Token != "explorer-token" {
			t.Errorf("Expected token 'explorer-token', got '%s'", creds.Token)
		}

		if creds.Empire != "voidborn" {
			t.Errorf("Expected default empire 'voidborn', got '%s'", creds.Empire)
		}
	})

	t.Run("GetCredentials Missing Required Fields", func(t *testing.T) {
		// Clear environment
		_ = os.Unsetenv("SPACEMOLT_AGENT_MINER_2_USERNAME")
		_ = os.Unsetenv("SPACEMOLT_AGENT_MINER_2_TOKEN")

		_, err := provider.GetCredentials(ctx, "miner-2")
		if err == nil {
			t.Error("Expected error when environment variables not set")
		}

		if !IsErrCredentialsNotFound(err) {
			t.Errorf("Expected ErrCredentialsNotFound, got: %v", err)
		}
	})

	t.Run("Custom Empire", func(t *testing.T) {
		_ = os.Setenv("SPACEMOLT_AGENT_MINER_2_USERNAME", "miner-2")
		_ = os.Setenv("SPACEMOLT_AGENT_MINER_2_TOKEN", "miner-token")
		_ = os.Setenv("SPACEMOLT_AGENT_MINER_2_EMPIRE", "minersguild")

		creds, err := provider.GetCredentials(ctx, "miner-2")
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}

		if creds.Empire != "minersguild" {
			t.Errorf("Expected empire 'minersguild', got '%s'", creds.Empire)
		}
	})

	t.Run("StoreCredentials Not Supported", func(t *testing.T) {
		err := provider.StoreCredentials(ctx, "explorer-7", &Credentials{})
		if err == nil {
			t.Error("Expected error when storing credentials in env provider")
		}
	})

	t.Run("ListAgents", func(t *testing.T) {
		// Set up multiple agents
		_ = os.Setenv("SPACEMOLT_AGENT_EXPLORER_7_USERNAME", "explorer-7")
		_ = os.Setenv("SPACEMOLT_AGENT_EXPLORER_7_TOKEN", "token1")
		_ = os.Setenv("SPACEMOLT_AGENT_MINER_2_USERNAME", "miner-2")
		_ = os.Setenv("SPACEMOLT_AGENT_MINER_2_TOKEN", "token2")

		agents, err := provider.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents failed: %v", err)
		}

		if len(agents) != 2 {
			t.Errorf("Expected 2 agents, got %d", len(agents))
		}

		agentMap := make(map[string]bool)
		for _, agent := range agents {
			agentMap[agent] = true
		}

		if !agentMap["explorer-7"] || !agentMap["miner-2"] {
			t.Error("Missing expected agents")
		}
	})
}

// TestIsErrCredentialsNotFound tests the error helper
func TestIsErrCredentialsNotFound(t *testing.T) {
	if !IsErrCredentialsNotFound(ErrCredentialsNotFound) {
		t.Error("IsErrCredentialsNotFound should return true for ErrCredentialsNotFound")
	}

	if IsErrCredentialsNotFound(ErrCredentialsInvalid) {
		t.Error("IsErrCredentialsNotFound should return false for other errors")
	}
}

// Helper: mock provider for testing
type mockProvider struct {
	getFunc    func(ctx context.Context, agentID string) (*Credentials, error)
	storeFunc  func(ctx context.Context, agentID string, creds *Credentials) error
	removeFunc func(ctx context.Context, agentID string) error
	listFunc   func(ctx context.Context) ([]string, error)
}

func (m *mockProvider) GetCredentials(ctx context.Context, agentID string) (*Credentials, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, agentID)
	}
	return nil, ErrCredentialsNotFound
}

func (m *mockProvider) StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error {
	if m.storeFunc != nil {
		return m.storeFunc(ctx, agentID, creds)
	}
	return nil
}

func (m *mockProvider) RemoveCredentials(ctx context.Context, agentID string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, agentID)
	}
	return nil
}

func (m *mockProvider) ListAgents(ctx context.Context) ([]string, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []string{}, nil
}
