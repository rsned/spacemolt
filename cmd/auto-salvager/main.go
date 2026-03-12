package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func updateCaptainsLog(agentID string, client game.GameClient) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("Credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	if len(state.Ship.Cargo) > 0 {
		notes = append(notes, fmt.Sprintf("Cargo: %d items (%.0f/%.0f)", len(state.Ship.Cargo), state.Ship.CargoUsed, state.Ship.CargoCapacity))
	}

	currentGoal := "Awaiting implementation of salvaging logic - currently monitoring for wrecks"
	if state.Doc {
		currentGoal = "Docked at station, preparing to sell salvaged materials"
	}

	entry := &game.AgentLog{
		AgentName:   state.Player.Username,
		CurrentGoal: currentGoal,
		Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
		Notes:       notes,
	}

	if err := game.WriteCaptainsLog(agentID, entry); err != nil {
		// Log error but don't fail - captain's log is not critical
		_ = err
	}
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: auto-salvager [flags] <agent-id>")
		fmt.Println("This tool controls a salvaging agent that finds and salvages wrecks")
		fmt.Println("NOTE: This agent is currently simplified and needs salvaging logic implemented")
		fmt.Println("")
		fmt.Println("Flags:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Example:")
		fmt.Println("  auto-salvager salvager-1              # Use WebSocket transport")
		fmt.Println("  auto-salvager -transport=mcp salvager-1 # Use MCP transport")
		os.Exit(1)
	}

	agentID := flag.Args()[0]

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	// Check captain's log for previous mission
	previousLog, err := game.ReadLatestCaptainsLog(agentID)
	if err != nil {
		logger.Printf("Failed to read captain's log: %v", err)
	} else if previousLog != nil {
		logger.Printf("📖 Captain's Log - Last Entry:")
		logger.Printf("   Mission: %s", previousLog.CurrentGoal)
		logger.Printf("   Location: %s", previousLog.Location)
		logger.Printf("   Time: %s", previousLog.Timestamp.Format("2006-01-02 15:04"))
	}

	ctx := context.Background()

	// Initialize game client based on transport selection
	var client game.GameClient
	var creds *game.Credentials

	switch *transport {
	case "mcp":
		logger.Printf("Using MCP transport")
		client, creds, err = game.InitializeMCPAgent(agentID, logger, ctx, *debug, false)
		if err != nil {
			log.Fatalf("Failed to initialize MCP agent: %v", err)
		}
	case "ws":
		logger.Printf("Using WebSocket transport")
		client, creds, err = game.InitializeAgent(agentID, logger, ctx, *debug)
		if err != nil {
			log.Fatalf("Failed to initialize agent: %v", err)
		}
	default:
		log.Fatalf("Unknown transport: %s (must be: ws, mcp)", *transport)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	state := client.GetState()
	logger.Printf("Ready! Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Empire, state.Credits, state.Ship.Name, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	logger.Printf("Salvager agent started - awaiting implementation of wreck salvaging logic")
	logger.Printf("Currently in simple monitoring mode")

	// Update captain's log on startup
	updateCaptainsLog(agentID, client)

	// Periodic captain's log updates
	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	statusTicker := time.NewTicker(10 * time.Second)
	defer statusTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-logTicker.C:
			updateCaptainsLog(agentID, client)
		case <-statusTicker.C:
			state := client.GetState()
			logger.Printf("Status: Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Docked: %v | Location: %s",
				state.Credits, state.Fuel, state.MaxFuel, state.Hull,
				state.MaxHull, state.Doc, state.System.Name)
		}
	}
}
