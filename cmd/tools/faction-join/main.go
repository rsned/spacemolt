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

var debug = flag.Bool("debug", false, "Enable debug logging")

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Empire   string `json:"empire"`
}

func loadCredentials(pirateNum int) (*Credentials, error) {
	credPath := filepath.Join("data", "agents", fmt.Sprintf("pirate-%d", pirateNum), "credentials.json")
	data, err := os.ReadFile(credPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}
	return &creds, nil
}

func connectPirate(pirateNum int) (*game.Client, context.Context, error) {
	creds, err := loadCredentials(pirateNum)
	if err != nil {
		return nil, nil, err
	}

	ctx := context.Background()
	logger := log.New(os.Stdout, fmt.Sprintf("[pirate-%d] ", pirateNum), log.LstdFlags)

	client := game.NewClient(gameServerURL, creds.Username, creds.Password, logger)
	client.SetDebugLogging(*debug)

	if err := client.Connect(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to connect: %w", err)
	}

	if err := client.Login(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to login: %w", err)
	}

	time.Sleep(1 * time.Second)
	return client, ctx, nil
}

func main() {
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: faction-join <command>")
		fmt.Println("Commands:")
		fmt.Println("  create        - Create Crimson Corsairs faction with pirate-1")
		fmt.Println("  invite <num>  - Pirate-1 invites pirate-<num>")
		fmt.Println("  join <num>    - Pirate-<num> accepts invitation")
		fmt.Println("  info <num>    - Show faction info for pirate-<num>")
		os.Exit(1)
	}

	command := flag.Args()[0]

	switch command {
	case "create":
		client, ctx, err := connectPirate(1)
		if err != nil {
			log.Fatalf("Failed to connect pirate-1: %v", err)
		}
		defer func() { _ = client.Disconnect() }()

		msg := protocol.Message{
			Type: "create_faction",
			Payload: map[string]any{
				"name": "Crimson Corsairs",
				"tag":  "CRCO",
			},
			Timestamp: time.Now().UnixMilli(),
		}

		if err := client.Send(ctx, msg); err != nil {
			log.Fatalf("Failed to create faction: %v", err)
		}

		time.Sleep(2 * time.Second)
		fmt.Println("✓ Faction creation command sent")

	case "info":
		if len(flag.Args()) < 2 {
			log.Fatal("Usage: faction-join info <pirate-num>")
		}
		pirateNum := 0
		_, _ = fmt.Sscanf(flag.Args()[1], "%d", &pirateNum)

		client, ctx, err := connectPirate(pirateNum)
		if err != nil {
			log.Fatalf("Failed to connect pirate-%d: %v", pirateNum, err)
		}
		defer func() { _ = client.Disconnect() }()

		msg := protocol.Message{
			Type:      "faction_info",
			Timestamp: time.Now().UnixMilli(),
		}

		if err := client.Send(ctx, msg); err != nil {
			log.Fatalf("Failed to get faction info: %v", err)
		}

		time.Sleep(2 * time.Second)
		state := client.GetState()
		fmt.Printf("Faction ID: %s\n", state.Player.FactionID)
		fmt.Printf("Faction Rank: %s\n", state.Player.FactionRank)

	case "invite":
		if len(flag.Args()) < 2 {
			log.Fatal("Usage: faction-join invite <pirate-num>")
		}
		targetNum := 0
		_, _ = fmt.Sscanf(flag.Args()[1], "%d", &targetNum)

		// Get target pirate's username
		targetCreds, err := loadCredentials(targetNum)
		if err != nil {
			log.Fatalf("Failed to load target credentials: %v", err)
		}

		client, ctx, err := connectPirate(1)
		if err != nil {
			log.Fatalf("Failed to connect pirate-1: %v", err)
		}
		defer func() { _ = client.Disconnect() }()

		msg := protocol.Message{
			Type: "faction_invite",
			Payload: map[string]any{
				"player_id": targetCreds.Username,
			},
			Timestamp: time.Now().UnixMilli(),
		}

		if err := client.Send(ctx, msg); err != nil {
			log.Fatalf("Failed to send invite: %v", err)
		}

		time.Sleep(2 * time.Second)
		fmt.Printf("✓ Invitation sent to %s\n", targetCreds.Username)

	case "join":
		if len(flag.Args()) < 2 {
			log.Fatal("Usage: faction-join join <pirate-num>")
		}
		pirateNum := 0
		_, _ = fmt.Sscanf(flag.Args()[1], "%d", &pirateNum)

		client, ctx, err := connectPirate(pirateNum)
		if err != nil {
			log.Fatalf("Failed to connect pirate-%d: %v", pirateNum, err)
		}
		defer func() { _ = client.Disconnect() }()

		// First get invites
		getInvitesMsg := protocol.Message{
			Type:      "faction_get_invites",
			Timestamp: time.Now().UnixMilli(),
		}

		if err := client.Send(ctx, getInvitesMsg); err != nil {
			log.Fatalf("Failed to get invites: %v", err)
		}

		time.Sleep(2 * time.Second)

		// For now, we'll need to manually specify the faction ID
		// In a real implementation, we'd parse the invites response
		fmt.Println("Please check invites and manually join with faction_id")
		fmt.Println("To join, send: {\"type\": \"join_faction\", \"payload\": {\"faction_id\": \"<id>\"}}")

	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}
