package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/user/spacemolt/pkg/credentials"
)

func main() {
	fmt.Println("=== Phase 1 Credential Provider Test ===\n")

	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "spacemolt-credentials-test-*")
	if err != nil {
		log.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Printf("Testing in: %s\n\n", tmpDir)

	// Test 1: StaticProvider
	fmt.Println("1. Testing StaticProvider...")
	staticProvider := credentials.NewStaticProvider("testuser", "testtoken", "voidborn")
	creds, err := staticProvider.GetCredentials(ctx, "any-agent")
	if err != nil {
		log.Fatalf("StaticProvider failed: %v", err)
	}
	fmt.Printf("   ✓ StaticProvider: username=%s, token=%s\n", creds.Username, creds.Token)

	// Test 2: FileProvider
	fmt.Println("\n2. Testing FileProvider...")
	agentsDir := filepath.Join(tmpDir, "agents")
	fileProvider := credentials.NewFileProvider(agentsDir)

	// Store credentials
	err = fileProvider.StoreCredentials(ctx, "explorer-7", &credentials.Credentials{
		Username: "explorer-7",
		Token:     "explorer-token-123",
		Empire:    "voidborn",
	})
	if err != nil {
		log.Fatalf("FileProvider StoreCredentials failed: %v", err)
	}

	// Retrieve credentials
	retrieved, err := fileProvider.GetCredentials(ctx, "explorer-7")
	if err != nil {
		log.Fatalf("FileProvider GetCredentials failed: %v", err)
	}
	fmt.Printf("   ✓ FileProvider: stored and retrieved username=%s\n", retrieved.Username)

	// Test 3: FallbackProvider
	fmt.Println("\n3. Testing FallbackProvider...")
	fallback := credentials.NewFallbackProvider(
		&mockProvider{exists: false},                                        // First: doesn't exist
		fileProvider,                                                        // Second: file provider
		credentials.NewStaticProvider("fallback", "fallback-token", "voidborn"), // Third: static
	)

	creds, err = fallback.GetCredentials(ctx, "explorer-7")
	if err != nil {
		log.Fatalf("FallbackProvider failed: %v", err)
	}
	fmt.Printf("   ✓ FallbackProvider: got credentials from file provider (username=%s)\n", creds.Username)

	// Test 4: LegacyProvider
	fmt.Println("\n4. Testing LegacyProvider...")
	legacyFile := filepath.Join(tmpDir, "legacy-creds.json")
	legacyProvider := credentials.NewLegacyProvider(legacyFile)

	err = legacyProvider.StoreCredentials(ctx, "agent-1", &credentials.Credentials{
		Username: "legacyuser",
		Token:     "legacy-token",
		Empire:    "voidborn",
	})
	if err != nil {
		log.Fatalf("LegacyProvider StoreCredentials failed: %v", err)
	}

	// Legacy provider appends agent ID to username
	agent1Creds, _ := legacyProvider.GetCredentials(ctx, "agent-1")
	agent2Creds, _ := legacyProvider.GetCredentials(ctx, "agent-2")
	fmt.Printf("   ✓ LegacyProvider: agent-1 username=%s, agent-2 username=%s\n", agent1Creds.Username, agent2Creds.Username)

	// Test 5: EnvProvider
	fmt.Println("\n5. Testing EnvProvider...")
	os.Setenv("SPACEMOLT_AGENT_MINER_2_USERNAME", "miner-2")
	os.Setenv("SPACEMOLT_AGENT_MINER_2_TOKEN", "miner-token-123")
	defer func() {
		os.Unsetenv("SPACEMOLT_AGENT_MINER_2_USERNAME")
		os.Unsetenv("SPACEMOLT_AGENT_MINER_2_TOKEN")
	}()

	envProvider := credentials.NewEnvProvider("")
	minerCreds, err := envProvider.GetCredentials(ctx, "miner-2")
	if err != nil {
		log.Fatalf("EnvProvider failed: %v", err)
	}
	fmt.Printf("   ✓ EnvProvider: loaded from environment (username=%s)\n", minerCreds.Username)

	// List agents from env
	agents, _ := envProvider.ListAgents(ctx)
	fmt.Printf("   ✓ EnvProvider.ListAgents: found %d agent(s) in environment\n", len(agents))

	// Test 6: ListAgents aggregation
	fmt.Println("\n6. Testing FallbackProvider ListAgents aggregation...")
	fallback2 := credentials.NewFallbackProvider(
		fileProvider,
		&mockProvider{
			agents: []string{"static-agent-1", "static-agent-2"},
		},
	)

	agents, err = fallback2.ListAgents(ctx)
	if err != nil {
		log.Fatalf("FallbackProvider ListAgents failed: %v", err)
	}
	fmt.Printf("   ✓ ListAgents: found %d agent(s) across providers\n", len(agents))

	// Test 7: Error handling
	fmt.Println("\n7. Testing error handling...")
	_, err = fileProvider.GetCredentials(ctx, "nonexistent")
	if !credentials.IsErrCredentialsNotFound(err) {
		log.Fatalf("Expected ErrCredentialsNotFound, got: %v", err)
	}
	fmt.Printf("   ✓ Error handling: correct error for missing credentials\n")

	// Test 8: File permissions
	fmt.Println("\n8. Testing file permissions...")
	err = fileProvider.ValidatePermissions("explorer-7")
	if err != nil {
		log.Fatalf("ValidatePermissions failed: %v", err)
	}
	fmt.Printf("   ✓ File permissions: 0600 (secure)\n")

	fmt.Println("\n=== All Phase 1 Tests Passed ===")
	fmt.Println("\nReady to proceed with Phase 2: SQLite encryption")
}

// mockProvider is a test provider for fallback testing
type mockProvider struct {
	exists  bool
	agents  []string
	getFunc func(ctx context.Context, agentID string) (*credentials.Credentials, error)
}

func (m *mockProvider) GetCredentials(ctx context.Context, agentID string) (*credentials.Credentials, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, agentID)
	}
	if !m.exists {
		return nil, credentials.ErrCredentialsNotFound
	}
	return &credentials.Credentials{
		Username: "mock-user",
		Token:     "mock-token",
		Empire:    "voidborn",
	}, nil
}

func (m *mockProvider) StoreCredentials(ctx context.Context, agentID string, creds *credentials.Credentials) error {
	return nil
}

func (m *mockProvider) RemoveCredentials(ctx context.Context, agentID string) error {
	return nil
}

func (m *mockProvider) ListAgents(ctx context.Context) ([]string, error) {
	return m.agents, nil
}
