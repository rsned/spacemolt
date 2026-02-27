// Command run-skill connects an agent to the game server and executes one or
// more named skills from data/skills/ in sequence.
//
// Usage:
//
//	go run cmd/tools/run-skill/main.go <agent-id> <skill> [skill...]
//
// Examples:
//
//	go run cmd/tools/run-skill/main.go miner-1 mine
//	go run cmd/tools/run-skill/main.go miner-1 mine sell refuel_repair
//	go run cmd/tools/run-skill/main.go miner-1 mine craft_items deposit_cargo refuel_repair
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/skills"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: run-skill <agent-id> <skill> [skill...]")
		fmt.Fprintln(os.Stderr, "  e.g.: run-skill miner-1 mine sell refuel_repair")
		os.Exit(1)
	}

	agentID := os.Args[1]
	skillNames := os.Args[2:]

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	// Load skill registry
	registry, err := skills.LoadRegistry("data/skills")
	if err != nil {
		logger.Fatalf("Failed to load skills: %v", err)
	}

	// Validate all skill names upfront
	for _, name := range skillNames {
		if !registry.Has(name) {
			logger.Fatalf("Unknown skill %q. Available: %v", name, registry.Names())
		}
	}

	logger.Printf("Loaded %d skills: %v", len(registry.Names()), registry.Names())
	logger.Printf("Chain: %s", strings.Join(skillNames, " → "))

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

	// Create dispatcher and executor
	dispatcher := skills.NewClientDispatcher(client, logger)
	dispatcher.EnsureSystemData(ctx)
	executor := skills.NewExecutor(registry, dispatcher, logger)

	// Print starting state
	printState(logger, "Starting State", client.GetState())

	// Run skills in sequence
	chainStart := time.Now()

	for i, skillName := range skillNames {
		if ctx.Err() != nil {
			logger.Printf("Chain interrupted")
			break
		}

		logger.Printf("═══ Chain [%d/%d]: %s ═══", i+1, len(skillNames), skillName)
		skillStart := time.Now()

		if err := executor.Run(ctx, skillName); err != nil {
			logger.Printf("Skill %s failed: %v", skillName, err)
			printState(logger, "State at failure", client.GetState())
			os.Exit(1)
		}

		logger.Printf("═══ %s done (%s) ═══", skillName, time.Since(skillStart).Round(time.Second))
	}

	// Print ending state
	printState(logger, fmt.Sprintf("Chain Complete (%s)", time.Since(chainStart).Round(time.Second)), client.GetState())
}

func printState(logger *log.Logger, header string, state *game.State) {
	logger.Printf("═══ %s ═══", header)
	logger.Printf("System: %s | POI: %s | Docked: %v", state.System.Name, state.CurrentPOI, state.Doc)
	logger.Printf("Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f",
		state.Credits, state.Fuel, state.MaxFuel, state.Hull, state.MaxHull)
	logger.Printf("Cargo: %.1f/%.1f (%d items)",
		state.Ship.CargoUsed, state.Ship.CargoCapacity, len(state.Ship.Cargo))
}
