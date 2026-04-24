# Plan: Incorporate log/slog Structured Logging into Spacemolt

## Context

The Spacemolt codebase currently uses Go's standard `log` package with manual log level prefixes ("OK:", "Error:", "Warning:"). This approach has several limitations:

- **No runtime log level filtering** - all logs are always shown
- **No structured data** - logs are unstructured text strings
- **Manual prefix parsing** - log aggregators can't filter by level
- **No compile-time safety** - any string can be used as a prefix
- **Debug vs production** - can't reduce verbosity in production

**Current State:**
- 200+ log.Printf calls in pkg/game/client.go alone
- 50+ functions take `*log.Logger` parameters across multiple packages
- Manual prefix patterns throughout: "OK:", "Error:", "Warning:", "✓", "✗"
- No external logging libraries - only standard library `log` package
- Some tools have `--debug` flag for binary on/off control
- Consistent dependency injection pattern established

**Goal:** Integrate Go's standard library `log/slog` package (Go 1.21+) to provide:
- Configurable log levels (debug, info, warn, error)
- Structured key-value logging for better observability
- Zero-allocation disabled logging for performance
- Backward compatibility during migration
- No external dependencies (slog is stdlib)

## Recommended Approach: Phased Migration with Dual Support

### Strategy Overview

**Dual Field Pattern:** Add new `slogLogger` fields alongside existing `logger`/`debugLogger` fields, allowing gradual migration without breaking changes.

**Configuration Hierarchy:** Flags > Environment Variables > Config File > Defaults

**Output Formats:** Text for development (human-readable), JSON for production (log aggregation)

## Phase 1: Foundation (2-3 days)

**Goal:** Create infrastructure for slog without breaking any existing code

### Files to Create/Modify:

1. **`pkg/logging/logging.go`** - NEW FILE
   - Create centralized logging package
   - Implement logger factory with configuration support
   - Support flags, env vars, and YAML config
   - Provide text/JSON handler selection

   ```go
   package logging

   import (
       "log/slog"
       "os"
       "strconv"
   )

   // Config holds logging configuration
   type Config struct {
       Level      string // debug, info, warn, error
       Format     string // text, json
       AddSource  bool   // include file:line
       Attrs      map[string]any // global attributes
   }

   // New creates a configured slog.Logger
   func New(cfg Config) *slog.Logger {
       // Parse level from env or config
       level := parseLevel(os.Getenv("SPACEMOLT_LOG_LEVEL"), cfg.Level)

       // Choose handler based on format
       var handler slog.Handler
       opts := &slog.HandlerOptions{
           Level: level,
           AddSource: cfg.AddSource,
       }

       if os.Getenv("SPACEMOLT_ENV") == "production" || cfg.Format == "json" {
           handler = slog.NewJSONHandler(os.Stdout, opts)
       } else {
           handler = slog.NewTextHandler(os.Stdout, opts)
       }

       logger := slog.New(handler)

       // Add global attributes
       if len(cfg.Attrs) > 0 {
           attrs := make([]any, 0, len(cfg.Attrs)*2)
           for k, v := range cfg.Attrs {
               attrs = append(attrs, k, v)
           }
           logger = logger.With(attrs...)
       }

       return logger
   }

   func parseLevel(env, fallback string) slog.Level {
       // Parse env var or fallback
       levelStr := env
       if levelStr == "" {
           levelStr = fallback
       }
       // Default to info if invalid
       switch levelStr {
       case "debug":
           return slog.LevelDebug
       case "info":
           return slog.LevelInfo
       case "warn", "warning":
           return slog.LevelWarn
       case "error":
           return slog.LevelError
       default:
           return slog.LevelInfo
       }
   }

   // NewWithFlags creates logger from command-line flags
   func NewWithFlags(level, format string) *slog.Logger {
       return New(Config{
           Level: level,
           Format: format,
           AddSource: false,
       })
   }
   ```

