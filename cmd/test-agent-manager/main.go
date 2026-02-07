package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/llm"
)

func main() {
	fmt.Println("=== Phase 3: Agent Manager Integration Test ===")
	fmt.Println()

	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "spacemolt-agent-manager-test-*")
	if err != nil {
		log.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	fmt.Printf("Testing in: %s\n\n", tmpDir)

	// Test 1: Create knowledge base
	fmt.Println("1. Creating knowledge base...")
	kb := knowledge.NewMemoryKB()
	fmt.Println("   ✓ Created in-memory knowledge base")
	fmt.Println()

	// Test 2: Create credential provider with multiple agents
	fmt.Println("2. Creating file credential provider...")
	agentsDir := filepath.Join(tmpDir, "agents")
	credsProvider := credentials.NewFileProvider(agentsDir)

	// Store credentials for multiple agents
	agents := []struct {
		id       string
		username string
		password string
	}{
		{"explorer-7", "explorer-7-user", "password-abc-123"},
		{"miner-2", "miner-2-user", "password-def-456"},
		{"trader-1", "trader-1-user", "password-ghi-789"},
	}

	for _, a := range agents {
		err := credsProvider.StoreCredentials(ctx, a.id, &credentials.Credentials{
			Username: a.username,
			Password: a.password,
			Empire:   "voidborn",
		})
		if err != nil {
			log.Fatalf("Failed to store credentials for %s: %v", a.id, err)
		}
		fmt.Printf("   ✓ Stored credentials for %s\n", a.id)
	}
	fmt.Println()

	// Test 3: Create LLM client (will fail if Ollama not running, but that's OK)
	fmt.Println("3. Creating LLM client...")
	llmClient, err := llm.New(llm.Config{
		BaseURL: "http://localhost:11434",
		Model:   "llama3.2",
		Timeout: 5,
	})
	if err != nil {
		fmt.Printf("   Warning: Failed to initialize LLM client: %v\n", err)
	} else {
		_ = llmClient.TestConnection(ctx) // Ignore errors
		fmt.Println("   ✓ Created LLM client")
	}
	fmt.Println()

	// Test 4: Create agent manager with credential provider
	fmt.Println("4. Creating agent manager...")
	agentMgr := agent.NewManagerLegacy(kb, llmClient, credsProvider, 10)
	fmt.Println("   ✓ Created agent manager with credential provider")
	fmt.Println()

	// Test 5: Spawn agents and verify credentials are loaded
	fmt.Println("5. Spawning agents with credential validation...")
	for _, a := range agents {
		// Create minimal personality
		p := agent.Personality{
			ID:      a.id,
			Name:    a.id,
			Role:    "explorer",
			Faction: "voidborn",
			Traits:  map[string]float64{"curiosity": 0.8},
			Motivations: agent.Motivations{
				Primary:   "exploration",
				Secondary: "learning",
				Weights:   map[string]float64{"exploration": 1.0},
			},
			Skills:    map[string]string{"navigation": "intermediate"},
			Biography: "A test agent",
		}

		agt, err := agentMgr.SpawnAgent(ctx, p)
		if err != nil {
			log.Fatalf("Failed to spawn agent %s: %v", a.id, err)
		}
		fmt.Printf("   ✓ Spawned agent: %s (%s)\n", agt.Name(), agt.ID())

		// Verify credentials can be retrieved
		creds, err := agentMgr.GetCredentials(ctx, a.id)
		if err != nil {
			log.Fatalf("Failed to get credentials for %s: %v", a.id, err)
		}
		if creds.Username != a.username || creds.Password != a.password {
			log.Fatalf("Credential mismatch for %s: expected %s/%s, got %s/%s",
				a.id, a.username, a.password, creds.Username, creds.Password)
		}
		fmt.Printf("   ✓ Verified credentials for %s: %s\n", a.id, creds.Username)
	}
	fmt.Println()

	// Test 6: Test credential not found error
	fmt.Println("6. Testing credential not found error...")
	_, err = agentMgr.GetCredentials(ctx, "nonexistent-agent")
	if err == nil {
		log.Fatal("Expected error for nonexistent agent")
	}
	if !credentials.IsErrCredentialsNotFound(err) {
		log.Fatalf("Expected ErrCredentialsNotFound, got: %v", err)
	}
	fmt.Println("   ✓ Correctly returns ErrCredentialsNotFound for missing credentials")
	fmt.Println()

	// Test 7: Spawn agent without credentials should fail
	fmt.Println("7. Testing spawn failure without credentials...")
	p := agent.Personality{
		ID:      "no-creds-agent",
		Name:    "No Creds",
		Role:    "explorer",
		Faction: "voidborn",
		Traits:  map[string]float64{"curiosity": 0.5},
		Motivations: agent.Motivations{
			Primary: "test",
			Weights: map[string]float64{},
		},
		Skills:    map[string]string{},
		Biography: "Should fail",
	}

	_, err = agentMgr.SpawnAgent(ctx, p)
	if err == nil {
		log.Fatal("Expected error when spawning agent without credentials")
	}
	fmt.Printf("   ✓ Correctly failed to spawn: %v\n", err)

	// Cleanup
	fmt.Println("\n8. Cleaning up...")
	if err := agentMgr.StopAll(); err != nil {
		log.Printf("Warning: Error stopping agents: %v", err)
	}
	fmt.Println("   ✓ Stopped all agents")

	fmt.Println("\n=== All Phase 3 Tests Passed ===")
	fmt.Println("\n🎉 Agent Manager Integration Complete!")
	fmt.Println("\nKey features verified:")
	fmt.Println("  • Manager accepts credential provider")
	fmt.Println("  • SpawnAgent validates credentials exist")
	fmt.Println("  • GetCredentials retrieves agent-specific credentials")
	fmt.Println("  • Spawning fails without credentials")
	fmt.Println("  • Multiple agents can have different credentials")
}
