package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/unified"
)

func main() {
	// Config file
	configPath := flag.String("config", "spacemolt-server.yaml", "Path to configuration file")

	// Common CLI overrides — these take precedence over the YAML config.
	port := flag.Int("port", 0, "Override HTTP port from config")
	agents := flag.String("agents", "", "Comma-separated list of agent IDs to start")
	agentsDir := flag.String("agents-dir", "", "Directory containing agent personalities")
	maxAgents := flag.Int("max-agents", 0, "Maximum number of concurrent agents")
	decisionInterval := flag.Duration("decision-interval", 0, "Decision interval for agents (e.g., 11s)")
	serverURL := flag.String("server-url", "", "Game server WebSocket URL")
	transport := flag.String("transport", "", "Agent transport: ws (default) or mcp")
	llmURL := flag.String("llm-url", "", "LLM server URL (Ollama)")
	llmModel := flag.String("llm-model", "", "LLM model name")
	dbBackend := flag.String("db-backend", "", "Knowledge base backend: sqlite or memory")
	dbPath := flag.String("db-path", "", "Path to SQLite database")
	credsBackend := flag.String("creds-backend", "", "Credentials backend: file, sqlite, or keyring")
	credsPath := flag.String("creds-path", "", "Path for credentials storage")
	registryURL := flag.String("registry-url", "", "Status registry URL (e.g., http://localhost:8081)")

	flag.Parse()

	log.Println("=== Spacemolt Unified Server ===")

	// Load configuration.
	cfg, err := unified.LoadConfig(*configPath)
	if err != nil {
		// If config file doesn't exist, use defaults.
		if os.IsNotExist(err) {
			log.Printf("config file %q not found, using defaults", *configPath)
			cfg = unified.DefaultConfig()
		} else {
			log.Fatalf("failed to load config: %v", err)
		}
	} else {
		log.Printf("loaded config from %s", *configPath)
	}

	// Apply CLI overrides (only when explicitly set).
	if *port > 0 {
		cfg.Server.HTTPPort = *port
	}
	if *agents != "" {
		var ids []string
		for _, s := range strings.Split(*agents, ",") {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				ids = append(ids, trimmed)
			}
		}
		cfg.Agents.Enabled = ids
	}
	if *agentsDir != "" {
		cfg.Agents.Dir = *agentsDir
	}
	if *maxAgents > 0 {
		cfg.Agents.Max = *maxAgents
	}
	if *decisionInterval > 0 {
		cfg.Agents.DecisionInterval = *decisionInterval
	}
	if *serverURL != "" {
		cfg.Game.ServerURL = *serverURL
	}
	if *transport != "" {
		cfg.Game.Transport = *transport
	}
	if *llmURL != "" {
		cfg.LLM.URL = *llmURL
	}
	if *llmModel != "" {
		cfg.LLM.Model = *llmModel
	}
	if *dbBackend != "" {
		cfg.Database.Backend = *dbBackend
	}
	if *dbPath != "" {
		cfg.Database.Path = *dbPath
	}
	if *credsBackend != "" {
		cfg.Credentials.Backend = *credsBackend
	}
	if *credsPath != "" {
		cfg.Credentials.Path = *credsPath
	}
	if *registryURL != "" {
		cfg.Server.RegistryURL = *registryURL
	}

	// Create unified server.
	srv, err := unified.New(cfg)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	// Spawn configured agents.
	ctx := context.Background()
	if len(cfg.Agents.Enabled) > 0 {
		log.Println("\n=== Spawning Agents ===")
		success, failed := srv.SpawnConfiguredAgents(ctx)
		log.Printf("started %d/%d agents", success, len(cfg.Agents.Enabled))
		if len(failed) > 0 {
			log.Printf("failed agents: %v", failed)
		}
	}

	// Start HTTP server.
	if err := srv.Start(); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}

	log.Printf("server listening on :%d", cfg.Server.HTTPPort)
	fmt.Println("\nPress Ctrl+C to stop")

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Println("\n=== Shutting Down ===")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}

	log.Println("Goodbye!")
}
