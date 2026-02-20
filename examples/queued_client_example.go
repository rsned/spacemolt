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

// QueuedAgent demonstrates using the command queue for reliable sequential execution
// This is the recommended approach for agents that need guaranteed delivery
// and proper response matching.
type QueuedAgent struct {
	client *game.Client
	ctx    context.Context
	cancel context.CancelFunc
	logger *log.Logger
}

func NewQueuedAgent(wsURL, username, password string) *QueuedAgent {
	logger := log.New(os.Stdout, "[QUEUED-AGENT] ", log.LstdFlags)
	ctx, cancel := context.WithCancel(context.Background())

	client := game.NewClient(wsURL, username, password, logger)

	return &QueuedAgent{
		client: client,
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
	}
}

// Start connects and authenticates
func (a *QueuedAgent) Start() error {
	a.logger.Printf("Starting queued agent...")

	// Connect to the server
	if err := a.client.Connect(a.ctx); err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}

	// Wait for connection to be ready
	if err := a.client.WaitForReady(a.ctx, 10*time.Second); err != nil {
		return fmt.Errorf("connection not ready: %w", err)
	}

	// Login
	if err := a.client.Login(a.ctx); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	a.logger.Printf("Agent started successfully")
	return nil
}

// Stop gracefully shuts down
func (a *QueuedAgent) Stop() error {
	a.logger.Printf("Stopping agent...")
	a.cancel()

	// Stop the command queue
	if a.client.CmdQueue != nil {
		a.client.CmdQueue.Stop()
	}

	return a.client.Close()
}

// MineAndSellWorkflow demonstrates a complete workflow using queued commands
// All commands are executed sequentially with guaranteed delivery and response matching.
func (a *QueuedAgent) MineAndSellWorkflow() error {
	state := a.client.GetState()

	// Check if already docked
	if state.Doc {
		a.logger.Printf("Already docked, undocking...")
		if err := a.client.UndockQueued(a.ctx); err != nil {
			return fmt.Errorf("undock failed: %w", err)
		}
		a.logger.Printf("✓ Undocked successfully")
	}

	// Travel to mining location
	a.logger.Printf("Traveling to mining location...")
	if err := a.client.TravelQueued(a.ctx, "mining_asteroid_1"); err != nil {
		return fmt.Errorf("travel failed: %w", err)
	}
	a.logger.Printf("✓ Arrived at mining location")

	// Mine multiple times - each command waits for the previous to complete
	for i := 0; i < 5; i++ {
		a.logger.Printf("Mining attempt %d/5...", i+1)
		if err := a.client.MineQueued(a.ctx); err != nil {
			a.logger.Printf("✗ Mine attempt %d failed: %v", i+1, err)
			continue
		}
		a.logger.Printf("✓ Mining attempt %d successful", i+1)
		time.Sleep(5 * time.Second) // Wait between mining attempts
	}

	// Return to station
	a.logger.Printf("Returning to station...")
	if err := a.client.TravelQueued(a.ctx, "station_1"); err != nil {
		return fmt.Errorf("travel to station failed: %w", err)
	}
	a.logger.Printf("✓ Arrived at station")

	// Dock at station
	a.logger.Printf("Docking at station...")
	if err := a.client.DockQueued(a.ctx); err != nil {
		return fmt.Errorf("dock failed: %w", err)
	}
	a.logger.Printf("✓ Docked successfully")

	// Get market listings for pricing
	a.logger.Printf("Getting market listings...")
	if err := a.client.GetListingsQueued(a.ctx); err != nil {
		a.logger.Printf("Warning: Failed to get listings: %v", err)
	}

	// Sell all cargo
	a.logger.Printf("Selling all cargo...")
	if err := a.client.SellAllBulk(a.ctx, nil); err != nil {
		return fmt.Errorf("sell failed: %w", err)
	}
	a.logger.Printf("✓ Sold all cargo")

	// Refuel
	a.logger.Printf("Refueling...")
	if err := a.client.RefuelQueued(a.ctx); err != nil {
		a.logger.Printf("Warning: Refuel failed: %v", err)
	} else {
		a.logger.Printf("✓ Refueled successfully")
	}

	a.logger.Printf("✓ Mining and selling cycle complete!")
	return nil
}

// CustomCommandWorkflow demonstrates using SendQueued for custom commands
func (a *QueuedAgent) CustomCommandWorkflow() error {
	a.logger.Printf("Running custom command workflow...")

	// Example: Send a custom command and get the response
	resp, err := a.client.SendQueued(a.ctx, protocol.Message{
		Type:      "get_system",
		Timestamp: time.Now().UnixMilli(),
	}, 15*time.Second)

	if err != nil {
		return fmt.Errorf("custom command failed: %w", err)
	}

	a.logger.Printf("✓ Custom command response type: %s", resp.Type)

	// You can access the response payload
	if system, ok := resp.Payload["system"].(map[string]any); ok {
		if name, ok := system["name"].(string); ok {
			a.logger.Printf("Current system: %s", name)
		}
	}

	return nil
}

// MonitorQueueStatus demonstrates monitoring the command queue
func (a *QueuedAgent) MonitorQueueStatus() {
	if a.client.CmdQueue == nil {
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			queueSize := a.client.CmdQueue.QueueSize()
			activeCmd := a.client.CmdQueue.GetActiveCommand()

			if queueSize > 0 || activeCmd != nil {
				a.logger.Printf("Queue Status - Size: %d, Active: %v", queueSize, activeCmd != nil)
				if activeCmd != nil {
					a.logger.Printf("  Active Command: %s (waiting %v)",
						activeCmd.ID, time.Since(activeCmd.Timestamp))
				}
			}
		}
	}
}

// Example usage (commented out to avoid duplicate main function):
/*
func main() {
	agent := NewQueuedAgent(
		"wss://game.spacemolt.com/ws",
		"username",
		"password",
	)

	if err := agent.Start(); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}
	defer agent.Stop()

	// Start monitoring queue status in the background
	go agent.MonitorQueueStatus()

	// Run the mining and selling workflow
	if err := agent.MineAndSellWorkflow(); err != nil {
		log.Printf("Mining workflow failed: %v", err)
	}

	// Run custom commands
	if err := agent.CustomCommandWorkflow(); err != nil {
		log.Printf("Custom command failed: %v", err)
	}

	// Keep the agent running
	log.Println("Agent running, press Ctrl+C to stop")
	<-agent.ctx.Done()
}
*/
