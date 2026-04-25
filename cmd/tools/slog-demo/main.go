// Slog demonstration - comparing std log vs log/slog
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
)

func currentLogging() {
	// Current approach in play_as/main.go
	logger := log.New(os.Stdout, "[PLAY_AS-explorer-1] ", log.LstdFlags)

	// Manual log level prefixes
	logger.Printf("Initializing agent explorer-1 via ws transport...")
	logger.Printf("OK: Action 'deposit_items' pending. Will execute on next tick.")
	logger.Printf("Warning: Failed to open knowledge base at /path/to/db: file not found")
	logger.Printf("Error closing client: connection reset by peer")

	fmt.Println("\n--- Current approach: No log level filtering, all messages always shown ---")
}

func slogBasic() {
	// slog with text handler (human-readable, similar to std log)
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo, // Set minimum log level
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))

	logger.Info("Initializing agent", "agent_id", "explorer-1", "transport", "ws")
	logger.Info("Action pending", "action", "deposit_items", "status", "will execute on next tick")
	logger.Warn("Failed to open knowledge base", "path", "/path/to/db", "error", "file not found")
	logger.Error("Failed to close client", "error", "connection reset by peer")

	fmt.Println("\n--- slog: Structured key-value pairs, built-in log levels ---")
}

func slogJSON() {
	// slog with JSON handler (machine-readable, great for log aggregation)
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))

	logger.Info("Agent connected", "agent_id", "explorer-1", "empire", "Iron Republic")
	logger.Error("Mining failed", "system", "Alpha Centauri", "resource", "iron_ore", "error", "asteroid depleted")

	fmt.Println("\n--- slog JSON: Perfect for log aggregation systems ---")
}

func slogWithLevel() {
	// Demonstrating log level filtering
	opts := &slog.HandlerOptions{
		Level: slog.LevelWarn, // Only show Warn and Error, hide Info/Debug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))

	logger.Debug("This won't be shown", "reason", "below minimum level")
	logger.Info("This won't be shown either", "reason", "below minimum level")
	logger.Warn("This WILL be shown", "reason", "meets minimum level")
	logger.Error("This WILL be shown", "reason", "meets minimum level")

	fmt.Println("\n--- Log levels: Debug < Info < Warn < Error ---")
}

func slogContext() {
	// slog supports context for trace propagation
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx := context.Background()

	// Add request/trace ID to all logs in a context
	logger.InfoContext(ctx, "Action completed",
		"action", "mine",
		"yield", 150,
		"system", "Proxima Centauri",
		"duration_ms", 1250,
	)

	fmt.Println("\n--- slog supports context for distributed tracing ---")
}

func slogCustomAttrs() {
	// Add global attributes to every log entry
	opts := &slog.HandlerOptions{
		AddSource: false, // Set to true to include file:line number
	}
	handler := slog.NewTextHandler(os.Stdout, opts)
	logger := slog.New(handler)

	// Create a logger with pre-set attributes
	agentLogger := logger.With(
		"agent_id", "explorer-1",
		"session_id", "abc-123-def",
		"transport", "websocket",
	)

	// All logs from agentLogger automatically include these fields
	agentLogger.Info("Connected to game server")
	agentLogger.Warn("High latency detected", "latency_ms", 450)
	agentLogger.Error("Connection lost", "reason", "timeout")

	fmt.Println("\n--- slog.With() adds attributes to all subsequent logs ---")
}

func slogCustomLevel() {
	// Custom log levels for game-specific needs
	// Game server levels: Trace < Debug < Info < Warn < Error
	const (
		LevelTrace = slog.Level(-8) // Below Debug
	)

	opts := &slog.HandlerOptions{
		Level: LevelTrace, // Show everything including trace
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))

	logger.Log(context.Background(), LevelTrace, "Game tick received", "tick", 12345)
	logger.Debug("Processing action", "action", "mine", "target", "asteroid_123")
	logger.Info("Action queued", "action", "mine", "position", 3)

	fmt.Println("\n--- slog supports custom log levels ---")
}

func migrationExample() {
	fmt.Println("=== MIGRATION: Current code → slog ===")

	// BEFORE (current)
	fmt.Println("// Before (current)")
	const before = `logger := log.New(os.Stdout, "[PLAY_AS-explorer-1] ", log.LstdFlags)
logger.Printf("OK: Action 'deposit_items' pending. Will execute on next tick.")
logger.Printf("Warning: Failed to open knowledge base at %s: %v", dbPath, err)
`
	_, _ = os.Stdout.WriteString(before)

	fmt.Println("\n// After (with slog)")
	fmt.Println(`logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	Level: slog.LevelInfo,
})).With("agent_id", "explorer-1")

logger.Info("Action pending", "action", "deposit_items", "status", "will execute on next tick")
logger.Warn("Failed to open knowledge base", "path", dbPath, "error", err)`)

	fmt.Println("\n=== Benefits of migration ===")
	fmt.Println("✓ Configurable log levels (can disable debug/trace in production)")
	fmt.Println("✓ Structured data (key-value pairs for search/filter in log aggregators)")
	fmt.Println("✓ Type-safe (compile-time checking of log level usage)")
	fmt.Println("✓ Context support (for distributed tracing)")
	fmt.Println("✓ No allocation overhead when logging is disabled")
	fmt.Println("✓ Standard library (no external dependencies)")
}

func main() {
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("          Go slog (Structured Logging) Demo")
	fmt.Println("═══════════════════════════════════════════════════════")

	currentLogging()
	slogBasic()
	slogJSON()
	slogWithLevel()
	slogContext()
	slogCustomAttrs()
	slogCustomLevel()
	migrationExample()
}
