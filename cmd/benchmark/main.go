package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)
	metrics := NewMetrics()
	reporter := NewReporter(metrics, cfg, logger)
	orchestrator := NewOrchestrator(cfg, metrics, logger)

	// Context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Println("Shutting down gracefully...")
		cancel()
	}()

	// Start periodic reporter
	go reporter.RunPeriodic(ctx)

	// Run benchmarks for each backend
	backends := cfg.backends()
	for _, backend := range backends {
		if ctx.Err() != nil {
			break
		}
		logger.Printf("=== Running benchmark: %s ===", backend)
		if err := orchestrator.Run(ctx, backend); err != nil {
			logger.Printf("Backend %s error: %v", backend, err)
		}
	}

	// Final summary
	reporter.PrintFinal()

	// Export if configured
	if err := reporter.Export(); err != nil {
		logger.Printf("Export error: %v", err)
	}
}
