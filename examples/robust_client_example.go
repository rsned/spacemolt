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

// ExampleAgent demonstrates how to use the robust WebSocket client
// in an agent that needs to execute multiple commands reliably.
type ExampleAgent struct {
	client    *game.Client
	builder   *game.SafeCommandBuilder
	executor  *game.RobustCommandExecutor
	ctx       context.Context
	cancel    context.CancelFunc
	logger    *log.Logger
}

func NewExampleAgent(wsURL, username, password string) *ExampleAgent {
	logger := log.New(os.Stdout, "[AGENT] ", log.LstdFlags)
	ctx, cancel := context.WithCancel(context.Background())

	client := game.NewClient(wsURL, username, password, logger)

	return &ExampleAgent{
		client:   client,
		builder:  game.NewSafeCommandBuilder(client),
		executor: game.NewRobustCommandExecutor(client),
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
	}
}

// Start connects to the server and starts the agent
func (a *ExampleAgent) Start() error {
	a.logger.Printf("Starting agent...")

	// Connect to the server
	if err := a.client.Connect(a.ctx); err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}

	// Wait for connection to be ready
	if err := a.client.WaitForReady(a.ctx, 10*time.Second); err != nil {
		return fmt.Errorf("connection not ready: %w", err)
	}

	// Set up message handler
	handler := game.NewReconnectingHandler(a.client, a, a.ctx, a.logger)
	a.client.SetHandler(handler)

	// Login (or register if needed)
	if err := a.client.Login(a.ctx); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	a.logger.Printf("Agent started successfully")
	return nil
}

// Stop gracefully shuts down the agent
func (a *ExampleAgent) Stop() error {
	a.logger.Printf("Stopping agent...")
	a.cancel()
	return a.client.Close()
}

// OnConnected is called when the client connects
func (a *ExampleAgent) OnConnected(state *game.State) {
	a.logger.Printf("Connected to server")
}

// OnMessage is called when a message is received
func (a *ExampleAgent) OnMessage(resp protocol.Response) {
	// Handle specific message types if needed
	switch resp.Type {
	case protocol.TypeDocked:
		a.logger.Printf("Docked at station")
	case protocol.TypeUndocked:
		a.logger.Printf("Undocked from station")
	}
}

// OnDisconnected is called when the client disconnects
func (a *ExampleAgent) OnDisconnected(err error) {
	a.logger.Printf("Disconnected: %v", err)
}

// MineAndSellExample demonstrates a complete mining and selling workflow
// using the queued command system for reliable sequential execution.
// This is the recommended approach for agents that need guaranteed delivery
// and proper command sequencing.
func (a *ExampleAgent) MineAndSellExample() error {
	state := a.client.GetState()

	// Check if already docked
	if state.Doc {
		a.logger.Printf("Already docked, undocking...")
		if err := a.builder.UndockQueued(a.ctx); err != nil {
			return fmt.Errorf("undock failed: %w", err)
		}
	}

	// Travel to mining location
	a.logger.Printf("Traveling to mining location...")
	if err := a.builder.TravelQueued(a.ctx, "mining_asteroid_1"); err != nil {
		return fmt.Errorf("travel failed: %w", err)
	}

	// Mine multiple times
	for i := 0; i < 5; i++ {
		a.logger.Printf("Mining attempt %d/5...", i+1)
		if err := a.builder.MineQueued(a.ctx); err != nil {
			a.logger.Printf("Mine attempt %d failed: %v", i+1, err)
			continue
		}
		time.Sleep(5 * time.Second) // Wait between mining attempts
	}

	// Return to station
	a.logger.Printf("Returning to station...")
	if err := a.builder.TravelQueued(a.ctx, "station_1"); err != nil {
		return fmt.Errorf("travel to station failed: %w", err)
	}

	// Dock at station
	a.logger.Printf("Docking at station...")
	if err := a.builder.DockQueued(a.ctx); err != nil {
		return fmt.Errorf("dock failed: %w", err)
	}

	// Get market listings for pricing
	a.logger.Printf("Getting market listings...")
	if err := a.builder.GetListingsQueued(a.ctx); err != nil {
		a.logger.Printf("Warning: Failed to get listings: %v", err)
	}

	// Sell all cargo
	a.logger.Printf("Selling all cargo...")
	if err := a.builder.SellAllBulk(a.ctx, nil); err != nil {
		return fmt.Errorf("sell failed: %w", err)
	}

	// Refuel
	a.logger.Printf("Refueling...")
	if err := a.builder.RefuelQueued(a.ctx); err != nil {
		a.logger.Printf("Warning: Refuel failed: %v", err)
	}

	a.logger.Printf("Mining and selling cycle complete!")
	return nil
}

// CustomCommandExample demonstrates using the executor for custom commands
func (a *ExampleAgent) CustomCommandExample() error {
	// Use the executor for custom commands that need retry logic
	err := a.executor.ExecuteCommand(a.ctx, func(ctx context.Context) error {
		// This could be any custom command sequence
		state := a.client.GetState()

		// Example: Check if we need to repair
		if state.Hull < state.MaxHull*0.5 {
			a.logger.Printf("Hull at %.0f/%.0f, repairing...", state.Hull, state.MaxHull)
			return a.client.Repair(ctx)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("custom command failed: %w", err)
	}

	return nil
}

// DirectClientExample shows the old way (still works, but less robust)
func (a *ExampleAgent) DirectClientExample() error {
	// Old way - directly calling client methods
	// This is less robust because it doesn't handle connection failures
	// Use SafeCommandBuilder instead for better reliability

	state := a.client.GetState()
	if !state.Doc {
		if err := a.client.Dock(a.ctx); err != nil {
			return fmt.Errorf("dock failed: %w", err)
		}
	}

	// Rest of the logic...
	return nil
}

func main() {
	// Example usage
	agent := NewExampleAgent(
		"wss://game.spacemolt.com/ws",
		"username",
		"password",
	)

	if err := agent.Start(); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}
	defer agent.Stop()

	// Run the mining and selling workflow
	if err := agent.MineAndSellExample(); err != nil {
		log.Printf("Mining workflow failed: %v", err)
	}

	// Run custom commands
	if err := agent.CustomCommandExample(); err != nil {
		log.Printf("Custom command failed: %v", err)
	}

	// Keep the agent running
	log.Println("Agent running, press Ctrl+C to stop")
	<-agent.ctx.Done()
}