2. **`pkg/unified/config.go`** - MODIFY
   - Add `LoggingConfig` struct
   - Add `Logging` field to `Config` struct
   - Update `DefaultConfig()` with logging defaults

   ```go
   type LoggingConfig struct {
       Level      string            `yaml:"level" json:"level"`               // debug, info, warn, error
       Format     string            `yaml:"format" json:"format"`             // text, json
       AddSource  bool              `yaml:"add_source" json:"add_source"`     // include file:line
       Attrs      map[string]any    `yaml:"attrs" json:"attrs"`               // global attributes
   }

   type Config struct {
       // ... existing fields ...
       Logging LoggingConfig `yaml:"logging" json:"logging"`
   }

   func DefaultConfig() Config {
       return Config{
           // ... existing defaults ...
           Logging: LoggingConfig{
               Level:     "info",
               Format:    "text",
               AddSource: false,
               Attrs: map[string]any{
                   "service": "spacemolt",
               },
           },
       }
   }
   ```

3. **`spacemolt-server.yaml`** - MODIFY
   - Add logging configuration section

   ```yaml
   logging:
     level: info      # debug, info, warn, error
     format: text     # text, json
     add_source: false # include file:line in logs
     attrs:
       service: spacemolt
   ```

4. **`pkg/unified/server.go`** - MODIFY
   - Import new `pkg/logging` package
   - Replace `log.New()` with `logging.New(cfg.Logging)`
   - Add `slogLogger *slog.Logger` field alongside existing `logger *log.Logger`
   - Use slog for new log statements

   ```go
   type Server struct {
       // ... existing fields ...
       logger    *log.Logger
       slogLogger *slog.Logger  // NEW: structured logger
   }

   func New(cfg Config) (*Server, error) {
       // Old logger (keep for backward compat)
       logger := log.New(os.Stdout, "[spacemolt] ", log.LstdFlags|log.Lmsgprefix)

       // New structured logger
       slogLogger := logging.New(cfg.Logging)

       return &Server{
           // ... existing fields ...
           logger:     logger,
           slogLogger: slogLogger,
       }, nil
   }
   ```

### Testing:
- Run `go build ./...` - should compile
- Run unified server with different log levels
- Verify text vs JSON output based on SPACEMOLT_ENV
- Check that existing functionality is unchanged

### Success Criteria:
- Zero breaking changes
- New logging package is usable
- Configuration works via YAML and env vars
- All existing tests pass

---

## Phase 2: High-Value Tools (3-4 days)

**Goal:** Migrate high-usage interactive tools to demonstrate slog benefits

### Files to Modify:

1. **`cmd/tools/play_as/main.go`** - MODIFY
   - Add `--log-level` and `--log-format` flags
   - Replace `log.New()` with `logging.NewWithFlags()`
   - Add `slogLogger *slog.Logger` alongside existing `logger *log.Logger`
   - Migrate all log calls to use slog levels

   ```go
   var (
       logLevel   = flag.String("log-level", "info", "Log level: debug, info, warn, error")
       logFormat  = flag.String("log-format", "text", "Log format: text, json")
       debug      = flag.Bool("debug", false, "Enable debug logging")
   )

   func main() {
       flag.Parse()

       // Create slog logger
       slogLogger := logging.NewWithFlags(*logLevel, *logFormat)
       slogLogger = slogLogger.With("agent", agentID, "component", "play_as")

       // Keep old logger for backward compat with InitializeAgent
       logger := log.New(os.Stdout, fmt.Sprintf("[PLAY_AS-%s] ", agentID), log.LstdFlags)

       // Example migration:
       // OLD: logger.Printf("OK: Action 'deposit_items' pending.")
       // NEW: slogLogger.Info("Action pending", "action", "deposit_items", "status", "pending")
   }

   // Migration examples in command handlers:
   func handleMine(parts []string, client game.GameClient, logger *slog.Logger) {
       // OLD: logger.Printf("Starting mining at %s", asteroid)
       // NEW: logger.Info("Starting mining", "asteroid", asteroidID, "system", systemID)
   }
   ```

