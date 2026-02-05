package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/llm"
)

var (
	dbBackend = flag.String("db-backend", "sqlite", "Database backend: 'sqlite' or 'memory'")
	dbPath    = flag.String("db-path", "spacemolt-knowledge.db", "Path to SQLite database file (only used with sqlite backend)")
)

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: agent <personality.json>")
		fmt.Println("Example: agent data/agents/explorer-7/personality.json")
		os.Exit(1)
	}

	personalityPath := args[0]

	fmt.Println("=== Spacemolt Agent Test ===")
	fmt.Printf("Loading personality from: %s\n", personalityPath)

	// Load personality
	personality, err := agent.LoadPersonalityJSON(personalityPath)
	if err != nil {
		log.Fatalf("Failed to load personality: %v", err)
	}

	fmt.Printf("✓ Loaded personality: %s (%s)\n", personality.Name, personality.Role)
	fmt.Printf("  Faction: %s\n", personality.Faction)
	fmt.Printf("  Traits: curiosity=%.2f, risk_tolerance=%.2f\n",
		personality.Traits["curiosity"], personality.Traits["risk_tolerance"])
	fmt.Printf("  Primary motivation: %s\n", personality.Motivations.Primary)

	// Create knowledge base
	var kb knowledge.Base
	switch *dbBackend {
	case "sqlite":
		sqliteKB, err := knowledge.NewSQLiteKB(knowledge.Config{
			DBPath:       *dbPath,
			WAL:          true,
			MaxOpenConns: 25,
			MaxIdleConns: 5,
			BusyTimeout:  5 * time.Second,
		})
		if err != nil {
			log.Fatalf("Failed to create SQLite knowledge base: %v", err)
		}
		kb = sqliteKB
		fmt.Printf("✓ Created SQLite knowledge base at %s\n", *dbPath)
		defer func() { _ = kb.Close() }()
	case "memory":
		kb = knowledge.NewMemoryKB()
		fmt.Println("✓ Created in-memory knowledge base")
	default:
		log.Fatalf("Unknown db-backend: %s (use 'sqlite' or 'memory')", *dbBackend)
	}

	// Create LLM client
	llmClient := llm.New(llm.Config{
		BaseURL: "http://localhost:11434",
		Model:   "llama3.2",
		Timeout: 60 * time.Second,
	})
	fmt.Println("✓ Created LLM client (Ollama at http://localhost:11434)")

	// Test LLM connection
	fmt.Println("\n=== Testing LLM Connection ===")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := llmClient.TestConnection(ctx); err != nil {
		log.Printf("Warning: Could not connect to Ollama: %v", err)
		log.Printf("Make sure Ollama is running: ollama serve")
		log.Printf("And the model is available: ollama pull llama3.2")
	} else {
		fmt.Println("✓ Connected to Ollama successfully")
	}

	// Create agent memory
	memory := agent.NewKBMemory(kb, personality.ID)

	// Create agent
	agnt := agent.NewBaseAgent(
		personality.ID,
		personality,
		memory,
		llmClient,
	)
	fmt.Printf("✓ Created agent: %s (ID: %s)\n", agnt.Name(), agnt.ID())

	// Start agent
	if err := agnt.Start(ctx); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}
	fmt.Println("✓ Agent started")

	// Show agent status
	status := agnt.Status()
	fmt.Printf("\n=== Agent Status ===\n")
	fmt.Printf("State: %v\n", status.State)
	fmt.Printf("Current Action: %s\n", status.CurrentAction)

	// Test decision making if Ollama is available
	fmt.Println("\n=== Testing Decision Making ===")
	fmt.Println("Creating mock game state for decision test...")

	// Create a mock game state for testing
	// Note: In a real scenario, this would come from the game client
	fmt.Println("Agent setup complete!")
	fmt.Println("\nNext steps:")
	fmt.Println("1. Run 'go run cmd/watcher' to start the watcher TUI")
	fmt.Println("2. The agent manager will spawn and control agents")
	fmt.Println("3. Agents will connect to the game and make autonomous decisions")
}
