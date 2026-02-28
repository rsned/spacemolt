package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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

type GameHandler struct {
	client  *game.Client
	logger  *log.Logger
	gotState bool
}

func (h *GameHandler) OnConnected(state *game.State) {
	h.logger.Printf("✓ Connected! Credits: %.0f, System: %s, Docked: %v",
		state.Credits, state.CurrentSystem, state.Doc)
	h.gotState = true
}

func (h *GameHandler) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeStateUpdate:
		h.gotState = true
		return // Don't spam
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.logger.Printf("✓ %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.logger.Printf("✗ %s", msg)
		}
	default:
		data, _ := json.MarshalIndent(resp.Payload, "", "  ")
		h.logger.Printf("[%s]:\n%s", resp.Type, string(data))
	}
}

func (h *GameHandler) OnDisconnected(err error) {
	if err != nil {
		h.logger.Printf("Disconnected: %v", err)
	}
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: play-simple <agent-id>")
		fmt.Println("Example: play-simple pirate-4")
		os.Exit(1)
	}

	agentID := flag.Args()[0]
	agentDir := fmt.Sprintf("data/agents/%s", agentID)

	// Load credentials
	credPath := filepath.Join(agentDir, "credentials.json")
	credData, err := os.ReadFile(credPath)
	if err != nil {
		log.Fatalf("Failed to load credentials: %v", err)
	}

	var creds Credentials
	if err := json.Unmarshal(credData, &creds); err != nil {
		log.Fatalf("Failed to parse credentials: %v", err)
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)
	logger.Printf("🏴‍☠️ Connecting as %s...", creds.Username)

	// Create game client
	gameLogger := log.New(os.Stdout, "", 0)
	client := game.NewClient(gameServerURL, creds.Username, creds.Password, gameLogger)
	client.SetDebugLogging(*debug)

	// Set up handler
	handler := &GameHandler{client: client, logger: logger}
	client.SetHandler(handler)

	// Connect
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = client.Close() }()

	<-client.Ready()
	time.Sleep(1 * time.Second)

	// Login
	if err := client.Login(ctx); err != nil {
		log.Fatalf("Failed to login: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Show status
	state := client.GetState()
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Printf("☣ CRIMSON CORSAIRS ENFORCER\n")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("💰 Credits: %.0f\n", state.Credits)
	fmt.Printf("🌍 System: %s\n", state.CurrentSystem)
	fmt.Printf("📍 POI: %s\n", state.CurrentPOI)
	fmt.Printf("🚢 Ship: %s\n", state.Ship.Name)
	fmt.Printf("⛽ Fuel: %.0f/%.0f\n", state.Fuel, state.MaxFuel)
	fmt.Printf("🛡️  Hull: %.0f/%.0f\n", state.Hull, state.MaxHull)
	cargoUsed := 0
	for _, item := range state.Cargo {
		if qty, ok := item["quantity"].(float64); ok {
			cargoUsed += int(qty)
		}
	}
	fmt.Printf("📦 Cargo: %d/%d\n", cargoUsed, state.MaxCargo)
	fmt.Printf("⚓ Docked: %v\n", state.Doc)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()

	// Interactive command loop
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Enter commands (or 'help', 'status', 'quit'):")
	fmt.Print("> ")

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}

		if line == "quit" || line == "exit" {
			break
		}

		if line == "status" {
			state := client.GetState()
			fmt.Printf("System: %s, POI: %s, Credits: %.0f, Fuel: %.0f/%.0f, Docked: %v\n",
				state.CurrentSystem, state.CurrentPOI, state.Credits, state.Fuel, state.MaxFuel, state.Doc)
			fmt.Print("> ")
			continue
		}

		// Parse command
		parts := strings.Fields(line)
		if len(parts) == 0 {
			fmt.Print("> ")
			continue
		}

		cmdType := parts[0]
		payload := make(map[string]any)

		// Parse simple arguments
		for i := 1; i < len(parts); i++ {
			kv := strings.SplitN(parts[i], "=", 2)
			if len(kv) == 2 {
				payload[kv[0]] = kv[1]
			}
		}

		// Send command
		msg := protocol.Message{
			Type:    cmdType,
			Payload: payload,
		}

		if err := client.Send(ctx, msg); err != nil {
			logger.Printf("Error sending command: %v", err)
		}

		time.Sleep(500 * time.Millisecond)
		fmt.Print("> ")
	}

	logger.Println("Goodbye!")
}
