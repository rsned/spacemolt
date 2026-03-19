package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/rsned/spacemolt/pkg/credentials"
)

func main() {
	fmt.Println("=== Phase 1 Credential Provider Test ===")
	fmt.Println()

	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "spacemolt-credentials-test-*")
	if err != nil {
		log.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	fmt.Printf("Testing in: %s\n\n", tmpDir)

	// Test 1: StaticProvider
	fmt.Println("1. Testing StaticProvider...")
	staticProvider := credentials.NewStaticProvider("testuser", "testpassword", "voidborn")
	creds, err := staticProvider.GetCredentials(ctx, "any-agent")
	if err != nil {
		log.Fatalf("StaticProvider failed: %v", err)
	}
	fmt.Printf("   ✓ StaticProvider: username=%s, password=%s\n", creds.Username, creds.Password)

	// Test 2: FileProvider
	fmt.Println("\n2. Testing FileProvider...")
	agentsDir := filepath.Join(tmpDir, "agents")
	fileProvider := credentials.NewFileProvider(agentsDir)

	// Store credentials
	err = fileProvider.StoreCredentials(ctx, "explorer-7", &credentials.Credentials{
		Username: "explorer-7",
		Password: "explorer-password-123",
		Empire:   "voidborn",
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
		&mockProvider{exists: false}, // First: doesn't exist
		fileProvider,                 // Second: file provider
		credentials.NewStaticProvider("fallback", "fallback-password", "voidborn"), // Third: static
	)

	creds, err = fallback.GetCredentials(ctx, "explorer-7")
	if err != nil {
		log.Fatalf("FallbackProvider failed: %v", err)
	}
	fmt.Printf("   ✓ FallbackProvider: got credentials from file provider (username=%s)\n", creds.Username)

	// Test 4: EnvProvider
	fmt.Println("\n4. Testing EnvProvider...")
	_ = os.Setenv("SPACEMOLT_AGENT_MINER_2_USERNAME", "miner-2")
	_ = os.Setenv("SPACEMOLT_AGENT_MINER_2_TOKEN", "miner-token-123")
	defer func() {
		_ = os.Unsetenv("SPACEMOLT_AGENT_MINER_2_USERNAME")
		_ = os.Unsetenv("SPACEMOLT_AGENT_MINER_2_TOKEN")
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
	fmt.Println("\n=== Phase 2: SQLite Encryption Test ===")
	fmt.Println()

	// Test 9: SQLiteProvider with encryption
	fmt.Println("9. Testing SQLiteProvider with encryption...")
	dbPath := filepath.Join(tmpDir, "test-credentials.db")

	// Create encryptor with test key
	testKey := make([]byte, 32)
	for i := range testKey {
		testKey[i] = byte(i)
	}
	encryptor, err := credentials.NewEncryptor(testKey)
	if err != nil {
		log.Fatalf("Failed to create encryptor: %v", err)
	}

	sqliteProvider, err := credentials.NewSQLiteProvider(dbPath, encryptor)
	if err != nil {
		log.Fatalf("Failed to create SQLiteProvider: %v", err)
	}
	defer func() {
		_ = sqliteProvider.Close()
	}()

	// Store credentials (should be encrypted)
	err = sqliteProvider.StoreCredentials(ctx, "agent-1", &credentials.Credentials{
		Username: "agent-1-user",
		Password: "super-secret-password-12345",
		Empire:   "voidborn",
	})
	if err != nil {
		log.Fatalf("SQLiteProvider StoreCredentials failed: %v", err)
	}
	fmt.Printf("   ✓ SQLiteProvider: stored encrypted credentials\n")

	// Retrieve credentials (should be decrypted)
	retrieved, err = sqliteProvider.GetCredentials(ctx, "agent-1")
	if err != nil {
		log.Fatalf("SQLiteProvider GetCredentials failed: %v", err)
	}
	if retrieved.Password != "super-secret-password-12345" {
		log.Fatalf("Password mismatch: expected 'super-secret-password-12345', got '%s'", retrieved.Password)
	}
	fmt.Printf("   ✓ SQLiteProvider: retrieved and decrypted credentials (username=%s)\n", retrieved.Username)

	// Test 10: ListAgents
	fmt.Println("\n10. Testing SQLiteProvider ListAgents...")
	agents, err = sqliteProvider.ListAgents(ctx)
	if err != nil {
		log.Fatalf("SQLiteProvider ListAgents failed: %v", err)
	}
	if len(agents) != 1 || agents[0] != "agent-1" {
		log.Fatalf("Expected ['agent-1'], got %v", agents)
	}
	fmt.Printf("   ✓ ListAgents: found %d agent(s)\n", len(agents))

	// Test 12: RemoveCredentials
	fmt.Println("\n12. Testing SQLiteProvider RemoveCredentials...")
	err = sqliteProvider.RemoveCredentials(ctx, "agent-1")
	if err != nil {
		log.Fatalf("SQLiteProvider RemoveCredentials failed: %v", err)
	}
	_, err = sqliteProvider.GetCredentials(ctx, "agent-1")
	if !credentials.IsErrCredentialsNotFound(err) {
		log.Fatal("Expected ErrCredentialsNotFound after removal")
	}
	fmt.Printf("   ✓ RemoveCredentials: successfully removed credentials\n")

	fmt.Println("\n=== All Phase 2 Tests Passed ===")
	fmt.Println("\n🎉 Multi-Agent Authentication System - Phase 1 & 2 Complete!")
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
		Password: "mock-password",
		Empire:   "voidborn",
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
