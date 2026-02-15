package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/registry"
)

func main() {
	// Command-line flags
	port := flag.Int("port", 8081, "Status registry HTTP port")
	dataDir := flag.String("data-dir", "data/registry", "Data directory (for persistence, future)")
	cleanupInterval := flag.Duration("cleanup-interval", 30*time.Second, "Interval between cleanup tasks")
	heartbeatTimeout := flag.Duration("heartbeat-timeout", 15*time.Second, "Heartbeat timeout before removing inactive tools")

	flag.Parse()

	log.Println("=== SpaceMolt Status Registry ===")
	log.Printf("Port: %d", *port)
	log.Printf("Data directory: %s", *dataDir)

	// Create registry server
	reg := registry.NewServer(*port)
	log.Printf("✓ Registry server created")

	// Start cleanup task for inactive tools
	reg.StartCleanupTask(*cleanupInterval, *heartbeatTimeout)
	log.Printf("✓ Cleanup task started (interval: %v, timeout: %v)", *cleanupInterval, *heartbeatTimeout)

	// Start HTTP server in background
	go func() {
		if err := reg.Start(); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	log.Println("=== Status Registry Running ===")
	log.Printf("Listening on http://localhost:%d", *port)
	log.Println("Endpoints:")
	log.Println("  POST   /api/tools/register         - Register a tool")
	log.Println("  GET    /api/tools                  - List all tools")
	log.Println("  GET    /api/tools/{id}             - Get tool details")
	log.Println("  POST   /api/tools/{id}/heartbeat  - Send heartbeat")
	log.Println("  GET    /api/status/stream         - SSE status stream")
	log.Println("")

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigCh
	log.Println("\n=== Shutting Down ===")

	// Graceful shutdown
	shutdownCtx, cancel := timeoutContext(5 * time.Second)
	defer cancel()

	if err := reg.Shutdown(shutdownCtx); err != nil {
		log.Printf("Warning: Shutdown error: %v", err)
	}

	log.Println("Goodbye!")
}

// timeoutContext creates a context with timeout
func timeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