2. **`cmd/tools/faction-join/main.go`** - MODIFY
   - Add `--log-level` flag
   - Replace logger creation with slog
   - Migrate log statements

3. **`cmd/tools/register-agent/main.go`** - MODIFY
   - Add `--log-level` flag
   - Replace logger creation with slog
   - Migrate log statements

### Testing:
- Run tools with different `--log-level` values
- Verify debug logs are hidden when `--log-level=info`
- Verify JSON output with `--log-format=json`
- Check backward compatibility (tools still work)

### Success Criteria:
- Tools respect log level flags
- JSON output is valid and parseable
- No breaking changes to tool functionality
- Developers can reduce log noise in production

---

## Phase 3: Core Client Migration (5-7 days)

**Goal:** Migrate pkg/game/client.go to structured logging (149 log calls!)

### Files to Modify:

1. **`pkg/game/client.go`** - MODIFY
   - Add `slogLogger *slog.Logger` field to Client struct
   - Add `SetSlogLogger()` method
   - Migrate debugLogger.Printf calls in batches (5 batches of ~30 calls each)

   ```go
   type Client struct {
       // ... existing fields ...
       debugLogger *log.Logger   // OLD: Keep unchanged
       slogLogger  *slog.Logger  // NEW: Structured logger
   }

   func (c *Client) SetSlogLogger(logger *slog.Logger) {
       c.slogLogger = logger
   }

   // Migration pattern:
   // OLD: c.debugLogger.Printf("Connected to %s", c.url)
   // NEW:
   func (c *Client) logConnection() {
       if c.slogLogger != nil {
           c.slogLogger.Info("Connected", "url", c.url, "connection_id", c.connectionID)
       } else {
           c.debugLogger.Printf("Connected to %s", c.url)
       }
   }
   ```

2. **`pkg/game/agent.go`** - MODIFY
   - Add `slogLogger *slog.Logger` field to SimpleHandler
   - Update `OnMessage()` to use structured logging
   - Create `InitializeAgentWithSlog()` for new agents

   ```go
   type SimpleHandler struct {
       Client    *Client
       Logger    *log.Logger   // OLD: Keep unchanged
       SlogLogger *slog.Logger // NEW: Structured logger
   }

   func (h *SimpleHandler) OnMessage(resp protocol.Response) {
       switch resp.Type {
       case protocol.TypeOK:
           if msg, ok := resp.Payload["message"].(string); ok {
               if h.SlogLogger != nil {
                   h.SlogLogger.Info("Action successful", "message", msg)
               } else {
                   h.Logger.Printf("OK: %s", msg)
               }
           }
       case protocol.TypeError:
           if msg, ok := resp.Payload["message"].(string); ok {
               if h.SlogLogger != nil {
                   h.SlogLogger.Error("Action failed", "message", msg)
               } else {
                   h.Logger.Printf("Error: %s", msg)
               }
           }
       case protocol.TypeMiningYield:
           if h.SlogLogger != nil {
               h.SlogLogger.Info("Mining yield",
                   "resource", resp.Payload["resource"],
                   "quantity", resp.Payload["quantity"],
                   "system", resp.Payload["system_id"],
               )
           }
       }
   }
   ```

3. **`pkg/game/interface.go`** - MODIFY
   - Update InitializeAgent to accept optional slog logger
   - Add InitializeAgentWithSlog() function signature

### Testing:
- Run agent with slog logger enabled
- Verify all game events are logged with structured data
- Check backward compatibility (agents work without slog)
- Performance test (< 5% overhead)

### Success Criteria:
- All 149 log calls migrated or have dual support
- Game events are queryable in JSON format
- Backward compatible with old code
- Performance overhead minimal

---

## Phase 4: Package Migration (7-10 days)

**Goal:** Migrate pkg/agent/, pkg/strategy/, and related packages

### Files to Modify:

1. **`pkg/agent/manager.go`** - MODIFY
   - Add `slogDebugLogger *slog.Logger` field
   - Update all DebugLogger.Printf calls
   - Maintain backward compatibility

