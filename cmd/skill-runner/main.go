// Command skill-runner is the unified agent binary that runs YAML-defined
// skills and Go strategies. It replaces the individual auto-* binaries with
// a single entry point driven by personality configs and CLI flags.
package main

import (
	"context"
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
	"github.com/rsned/spacemolt/pkg/strategy"
)

func main() {
	agentID := flag.String("agent", "", "Agent ID (required, e.g. miner-1)")
	skillFlag := flag.String("skill", "", "Comma-separated skill/strategy names (overrides personality)")
	once := flag.Bool("once", false, "Run the strategy once then exit")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *agentID == "" {
		fmt.Fprintln(os.Stderr, "Usage: skill-runner --agent <agent-id> [--skill name1,name2] [--once] [--debug]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", *agentID), log.LstdFlags)

	// Load personality from agent directory.
	personalityPath := filepath.Join("data", "agents", *agentID, "personality.json")
	personality, err := agent.LoadPersonalityJSON(personalityPath)
	if err != nil {
		logger.Printf("Warning: could not load personality: %v", err)
		// Continue with empty personality; skill flag is required in this case.
	}

	// Determine which skills to run.
	var skillNames []string
	if *skillFlag != "" {
		for s := range strings.SplitSeq(*skillFlag, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				skillNames = append(skillNames, s)
			}
		}
	} else if personality.PrimarySkill != "" {
		skillNames = []string{personality.PrimarySkill}
	}

	if len(skillNames) == 0 {
		log.Fatal("No skills specified: use --skill flag or set primary_skill in personality.json")
	}

	// Load YAML skill registry.
	yamlRegistry, err := skills.LoadRegistry("data/skills")
	if err != nil {
		log.Fatalf("Failed to load skill registry: %v", err)
	}

	// Create unified registry (Go strategies + YAML skills).
	registry := strategy.NewUnifiedRegistry(yamlRegistry)

	// Validate all skill names exist.
	for _, name := range skillNames {
		if !registry.Has(name) {
			log.Fatalf("Unknown skill or strategy: %q (available: %s)", name, strings.Join(registry.Names(), ", "))
		}
	}

	logger.Printf("Skills: %s", strings.Join(skillNames, ", "))

	// Set up signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	// Connect to game server.
	var client game.GameClient
	switch *transport {
	case "mcp":
		logger.Printf("Using MCP transport")
		client, _, err = game.InitializeMCPAgent(*agentID, logger, ctx, *debug, false)
		if err != nil {
			log.Fatalf("Failed to initialize MCP agent: %v", err)
		}
	case "ws":
		logger.Printf("Using WebSocket transport")
		client, _, err = game.InitializeAgent(*agentID, logger, ctx, *debug)
		if err != nil {
			log.Fatalf("Failed to initialize agent: %v", err)
		}
	default:
		log.Fatalf("Unknown transport: %s (must be: ws, mcp)", *transport)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			logger.Printf("Warning: failed to close client: %v", closeErr)
		}
	}()

	// Create dispatcher for YAML skills.
	dispatcher := skills.NewClientDispatcher(client, "data", *agentID, logger)
	dispatcher.EnsureSystemData(ctx)

	// Build primary strategy from skill names.
	primary, err := buildStrategy(skillNames, registry, yamlRegistry, dispatcher, logger)
	if err != nil {
		log.Fatalf("Failed to build strategy: %v", err)
	}

	// Optionally wrap with CompositeStrategy for background skill.
	runnable := primary
	if personality.BackgroundSkill != "" && registry.Has(personality.BackgroundSkill) {
		bgSkill, bgErr := buildStrategy(
			[]string{personality.BackgroundSkill},
			registry, yamlRegistry, dispatcher, logger,
		)
		if bgErr != nil {
			logger.Printf("Warning: failed to build background skill %q: %v", personality.BackgroundSkill, bgErr)
		} else {
			runnable = strategy.NewCompositeStrategy(primary, bgSkill, strategy.CompositeConfig{
				CleanupTimeout: 30 * time.Second,
				Logger:         logger,
			})
			logger.Printf("Background skill: %s", personality.BackgroundSkill)
		}
	}

	// Write captain's log at start.
	_ = game.WriteCaptainsLog(*agentID, &game.AgentLog{
		AgentName:   personality.Name,
		CurrentGoal: fmt.Sprintf("Skill runner started: %s", strings.Join(skillNames, ", ")),
		Location:    "starting",
		Notes:       []string{"Starting skill-runner"},
	})

	cfg := strategy.Config{
		AgentID:    *agentID,
		Logger:     logger,
		Parameters: make(map[string]string),
	}

	// Main loop with error recovery.
	const (
		maxFailures     = 3
		failureWindow   = 5 * time.Minute
		backoffDuration = game.SleepLong
	)

	var failures []time.Time

	for ctx.Err() == nil {
		logger.Printf("Running strategy: %s", runnable.Name())
		runErr := runnable.Run(ctx, client, cfg)

		if ctx.Err() != nil {
			// Graceful shutdown requested.
			break
		}

		if runErr != nil {
			logger.Printf("Strategy error: %v", runErr)

			// Track failure for backoff.
			now := time.Now()
			failures = append(failures, now)

			// Prune old failures outside the window.
			cutoff := now.Add(-failureWindow)
			pruned := failures[:0]
			for _, t := range failures {
				if t.After(cutoff) {
					pruned = append(pruned, t)
				}
			}
			failures = pruned

			if len(failures) >= maxFailures {
				logger.Printf("Too many failures (%d in %v), backing off for %v",
					len(failures), failureWindow, backoffDuration)
				select {
				case <-time.After(backoffDuration):
				case <-ctx.Done():
				}
			} else {
				select {
				case <-time.After(game.SleepReconnect):
				case <-ctx.Done():
				}
			}
			continue
		}

		// Strategy completed successfully.
		if *once {
			logger.Printf("Strategy completed (--once mode), exiting.")
			break
		}

		logger.Printf("Strategy completed, restarting after cooldown...")
		select {
		case <-time.After(game.SleepShort):
		case <-ctx.Done():
		}
	}

	// Write captain's log at stop.
	_ = game.WriteCaptainsLog(*agentID, &game.AgentLog{
		AgentName:   personality.Name,
		CurrentGoal: "Skill runner stopped",
		Location:    "shutdown",
		Notes:       []string{"Stopping skill-runner"},
	})

	logger.Printf("Shutdown complete.")
}

