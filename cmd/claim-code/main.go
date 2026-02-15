package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

const gameServerURL = "wss://game.spacemolt.com/ws"

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Empire   string `json:"empire"`
}

func loadCredentials(agentDir string) (*Credentials, error) {
	data, err := os.ReadFile(filepath.Join(agentDir, "credentials.json"))
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

type SimpleHandler struct {
	client *game.Client
	logger *log.Logger
}

func (h *SimpleHandler) OnConnected(state *game.State) {
	h.logger.Printf("✓ Connected!")
}

func (h *SimpleHandler) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.logger.Printf("✓ %s", msg)
		}
		// Log full response for debugging
		data, _ := json.MarshalIndent(resp, "", "  ")
		h.logger.Printf("Response:\n%s", string(data))
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.logger.Printf("✗ Error: %s", msg)
		}
		// Log full error response
		data, _ := json.MarshalIndent(resp, "", "  ")
		h.logger.Printf("Error Response:\n%s", string(data))
	default:
		// Log any other response type
		data, _ := json.MarshalIndent(resp, "", "  ")
		h.logger.Printf("Response [%s]:\n%s", resp.Type, string(data))
	}
}

func (h *SimpleHandler) OnDisconnected(err error) {
	if err != nil {
		h.logger.Printf("Disconnected: %v", err)
	} else {
		h.logger.Printf("Disconnected cleanly")
	}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: claim-code <agent-name> <registration-code>")
		fmt.Println("Example: claim-code pirate-4 379e45614d2a4098fdfb8461b49abad7")
		os.Exit(1)
	}

	agentName := os.Args[1]
	registrationCode := os.Args[2]
	agentDir := fmt.Sprintf("data/agents/%s", agentName)

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentName), log.LstdFlags)

	// Load credentials
	creds, err := loadCredentials(agentDir)
	if err != nil {
		log.Fatalf("Failed to load credentials: %v", err)
	}

	logger.Printf("Claiming registration code for %s (%s)...", creds.Username, creds.Empire)

	// Create game client
	gameLogger := log.New(os.Stdout, "[GAME] ", log.LstdFlags)
	client := game.NewClient(gameServerURL, creds.Username, creds.Password, gameLogger)

	// Set up handler
	handler := &SimpleHandler{client: client, logger: logger}
	client.SetHandler(handler)

	// Connect to game
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for connection to be ready
	<-client.Ready()
	time.Sleep(1 * time.Second)

	// Login
	logger.Printf("Logging in...")
	if err := client.Login(ctx); err != nil {
		log.Fatalf("Failed to login: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Claim the registration code
	logger.Printf("Claiming registration code: %s", registrationCode)
	if err := client.Claim(ctx, registrationCode); err != nil {
		logger.Printf("❌ Claim failed: %v", err)
		os.Exit(1)
	}

	logger.Printf("✅ Successfully claimed registration code!")
	logger.Printf("Your agent is now linked to your SpaceMolt account.")

	// Wait a moment for any additional messages
	time.Sleep(3 * time.Second)
}
