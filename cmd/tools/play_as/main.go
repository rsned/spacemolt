// Command: play_as
// Usage: play_as <agent-id>
//
// Interactive game terminal for playing as an agent using MCP transport.
// Provides a shell-like prompt for sending game commands and viewing responses.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: play_as <agent-id>")
		fmt.Println("Example: play_as explorer-1")
		fmt.Println("\nCommands (case-insensitive):")
		fmt.Println("  dock              - Dock at current POI")
		fmt.Println("  undock            - Undock from current POI")
		fmt.Println("  travel <poi>      - Travel to a POI")
		fmt.Println("  get_status        - Get current status")
		fmt.Println("  get_system        - Get current system details")
		fmt.Println("  get_poi           - Get current POI details")
		fmt.Println("  mine              - Start mining")
		fmt.Println("  repair            - Repair ship")
		fmt.Println("  refuel            - Refuel ship")
		fmt.Println("  status            - Show current game state")
		fmt.Println("  exit, quit        - Exit the terminal")
		os.Exit(1)
	}

	agentID := args[0]
	logger := log.New(os.Stdout, fmt.Sprintf("[PLAY_AS-%s] ", agentID), log.LstdFlags)

	ctx := context.Background()

	// Initialize MCP agent
	logger.Printf("Initializing agent %s...", agentID)
	client, creds, err := game.InitializeMCPAgent(agentID, logger, ctx, true) // Enable debug logging
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Error closing client: %v", err)
		}
	}()

	logger.Printf("Connected as: %s (Empire: %s)", creds.Username, creds.Empire)

	// Show initial status
	fmt.Println("\n╔════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    SPACE MOLT GAME TERMINAL                      ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════╝")
	fmt.Printf("\nLogged in as: %s\n", creds.Username)
	fmt.Printf("Empire: %s\n", creds.Empire)
	fmt.Println("\nType 'help' for available commands, 'exit' or 'quit' to leave.")
	fmt.Println()

	// Run REPL loop
	runREPL(client, ctx)
}

func runREPL(client game.GameClient, ctx context.Context) {
	reader := bufio.NewReader(os.Stdin)

	for {
		// Show prompt
		fmt.Print("$ ")

		// Read input
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		// Trim whitespace
		cmd := strings.TrimSpace(line)
		if cmd == "" {
			continue
		}

		// Parse command
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			continue
		}

		command := strings.ToLower(parts[0])

		// Handle exit/quit
		if command == "exit" || command == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		// Handle help
		if command == "help" {
			printHelp()
			continue
		}

		// Execute command
		if err := executeCommand(client, ctx, parts); err != nil {
			fmt.Printf("Error: %v\n", err)
		}

		// Add small delay between commands
		time.Sleep(500 * time.Millisecond)
	}
}

func executeCommand(client game.GameClient, ctx context.Context, parts []string) error {
	cmd := strings.ToLower(parts[0])

	fmt.Printf("\n─── Executing: %s ───\n", cmd)

	switch cmd {
	case "dock":
		if err := client.Dock(ctx); err != nil {
			return err
		}
		printState(client)
		fmt.Println("✓ Docked")

	case "undock":
		if err := client.Undock(ctx); err != nil {
			return err
		}
		time.Sleep(12 * time.Second) // Wait for undock to complete
		printState(client)
		fmt.Println("✓ Undocked")

	case "travel":
		if len(parts) < 2 {
			return fmt.Errorf("usage: travel <poi-id>")
		}
		targetPOI := strings.Join(parts[1:], " ")
		_, err := client.Travel(ctx, targetPOI)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Travel initiated to: %s\n", targetPOI)
		time.Sleep(12 * time.Second) // Wait for travel
		printState(client)

	case "get_status":
		if err := client.GetStatus(ctx); err != nil {
			return err
		}
		printState(client)

	case "get_system":
		if err := client.GetSystem(ctx); err != nil {
			return err
		}
		printState(client)

	case "get_poi":
		if err := client.GetPOI(ctx); err != nil {
			return err
		}
		printState(client)

	case "mine":
		if err := client.Mine(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Mining started")
		time.Sleep(12 * time.Second)
		printState(client)

	case "repair":
		if err := client.Repair(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Repairing...")
		time.Sleep(3 * time.Second)
		printState(client)

	case "refuel":
		if err := client.Refuel(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Refueling...")
		time.Sleep(3 * time.Second)
		printState(client)

	case "status":
		printState(client)

	default:
		return fmt.Errorf("unknown command: %s (type 'help' for available commands)", cmd)
	}

	fmt.Println("─────────────────────")
	return nil
}

func printState(client game.GameClient) {
	state := client.GetState()

	// Pretty print JSON
	jsonData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling state: %v\n", err)
		return
	}

	fmt.Println("\n📊 Current State:")
	fmt.Println(string(jsonData))
}

func printHelp() {
	fmt.Println("\nAvailable Commands:")
	fmt.Println("  dock              - Dock at current POI")
	fmt.Println("  undock            - Undock from current POI")
	fmt.Println("  travel <poi>      - Travel to a POI (e.g., 'travel mars')")
	fmt.Println("  get_status        - Get current status")
	fmt.Println("  get_system        - Get current system details")
	fmt.Println("  get_poi           - Get current POI details")
	fmt.Println("  mine              - Start mining")
	fmt.Println("  repair            - Repair ship")
	fmt.Println("  refuel            - Refuel ship")
	fmt.Println("  status            - Show current game state")
	fmt.Println("  help              - Show this help message")
	fmt.Println("  exit, quit        - Exit the terminal")
	fmt.Println()
	fmt.Println("Note: Commands are case-insensitive. Arguments are case-sensitive.")
	fmt.Println("      JSON responses are pretty-printed for readability.")
	fmt.Println()
}
