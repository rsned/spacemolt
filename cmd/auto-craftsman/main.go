package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

type CraftsmanAgent struct {
	logger *log.Logger
}

func (c *CraftsmanAgent) OnConnected(state *game.State) {
	c.logger.Printf("Connected! Credits: %.2f", state.Credits)
}

func (c *CraftsmanAgent) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			c.logger.Printf("OK: %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			c.logger.Printf("ERROR: %s", msg)
		}
	}
}

func (c *CraftsmanAgent) OnDisconnected(err error) {
	c.logger.Printf("Disconnected: %v", err)
}

func updateCaptainsLog(agentID string, client *game.Client) {
	state := client.GetState()

	var notes []string
	notes = append(notes, fmt.Sprintf("Credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	if len(state.Ship.Cargo) > 0 {
		notes = append(notes, fmt.Sprintf("Cargo: %d items (%.0f/%.0f)", len(state.Ship.Cargo), state.Ship.CargoUsed, state.Ship.CargoCapacity))
	}

	currentGoal := "Awaiting implementation of crafting logic - monitoring for opportunities"
	if state.Doc {
		currentGoal = "Docked at station, ready to craft items when logic is implemented"
	}

	entry := &game.AgentLog{
		AgentName:   state.Player.Username,
		CurrentGoal: currentGoal,
		Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
		Notes:       notes,
	}

	game.WriteCaptainsLog(agentID, entry)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: auto-craftsman <agent-id>")
		fmt.Println("This tool controls a crafting agent that buys materials, crafts items, and sells them")
		fmt.Println("NOTE: This agent is currently simplified and needs recipe/crafting logic implemented")
		fmt.Println()
		os.Exit(1)
	}

	agentID := os.Args[1]

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

	client, creds, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
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

	logger.Printf("Crafting agent started - awaiting implementation of recipe/crafting logic")
	logger.Printf("Currently in simple monitoring mode")

	// Update captain's log on startup
	updateCaptainsLog(agentID, client)

	// Periodic captain's log updates
	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	statusTicker := time.NewTicker(10 * time.Second)
	defer statusTicker.Stop()

	// Simple monitoring loop
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
