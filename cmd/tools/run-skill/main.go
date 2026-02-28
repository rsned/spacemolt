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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/skills"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if len(flag.Args()) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: run-skill [flags] <agent-id> <skill> [skill...]")
		fmt.Fprintln(os.Stderr, "  e.g.: run-skill miner-1 mine sell refuel_repair")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	agentID := flag.Args()[0]
	skillNames := flag.Args()[1:]

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
	client, _, err := game.InitializeAgent(agentID, logger, ctx, *debug)
	if err != nil {
		logger.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Brief pause for state to settle
	time.Sleep(game.SleepQuick)

	// Create dispatcher and executor
	dispatcher := skills.NewClientDispatcher(client, "data", agentID, logger)
	dispatcher.EnsureSystemData(ctx)

	// Load agent personality to extract skill params (e.g. service_name)
	agentParams := make(map[string]string)
	personalityPath := filepath.Join("data", "agents", agentID, "personality.json")
	if data, err := os.ReadFile(personalityPath); err == nil {
		var p agent.Personality
		if err := json.Unmarshal(data, &p); err == nil && p.ServiceName != "" {
			agentParams["service_name"] = p.ServiceName
			logger.Printf("Loaded service_name: %s", p.ServiceName)
		}
	}

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

		if err := executor.RunWithParams(ctx, skillName, agentParams); err != nil {
			logger.Printf("Skill %s failed: %v", skillName, err)
			continue
		}

		logger.Printf("✓ Skill %s completed in %s", skillName, time.Since(skillStart))
		printState(logger, fmt.Sprintf("After %s", skillName), client.GetState())
	}

	logger.Printf("═══ Chain completed in %s ═══", time.Since(chainStart))
	printState(logger, "Final State", client.GetState())
}

func printState(logger *log.Logger, label string, state *game.State) {
	logger.Printf("─── %s ───", label)
	logger.Printf("  System: %s (%s)", state.System.Name, state.System.ID)
	logger.Printf("  POI: %s", state.CurrentPOI)
	logger.Printf("  Credits: %.0f", state.Credits)
	logger.Printf("  Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel)
	logger.Printf("  Hull: %.0f/%.0f", state.Hull, state.MaxHull)
	logger.Printf("  Cargo: %d/%d items", len(state.Ship.Cargo), state.MaxCargo)
}
