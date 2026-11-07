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
	serverURL        = flag.String("server", "wss://game.spacemolt.com/ws", "WebSocket server URL")
	username         = flag.String("username", "", "Agent username (required)")
	empire           = flag.String("empire", "solarian", "Empire/faction name")
	registrationCode = flag.String("code", "", "Registration code from https://spacemolt.com/dashboard (required for linking to website account)")
	savePassword     = flag.Bool("save-password", false, "Save password to ~/.spacemolt/agents/<username>.password")
	verbose          = flag.Bool("v", false, "Enable verbose output")
	transport        = flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
)

func main() {
	flag.Parse()

	if *transport != "ws" {
		fmt.Fprintln(os.Stderr, "Error: register-agent only supports --transport=ws (registration requires WebSocket)")
		os.Exit(1)
	}

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
		"", // No password for registration
		logger,
	)

	// Progress goes to stderr so stdout is only the credentials JSON, ready to
	// redirect into data/agents/<id>/credentials.json.
	fmt.Fprintf(os.Stderr, "Connecting to %s...\n", *serverURL)
	if err := client.Connect(interruptCtx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Wait for connection ready
	select {
	case <-client.Ready():
		fmt.Fprintln(os.Stderr, "Connected!")
	case <-interruptCtx.Done():
		log.Fatalf("Connection interrupted")
	case <-time.After(10 * time.Second):
		log.Fatalf("Connection timeout")
	}

	// Send register command
	fmt.Fprintf(os.Stderr, "Registering agent '%s' with empire '%s'...\n", *username, *empire)
	if *registrationCode != "" {
		fmt.Fprintf(os.Stderr, "Using registration code: %s\n", *registrationCode)
	}
	if err := client.Register(interruptCtx, *empire, *registrationCode); err != nil {
		log.Fatalf("Registration failed: %v", err)
	}

	// Get the password from client state (updated by Register)
	state := client.GetState()
	receivedPassword := state.Password
	if receivedPassword == "" {
		log.Fatalf("Registration completed but no password received")
	}

	out, err := credentialsJSON(*username, receivedPassword, *empire, state.Player.ID)
	if err != nil {
		log.Fatalf("Encode credentials: %v", err)
	}
	fmt.Fprintln(os.Stderr, "Registration successful.")
	fmt.Println(string(out))

	// Save password if requested
	if *savePassword {
		if err := savePasswordFile(*username, receivedPassword); err != nil {
			log.Printf("Warning: failed to save password: %v", err)
		} else {
			fmt.Fprintf(os.Stderr, "Password saved to ~/.spacemolt/agents/%s.password\n", *username)
		}
	}
}

func savePasswordFile(username, password string) error {
	// Create agents directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	agentsDir := fmt.Sprintf("%s/.spacemolt/agents", homeDir)
	if err := os.MkdirAll(agentsDir, 0700); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	// Write password file
	passwordFile := fmt.Sprintf("%s/%s.password", agentsDir, username)
	if err := os.WriteFile(passwordFile, []byte(password), 0600); err != nil {
		return fmt.Errorf("failed to write password file: %w", err)
	}

	return nil
}
