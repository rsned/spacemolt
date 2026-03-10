package main

import (
	"context"
	"encoding/json"
	"flag"
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
}

func loadCredentials(agentDir string) (*Credentials, error) {
	data, err := os.ReadFile(filepath.Join(agentDir, "credentials.json"))
	if err != nil {
		return nil, err
	}
	var creds Credentials
	return &creds, json.Unmarshal(data, &creds)
}

type Handler struct {
	gotResult bool
}

func (h *Handler) OnConnected(state *game.State) {}
func (h *Handler) OnMessage(resp protocol.Response) {
	if resp.Type == "action_result" || resp.Type == "ok" {
		if action, ok := resp.Payload["action"].(string); ok && action == "types" {
			h.gotResult = true
			data, _ := json.MarshalIndent(resp.Payload, "", "  ")
			fmt.Println(string(data))
		} else if _, ok := resp.Payload["types"]; ok {
			h.gotResult = true
			data, _ := json.MarshalIndent(resp.Payload, "", "  ")
			fmt.Println(string(data))
		}
	}
}
func (h *Handler) OnDisconnected(err error) {}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	flag.Parse()

	if *transport != "ws" {
		fmt.Println("Error: facility-check only supports --transport=ws (facility protocol requires WebSocket)")
		os.Exit(1)
	}

	agentDir := "data/agents/pirate-4"
	creds, _ := loadCredentials(agentDir)

	client := game.NewClient(gameServerURL, creds.Username, creds.Password, log.New(os.Stderr, "", 0))
	client.SetDebugLogging(*debug)
	handler := &Handler{}
	client.SetHandler(handler)

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	<-client.Ready()
	time.Sleep(1 * time.Second)

	if err := client.Login(ctx); err != nil {
		log.Fatalf("Failed to login: %v", err)
	}
	time.Sleep(2 * time.Second)

	// Request personal_workbench details
	if err := client.Send(ctx, protocol.Message{
		Type: "facility",
		Payload: map[string]any{
			"action":        "types",
			"facility_type": "personal_workbench",
		},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		log.Fatalf("Failed to send facility request: %v", err)
	}

	// Wait for response
	for i := 0; i < 50 && !handler.gotResult; i++ {
		time.Sleep(200 * time.Millisecond)
	}

	if err := client.Close(); err != nil {
		log.Printf("Warning: Failed to close client: %v", err)
	}
}
