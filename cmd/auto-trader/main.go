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

type TraderAgent struct {
	client *game.Client
	logger *log.Logger
	ctx    context.Context
}

func (t *TraderAgent) OnConnected(state *game.State) {
	t.logger.Printf("Connected! Credits: %.2f", state.Credits)
}

func (t *TraderAgent) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			t.logger.Printf("OK: %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			t.logger.Printf("ERROR: %s", msg)
		}
	}
}

func (t *TraderAgent) OnDisconnected(err error) {
	t.logger.Printf("Disconnected: %v", err)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: auto-trader <agent-id>")
		fmt.Println("This tool controls a trading agent that finds profitable trade routes")
		fmt.Println("NOTE: This agent is currently simplified and needs trading logic implemented")
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

	logger.Printf("Trader agent started - awaiting implementation of trading logic")
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