// buildStrategy creates a Strategy from the given skill names. A single name
// produces a single strategy; multiple names produce a ChainStrategy.
func buildStrategy(
	names []string,
	registry *strategy.UnifiedRegistry,
	yamlRegistry *skills.Registry,
	dispatcher *skills.ClientDispatcher,
	logger *log.Logger,
) (strategy.Strategy, error) {
	steps := make([]strategy.Strategy, 0, len(names))

	for _, name := range names {
		if registry.IsGoStrategy(name) {
			s, err := registry.Resolve(name)
			if err != nil {
				return nil, fmt.Errorf("resolving Go strategy %q: %w", name, err)
			}
			steps = append(steps, s)
		} else {
			// YAML skill - wrap in SkillStrategy.
			desc := fmt.Sprintf("YAML skill: %s", name)
			if skill := yamlRegistry.Get(name); skill != nil && skill.Description != "" {
				desc = skill.Description
			}
			steps = append(steps, strategy.NewSkillStrategy(strategy.SkillStrategyConfig{
				SkillName:   name,
				Description: desc,
				Registry:    yamlRegistry,
				Dispatcher:  dispatcher,
				Logger:      logger,
			}))
		}
	}

	if len(steps) == 1 {
		return steps[0], nil
	}

	chainName := strings.Join(names, "+")
	return strategy.NewChainStrategy(chainName, steps...), nil
}
