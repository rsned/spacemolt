// Example: Integrating slog into play_as/main.go
// This shows how to add configurable log levels to the existing tool
package main

import (
	"flag"
	"log/slog"
	"os"
)

// Example 1: Add --log-level flag to control verbosity
var logLevel string

func init() {
	flag.StringVar(&logLevel, "log-level", "info", "Log level: debug, info, warn, error")
}

// Example 2: Create a logger factory that respects the flag
func createLogger(agentID string) *slog.Logger {
	// Parse log level from flag
	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Choose handler based on environment
	// JSON format in production (for log aggregation), text in dev
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: level,
		// AddSource: true, // Uncomment to include file:line in logs
	}

	if os.Getenv("SPACEMOLT_ENV") == "production" {
		// Production: JSON output for log aggregation
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		// Development: Human-readable text output
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	// Create logger with agent-specific context
	logger := slog.New(handler).With(
		"agent", agentID,
		"component", "play_as",
	)

	return logger
}

// Example 3: Updating SimpleHandler in pkg/game/agent.go to use slog
type SlogHandler struct {
	Logger *slog.Logger
}

// Integration with existing MessageHandler interface
// This replaces the manual "OK:", "Error:", "Warning:" prefixes
/*
func (h *SlogHandler) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.Logger.Info("Action successful", "message", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.Logger.Error("Action failed", "message", msg)
		}
	case protocol.TypeActionError:
		if msg, ok := resp.Payload["message"].(string); ok {
			h.Logger.Error("Action error", "message", msg)
		}
	case protocol.TypePirateWarning:
		h.Logger.Warn("Pirate detected", "system", resp.Payload["system_id"])
	case protocol.TypeMiningYield:
		// Structured data makes queries like "show all iron_ore yields" easy
		h.Logger.Info("Mining yield",
			"resource", resp.Payload["resource"],
			"quantity", resp.Payload["quantity"],
			"system", resp.Payload["system_id"],
		)
	}
}
*/

// Example 4: Conditional debug logging (only enabled when --log-level=debug)
func exampleDebugUsage(logger *slog.Logger) {
	// This only appears when --log-level=debug
	logger.Debug("Parsing market response", "bytes", 4096)

	// This appears at --log-level=info or lower
	logger.Info("Connected to game server", "endpoint", "wss://game.spacemolt.com/ws")

	// This appears at --log-level=warn or lower
	logger.Warn("High memory usage", "allocated_mb", 512)

	// This always appears
	logger.Error("Connection failed", "error", "timeout")
}

// Example 5: Using context for request tracing
func exampleWithContext(logger *slog.Logger) {
	// Using context for request-scoped logging (useful for distributed tracing)
	logger.Info("Processing game tick",
		"request_id", "req-123",
		"tick", 45678,
	)

	logger.Debug("Agent decision",
		"action", "mine",
		"target", "asteroid_123",
		"confidence", 0.95,
	)
}

// Example 6: Creating child loggers for subsystems
func exampleChildLoggers(baseLogger *slog.Logger) {
	// Each subsystem gets its own logger with specific attributes
	miningLogger := baseLogger.With("subsystem", "mining")
	tradingLogger := baseLogger.With("subsystem", "trading")
	combatLogger := baseLogger.With("subsystem", "combat")

	// All logs from miningLogger include subsystem=mining
	miningLogger.Info("Starting mining cycle", "asteroid", "ast-123")
	miningLogger.Debug("Yield calculated", "expected_yield", 150)

	// All logs from tradingLogger include subsystem=trading
	tradingLogger.Info("Market snapshot received", "listings", 42)
	tradingLogger.Warn("Low profit margin", "margin_percent", 2.3)

	// All logs from combatLogger include subsystem=combat
	combatLogger.Error("Hull critical", "hull_percent", 15)
}

func main() {
	// Parse flags
	flag.Parse()

	logger := createLogger("explorer-1")

	logger.Info("play_as tool starting", "log_level", logLevel)

	exampleDebugUsage(logger)
	exampleWithContext(logger)
	exampleChildLoggers(logger)

	// Example: Parse existing code patterns
	logger.Info("Action pending",
		"action", "deposit_items",
		"tick", 12345,
		"reason", "waiting for execution window",
	)

	logger.Warn("Failed to open knowledge base",
		"path", "/data/kb.sqlite",
		"error", "file not found",
		"fallback", "in-memory mode",
	)

	logger.Error("Connection lost",
		"reason", "timeout",
		"duration_sec", 30,
		"reconnect_attempt", 3,
	)

	logger.Info("Example complete", "next_step", "try running with --log-level=debug")
}
