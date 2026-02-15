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

const (
	// Pirating thresholds and priorities
	TARGET_SHIP_CLASS   = "fighter" // Preferred target ship class
	MAX_COMBAT_DISTANCE = 15        // Maximum distance to engage targets (AU)
	MIN_COMBAT_TICKS    = 20        // Minimum ticks in combat before disengaging
	SAFE_HULL_PERCENT   = 0.3       // Disengage if hull drops below 30%
	RESERVE_CREDITS     = 100.0     // Reserve for repairs and refueling
)

type PirateAgent struct {
	client *game.Client
	logger *log.Logger
	ctx    context.Context
}

func (p *PirateAgent) OnConnected(state *game.State) {
	p.logger.Printf("Connected! Credits: %.2f", state.Credits)
}

func (p *PirateAgent) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			p.logger.Printf("OK: %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			p.logger.Printf("ERROR: %s", msg)
		}
	}
}

func (p *PirateAgent) OnDisconnected(err error) {
	p.logger.Printf("Disconnected: %v", err)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: auto-pirate <agent-id>")
		fmt.Println("This tool controls a pirate agent that hunts NPCs and players")
		fmt.Println("NOTE: This agent is currently simplified and needs combat logic implemented")
		fmt.Println()
		os.Exit(1)
	}

	agentID := os.Args[1]

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	ctx := context.Background()

	client, creds, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer client.Close()

	time.Sleep(1 * time.Second)

	state := client.GetState()
	logger.Printf("Ready! Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Empire, state.Credits, state.Ship.Name, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	logger.Printf("Pirate agent started - awaiting implementation of combat logic")
	logger.Printf("Currently in simple monitoring mode")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		state := client.GetState()
		logger.Printf("Status: Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Docked: %v | Location: %s",
			state.Credits, state.Fuel, state.MaxFuel, state.Hull,
			state.MaxHull, state.Doc, state.System.Name)

		time.Sleep(10 * time.Second)
	}
}
