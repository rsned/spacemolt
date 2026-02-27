// Command run-skill connects an agent to the game server and executes a
// named skill from data/skills/. Use it to test skill state machines with
// a real agent.
//
// Usage:
//
//	go run cmd/tools/run-skill/main.go <agent-id> <skill-name>
//
// Examples:
//
//	go run cmd/tools/run-skill/main.go miner-1 mine
//	go run cmd/tools/run-skill/main.go miner-1 refuel_repair
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/skills"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: run-skill <agent-id> <skill-name>")
		fmt.Fprintln(os.Stderr, "  e.g.: run-skill miner-1 mine")
		os.Exit(1)
	}

	agentID := os.Args[1]
	skillName := os.Args[2]

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	// Load skill registry
	registry, err := skills.LoadRegistry("data/skills")
	if err != nil {
		logger.Fatalf("Failed to load skills: %v", err)
	}

	if !registry.Has(skillName) {
		logger.Fatalf("Unknown skill %q. Available: %v", skillName, registry.Names())
	}

	logger.Printf("Loaded %d skills: %v", len(registry.Names()), registry.Names())

	// Set up context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("Received %v, shutting down...", sig)
		cancel()
	}()

	// Connect agent to game server
	logger.Printf("Initializing agent %s...", agentID)
	client, _, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		logger.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Brief pause for state to settle
	time.Sleep(game.SleepQuick)

	// Print starting state
	state := client.GetState()
	logger.Printf("═══ Starting State ═══")
	logger.Printf("System: %s | POI: %s | Docked: %v", state.System.Name, state.CurrentPOI, state.Doc)
	logger.Printf("Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f",
		state.Credits, state.Fuel, state.MaxFuel, state.Hull, state.MaxHull)
	logger.Printf("Cargo: %.1f/%.1f (%d items)",
		state.Ship.CargoUsed, state.Ship.CargoCapacity, len(state.Ship.Cargo))

	// Create dispatcher and executor
	dispatcher := skills.NewClientDispatcher(client)
	executor := skills.NewExecutor(registry, dispatcher, logger)

	// Run the skill
	logger.Printf("═══ Running skill: %s ═══", skillName)
	startTime := time.Now()

	if err := executor.Run(ctx, skillName); err != nil {
		logger.Printf("Skill %s failed: %v", skillName, err)
		os.Exit(1)
	}

	elapsed := time.Since(startTime)

	// Print ending state
	state = client.GetState()
	logger.Printf("═══ Skill Complete (%s) ═══", elapsed.Round(time.Second))
	logger.Printf("Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f",
		state.Credits, state.Fuel, state.MaxFuel, state.Hull, state.MaxHull)
	logger.Printf("Cargo: %.1f/%.1f (%d items)",
		state.Ship.CargoUsed, state.Ship.CargoCapacity, len(state.Ship.Cargo))
}