2. **`pkg/agent/runner.go`** - MODIFY
   - Add `slogLogger *slog.Logger` field
   - Update agent decision logging
   - Add structured context for agent actions

3. **`pkg/strategy/*.go`** - MODIFY (multiple files)
   - Update logging in mining.go, explorer.go, fighter.go, etc.
   - Add structured data for strategy decisions
   - Maintain emoji-based logging for user-friendly output

4. **`pkg/game/mining.go`, `pkg/game/crafting_loop.go`** - MODIFY
   - Update function signatures to accept slog.Logger
   - Add structured logging for operations
   - Keep backward compat with *log.Logger

   ```go
   // Pattern for updating function signatures:
   // OLD:
   func MiningLoop(client GameClient, logger *log.Logger, ctx context.Context, config *MiningLoopConfig)

   // NEW (dual support):
   func MiningLoop(client GameClient, logger interface{}, ctx context.Context, config *MiningLoopConfig) (*MiningLoopResult, error) {
       // Determine which logger type we have
       var slogLogger *slog.Logger
       var stdLogger *log.Logger

       switch l := logger.(type) {
       case *slog.Logger:
           slogLogger = l
       case *log.Logger:
           stdLogger = l
       }

       // Use appropriate logger
       if slogLogger != nil {
           slogLogger.Info("Mining cycle started", "config", config)
       } else {
           stdLogger.Printf("Mining cycle started")
       }
   }
   ```

### Testing:
- Run agents with strategies
- Verify structured decision logs
- Check backward compatibility
- Test with both log types

### Success Criteria:
- All packages use slog or support both types
- Agent decisions are logged with structured data
- Backward compatible with existing code
- No performance regression

---

## Phase 5: Final Migration (5-7 days)

**Goal:** Complete migration of remaining tools and cleanup

### Files to Modify:

1. **Remaining `cmd/tools/*.go`** - MODIFY
   - Migrate claim-code, facility-check, agent-status, etc.
   - Add `--log-level` flags to all tools
   - Update logging patterns

2. **Create migration helper script** - NEW FILE
   - `scripts/migrate-to-slog.sh`
   - Automate common migration patterns
   - Search and replace for common log patterns

3. **Update documentation** - MODIFY
   - Update CLAUDE.md with slog usage
   - Add logging guide to docs/
   - Document configuration options

4. **Cleanup** - MODIFY
   - Remove old log.New() calls where safe
   - Update function signatures to use slog.Logger
   - Deprecate dual-field pattern in favor of slog-only

### Testing:
- Full test suite: `go test ./...`
- Linting: `golangci-lint run ./...`
- Manual testing of all tools
- Integration tests with real game server

### Success Criteria:
- All packages migrated to slog
- Zero breaking changes remaining
- All tests pass
- No new lint findings
- Documentation complete

---

## Configuration Approach

### Hierarchy: Flags > Environment > Config File > Defaults

```bash
# Flags (highest priority)
play_as explorer-1 --log-level=debug --log-format=json

# Environment variables (medium priority)
export SPACEMOLT_LOG_LEVEL=debug
export SPACEMOLT_LOG_FORMAT=json
export SPACEMOLT_ENV=production  # Auto-sets format=json

# Config file (low priority)
# spacemolt-server.yaml:
# logging:
#   level: info
#   format: text

# Defaults (lowest priority)
# level: info, format: text
```

### Supported Log Levels:
- `debug` - Detailed diagnostics (development)
- `info` - Normal operational messages (default)
- `warn` - Unexpected but recoverable situations
- `error` - Errors that need attention (production)

### Supported Formats:
- `text` - Human-readable key=value pairs (development)
- `json` - Machine-readable JSON logs (production/log aggregation)

---

## Backward Compatibility Strategy

### Dual Field Pattern
```go
type SomeStruct struct {
    logger     *log.Logger   // OLD: Keep for backward compat
    slogLogger *slog.Logger  // NEW: Add for structured logs
}

// Use appropriate logger
func (s *SomeStruct) LogEvent(msg string) {
    if s.slogLogger != nil {
        s.slogLogger.Info(msg, "key", "value")
    } else {
        s.logger.Printf("%s: key=%s", msg, "value")
    }
}
```

