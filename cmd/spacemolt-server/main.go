package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/unified"
)

func main() {
	configPath := flag.String("config", "spacemolt-server.yaml", "Path to configuration file")
	port := flag.Int("port", 0, "Override HTTP port from config")
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

	// Apply CLI overrides.
	if *port > 0 {
		cfg.Server.HTTPPort = *port
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
