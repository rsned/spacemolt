package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

var (
	serverURL = flag.String("server", "ws://localhost:8080/ws", "WebSocket server URL")
	username  = flag.String("username", "", "Agent username (required)")
	empire    = flag.String("empire", "voidborn", "Empire/faction name")
	saveToken = flag.Bool("save-token", false, "Save token to ~/.spacemolt/agents/<username>.token")
	verbose   = flag.Bool("v", false, "Enable verbose output")
)

func main() {
	flag.Parse()

	if *username == "" {
		fmt.Fprintln(os.Stderr, "Error: --username is required")
		flag.Usage()
		os.Exit(1)
	}

	// Setup logging
	var logger *log.Logger
	if *verbose {
		logger = log.New(os.Stdout, "[REGISTER] ", log.Ltime|log.Lmicroseconds)
	} else {
		logger = log.New(io.Discard, "", 0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Handle interrupt signals
	interruptCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Create game client
	client := game.NewClient(
		*serverURL,
		*username,
		"", // No token for registration
		logger,
	)

	// Connect to server
	fmt.Printf("Connecting to %s...\n", *serverURL)
	if err := client.Connect(interruptCtx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Wait for connection ready
	select {
	case <-client.Ready():
		fmt.Println("Connected!")
	case <-interruptCtx.Done():
		log.Fatalf("Connection interrupted")
	case <-time.After(10 * time.Second):
		log.Fatalf("Connection timeout")
	}

	// Send register command
	fmt.Printf("Registering agent '%s' with empire '%s'...\n", *username, *empire)
	if err := client.Register(interruptCtx, *empire); err != nil {
		log.Fatalf("Registration failed: %v", err)
	}

	// Get the token from client state (updated by Register)
	receivedToken := client.GetState().Token
	if receivedToken == "" {
		log.Fatalf("Registration completed but no token received")
	}

	// Success!
	fmt.Println("\n✓ Registration successful!")
	fmt.Printf("\nAgent Details:\n")
	fmt.Printf("  Username: %s\n", *username)
	fmt.Printf("  Empire:   %s\n", *empire)
	fmt.Printf("  Token:    %s\n", receivedToken)

	// Save token if requested
	if *saveToken {
		if err := saveTokenFile(*username, receivedToken); err != nil {
			log.Printf("Warning: failed to save token: %v", err)
		} else {
			fmt.Printf("\nToken saved to ~/.spacemolt/agents/%s.token\n", *username)
		}
	}

	// Display next steps
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  To login:  use this token with agent client\n")
	fmt.Printf("  To play:   run: ./agent --username %s --token %s\n", *username, receivedToken)
}

func saveTokenFile(username, token string) error {
	// Create agents directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	agentsDir := fmt.Sprintf("%s/.spacemolt/agents", homeDir)
	if err := os.MkdirAll(agentsDir, 0700); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	// Write token file
	tokenFile := fmt.Sprintf("%s/%s.token", agentsDir, username)
	if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}