### Function Signature Updates
```go
// Keep old signature, add new optional parameter
func OldFunc(client GameClient, logger *log.Logger, ctx context.Context)
func NewFunc(client GameClient, logger interface{}, ctx context.Context)  // Accepts either

// Or use overload
func InitializeAgent(agentID string, logger *log.Logger, ctx context.Context, debug bool) (*Client, *Credentials, error)
func InitializeAgentWithSlog(agentID string, slogLogger *slog.Logger, ctx context.Context, debug bool) (*Client, *Credentials, error)
```

---

## Verification & Testing

### Unit Tests:
- Test logger factory with all config combinations
- Test log level parsing
- Test JSON vs text output
- Test backward compatibility

### Integration Tests:
- Run agents with slog enabled
- Verify structured data in logs
- Test log level filtering
- Test JSON parsing in production mode

### Performance Tests:
- Benchmark slog vs log overhead
- Verify < 5% performance impact
- Test with high-volume logging (mining loops)

### Manual Testing:
```bash
# Test log levels
play_as explorer-1 --log-level=debug   # Show all logs
play_as explorer-1 --log-level=error   # Show only errors

# Test formats
play_as explorer-1 --log-format=text   # Human-readable
play_as explorer-1 --log-format=json   # JSON logs

# Test environment
export SPACEMOLT_LOG_LEVEL=warn
play_as explorer-1

# Test production mode
export SPACEMOLT_ENV=production
play_as explorer-1  # Auto-uses JSON
```

---

## Rollback Plan

If issues arise:
1. **Phase 1-2**: Easy rollback - remove new logging package, restore old code
2. **Phase 3**: Keep dual field pattern, disable slog logger
3. **Phase 4-5**: Incremental rollback per package

**Rollback triggers:**
- Performance degradation > 10%
- Breaking changes in production
- Data loss or corruption
- Critical bugs with no workaround

---

## Success Criteria

- ✅ Zero breaking changes to existing code
- ✅ All tools work with all log levels
- ✅ JSON logs are valid and parseable
- ✅ No new golangci-lint findings
- ✅ Performance overhead < 5%
- ✅ All tests pass
- ✅ Documentation complete
- ✅ Configuration via flags, env vars, and YAML

---

## Timeline

- **Phase 1**: 2-3 days (Foundation)
- **Phase 2**: 3-4 days (High-value tools)
- **Phase 3**: 5-7 days (Core client)
- **Phase 4**: 7-10 days (Package migration)
- **Phase 5**: 5-7 days (Final migration)

**Total: 22-31 days (4-6 weeks)**

**Note:** Each phase is independently valuable. You can stop after any phase and still have working improvements.

---

## Critical Files Reference

### New Files:
- `pkg/logging/logging.go` - Central logging package
- `scripts/migrate-to-slog.sh` - Migration helper script
- `docs/slog-integration-guide.md` - Integration guide (already exists!)

### Modified Files (Priority Order):
1. `pkg/unified/config.go` - Add logging config
2. `pkg/unified/server.go` - Use slog logger
3. `cmd/tools/play_as/main.go` - Add log level flags
4. `pkg/game/agent.go` - Update SimpleHandler
5. `pkg/game/client.go` - Add slogLogger field (149 calls!)
6. `pkg/agent/manager.go` - Update agent logging
7. `pkg/agent/runner.go` - Update runner logging
8. `pkg/strategy/*.go` - Update strategy logging
9. `pkg/game/mining.go`, `crafting_loop.go` - Update function signatures
10. Remaining `cmd/tools/*.go` - Migrate tools

---

## Next Steps (When Approved)

1. Start with Phase 1 (Foundation)
2. Create `pkg/logging/logging.go`
3. Update `pkg/unified/config.go`
4. Test configuration via flags, env vars, YAML
5. Demonstrate log level filtering works
6. Get feedback before proceeding to Phase 2
